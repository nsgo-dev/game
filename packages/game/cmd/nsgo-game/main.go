// Command nsgo-game installs the `nsgo-game` extension binary on PATH so `nsgo game` works outside a Go module (for example, to run `nsgo game new` before creating a project) via NSGo's extension mechanism. Inside a module whose go.mod declares `tool github.com/nsgo-dev/game/packages/game/cmd/game`, `nsgo game` runs the module's pinned `game` tool instead.
package main

import "github.com/nsgo-dev/game/packages/game/internal/gamecli"

func main() {
	gamecli.Main()
}
