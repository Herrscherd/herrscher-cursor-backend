//go:build unix

package cursor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const turnResultLine = `printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-7"}\n'`

type grandchildRun struct {
	text    string
	err     error
	elapsed time.Duration
	pid     int
}

func runTurnWithGrandchild(ctx context.Context, t *testing.T, spawn string) grandchildRun {
	t.Helper()
	pidPath := grandchildPidFile(t)
	stub := writeStubScript(t, "cat > /dev/null\n"+fmt.Sprintf(spawn, pidPath)+"\n"+turnResultLine)
	backend, err := NewBackend(ctx, Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	text, err := backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	elapsed := time.Since(start)

	return grandchildRun{text: text, err: err, elapsed: elapsed, pid: readGrandchildPid(t, pidPath)}
}

func grandchildPidFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "pid")
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
	t.Cleanup(func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Errorf("cleanup kill of %d: %v", pid, killErr)
		}
	})
	return pid
}

func assertGrandchildGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive: kill(pid, 0) = %v, want ESRCH", pid, err)
		}
		<-time.After(10 * time.Millisecond)
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

	_, err = backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	if _, ok := errors.AsType[*OutputTooLargeError](err); !ok {
		t.Fatalf("err = %v, want an output too large error", err)
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

	_, err = backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	if _, ok := errors.AsType[*LineTooLongError](err); !ok {
		t.Fatalf("err = %v, want a line too long error", err)
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

	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		text, err := backend.Respond(t.Context(), contracts.Prompt{Content: strings.Repeat("x", 1<<20)}, nil)
		done <- outcome{text: text, err: err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Respond error = %v, want nil", got.err)
		}
		if got.text != "ok" {
			t.Fatalf("Respond text = %q, want ok", got.text)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("Respond took %v, want it bounded by the output grace", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Respond did not return: the prompt writer was not released and joined")
	}
	if err := syscall.Kill(readGrandchildPid(t, pidPath), 0); err != nil {
		t.Fatalf("escaped grandchild is gone: %v; the stdin holder was not exercised", err)
	}
}
