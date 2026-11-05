package cursor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const childOutputGrace = time.Second

type stdoutDeadline func(*os.File, time.Time) error

func setReadDeadline(file *os.File, deadline time.Time) error {
	return file.SetReadDeadline(deadline)
}

type turnProcess struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *os.File
	setDeadline stdoutDeadline
	prompted    chan struct{}
	exited      chan struct{}
	promptErr   error
	waitErr     error
}

type readOutcome[Result any] struct {
	value Result
	err   error
}

func startTurn(ctx context.Context, argv []string, dir, prompt string, stderr io.Writer, setDeadline stdoutDeadline) (*turnProcess, error) {
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(err, stdinReader.Close(), stdinWriter.Close())
	}
	cmd := turnCommand(ctx, argv, dir, stderr)
	cmd.Stdin = stdinReader
	cmd.Stdout = stdoutWriter
	startErr := cmd.Start()
	childEnds := errors.Join(stdinReader.Close(), stdoutWriter.Close())
	if startErr != nil {
		return nil, errors.Join(startErr, childEnds, stdinWriter.Close(), stdoutReader.Close())
	}
	proc := &turnProcess{
		cmd:         cmd,
		stdin:       stdinWriter,
		stdout:      stdoutReader,
		setDeadline: setDeadline,
		prompted:    make(chan struct{}),
		exited:      make(chan struct{}),
	}
	go proc.writePrompt(prompt)
	go proc.reap()
	if childEnds != nil {
		return nil, errors.Join(childEnds, killGroup(cmd.Process), proc.wait(), stdoutReader.Close())
	}
	return proc, nil
}

func (proc *turnProcess) writePrompt(prompt string) {
	defer close(proc.prompted)
	_, err := io.Copy(proc.stdin, strings.NewReader(prompt))
	closeQuietly(proc.stdin)
	if err != nil && !isStdinGone(err) {
		proc.promptErr = err
	}
}

func isStdinGone(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed)
}

func (proc *turnProcess) reap() {
	waitErr := proc.cmd.Wait()
	killErr := killGroup(proc.cmd.Process)
	closeQuietly(proc.stdin)
	<-proc.prompted
	proc.waitErr = errors.Join(waitErr, killErr, proc.promptErr)
	close(proc.exited)
}

func killAfterTreeFailure(process *os.Process, treeErr error) error {
	slog.Warn("taskkill failed, killing the cursor-agent process only", "error", treeErr)
	err := process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func closeQuietly(file io.Closer) {
	if err := file.Close(); err != nil {
		return
	}
}

func (proc *turnProcess) closeStdout() error {
	if err := proc.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

func (proc *turnProcess) wait() error {
	<-proc.exited
	return proc.waitErr
}

func readOutput[Result any](proc *turnProcess, stop context.CancelFunc, read func(io.Reader) (Result, error)) (Result, error) {
	reads := make(chan readOutcome[Result], 1)
	go func() {
		value, err := read(proc.stdout)
		reads <- readOutcome[Result]{value: value, err: err}
	}()
	var outcome readOutcome[Result]
	select {
	case outcome = <-reads:
	case <-proc.exited:
		graced, err := awaitOutputGrace(proc, reads)
		if err != nil {
			stop()
			var zero Result
			return zero, err
		}
		outcome = graced
	}
	if outcome.err != nil {
		stop()
	}
	return outcome.value, outcome.err
}

func awaitOutputGrace[Result any](proc *turnProcess, reads <-chan readOutcome[Result]) (readOutcome[Result], error) {
	err := proc.setDeadline(proc.stdout, time.Now().Add(childOutputGrace))
	if err == nil {
		return <-reads, nil
	}
	if !errors.Is(err, os.ErrNoDeadline) {
		return readOutcome[Result]{}, err
	}
	timer := time.NewTimer(childOutputGrace)
	defer timer.Stop()
	select {
	case outcome := <-reads:
		return outcome, nil
	case <-timer.C:
	}
	if err := proc.closeStdout(); err != nil {
		return readOutcome[Result]{}, err
	}
	return <-reads, nil
}

func ignoreOwnCut(err error) error {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
