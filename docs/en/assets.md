# assets

`game/assets` loads sprite atlases, sounds, images, raw bytes, and custom game data asynchronously off the engine goroutine, organizes them into per-scene groups with reactive progress signals, releases them cleanly when a scene ends, and provides a ready-made `LoadingScreen` component.

```go
import "github.com/nsgo-dev/game/packages/game/assets"
```

All `Manager` and `Group` methods run on the engine goroutine; file reading and parsing run in background goroutines and post results back via `core.Scope.Post` so animations and `game.Loop` never hitch.

## Creating a Manager and defining groups

Create a `Manager` bound to a `*core.Scope` (typically your root game component's scope). Pass `WithFS` to serve `"asset:"` URIs from an `embed.FS` or `os.DirFS`, or `WithReadAsset` for custom storage:

```go
//go:embed assets/*
var assetFS embed.FS

mgr := assets.NewManager(cx, assets.WithFS(assetFS))

// Register a scene's assets as a named group
level1 := mgr.Group("level1").
	AddAtlas("hero", "asset:hero.json", core.Src("asset:hero.png")).
	AddSound("jump", audio.Source{URI: "asset:sounds/jump.wav"}).
	AddImage("bg", core.Src("asset:bg.png")).
	AddCustom("map", "asset:maps/level1.tmj", parseMap, nil)
```

Chainable builders on `*Group`:

| Method | What it loads |
| --- | --- |
| `AddAtlas(name, jsonURI, img)` | Reads & parses `sprite.LoadAtlas` off-thread, and prefetches `img` when URI-backed. |
| `AddSound(name, src)` | Decodes a short sound via `audio.LoadSound`. |
| `AddImage(name, src)` | Prefetches a URI-backed `core.ImageSource` via `core.Scope.PrefetchImage`. |
| `AddBytes(name, uri)` | Reads raw bytes from `uri` off-thread. |
| `AddCustom(name, uri, parse, release)` | Reads `uri` and parses it off-thread; invokes `release(val)` on group release or cancellation. |
| `AddCustomLoader(name, uri, load, release)` | Same as `AddCustom`, passing a cancellable `context.Context` to `load`. |

For one-off loads outside a group, `Manager` also exposes `LoadAtlas`, `LoadSound`, `LoadBytes`, and `LoadCustom`.

## Loading, reactive progress, and reading assets

Start a group with `mgr.LoadGroup("level1", onDone)` or `level1.Load(onDone)`. Loading is idempotent: calling `Load` on an already-loaded group runs `onDone(nil)` immediately, and calling it while loading queues `onDone` onto the active load.

Every status method on `*Group` is backed by a `core.Signal`, so reading them inside an NSGo view subscribes automatically:

- `g.Progress()` (`float64` in `[0, 1]`)
- `g.Loaded()` and `g.Total()` (`int`)
- `g.Loading()`, `g.Done()` (`bool`), and `g.Err()` (`error`)

Once loaded, retrieve typed assets from the group or from `Manager` (which searches the most recently registered group holding `name`):

```go
heroAtlas, ok := mgr.Atlas("hero")
jumpSound, ok := mgr.Sound("jump")
bgImage, ok := mgr.Image("bg")
rawLevel, ok := mgr.Get("map")
```

## Releasing groups

Release a scene's assets when leaving the scene with `mgr.ReleaseGroup("level1")` (or `level1.Release()`, or `mgr.ReleaseAll()`):

```go
mgr.ReleaseGroup("level1")
```

Releasing closes every loaded `*audio.Sound`, calls custom `release` hooks, drops atlas/image/byte references for garbage collection, cancels any in-flight load, and resets progress to `0` so the group can be loaded again later. Disposing `Manager`'s `cx` scope releases all groups automatically and discards late completions without leaking.

> Keeping decoded atlas images pinned outside NSGo's 64-source canvas cache is tracked in [nsgo#1127](https://github.com/nsgo-dev/nsgo/issues/1127).

## LoadingScreen

Drop `assets.LoadingScreen` (or `assets.LoadingView(cx, props)`) directly into your component tree to start loading a group on mount, render a reactive progress bar, and transition once ready:

```go
return assets.LoadingScreen{
	Manager: mgr,
	Group:   "level1",
	Title:   "Loading Cavern",
	OnReady: func() {
		stage.Set("play")
	},
	OnError: func(err error) {
		loadErr.Set(err)
	},
}
```

Set `View: func(progress float64, err error) core.Node` on `LoadingScreen` (or `Render` on `LoadingScreenProps`) to replace the default progress UI with your own branded loading view.
