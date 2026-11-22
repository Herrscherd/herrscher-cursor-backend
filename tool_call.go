package cursor

import (
	"encoding/json"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxTrackedToolCalls = 256
	maxToolErrorRunes   = 200
	toolResultSuccess   = "success"
)

type toolCallTracker struct {
	tools map[string]string
}

func (t *toolCallTracker) remember(callID, tool string) {
	if t.tools == nil {
		t.tools = make(map[string]string)
	}
	if len(t.tools) >= maxTrackedToolCalls {
		return
	}
	t.tools[callID] = tool
}

func (t *toolCallTracker) finish(callID string) (string, bool) {
	tool, known := t.tools[callID]
	delete(t.tools, callID)
	return tool, known
}

type toolEntry struct {
	name string
	body json.RawMessage
}

func soleTool(call map[string]json.RawMessage) *toolEntry {
	for name, body := range call {
		return &toolEntry{name: name, body: body}
	}
	return nil
}

type toolResult struct {
	state  contracts.ToolCallState
	reason string
}

func toolCallResult(call map[string]json.RawMessage) toolResult {
	entry := soleTool(call)
	if entry == nil {
		return toolResult{state: contracts.ToolCallSucceeded}
	}
	var body struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(entry.body, &body) != nil {
		return toolResult{state: contracts.ToolCallSucceeded}
	}
	for outcome, value := range body.Result {
		if outcome != toolResultSuccess {
			return toolResult{state: contracts.ToolCallFailed, reason: toolFailureReason(outcome, value)}
		}
	}
	return toolResult{state: contracts.ToolCallSucceeded}
}

func toolFailureReason(outcome string, value json.RawMessage) string {
	if reason := boundedReason(failureText(value)); reason != "" {
		return reason
	}
	return outcome
}

func failureText(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var fields map[string]any
	if json.Unmarshal(value, &fields) != nil {
		return ""
	}
	for _, field := range []string{"message", "error", "stderr", "reason"} {
		if reason, ok := fields[field].(string); ok && reason != "" {
			return reason
		}
	}
	return ""
}

func boundedReason(text string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(strings.TrimSpace(firstLine))
	if len(runes) > maxToolErrorRunes {
		runes = runes[:maxToolErrorRunes]
	}
	return string(runes)
}
