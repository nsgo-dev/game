# pixelfx

`game/pixelfx` is a Go port of [pixelfx](https://github.com/patch-gg/pixelfx), a library of pixel art effects and animations under the MIT license. Each effect is a small procedural program (fire, explosions, magic, weather, UI, pickups, transitions, critters, and character moves acted out with wind-up, hit-stop and recovery) that takes a palette, a seed and placement options and draws a frame with a back layer, a front layer and additive light. A game built on NSGo plays, recolors and mirrors them at run time and exports them as sprite sheets, with no JavaScript.

```go
import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/fire" // registers fire/torch and the rest of its category
)
```

The port follows pixelfx at commit `5294518ce7c72e48b6100a744596e0acf57f2826` with the same math in float64 and in the same order, so it draws the same pixels: every category's tests compare its effects with frames pixelfx itself rendered (see [Parity with pixelfx](#parity-with-pixelfx)).

## Packages

| Package | What it holds |
| --- | --- |
| `game/pixelfx` | The core: frames and compositing, the painter, randomness and noise, particles, the simulation, colors and palettes, the two pixel fonts, the acting system (rig, poses, acts, chains), the effect registry, sprite sheet export and the view helper |
| `game/pixelfx/fire` | Fire: the torch so far |
| `game/pixelfx/explosions` | Explosions: the small explosion so far |
| `game/pixelfx/impacts` | Impacts: the ground slam so far |
| `game/pixelfx/moves` | Moves acted out by a character: the ground pound so far |
| `game/pixelfx/pickups` | Pickups: chest open, coin spin, collect burst, crystal shard, gem spin, heart pickup, key glint, loot beam, potion, power star and XP orb |
| `game/pixelfx/projectiles` | Projectiles (13 effects): `arrow`, `bullet-tracer`, `cannonball`, `energy-disc`, `flare`, `homing-missile`, `laser-bolt`, `magic-missile`, `muzzle-pistol`, `muzzle-shotgun`, `muzzle-smg`, `plasma-shot`, `rocket` |
| `game/pixelfx/spawn` | Spawn and death: coin drop, disintegrate, dissolve burn, egg hatch, poof, respawn beam, shatter death, soul rise, spawn portal and summon rise |

Effects live in one subpackage per category, which registers its effects as `"<category>/<name>"` when it is imported, so a game links only the categories it imports. The other categories of pixelfx (water, ice, lightning, sci-fi, melee, arcane, holy, shadow, status, nature, weather, ui, transitions, critters) follow one change each, as [Porting a category](#porting-a-category) describes.

## Playing an effect

An `Effect` holds its `Meta` (name, description, tags, frame size, the origin its local (0, 0) sits at, duration, loop or one-shot, frame rate, preview backdrop, stock actor and its options with their defaults) and `New`, which starts it. `NewPlayer` starts one with `Options`: `X` and `Y` place its origin in the frame, `Flip` mirrors it, `Angle` turns it, `Light` scales its glow, and `Values` sets its own options by name over the defaults, such as `seed` and `palette`. A palette of a different length than the default is resampled to it, so any ramp recolors any effect. `Set` checks nothing itself: a value whose kind the option does not take (a number stands for a bool, and a color for a palette of that one color) or an empty palette is ignored and the default kept, so compare a value with `Meta.Default` first to report a mistake.

```go
e, _ := pixelfx.Lookup("fire/torch")
p := pixelfx.NewPlayer(e, pixelfx.Options{X: 36, Y: 94}.
	Set("palette", pixelfx.RampOf(pixelfx.Ramps["ice"])).
	Set("seed", pixelfx.Num(9)))
frame := pixelfx.NewFrame(e.Meta.Size[0], e.Meta.Size[1], 0, 0)

// each frame of the game:
p.Update(dt)
frame.Clear()
p.Draw(frame, pixelfx.LayerBoth)
```

`Update` steps simulations at 120 Hz or finer whatever the game's frame rate, so an effect looks the same at any frame rate. `Draw` with `LayerBack` draws what goes behind a character and `LayerFront` what goes in front, so a character can stand between them. `Stop` lets a loop fade out over 0.3 seconds and cuts a one-shot short; `Done` reports when it has ended.

A frame holds opaque color (`Color`) and additive light (`Light`). `Composite` puts them together as RGBA bytes over transparency, a flat color or a backdrop: over transparency, light becomes color with alpha, which reads as additive over dark backgrounds.

## Showing frames in NSGo

`Image` turns frames into an NSGo image source: `Set` composites a frame over transparency into premultiplied pixels and steps the source's version, so the same `core.Image`, canvas or shader shows each new frame in place. `View` is a canvas that draws a source scaled up with smoothing off, as pixel art wants; any scale is accepted, and a whole number keeps every pixel the same size:

```go
var img pixelfx.Image
src := core.NewSignalFunc(cx, core.ImageSource{}, nil)

// in the game's frame callback, after drawing the frame:
src.Set(img.Set(frame))

// in the UI:
pixelfx.View{Source: src.Get, Scale: 4}
```

`DrawScaled` draws one source the same way inside a canvas of your own.

## Sprite sheets

`Render` renders every frame of an effect the way pixelfx renders a sprite sheet: at its own frame rate (or the one given), round(duration × fps) frames, one more for a one-shot so its last frame is its end, with light eased out over the 8 pixels at each edge unless the effect fills the screen. `Frames.Sheet` lays them out in a grid and writes Aseprite JSON (json-array) that `sprite.LoadAtlas` reads: each frame's rectangle, its duration (1000 / fps milliseconds, rounded) and its pivot at the effect's origin, one frame tag that loops a loop and plays a one-shot once, and pixelfx's own meta (`id`, `fps`, `loop`, `frameSize`, `pivot`, `columns`, and an act's timing). The sheet stores straight alpha, as PNG does.

```sh
go tool game pixelfx -out assets/fx fire/torch
go tool game pixelfx -set seed=4 -set palette=#102,#215,#43a,#8cf,#fff -flip -out assets/fx -name frost-torch fire/torch
go tool game pixelfx -act -out assets/fx moves/ground-pound
go tool game pixelfx -list
```

`game pixelfx` writes `<name>.png` and `<name>.json` into `-out` (the effect's name, such as `torch`, unless `-name` says otherwise). `-set name=value` sets one of the effect's options: a number, `true` or `false`, a color such as `#4a3f69`, a ramp of colors separated by commas, or text; `-list` lists the effects with their options and defaults. `-act` renders the effect acted out, the stock character performing it with its target, at 60 frames per second unless `-fps` says otherwise.

```go
atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:fx/torch.png"}, torchJSON)
anim := atlas.Animations["torch"] // loops, 30 fps, pivot at the torch's origin
```

## Acting

An effect a character acts out has an `Act`: the poses over time (`Key`s, each arriving smoothly, `"out"`, `"in"`, `"line"` or with a `"snap"`), the hits that freeze the picture, shake it and push the straw target, the dust kicked up, and where the targets stand. `Perform` plays it with the stock character, drawn from a 3D skeleton (`Rig`, `Pose`) down to pixels: a pose only says where the hands and feet go and how the body carries itself, and the solver works out every joint. Limbs move on 12 fps steps from each key, so they read as held drawings, while the feet move smoothly.

```go
rig := pixelfx.NewRig()
w, h, x, y := pixelfx.ActFrameSize(moves.GroundPound, rig)
frame := pixelfx.NewFrame(w, h, 0, 0)
pf := pixelfx.Perform(moves.GroundPound, pixelfx.Options{X: x, Y: y}, rig)
for !pf.Done() {
	pf.Update(1.0 / 60) // holds still while a hit freezes the picture
	frame.Clear()
	pf.Draw(frame, true, true)
	sx, sy := pf.Shake() // how far to throw the picture
	_, _ = sx, sy
}
```

A game with its own character reads the same timing as plain data with `ActTiming`: the wind-up before the effect starts, the recovery after it, each hit's freeze, shake and push, and the poses. A `Rig` caches the poses it draws as pixelfx does, keyed by every value rounded to a pixel, so give `ActBounds` and `Perform` the same one, as `ActFrameSize` and the sheet export do.

`Chain` makes one effect of several, each started at its own time and place, with x and y either numbers or functions of the move's time: a projectile is an effect whose x moves. The moves category is chains of the other categories' effects with an act on top.

## Determinism

An effect played with the same options and the same updates draws the same frames on every run: its randomness comes from `Rng` (pixelfx's mulberry32, the same sequence for the same seed) and `Hash`. The tests of each category render each effect twice and compare the bytes. JavaScript's 32-bit conversions (`x | 0`, `x >>> 0`, `Math.round`, `Math.hypot`) are reproduced by `ToInt32`, `ToUint32`, `Round` and `Hypot`.

Frames can differ by a step of a channel between machines: Go may fuse a multiply and an add into one instruction on arm64, which amd64 does not, and its `math.Sin`, `math.Cos`, `math.Atan2`, `math.Exp` and `math.Pow` may differ from V8's in the last bit.

## Parity with pixelfx

`tools/pixelfxref` renders effects with pixelfx itself, at the pinned commit, into reference PNGs, and `pixelfx/internal/pixelfxtest` renders the same effects with the port and compares them. Both sides render three cases of each effect:

- `default`: the effect with its default options, as a sprite sheet renders it, at the frames 25%, 50% and 75% of the way through it;
- `flip`: the same, mirrored;
- `act`, for an effect a character acts out: the performance at 60 fps, at the first frames whose time reaches 25%, 50% and 75% of the way from the first pose to the end (two loops for a loop).

The references are `<case>-<frame>.png` in `testdata/ref/<name>` of the effect's category package, and the core has references of its own: the rig drawing a set of poses (every weapon, both hands, turns, the air) in `testdata/ref/rig`, and every shape of the painter (placed plainly, mirrored and turned) and every particle, debris and light of the simulation in `testdata/ref/core`. A frame matches when at most 0.5% of its pixels differ from the reference by more than 2 in any channel; the tolerance is `pixelfxtest.MaxChannelDelta` and `pixelfxtest.MaxDifferingShare`, documented in one place, and only loosened with a written reason. On amd64 the ported effects match to the bit but for the odd pixel, and on arm64 within a step of a channel but for the odd pixel the fused instructions move across an edge. Set `PIXELFXTEST_OUT` to a directory to have frames that differ written there.

## Porting a category

Each category is one change against its issue. To port one:

1. Clone pixelfx and move it to the pinned commit: `git clone https://github.com/patch-gg/pixelfx` and `git -C pixelfx switch --detach 5294518ce7c72e48b6100a744596e0acf57f2826`. Do not run its npm install scripts; the reference tool needs only Node 18 or newer, and pixelfx's render code has no dependencies.
2. Port the category's `_lib.js` into `lib.go` of `packages/game/pixelfx/<category>` (the subpackage may already hold a few of its functions from the core change) and each effect into a file of its own named after it (`fire-ring.js` becomes `fire_ring.go`), with the upstream file and commit, the MIT license and any Sprite Fusion notice the file carries at its top. Keep pixelfx's math, the order of its float operations and of its calls to the random generator: arguments and calls are evaluated left to right in Go as in JavaScript, but a JavaScript loop condition that draws a random number draws it on every pass, and a constant expression such as `Math.PI + 0.07` must use a variable for π (Go folds constants exactly at compile time, JavaScript rounds each step).
3. Export each effect as a variable of the package (`var FireRing = &pixelfx.Effect{...}`) and register it in an `init` as `"<category>/<name>"`. A JavaScript color or alpha that is a function becomes the painter's `...Fn` method; options become `Meta.Options` with their defaults; an `act` becomes `Act`, with `Opt` for a hit's values pixelfx defaults when they are left out.
4. Render the references: `node tools/pixelfxref/render.mjs --pixelfx <clone> <category>`, and add the category's `parity_test.go` calling `pixelfxtest.CheckEffect` and `pixelfxtest.CheckDeterministic` for each effect, with a `pixelfxtest.BenchmarkFrames` benchmark, as `pixelfx/fire` does.
5. Import the category in `packages/game/cmd/game/pixelfx.go` so `game pixelfx` renders it, keep its row in the stability tables and its `example_test.go`, update the table above and its Chinese twin, and add a changelog fragment.

When a frame differs beyond the tolerance, write it out with `PIXELFXTEST_OUT` and compare it with the reference: a shape out of place is a porting mistake (often the order of random draws), a step of a channel here and there is floating point.

## License

The port is under pixelfx's MIT license: `packages/game/pixelfx/LICENSE` holds its text and `NOTICE.md` its notices, Sprite Fusion's for the simulation and the effects adapted from its VFX, and Hollow Crown's for the acting system. The Patch Pixel font of pixelfx's gallery is not MIT licensed and is not ported; the two pixel fonts drawn in code are.
