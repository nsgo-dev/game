# replay

`game/replay` records fixed-step game inputs into delta-encoded YAML text files and replays them deterministically for bug reproduction, ghost runs, and automated regression tests.

```go
import "github.com/nsgo-dev/game/pkg/replay"
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

// Snapshot and save as YAML text
if err := replay.SaveFile("runs/boss-bug.replay.yaml", rec.Recording()); err != nil {
	return err
}
```

`Recording` is delta-encoded: only steps where a watched action's pressed state or a watched axis's value changed from the previous step are written to `Events`, keeping long sessions small. Serialize in memory with `rec.Encode()` and `replay.Decode`, and on disk with `replay.SaveFile` and `replay.LoadFile`.

## The recording file

A recording is a YAML text file you can read, diff in review and edit by hand. The header holds the format version, the seed, the step rate, the watched action and axis names and the total step count; after it comes one line per step on which input changed, naming the actions pressed and released and the new axis values:

```yaml
version: 2
seed: 99537266
rate: 60
actions: [left, right, jump, dash]
axes: [move_x]
total_steps: 254
steps:
  - {step: 6, press: [right], axes: {move_x: 1}}
  - {step: 39, press: [left], release: [right], axes: {move_x: -1}}
  - {step: 85, press: [jump, dash]}
  - {step: 95, release: [jump, dash]}
```

Steps count from 0 and must be listed in increasing order below `total_steps`. An action stays pressed from the step that presses it until a step releases it, and an axis keeps its value until a later step sets another. An edited file replays as edited: move a press to another step, change an axis value or add a line, and `LoadFile` and `Player` follow it.

`Decode` refuses unknown keys, a missing `seed` or `total_steps`, a line without `step`, names missing from the header and trailing YAML documents, and names the step or line at fault. `Encode` always writes the same bytes for the same recording, so loading a file it wrote and saving it again changes nothing; an axis value written as `-0.0` is read as `0`. Comments and block-style entries you add are read, but a program that saves the recording again writes the canonical one-line form without them. The conventional extension is `.replay.yaml`. Version 1, the earlier compact JSON form, is no longer read.

## Playing back a Recording

Load a recording and feed `NewPlayer(recording)` into the same simulation:

```go
recording, err := replay.LoadFile("runs/boss-bug.replay.yaml")
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
	rec, err := replay.LoadFile("testdata/level1-clear.replay.yaml")
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

To check the whole state rather than a hand-picked struct, dump it as text with [`statedump.Run`](statedump.md) and compare it against a golden file.

## Playing recordings in CI

`nsgo game replay` plays a project's recordings headless: it checks that every `*.replay.yaml` file under the directories it is given can be played, then runs the tests with `Replay` in their names, such as `TestLevel1ClearReplay` above, and fails when a recording is broken, a run ends in another state than the test expects or no test passed, so a misnamed test cannot leave the recordings unplayed. See [Command Line](cli.md#replay).
