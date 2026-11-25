# pixelfxref

Renders pixelfx's effects with pixelfx itself, at the commit the Go port follows (`5294518ce7c72e48b6100a744596e0acf57f2826`), into the reference PNGs that the parity tests of `packages/game/pixelfx` compare the port with. It is run by hand when a category is ported or its references need rendering again; nothing in the Go build or CI runs it, and its output is committed.

## Running it

It needs Node 18 or newer and a clone of pixelfx at the pinned commit. pixelfx's render code has no dependencies, so do not run its npm install scripts: the tool imports `src/core`, the effect files and `tools/lib` straight from the clone.

```sh
git clone https://github.com/patch-gg/pixelfx ../pixelfx
git -C ../pixelfx switch --detach 5294518ce7c72e48b6100a744596e0acf57f2826

node tools/pixelfxref/render.mjs --pixelfx ../pixelfx fire               # every effect of a category
node tools/pixelfxref/render.mjs --pixelfx ../pixelfx fire/torch moves/ground-pound
node tools/pixelfxref/render.mjs --pixelfx ../pixelfx rig core           # the core's own references
```

It refuses a clone at any other commit. For each effect it deletes the effect's old references and writes new ones to `packages/game/pixelfx/<category>/testdata/ref/<name>/` (or under `--out <dir>`), and `rig` and `core` write `packages/game/pixelfx/testdata/ref/rig/` and `testdata/ref/core/`.

## What it renders

Each case of each effect runs in a fresh Node process, so the rig's cache of drawn poses starts empty, as the Go port's `Rig` does in the tests. The cases, which `packages/game/pixelfx/internal/pixelfxtest` renders the same way:

- `default-<frame>.png`: the effect with its default options, rendered as pixelfx's `tools/lib/fxlib.mjs` renders a sprite sheet (at its own frame rate, no margin, no backdrop, light eased out at the edges unless it fills the screen), at the frames 25%, 50% and 75% of the way through it.
- `flip-<frame>.png`: the same, mirrored.
- `act-<frame>.png`, for an effect with an act: the effect acted out as pixelfx's `renderAct` does (the stock character and the straw target, 60 fps), without the backdrop or the shake, at the first frames whose time reaches 25%, 50% and 75% of the way from the first pose to the end.

`<frame>` is the frame's 0-based index in three digits. The rig references draw each of a fixed list of poses (every weapon, both hands, turns, the air and the switches) with its feet at (100, 110) of a 200 x 140 image as `<pose>.png`, and beside it `<pose>-src.png`, each pixel's source in the standing character plus 1 in red and green and ink in blue. The core references draw every shape of the painter with its origin at (80, 60) of a 160 x 120 image, placed plainly, mirrored and turned by 0.3 radians (`shapes-<case>.png`), and a simulation of every particle kind, debris kind, a light and a blaze after 30, 72 and 120 steps of 1/120 s (`sim-<step>.png`). The poses and the scenes are written twice, in `render.mjs` and in `packages/game/pixelfx/rig_test.go` and `core_test.go`; keep them in step, as the cases are in `render.mjs` and `pixelfxtest`.
