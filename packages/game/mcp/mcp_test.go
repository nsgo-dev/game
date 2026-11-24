package mcp_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type posComp struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type velComp struct {
	VX float32 `json:"vx"`
	VY float32 `json:"vy"`
}

type healthComp struct {
	HP  int `json:"hp"`
	Max int `json:"max"`
}

type aiStateComp struct {
	Mode   string  `json:"mode"`
	Target float64 `json:"target"`
	OnTick func()  `json:"on_tick,omitempty"`
}

type cyclicComp struct {
	Label string      `json:"label"`
	Next  *cyclicComp `json:"next,omitempty"`
}

func callTool[T any](t *testing.T, app *nsgotest.App, cx *core.Scope, name string, args any) T {
	t.Helper()
	raw, err := callToolRaw(t, app, cx, name, args)
	if err != nil {
		t.Fatalf("%s(%v) failed: %v", name, args, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s unmarshal %s: %v", name, raw, err)
	}
	return out
}

func callToolRaw(t *testing.T, app *nsgotest.App, cx *core.Scope, name string, args any) (json.RawMessage, error) {
	t.Helper()
	var payload json.RawMessage
	switch a := args.(type) {
	case nil:
	case string:
		payload = json.RawMessage(a)
	case json.RawMessage:
		payload = a
	default:
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal args for %s: %v", name, err)
		}
		payload = b
	}
	var (
		res json.RawMessage
		err error
	)
	app.Post(func() {
		res, err = automation.Call(cx, t.Context(), name, payload)
	})
	app.Settle()
	return res, err
}

// TestExampleGameInspection mirrors a realistic multi-layer 2D game (like caverun)
// and verifies an agent can list scenes, layers and entities, advance simulation
// steps, and read updated component values using the same stable entity references.
func TestExampleGameInspection(t *testing.T) {
	var (
		cx     *core.Scope
		mgr    *scene.Manager
		world  *entities.World
		bridge *mcp.Bridge
		err    error

		player entities.Entity
		sword  entities.Entity
		slime  entities.Entity
		gem    entities.Entity
	)

	cam := &tilemap.Camera{X: 160, Y: 90, Width: 320, Height: 180}
	gameplayScene := scene.NewScene(cam)

	// Add layers out of draw order to verify draw-order sorting.
	hudLayer := scene.NewLayer("hud", scene.SpaceScreen, 100)
	hudLayer.AddDrawable(scene.NewDrawable(func(*core.Drawing) {}))
	gameplayScene.AddLayer(hudLayer)

	bgLayer := scene.NewParallaxLayer("backdrop", -10, 0.25, 0.25)
	bgLayer.RepeatX = true
	bgLayer.RepeatWidth = 320
	gameplayScene.AddLayer(bgLayer)

	worldLayer := scene.NewLayer("terrain", scene.SpaceWorld, 0)
	worldLayer.AddTilemap(tilemap.New(20, 12, 16, 16))
	gameplayScene.AddLayer(worldLayer)

	actorLayer := scene.NewLayer("actors", scene.SpaceWorld, 10)
	actorLayer.DepthSort = scene.SortY
	batch := &sprite.Batch{Sprites: []*sprite.Sprite{{X: 10, Y: 20}, {X: 50, Y: 60}, nil}}
	actorLayer.AddBatch(batch)
	actorLayer.AddDrawable(scene.NewDrawable(func(*core.Drawing) {}))
	gameplayScene.AddLayer(actorLayer)

	debugLayer := scene.NewLayer("debug_grid", scene.SpaceWorld, 50)
	debugLayer.Visible = false
	gameplayScene.AddLayer(debugLayer)

	pauseScene := scene.NewScene(nil)
	pauseScene.AddLayer(scene.NewLayer("pause_menu", scene.SpaceScreen, 0))

	titleScene := scene.NewScene(nil)
	titleScene.AddLayer(scene.NewLayer("title_bg", scene.SpaceScreen, 0))

	gameplayCtrl := &scene.Controller{Scene: gameplayScene}
	pauseCtrl := &scene.Controller{Scene: pauseScene, Transparent: true}
	sceneNames := map[*scene.Controller]string{
		gameplayCtrl: "gameplay",
		pauseCtrl:    "pause",
	}

	world = entities.NewWorld()
	if err := entities.RegisterCodec[posComp](world, "position"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[velComp](world, "velocity"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[healthComp](world, "health"); err != nil {
		t.Fatal(err)
	}

	player = world.Spawn()
	entities.Set(world, player, posComp{X: 32, Y: 64})
	entities.Set(world, player, velComp{VX: 60, VY: 0})
	entities.Set(world, player, healthComp{HP: 3, Max: 3})
	entities.Set(world, player, sceneformat.ObjectRef{ID: "explorer", Layer: "actors"})

	sword = world.Spawn()
	entities.Set(world, sword, sceneformat.ObjectRef{ID: "explorer_sword", Layer: "actors"})
	h := world.Hierarchy2D()
	h.SetLocal(player, entities.NewTransform2D(32, 64))
	h.SetLocal(sword, entities.NewTransform2D(12, 0))
	if err := h.SetParent(sword, player, false); err != nil {
		t.Fatal(err)
	}
	h.Sync()

	slime = world.Spawn()
	entities.Set(world, slime, posComp{X: 120, Y: 64})
	entities.Set(world, slime, velComp{VX: -30, VY: 0})
	entities.Set(world, slime, healthComp{HP: 2, Max: 2})
	entities.Set(world, slime, aiStateComp{Mode: "patrol", Target: 90})

	gem = world.Spawn()
	entities.Set(world, gem, posComp{X: 80, Y: 48})

	names := map[entities.Entity]string{
		slime: "slime_0",
		gem:   "gem_0",
	}
	tags := map[entities.Entity][]string{
		player: {"player"},
		slime:  {"enemy", "ground"},
		gem:    {"collectible"},
	}
	layers := map[entities.Entity]string{
		slime: "actors",
		gem:   "actors",
	}

	stepSim := func(dt time.Duration) {
		sec := float32(dt.Seconds())
		entities.Each2(world, func(e entities.Entity, p *posComp, v *velComp) bool {
			p.X += v.VX * sec
			p.Y += v.VY * sec
			if entities.Has[entities.Transform2D](world, e) {
				h.SetPosition(e, p.X, p.Y)
			}
			return true
		})
		h.Sync()
	}

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		world.Attach(cx)
		mgr = scene.NewManager(cx)
		mgr.Switch(gameplayCtrl, nil)
		mgr.Push(pauseCtrl)

		bridge, err = mcp.Attach(cx, mcp.Config{
			Scenes:      mgr,
			SceneName:   func(c *scene.Controller) string { return sceneNames[c] },
			NamedScenes: map[string]*scene.Scene{"title": titleScene},
			World:       world,
			EntityName:  func(e entities.Entity) string { return names[e] },
			EntityTags:  func(e entities.Entity) []string { return tags[e] },
			EntityLayer: func(e entities.Entity) string { return layers[e] },
		})
		if err == nil {
			err = mcp.RegisterComponent[aiStateComp](bridge, "ai")
		}
		return core.Canvas{Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}}
	})
	if err != nil {
		t.Fatalf("Attach/RegisterComponent failed: %v", err)
	}
	if !bridge.Attached() {
		t.Fatal("expected bridge to be attached")
	}

	// 1. Agent lists scenes: sees active stack [gameplay, pause] + inactive named scene "title".
	scenesRes := callTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
	if scenesRes.Current != "pause" || scenesRes.StackDepth != 2 {
		t.Fatalf("scenes = %+v, want current=pause stack_depth=2", scenesRes)
	}
	if len(scenesRes.Scenes) != 3 {
		t.Fatalf("len(scenes) = %d, want 3: %+v", len(scenesRes.Scenes), scenesRes.Scenes)
	}
	if scenesRes.Scenes[0].Name != "gameplay" || !scenesRes.Scenes[0].Active || scenesRes.Scenes[0].Current || scenesRes.Scenes[0].LayerCount != 5 {
		t.Fatalf("scenes[0] = %+v", scenesRes.Scenes[0])
	}
	if scenesRes.Scenes[0].Camera == nil || scenesRes.Scenes[0].Camera.X != 160 || scenesRes.Scenes[0].Camera.Zoom != 1 {
		t.Fatalf("gameplay camera = %+v, want x=160 zoom=1", scenesRes.Scenes[0].Camera)
	}
	if scenesRes.Scenes[1].Name != "pause" || !scenesRes.Scenes[1].Current || !scenesRes.Scenes[1].Transparent {
		t.Fatalf("scenes[1] = %+v", scenesRes.Scenes[1])
	}
	if scenesRes.Scenes[2].Name != "title" || scenesRes.Scenes[2].Active {
		t.Fatalf("scenes[2] = %+v", scenesRes.Scenes[2])
	}

	// 2. Agent lists layers of the inactive-top "gameplay" scene by name, in draw order.
	layersRes := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, map[string]any{"scene": "gameplay"})
	if layersRes.Scene != "gameplay" || len(layersRes.Layers) != 5 {
		t.Fatalf("layers = %+v", layersRes)
	}
	wantOrder := []string{"backdrop", "terrain", "actors", "debug_grid", "hud"}
	for i, want := range wantOrder {
		if layersRes.Layers[i].Name != want || layersRes.Layers[i].DrawIndex != i {
			t.Fatalf("layer[%d] = %+v, want name=%q draw_index=%d", i, layersRes.Layers[i], want, i)
		}
	}
	if layersRes.Layers[0].ParallaxX != 0.25 || !layersRes.Layers[0].RepeatX || layersRes.Layers[0].RepeatWidth != 320 {
		t.Fatalf("backdrop layer = %+v", layersRes.Layers[0])
	}
	if layersRes.Layers[1].TilemapCount != 1 {
		t.Fatalf("terrain layer = %+v", layersRes.Layers[1])
	}
	if layersRes.Layers[2].DepthSort != "y" || layersRes.Layers[2].BatchCount != 1 || layersRes.Layers[2].SpriteCount != 2 || layersRes.Layers[2].DrawableCount != 1 {
		t.Fatalf("actors layer = %+v", layersRes.Layers[2])
	}
	if layersRes.Layers[3].Visible {
		t.Fatalf("debug_grid should be hidden: %+v", layersRes.Layers[3])
	}
	if layersRes.Layers[4].Space != "screen" {
		t.Fatalf("hud space = %q, want screen", layersRes.Layers[4].Space)
	}

	// Filter out hidden layers.
	visLayers := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, map[string]any{
		"scene":          "gameplay",
		"include_hidden": false,
	})
	if len(visLayers.Layers) != 4 {
		t.Fatalf("visible layers count = %d, want 4", len(visLayers.Layers))
	}

	// Default scene (omitted) inspects the current top scene ("pause").
	curLayers := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, nil)
	if curLayers.Scene != "pause" || len(curLayers.Layers) != 1 || curLayers.Layers[0].Name != "pause_menu" {
		t.Fatalf("default layers = %+v", curLayers)
	}

	// 3. Agent lists entities and captures stable refs.
	entRes := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, nil)
	if entRes.Total != 4 || entRes.Returned != 4 {
		t.Fatalf("entities list = %+v, want 4", entRes)
	}
	playerEntry := entRes.Entities[0]
	if playerEntry.Ref != "entity:0:1" || playerEntry.Name != "explorer" || playerEntry.Layer != "actors" || playerEntry.ChildCount != 1 {
		t.Fatalf("player entry = %+v", playerEntry)
	}
	swordEntry := entRes.Entities[1]
	if swordEntry.Ref != "entity:1:1" || swordEntry.Parent != playerEntry.Ref || swordEntry.Name != "explorer_sword" {
		t.Fatalf("sword entry = %+v", swordEntry)
	}
	slimeEntry := entRes.Entities[2]
	if slimeEntry.Ref != "entity:2:1" || slimeEntry.Name != "slime_0" {
		t.Fatalf("slime entry = %+v", slimeEntry)
	}

	// 4. Advance simulation 60 steps (1 second), destroy gem, spawn a new entity reusing gem's slot.
	app.Post(func() {
		for range 60 {
			stepSim(time.Second / 60)
		}
		world.Destroy(gem)
		replacement := world.Spawn()
		entities.Set(world, replacement, posComp{X: 200, Y: 100})
		names[replacement] = "gem_respawned"
	})
	app.Settle()

	// Re-list entities: player and slime keep their exact same refs across steps!
	entRes2 := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, nil)
	if entRes2.Entities[0].Ref != playerEntry.Ref || entRes2.Entities[2].Ref != slimeEntry.Ref {
		t.Fatalf("refs drifted across steps: before=(%s,%s) after=(%s,%s)",
			playerEntry.Ref, slimeEntry.Ref, entRes2.Entities[0].Ref, entRes2.Entities[2].Ref)
	}
	// Recycled slot got generation 2 instead of aliasing generation 1.
	if entRes2.Entities[3].Index != gem.Index() || entRes2.Entities[3].Generation != 2 || entRes2.Entities[3].Ref != "entity:3:2" {
		t.Fatalf("recycled entity = %+v, want entity:3:2", entRes2.Entities[3])
	}

	// Stale ref for destroyed gem must fail cleanly even though slot 3 is occupied again!
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": mcp.FormatEntityRef(gem)}); err == nil || !strings.Contains(err.Error(), "not alive") {
		t.Fatalf("inspecting stale ref = %v, want not alive error", err)
	}

	// 5. Read player's updated position component using the original stable ref from before stepping.
	compRes := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       playerEntry.Ref,
		"component": "position",
	})
	var gotPos posComp
	if err := json.Unmarshal(compRes.Value, &gotPos); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(gotPos.X-92)) > 0.01 || gotPos.Y != 64 {
		t.Fatalf("player position after 1s = %+v, want (92, 64)", gotPos)
	}

	// 6. Inspect player entity: returns hierarchy children, codec components AND built-in transform2d/world_transform2d/object_ref.
	inspectRes := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": playerEntry.Ref})
	if len(inspectRes.Children) != 1 || inspectRes.Children[0] != swordEntry.Ref {
		t.Fatalf("player children = %v, want [%s]", inspectRes.Children, swordEntry.Ref)
	}
	for _, wantComp := range []string{"health", "object_ref", "position", "transform2d", "velocity", "world_transform2d"} {
		if _, ok := inspectRes.Components[wantComp]; !ok {
			t.Fatalf("player inspect missing component %q; got keys %v", wantComp, inspectRes.Components)
		}
	}

	// 7. Read bridge-registered transient component ("ai") on slime.
	aiRes := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       slimeEntry.Ref,
		"component": "ai",
	})
	var gotAI struct {
		Mode   string  `json:"mode"`
		Target float64 `json:"target"`
	}
	if err := json.Unmarshal(aiRes.Value, &gotAI); err != nil {
		t.Fatal(err)
	}
	if gotAI.Mode != "patrol" || gotAI.Target != 90 {
		t.Fatalf("slime ai = %+v, want patrol/90", gotAI)
	}
}

func TestEntitiesListFiltersAndPagination(t *testing.T) {
	w := entities.NewWorld()
	if err := entities.RegisterCodec[posComp](w, "position"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[healthComp](w, "health"); err != nil {
		t.Fatal(err)
	}
	a := w.Spawn()
	b := w.Spawn()
	c := w.Spawn()
	entities.Set(w, a, posComp{X: 1, Y: 2})
	entities.Set(w, b, posComp{X: 3, Y: 4})
	entities.Set(w, b, healthComp{HP: 10, Max: 10})
	entities.Set(w, c, healthComp{HP: 5, Max: 10})
	h := w.Hierarchy2D()
	if err := h.SetParent(b, a, false); err != nil {
		t.Fatal(err)
	}

	names := map[entities.Entity]string{a: "hero", b: "goblin_1", c: "goblin_2"}
	layers := map[entities.Entity]string{a: "actors", b: "actors", c: "bg"}
	tags := map[entities.Entity][]string{a: {"ally"}, b: {"enemy"}, c: {"enemy"}}

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			World:       w,
			EntityName:  func(e entities.Entity) string { return names[e] },
			EntityLayer: func(e entities.Entity) string { return layers[e] },
			EntityTags:  func(e entities.Entity) []string { return tags[e] },
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Filter by component + include_components.
	byComp := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{
		"component":          "health",
		"include_components": true,
	})
	if byComp.Total != 2 || byComp.Returned != 2 || byComp.Entities[0].Name != "goblin_1" || byComp.Entities[1].Name != "goblin_2" {
		t.Fatalf("filter by component = %+v", byComp)
	}
	if _, ok := byComp.Entities[0].Values["health"]; !ok {
		t.Fatalf("expected inline health value when include_components=true: %+v", byComp.Entities[0])
	}

	// Filter by name substring + layer + tag.
	byMeta := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{
		"name":  "goblin",
		"layer": "actors",
		"tag":   "enemy",
	})
	if byMeta.Total != 1 || byMeta.Entities[0].Ref != mcp.FormatEntityRef(b) {
		t.Fatalf("filter by meta = %+v", byMeta)
	}

	// Filter by parent ref and by "root".
	children := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{
		"parent": mcp.FormatEntityRef(a),
	})
	if children.Total != 1 || children.Entities[0].Ref != mcp.FormatEntityRef(b) {
		t.Fatalf("filter by parent = %+v", children)
	}
	roots := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{
		"parent": "root",
	})
	if roots.Total != 2 || roots.Entities[0].Ref != mcp.FormatEntityRef(a) || roots.Entities[1].Ref != mcp.FormatEntityRef(c) {
		t.Fatalf("filter by root = %+v", roots)
	}

	// Pagination: offset + limit.
	paged := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{
		"offset": 1,
		"limit":  1,
	})
	if paged.Total != 3 || paged.Returned != 1 || paged.Entities[0].Ref != mcp.FormatEntityRef(b) {
		t.Fatalf("pagination = %+v", paged)
	}

	// Negative offset / limit and invalid parent ref rejected.
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitiesList, map[string]any{"offset": -1}); err == nil {
		t.Fatal("expected error for negative offset")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitiesList, map[string]any{"limit": -2}); err == nil {
		t.Fatal("expected error for negative limit")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitiesList, map[string]any{"parent": "not_a_ref"}); err == nil {
		t.Fatal("expected error for invalid parent ref")
	}
}

func TestManagerTransitionAndSceneFuncFallback(t *testing.T) {
	sc := scene.NewScene(nil)
	sc.AddLayer(scene.NewLayer("fallback_layer", scene.SpaceWorld, 0))

	c1 := &scene.Controller{} // nil Scene, uses SceneFunc fallback
	c2 := &scene.Controller{}

	var (
		cx  *core.Scope
		mgr *scene.Manager
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		mgr = scene.NewManager(cx)
		mgr.Switch(c1, nil)
		if _, err := mcp.Attach(cx, mcp.Config{
			Scenes:    mgr,
			SceneFunc: func() *scene.Scene { return sc },
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Fallback scene name ("scene:0") and SceneFunc layers are surfaced when Controller.Scene is nil.
	sr := callTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
	if sr.Current != "scene:0" || sr.Scenes[0].LayerCount != 1 {
		t.Fatalf("fallback scenes = %+v", sr)
	}
	lr := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, nil)
	if lr.Scene != "scene:0" || len(lr.Layers) != 1 || lr.Layers[0].Name != "fallback_layer" {
		t.Fatalf("fallback layers = %+v", lr)
	}

	// Start a timed transition and verify Transitioning/Progress are reported.
	app.Post(func() {
		mgr.Switch(c2, scene.Fade(200*time.Millisecond, core.RGB(0x000000)))
	})
	app.Advance(100 * time.Millisecond)

	srTrans := callTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
	if !srTrans.Transitioning || srTrans.Progress <= 0 || srTrans.Progress >= 1 {
		t.Fatalf("mid-transition scenes = %+v, want transitioning=true with 0 < progress < 1", srTrans)
	}
}

func TestSafeComponentEncodingAndCustomReader(t *testing.T) {
	w := entities.NewWorld()
	// Register Transform2D under a custom codec name to verify built-in deduplication.
	if err := entities.RegisterCodec[entities.Transform2D](w, "custom_xform"); err != nil {
		t.Fatal(err)
	}
	e := w.Spawn()
	entities.Set(w, e, entities.NewTransform2D(5, 6))
	entities.Set(w, e, aiStateComp{
		Mode:   "broken",
		Target: math.NaN(),
		OnTick: func() {},
	})
	node := &cyclicComp{Label: "loop"}
	node.Next = node
	entities.Set(w, e, node)

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		b := mcp.New(mcp.Config{World: w})
		if err := mcp.RegisterComponent[aiStateComp](b, "ai"); err != nil {
			t.Fatal(err)
		}
		if err := mcp.RegisterComponent[*cyclicComp](b, "cyclic"); err != nil {
			t.Fatal(err)
		}
		if err := mcp.RegisterComponentFunc(b, "computed", func(_ *entities.World, ent entities.Entity) (any, bool, error) {
			if ent != e {
				return nil, false, nil
			}
			return map[string]any{"pos_inf": math.Inf(1), "neg_inf": math.Inf(-1)}, true, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := b.Attach(cx); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	inspected := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{
		"ref": mcp.FormatEntityRef(e),
	})
	// Built-in "transform2d" must NOT duplicate "custom_xform".
	if _, hasCustom := inspected.Components["custom_xform"]; !hasCustom {
		t.Fatalf("missing custom_xform: %v", inspected.Components)
	}
	if _, hasDup := inspected.Components["transform2d"]; hasDup {
		t.Fatalf("transform2d should be deduplicated when registered as codec: %v", inspected.Components)
	}

	var ai map[string]any
	if err := json.Unmarshal(inspected.Components["ai"], &ai); err != nil {
		t.Fatal(err)
	}
	if ai["target"] != "NaN" || ai["on_tick"] != "<func>" {
		t.Fatalf("sanitized ai = %v, want target=NaN on_tick=<func>", ai)
	}

	var cyc map[string]any
	if err := json.Unmarshal(inspected.Components["cyclic"], &cyc); err != nil {
		t.Fatal(err)
	}
	if cyc["label"] != "loop" || cyc["next"] != "<cycle>" {
		t.Fatalf("sanitized cyclic = %v, want next=<cycle>", cyc)
	}

	var comp map[string]any
	if err := json.Unmarshal(inspected.Components["computed"], &comp); err != nil {
		t.Fatal(err)
	}
	if comp["pos_inf"] != "+Inf" || comp["neg_inf"] != "-Inf" {
		t.Fatalf("sanitized computed = %v, want +Inf/-Inf", comp)
	}
}

func TestDynamicWorldSceneAndLifecycle(t *testing.T) {
	sc1 := scene.NewScene(nil)
	sc1.AddLayer(scene.NewLayer("first", scene.SpaceWorld, 0))
	sc2 := scene.NewScene(nil)
	sc2.AddLayer(scene.NewLayer("second", scene.SpaceScreen, 0))

	w1 := entities.NewWorld()
	e1 := w1.Spawn()
	w2 := entities.NewWorld()
	e2 := w2.Spawn()
	entities.Set(w2, e2, posComp{X: 9, Y: 8})
	_ = entities.RegisterCodec[posComp](w2, "pos")

	curScene := sc1
	curWorld := w1

	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{
			SceneFunc:   func() *scene.Scene { return curScene },
			WorldFunc:   func() *entities.World { return curWorld },
			NamedScenes: map[string]*scene.Scene{"level1": sc1, "level2": sc2},
		})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Standalone scene matches NamedScenes key "level1".
	sr := callTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
	if sr.Current != "level1" || len(sr.Scenes) != 2 {
		t.Fatalf("scenes = %+v", sr)
	}
	lr := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, nil)
	if lr.Scene != "level1" || len(lr.Layers) != 1 || lr.Layers[0].Name != "first" {
		t.Fatalf("layers1 = %+v", lr)
	}

	// Swap active scene and world dynamically.
	curScene = sc2
	curWorld = w2

	lr2 := callTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, nil)
	if lr2.Scene != "level2" || len(lr2.Layers) != 1 || lr2.Layers[0].Name != "second" {
		t.Fatalf("layers2 = %+v", lr2)
	}
	er := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, nil)
	if er.Total != 1 || er.Entities[0].Ref != mcp.FormatEntityRef(e2) || len(er.Entities[0].Components) != 1 || er.Entities[0].Components[0] != "pos" {
		t.Fatalf("entities2 = %+v (w1 had %v)", er, e1)
	}

	// Attaching a second bridge to the same graph fails without leaking partial registrations.
	var dupErr, reattachErr error
	app.Post(func() {
		reattachErr = bridge.Attach(cx)
		_, dupErr = mcp.Attach(cx, mcp.Config{})
	})
	app.Settle()
	if !errors.Is(reattachErr, mcp.ErrAlreadyAttached) {
		t.Fatalf("reattach = %v, want ErrAlreadyAttached", reattachErr)
	}
	if dupErr == nil {
		t.Fatal("expected duplicate Attach on same graph to fail")
	}

	// Detach removes every tool cleanly, and the bridge can be attached again afterwards.
	app.Post(bridge.Detach)
	app.Settle()
	if bridge.Attached() {
		t.Fatal("expected Detached bridge")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolScenesList, nil); err == nil {
		t.Fatal("expected tool call after Detach to fail")
	}
	app.Post(func() {
		if err := bridge.Attach(cx); err != nil {
			t.Fatalf("re-attach after Detach: %v", err)
		}
	})
	app.Settle()
	_ = callTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
}

func TestErrorCasesAndRefParsing(t *testing.T) {
	// ParseEntityRef edge cases.
	for _, bad := range []string{"", "0:1", "entity:", "entity:0", "entity:0:0", "entity:0:1:2", "entity:-1:1", "entity:abc:1", "entity:1:xyz"} {
		if _, err := mcp.ParseEntityRef(bad); err == nil {
			t.Errorf("ParseEntityRef(%q) succeeded, want error", bad)
		}
	}
	if mcp.FormatEntityRef(entities.Zero) != "" {
		t.Fatal("FormatEntityRef(Zero) should be empty")
	}
	parsed, err := mcp.ParseEntityRef("entity:7:3")
	if err != nil || parsed.Index() != 7 || parsed.Generation() != 3 || mcp.FormatEntityRef(parsed) != "entity:7:3" {
		t.Fatalf("ParseEntityRef round-trip = (%v, %v)", parsed, err)
	}

	// Empty config errors and unknown-field rejection.
	var cx *core.Scope
	var b *mcp.Bridge
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		b, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Unknown argument field rejected.
	if _, err := callToolRaw(t, app, cx, mcp.ToolScenesList, `{"bogus":true}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown arg = %v, want unknown field error", err)
	}
	// No active scene.
	if _, err := callToolRaw(t, app, cx, mcp.ToolLayersList, nil); err == nil || !strings.Contains(err.Error(), "no active scene") {
		t.Fatalf("layers with no scene = %v", err)
	}
	// Unknown scene name.
	if _, err := callToolRaw(t, app, cx, mcp.ToolLayersList, map[string]any{"scene": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("layers unknown scene = %v", err)
	}
	// No entity world configured.
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitiesList, nil); err == nil || !strings.Contains(err.Error(), "no entity world") {
		t.Fatalf("entities with no world = %v", err)
	}

	// Attach a disposed world and verify clean error.
	w := entities.NewWorld()
	e := w.Spawn()
	b.SetWorld(w)
	// Missing component on live entity reports clear error.
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       mcp.FormatEntityRef(e),
		"component": "missing",
	}); err == nil || !strings.Contains(err.Error(), "does not have component") {
		t.Fatalf("missing component = %v", err)
	}
	// Missing required args.
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntityInspect, `{}`); err == nil || !strings.Contains(err.Error(), "ref is required") {
		t.Fatalf("missing ref = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentGet, map[string]any{"ref": mcp.FormatEntityRef(e)}); err == nil || !strings.Contains(err.Error(), "component is required") {
		t.Fatalf("missing component arg = %v", err)
	}

	w.Dispose()
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitiesList, nil); err == nil || !strings.Contains(err.Error(), "disposed") {
		t.Fatalf("disposed world = %v", err)
	}

	// Registration validation errors.
	if err := mcp.RegisterComponent[posComp](nil, "pos"); !errors.Is(err, mcp.ErrNilBridge) {
		t.Fatalf("nil bridge register = %v", err)
	}
	if err := mcp.RegisterComponent[posComp](b, ""); !errors.Is(err, mcp.ErrEmptyComponentName) {
		t.Fatalf("empty name register = %v", err)
	}
	if err := mcp.RegisterComponent[posComp](b, "pos"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.RegisterComponent[posComp](b, "pos"); !errors.Is(err, mcp.ErrDuplicateComponent) {
		t.Fatalf("dup register = %v", err)
	}
}
