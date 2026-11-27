package gamecli

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// schemaCommand writes the project's scene JSON Schema to stdout or -out. When the project has its own editor command (a main package in cmd/editor importing editorcmd, or a main built with the gameeditor tag), it runs that command with -schema so the project's registered component types and plugins appear in the schema; otherwise it writes the built-in scene schema.
var schemaCommand = &command{
	name:    "schema",
	args:    "[project-dir | game.yaml]",
	summary: "Write the project's scene JSON Schema, including its registered components and plugins, or the built-in scene schema when the project has no editor command of its own",
	define:  defineSchema,
	complete: map[string]completion{
		"": filePaths,
	},
}

func defineSchema(a *app, fs *flag.FlagSet) func() error {
	outPath := outFlag(fs, "", "write the JSON Schema to `file` instead of stdout")
	return func() error {
		if fs.NArg() > 1 {
			return usagef("expected at most one project directory or game.yaml, got %d arguments", fs.NArg())
		}
		target := "."
		if fs.NArg() == 1 {
			target = fs.Arg(0)
			if _, err := os.Stat(target); err != nil {
				return err
			}
		}

		data, err := generateProjectSchema(a, target)
		if err != nil {
			return err
		}
		if *outPath == "" {
			_, err = a.stdout.Write(data)
			return err
		}
		if dir := filepath.Dir(*outPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		return os.WriteFile(*outPath, data, 0o644)
	}
}

func generateProjectSchema(a *app, target string) ([]byte, error) {
	if root, inProject := editorcmd.ProjectRoot(target); inProject {
		run, ok, err := projectEditor(root)
		if err != nil {
			return nil, err
		}
		if ok {
			var buf bytes.Buffer
			cmd := append(slices.Clone(run), "-schema")
			if err := a.goRun(root, cmd, &buf, a.stderr); err != nil {
				return nil, fmt.Errorf("the project's editor (go %s in %s): %w", strings.Join(run, " "), root, err)
			}
			return buf.Bytes(), nil
		}
		if p, err := build.ImportDir(filepath.Join(root, "cmd", "editor"), 0); err == nil && p.Name == "main" {
			fmt.Fprintf(a.stderr, "schema: %s does not use package editorcmd, so it may not support -schema; writing the built-in scene schema instead\n", filepath.Join(root, "cmd", "editor"))
		}
	}
	s, err := sceneformat.SceneSchema(nil)
	if err != nil {
		return nil, err
	}
	return s.Marshal()
}
