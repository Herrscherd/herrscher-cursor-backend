package cursor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestPromptDeliveredOnStdinNotArgv(t *testing.T) {
	const (
		content = "distinctive prompt: do not leak this in argv"
		memory  = "recalled context: secret-memory-marker"
		model   = "test-model"
	)

	stub := writeCursorStub(t)
	prompt := contracts.Prompt{
		Content:     content,
		Context:     memory,
		Attachments: []string{"/tmp/distinctive-attachment.png"},
	}
	expectedStdin := withContext(memory, withAttachments(content, prompt.Attachments))

	for _, tc := range []struct {
		name   string
		kind   string
		format string
	}{
		{name: "oneshot", kind: "oneshot", format: "json"},
		{name: "stream", kind: "stream", format: "stream-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argvPath := filepath.Join(t.TempDir(), "argv")
			if err := os.Setenv("CURSOR_STUB_ARGV", argvPath); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Unsetenv("CURSOR_STUB_ARGV") })

			backend, err := NewBackend(context.Background(), Config{
				Kind:  tc.kind,
				Cmd:   stub,
				Model: model,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := backend.Respond(context.Background(), prompt, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantResult := fmt.Sprintf("stdin-proof:%d:%x", len(expectedStdin), sha256Bytes(expectedStdin))
			if got != wantResult {
				t.Fatalf("result = %q, want %q (stdin was not delivered as expected)", got, wantResult)
			}

			argvBytes, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			argv := strings.Fields(string(argvBytes))
			for _, arg := range argv {
				if strings.Contains(arg, content) || strings.Contains(arg, memory) {
					t.Fatalf("prompt data leaked into argv: %q", arg)
				}
			}
			assertContainsArgs(t, argv, "-p", "--output-format", tc.format, "--model", model)
		})
	}
}

func writeCursorStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "cursor-agent-stub")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CURSOR_STUB_ARGV"
stdin=$(cat)
length=$(printf '%s' "$stdin" | wc -c | tr -d ' ')
hash=$(printf '%s' "$stdin" | sha256sum | cut -d ' ' -f 1)
printf '{"type":"result","subtype":"success","result":"stdin-proof:%s:%s"}\n' "$length" "$hash"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func sha256Bytes(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

func assertContainsArgs(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, expected := range want {
		found := false
		for _, actual := range got {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("argv = %v, missing %q", got, expected)
		}
	}
}

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
