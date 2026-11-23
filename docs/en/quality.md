# quality

`game/quality` picks a low, medium or high quality preset for the device a game runs on, moves it with the frame times the game measures, and holds whatever the player picks in settings instead.

```go
import "github.com/nsgo-dev/game/packages/game/quality"
```

## Presets

A `Preset` is what one level turns on and how far it scales the game's costs:

| Field | Low | Medium | High | Meaning |
| --- | --- | --- | --- | --- |
| `ResolutionScale` | 0.5 | 0.75 | 1 | Scales buffers the game renders or rasterizes itself, such as `render3d.OcclusionBuffer` and procedurally drawn images; read it with `Scale` and `ScaleSize` |
| `ParticleScale` | 0.25 | 0.6 | 1 | Scales particle budgets and burst counts; read it with `Particles` |
| `Effects` | off | on | on | Effects that only decorate the frame: screen flashes, parallax backdrops, post effects, trails |
| `Shadows` | off | off | on | Shadow casting, such as `lights2d.Scene.DisableShadows` |

`DefaultPreset(level)` returns these values. `Particles(n)` and `Scale(n)` round and never return less than 1 for a positive `n`, so a scaled budget never switches an emitter off:

```go
p := quality.DefaultPreset(quality.Low)
emitter.MaxParticles = p.Particles(512) // 128
emitter.Burst(p.Particles(24))          // 6
lights.DisableShadows = !p.Shadows
foliage := world3d.DefaultQualityProfile(world3d.QualityLevelFor(quality.Low))
```

`ResolutionScale` does not lower the resolution of a `core.Canvas`: NSGo v0.3.0 draws a canvas at the display's full pixel density and has no setting to render it smaller and upscale it. Nothing in this repository applies `ResolutionScale` yet, Starburst included: it is a value for a game to apply to the buffers it sizes itself.

## The starting level

`DeviceFromEnv(cx.Env())` reads the operating system, idiom, device model, OS version and CPU count. `Classify` places a device no rule names:

| Device | Starts at |
| --- | --- |
| Android | Low; NSGo reports neither its memory nor its GPU, so measured frame times step it up |
| iPhone | High from the iPhone 13 (`iPhone14,x`), Medium from the iPhone 11 (`iPhone12,x`), Low before |
| iPad | High from `iPad13,x`, Medium from `iPad11,x`, Low before |
| macOS, Windows, Linux | High with 8 or more CPUs, Medium with 4 to 7, Low with fewer |
| TV and watch | Low |
| Web and anything unknown | Medium |

A configuration's device rules come first: the first rule whose model is a prefix of the device's model, ignoring case, and whose OS matches when it names one, gives the starting level.

## Adapting to frame times

A `Selector` starts at the configured level and, while the setting is `auto`, judges frames a window at a time (90 by default), after skipping 30 frames each time it starts or changes level:

- More than a quarter of a window's frames slower than the target frame rate (30 fps by default) step it down a level.
- Several windows in a row (4 by default) with nine in ten frames inside 60% of the target frame time step it up a level.
- It never steps back up to a level it stepped down from, so it settles instead of swinging between two levels.
- Frames longer than half a second, such as loading or the first frame back from the background, do not count.

Feed it the loop's raw frame lengths through `Loop.FrameTime`, and apply each new preset in `OnChange`:

```go
sel, err := quality.NewSelector(quality.DefaultConfig(), quality.DeviceFromEnv(cx.Env()))
if err != nil {
	panic(err) // DefaultConfig always validates
}
sel.OnChange = func(_ quality.Level, p quality.Preset) { arena.ApplyQuality(p) }
arena.ApplyQuality(sel.Preset())

loop := &game.Loop{
	FrameTime: func(dt time.Duration) {
		if !arena.Paused {
			sel.Observe(dt)
		}
	},
	Update: arena.Step,
	Draw:   draw,
}
```

`FrameTime` keeps firing while the loop or the game is paused. Skip observing frames that simulate nothing, such as a pause or game-over screen: they are cheap and would step the level up toward one gameplay cannot hold.

A `Selector` is not safe for concurrent use; call it from the scope that runs the loop.

## The player's setting

`save.DisplaySettings.Quality` stores the player's choice: `"auto"` (the default) or `"low"`, `"medium"` or `"high"`. `Settings.Validate` normalizes it and turns anything else into `"auto"`. `Selector.SetSetting` applies it: a level's name holds that level whatever the frame times, and `"auto"` returns to the starting level and adapts again. `Selector.Setting` returns the value to store.

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
if s, err := store.Load(); err == nil {
	_ = sel.SetSetting(s.Display.Quality) // Load has validated it
}

// From the settings menu:
if err := sel.SetSetting("high"); err == nil {
	// Load returns the defaults when nothing is stored yet; any other error, such as settings a newer build wrote, keeps the setting for this run only.
	if s, err := store.Load(); err == nil {
		s.Display.Quality = sel.Setting()
		_ = store.Save(s)
	}
}
```

## The configuration file

A game can keep its presets, device rules and adaptation in a YAML file. `ParseConfig` refuses unknown fields, a missing or other `version`, scales outside (0, 1], duplicate device rules, device models with leading or trailing spaces, unknown levels or operating systems, and trailing documents. `EncodeConfig` writes fields in a fixed order, so a file read and written back is unchanged.

```yaml
version: 1
presets:
  low:
    resolution_scale: 0.5
    particle_scale: 0.25
    effects: false
    shadows: false
  medium:
    resolution_scale: 0.75
    particle_scale: 0.6
    effects: true
    shadows: false
  high:
    resolution_scale: 1
    particle_scale: 1
    effects: true
    shadows: true
devices:
  - model: Google Pixel 8
    os: android
    level: high
adapt:
  target_fps: 30
  window_frames: 120
  upgrade_windows: 3
```

| `adapt` field | Range | Default |
| --- | --- | --- |
| `off` | `true` keeps the starting level for the whole run | `false` |
| `target_fps` | 3 to 240 (below 3 fps a slow frame would be over the half second the selector ignores) | 30 |
| `window_frames` | 10 to 3600 | 90 |
| `upgrade_windows` | 1 to 100 | 4 |

## In the Starburst example

`examples/starburst` starts a low-end Android phone on the low preset: a quarter of the particles, and no parallax starfield, floor grid or screen flashes. The HUD's quality button steps through auto, low, medium and high and stores the choice in the player's settings. `BenchmarkArenaPresets` measures one stress-mode step and draw at each level:

```sh
cd examples/starburst
go tool nsgo test -bench BenchmarkArenaPresets ./...
```
