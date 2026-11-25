package main

import (
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editorcmd"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo/core"
)

// editorCommand opens the scene editor on a game's project, as its directory or its game.yaml, which opens the project and its main scene, or on a YAML scene file, which inside a project opens that project too, or on a demo scene when nothing is named. A project's scenes draw its own atlases, which the asset browser lists, and its scene files place as instances. A project with an editor command of its own, which `game generate editor` writes, is opened by running that command with go run, so its component types are checked and its plugins built in. With -library it shows the library panel, which installs packs from that library into the open project. A second argument is an animation clip file the animation panel opens on the object named by the third argument, or on the scene's first object.
var editorCommand = &command{
	name:    "editor",
	args:    "[project-dir | game.yaml | scene.yaml] [clip.animation.yaml [object-id]]",
	summary: "Open the scene editor on a project or a YAML scene, and an animation clip on one of its objects, with the project's own editor command when it has one, or on a demo scene when none is named",
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
		if err := editorcmd.Open(ed, args); err != nil {
			return err
		}
		a.launch(ed)
		return nil
	}
}

// gameEditorTag is the build tag that builds a project's package as its editor command, as `game generate editor` writes it.
const gameEditorTag = "gameeditor"

// editorcmdPath is the import path of package editorcmd, whose command line game editor runs a project's editor command with.
const editorcmdPath = "github.com/nsgo-dev/game/packages/game/editor/editorcmd"

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

// newDemoEditor returns an editor on a small demo room, what the editor opens when no scene is named, and what opens a scene outside any project, with the demo's two atlases and its body component.
func newDemoEditor() (*editor.Editor, error) {
	resolver := sceneformat.AtlasMap{
		"tiles": {
			Image: core.ImageSource{URI: "asset:tiles.png"},
			Frames: map[string]sprite.Frame{
				"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
		"actors": {
			Image: core.ImageSource{URI: "asset:actors.png"},
			Frames: map[string]sprite.Frame{
				"hero":  {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"torch": {Name: "torch", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
	}

	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Demo Room",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "floor",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  16,
					TileHeight: 16,
					Width:      8,
					Height:     5,
					Tileset:    "tiles",
					Data: []int{
						1, 1, 1, 1, 1, 1, 1, 1,
						1, 2, 2, 2, 2, 2, 2, 1,
						1, 2, 0, 0, 0, 0, 2, 1,
						1, 2, 2, 2, 2, 2, 2, 1,
						1, 1, 1, 1, 1, 1, 1, 1,
					},
				},
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:      "hero",
						Name:    "Hero",
						X:       64,
						Y:       40,
						OriginX: 8,
						OriginY: 8,
						Z:       5,
						Sprite:  &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
						Components: map[string]any{
							sceneformat.Body2DComponentName: map[string]any{
								"version":      sceneformat.PhysicsComponentVersion,
								"kind":         "dynamic",
								"mass":         1.0,
								"gravityScale": 0.0,
								"vx":           24.0,
							},
							sceneformat.Collider2DComponentName: map[string]any{
								"version": sceneformat.PhysicsComponentVersion,
								"shape":   "box",
								"width":   16.0,
								"height":  16.0,
							},
						},
						Properties: map[string]any{
							"hp":     10,
							"tag":    "player",
							"active": true,
						},
					},
				},
			},
		},
	}

	ed, err := editor.New(doc, resolver)
	if err != nil {
		return nil, err
	}
	if err := ed.SetComponentRegistry(demoComponents()); err != nil {
		return nil, err
	}
	ed.SetActiveLayer("actors")
	ed.SelectObject("hero")

	var elapsed float64
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, dt time.Duration) {
		elapsed += dt.Seconds()
		if hero := scene.Object("hero"); hero != nil && hero.Sprite != nil {
			dx := float32(math.Cos(elapsed*2) * 20 * dt.Seconds())
			hero.Sprite.X += dx
			hero.Doc.X = hero.Sprite.X
		}
	}
	return ed, nil
}

// demoComponents registers the built-in 2D and 3D physics component types the demo scene and opened files may use, so a misspelled field fails to open with its file and line and the inspector lists every field.
func demoComponents() *sceneformat.ComponentRegistry {
	return sceneformat.PhysicsComponentRegistry()
}
