package sceneformat_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// Listeners run after the file's connections for the same event, in the order Listen added them, with the source object, the event and the payload in their Call.
func TestListenRunsAfterConnectionsInOrder(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	_, sig := connectScene(t, doorScene, reg)
	var order []string
	var listened []sceneformat.Call
	for _, name := range []string{"first", "second"} {
		name := name
		stop, err := sig.Listen("button", "pressed", func(c sceneformat.Call) {
			order = append(order, name)
			listened = append(listened, c)
		}, sceneformat.ListenOptions{})
		if err != nil {
			t.Fatalf("Listen %s: %v", name, err)
		}
		if stop == nil {
			t.Fatalf("Listen %s returned a nil stop", name)
		}
	}
	if err := sig.Emit("button", "pressed", "now"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(calls) != 1 || len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("connection calls %d, listener order %v", len(calls), order)
	}
	c := listened[0]
	if c.Source == nil || c.Source.ID != "button" || c.SourceComponent != "button" || c.Event != "pressed" || c.Payload != "now" {
		t.Fatalf("call %+v", c)
	}
	if c.Target != nil || c.Component != "" || c.Handler != "" || c.Args != nil {
		t.Fatalf("a listener is not a connection, call %+v", c)
	}
	if c.Signals != sig {
		t.Fatal("the call does not carry the Signals")
	}
}

// A listener with an empty source hears every object of the scene that declares the event, each run naming its own source.
func TestListenWildcardHearsEveryObject(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	_, sig := connectScene(t, doorScene, reg)
	var heard []string
	if _, err := sig.Listen("", "pressed", func(c sceneformat.Call) { heard = append(heard, c.Source.ID+":"+c.Event) }, sceneformat.ListenOptions{}); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if _, err := sig.Listen("", "button.pressed", func(c sceneformat.Call) { heard = append(heard, "qualified:"+c.Source.ID) }, sceneformat.ListenOptions{}); err != nil {
		t.Fatalf("Listen qualified: %v", err)
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("door", "opened", nil); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 2 || heard[0] != "button:pressed" || heard[1] != "qualified:button" {
		t.Fatalf("heard %v", heard)
	}
	if _, err := sig.Listen("", "opne", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), `no object of the scene declares the event`) {
		t.Fatalf("Listen of a misspelt event = %v, want it refused at the call", err)
	}
	if _, err := sig.Listen("", "a.b.c", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), "must be a name or component.name") {
		t.Fatalf("Listen of a malformed event = %v", err)
	}
}

// A once listener runs the first time the event fires only, whether it was added before or after another listener.
func TestListenOnce(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, doorScene, doorRegistry(t, &calls))
	runs := 0
	if _, err := sig.Listen("button", "pressed", func(sceneformat.Call) { runs++ }, sceneformat.ListenOptions{Once: true}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := sig.Emit("button", "pressed", nil); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Fatalf("a once listener ran %d times", runs)
	}
}

// A deferred listener does not run inside the event but at the next Flush, in order among the enqueued events, and a deferred once listener is used up by the first emission it queues.
func TestListenDeferred(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	_, sig := connectScene(t, doorScene, reg)
	var heard []any
	if _, err := sig.Listen("button", "pressed", func(c sceneformat.Call) { heard = append(heard, c.Payload) }, sceneformat.ListenOptions{Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := sig.Listen("button", "pressed", func(c sceneformat.Call) { heard = append(heard, "once") }, sceneformat.ListenOptions{Deferred: true, Once: true}); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("button", "pressed", 1); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 0 || len(calls) != 1 || sig.Pending() != 2 {
		t.Fatalf("deferred listeners ran inside Emit: heard %v, %d calls, %d pending", heard, len(calls), sig.Pending())
	}
	if err := sig.Emit("button", "pressed", 2); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 3 {
		t.Fatalf("a deferred once listener queued twice: %d pending", sig.Pending())
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 3 || heard[0] != 1 || heard[1] != "once" || heard[2] != 2 {
		t.Fatalf("heard %v, want [1 once 2]", heard)
	}
	// The connections of a deferred event still run at the event, not at the Flush.
	if len(calls) != 2 {
		t.Fatalf("%d connection calls, want 2", len(calls))
	}
}

// stop ends the subscription, is idempotent, and is safe to call from inside the listener itself.
func TestListenStopInsideHandler(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, doorScene, doorRegistry(t, &calls))
	runs, other := 0, 0
	var stop func()
	var err error
	stop, err = sig.Listen("button", "pressed", func(sceneformat.Call) {
		runs++
		stop()
		stop() // idempotent
	}, sceneformat.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stopOther, err := sig.Listen("button", "pressed", func(sceneformat.Call) { other++ }, sceneformat.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || other != 2 {
		t.Fatalf("self-stopping listener ran %d times, the other %d", runs, other)
	}
	stopOther()
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if other != 2 {
		t.Fatalf("a stopped listener ran again: %d", other)
	}
}

// A listener on an object whose entity has been destroyed never runs for it again, and a deferred run queued before the destruction is dropped at the Flush.
func TestListenStopsWithADestroyedEntity(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(doorScene), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnConnected(doc, w, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	heard := 0
	if _, err := sig.Listen("button", "pressed", func(sceneformat.Call) { heard++ }, sceneformat.ListenOptions{Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := sig.EmitEntity(spawned["button"], "pressed", nil); err != nil {
		t.Fatal(err)
	}
	w.Destroy(spawned["button"])
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if heard != 0 {
		t.Fatalf("a deferred listener ran for a destroyed entity: %d", heard)
	}
	if err := sig.Emit("button", "pressed", nil); err == nil {
		t.Fatal("a destroyed entity emitted")
	}
	if heard != 0 {
		t.Fatalf("a listener heard a destroyed entity: %d", heard)
	}
	if _, err := sig.Listen("button", "pressed", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), "destroyed") {
		t.Fatalf("Listen on a destroyed entity = %v, want it refused", err)
	}
	if _, err := sig.Listen("", "pressed", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), "destroyed") {
		t.Fatalf("wildcard Listen with every source destroyed = %v, want it refused", err)
	}
}

// Listen refuses what Emit refuses, and a nil Signals or listener func.
func TestListenRefuses(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, doorScene, doorRegistry(t, &calls))
	for _, tc := range []struct{ from, event, want string }{
		{"ghost", "pressed", `the scene has no object "ghost"`},
		{"button", "opened", `no registered component of object "button" declares the event`},
		{"door", "pressed", `no registered component of object "door" declares the event`},
	} {
		if _, err := sig.Listen(tc.from, tc.event, func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Listen(%q, %q) = %v, want an error containing %q", tc.from, tc.event, err, tc.want)
		}
	}
	if _, err := sig.Listen("button", "pressed", nil, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("Listen with a nil func = %v", err)
	}
	var nilSig *sceneformat.Signals
	if _, err := nilSig.Listen("button", "pressed", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil {
		t.Fatal("a nil Signals accepted a listener")
	}
}

// An object whose two components declare the same event under a bare name is skipped by a wildcard listener, as Emit from it with that name is refused.
func TestListenWildcardSkipsAmbiguousSources(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	for _, name := range []string{"left", "right"} {
		if err := reg.Register(sceneformat.ComponentType{Name: name, Events: []sceneformat.Event{{Name: "ping"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Register(sceneformat.ComponentType{Name: "bell", Events: []sceneformat.Event{{Name: "ping"}}}); err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: a
    objects:
      - id: both
        components:
          left: {}
          right: {}
      - id: plain
        components:
          bell: {}
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var heard []string
	if _, err := sig.Listen("", "ping", func(c sceneformat.Call) { heard = append(heard, c.Source.ID) }, sceneformat.ListenOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("both", "ping", nil); err == nil || !strings.Contains(err.Error(), "several components") {
		t.Fatalf("Emit of an ambiguous bare name = %v", err)
	}
	if err := sig.Emit("both", "left.ping", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("plain", "ping", nil); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || heard[0] != "plain" {
		t.Fatalf("heard %v, want only [plain]", heard)
	}
	if _, err := sig.Listen("both", "ping", func(sceneformat.Call) {}, sceneformat.ListenOptions{}); err == nil || !strings.Contains(err.Error(), "several components") {
		t.Fatalf("Listen on an ambiguous bare name = %v", err)
	}
}
