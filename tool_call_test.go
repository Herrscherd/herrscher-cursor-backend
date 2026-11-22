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
	line := fmt.Sprintf(`{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"readToolCall":{"args":{"path":"x"},"result":{"error":{"message":%q}}}},"session_id":"s"}`, long)
	events := toolEvents(readTurnEvents(t, readStartedLine, line))
	got := events[len(events)-1].ToolCall
	if got == nil || got.State != contracts.ToolCallFailed {
		t.Fatalf("call = %+v, want a failed call", got)
	}
	if n := utf8.RuneCountInString(got.Error); n != maxToolErrorRunes {
		t.Fatalf("error has %d runes, want %d", n, maxToolErrorRunes)
	}
}

func TestCompletedToolCallWithoutStartEmitsNothing(t *testing.T) {
	if events := toolEvents(readTurnEvents(t, readSucceededLine)); len(events) != 0 {
		t.Fatalf("tool events = %+v, want none", events)
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
		readSucceededLine,
		strings.Replace(readStartedLine, "call-1", "call-2", 1),
		strings.Replace(readFailedLine, "call-1", "call-2", 1),
	)
	if tools := toolEvents(events); len(tools) != 4 {
		t.Fatalf("tool events = %+v, want two starts and two dones", tools)
	}
	for i := range events {
		if err := events[i].ValidateToolCall(); err != nil {
			t.Fatalf("event %+v: ValidateToolCall() = %v", events[i], err)
		}
	}
}

func TestToolCallTrackingStopsAtItsBound(t *testing.T) {
	lines := make([]string, 0, maxTrackedToolCalls+3)
	for i := range maxTrackedToolCalls + 1 {
		lines = append(lines, strings.Replace(readStartedLine, "call-1", fmt.Sprintf("call-%d", i), 1))
	}
	lines = append(lines,
		strings.Replace(readSucceededLine, "call-1", "call-0", 1),
		strings.Replace(readSucceededLine, "call-1", fmt.Sprintf("call-%d", maxTrackedToolCalls), 1),
	)
	dones := 0
	for _, ev := range toolEvents(readTurnEvents(t, lines...)) {
		if ev.Kind == contracts.BackendKindToolDone {
			dones++
			if ev.ToolCall.ID != "call-0" {
				t.Fatalf("done for %q, want only call-0 to be tracked", ev.ToolCall.ID)
			}
		}
	}
	if dones != 1 {
		t.Fatalf("dones = %d, want 1", dones)
	}
}
