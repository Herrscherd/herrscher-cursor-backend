package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errPipeBroken = errors.New("pipe broke")

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) {
	return 0, errPipeBroken
}

func TestDrainPipeReturnsReadError(t *testing.T) {
	if err := drainPipe(brokenReader{}); !errors.Is(err, errPipeBroken) {
		t.Fatalf("drainPipe error = %v, want %v", err, errPipeBroken)
	}
}

func TestDrainPipeConsumesEverything(t *testing.T) {
	if err := drainPipe(strings.NewReader("left over output")); err != nil {
		t.Fatalf("drainPipe error = %v, want nil", err)
	}
}

func TestStreamOutcomeReturnsDrainError(t *testing.T) {
	err := streamOutcome(turnResult{Text: "ok"}, nil, errPipeBroken, nil)
	if !errors.Is(err, errPipeBroken) {
		t.Fatalf("streamOutcome error = %v, want %v", err, errPipeBroken)
	}
}

func TestRespondReturnsDrainError(t *testing.T) {
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: writeStreamStub(t, "0")})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := backend.(*streamResponder)
	if !ok {
		t.Fatalf("backend is %T, want *streamResponder", backend)
	}
	r.drain = func(io.Reader) error { return errPipeBroken }

	_, err = r.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if !errors.Is(err, errPipeBroken) {
		t.Fatalf("Respond error = %v, want %v", err, errPipeBroken)
	}
}

func TestRespondWithoutDrainSeamUsesPipeDrain(t *testing.T) {
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: writeStreamStub(t, "0")})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := backend.(*streamResponder)
	if !ok {
		t.Fatalf("backend is %T, want *streamResponder", backend)
	}
	r.drain = nil

	got, err := r.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if err != nil {
		t.Fatalf("Respond error = %v, want nil", err)
	}
	if got != "ok" {
		t.Fatalf("Respond text = %q, want ok", got)
	}
}

func TestRespondStopsChildAfterOverlongLine(t *testing.T) {
	stub := writeStubScript(t, fmt.Sprintf("cat > /dev/null\nhead -c %d /dev/zero\nexec sleep 600", 2*maxStreamLine))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	backend, err := NewBackend(ctx, Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	_, err = backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	if _, ok := errors.AsType[*LineTooLongError](err); !ok {
		t.Fatalf("err = %v, want a line too long error", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Respond waited for the child until the deadline: %v", ctx.Err())
	}
}

func TestResumeTokenReturnsWhileTurnRuns(t *testing.T) {
	stub := writeStubScript(t, `cat > /dev/null
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-2"}\n'`)
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: stub, ResumeID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := backend.(*streamResponder)
	if !ok {
		t.Fatalf("backend is %T, want *streamResponder", backend)
	}

	var during string
	tokens := make(chan string, 1)
	_, err = r.Respond(t.Context(), contracts.Prompt{Content: "hi"}, func(event contracts.BackendEvent) {
		if event.Kind != "text" {
			return
		}
		go func() { tokens <- r.ResumeToken() }()
		select {
		case during = <-tokens:
		case <-time.After(5 * time.Second):
			t.Fatal("ResumeToken blocked behind the running turn")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if during != "sess-1" {
		t.Fatalf("ResumeToken during the turn = %q, want the previous sess-1", during)
	}
	if got := r.ResumeToken(); got != "sess-2" {
		t.Fatalf("ResumeToken after the turn = %q, want sess-2", got)
	}
}
