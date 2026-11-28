// Package devplugin installs development-time plugin contributions (MCP bridge prefabs and playtest bots) separately from [github.com/nsgo-dev/game/pkg/gameplugin].
//
// Separating MCP and playtest wiring into this subpackage keeps shipped game binaries that import pkg/gameplugin from linking pkg/mcp, pkg/tooling/playtest, pkg/replay, and their transitive dependencies.
package devplugin
