// Package skills embeds the versioned game-agent skill served by the editor MCP server.
package skills

import (
	"embed"
	"io/fs"
)

// FS contains the nsgo-game skill and its references.
//
//go:embed nsgo-game/**
var FS embed.FS

// Files returns the embedded skill paths in deterministic order.
func Files() []string {
	var out []string
	_ = fs.WalkDir(FS, "nsgo-game", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, path)
		}
		return err
	})
	return out
}
