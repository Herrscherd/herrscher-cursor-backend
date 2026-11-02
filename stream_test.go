package cursor

import (
	"errors"
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
