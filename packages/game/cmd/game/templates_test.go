package main

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// repoRoot is this repository's root, relative to this package.
const repoRoot = "../../../.."

// templateNamePattern is what a template's name may be: it is a directory name under template/ and _templates/projects/, a module path element and a -template value.
var templateNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// needRepo skips t unless the repository is checked out around this module, as it is in the workspace and CI but not in the module cache.
func needRepo(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repoRoot, "template", "go.mod")); err != nil {
		t.Skipf("no repository checked out around this module: %v", err)
	}
}

// collectFiles returns every regular file of fsys by its slash-separated name.
func collectFiles(t *testing.T, fsys fs.FS) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[name], err = fs.ReadFile(fsys, name)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestTemplateRegistry checks each entry of projectTemplates: a usable unique name, a summary, the module its name implies, and the files every template needs.
func TestTemplateRegistry(t *testing.T) {
	if len(projectTemplates) == 0 || projectTemplates[0].name != "starter" || projectTemplates[0].module != templateModuleRoot {
		t.Fatalf("the first template, the default, is not starter from %s", templateModuleRoot)
	}
	seen := map[string]bool{}
	for i, tmpl := range projectTemplates {
		if !templateNamePattern.MatchString(tmpl.name) || tmpl.name == "template" {
			t.Errorf("template name %q: use lower-case letters, digits and -, starting with a letter, and not template, starter's directory", tmpl.name)
		}
		if seen[tmpl.name] {
			t.Errorf("template %s is registered twice", tmpl.name)
		}
		seen[tmpl.name] = true
		if tmpl.summary == "" || strings.ContainsAny(tmpl.summary, "\n\t") || strings.HasSuffix(tmpl.summary, ".") {
			t.Errorf("template %s: summary %q is not one line without a final period", tmpl.name, tmpl.summary)
		}
		if want := templateModuleRoot + "/" + tmpl.name; i > 0 && tmpl.module != want {
			t.Errorf("template %s: module %s, want %s", tmpl.name, tmpl.module, want)
		}

		files := collectFiles(t, tmpl.files)
		for _, need := range []string{"go.mod.tmpl", "go.sum", "game.yaml", "nsgo.yaml", "main.go", "README.md", ".gitignore"} {
			if _, ok := files[need]; !ok {
				t.Errorf("template %s has no %s", tmpl.name, need)
			}
		}
		tests := false
		for name, content := range files {
			tests = tests || strings.HasSuffix(name, "_test.go")
			// Only go.mod.tmpl is rendered by both game new and the CI job, and the rest must build as they are in the workspace.
			if name != "go.mod.tmpl" && bytes.Contains(content, []byte("{{")) {
				t.Errorf("template %s: %s holds a template action; only go.mod.tmpl may", tmpl.name, name)
			}
			if path.Base(name) == "go.mod" {
				t.Errorf("template %s: %s would make it a module of its own; name it go.mod.tmpl", tmpl.name, name)
			}
		}
		if !tests {
			t.Errorf("template %s has no tests", tmpl.name)
		}
		if mod := string(files["go.mod.tmpl"]); !strings.HasPrefix(mod, "module {{.Module}}\n") || !strings.Contains(mod, "\t"+gameModule+" ") {
			t.Errorf("template %s: go.mod.tmpl does not start with module {{.Module}} and require %s", tmpl.name, gameModule)
		}
	}

	entries, err := fs.ReadDir(projectsFS, "_templates/projects")
	if err != nil {
		t.Fatal(err)
	}
	var embedded []string
	for _, e := range entries {
		embedded = append(embedded, e.Name())
	}
	if names := slices.Sorted(slices.Values(templateNames())); !slices.Equal(embedded, names) {
		t.Errorf("_templates/projects holds %q, but the registered templates are %q", embedded, names)
	}
}

// TestTemplatesMatchTemplateModules keeps each template game new embeds identical to the template directory of its module, the one nsgo init -template writes, and every template module in the repository registered and in go.work. After changing a template, copy it over from this directory: rm -r _templates/projects/<name> && cp -R ../../../../<dir> _templates/projects/<name>, where <dir> is template/template for starter and template/<name>/template otherwise.
func TestTemplatesMatchTemplateModules(t *testing.T) {
	needRepo(t)
	work, err := os.ReadFile(filepath.Join(repoRoot, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	var uses []string
	for line := range strings.Lines(string(work)) {
		if f := strings.Fields(line); len(f) == 1 && strings.HasPrefix(f[0], "./") {
			uses = append(uses, strings.TrimPrefix(f[0], "./"))
		} else if len(f) == 2 && f[0] == "use" {
			uses = append(uses, strings.TrimPrefix(f[1], "./"))
		}
	}

	registered := map[string]string{}
	for _, tmpl := range projectTemplates {
		dir := tmpl.repoDir()
		registered[dir] = tmpl.name
		embedded := "_templates/projects/" + tmpl.name
		want, got := collectFiles(t, os.DirFS(filepath.Join(repoRoot, filepath.FromSlash(dir)))), collectFiles(t, tmpl.files)
		if len(want) == 0 {
			t.Errorf("template %s: %s is missing or empty", tmpl.name, dir)
		}
		for name, content := range want {
			if g, ok := got[name]; !ok {
				t.Errorf("%s has no %s", embedded, name)
			} else if !bytes.Equal(g, content) {
				t.Errorf("%s/%s differs from %s's", embedded, name, dir)
			}
		}
		for name := range got {
			if _, ok := want[name]; !ok {
				t.Errorf("%s/%s is not in %s", embedded, name, dir)
			}
		}

		moduleDir := path.Dir(dir)
		mod, err := os.Open(filepath.Join(repoRoot, filepath.FromSlash(moduleDir), "go.mod"))
		if err != nil {
			t.Errorf("template %s: %v", tmpl.name, err)
			continue
		}
		first, _, _ := bufio.NewReader(mod).ReadLine()
		mod.Close()
		if string(first) != "module "+tmpl.module {
			t.Errorf("template %s: %s/go.mod declares %q, want module %s", tmpl.name, moduleDir, first, tmpl.module)
		}
		if !slices.Contains(uses, moduleDir) {
			t.Errorf("template %s: go.work does not use ./%s", tmpl.name, moduleDir)
		}
	}

	onDisk, err := filepath.Glob(filepath.Join(repoRoot, "template", "*", "template"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range onDisk {
		rel := filepath.ToSlash(strings.TrimPrefix(d, repoRoot+string(filepath.Separator)))
		if _, ok := registered[rel]; !ok {
			t.Errorf("%s is a template no entry of projectTemplates names", rel)
		}
	}
}

// TestTemplatesRunInCI keeps the templates job of the Test workflow testing every template, in registry order, from the directory its module holds it in.
func TestTemplatesRunInCI(t *testing.T) {
	needRepo(t)
	src, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						Template string `yaml:"template"`
						Dir      string `yaml:"dir"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(src, &workflow); err != nil {
		t.Fatal(err)
	}
	job, ok := workflow.Jobs["templates"]
	if !ok {
		t.Fatal("test.yml has no templates job")
	}
	var got, want []string
	for _, e := range job.Strategy.Matrix.Include {
		got = append(got, e.Template+" "+e.Dir)
	}
	for _, tmpl := range projectTemplates {
		want = append(want, tmpl.name+" "+tmpl.repoDir())
	}
	if !slices.Equal(got, want) {
		t.Errorf("test.yml templates matrix = %q, want %q", got, want)
	}
}

// TestTemplatesListedInDocs keeps the templates page in each language naming every template with the module nsgo init -template takes for it.
func TestTemplatesListedInDocs(t *testing.T) {
	needRepo(t)
	for _, lang := range []string{"en", "zh"} {
		page := filepath.Join(repoRoot, "docs", lang, "templates.md")
		src, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, tmpl := range projectTemplates {
			row := "| `" + tmpl.name + "` |"
			i := bytes.Index(src, []byte(row))
			if i < 0 {
				t.Errorf("%s has no row starting %s", page, row)
				continue
			}
			line, _, _ := bytes.Cut(src[i:], []byte("\n"))
			if !bytes.Contains(line, []byte("`"+tmpl.module+"`")) {
				t.Errorf("%s: the row of %s does not name its module `%s`", page, tmpl.name, tmpl.module)
			}
		}
	}
}

func TestNewList(t *testing.T) {
	calls := stubGo(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", "-list"}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != len(projectTemplates) {
		t.Fatalf("game new -list printed %d lines, want %d:\n%s", len(lines), len(projectTemplates), stdout)
	}
	for i, tmpl := range projectTemplates {
		name, summary, _ := strings.Cut(lines[i], "  ")
		if i == 0 {
			name = strings.TrimSuffix(name, " (default)")
		}
		if name != tmpl.name || strings.TrimSpace(summary) != tmpl.summary {
			t.Errorf("line %d = %q, want %s and its summary", i, lines[i], tmpl.name)
		}
	}
	if !strings.HasPrefix(lines[0], "starter (default)  ") {
		t.Errorf("the default is not marked: %q", lines[0])
	}
	if len(*calls) != 0 {
		t.Errorf("game new -list ran go %q", *calls)
	}

	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"new", "-list", "mygame"}, a); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "game new: -list takes no directory") {
		t.Errorf("game new -list mygame: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestNewEachTemplate creates a project from every template and checks what any project game new writes: the template's files under their names, a go.mod for the module asked for, and a valid canonical game.yaml that nsgo.yaml agrees with.
func TestNewEachTemplate(t *testing.T) {
	for _, tmpl := range projectTemplates {
		t.Run(tmpl.name, func(t *testing.T) {
			stubGo(t, nil)
			dir := filepath.Join(t.TempDir(), "mygame")
			a, _, stderr := newTestApp(t)
			if code := run([]string{"new", "-template", tmpl.name, "-name", "My Game", "-module", "example.com/mygame", dir}, a); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			for name, content := range collectFiles(t, tmpl.files) {
				out := strings.TrimSuffix(name, ".tmpl")
				got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(out)))
				if err != nil {
					t.Error(err)
					continue
				}
				if name != out || name == "nsgo.yaml" || name == "game.yaml" {
					continue
				}
				if !bytes.Equal(got, content) {
					t.Errorf("%s differs from the template's", name)
				}
			}
			if mod, err := os.ReadFile(filepath.Join(dir, "go.mod")); err != nil || !strings.HasPrefix(string(mod), "module example.com/mygame\n") {
				t.Errorf("go.mod (%v):\n%s", err, mod)
			}
			checkProjectFile(t, dir, "My Game", "dev.nsgo.mygame")
		})
	}
}

func TestNewUnknownTemplateListsKnownOnes(t *testing.T) {
	calls := stubGo(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", "-template", "nope", filepath.Join(t.TempDir(), "x")}, a); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if want := `game new: no template "nope"; use ` + strings.Join(templateNames(), ", "); !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout.Len() != 0 || len(*calls) != 0 {
		t.Errorf("wrote %q and ran go %q", stdout, *calls)
	}
}

func TestNewCompletesTemplateNames(t *testing.T) {
	if got := newCommand.complete["template"].words; !slices.Equal(got, templateNames()) {
		t.Errorf("game new -template completes %q, want %q", got, templateNames())
	}
}
