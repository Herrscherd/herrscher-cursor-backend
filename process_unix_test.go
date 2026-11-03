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
	pidPath := filepath.Join(t.TempDir(), "pid")
	stub := writeStubScript(t, "cat > /dev/null\n"+fmt.Sprintf(spawn, pidPath)+"\n"+turnResultLine)
	backend, err := NewBackend(ctx, Config{Kind: BackendKindStream, Cmd: stub})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	text, err := backend.Respond(ctx, contracts.Prompt{Content: "hi"}, nil)
	elapsed := time.Since(start)

	raw, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("grandchild pid not written: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		t.Fatal(convErr)
	}
	t.Cleanup(func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Errorf("cleanup kill of %d: %v", pid, killErr)
		}
	})
	return grandchildRun{text: text, err: err, elapsed: elapsed, pid: pid}
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
