package gamecli

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

// originTemplate returns the embedded template o names: the one whose name is o.Path, or, when o has no path, the one whose module is o.Source. It reports false for an origin of another kind or a template this CLI does not embed.
func originTemplate(o *project.Origin) (projectTemplate, bool) {
	if o == nil || o.Kind != "template" {
		return projectTemplate{}, false
	}
	for _, t := range projectTemplates {
		if (o.Path != "" && o.Path == t.name) || (o.Path == "" && o.Source == t.module) {
			return t, true
		}
	}
	return projectTemplate{}, false
}

// residueKind returns the origin.keep category a template file counts toward, "rules" or "data", or "" for a file the comparison leaves out: the files game new renders from the project's name and module, the project and build settings, tests, playtest recordings and touch layouts.
func residueKind(name string) string {
	base := path.Base(name)
	switch {
	case strings.HasSuffix(base, ".tmpl"), base == "main.go", base == project.FileName, base == project.NSGoFileName, base == "game.index.yaml", base == "playtest.yaml", strings.HasSuffix(base, ".touch.yaml"):
		return ""
	case strings.HasSuffix(base, ".go"):
		if strings.HasSuffix(base, "_test.go") {
			return ""
		}
		return "rules"
	}
	switch path.Ext(base) {
	case ".yaml", ".yml", ".json", ".csv", ".yarn":
		return "data"
	}
	return ""
}

// printTemplateResidue prints each of warnings to standard error as "file:line: warning: message", with the file named from the working directory as game check names a problem's.
func printTemplateResidue(a *app, dir string, warnings []assetcheck.Problem) {
	for _, w := range warnings {
		w.File = filepath.Join(dir, filepath.FromSlash(w.File))
		fmt.Fprintln(a.stderr, w.Location()+": warning: "+w.Message)
	}
}

// templateResidue returns advisory warnings for a project whose game.yaml records an origin of kind template that this CLI embeds: the rule and data files still byte-identical to the template's, a name or window title still the template's, and a README.md or CREDITS.md still the template's. origin.keep leaves out the categories it names. A directory without a readable game.yaml, or with an origin of another kind, gets none.
func templateResidue(fsys fs.FS) []assetcheck.Problem {
	data, err := fs.ReadFile(fsys, project.FileName)
	if err != nil {
		return nil
	}
	f, err := project.Parse(data)
	if err != nil {
		return nil
	}
	t, ok := originTemplate(f.Origin)
	if !ok {
		return nil
	}
	keep := func(kind string) bool { return slices.Contains(f.Origin.Keep, kind) }
	same := func(name string) bool {
		want, err := fs.ReadFile(t.files, name)
		if err != nil {
			return false
		}
		got, err := fs.ReadFile(fsys, name)
		return err == nil && bytes.Equal(got, want)
	}
	line := func(field string) int {
		l, _ := loaderr.FindField(data, field)
		return l
	}
	var out []assetcheck.Problem
	var unchanged []string
	_ = fs.WalkDir(t.files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if kind := residueKind(name); kind != "" && !keep(kind) && same(name) {
			unchanged = append(unchanged, name)
		}
		return nil
	})
	if len(unchanged) > 0 {
		word := "files"
		if len(unchanged) == 1 {
			word = "file"
		}
		out = append(out, assetcheck.Problem{File: project.FileName, Line: line("origin"), Message: fmt.Sprintf("%d rule and data %s still equal template %q: %s; change them, or add rules or data to origin.keep for a deliberate copy", len(unchanged), word, t.name, strings.Join(unchanged, ", "))})
	}
	if !keep("name") {
		if tmplData, err := fs.ReadFile(t.files, project.FileName); err == nil {
			if tmpl, err := project.Parse(tmplData); err == nil {
				if f.Name == tmpl.Name {
					out = append(out, assetcheck.Problem{File: project.FileName, Line: line("name"), Message: fmt.Sprintf("name %q is still template %q's; name the game, or add name to origin.keep", f.Name, t.name)})
				}
				if tmpl.Window.Title != "" && f.Window.Title == tmpl.Window.Title {
					out = append(out, assetcheck.Problem{File: project.FileName, Line: line("window.title"), Message: fmt.Sprintf("window title %q is still template %q's; retitle the window, or add name to origin.keep", f.Window.Title, t.name)})
				}
			}
		}
	}
	for _, doc := range []struct{ file, kind string }{{"README.md", "readme"}, {"CREDITS.md", "credits"}} {
		if !keep(doc.kind) && same(doc.file) {
			out = append(out, assetcheck.Problem{File: doc.file, Message: fmt.Sprintf("%s is still template %q's; rewrite it for this game, or add %s to origin.keep", doc.file, t.name, doc.kind)})
		}
	}
	return out
}
