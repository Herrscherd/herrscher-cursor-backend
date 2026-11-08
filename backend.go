// Package cursor runs the Cursor Agent CLI as a herrscher backend.
package cursor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// BackendKind selects how a turn reaches Cursor Agent. The zero value means the
// kind is not chosen: [NewBackend] then derives it from [Config.Stream].
type BackendKind uint8

// The closed set of backend kinds.
const (
	BackendKindOneShot BackendKind = iota + 1
	BackendKindStream
)

const (
	oneShotKindText = "oneshot"
	streamKindText  = "stream"
	defaultCmd      = "cursor-agent"
	maxStreamLine   = 16 << 20
)

const maxOneShotOutput int64 = maxStreamLine

// UnknownBackendKindError reports a kind that is neither oneshot nor stream.
type UnknownBackendKindError struct {
	Kind string
}

func (e *UnknownBackendKindError) Error() string {
	return fmt.Sprintf("unknown backend kind %q", e.Kind)
}

// ParseBackendKind reads the text form of a BackendKind, "oneshot" or "stream".
//
// It returns an [*UnknownBackendKindError] for any other text, the empty text
// included: the zero value has no text form that parses back.
func ParseBackendKind(text string) (BackendKind, error) {
	switch text {
	case oneShotKindText:
		return BackendKindOneShot, nil
	case streamKindText:
		return BackendKindStream, nil
	}
	return 0, &UnknownBackendKindError{Kind: text}
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

// MarshalText writes the text form of k. The zero value writes empty text, which
// [BackendKind.UnmarshalText] reads back as the zero value.
func (k BackendKind) MarshalText() ([]byte, error) {
	switch k {
	case 0:
		return nil, nil
	case BackendKindOneShot, BackendKindStream:
		return []byte(k.String()), nil
	}
	return nil, &UnknownBackendKindError{Kind: k.String()}
}

// UnmarshalText reads the text form of a kind. Empty text sets the zero value,
// any other text goes through [ParseBackendKind].
func (k *BackendKind) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*k = 0
		return nil
	}
	parsed, err := ParseBackendKind(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}

const (
	eventKindTool     = "tool"
	cursorEventResult = "result"
)

func resolveBackend(config Config) BackendKind {
	switch {
	case config.Kind != 0:
		return config.Kind
	case config.Stream:
		return BackendKindStream
	}
	return BackendKindOneShot
}

// NewBackend builds a configured Cursor Agent backend. Config.Cmd is split on
// spaces, and an empty Cmd runs cursor-agent. It does not start Cursor Agent.
//
// It returns an [*UnknownBackendKindError] when Config.Kind is neither zero nor
// a defined kind.
func NewBackend(_ context.Context, config Config) (contracts.Backend, error) {
	base := baseCommand(config.Cmd)
	stderr := diagnosticsOutput(config)
	kind := resolveBackend(config)
	switch kind {
	case BackendKindOneShot:
		return &oneShotResponder{base: base, model: config.Model, dir: config.Dir, stderr: stderr, setDeadline: setReadDeadline}, nil
	case BackendKindStream:
		return &streamResponder{base: base, model: config.Model, dir: config.Dir, stderr: stderr, drain: drainPipe, setDeadline: setReadDeadline, session: config.ResumeID}, nil
	}
	return nil, &UnknownBackendKindError{Kind: kind.String()}
}

func diagnosticsOutput(config Config) io.Writer {
	if config.Verbose {
		return os.Stderr
	}
	return nil
}

type oneShotResponder struct {
	base        []string
	model       string
	dir         string
	stderr      io.Writer
	setDeadline stdoutDeadline
}

// OutputTooLargeError reports a oneshot turn whose stdout is longer than Limit
// bytes. The child is killed, its output is not read further.
type OutputTooLargeError struct {
	Limit int64
}

func (e *OutputTooLargeError) Error() string {
	return fmt.Sprintf("cursor output is longer than %d bytes", e.Limit)
}

func (o *oneShotResponder) Respond(ctx context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	proc, err := startTurn(runCtx, baseArgv(o.base, "json", o.model, ""), o.dir, turnInput(prompt), o.stderr, o.setDeadline)
	if err != nil {
		return "", fmt.Errorf("cursor start: %w", err)
	}
	stdout, readErr := readOutput(proc, stop, func(out io.Reader) ([]byte, error) {
		return readBounded(out, maxOneShotOutput)
	})
	waitErr := errors.Join(proc.wait(), proc.closeStdout())
	if readErr != nil {
		return "", fmt.Errorf("cursor exec failed: %w", readErr)
	}
	if waitErr != nil {
		return parseResult(string(stdout)), fmt.Errorf("cursor exec failed: %w", waitErr)
	}
	return parseResult(string(stdout)), nil
}

func readBounded(pipe io.Reader, limit int64) ([]byte, error) {
	data, readErr := io.ReadAll(io.LimitReader(pipe, limit+1))
	if err := ignoreOwnCut(readErr); err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &OutputTooLargeError{Limit: limit}
	}
	return data, nil
}

func (o *oneShotResponder) Close() error { return nil }

func turnInput(p contracts.Prompt) string {
	return withContext(p.Context, withAttachments(p.Content, p.Attachments))
}

func turnCommand(ctx context.Context, argv []string, dir string, stderr io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	configureChild(cmd)
	cmd.Dir = dir
	cmd.Stderr = stderr
	return cmd
}

func parseResult(out string) string {
	var event cursorEvent
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &event) == nil && event.Result != "" {
		return event.Result
	}
	return strings.TrimSpace(out)
}

func baseCommand(cmd string) []string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return []string{defaultCmd}
	}
	return fields
}

func cursorArgv(base []string, model, resume string) []string {
	return baseArgv(base, "stream-json", model, resume)
}

func baseArgv(base []string, format, model, resume string) []string {
	argv := slices.Clone(base)
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

// LineTooLongError reports a line of the stream longer than the reader accepts.
type LineTooLongError struct {
	Limit int
}

func (e *LineTooLongError) Error() string {
	return fmt.Sprintf("cursor stream line is longer than %d bytes", e.Limit)
}

// StreamEndedError reports that the stream ended before its result event.
// Unparsable counts the non-empty lines that were not JSON, and Cause is the
// read error that ended the stream.
type StreamEndedError struct {
	Unparsable int
	Cause      error
}

func (e *StreamEndedError) Error() string {
	return fmt.Sprintf("cursor stream ended after %d unparsable line(s): %v", e.Unparsable, e.Cause)
}

func (e *StreamEndedError) Unwrap() error {
	return e.Cause
}

func readTurn(reader *bufio.Reader, onEvent func(contracts.BackendEvent)) (turnResult, error) {
	var skipped int
	for {
		line, err := readLine(reader)
		if _, isTooLong := errors.AsType[*LineTooLongError](err); isTooLong {
			return turnResult{}, err
		}
		if len(line) > 0 {
			outcome := consumeLine(line, onEvent)
			if outcome.isDone {
				return outcome.turn, nil
			}
			if outcome.isUnparsable {
				skipped++
			}
		}
		if err != nil {
			return turnResult{}, &StreamEndedError{Unparsable: skipped, Cause: err}
		}
	}
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > maxStreamLine {
			return nil, &LineTooLongError{Limit: maxStreamLine}
		}
		line = append(line, chunk...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

type lineOutcome struct {
	turn         turnResult
	isDone       bool
	isUnparsable bool
}

func consumeLine(line []byte, onEvent func(contracts.BackendEvent)) lineOutcome {
	var event cursorEvent
	if json.Unmarshal(line, &event) != nil {
		return lineOutcome{isUnparsable: len(bytes.TrimSpace(line)) != 0}
	}
	turn, isDone := applyEvent(event, onEvent)
	return lineOutcome{turn: turn, isDone: isDone}
}

func applyEvent(event cursorEvent, onEvent func(contracts.BackendEvent)) (turnResult, bool) {
	switch event.Type {
	case "assistant":
		emitAssistantText(event, onEvent)
	case "tool_call":
		if event.Subtype == "started" {
			emitToolCall(event, onEvent)
		}
	case cursorEventResult:
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
