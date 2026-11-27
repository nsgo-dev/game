# sound

`game/sound` layers a persistent volume `Mixer`, voice-limited sound `Effect`s, crossfading `Music`, and automatic background muting on top of NSGo's `audio` package.

```go
import (
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
)
```

All `Mixer`, `Effect` and `Music` methods run on the engine's goroutine, just like `audio`'s methods. From any other goroutine, dispatch through `cx.Post`. Ship audio files under `resources/` as **WAV**, **AAC (`.m4a`)** or **MP3**.

## Mixer and persistent volumes

A `Mixer` holds one master volume and a volume for every group name your game uses (for example `"effects"`, `"music"` and `"ui"`). Every volume is in `[0, 1]` and defaults to `1`.

```go
mixer := sound.NewMixer(cx, "sound")
```

`NewMixer(cx, key)` stores values in `cx.Storage()` (`core.Storage`) under:

- `<key>.master`
- `<key>.group.<group>`

Volumes persist automatically across app launches. Passing values outside `[0, 1]` clamps them (and `NaN` resets to `1`). If storage is full, `SetMaster` and `SetVolume` return `*core.StorageFullError` and keep the previous volume.

`SetDefaultMaster` and `SetDefaultVolume(group, v)` set the volume a mixer has until the player sets one, such as a bus volume from the [project file](project.md). They keep nothing in storage, so a volume the player set wins; set them before the game reads volumes.

### Reactive settings UI

`Master()` and `Volume(group)` read from `core.Storage`, so calling them inside a reactive view function subscribes that view to volume changes automatically:

```go
core.Text{
	ContentFunc: func() string {
		return fmt.Sprintf("Music: %.0f%%", mixer.Volume("music")*100)
	},
}
```

### How volume changes take effect

- **Effects**: each playback uses `voice.Volume * mixer.Volume(group) * mixer.Master()`. Volume changes apply at once to effects already playing, through each playback's `audio.Playback`, and to those started afterwards. A playback whose group or master volume reaches `0` plays on in silence and is heard again when the volume comes back.
- **Music**: active tracks update live at `mixer.Volume(group) * mixer.Master()`, pausing when the effective gain reaches `0` and resuming from the same position when raised above `0`.

## Effect and voice limits

Short sounds (under 8 MiB encoded and 5 seconds decoded) are decoded into memory once with `audio.LoadSound`, then wrapped in a mixer `Effect`:

```go
var hit *sound.Effect
audio.LoadSound(cx, audio.Source{URI: "asset:sounds/hit.wav"}, func(s *audio.Sound, err error) {
	if err != nil {
		return
	}
	hit = mixer.Effect(s, "effects", 4)
})
```

The third argument (`limit`) caps how many overlapping playbacks of that sound may run at once (`0` means no per-effect cap, still bounded by the platform's 32 concurrent sound voices). This prevents rapid bursts — such as coins or footsteps — from starving every other sound.

- `Play(v audio.Voice) bool` starts a playback and reports whether it asked for one. It returns `false` when effective gain is `0` (including a voice with `Muted` set) or the app is in the background, and `true` otherwise, even where the playback ends at once because the backend could not start it. When `Limit` playbacks are still playing, it stops the oldest of them to make room, so the newest are heard; playbacks that end on their own free their place without stopping another, and one that never started leaves them playing. A backend that cannot address playbacks plays the voice as it started, outside the limit and later volume changes; every NSGo backend with audio can.
- `Stop()` stops every playback of that sound (and any other `*audio.Sound` sharing the same source).
- Close the underlying `*audio.Sound` when you no longer need it, or let `cx`'s scope disposal free it.

```go
if hit != nil {
	hit.Play(audio.Voice{Volume: 0.9, Rate: 1.05})
}
```

## Bus effect chains

`mixer.SetGroupEffects(group, ...)` gives a group a DSP chain of bus effects — `sound.LowPass`, `sound.HighPass`, `sound.Delay`, `sound.Reverb`, `sound.Gain` and `sound.Bitcrush` — run in order, and `mixer.GroupEffects(group)` reads it back. A project's `game.yaml` `audio.buses` `effects` install the same chains through [`project.File.ConfigureMixer`](project.md).

The chain applies where a source is still in memory before it reaches the player:

- `Music.Play(src, fade)` (and `PlaySections` / `PlayWithIntro`) runs src through the music's group chain when src holds in-memory 16-bit PCM WAV data, and plays the result.
- `mixer.LoadEffect(src, group, limit, done)` loads src through its group chain the same way, then wraps it as `mixer.Effect` does.
- `mixer.ProcessSource(group, src)` exposes the same processing on its own; the editor's bus audition uses it.

Any other source — a URI, or in-memory MP3, AAC, Ogg or non-PCM WAV data — cannot be processed this way and plays without the chain, with one log line per group saying its effects were skipped for it. `mixer.Effect(s, group, limit)` on an already decoded `*audio.Sound` plays without the chain too: it applies at load time, so load through `LoadEffect` to hear it. Processing whole buffers as they load is the interim pipeline; live effects on every playing voice, covering every format, are a planned follow-up.

## Positional effects in 2D

A `Listener2D` hears effects placed in the world from where the 2D camera looks: an effect to the left of the camera plays panned left, one to the right panned right, and both play quieter the further they are.

```go
listener := sound.Listener2D{
	Position: math2d.V2(cam.X, cam.Y),
	Rotation: cam.Rotation,
	Near:     64,  // full volume up to 64 world units away
	Far:      640, // silent from 640 away
	Rolloff:  sound.RolloffInverse,
}
hit.PlayAt(listener, math2d.V2(enemy.X, enemy.Y), audio.Voice{Volume: 0.9})
```

- `Position` and `Rotation` follow the camera, such as `tilemap.Camera`'s `X`, `Y` and `Rotation`, so left and right are the view's even when the camera turns. Distances are in world units; the camera's zoom does not change them.
- `Gain(pos)` is `1` within `Near`, `0` at `Far` and beyond, and falls between them by `Rolloff`: `RolloffLinear` (the default) in a straight line, `RolloffInverse` as `Near / distance` rescaled to reach `0` at `Far`, and `RolloffExponential` as `(Near / distance)^2` rescaled to reach `0` at `Far`. A `Far` that is not greater than `Near`, infinite or `NaN` turns attenuation off.
- `Pan(pos)` is the effect's offset across the view divided by `PanWidth`, clamped to `[-1, 1]`; a `PanWidth` of `0` takes `Far`. An effect straight above or below the camera plays centered.
- `PlayAt(l, pos, v)` plays with `v.Volume * l.Gain(pos)`, then the group and master volumes as `Play` does, and `v.Pan` replaced by `l.Pan(pos)`. It returns `false` and plays nothing beyond `Far`, so silent effects never take a voice or count against `Limit`.
- `Cue.PlayAt(l, pos)` picks the cue's next variant as `Play` does and plays it through its effect's `PlayAt`, at the variant's volume and pitch, so randomized footsteps and hits can be placed too.
- `Voice(pos, v)` returns the voice `PlayAt` would play with, for playing an `audio.Sound` directly. Where the effect is silent it sets `Muted`, since `audio` plays a volume of `0` at full volume.
- The camera's rotation only turns the pan; distance ignores it, and an infinite or `NaN` `Rotation` counts as `0`.

The pan and volume are fixed when the playback starts, because NSGo's `audio.Playback` cannot change its pan once playing. Suit it to short effects; a long sound that follows a moving source would need to be replayed.

## Positional effects in 3D

`Listener3D` hears effects placed in a right-handed Y-up 3D world (`+X` right, `+Y` up, `-Z` forward — matching `math3d` and `scene3d.Camera`). Because `sound.Vec3` shares the exact field layout of `math3d.Vec3`, convert camera vectors directly with `sound.Vec3(...)`:

```go
listener := sound.Listener3D{
	Position: sound.Vec3(cam.Pos),
	Forward:  sound.Vec3(cam.Forward()),
	Up:       sound.Vec3(cam.Up()),
	Near:     2,
	Far:      40,
	Rolloff:  sound.RolloffInverse,
}
mixer.SetListener3D(listener)

// Fire-and-forget 3D effect (mirrors Effect.PlayAt):
hit.PlayAt3D(listener, sound.Vec3(enemyPos), audio.Voice{Volume: 0.9})

// Live 3D playback that tracks camera and source motion:
engine := loopSFX.Play3D(sound.Vec3(shipPos), audio.Voice{Loop: true}, sound.Spatial3DOptions{})
engine.SetPosition(sound.Vec3(nextShipPos))
```

- **Distance attenuation**: `Gain(pos)` is `1` within `Near`, `0` at `Far` and beyond, and falls between them by `Rolloff` (`RolloffLinear`, `RolloffInverse`, or `RolloffExponential`, shaped by optional `RolloffFactor`). A `Far` not greater than `Near`, infinite, or `NaN` turns distance attenuation off.
- **Rear head-shadow attenuation**: sounds behind the listener (`dot(dir, forward) < 0`) interpolate linearly from `1` at the lateral plane down to `RearGain` directly behind (`DefaultRearGain`, `0.7`, when `RearGain` is zero, negative, or `NaN`; set `RearGain: 1` to disable). An effect behind the camera is therefore quieter than an equidistant effect in front while remaining panned to its left or right side.
- **Stereo pan**: `Pan(pos)` projects the unit direction from the listener to the source onto the listener's right axis (`dot(dir, right)`), clamped to `[-1, 1]`. Sounds at the listener, directly ahead, directly behind, directly overhead/underfoot, or with `NaN` coordinates play centered (`0`). Set a positive finite `PanWidth` to use linear lateral-offset panning like `Listener2D`.
- **Directional source cones & overrides**: pass `Spatial3DOptions` to `Listener3D.Spatialize`, `Spatialize3D`, `Listener3D.VoiceWithOptions`, `Effect.Play3D`, or `Cue.Play3D` to add a directional emission cone (`SourceForward`, `InnerConeAngle`, `OuterConeAngle`, `OuterGain`) or per-source `Near`/`Far`/`Rolloff`/`RolloffFactor` overrides.
- **Live `Playback3D` tracking**: `Mixer.SetListener3D(l)` refreshes every active `Playback3D` started by `Effect.Play3D` or `Cue.Play3D`, and `Playback3D.SetPosition` / `SetOptions` refresh a single source. Live distance, rear, and source-cone attenuation update immediately through `audio.Playback.SetVolume` while honoring master/group volumes, `SetPaused`, `SetPauseExempt`, `Suspend`, and background muting. Because NSGo's `audio.Playback` cannot change stereo pan after starting, `Playback3D.Pan()` tracks the latest computed pan while the backend playback keeps its initial pan.
- **Pose helpers & cues**: `Listener3D.SetPose(pos, forward, up)` and `Listener3D.LookAt(target, up)` update orientation without altering attenuation settings. `Cue.PlayAt3D` and `Cue.Play3D` position randomized sound cues in 3D.

## Music and crossfades

`mixer.Music(group)` plays streaming tracks through `audio.Player`, one active track at a time, with smooth crossfades:

```go
music := mixer.Music("music")
music.Loop = true
music.Play(audio.Source{URI: "asset:music/overworld.m4a"}, 2*time.Second)
```

- `Loop`: when `true`, tracks started by subsequent `Play` calls loop at their end.
- `Play(src, fade)`: starts the new track at once, muted, and fades it in over `fade` while fading all currently playing tracks out on display frames (`core.Scope.Animate`), closing faded-out players once they reach silence. When `fade <= 0`, it switches immediately. Starting another `Play` mid-crossfade smoothly fades out the incoming track from its current level.
- `Stop(fade)`: fades active music out over `fade` and closes its players (`fade <= 0` stops immediately).
- A `src` holding in-memory 16-bit PCM WAV data is processed through the group's [bus effect chain](#bus-effect-chains) before playing; any other source plays as it is.

On the web, browsers enforce autoplay policies and refuse playback (`audio.NotAllowed`) until the user interacts with the page; start music from a button press or first tap/click.

## Pausing with the game

`mixer.SetPaused(true)` pauses gameplay audio when a pause menu opens, and `mixer.SetPaused(false)` resumes it when the menu closes:

- **Gameplay groups** (any group not marked exempt): playing `Effect`s are silenced through their `audio.Playback` without stopping, new `Effect.Play` calls return `false`, and `Music` pauses at its current position. On resume, effects still playing return to their volume and music continues from where it paused. Because NSGo v0.3.0's `audio.Playback` provides volume, rate and stop controls without a playhead pause command, short one-shot effects finish silently if the pause lasts longer than their remaining duration, while looping effects and `Music` (`audio.Player`) resume right where they left off.
- **Exempt groups**: call `mixer.SetPauseExempt("ui", true)` so pause-menu clicks or menu music keep playing while `mixer.Paused()` is `true`. `mixer.PauseExempt(group)` reports whether a group is exempt, and toggling exemption while paused applies immediately.
- **Loop sync**: pair `mixer.SetPaused` with [`game.Loop.OnPause`](loop.md#pausing-and-syncing-subsystems) so gameplay sounds pause and resume automatically whenever `loop.SetPaused` or `loop.Paused` changes:

```go
mixer.SetPauseExempt("ui", true)
loop := &game.Loop{
	Rate:    60,
	OnPause: mixer.SetPaused,
	Update:  stepWorld,
	Draw:    renderWorld,
}
loop.Run(cx)
```

## Background muting

`NewMixer` watches `cx.Env().Lifecycle()` on the same condition as `game.Loop.Run`, so backgrounding pauses and resumes audio in sync with the loop:

- When the app enters **`core.LifecycleBackground`** (minimized, hidden or covered on desktop; backgrounded on iOS or Android), the mixer silences every playing effect (including pause-exempt groups such as `"ui"`), drops new `Effect.Play` calls, and pauses all music.
- When the window returns to **`core.LifecycleActive`** or **`core.LifecycleInactive`** (visible, even unfocused on desktop), pause-exempt groups resume at once, while gameplay groups resume only if `mixer.Paused()` is `false` — returning from the background while the pause menu is still open keeps gameplay sounds silenced and gameplay music paused at its position until the menu closes.

## Suspending for a cutscene

`mixer.Suspend()` silences the mixer the same way the background does, until the `resume` function it returns is called: effects playing are silenced, new `Effect.Play` calls play nothing, and music pauses and later plays on from where it stopped. Holds nest, so the mixer plays again only once every `Suspend` has been resumed; calling one `resume` twice does nothing. `mixer.Suspended()` reports whether a hold is left.

```go
resume := mixer.Suspend()
// ... something else has the speakers ...
resume()
```

[cutscene](cutscene.md)'s `Player` suspends the mixer while a video plays.

## Testing with nsgotest

Enable virtual audio in `nsgotest` with `nsgotest.Environment(core.Environment{Audio: true})` and configure decoded durations on `app.AudioDurations` to test voice limits, crossfades, persistence and background muting deterministically:

```go
package mygame_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestMixerCrossfadeAndMute(t *testing.T) {
	var mixer *sound.Mixer
	var music *sound.Music

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		music = mixer.Music("music")
		music.Loop = true
		return core.View{}
	},
		nsgotest.Environment(core.Environment{Audio: true}),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				"asset:a.m4a": time.Minute,
				"asset:b.m4a": time.Minute,
			}
		},
	)

	mixer.SetVolume("music", 0.5)
	music.Play(audio.Source{URI: "asset:a.m4a"}, 0)
	music.Play(audio.Source{URI: "asset:b.m4a"}, time.Second)
	app.Advance(1100 * time.Millisecond)

	if !app.Players[0].Closed || !app.Players[1].Playing {
		t.Fatalf("expected track A closed and track B playing")
	}

	app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleBackground})
	app.Settle()
	if app.Players[1].Playing {
		t.Fatal("expected music paused in background")
	}
}
```
