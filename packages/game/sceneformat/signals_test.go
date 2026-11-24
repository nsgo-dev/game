package sceneformat_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// relayScene connects a relay's ping event to its own pong handler, whose Func emits or enqueues ping again.
const relayScene = `version: 1
layers:
  - name: a
    objects:
      - id: r
        components:
          relay: {}
connections:
  - from: r
    event: ping
    to: r
    handler: pong
`

// relayRegistry registers the relay component type of relayScene; pong runs fn.
func relayRegistry(t testing.TB, fn sceneformat.HandlerFunc) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "relay",
		Events:   []sceneformat.Event{{Name: "ping"}},
		Handlers: []sceneformat.Handler{{Name: "pong", Func: fn}},
	}); err != nil {
		t.Fatalf("Register relay: %v", err)
	}
	return reg
}

func connectScene(t testing.TB, src string, reg *sceneformat.ComponentRegistry) (*sceneformat.SceneDoc, *sceneformat.Signals) {
	t.Helper()
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return doc, sig
}

func payloads(calls []sceneformat.Call) []any {
	out := make([]any, len(calls))
	for i, c := range calls {
		out[i] = c.Payload
	}
	return out
}

// Events enqueued during a step run when the step flushes them, in the order they were enqueued, and not before.
func TestEnqueuedEventsRunAtFlushInOrder(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, sig := connectScene(t, doorScene, reg)
	for _, p := range []any{1, 2, 3} {
		if err := sig.Enqueue("button", "pressed", p); err != nil {
			t.Fatalf("Enqueue %v: %v", p, err)
		}
	}
	if err := sig.Enqueue("button", "button.released", 4); err != nil {
		t.Fatalf("Enqueue an unconnected event: %v", err)
	}
	if len(calls) != 0 || doorOpen(doc) || sig.Pending() != 4 {
		t.Fatalf("before Flush: %d calls, door open %v, %d pending", len(calls), doorOpen(doc), sig.Pending())
	}
	if err := sig.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := payloads(calls); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("payloads %v, want [1 2 3]", got)
	}
	if !doorOpen(doc) || sig.Pending() != 0 {
		t.Fatalf("after Flush: door open %v, %d pending", doorOpen(doc), sig.Pending())
	}
	if c := calls[0]; c.SourceEntity != entities.Zero || c.TargetEntity != entities.Zero || c.Args["speed"] != 2.5 {
		t.Fatalf("call %+v", c)
	}
	// An empty queue flushes to nothing, and the queue is reused.
	if err := sig.Flush(); err != nil || len(calls) != 3 {
		t.Fatalf("second Flush: %v, %d calls", err, len(calls))
	}
	if err := sig.Enqueue("button", "pressed", 5); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil || len(calls) != 4 || calls[3].Payload != 5 {
		t.Fatalf("third Flush: %v, payloads %v", err, payloads(calls))
	}
}

// A handler that enqueues its own event does not re-enter itself: each Flush runs it once and leaves the next event for the following Flush, where Emit would recurse.
func TestHandlerThatEnqueuesDoesNotRecurse(t *testing.T) {
	running, depth, runs := 0, 0, 0
	var enqueueErr, flushErr error
	reg := relayRegistry(t, func(c sceneformat.Call) {
		running++
		depth = max(depth, running)
		runs++
		enqueueErr = c.Signals.Enqueue(c.Target.ID, "ping", runs)
		flushErr = c.Signals.Flush()
		running--
	})
	_, sig := connectScene(t, relayScene, reg)
	if err := sig.Enqueue("r", "ping", 0); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= 5; step++ {
		if err := sig.Flush(); err != nil {
			t.Fatalf("step %d: Flush: %v", step, err)
		}
		if runs != step || depth != 1 || sig.Pending() != 1 {
			t.Fatalf("step %d: %d runs, depth %d, %d pending; want %d runs at depth 1 with 1 pending", step, runs, depth, sig.Pending(), step)
		}
		if enqueueErr != nil {
			t.Fatalf("step %d: Enqueue from a handler: %v", step, enqueueErr)
		}
		if flushErr == nil || !strings.Contains(flushErr.Error(), "flush from a handler") {
			t.Fatalf("step %d: Flush from a handler = %v, want it refused", step, flushErr)
		}
	}
}

// Flush from a handler that Emit runs is refused too, and leaves the queue for the update's Flush.
func TestFlushFromAnEmittedHandlerIsRefused(t *testing.T) {
	var flushErr error
	reg := relayRegistry(t, func(c sceneformat.Call) { flushErr = c.Signals.Flush() })
	_, sig := connectScene(t, relayScene, reg)
	if err := sig.Enqueue("r", "ping", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("r", "ping", nil); err != nil {
		t.Fatal(err)
	}
	if flushErr == nil || sig.Pending() != 1 {
		t.Fatalf("Flush inside Emit = %v with %d pending", flushErr, sig.Pending())
	}
}

func TestEnqueueRefusesWhatEmitRefuses(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, doorScene, doorRegistry(t, &calls))
	for _, tc := range []struct{ from, event, want string }{
		{"ghost", "pressed", `enqueue "pressed": the scene has no object "ghost"`},
		{"button", "opened", `no registered component of object "button" declares the event`},
	} {
		if err := sig.Enqueue(tc.from, tc.event, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Enqueue(%q, %q) = %v, want an error containing %q", tc.from, tc.event, err, tc.want)
		}
	}
	if sig.Pending() != 0 {
		t.Fatalf("refused events were queued: %d pending", sig.Pending())
	}
	var nilSig *sceneformat.Signals
	if nilSig.Enqueue("button", "pressed", nil) == nil || nilSig.Flush() == nil || nilSig.Pending() != 0 {
		t.Fatal("a nil Signals accepted an event")
	}
	if _, ok := nilSig.Entity("button"); ok {
		t.Fatal("a nil Signals has an entity")
	}
	if _, ok := sig.Entity("button"); ok {
		t.Fatal("Connect's Signals has an entity")
	}
	if err := sig.EmitEntity(1, "pressed", nil); err == nil || !strings.Contains(err.Error(), "not made by SpawnConnected") {
		t.Fatalf("EmitEntity on Connect's Signals = %v", err)
	}
}

// A scene spawned into entities dispatches its declared connections between those entities.
func TestSpawnedSceneDispatchesBetweenEntities(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(doorScene), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnConnected(doc, w, reg, nil)
	if err != nil {
		t.Fatalf("SpawnConnected: %v", err)
	}
	button, door := spawned["button"], spawned["door"]
	if !w.Alive(button) || !w.Alive(door) || w.Count() != 2 {
		t.Fatalf("spawned %v into a world of %d", spawned, w.Count())
	}
	if e, ok := sig.Entity("door"); !ok || e != door {
		t.Fatalf("Entity(door) = %v, %v", e, ok)
	}
	if err := sig.EmitEntity(button, "pressed", "now"); err != nil {
		t.Fatalf("EmitEntity: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("%d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.SourceEntity != button || c.TargetEntity != door || c.Target.ID != "door" || c.Payload != "now" {
		t.Fatalf("call source %v target %v (%s), payload %v", c.SourceEntity, c.TargetEntity, c.Target.ID, c.Payload)
	}
	if ref, ok := entities.Component[sceneformat.ObjectRef](w).Get(c.TargetEntity); !ok || ref.ID != "door" {
		t.Fatalf("target entity's ObjectRef = %v, %v", ref, ok)
	}

	if err := sig.EnqueueEntity(button, "pressed", "later"); err != nil {
		t.Fatalf("EnqueueEntity: %v", err)
	}
	if err := sig.Flush(); err != nil || len(calls) != 2 || calls[1].TargetEntity != door {
		t.Fatalf("Flush: %v, %d calls", err, len(calls))
	}
	if err := sig.EmitEntity(door, "pressed", nil); err == nil {
		t.Fatal("the door emitted the button's event")
	}
	if err := sig.EmitEntity(w.Spawn(), "pressed", nil); err == nil || !strings.Contains(err.Error(), "was not spawned from the scene") {
		t.Fatalf("EmitEntity of a foreign entity = %v", err)
	}

	// Connections to a destroyed entity are skipped.
	w.Destroy(door)
	if err := sig.EmitEntity(button, "pressed", nil); err != nil || len(calls) != 2 {
		t.Fatalf("emit to a destroyed door: %v, %d calls", err, len(calls))
	}
	// A destroyed entity emits nothing, and what it enqueued before is dropped.
	_, sig, err = sceneformat.SpawnConnected(doc, w, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	button2, _ := sig.Entity("button")
	if err := sig.EnqueueEntity(button2, "pressed", nil); err != nil {
		t.Fatal(err)
	}
	w.Destroy(button2)
	if err := sig.Flush(); err != nil || len(calls) != 2 || sig.Pending() != 0 {
		t.Fatalf("Flush of a destroyed source: %v, %d calls, %d pending", err, len(calls), sig.Pending())
	}
	for _, err := range []error{
		sig.EmitEntity(button2, "pressed", nil),
		sig.Emit("button", "pressed", nil),
		sig.Enqueue("button", "pressed", nil),
	} {
		if err == nil || !strings.Contains(err.Error(), "destroyed") && !strings.Contains(err.Error(), "not spawned") {
			t.Fatalf("emit from a destroyed entity = %v", err)
		}
	}
}

// SpawnConnected checks the scene and its connections before it spawns the first entity.
func TestSpawnConnectedValidatesBeforeSpawning(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	for _, tc := range []struct {
		name, src, want string
		// edit, when set, breaks the parsed scene, for a fault Parse itself refuses.
		edit func(*sceneformat.SceneDoc)
	}{
		{"missing handler", strings.Replace(doorScene, "handler: open", "handler: slam", 1), `declares handler "slam"`, nil},
		{"missing event", strings.Replace(doorScene, "event: pressed", "event: held", 1), `declares event "held"`, nil},
		{"missing object", doorScene, `"gate"`, func(d *sceneformat.SceneDoc) { d.Connections[0].To = "gate" }},
		{"bad component", strings.Replace(doorScene, "door: {}", "door: {open: 3}", 1), `"open"`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := sceneformat.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if tc.edit != nil {
				tc.edit(doc)
			}
			w := entities.NewWorld()
			spawned, sig, err := sceneformat.SpawnConnected(doc, w, reg, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || spawned != nil || sig != nil {
				t.Fatalf("SpawnConnected = %v, %v, %v; want an error containing %q", spawned, sig, err, tc.want)
			}
			if w.Count() != 0 {
				t.Fatalf("a refused scene spawned %d entities", w.Count())
			}
		})
	}
	doc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sceneformat.SpawnConnected(doc, nil, reg, nil); err == nil {
		t.Fatal("SpawnConnected into a nil world succeeded")
	}
	w := entities.NewWorld()
	w.Dispose()
	if _, _, err := sceneformat.SpawnConnected(doc, w, reg, nil); err == nil {
		t.Fatal("SpawnConnected into a disposed world succeeded")
	}
	if _, _, err := sceneformat.SpawnConnected(doc, entities.NewWorld(), nil, nil); err == nil {
		t.Fatal("SpawnConnected with a nil registry succeeded")
	}
}

// The resolved connections of an instanced scene reach the entities of its instances.
func TestSpawnConnectedReachesInstancedObjects(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	level := `version: 2
layers:
  - name: rooms
    objects:
      - id: west
        scene: hall.scene.yaml
`
	doc, err := sceneformat.Parse([]byte(level))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := sceneformat.Resolve(doc, mapScenes{"hall.scene.yaml": doorScene})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	spawned, sig, err := sceneformat.SpawnConnected(resolved, entities.NewWorld(), reg, nil)
	if err != nil {
		t.Fatalf("SpawnConnected: %v", err)
	}
	if err := sig.EnqueueEntity(spawned["west/button"], "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil || len(calls) != 1 || calls[0].TargetEntity != spawned["west/door"] {
		t.Fatalf("Flush: %v, calls %+v", err, calls)
	}
}

// mapScenes resolves scene paths to the scene sources it maps them to.
type mapScenes map[string]string

func (m mapScenes) Scene(path string) (*sceneformat.SceneDoc, error) {
	return sceneformat.Parse([]byte(m[path]))
}

// Enqueue and Flush allocate no more than emitting the same events at once does, once the queue has grown to a step's events.
func TestEnqueueFlushAllocatesNoMoreThanEmit(t *testing.T) {
	reg := relayRegistry(t, func(sceneformat.Call) {})
	_, sig := connectScene(t, relayScene, reg)
	queued := func() {
		for range 8 {
			if err := sig.Enqueue("r", "ping", nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := sig.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	emitted := func() {
		for range 8 {
			if err := sig.Emit("r", "ping", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	queued()
	queued()
	if q, e := testing.AllocsPerRun(100, queued), testing.AllocsPerRun(100, emitted); q > e {
		t.Fatalf("a queued step allocates %v times, emitting it %v", q, e)
	}
}

func BenchmarkEnqueueFlush(b *testing.B) {
	reg := relayRegistry(b, func(sceneformat.Call) {})
	_, sig := connectScene(b, relayScene, reg)
	b.ReportAllocs()
	for b.Loop() {
		if err := sig.Enqueue("r", "ping", nil); err != nil {
			b.Fatal(err)
		}
		if err := sig.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

// The map SpawnConnected returns is the caller's: changing it does not change what the Signals dispatches.
func TestSpawnConnectedKeepsItsOwnEntities(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.Parse([]byte(doorScene))
	if err != nil {
		t.Fatal(err)
	}
	spawned, sig, err := sceneformat.SpawnConnected(doc, entities.NewWorld(), reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	button, door := spawned["button"], spawned["door"]
	clear(spawned)
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatalf("Emit after the caller cleared its map: %v", err)
	}
	if len(calls) != 1 || calls[0].SourceEntity != button || calls[0].TargetEntity != door {
		t.Fatalf("calls %+v", calls)
	}
}

// A handler's panic passes through Flush, and the events after the one that panicked stay queued for the next Flush.
func TestFlushKeepsTheEventsAfterAPanic(t *testing.T) {
	var ran []any
	reg := relayRegistry(t, func(c sceneformat.Call) {
		if c.Payload == 2 {
			panic("handler failed")
		}
		ran = append(ran, c.Payload)
	})
	_, sig := connectScene(t, relayScene, reg)
	for _, p := range []any{1, 2, 3, 4} {
		if err := sig.Enqueue("r", "ping", p); err != nil {
			t.Fatal(err)
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the handler's panic did not pass through Flush")
			}
		}()
		_ = sig.Flush()
	}()
	if len(ran) != 1 || ran[0] != 1 || sig.Pending() != 2 {
		t.Fatalf("after the panic: ran %v, %d pending; want [1] with 2 pending", ran, sig.Pending())
	}
	if err := sig.Enqueue("r", "ping", 5); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatalf("Flush after a panic: %v", err)
	}
	if len(ran) != 4 || ran[1] != 3 || ran[2] != 4 || ran[3] != 5 {
		t.Fatalf("ran %v, want [1 3 4 5]", ran)
	}
}
