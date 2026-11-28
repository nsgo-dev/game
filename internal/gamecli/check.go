package gamecli

import (
	"errors"
	"flag"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

// checkCommand checks the scenes, maps and atlases under a directory and the references between them, printing each problem with its file and line. When the directory is a project with its own editor command (a main package in cmd/editor importing editorcmd, or a main built with the gameeditor tag), it runs that command with -check so the project's component types, events, handlers and handler arguments are checked too.
var checkCommand = &command{
	name:    "check",
	args:    "[dir]",
	summary: "Check the scenes, Tiled and LDtk maps and sprite atlases under a directory, and the files they refer to",
	define:  defineCheck,
	complete: map[string]completion{
		"": filePaths,
	},
}

func defineCheck(a *app, fs *flag.FlagSet) func() error {
	inArg := inFlag(fs, "", "directory to check (alternative to the positional argument; defaults to the current directory)")

	return func() error {
		dir := strings.TrimSpace(*inArg)
		switch {
		case dir != "" && fs.NArg() > 0:
			return usagef("specify either -in or a positional directory, not both")
		case fs.NArg() > 1:
			return usagef("expected at most one directory, got %d", fs.NArg())
		case fs.NArg() == 1:
			dir = fs.Arg(0)
		case dir == "":
			dir = "."
		}
		info, err := os.Stat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		var nonEditorCmdNote bool
		if root, inProject := editorcmd.ProjectRoot(dir); inProject {
			run, ok, err := projectEditor(root)
			if err != nil {
				return err
			}
			if ok {
				err := runOwnEditorCheck(a, root, run, dir)
				printTemplateResidue(a, dir, templateResidue(os.DirFS(dir)))
				return err
			}
			if p, err := build.ImportDir(filepath.Join(root, "cmd", "editor"), 0); err == nil && p.Name == "main" {
				nonEditorCmdNote = true
				fmt.Fprintf(a.stderr, "check: %s does not use package editorcmd, so it may not support -check; checking connection objects only without the project's component registry\n", filepath.Join(root, "cmd", "editor"))
			}
		}
		report, err := assetcheck.Check(os.DirFS(dir))
		if err != nil {
			return err
		}
		for _, p := range report.Problems {
			// Name each file from the working directory, so editors and terminals can open it.
			p.File = filepath.Join(dir, filepath.FromSlash(p.File))
			fmt.Fprintln(a.stderr, p)
		}
		warnings := templateResidue(os.DirFS(dir))
		printTemplateResidue(a, dir, warnings)
		if !nonEditorCmdNote && report.Connections > 0 {
			fmt.Fprintln(a.stderr, "check: note: connection events and handlers were not checked without a project editor command; run `game generate editor` to enable it")
		}
		switch n := len(report.Problems); n {
		case 0:
		case 1:
			return errors.New("1 problem")
		default:
			return fmt.Errorf("%d problems", n)
		}
		summary := report.Summary()
		if len(warnings) > 0 {
			summary += fmt.Sprintf(", %d template residue warning", len(warnings))
			if len(warnings) > 1 {
				summary += "s"
			}
		}
		fmt.Fprintln(a.stdout, summary)
		return nil
	}
}

// runOwnEditorCheck runs the project in root's own editor command with -check on dir, so its registered component types (plus built-in physics and skeletal components) are checked against scenes and connections.
func runOwnEditorCheck(a *app, root string, run []string, dir string) error {
	target := dir
	if filepath.Clean(target) == "." {
		target = "."
	} else {
		abs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		target = abs
	}
	cmd := append(slices.Clone(run), "-check", target)
	if err := a.goRun(root, cmd, a.stdout, a.stderr); err != nil {
		return fmt.Errorf("the project's editor (go %s in %s): %w", strings.Join(run, " "), root, err)
	}
	return nil
}
