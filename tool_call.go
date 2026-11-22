package cursor

import (
	"encoding/json"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxTrackedToolCalls        = 256
	maxToolErrorRunes          = 200
	toolResultSuccess          = "success"
	toolResultFailure          = "failure"
	toolResultTimeout          = "timeout"
	toolResultRejected         = "rejected"
	toolResultSpawnError       = "spawnError"
	toolResultPermissionDenied = "permissionDenied"
	shellToolName              = "shellToolCall"
)

type toolCallTracker struct {
	tools map[string]string
}

func (t *toolCallTracker) remember(callID, tool string) bool {
	if _, open := t.tools[callID]; open {
		return false
	}
	if len(t.tools) >= maxTrackedToolCalls {
		return false
	}
	if t.tools == nil {
		t.tools = make(map[string]string)
	}
	t.tools[callID] = tool
	return true
}

func (t *toolCallTracker) finish(callID string) (string, bool) {
	tool, known := t.tools[callID]
	delete(t.tools, callID)
	return tool, known
}

func singleTool(call map[string]json.RawMessage) (string, json.RawMessage, bool) {
	if len(call) != 1 {
		return "", nil, false
	}
	for name, body := range call {
		return name, body, true
	}
	return "", nil, false
}

type toolResult struct {
	state  contracts.ToolCallState
	reason string
}

func toolCallResult(call map[string]json.RawMessage) (toolResult, bool) {
	name, body, ok := singleTool(call)
	if !ok {
		return toolResult{}, false
	}
	var parsed struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Result) == 0 {
		return toolResult{}, false
	}
	for outcome, value := range parsed.Result {
		if outcome != toolResultSuccess {
			return toolResult{state: contracts.ToolCallFailed, reason: failureReason(name, outcome, value)}, true
		}
	}
	return toolResult{state: contracts.ToolCallSucceeded}, true
}

func failureReason(tool, outcome string, value json.RawMessage) string {
	reason := ""
	if tool == shellToolName {
		reason = shellFailureText(outcome, value)
	}
	if reason == "" {
		reason = failureText(value)
	}
	if reason == "" {
		reason = outcome
	}
	return boundRunes(firstLine(reason))
}

func shellFailureText(outcome string, value json.RawMessage) string {
	switch outcome {
	case toolResultFailure:
		var failure struct {
			ExitCode json.Number `json:"exitCode"`
			Stderr   string      `json:"stderr"`
		}
		if json.Unmarshal(value, &failure) != nil {
			return ""
		}
		return exitCodeText(failure.ExitCode.String(), firstLine(failure.Stderr))
	case toolResultTimeout:
		var timeout struct {
			TimeoutMs json.Number `json:"timeoutMs"`
		}
		if json.Unmarshal(value, &timeout) != nil || timeout.TimeoutMs == "" {
			return ""
		}
		return "timeout after " + timeout.TimeoutMs.String() + "ms"
	case toolResultRejected:
		var rejected struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(value, &rejected) != nil {
			return ""
		}
		return "rejected: " + orDefault(rejected.Reason, "unknown reason")
	case toolResultSpawnError:
		var spawn struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(value, &spawn) != nil {
			return ""
		}
		return "spawn error: " + spawn.Error
	case toolResultPermissionDenied:
		var denied struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(value, &denied) != nil {
			return ""
		}
		return "permission denied: " + denied.Error
	}
	return ""
}

func exitCodeText(code, stderrLine string) string {
	if code == "" {
		return ""
	}
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
	var fields struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Stderr  string `json:"stderr"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(value, &fields) != nil {
		return ""
	}
	for _, field := range []string{fields.Message, fields.Error, fields.Stderr, fields.Reason} {
		if field != "" {
			return field
		}
	}
	return ""
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
