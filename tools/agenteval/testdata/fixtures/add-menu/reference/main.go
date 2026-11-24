package main

import (
	_ "embed"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed scenes/menu.scene.yaml
var menuSceneYAML []byte

//go:embed scenes/gameplay.scene.yaml
var gameplaySceneYAML []byte

func main() {
	nsgo.Run(Root)
}

// Root boots the reference game with title, gameplay, and pause menu controllers wired through scene.Manager and mcp.Bridge.
func Root(cx *core.Scope) core.Node {
	if _, err := sceneformat.Parse(menuSceneYAML); err != nil {
		panic(err)
	}
	if _, err := sceneformat.Parse(gameplaySceneYAML); err != nil {
		panic(err)
	}

	mgr := scene.NewManager(cx)

	gameplayScene := scene.NewScene(nil)
	gameplayScene.AddLayer(scene.NewLayer("arena", scene.SpaceWorld, 0))
	gameplayCtrl := &scene.Controller{Scene: gameplayScene}

	pauseScene := scene.NewScene(nil)
	pauseLayer := scene.NewLayer("pause_ui", scene.SpaceScreen, 100)
	pauseScene.AddLayer(pauseLayer)
	pauseCtrl := &scene.Controller{Scene: pauseScene, Transparent: true}

	menuRoot := gameui.NewRoot(&gameui.Panel{
		Child: &gameui.VBox{
			Spacing: 12,
			Children: []gameui.Widget{
				&gameui.Label{Text: "Menu Eval"},
				&gameui.Button{
					Text: "Play",
					OnClick: func() {
						mgr.Switch(gameplayCtrl, nil)
					},
				},
				&gameui.Button{
					Text: "Pause",
					OnClick: func() {
						mgr.Push(pauseCtrl)
					},
				},
			},
		},
	})
	menuBounds := core.Bounds{Size: core.Size{Width: 480, Height: 320}}
	menuRoot.Layout(menuBounds)

	menuScene := scene.NewScene(nil)
	menuLayer := scene.NewLayer("menu_ui", scene.SpaceScreen, 100)
	menuLayer.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
		menuRoot.Draw(d, menuBounds)
	}))
	menuScene.AddLayer(menuLayer)
	menuCtrl := &scene.Controller{Scene: menuScene}

	mgr.Push(menuCtrl)

	sceneNames := map[*scene.Controller]string{
		menuCtrl:     "menu",
		gameplayCtrl: "gameplay",
		pauseCtrl:    "pause",
	}

	world := entities.NewWorld()
	world.Attach(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: func(dt time.Duration) { mgr.Update(dt) },
		Draw:   func(float64) {},
	}
	loop.Run(cx)

	if _, err := mcp.Attach(cx, mcp.Config{
		Scenes: mgr,
		SceneName: func(c *scene.Controller) string {
			return sceneNames[c]
		},
		NamedScenes: map[string]*scene.Scene{
			"gameplay": gameplayScene,
			"pause":    pauseScene,
		},
		World: world,
		Loop:  loop,
	}); err != nil {
		panic(err)
	}
	return core.View{}
}
