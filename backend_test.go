package cursor

import (
	"bufio"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestCursorArgv(t *testing.T) {
	got := cursorArgv([]string{"cursor-agent"}, "gpt-5", "sess-1")
	want := []string{"cursor-agent", "-p", "--output-format", "stream-json", "--model", "gpt-5", "--resume", "sess-1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestParseResult(t *testing.T) {
	got := parseResult(`{"type":"result","subtype":"success","result":"done","session_id":"s"}`)
	if got != "done" {
		t.Fatalf("result = %q, want done", got)
	}
}

func TestWithAttachments(t *testing.T) {
	got := withAttachments("look", []string{"/tmp/a.png", "/tmp/b.png"})
	want := "look\n\n[Image jointe : /tmp/a.png]\n[Image jointe : /tmp/b.png]"
	if got != want {
		t.Fatalf("attachments = %q, want %q", got, want)
	}
}

func TestReadTurn(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]},"session_id":"sess-1"}`,
		`{"type":"tool_call","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"sess-1"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Hello","session_id":"sess-1"}`,
	}, "\n") + "\n"

	var events []contracts.BackendEvent
	tr, err := readTurn(bufio.NewReader(strings.NewReader(input)), func(e contracts.BackendEvent) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "Hello" || tr.SessionID != "sess-1" || tr.IsError {
		t.Fatalf("turn = %+v", tr)
	}
	want := []contracts.BackendEvent{
		{Kind: "text", Detail: "Hello"},
		{Kind: "tool", Tool: "readToolCall", Detail: "README.md"},
		{Kind: "result", IsError: false},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event[%d] = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestNewBackendSelection(t *testing.T) {
	b, err := NewBackend(nil, Config{Kind: "stream", Cmd: "cursor-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*streamResponder); !ok {
		t.Fatalf("backend = %T, want *streamResponder", b)
	}
}
