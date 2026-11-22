package cursor

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxOpenToolCalls           = 256
	maxSeenToolCalls           = 4096
	maxCallIDBytes             = 256
	maxToolErrorRunes          = 200
	toolResultSuccess          = "success"
	toolResultFailure          = "failure"
	toolResultTimeout          = "timeout"
	toolResultRejected         = "rejected"
	toolResultSpawnError       = "spawnError"
	toolResultPermissionDenied = "permissionDenied"
	shellToolName              = "shellToolCall"
)

var wholeNumber = regexp.MustCompile(`^-?(0|[1-9]\d*)$`)

type toolCallTracker struct {
	open      map[string]string
	seen      map[string]struct{}
	openBound boundReport
	seenBound boundReport
}

type boundReport struct {
	name     string
	limit    int
	reported bool
}

func (t *toolCallTracker) begin(callID, tool string) bool {
	if len(callID) > maxCallIDBytes {
		return false
	}
	if _, known := t.seen[callID]; known {
		return false
	}
	if len(t.seen) >= maxSeenToolCalls {
		t.seenBound.warnOnce()
		return false
	}
	if t.seen == nil {
		t.seen = make(map[string]struct{})
		t.open = make(map[string]string)
		t.openBound = boundReport{name: "max_open", limit: maxOpenToolCalls}
		t.seenBound = boundReport{name: "max_seen", limit: maxSeenToolCalls}
	}
	t.seen[callID] = struct{}{}
	if tool == "" {
		return false
	}
	if len(t.open) >= maxOpenToolCalls {
		t.openBound.warnOnce()
		return false
	}
	t.open[callID] = tool
	return true
}

func (b *boundReport) warnOnce() {
	if b.reported {
		return
	}
	b.reported = true
	slog.Warn("tool calls reach a bound of the turn, later starts carry no lifecycle", "bound", b.name, "limit", b.limit)
}

func (t *toolCallTracker) finish(callID string) (string, bool) {
	tool, open := t.open[callID]
	delete(t.open, callID)
	return tool, open
}

type toolEntry struct {
	name string
	body json.RawMessage
}

func singleTool(call map[string]json.RawMessage) (toolEntry, bool) {
	if len(call) != 1 {
		return toolEntry{}, false
	}
	for name, body := range call {
		return toolEntry{name: name, body: body}, true
	}
	return toolEntry{}, false
}

type toolResult struct {
	state  contracts.ToolCallState
	reason string
}

func toolCallResult(call map[string]json.RawMessage) (toolResult, bool) {
	entry, ok := singleTool(call)
	if !ok {
		return toolResult{}, false
	}
	var parsed struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(entry.body, &parsed) != nil || len(parsed.Result) == 0 {
		return toolResult{}, false
	}
	for outcome, value := range parsed.Result {
		if outcome != toolResultSuccess {
			return toolResult{state: contracts.ToolCallFailed, reason: failureReason(entry.name, outcome, value)}, true
		}
	}
	return toolResult{state: contracts.ToolCallSucceeded}, true
}

func failureReason(tool, outcome string, value json.RawMessage) string {
	if tool == shellToolName && isShellOutcome(outcome) {
		return boundRunes(firstLine(orDefault(shellFailureText(outcome, value), outcome)))
	}
	return boundRunes(firstLine(orDefault(failureText(value), outcome)))
}

func isShellOutcome(outcome string) bool {
	switch outcome {
	case toolResultFailure, toolResultTimeout, toolResultRejected, toolResultSpawnError, toolResultPermissionDenied:
		return true
	}
	return false
}

func shellFailureText(outcome string, value json.RawMessage) string {
	fields, isObject := decodeFields(value)
	if !isObject {
		return ""
	}
	switch outcome {
	case toolResultFailure:
		code, ok := fields.whole("exitCode")
		if !ok {
			return ""
		}
		return exitCodeText(code, firstLine(fields.text("stderr")))
	case toolResultTimeout:
		milliseconds, ok := fields.whole("timeoutMs")
		if !ok {
			return ""
		}
		return "timeout after " + milliseconds + "ms"
	case toolResultRejected:
		return "rejected: " + orDefault(fields.text("reason"), "unknown reason")
	case toolResultSpawnError:
		return "spawn error: " + fields.text("error")
	case toolResultPermissionDenied:
		return "permission denied: " + fields.text("error")
	}
	return ""
}

func exitCodeText(code, stderrLine string) string {
	if stderrLine == "" {
		return "exit code " + code
	}
	return "exit code " + code + " (" + stderrLine + ")"
}

func orDefault(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

func failureText(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	fields, isObject := decodeFields(value)
	if !isObject {
		return ""
	}
	for _, name := range []string{"message", "error", "stderr", "reason"} {
		if field := fields.text(name); field != "" {
			return field
		}
	}
	return ""
}

type rawFields map[string]json.RawMessage

func decodeFields(value json.RawMessage) (rawFields, bool) {
	var fields rawFields
	if json.Unmarshal(value, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func (f rawFields) text(name string) string {
	var text string
	if json.Unmarshal(f[name], &text) != nil {
		return ""
	}
	return text
}

func (f rawFields) whole(name string) (string, bool) {
	raw := f[name]
	if !wholeNumber.Match(raw) {
		return "", false
	}
	return string(raw), true
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

func boundRunes(text string) string {
	runes := []rune(text)
	if len(runes) > maxToolErrorRunes {
		runes = runes[:maxToolErrorRunes]
	}
	return string(runes)
}
