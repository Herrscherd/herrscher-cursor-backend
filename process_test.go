package cursor

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

var errPromptWrite = errors.New("prompt write failed")

type failingStdin struct {
	err error
}

func (s failingStdin) Write([]byte) (int, error) {
	return 0, s.err
}

func (s failingStdin) Close() error {
	return nil
}

func TestWritePromptReportsWriteError(t *testing.T) {
	proc := &turnProcess{stdin: failingStdin{err: errPromptWrite}, prompted: make(chan struct{})}
	proc.writePrompt("hello")
	if !errors.Is(proc.promptErr, errPromptWrite) {
		t.Fatalf("promptErr = %v, want the write error", proc.promptErr)
	}
}

func TestWritePromptIgnoresChildThatExitedEarly(t *testing.T) {
	peerGone := []error{
		&os.PathError{Op: "write", Path: "stdin", Err: syscall.EPIPE},
		os.ErrClosed,
	}
	for _, err := range peerGone {
		proc := &turnProcess{stdin: failingStdin{err: err}, prompted: make(chan struct{})}
		proc.writePrompt("hello")
		if proc.promptErr != nil {
			t.Fatalf("promptErr for %v = %v, want nil", err, proc.promptErr)
		}
	}
}
