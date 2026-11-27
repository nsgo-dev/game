package sceneformat_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// timerRegistry registers the built-in timer component and a spawner whose spawn handler records each call in calls.
func timerRegistry(t testing.TB, calls *[]sceneformat.Call) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterTimerComponent(reg); err != nil {
		t.Fatalf("RegisterTimerComponent: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "spawner",
		Handlers: []sceneformat.Handler{{Name: "spawn", Func: func(c sceneformat.Call) { *calls = append(*calls, c) }}},
	}); err != nil {
		t.Fatalf("Register spawner: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}, {Name: "released"}},
	}); err != nil {
		t.Fatalf("Register button: %v", err)
	}
	return reg
}

// A one-shot timer with autostart fires once after its seconds of game time and then stops.
func TestTimerOneShotAutostart(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, `version: 1
layers:
  - name: main
    objects:
      - id: clock
        components:
          timer:
            seconds: 0.5
            oneShot: true
            autostart: true
      - id: spawner
        components:
          spawner: {}
connections:
  - from: clock
    event: timeout
    to: spawner
    handler: spawn
`, timerRegistry(t, &calls))
	if err := sig.Update(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 0 {
		t.Fatalf("the timer fired early: %d pending", sig.Pending())
	}
	if err := sig.Update(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 1 {
		t.Fatalf("the timer did not fire at its seconds: %d pending", sig.Pending())
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Source.ID != "clock" || calls[0].Event != "timeout" || calls[0].Handler != "spawn" {
		t.Fatalf("calls %+v", calls)
	}
	for range 3 {
		if err := sig.Update(time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if sig.Pending() != 0 || len(calls) != 1 {
		t.Fatalf("a one-shot timer fired again: %d pending, %d calls", sig.Pending(), len(calls))
	}
}

// A repeating timer fires every seconds of game time, carrying the overshoot into the next period so it does not drift, and a timer with seconds 0 fires once per Update.
func TestTimerRepeat(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, `version: 1
layers:
  - name: main
    objects:
      - id: clock
        components:
          timer:
            seconds: 0.5
            autostart: true
      - id: fast
        components:
          timer:
            seconds: 0
            autostart: true
      - id: spawner
        components:
          spawner: {}
connections:
  - from: clock
    event: timeout
    to: spawner
    handler: spawn
  - from: fast
    event: timeout
    to: spawner
    handler: spawn
`, timerRegistry(t, &calls))
	// 1.5 seconds at a 0.5 period fire at 0.5, 1.0 and 1.5, in one Update.
	if err := sig.Update(1500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 4 {
		t.Fatalf("after 1.5s: %d pending, want 3 timeouts of clock and 1 of fast", sig.Pending())
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	// The timers enqueue in document order: clock's three, then fast's one.
	if len(calls) != 4 || calls[0].Source.ID != "clock" || calls[3].Source.ID != "fast" {
		t.Fatalf("calls %+v", calls)
	}
	// The next 0.4 seconds leave the period 0.1 short; 0.6 more fire it once.
	if err := sig.Update(400 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(600 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	clock := 0
	for _, c := range calls[4:] {
		if c.Source.ID == "clock" {
			clock++
		}
	}
	if clock != 2 || len(calls) != 8 {
		t.Fatalf("after 2.5s: %d timeouts of clock in %d calls, want 2 in 8", clock, len(calls))
	}
}

// A timer without autostart starts when a connection runs its start handler, over from its full seconds, and stops on stop.
func TestTimerStartStopHandlers(t *testing.T) {
	var calls []sceneformat.Call
	_, sig := connectScene(t, `version: 1
layers:
  - name: main
    objects:
      - id: button
        components:
          button: {}
      - id: clock
        components:
          timer:
            seconds: 0.5
      - id: spawner
        components:
          spawner: {}
connections:
  - from: button
    event: pressed
    to: clock
    handler: start
  - from: button
    event: released
    to: clock
    handler: stop
  - from: clock
    event: timeout
    to: spawner
    handler: spawn
`, timerRegistry(t, &calls))
	if err := sig.Update(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 0 {
		t.Fatalf("a timer without autostart ran: %d pending", sig.Pending())
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// A restart begins again from the full seconds.
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 0 {
		t.Fatalf("a restarted timer kept its progress: %d pending", sig.Pending())
	}
	if err := sig.Update(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("%d calls, want 1", len(calls))
	}
	// stop holds a repeating timer.
	if err := sig.Emit("button", "released", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 0 || len(calls) != 1 {
		t.Fatalf("a stopped timer fired: %d pending, %d calls", sig.Pending(), len(calls))
	}
}

// The timer of an object whose entity has been destroyed stops, and Update from a handler is refused, as Flush is.
func TestTimerStopsWithADestroyedEntity(t *testing.T) {
	var calls []sceneformat.Call
	reg := timerRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: main
    objects:
      - id: clock
        components:
          timer:
            seconds: 0.5
            autostart: true
      - id: spawner
        components:
          spawner: {}
connections:
  - from: clock
    event: timeout
    to: spawner
    handler: spawn
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnConnected(doc, w, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	w.Destroy(spawned["clock"])
	if err := sig.Update(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if sig.Pending() != 1 {
		t.Fatalf("a destroyed timer kept running: %d pending", sig.Pending())
	}
	// The timeout enqueued before the destruction is dropped at the Flush.
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("a destroyed timer's timeout ran: %+v", calls)
	}
	var nilSig *sceneformat.Signals
	if err := nilSig.Update(time.Second); err == nil {
		t.Fatal("a nil Signals accepted an update")
	}
	if err := sig.Update(0); err != nil {
		t.Fatalf("a zero step: %v", err)
	}
}

// Update from a handler runs nothing and reports it.
func TestTimerUpdateFromHandlerIsRefused(t *testing.T) {
	var calls []sceneformat.Call
	reg := timerRegistry(t, &calls)
	var updateErr error
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "relay",
		Events:   []sceneformat.Event{{Name: "ping"}},
		Handlers: []sceneformat.Handler{{Name: "pong", Func: func(c sceneformat.Call) { updateErr = c.Signals.Update(time.Second) }}},
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: main
    objects:
      - id: r
        components:
          relay: {}
      - id: clock
        components:
          timer:
            seconds: 0.5
            autostart: true
connections:
  - from: r
    event: ping
    to: r
    handler: pong
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("r", "ping", nil); err != nil {
		t.Fatal(err)
	}
	if updateErr == nil || !strings.Contains(updateErr.Error(), "update from a handler") {
		t.Fatalf("Update from a handler = %v, want it refused", updateErr)
	}
	if sig.Pending() != 0 {
		t.Fatalf("the refused Update advanced the timer: %d pending", sig.Pending())
	}
}

// The timer scene file in testdata wires a repeating autostart timer to a spawner with no Go code.
func TestTimerSceneFile(t *testing.T) {
	data, err := os.ReadFile("testdata/timer.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var calls []sceneformat.Call
	reg := timerRegistry(t, &calls)
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "testdata/timer.scene.yaml", Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sig.Update(2500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Source.ID != "spawn_timer" || calls[0].Target.ID != "spawner" || calls[0].Handler != "spawn" {
		t.Fatalf("calls %+v", calls)
	}
	// A listener hears the file's timer too.
	var heard []string
	stop, err := sig.Listen("", "timeout", func(c sceneformat.Call) { heard = append(heard, c.Source.ID) }, sceneformat.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := sig.Update(2500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || heard[0] != "spawn_timer" {
		t.Fatalf("heard %v", heard)
	}
}

// The timer component type checks its fields as every component type does.
func TestTimerComponentFields(t *testing.T) {
	reg := sceneformat.TimerComponentRegistry()
	if _, err := reg.Resolve("timer", map[string]any{"seconds": -1}); err == nil {
		t.Fatal("a negative seconds resolved")
	}
	resolved, err := reg.Resolve("timer", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved["seconds"] != 1.0 || resolved["oneShot"] != false || resolved["autostart"] != false {
		t.Fatalf("defaults %v", resolved)
	}
}
