//go:build (!darwin && !linux && !freebsd && !openbsd && !netbsd && !windows) || android || ios

package editor

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

const canConnectRemoteCommand = false

func newRemoteCommandTransport(string, ...string) (sdkmcp.Transport, error) {
	return nil, ErrRemoteUnsupported
}
