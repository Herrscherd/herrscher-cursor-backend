package cursor

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

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
