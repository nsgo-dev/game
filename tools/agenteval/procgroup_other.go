//go:build !unix

package main

import "os/exec"

// killProcessGroupOnCancel leaves cmd's default cancellation, which kills only the shell, on systems without Unix process groups.
func killProcessGroupOnCancel(*exec.Cmd) {}
