//go:build (darwin || linux || freebsd || openbsd || netbsd) && !android && !ios

package editor

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func configureCodeReviewCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Terminate the entire process group so nested go/nsgo/test binaries do not outlive cancellation.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
}
