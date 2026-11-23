# Packages

What each package holds and how its parts behave, from the packages' doc comments. The full API is in the Go doc of each package.

## Loop

```go
import "github.com/nsgo-dev/game/packages/game"
```

`Loop` runs a game's updates at a fixed rate on a scope's display frames, and draws once a frame in between.

```go
type Loop struct {
	Rate     int                     // updates a second, 60 when 0
	MaxSteps int                     // most updates one frame runs, Rate/10 and at least 1 when 0
	Update   func(dt time.Duration)  // moves the game on by dt, always 1/Rate seconds
	Draw     func(alpha float64)     // draws the frame, alpha in [0, 1)
}

func (l *Loop) Run(cx *core.Scope) (stop func())
```

- Each frame adds the time since the last one and runs `Update` once for each whole step of `1/Rate` seconds it holds, then `Draw` once with how far the time left over reaches into the next step. A game moves its state in `Update` by the fixed `dt`, so it plays alike at any display rate, and `Draw` places what moves between the last two states by `alpha`, so it moves smoothly.
- A frame that would run more than `MaxSteps` updates drops the whole steps past it, so an `Update` slower than its step, or a long hitch, slows the game rather than making each frame run more updates than the last.
- `Run` starts the loop until the scope is disposed or `stop` is called. It reads the fields on each frame, so a game may change them while the loop runs.
- The loop pauses while the scope's window is in the background (`core.Env.Lifecycle`) and resumes from the time it shows again, so the time away runs no updates.
- A frame's time is the display's frame time where the platform stamps one, and the graph clock's otherwise; the loop adds it as it is.

## sprite

```go
import "github.com/nsgo-dev/game/packages/game/sprite"
```

Atlases, frames, animations, sprites and batches drawn into a `Canvas`.

### Atlas and Frame

An `Atlas` is an image that packs many pictures (`Image`), the frames that say where each one is (`Frames`, by name), and its animations (`Animations`, by name).

`LoadAtlas(img, data)` reads the JSON atlas data that TexturePacker ("JSON (Hash)" and "JSON (Array)") or Aseprite (json-hash and json-array) exports for the image it packed:

- Rectangles are in the image's pixels, divided by the export's `meta.scale`, so the image measures its points as a 1x image does.
- Aseprite's frame tags become `Animations`, each frame for the duration the export gives it, in the tag's direction, looping unless the tag has a repeat count.
- Frames a packer rotated are refused: export with rotation off.

A game can also write the table by hand as an `Atlas` literal. A `Frame` is one picture: the part of the image it draws (`X`, `Y`, `W`, `H`), where that part sits in the untrimmed picture (`OffsetX`, `OffsetY`), and the untrimmed size (`Width`, `Height`).

### Animation

An `Animation` plays frames one after another, each for its own duration, moved on by `Update` with a `Loop`'s fixed steps. It is a value that keeps where it is in its frames, so each sprite plays its own copy: copy one from an atlas's `Animations`, or write one by hand.

- `Update(dt)` moves it on past as many frames as `dt` covers.
- `Frame()` is the frame showing now, `Index()` its index, and `Done()` reports whether an animation that does not loop has shown its last frame for that frame's duration.
- `Reset()` starts it over.

### Sprite and Batch

A `Sprite` is a frame placed in a drawing: moved (`X`, `Y`), turned (`Rotation`, in radians, clockwise), scaled (`ScaleX`, `ScaleY`; negative mirrors), flipped inside its untrimmed frame (`FlipX`, `FlipY`), faded (`Alpha`) and tinted (`Tint`) about its origin (`OriginX`, `OriginY`). `Z` orders the sprites of a batch.

Its zero value draws nothing; `NewSprite(f)` returns one at full size and opacity. A tint multiplies only the sprite's own pixels, leaving what is drawn under it unchanged.

A `Batch` draws sprites of one atlas into a `core.Drawing`, lowest `Z` first and in the batch's order for the same `Z`, with one `DrawImageFrom` each, or `DrawTintedImageFrom` for a tinted sprite. It leaves the drawing's state as it found it. Drawing a batch with NSGo's batched `DrawAtlas` is [#28](https://github.com/nsgo-dev/game/issues/28).

```go
walk := atlas.Animations["walk"]
hero := sprite.NewSprite(walk.Frame())
batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&hero}}

loop := &game.Loop{
	Update: func(dt time.Duration) {
		walk.Update(dt)
		hero.Frame = walk.Frame()
	},
}
// In a core.Canvas's Draw:
batch.Draw(d)
```

## sound

```go
import "github.com/nsgo-dev/game/packages/game/sound"
```

The `Mixer`, its effects and music, on top of NSGo's `audio` package. Its methods are called on the engine's goroutine, as `audio`'s are.

### Mixer

A `Mixer` sets how loud a game's sounds and music play: a master volume and a volume for each group the app names, such as `"effects"`, `"music"` and `"ui"`, each from 0 to 1 and 1 until set.

- `NewMixer(cx, key)` keeps the volumes in the app's storage (`core.Storage`) under keys starting with `key`, so they hold across launches.
- `Master`, `SetMaster`, `Volume(group)` and `SetVolume(group, v)` read and set them. Read in a computation, a volume subscribes it to changes; a set fails with a `*core.StorageFullError` when the storage is full.
- An effect plays at its own voice's volume times its group's and the master volume, and music at its group's and the master volume.
- A volume change applies at once to the effects playing and to music as it plays. An effect whose group or master volume reaches 0 plays on in silence and is heard again as the volume comes back.
- While the window is in the background, the mixer silences the effects playing and pauses the music; when the window shows again, it restores the effects still playing and plays the music on from there. `Suspend()` does the same until the `resume` it returns is called, as a cutscene does while it plays ([cutscene](cutscene.md)).

### Effect

`mixer.Effect(s, group, limit)` plays an `*audio.Sound` through the mixer in a group, at most `limit` playbacks at once (0 for no limit of its own, under the platform's 32 for all sounds), so a burst of one sound does not take every voice.

`Play(voice)` starts a playback and reports whether it did. It plays nothing while the volume is 0 or while the app is in the background. With `Limit` playbacks still playing, it stops the oldest to make room. `Stop()` stops every playback of the sound.

### Music

`mixer.Music(group)` plays long tracks through the mixer, one at a time, each through an `audio.Player`.

- `Play(src, fade)` fades the new track in over `fade` as the tracks playing fade out, by the display's frames, closing them as they reach silence; it switches at once when `fade` is 0.
- `Stop(fade)` fades the music out.

```go
mixer := sound.NewMixer(cx, "sound")
music := mixer.Music("music")
music.Play(audio.Source{URI: "asset:theme.m4a"}, 2*time.Second)
```
