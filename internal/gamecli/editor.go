package gamecli

import (
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/library"
)

// editorCommand opens the scene editor on a game's project, as its directory or its game.yaml, which opens the project and its main scene, or on a YAML scene file, which inside a project opens that project too, or on the Start Screen when nothing is named. A project's scenes draw its own atlases, which the asset browser lists, and its scene files place as instances. A project with an editor command of its own, which `game generate editor` writes, is opened by running that command with go run, so its component types are checked and its plugins built in. With -library it shows the library panel, which installs packs from that library into the open project. A second argument is an animation clip file the animation panel opens on the object named by the third argument, or on the scene's first object.
var editorCommand = &command{
	name:    "editor",
	args:    "[project-dir | game.yaml | scene.yaml] [clip.animation.yaml [object-id]]",
	summary: "Open the scene editor on a project or a YAML scene, and an animation clip on one of its objects, with the project's own editor command when it has one, or on the start screen when none is named",
	define:  defineEditor,
	complete: map[string]completion{
		"library": filePaths,
		"":        filePaths,
	},
}

func defineEditor(a *app, fs *flag.FlagSet) func() error {
	lib := fs.String("library", "", "show the library panel, which installs packs into the open project, for this library index: an https URL such as "+library.DefaultIndex+", an index file, or a directory holding index.yaml")
	return func() error {
		args := fs.Args()
		if len(args) > 3 {
			return usagef("expected at most a project or scene file, a clip file and an object ID, got %d arguments", len(args))
		}
		var ed *editor.Editor
		root, inProject := "", false
		if len(args) > 0 {
			root, inProject = editorcmd.ProjectRoot(args[0])
		}
		switch {
		case inProject:
			run, ok, err := projectEditor(root)
			if err != nil {
				return err
			}
			if ok {
				return runOwnEditor(a, root, run, *lib, args)
			}
			if p, err := build.ImportDir(filepath.Join(root, "cmd", "editor"), 0); err == nil && p.Name == "main" {
				fmt.Fprintf(a.stderr, "editor: %s does not use package editorcmd, so it may not take game editor's arguments; game editor opens the project itself instead. Run that command with go run ./cmd/editor\n", filepath.Join(root, "cmd", "editor"))
			}
			// Without an editor of its own, a project's components are free-form, since only the game's Go code registers their types.
			if ed, err = editorcmd.New(editorcmd.Options{Library: *lib}); err != nil {
				return err
			}
		default:
			var err error
			if ed, err = newDemoEditor(); err != nil {
				return err
			}
			ed.ProjectAssets = true
			if len(args) > 0 {
				// A scene outside any project names the files it uses, such as a layered tilemap's YAML tileset or a Tiled or LDtk source, by their paths from its own directory, and draws the demo's atlases.
				root := filepath.Dir(args[0])
				if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
					root = args[0]
				}
				atlases, _ := ed.Resolver.(sceneformat.AtlasMap)
				ed.Resolver = sceneformat.FSAssets{FS: os.DirFS(root), Atlases: atlases}
			}
			if *lib != "" {
				if ed.Library, err = library.Open(context.Background(), *lib); err != nil {
					return err
				}
			}
		}
		configureEditorStartScreen(ed, a)
		if len(args) == 0 {
			ed.SetStartScreenOpen(true)
		} else if err := editorcmd.Open(ed, args); err != nil {
			return err
		}
		a.launch(ed)
		return nil
	}
}

// gameEditorTag is the build tag that builds a project's package as its editor command, as `game generate editor` writes it.
const gameEditorTag = "gameeditor"

// configureEditorStartScreen populates ed's Start Screen template list from `game new`'s template registry,
// connects project creation to `game new`, and loads the per-user recent projects file.
func configureEditorStartScreen(ed *editor.Editor, a *app) {
	ed.SetProjectTemplates(editorTemplatesFromRegistry())
	ed.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, stdout, stderr io.Writer) error {
		return createProject(createProjectOptions{
			dir:      req.TargetDir(),
			name:     req.Name,
			module:   req.Module,
			template: req.Template,
		}, stdout, stderr)
	}
	recentPath := ""
	if a != nil {
		recentPath = a.recentProjectsPath
	}
	if recentPath == "" {
		if p, err := editor.DefaultRecentProjectsPath(); err == nil {
			recentPath = p
		}
	}
	if recentPath != "" {
		_ = ed.SetRecentProjectsPath(recentPath)
	}
	errOut := io.Writer(os.Stderr)
	if a != nil && a.stderr != nil {
		errOut = a.stderr
	}
	layoutPath := ""
	if a != nil {
		layoutPath = a.layoutPath
	}
	if layoutPath == "" {
		if p, err := editor.DefaultLayoutPath(); err != nil {
			fmt.Fprintf(errOut, "editor layout: %v\n", err)
		} else {
			layoutPath = p
		}
	}
	if layoutPath != "" {
		if err := ed.SetLayoutPath(layoutPath); err != nil {
			fmt.Fprintf(errOut, "editor layout: %v\n", err)
		}
	}
}

// editorTemplatesFromRegistry builds the Start Screen template cards directly from `game new`'s projectTemplates registry.
func editorTemplatesFromRegistry() []editor.ProjectTemplate {
	out := make([]editor.ProjectTemplate, len(projectTemplates))
	for i, t := range projectTemplates {
		out[i] = editor.ProjectTemplate{
			Name:    t.name,
			Summary: t.summary,
			Default: i == 0,
		}
	}
	return out
}

// editorcmdPath is the import path of package editorcmd, whose command line game editor runs a project's editor command with.
const editorcmdPath = "github.com/nsgo-dev/game/pkg/editor/editorcmd"

// projectEditor returns the go command's arguments that run the editor command of the project in root, and false when it has none, and an error only when a Go file that would hold it does not parse: the main package in cmd/editor that imports package editorcmd, so it takes the command line game editor passes on, as Field Medic's does, or the project's own main package built with the gameeditor tag, when that tag swaps its main function for one, as `game generate editor` writes it.
func projectEditor(root string) ([]string, bool, error) {
	if p, err := build.ImportDir(filepath.Join(root, "cmd", "editor"), 0); err == nil && p.Name == "main" && slices.Contains(p.Imports, editorcmdPath) {
		return []string{"run", "./cmd/editor"}, true, nil
	}
	plain, err := build.ImportDir(root, 0)
	if err != nil {
		// Without a Go package of its own that the go command can build, the project has no editor command there, and game editor opens it itself.
		return nil, false, nil
	}
	ctx := build.Default
	ctx.BuildTags = append(slices.Clone(ctx.BuildTags), gameEditorTag)
	tagged, err := ctx.ImportDir(root, 0)
	if err != nil || tagged.Name != "main" {
		return nil, false, nil
	}
	fset := token.NewFileSet()
	for _, name := range tagged.GoFiles {
		if slices.Contains(plain.GoFiles, name) {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, false, err
		}
		if declaresMain(f) {
			return []string{"run", "-tags", gameEditorTag, "."}, true, nil
		}
	}
	return nil, false, nil
}

// declaresMain reports whether f declares a main function.
func declaresMain(f *ast.File) bool {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "main" {
			return true
		}
	}
	return false
}

// runOwnEditor runs the project in root's own editor command, whose go command arguments run are, on args, the editor command's arguments, whose scene and clip paths are made absolute since the command runs in root, after -library lib when lib is set.
func runOwnEditor(a *app, root string, run []string, lib string, args []string) error {
	cmd := slices.Clone(run)
	if lib != "" {
		if !strings.Contains(lib, "://") {
			abs, err := filepath.Abs(lib)
			if err != nil {
				return err
			}
			lib = abs
		}
		cmd = append(cmd, "-library", lib)
	}
	for i, arg := range args {
		// The scene and clip are paths, made absolute; the object ID is not a path and passes through as it is.
		if i < 2 {
			abs, err := filepath.Abs(arg)
			if err != nil {
				return err
			}
			arg = abs
		}
		cmd = append(cmd, arg)
	}
	if err := a.goRun(root, cmd, a.stdout, a.stderr); err != nil {
		return fmt.Errorf("the project's editor (go %s in %s): %w", strings.Join(run, " "), root, err)
	}
	return nil
}

// runGoIn runs the go command with args in dir, with the terminal's standard input, for the editor command a project has of its own.
func runGoIn(dir string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return cmd.Run()
}

// newDemoEditor returns an editor on the built-in demo room, also used to open a scene outside any project with the demo's atlases and physics components.
func newDemoEditor() (*editor.Editor, error) {
	return editor.NewDemo()
}
