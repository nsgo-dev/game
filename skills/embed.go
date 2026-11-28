// Package skills embeds the nsgo-game agent skill, so the editor's MCP server and the go tool game mcp proxy serve the skill in the version they were built with. It sits beside skills/nsgo-game rather than under pkg/ because go:embed cannot reach a parent directory.
package skills

import (
	"embed"
	"io/fs"
)

// fsys holds skills/nsgo-game: SKILL.md and its references.
//
//go:embed nsgo-game
var fsys embed.FS

// Files returns the paths of the skill's files relative to skills/nsgo-game, such as SKILL.md and references/mcp-tools.md, in lexical order.
func Files() []string {
	var out []string
	err := fs.WalkDir(fsys, "nsgo-game", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p[len("nsgo-game/"):])
		}
		return nil
	})
	if err != nil {
		// The embedded tree is fixed at build time, so a walk error is a broken build.
		panic(err)
	}
	return out
}

// ReadFile returns the bytes of the skill file at rel, a path that Files returns.
func ReadFile(rel string) ([]byte, error) {
	if !fs.ValidPath(rel) {
		return nil, &fs.PathError{Op: "read", Path: rel, Err: fs.ErrInvalid}
	}
	return fsys.ReadFile("nsgo-game/" + rel)
}
