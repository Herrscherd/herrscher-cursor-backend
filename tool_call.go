package cursor

import (
	"encoding/json"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxOpenToolCalls           = 256
	maxSeenToolCalls           = 4096
	maxCallIDBytes             = 256
	maxToolNameBytes           = 256
	maxToolErrorRunes          = 200
	toolResultSuccess          = "success"
	toolResultFailure          = "failure"
	toolResultTimeout          = "timeout"
	toolResultRejected         = "rejected"
	toolResultSpawnError       = "spawnError"
	toolResultPermissionDenied = "permissionDenied"
	shellToolName              = "shellToolCall"
)

var wholeNumber = regexp.MustCompile(`^(0|-?[1-9]\d*)$`)

type toolCallTracker struct {
	open              map[string]string
	seen              map[string]struct{}
	openBound         boundReport
	seenBound         boundReport
	malformedReported onceWarning
}

func (t *toolCallTracker) warnMalformedOnce() {
	if t.malformedReported.claim() {
		slog.Warn("tool call result carries more than one outcome or another tool than its start, the call stays started")
	}
}

type onceWarning struct {
	reported bool
}

func (w *onceWarning) claim() bool {
	if w.reported {
		return false
	}
	w.reported = true
	return true
}

type boundReport struct {
	name     string
	limit    int
	reported onceWarning
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
	if tool == "" || len(tool) > maxToolNameBytes {
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
	if b.reported.claim() {
		slog.Warn("tool calls reach a bound of the turn, later starts carry no lifecycle", "bound", b.name, "limit", b.limit)
	}
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

type resultReading int

const (
	resultAbsent resultReading = iota
	resultMalformed
	resultRead
)

func toolCallResult(call map[string]json.RawMessage, startTool string) (toolResult, resultReading) {
	entry, ok := singleTool(call)
	if !ok {
		return toolResult{}, resultAbsent
	}
	if entry.name != startTool {
		return toolResult{}, resultMalformed
	}
	var parsed struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(entry.body, &parsed) != nil || len(parsed.Result) == 0 {
		return toolResult{}, resultAbsent
	}
	if len(parsed.Result) > 1 {
		return toolResult{}, resultMalformed
	}
	for outcome, value := range parsed.Result {
		if outcome == "" || firstLine(outcome) != outcome {
			return toolResult{}, resultAbsent
		}
		return outcomeResult(entry.name, outcome, value), resultRead
	}
	return toolResult{}, resultAbsent
}

func outcomeResult(tool, outcome string, value json.RawMessage) toolResult {
	if outcome == toolResultSuccess {
		return toolResult{state: contracts.ToolCallSucceeded}
	}
	return toolResult{state: contracts.ToolCallFailed, reason: failureReason(tool, outcome, value)}
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
		if fields.isTrue("aborted") {
			return abortedText(fields)
		}
		code, ok := fields.int32Text("exitCode", math.MinInt32)
		if !ok {
			return ""
		}
		return exitCodeText(code, firstLine(fields.text("stderr")))
	case toolResultTimeout:
		milliseconds, ok := fields.int32Text("timeoutMs", 0)
		if !ok {
			return ""
		}
		return "timeout after " + milliseconds + "ms"
	case toolResultRejected:
		return labelled("rejected: ", fields.text("reason"))
	case toolResultSpawnError:
		return labelled("spawn error: ", fields.text("error"))
	case toolResultPermissionDenied:
		return labelled("permission denied: ", fields.text("error"))
	}
	return ""
}

func abortedText(fields rawFields) string {
	label := "aborted"
	if code, ok := fields.int32Text("exitCode", math.MinInt32); ok {
		label = "aborted, exit code " + code
	}
	stderrLine := firstLine(fields.text("stderr"))
	if stderrLine == "" {
		return label
	}
	return label + " (" + stderrLine + ")"
}

func labelled(label, text string) string {
	line := firstLine(text)
	if line == "" {
		return ""
	}
	return label + line
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
		return firstLine(text)
	}
	fields, isObject := decodeFields(value)
	if !isObject {
		return ""
	}
	for _, name := range []string{"message", "error", "stderr", "reason"} {
		if line := firstLine(fields.text(name)); line != "" {
			return line
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

func (f rawFields) isTrue(name string) bool {
	var flag bool
	return json.Unmarshal(f[name], &flag) == nil && flag
}

func (f rawFields) int32Text(name string, least int64) (string, bool) {
	raw := string(f[name])
	if !wholeNumber.MatchString(raw) {
		return "", false
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < least {
		return "", false
	}
	return raw, true
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
