package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// Root is the game's root NSGo component. It shows the room rules from coop.yaml; replace it with the game's lobby and play screens, which talk to a Server over the game's netplay transport.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, Project.Title())
	return core.Text{Content: fmt.Sprintf("Co-op room: up to %d players, %d Hz authoritative tick", Coop.MaxPlayers, Coop.TickRate)}
}
