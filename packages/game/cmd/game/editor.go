package main

import (
	"flag"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

// editorCommand opens the scene editor on a YAML scene file, or on a demo scene when none is named.
var editorCommand = &command{
	name:    "editor",
	args:    "[scene.yaml]",
	summary: "Open the scene editor on a YAML scene, or on a demo scene when none is named",
	define:  defineEditor,
}

func defineEditor(a *app, fs *flag.FlagSet) func() error {
	return func() error {
		if fs.NArg() > 1 {
			return usagef("expected at most one scene file, got %d", fs.NArg())
		}
		ed, err := newDemoEditor()
		if err != nil {
			return err
		}
		if fs.NArg() == 1 {
			if err := ed.OpenFile(fs.Arg(0)); err != nil {
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
