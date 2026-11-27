package webmcpbridge_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/webmcpbridge"
)

func TestIsSameOriginPath(t *testing.T) {
	valid := []string{
		"/__nsgo_mcp/info",
		"/__nsgo_mcp/bridge.js",
		"/app/info?v=1",
	}
	for _, p := range valid {
		if !webmcpbridge.IsSameOriginPath(p) {
			t.Errorf("IsSameOriginPath(%q) = false, want true", p)
		}
	}

	invalid := []string{
		"",
		"http://evil.example/__nsgo_mcp/bridge.js",
		"https://evil.example/bridge.js",
		"//evil.example/bridge.js",
		"relative/path.js",
		"/__nsgo_mcp/\\evil.js",
		"/__nsgo_mcp/info\r\nX-Injected: 1",
		"/__nsgo_mcp/bridge.js ",
	}
	for _, p := range invalid {
		if webmcpbridge.IsSameOriginPath(p) {
			t.Errorf("IsSameOriginPath(%q) = true, want false", p)
		}
	}
}
