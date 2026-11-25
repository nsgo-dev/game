// Package editorcmd is the scene editor's command line, which `go tool game editor` runs and which a game's own editor command runs with the game's component types and editor plugins built in. `game generate editor` writes such a command into a project: a main function built with the gameeditor tag that calls Main with the project's Components and plugins, so its scenes' components are checked against their registered types. `go tool game editor <dir>` runs that command when the project has one.
//
// An editor command opens a project, as its directory or its game.yaml, which opens its main scene, or a scene file, which inside a project opens that project too, and optionally an animation clip on one of the scene's objects. The editor New returns sets editor.Editor.ProjectAssets, so an open project's scenes draw its own atlases, its atlases fill the asset browser and its scene files can be placed as instances (project.Resolver).
package editorcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

// Usage is the arguments an editor command takes after its flags.
const Usage = "[project-dir | game.yaml | scene.yaml] [clip.animation.yaml [object-id]]"

// Options are what an editor command builds its editor with.
type Options struct {
	// Components are the component types the scenes' objects may set, which the editor checks every component and field against and lists in the inspector, together with the built-in 2D and 3D physics components (sceneformat.RegisterPhysicsComponents) the registry does not register itself; nil leaves components free-form.
	Components *sceneformat.ComponentRegistry
	// Plugins are the editor plugins built into the command, which a project turns on under editor.plugins in its game.yaml.
	Plugins []editor.Plugin
	// AppName is the editor's name in its window title (editor.Editor.AppName); empty means the editor's default.
	AppName string
	// Library is the library index the library panel installs packs from (editor.Editor.Library), as library.Open takes it: an https URL, an index file or a directory holding index.yaml; empty hides the panel.
	Library string
	// WindowWidth and WindowHeight are the size, in points, of the window Run shows the editor in; zero for either means the default, 1120 by 680.
	WindowWidth, WindowHeight core.Points
	// RequirePlugins makes Run refuse a project whose game.yaml lists an editor plugin that Plugins lacks (editor.Editor.MissingPlugins), for a command that should not edit the project's scenes without them.
	RequirePlugins bool
}

// New returns an editor on an empty scene with opts' component types, plugins, name and library, and ProjectAssets set, so the project Open opens supplies the scenes' assets. A scene opened outside any project resolves no atlases.
func New(opts Options) (*editor.Editor, error) {
	// A scene opened outside any project resolves no atlases; one inside a project gets the project's (ProjectAssets).
	ed, err := editor.New(nil, sceneformat.AtlasMap{})
	if err != nil {
		return nil, err
	}
	ed.ProjectAssets = true
	ed.AppName = opts.AppName
	components, err := withPhysicsComponents(opts.Components)
	if err != nil {
		return nil, err
	}
	if err := ed.SetComponentRegistry(components); err != nil {
		return nil, err
	}
	for _, p := range opts.Plugins {
		if err := ed.RegisterPlugin(p); err != nil {
			return nil, err
		}
	}
	if opts.Library != "" {
		if ed.Library, err = library.Open(context.Background(), opts.Library); err != nil {
			return nil, err
		}
	}
	return ed, nil
}

// withPhysicsComponents returns a copy of reg that also registers each built-in physics component (sceneformat.PhysicsComponentRegistry) reg lacks, so a game's registry, which game generate starts empty, still opens scenes with physics bodies and colliders as the built-in editor does; a type reg registers under a physics component's name is kept. It returns nil for a nil reg and leaves reg unchanged.
func withPhysicsComponents(reg *sceneformat.ComponentRegistry) (*sceneformat.ComponentRegistry, error) {
	if reg == nil {
		return nil, nil
	}
	out := sceneformat.NewComponentRegistry()
	for _, t := range reg.Types() {
		if err := out.Register(t); err != nil {
			return nil, err
		}
	}
	for _, t := range sceneformat.PhysicsComponentRegistry().Types() {
		if _, ok := out.Lookup(t.Name); ok {
			continue
		}
		if err := out.Register(t); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Open opens in ed what an editor command's arguments name, as Usage gives them. args[0] is a project, as its directory or its game.yaml, which opens the project and its main scene (editor.Editor.OpenProject), or a scene file, which opens with the project whose directory holds it, when one does (editor.Editor.OpenProjectScene, which sets ScenePath), and on its own otherwise (editor.Editor.OpenFile). args[1] is an animation clip file the animation panel opens on the object named by args[2], or on the scene's first object; that object is selected, so when the clip file does not exist yet the toolbar's Animate button starts a clip on it, and the panel's Save Clip button writes the clip to args[1]. No arguments open nothing.
func Open(ed *editor.Editor, args []string) error {
	if len(args) > 3 {
		return fmt.Errorf("expected at most a project or scene file, a clip file and an object ID, got %d arguments", len(args))
	}
	if len(args) == 0 {
		return nil
	}
	if err := openPath(ed, args[0]); err != nil {
		return fmt.Errorf("open %s: %w", args[0], err)
	}
	if len(args) < 2 {
		return nil
	}
	clipPath := args[1]
	target := firstObjectID(ed.Doc)
	if len(args) > 2 {
		target = args[2]
	}
	if target == "" {
		return fmt.Errorf("open clip %s: %s has no object to play it on", clipPath, args[0])
	}
	if !ed.SelectObject(target) {
		return fmt.Errorf("open clip %s: %s has no object %q", clipPath, args[0], target)
	}
	if _, err := os.Stat(clipPath); err == nil {
		if err := ed.OpenClipFile(clipPath, target); err != nil {
			return fmt.Errorf("open clip %s: %w", clipPath, err)
		}
	}
	ed.OnSaveClip = func(data []byte) error { return os.WriteFile(clipPath, data, 0o644) }
	return nil
}

// openPath opens p in ed: a project, as a directory or its game.yaml, or else a scene file, with the project it lies in when there is one.
func openPath(ed *editor.Editor, p string) error {
	if IsProject(p) {
		return ed.OpenProject(p)
	}
	if root, ok := ProjectRoot(p); ok {
		return ed.OpenProjectScene(root, p)
	}
	return ed.OpenFile(p)
}

// IsProject reports whether p names a project rather than a scene file: a directory, or a file named game.yaml.
func IsProject(p string) bool {
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return true
	}
	return filepath.Base(p) == project.FileName
}

// ProjectRoot returns the directory of the project p belongs to, and false when it belongs to none: p itself for a directory holding a game.yaml, the directory of a game.yaml, and for any other file the nearest directory above it that holds a game.yaml, unless a directory nearer the file holds an nsgo.yaml or a go.mod, which makes that directory the file's root as game check finds it. For a file, the directory it returns is absolute.
func ProjectRoot(p string) (string, bool) {
	if IsProject(p) {
		dir := p
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			dir = filepath.Dir(p)
		}
		_, err := os.Stat(filepath.Join(dir, project.FileName))
		return dir, err == nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	// The walk stops at the first directory holding game.yaml, nsgo.yaml or go.mod, the root game check and the project index find for the file, so a file in a Go module or NSGo app of its own never opens with a project further up.
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if isFile(filepath.Join(dir, project.FileName)) {
			return dir, true
		}
		if isFile(filepath.Join(dir, project.NSGoFileName)) || isFile(filepath.Join(dir, "go.mod")) || filepath.Dir(dir) == dir {
			return "", false
		}
	}
}

// isFile reports whether p is a file that exists.
func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// firstObjectID returns the ID of the first object in doc's layers, or "".
func firstObjectID(doc *sceneformat.SceneDoc) string {
	if doc == nil {
		return ""
	}
	for _, layer := range doc.Layers {
		for _, o := range layer.Objects {
			if o.ID != "" {
				return o.ID
			}
		}
	}
	return ""
}

// Default window size, in points, Show opens the editor at.
const (
	DefaultWindowWidth  = 1120
	DefaultWindowHeight = 680
)

// Show shows ed in the scene editor's window, DefaultWindowWidth by DefaultWindowHeight, serving its MCP server for go tool game mcp (Serve), and returns once it closes.
func Show(ed *editor.Editor) {
	show(ed, DefaultWindowWidth, DefaultWindowHeight)
}

// show is Show in a window width by height points.
func show(ed *editor.Editor, width, height core.Points) {
	if ed != nil && ed.SettingsPath == "" {
		if err := ed.LoadUserSettings(); err != nil {
			fmt.Fprintf(os.Stderr, "editor settings: %v\n", err)
		}
	}
	nsgo.Run(func(cx *core.Scope) core.Node {
		Serve(cx, ed, os.Stderr)
		// EditorView titles the window after the open scene.
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(width, height))
}

// Serve serves ed's MCP server (package editormcp) on the endpoint of the project ed has open, and of each project it opens later, until cx is cleaned up, and offers its tools to nsgo mcp under go tool nsgo dev -automation, so an agent reaches a game's own editor as it reaches go tool game editor. An endpoint it cannot open, as when another editor has the project open, is reported on stderr; the editor works without it.
func Serve(cx *core.Scope, ed *editor.Editor, stderr io.Writer) *editormcp.Server {
	s := editormcp.New(ed, editormcp.Options{Run: cx.Post})
	if err := s.Open(); err != nil {
		fmt.Fprintf(stderr, "editor: agents cannot reach this editor: %v\n", err)
	}
	if err := s.RegisterAutomation(cx); err != nil {
		fmt.Fprintf(stderr, "editor: %v\n", err)
	}
	cx.Cleanup(func() { _ = s.Close() })
	return s
}

// errUsage marks an error in an editor command's command line.
var errUsage = errors.New("usage")

// Run is an editor command run with args, its command line after the program's name: it parses the -library flag into opts.Library and the arguments Usage gives, builds the editor with New, opens the arguments with Open, or the project in the working directory without any, refuses a project listing plugins the editor lacks when opts.RequirePlugins is set, and shows the editor with launch, or when launch is nil as Show does, in a window of opts.WindowWidth by opts.WindowHeight. Usage and flag errors are reported on stderr and wrap a usage error that Main exits 2 for.
func Run(args []string, opts Options, stderr io.Writer, launch func(*editor.Editor)) error {
	fs := flag.NewFlagSet("editor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: editor [-library index] %s\n", Usage)
		fs.PrintDefaults()
	}
	lib := fs.String("library", opts.Library, "show the library panel, which installs packs into the open project, for this library index: an https URL such as "+library.DefaultIndex+", an index file, or a directory holding index.yaml")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if fs.NArg() > 3 {
		err := fmt.Errorf("expected at most a project or scene file, a clip file and an object ID, got %d arguments", fs.NArg())
		fmt.Fprintf(stderr, "editor: %v\n", err)
		fs.Usage()
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	opts.Library = *lib
	ed, err := New(opts)
	if err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		rest = []string{"."}
	}
	if err := Open(ed, rest); err != nil {
		return err
	}
	if missing := ed.MissingPlugins(); opts.RequirePlugins && len(missing) > 0 {
		return fmt.Errorf("%s lists editor plugins this editor is built without: %v", ed.ProjectPath, missing)
	}
	if launch == nil {
		w, h := opts.WindowWidth, opts.WindowHeight
		if w <= 0 || h <= 0 {
			w, h = DefaultWindowWidth, DefaultWindowHeight
		}
		launch = func(ed *editor.Editor) { show(ed, w, h) }
	}
	launch(ed)
	return nil
}

// Main is a game's editor command: it runs Run with the process's arguments and opts, and exits with status 2 for a usage error and 1 for any other, after printing it.
func Main(opts Options) {
	err := Run(os.Args[1:], opts, os.Stderr, nil)
	switch {
	case err == nil:
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case errors.Is(err, errUsage):
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "editor: %v\n", err)
		os.Exit(1)
	}
}
