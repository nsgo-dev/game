// Package devtools installs development-time plugin contributions (MCP bridge prefabs and playtest bots) separately from [github.com/nsgo-dev/game/pkg/plugin].
//
// Separating MCP and playtest wiring into this subpackage keeps shipped game binaries that import pkg/plugin from linking pkg/mcp, pkg/playtest, pkg/replay, and their transitive dependencies.
package devtools
