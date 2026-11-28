package gamecli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"text/template"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

// repoRoot is this repository's root, relative to this package.
const repoRoot = "../.."

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
	files, err := regularFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// regularFiles returns every regular file of fsys by its slash-separated name.
func regularFiles(fsys fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[name], err = fs.ReadFile(fsys, name)
		return err
	})
	return files, err
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
		for _, need := range []string{"go.mod.tmpl", "go.sum", "game.yaml", "nsgo.yaml", "main.go", "README.md", ".gitignore", ".gitattributes"} {
			if _, ok := files[need]; !ok {
				t.Errorf("template %s has no %s", tmpl.name, need)
			}
		}
		tests := false
		for name, content := range files {
			tests = tests || strings.HasSuffix(name, "_test.go")
			// game new renders nsgo.yaml, main.go and every .tmpl file, but the CI job renders only go.mod.tmpl and the template must build as it is in the workspace, so every other rendered file must render to itself. Files game new copies as they are, such as other Go files and assets, may hold any bytes.
			if rendered(name) && name != "go.mod.tmpl" {
				if got, err := renderTemplateFile(name, content); err != nil || !bytes.Equal(got, content) {
					t.Errorf("template %s: %s does not render to itself (%v); only go.mod.tmpl may hold a template action", tmpl.name, name, err)
				}
			}
			if path.Base(name) == "go.mod" {
				t.Errorf("template %s: %s would make it a module of its own; name it go.mod.tmpl", tmpl.name, name)
			}
		}
		if !tests {
			t.Errorf("template %s has no tests", tmpl.name)
		}
		if mod := string(files["go.mod.tmpl"]); !strings.HasPrefix(mod, "module {{.Module}}\n") || !requiresModule(mod, gameModule) {
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

// TestTemplateExportPresets checks that every template's game.yaml declares all six primary platforms with an iOS device preset (never ios_simulator) and a Linux preset (linux_x64 and linux_arm64), and that nsgo.yaml's build.targets keeps only cross-host portable targets so plain `nsgo build` succeeds on every host.
func TestTemplateExportPresets(t *testing.T) {
	for _, tmpl := range projectTemplates {
		t.Run(tmpl.name, func(t *testing.T) {
			gameData, err := fs.ReadFile(tmpl.files, project.FileName)
			if err != nil {
				t.Fatalf("read %s: %v", project.FileName, err)
			}
			f, err := project.Parse(gameData)
			if err != nil {
				t.Fatalf("parse %s: %v", project.FileName, err)
			}
			if f.Export == nil || len(f.Export.Presets) == 0 {
				t.Fatalf("%s has no export presets", project.FileName)
			}
			for _, platform := range releaseplan.DefaultPlatforms {
				if _, ok := f.Preset(platform); !ok {
					t.Errorf("%s: export.presets misses platform %q", project.FileName, platform)
				}
			}
			if ios, ok := f.Preset("ios"); !ok {
				t.Errorf("%s: missing ios export preset", project.FileName)
			} else {
				if slices.Contains(ios.Targets, "ios_simulator") {
					t.Errorf("%s: ios export preset lists ios_simulator (%v); release preset must target devices", project.FileName, ios.Targets)
				}
				if len(ios.Targets) > 0 && !slices.Contains(ios.Targets, "ios") {
					t.Errorf("%s: ios export preset targets = %v, want ios device target", project.FileName, ios.Targets)
				}
			}
			if linux, ok := f.Preset("linux"); !ok {
				t.Errorf("%s: missing linux export preset", project.FileName)
			} else if len(linux.Targets) > 0 {
				for _, want := range []string{"linux_x64", "linux_arm64"} {
					if !slices.Contains(linux.Targets, want) {
						t.Errorf("%s: linux export preset targets = %v, missing %q", project.FileName, linux.Targets, want)
					}
				}
			}

			nsgoData, err := fs.ReadFile(tmpl.files, project.NSGoFileName)
			if err != nil {
				t.Fatalf("read %s: %v", project.NSGoFileName, err)
			}
			if err := f.CheckNSGo(nsgoData); err != nil {
				t.Errorf("CheckNSGo: %v", err)
			}
			var nsgo struct {
				Build struct {
					Targets []string `yaml:"targets"`
				} `yaml:"build"`
			}
			if err := yaml.Unmarshal(nsgoData, &nsgo); err != nil {
				t.Fatalf("parse %s: %v", project.NSGoFileName, err)
			}
			if len(nsgo.Build.Targets) == 0 {
				t.Fatalf("%s: build.targets is empty", project.NSGoFileName)
			}
			for _, target := range nsgo.Build.Targets {
				if !project.IsPortableTarget(target) {
					t.Errorf("%s: build.targets includes non-portable target %q (%v); plain nsgo build must succeed on Linux and Windows", project.NSGoFileName, target, nsgo.Build.Targets)
				}
			}
			for _, want := range []string{"linux_x64", "linux_arm64"} {
				if !slices.Contains(nsgo.Build.Targets, want) {
					t.Errorf("%s: build.targets = %v, missing %q", project.NSGoFileName, nsgo.Build.Targets, want)
				}
			}
		})
	}
}

// TestTemplateGitFiles checks that every template's .gitattributes keeps text files on LF, marks binary media extensions as binary after the wildcard rule, and configures the scene merge driver, and that every template's .gitignore ignores .nsgo/, .game/, and dist/.
func TestTemplateGitFiles(t *testing.T) {
	wantAttrs := defaultGitAttributesContent()
	for _, tmpl := range projectTemplates {
		t.Run(tmpl.name, func(t *testing.T) {
			attrs, err := fs.ReadFile(tmpl.files, ".gitattributes")
			if err != nil {
				t.Fatalf(".gitattributes: %v", err)
			}
			if bytes.ContainsRune(attrs, '\r') {
				t.Error(".gitattributes contains CR bytes; use LF line endings")
			}
			if string(attrs) != wantAttrs {
				t.Errorf(".gitattributes differs from defaultGitAttributesContent()")
			}
			s := string(attrs)
			lfIdx := strings.Index(s, "* text=auto eol=lf\n")
			if lfIdx < 0 {
				t.Error(".gitattributes missing `* text=auto eol=lf`")
			}
			for _, pat := range binaryMediaPatterns {
				idx := strings.Index(s, pat+" binary\n")
				if idx < 0 {
					t.Errorf(".gitattributes missing `%s binary`", pat)
				} else if lfIdx >= 0 && idx < lfIdx {
					t.Errorf(".gitattributes places `%s binary` before `* text=auto eol=lf`", pat)
				}
			}
			for _, rule := range []string{"*.scene.yaml merge=" + sceneMergeDriverName, "*.scene.yml merge=" + sceneMergeDriverName} {
				if !strings.Contains(s, rule+"\n") {
					t.Errorf(".gitattributes missing %q", rule)
				}
			}
			// ensureSceneGitAttributes must treat every template's .gitattributes as already up to date.
			tmpAttr := filepath.Join(t.TempDir(), ".gitattributes")
			if err := os.WriteFile(tmpAttr, attrs, 0o644); err != nil {
				t.Fatal(err)
			}
			if changed, err := ensureSceneGitAttributes(tmpAttr); err != nil || changed {
				t.Errorf("ensureSceneGitAttributes reported changed=%v, err=%v on template .gitattributes", changed, err)
			}

			ignore, err := fs.ReadFile(tmpl.files, ".gitignore")
			if err != nil {
				t.Fatalf(".gitignore: %v", err)
			}
			if bytes.ContainsRune(ignore, '\r') {
				t.Error(".gitignore contains CR bytes; use LF line endings")
			}
			lines := strings.Split(strings.TrimSuffix(string(ignore), "\n"), "\n")
			for _, want := range []string{".nsgo/", ".game/", "dist/"} {
				if !slices.Contains(lines, want) {
					t.Errorf(".gitignore missing %q:\n%s", want, ignore)
				}
			}
		})
	}
}

// rendered reports whether renderProject renders the template file name with text/template rather than copying it as it is.
func rendered(name string) bool {
	return strings.HasSuffix(name, ".tmpl") || name == "nsgo.yaml" || name == "main.go"
}

// renderTemplateFile renders the template file name, holding content, as renderProject does, with the data game new gives a project called My Game.
func renderTemplateFile(name string, content []byte) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, newData{Name: "My Game", ID: "dev.nsgo.mygame", Module: "example.com/mygame", Slug: "my-game"})
	return b.Bytes(), err
}

// requiresModule reports whether the go.mod file src requires module, on a require line of its own or inside a require ( ... ) block.
func requiresModule(src, module string) bool {
	for line := range strings.Lines(src) {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "require" && f[1] == module || len(f) >= 2 && f[0] == module {
			return true
		}
	}
	return false
}

func TestTemplateFileChecks(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"module {{.Module}}\n\nrequire (\n\t" + gameModule + " v0.1.0\n)\n", true},
		{"module {{.Module}}\n\nrequire " + gameModule + " v0.1.0\n", true},
		{"module {{.Module}}\n\nrequire " + gameModule + "/x v0.1.0\n", false},
		{"module {{.Module}}\n", false},
	} {
		if got := requiresModule(tc.src, gameModule); got != tc.want {
			t.Errorf("requiresModule(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}

	// Go code and assets that game new copies as they are may hold "{{", as a composite literal such as []T{{...}} does, and are not checked; main.go is rendered, so it may not.
	for name, want := range map[string]bool{"main.go": true, "nsgo.yaml": true, "go.mod.tmpl": true, "levels/one.yaml.tmpl": true, "renderer.go": false, "assets/sprite.png": false, "game.yaml": false} {
		if got := rendered(name); got != want {
			t.Errorf("rendered(%q) = %v, want %v", name, got, want)
		}
	}
	if got, err := renderTemplateFile("main.go", []byte("package main\n")); err != nil || string(got) != "package main\n" {
		t.Errorf("a plain main.go renders to %q, %v", got, err)
	}
	literal := []byte("package main\n\nvar s = []struct{ a int }{{1}}\n")
	if got, err := renderTemplateFile("main.go", literal); err == nil && bytes.Equal(got, literal) {
		t.Error("a main.go holding a composite literal with {{ renders to itself, though text/template cannot parse it")
	}
}

// TestTemplatesMatchTemplateModules keeps each template game new embeds identical to the template directory of its module, the one nsgo init -template writes, and every template module in the repository registered and in go.work. After changing a template, copy it over from this directory: rm -r _templates/projects/<name> && cp -R ../../../../<dir> _templates/projects/<name>, where <dir> is template/template for starter and template/<name>/template otherwise.
func TestTemplatesMatchTemplateModules(t *testing.T) {
	needRepo(t)
	for _, problem := range checkTemplateModules(repoRoot, projectTemplates) {
		t.Error(problem)
	}
}

// TestTemplatesShipBriefsAndResolveBriefPlaytestGoals checks that every template ships a canonical brief.yaml, one that ParseBrief and EncodeBrief round-trip byte for byte, and that every playtest.yaml goal whose source is brief.yaml names a target of that brief for the playtest's scene.
func TestTemplatesShipBriefsAndResolveBriefPlaytestGoals(t *testing.T) {
	for _, tmpl := range projectTemplates {
		raw, err := fs.ReadFile(tmpl.files, project.BriefFileName)
		if err != nil {
			t.Errorf("%s: %v", tmpl.name, err)
			continue
		}
		brief, err := project.ParseBrief(raw)
		if err != nil {
			t.Errorf("%s brief.yaml: %v", tmpl.name, err)
			continue
		}
		if encoded, err := project.EncodeBrief(brief); err != nil {
			t.Errorf("%s brief.yaml: %v", tmpl.name, err)
		} else if !bytes.Equal(raw, encoded) {
			t.Errorf("%s brief.yaml is not canonical; want:\n%s", tmpl.name, encoded)
		}
		cfgRaw, err := fs.ReadFile(tmpl.files, playtest.FileName)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tmpl.name, err)
			continue
		}
		cfg, err := playtest.ParseConfig(cfgRaw)
		if err != nil {
			t.Errorf("%s playtest.yaml: %v", tmpl.name, err)
			continue
		}
		targets := brief.TargetsForScene(cfg.Scope.Scene)
		for _, metric := range cfg.Metrics {
			if metric.Goal == nil || metric.Goal.Source != project.BriefFileName {
				continue
			}
			i := slices.IndexFunc(targets, func(target project.BriefPlaytestTarget) bool { return target.Metric == metric.Name })
			if i < 0 {
				t.Errorf("%s playtest.yaml metric %q takes its goal from brief.yaml, which has no %q target for scene %q", tmpl.name, metric.Name, metric.Name, cfg.Scope.Scene)
				continue
			}
			if want := playtest.GoalFromBriefTarget(targets[i]); !sameBound(metric.Goal.Min, want.Min) || !sameBound(metric.Goal.Max, want.Max) || !sameBound(metric.Goal.Equal, want.Equal) {
				t.Errorf("%s playtest.yaml metric %q goal %s differs from its brief.yaml target %s", tmpl.name, metric.Name, metric.Goal.Describe(""), want.Describe(""))
			}
		}
	}
}

// sameBound reports whether two optional goal bounds are both unset or both set to the same value.
func sameBound(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// TestCheckTemplateModulesNested runs checkTemplateModules against a repository made up here, with starter and a nested template x, so that what only a template other than the default reaches is covered before one is registered.
func TestCheckTemplateModulesNested(t *testing.T) {
	files := fstest.MapFS{"main.go": {Data: []byte("package main\n")}}
	starter := projectTemplate{name: "starter", module: templateModuleRoot, files: files}
	x := projectTemplate{name: "x", module: templateModuleRoot + "/x", files: files}
	if got := starter.repoDir(); got != "template/template" {
		t.Errorf("repoDir of starter = %q, want template/template", got)
	}
	if got := x.repoDir(); got != "template/x/template" {
		t.Errorf("repoDir of x = %q, want template/x/template", got)
	}

	write := func(root, name, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := func(work string) string {
		root := t.TempDir()
		write(root, "go.work", work)
		write(root, "template/go.mod", "module "+templateModuleRoot+"\n\ngo 1.27\n")
		write(root, "template/template/main.go", "package main\n")
		write(root, "template/x/go.mod", "module "+templateModuleRoot+"/x\n\ngo 1.27\n")
		write(root, "template/x/template/main.go", "package main\n")
		return root
	}
	tmpls := []projectTemplate{starter, x}

	for _, work := range []string{
		"go 1.27\n\nuse (\n\t./template\n\t./template/x\n)\n",
		"go 1.27\n\nuse ./template\nuse ./template/x\n",
	} {
		if problems := checkTemplateModules(repo(work), tmpls); len(problems) != 0 {
			t.Errorf("go.work %q: %q", work, problems)
		}
	}

	root := repo("go 1.27\n\nuse ./template\n")
	write(root, "template/x/template/extra.go", "package main\n")
	write(root, "template/y/template/main.go", "package main\n")
	want := []string{
		"_templates/projects/x has no extra.go",
		"template x: go.work does not use ./template/x",
		"template/y/template is a template no entry of projectTemplates names",
	}
	if got := checkTemplateModules(root, tmpls); !slices.Equal(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}

	write(root, "template/x/go.mod", "module example.com/x\n")
	if got, want := checkTemplateModules(root, tmpls), `template x: template/x/go.mod declares "module example.com/x", want module `+templateModuleRoot+"/x"; !slices.Contains(got, want) {
		t.Errorf("problems = %q, want one %q", got, want)
	}
}

// checkTemplateModules returns what is wrong with the templates tmpls in the repository at root: an embedded copy that differs from its module's template directory, a module whose go.mod is missing or declares another path, a module go.work does not use, and a template/*/template directory no template names.
func checkTemplateModules(root string, tmpls []projectTemplate) []string {
	work, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return []string{err.Error()}
	}
	uses := workUses(string(work))

	var problems []string
	registered := map[string]bool{}
	for _, tmpl := range tmpls {
		dir := tmpl.repoDir()
		registered[dir] = true
		embedded := "_templates/projects/" + tmpl.name
		want, err := regularFiles(os.DirFS(filepath.Join(root, filepath.FromSlash(dir))))
		if err != nil || len(want) == 0 {
			problems = append(problems, fmt.Sprintf("template %s: %s is missing or empty (%v)", tmpl.name, dir, err))
		}
		got, err := regularFiles(tmpl.files)
		if err != nil {
			problems = append(problems, fmt.Sprintf("template %s: %v", tmpl.name, err))
		}
		for _, name := range slices.Sorted(maps.Keys(want)) {
			if g, ok := got[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s has no %s", embedded, name))
			} else if !bytes.Equal(g, want[name]) {
				problems = append(problems, fmt.Sprintf("%s/%s differs from %s's", embedded, name, dir))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(got)) {
			if _, ok := want[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s/%s is not in %s", embedded, name, dir))
			}
		}

		moduleDir := path.Dir(dir)
		mod, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(moduleDir), "go.mod"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("template %s: %v", tmpl.name, err))
			continue
		}
		first, _, _ := strings.Cut(string(mod), "\n")
		if first = strings.TrimSpace(first); first != "module "+tmpl.module {
			problems = append(problems, fmt.Sprintf("template %s: %s/go.mod declares %q, want module %s", tmpl.name, moduleDir, first, tmpl.module))
		}
		if !slices.Contains(uses, moduleDir) {
			problems = append(problems, fmt.Sprintf("template %s: go.work does not use ./%s", tmpl.name, moduleDir))
		}
	}

	onDisk, err := filepath.Glob(filepath.Join(root, "template", "*", "template"))
	if err != nil {
		return append(problems, err.Error())
	}
	for _, d := range onDisk {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if rel = filepath.ToSlash(rel); !registered[rel] {
			problems = append(problems, rel+" is a template no entry of projectTemplates names")
		}
	}
	return problems
}

// workUses returns the slash-separated directories the go.work file src uses, from use ./dir lines and use ( ... ) blocks alike, without their leading ./.
func workUses(src string) []string {
	var uses []string
	for line := range strings.Lines(src) {
		if f := strings.Fields(line); len(f) == 1 && strings.HasPrefix(f[0], "./") {
			uses = append(uses, strings.TrimPrefix(f[0], "./"))
		} else if len(f) == 2 && f[0] == "use" && f[1] != "(" {
			uses = append(uses, strings.TrimPrefix(f[1], "./"))
		}
	}
	return uses
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
