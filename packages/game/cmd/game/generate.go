package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// generateCommand writes the Go code and the YAML files of a new component, system, scene or prefab into a game project.
var generateCommand = &command{
	name:    "generate",
	args:    "component|system|scene|prefab <name>",
	summary: "Write a new component, system, scene or prefab into a game project: its Go code and a test, the scene file of a scene or prefab, and its registration",
	define:  defineGenerate,
	complete: map[string]completion{
		"":    oneOf(generateKinds...),
		"dir": filePaths,
	},
}

// generateKinds are the kinds game generate writes, in the order its usage names them.
var generateKinds = []string{"component", "system", "scene", "prefab"}

// generateFS holds the templates of the Go files game generate writes, rendered with text/template and generateData.
//
//go:embed _templates/generate
var generateFS embed.FS

// generateData is what the templates of game generate render: the package the Go code goes in and the generated thing's name in each form it is written in.
type generateData struct {
	// Package is the name of the Go package in the project directory.
	Package string
	// Go is the name as an exported Go identifier, such as PlayerHealth.
	Go string
	// Camel is the name in lower camel case, such as playerHealth: the key a scene's components mapping, the scheduler, the prefab registry and game.yaml's autoload know it by, and the stem of the unexported identifiers.
	Camel string
	// Snake is the name in lower case with its words joined by _, such as player_health: the stem of the file names and a prefab's object ID.
	Snake string
	// Title is the name's words joined by spaces, each starting with a capital, such as Player Health: a scene's name.
	Title string
	// Path is the slash-separated path of the scene file of a scene or prefab from the project directory, such as scenes/player_health.scene.yaml.
	Path string
}

// generatedFile is a Go file game generate renders from a template and the identifiers it declares at the package's top level.
type generatedFile struct {
	// name is the file's name in the project directory, a template rendered with generateData.
	name string
	// template is the template's file name under _templates/generate.
	template string
	// declares are the top-level identifiers the file declares, templates rendered with generateData, which must not be declared in the package already.
	declares []string
}

// registry is a package-level registry the generated code adds itself to: the file that declares it, written when the package does not declare it yet.
type registry struct {
	// ident is the registry's identifier.
	ident string
	// file declares it.
	file generatedFile
}

var (
	componentsRegistry = registry{"Components", generatedFile{name: "components.go", template: "components.go.tmpl", declares: []string{"Components"}}}
	systemsRegistry    = registry{"Systems", generatedFile{name: "systems.go", template: "systems.go.tmpl", declares: []string{"Systems"}}}
	prefabsRegistry    = registry{"Prefabs", generatedFile{name: "prefabs.go", template: "prefabs.go.tmpl", declares: []string{"Prefabs"}}}
)

// generateKind is one kind of thing game generate writes.
type generateKind struct {
	// files are its Go files.
	files []generatedFile
	// registries are the registries its Go code uses.
	registries []registry
	// sceneDir is the directory of its scene file, empty when it has none.
	sceneDir string
	// prefab reports whether its scene file is a prefab's: one object for other scenes to instance.
	prefab bool
}

// kinds are what each word game generate takes writes.
var kinds = map[string]generateKind{
	"component": {
		files: []generatedFile{
			{name: "{{.Snake}}_component.go", template: "component.go.tmpl", declares: []string{"{{.Go}}", "{{.Camel}}Component"}},
			{name: "{{.Snake}}_component_test.go", template: "component_test.go.tmpl", declares: []string{"Test{{.Go}}Component"}},
		},
		registries: []registry{componentsRegistry},
	},
	"system": {
		files: []generatedFile{
			{name: "{{.Snake}}_system.go", template: "system.go.tmpl", declares: []string{"{{.Go}}System"}},
			{name: "{{.Snake}}_system_test.go", template: "system_test.go.tmpl", declares: []string{"Test{{.Go}}System"}},
		},
		registries: []registry{systemsRegistry},
	},
	"scene": {
		files: []generatedFile{
			{name: "{{.Snake}}_scene.go", template: "scene.go.tmpl", declares: []string{"{{.Camel}}SceneYAML", "Load{{.Go}}Scene"}},
			{name: "{{.Snake}}_scene_test.go", template: "scene_test.go.tmpl", declares: []string{"TestLoad{{.Go}}Scene"}},
		},
		registries: []registry{componentsRegistry},
		sceneDir:   "scenes",
	},
	"prefab": {
		files: []generatedFile{
			{name: "{{.Snake}}_prefab.go", template: "prefab.go.tmpl", declares: []string{"{{.Go}}Prefab"}},
			{name: "{{.Snake}}_prefab_test.go", template: "prefab_test.go.tmpl", declares: []string{"Test{{.Go}}Prefab"}},
		},
		registries: []registry{componentsRegistry, prefabsRegistry},
		sceneDir:   "prefabs",
		prefab:     true,
	},
}

// generateNamePattern is what game generate takes as a name: words of ASCII letters and digits, starting with a letter, written in camel case or joined by - or _.
var generateNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*([-_][A-Za-z0-9]+)*$`)

func defineGenerate(a *app, fs *flag.FlagSet) func() error {
	dirArg := fs.String("dir", ".", "the game's project directory, which holds its game.yaml and the Go package the code is written into")
	autoload := fs.Bool("autoload", false, "for a scene, also add it to the scenes game.yaml autoloads")
	mainScene := fs.Bool("main", false, "for a scene, also add it to the scenes game.yaml autoloads and make it the main scene")

	return func() error {
		if fs.NArg() != 2 {
			return usagef("expected a kind (%s) and a name", strings.Join(generateKinds, ", "))
		}
		kindName, name := fs.Arg(0), fs.Arg(1)
		kind, ok := kinds[kindName]
		if !ok {
			return usagef("unknown kind %q; use %s", kindName, strings.Join(generateKinds, ", "))
		}
		if (*autoload || *mainScene) && kindName != "scene" {
			return usagef("-autoload and -main are for a scene")
		}
		data, err := generateNames(name)
		if err != nil {
			return err
		}
		dir := *dirArg
		written, projectFile, err := generate(dir, kind, data, *autoload || *mainScene, *mainScene)
		if err != nil {
			return err
		}
		for _, name := range written {
			fmt.Fprintln(a.stdout, filepath.Join(dir, filepath.FromSlash(name)))
		}
		if projectFile {
			fmt.Fprintln(a.stdout, filepath.Join(dir, project.FileName))
		}
		return nil
	}
}

// generateNames returns the forms of name the templates render, or a usage error when name is not one game generate takes.
func generateNames(name string) (generateData, error) {
	if !generateNamePattern.MatchString(name) {
		return generateData{}, usagef("name %q is not a name game generate takes; use ASCII letters and digits, starting with a letter, in camel case or with words joined by - or _, such as player_health or PlayerHealth", name)
	}
	words := splitWords(name)
	var d generateData
	var snake, title []string
	for i, w := range words {
		lower := strings.ToLower(w)
		upper := w
		if !isUpper(w) {
			upper = strings.ToUpper(w[:1]) + lower[1:]
		}
		d.Go += upper
		if i == 0 {
			d.Camel = lower
		} else {
			d.Camel += upper
		}
		snake = append(snake, lower)
		title = append(title, upper)
	}
	d.Snake = strings.Join(snake, "_")
	d.Title = strings.Join(title, " ")
	return d, nil
}

// splitWords splits name, which generateNamePattern matches, into its words: at - and _, before a capital that follows a lower-case letter or a digit, and before the last capital of a run of them that a lower-case letter follows, so HTTPServer is HTTP and Server. Digits stay with the word before them.
func splitWords(name string) []string {
	var words []string
	for part := range strings.FieldsFuncSeq(name, func(r rune) bool { return r == '-' || r == '_' }) {
		start := 0
		for i := 1; i < len(part); i++ {
			prev, c := rune(part[i-1]), rune(part[i])
			next := rune(0)
			if i+1 < len(part) {
				next = rune(part[i+1])
			}
			if unicode.IsUpper(c) && (unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && unicode.IsLower(next)) {
				words = append(words, part[start:i])
				start = i
			}
		}
		words = append(words, part[start:])
	}
	return words
}

// isUpper reports whether word, of at least two characters, has no lower-case letter, as an initialism such as HTTP does; such a word keeps its case in a Go identifier.
func isUpper(word string) bool {
	return len(word) > 1 && !strings.ContainsFunc(word, unicode.IsLower)
}

// generate writes what kind needs for the thing data names into the project in dir: the registries the package does not declare yet, the kind's Go files, its scene file, and, when autoload is set, the scene's entry in game.yaml's autoload, made its main scene when mainScene is set. It returns the slash-separated paths it wrote from dir, and whether it rewrote game.yaml. It writes nothing when a file it would write exists or an identifier it would declare is declared, and removes what it wrote when it fails partway.
func generate(dir string, kind generateKind, data generateData, autoload, mainScene bool) ([]string, bool, error) {
	pkg, declared, err := packageDecls(dir)
	if err != nil {
		return nil, false, err
	}
	data.Package = pkg
	if kind.sceneDir != "" {
		data.Path = kind.sceneDir + "/" + data.Snake + ".scene.yaml"
	}

	var goFiles []generatedFile
	for _, r := range kind.registries {
		if !declared[r.ident] {
			goFiles = append(goFiles, r.file)
		}
	}
	goFiles = append(goFiles, kind.files...)

	var files []projectFile
	for _, f := range goFiles {
		name, err := render(f.name, data)
		if err != nil {
			return nil, false, err
		}
		for _, d := range f.declares {
			ident, err := render(d, data)
			if err != nil {
				return nil, false, err
			}
			if declared[ident] {
				return nil, false, fmt.Errorf("package %s in %s already declares %s, which %s would declare; choose another name", pkg, dir, ident, name)
			}
			declared[ident] = true
		}
		src, err := fs.ReadFile(generateFS, "_templates/generate/"+f.template)
		if err != nil {
			return nil, false, err
		}
		content, err := render(string(src), data)
		if err != nil {
			return nil, false, err
		}
		formatted, err := format.Source([]byte(content))
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", name, err)
		}
		files = append(files, projectFile{name: name, content: formatted})
	}

	var gameYAML []byte
	if data.Path != "" {
		var f *project.File
		if !kind.prefab {
			if f, err = readProjectFile(dir, autoload); err != nil {
				return nil, false, err
			}
		}
		content, err := sceneFile(kind, data, f)
		if err != nil {
			return nil, false, err
		}
		files = append(files, projectFile{name: kind.sceneDir, dir: true}, projectFile{name: data.Path, content: content})
		if autoload {
			if gameYAML, err = addAutoload(f, data.Camel, data.Path, mainScene); err != nil {
				return nil, false, err
			}
		}
	}

	created, err := newDirs(dir, files)
	if err != nil {
		return nil, false, err
	}
	if err := writeProject(dir, files); err != nil {
		return nil, false, err
	}
	var written []string
	for _, f := range files {
		if !f.dir {
			written = append(written, f.name)
		}
	}
	if gameYAML != nil {
		if err := replaceFile(filepath.Join(dir, project.FileName), gameYAML); err != nil {
			for _, name := range slices.Backward(append(created, written...)) {
				err = errors.Join(err, os.Remove(filepath.Join(dir, filepath.FromSlash(name))))
			}
			return nil, false, err
		}
	}
	return written, gameYAML != nil, nil
}

// render executes text, a template, with data, failing on a missing key.
func render(text string, data generateData) (string, error) {
	t, err := template.New("").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// packageDecls returns the name of the Go package in dir and the identifiers it declares at its top level, its in-package tests and files built only for other platforms included.
func packageDecls(dir string) (string, map[string]bool, error) {
	bp, err := build.ImportDir(dir, 0)
	if err != nil {
		if _, ok := errors.AsType[*build.NoGoError](err); ok {
			return "", nil, fmt.Errorf("%s holds no Go package; game generate writes Go code into the package of the project directory (-dir), such as the one game new writes", dir)
		}
		return "", nil, err
	}
	declared := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range slices.Concat(bp.GoFiles, bp.CgoFiles, bp.TestGoFiles, bp.IgnoredGoFiles) {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			if slices.Contains(bp.IgnoredGoFiles, name) {
				continue
			}
			return "", nil, err
		}
		if f.Name.Name != bp.Name {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name != "init" {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							declared[n.Name] = true
						}
					}
				}
			}
		}
	}
	return bp.Name, declared, nil
}

// readProjectFile reads dir's game.yaml, nil when there is none and it is not required. When required, which adding to it is, it must exist and be in the form gameproject fmt writes, since rewriting it would lose what differs, such as comments.
func readProjectFile(dir string, required bool) (*project.File, error) {
	data, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if errors.Is(err, fs.ErrNotExist) && !required {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := project.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, project.FileName), err)
	}
	if required {
		canonical, err := project.Encode(f)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(canonical, data) {
			return nil, fmt.Errorf("%s is not in the form gameproject fmt writes, so adding the scene to it would rewrite the rest; run gameproject fmt first", filepath.Join(dir, project.FileName))
		}
	}
	return f, nil
}

// sceneFile returns the scene file of a scene or prefab, in the scene format's current version and the form sceneformat.Encode writes. A scene's virtual size is the project's virtual resolution, or its window's size without one, and it has one empty layer; a prefab is a scene of one object named data.Snake whose prefab is data.Camel, for other scenes to instance.
func sceneFile(kind generateKind, data generateData, f *project.File) ([]byte, error) {
	doc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Name: data.Title}
	layer := sceneformat.LayerDoc{Name: "main"}
	if kind.prefab {
		layer.Objects = []sceneformat.ObjectDoc{{ID: data.Snake, Prefab: data.Camel}}
	} else if f != nil {
		if f.Display != nil {
			doc.VirtualWidth, doc.VirtualHeight = f.Display.Width, f.Display.Height
		} else {
			doc.VirtualWidth, doc.VirtualHeight = f.Window.Width, f.Window.Height
		}
	}
	doc.Layers = []sceneformat.LayerDoc{layer}
	return sceneformat.Encode(doc)
}

// addAutoload returns f with the scene at path added to its autoloaded scenes under name, as its main scene when main is set, in the form project.Encode writes.
func addAutoload(f *project.File, name, scenePath string, main bool) ([]byte, error) {
	f = f.Clone()
	if f.Autoload == nil {
		f.Autoload = &project.Autoload{}
	}
	for _, s := range f.Autoload.Scenes {
		if s.Name == name {
			return nil, fmt.Errorf("%s already autoloads a scene called %s, from %s", project.FileName, name, s.Path)
		}
	}
	f.Autoload.Scenes = append(f.Autoload.Scenes, project.Entry{Name: name, Path: scenePath})
	if main {
		f.Autoload.MainScene = name
	}
	return project.Encode(f)
}

// newDirs returns the directories among files that dir does not hold yet, so they can be removed again.
func newDirs(dir string, files []projectFile) ([]string, error) {
	var created []string
	for _, f := range files {
		if !f.dir {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(f.name))); errors.Is(err, fs.ErrNotExist) {
			created = append(created, f.name)
		} else if err != nil {
			return nil, err
		}
	}
	return created, nil
}

// replaceFile replaces the file at name with content, writing it beside name first and renaming it over name, so a failure leaves the old file whole. The new file keeps the old one's permissions.
func replaceFile(name string, content []byte) (err error) {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+"-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	_, werr := tmp.Write(content)
	if err := errors.Join(werr, tmp.Chmod(info.Mode().Perm()), tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}
