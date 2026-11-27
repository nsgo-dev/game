package editormcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/project"
)

// EndpointDirEnv names the environment variable that, when set, holds the directory the editors' endpoints live in instead of the user's cache directory, as a test or a sandboxed agent sets it for both the editor and go tool game mcp.
const EndpointDirEnv = "NSGO_GAME_EDITOR_ENDPOINTS"

// maxSocketPath is the longest socket path every platform accepts: sockaddr_un holds 104 bytes on macOS and the BSDs and 108 on Linux and Windows, terminator included.
const maxSocketPath = 103

var (
	// ErrNoEditor reports that no editor has the project open: its endpoint does not exist or nothing answers on it.
	ErrNoEditor = errors.New("editormcp: no editor has the project open")
	// ErrInUse reports that another editor already serves the project's endpoint.
	ErrInUse = errors.New("editormcp: another editor already serves the project")
)

// ProjectRoot returns the absolute, symlink-free directory of the project at path: a game.yaml, the directory holding one, or any directory below that one, as an agent started in a subdirectory of the project names it. It returns an error naming path when no game.yaml is found on the way up.
func ProjectRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Base(abs) == project.FileName {
		abs = filepath.Dir(abs)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	for dir := abs; ; {
		if info, err := os.Stat(filepath.Join(dir, project.FileName)); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("editormcp: no %s in %s or a directory above it; run this in a game's project or name it", project.FileName, path)
		}
		dir = parent
	}
}

// Endpoint returns the path of the local socket the editor with the project at root open serves on: a file named after a hash of the project's absolute path in the endpoint directory, which is $NSGO_GAME_EDITOR_ENDPOINTS when set (EndpointDirEnv) and otherwise nsgo-game/editor in the user's cache directory. root should come from ProjectRoot, so that every spelling of the project's path names the same endpoint. The endpoint is a Unix domain socket, which macOS, Linux and Windows 10 and later all have; it is never a network port.
func Endpoint(root string) (string, error) {
	dir, err := endpointDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".sock")
	if len(path) > maxSocketPath {
		return "", fmt.Errorf("editormcp: endpoint %s is longer than the %d bytes a socket path may have; set %s to a shorter directory", path, maxSocketPath, EndpointDirEnv)
	}
	return path, nil
}

// endpointDir returns the directory endpoints live in.
func endpointDir() (string, error) {
	if dir := os.Getenv(EndpointDirEnv); dir != "" {
		return filepath.Abs(dir)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("editormcp: no directory for the editor's endpoint: %w; set %s", err, EndpointDirEnv)
	}
	return filepath.Join(cache, "nsgo-game", "editor"), nil
}

// Listen opens the endpoint for the project at root, which only this user can connect to: its directory is created private to the user. When the endpoint is left over from an editor that quit without closing it, Listen replaces it; when another editor answers on it, Listen returns ErrInUse.
func Listen(root string) (net.Listener, error) {
	path, err := Endpoint(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		if conn, err := net.Dial("unix", path); err == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w at %s", ErrInUse, root)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// Dial connects to the editor that has the project at root open. It returns an error wrapping ErrNoEditor, which says how to open the project, when none does.
func Dial(ctx context.Context, root string) (net.Conn, error) {
	path, err := Endpoint(root)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w at %s: open it with `go tool game editor %s` and try again", ErrNoEditor, root, root)
	}
	return conn, nil
}
