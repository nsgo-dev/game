package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// runGoToolNSGo runs `go tool nsgo` with args in the current directory, the NSGo command of the module game is run from, so tests and benchmarks build as nsgo test builds them, with the app's linker flags and NSGo's check tags.
func runGoToolNSGo(args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", append([]string{"tool", "nsgo"}, args...)...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go tool nsgo %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
