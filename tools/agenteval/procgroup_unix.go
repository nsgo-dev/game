//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel starts cmd in its own process group and kills the whole group when its context ends, so an agent's child processes do not outlive the timeout.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
