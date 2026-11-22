package cursor

import (
	"bufio"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	readStartedLine   = `{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"model_call_id":"m1","session_id":"s","timestamp_ms":1}`
	readSucceededLine = `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"README.md"},"result":{"success":{"content":"# herrscher"}}}},"model_call_id":"m1","session_id":"s","timestamp_ms":2}`
	readFailedLine    = `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"README.md"},"result":{"error":{"message":"no such file: README.md\nat /work/tree/stack"}}}},"model_call_id":"m1","session_id":"s","timestamp_ms":2}`
)

func readTurnEvents(t *testing.T, lines ...string) []contracts.BackendEvent {
	t.Helper()
	input := strings.Join(append(slices.Clone(lines), streamTurnResultLine), "\n") + "\n"
	var events []contracts.BackendEvent
	if _, err := readTurn(bufio.NewReader(strings.NewReader(input)), func(e contracts.BackendEvent) {
		events = append(events, e)
	}); err != nil {
		t.Fatal(err)
	}
	return events
}

func toolEvents(events []contracts.BackendEvent) []contracts.BackendEvent {
	var tools []contracts.BackendEvent
	for i := range events {
		if events[i].Kind == contracts.BackendKindTool || events[i].Kind == contracts.BackendKindToolDone {
			tools = append(tools, events[i])
		}
	}
	return tools
}

func completedLine(toolCall string) string {
	return `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":` + toolCall + `,"session_id":"s"}`
}

func readCallWithResult(result string) string {
	return fmt.Sprintf(`{"readToolCall":{"args":{"path":"README.md"},"result":%s}}`, result)
}

func TestStartedToolCallCarriesItsCallID(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine))
	if len(events) != 1 {
		t.Fatalf("tool events = %+v, want one start", events)
	}
	start := events[0]
	if start.Kind != contracts.BackendKindTool || start.Tool != "readToolCall" || start.Detail != "README.md" {
		t.Fatalf("start = %+v, want the tool and detail of the call", start)
	}
	want := contracts.ToolCall{ID: "call-1", Tool: "readToolCall", Detail: "README.md", State: contracts.ToolCallStarted}
	if start.ToolCall == nil || *start.ToolCall != want {
		t.Fatalf("start call = %+v, want %+v", start.ToolCall, want)
	}
}

func TestSucceededToolCallEmitsToolDone(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, readSucceededLine))
	if len(events) != 2 {
		t.Fatalf("tool events = %+v, want a start and a done", events)
	}
	done := events[1]
	if done.Kind != contracts.BackendKindToolDone || done.Tool != "" || done.Detail != "" {
		t.Fatalf("done = %+v, want tool_done with empty tool and detail", done)
	}
	want := contracts.ToolCall{ID: "call-1", Tool: "readToolCall", State: contracts.ToolCallSucceeded}
	if done.ToolCall == nil || *done.ToolCall != want {
		t.Fatalf("done call = %+v, want %+v", done.ToolCall, want)
	}
}

func TestToolCallEndStatesFollowTheResultOutcome(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result string
		want   contracts.ToolCall
	}{
		{
			name:   "success",
			tool:   "readToolCall",
			result: `{"success":{"content":"# herrscher"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "readToolCall", State: contracts.ToolCallSucceeded},
		},
		{
			name:   "error",
			tool:   "readToolCall",
			result: `{"error":{"message":"no such file: README.md\nat /work"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "readToolCall", State: contracts.ToolCallFailed, Error: "no such file: README.md"},
		},
		{
			name:   "shell failure",
			tool:   "shellToolCall",
			result: `{"failure":{"exitCode":2,"stderr":"rm: cannot remove\nsecond line"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "exit code 2 (rm: cannot remove)"},
		},
		{
			name:   "shell failure without stderr",
			tool:   "shellToolCall",
			result: `{"failure":{"exitCode":1}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "exit code 1"},
		},
		{
			name:   "shell timeout",
			tool:   "shellToolCall",
			result: `{"timeout":{"timeoutMs":5000}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "timeout after 5000ms"},
		},
		{
			name:   "shell rejected",
			tool:   "shellToolCall",
			result: `{"rejected":{"reason":"command not allowed"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "rejected: command not allowed"},
		},
		{
			name:   "shell spawn error",
			tool:   "shellToolCall",
			result: `{"spawnError":{"error":"ENOENT"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "spawn error: ENOENT"},
		},
		{
			name:   "shell permission denied",
			tool:   "shellToolCall",
			result: `{"permissionDenied":{"error":"EACCES"}}`,
			want:   contracts.ToolCall{ID: "call-1", Tool: "shellToolCall", State: contracts.ToolCallFailed, Error: "permission denied: EACCES"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			started := fmt.Sprintf(`{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{%q:{"args":{"path":"README.md"}}},"session_id":"s"}`, tc.tool)
			done := completedLine(fmt.Sprintf(`{%q:{"args":{"path":"README.md"},"result":%s}}`, tc.tool, tc.result))
			events := toolEvents(readTurnEvents(t, started, done))
			if len(events) != 2 || events[1].Kind != contracts.BackendKindToolDone || events[1].ToolCall == nil {
				t.Fatalf("tool events = %+v, want a start and a tool_done", events)
			}
			if got := *events[1].ToolCall; got != tc.want {
				t.Fatalf("done call = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFailedToolCallEmitsItsFirstLineAsError(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, readFailedLine))
	if len(events) != 2 || events[1].Kind != contracts.BackendKindToolDone {
		t.Fatalf("tool events = %+v, want a start and a tool_done", events)
	}
	want := contracts.ToolCall{ID: "call-1", Tool: "readToolCall", State: contracts.ToolCallFailed, Error: "no such file: README.md"}
	if events[1].ToolCall == nil || *events[1].ToolCall != want {
		t.Fatalf("failed call = %+v, want %+v", events[1].ToolCall, want)
	}
}

func TestFailedToolCallErrorIsBounded(t *testing.T) {
	long := strings.Repeat("é", maxToolErrorRunes+50)
	line := completedLine(fmt.Sprintf(`{"readToolCall":{"args":{"path":"x"},"result":{"error":{"message":%q}}}}`, long))
	events := toolEvents(readTurnEvents(t, readStartedLine, line))
	got := events[len(events)-1].ToolCall
	if got == nil || got.State != contracts.ToolCallFailed {
		t.Fatalf("call = %+v, want a failed call", got)
	}
	if n := utf8.RuneCountInString(got.Error); n != maxToolErrorRunes {
		t.Fatalf("error has %d runes, want %d", n, maxToolErrorRunes)
	}
}

func TestShellFailureErrorIsBounded(t *testing.T) {
	long := strings.Repeat("é", maxToolErrorRunes+50)
	line := completedLine(fmt.Sprintf(`{"shellToolCall":{"args":{"command":"x"},"result":{"failure":{"exitCode":3,"stderr":%q}}}}`, long))
	started := `{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{"shellToolCall":{"args":{"command":"x"}}},"session_id":"s"}`
	events := toolEvents(readTurnEvents(t, started, line))
	got := events[len(events)-1].ToolCall
	if got == nil || got.State != contracts.ToolCallFailed {
		t.Fatalf("call = %+v, want a failed call", got)
	}
	if n := utf8.RuneCountInString(got.Error); n != maxToolErrorRunes {
		t.Fatalf("error has %d runes, want %d", n, maxToolErrorRunes)
	}
	if !strings.HasPrefix(got.Error, "exit code 3 (") {
		t.Fatalf("error = %q, want the exit code first", got.Error)
	}
}

func TestCompletedToolCallWithoutStartEmitsNothing(t *testing.T) {
	if events := toolEvents(readTurnEvents(t, readSucceededLine)); len(events) != 0 {
		t.Fatalf("tool events = %+v, want none", events)
	}
}

func TestEndWithoutReadableResultEmitsNothing(t *testing.T) {
	cases := []struct {
		name string
		end  string
	}{
		{name: "absent result", end: completedLine(`{"readToolCall":{"args":{"path":"README.md"}}}`)},
		{name: "empty result", end: completedLine(readCallWithResult(`{}`))},
		{name: "unreadable result", end: completedLine(readCallWithResult(`"oops"`))},
		{name: "no tool", end: completedLine(`{}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := toolEvents(readTurnEvents(t, readStartedLine, tc.end))
			if len(events) != 1 || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
				t.Fatalf("tool events = %+v, want the start alone, still started", events)
			}
		})
	}
}

func TestCallOpenWhenTurnEndsStaysStarted(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine))
	if len(events) != 1 || events[0].Kind != contracts.BackendKindTool {
		t.Fatalf("tool events = %+v, want the start and no tool_done", events)
	}
}

func TestRepeatedToolEndEmitsNothing(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, readSucceededLine, readSucceededLine))
	if len(events) != 2 {
		t.Fatalf("tool events = %+v, want a start and one done", events)
	}
}

func TestStartedWithOpenCallIDEmitsBareTool(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, readStartedLine, readSucceededLine))
	if len(events) != 3 {
		t.Fatalf("tool events = %+v, want two starts and one done", events)
	}
	if events[0].ToolCall == nil {
		t.Fatalf("first start = %+v, want a tracked call", events[0])
	}
	if repeat := events[1]; repeat.Kind != contracts.BackendKindTool || repeat.Tool != "readToolCall" || repeat.ToolCall != nil {
		t.Fatalf("repeated start = %+v, want a bare tool event", repeat)
	}
	want := contracts.ToolCall{ID: "call-1", Tool: "readToolCall", State: contracts.ToolCallSucceeded}
	if events[2].ToolCall == nil || *events[2].ToolCall != want {
		t.Fatalf("done call = %+v, want %+v", events[2].ToolCall, want)
	}
}

func TestToolCallWithoutCallIDKeepsOnlyTheLegacyStart(t *testing.T) {
	legacy := `{"type":"tool_call","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"s"}`
	events := toolEvents(readTurnEvents(t, legacy))
	if len(events) != 1 {
		t.Fatalf("tool events = %+v, want one start", events)
	}
	if events[0].Tool != "readToolCall" || events[0].Detail != "README.md" || events[0].ToolCall != nil {
		t.Fatalf("start = %+v, want the legacy tool event with no call", events[0])
	}
}

func TestToolCallOfAnEarlierTurnIsNotClosedByTheNextTurn(t *testing.T) {
	readTurnEvents(t, readStartedLine)
	if events := toolEvents(readTurnEvents(t, readSucceededLine)); len(events) != 0 {
		t.Fatalf("tool events = %+v, want none from the next turn", events)
	}
}

func TestEveryEmittedToolEventPassesValidateToolCall(t *testing.T) {
	events := readTurnEvents(t,
		readStartedLine,
		readStartedLine,
		readSucceededLine,
		strings.Replace(readStartedLine, "call-1", "call-2", 1),
		strings.Replace(readFailedLine, "call-1", "call-2", 1),
	)
	if tools := toolEvents(events); len(tools) != 5 {
		t.Fatalf("tool events = %+v, want two starts, two dones and a bare repeat", tools)
	}
	for i := range events {
		if err := events[i].ValidateToolCall(); err != nil {
			t.Fatalf("event %+v: ValidateToolCall() = %v", events[i], err)
		}
	}
}

func TestStartedBeyondTheBoundEmitsBareTool(t *testing.T) {
	lines := make([]string, 0, maxTrackedToolCalls+3)
	for i := range maxTrackedToolCalls + 1 {
		lines = append(lines, strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", i), 1))
	}
	lines = append(lines,
		strings.Replace(readSucceededLine, "call-1", "call-0", 1),
		strings.Replace(readSucceededLine, "call-1", fmt.Sprintf("call-%d", maxTrackedToolCalls), 1),
	)
	events := toolEvents(readTurnEvents(t, lines...))
	if len(events) != maxTrackedToolCalls+2 {
		t.Fatalf("tool events = %d, want %d: the overflow call must not get a done", len(events), maxTrackedToolCalls+2)
	}
	overflow := events[maxTrackedToolCalls]
	if overflow.Kind != contracts.BackendKindTool || overflow.Tool != "readToolCall" || overflow.ToolCall != nil {
		t.Fatalf("overflow start = %+v, want a bare tool event", overflow)
	}
	last := events[len(events)-1]
	if last.Kind != contracts.BackendKindToolDone || last.ToolCall == nil || last.ToolCall.ID != "call-0" {
		t.Fatalf("last event = %+v, want the done of call-0", last)
	}
}
