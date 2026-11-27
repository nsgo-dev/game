package webmcpbridge

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
)

const (
	// PriorityLoop is the fallback registration priority used by game.Loop.Run
	// when a game does not attach a full pkg/mcp.Bridge.
	PriorityLoop = 1
	// PriorityBridge is the full pkg/mcp.Bridge registration priority,
	// superseding any game.Loop fallback while attached.
	PriorityBridge = 2
)

// ToolInfo describes one game MCP tool exposed through window.__nsgoMCP.listTools().
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Config registers a runtime's MCP tool dispatcher on window.__nsgoMCP in js/wasm web builds.
type Config struct {
	// Source identifies who registered the runtime (for example "mcp.Bridge" or "game.Loop").
	Source string
	// Priority selects which active registration wins when more than one is attached.
	Priority int
	// Post schedules fn to run on the engine/graph goroutine (typically core.Scope.Post).
	// When nil, CallTool runs directly.
	Post func(fn func())
	// ListTools returns the tools currently exposed by this registration.
	ListTools func() []ToolInfo
	// CallTool invokes a named tool with JSON object arguments on the engine goroutine
	// and returns its JSON-encoded result.
	CallTool func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
}

// IsSameOriginPath reports whether raw is a safe same-origin absolute URL path
// (starts with "/" and not "//", contains no backslashes, whitespace, or control characters).
func IsSameOriginPath(raw string) bool {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") {
		return false
	}
	for _, r := range raw {
		if r == '\\' || r == 0 || unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
