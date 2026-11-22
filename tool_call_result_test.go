package cursor

import (
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const ambiguousResult = `{"success":{},"failure":{"message":"boom"}}`

func TestFailureErrorIsTheFirstNonBlankLine(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result string
		want   string
	}{
		{name: "object field starting with a newline", tool: "readToolCall", result: `{"error":{"message":"\n  boom\nx"}}`, want: "boom"},
		{name: "bare string starting with a newline", tool: "readToolCall", result: `{"error":"\n  boom\nx"}`, want: "boom"},
		{name: "shell stderr starting with a newline", tool: "shellToolCall", result: `{"failure":{"exitCode":3,"stderr":"\n  boom\nx"}}`, want: "exit code 3 (boom)"},
		{name: "object field with a carriage return", tool: "readToolCall", result: `{"error":{"message":"boom\rsecret"}}`, want: "boom"},
		{name: "shell stderr with a carriage return", tool: "shellToolCall", result: `{"failure":{"exitCode":3,"stderr":"boom\rsecret"}}`, want: "exit code 3 (boom)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, tc.tool, tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}

func TestAbortedFailureNamesTheAbort(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{name: "with exit code and stderr", result: `{"failure":{"aborted":true,"exitCode":3,"stderr":"\nboom"}}`, want: "aborted, exit code 3 (boom)"},
		{name: "with exit code alone", result: `{"failure":{"aborted":true,"exitCode":3}}`, want: "aborted, exit code 3"},
		{name: "with stderr and no exit code", result: `{"failure":{"aborted":true,"stderr":"boom"}}`, want: "aborted (boom)"},
		{name: "with a wrong type exit code", result: `{"failure":{"aborted":true,"exitCode":"3"}}`, want: "aborted"},
		{name: "alone", result: `{"failure":{"aborted":true}}`, want: "aborted"},
		{name: "not aborted keeps the exit code", result: `{"failure":{"aborted":false,"exitCode":3}}`, want: "exit code 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "shellToolCall", tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}

func TestAmbiguousResultEmitsNoToolDone(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, completedLine(readCallWithResult(ambiguousResult))))
	if len(events) != 1 || events[0].Kind != contracts.BackendKindTool || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
		t.Fatalf("tool events = %+v, want the started call alone, with no tool_done", events)
	}
}

func TestAmbiguousResultFreesTheOpenPlace(t *testing.T) {
	lines := make([]string, 0, maxOpenToolCalls+2)
	for i := range maxOpenToolCalls {
		lines = append(lines, withCallID(readStartedLine, fmt.Sprintf("call-%d", i)))
	}
	lines = append(lines,
		withCallID(completedLine(readCallWithResult(ambiguousResult)), "call-0"),
		withCallID(readStartedLine, fmt.Sprintf("call-%d", maxOpenToolCalls)),
	)
	events := toolEvents(readTurnEvents(t, lines...))
	last := events[len(events)-1]
	if last.Kind != contracts.BackendKindTool || last.ToolCall == nil || last.ToolCall.ID != fmt.Sprintf("call-%d", maxOpenToolCalls) {
		t.Fatalf("last event = %+v, want call-%d opened in the place freed", last, maxOpenToolCalls)
	}
}

func TestAmbiguousResultLeavesTheIDSeen(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, completedLine(readCallWithResult(ambiguousResult)), readStartedLine))
	if len(events) != 2 || events[1].Kind != contracts.BackendKindTool || events[1].ToolCall != nil {
		t.Fatalf("tool events = %+v, want the repeated start bare after an ambiguous end", events)
	}
}

func TestMalformedResultWarnsOnceAPerTurn(t *testing.T) {
	logged := captureLog(t)
	ambiguous := completedLine(readCallWithResult(ambiguousResult))
	readTurnEvents(t, readStartedLine, ambiguous, withCallID(readStartedLine, "call-2"), withCallID(ambiguous, "call-2"))
	readTurnEvents(t, readStartedLine, ambiguous)
	got := nonEmptyLines(logged.String())
	if len(got) != 2 {
		t.Fatalf("log has %d lines, want one warning per turn:\n%s", len(got), logged)
	}
	for _, line := range got {
		if !strings.Contains(line, "level=WARN") || !strings.HasSuffix(line, `msg="tool call result carries more than one outcome or another tool than its start, the call stays started"`) {
			t.Fatalf("warning = %q, want a WARN with the message alone and no value", line)
		}
	}
}

func TestEndOfAnotherToolEmitsNoToolDone(t *testing.T) {
	shellEnd := completedLine(`{"shellToolCall":{"args":{"command":"ls"},"result":{"failure":{"exitCode":3,"stderr":"boom"}}}}`)
	events := toolEvents(readTurnEvents(t, readStartedLine, shellEnd))
	if len(events) != 1 || events[0].Kind != contracts.BackendKindTool || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
		t.Fatalf("tool events = %+v, want the started read call alone, with no tool_done", events)
	}
}

func TestEndOfAnotherToolKeepsTheIDSeen(t *testing.T) {
	shellEnd := completedLine(`{"shellToolCall":{"args":{"command":"ls"},"result":{"success":{}}}}`)
	events := toolEvents(readTurnEvents(t, readStartedLine, shellEnd, readStartedLine, readSucceededLine))
	if len(events) != 2 || events[1].Kind != contracts.BackendKindTool || events[1].ToolCall != nil {
		t.Fatalf("tool events = %+v, want the reused id to start no second call and the later end to emit nothing", events)
	}
}

func TestEndOfAnotherToolWarnsOncePerTurn(t *testing.T) {
	logged := captureLog(t)
	shellEnd := completedLine(`{"shellToolCall":{"args":{"command":"ls"},"result":{"success":{}}}}`)
	readTurnEvents(t, readStartedLine, shellEnd, withCallID(readStartedLine, "call-2"), withCallID(shellEnd, "call-2"))
	got := nonEmptyLines(logged.String())
	if len(got) != 1 {
		t.Fatalf("log has %d lines, want one warning for the turn:\n%s", len(got), logged)
	}
	if !strings.Contains(got[0], "level=WARN") || !strings.HasSuffix(got[0], `msg="tool call result carries more than one outcome or another tool than its start, the call stays started"`) {
		t.Fatalf("warning = %q, want a WARN with the message alone and no value", got[0])
	}
}

func TestEndAfterAStartOfTwoToolsEmitsNothing(t *testing.T) {
	twoTools := `{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"README.md"}},"shellToolCall":{"args":{"command":"ls"}}},"session_id":"s"}`
	events := toolEvents(readTurnEvents(t, twoTools, readSucceededLine))
	for i, event := range events {
		if event.ToolCall != nil || event.Kind == contracts.BackendKindToolDone {
			t.Fatalf("event %d = %+v, want no call opened and no tool_done", i, event)
		}
	}
}

func TestEndWithAnUntrimmedOutcomeEmitsNoToolDoneAndNoWarning(t *testing.T) {
	cases := []struct {
		name   string
		result string
	}{
		{name: "empty", result: `{"":{"message":"boom"}}`},
		{name: "space", result: `{" ":{}}`},
		{name: "newline", result: `{"\n":""}`},
		{name: "tab", result: `{"\t":{"message":"  "}}`},
		{name: "leading newline", result: `{"\nsuccess":{}}`},
		{name: "trailing newline", result: `{"success\n":{}}`},
		{name: "leading space", result: `{" failure":{"message":"boom"}}`},
		{name: "two lines", result: `{"failure\nmore":{}}`},
		{name: "carriage return", result: `{"fail\rure":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureLog(t)
			events := toolEvents(readTurnEvents(t, readStartedLine, completedLine(readCallWithResult(tc.result))))
			if len(events) != 1 || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
				t.Fatalf("tool events = %+v, want the started call alone, with no tool_done", events)
			}
			if got := nonEmptyLines(logged.String()); len(got) != 0 {
				t.Fatalf("log = %q, want no warning for an untrimmed outcome", got)
			}
		})
	}
}

func TestEndWithTwoToolKeysEmitsNoToolDoneAndNoWarning(t *testing.T) {
	logged := captureLog(t)
	twoTools := completedLine(`{"readToolCall":{"result":{"success":{}}},"shellToolCall":{"result":{"success":{}}}}`)
	events := toolEvents(readTurnEvents(t, readStartedLine, twoTools))
	if len(events) != 1 || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
		t.Fatalf("tool events = %+v, want the started call alone, with no tool_done", events)
	}
	if got := nonEmptyLines(logged.String()); len(got) != 0 {
		t.Fatalf("log = %q, want no warning for an end with two tool keys", got)
	}
}
