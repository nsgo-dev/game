//go:build (darwin || linux || freebsd || openbsd || netbsd || windows) && !android && !ios

package editor

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/internal/gotoolchain"
)

const canConnectRemoteCommand = true

func newRemoteCommandTransport(dir string, extraArgs ...string) (sdkmcp.Transport, error) {
	args := append([]string{"tool", "nsgo", "mcp"}, extraArgs...)
	cmd := gotoolchain.Command(args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return &sdkmcp.CommandTransport{Command: cmd}, nil
}
