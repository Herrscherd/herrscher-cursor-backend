package cursor

import (
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

const outputGuardLimit = 5 * time.Second

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

func TestReadOutputGraceWithoutReadDeadlineIsBounded(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	t.Cleanup(func() {
		closeQuietly(writer)
		closeQuietly(reader)
		select {
		case <-finished:
		case <-time.After(outputGuardLimit):
			t.Error("readOutput goroutine did not finish after the pipe was closed")
		}
	})
	exited := make(chan struct{})
	close(exited)
	proc := &turnProcess{
		stdout:      reader,
		exited:      exited,
		setDeadline: func(*os.File, time.Time) error { return os.ErrNoDeadline },
	}
	done := make(chan error, 1)
	go func() {
		defer close(finished)
		_, readErr := readOutput(proc, func() {}, func(pipe io.Reader) (struct{}, error) {
			_, err := io.ReadAll(pipe)
			return struct{}{}, err
		})
		done <- readErr
	}()
	select {
	case readErr := <-done:
		if !errors.Is(readErr, os.ErrClosed) {
			t.Fatalf("readOutput error = %v, want the read cut by the grace close", readErr)
		}
	case <-time.After(outputGuardLimit):
		t.Fatalf("readOutput did not return within %v", outputGuardLimit)
	}
}
