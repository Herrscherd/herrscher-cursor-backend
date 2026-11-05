//go:build !unix

package cursor

import (
	"context"
	"os"
	"os/exec"
	"strconv"
)

func configureChild(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return killGroup(cmd.Process)
	}
}

func killGroup(process *os.Process) error {
	treeErr := exec.CommandContext(context.Background(), "taskkill", "/T", "/F", "/PID", strconv.Itoa(process.Pid)).Run()
	if treeErr == nil {
		return nil
	}
	return killAfterTreeFailure(process, treeErr)
}
