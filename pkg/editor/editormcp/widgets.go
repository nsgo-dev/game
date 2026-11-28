package editormcp

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// mcpAppsExtension is the client capability extension that declares MCP Apps support.
	mcpAppsExtension = "io.modelcontextprotocol/ui"
	// mcpAppsMIME is the MIME type a client lists under mcpAppsExtension to accept HTML widgets.
	mcpAppsMIME = "text/html;profile=mcp-app"
	// widgetURIPrefix is the prefix every widget resource URI shares.
	widgetURIPrefix = "ui://nsgo-game/"
	// PlaytestWidgetURI is the MCP Apps widget that game_playtest and playtest_report results point at.
	PlaytestWidgetURI = widgetURIPrefix + "playtest-report"
	// ViewportWidgetURI is the MCP Apps widget that viewport_image results point at.
	ViewportWidgetURI = widgetURIPrefix + "viewport"
)

// widgetCSP allows no network connections, no scripts, inline styles only, and images only from data: and blob: URLs.
const widgetCSP = "default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"

const widgetStyle = `<style>body{font:14px system-ui;color:#e5e7eb;background:#111827;padding:16px}h1{font-size:18px}</style>`

const playtestWidgetHTML = `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="` + widgetCSP + `"><title>NSGo playtest report</title>` + widgetStyle + `<h1>Playtest report</h1>`

const viewportWidgetHTML = `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="` + widgetCSP + `"><title>NSGo viewport</title>` + widgetStyle + `<h1>Viewport</h1>`

// sessionSupportsMCPApps reports whether the client of ss declared mcpAppsMIME under mcpAppsExtension when it initialized.
func sessionSupportsMCPApps(ss *mcp.ServerSession) bool {
	if ss == nil {
		return false
	}
	params := ss.InitializeParams()
	if params == nil {
		return false
	}
	return capabilitiesSupportMCPApps(params.Capabilities)
}

func capabilitiesSupportMCPApps(caps *mcp.ClientCapabilities) bool {
	if caps == nil {
		return false
	}
	settings, ok := caps.Extensions[mcpAppsExtension].(map[string]any)
	if !ok {
		return false
	}
	switch types := settings["mimeTypes"].(type) {
	case []any:
		return slices.Contains(types, any(mcpAppsMIME))
	case []string:
		return slices.Contains(types, mcpAppsMIME)
	}
	return false
}

// forwardedCapabilities is what the proxy declares to the editor for its client: only the MCP Apps extension, when the client declared it, since the proxy handles none of the other client features (sampling, elicitation, roots) itself.
func forwardedCapabilities(caps *mcp.ClientCapabilities) *mcp.ClientCapabilities {
	if !capabilitiesSupportMCPApps(caps) {
		return nil
	}
	settings := maps.Clone(caps.Extensions[mcpAppsExtension].(map[string]any))
	out := &mcp.ClientCapabilities{}
	out.AddExtension(mcpAppsExtension, settings)
	return out
}

// gateMCPApps hides the widget resources from resources/list and refuses to read them for a session whose client did not declare MCP Apps support.
func gateMCPApps(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "resources/list" && method != "resources/read" {
			return next(ctx, method, req)
		}
		ss, _ := req.GetSession().(*mcp.ServerSession)
		if sessionSupportsMCPApps(ss) {
			return next(ctx, method, req)
		}
		if read, ok := req.(*mcp.ReadResourceRequest); ok && read.Params != nil && strings.HasPrefix(read.Params.URI, widgetURIPrefix) {
			return nil, fmt.Errorf("editormcp: reading %s requires the client to declare %s under the %s extension", read.Params.URI, mcpAppsMIME, mcpAppsExtension)
		}
		res, err := next(ctx, method, req)
		if listed, ok := res.(*mcp.ListResourcesResult); ok && listed != nil {
			listed.Resources = slices.DeleteFunc(listed.Resources, func(r *mcp.Resource) bool {
				return r != nil && strings.HasPrefix(r.URI, widgetURIPrefix)
			})
		}
		return res, err
	}
}

// withMCPAppsWidget points a successful game_playtest, playtest_report, or viewport_image result at its widget in _meta.ui when the session's client declared MCP Apps support, and returns every other result unchanged.
func withMCPAppsWidget(tool string, r *mcp.CallToolResult, ss *mcp.ServerSession) *mcp.CallToolResult {
	if r == nil || r.IsError || !sessionSupportsMCPApps(ss) {
		return r
	}
	var uri string
	switch tool {
	case ToolGamePlaytest, ToolPlaytestReport:
		uri = PlaytestWidgetURI
	case ToolViewportImage:
		uri = ViewportWidgetURI
	default:
		return r
	}
	if r.Meta == nil {
		r.Meta = mcp.Meta{}
	}
	r.Meta["ui"] = map[string]any{"resourceUri": uri}
	return r
}
