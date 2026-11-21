# replay

`game/replay` records fixed-step game inputs into compact delta-encoded files and replays them deterministically for bug reproduction, ghost runs, and automated regression tests.

```go
import "github.com/nsgo-dev/game/packages/game/replay"
```

## ActionSource abstraction

Write your simulation against the `ActionSource` interface (`Pressed`, `JustPressed`, `JustReleased`, `Axis`, `Step`):

```go
type ActionSource interface {
	Pressed(name string) bool
	JustPressed(name string) bool
	JustReleased(name string) bool
	Axis(name string) float64
	Step()
}
```

`*input.Actions` satisfies `ActionSource` for live play, `*replay.Recorder` wraps any `ActionSource` to record it transparently, and `*replay.Player` satisfies `ActionSource` for playback — so your game update loop stays identical in all three modes.

## Recording live play

Wrap your `*input.Actions` with `NewRecorder`, listing the seed, fixed step rate (`<= 0` defaults to `60`), and watched action and axis names:

```go
rec := replay.NewRecorder(
	actions,
	seed,
	60,
	[]string{"jump", "attack"},
	[]string{"move_x"},
)

// Pass rec to your simulation instead of actions directly:
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		sim.Step(rec, dt) // calls rec.Step() once at the start
	},
}

// Snapshot and save (".gz" writes gzip-compressed JSON automatically)
if err := replay.SaveFile("runs/boss-bug.json.gz", rec.Recording()); err != nil {
	return err
}
```

`Recording` is delta-encoded: only steps where a watched action's pressed state or a watched axis's value changed from the previous step are written to `Events`, keeping long sessions small. Serialize in memory with `rec.Encode()` / `rec.EncodeGzip()`, decode with `replay.Decode` / `replay.DecodeGzip`, and read plain or gzipped files with `replay.LoadFile(path)`.

## Playing back a Recording

Load a recording and feed `NewPlayer(recording)` into the same simulation:

```go
recording, err := replay.LoadFile("runs/boss-bug.json.gz")
if err != nil {
	return err
}

player := replay.NewPlayer(recording)
sim := newGameSim(player.Seed())

for !player.Done() {
	sim.Step(player, recording.StepDuration())
}
```

Inspect playback progress with `player.StepIndex()`, `player.TotalSteps()`, `player.Done()`, and rewind with `player.Reset()`. Once `Done()` is `true`, a subsequent `Step()` releases any actions still held on the final step (firing `JustReleased` once) and leaves all inputs idle.

## Automated regression testing (`Run` and `AssertEndState`)

Commit recorded runs (e.g. under `testdata/`) and verify deterministic behavior in `go test` using `replay.Run` and `replay.AssertEndState`:

```go
func TestLevel1ClearReplay(t *testing.T) {
	rec, err := replay.LoadFile("testdata/level1-clear.json.gz")
	if err != nil {
		t.Fatal(err)
	}

	want := SimState{Score: 1200, Cleared: true}
	replay.AssertEndState(t, rec, want, nil, func(seed uint64, src replay.ActionSource) (func(time.Duration), func() SimState) {
		sim := newGameSim(seed)
		return func(dt time.Duration) {
				sim.Step(src, dt)
			}, func() SimState {
				return sim.Snapshot()
			}
	})
}
```

`Run` drives `step` exactly `rec.TotalSteps` times at `rec.StepDuration()` and returns the final state; `AssertEndState` compares it against `want` using your `equal` function (or `reflect.DeepEqual` when `nil`) and fails the test with a clear diagnostic on mismatch.
