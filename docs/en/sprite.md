# sprite

`game/sprite` loads packed sprite atlases, plays frame animations on a fixed step, and draws Z-ordered sprite batches into an NSGo `core.Canvas`.

```go
import "github.com/nsgo-dev/game/packages/game/sprite"
```

## Atlases from TexturePacker and Aseprite

An `Atlas` pairs one packed image (`Image`) with its named frames (`Frames`) and optional named animations (`Animations`).

`LoadAtlas(img, data)` reads the JSON exported by:

- **TexturePacker**: "JSON (Hash)" and "JSON (Array)"
- **Aseprite**: `json-hash` and `json-array` (with frame tags enabled to populate `Animations`)

Put the packed PNG in `resources/` so NSGo packages it as `"asset:..."`, and embed the JSON with `//go:embed`:

```go
//go:embed resources/hero.json
var heroJSON []byte

atlas, err := sprite.LoadAtlas(core.Src("asset:hero.png"), heroJSON)
if err != nil {
	return err
}
```

### Export settings and scale

- **Turn rotation off**: `LoadAtlas` rejects rotated frames with an error.
- **Keep frame names unique**: duplicate frame names return an error.
- **High-DPI sheets (`meta.scale`)**: all frame rectangles, offsets and source sizes are divided by `meta.scale` (defaults to `1`). A 2× sheet exported with `"scale": "2"` produces coordinates in 1× points, matching NSGo's point coordinates.

### Hand-written atlases

For simple tile sheets, prototypes or tests, build an `Atlas` literal directly without JSON:

```go
sheet := &sprite.Atlas{
	Image: core.Src("asset:sheet.png"),
	Frames: map[string]sprite.Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
	},
}
```

## Trimmed frames

Packers often trim transparent borders around each frame to pack more pictures into one image. `Frame` keeps both the trimmed rectangle in the atlas and its untrimmed box:

```go
type Frame struct {
	Name             string
	X, Y, W, H       float32 // trimmed rectangle in the atlas image, in points
	OffsetX, OffsetY float32 // position of the trimmed rectangle inside the untrimmed box
	Width, Height    float32 // untrimmed frame size (W, H when untrimmed)
}
```

`Sprite` always aligns `OriginX`, `OriginY`, `FlipX`, `FlipY`, rotation and scaling to the untrimmed box (`Width`, `Height`). Trimming never shifts a character's feet or hitbox as animations change from frame to frame.

## Animation

`Animation` advances through a slice of `AnimationFrame` values, each with its own `Duration`, driven by `Update(dt)` inside `game.Loop.Update`:

```go
type Animation struct {
	Frames []AnimationFrame
	Loop   bool
	// ...
}

type AnimationFrame struct {
	Frame    Frame
	Duration time.Duration
}
```

`Animation` is a value that holds its playback state (current frame index, elapsed time, and completion flag). Copy it from `atlas.Animations` so every entity advances independently:

```go
walk := atlas.Animations["walk"]
attack := atlas.Animations["attack"]
```

Methods on `Animation`:

- `Update(dt)`: advances by `dt`, skipping past as many frames (or full loops) as `dt` covers.
- `Frame()`: returns the current `Frame` (zero `Frame` if `Frames` is empty).
- `Index()`: returns the current index in `Frames`.
- `Done()`: reports whether a non-looping animation has finished displaying its final frame for that frame's full duration.
- `Reset()`: rewinds back to frame 0 and clears `Done()`.

### Aseprite frame tags

When loading an Aseprite export, each entry in `meta.frameTags` becomes an entry in `atlas.Animations`:

- Frame durations come from each frame's `duration` in milliseconds (every frame used by a tag must have a positive duration).
- Supported tag directions: `"forward"` (default), `"reverse"`, `"pingpong"` and `"pingpong_reverse"`.
- A tag with no `repeat` (or `repeat: 0`) sets `Loop: true`. A tag with `repeat: N` expands `N` passes into a non-looping `Animation` (`Loop: false`). Ping-pong tags count each direction pass as one repeat and never show the turning frame twice in a row.

### Hand-written animations

For TexturePacker sheets (which carry no frame tags), assemble an `Animation` from `atlas.Frames`:

```go
spin := sprite.Animation{
	Loop: true,
	Frames: []sprite.AnimationFrame{
		{Frame: atlas.Frames["coin_0.png"], Duration: 100 * time.Millisecond},
		{Frame: atlas.Frames["coin_1.png"], Duration: 100 * time.Millisecond},
		{Frame: atlas.Frames["coin_2.png"], Duration: 100 * time.Millisecond},
	},
}
```

## Sprite placement and transforms

A `Sprite` places one `Frame` in a `core.Drawing`. Its zero value has `ScaleX == 0`, `ScaleY == 0` and `Alpha == 0`, so it draws nothing. Start with `sprite.NewSprite(frame)` to get full scale (`1, 1`) and full opacity (`1`):

```go
hero := sprite.NewSprite(walk.Frame())
hero.OriginX = hero.Frame.Width / 2
hero.OriginY = hero.Frame.Height // anchor at the character's feet
hero.X, hero.Y = 120, 200
```

Fields on `Sprite`:

| Field | Meaning |
| --- | --- |
| `Frame` | The `Frame` to draw; a zero frame (`W <= 0` or `H <= 0`) draws nothing. |
| `X`, `Y` | Where the sprite's origin sits in the drawing, in points. |
| `OriginX`, `OriginY` | Pivot point inside the untrimmed frame, from its top left. |
| `Rotation` | Clockwise rotation about the origin, in radians. |
| `ScaleX`, `ScaleY` | Scale about the origin; a negative value mirrors about the origin; `0` draws nothing. |
| `FlipX`, `FlipY` | Mirrors the frame inside its untrimmed box before placement, keeping the origin in place. |
| `Alpha` | Opacity in `[0, 1]`; `<= 0` draws nothing. Replaces the drawing's alpha for this sprite. |
| `Tint` | A `core.Color` that multiplies the frame's red, green, blue and alpha before the sprite is composited; the zero value and opaque white draw the frame unchanged, and any other tint with alpha `0` draws nothing. To fade a sprite out, lower `Alpha`: a tint that reaches transparent black is the zero value and draws the frame unchanged. |
| `Z` | Draw order inside a `Batch`; higher `Z` draws on top of lower `Z`. |

A tinted sprite recolors only its own pixels: it draws with NSGo's `DrawTintedImageFrom`, which multiplies the image's channels by the tint before compositing with the default source-over mode, so transparent pixels leave what is under the sprite unchanged. `Tint` combines with `Alpha`, `Blend` and `Mask` in the same draw:

```go
hit := sprite.NewSprite(atlas.Frames["slime.png"])
hit.Tint = core.RGB(0xff6060) // flash red when damaged
ghost := hit
ghost.Tint = core.RGBA(0x80c0ff, 0.5) // a pale blue at half the sprite's opacity
```

On the web, a cross-origin atlas image must allow anonymous CORS requests for NSGo to tint its pixels. On HarmonyOS, NSGo tints atlases loaded from bytes or the app's resources, but tinting is not yet checked on a device there, and an atlas built from a pixel buffer or an offscreen image draws nothing there yet.

## Batch and Z ordering

A `Batch` draws sprites from a single `Atlas` into a `*core.Drawing`:

```go
batch := &sprite.Batch{
	Atlas:   atlas,
	Sprites: []*sprite.Sprite{&shadow, &hero, &weapon},
}

// Inside core.Canvas.Draw:
batch.Draw(d)
```

- **Ordering**: `Draw` stably sorts sprites into an internal scratch slice by ascending `Z` (lowest `Z` drawn first, equal `Z` in the order `Sprites` lists them). `batch.Sprites` itself is never reordered.
- **Drawing state**: `Draw` leaves `d`'s transform, alpha, composite and clip exactly as it found them.
- **Draw calls**: consecutive sprites, in Z order, that are only moved, rotated and scaled evenly (`ScaleX == ScaleY`, or both flips together) with the default `Blend` and no `Mask` of their own draw with one NSGo `DrawAtlas`. A mirrored, unevenly scaled, blended or masked sprite, and a run of only one sprite, draws with its own `DrawImageFrom`, wrapped in `Save`/`Restore` when it is turned, mirrored, faded, blended or masked.
- **Alpha in a run**: a run of sprites at full opacity keeps the drawing's alpha, as a single sprite does. A run of faded sprites draws between `Save` and `Restore` with the alpha set to `1` and each sprite's own `Alpha`, so `Alpha` still replaces the drawing's alpha. A change from opaque to faded sprites starts a new run.
- **Depth-sorted scene layers**: a `game/scene` layer sorted by Y (`SortY`) or by a custom key draws its sprites one at a time, so each is a run of one and still draws with its own `DrawImageFrom`. Only layers without per-sprite sorting, and batches drawn directly, get `DrawAtlas`.
- **Skipped sprites**: a sprite whose position, origin, rotation, scale or alpha is NaN or infinite draws nothing, as does a batch with no `Atlas`.

The picture is the same as one `DrawImageFrom` per sprite: the tests replay both recordings and compare every drawn rectangle, a fuzz test does so for random batches, and `gametest.RenderOps` renders both to identical pixels.

### Recording cost before and after `DrawAtlas`

`BenchmarkBatchRecord` records a frame of 1,000 and 5,000 moving sprites, a fifth of them rotated and scaled, with one `DrawImageFrom` each (`per-sprite`) and with `DrawAtlas` (`atlas`). It times only recording on the CPU, not the platform's drawing. `sprites/60Hz` is 16.7 ms divided by the recording time per sprite, an upper bound that no frame reaches, not a measured sprite count at 60 frames per second:

```sh
cd packages/game
go tool nsgo test ./sprite -- -run '^$' -bench BatchRecord -benchmem -cpu 1 -benchtime 2s
```

Readings from 2026-11-23 on a shared 24-core x86-64 Linux machine; the times move with the machine's load, the allocations do not:

| Sprites | Recording time per frame | Allocations per frame | Sprites recorded per 16.7 ms |
| ---: | ---: | ---: | ---: |
| 1,000 | 219 µs → 81 µs | 1,000 → 3 | 76,000 → 206,400 |
| 5,000 | 926 µs → 586 µs | 5,000 → 3 | 90,000 → 142,300 |

Recording is the smaller part of a frame: the platform still draws every sprite. The largest sprite count this repository's `Batch` holds at 60 frames per second on a device, before and after, has not been measured yet ([#28](https://github.com/nsgo-dev/game/issues/28)).

NSGo's own [many-sprite comparison](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/performance.md#canvas-atlas-batching) of `DrawAtlas` against one `DrawImageFrom` per sprite, with its own fixture of 32 × 32 crops rather than this `Batch`, measured frame rates at a fixed sprite count: 5,000 sprites rose from 29.3 to 39.9 frames per second on a Pixel 3 XL, from 55.5 to 60.0 in headless Chromium and from 6.6 to 7.8 on macOS, with 1,000 sprites already at 60 frames per second on Android and Chromium. On Linux it has no reliable reading yet: in a shared Ubuntu VM the isolated Cairo replay of 5,000 sprites fell from 20.86 to 15.81 ms, but the end-to-end rates, which moved with the host's load, were lower in the first atlas run (25.54 to 10.83 at 1,000 sprites and 10.37 to 1.98 at 5,000), and NSGo calls for a paired run on a dedicated machine.

## Testing with nsgotest

Because `Batch.Draw` records ordinary `core.DrawOp` operations on a `core.Canvas`, you can inspect what a batch drew headlessly with `nsgotest`:

```go
app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
	return core.Canvas{
		TestID: "stage",
		Style:  core.Style{Width: core.Pt(320), Height: core.Pt(240)},
		Draw: func(d *core.Drawing, _ core.Size) {
			batch.Draw(d)
		},
	}
})
ops := app.Find("stage").Ops
if len(ops) == 0 {
	t.Fatal("expected sprite draw operations")
}
```

A batch records its sprites in `DrawAtlas` operations as well as `DrawImageFrom` ones, so check what it drew with `gametest.ImageDraws(ops)` rather than by counting `DrawImage` operations. It returns each drawn rectangle of an image, with its source rectangle, its corners on the canvas, its alpha, composite, tint, sampling and clip, however it was recorded. `gametest.AssertSameImages(t, got, want)` compares two recordings that way, images only (fills, strokes and texts are checked separately), such as a batch against hand-written `DrawImageFrom` calls, and `gametest.RenderOps` renders `DrawAtlas` too.
