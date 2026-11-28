package gamecli

import (
	"errors"
	"flag"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/project"
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

func templateResidueProblems(dir string, f *project.File) []assetcheck.Problem {
	if f == nil || f.Origin == nil || f.Origin.Kind != "template" || f.Origin.Name == "" {
		return nil
	}
	root := "_templates/projects/" + f.Origin.Name
	if _, err := fs.Stat(projectsFS, root); err != nil {
		return nil
	}
	keep := func(name string) bool { return slices.Contains(f.Origin.Keep, name) }
	var out []assetcheck.Problem
	read := func(rel string) ([]byte, bool) {
		data, err := fs.ReadFile(projectsFS, root+"/"+rel)
		if err != nil {
			return nil, false
		}
		return data, true
	}
	equal := func(rel string) bool {
		want, ok := read(rel)
		if !ok {
			return false
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil && string(got) == string(want)
	}
	if !keep("rules") || !keep("data") {
		var unchanged []string
		entries, _ := fs.ReadDir(projectsFS, root)
		for _, e := range entries {
			if e.IsDir() || (!strings.HasSuffix(e.Name(), ".go") && e.Name() != "data") {
				continue
			}
			if e.Name() == "data" {
				files, _ := fs.Glob(projectsFS, root+"/data/*")
				for _, p := range files {
					rel := strings.TrimPrefix(p, root+"/")
					if equal(rel) {
						unchanged = append(unchanged, rel)
					}
				}
			} else if equal(e.Name()) {
				unchanged = append(unchanged, e.Name())
			}
		}
		if len(unchanged) > 0 {
			msg := fmt.Sprintf("template starter residue: %d rules/data files still match template %q", len(unchanged), f.Origin.Name)
			out = append(out, assetcheck.Problem{File: project.FileName, Line: 1, Message: msg, Warning: true})
		}
	}
	if !keep("name") {
		if data, ok := read(project.FileName); ok {
			if tmpl, err := project.Parse(data); err == nil && f.Name == tmpl.Name {
				out = append(out, assetcheck.Problem{File: project.FileName, Line: 2, Message: fmt.Sprintf("project name still matches template %q", f.Origin.Name), Warning: true})
			}
		}
	}
	if !keep("readme") && equal("README.md") {
		out = append(out, assetcheck.Problem{File: "README.md", Line: 1, Message: "README.md still matches the template; rewrite it for this game", Warning: true})
	}
	return out
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
				return runOwnEditorCheck(a, root, run, dir)
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
		if f, readErr := project.ReadFS(os.DirFS(dir)); readErr == nil {
			report.Problems = append(report.Problems, templateResidueProblems(dir, f)...)
			slices.SortFunc(report.Problems, func(a, b assetcheck.Problem) int { return strings.Compare(a.String(), b.String()) })
		}
		for _, p := range report.Problems {
			// Name each file from the working directory, so editors and terminals can open it.
			p.File = filepath.Join(dir, filepath.FromSlash(p.File))
			fmt.Fprintln(a.stderr, p)
		}
		if !nonEditorCmdNote && report.Connections > 0 {
			fmt.Fprintln(a.stderr, "check: note: connection events and handlers were not checked without a project editor command; run `game generate editor` to enable it")
		}
		errorsFound := 0
		for _, p := range report.Problems {
			if !p.Warning {
				errorsFound++
			}
		}
		switch n := errorsFound; n {
		case 0:
		case 1:
			return errors.New("1 problem")
		default:
			return fmt.Errorf("%d problems", n)
		}
		fmt.Fprintln(a.stdout, report.Summary())
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
