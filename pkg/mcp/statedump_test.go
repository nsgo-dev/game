package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/input"
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

// TestStateCaptureStepWithInputDiffShowsOnlyMovedEntity covers the core workflow:
// capture -> press input -> step loop -> capture -> diff shows only the moved entity.
func TestStateCaptureStepWithInputDiffShowsOnlyMovedEntity(t *testing.T) {
	var cx *core.Scope

	world := entities.NewWorld()
	t.Cleanup(world.Dispose)
	if err := entities.RegisterCodec[posComp](world, "pos"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[healthComp](world, "health"); err != nil {
		t.Fatal(err)
	}

	hero := world.Spawn()
	entities.Set(world, hero, posComp{X: 10, Y: 20})
	entities.Set(world, hero, healthComp{HP: 100, Max: 100})

	statue := world.Spawn()
	entities.Set(world, statue, posComp{X: 200, Y: 20})
	entities.Set(world, statue, healthComp{HP: 50, Max: 50})

	actions := input.NewActions()
	actions.Bind("move_right")

	score := 100
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			actions.Step()
			if actions.Pressed("move_right") {
				if p, ok := entities.Get[posComp](world, hero); ok {
					p.X += 60 * float32(dt.Seconds())
				}
			}
		},
	}
	loop.SetPaused(true)

	sc := scene.NewScene(nil)
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		if _, err := mcp.Attach(s, mcp.Config{
			Scene:   sc,
			World:   world,
			Loop:    loop,
			Actions: actions,
			StateValues: func() map[string]any {
				return map[string]any{
					"score": score,
					"room":  "hall",
				}
			},
		}); err != nil {
			t.Fatalf("mcp.Attach: %v", err)
		}
		return core.View{}
	})

	heroRef := mcp.FormatEntityRef(hero)
	statueRef := mcp.FormatEntityRef(statue)

	cap1 := callTool[mcp.StateCaptureResult](t, app, cx, mcp.ToolStateCapture, map[string]any{
		"name": "before",
	})
	if cap1.Name != "before" || cap1.EntityCount != 2 || cap1.StoredCount != 1 || cap1.Bytes == 0 || cap1.YAML != "" {
		t.Fatalf("cap1 = %+v", cap1)
	}
	if !slices.Equal(cap1.ValueKeys, []string{"room", "score"}) {
		t.Fatalf("value_keys = %v, want [room score]", cap1.ValueKeys)
	}

	// Identical diff before moving reports equal=true and empty diff.
	sameDiff := callTool[mcp.StateDiffResult](t, app, cx, mcp.ToolStateDiff, map[string]any{
		"from": "before",
	})
	if !sameDiff.Equal || sameDiff.Diff != "" || len(sameDiff.ChangedEntities) != 0 {
		t.Fatalf("sameDiff = %+v, want equal=true with empty diff", sameDiff)
	}

	// Hold move_right and step 10 frames (10 pixels to the right at 60 px/s).
	_ = callTool[mcp.InputActionResult](t, app, cx, mcp.ToolInputAction, map[string]any{
		"name": "move_right",
		"mode": "press",
	})
	_ = callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{
		"count": 10,
	})

	cap2 := callTool[mcp.StateCaptureResult](t, app, cx, mcp.ToolStateCapture, map[string]any{
		"name": "after",
	})
	if cap2.Steps != 10 || cap2.StoredCount != 2 {
		t.Fatalf("cap2 = %+v", cap2)
	}

	diff := callTool[mcp.StateDiffResult](t, app, cx, mcp.ToolStateDiff, map[string]any{
		"from": "before",
		"to":   "after",
	})
	if diff.Equal {
		t.Fatal("diff.Equal = true, want false after hero moved")
	}
	if diff.StepsDelta != 10 {
		t.Fatalf("steps_delta = %d, want 10", diff.StepsDelta)
	}
	if !slices.Equal(diff.ChangedEntities, []string{heroRef}) {
		t.Fatalf("changed_entities = %v, want only [%s]", diff.ChangedEntities, heroRef)
	}
	if !slices.Equal(diff.ModifiedEntities, []string{heroRef}) || len(diff.AddedEntities) != 0 || len(diff.RemovedEntities) != 0 {
		t.Fatalf("entity buckets: added=%v removed=%v modified=%v", diff.AddedEntities, diff.RemovedEntities, diff.ModifiedEntities)
	}
	if len(diff.EntityChanges) != 1 || diff.EntityChanges[0].Ref != heroRef {
		t.Fatalf("entity_changes = %+v, want single change for %s", diff.EntityChanges, heroRef)
	}
	ec := diff.EntityChanges[0]
	if ec.Kind != "modified" || !slices.Equal(ec.ChangedComponents, []string{"pos"}) {
		t.Fatalf("hero change = %+v", ec)
	}
	if len(ec.FieldChanges) != 1 || ec.FieldChanges[0].Path != "pos.x" || string(ec.FieldChanges[0].Before) != "10" || string(ec.FieldChanges[0].After) != "20" {
		t.Fatalf("field_changes = %+v, want pos.x 10 -> 20", ec.FieldChanges)
	}
	if !strings.Contains(diff.Diff, "-          x: 10") || !strings.Contains(diff.Diff, "+          x: 20") {
		t.Fatalf("unified YAML diff missing x change:\n%s", diff.Diff)
	}
	if strings.Contains(diff.Diff, "200") {
		t.Fatalf("unified YAML diff unexpectedly mentioned unchanged statue:\n%s", diff.Diff)
	}

	// Filtering to the unchanged statue yields equal=true and no diff lines.
	statueDiff := callTool[mcp.StateDiffResult](t, app, cx, mcp.ToolStateDiff, map[string]any{
		"from":  "before",
		"to":    "after",
		"paths": []string{statueRef},
	})
	if !statueDiff.Equal || statueDiff.Diff != "" || len(statueDiff.ChangedEntities) != 0 {
		t.Fatalf("statueDiff = %+v, want equal=true and empty diff", statueDiff)
	}

	// Filtering to entities only excludes steps from YAML diff and still shows only hero.
	entOnlyDiff := callTool[mcp.StateDiffResult](t, app, cx, mcp.ToolStateDiff, map[string]any{
		"from":  "before",
		"to":    "after",
		"paths": []string{"entities"},
	})
	if entOnlyDiff.Equal || strings.Contains(entOnlyDiff.Diff, "steps:") || !slices.Equal(entOnlyDiff.ChangedEntities, []string{heroRef}) {
		t.Fatalf("entOnlyDiff = %+v", entOnlyDiff)
	}
}

func TestStateCaptureDiffAddRemoveValuesPathsAndBoundedEviction(t *testing.T) {
	world := entities.NewWorld()
	t.Cleanup(world.Dispose)
	if err := entities.RegisterCodec[posComp](world, "pos"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[healthComp](world, "health"); err != nil {
		t.Fatal(err)
	}

	parent := world.Spawn()
	entities.Set(world, parent, posComp{X: 1, Y: 2})
	child := world.Spawn()
	entities.Set(world, child, posComp{X: 3, Y: 4})
	entities.Set(world, child, healthComp{HP: 10, Max: 10})
	if err := world.Hierarchy2D().SetParent(child, parent, false); err != nil {
		t.Fatal(err)
	}

	values := map[string]any{
		"score": 10,
		"meta":  map[string]any{"wave": 1, "zone": "alpha"},
		"temp":  "skip-me",
	}
	b := mcp.New(mcp.Config{
		World: world,
		StateValues: func() map[string]any {
			return values
		},
	})

	callDirect := func(tool string, args any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := b.CallTool(context.Background(), tool, payload)
		if err != nil {
			t.Fatalf("%s(%s): %v", tool, payload, err)
		}
		return raw
	}

	// Capture only selected values keys, including one missing key.
	rawCap := callDirect(mcp.ToolStateCapture, map[string]any{
		"name":         "v1",
		"values":       []string{"score", "meta", "nonexistent"},
		"include_yaml": true,
	})
	var cap1 mcp.StateCaptureResult
	if err := json.Unmarshal(rawCap, &cap1); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cap1.ValueKeys, []string{"meta", "score"}) {
		t.Fatalf("value_keys = %v, want [meta score]", cap1.ValueKeys)
	}
	if !slices.Equal(cap1.MissingValues, []string{"nonexistent"}) {
		t.Fatalf("missing_values = %v, want [nonexistent]", cap1.MissingValues)
	}
	if cap1.YAML == "" || strings.Contains(cap1.YAML, "skip-me") {
		t.Fatalf("unexpected YAML:\n%s", cap1.YAML)
	}

	// Mutate: remove child, spawn enemy, change parent component, change nested value.
	world.Destroy(child)
	enemy := world.Spawn()
	entities.Set(world, enemy, posComp{X: 50, Y: 60})
	entities.Set(world, parent, healthComp{HP: 99, Max: 100})
	values["score"] = 25
	values["meta"] = map[string]any{"wave": 2, "zone": "alpha"}

	// Diff against live state (omitting "to" reuses v1's value filter).
	rawDiff := callDirect(mcp.ToolStateDiff, map[string]any{"from": "v1"})
	var diff mcp.StateDiffResult
	if err := json.Unmarshal(rawDiff, &diff); err != nil {
		t.Fatal(err)
	}
	if diff.To != "<live>" || diff.Equal {
		t.Fatalf("diff header = %+v", diff)
	}
	parentRef := mcp.FormatEntityRef(parent)
	childRef := mcp.FormatEntityRef(child)
	enemyRef := mcp.FormatEntityRef(enemy)
	if !slices.Equal(diff.AddedEntities, []string{enemyRef}) {
		t.Fatalf("added = %v, want [%s]", diff.AddedEntities, enemyRef)
	}
	if !slices.Equal(diff.RemovedEntities, []string{childRef}) {
		t.Fatalf("removed = %v, want [%s]", diff.RemovedEntities, childRef)
	}
	if !slices.Equal(diff.ModifiedEntities, []string{parentRef}) {
		t.Fatalf("modified = %v, want [%s]", diff.ModifiedEntities, parentRef)
	}
	if len(diff.ValueChanges) != 2 {
		t.Fatalf("value_changes = %+v, want 2 changes (meta.wave and score)", diff.ValueChanges)
	}
	if diff.ValueChanges[0].Path != "values.meta.wave" || diff.ValueChanges[1].Path != "values.score" {
		t.Fatalf("value_changes paths = %+v", diff.ValueChanges)
	}

	// Path filter to a single nested value field.
	rawPathDiff := callDirect(mcp.ToolStateDiff, map[string]any{
		"from":  "v1",
		"paths": []string{"values.meta.wave"},
	})
	var pathDiff mcp.StateDiffResult
	if err := json.Unmarshal(rawPathDiff, &pathDiff); err != nil {
		t.Fatal(err)
	}
	if pathDiff.Equal || len(pathDiff.ChangedEntities) != 0 || len(pathDiff.ValueChanges) != 1 || pathDiff.ValueChanges[0].Path != "values.meta.wave" {
		t.Fatalf("pathDiff = %+v", pathDiff)
	}

	// Path filter to an unchanged nested field reports equal=true.
	rawZoneDiff := callDirect(mcp.ToolStateDiff, map[string]any{
		"from":  "v1",
		"paths": []string{"values.meta.zone"},
	})
	var zoneDiff mcp.StateDiffResult
	if err := json.Unmarshal(rawZoneDiff, &zoneDiff); err != nil {
		t.Fatal(err)
	}
	if !zoneDiff.Equal || zoneDiff.Diff != "" {
		t.Fatalf("zoneDiff = %+v, want equal=true", zoneDiff)
	}

	// Bounded capture eviction: exceeding MaxStateCaptures evicts oldest FIFO entries.
	for i := range mcp.MaxStateCaptures + 3 {
		name := fmt.Sprintf("snap-%02d", i)
		raw := callDirect(mcp.ToolStateCapture, map[string]any{"name": name})
		var res mcp.StateCaptureResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		if res.StoredCount > mcp.MaxStateCaptures {
			t.Fatalf("stored_count = %d, exceeds MaxStateCaptures=%d", res.StoredCount, mcp.MaxStateCaptures)
		}
	}
	// Original "v1" should have been evicted.
	if _, err := b.CallTool(context.Background(), mcp.ToolStateDiff, json.RawMessage(`{"from":"v1"}`)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected evicted capture error, got %v", err)
	}

	// Concurrent captures/diffs are race-free.
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 10 {
				name := fmt.Sprintf("c-%d-%d", id, j)
				payload, _ := json.Marshal(map[string]any{"name": name})
				if _, err := b.CallTool(context.Background(), mcp.ToolStateCapture, payload); err != nil {
					t.Errorf("concurrent capture: %v", err)
				}
				diffPayload, _ := json.Marshal(map[string]any{"from": name, "to": name})
				if _, err := b.CallTool(context.Background(), mcp.ToolStateDiff, diffPayload); err != nil {
					t.Errorf("concurrent diff: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestStateCaptureAndDiffValidationErrors(t *testing.T) {
	b := mcp.New(mcp.Config{})
	cases := []struct {
		tool string
		args string
		want string
	}{
		{mcp.ToolStateCapture, `{}`, "name is required"},
		{mcp.ToolStateCapture, `{"name":" bad "}`, "whitespace"},
		{mcp.ToolStateCapture, `{"name":"ok","values":[""]}`, "non-empty"},
		{mcp.ToolStateCapture, `{"name":"ok","unknown":1}`, "unknown field"},
		{mcp.ToolStateDiff, `{}`, "name is required"},
		{mcp.ToolStateDiff, `{"from":"missing"}`, "not found"},
		{mcp.ToolStateDiff, `{"from":"missing","unknown":true}`, "unknown field"},
	}
	for _, tc := range cases {
		_, err := b.CallTool(context.Background(), tc.tool, json.RawMessage(tc.args))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s(%s) err = %v, want substring %q", tc.tool, tc.args, err, tc.want)
		}
	}

	// Save one valid capture to test invalid "to" and invalid "paths".
	if _, err := b.CallTool(context.Background(), mcp.ToolStateCapture, json.RawMessage(`{"name":"base"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CallTool(context.Background(), mcp.ToolStateDiff, json.RawMessage(`{"from":"base","to":"nope"}`)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing to err = %v", err)
	}
	if _, err := b.CallTool(context.Background(), mcp.ToolStateDiff, json.RawMessage(`{"from":"base","paths":["a..b"]}`)); err == nil || !strings.Contains(err.Error(), "empty segment") {
		t.Fatalf("invalid path err = %v", err)
	}
}
