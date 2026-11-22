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
	readFailedLine    = `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"README.md"},"result":{"error":{"path":"README.md","error":"no such file: README.md\nat /work/tree/stack"}}}},"model_call_id":"m1","session_id":"s","timestamp_ms":2}`
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

func doneCallOf(t *testing.T, tool, result string) contracts.ToolCall {
	t.Helper()
	started := fmt.Sprintf(`{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{%q:{"args":{"path":"README.md"}}},"session_id":"s"}`, tool)
	done := completedLine(fmt.Sprintf(`{%q:{"args":{"path":"README.md"},"result":%s}}`, tool, result))
	events := toolEvents(readTurnEvents(t, started, done))
	if len(events) != 2 || events[1].Kind != contracts.BackendKindToolDone || events[1].ToolCall == nil {
		t.Fatalf("tool events = %+v, want a start and a tool_done", events)
	}
	return *events[1].ToolCall
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
			result: `{"error":{"path":"README.md","error":"no such file: README.md\nat /work"}}`,
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
			if got := doneCallOf(t, tc.tool, tc.result); got != tc.want {
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
	line := completedLine(fmt.Sprintf(`{"readToolCall":{"args":{"path":"x"},"result":{"error":{"path":"x","error":%q}}}}`, long))
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
			logged := captureLog(t)
			events := toolEvents(readTurnEvents(t, readStartedLine, tc.end))
			if len(events) != 1 || events[0].ToolCall == nil || events[0].ToolCall.State != contracts.ToolCallStarted {
				t.Fatalf("tool events = %+v, want the start alone, still started", events)
			}
			if got := nonEmptyLines(logged.String()); len(got) != 0 {
				t.Fatalf("log = %q, want no warning for an absent result", got)
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
	lines := make([]string, 0, maxOpenToolCalls+3)
	for i := range maxOpenToolCalls + 1 {
		lines = append(lines, strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", i), 1))
	}
	lines = append(lines,
		strings.Replace(readSucceededLine, "call-1", "call-0", 1),
		strings.Replace(readSucceededLine, "call-1", fmt.Sprintf("call-%d", maxOpenToolCalls), 1),
	)
	events := toolEvents(readTurnEvents(t, lines...))
	if len(events) != maxOpenToolCalls+2 {
		t.Fatalf("tool events = %d, want %d: the overflow call must not get a done", len(events), maxOpenToolCalls+2)
	}
	overflow := events[maxOpenToolCalls]
	if overflow.Kind != contracts.BackendKindTool || overflow.Tool != "readToolCall" || overflow.ToolCall != nil {
		t.Fatalf("overflow start = %+v, want a bare tool event", overflow)
	}
	last := events[len(events)-1]
	if last.Kind != contracts.BackendKindToolDone || last.ToolCall == nil || last.ToolCall.ID != "call-0" {
		t.Fatalf("last event = %+v, want the done of call-0", last)
	}
}

func TestFinishedCallIDComingBackStaysBare(t *testing.T) {
	events := toolEvents(readTurnEvents(t, readStartedLine, readSucceededLine, readStartedLine, readSucceededLine))
	if len(events) != 3 {
		t.Fatalf("tool events = %+v, want a start, a done and a bare start", events)
	}
	if returning := events[2]; returning.Kind != contracts.BackendKindTool || returning.Tool != "readToolCall" || returning.ToolCall != nil {
		t.Fatalf("returning start = %+v, want a bare tool event", returning)
	}
}

func TestCallIDSeenBeyondTheBoundStaysBare(t *testing.T) {
	lines := make([]string, 0, maxSeenToolCalls+3)
	for i := range maxSeenToolCalls {
		lines = append(lines, strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", i), 1))
	}
	lines = append(lines,
		strings.Replace(readSucceededLine, "call-1", "call-0", 1),
		strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", maxSeenToolCalls), 1),
		strings.Replace(readSucceededLine, "call-1", fmt.Sprintf("call-%d", maxSeenToolCalls), 1),
	)
	events := toolEvents(readTurnEvents(t, lines...))
	if len(events) != maxSeenToolCalls+2 {
		t.Fatalf("tool events = %d, want %d: the start past the seen bound must not open, and its done must not emit", len(events), maxSeenToolCalls+2)
	}
	if freed := events[maxSeenToolCalls]; freed.Kind != contracts.BackendKindToolDone || freed.ToolCall == nil || freed.ToolCall.ID != "call-0" {
		t.Fatalf("freed done = %+v, want the done of call-0", freed)
	}
	if beyond := events[maxSeenToolCalls+1]; beyond.Kind != contracts.BackendKindTool || beyond.Tool != "readToolCall" || beyond.ToolCall != nil {
		t.Fatalf("start beyond the seen bound = %+v, want a bare tool event", beyond)
	}
}

func TestStartRefusedByTheOpenBoundStaysBareAfterAPlaceFrees(t *testing.T) {
	lines := make([]string, 0, maxOpenToolCalls+4)
	for i := range maxOpenToolCalls {
		lines = append(lines, strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", i), 1))
	}
	refused := strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", maxOpenToolCalls), 1)
	lines = append(lines,
		refused,
		strings.Replace(readSucceededLine, "call-1", "call-0", 1),
		refused,
		strings.Replace(readSucceededLine, "call-1", fmt.Sprintf("call-%d", maxOpenToolCalls), 1),
	)
	events := toolEvents(readTurnEvents(t, lines...))
	if len(events) != maxOpenToolCalls+3 {
		t.Fatalf("tool events = %d, want %d: the refused start stays bare after a place frees", len(events), maxOpenToolCalls+3)
	}
	if first := events[maxOpenToolCalls]; first.Kind != contracts.BackendKindTool || first.ToolCall != nil {
		t.Fatalf("refused start = %+v, want a bare tool event", first)
	}
	if done := events[maxOpenToolCalls+1]; done.Kind != contracts.BackendKindToolDone || done.ToolCall == nil || done.ToolCall.ID != "call-0" {
		t.Fatalf("freed done = %+v, want the done of call-0", done)
	}
	if repeat := events[maxOpenToolCalls+2]; repeat.Kind != contracts.BackendKindTool || repeat.ToolCall != nil {
		t.Fatalf("repeated refused start = %+v, want a bare tool event", repeat)
	}
}

func TestStartWithoutToolNameMarksItsIDSeen(t *testing.T) {
	nameless := `{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{},"session_id":"s"}`
	events := toolEvents(readTurnEvents(t, nameless, readStartedLine, readSucceededLine))
	if len(events) != 2 {
		t.Fatalf("tool events = %+v, want the nameless start and the later start, both bare", events)
	}
	for i := range events {
		if events[i].ToolCall != nil {
			t.Fatalf("event %d = %+v, want a bare tool event", i, events[i])
		}
	}
}

func TestFailureTextKeepsEachStringField(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{name: "wrong type message keeps error", result: `{"error":{"message":5,"error":"boom"}}`, want: "boom"},
		{name: "wrong type error keeps message", result: `{"error":{"message":"first","error":5}}`, want: "first"},
		{name: "wrong type reason keeps stderr", result: `{"error":{"reason":7,"stderr":"boom"}}`, want: "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "readToolCall", tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}

func TestFailureTextSkipsFieldsWhoseFirstLineIsBlank(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{name: "blank message keeps the next field", result: `{"error":{"message":" ","stderr":"boom"}}`, want: "boom"},
		{name: "blank fields fall back to the result name", result: `{"error":{"message":" ","stderr":"\n\t"}}`, want: "error"},
		{name: "blank bare string falls back to the result name", result: `{"error":"  "}`, want: "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "readToolCall", tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}

func TestShellFailureKeepsItsExitCodeWhenStderrHasTheWrongType(t *testing.T) {
	got := doneCallOf(t, "shellToolCall", `{"failure":{"exitCode":2,"stderr":5}}`)
	if got.Error != "exit code 2" {
		t.Fatalf("error = %q, want %q", got.Error, "exit code 2")
	}
}

func TestShellExitCodeCountsOnlyWholeNumbers(t *testing.T) {
	cases := []struct {
		name   string
		result string
	}{
		{name: "string", result: `{"failure":{"exitCode":"2"}}`},
		{name: "null", result: `{"failure":{"exitCode":null,"stderr":"boom"}}`},
		{name: "absent", result: `{"failure":{"stderr":"boom"}}`},
		{name: "fraction", result: `{"failure":{"exitCode":2.5}}`},
		{name: "exponent", result: `{"failure":{"exitCode":2e3}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "shellToolCall", tc.result); got.Error != toolResultFailure {
				t.Fatalf("error = %q, want the outcome name %q", got.Error, toolResultFailure)
			}
		})
	}
}

func TestShellTimeoutCountsOnlyWholeNumbers(t *testing.T) {
	cases := []struct {
		name   string
		result string
	}{
		{name: "string", result: `{"timeout":{"timeoutMs":"5000"}}`},
		{name: "null", result: `{"timeout":{"timeoutMs":null}}`},
		{name: "absent", result: `{"timeout":{}}`},
		{name: "fraction", result: `{"timeout":{"timeoutMs":5000.5}}`},
		{name: "exponent", result: `{"timeout":{"timeoutMs":5e3}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "shellToolCall", tc.result); got.Error != toolResultTimeout {
				t.Fatalf("error = %q, want the outcome name %q", got.Error, toolResultTimeout)
			}
		})
	}
}
