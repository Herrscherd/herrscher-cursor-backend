package cursor

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestStreamResponderResumeToken(t *testing.T) {
	// Before the first turn, the token is whatever was passed in at construction.
	r := &streamResponder{session: "boot-id"}
	if got := r.ResumeToken(); got != "boot-id" {
		t.Fatalf("fresh: want boot-id, got %q", got)
	}
	// After a turn records a session id, the live id wins.
	r.session = "sess-1"
	if got := r.ResumeToken(); got != "sess-1" {
		t.Fatalf("live: want sess-1, got %q", got)
	}
}

func TestStreamResponderIsResumeAware(t *testing.T) {
	var _ contracts.ResumeAware = (*streamResponder)(nil)
}

func TestNewBackendThreadsResumeID(t *testing.T) {
	b, err := NewBackend(context.Background(), Config{Kind: "stream", Cmd: "cursor-agent", ResumeID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := b.(*streamResponder)
	if !ok {
		t.Fatalf("want *streamResponder, got %T", b)
	}
	if r.session != "x" {
		t.Fatalf("resume id not threaded into session: got %q", r.session)
	}
}
