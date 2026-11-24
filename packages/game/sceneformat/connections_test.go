package sceneformat_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// doorScene is written exactly as Encode writes it after the schema comment line, so a load and save must reproduce it byte for byte.
const doorScene = `version: 1
name: Hall
layers:
  - name: props
    objects:
      - id: button
        x: 40
        components:
          button: {}
      - id: door
        x: 120
        components:
          door: {}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      speed: 2.5
`

// doorRegistry registers the button and door component types of doorScene. The door's open handler sets its open field and records each call in calls.
func doorRegistry(t testing.TB, calls *[]sceneformat.Call) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "button",
		Doc:    "Something the player presses.",
		Events: []sceneformat.Event{{Name: "pressed", Doc: "The player pressed it."}, {Name: "released"}},
	}); err != nil {
		t.Fatalf("Register button: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "door",
		Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}},
		Events: []sceneformat.Event{{Name: "opened"}},
		Handlers: []sceneformat.Handler{
			{
				Name:   "open",
				Doc:    "Swings the door open.",
				Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}, {Name: "sound", Kind: sceneformat.FieldString, Default: "creak"}},
				Func: func(c sceneformat.Call) {
					c.Target.Components[c.Component].(map[string]any)["open"] = true
					*calls = append(*calls, c)
				},
			},
			{Name: "close", Func: func(c sceneformat.Call) { *calls = append(*calls, c) }},
		},
	}); err != nil {
		t.Fatalf("Register door: %v", err)
	}
	return reg
}

func doorOpen(doc *sceneformat.SceneDoc) bool {
	for li := range doc.Layers {
		for _, o := range doc.Layers[li].Objects {
			if o.ID == "door" {
				open, _ := o.Components["door"].(map[string]any)["open"].(bool)
				return open
			}
		}
	}
	return false
}

// The issue's acceptance: a button that opens a door, connected in the scene file, with no Go code that names either object.
func TestButtonOpensDoorDeclaredInYAML(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(doorScene), sceneformat.ParseOptions{File: "hall.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := sceneformat.SchemaComment + "\n" + doorScene; string(out) != want {
		t.Fatalf("round trip changed the scene:\n%s\n--- want ---\n%s", out, want)
	}

	type game struct{ name string }
	ctx := &game{name: "hall"}
	sig, err := sceneformat.Connect(doc, reg, ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if doorOpen(doc) {
		t.Fatal("the door is open before the button is pressed")
	}
	if err := sig.Emit("button", "released", nil); err != nil || len(calls) != 0 {
		t.Fatalf("an unconnected event: err %v, %d calls", err, len(calls))
	}
	if err := sig.Emit("button", "pressed", "payload"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !doorOpen(doc) {
		t.Fatal("pressing the button did not open the door")
	}
	if len(calls) != 1 {
		t.Fatalf("%d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Signals != sig || c.Context != ctx || c.Source.ID != "button" || c.SourceComponent != "button" || c.Event != "pressed" ||
		c.Target.ID != "door" || c.Component != "door" || c.Handler != "open" || c.Payload != "payload" {
		t.Fatalf("call = %+v", c)
	}
	if want := map[string]any{"speed": 2.5, "sound": "creak"}; !reflect.DeepEqual(c.Args, want) {
		t.Fatalf("Args = %v, want %v", c.Args, want)
	}
	c.Args["speed"] = 9.0
	if err := sig.Emit("button", "button.pressed", nil); err != nil {
		t.Fatalf("Emit qualified: %v", err)
	}
	if len(calls) != 2 || calls[1].Args["speed"] != 2.5 {
		t.Fatalf("a handler's change to its Args reached the next call: %v", calls[1].Args)
	}
}

func TestConnectionsRunInOrderAndOnce(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: props
    objects:
      - id: button
        components: {button: {}}
      - id: door
        components: {door: {}}
      - id: gate
        components: {door: {}}
connections:
  - {from: button, event: pressed, to: gate, handler: open, once: true}
  - {from: button, event: pressed, to: door, handler: close}
  - {from: button, event: pressed, to: door, handler: door.open}
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	for range 2 {
		if err := sig.Emit("button", "pressed", nil); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, c := range calls {
		got = append(got, c.Target.ID+"."+c.Handler)
	}
	if want := []string{"gate.open", "door.close", "door.open", "door.close", "door.open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func TestHandlersEmitFurtherEventsUpToADepth(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	var depth int
	var deepest error
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "relay",
		Events: []sceneformat.Event{{Name: "ping"}},
		Handlers: []sceneformat.Handler{{Name: "pass", Func: func(c sceneformat.Call) {
			depth++
			if err := c.Signals.Emit(c.Target.ID, "ping", nil); err != nil {
				deepest = err
			}
		}}},
	})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: a
    objects:
      - {id: r, components: {relay: {}}}
connections:
  - {from: r, event: ping, to: r, handler: pass}
`))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("r", "ping", nil); err != nil {
		t.Fatalf("outer Emit: %v", err)
	}
	if depth != sceneformat.MaxEmitDepth || deepest == nil || !strings.Contains(deepest.Error(), "deeper than") {
		t.Fatalf("depth %d, deepest error %v", depth, deepest)
	}
	// The depth unwinds, so the next emit runs as deep again.
	depth, deepest = 0, nil
	if err := sig.Emit("r", "ping", nil); err != nil || depth != sceneformat.MaxEmitDepth {
		t.Fatalf("second Emit: %v, depth %d", err, depth)
	}
}

func TestEmitRefusesUnknownObjectsAndEvents(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	reg.MustRegister(sceneformat.ComponentType{Name: "lever", Events: []sceneformat.Event{{Name: "pressed"}}})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: a
    objects:
      - {id: both, components: {button: {}, lever: {}}}
      - {id: door, components: {door: {}}}
      - {id: plain}
connections:
  - {from: both, event: lever.pressed, to: door, handler: open}
`))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ from, event, want string }{
		{"ghost", "pressed", `no object "ghost"`},
		{"plain", "pressed", `no registered component of object "plain"`},
		{"door", "pressed", `no registered component of object "door"`},
		{"both", "pressed", "name it as component.pressed"},
		{"both", "lever.missing", `no registered component of object "both"`},
	} {
		if err := sig.Emit(tc.from, tc.event, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Emit(%q, %q) = %v, want an error containing %q", tc.from, tc.event, err, tc.want)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("a refused Emit called %d handlers", len(calls))
	}
	if err := sig.Emit("both", "button.pressed", nil); err != nil || len(calls) != 0 {
		t.Fatalf("the button's unconnected event: %v, %d calls", err, len(calls))
	}
	if err := sig.Emit("both", "lever.pressed", nil); err != nil || len(calls) != 1 {
		t.Fatalf("the lever's connected event: %v, %d calls", err, len(calls))
	}
	var nilSig *sceneformat.Signals
	if err := nilSig.Emit("both", "pressed", nil); err == nil {
		t.Fatal("Emit on nil Signals succeeded")
	}
}

func TestParseWithLocatesConnectionErrors(t *testing.T) {
	reg := doorRegistry(t, new([]sceneformat.Call))
	reg.MustRegister(sceneformat.ComponentType{Name: "lever", Events: []sceneformat.Event{{Name: "pressed"}}})
	head := `version: 1
layers:
  - name: a
    objects:
      - {id: button, components: {button: {}, lever: {}}}
      - {id: door, components: {door: {}}}
      - {id: wall}
connections:
`
	for _, tc := range []struct {
		name, conn  string
		line, col   int
		field, arg  string
		wantInError string
	}{
		{"unknown event", "  - {from: door, event: pressed, to: door, handler: open}", 9, 25, "event", "", `declares event "pressed" (it has: door.opened)`},
		{"no events", "  - {from: wall, event: pressed, to: door, handler: open}", 9, 25, "event", "", `(it has no events)`},
		{"ambiguous event", "  - {from: button, event: pressed, to: door, handler: open}", 9, 27, "event", "", "write one of button.pressed, lever.pressed"},
		{"unknown handler", "  - {from: button, event: button.pressed, to: door, handler: shut}", 9, 62, "handler", "", `(it has: door.open, door.close)`},
		{"missing component", "  - {from: button, event: door.opened, to: door, handler: open}", 9, 27, "event", "", `object "button" has no component "door"`},
		{"undeclared qualified", "  - {from: door, event: door.closed, to: door, handler: open}", 9, 25, "event", "", `component type "door" declares no event "closed"`},
		{"unknown argument", "  - {from: button, event: lever.pressed, to: door, handler: open, args: {sped: 2}}", 9, 80, "args", "sped", "unknown argument (params: speed, sound)"},
		{"no params", "  - {from: button, event: lever.pressed, to: door, handler: close, args: {x: 1}}", 9, 78, "args", "x", `handler "close" has no params`},
		{"wrong kind", "  - {from: button, event: lever.pressed, to: door, handler: open, args: {sound: 3}}", 9, 81, "args", "sound", "want string, got int 3"},
		{"out of range", "  - {from: button, event: lever.pressed, to: door, handler: open, args: {speed: -1}}", 9, 81, "args", "speed", "below the minimum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.ParseWith([]byte(head+tc.conn+"\n"), sceneformat.ParseOptions{File: "hall.scene.yaml", Components: reg})
			var ce *sceneformat.ConnectionError
			if !errors.As(err, &ce) {
				t.Fatalf("ParseWith = %v, want a *ConnectionError", err)
			}
			if ce.File != "hall.scene.yaml" || ce.Line != tc.line || ce.Column != tc.col || ce.Index != 0 || ce.Field != tc.field || ce.Arg != tc.arg {
				t.Errorf("error at %s:%d:%d field %q arg %q, want line %d col %d field %q arg %q", ce.File, ce.Line, ce.Column, ce.Field, ce.Arg, tc.line, tc.col, tc.field, tc.arg)
			}
			if !strings.Contains(err.Error(), tc.wantInError) || !strings.HasPrefix(err.Error(), fmt.Sprintf("hall.scene.yaml:%d:%d: sceneformat: connection 0 (", ce.Line, ce.Column)) {
				t.Errorf("error %q, want it to contain %q", err, tc.wantInError)
			}
		})
	}
	// Without a registry the same files load: names are only checked for their form.
	if _, err := sceneformat.Parse([]byte(head + "  - {from: door, event: pressed, to: door, handler: open}\n")); err != nil {
		t.Fatalf("Parse without a registry: %v", err)
	}
	// Two spellings of one connection are the same connection.
	_, err := sceneformat.ParseWith([]byte(head+"  - {from: door, event: opened, to: door, handler: open}\n  - {from: door, event: door.opened, to: door, handler: door.open}\n"), sceneformat.ParseOptions{Components: reg})
	if err == nil || !strings.Contains(err.Error(), "same event and handler as connection 0") {
		t.Fatalf("duplicate connection: %v", err)
	}
}

func TestValidateRefusesMalformedConnections(t *testing.T) {
	head := `version: 1
layers:
  - name: a
    objects:
      - id: b
connections:
`
	for _, tc := range []struct{ conn, want string }{
		{"  - {event: e, to: b, handler: h}", "connection 0 has an empty from"},
		{"  - {from: b, to: b, handler: h}", "connection 0 has an empty event"},
		{"  - {from: b, event: e, handler: h}", "connection 0 has an empty to"},
		{"  - {from: b, event: e, to: b}", "connection 0 has an empty handler"},
		{"  - {from: b, event: a.b.c, to: b, handler: h}", `event "a.b.c" must be a name or component.name`},
		{"  - {from: b, event: .e, to: b, handler: h}", `event ".e" must be a name or component.name`},
		{"  - {from: b, event: e, to: b, handler: h.}", `handler "h." must be a name or component.name`},
		{"  - {from: x, event: e, to: b, handler: h}", `from "x" names no object of the scene`},
		{"  - {from: b, event: e, to: x, handler: h}", `to "x" names no object of the scene`},
		{"  - {from: b, event: e, to: b, handler: h, args: {a: [1]}}", `argument "a" must be a bool, a finite number or a string, got a sequence`},
		{"  - {from: b, event: e, to: b, handler: h, args: {a: {k: 1}}}", `argument "a" must be a bool, a finite number or a string, got a mapping`},
		{"  - {from: b, event: e, to: b, handler: h, args: {a: null}}", `got null`},
		{"  - {from: b, event: e, to: b, handler: h, args: {a: .nan}}", `got float NaN`},
		{"  - {from: b, event: e, to: b, handler: h, args: {\"\": 1}}", "argument with an empty name"},
		{"  - {from: b, event: e, to: b, handler: h}\n  - {from: b, event: e, to: b, handler: h}", "connection 1 repeats connection 0"},
		{"  - {from: b, event: e, to: b, handler: h, delay: 1}", `unknown field "delay"`},
	} {
		_, err := sceneformat.Parse([]byte(head + tc.conn + "\n"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Parse = %v, want an error containing %q", tc.conn, err, tc.want)
		}
	}
	// Validate names the field at fault so tools can point at its line.
	doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "a", Objects: []sceneformat.ObjectDoc{{ID: "b"}}}}, Connections: []sceneformat.ConnectionDoc{{From: "b", Event: "e", To: "gone", Handler: "h"}}}
	var fe *sceneformat.FieldError
	if err := sceneformat.Validate(doc); !errors.As(err, &fe) || strings.Join(fe.Path, "/") != "connections/0/to" {
		t.Fatalf("Validate = %v, want a FieldError at connections/0/to", err)
	}
	if _, err := sceneformat.Encode(doc); err == nil {
		t.Fatal("Encode wrote a connection to a missing object")
	}
}

func TestConnectRefusesWhatItCannotRun(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Connect(doc, nil, nil); err == nil {
		t.Fatal("Connect without a registry succeeded")
	}
	if _, err := sceneformat.Connect(nil, reg, nil); err == nil {
		t.Fatal("Connect of a nil scene succeeded")
	}

	checkOnly := sceneformat.NewComponentRegistry()
	checkOnly.MustRegister(sceneformat.ComponentType{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}})
	checkOnly.MustRegister(sceneformat.ComponentType{Name: "door", Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}}, Handlers: []sceneformat.Handler{{Name: "open", Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat}}}}})
	if err := sceneformat.ValidateConnections(doc, checkOnly); err != nil {
		t.Fatalf("ValidateConnections with a registry without funcs: %v", err)
	}
	var ce *sceneformat.ConnectionError
	if _, err := sceneformat.Connect(doc, checkOnly, nil); !errors.As(err, &ce) || ce.Field != "handler" || !strings.Contains(err.Error(), `handler "open" of component type "door" has no Func`) {
		t.Fatalf("Connect to a handler without a Func = %v", err)
	}

	bad := doc.Clone()
	bad.Layers[0].Objects[1].Components["door"] = map[string]any{"open": "yes"}
	var cmpErr *sceneformat.ComponentError
	if _, err := sceneformat.Connect(bad, reg, nil); !errors.As(err, &cmpErr) {
		t.Fatalf("Connect of a scene with a bad component = %v, want a *ComponentError", err)
	}

	inst, err := sceneformat.Parse([]byte("version: 2\nlayers:\n  - name: a\n    objects:\n      - {id: d, scene: door.scene.yaml}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Connect(inst, reg, nil); err == nil || !strings.Contains(err.Error(), "Resolve it first") {
		t.Fatalf("Connect of an unresolved scene = %v", err)
	}
	if err := sceneformat.ValidateConnections(inst, reg); err == nil {
		t.Fatal("ValidateConnections of an unresolved scene succeeded")
	}
	odd := doc.Clone()
	odd.Layers[0].Objects[0].Components["mystery"] = map[string]any{}
	odd.Connections[0].Event = "mystery.boom"
	if err := sceneformat.ValidateConnections(odd, reg); !errors.As(err, &ce) || ce.Line != 0 || !strings.Contains(err.Error(), `component "mystery" of object "button" is not registered`) {
		t.Fatalf("ValidateConnections naming an unregistered component = %v", err)
	}
	if err := sceneformat.ValidateConnections(doc, nil); err != nil {
		t.Fatalf("ValidateConnections without a registry = %v", err)
	}
}

// A prefab can connect its own objects, and every instance of it keeps the connection between its own copies.
func TestResolveCarriesConnectionsOfInstancedAndInheritedScenes(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"doorway.scene.yaml": `version: 2
layers:
  - name: a
    objects:
      - id: button
        components: {button: {}}
      - id: door
        components: {door: {}}
connections:
  - {from: button, event: pressed, to: door, handler: open}
`,
		"base.scene.yaml": `version: 2
layers:
  - name: a
    objects:
      - id: west
        scene: doorway.scene.yaml
      - id: alarm
        components: {button: {}}
connections:
  - {from: alarm, event: pressed, to: west/door, handler: close}
`,
	})
	doc, err := sceneformat.Parse([]byte(`version: 2
inherits: base.scene.yaml
layers:
  - name: a
    objects:
      - id: east
        scene: doorway.scene.yaml
connections:
  - {from: alarm, event: pressed, to: east/door, handler: close}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	before, _ := sceneformat.Encode(doc)
	res, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	after, _ := sceneformat.Encode(doc)
	if !bytes.Equal(before, after) {
		t.Fatal("Resolve changed its input")
	}
	var got []string
	for _, c := range res.Connections {
		got = append(got, c.From+" "+c.Event+" -> "+c.To+" "+c.Handler)
	}
	want := []string{
		"west/button pressed -> west/door open",
		"alarm pressed -> west/door close",
		"east/button pressed -> east/door open",
		"alarm pressed -> east/door close",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved connections\n%v\nwant\n%v", got, want)
	}

	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	sig, err := sceneformat.Connect(res, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := sig.Emit("east/button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("alarm", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, c := range calls {
		got = append(got, c.Target.ID+" "+c.Handler)
	}
	if want := []string{"east/door open", "west/door close", "east/door close"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}

	// A connection to an object the instance does not hold fails once resolved.
	broken := strings.Replace(string(before), "to: east/door", "to: east/gate", 1)
	bdoc, err := sceneformat.Parse([]byte(broken))
	if err != nil {
		t.Fatalf("Parse leaves objects of other files to Resolve: %v", err)
	}
	if _, err := sceneformat.Resolve(bdoc, scenes); err == nil || !strings.Contains(err.Error(), `to "east/gate" names no object`) {
		t.Fatalf("Resolve of a connection to a missing object = %v", err)
	}
}

func TestCloneCopiesConnections(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		t.Fatal(err)
	}
	c := doc.Clone()
	if !reflect.DeepEqual(c, doc) {
		t.Fatal("Clone differs from its source")
	}
	c.Connections[0].Args["speed"] = 9.0
	c.Connections[0].To = "button"
	if doc.Connections[0].Args["speed"] != 2.5 || doc.Connections[0].To != "door" {
		t.Fatal("changing a clone's connection changed its source")
	}
}

func TestRegisterChecksEventsAndHandlers(t *testing.T) {
	for _, tc := range []struct {
		typ  sceneformat.ComponentType
		want string
	}{
		{sceneformat.ComponentType{Name: "c", Events: []sceneformat.Event{{}}}, `component type "c" event 0 has an empty name`},
		{sceneformat.ComponentType{Name: "c", Events: []sceneformat.Event{{Name: "a.b"}}}, `event "a.b" must not contain a dot`},
		{sceneformat.ComponentType{Name: "c", Events: []sceneformat.Event{{Name: "e"}, {Name: "e"}}}, `component type "c" has duplicate event "e"`},
		{sceneformat.ComponentType{Name: "c", Handlers: []sceneformat.Handler{{}}}, `component type "c" handler 0 has an empty name`},
		{sceneformat.ComponentType{Name: "c", Handlers: []sceneformat.Handler{{Name: "h"}, {Name: "h"}}}, `component type "c" has duplicate handler "h"`},
		{sceneformat.ComponentType{Name: "c", Handlers: []sceneformat.Handler{{Name: "h", Params: []sceneformat.Field{{Name: "p", Kind: sceneformat.FieldInt, Default: 2.5}}}}}, `component type "c" handler "h" param "p" default: want int`},
		{sceneformat.ComponentType{Name: "c", Handlers: []sceneformat.Handler{{Name: "h", Params: []sceneformat.Field{{Name: "p", Kind: sceneformat.FieldBool}, {Name: "p", Kind: sceneformat.FieldBool}}}}}, `handler "h" has duplicate param "p"`},
		{sceneformat.ComponentType{Name: "c", Handlers: []sceneformat.Handler{{Name: "h", Params: []sceneformat.Field{{Name: "p"}}}}}, `handler "h" param "p" has unknown kind`},
	} {
		if err := sceneformat.NewComponentRegistry().Register(tc.typ); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Register(%+v) = %v, want an error containing %q", tc.typ, err, tc.want)
		}
	}

	reg := doorRegistry(t, new([]sceneformat.Call))
	door, _ := reg.Lookup("door")
	if _, ok := door.Event("opened"); !ok {
		t.Fatal("Lookup lost the door's event")
	}
	h, ok := door.Handler("open")
	if !ok || h.Func == nil || h.Doc != "Swings the door open." {
		t.Fatalf("Handler(open) = %+v, %v", h, ok)
	}
	if p, ok := h.Param("speed"); !ok || p.Default != 1.0 || *p.Min != 0 {
		t.Fatalf("Param(speed) = %+v, %v", p, ok)
	}
	if p, _ := h.Param("sound"); p.Default != "creak" {
		t.Fatalf("Param(sound) default = %v", p.Default)
	}
	if _, ok := h.Param("missing"); ok {
		t.Fatal("Param found a param the handler does not have")
	}
	if _, ok := door.Handler("missing"); ok {
		t.Fatal("Handler found a handler the type does not have")
	}
	*door.Handlers[0].Params[0].Min = 5
	door.Handlers[0].Params[0].Name = "renamed"
	door.Events[0].Name = "renamed"
	again, _ := reg.Lookup("door")
	if p, ok := again.Handlers[0].Param("speed"); !ok || *p.Min != 0 {
		t.Fatal("mutating a Lookup copy changed a registered handler")
	}
	if _, ok := again.Event("opened"); !ok {
		t.Fatal("mutating a Lookup copy renamed a registered event")
	}
}

func FuzzParseWithConnections(f *testing.F) {
	f.Add([]byte(doorScene))
	f.Add([]byte(strings.Replace(doorScene, "pressed", "button.pressed", 1)))
	f.Add([]byte(strings.Replace(doorScene, "speed: 2.5", "speed: -1", 1)))
	f.Add([]byte(doorScene + "  - {from: door, event: opened, to: button, handler: open, once: true}\n"))
	reg := doorRegistry(f, new([]sceneformat.Call))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "fuzz.yaml", Components: reg})
		if err != nil {
			var ce *sceneformat.ConnectionError
			if errors.As(err, &ce) && (ce.File != "fuzz.yaml" || ce.Line <= 0) {
				t.Fatalf("connection error without a position: %v", err)
			}
			return
		}
		out, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed scene: %v", err)
		}
		again, err := sceneformat.ParseWith(out, sceneformat.ParseOptions{Components: reg})
		if err != nil {
			t.Fatalf("re-parse of encoded scene: %v\n%s", err, out)
		}
		out2, err := sceneformat.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable: %v\n%s\n---\n%s", err, out, out2)
		}
		if doc.RefersToScenes() {
			return
		}
		sig, err := sceneformat.Connect(doc, reg, nil)
		if err != nil {
			t.Fatalf("Connect of a scene ParseWith accepted: %v", err)
		}
		for _, c := range doc.Connections {
			if err := sig.Emit(c.From, c.Event, nil); err != nil {
				t.Fatalf("Emit of a connected event: %v", err)
			}
		}
	})
}

func BenchmarkEmit(b *testing.B) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}})
	n := 0
	reg.MustRegister(sceneformat.ComponentType{Name: "door", Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}}, Handlers: []sceneformat.Handler{{Name: "open", Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat}}, Func: func(sceneformat.Call) { n++ }}}})
	doc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		b.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := sig.Emit("button", "pressed", nil); err != nil {
			b.Fatal(err)
		}
	}
	if n == 0 {
		b.Fatal("no handler ran")
	}
}

func TestEnqueueAndFlushRunInOrderAfterStepWithoutRecursion(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	var trace []string
	active := 0
	maxActive := 0
	enter := func(label string) {
		active++
		if active > maxActive {
			maxActive = active
		}
		trace = append(trace, "enter:"+label)
	}
	exit := func(label string) {
		trace = append(trace, "exit:"+label)
		active--
	}
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "step",
		Events: []sceneformat.Event{{Name: "fire"}},
		Handlers: []sceneformat.Handler{
			{Name: "relay", Func: func(c sceneformat.Call) {
				label := fmt.Sprintf("%s(%v)", c.Target.ID, c.Payload)
				enter(label)
				// Enqueuing inside a handler must NOT recurse; it runs after the current wave's handlers return.
				if err := c.Signals.Enqueue(c.Target.ID, "fire", fmt.Sprintf("from-%s", c.Target.ID)); err != nil {
					t.Errorf("Enqueue inside handler: %v", err)
				}
				exit(label)
			}},
			{Name: "sink", Func: func(c sceneformat.Call) {
				label := fmt.Sprintf("%s(%v)", c.Target.ID, c.Payload)
				enter(label)
				exit(label)
			}},
		},
	})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: a
    objects:
      - {id: a, components: {step: {}}}
      - {id: b, components: {step: {}}}
      - {id: c, components: {step: {}}}
      - {id: d, components: {step: {}}}
connections:
  - {from: a, event: fire, to: b, handler: relay}
  - {from: b, event: fire, to: c, handler: sink}
  - {from: d, event: fire, to: c, handler: sink}
`))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Enqueue two events during a simulated step: nothing runs yet.
	if err := sig.Enqueue("a", "fire", 1); err != nil {
		t.Fatal(err)
	}
	if err := sig.Enqueue("d", "fire", 2); err != nil {
		t.Fatal(err)
	}
	if got := sig.Queued(); got != 2 {
		t.Fatalf("Queued() = %d, want 2", got)
	}
	if len(trace) != 0 {
		t.Fatalf("handlers ran before Flush: %v", trace)
	}
	if err := sig.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := sig.Queued(); got != 0 {
		t.Fatalf("Queued() after Flush = %d, want 0", got)
	}
	if maxActive != 1 {
		t.Fatalf("max concurrent handler depth = %d, want 1 (no recursion)", maxActive)
	}
	// Wave 1 runs a->b and d->c in enqueue order; wave 2 runs the event b enqueued (b->c) after wave 1 finishes.
	want := []string{
		"enter:b(1)", "exit:b(1)",
		"enter:c(2)", "exit:c(2)",
		"enter:c(from-b)", "exit:c(from-b)",
	}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("trace = %v, want %v", trace, want)
	}
	// Flushing an empty queue is a no-op.
	if err := sig.Flush(); err != nil {
		t.Fatalf("empty Flush: %v", err)
	}
}

func TestFlushGuardsSelfEnqueueLoopAndReentrantFlush(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	runs := 0
	maxActive := 0
	active := 0
	var nestedFlushErr error
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "relay",
		Events: []sceneformat.Event{{Name: "ping"}},
		Handlers: []sceneformat.Handler{{Name: "pass", Func: func(c sceneformat.Call) {
			active++
			if active > maxActive {
				maxActive = active
			}
			runs++
			if runs == 1 {
				nestedFlushErr = c.Signals.Flush()
			}
			if err := c.Signals.Enqueue(c.Target.ID, "ping", nil); err != nil {
				t.Errorf("Enqueue: %v", err)
			}
			active--
		}}},
	})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: a
    objects:
      - {id: r, components: {relay: {}}}
connections:
  - {from: r, event: ping, to: r, handler: pass}
`))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Enqueue("r", "ping", nil); err != nil {
		t.Fatal(err)
	}
	err = sig.Flush()
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("Flush on self-enqueue loop = %v, want wave limit error", err)
	}
	if runs != sceneformat.MaxFlushWaves {
		t.Fatalf("runs = %d, want %d", runs, sceneformat.MaxFlushWaves)
	}
	if maxActive != 1 {
		t.Fatalf("maxActive = %d, want 1 (no recursion)", maxActive)
	}
	if nestedFlushErr == nil || !strings.Contains(nestedFlushErr.Error(), "another Flush is running") {
		t.Fatalf("re-entrant Flush = %v, want refusal", nestedFlushErr)
	}
	if sig.Queued() != 0 {
		t.Fatalf("queue was not cleared after wave overflow: %d", sig.Queued())
	}
	// Signals recovers cleanly for subsequent Enqueue + Flush calls.
	runs = 0
	if err := sig.Enqueue("r", "ping", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err == nil || runs != sceneformat.MaxFlushWaves {
		t.Fatalf("second Flush: err=%v runs=%d", err, runs)
	}
}

func TestEnqueueAndFlushValidation(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	reg.MustRegister(sceneformat.ComponentType{Name: "lever", Events: []sceneformat.Event{{Name: "pressed"}}})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: a
    objects:
      - {id: both, components: {button: {}, lever: {}}}
      - {id: door, components: {door: {}}}
connections:
  - {from: both, event: lever.pressed, to: door, handler: open}
`))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ from, event, want string }{
		{"ghost", "pressed", `no object "ghost"`},
		{"door", "pressed", `no registered component of object "door"`},
		{"both", "pressed", "name it as component.pressed"},
	} {
		if err := sig.Enqueue(tc.from, tc.event, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Enqueue(%q, %q) = %v, want %q", tc.from, tc.event, err, tc.want)
		}
	}
	if sig.Queued() != 0 {
		t.Fatalf("refused Enqueue queued %d events", sig.Queued())
	}
	var nilSig *sceneformat.Signals
	if err := nilSig.Enqueue("both", "pressed", nil); err == nil {
		t.Fatal("Enqueue on nil Signals succeeded")
	}
	if err := nilSig.Flush(); err == nil {
		t.Fatal("Flush on nil Signals succeeded")
	}
	if nilSig.Queued() != 0 {
		t.Fatalf("nil Signals Queued() = %d", nilSig.Queued())
	}
}

func TestSpawnEntitiesWithSignalsDispatchesAndSkipsDespawned(t *testing.T) {
	type doorState struct {
		open  bool
		speed float64
		count int
	}
	reg := sceneformat.NewComponentRegistry()
	var calls []sceneformat.Call
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "door",
		Events: []sceneformat.Event{{Name: "opened"}},
		Handlers: []sceneformat.Handler{
			{
				Name:   "open",
				Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0}},
				Func: func(c sceneformat.Call) {
					calls = append(calls, c)
					if st, ok := entities.Get[doorState](c.World, c.TargetEntity); ok {
						st.open = true
						st.speed = c.Args["speed"].(float64)
						st.count++
					}
				},
			},
			{
				Name: "destroy_other",
				Func: func(c sceneformat.Call) {
					calls = append(calls, c)
					if other, ok := c.Signals.Entity("gate"); ok {
						c.World.Destroy(other)
					}
				},
			},
		},
	})
	doc, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: props
    objects:
      - id: button
        x: 10
        components: {button: {}}
      - id: door
        x: 50
        components: {door: {}}
      - id: gate
        x: 90
        components: {door: {}}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args: {speed: 3.5}
  - from: button
    event: pressed
    to: gate
    handler: open
    args: {speed: 2.0}
`))
	if err != nil {
		t.Fatal(err)
	}

	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnEntitiesWithSignals(doc, reg, w, "game-ctx")
	if err != nil {
		t.Fatalf("SpawnEntitiesWithSignals: %v", err)
	}
	if w.Count() != 3 || len(spawned) != 3 {
		t.Fatalf("spawned %d entities (world count %d), want 3", len(spawned), w.Count())
	}
	btnEnt, ok := sig.Entity("button")
	if !ok || btnEnt != spawned["button"] {
		t.Fatalf("Entity(button) = %v, %v; want %v", btnEnt, ok, spawned["button"])
	}
	doorEnt := spawned["door"]
	gateEnt := spawned["gate"]
	entities.Set(w, doorEnt, doorState{})
	entities.Set(w, gateEnt, doorState{})

	// EmitEntity dispatches synchronously with World, SourceEntity and TargetEntity set.
	if err := sig.EmitEntity(btnEnt, "pressed", "click"); err != nil {
		t.Fatalf("EmitEntity: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].World != w || calls[0].SourceEntity != btnEnt || calls[0].TargetEntity != doorEnt || calls[0].Context != "game-ctx" || calls[0].Payload != "click" {
		t.Fatalf("call[0] = %+v", calls[0])
	}
	if st, _ := entities.Get[doorState](w, doorEnt); !st.open || st.speed != 3.5 || st.count != 1 {
		t.Fatalf("door state = %+v", st)
	}
	if st, _ := entities.Get[doorState](w, gateEnt); !st.open || st.speed != 2.0 || st.count != 1 {
		t.Fatalf("gate state = %+v", st)
	}

	// EnqueueEntity + despawn target before Flush: despawned target is skipped cleanly, live target still runs.
	if err := sig.EnqueueEntity(btnEnt, "pressed", "queued"); err != nil {
		t.Fatalf("EnqueueEntity: %v", err)
	}
	w.Destroy(gateEnt)
	if err := sig.Flush(); err != nil {
		t.Fatalf("Flush after target despawn: %v", err)
	}
	if len(calls) != 3 || calls[2].TargetEntity != doorEnt {
		t.Fatalf("expected only live door to run after gate despawn, calls = %+v", calls)
	}
	if st, _ := entities.Get[doorState](w, doorEnt); st.count != 2 {
		t.Fatalf("door count = %d, want 2", st.count)
	}

	// EnqueueEntity + despawn source before Flush: despawned source is skipped cleanly.
	if err := sig.EnqueueEntity(btnEnt, "pressed", "will-skip"); err != nil {
		t.Fatalf("EnqueueEntity: %v", err)
	}
	w.Destroy(btnEnt)
	if err := sig.Flush(); err != nil {
		t.Fatalf("Flush after source despawn: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("despawned source still dispatched: %d calls", len(calls))
	}

	// Direct EmitEntity / EnqueueEntity on despawned source skips cleanly (nil error), while event typos still error.
	if err := sig.EmitEntity(btnEnt, "pressed", nil); err != nil {
		t.Fatalf("EmitEntity on despawned source = %v, want nil", err)
	}
	if err := sig.EnqueueEntity(btnEnt, "pressed", nil); err != nil || sig.Queued() != 0 {
		t.Fatalf("EnqueueEntity on despawned source = %v (queued %d), want nil and 0", err, sig.Queued())
	}
	if err := sig.EmitEntity(btnEnt, "typo", nil); err == nil {
		t.Fatal("EmitEntity with unknown event on despawned source succeeded")
	}

	// Recycling the slot yields a new generation that is NOT bound in this scene.
	recycled := w.Spawn()
	if err := sig.EmitEntity(recycled, "pressed", nil); err == nil || !strings.Contains(err.Error(), "not bound in this scene") {
		t.Fatalf("EmitEntity on recycled entity = %v, want unbound error", err)
	}
	if err := sig.EnqueueEntity(entities.Zero, "pressed", nil); err == nil {
		t.Fatal("EnqueueEntity(Zero) succeeded")
	}

	// Earlier handler despawning a later handler's target in the same Emit skips the later handler cleanly.
	doc2, err := sceneformat.Parse([]byte(`version: 1
layers:
  - name: props
    objects:
      - {id: button, components: {button: {}}}
      - {id: door, components: {door: {}}}
      - {id: gate, components: {door: {}}}
connections:
  - {from: button, event: pressed, to: door, handler: destroy_other}
  - {from: button, event: pressed, to: gate, handler: open}
`))
	if err != nil {
		t.Fatal(err)
	}
	calls = nil
	w2 := entities.NewWorld()
	spawned2, sig2, err := sceneformat.SpawnEntitiesWithSignals(doc2, reg, w2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig2.EmitEntity(spawned2["button"], "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Handler != "destroy_other" {
		t.Fatalf("handler targeting mid-emit despawned entity ran: %+v", calls)
	}
}

func TestSpawnEntitiesWithSignalsLeavesWorldUntouchedOnValidationError(t *testing.T) {
	reg := doorRegistry(t, new([]sceneformat.Call))
	validDoc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		t.Fatal(err)
	}

	// Handler without Func.
	noFuncReg := sceneformat.NewComponentRegistry()
	noFuncReg.MustRegister(sceneformat.ComponentType{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}})
	noFuncReg.MustRegister(sceneformat.ComponentType{Name: "door", Handlers: []sceneformat.Handler{{Name: "open", Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat}}}}})

	// Bad connection (unknown handler).
	badConnDoc := validDoc.Clone()
	badConnDoc.Connections[0].Handler = "shut"

	// Bad component field.
	badCompDoc := validDoc.Clone()
	badCompDoc.Layers[0].Objects[1].Components["door"] = map[string]any{"open": "nope"}

	// Unresolved scene.
	unresolvedDoc, err := sceneformat.Parse([]byte("version: 2\nlayers:\n  - name: a\n    objects:\n      - {id: d, scene: door.scene.yaml}\n"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		doc  *sceneformat.SceneDoc
		reg  *sceneformat.ComponentRegistry
	}{
		{"nil doc", nil, reg},
		{"nil reg", validDoc, nil},
		{"handler without Func", validDoc, noFuncReg},
		{"bad connection", badConnDoc, reg},
		{"bad component", badCompDoc, reg},
		{"unresolved scene", unresolvedDoc, reg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := entities.NewWorld()
			existing := w.Spawn()
			spawned, sig, err := sceneformat.SpawnEntitiesWithSignals(tc.doc, tc.reg, w, nil)
			if err == nil || spawned != nil || sig != nil {
				t.Fatalf("SpawnEntitiesWithSignals = (%v, %v, %v), want error and nil results", spawned, sig, err)
			}
			if w.Count() != 1 || !w.Alive(existing) {
				t.Fatalf("World was modified on failed SpawnEntitiesWithSignals: count=%d", w.Count())
			}
		})
	}

	// Nil and disposed worlds are refused cleanly.
	if _, _, err := sceneformat.SpawnEntitiesWithSignals(validDoc, reg, nil, nil); err == nil {
		t.Fatal("SpawnEntitiesWithSignals with nil world succeeded")
	}
	disposed := entities.NewWorld()
	disposed.Dispose()
	if _, _, err := sceneformat.SpawnEntitiesWithSignals(validDoc, reg, disposed, nil); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("SpawnEntitiesWithSignals on disposed world = %v, want ErrWorldDisposed", err)
	}
}

func TestConnectEntitiesAndResolvedSceneEntities(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)

	scenes := newMemScenes(map[string]string{
		"doorway.scene.yaml": `version: 2
layers:
  - name: a
    objects:
      - {id: button, components: {button: {}}}
      - {id: door, components: {door: {}}}
connections:
  - {from: button, event: pressed, to: door, handler: open}
`,
	})
	doc, err := sceneformat.Parse([]byte(`version: 2
layers:
  - name: a
    objects:
      - {id: east, scene: doorway.scene.yaml}
      - {id: alarm, components: {button: {}}}
connections:
  - {from: alarm, event: pressed, to: east/door, handler: close}
`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatal(err)
	}

	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntities(res, w)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.ConnectEntities(res, reg, w, spawned, nil)
	if err != nil {
		t.Fatalf("ConnectEntities: %v", err)
	}
	if err := sig.EmitEntity(spawned["east/button"], "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.EnqueueEntity(spawned["alarm"], "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].TargetEntity != spawned["east/door"] || calls[0].Handler != "open" || calls[1].TargetEntity != spawned["east/door"] || calls[1].Handler != "close" {
		t.Fatalf("calls = %+v", calls)
	}

	// ConnectEntities refuses missing, dead, or duplicate entity mappings, and unbound Signals refuses EmitEntity/EnqueueEntity.
	incomplete := map[string]entities.Entity{"alarm": spawned["alarm"]}
	if _, err := sceneformat.ConnectEntities(res, reg, w, incomplete, nil); err == nil || !strings.Contains(err.Error(), "missing entity") {
		t.Fatalf("ConnectEntities missing entity = %v", err)
	}
	dupMap := make(map[string]entities.Entity, len(spawned))
	for k := range spawned {
		dupMap[k] = spawned["alarm"]
	}
	if _, err := sceneformat.ConnectEntities(res, reg, w, dupMap, nil); err == nil || !strings.Contains(err.Error(), "share entity") {
		t.Fatalf("ConnectEntities duplicate entity = %v", err)
	}
	deadMap := make(map[string]entities.Entity, len(spawned))
	for k, v := range spawned {
		deadMap[k] = v
	}
	w.Destroy(spawned["alarm"])
	if _, err := sceneformat.ConnectEntities(res, reg, w, deadMap, nil); err == nil || !strings.Contains(err.Error(), "not alive") {
		t.Fatalf("ConnectEntities dead entity = %v", err)
	}
	unbound, err := sceneformat.Connect(res, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := unbound.EmitEntity(spawned["east/door"], "opened", nil); err == nil || !strings.Contains(err.Error(), "not bound to an entity world") {
		t.Fatalf("EmitEntity on unbound Signals = %v", err)
	}
	if err := unbound.EnqueueEntity(spawned["east/door"], "opened", nil); err == nil || !strings.Contains(err.Error(), "not bound to an entity world") {
		t.Fatalf("EnqueueEntity on unbound Signals = %v", err)
	}
}
