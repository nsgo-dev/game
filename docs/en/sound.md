# sound

`game/sound` layers a persistent volume `Mixer`, voice-limited sound `Effect`s, crossfading `Music`, and automatic background muting on top of NSGo's `audio` package.

```go
import (
	"github.com/nsgo-dev/game/packages/game/sound"
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
- `Gain(pos)` is `1` within `Near`, `0` at `Far` and beyond, and falls between them by `Rolloff`: `RolloffLinear` (the default) in a straight line, `RolloffInverse` as `Near / distance` rescaled to reach `0` at `Far`. A `Far` that is not greater than `Near`, infinite or `NaN` turns attenuation off.
- `Pan(pos)` is the effect's offset across the view divided by `PanWidth`, clamped to `[-1, 1]`; a `PanWidth` of `0` takes `Far`. An effect straight above or below the camera plays centered.
- `PlayAt(l, pos, v)` plays with `v.Volume * l.Gain(pos)`, then the group and master volumes as `Play` does, and `v.Pan` replaced by `l.Pan(pos)`. It returns `false` and plays nothing beyond `Far`, so silent effects never take a voice or count against `Limit`.
- `Cue.PlayAt(l, pos)` picks the cue's next variant as `Play` does and plays it through its effect's `PlayAt`, at the variant's volume and pitch, so randomized footsteps and hits can be placed too.
- `Voice(pos, v)` returns the voice `PlayAt` would play with, for playing an `audio.Sound` directly. Where the effect is silent it sets `Muted`, since `audio` plays a volume of `0` at full volume.
- The camera's rotation only turns the pan; distance ignores it, and an infinite or `NaN` `Rotation` counts as `0`.

The pan and volume are fixed when the playback starts, because NSGo's `audio.Playback` cannot change its pan once playing. Suit it to short effects; a long sound that follows a moving source would need to be replayed.

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

On the web, browsers enforce autoplay policies and refuse playback (`audio.NotAllowed`) until the user interacts with the page; start music from a button press or first tap/click.

## Background muting

`NewMixer` watches `cx.Env().Lifecycle()`:

- When the app enters **`core.LifecycleBackground`** (minimized, hidden or covered on desktop; backgrounded on iOS or Android), the mixer silences the effects playing, drops new `Effect.Play` calls, and pauses music.
- When the window returns to **`core.LifecycleActive`** or **`core.LifecycleInactive`** (visible, even unfocused on desktop), music resumes from where it paused, effects still playing are heard again at their volume, and new effects can play.

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

	"github.com/nsgo-dev/game/packages/game/sound"
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
