# Tutorial 9: Shipping

Get Star Catcher ready for players: give it a name, version and icon for the stores, pause it when the app loses the focus, play whole games in a test before each release, check every target, and build release packages.

The finished game is in [`examples/tutorial/ch09-ship`](../../examples/tutorial/ch09-ship/). [Building and Shipping](shipping.md) covers each step below in more depth, signing and store uploads included.

## Name, version and icon

`nsgo.yaml` now says what the stores show:

```yaml
name: Star Catcher
id: dev.nsgo.starcatcher
version: 1.0.0
build_number: 1
copyright: "© 2026 Star Catcher authors"

icon:
  png: assets/Icon.png

dev:
  target: native

build:
  output: dist
  targets: [macos, windows, linux, android, web, ios]
```

- `id` must stay the same in every release: the App Store, Google Play and the Microsoft Store know the app by it. Use a reverse-DNS name of a domain you own.
- `version` is what players see; `build_number` goes up by one for every upload to a store.
- `icon.png` is a 1024-pixel square PNG, which `nsgo build` turns into every size each platform wants. Add `icon.apple`, an Icon Composer `.icon`, for the layered icons of macOS and iOS.

## An icon from code

The icon is drawn by the same kind of code as the sprites, in `internal/icon`, and written by a small tool:

```go
//go:generate go run ./tools/genicon assets/Icon.png
```

`go generate ./...` draws it again after a change. The PNG is checked in, because `nsgo build` reads it from `assets/`; a test keeps the two from drifting apart:

```go
func TestIconIsUpToDate(t *testing.T) {
	data, err := os.ReadFile("assets/Icon.png")
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("assets/Icon.png: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("icon is %dx%d, want 1024x1024", b.Dx(), b.Dy())
	}
	want := icon.Render(1024)
	// Compare every pixel of got and want…
}
```

A game with drawn art keeps its icon in `assets/` and skips the generator.

## Pause when the app loses the focus

`game.Loop` already runs no updates while the app is in the background, but it keeps stepping while the app is only inactive: while a call rings, while the app switcher is open, or while another window has the focus on a desktop. A player who looks away should come back to the pause menu, not to stars that fell while they were gone. An effect watches the app's lifecycle and pauses a game in play whenever the app is not active:

```go
cx.Effect(func() {
	if cx.Env().Lifecycle() != core.LifecycleActive && s.phase.GetUntracked() == phasePlaying {
		s.setPhase(phasePaused)
	}
})
```

`cx.Effect` runs its function again whenever a signal it read changes. It reads the lifecycle and nothing else: the phase is read untracked, so starting or pausing a game does not run it.

`TestLosingTheFocusPausesTheGame` makes the app inactive, then sends it to the background, with `app.SetEnvironment`, brings it back each time, and finds the pause menu.

## Play whole games before a release

Unit tests check one rule at a time. Before a release, it is worth knowing that the rules add up to a game: one that can be played, gets harder, and ends. `TestAWholeGame` plays whole games of three seeds with a simple bot, which heads for the lowest star and steps aside from a rock about to land in the basket:

```go
func TestAWholeGame(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		w := newWorld(seed)
		steps := 0
		for !w.over && steps < 60*60*20 {
			w.step(step, bot(w))
			steps++
		}
		if !w.over {
			t.Fatalf("seed %d: a game lasted over 20 minutes", seed)
		}
		if w.score < 20 {
			t.Fatalf("seed %d: the bot caught only %d stars", seed, w.score)
		}
	}
}
```

Because the world is deterministic, each seed plays the same game every run, so the test never flakes; and because it steps the world directly, twenty minutes of play take milliseconds. If a change to the rules makes the game unwinnable or endless, this test says so before the players do.

## Check every target

`nsgo check` vets the code for each target, catching a call that does not build on one of them:

```sh
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo check -target ios,ios_simulator   # on a Mac with Xcode
```

The repository's CI runs both for every chapter on each change, with every chapter's tests on Linux and macOS, as `.github/workflows/examples.yml` shows.

## Build release packages

`nsgo build` builds the targets `nsgo.yaml` lists, into one directory per target under `dist/`, with installers and archives beside them:

```sh
go tool nsgo build
go tool nsgo build -target web
go tool nsgo build -channel store -target macos,windows,ios,android
```

The web build is a folder to put on any static host. Try it locally with `go tool nsgo build -target web -run`, which serves it and opens a browser. The `store` channel makes the packages the Mac App Store, Microsoft Store, App Store and Google Play take; [Building and Shipping](shipping.md) explains signing and uploading them.

## Where next

Star Catcher is finished: it opens on any of six platforms, plays the same at any frame rate, takes touch, mouse and keyboard, sounds, remembers, pauses, and is tested from its rules to its whole games. To grow it, or to start a game of your own:

- Load drawn art with `sprite.LoadAtlas` and recorded sounds by `"asset:"` URI, as [Getting Started](getting-started.md) shows.
- Add bursts when a star is caught with [particles](particles.md), and a pop on the score with [tween](tween.md).
- Let players remap the controls, and save them, with [input](input.md)'s `Save` and `Load`.
- Record a game's inputs and replay it in a test with [replay](replay.md).
