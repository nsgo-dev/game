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
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type armorComp struct {
	Points int            `json:"points"`
	Kind   string         `json:"kind,omitempty"`
	Mods   map[string]int `json:"mods,omitempty"`
}

type aiHandle struct{ id int }

// aiComp mixes JSON fields with runtime-only state: an exported callback (which makes json.Marshal fail), a json:"-" pointer, and an unexported cache.
type aiComp struct {
	State   string         `json:"state"`
	Mods    map[string]int `json:"mods,omitempty"`
	OnEnter func()
	Path    *aiHandle `json:"-"`
	cache   *aiHandle
}

type nanFloatComp struct {
	V float64
}

func (n *nanFloatComp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil && s == "make_nan" {
		n.V = math.NaN()
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	n.V = f
	return nil
}

// TestSpawnEditStepAndDestroyLifecycle exercises the full MCP mutation workflow: listing prefabs, spawning prefabs (custom, entities.Prefab, sceneformat.SceneDoc, and empty), editing components so changes take effect on the very next simulation step, removing components, and destroying entities with every hierarchy policy.
func TestSpawnEditStepAndDestroyLifecycle(t *testing.T) {
	w := entities.NewWorld()
	if err := entities.RegisterCodec[posComp](w, "position"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[velComp](w, "velocity"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[healthComp](w, "health"); err != nil {
		t.Fatal(err)
	}
	h := w.Hierarchy2D()

	names := make(map[entities.Entity]string)
	tags := make(map[entities.Entity][]string)

	// Entity prefab template using game/entities.Prefab.
	coinPrefab := entities.With(
		entities.With(entities.NewPrefab("coin"), posComp{X: 0, Y: 0}),
		velComp{VX: 0, VY: 0},
	)

	// Scene prefab template using game/sceneformat.SceneDoc with a parent-child subtree.
	turretDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "turret_prefab",
		Layers: []sceneformat.LayerDoc{
			{
				Name: "actors",
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "turret_base",
						X:  10,
						Y:  20,
						Children: []sceneformat.ObjectDoc{
							{ID: "turret_barrel", X: 5, Y: 0},
						},
					},
				},
			},
		},
	}

	stepSim := func(dt time.Duration) {
		sec := float32(dt.Seconds())
		entities.Each2(w, func(e entities.Entity, p *posComp, v *velComp) bool {
			p.X += v.VX * sec
			p.Y += v.VY * sec
			if entities.Has[entities.Transform2D](w, e) {
				h.SetPosition(e, p.X, p.Y)
			}
			return true
		})
		h.Sync()
	}

	var (
		cx     *core.Scope
		bridge *mcp.Bridge
		err    error
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		w.Attach(cx)
		bridge = mcp.New(mcp.Config{
			World:      w,
			EntityName: func(e entities.Entity) string { return names[e] },
			EntityTags: func(e entities.Entity) []string { return tags[e] },
			Prefabs: map[string]mcp.PrefabSpawner{
				"slime": func(world *entities.World, overrides map[string]any) (entities.Entity, error) {
					hp := 10
					if rawHP, ok := overrides["hp"]; ok {
						f, ok := rawHP.(float64)
						if !ok {
							return entities.Zero, errors.New("slime hp override must be a number")
						}
						hp = int(f)
					}
					e := world.Spawn()
					entities.Set(world, e, posComp{X: 0, Y: 0})
					entities.Set(world, e, velComp{VX: 10, VY: 0})
					entities.Set(world, e, healthComp{HP: hp, Max: hp})
					entities.Set(world, e, entities.NewTransform2D(0, 0))
					names[e] = "slime"
					tags[e] = []string{"enemy"}
					return e, nil
				},
			},
		})
		if err = bridge.RegisterEntityPrefab(coinPrefab); err != nil {
			return core.View{}
		}
		if err = bridge.RegisterScenePrefab("turret", turretDoc); err != nil {
			return core.View{}
		}
		if err = mcp.RegisterMutableComponent[armorComp](bridge, "armor"); err != nil {
			return core.View{}
		}
		if err = mcp.RegisterComponentEditor[healthComp](bridge, "health", func(world *entities.World, e entities.Entity, v healthComp) error {
			if v.HP < 0 {
				return errors.New("hp cannot be negative")
			}
			if v.Max > 0 && v.HP > v.Max {
				v.HP = v.Max
			}
			entities.Set(world, e, v)
			return nil
		}); err != nil {
			return core.View{}
		}
		err = bridge.Attach(cx)
		return core.View{}
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// 1. Agent lists registered prefabs in deterministic sorted order.
	prefabsRes := callTool[mcp.PrefabsListResult](t, app, cx, mcp.ToolPrefabsList, nil)
	if len(prefabsRes.Prefabs) != 3 {
		t.Fatalf("prefabs = %+v, want 3", prefabsRes)
	}
	wantPrefabs := []mcp.PrefabInfo{
		{Name: "coin", Kind: "entity_prefab"},
		{Name: "slime", Kind: "custom"},
		{Name: "turret", Kind: "scene"},
	}
	for i, want := range wantPrefabs {
		if prefabsRes.Prefabs[i] != want {
			t.Fatalf("prefabs[%d] = %+v, want %+v", i, prefabsRes.Prefabs[i], want)
		}
	}

	// 2. Spawn custom prefab "slime" with overrides, x/y position, and component patches.
	slimeSpawn := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"prefab":    "slime",
		"overrides": map[string]any{"hp": 25},
		"x":         40,
		"y":         60,
		"components": map[string]any{
			"position": map[string]any{"x": 40, "y": 60},
			"armor":    map[string]any{"points": 5, "kind": "gel", "mods": map[string]int{"cold": 2}},
		},
	})
	if slimeSpawn.Ref == "" || slimeSpawn.Prefab != "slime" || slimeSpawn.Name != "slime" {
		t.Fatalf("slime spawn = %+v", slimeSpawn)
	}
	var spawnXform entities.Transform2D
	if err := json.Unmarshal(slimeSpawn.Components["transform2d"], &spawnXform); err != nil {
		t.Fatal(err)
	}
	if spawnXform.X != 40 || spawnXform.Y != 60 || spawnXform.ScaleX != 1 || spawnXform.ScaleY != 1 {
		t.Fatalf("slime transform2d = %+v, want (40,60) scale (1,1)", spawnXform)
	}
	var spawnHP healthComp
	if err := json.Unmarshal(slimeSpawn.Components["health"], &spawnHP); err != nil {
		t.Fatal(err)
	}
	if spawnHP.HP != 25 || spawnHP.Max != 25 {
		t.Fatalf("slime health = %+v, want 25/25", spawnHP)
	}

	// Spawned entity is immediately visible in game_entities_list and game_entity_inspect.
	listed := callTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{"name": "slime"})
	if listed.Total != 1 || listed.Entities[0].Ref != slimeSpawn.Ref {
		t.Fatalf("entities list after spawn = %+v", listed)
	}
	inspected := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": slimeSpawn.Ref})
	if inspected.Ref != slimeSpawn.Ref || len(inspected.Components) < 5 {
		t.Fatalf("inspect spawned slime = %+v", inspected)
	}

	// 3. Edit velocity and armor (patch mode) and verify the edit takes effect on the VERY NEXT step!
	velSet := callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "velocity",
		"value":     map[string]any{"vx": 120}, // vy omitted -> preserved (0)
	})
	var gotVel velComp
	if err := json.Unmarshal(velSet.Value, &gotVel); err != nil {
		t.Fatal(err)
	}
	if gotVel.VX != 120 || gotVel.VY != 0 {
		t.Fatalf("set velocity = %+v, want (120, 0)", gotVel)
	}

	// Patch armor: update points only, keeping kind="gel" and mods.
	armorSet := callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "armor",
		"value":     map[string]any{"points": 9},
	})
	var gotArmor armorComp
	if err := json.Unmarshal(armorSet.Value, &gotArmor); err != nil {
		t.Fatal(err)
	}
	if gotArmor.Points != 9 || gotArmor.Kind != "gel" || gotArmor.Mods["cold"] != 2 {
		t.Fatalf("patched armor = %+v, want points=9 kind=gel mods={cold:2}", gotArmor)
	}

	// Replace armor completely (replace=true resets omitted fields).
	armorReplace := callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "armor",
		"value":     map[string]any{"points": 3},
		"replace":   true,
	})
	gotArmor = armorComp{}
	if err := json.Unmarshal(armorReplace.Value, &gotArmor); err != nil {
		t.Fatal(err)
	}
	if gotArmor.Points != 3 || gotArmor.Kind != "" || len(gotArmor.Mods) != 0 {
		t.Fatalf("replaced armor = %+v, want points=3 empty kind/mods", gotArmor)
	}

	// Custom RegisterComponentEditor clamps HP to Max and rejects negative HP.
	clampedHP := callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "health",
		"value":     map[string]any{"hp": 999},
	})
	var gotHP healthComp
	if err := json.Unmarshal(clampedHP.Value, &gotHP); err != nil {
		t.Fatal(err)
	}
	if gotHP.HP != 25 || gotHP.Max != 25 {
		t.Fatalf("clamped health = %+v, want 25/25", gotHP)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "health",
		"value":     map[string]any{"hp": -5},
	}); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("negative hp = %v, want editor error", err)
	}

	// Advance ONE simulation step (0.5s) and verify edited velocity took effect!
	app.Post(func() { stepSim(500 * time.Millisecond) })
	app.Settle()

	posAfter := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "position",
	})
	var gotPos posComp
	if err := json.Unmarshal(posAfter.Value, &gotPos); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(gotPos.X-100)) > 0.01 || gotPos.Y != 60 {
		t.Fatalf("position after 1 step at vx=120 = %+v, want (100, 60)", gotPos)
	}

	// 4. Spawn sceneformat prefab "turret" (root + child) at (100, 50) and verify Hierarchy2D propagation!
	turretSpawn := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"prefab": "turret",
		"x":      100,
		"y":      50,
	})
	if turretSpawn.Name != "turret_base" || turretSpawn.Layer != "actors" || len(turretSpawn.Children) != 1 {
		t.Fatalf("turret spawn = %+v", turretSpawn)
	}
	barrelRef := turretSpawn.Children[0]
	app.Post(func() { stepSim(time.Second / 60) })
	app.Settle()

	barrelWT := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       barrelRef,
		"component": "world_transform2d",
	})
	var wt entities.WorldTransform2D
	if err := json.Unmarshal(barrelWT.Value, &wt); err != nil {
		t.Fatal(err)
	}
	if wt.X != 105 || wt.Y != 50 {
		t.Fatalf("barrel world transform = (%v, %v), want (105, 50)", wt.X, wt.Y)
	}

	// Edit turret root's built-in transform2d via game_component_set -> child's world transform updates next step!
	_ = callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       turretSpawn.Ref,
		"component": "transform2d",
		"value":     map[string]any{"x": 200, "y": 80},
	})
	app.Post(func() { stepSim(time.Second / 60) })
	app.Settle()

	barrelWT2 := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       barrelRef,
		"component": "world_transform2d",
	})
	if err := json.Unmarshal(barrelWT2.Value, &wt); err != nil {
		t.Fatal(err)
	}
	if wt.X != 205 || wt.Y != 80 {
		t.Fatalf("barrel world transform after parent edit = (%v, %v), want (205, 80)", wt.X, wt.Y)
	}

	// 5. Spawn entity prefab "coin" parented under turret root, and an empty entity.
	coinSpawn := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"prefab": "coin",
		"parent": turretSpawn.Ref,
		"x":      2,
		"y":      3,
	})
	if coinSpawn.Parent != turretSpawn.Ref {
		t.Fatalf("coin parent = %q, want %q", coinSpawn.Parent, turretSpawn.Ref)
	}
	emptySpawn := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"x": 7,
		"y": 8,
		"components": map[string]any{
			"object_ref": map[string]any{"ID": "marker_1", "Layer": "ui"},
		},
	})
	if emptySpawn.Name != "marker_1" || emptySpawn.Layer != "ui" {
		t.Fatalf("empty spawn with object_ref = %+v", emptySpawn)
	}

	// 6. Remove components (bridge-registered, codec, built-in) and verify idempotency.
	remArmor := callTool[mcp.ComponentRemoveResult](t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "armor",
	})
	if !remArmor.Removed {
		t.Fatalf("remove armor = %+v, want removed=true", remArmor)
	}
	remArmorAgain := callTool[mcp.ComponentRemoveResult](t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "armor",
	})
	if remArmorAgain.Removed {
		t.Fatalf("second remove armor = %+v, want removed=false", remArmorAgain)
	}
	remVel := callTool[mcp.ComponentRemoveResult](t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       slimeSpawn.Ref,
		"component": "velocity",
	})
	if !remVel.Removed {
		t.Fatalf("remove velocity = %+v, want removed=true", remVel)
	}
	remXform := callTool[mcp.ComponentRemoveResult](t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       emptySpawn.Ref,
		"component": "transform2d",
	})
	if !remXform.Removed {
		t.Fatalf("remove transform2d = %+v, want removed=true", remXform)
	}

	// 7. Destroy policies: cascade, orphan, keep_world, reparent!
	// 7a. Cascade on turret destroys turret + barrel + coin (3 entities).
	cascRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{
		"ref":    turretSpawn.Ref,
		"policy": "cascade",
	})
	if cascRes.Destroyed != 3 || cascRes.Policy != "cascade" {
		t.Fatalf("cascade destroy = %+v, want destroyed=3", cascRes)
	}
	// Stale refs for parent and child must fail cleanly on inspect/edit/destroy.
	for _, stale := range []string{turretSpawn.Ref, barrelRef, coinSpawn.Ref} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": stale}); err == nil || !strings.Contains(err.Error(), "not alive") {
			t.Fatalf("inspect stale %s = %v, want not alive", stale, err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{"ref": stale, "component": "position", "value": map[string]any{"x": 1}}); err == nil || !strings.Contains(err.Error(), "not alive") {
			t.Fatalf("set stale %s = %v, want not alive", stale, err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolEntityDestroy, map[string]any{"ref": stale}); err == nil || !strings.Contains(err.Error(), "not alive") {
			t.Fatalf("destroy stale %s = %v, want not alive", stale, err)
		}
	}

	// 7b. Reparent policy: grandparent -> mid -> leaf; destroy mid with reparent -> leaf becomes child of grandparent and keeps its world position (10 + 20 + 5 = 35), so its local X under the grandparent is 25.
	gp := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"x": 10, "y": 10})
	mid := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"parent": gp.Ref, "x": 20, "y": 0})
	leaf := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"parent": mid.Ref, "x": 5, "y": 0})
	app.Post(func() { h.Sync() })
	app.Settle()
	repRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{
		"ref":    mid.Ref,
		"policy": "reparent",
	})
	if repRes.Destroyed != 1 || repRes.Policy != "reparent" {
		t.Fatalf("reparent destroy = %+v", repRes)
	}
	leafAfterReparent := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": leaf.Ref})
	if leafAfterReparent.Parent != gp.Ref {
		t.Fatalf("leaf parent after reparent = %q, want %q", leafAfterReparent.Parent, gp.Ref)
	}
	var leafXform entities.Transform2D
	if err := json.Unmarshal(leafAfterReparent.Components["transform2d"], &leafXform); err != nil {
		t.Fatal(err)
	}
	if leafXform.X != 25 || leafXform.Y != 0 {
		t.Fatalf("leaf local transform after reparent = (%v, %v), want (25, 0)", leafXform.X, leafXform.Y)
	}

	// 7c. keep_world policy: sync transforms, destroy gp with keep_world -> leaf becomes root with local X = 10 + 25 = 35.
	app.Post(func() { h.Sync() })
	app.Settle()
	kwRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{
		"ref":    gp.Ref,
		"policy": "keep_world",
	})
	if kwRes.Destroyed != 1 || kwRes.Policy != "keep_world" {
		t.Fatalf("keep_world destroy = %+v", kwRes)
	}
	leafAfterKW := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": leaf.Ref})
	if leafAfterKW.Parent != "" {
		t.Fatalf("leaf parent after keep_world = %q, want root", leafAfterKW.Parent)
	}
	if err := json.Unmarshal(leafAfterKW.Components["transform2d"], &leafXform); err != nil {
		t.Fatal(err)
	}
	if leafXform.X != 35 || leafXform.Y != 10 {
		t.Fatalf("leaf local transform after keep_world = (%v, %v), want (35, 10)", leafXform.X, leafXform.Y)
	}

	// 7d. orphan policy: parent -> child; destroy parent with orphan -> child becomes root keeping local transform (3, 4).
	pOrph := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"x": 100, "y": 100})
	cOrph := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"parent": pOrph.Ref, "x": 3, "y": 4})
	orphRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{
		"ref":    pOrph.Ref,
		"policy": "orphan",
	})
	if orphRes.Destroyed != 1 || orphRes.Policy != "orphan" {
		t.Fatalf("orphan destroy = %+v", orphRes)
	}
	cAfterOrph := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": cOrph.Ref})
	if cAfterOrph.Parent != "" {
		t.Fatalf("child parent after orphan = %q, want root", cAfterOrph.Parent)
	}
	if err := json.Unmarshal(cAfterOrph.Components["transform2d"], &leafXform); err != nil {
		t.Fatal(err)
	}
	if leafXform.X != 3 || leafXform.Y != 4 {
		t.Fatalf("child local transform after orphan = (%v, %v), want (3, 4)", leafXform.X, leafXform.Y)
	}

	// 7e. Default destroy of a parent reports the hierarchy's default policy (orphan), and of a childless leaf reports "none"; a recycled slot rejects the old ref.
	pDef := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, nil)
	cDef := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{"parent": pDef.Ref})
	parentDefRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{"ref": pDef.Ref})
	if parentDefRes.Destroyed != 1 || parentDefRes.Policy != "orphan" {
		t.Fatalf("default parent destroy = %+v, want orphan", parentDefRes)
	}
	callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{"ref": cDef.Ref})
	defRes := callTool[mcp.EntityDestroyResult](t, app, cx, mcp.ToolEntityDestroy, map[string]any{"ref": leaf.Ref})
	if defRes.Destroyed != 1 || defRes.Policy != "none" {
		t.Fatalf("default destroy = %+v, want none", defRes)
	}
	reused := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, nil)
	if reused.Ref == leaf.Ref {
		t.Fatalf("reused ref %q must not alias destroyed ref %q", reused.Ref, leaf.Ref)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       leaf.Ref,
		"component": "transform2d",
		"value":     map[string]any{"x": 1},
	}); err == nil || !strings.Contains(err.Error(), "not alive") {
		t.Fatalf("edit recycled stale ref = %v, want not alive", err)
	}
}

func TestEditValidationAtomicityAndErrorCases(t *testing.T) {
	var (
		cx *core.Scope
		b  *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		b, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Nil world errors across all mutation tools (no panic).
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, nil); err == nil || !strings.Contains(err.Error(), "no entity world") {
		t.Fatalf("spawn with nil world = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntityDestroy, map[string]any{"ref": "entity:0:1"}); err == nil || !strings.Contains(err.Error(), "no entity world") {
		t.Fatalf("destroy with nil world = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{"ref": "entity:0:1", "component": "transform2d", "value": map[string]any{"x": 1}}); err == nil || !strings.Contains(err.Error(), "no entity world") {
		t.Fatalf("set with nil world = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentRemove, map[string]any{"ref": "entity:0:1", "component": "transform2d"}); err == nil || !strings.Contains(err.Error(), "no entity world") {
		t.Fatalf("remove with nil world = %v", err)
	}
	// game_prefabs_list works even with no world and returns empty slice.
	emptyPrefabs := callTool[mcp.PrefabsListResult](t, app, cx, mcp.ToolPrefabsList, nil)
	if len(emptyPrefabs.Prefabs) != 0 {
		t.Fatalf("empty prefabs = %+v", emptyPrefabs)
	}

	// Unknown argument fields rejected on every edit tool.
	for _, tool := range []string{mcp.ToolPrefabsList, mcp.ToolEntitySpawn, mcp.ToolEntityDestroy, mcp.ToolComponentSet, mcp.ToolComponentRemove} {
		if _, err := callToolRaw(t, app, cx, tool, `{"bogus":1}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s unknown arg = %v, want unknown field", tool, err)
		}
	}

	// Wire up a live world and test validation & atomicity.
	w := entities.NewWorld()
	if err := entities.RegisterCodec[healthComp](w, "health"); err != nil {
		t.Fatal(err)
	}
	b.SetWorld(w)
	if err := mcp.RegisterComponent[posComp](b, "readonly_pos"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.RegisterMutableComponent[armorComp](b, "armor"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.RegisterMutableComponent[nanFloatComp](b, "nan_float"); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterEntityPrefab(entities.With(entities.NewPrefab("crate"), healthComp{HP: 5, Max: 5})); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterPrefab("bad_spawner", func(*entities.World, map[string]any) (entities.Entity, error) {
		return entities.Zero, nil
	}); err != nil {
		t.Fatal(err)
	}

	// Spawn rollback: invalid component or unknown field must NOT leak an entity into w!
	before := w.Count()
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"prefab": "crate",
		"components": map[string]any{
			"health": map[string]any{"hp": 1, "unknown_field": 2},
		},
	}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("spawn with unknown component field = %v", err)
	}
	if w.Count() != before {
		t.Fatalf("failed spawn leaked entity: count=%d, want %d", w.Count(), before)
	}

	// Unregistered prefab, overrides without prefab, overrides on entity prefab, dead parent, overflow x, bad spawner.
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"prefab": "ghost"}); err == nil || !strings.Contains(err.Error(), "unknown prefab") {
		t.Fatalf("unknown prefab = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"overrides": map[string]any{"a": 1}}); err == nil || !strings.Contains(err.Error(), "overrides require a prefab") {
		t.Fatalf("overrides without prefab = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"prefab": "crate", "overrides": map[string]any{"a": 1}}); err == nil || !strings.Contains(err.Error(), "does not accept overrides") {
		t.Fatalf("overrides on entity prefab = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"parent": "entity:99:1"}); err == nil || !strings.Contains(err.Error(), "not alive") {
		t.Fatalf("dead parent = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"x": 1e300}); err == nil || !strings.Contains(err.Error(), "finite float32") {
		t.Fatalf("overflow x = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, map[string]any{"prefab": "bad_spawner"}); err == nil || !strings.Contains(err.Error(), "non-live") {
		t.Fatalf("bad spawner = %v", err)
	}
	if w.Count() != before {
		t.Fatalf("error spawns leaked entities: count=%d, want %d", w.Count(), before)
	}

	// Spawn a valid entity to test component set/remove/destroy errors.
	ent := callTool[mcp.EntitySpawnResult](t, app, cx, mcp.ToolEntitySpawn, map[string]any{
		"components": map[string]any{
			"armor": map[string]any{"points": 4, "mods": map[string]int{"acid": 1}},
		},
	})

	// Failed patch on armor must leave existing map untouched.
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "armor",
		"value":     map[string]any{"mods": map[string]int{"poison": 9}, "nope": 1},
	}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field on mutable component = %v", err)
	}
	gotArmorRes := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       ent.Ref,
		"component": "armor",
	})
	var gotArmor armorComp
	if err := json.Unmarshal(gotArmorRes.Value, &gotArmor); err != nil {
		t.Fatal(err)
	}
	if _, leaked := gotArmor.Mods["poison"]; leaked || gotArmor.Mods["acid"] != 1 {
		t.Fatalf("failed patch corrupted armor map: %+v", gotArmor.Mods)
	}

	// Patching one field keeps the callback, json:"-" pointer and unexported cache, and a failed or rejected patch leaves the live map unchanged.
	if err := mcp.RegisterMutableComponent[aiComp](b, "ai"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.RegisterComponentEditor[aiComp](b, "ai_strict", func(*entities.World, entities.Entity, aiComp) error {
		return errors.New("rejected by editor")
	}); err != nil {
		t.Fatal(err)
	}
	aiEnt, err := mcp.ParseEntityRef(ent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	path, cache := &aiHandle{id: 1}, &aiHandle{id: 2}
	called := false
	app.Post(func() {
		entities.Set(w, aiEnt, aiComp{State: "idle", Mods: map[string]int{"a": 1}, OnEnter: func() { called = true }, Path: path, cache: cache})
	})
	app.Settle()
	callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{"ref": ent.Ref, "component": "ai", "value": map[string]any{"state": "chase"}})
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{"ref": ent.Ref, "component": "ai", "value": map[string]any{"mods": map[string]int{"b": 2}, "bogus": 1}}); err == nil {
		t.Fatal("patch with an unknown trailing field succeeded")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{"ref": ent.Ref, "component": "ai_strict", "value": map[string]any{"mods": map[string]int{"c": 3}}}); err == nil || !strings.Contains(err.Error(), "rejected by editor") {
		t.Fatalf("editor rejection = %v", err)
	}
	var gotAI aiComp
	app.Post(func() {
		if v, ok := entities.Get[aiComp](w, aiEnt); ok {
			gotAI = *v
		}
	})
	app.Settle()
	if gotAI.State != "chase" || gotAI.Path != path || gotAI.cache != cache || gotAI.OnEnter == nil {
		t.Fatalf("patch lost runtime fields: %+v", gotAI)
	}
	if len(gotAI.Mods) != 1 || gotAI.Mods["a"] != 1 {
		t.Fatalf("failed or rejected patch mutated the live map: %v", gotAI.Mods)
	}
	gotAI.OnEnter()
	if !called {
		t.Fatal("patched component lost its callback")
	}

	// Reject NaN produced by custom unmarshaler, overflowing float32 in transform2d, null for struct, read-only & derived components, unknown component, missing required args, invalid policy.
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "nan_float",
		"value":     "make_nan",
	}); err == nil || !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("nan float = %v, want non-finite error", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "transform2d",
		"value":     map[string]any{"x": 1e300},
	}); err == nil {
		t.Fatal("expected error for float32 overflow in transform2d")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "transform2d",
		"value":     nil,
	}); err == nil || !strings.Contains(err.Error(), "null") {
		t.Fatalf("null transform2d = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "world_transform2d",
		"value":     map[string]any{"X": 1},
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("edit world_transform2d = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       ent.Ref,
		"component": "world_transform2d",
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("remove world_transform2d = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "readonly_pos",
		"value":     map[string]any{"x": 1},
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("edit readonly component = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentRemove, map[string]any{
		"ref":       ent.Ref,
		"component": "readonly_pos",
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("remove readonly component = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "no_such_comp",
		"value":     map[string]any{},
	}); err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("unknown component = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "health",
	}); err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Fatalf("missing value = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntityDestroy, map[string]any{
		"ref":    ent.Ref,
		"policy": "explode",
	}); err == nil || !strings.Contains(err.Error(), "invalid destroy policy") {
		t.Fatalf("invalid policy = %v", err)
	}

	// Upgrading a read-only RegisterComponent[posComp] via RegisterComponentEditor[posComp] works, while a mismatched type or duplicate editor is rejected!
	if err := mcp.RegisterComponentEditor[velComp](b, "readonly_pos", func(*entities.World, entities.Entity, velComp) error { return nil }); !errors.Is(err, mcp.ErrDuplicateComponent) {
		t.Fatalf("mismatched editor type = %v, want ErrDuplicateComponent", err)
	}
	if err := mcp.RegisterComponentEditor[posComp](b, "readonly_pos", func(world *entities.World, e entities.Entity, v posComp) error {
		entities.Set(world, e, v)
		return nil
	}); err != nil {
		t.Fatalf("upgrade readonly_pos to editable = %v", err)
	}
	if err := mcp.RegisterComponentEditor[posComp](b, "readonly_pos", func(*entities.World, entities.Entity, posComp) error { return nil }); !errors.Is(err, mcp.ErrDuplicateComponent) {
		t.Fatalf("duplicate editor = %v, want ErrDuplicateComponent", err)
	}
	posSet := callTool[mcp.ComponentSetResult](t, app, cx, mcp.ToolComponentSet, map[string]any{
		"ref":       ent.Ref,
		"component": "readonly_pos",
		"value":     map[string]any{"x": 12, "y": 34},
	})
	var gotPos posComp
	if err := json.Unmarshal(posSet.Value, &gotPos); err != nil {
		t.Fatal(err)
	}
	if gotPos.X != 12 || gotPos.Y != 34 {
		t.Fatalf("upgraded readonly_pos = %+v, want (12, 34)", gotPos)
	}

	// Registration validation errors.
	var nilBridge *mcp.Bridge
	if err := nilBridge.RegisterPrefab("a", func(*entities.World, map[string]any) (entities.Entity, error) { return entities.Zero, nil }); !errors.Is(err, mcp.ErrNilBridge) {
		t.Fatalf("nil bridge RegisterPrefab = %v", err)
	}
	if err := b.RegisterPrefab("", func(*entities.World, map[string]any) (entities.Entity, error) { return entities.Zero, nil }); !errors.Is(err, mcp.ErrEmptyPrefabName) {
		t.Fatalf("empty RegisterPrefab = %v", err)
	}
	if err := b.RegisterPrefab("x", nil); err == nil {
		t.Fatal("expected error for nil prefab spawner")
	}
	if err := b.RegisterPrefab("crate", func(*entities.World, map[string]any) (entities.Entity, error) { return entities.Zero, nil }); !errors.Is(err, mcp.ErrDuplicatePrefab) {
		t.Fatalf("dup prefab = %v, want ErrDuplicatePrefab", err)
	}
	if err := b.RegisterEntityPrefab(nil); err == nil {
		t.Fatal("expected error for nil entity prefab")
	}
	if err := b.RegisterEntityPrefab(entities.NewPrefab("")); !errors.Is(err, mcp.ErrEmptyPrefabName) {
		t.Fatalf("empty entity prefab = %v", err)
	}
	if err := b.RegisterScenePrefab("empty_scene", &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Name: "empty"}); err == nil {
		t.Fatal("expected error for scene prefab with no objects")
	}
	twoRoots := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "two_roots",
		Layers: []sceneformat.LayerDoc{
			{Name: "a", Objects: []sceneformat.ObjectDoc{{ID: "one"}}},
			{Name: "b", Objects: []sceneformat.ObjectDoc{{ID: "two"}}},
		},
	}
	if err := b.RegisterScenePrefab("two_roots", twoRoots); err == nil || !strings.Contains(err.Error(), "2 top-level objects") {
		t.Fatalf("scene prefab with two top-level objects = %v, want refusal", err)
	}
	if err := mcp.RegisterComponentFunc(b, "func_pos", func(*entities.World, entities.Entity) (any, bool, error) { return posComp{}, true, nil }); err != nil {
		t.Fatal(err)
	}
	if err := mcp.RegisterComponentEditor[posComp](b, "func_pos", func(*entities.World, entities.Entity, posComp) error { return nil }); !errors.Is(err, mcp.ErrDuplicateComponent) {
		t.Fatalf("editor over RegisterComponentFunc = %v, want ErrDuplicateComponent", err)
	}
	if err := mcp.RegisterMutableComponent[posComp](nil, "p"); !errors.Is(err, mcp.ErrNilBridge) {
		t.Fatalf("nil bridge RegisterMutableComponent = %v", err)
	}
	if err := mcp.RegisterComponentEditor[posComp](b, "e", nil); err == nil {
		t.Fatal("expected error for nil editor func")
	}

	// Disposed world error on mutation tools.
	w.Dispose()
	if _, err := callToolRaw(t, app, cx, mcp.ToolEntitySpawn, nil); err == nil || !strings.Contains(err.Error(), "disposed") {
		t.Fatalf("spawn on disposed world = %v", err)
	}
}
