package cursor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Config configures a Cursor Agent backend.
type Config struct {
	Kind     string
	Stream   bool
	Cmd      string
	Model    string
	Dir      string
	Verbose  bool
	ResumeID string
}

const (
	backendKindOneShot = "oneshot"
	backendKindStream  = "stream"
)

func resolveBackend(kind string, stream bool) string {
	if kind != "" {
		return kind
	}
	if stream {
		return backendKindStream
	}
	return "oneshot"
}

// NewBackend builds a configured Cursor Agent backend.
func NewBackend(_ context.Context, c Config) (contracts.Backend, error) {
	kind := resolveBackend(c.Kind, c.Stream)
	base := streamBase(strings.Fields(c.Cmd))
	switch kind {
	case backendKindOneShot:
		return &oneShotResponder{base: base, model: c.Model, dir: c.Dir, verbose: c.Verbose}, nil
	case backendKindStream:
		return &streamResponder{base: base, model: c.Model, dir: c.Dir, verbose: c.Verbose, session: c.ResumeID}, nil
	default:
		return nil, &unknownBackendKindError{kind: kind}
	}
}

type unknownBackendKindError struct {
	kind string
}

func (e *unknownBackendKindError) Error() string {
	return fmt.Sprintf("unknown backend kind %q", e.kind)
}

type oneShotResponder struct {
	base    []string
	model   string
	dir     string
	verbose bool
}

func (o *oneShotResponder) Respond(ctx context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	return runCmd(ctx, o.base, o.model, o.dir, o.verbose, p)
}

func (o *oneShotResponder) Close() error { return nil }

func runCmd(ctx context.Context, base []string, model, dir string, verbose bool, p contracts.Prompt) (string, error) {
	content := withContext(p.Context, withAttachments(p.Content, p.Attachments))
	argv := baseArgv(base, "json", model, "")
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = promptViaStdin(content)
	if verbose {
		cmd.Stderr = os.Stderr
	}
	out, err := cmd.Output()
	if err != nil {
		return parseResult(string(out)), fmt.Errorf("cursor exec failed: %w", err)
	}
	return parseResult(string(out)), nil
}

func parseResult(out string) string {
	var ev cursorEvent
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &ev) == nil && ev.Result != "" {
		return ev.Result
	}
	return strings.TrimSpace(out)
}

func streamBase(fields []string) []string {
	if len(fields) == 0 {
		return []string{"cursor-agent"}
	}
	return fields
}

func promptViaStdin(prompt string) io.Reader {
	return strings.NewReader(prompt)
}

func cursorArgv(base []string, model, resume string) []string {
	return baseArgv(base, "stream-json", model, resume)
}

func baseArgv(base []string, format, model, resume string) []string {
	b := streamBase(base)
	argv := slices.Clone(b)
	argv = append(argv, "-p", "--output-format", format)
	if model != "" {
		argv = append(argv, "--model", model)
	}
	if resume != "" {
		argv = append(argv, "--resume", resume)
	}
	return argv
}

type cursorEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	ToolCall map[string]json.RawMessage `json:"tool_call"`
}

func readTurn(r *bufio.Reader, onEvent func(contracts.BackendEvent)) (turnResult, error) {
	var skipped int
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			tr, done, unparsable := consumeLine(line, onEvent)
			if done {
				return tr, nil
			}
			if unparsable {
				skipped++
			}
		}
		if err != nil {
			return turnResult{}, fmt.Errorf("cursor stream ended after %d unparsable line(s): %w", skipped, err)
		}
	}
}

func consumeLine(line []byte, onEvent func(contracts.BackendEvent)) (turnResult, bool, bool) {
	var ev cursorEvent
	if json.Unmarshal(line, &ev) != nil {
		return turnResult{}, false, strings.TrimSpace(string(line)) != ""
	}
	tr, done := applyEvent(ev, onEvent)
	return tr, done, false
}

func applyEvent(ev cursorEvent, onEvent func(contracts.BackendEvent)) (turnResult, bool) {
	switch ev.Type {
	case "assistant":
		emitAssistantText(ev, onEvent)
	case "tool_call":
		if ev.Subtype == "started" {
			emitToolCall(ev, onEvent)
		}
	case "result":
		return finishTurn(ev, onEvent), true
	}
	return turnResult{}, false
}

func emitAssistantText(ev cursorEvent, onEvent func(contracts.BackendEvent)) {
	for _, block := range ev.Message.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			emit(onEvent, contracts.BackendEvent{Kind: "text", Detail: block.Text})
		}
	}
}

func emitToolCall(ev cursorEvent, onEvent func(contracts.BackendEvent)) {
	emit(onEvent, toolCallEvent(ev.ToolCall))
}

func finishTurn(ev cursorEvent, onEvent func(contracts.BackendEvent)) turnResult {
	tr := turnResult{Text: ev.Result, SessionID: ev.SessionID, IsError: ev.IsError}
	if ev.IsError {
		tr.ErrMsg = ev.Result
	}
	emit(onEvent, contracts.BackendEvent{Kind: "result", IsError: ev.IsError})
	return tr
}

func emit(onEvent func(contracts.BackendEvent), event contracts.BackendEvent) {
	if onEvent != nil {
		onEvent(event)
	}
}

type turnResult struct {
	Text      string
	SessionID string
	IsError   bool
	ErrMsg    string
}

func toolCallEvent(call map[string]json.RawMessage) contracts.BackendEvent {
	for name, raw := range call {
		var body struct {
			Args map[string]any `json:"args"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return contracts.BackendEvent{Kind: "tool", Tool: name}
		}
		return contracts.BackendEvent{Kind: "tool", Tool: name, Detail: toolCallSummary(body.Args)}
	}
	return contracts.BackendEvent{Kind: "tool"}
}

func toolCallSummary(args map[string]any) string {
	for _, key := range []string{"command", "path", "filePath", "pattern", "query", "url", "description"} {
		if value, ok := args[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
