package mcp_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestTunablesChangeAffectsLiveSimulation verifies the end-to-end acceptance
// flow: an agent lists tunables, reads physics parameters, raises jump_speed
// and lowers gravity via game_tunable_set, steps the live simulation, and sees
// the player reach a measurably higher apex via game_component_get.
func TestTunablesChangeAffectsLiveSimulation(t *testing.T) {
	gravity := 800.0
	jumpSpeed := 300.0
	maxJumps := 1
	godMode := false
	levelName := "cave_1"

	console := debug.NewConsole()
	console.RegisterFloat("player.jump_speed", 100, 1200, &jumpSpeed)
	console.RegisterFloat("physics.gravity", 0, 2000, &gravity)
	console.RegisterInt("player.max_jumps", 1, 5, &maxJumps)
	console.RegisterBool("player.god_mode", &godMode)
	console.RegisterString("level.name", &levelName)

	world := entities.NewWorld()
	if err := entities.RegisterCodec[posComp](world, "position"); err != nil {
		t.Fatal(err)
	}
	player := world.Spawn()
	entities.Set(world, player, posComp{X: 0, Y: 0})

	// Simulate a single jump from Y=0 using the live tunable pointers and record the peak height.
	simulateJumpApex := func() float64 {
		y := 0.0
		vy := jumpSpeed
		peak := 0.0
		const dt = 1.0 / 60.0
		for range 240 {
			vy -= gravity * dt
			y += vy * dt
			if y > peak {
				peak = y
			}
			if y <= 0 && vy < 0 {
				break
			}
		}
		entities.Set(world, player, posComp{X: 0, Y: float32(peak)})
		return peak
	}

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		world.Attach(cx)
		if _, err := mcp.Attach(cx, mcp.Config{
			World:   world,
			Console: console,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. List all tunables and verify deterministic sorted order and metadata (including min=0).
	listRaw, err := callToolRaw(t, app, cx, mcp.ToolTunablesList, nil)
	if err != nil {
		t.Fatalf("game_tunables_list failed: %v", err)
	}
	var listRes mcp.TunablesListResult
	if err := json.Unmarshal(listRaw, &listRes); err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"level.name", "physics.gravity", "player.god_mode", "player.jump_speed", "player.max_jumps"}
	if listRes.Total != len(wantNames) || len(listRes.Tunables) != len(wantNames) {
		t.Fatalf("tunables list = %+v, want %d items", listRes, len(wantNames))
	}
	for i, want := range wantNames {
		if listRes.Tunables[i].Name != want {
			t.Fatalf("tunables[%d].Name = %q, want %q", i, listRes.Tunables[i].Name, want)
		}
	}
	// Verify numeric min=0 is serialized in JSON rather than omitted.
	if !strings.Contains(string(listRaw), `"min":0`) {
		t.Fatalf("expected physics.gravity min:0 in JSON output: %s", listRaw)
	}

	// Prefix filter narrows to player.* tunables.
	playerOnly := callTool[mcp.TunablesListResult](t, app, cx, mcp.ToolTunablesList, map[string]any{
		"prefix": "player.",
	})
	if playerOnly.Total != 3 || playerOnly.Tunables[0].Name != "player.god_mode" || playerOnly.Tunables[2].Name != "player.max_jumps" {
		t.Fatalf("prefix-filtered tunables = %+v", playerOnly)
	}

	// 2. Read a single tunable before changing it.
	gotJump := callTool[mcp.TunableInfo](t, app, cx, mcp.ToolTunableGet, map[string]any{"name": "player.jump_speed"})
	if gotJump.Type != "float" || gotJump.Value != 300.0 || gotJump.Default != 300.0 || gotJump.Min != 100.0 || gotJump.Max != 1200.0 {
		t.Fatalf("tunable get(player.jump_speed) = %+v", gotJump)
	}

	// Baseline jump height with default tunables.
	var baselinePeak float64
	app.Post(func() { baselinePeak = simulateJumpApex() })
	app.Settle()

	baseComp := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       mcp.FormatEntityRef(player),
		"component": "position",
	})
	var basePos posComp
	if err := json.Unmarshal(baseComp.Value, &basePos); err != nil {
		t.Fatal(err)
	}

	// 3. Agent changes float, int, bool and string tunables via game_tunable_set.
	setJump := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "player.jump_speed",
		"value": 600,
	})
	if setJump.OldValue != 300.0 || setJump.Value != 600.0 || setJump.Default != 300.0 || setJump.Clamped || jumpSpeed != 600.0 {
		t.Fatalf("set player.jump_speed = %+v, var=%v", setJump, jumpSpeed)
	}

	setGrav := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "physics.gravity",
		"value": 400.0,
	})
	if setGrav.OldValue != 800.0 || setGrav.Value != 400.0 || gravity != 400.0 {
		t.Fatalf("set physics.gravity = %+v, var=%v", setGrav, gravity)
	}

	setJumps := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "player.max_jumps",
		"value": 3,
	})
	if setJumps.OldValue != 1.0 || setJumps.Value != 3.0 || maxJumps != 3 {
		t.Fatalf("set player.max_jumps = %+v, var=%d", setJumps, maxJumps)
	}

	setGod := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "player.god_mode",
		"value": true,
	})
	if setGod.OldValue != false || setGod.Value != true || !godMode {
		t.Fatalf("set player.god_mode = %+v, var=%v", setGod, godMode)
	}

	setLevel := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "level.name",
		"value": "boss_lair",
	})
	if setLevel.OldValue != "cave_1" || setLevel.Value != "boss_lair" || levelName != "boss_lair" {
		t.Fatalf("set level.name = %+v, var=%q", setLevel, levelName)
	}

	// 4. Re-run jump simulation and verify the agent sees the dramatic height increase!
	var boostedPeak float64
	app.Post(func() { boostedPeak = simulateJumpApex() })
	app.Settle()

	boostedComp := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       mcp.FormatEntityRef(player),
		"component": "position",
	})
	var boostedPos posComp
	if err := json.Unmarshal(boostedComp.Value, &boostedPos); err != nil {
		t.Fatal(err)
	}
	if boostedPeak <= baselinePeak*4 || float64(boostedPos.Y) <= float64(basePos.Y)*4 {
		t.Fatalf("expected tunable change to increase jump apex >4x: baseline=%v (%v) boosted=%v (%v)",
			baselinePeak, basePos.Y, boostedPeak, boostedPos.Y)
	}

	// 5. Optional clamp clamps out-of-range numeric values cleanly and reports clamped=true.
	clampedFloat := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "player.jump_speed",
		"value": 9999,
		"clamp": true,
	})
	if clampedFloat.OldValue != 600.0 || clampedFloat.Value != 1200.0 || !clampedFloat.Clamped || jumpSpeed != 1200.0 {
		t.Fatalf("clamped float = %+v, var=%v", clampedFloat, jumpSpeed)
	}
	clampedInt := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{
		"name":  "player.max_jumps",
		"value": 0,
		"clamp": true,
	})
	if clampedInt.OldValue != 3.0 || clampedInt.Value != 1.0 || !clampedInt.Clamped || maxJumps != 1 {
		t.Fatalf("clamped int = %+v, var=%d", clampedInt, maxJumps)
	}
	// Whole float JSON number (e.g. 4.0) is accepted for int tunables.
	wholeFloatInt := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, `{"name":"player.max_jumps","value":4.0}`)
	if wholeFloatInt.Value != 4.0 || maxJumps != 4 {
		t.Fatalf("whole float int = %+v, var=%d", wholeFloatInt, maxJumps)
	}
	// Reading back reflects current value while preserving original registration default.
	afterJump := callTool[mcp.TunableInfo](t, app, cx, mcp.ToolTunableGet, map[string]any{"name": "player.jump_speed"})
	if math.Abs(afterJump.Value.(float64)-1200) > 1e-9 || math.Abs(afterJump.Default.(float64)-300) > 1e-9 {
		t.Fatalf("after get = %+v", afterJump)
	}
}

func TestConsoleCommandsListAndExec(t *testing.T) {
	gravity := 980.0
	spawned := make([]string, 0)

	console := debug.NewConsole()
	console.RegisterFloat("gravity", 0, 5000, &gravity)
	console.RegisterCommand("spawn", "spawn <kind> [count] - spawn enemies into the room", func(args []string) (string, error) {
		if len(args) < 1 || len(args) > 2 {
			return "", errors.New("usage: spawn <kind> [count]")
		}
		count := 1
		if len(args) == 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return "", fmt.Errorf("invalid count %q", args[1])
			}
			count = n
		}
		lines := make([]string, 0, count)
		for i := range count {
			id := fmt.Sprintf("%s_%d", args[0], i+1)
			spawned = append(spawned, id)
			lines = append(lines, "spawned "+id)
		}
		return strings.Join(lines, "\n"), nil
	})
	console.RegisterCommand("echo", "echo <msg> - echo message", func(args []string) (string, error) {
		return strings.Join(args, " | "), nil
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{Console: console}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. List all commands (built-ins + custom), sorted alphabetically.
	allCmds := callTool[mcp.ConsoleCommandsListResult](t, app, cx, mcp.ToolConsoleCommandsList, nil)
	wantAll := []string{"clear", "echo", "get", "help", "list", "set", "spawn"}
	if allCmds.Total != len(wantAll) || len(allCmds.Commands) != len(wantAll) {
		t.Fatalf("commands list = %+v, want %v", allCmds, wantAll)
	}
	for i, want := range wantAll {
		if allCmds.Commands[i].Name != want {
			t.Fatalf("commands[%d] = %+v, want %q", i, allCmds.Commands[i], want)
		}
	}
	if allCmds.Commands[6].Builtin || !strings.Contains(allCmds.Commands[6].Help, "spawn enemies") {
		t.Fatalf("spawn command metadata = %+v", allCmds.Commands[6])
	}

	// Filter to custom commands only.
	customCmds := callTool[mcp.ConsoleCommandsListResult](t, app, cx, mcp.ToolConsoleCommandsList, map[string]any{
		"include_builtin": false,
	})
	if customCmds.Total != 2 || customCmds.Commands[0].Name != "echo" || customCmds.Commands[1].Name != "spawn" {
		t.Fatalf("custom commands = %+v", customCmds)
	}

	// 2. Execute built-in set command and verify variable updated.
	setRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": "set gravity 1200",
	})
	if !setRes.OK || setRes.Error != "" || setRes.Output != "gravity = 1200" || len(setRes.Lines) != 1 || setRes.Lines[0] != "gravity = 1200" || gravity != 1200 {
		t.Fatalf("exec set gravity = %+v, gravity=%v", setRes, gravity)
	}

	// 3. Execute custom multi-line command and verify all output lines are captured cleanly.
	spawnRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": "spawn enemy 3",
	})
	if !spawnRes.OK || spawnRes.Error != "" || len(spawned) != 3 {
		t.Fatalf("exec spawn = %+v, spawned=%v", spawnRes, spawned)
	}
	wantLines := []string{"spawned enemy_1", "spawned enemy_2", "spawned enemy_3"}
	if len(spawnRes.Lines) != 3 || spawnRes.Lines[0] != wantLines[0] || spawnRes.Lines[2] != wantLines[2] {
		t.Fatalf("spawn lines = %#v, want %#v", spawnRes.Lines, wantLines)
	}

	// Quoted arguments pass through splitConsoleArgs properly.
	echoRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": `echo "hello world" second`,
	})
	if !echoRes.OK || echoRes.Output != "hello world | second" {
		t.Fatalf("exec echo = %+v", echoRes)
	}

	// 4. Unknown command and failing custom command return {ok: false, error: ...} without panicking.
	unknownRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": "does_not_exist arg1",
	})
	if unknownRes.OK || !strings.Contains(unknownRes.Error, "unknown command") || len(unknownRes.Lines) != 0 {
		t.Fatalf("exec unknown = %+v, want ok=false with unknown command error", unknownRes)
	}

	badArgRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": "spawn enemy nope",
	})
	if badArgRes.OK || !strings.Contains(badArgRes.Error, "invalid count") {
		t.Fatalf("exec bad arg = %+v, want ok=false with invalid count", badArgRes)
	}

	badQuoteRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": `echo "unterminated`,
	})
	if badQuoteRes.OK || !strings.Contains(badQuoteRes.Error, "unterminated") {
		t.Fatalf("exec bad quote = %+v", badQuoteRes)
	}

	// Built-in clear succeeds with empty output and empty lines slice.
	clearRes := callTool[mcp.ConsoleExecResult](t, app, cx, mcp.ToolConsoleExec, map[string]any{
		"command": "clear",
	})
	if !clearRes.OK || clearRes.Error != "" || clearRes.Output != "" || len(clearRes.Lines) != 0 {
		t.Fatalf("exec clear = %+v", clearRes)
	}
}

func TestConsoleDynamicResolutionAndErrors(t *testing.T) {
	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. Nil Console returns clean errors on every console/tunable tool without panicking.
	for _, tc := range []struct {
		tool string
		args any
	}{
		{mcp.ToolTunablesList, nil},
		{mcp.ToolTunableGet, map[string]any{"name": "gravity"}},
		{mcp.ToolTunableSet, map[string]any{"name": "gravity", "value": 10}},
		{mcp.ToolConsoleCommandsList, nil},
		{mcp.ToolConsoleExec, map[string]any{"command": "help"}},
	} {
		if _, err := callToolRaw(t, app, cx, tc.tool, tc.args); err == nil || !strings.Contains(err.Error(), "no debug console") {
			t.Fatalf("%s with nil console = %v, want no debug console error", tc.tool, err)
		}
	}

	// 2. Attach a console via SetConsole and test validation & error handling.
	c1 := debug.NewConsole()
	speed := 10.0
	lives := 3
	debugDraw := false
	title := "intro"
	c1.RegisterFloat("speed", 0, 100, &speed)
	c1.RegisterInt("lives", 1, 10, &lives)
	c1.RegisterBool("debug_draw", &debugDraw)
	c1.RegisterString("title", &title)

	app.Post(func() { bridge.SetConsole(c1) })
	app.Settle()

	// Unknown JSON fields rejected on every tool.
	for _, tool := range []string{
		mcp.ToolTunablesList,
		mcp.ToolTunableGet,
		mcp.ToolTunableSet,
		mcp.ToolConsoleCommandsList,
		mcp.ToolConsoleExec,
	} {
		if _, err := callToolRaw(t, app, cx, tool, `{"bogus":1}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s with unknown field = %v, want unknown field error", tool, err)
		}
	}

	// Missing required arguments rejected.
	if _, err := callToolRaw(t, app, cx, mcp.ToolTunableGet, `{}`); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("tunable_get missing name = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolTunableSet, `{"name":"speed"}`); err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Fatalf("tunable_set missing value = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolTunableSet, `{"value":5}`); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("tunable_set missing name = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolConsoleExec, `{"command":"   "}`); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("console_exec empty command = %v", err)
	}

	// Unknown tunable reports available names.
	if _, err := callToolRaw(t, app, cx, mcp.ToolTunableGet, map[string]any{"name": "nope"}); err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "speed") {
		t.Fatalf("tunable_get unknown = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolTunableSet, map[string]any{"name": "nope", "value": 1}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("tunable_set unknown = %v", err)
	}

	// Type mismatches, null values and out-of-range values fail without mutating variables.
	badSets := []struct {
		payload string
		want    string
	}{
		{`{"name":"speed","value":null}`, "expects float, got null"},
		{`{"name":"speed","value":"fast"}`, "expects float, got string"},
		{`{"name":"speed","value":true}`, "expects float, got bool"},
		{`{"name":"speed","value":101}`, "out of range"},
		{`{"name":"speed","value":-1}`, "out of range"},
		{`{"name":"speed","value":1e500}`, "finite float"},
		{`{"name":"lives","value":2.5}`, "non-integer"},
		{`{"name":"lives","value":9007199254740993.5}`, "non-integer"},
		{`{"name":"lives","value":1e30}`, "overflows int"},
		{`{"name":"lives","value":"3"}`, "expects int, got string"},
		{`{"name":"lives","value":0}`, "out of range"},
		{`{"name":"lives","value":11}`, "out of range"},
		{`{"name":"debug_draw","value":"true"}`, "expects bool, got string"},
		{`{"name":"debug_draw","value":1}`, "expects bool, got number"},
		{`{"name":"title","value":123}`, "expects string, got number"},
		{`{"name":"title","value":false}`, "expects string, got bool"},
	}
	for _, tc := range badSets {
		if _, err := callToolRaw(t, app, cx, mcp.ToolTunableSet, tc.payload); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("tunable_set(%s) = %v, want substring %q", tc.payload, err, tc.want)
		}
	}
	if speed != 10.0 || lives != 3 || debugDraw != false || title != "intro" {
		t.Fatalf("variables mutated after rejected calls: speed=%v lives=%d debugDraw=%v title=%q", speed, lives, debugDraw, title)
	}

	// Scientific-notation integer values are accepted for int tunables.
	sciInt := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, `{"name":"lives","value":5e0}`)
	if sciInt.Value != 5.0 || lives != 5 {
		t.Fatalf("scientific int = %+v, lives=%d", sciInt, lives)
	}

	// Non-finite runtime floats serialize safely as "NaN"/"+Inf" rather than failing JSON encoding,
	// and can be inspected and recovered via game_tunable_set.
	speed = math.NaN()
	nanList := callTool[mcp.TunablesListResult](t, app, cx, mcp.ToolTunablesList, map[string]any{"prefix": "speed"})
	if nanList.Total != 1 || nanList.Tunables[0].Value != "NaN" {
		t.Fatalf("nan list = %+v, want Value=\"NaN\"", nanList)
	}
	nanGet := callTool[mcp.TunableInfo](t, app, cx, mcp.ToolTunableGet, map[string]any{"name": "speed"})
	if nanGet.Value != "NaN" {
		t.Fatalf("nan get = %+v, want Value=\"NaN\"", nanGet)
	}
	recoverRes := callTool[mcp.TunableSetResult](t, app, cx, mcp.ToolTunableSet, map[string]any{"name": "speed", "value": 25.0})
	if recoverRes.OldValue != "NaN" || recoverRes.Value != 25.0 || speed != 25.0 {
		t.Fatalf("recover from NaN = %+v, speed=%v", recoverRes, speed)
	}

	// Structured tunable/command queries do not pollute interactive console history or output entries.
	_ = callTool[mcp.ConsoleCommandsListResult](t, app, cx, mcp.ToolConsoleCommandsList, nil)
	if len(c1.History()) != 0 || len(c1.Entries()) != 0 {
		t.Fatalf("structured queries polluted console state: history=%v entries=%v", c1.History(), c1.Entries())
	}

	// 3. ConsoleFunc dynamically switches active console on each call.
	c2 := debug.NewConsole()
	volume := 0.5
	c2.RegisterFloat("volume", 0, 1, &volume)

	active := c1
	app.Post(func() {
		bridge.Detach()
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{
			ConsoleFunc: func() *debug.Console { return active },
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	app.Settle()

	res1 := callTool[mcp.TunablesListResult](t, app, cx, mcp.ToolTunablesList, nil)
	if res1.Total != 4 {
		t.Fatalf("c1 tunables = %+v, want 4", res1)
	}
	active = c2
	res2 := callTool[mcp.TunablesListResult](t, app, cx, mcp.ToolTunablesList, nil)
	if res2.Total != 1 || res2.Tunables[0].Name != "volume" {
		t.Fatalf("c2 tunables = %+v, want [volume]", res2)
	}

	// Nil bridge SetConsole is a safe no-op.
	var nilBridge *mcp.Bridge
	nilBridge.SetConsole(c1)
}
