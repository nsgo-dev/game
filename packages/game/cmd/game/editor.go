package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

// editorCommand opens the scene editor on a game's project, as its directory or its game.yaml, which opens the project and its main scene, or on a YAML scene file, or on a demo scene when nothing is named. With -library it shows the library panel, which installs packs from that library into the open project. A second argument is an animation clip file the animation panel opens on the object named by the third argument, or on the scene's first object.
var editorCommand = &command{
	name:    "editor",
	args:    "[project-dir | game.yaml | scene.yaml] [clip.animation.yaml [object-id]]",
	summary: "Open the scene editor on a project or a YAML scene, and an animation clip on one of its objects, or on a demo scene when none is named",
	define:  defineEditor,
	complete: map[string]completion{
		"library": filePaths,
		"":        filePaths,
	},
}

func defineEditor(a *app, fs *flag.FlagSet) func() error {
	lib := fs.String("library", "", "show the library panel, which installs packs into the open project, for this library index: an https URL such as "+library.DefaultIndex+", an index file, or a directory holding index.yaml")
	return func() error {
		if fs.NArg() > 3 {
			return usagef("expected at most a project or scene file, a clip file and an object ID, got %d arguments", fs.NArg())
		}
		ed, err := newDemoEditor()
		if err != nil {
			return err
		}
		if *lib != "" {
			if ed.Library, err = library.Open(context.Background(), *lib); err != nil {
				return err
			}
		}
		if err := openArgs(ed, fs.Args()); err != nil {
			return err
		}
		a.launch(ed)
		return nil
	}
}

// runEditor shows ed in the NSGo Scene Editor window, serving its MCP server on the open project's endpoint for go tool game mcp, and returns once it closes.
func runEditor(ed *editor.Editor) {
	nsgo.Run(func(cx *core.Scope) core.Node {
		serveEditor(cx, ed, os.Stderr)
		// EditorView titles the window after the open scene.
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(1120, 680))
}

// open opens path in ed: a project, as a directory or its game.yaml, or else a scene file. It first points ed's resolver at the project's files, so layered tilemaps, YAML tilesets and Tiled and LDtk sources build and the tile palette lists their tiles.
func open(ed *editor.Editor, path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() || filepath.Base(path) == project.FileName {
		root := path
		if err != nil || !info.IsDir() {
			root = filepath.Dir(path)
		}
		ed.Resolver = projectAssets(ed.Resolver, root)
		return ed.OpenProject(path)
	}
	ed.Resolver = projectAssets(ed.Resolver, sceneProjectRoot(path))
	return ed.OpenFile(path)
}

// projectAssets returns a resolver that reads the project files under root and keeps the sprite atlases of resolver, the demo atlases the editor starts with.
func projectAssets(resolver sceneformat.AssetResolver, root string) sceneformat.FSAssets {
	var atlases sceneformat.AtlasMap
	switch r := resolver.(type) {
	case sceneformat.AtlasMap:
		atlases = r
	case sceneformat.FSAssets:
		atlases = r.Atlases
	}
	return sceneformat.FSAssets{FS: os.DirFS(root), Atlases: atlases}
}

// sceneProjectRoot returns the root of the project the scene file at path belongs to: the nearest directory above it that holds game.yaml, or the scene's own directory when none does. Scenes name the files they use by their paths from that root.
func sceneProjectRoot(path string) string {
	start := filepath.Dir(path)
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	for dir := start; ; {
		if _, err := os.Stat(filepath.Join(dir, project.FileName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// openArgs opens what the command line names: the project or scene args[0], then the animation clip file args[1] on the object args[2], or on the scene's first object when args[2] is absent. That object is selected, so when the clip file does not exist yet the toolbar's Animate button starts a clip on it, and the panel's Save Clip button writes the clip to args[1].
func openArgs(ed *editor.Editor, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if err := open(ed, args[0]); err != nil {
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

// newDemoEditor returns an editor on a small demo room, what the editor opens when no scene is named.
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
