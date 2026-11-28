package gamecli

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"text/tabwriter"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/tooling/templatemeta"
)

// projectsFS holds the project templates game new offers, each under _templates/projects/<name> as a byte-for-byte copy of the template directory of its module in this repository, which TestTemplatesMatchTemplateModules keeps identical.
//
//go:embed all:_templates/projects
var projectsFS embed.FS

const (
	// templateRepo is the module path prefix of every module in this repository.
	templateRepo = "github.com/nsgo-dev/game/"
	// templateModuleRoot is the module that publishes the default template, starter, from its template directory; every other template is the module templateModuleRoot/<name>, in the repository directory template/<name>.
	templateModuleRoot = templateRepo + "template"
)

// projectTemplate is a project game new can start from, published as well as a Go module template that nsgo init -template writes.
type projectTemplate struct {
	// name is what -template takes: lower-case letters, digits and -, starting with a letter.
	name string
	// summary is the one line game new -list prints for it.
	summary string
	// module is the Go module whose template directory nsgo init -template writes: templateModuleRoot for starter and templateModuleRoot/<name> for every other template.
	module string
	// files are the project's files: a file ending in .tmpl, nsgo.yaml and main.go are rendered with text/template and newData's fields, as nsgo init -template renders a template module's, and the rest are copied.
	files       fs.FS
	manifest    templatemeta.Manifest
	manifestErr error
}

// newProjectTemplate returns the template called name, whose files game new embeds under _templates/projects/<name>.
func newProjectTemplate(name, module string) projectTemplate {
	files := mustSub(projectsFS, "_templates/projects/"+name)
	raw, err := fs.ReadFile(files, "template.yaml")
	m := templatemeta.Manifest{ID: name}
	if err == nil {
		m, err = templatemeta.Parse(raw)
	}
	return projectTemplate{name: name, summary: m.SummaryFor("en"), module: module, files: files, manifest: m, manifestErr: err}
}

// projectTemplates are the templates game new offers, the default first and the rest in the order game new -list prints them. Adding a template adds its module under template/<name>, its copy under _templates/projects/<name> and one entry here, as docs/en/templates.md describes; CI finds it from its directory.
var projectTemplates = []projectTemplate{
	newProjectTemplate("starter", templateModuleRoot),
	newProjectTemplate("action3d", templateModuleRoot+"/action3d"),
	newProjectTemplate("cards2d", templateModuleRoot+"/cards2d"),
	newProjectTemplate("fighting2d", templateModuleRoot+"/fighting2d"),
	newProjectTemplate("fps3d", templateModuleRoot+"/fps3d"),
	newProjectTemplate("idle", templateModuleRoot+"/idle"),
	newProjectTemplate("platformer2d", templateModuleRoot+"/platformer2d"),
	newProjectTemplate("puzzle2d", templateModuleRoot+"/puzzle2d"),
	newProjectTemplate("racing3d", templateModuleRoot+"/racing3d"),
	newProjectTemplate("rhythm", templateModuleRoot+"/rhythm"),
	newProjectTemplate("roguelike2d", templateModuleRoot+"/roguelike2d"),
	newProjectTemplate("rpg2d", templateModuleRoot+"/rpg2d"),
	newProjectTemplate("rts2d", templateModuleRoot+"/rts2d"),
	newProjectTemplate("shmup2d", templateModuleRoot+"/shmup2d"),
	newProjectTemplate("towerdefense2d", templateModuleRoot+"/towerdefense2d"),
	newProjectTemplate("visualnovel", templateModuleRoot+"/visualnovel"),
}

// repoDir returns the slash-separated directory of this repository that holds t's files, the template directory of its module.
func (t projectTemplate) repoDir() string {
	return strings.TrimPrefix(t.module, templateRepo) + "/template"
}

// templateNames returns the names of projectTemplates, in order.
func templateNames() []string {
	names := make([]string, len(projectTemplates))
	for i, t := range projectTemplates {
		names[i] = t.name
	}
	return names
}

// writeTemplateList writes one line per template to w, its name, the default marked, beside its summary.
func writeTemplateList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, t := range projectTemplates {
		name := t.name
		if i == 0 {
			name += " (default)"
		}
		fmt.Fprintf(tw, "%s\t%s\n", name, t.summary)
	}
	return tw.Flush()
}

// mustSub returns the subtree of fsys at dir, which is embedded and so always there.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// templateMinGoVersion returns the highest `go` directive version required across all
// embedded project templates' go.mod.tmpl (or go.mod) files, falling back to gotoolchain.DefaultMinVersion.
func templateMinGoVersion() string {
	var versions []string
	for _, t := range projectTemplates {
		for _, name := range []string{"go.mod.tmpl", "go.mod"} {
			data, err := fs.ReadFile(t.files, name)
			if err != nil {
				continue
			}
			if v, ok := gotoolchain.ParseGoModMinVersion(data); ok {
				versions = append(versions, v)
			}
			break
		}
	}
	if maxV := gotoolchain.MaxGoVersion(versions...); maxV != "" {
		return maxV
	}
	return gotoolchain.DefaultMinVersion
}
