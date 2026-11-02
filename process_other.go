//go:build !unix

package cursor

import "os/exec"

func configureChild(*exec.Cmd) {}
