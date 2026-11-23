package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"text/template"

	"github.com/nsgo-dev/game/packages/game/project"
	"go.yaml.in/yaml/v3"
)

// newCommand creates a game project from a template.
var newCommand = &command{
	name:    "new",
	args:    "<dir>",
	summary: "Create a game project in a directory from a template, with its go.mod requiring the game layer and the game and nsgo tools",
	define:  defineNew,
	complete: map[string]completion{
		"": filePaths,
	},
}

const (
	// gameModule is the game layer's module, which a new project requires.
	gameModule = "github.com/nsgo-dev/game/packages/game"
	// gameTool is this command's package, which a new project adds as a tool.
	gameTool = gameModule + "/cmd/game"
)

// starterFS holds the starter project, a copy of template/template in this repository that TestStarterMatchesTemplateModule keeps identical.
//
//go:embed all:_templates/starter
var starterFS embed.FS

// projectTemplate is a project game new can start from.
type projectTemplate struct {
	// name is what -template takes.
	name string
	// files are the project's files: a file ending in .tmpl, nsgo.yaml and main.go are rendered with text/template and newData's fields, as nsgo init -template renders a template module's, and the rest are copied.
	files fs.FS
}

// projectTemplates are the templates game new offers, the default first.
var projectTemplates = []projectTemplate{
	{name: "starter", files: mustSub(starterFS, "_templates/starter")},
}

// mustSub returns the subtree of fsys at dir, which is embedded and so always there.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// runGo runs the go command with args in dir, writing its output to stdout and stderr. A test replaces it to keep go from reaching the network.
var runGo = func(dir string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.Command("go", args...)
	// The project is a module of its own, even when created inside a workspace that does not use it.
	cmd.Dir, cmd.Env = dir, append(os.Environ(), "GOWORK=off")
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// isStdPackage reports whether path is a standard-library package of the go command that game new runs. A test can replace it.
var isStdPackage = func(path string) (bool, error) {
	cmd := exec.Command("go", "list", "-e", "-f", "{{.Standard}}", "--", path)
	// Outside any module and workspace, so only the standard library can provide path.
	cmd.Dir, cmd.Env = os.TempDir(), append(os.Environ(), "GOWORK=off", "GO111MODULE=on")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("go list %s: %w: %s", path, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// newData is what a template's rendered files can use, the same fields nsgo init -template offers.
type newData struct {
	// Name is the game's display name, written to the name of nsgo.yaml and game.yaml.
	Name string
	// ID is the app identifier, written to the id of nsgo.yaml and game.yaml.
	ID string
	// Module is the module path written to go.mod.
	Module string
	// Slug is Name in lower case with its words joined by -.
	Slug string
}

var (
	appNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _-]*$`)
	nonAlnum       = regexp.MustCompile(`[^A-Za-z0-9]+`)
	notNameChars   = regexp.MustCompile(`[^A-Za-z0-9 _-]+`)
)

func defineNew(a *app, fs *flag.FlagSet) func() error {
	names := make([]string, len(projectTemplates))
	for i, t := range projectTemplates {
		names[i] = t.name
	}
	name := fs.String("name", "", "the game's display name in game.yaml and nsgo.yaml: letters, digits, spaces, - and _ (default: the directory's name)")
	module := fs.String("module", "", "the module path in go.mod (default: the name in lower case with its words joined by -)")
	tmpl := fs.String("template", projectTemplates[0].name, "the template to start from: "+strings.Join(names, ", "))
	version := fs.String("version", "", "the version of "+gameModule+" to require, such as v0.4.0, a commit or latest (default: this command's own version, or latest for a development build)")

	return func() error {
		if fs.NArg() == 0 {
			return usagef("a directory is required")
		}
		if fs.NArg() > 1 {
			return usagef("unexpected arguments %q", fs.Args()[1:])
		}
		dir := fs.Arg(0)
		i := slices.IndexFunc(projectTemplates, func(t projectTemplate) bool { return t.name == *tmpl })
		if i < 0 {
			return usagef("no template %q; use %s", *tmpl, strings.Join(names, ", "))
		}
		data, err := projectData(dir, *name, *module)
		if err != nil {
			return err
		}
		v := *version
		if v == "" {
			v = toolVersion()
		} else if strings.ContainsFunc(v, func(r rune) bool { return r <= ' ' || r == '@' }) || strings.HasPrefix(v, "-") {
			return usagef("-version %q is not a module version", v)
		}

		files, err := renderProject(projectTemplates[i].files, data)
		if err != nil {
			return fmt.Errorf("template %s: %w", *tmpl, err)
		}
		if err := writeProject(dir, files); err != nil {
			return err
		}
		for _, f := range files {
			if !f.dir {
				fmt.Fprintln(a.stdout, filepath.Join(dir, f.name))
			}
		}
		steps := [][]string{{"get", "-tool", gameTool + "@" + v}, {"mod", "tidy"}}
		for i, args := range steps {
			if err := runGo(dir, a.stdout, a.stderr, args...); err != nil {
				rest := make([]string, len(steps)-i)
				for j, r := range steps[i:] {
					rest[j] = "go " + strings.Join(r, " ")
				}
				return fmt.Errorf("go %s in %s: %w; the project's files are written, so run %s there once it can succeed", strings.Join(args, " "), dir, err, strings.Join(rest, " && "))
			}
		}
		fmt.Fprintf(a.stdout, "\ncd %s && go tool nsgo dev\n", shellWord(dir, runtime.GOOS))
		return nil
	}
}

// shellWord returns s as one word for a shell on goos: as it is when it holds only characters a shell takes literally, and otherwise in single quotes for a POSIX shell or in double quotes on Windows, where cmd and PowerShell both take them and \ separates paths.
func shellWord(s, goos string) string {
	literal := "-_./+,:=@%"
	if goos == "windows" {
		literal += `\`
	}
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune(literal, r))
	}) {
		return s
	}
	if goos == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// projectData returns what a template renders for a project in dir, naming it after the directory and deriving its module from the name where name and module are empty.
func projectData(dir, name, module string) (newData, error) {
	if name == "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return newData{}, err
		}
		base := filepath.Base(abs)
		name = strings.TrimLeft(strings.Trim(notNameChars.ReplaceAllString(base, "-"), " _-"), "0123456789 _-")
		if name == "" {
			return newData{}, usagef("cannot name a game after %q; give one with -name", base)
		}
	} else if !appNamePattern.MatchString(name) {
		return newData{}, usagef("-name %q is not a game name; use letters, digits, spaces, - and _, starting with a letter", name)
	}
	slug := strings.Trim(strings.ToLower(nonAlnum.ReplaceAllString(name, "-")), "-")
	if module == "" {
		module = slug
	} else if err := checkModulePath(module); err != nil {
		return newData{}, usagef("-module %q: %v", module, err)
	}
	// A main module whose path is a standard-library package's, such as time, cannot build: go reports an ambiguous import of that package. Only a path whose first element has no dot can be one.
	if first, _, _ := strings.Cut(module, "/"); !strings.Contains(first, ".") {
		std, err := isStdPackage(module)
		if err != nil {
			return newData{}, err
		}
		if std {
			return newData{}, usagef("module path %q is a standard-library package, so the project could not build; give another with -module, such as example.com/%s", module, module)
		}
	}
	return newData{
		Name:   name,
		ID:     "dev.nsgo." + strings.ToLower(nonAlnum.ReplaceAllString(name, "")),
		Module: module,
		Slug:   slug,
	}, nil
}

// checkModulePath reports whether path is a module path go.mod can hold: slash-separated elements of ASCII letters, digits and - . _ ~, none empty or starting or ending with a dot, and not starting with -.
func checkModulePath(path string) error {
	if strings.HasPrefix(path, "-") {
		return errors.New("starts with -")
	}
	for elem := range strings.SplitSeq(path, "/") {
		if elem == "" {
			return errors.New("has an empty path element")
		}
		if strings.HasPrefix(elem, ".") || strings.HasSuffix(elem, ".") {
			return fmt.Errorf("element %q starts or ends with a dot", elem)
		}
		for _, r := range elem {
			if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("-._~", r)) {
				return fmt.Errorf("element %q holds %q; use ASCII letters, digits and - . _ ~", elem, r)
			}
		}
	}
	return nil
}

// toolVersion is the version of the game module this command was built from, which a new project then requires, or latest when it was built from a local checkout and so has no version another module can require.
func toolVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "latest"
	}
	return buildVersion(info)
}

// buildVersion returns the version of the game module that info records, as the main module (go run …@version, go install) or a dependency (go tool game in another module), or latest when it has none or it is replaced or modified.
func buildVersion(info *debug.BuildInfo) string {
	m := &info.Main
	if m.Path != gameModule {
		i := slices.IndexFunc(info.Deps, func(d *debug.Module) bool { return d.Path == gameModule })
		if i < 0 {
			return "latest"
		}
		m = info.Deps[i]
	}
	if m.Replace != nil || m.Version == "" || m.Version == "(devel)" || strings.HasSuffix(m.Version, "+dirty") {
		return "latest"
	}
	return m.Version
}

// projectFile is a file or directory of a new project.
type projectFile struct {
	// name is its slash-separated path in the project.
	name string
	// dir is whether it is a directory.
	dir bool
	// content is a file's content.
	content []byte
}

// renderProject returns the project files of template src rendered with data, directories before what they hold: a file ending in .tmpl, nsgo.yaml and main.go are rendered with text/template, failing on a missing key, and lose .tmpl, and the name and id of nsgo.yaml and, when the template has one, game.yaml are then set to data's.
func renderProject(src fs.FS, data newData) ([]projectFile, error) {
	var files []projectFile
	seen := map[string]bool{}
	err := fs.WalkDir(src, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		f := projectFile{name: name, dir: d.IsDir()}
		if !f.dir {
			content, err := fs.ReadFile(src, name)
			if err != nil {
				return err
			}
			var marked bool
			f.name, marked = strings.CutSuffix(name, ".tmpl")
			if marked || f.name == "nsgo.yaml" || f.name == "main.go" {
				t, err := template.New(name).Option("missingkey=error").Parse(string(content))
				if err != nil {
					return err
				}
				var b bytes.Buffer
				if err := t.Execute(&b, data); err != nil {
					return err
				}
				content = b.Bytes()
			}
			// game.yaml, the project file, holds the name and id that nsgo.yaml repeats, and project.Check refuses the pair when they differ.
			if f.name == "nsgo.yaml" || f.name == project.FileName {
				for _, kv := range [][2]string{{"name", data.Name}, {"id", data.ID}} {
					if content, err = setYAMLScalar(content, kv[0], kv[1]); err != nil {
						return fmt.Errorf("%s: %w", f.name, err)
					}
				}
			}
			f.content = content
		}
		if seen[f.name] {
			return fmt.Errorf("%s comes from two template files", f.name)
		}
		seen[f.name] = true
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, need := range []string{"go.mod", "nsgo.yaml", "main.go"} {
		if !seen[need] {
			return nil, fmt.Errorf("no %s", need)
		}
	}
	return files, nil
}

// setYAMLScalar replaces the value of the top-level key in the YAML document src, which must appear once as "key: value" on a line of its own, with value written as a YAML string, quoted where a bare one would read as something else, and leaves every other line, comments included, as it is.
func setYAMLScalar(src []byte, key, value string) ([]byte, error) {
	lines := bytes.SplitAfter(src, []byte("\n"))
	at := -1
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte(key+":")) {
			if at >= 0 {
				return nil, fmt.Errorf("%s appears twice", key)
			}
			at = i
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("no top-level %s", key)
	}
	scalar, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	end := ""
	if bytes.HasSuffix(lines[at], []byte("\n")) {
		end = "\n"
	}
	lines[at] = []byte(key + ": " + strings.TrimSuffix(string(scalar), "\n") + end)
	return bytes.Join(lines, nil), nil
}

// writeProject writes files into dir, creating it if needed. It writes nothing when any of the files already exists there, and removes what it wrote when it fails partway.
func writeProject(dir string, files []projectFile) (err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	for _, f := range files {
		info, err := root.Lstat(f.name)
		switch {
		case err == nil && !(f.dir && info.IsDir()):
			return fmt.Errorf("%s exists", filepath.Join(dir, f.name))
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	var created []string
	defer func() {
		if err != nil {
			for _, name := range slices.Backward(created) {
				err = errors.Join(err, root.Remove(name))
			}
		}
	}()
	for _, f := range files {
		if f.dir {
			if err := root.Mkdir(f.name, 0o755); err != nil {
				if errors.Is(err, fs.ErrExist) {
					continue
				}
				return err
			}
		} else if err := createFile(root, f.name, f.content); err != nil {
			return err
		}
		created = append(created, f.name)
	}
	return nil
}

// createFile writes content to a new file called name in root, failing if one is already there, and removes the file again when writing fails.
func createFile(root *os.Root, name string, content []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(content)
	if err := errors.Join(werr, f.Close()); err != nil {
		return errors.Join(err, root.Remove(name))
	}
	return nil
}
