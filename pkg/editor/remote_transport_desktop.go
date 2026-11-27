//go:build (darwin || linux || freebsd || openbsd || netbsd || windows) && !android && !ios

package editor

import (
	"os/exec"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const canConnectRemoteCommand = true

func newRemoteCommandTransport(dir string, extraArgs ...string) (sdkmcp.Transport, error) {
	args := append([]string{"tool", "nsgo", "mcp"}, extraArgs...)
	cmd := exec.Command("go", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return &sdkmcp.CommandTransport{Command: cmd}, nil
}
