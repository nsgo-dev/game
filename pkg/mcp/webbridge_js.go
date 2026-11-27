//go:build js && wasm

package mcp

import (
	"context"
	"encoding/json"

	"github.com/nsgo-dev/game/internal/webmcpbridge"
	"github.com/nsgo-dev/nsgo/core"
)

func (b *Bridge) attachWebBridge(sub *core.Scope) func() {
	if b == nil || sub == nil {
		return func() {}
	}
	return webmcpbridge.Register(webmcpbridge.Config{
		Source:   "mcp.Bridge",
		Priority: webmcpbridge.PriorityBridge,
		Post:     sub.Post,
		ListTools: func() []webmcpbridge.ToolInfo {
			tools := b.Tools()
			out := make([]webmcpbridge.ToolInfo, 0, len(tools))
			for _, t := range tools {
				out = append(out, webmcpbridge.ToolInfo{
					Name:        t.Name,
					Description: t.Description,
				})
			}
			return out
		},
		CallTool: func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
			return b.CallTool(ctx, name, args)
		},
	})
}
