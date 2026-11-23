# statedump

`game/statedump` runs a game headless for a fixed number of steps and writes its entities, components and scene state as deterministic YAML. Two runs with the same seed and inputs write identical files, so an agent or a test checks its work by diffing one run against another, and a change shows as a few changed lines.

The files match byte for byte on the same CPU architecture. A game's own float math can differ between arm64 and amd64, because the Go compiler may fuse a multiply and an add into one FMA instruction on arm64; a golden dump shared across machines needs an explicit conversion on such products, as in `x += float64(v * speed * dt)`, which rounds the product and stops the fusion.

```go
import "github.com/nsgo-dev/game/packages/game/statedump"
```

## Running headless

`Run` plays a [`replay.Recording`](replay.md) against a simulation and captures its state after `rec.TotalSteps` steps. Like `replay.Run`, `makeSim` receives the recording's seed and a `*replay.Player` as its `ActionSource`; unlike it, `Run` needs no `testing.TB`, so a command or an agent's tool can call it too. `Run` returns an error when `Step` did not call `src.Step()` once per step, since a sim that never reads the recording would dump the same state for any input.

```go
rec, err := replay.LoadFile("runs/boss-bug.replay.yaml")
if err != nil {
	return err
}
dump, err := statedump.Run(rec, func(seed uint64, src replay.ActionSource) (statedump.Sim, error) {
	g := newGame(seed)
	return statedump.Sim{
		Step: func(dt time.Duration) { g.Update(src, dt) }, // calls src.Step() once, as game.Loop's Update does
		Source: statedump.Source{
			World:     g.World,
			Scenes:    g.Scenes,
			SceneName: func(c *scene.Controller) string { return g.SceneNames[c] },
			Values: func() map[string]any {
				return map[string]any{"score": g.Score, "level": g.Level}
			},
		},
	}, nil
})
if err != nil {
	return err
}
return statedump.WriteFile("runs/boss-bug.state.yaml", dump)
```

To run N steps with no input, pass a recording without events: `&replay.Recording{Version: replay.FormatVersion, Seed: seed, Rate: 60, TotalSteps: n}`.

`Capture(src)` takes the same state at any moment, with the seed, rate and step count left for the caller to fill in.

## What a dump holds

- `version`, `seed`, `rate` and `steps`: the schema version (1) and how the run was made.
- `scene`: the `scene.Manager` stack's `depth`, the `current` scene's name when `SceneName` is set, and `transitioning` with its eased `progress` while a `Switch` transition runs; a curve that overshoots, such as an ease-out-back, takes `progress` past 1 for part of the transition. It is left out without a manager.
- `values`: game-wide state outside the entity world, such as score, level or an RNG position, each encoded with `encoding/json` and keyed by name, in ascending byte order of the names.
- `entities`: every live entity in ascending slot index, with its `generation`, its hierarchy `parent` and its components in ascending codec name. Only components registered with [`entities.RegisterCodec`](entities.md) appear, as in a world snapshot.

```yaml
version: 1
seed: 42
rate: 60
steps: 90
scene:
  depth: 2
  current: pause
values:
  score: 40
entities:
  - index: 0
    generation: 1
    components:
      - type: health
        data:
          hp: 100
      - type: pos
        data:
          x: 12.5
          y: 12
  - index: 1
    generation: 1
    parent: 0
    components:
      - type: name
        data: sword
```

Component and value data become YAML mappings and sequences rather than embedded JSON strings, so one changed field is one changed line. They keep the JSON field order of the Go struct and the exact text `encoding/json` writes for each number. Strings that YAML 1.2 or a YAML 1.1 reader would read as another type (`"1"`, `"true"`, `"yes"`, `"2001-12-14"`, `"1:20"`) are quoted, and a JSON `null` stays `null`.

## Reading and diffing

`Encode` writes a dump, `Parse` reads one, and `WriteFile` and `ReadFile` do the same on disk. `Encode(Parse(b))` returns `b` for every `b` that `Encode` wrote. `Parse` refuses unknown fields, other versions, trailing documents, entities out of slot order, components out of name order, parents missing from the dump, and data JSON cannot hold: non-string keys, anchors and aliases, binary, NaN and infinities.

`Diff(a, b)` returns a unified line diff with three lines of context, or `""` when the dumps are equal:

```diff
--- a
+++ b
@@ -28,7 +28,7 @@
           second line
       - type: pos
         data:
-          x: 4.0000009393637015e-7
+          x: 119.99999559999982
           y: 12
   - index: 1
     generation: 1
```

A test keeps a golden dump under `testdata/` and fails with this diff when a run no longer matches it:

```go
got, err := statedump.Encode(dump)
if err != nil {
	t.Fatal(err)
}
want, err := os.ReadFile("testdata/boss-bug.state.yaml")
if err != nil {
	t.Fatal(err)
}
if diff := statedump.Diff(want, got); diff != "" {
	t.Fatalf("state differs from the golden dump:\n%s", diff)
}
```

`dump.Snapshot()` returns the entities as an `entities.WorldSnapshot`, so `World.Restore` loads a dump back into a world that registers the same codecs.
