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

## Reloading on save

Run the game with [`go tool game dev`](cli.md#dev) and start a watcher on the asset directory it names, and every atlas, image and raw-bytes asset the manager has loaded changes in the running game within a second of its file being saved:

```go
var opts []assets.Option
if dir := hotreload.DevAssetsDir(); dir != "" {
	// Under game dev, read "asset:" URIs from the directory the watcher watches.
	opts = append(opts, assets.WithFS(os.DirFS(dir)))
}
mgr := assets.NewManager(cx, opts...)
w := hotreload.NewDevWatcher(cx) // nil outside game dev and in release builds
w.WatchAssets(mgr, func(path string, names []string, err error) {
	if err != nil {
		log.Printf("reload %s: %v", path, err)
	}
})
```

The watcher and the manager must see the same files: a `Manager` without `WithFS` reads `"asset:"` URIs from `assets/` in the working directory, and `nsgo dev` starts the game in the directory `game dev` was run from, not the project's, so under `game dev` the manager reads from `hotreload.DevAssetsDir()`, the absolute path of the directory `game dev` passes, as above. `WatchAssets` passes each changed file under the directory to `Manager.Reload` as `"asset:<path>"` on the engine goroutine. An atlas whose JSON changed is parsed again, and one whose image changed draws the file's new bytes with the same frames; the `*sprite.Atlas` the group holds is updated in place, so code that kept the pointer draws the edit. JSON that no longer parses keeps the previous frames and reports the error. Images show the new bytes and raw bytes are replaced; sounds and custom assets load again only when their group does. An image that ships in several densities (`Scales`) reloads from the file the screen shows, such as `hero@2x.png` on a 2x screen; saving another density changes nothing until the screen shows it. A group mounted on a DLC pack with `SetFS` keeps the pack's copy of a file the pack has, so saving the base file does not change it. `Manager.Revision` counts the reloads that replaced something, so a `core.Canvas` that reads it in `Draw` draws again after each one; a game drawn every frame by `game.Loop` needs nothing more.

For an atlas the game loads itself, such as one embedded with `go:embed`, `w.WatchAtlasFiles("sprites.json", "sprites.png", img, onReload)` delivers the atlas parsed from the saved files. For WGSL/Kage shaders and 3D shader materials, attach `hotreload.ShaderReloader` and `hotreload.MaterialReloader` with `w.WatchShaders` and `w.WatchMaterials`; see [Hot-reloading shaders and materials](render3d.md#hot-reloading-shaders-and-materials). `NewDevWatcher` returns nil when the game is not running under `game dev`, and a nil watcher's methods do nothing, so the calls can stay in the game; in release builds (`-tags release`) they compile to nothing.

## Downloadable packs (DLC)

`assets.NewDLCManager(assets.DLCOptions{...})` downloads ZIP packs described by a `PackSpec` (usually parsed from a manifest with `ParsePackManifest`), resumes interrupted downloads with HTTP Range requests, verifies size, SHA-256 and an Ed25519 signature, and caches them in a `PackCache`. `Fetch` returns the verified pack as an `fs.FS`; `FetchGroup` also mounts it into a `Group` and loads it.

```go
dlc := assets.NewDLCManager(assets.DLCOptions{
	AppBuild:  42,
	PublicKey: packPublicKey,
	Cache:     assets.NewDirPackCache(cacheDir),
	Limits:    assets.PackLimits{MaxTotalBytes: 256 << 20},
})
group, err := dlc.FetchGroup(ctx, mgr, spec, registerAssets, onProgress)
```

- Without a `PublicKey`, every pack is refused with `ErrInvalidSignature`, because whoever supplies the `PackSpec` (URL, size and hash) would otherwise choose what is mounted. Set `AllowUnsigned` to accept unsigned packs during development or when the specs ship inside the app.
- A cached pack is verified again on the same reader that is mounted. `DirPackCache` reads a mounted pack through one open file, so replacing or deleting the cache file afterwards does not change what the mount serves (on Windows, deleting a mounted pack fails until it is released).
- `Limits` caps the number of entries, the size of one file and the total unpacked size (zero fields use `DefaultPackLimits()`: 32768 entries, 256 MiB, 1 GiB). A pack over a limit is refused before mounting, and an entry cannot decompress past the size its header declares, so the caps bound what reading an asset allocates.
