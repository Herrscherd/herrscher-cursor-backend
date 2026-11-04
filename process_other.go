//go:build !unix

package cursor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
)

func configureChild(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return killGroup(cmd.Process.Pid)
	}
}

func killGroup(pid int) error {
	if exec.CommandContext(context.Background(), "taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run() == nil {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
