//go:build !unix

package cursor

import "os/exec"

func configureChild(*exec.Cmd) {}

func killGroup(int) error { return nil }
