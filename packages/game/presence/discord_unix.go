//go:build (darwin && !ios) || (linux && !android) || freebsd || openbsd || netbsd

package presence

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

func discordSocketDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		if !seen[clean] {
			seen[clean] = true
			dirs = append(dirs, clean)
		}
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		add(xdg)
		add(filepath.Join(xdg, "app", "com.discordapp.Discord"))
		add(filepath.Join(xdg, "snap.discord"))
	}
	add(os.Getenv("TMPDIR"))
	add(os.Getenv("TMP"))
	add(os.Getenv("TEMP"))
	add("/tmp")
	return dirs
}

func dialDiscordIPC(ctx context.Context) (io.ReadWriteCloser, error) {
	var dialer net.Dialer
	for _, dir := range discordSocketDirs() {
		for i := range 10 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			socketPath := filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i))
			conn, err := dialer.DialContext(ctx, "unix", socketPath)
			if err == nil {
				return conn, nil
			}
		}
	}
	return nil, ErrUnavailable
}
