package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/statedump"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestStateDumpToolAndDirectCallTool(t *testing.T) {
	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)

	world := entities.NewWorld()
	t.Cleanup(world.Dispose)
	entities.RegisterCodec[posComp](world, "pos")
	entities.RegisterCodec[healthComp](world, "health")

	hero := world.Spawn()
	entities.Set(world, hero, posComp{X: 120, Y: 48})
	entities.Set(world, hero, healthComp{HP: 80, Max: 100})

	gameplay := scene.NewScene(nil)
	gameplay.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	ctrl := &scene.Controller{Scene: gameplay}

	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) {},
	}
	loop.StepNow(12)

	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		mgr := scene.NewManager(s)
		mgr.Push(ctrl)
		var err error
		bridge, err = mcp.Attach(s, mcp.Config{
			Scenes:    mgr,
			SceneName: func(c *scene.Controller) string { return "dungeon_01" },
			World:     world,
			Loop:      loop,
			StateValues: func() map[string]any {
				return map[string]any{
					"score": 250,
					"level": "floor_1",
				}
			},
		})
		if err != nil {
			t.Fatalf("mcp.Attach: %v", err)
		}
		return core.View{}
	})

	// 1. Call via automation.Call.
	res := callTool[mcp.StateDumpResult](t, app, cx, mcp.ToolStateDump, nil)
	if res.Version != statedump.FormatVersion {
		t.Fatalf("version = %d, want %d", res.Version, statedump.FormatVersion)
	}
	if res.Rate != 60 || res.Steps != 12 {
		t.Fatalf("rate=%d steps=%d, want 60 / 12", res.Rate, res.Steps)
	}
	if res.Scene == nil || res.Scene.Current != "dungeon_01" || res.Scene.Depth != 1 {
		t.Fatalf("scene = %+v, want dungeon_01 depth 1", res.Scene)
	}
	if len(res.Entities) != 1 || len(res.Entities[0].Components) != 2 {
		t.Fatalf("entities = %+v, want 1 entity with 2 components", res.Entities)
	}
	if string(res.Values["score"]) != "250" {
		t.Fatalf("values[score] = %s, want 250", res.Values["score"])
	}
	parsed, err := statedump.Parse([]byte(res.YAML))
	if err != nil {
		t.Fatalf("statedump.Parse returned YAML failed: %v\n%s", err, res.YAML)
	}
	if parsed.Steps != 12 || parsed.Rate != 60 || len(parsed.Entities) != 1 {
		t.Fatalf("parsed YAML dump mismatch: %+v", parsed)
	}

	// 2. Direct Bridge.CallTool works for both game_state_dump and unknown/error cases.
	var (
		directRaw json.RawMessage
		directErr error
	)
	app.Post(func() {
		directRaw, directErr = bridge.CallTool(context.Background(), mcp.ToolStateDump, nil)
	})
	app.Settle()
	if directErr != nil {
		t.Fatalf("bridge.CallTool(game_state_dump): %v", directErr)
	}
	var direct mcp.StateDumpResult
	if err := json.Unmarshal(directRaw, &direct); err != nil {
		t.Fatalf("unmarshal direct CallTool result: %v", err)
	}
	if direct.YAML != res.YAML {
		t.Fatalf("direct CallTool YAML mismatch:\ngot:\n%s\nwant:\n%s", direct.YAML, res.YAML)
	}

	var unknownErr error
	app.Post(func() {
		_, unknownErr = bridge.CallTool(context.Background(), "no_such_tool", nil)
	})
	app.Settle()
	if unknownErr == nil || !strings.Contains(unknownErr.Error(), "unknown tool") {
		t.Fatalf("unknown tool err = %v, want unknown tool error", unknownErr)
	}

	var nilBridge *mcp.Bridge
	if _, err := nilBridge.CallTool(context.Background(), mcp.ToolStateDump, nil); !errors.Is(err, mcp.ErrNilBridge) {
		t.Fatalf("nil bridge CallTool err = %v, want ErrNilBridge", err)
	}
}

func TestStateDumpStandaloneScene(t *testing.T) {
	sc := scene.NewScene(nil)
	sc.AddLayer(scene.NewLayer("main", scene.SpaceWorld, 0))
	b := mcp.New(mcp.Config{Scene: sc})
	raw, err := b.CallTool(context.Background(), mcp.ToolStateDump, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var res mcp.StateDumpResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Scene == nil || res.Scene.Current != "main" || res.Scene.Depth != 1 {
		t.Fatalf("standalone scene state = %+v, want current=main depth=1", res.Scene)
	}
}
