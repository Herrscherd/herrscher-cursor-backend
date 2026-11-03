package cursor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPromptDeliveredOnStdinNotArgv(t *testing.T) {
	const (
		content = "distinctive prompt: do not leak this in argv"
		memory  = "recalled context: secret-memory-marker"
		model   = "test-model"
	)

	stub := writeCursorStub(t)
	attachment := "/tmp/distinctive-attachment.png"
	prompt := contracts.Prompt{
		Content:     content,
		Context:     memory,
		Attachments: []string{attachment},
	}
	expectedStdin := withContext(memory, withAttachments(content, prompt.Attachments))

	for _, tc := range []struct {
		name   string
		kind   BackendKind
		format string
	}{
		{name: "oneshot", kind: BackendKindOneShot, format: "json"},
		{name: "stream", kind: BackendKindStream, format: "stream-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, argv := runStubTurn(t, tc.kind, stub, prompt, model)
			wantResult := fmt.Sprintf("stdin-proof:%d:%x", len(expectedStdin), sha256Bytes(expectedStdin))
			if got != wantResult {
				t.Fatalf("result = %q, want %q (stdin was not delivered as expected)", got, wantResult)
			}
			assertNoPromptInArgv(t, argv, content, memory, attachment)
			assertContainsArgs(t, argv, "-p", "--output-format", tc.format, "--model", model)
		})
	}
}

func runStubTurn(t *testing.T, kind BackendKind, stub string, prompt contracts.Prompt, model string) (string, []string) {
	t.Helper()
	argvPath := filepath.Join(t.TempDir(), "argv")
	t.Setenv("CURSOR_STUB_ARGV", argvPath)

	backend, err := NewBackend(t.Context(), Config{Kind: kind, Cmd: stub, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backend.Respond(t.Context(), prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	return got, splitArgv(argvBytes)
}

func splitArgv(record []byte) []string {
	return strings.Split(strings.TrimSuffix(string(record), "\x00"), "\x00")
}

const promptWindow = 8

func assertNoPromptInArgv(t *testing.T, argv []string, secrets ...string) {
	t.Helper()
	joined := strings.Join(argv, " ")
	for _, secret := range secrets {
		if strings.Contains(joined, secret) {
			t.Fatalf("prompt data leaked into argv: %q", joined)
		}
		for _, word := range strings.Fields(secret) {
			if len(word) > 3 && strings.Contains(joined, word) {
				t.Fatalf("prompt word %q leaked into argv: %q", word, joined)
			}
		}
		for start := 0; start+promptWindow <= len(secret); start++ {
			window := secret[start : start+promptWindow]
			if strings.Contains(joined, window) {
				t.Fatalf("prompt fragment %q leaked into argv: %q", window, joined)
			}
		}
	}
}

func writeCursorStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "cursor-agent-stub")
	script := `#!/bin/sh
printf '%s\0' "$@" > "$CURSOR_STUB_ARGV"
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
		if fmt.Sprintf("%+v", events[i]) != fmt.Sprintf("%+v", want[i]) {
			t.Fatalf("event[%d] = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestNewBackendSelection(t *testing.T) {
	b, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: "cursor-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*streamResponder); !ok {
		t.Fatalf("backend = %T, want *streamResponder", b)
	}
}

func TestReadTurnWithoutResultReportsContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		skipped int
	}{
		{name: "empty", input: "", skipped: 0},
		{name: "banner only", input: "update available\n", skipped: 1},
		{name: "banner and truncated json", input: "update available\n{\"type\":\"assis\n", skipped: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readTurn(bufio.NewReader(strings.NewReader(tc.input)), nil)
			if !errors.Is(err, io.EOF) {
				t.Fatalf("err = %v, want wrapped io.EOF", err)
			}
			ended, ok := errors.AsType[*StreamEndedError](err)
			if !ok {
				t.Fatalf("err = %v, want a stream ended error", err)
			}
			if ended.Unparsable != tc.skipped {
				t.Fatalf("unparsable = %d, want %d", ended.Unparsable, tc.skipped)
			}
		})
	}
}

func TestStreamRespondKeepsSessionAndWaitError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		exitCode string
		wantExit bool
	}{
		{name: "clean exit", exitCode: "0"},
		{name: "non zero exit", exitCode: "3", wantExit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := writeStreamStub(t, tc.exitCode)
			backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: stub})
			if err != nil {
				t.Fatal(err)
			}
			r, ok := backend.(*streamResponder)
			if !ok {
				t.Fatalf("backend is %T, want *streamResponder", backend)
			}
			_, err = r.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
			assertTurnError(t, err, tc.wantExit)
			if got := r.ResumeToken(); got != "sess-9" {
				t.Fatalf("ResumeToken = %q, want sess-9", got)
			}
		})
	}
}

func writeStreamStub(t *testing.T, exitCode string) string {
	t.Helper()
	return writeStubScript(t, "cat > /dev/null\nprintf '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"ok\",\"session_id\":\"sess-9\"}\\n'\nexit "+exitCode)
}

func writeStubScript(t *testing.T, body string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func assertTurnError(t *testing.T, err error, wantExit bool) {
	t.Helper()
	if !wantExit {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Fatalf("err = %v, want an exit error", err)
	}
}

func TestParseBackendKind(t *testing.T) {
	for _, text := range []string{"oneshot", "stream"} {
		kind, err := ParseBackendKind(text)
		if err != nil {
			t.Fatalf("ParseBackendKind(%q): %v", text, err)
		}
		if kind.String() != text {
			t.Fatalf("ParseBackendKind(%q).String() = %q", text, kind.String())
		}
	}
	kind, err := ParseBackendKind("batch")
	if err == nil {
		t.Fatalf("ParseBackendKind accepted an unknown kind as %v", kind)
	}
	if _, ok := errors.AsType[*UnknownBackendKindError](err); !ok {
		t.Fatalf("err = %v, want an unknown backend kind error", err)
	}
}

func TestReadTurnRefusesOverlongLine(t *testing.T) {
	input := strings.Repeat("x", maxStreamLine+1)

	_, err := readTurn(bufio.NewReader(strings.NewReader(input)), nil)
	tooLong, ok := errors.AsType[*LineTooLongError](err)
	if !ok {
		t.Fatalf("err = %v, want a line too long error", err)
	}
	if tooLong.Limit != maxStreamLine {
		t.Fatalf("limit = %d, want %d", tooLong.Limit, maxStreamLine)
	}
}

func TestStreamRespondRefusesOverlongLine(t *testing.T) {
	stub := writeStubScript(t, `cat > /dev/null
head -c 17000000 /dev/zero | tr '\0' 'x'
printf '\n'`)
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if _, ok := errors.AsType[*LineTooLongError](err); !ok {
		t.Fatalf("err = %v, want a line too long error", err)
	}
}

func TestOneShotRefusesOutputPastTheCap(t *testing.T) {
	stub := writeStubScript(t, "cat > /dev/null\nexec head -c 1073741824 /dev/zero")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	backend, err := NewBackend(ctx, Config{Kind: BackendKindOneShot, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	_, err = backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	tooLarge, ok := errors.AsType[*OutputTooLargeError](err)
	if !ok {
		t.Fatalf("err = %v, want an output too large error", err)
	}
	if tooLarge.Limit != maxOneShotOutput {
		t.Fatalf("limit = %d, want %d", tooLarge.Limit, maxOneShotOutput)
	}
	if ctx.Err() != nil {
		t.Fatalf("Respond waited for the child until the deadline: %v", ctx.Err())
	}
}

func TestExecErrorDoesNotCarryStderr(t *testing.T) {
	const secret = "token=sk-do-not-log"
	for _, tc := range []struct {
		name string
		kind BackendKind
	}{
		{name: "oneshot", kind: BackendKindOneShot},
		{name: "stream", kind: BackendKindStream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := writeStubScript(t, `cat > /dev/null
printf '{"type":"result","subtype":"success","result":"ok","session_id":"sess-9"}\n'
printf '`+secret+`\n' >&2
exit 3`)
			backend, err := NewBackend(t.Context(), Config{Kind: tc.kind, Cmd: stub})
			if err != nil {
				t.Fatal(err)
			}
			_, err = backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
			exitErr, ok := errors.AsType[*exec.ExitError](err)
			if !ok {
				t.Fatalf("err = %v, want an exit error", err)
			}
			if len(exitErr.Stderr) != 0 {
				t.Fatalf("ExitError.Stderr = %q, want empty", exitErr.Stderr)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error text carries stderr: %v", err)
			}
		})
	}
}

func FuzzConsumeLine(f *testing.F) {
	for _, line := range []string{
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]},"session_id":"sess-1"}`,
		`{"type":"tool_call","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"sess-1"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Hello","session_id":"sess-1"}`,
		"update available",
		`{"type":"assis`,
	} {
		f.Add([]byte(line))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		outcome := consumeLine(line, func(contracts.BackendEvent) {})
		if !outcome.isDone && outcome.turn != (turnResult{}) {
			t.Fatalf("line %q yields a turn without ending the turn: %+v", line, outcome.turn)
		}
	})
}
