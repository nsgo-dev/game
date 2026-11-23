package main

import (
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
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

// editorCommand opens the scene editor on a game's project, as its directory or its game.yaml, which opens the project and its main scene, or on a YAML scene file, or on a demo scene when nothing is named.
var editorCommand = &command{
	name:    "editor",
	args:    "[project-dir | game.yaml | scene.yaml]",
	summary: "Open the scene editor on a project or a YAML scene, or on a demo scene when none is named",
	define:  defineEditor,
}

func defineEditor(a *app, fs *flag.FlagSet) func() error {
	return func() error {
		if fs.NArg() > 1 {
			return usagef("expected at most one project or scene file, got %d", fs.NArg())
		}
		ed, err := newDemoEditor()
		if err != nil {
			return err
		}
		if fs.NArg() == 1 {
			if err := open(ed, fs.Arg(0)); err != nil {
				return fmt.Errorf("open %s: %w", fs.Arg(0), err)
			}
		}
		a.launch(ed)
		return nil
	}
}

// runEditor shows ed in the NSGo Scene Editor window and returns once it closes.
func runEditor(ed *editor.Editor) {
	nsgo.Run(func(cx *core.Scope) core.Node {
		cx.SetTitle("NSGo Scene Editor")
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(1120, 680))
}

// open opens path in ed: a project, as a directory or its game.yaml, or else a scene file.
func open(ed *editor.Editor, path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() || filepath.Base(path) == project.FileName {
		return ed.OpenProject(path)
	}
	return ed.OpenFile(path)
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
							"body": map[string]any{
								"solid": true,
								"speed": 48.0,
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

// demoComponents registers the component types the demo scene and opened files may use, so a misspelled field fails to open with its file and line and the inspector lists every field.
func demoComponents() *sceneformat.ComponentRegistry {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "body",
		Doc:  "A moving body that collides with the level.",
		Fields: []sceneformat.Field{
			{Name: "solid", Kind: sceneformat.FieldBool, Default: true, Doc: "Blocks other solid bodies."},
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 32.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(500), Doc: "Top speed in points per second."},
			{Name: "friction", Kind: sceneformat.FieldFloat, Default: 0.5, Min: sceneformat.Float64(0), Max: sceneformat.Float64(1), Doc: "Fraction of speed lost per second on the ground."},
			{Name: "layer", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31), Doc: "Collision layer."},
		},
	})
	return reg
}
