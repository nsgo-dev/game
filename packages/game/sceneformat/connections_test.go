package sceneformat_test

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

type doorState struct {
	Open      bool
	OpenCount int
	Speed     float64
	Reason    string
	LastBy    string
}

type lampState struct {
	Lit bool
}

// buttonDoorRegistry registers reusable "button" and "door" events and handlers with zero knowledge of which objects are wired together in any scene.
func buttonDoorRegistry(t testing.TB) *sceneformat.EventRegistry {
	t.Helper()
	reg := sceneformat.NewEventRegistry()
	reg.MustRegisterEvent(sceneformat.EventType{
		Name:      "pressed",
		Component: "button",
		Doc:       "Emitted when a button is pressed.",
	})
	reg.MustRegisterEvent(sceneformat.EventType{
		Name:      "opened",
		Component: "door",
		Doc:       "Emitted when a door finishes opening.",
	})
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name:      "open",
		Component: "door",
		Doc:       "Opens the target door.",
		Args: []sceneformat.Field{
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(10)},
			{Name: "reason", Kind: sceneformat.FieldString, Default: "default"},
		},
		Func: func(call sceneformat.EventCall) {
			if call.World != nil {
				doors := entities.Component[doorState](call.World)
				var st doorState
				if cur, ok := doors.Get(call.TargetEntity); ok {
					st = *cur
				}
				st.Open = true
				st.OpenCount++
				st.Speed = call.Args["speed"].(float64)
				st.Reason = call.Args["reason"].(string)
				if who, ok := call.Payload.(string); ok {
					st.LastBy = who
				}
				doors.Set(call.TargetEntity, st)
			}
			if call.TargetObject != nil {
				if call.TargetObject.Doc.Properties == nil {
					call.TargetObject.Doc.Properties = map[string]any{}
				}
				call.TargetObject.Doc.Properties["open"] = true
				call.TargetObject.Doc.Properties["speed"] = call.Args["speed"]
				call.TargetObject.Doc.Properties["reason"] = call.Args["reason"]
				if call.TargetObject.Sprite != nil {
					call.TargetObject.Sprite.Alpha = 0.25
				}
			}
			// Re-emit door's own "opened" event so scenes can chain connections without Go glue.
			call.Bus.Emit(call.Target, "opened", call.Source)
		},
	})
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name:      "turn_on",
		Component: "lamp",
		Doc:       "Turns on a lamp.",
		Func: func(call sceneformat.EventCall) {
			if call.World != nil {
				lamps := entities.Component[lampState](call.World)
				lamps.Set(call.TargetEntity, lampState{Lit: true})
			}
		},
	})
	return reg
}

// TestButtonOpensDoorDeclaredInYAML proves the acceptance criterion for #490:
// a button-opens-door connection declared purely in YAML works on instantiation with zero Go wiring code between the button and the door.
func TestButtonOpensDoorDeclaredInYAML(t *testing.T) {
	const sceneYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Button Door Room
layers:
  - name: actors
    objects:
      - id: red_button
        x: 32
        "y": 64
        components:
          button: {}
      - id: exit_door
        x: 128
        "y": 64
        sprite:
          atlas: actors
          frame: hero
        components:
          door:
            open: false
      - id: indicator_lamp
        x: 128
        "y": 32
        components:
          lamp: {}
connections:
  - from: red_button
    event: pressed
    to: exit_door
    handler: open
    args:
      reason: player_press
      speed: 3.5
  - from: exit_door
    event: opened
    to: indicator_lamp
    handler: turn_on
`
	reg := buttonDoorRegistry(t)

	// 1. Parse and validate against the EventRegistry, and verify byte-for-byte deterministic round-trip.
	doc, err := sceneformat.ParseWith([]byte(sceneYAML), sceneformat.ParseOptions{
		File:   "scenes/room.scene.yaml",
		Events: reg,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != sceneYAML {
		t.Fatalf("round trip changed YAML:\n--- got ---\n%s\n--- want ---\n%s", encoded, sceneYAML)
	}

	// 2. End-to-end with entities.World (SpawnEntitiesWith): zero Go wiring between red_button, exit_door, and indicator_lamp.
	world := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{Events: reg})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	doors := entities.Component[doorState](world)
	lamps := entities.Component[lampState](world)
	if st, ok := doors.Get(spawned.Entities["exit_door"]); ok && st.Open {
		t.Fatal("door started open before button press")
	}

	// Trigger the button event on the button entity only.
	if n := spawned.Events.EmitEntity(spawned.Entities["red_button"], "pressed", "hero"); n != 1 {
		t.Fatalf("EmitEntity returned %d handlers, want 1", n)
	}

	gotDoor, ok := doors.Get(spawned.Entities["exit_door"])
	if !ok || !gotDoor.Open {
		t.Fatalf("door state = %+v (ok=%v), want open", gotDoor, ok)
	}
	if gotDoor.Speed != 3.5 || gotDoor.Reason != "player_press" || gotDoor.LastBy != "hero" {
		t.Fatalf("door state = %+v, want speed=3.5 reason=player_press lastBy=hero", gotDoor)
	}
	gotLamp, ok := lamps.Get(spawned.Entities["indicator_lamp"])
	if !ok || !gotLamp.Lit {
		t.Fatalf("chained indicator_lamp = %+v (ok=%v), want lit via door.opened", gotLamp, ok)
	}

	// 3. End-to-end with BuiltScene (BuildWith): same YAML works for 2D sprite scenes too.
	built, err := sceneformat.BuildWith(doc, caveResolver, sceneformat.BuildOptions{Events: reg})
	if err != nil {
		t.Fatalf("BuildWith: %v", err)
	}
	if n := built.Events.Emit("red_button", "pressed", nil); n != 1 {
		t.Fatalf("BuiltScene.Events.Emit = %d, want 1", n)
	}
	doorObj := built.Object("exit_door")
	if doorObj.Sprite.Alpha != 0.25 || doorObj.Doc.Properties["open"] != true || doorObj.Doc.Properties["speed"] != 3.5 {
		t.Fatalf("door BuiltObject after emit: alpha=%v props=%v", doorObj.Sprite.Alpha, doorObj.Doc.Properties)
	}
}

func TestInstancedSubSceneConnectionIsolationAndOuterWiring(t *testing.T) {
	raw := readProject(t, "puzzle_room.scene.yaml")
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse puzzle_room: %v", err)
	}
	// Verify round-trip byte for byte on the project fixture.
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode puzzle_room: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("puzzle_room round trip differed:\n%s\n--- want ---\n%s", encoded, raw)
	}

	reg := buttonDoorRegistry(t)
	world := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{
		Scenes: projectScenes,
		Events: reg,
	})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	doors := entities.Component[doorState](world)

	// Pressing west/button opens ONLY west/door, NEVER east/door.
	if n := spawned.Events.EmitEntity(spawned.Entities["west/button"], "pressed", nil); n != 1 {
		t.Fatalf("west/button emit = %d, want 1", n)
	}
	westDoor, _ := doors.Get(spawned.Entities["west/door"])
	if westDoor == nil || !westDoor.Open || westDoor.OpenCount != 1 || westDoor.Speed != 2.5 || westDoor.Reason != "button" {
		t.Fatalf("west/door = %+v, want open count=1 speed=2.5 reason=button", westDoor)
	}
	if st, ok := doors.Get(spawned.Entities["east/door"]); ok && st.Open {
		t.Fatalf("east/door opened when west/button was pressed: %+v", st)
	}

	// button_door.scene.yaml declares once: true on its button->door connection, so pressing west/button a second time does not fire again.
	if n := spawned.Events.EmitEntity(spawned.Entities["west/button"], "pressed", nil); n != 0 {
		t.Fatalf("second west/button emit = %d, want 0 (once: true)", n)
	}
	westDoor, _ = doors.Get(spawned.Entities["west/door"])
	if westDoor.OpenCount != 1 {
		t.Fatalf("west/door OpenCount = %d after second press, want 1", westDoor.OpenCount)
	}

	// Outer connection: master_switch opens both west/door and east/door with handler defaults (speed=1.0, reason="default").
	if n := spawned.Events.EmitEntity(spawned.Entities["master_switch"], "pressed", "lever"); n != 2 {
		t.Fatalf("master_switch emit = %d, want 2", n)
	}
	westDoor, _ = doors.Get(spawned.Entities["west/door"])
	eastDoor, _ := doors.Get(spawned.Entities["east/door"])
	if westDoor == nil || westDoor.OpenCount != 2 || westDoor.Speed != 1.0 || westDoor.Reason != "default" {
		t.Fatalf("west/door after master_switch = %+v, want count=2 speed=1.0 reason=default", westDoor)
	}
	if eastDoor == nil || !eastDoor.Open || eastDoor.OpenCount != 1 || eastDoor.Speed != 1.0 || eastDoor.Reason != "default" {
		t.Fatalf("east/door after master_switch = %+v, want open count=1 speed=1.0 reason=default", eastDoor)
	}
}

func TestNestedInstancingAndInheritanceConnections(t *testing.T) {
	files := newMemScenes(map[string]string{
		"scenes/leaf.scene.yaml": `version: 2
layers:
  - name: root
    objects:
      - id: btn
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: btn
    event: pressed
    to: door
    handler: open
`,
		"scenes/room.scene.yaml": `version: 2
layers:
  - name: root
    objects:
      - id: pair
        scene: scenes/leaf.scene.yaml
`,
		"scenes/base.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: wing
        scene: scenes/room.scene.yaml
      - id: base_btn
        components: { button: {} }
      - id: base_door
        components: { door: {} }
connections:
  - from: base_btn
    event: pressed
    to: base_door
    handler: open
`,
		"scenes/derived.scene.yaml": `version: 2
inherits: scenes/base.scene.yaml
layers:
  - name: actors
    objects:
      - id: bonus_lamp
        components: { lamp: {} }
connections:
  - from: base_door
    event: opened
    to: bonus_lamp
    handler: turn_on
`,
	})
	raw, err := files.Scene("scenes/derived.scene.yaml")
	if err != nil {
		t.Fatalf("parse derived: %v", err)
	}
	resolved, err := sceneformat.Resolve(raw, files)
	if err != nil {
		t.Fatalf("Resolve derived: %v", err)
	}

	// Expect 3 connections in order: nested instanced, base's own, derived's own.
	wantPairs := []string{
		"wing/pair/btn->wing/pair/door",
		"base_btn->base_door",
		"base_door->bonus_lamp",
	}
	var gotPairs []string
	for _, c := range resolved.Connections {
		gotPairs = append(gotPairs, c.From+"->"+c.To)
	}
	if !slices.Equal(gotPairs, wantPairs) {
		t.Fatalf("resolved connections = %v, want %v", gotPairs, wantPairs)
	}

	world := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(resolved, world, sceneformat.SpawnOptions{Events: buttonDoorRegistry(t)})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	spawned.Events.Emit("wing/pair/btn", "pressed", nil)
	doors := entities.Component[doorState](world)
	if st, ok := doors.Get(spawned.Entities["wing/pair/door"]); !ok || !st.Open {
		t.Fatal("nested instanced door did not open")
	}
	if st, ok := doors.Get(spawned.Entities["base_door"]); ok && st.Open {
		t.Fatal("base_door opened from wing/pair/btn")
	}

	spawned.Events.Emit("base_btn", "pressed", nil)
	if st, ok := doors.Get(spawned.Entities["base_door"]); !ok || !st.Open {
		t.Fatal("inherited base_door did not open")
	}
	if st, ok := entities.Component[lampState](world).Get(spawned.Entities["bonus_lamp"]); !ok || !st.Lit {
		t.Fatal("derived bonus_lamp did not light from inherited base_door.opened")
	}
}

func TestConnectionValidationAndLoadErrLocations(t *testing.T) {
	reg := buttonDoorRegistry(t)
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name: "ping",
		Doc:  "A handler that accepts no args.",
		Func: func(sceneformat.EventCall) {},
	})

	cases := []struct {
		name      string
		yaml      string
		useEvents bool
		wantField string
		wantMsg   string
	}{
		{
			name: "version 1 refuses connections",
			yaml: `version: 1
layers:
  - name: actors
    objects:
      - id: a
      - id: b
connections:
  - from: a
    event: pressed
    to: b
    handler: open
`,
			wantField: "connections",
			wantMsg:   "version 2",
		},
		{
			name: "unknown field on connection",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: a
      - id: b
connections:
  - from: a
    event: pressed
    to: b
    handler: open
    bogus: 1
`,
			wantField: "",
			wantMsg:   "bogus",
		},
		{
			name: "empty from",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: a
connections:
  - from: ""
    event: pressed
    to: a
    handler: open
`,
			wantField: "connections[0].from",
			wantMsg:   "empty from",
		},
		{
			name: "empty event",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: a
connections:
  - from: a
    event: ""
    to: a
    handler: open
`,
			wantField: "connections[0].event",
			wantMsg:   "empty event",
		},
		{
			name: "empty to",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: a
connections:
  - from: a
    event: pressed
    to: ""
    handler: open
`,
			wantField: "connections[0].to",
			wantMsg:   "empty to",
		},
		{
			name: "empty handler",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: a
connections:
  - from: a
    event: pressed
    to: a
    handler: ""
`,
			wantField: "connections[0].handler",
			wantMsg:   "empty handler",
		},
		{
			name: "unknown source object",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: door
connections:
  - from: ghost_button
    event: pressed
    to: door
    handler: open
`,
			wantField: "connections[0].from",
			wantMsg:   `unknown source object "ghost_button"`,
		},
		{
			name: "unknown target object",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
connections:
  - from: button
    event: pressed
    to: ghost_door
    handler: open
`,
			wantField: "connections[0].to",
			wantMsg:   `unknown target object "ghost_door"`,
		},
		{
			name: "unregistered event",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: presed
    to: door
    handler: open
`,
			useEvents: true,
			wantField: "connections[0].event",
			wantMsg:   `unknown event "presed"`,
		},
		{
			name: "unregistered handler",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: opne
`,
			useEvents: true,
			wantField: "connections[0].handler",
			wantMsg:   `unknown handler "opne"`,
		},
		{
			name: "source object missing required component",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: rock
      - id: door
        components: { door: {} }
connections:
  - from: rock
    event: pressed
    to: door
    handler: open
`,
			useEvents: true,
			wantField: "connections[0].from",
			wantMsg:   `lacks component "button"`,
		},
		{
			name: "target object missing required component",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: wall
connections:
  - from: button
    event: pressed
    to: wall
    handler: open
`,
			useEvents: true,
			wantField: "connections[0].to",
			wantMsg:   `lacks component "door"`,
		},
		{
			name: "unknown handler arg",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      sped: 2
`,
			useEvents: true,
			wantField: "connections[0].args.sped",
			wantMsg:   `unknown arg "sped"`,
		},
		{
			name: "handler takes no args",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: ping
    args:
      extra: 1
`,
			useEvents: true,
			wantField: "connections[0].args.extra",
			wantMsg:   `takes no args`,
		},
		{
			name: "handler arg wrong kind",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      speed: fast
`,
			useEvents: true,
			wantField: "connections[0].args.speed",
			wantMsg:   "want float",
		},
		{
			name: "handler arg out of range",
			yaml: `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      speed: 99
`,
			useEvents: true,
			wantField: "connections[0].args.speed",
			wantMsg:   "above the maximum 10",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := sceneformat.ParseOptions{File: "room.scene.yaml"}
			if tc.useEvents {
				opts.Events = reg
			}
			_, err := sceneformat.ParseWith([]byte(tc.yaml), opts)
			if err == nil {
				t.Fatalf("ParseWith succeeded, want error containing %q", tc.wantMsg)
			}
			if tc.wantField != "" {
				e := loaderrtest.ExpectField(t, err, "room.scene.yaml", []byte(tc.yaml), tc.wantField)
				if !strings.Contains(e.Error(), tc.wantMsg) {
					t.Fatalf("error %q does not mention %q", e, tc.wantMsg)
				}
				var fe *sceneformat.FieldError
				if !errors.As(err, &fe) {
					t.Fatalf("error %v does not wrap *FieldError", err)
				}
			} else {
				e := loaderrtest.Expect(t, err, "room.scene.yaml", 12)
				if !strings.Contains(e.Error(), tc.wantMsg) {
					t.Fatalf("error %q does not mention %q", e, tc.wantMsg)
				}
			}
		})
	}
}

func TestResolveRefusesUnknownConnectionTargetWithFieldPath(t *testing.T) {
	files := newMemScenes(map[string]string{
		"scenes/base.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: button
      - id: door
connections:
  - from: button
    event: pressed
    to: door
    handler: open
`,
		"scenes/bad.scene.yaml": `version: 2
inherits: scenes/base.scene.yaml
connections:
  - from: button
    event: pressed
    to: nonexistent
    handler: open
`,
	})
	doc, err := files.Scene("scenes/bad.scene.yaml")
	if err != nil {
		t.Fatalf("Parse bad: %v", err)
	}
	_, err = sceneformat.Resolve(doc, files)
	if err == nil {
		t.Fatal("Resolve succeeded with unknown connection target")
	}
	var fe *sceneformat.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("Resolve error = %v (%T), want *FieldError", err, err)
	}
	if !slices.Equal(fe.Path, []string{"connections", "0", "to"}) {
		t.Fatalf("FieldError.Path = %v, want [connections 0 to]", fe.Path)
	}
}

func TestEventBusDispatchSemantics(t *testing.T) {
	var log []string
	reg := sceneformat.NewEventRegistry()
	reg.MustRegisterEvent(sceneformat.EventType{Name: "tick"})
	reg.MustRegisterEvent(sceneformat.EventType{Name: "step"})
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name: "record",
		Args: []sceneformat.Field{
			{Name: "label", Kind: sceneformat.FieldString, Default: "x"},
		},
		Func: func(call sceneformat.EventCall) {
			// Mutate call.Args to prove later invocations get an uncorrupted copy.
			label := call.Args["label"].(string)
			call.Args["label"] = "mutated"
			log = append(log, call.Source+"->"+call.Target+":"+label)
		},
	})
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name: "reenter_once",
		Func: func(call sceneformat.EventCall) {
			log = append(log, "once:"+call.Target)
			// Re-emitting the same event inside a Once handler must NOT re-trigger it.
			call.Bus.Emit(call.Source, call.Event, nil)
		},
	})
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name: "enqueue_next",
		Func: func(call sceneformat.EventCall) {
			log = append(log, "enq:"+call.Target)
			call.Bus.Enqueue(call.Target, "step", nil)
		},
	})

	const src = `version: 2
layers:
  - name: actors
    objects:
      - id: a
      - id: b
connections:
  - from: a
    event: tick
    to: a
    handler: reenter_once
    once: true
  - from: a
    event: tick
    to: b
    handler: record
    args:
      label: first
  - from: a
    event: tick
    to: b
    handler: record
  - from: b
    event: tick
    to: a
    handler: enqueue_next
  - from: a
    event: step
    to: b
    handler: record
    args:
      label: stepped
`
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Events: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	bus, err := reg.Bind(doc)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bus.Len() != 5 {
		t.Fatalf("Len = %d, want 5", bus.Len())
	}

	// Emit a.tick: reenter_once fires once and re-emits a.tick, which runs only the two record handlers (NOT reenter_once again!).
	bus.Emit("a", "tick", nil)
	want := []string{
		"once:a",
		"a->b:first",
		"a->b:x",
		"a->b:first",
		"a->b:x",
	}
	if !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	if bus.Len() != 4 {
		t.Fatalf("Len after once fired = %d, want 4", bus.Len())
	}

	// Enqueue + Flush delivers FIFO including events enqueued during Flush.
	log = nil
	bus.Enqueue("b", "tick", nil)
	if n := bus.Flush(); n != 2 {
		t.Fatalf("Flush = %d, want 2", n)
	}
	if !slices.Equal(log, []string{"enq:a", "a->b:stepped"}) {
		t.Fatalf("flush log = %v, want [enq:a a->b:stepped]", log)
	}

	// DisconnectAll stops further delivery and drops queued events.
	log = nil
	bus.Enqueue("a", "step", nil)
	bus.DisconnectAll()
	if bus.Len() != 0 {
		t.Fatalf("Len after DisconnectAll = %d, want 0", bus.Len())
	}
	if n := bus.Flush(); n != 0 {
		t.Fatalf("Flush after DisconnectAll = %d, want 0", n)
	}
	if n := bus.Emit("a", "tick", nil); n != 0 {
		t.Fatalf("Emit after DisconnectAll = %d, want 0", n)
	}
}

func TestEventBusSkipsDespawnedEntitiesAndSpawnAtomicity(t *testing.T) {
	reg := buttonDoorRegistry(t)
	const src = `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components: { button: {} }
      - id: door
        components: { door: {} }
connections:
  - from: button
    event: pressed
    to: door
    handler: open
`
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Events: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	world := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{Events: reg})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	world.Destroy(spawned.Entities["door"])
	if n := spawned.Events.EmitEntity(spawned.Entities["button"], "pressed", nil); n != 0 {
		t.Fatalf("EmitEntity with dead target = %d, want 0", n)
	}
	world.Destroy(spawned.Entities["button"])
	if n := spawned.Events.EmitEntity(spawned.Entities["button"], "pressed", nil); n != 0 {
		t.Fatalf("EmitEntity with dead source = %d, want 0", n)
	}

	// Atomicity: a validation-only handler (Func == nil) parses fine, but SpawnEntitiesWith refuses it before spawning any entity.
	valOnly := sceneformat.NewEventRegistry()
	valOnly.MustRegisterEvent(sceneformat.EventType{Name: "pressed"})
	valOnly.MustRegisterHandler(sceneformat.HandlerType{Name: "open"}) // Func == nil
	if _, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Events: valOnly}); err != nil {
		t.Fatalf("validation-only ParseWith: %v", err)
	}
	freshWorld := entities.NewWorld()
	if _, err := sceneformat.SpawnEntitiesWith(doc, freshWorld, sceneformat.SpawnOptions{Events: valOnly}); err == nil || !strings.Contains(err.Error(), "no callback") {
		t.Fatalf("SpawnEntitiesWith nil Func = %v, want no callback error", err)
	}
	if freshWorld.Count() != 0 {
		t.Fatalf("freshWorld.Count() = %d after failed SpawnEntitiesWith, want 0", freshWorld.Count())
	}
}

func TestEventRegistryValidationAndClone(t *testing.T) {
	var nilReg *sceneformat.EventRegistry
	if err := nilReg.RegisterEvent(sceneformat.EventType{Name: "a"}); err == nil {
		t.Fatal("nil RegisterEvent succeeded")
	}
	if err := nilReg.RegisterHandler(sceneformat.HandlerType{Name: "a"}); err == nil {
		t.Fatal("nil RegisterHandler succeeded")
	}

	reg := sceneformat.NewEventRegistry()
	if err := reg.RegisterEvent(sceneformat.EventType{}); err == nil {
		t.Fatal("empty event name succeeded")
	}
	if err := reg.RegisterHandler(sceneformat.HandlerType{}); err == nil {
		t.Fatal("empty handler name succeeded")
	}
	reg.MustRegisterEvent(sceneformat.EventType{Name: "b", Doc: "B"})
	reg.MustRegisterEvent(sceneformat.EventType{Name: "a", Doc: "A"})
	if err := reg.RegisterEvent(sceneformat.EventType{Name: "a"}); err == nil {
		t.Fatal("duplicate event succeeded")
	}
	reg.MustRegisterHandler(sceneformat.HandlerType{
		Name: "z",
		Args: []sceneformat.Field{{Name: "k", Kind: sceneformat.FieldInt, Default: 1}},
	})
	reg.MustRegisterHandler(sceneformat.HandlerType{Name: "m"})
	if err := reg.RegisterHandler(sceneformat.HandlerType{Name: "m"}); err == nil {
		t.Fatal("duplicate handler succeeded")
	}
	if err := reg.RegisterHandler(sceneformat.HandlerType{
		Name: "bad_arg",
		Args: []sceneformat.Field{{Name: "x", Kind: sceneformat.FieldBool, Min: sceneformat.Float64(0)}},
	}); err == nil {
		t.Fatal("handler with invalid arg field succeeded")
	}
	if !slices.Equal(reg.EventNames(), []string{"a", "b"}) {
		t.Fatalf("EventNames = %v", reg.EventNames())
	}
	if !slices.Equal(reg.HandlerNames(), []string{"m", "z"}) {
		t.Fatalf("HandlerNames = %v", reg.HandlerNames())
	}
	if ev, ok := reg.LookupEvent("a"); !ok || ev.Doc != "A" {
		t.Fatalf("LookupEvent(a) = %+v, %v", ev, ok)
	}
	if h, ok := reg.LookupHandler("z"); !ok || len(h.Args) != 1 || h.Args[0].Name != "k" {
		t.Fatalf("LookupHandler(z) = %+v, %v", h, ok)
	}
	if len(reg.Events()) != 2 || len(reg.Handlers()) != 2 {
		t.Fatalf("Events=%d Handlers=%d, want 2 each", len(reg.Events()), len(reg.Handlers()))
	}

	// SceneDoc.Clone deep-copies Connections and Args.
	orig := &sceneformat.SceneDoc{
		Version: 2,
		Layers:  []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{{ID: "a"}, {ID: "b"}}}},
		Connections: []sceneformat.ConnectionDoc{{
			From: "a", Event: "e", To: "b", Handler: "h",
			Args: map[string]any{"nested": map[string]any{"v": 1}},
			Once: true,
		}},
	}
	cloned := orig.Clone()
	cloned.Connections[0].To = "changed"
	cloned.Connections[0].Args["nested"].(map[string]any)["v"] = 99
	if orig.Connections[0].To != "b" || !reflect.DeepEqual(orig.Connections[0].Args["nested"], map[string]any{"v": 1}) {
		t.Fatalf("mutating Clone changed original: %+v", orig.Connections[0])
	}
}
