//go:build unix

package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	turnResultLine = `printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-7"}\n'`
	respondLimit   = 5 * time.Second
)

type grandchildRun struct {
	text    string
	err     error
	elapsed time.Duration
	pid     int
}

type respondOutcome struct {
	text    string
	err     error
	elapsed time.Duration
}

func runTurnWithGrandchild(ctx context.Context, t *testing.T, spawn string) grandchildRun {
	t.Helper()
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, "cat > /dev/null\n"+fmt.Sprintf(spawn, pidPath)+"\n"+turnResultLine)
	backend, err := NewBackend(ctx, Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	outcome := respondWithGuard(ctx, t, backend, "hi", respondLimit)
	return grandchildRun{text: outcome.text, err: outcome.err, elapsed: outcome.elapsed, pid: readGrandchildPid(t, pidPath)}
}

func respondWithGuard(ctx context.Context, t *testing.T, backend contracts.Backend, content string, limit time.Duration) respondOutcome {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(respondLimit):
			t.Error("Respond goroutine did not finish after its context was cancelled")
		}
	})
	done := make(chan respondOutcome, 1)
	start := time.Now()
	go func() {
		defer close(finished)
		text, err := backend.Respond(ctx, contracts.Prompt{Content: content}, nil)
		done <- respondOutcome{text: text, err: err, elapsed: time.Since(start)}
	}()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(limit):
		t.Fatalf("Respond did not return within %v", limit)
		return respondOutcome{}
	}
}

func grandchildPidFile(t *testing.T) string {
	t.Helper()
	pidPath := filepath.Join(t.TempDir(), "pid")
	t.Cleanup(func() {
		killRecordedGrandchild(t, pidPath)
	})
	return pidPath
}

func killRecordedGrandchild(t *testing.T, pidPath string) {
	t.Helper()
	raw, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("read grandchild pid: %v", err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Errorf("parse grandchild pid: %v", err)
		return
	}
	if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		t.Errorf("cleanup kill of %d: %v", pid, killErr)
	}
}

func readGrandchildPid(t *testing.T, pidPath string) int {
	t.Helper()
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("grandchild pid not written: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func assertGrandchildGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !grandchildGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive: want ESRCH or a zombie", pid)
		}
		<-time.After(10 * time.Millisecond)
	}
}

func grandchildGone(pid int) bool {
	err := syscall.Kill(pid, 0)
	return errors.Is(err, syscall.ESRCH) || isZombie(pid)
}

func isZombie(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	content := string(stat)
	fields := strings.Fields(content[strings.LastIndexByte(content, ')')+1:])
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func TestAssertGrandchildGoneAcceptsZombie(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for !isZombie(cmd.Process.Pid) {
		select {
		case <-timeout:
			t.Fatal("child did not become a zombie")
		case <-time.After(10 * time.Millisecond):
		}
	}
	assertGrandchildGone(t, cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("reap zombie: %v", err)
	}
}

func TestSuccessfulTurnKillsBackgroundGrandchildBeforeDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	run := runTurnWithGrandchild(ctx, t, "sleep 25 &\necho $! > %[1]s")
	if run.err != nil {
		t.Fatalf("Respond error = %v, want nil", run.err)
	}
	if run.text != "ok" {
		t.Fatalf("Respond text = %q, want ok", run.text)
	}
	if run.elapsed > 2*time.Second {
		t.Fatalf("Respond took %v, want well under the 3s deadline", run.elapsed)
	}
	assertGrandchildGone(t, run.pid)
}

func TestSuccessfulTurnKillsGrandchildWithoutDeadline(t *testing.T) {
	run := runTurnWithGrandchild(t.Context(), t, "sleep 8 &\necho $! > %[1]s")
	if run.err != nil {
		t.Fatalf("Respond error = %v, want nil", run.err)
	}
	if run.text != "ok" {
		t.Fatalf("Respond text = %q, want ok", run.text)
	}
	if run.elapsed > 2*time.Second {
		t.Fatalf("Respond took %v, want it to return without waiting for the grandchild", run.elapsed)
	}
	assertGrandchildGone(t, run.pid)
}

func TestSuccessfulTurnReturnsWhenGrandchildEscapesGroup(t *testing.T) {
	run := runTurnWithGrandchild(t.Context(), t, "setsid sh -c 'echo $$ > %[1]s; exec sleep 25' &\nwhile [ ! -s %[1]s ]; do :; done")
	if run.err != nil {
		t.Fatalf("Respond error = %v, want nil", run.err)
	}
	if run.text != "ok" {
		t.Fatalf("Respond text = %q, want ok", run.text)
	}
	if run.elapsed > 5*time.Second {
		t.Fatalf("Respond took %v, want it bounded by the output grace", run.elapsed)
	}
	if err := syscall.Kill(run.pid, 0); err != nil {
		t.Fatalf("escaped grandchild %d is gone: %v; the escape path was not exercised", run.pid, err)
	}
}

func TestOneShotKillsBackgroundChildAfterOverCap(t *testing.T) {
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, fmt.Sprintf("cat > /dev/null\nsleep 25 &\necho $! > %s\nexec head -c 1073741824 /dev/zero", pidPath))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	backend, err := NewBackend(ctx, Config{Kind: BackendKindOneShot, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	outcome := respondWithGuard(ctx, t, backend, "hi", respondLimit)
	if _, ok := errors.AsType[*OutputTooLargeError](outcome.err); !ok {
		t.Fatalf("err = %v, want an output too large error", outcome.err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Respond waited for a background child until the deadline: %v", ctx.Err())
	}
	assertGrandchildGone(t, readGrandchildPid(t, pidPath))
}

func TestRespondKillsBackgroundChildAfterOverlongLine(t *testing.T) {
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, fmt.Sprintf("cat > /dev/null\nsleep 25 &\necho $! > %s\nhead -c %d /dev/zero\nexec sleep 600", pidPath, 2*maxStreamLine))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	backend, err := NewBackend(ctx, Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	outcome := respondWithGuard(ctx, t, backend, "hi", respondLimit)
	if _, ok := errors.AsType[*LineTooLongError](outcome.err); !ok {
		t.Fatalf("err = %v, want a line too long error", outcome.err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Respond waited for a background child until the deadline: %v", ctx.Err())
	}
	assertGrandchildGone(t, readGrandchildPid(t, pidPath))
}

func TestSuccessfulTurnReturnsWhenEscapedGrandchildHoldsStdin(t *testing.T) {
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, fmt.Sprintf("exec 3<&0\nsetsid sh -c 'echo $$ > %[1]s; exec sleep 25' 0<&3 &\nwhile [ ! -s %[1]s ]; do :; done\n%[2]s", pidPath, turnResultLine))
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	outcome := respondWithGuard(t.Context(), t, backend, strings.Repeat("x", 1<<20), 10*time.Second)
	if outcome.err != nil {
		t.Fatalf("Respond error = %v, want nil", outcome.err)
	}
	if outcome.text != "ok" {
		t.Fatalf("Respond text = %q, want ok", outcome.text)
	}
	if outcome.elapsed > 5*time.Second {
		t.Fatalf("Respond took %v, want it bounded by the output grace", outcome.elapsed)
	}
	if err := syscall.Kill(readGrandchildPid(t, pidPath), 0); err != nil {
		t.Fatalf("escaped grandchild is gone: %v; the stdin holder was not exercised", err)
	}
}

func TestReadOutputGraceWithoutReadDeadlineIsBounded(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeQuietly(writer)
	})
	exited := make(chan struct{})
	close(exited)
	proc := &turnProcess{stdout: reader, exited: exited, setDeadline: func(*os.File, time.Time) error { return os.ErrNoDeadline }}

	start := time.Now()
	_, err = readOutput(proc, func() {}, func(pipe io.Reader) (struct{}, error) {
		_, readErr := io.ReadAll(pipe)
		return struct{}{}, readErr
	})
	elapsed := time.Since(start)

	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("readOutput error = %v, want the read cut by the grace close", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("readOutput took %v, want it bounded by the output grace", elapsed)
	}
}

func TestOneShotKeepsOutputWhenEscapedHolderHasNoReadDeadline(t *testing.T) {
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, fmt.Sprintf("cat > /dev/null\nsetsid sh -c 'echo $$ > %[1]s; exec sleep 25' &\nwhile [ ! -s %[1]s ]; do :; done\nprintf ok", pidPath))
	backend, err := NewBackend(t.Context(), Config{Kind: BackendKindOneShot, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}
	oneShot, ok := backend.(*oneShotResponder)
	if !ok {
		t.Fatalf("backend is %T, want *oneShotResponder", backend)
	}
	oneShot.setDeadline = func(*os.File, time.Time) error { return os.ErrNoDeadline }

	outcome := respondWithGuard(t.Context(), t, backend, "hi", respondLimit)
	if outcome.err != nil {
		t.Fatalf("Respond error = %v, want nil", outcome.err)
	}
	if outcome.text != "ok" {
		t.Fatalf("Respond text = %q, want ok", outcome.text)
	}
}
