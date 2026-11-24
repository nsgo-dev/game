# Tutorial 4: Sprites

Draw the basket and the star as sprites: paint a sprite sheet, describe its frames in a `sprite.Atlas`, play the star's twinkle with a `sprite.Animation`, and draw everything with one `sprite.Batch`.

The finished code is in [`examples/tutorial/ch04-sprites`](../../examples/tutorial/ch04-sprites/).

## An atlas

An atlas is one image that packs many pictures, and the frames that say where each one is. Games usually export it from a packer such as TexturePacker or Aseprite and load it with `sprite.LoadAtlas`, as the [sprite guide](sprite.md) shows. So that the tutorial needs no image files, Star Catcher paints its sheet in Go instead, in `art.go`, and writes the table by hand:

```go
func makeAtlas() *sprite.Atlas {
	sheet := image.NewRGBA(image.Rect(0, 0, sheetWidth, sheetHeight))
	paintBasket(sheet, 0)
	paintStar(sheet, basketWidth, 1, color.RGBA{R: 0xfd, G: 0xe0, B: 0x47, A: 0xff})
	paintStar(sheet, basketWidth+itemSize, 0.8, color.RGBA{R: 0xfa, G: 0xcc, B: 0x15, A: 0xff})

	basket := frame("basket", 0, basketWidth, basketHeight)
	star0 := frame("star_0", basketWidth, itemSize, itemSize)
	star1 := frame("star_1", basketWidth+itemSize, itemSize, itemSize)
	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{basket.Name: basket, star0.Name: star0, star1.Name: star1},
		Animations: map[string]sprite.Animation{
			"twinkle": {
				Loop: true,
				Frames: []sprite.AnimationFrame{
					{Frame: star0, Duration: twinkleFrame},
					{Frame: star1, Duration: twinkleFrame},
				},
			},
		},
	}
}
```

- The sheet is one row: the basket, a bright star and a dimmer, smaller one. `paintStar` fills the pixels inside a five-pointed star; `paintBasket` a woven bowl.
- `core.ImageSource{Pixels: sheet}` hands the pixels to the platform, which uploads them once.
- `frame` makes an untrimmed frame with its pivot in its middle, so a sprite of it turns about its center.

To try art from files without drawing any, start from the CC0 [placeholder pack](placeholder.md): `placeholder.Sheet("player")` loads a sprite sheet and its Aseprite export through `sprite.LoadAtlas`, animations included, and the pack's files can be copied into a game and replaced one by one.

## Sprites and a batch

A `sprite.Sprite` places a frame: where its origin goes, how it turns, scales, flips and fades. A `sprite.Batch` draws sprites of one atlas, in order:

```go
atlas := makeAtlas()
s.twinkle = atlas.Animations["twinkle"]
s.basket = sprite.NewSprite(atlas.Frames["basket"])
s.basket.OriginY = 0
s.star = sprite.NewSprite(s.twinkle.Frame())
s.batch = sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&s.basket, &s.star}}
```

`NewSprite` puts the origin at the frame's pivot. The basket moves its origin to its top edge, so its `Y` is the top of the basket, as in the world.

Drawing now places the two sprites and draws the batch:

```go
s.basket.X, s.basket.Y = w.basketAt(a), basketY
s.star.X, s.star.Y = laneX(w.starLane), w.starAt(a)
// The star turns a little as it falls.
s.star.Rotation = s.star.Y / 60
s.batch.Draw(d)
```

## Animation

An `Animation` is a value that keeps its own place in its frames. Copy one out of the atlas, move it on with the loop's `dt` in `update`, and show its current frame:

```go
s.world.step(dt, move)
s.twinkle.Update(dt)
s.star.Frame = s.twinkle.Frame()
```

Because it moves with the fixed step, the twinkle keeps time with the game, and stops when the game pauses in [chapter 6](tutorial-06-screens.md).

Run the chapter: the basket is a basket now, and the star twinkles and turns as it falls.

## Test the art

The tests check the atlas itself, that every frame is painted in its middle and transparent in its corner, and that the star changes frame after one twinkle and comes back after two:

```go
func TestStarTwinkles(t *testing.T) {
	var s *starCatcher
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.ManualFrames())

	first := s.star.Frame.Name
	s.update(twinkleFrame)
	if s.star.Frame.Name == first {
		t.Fatalf("star still shows %s after a twinkle frame", first)
	}
	s.update(twinkleFrame)
	if s.star.Frame.Name != first {
		t.Fatalf("star shows %s after two twinkle frames, want %s again", s.star.Frame.Name, first)
	}
}
```

`nsgotest.ManualFrames` runs display frames only when the test advances the clock, so the loop does not call `update` on its own while the test calls it. An app test also reads the images the canvas draws with `gametest.ImageDraws`, which sees each sprite of the batch's single `DrawAtlas`: two, the basket and the star.

## Next

One star falling forever is not yet a game. [Chapter 5](tutorial-05-rules.md) adds its rules.
