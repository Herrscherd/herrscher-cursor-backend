// Package cursor runs the Cursor Agent CLI as a herrscher backend.
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

// Config configures a Cursor Agent backend. A zero Kind is derived from Stream.
type Config struct {
	Kind     BackendKind
	Stream   bool
	Cmd      string
	Model    string
	Dir      string
	Verbose  bool
	ResumeID string
}

// BackendKind selects how a turn reaches Cursor Agent.
type BackendKind uint8

// The closed set of backend kinds.
const (
	BackendKindOneShot BackendKind = iota + 1
	BackendKindStream
)

const (
	oneShotKindText = "oneshot"
	streamKindText  = "stream"
)

// ParseBackendKind reads the text form of a BackendKind.
func ParseBackendKind(text string) (BackendKind, error) {
	switch text {
	case oneShotKindText:
		return BackendKindOneShot, nil
	case streamKindText:
		return BackendKindStream, nil
	}
	return 0, &unknownBackendKindError{kind: text}
}

func (k BackendKind) String() string {
	switch k {
	case BackendKindOneShot:
		return oneShotKindText
	case BackendKindStream:
		return streamKindText
	}
	return fmt.Sprintf("BackendKind(%d)", uint8(k))
}

const eventKindTool = "tool"

func resolveBackend(kind BackendKind, stream bool) BackendKind {
	if kind != 0 {
		return kind
	}
	if stream {
		return BackendKindStream
	}
	return BackendKindOneShot
}

// NewBackend builds a configured Cursor Agent backend.
func NewBackend(_ context.Context, config Config) (contracts.Backend, error) {
	kind := resolveBackend(config.Kind, config.Stream)
	base := streamBase(strings.Fields(config.Cmd))
	switch kind {
	case BackendKindOneShot:
		return &oneShotResponder{base: base, model: config.Model, dir: config.Dir, verbose: config.Verbose}, nil
	case BackendKindStream:
		return &streamResponder{base: base, model: config.Model, dir: config.Dir, verbose: config.Verbose, session: config.ResumeID}, nil
	}
	return nil, &unknownBackendKindError{kind: kind.String()}
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
	var event cursorEvent
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &event) == nil && event.Result != "" {
		return event.Result
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

func readTurn(reader *bufio.Reader, onEvent func(contracts.BackendEvent)) (turnResult, error) {
	var skipped int
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			outcome := consumeLine(line, onEvent)
			if outcome.done {
				return outcome.turn, nil
			}
			if outcome.unparsable {
				skipped++
			}
		}
		if err != nil {
			return turnResult{}, &streamEndedError{unparsable: skipped, cause: err}
		}
	}
}

type streamEndedError struct {
	unparsable int
	cause      error
}

func (e *streamEndedError) Error() string {
	return fmt.Sprintf("cursor stream ended after %d unparsable line(s): %v", e.unparsable, e.cause)
}

func (e *streamEndedError) Unwrap() error {
	return e.cause
}

type lineOutcome struct {
	turn       turnResult
	done       bool
	unparsable bool
}

func consumeLine(line []byte, onEvent func(contracts.BackendEvent)) lineOutcome {
	var event cursorEvent
	if json.Unmarshal(line, &event) != nil {
		return lineOutcome{unparsable: strings.TrimSpace(string(line)) != ""}
	}
	turn, done := applyEvent(event, onEvent)
	return lineOutcome{turn: turn, done: done}
}

func applyEvent(event cursorEvent, onEvent func(contracts.BackendEvent)) (turnResult, bool) {
	switch event.Type {
	case "assistant":
		emitAssistantText(event, onEvent)
	case "tool_call":
		if event.Subtype == "started" {
			emitToolCall(event, onEvent)
		}
	case "result":
		return finishTurn(event, onEvent), true
	}
	return turnResult{}, false
}

func emitAssistantText(event cursorEvent, onEvent func(contracts.BackendEvent)) {
	for _, block := range event.Message.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			emit(onEvent, contracts.BackendEvent{Kind: "text", Detail: block.Text})
		}
	}
}

func emitToolCall(event cursorEvent, onEvent func(contracts.BackendEvent)) {
	emit(onEvent, toolCallEvent(event.ToolCall))
}

func finishTurn(event cursorEvent, onEvent func(contracts.BackendEvent)) turnResult {
	turn := turnResult{Text: event.Result, SessionID: event.SessionID, IsError: event.IsError}
	if event.IsError {
		turn.ErrMsg = event.Result
	}
	emit(onEvent, contracts.BackendEvent{Kind: "result", IsError: event.IsError})
	return turn
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
			return contracts.BackendEvent{Kind: eventKindTool, Tool: name}
		}
		return contracts.BackendEvent{Kind: eventKindTool, Tool: name, Detail: toolCallSummary(body.Args)}
	}
	return contracts.BackendEvent{Kind: eventKindTool}
}

func toolCallSummary(args map[string]any) string {
	for _, key := range []string{"command", "path", "filePath", "pattern", "query", "url", "description"} {
		if value, ok := args[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
