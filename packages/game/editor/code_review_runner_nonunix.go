//go:build !ios && !android && !js && !wasm && !(darwin || linux || freebsd || openbsd || netbsd)

package editor

import (
	"os/exec"
	"time"
)

func configureCodeReviewCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.WaitDelay = 2 * time.Second
}
