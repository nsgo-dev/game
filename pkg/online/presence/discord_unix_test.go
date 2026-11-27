//go:build (darwin && !ios) || (linux && !android)

package presence_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/presence"
)

func TestDiscordUnixSocketDiscovery(t *testing.T) {
	// Keep the socket path short so macOS's 104-byte sun_path limit is never exceeded.
	dir, err := os.MkdirTemp("/tmp", "nsgo-dp-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "discord-ipc-0")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	srv := &mockDiscordServer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		srv.wg.Add(1)
		srv.serve(conn)
	}()

	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("TMPDIR", dir)

	// Default Dial discovers discord-ipc-0 over a real Unix domain socket.
	d, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "unix-socket-test",
		PID:      9001,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := d.Set(context.Background(), presence.Activity{
		Mode:  "Arcade",
		Level: "Neon City",
	}); err != nil {
		t.Fatalf("Set over real unix socket: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
	srv.wait()

	frames := srv.recordedFrames()
	if len(frames) < 2 {
		t.Fatalf("expected at least 2 frames over unix socket, got %d", len(frames))
	}
	var setMsg struct {
		Args struct {
			Activity struct {
				Details string `json:"details"`
				State   string `json:"state"`
			} `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(frames[1].Payload, &setMsg); err != nil {
		t.Fatal(err)
	}
	if setMsg.Args.Activity.Details != "Arcade" || setMsg.Args.Activity.State != "Neon City" {
		t.Fatalf("unix socket activity = %+v", setMsg.Args.Activity)
	}
}
