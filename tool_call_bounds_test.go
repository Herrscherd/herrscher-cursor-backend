package cursor

import (
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func withCallID(line, id string) string {
	return strings.Replace(line, "call-1", id, 1)
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestEachBoundWarnsOnceAsItsFirstRefusalPerTurn(t *testing.T) {
	logged := captureLog(t)
	firstTurn := make([]string, 0, maxSeenToolCalls+1)
	for i := range maxSeenToolCalls + 1 {
		firstTurn = append(firstTurn, withCallID(readStartedLine, fmt.Sprintf("call-%d", i)))
	}
	readTurnEvents(t, firstTurn...)
	secondTurn := make([]string, 0, maxOpenToolCalls+1)
	for i := range maxOpenToolCalls + 1 {
		secondTurn = append(secondTurn, withCallID(readStartedLine, fmt.Sprintf("call-%d", i)))
	}
	readTurnEvents(t, secondTurn...)

	want := []string{
		"bound=max_open limit=256",
		"bound=max_seen limit=4096",
		"bound=max_open limit=256",
	}
	got := nonEmptyLines(logged.String())
	if len(got) != len(want) {
		t.Fatalf("log has %d lines, want %d warnings, one per bound per turn:\n%s", len(got), len(want), logged)
	}
	for i := range want {
		if !strings.Contains(got[i], "level=WARN") || !strings.HasSuffix(got[i], want[i]) {
			t.Fatalf("warning %d = %q, want a WARN ending with %q", i, got[i], want[i])
		}
	}
}

func TestNoBoundWarningWhileNoStartIsRefused(t *testing.T) {
	logged := captureLog(t)
	lines := make([]string, 0, maxOpenToolCalls)
	for i := range maxOpenToolCalls {
		lines = append(lines, withCallID(readStartedLine, fmt.Sprintf("call-%d", i)))
	}
	readTurnEvents(t, lines...)
	if logged.Len() != 0 {
		t.Fatalf("log = %q, want none while every start is accepted", logged)
	}
}

func TestCallIDBeyondTheByteBoundNeverOpensACall(t *testing.T) {
	id := strings.Repeat("é", maxCallIDBytes/2) + "x"
	tools := toolEvents(readTurnEvents(t, withCallID(readStartedLine, id), withCallID(readSucceededLine, id)))
	if len(tools) != 1 || tools[0].Kind != contracts.BackendKindTool || tools[0].Tool != "readToolCall" || tools[0].ToolCall != nil {
		t.Fatalf("tool events = %+v, want the start alone as a bare tool event", tools)
	}
}

func TestCallIDAtTheByteBoundOpensACall(t *testing.T) {
	id := strings.Repeat("é", maxCallIDBytes/2)
	tools := toolEvents(readTurnEvents(t, withCallID(readStartedLine, id), withCallID(readSucceededLine, id)))
	if len(tools) != 2 || tools[0].ToolCall == nil || tools[0].ToolCall.State != contracts.ToolCallStarted {
		t.Fatalf("tool events = %+v, want a started call for an id of %d bytes", tools, maxCallIDBytes)
	}
	want := contracts.ToolCall{ID: id, Tool: "readToolCall", State: contracts.ToolCallSucceeded}
	if tools[1].Kind != contracts.BackendKindToolDone || tools[1].ToolCall == nil || *tools[1].ToolCall != want {
		t.Fatalf("done = %+v, want %+v", tools[1], want)
	}
}

func TestShellWholeNumbersStayWithinTheirRange(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{name: "exit code of minus zero", result: `{"failure":{"exitCode":-0}}`, want: toolResultFailure},
		{name: "exit code just beyond int32", result: `{"failure":{"exitCode":2147483648}}`, want: toolResultFailure},
		{name: "exit code just below int32", result: `{"failure":{"exitCode":-2147483649}}`, want: toolResultFailure},
		{name: "exit code of the int32 maximum", result: `{"failure":{"exitCode":2147483647}}`, want: "exit code 2147483647"},
		{name: "exit code of the int32 minimum", result: `{"failure":{"exitCode":-2147483648}}`, want: "exit code -2147483648"},
		{name: "negative exit code within int32", result: `{"failure":{"exitCode":-1}}`, want: "exit code -1"},
		{name: "timeout of minus zero", result: `{"timeout":{"timeoutMs":-0}}`, want: toolResultTimeout},
		{name: "negative timeout", result: `{"timeout":{"timeoutMs":-1}}`, want: toolResultTimeout},
		{name: "timeout just beyond int32", result: `{"timeout":{"timeoutMs":2147483648}}`, want: toolResultTimeout},
		{name: "timeout of the int32 maximum", result: `{"timeout":{"timeoutMs":2147483647}}`, want: "timeout after 2147483647ms"},
		{name: "timeout of zero", result: `{"timeout":{"timeoutMs":0}}`, want: "timeout after 0ms"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "shellToolCall", tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}

func TestShellVariantWithoutDetailNamesItsResult(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{name: "failure without exit code", result: `{"failure":{}}`, want: toolResultFailure},
		{name: "timeout without milliseconds", result: `{"timeout":{}}`, want: toolResultTimeout},
		{name: "rejection without reason", result: `{"rejected":{}}`, want: toolResultRejected},
		{name: "rejection with an empty reason", result: `{"rejected":{"reason":""}}`, want: toolResultRejected},
		{name: "spawn error without error", result: `{"spawnError":{}}`, want: toolResultSpawnError},
		{name: "spawn error with an empty error", result: `{"spawnError":{"error":""}}`, want: toolResultSpawnError},
		{name: "spawn error with a blank error", result: `{"spawnError":{"error":"\n"}}`, want: toolResultSpawnError},
		{name: "spawn error as a bare string", result: `{"spawnError":""}`, want: toolResultSpawnError},
		{name: "permission denied without error", result: `{"permissionDenied":{}}`, want: toolResultPermissionDenied},
		{name: "permission denied with an empty error", result: `{"permissionDenied":{"error":""}}`, want: toolResultPermissionDenied},
		{name: "permission denied as a bare string", result: `{"permissionDenied":""}`, want: toolResultPermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := doneCallOf(t, "shellToolCall", tc.result); got.State != contracts.ToolCallFailed || got.Error != tc.want {
				t.Fatalf("call = %+v, want a failed call with error %q", got, tc.want)
			}
		})
	}
}
