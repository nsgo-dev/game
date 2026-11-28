package presence_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/presence"
	"github.com/nsgo-dev/game/pkg/online/steamworks"
	"github.com/nsgo-dev/nsgo/core"
)

func TestSteamWritesThroughSharedClientAndLeavesItOpen(t *testing.T) {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	drv := steamworks.NewFakeDriver()
	client, err := steamworks.Open(root, steamworks.Options{Driver: drv, Args: []string{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := presence.NewSteam(presence.SteamConfig{Client: client, AutoInit: true})
	ctx := context.Background()

	if err := s.Set(ctx, presence.Activity{Level: "Forest 1-2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := drv.RichPresence()["level"]; got != "Forest 1-2" {
		t.Fatalf("level through the shared client = %q, want Forest 1-2", got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if client.Closed() || drv.ShutdownCount() != 0 || drv.InitCount() != 1 {
		t.Fatalf("presence changed the shared client's lifecycle: closed=%v init=%d shutdown=%d", client.Closed(), drv.InitCount(), drv.ShutdownCount())
	}

	root.Dispose()
	s2 := presence.NewSteam(presence.SteamConfig{Client: client})
	if err := s2.Set(ctx, presence.Activity{Level: "x"}); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("Set after the client closed: err = %v, want ErrUnavailable", err)
	}
}
