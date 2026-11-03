package cursor

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const childOutputGrace = time.Second

type turnProcess struct {
	cmd     *exec.Cmd
	stdout  *os.File
	exited  chan struct{}
	waitErr error
}

type readOutcome[Result any] struct {
	value Result
	err   error
}

func startTurn(ctx context.Context, argv []string, dir, prompt string, stderr io.Writer) (*turnProcess, error) {
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
	proc := &turnProcess{cmd: cmd, stdout: stdoutReader, exited: make(chan struct{})}
	go proc.reap()
	if childEnds != nil {
		return nil, errors.Join(childEnds, killGroup(cmd.Process.Pid), proc.wait(), stdinWriter.Close(), stdoutReader.Close())
	}
	go writePrompt(stdinWriter, prompt)
	return proc, nil
}

func writePrompt(pipe *os.File, prompt string) {
	if _, err := io.Copy(pipe, strings.NewReader(prompt)); err != nil {
		closeQuietly(pipe)
		return
	}
	closeQuietly(pipe)
}

func closeQuietly(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}

func (proc *turnProcess) reap() {
	waitErr := proc.cmd.Wait()
	proc.waitErr = errors.Join(waitErr, killGroup(proc.cmd.Process.Pid))
	close(proc.exited)
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
		if err := proc.stdout.SetReadDeadline(time.Now().Add(childOutputGrace)); err != nil {
			stop()
			var zero Result
			return zero, err
		}
		outcome = <-reads
	}
	if outcome.err != nil {
		stop()
	}
	return outcome.value, outcome.err
}

func ignoreOwnDeadline(err error) error {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return nil
	}
	return err
}
