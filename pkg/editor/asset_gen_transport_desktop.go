//go:build (darwin || linux || freebsd || openbsd || netbsd || windows) && !android && !ios

package editor

import (
	"errors"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newAssetGeneratorCommandTransport(dir, command string, args []string) (sdkmcp.Transport, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("editor: generator server command is empty")
	}
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return &sdkmcp.CommandTransport{Command: cmd}, nil
}
