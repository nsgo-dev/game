package main

import (
	_ "embed"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
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

// Root boots the reference game: the menu's buttons come from scenes/menu.scene.yaml, Play switches to gameplay, and scene.Manager and mcp.Bridge expose the menu, gameplay and pause scenes.
func Root(cx *core.Scope) core.Node {
	menuDoc, err := sceneformat.Parse(menuSceneYAML)
	if err != nil {
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
	pauseScene.AddLayer(scene.NewLayer("pause_ui", scene.SpaceScreen, 100))
	pauseCtrl := &scene.Controller{Scene: pauseScene, Transparent: true}

	actions := map[string]func(){
		"play_button": func() { mgr.Switch(gameplayCtrl, nil) },
	}

	menuScene := scene.NewScene(nil)
	for _, layerDoc := range menuDoc.Layers {
		space := scene.SpaceWorld
		if layerDoc.EffectiveSpace() == sceneformat.SpaceScreen {
			space = scene.SpaceScreen
		}
		layer := scene.NewLayer(layerDoc.Name, space, layerDoc.Order)
		for _, obj := range layerDoc.Objects {
			if obj.Prefab != "button" {
				continue
			}
			x, y := obj.X, obj.Y
			w := propFloat(obj.Properties, "width", 120)
			h := propFloat(obj.Properties, "height", 36)
			btn := scene.NewDrawable(func(d *core.Drawing) {
				d.BeginPath().Rect(x, y, w, h).Fill()
			})
			btn.Hit = func(p core.Point) bool {
				px, py := float32(p.X), float32(p.Y)
				return px >= x && px < x+w && py >= y && py < y+h
			}
			if onPress := actions[obj.ID]; onPress != nil {
				btn.OnPress = func(scene.HitResult, core.PressEvent) { onPress() }
			}
			layer.AddDrawable(btn)
		}
		menuScene.AddLayer(layer)
	}
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

func propFloat(m map[string]any, key string, fallback float32) float32 {
	switch v := m[key].(type) {
	case int:
		return float32(v)
	case float64:
		return float32(v)
	default:
		return fallback
	}
}
