package editor_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDeleteObjectTakesItsConnectionsAndUndoPutsThemBack(t *testing.T) {
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{
		{ID: "button"},
		{ID: "door", Children: []sceneformat.ObjectDoc{{ID: "hinge"}}},
		{ID: "lamp"},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "button", Event: "pressed", To: "lamp", Handler: "on"},
		{From: "button", Event: "pressed", To: "door", Handler: "open", Args: map[string]any{"speed": 2}},
		{From: "hinge", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "button", Handler: "glow"},
	}
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	if err := ed.DeleteObject("door"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "button", Event: "pressed", To: "lamp", Handler: "on"},
		{From: "lamp", Event: "lit", To: "button", Handler: "glow"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after delete = %+v, want %+v", ed.Doc.Connections, want)
	}
	if _, err := ed.SaveYAML(); err != nil {
		t.Fatalf("SaveYAML after delete: %v", err)
	}

	if !ed.Undo() {
		t.Fatal("Undo delete failed")
	}
	if after, err := ed.SaveYAML(); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("undoing the delete did not restore the connections (%v):\n%s\nwant:\n%s", err, after, before)
	}
	if !ed.Redo() {
		t.Fatal("Redo delete failed")
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after redo = %+v, want %+v", ed.Doc.Connections, want)
	}

	// Deleting the lamp takes both connections that are left, since each names it.
	if err := ed.DeleteObject("lamp"); err != nil {
		t.Fatalf("DeleteObject lamp: %v", err)
	}
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("connections after deleting lamp = %+v", ed.Doc.Connections)
	}
	if !ed.Undo() || !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("undoing the lamp delete: connections = %+v", ed.Doc.Connections)
	}
}

func TestDeleteObjectKeepsASiblingWhoseIDHasItAsPrefix(t *testing.T) {
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{{ID: "door"}, {ID: "door/x"}, {ID: "lamp"}} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "door", Event: "opened", To: "lamp", Handler: "on"},
		{From: "door/x", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "door/x", Handler: "glow"},
	}
	if err := ed.DeleteObject("door"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "door/x", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "door/x", Handler: "glow"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after deleting door = %+v, want %+v", ed.Doc.Connections, want)
	}
}

func TestDeleteObjectTakesTheConnectionsOfTheSceneItInstances(t *testing.T) {
	doc := testSceneDoc()
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/slime.scene.yaml": slimeScene}}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	inst, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "lamp"}); err != nil {
		t.Fatalf("PlaceObject lamp: %v", err)
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: inst + "/body", Event: "hit", To: "lamp", Handler: "on"},
		{From: "lamp", Event: "lit", To: "lamp", Handler: "glow"},
	}
	if err := ed.DeleteObject(inst); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{{From: "lamp", Event: "lit", To: "lamp", Handler: "glow"}}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after deleting %s = %+v, want %+v", inst, ed.Doc.Connections, want)
	}
}

func TestSetComponentRegistryChecksTheOpenScenesConnections(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	for _, o := range []sceneformat.ObjectDoc{
		{ID: "button", Components: map[string]any{"button": map[string]any{}}},
		{ID: "door", Components: map[string]any{"door": map[string]any{}}},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{{From: "button", Event: "pressed", To: "door", Handler: "open"}}

	// physicsComponents covers the components testSceneDoc's objects already have.
	registry := func(handlers ...sceneformat.Handler) *sceneformat.ComponentRegistry {
		reg := physicsComponents(t)
		for _, ct := range []sceneformat.ComponentType{
			{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}},
			{Name: "door", Handlers: handlers},
		} {
			if err := reg.Register(ct); err != nil {
				t.Fatalf("Register %s: %v", ct.Name, err)
			}
		}
		return reg
	}

	var connErr *sceneformat.ConnectionError
	if err := ed.SetComponentRegistry(registry()); !errors.As(err, &connErr) || connErr.Field != "handler" {
		t.Fatalf("SetComponentRegistry with a registry lacking door.open = %v, want a *ConnectionError on the handler", err)
	}
	if ed.ComponentRegistry() != nil {
		t.Fatal("a refused registry was set")
	}
	reg := registry(sceneformat.Handler{Name: "open"})
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if err := ed.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML of the saved scene with the same registry: %v", err)
	}
}

type doorCallRecord struct {
	calls  int
	speeds []float64
	locked []bool
}

func buttonDoorEditor(t *testing.T) (*editor.Editor, *sceneformat.ComponentRegistry, *doorCallRecord) {
	t.Helper()
	ed := newTestEditor(t)
	rec := &doorCallRecord{}
	reg := physicsComponents(t)
	for _, ct := range []sceneformat.ComponentType{
		{
			Name:   "button",
			Events: []sceneformat.Event{{Name: "pressed", Doc: "The player pressed the button."}},
		},
		{
			Name:   "switch",
			Events: []sceneformat.Event{{Name: "pressed", Doc: "The switch was toggled."}},
		},
		{
			Name:   "door",
			Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}},
			Handlers: []sceneformat.Handler{
				{
					Name: "open",
					Doc:  "Opens the door.",
					Params: []sceneformat.Field{
						{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(10), Step: 0.5, Doc: "Opening speed."},
					},
					Func: func(c sceneformat.Call) {
						r := c.Context.(*doorCallRecord)
						r.calls++
						r.speeds = append(r.speeds, c.Args["speed"].(float64))
						c.Target.Components["door"].(map[string]any)["open"] = true
					},
				},
				{
					Name: "lock",
					Doc:  "Locks or unlocks the door.",
					Params: []sceneformat.Field{
						{Name: "locked", Kind: sceneformat.FieldBool, Default: true},
					},
					Func: func(c sceneformat.Call) {
						r := c.Context.(*doorCallRecord)
						r.locked = append(r.locked, c.Args["locked"].(bool))
					},
				},
			},
		},
	} {
		if err := reg.Register(ct); err != nil {
			t.Fatalf("Register %s: %v", ct.Name, err)
		}
	}
	for _, o := range []sceneformat.ObjectDoc{
		{
			ID:         "button",
			X:          40,
			Y:          80,
			Sprite:     &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
			Components: map[string]any{"button": map[string]any{}},
		},
		{
			ID:         "door",
			X:          180,
			Y:          80,
			Sprite:     &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
			Components: map[string]any{"door": map[string]any{"open": false}},
		},
		{
			ID:     "wall",
			X:      260,
			Y:      80,
			Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
		},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	return ed, reg, rec
}

// #872 acceptance: connecting a button's pressed event to a door's open handler with one argument saves a connection that Connect runs, and Undo removes it (and Redo restores it).
func TestConnectionsPanelConnectRunsAndUndoes(t *testing.T) {
	ed, reg, rec := buttonDoorEditor(t)
	beforeYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML before: %v", err)
	}
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	if !app.Exists("id:inspector-conn-event-button-pressed") {
		t.Fatal("declared event button.pressed is not listed in the Connections section")
	}

	app.Press("id:inspector-conn-event-button-pressed-connect")
	app.Press("id:inspector-conn-target")
	app.Press("role:option door")
	app.Press("id:inspector-conn-handler-opt-open")

	if !app.Exists("id:inspector-conn-param-speed") {
		t.Fatal("typed param field for door.open's speed argument did not appear")
	}
	app.Clear("id:inspector-conn-param-speed")
	app.Type("id:inspector-conn-param-speed", "2.5")
	app.Submit("id:inspector-conn-param-speed")
	app.Press("id:inspector-conn-save")

	if app.Exists("id:inspector-conn-form") {
		t.Fatal("connect form stayed open after a valid connection")
	}
	if !app.Exists("id:inspector-conn-out-0") {
		t.Fatal("outgoing connection row did not appear after connecting")
	}
	want := []sceneformat.ConnectionDoc{{
		From:    "button",
		Event:   "pressed",
		To:      "door",
		Handler: "open",
		Args:    map[string]any{"speed": 2.5},
	}}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections = %+v, want %+v", ed.Doc.Connections, want)
	}

	afterYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after: %v", err)
	}
	parsed, err := sceneformat.ParseWith(afterYAML, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith saved scene: %v\n%s", err, afterYAML)
	}
	resaved, err := sceneformat.Encode(parsed)
	if err != nil || !bytes.Equal(resaved, afterYAML) {
		t.Fatalf("saved YAML did not round-trip byte-for-byte (%v):\n%s\nwant:\n%s", err, resaved, afterYAML)
	}
	signals, err := sceneformat.Connect(parsed, reg, rec)
	if err != nil {
		t.Fatalf("sceneformat.Connect: %v", err)
	}
	if err := signals.Emit("button", "pressed", nil); err != nil {
		t.Fatalf("signals.Emit: %v", err)
	}
	if rec.calls != 1 || !reflect.DeepEqual(rec.speeds, []float64{2.5}) {
		t.Fatalf("handler calls = %d, speeds = %v; want 1 call with speed 2.5", rec.calls, rec.speeds)
	}
	var doorOpen bool
	for _, l := range parsed.Layers {
		for _, o := range l.Objects {
			if o.ID == "door" {
				doorOpen, _ = o.Components["door"].(map[string]any)["open"].(bool)
			}
		}
	}
	if !doorOpen {
		t.Fatal("door.open handler did not set door component open=true")
	}

	// Undo removes the connection cleanly and restores the pre-connect YAML byte-for-byte.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if len(ed.Doc.Connections) != 0 || app.Exists("id:inspector-conn-out-0") {
		t.Fatalf("Undo did not remove the connection: %+v", ed.Doc.Connections)
	}
	undoneYAML, err := ed.SaveYAML()
	if err != nil || !bytes.Equal(undoneYAML, beforeYAML) {
		t.Fatalf("YAML after Undo differs from before (%v):\n%s\nwant:\n%s", err, undoneYAML, beforeYAML)
	}

	// Redo restores the connection cleanly.
	app.Post(func() { ed.Redo() })
	app.Settle()
	if !reflect.DeepEqual(ed.Doc.Connections, want) || !app.Exists("id:inspector-conn-out-0") {
		t.Fatalf("Redo did not restore the connection: %+v", ed.Doc.Connections)
	}
}

// #872 acceptance: a connection naming a handler the target lacks is refused before it reaches History, reporting the reason in Status.
func TestConnectionsRefusesInvalidConnectionsBeforeHistory(t *testing.T) {
	ed, _, _ := buttonDoorEditor(t)
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	beforeHist := ed.History.Len()

	// UI path: type a handler door lacks and press Connect.
	app.Press("id:inspector-conn-event-button-pressed-connect")
	app.Press("id:inspector-conn-target")
	app.Press("role:option door")
	app.Type("id:inspector-conn-handler", "explode")
	app.Press("id:inspector-conn-save")

	if ed.History.Len() != beforeHist {
		t.Fatalf("refused UI connection reached history: len = %d, want %d", ed.History.Len(), beforeHist)
	}
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("refused UI connection mutated Doc.Connections: %+v", ed.Doc.Connections)
	}
	if !strings.Contains(ed.Status, `declares handler "explode"`) {
		t.Fatalf("Status = %q, want refusal naming missing handler explode", ed.Status)
	}
	if !app.Exists("id:inspector-conn-form") {
		t.Fatal("form closed despite refusal; author should be able to correct it")
	}

	// Go API path (headless editor so subtests run cleanly on their own goroutines):
	// various ValidateConnections refusals must never reach History.
	edAPI, _, _ := buttonDoorEditor(t)
	apiHist := edAPI.History.Len()
	cases := []struct {
		name string
		conn sceneformat.ConnectionDoc
		want string
	}{
		{
			name: "missing handler on target",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "explode"},
			want: `declares handler "explode"`,
		},
		{
			name: "target with no handlers",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "wall", Handler: "open"},
			want: `declares handler "open"`,
		},
		{
			name: "wrong param type",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "open", Args: map[string]any{"speed": "fast"}},
			want: `want float`,
		},
		{
			name: "param out of range",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "open", Args: map[string]any{"speed": -2.0}},
			want: `below the minimum`,
		},
		{
			name: "unknown arg",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "open", Args: map[string]any{"bogus": 1}},
			want: `unknown argument`,
		},
		{
			name: "missing event on source",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "released", To: "door", Handler: "open"},
			want: `declares event "released"`,
		},
		{
			name: "missing target object",
			conn: sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "ghost", Handler: "open"},
			want: `names no object`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := edAPI.AddConnection(tc.conn)
			if err == nil {
				t.Fatalf("AddConnection succeeded (index %d), want error containing %q", idx, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("AddConnection error = %q, want substring %q", err.Error(), tc.want)
			}
			if !strings.Contains(edAPI.Status, tc.want) {
				t.Fatalf("Status = %q, want substring %q", edAPI.Status, tc.want)
			}
			if edAPI.History.Len() != apiHist {
				t.Fatalf("history len grew to %d after refused AddConnection", edAPI.History.Len())
			}
		})
	}

	// Add one valid connection, then verify duplicate add and invalid UpdateConnection are also refused before History.
	if _, err := edAPI.AddConnection(sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "open"}); err != nil {
		t.Fatalf("valid AddConnection: %v", err)
	}
	afterValid := edAPI.History.Len()

	var connErr *sceneformat.ConnectionError
	if _, err := edAPI.AddConnection(sceneformat.ConnectionDoc{From: "button", Event: "button.pressed", To: "door", Handler: "door.open"}); !errors.As(err, &connErr) {
		t.Fatalf("duplicate canonical AddConnection = %v, want *ConnectionError", err)
	}
	if err := edAPI.UpdateConnection(0, sceneformat.ConnectionDoc{From: "button", Event: "pressed", To: "door", Handler: "nope"}); !errors.As(err, &connErr) || connErr.Field != "handler" {
		t.Fatalf("invalid UpdateConnection = %v, want *ConnectionError on handler", err)
	}
	if edAPI.History.Len() != afterValid {
		t.Fatalf("refused duplicate/update grew history to %d, want %d", edAPI.History.Len(), afterValid)
	}
	if edAPI.Doc.Connections[0].Handler != "open" {
		t.Fatalf("refused UpdateConnection mutated connection: %+v", edAPI.Doc.Connections[0])
	}
}

func TestConnectionsPickTargetFromViewportAndSceneTree(t *testing.T) {
	ed, _, _ := buttonDoorEditor(t)
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	// 1. Pick target from 2D viewport.
	app.Press("id:inspector-conn-event-button-pressed-connect")
	app.Press("id:inspector-conn-pick-target")
	if !ed.PickingConnectionTarget() {
		t.Fatal("expected PickingConnectionTarget to be true after pressing pick button")
	}

	// Press on door in the 2D viewport.
	doorX, doorY := ed.WorldToCanvas(180, 80)
	app.Post(func() { ed.HandleCanvasPress(doorX, doorY) })
	app.Settle()

	if ed.PickingConnectionTarget() {
		t.Fatal("PickingConnectionTarget stayed true after picking door in viewport")
	}
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID changed to %q during target pick, want button", ed.SelectedID)
	}
	if !app.Exists("id:inspector-conn-handler-opt-open") {
		t.Fatal("picking door in 2D viewport did not populate door's handlers")
	}
	app.Press("id:inspector-conn-handler-opt-open")
	app.Press("id:inspector-conn-save")
	if len(ed.Doc.Connections) != 1 || ed.Doc.Connections[0].To != "door" {
		t.Fatalf("connections after viewport pick = %+v", ed.Doc.Connections)
	}

	// 2. Pick target from the Scene Tree.
	app.Post(func() { ed.Undo() })
	app.Settle()
	app.Press("id:inspector-conn-event-button-pressed-connect")
	app.Press("id:inspector-conn-pick-target")
	app.Press("id:scene-tree-label-door")

	if ed.PickingConnectionTarget() {
		t.Fatal("PickingConnectionTarget stayed true after picking door in scene tree")
	}
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID changed to %q during scene-tree pick, want button", ed.SelectedID)
	}
	if !app.Exists("id:inspector-conn-handler-opt-open") {
		t.Fatal("picking door in scene tree did not populate door's handlers")
	}
	app.Press("id:inspector-conn-handler-opt-open")
	app.Press("id:inspector-conn-save")
	if len(ed.Doc.Connections) != 1 || ed.Doc.Connections[0].To != "door" {
		t.Fatalf("connections after scene-tree pick = %+v", ed.Doc.Connections)
	}

	// 3. Cancel pick mode via Escape key and via CancelConnectionPick.
	app.Press("id:inspector-conn-new")
	app.Press("id:inspector-conn-pick-target")
	if !ed.PickingConnectionTarget() {
		t.Fatal("expected PickingConnectionTarget to be true")
	}
	app.Key("escape")
	if ed.PickingConnectionTarget() {
		t.Fatal("Escape did not exit pick mode")
	}
	app.Press("id:inspector-conn-pick-target")
	if !ed.PickingConnectionTarget() {
		t.Fatal("expected PickingConnectionTarget to be true")
	}
	app.Post(func() { ed.CancelConnectionPick() })
	app.Settle()
	if ed.PickingConnectionTarget() {
		t.Fatal("CancelConnectionPick did not exit pick mode")
	}

	// 4. 3D viewport target pick.
	var picked3D string
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
		ed.Camera3D.Target = [3]float32{180, 80, 0}
		ed.Camera3D.Distance = 200
		ed.BeginConnectionPick(func(id string) { picked3D = id })
		sx, sy, _, vis := ed.Camera3D.Project([3]float32{180, 80, 0}, 640, 360)
		if vis {
			ed.HandleViewport3DPress(sx, sy, 640, 360)
		}
	})
	app.Settle()
	if picked3D != "door" {
		t.Fatalf("3D viewport pick = %q, want door", picked3D)
	}
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID after 3D pick = %q, want button", ed.SelectedID)
	}
}

func TestConnectionsEditOnceRemoveAndJump(t *testing.T) {
	ed, reg, rec := buttonDoorEditor(t)
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "button",
		Event:   "pressed",
		To:      "door",
		Handler: "open",
		Args:    map[string]any{"speed": 1.5},
	}); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	// Edit outgoing connection: change speed to 4.0 and enable once.
	app.Press("id:inspector-conn-out-0-edit")
	app.Clear("id:inspector-conn-param-speed")
	app.Type("id:inspector-conn-param-speed", "4")
	app.Submit("id:inspector-conn-param-speed")
	app.Press("id:inspector-conn-once")
	app.Press("id:inspector-conn-save")

	got := ed.Doc.Connections[0]
	if !got.Once || got.Args["speed"] != 4.0 {
		t.Fatalf("edited connection = %+v, want Once=true and speed=4.0", got)
	}
	// Verify once behavior through sceneformat.Connect.
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	parsed, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	signals, err := sceneformat.Connect(parsed, reg, rec)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = signals.Emit("button", "pressed", nil)
	_ = signals.Emit("button", "pressed", nil)
	if rec.calls != 1 || !reflect.DeepEqual(rec.speeds, []float64{4.0}) {
		t.Fatalf("once connection ran %d times (%v), want 1 time with speed 4.0", rec.calls, rec.speeds)
	}

	// Jump from button (outgoing) -> door.
	app.Press("id:inspector-conn-out-0-jump")
	if ed.SelectedID != "door" {
		t.Fatalf("SelectedID after jumping from outgoing = %q, want door", ed.SelectedID)
	}
	if !app.Exists("id:inspector-conn-in-0") {
		t.Fatal("incoming connection row is not shown on door")
	}

	// Edit from incoming row: switch handler from open to lock (stale speed arg must be dropped).
	app.Press("id:inspector-conn-in-0-edit")
	app.Press("id:inspector-conn-handler-opt-lock")
	app.Press("id:inspector-conn-save")
	if ed.Doc.Connections[0].Handler != "lock" || ed.Doc.Connections[0].Args != nil {
		t.Fatalf("connection after switching handler = %+v, want handler=lock and nil Args", ed.Doc.Connections[0])
	}
	// Undo switches handler back to open with speed=4.0.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if ed.Doc.Connections[0].Handler != "open" || ed.Doc.Connections[0].Args["speed"] != 4.0 {
		t.Fatalf("connection after undoing handler switch = %+v", ed.Doc.Connections[0])
	}

	// Jump from door (incoming) -> button.
	app.Press("id:inspector-conn-in-0-jump")
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID after jumping from incoming = %q, want button", ed.SelectedID)
	}

	// Remove via UI, Undo puts it back, Redo removes it again.
	app.Press("id:inspector-conn-out-0-remove")
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("connections after remove = %+v, want empty", ed.Doc.Connections)
	}
	app.Post(func() { ed.Undo() })
	app.Settle()
	if len(ed.Doc.Connections) != 1 || ed.Doc.Connections[0].Handler != "open" {
		t.Fatalf("connections after undo remove = %+v", ed.Doc.Connections)
	}
	app.Post(func() { ed.Redo() })
	app.Settle()
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("connections after redo remove = %+v", ed.Doc.Connections)
	}
}

func TestConnectionsQualifiesAmbiguousEventsAndRespectsPlayMode(t *testing.T) {
	ed, _, _ := buttonDoorEditor(t)
	// Add a second component ("switch") that also declares "pressed" onto button.
	if err := ed.AddComponent("button", "switch"); err != nil {
		t.Fatalf("AddComponent switch: %v", err)
	}
	events := ed.ObjectEvents("button")
	wantRefs := []string{"button.pressed", "switch.pressed"}
	gotRefs := make([]string, len(events))
	for i, ev := range events {
		gotRefs[i] = ev.Ref
	}
	if !reflect.DeepEqual(gotRefs, wantRefs) {
		t.Fatalf("ObjectEvents(button) refs = %v, want %v", gotRefs, wantRefs)
	}
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "button",
		Event:   events[0].Ref,
		To:      "door",
		Handler: "open",
	}); err != nil {
		t.Fatalf("AddConnection with qualified ref: %v", err)
	}

	// Play mode refuses all connection mutations with ErrPlaying.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{From: "button", Event: "switch.pressed", To: "door", Handler: "open"}); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("AddConnection while playing = %v, want ErrPlaying", err)
	}
	if err := ed.UpdateConnection(0, sceneformat.ConnectionDoc{From: "button", Event: "button.pressed", To: "door", Handler: "lock"}); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("UpdateConnection while playing = %v, want ErrPlaying", err)
	}
	if err := ed.RemoveConnection(0); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("RemoveConnection while playing = %v, want ErrPlaying", err)
	}
	ed.BeginConnectionPick(func(string) {})
	if ed.PickingConnectionTarget() {
		t.Fatal("BeginConnectionPick activated while playing")
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}

func TestConnectionsWithoutRegistryAndJumpAPI(t *testing.T) {
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{{ID: "lever"}, {ID: "gate"}} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	idx, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "lever",
		Event:   "pulled",
		To:      "gate",
		Handler: "raise",
		Args:    map[string]any{"height": 3},
	})
	if err != nil || idx != 0 {
		t.Fatalf("AddConnection without registry = (%d, %v), want (0, nil)", idx, err)
	}
	out, in := ed.ObjectConnections("lever")
	if len(out) != 1 || len(in) != 0 {
		t.Fatalf("ObjectConnections(lever) = (%d out, %d in), want (1, 0)", len(out), len(in))
	}
	ed.SelectObject("lever")
	if !ed.JumpToConnectionOtherEnd(0) || ed.SelectedID != "gate" {
		t.Fatalf("JumpToConnectionOtherEnd from lever: SelectedID = %q, want gate", ed.SelectedID)
	}
	if !ed.JumpToConnectionOtherEnd(0) || ed.SelectedID != "lever" {
		t.Fatalf("JumpToConnectionOtherEnd from gate: SelectedID = %q, want lever", ed.SelectedID)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))
	if !app.Exists("id:inspector-conn-no-registry") {
		t.Fatal("expected no-registry hint when no component registry is set")
	}
	if !app.Exists("id:inspector-conn-out-0") {
		t.Fatal("expected existing outgoing connection to still be shown without a registry")
	}
	app.Press("id:inspector-conn-out-0-edit")
	app.Clear("id:inspector-conn-param-height")
	app.Type("id:inspector-conn-param-height", "5")
	app.Submit("id:inspector-conn-param-height")
	app.Press("id:inspector-conn-save")
	if got := ed.Doc.Connections[0].Args["height"]; got != 5 {
		t.Fatalf("edited height arg without registry = %#v, want 5", got)
	}
}

func TestConnectionsPickTargetWorksWithPaintTileToolAndIgnoresLayerRow(t *testing.T) {
	ed, _, _ := buttonDoorEditor(t)
	// Place door over empty tile (1, 1) at world (24, 24) on the floor tilemap.
	if err := ed.MoveObject("door", 24, 24); err != nil {
		t.Fatalf("MoveObject door: %v", err)
	}
	ed.SelectObject("button")
	ed.SetActiveLayer("floor")
	ed.SetActiveTileID(2)
	ed.SetTool(editor.ToolPaintTile)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	app.Press("id:inspector-conn-event-button-pressed-connect")
	app.Press("id:inspector-conn-pick-target")
	if !ed.PickingConnectionTarget() {
		t.Fatal("expected PickingConnectionTarget to be true")
	}

	// Clicking a layer row in the scene tree while picking must not clear SelectedID or cancel pick mode.
	app.Press("id:scene-tree-layer-floor")
	if !ed.PickingConnectionTarget() {
		t.Fatal("clicking a layer row cancelled target pick mode")
	}
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID = %q after clicking layer row while picking, want button", ed.SelectedID)
	}

	// Click door in the viewport while ToolPaintTile is active: both mouse down/up and PressAt must pick door instead of painting tile (1, 1).
	px, py := ed.WorldToCanvas(24, 24)
	pt := core.Point{X: core.Points(px), Y: core.Points(py)}
	app.MouseDownAt("id:editor-viewport-frame", pt, core.MouseButtonLeft)
	app.MouseUpAt("id:editor-viewport-frame", pt, core.MouseButtonLeft)
	app.PressAt("id:editor-viewport", pt)

	if ed.PickingConnectionTarget() {
		t.Fatal("PickingConnectionTarget stayed true after clicking door with ToolPaintTile active")
	}
	if ed.SelectedID != "button" {
		t.Fatalf("SelectedID = %q after viewport pick with ToolPaintTile, want button", ed.SelectedID)
	}
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 0 {
		t.Fatalf("tile (1,1) = %d after picking target with ToolPaintTile, want 0 (unpainted)", got)
	}
	if !app.Exists("id:inspector-conn-handler-opt-open") {
		t.Fatal("picking door with ToolPaintTile active did not populate door's handlers")
	}

	// Also verify the OnPress tile-stroke fallback (without prior mouse down/up) picks instead of painting.
	app.Press("id:inspector-conn-pick-target")
	app.PressAt("id:editor-viewport", pt)
	if ed.PickingConnectionTarget() {
		t.Fatal("PickingConnectionTarget stayed true after PressAt fallback with ToolPaintTile")
	}
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 0 {
		t.Fatalf("tile (1,1) = %d after PressAt fallback while picking, want 0", got)
	}
}

func TestConnectionsPrunesStaleArgsOnHandlerTypeAndTargetSwitch(t *testing.T) {
	ed, reg, _ := buttonDoorEditor(t)
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "lamp",
		Handlers: []sceneformat.Handler{{Name: "toggle", Doc: "Toggles the lamp."}},
	}); err != nil {
		t.Fatalf("Register lamp: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:         "lamp",
		X:          220,
		Y:          80,
		Sprite:     &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
		Components: map[string]any{"lamp": map[string]any{}},
	}); err != nil {
		t.Fatalf("PlaceObject lamp: %v", err)
	}
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "button",
		Event:   "pressed",
		To:      "door",
		Handler: "open",
		Args:    map[string]any{"speed": 2.5},
	}); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	// 1. Typing a different handler into the text input (instead of clicking a chip) must prune stale "speed".
	app.Press("id:inspector-conn-out-0-edit")
	app.Clear("id:inspector-conn-handler")
	app.Type("id:inspector-conn-handler", "lock")
	app.Press("id:inspector-conn-save")

	if app.Exists("id:inspector-conn-form") {
		t.Fatalf("form stayed open after typing handler lock (Status=%q)", ed.Status)
	}
	if got := ed.Doc.Connections[0]; got.Handler != "lock" || len(got.Args) != 0 {
		t.Fatalf("connection after typing handler = %+v, want handler=lock and empty Args", got)
	}

	// Undo back to open(speed=2.5), then switch target via dropdown to lamp and pick toggle.
	app.Post(func() { ed.Undo() })
	app.Settle()
	app.Press("id:inspector-conn-out-0-edit")
	app.Press("id:inspector-conn-target")
	app.Press("role:option lamp")
	app.Press("id:inspector-conn-handler-opt-toggle")
	app.Press("id:inspector-conn-save")

	if app.Exists("id:inspector-conn-form") {
		t.Fatalf("form stayed open after switching target via dropdown (Status=%q)", ed.Status)
	}
	if got := ed.Doc.Connections[0]; got.To != "lamp" || got.Handler != "toggle" || len(got.Args) != 0 {
		t.Fatalf("connection after target dropdown switch = %+v, want lamp.toggle with empty Args", got)
	}

	// Undo back to open(speed=2.5), then switch target via viewport/scene-tree pick to lamp and type toggle.
	app.Post(func() { ed.Undo() })
	app.Settle()
	app.Press("id:inspector-conn-out-0-edit")
	app.Press("id:inspector-conn-pick-target")
	app.Press("id:scene-tree-label-lamp")
	app.Clear("id:inspector-conn-handler")
	app.Type("id:inspector-conn-handler", "toggle")
	app.Press("id:inspector-conn-save")

	if app.Exists("id:inspector-conn-form") {
		t.Fatalf("form stayed open after switching target via pick (Status=%q)", ed.Status)
	}
	if got := ed.Doc.Connections[0]; got.To != "lamp" || got.Handler != "toggle" || len(got.Args) != 0 {
		t.Fatalf("connection after target pick switch = %+v, want lamp.toggle with empty Args", got)
	}
}

func TestConnectionsEditFormClosesWhenEditedConnectionBecomesStale(t *testing.T) {
	ed, _, _ := buttonDoorEditor(t)
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "button",
		Event:   "pressed",
		To:      "door",
		Handler: "open",
	}); err != nil {
		t.Fatalf("AddConnection 0: %v", err)
	}
	if _, err := ed.AddConnection(sceneformat.ConnectionDoc{
		From:    "button",
		Event:   "pressed",
		To:      "door",
		Handler: "lock",
	}); err != nil {
		t.Fatalf("AddConnection 1: %v", err)
	}
	ed.SelectObject("button")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 1600))

	// Open edit on index 0, then remove index 0 externally so index 1 shifts into index 0.
	app.Press("id:inspector-conn-out-0-edit")
	if !app.Exists("id:inspector-conn-form") {
		t.Fatal("edit form did not open")
	}
	app.Post(func() { _ = ed.RemoveConnection(0) })
	app.Settle()
	if app.Exists("id:inspector-conn-form") {
		t.Fatal("edit form stayed open after another connection shifted into its index")
	}
}
