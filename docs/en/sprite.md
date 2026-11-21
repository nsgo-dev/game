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
| `Z` | Draw order inside a `Batch`; higher `Z` draws on top of lower `Z`. |

A sprite does not yet support color tinting ([#29](https://github.com/nsgo-dev/game/issues/29)).

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
- **Drawing state**: `Draw` leaves `d`'s transform and alpha exactly as it found them, wrapping transformed, mirrored or translucent sprites in `Save`/`Restore`. Unrotated, unmirrored sprites at full opacity draw directly without pushing a transform.
- **Draw calls**: today each visible sprite emits one `DrawImageFrom`; NSGo's batched `DrawAtlas` is tracked in [#28](https://github.com/nsgo-dev/game/issues/28).

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
