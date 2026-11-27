// Command editor is the standalone desktop application for the NSGo Game Editor.
// Launched with no arguments (such as by double-clicking the packaged desktop app),
// it opens on the Start Screen with recent projects, templates, and the built-in demo.
// Arguments match `nsgo game editor`: an optional -library index, a project directory,
// game.yaml, or scene YAML file, and optionally an animation clip and object ID.
package main

import (
	"os"
	"strings"

	"github.com/nsgo-dev/game/internal/gamecli"
)

func main() {
	os.Exit(gamecli.Run(editorArgs(os.Args[1:])))
}

// editorArgs prefixes the editor subcommand and drops macOS Finder's -psn_* process serial flag.
func editorArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, "editor")
	for _, a := range args {
		if strings.HasPrefix(a, "-psn_") {
			continue
		}
		out = append(out, a)
	}
	return out
}
