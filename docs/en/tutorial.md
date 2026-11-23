# Tutorial: Star Catcher

Build a whole game, Star Catcher, from an empty project to a release you can ship, in nine chapters. Each chapter adds one idea to the game, and each chapter's finished code is in the repository, where CI builds it for every target and runs its tests.

In Star Catcher, stars and rocks fall down five lanes. The player moves a basket from lane to lane to catch the stars and dodge the rocks. A missed star or a caught rock costs one of three lives, and the game speeds up as the score grows.

## Chapters

| Chapter | You add | Packages | Code |
| --- | --- | --- | --- |
| [1. An empty project](tutorial-01-empty-project.md) | A module, `nsgo.yaml`, a `Canvas` and a first test | `core`, `nsgotest` | [`ch01-empty-project`](../../examples/tutorial/ch01-empty-project/) |
| [2. The game loop](tutorial-02-game-loop.md) | A fixed-step loop, a virtual resolution and a world that tests can step | `game`, `screen` | [`ch02-game-loop`](../../examples/tutorial/ch02-game-loop/) |
| [3. Input](tutorial-03-input.md) | Touch, mouse and keyboard moving the basket through named actions | `input` | [`ch03-input`](../../examples/tutorial/ch03-input/) |
| [4. Sprites](tutorial-04-sprites.md) | A sprite atlas painted in code, sprites, a batch and an animation | `sprite` | [`ch04-sprites`](../../examples/tutorial/ch04-sprites/) |
| [5. Rules](tutorial-05-rules.md) | Random spawns, collisions, score, lives and the end of a game | `random`, `physics` | [`ch05-rules`](../../examples/tutorial/ch05-rules/) |
| [6. Screens](tutorial-06-screens.md) | A title screen, a pause menu and a game-over screen in native views | `core`, `base` | [`ch06-screens`](../../examples/tutorial/ch06-screens/) |
| [7. Sound](tutorial-07-sound.md) | Effects synthesized in Go, a mixer and a mute switch that is remembered | `sound`, `audio` | [`ch07-sound`](../../examples/tutorial/ch07-sound/) |
| [8. Saving](tutorial-08-save.md) | The best score kept across launches in a versioned save slot | `save` | [`ch08-save`](../../examples/tutorial/ch08-save/) |
| [9. Shipping](tutorial-09-ship.md) | An icon, store metadata, a pause when the app loses the focus and release builds | `nsgo build` | [`ch09-ship`](../../examples/tutorial/ch09-ship/) |

Every chapter is a complete program. Its directory holds the whole game as it stands at the end of the chapter, so you can run any chapter on its own, or compare two to see exactly what a chapter changed:

```sh
git diff --no-index examples/tutorial/ch04-sprites examples/tutorial/ch05-rules
```

## Before you start

You need Go 1.27 and NSGo v0.3.0 or later, as [Getting Started](getting-started.md) describes. The tutorial explains each API it uses as it goes; the package guides, such as [Loop](loop.md) and [input](input.md), cover the rest of each package.

To follow along in your own module, start with chapter 1, which creates one. To run the chapters from a checkout of this repository instead, work in `examples/tutorial`, a Go module that holds all nine:

```sh
cd examples/tutorial
go tool nsgo dev ./ch05-rules
go tool nsgo test ./ch05-rules
go tool nsgo test ./...
```

`nsgo dev` runs a chapter on this machine and reloads it when you save; add `-target web`, `-target ios_simulator` or `-target android` to run it elsewhere. `nsgo test` runs a chapter's tests, or every chapter's with `./...`.

## How the game is built

Every chapter keeps the same split, which is worth knowing before you start:

- **`world.go`** holds the game's state and rules, and only its `step` method changes them. It knows nothing about windows, so tests step it directly, and the same seed and moves always play the same game.
- **`game.go`** ties the world to the screen. A `game.Loop` calls `step` sixty times a second with the player's input, and a `Canvas` draws the world in between.
- **`screens.go`**, from chapter 6, builds the native NSGo views over the canvas: the score, the menus and their buttons.
- **`*_test.go`** tests both: the rules through `world`, and the whole app through `nsgotest`, which runs it without a window and lets a test press buttons, type keys, touch the screen and advance time.

Start with [chapter 1](tutorial-01-empty-project.md).
