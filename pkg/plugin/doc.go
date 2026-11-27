// Package plugin defines the runtime plugin contract and deterministic installer for Go plugins.
//
// Because iOS and web targets link all native code at compile time, Go plugins are ordinary Go packages wired into a game project via go.mod. A plugin's runtime package exposes a constructor returning a [Plugin] value (conventionally func Plugin() plugin.Plugin), and the game installs every active plugin onto a [Host] with [Install].
//
// [Install] validates every plugin's [Plugin.API] against [APIVersion], refuses duplicate plugin names, and verifies that every contributed component, system, pixel effect, data table row type, and console command name is free before registering anything. Contributions are applied in deterministic order: plugins in argument order, slice contributions in declaration order, and map contributions in sorted key order.
//
// [Host.Scheduler] receives four standard stage anchor pairs ([StageInput], [StageUpdate], [StagePhysics], [StageLate]) added once per scheduler so third-party systems can attach to a stage without knowing the game's own system names. [Host.OnWorld] applies every installed plugin's [Plugin.Codecs] callback to each [entities.World] the game creates.
//
// Process-global registries ([pixelfx]) are written once per process per owning plugin so repeated [Install] calls in tests or after an editor reload neither panic nor overwrite another plugin's entry.
//
// Development-time contributions that depend on heavier packages (MCP bridge prefabs and playtest bots) live in the separate [github.com/nsgo-dev/game/pkg/plugin/devtools] package so shipped games that import pkg/plugin do not link pkg/mcp, pkg/playtest, or pkg/editor.
package plugin
