# assets

`game/assets` loads sprite atlases, sounds, images, raw bytes, and custom game data asynchronously off the engine goroutine, organizes them into per-scene groups with reactive progress signals, releases them cleanly when a scene ends, and provides a ready-made `LoadingScreen` component.

```go
import "github.com/nsgo-dev/game/pkg/assets"
```

All `Manager` and `Group` methods run on the engine goroutine; file reading and parsing run in background goroutines and post results back via `core.Scope.Post` so animations and `game.Loop` never hitch.

## Creating a Manager and defining groups

Create a `Manager` bound to a `*core.Scope` (typically your root game component's scope). With no options, `"asset:"` URIs resolve the same app `resources/` files that images (`core.Src("asset:...")`) and sounds (`audio.Source{URI: "asset:..."}`) resolve on every target, through `core.Scope.ReadAsset` (fetched on demand on the web without embedding them in the binary):

```go
mgr := assets.NewManager(cx)

// Register a scene's assets as a named group (all loaded from resources/)
level1 := mgr.Group("level1").
	AddAtlas("hero", "asset:hero.json", core.Src("asset:hero.png")).
	AddSound("jump", audio.Source{URI: "asset:sounds/jump.wav"}).
	AddImage("bg", core.Src("asset:bg.png")).
	AddCustom("map", "asset:maps/level1.tmj", parseMap, nil)
```

Pass `assets.WithFS(fsys)` to override `"asset:"` byte reads with an `embed.FS` or `os.DirFS` (for example small startup assets embedded in the binary, or `os.DirFS(hotreload.DevAssetsDir())` under `game dev`), or `assets.WithReadAsset(fn)` for custom storage.

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

The watcher and the manager must see the same files: a `Manager` without `WithFS` reads `"asset:"` URIs from the app's packaged `resources/` through `core.Scope.ReadAsset`, so under `game dev` pass `assets.WithFS(os.DirFS(hotreload.DevAssetsDir()))`, the absolute path of the directory `game dev` watches, as above (this also lets `ReloadAsset` and `ReloadGroup` re-read edited files synchronously). `WatchAssets` passes each changed file under the directory to `Manager.Reload` as `"asset:<path>"` on the engine goroutine. An atlas whose JSON changed is parsed again, and one whose image changed draws the file's new bytes with the same frames; the `*sprite.Atlas` the group holds is updated in place, so code that kept the pointer draws the edit. JSON that no longer parses keeps the previous frames and reports the error. Images show the new bytes and raw bytes are replaced; sounds and custom assets load again only when their group does. An image that ships in several densities (`Scales`) reloads from the file the screen shows, such as `hero@2x.png` on a 2x screen; saving another density changes nothing until the screen shows it. A group mounted on a DLC pack with `SetFS` keeps the pack's copy of a file the pack has, so saving the base file does not change it. `Manager.Revision` counts the reloads that replaced something, so a `core.Canvas` that reads it in `Draw` draws again after each one; a game drawn every frame by `game.Loop` needs nothing more.

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
- The `fs.FS` that `Fetch` returns is also an `io.Closer`. Close it when the pack is no longer used; one never closed is released when it is garbage collected. `FetchGroup` hands its pack to the group, which releases it on `Group.Reset`, `Group.SetFS`, the next `FetchGroup` into the same group, or when the asset manager's scope is disposed. `Remove` of a pack that is still mounted deletes its partial download at once and the cached pack when the last mount is released, so it never pulls the bytes from under a loaded group; a `Fetch` of the pack before then keeps it.
- `Limits` caps the number of entries, the size of one file, the total unpacked size and the archive itself (zero fields use `DefaultPackLimits()`: 32768 entries, 256 MiB, 1 GiB and 1088 MiB). A pack over a limit is refused before mounting. A download stops with `ErrPackTooLarge` once it would pass `MaxPackBytes`, which bounds the `.part` file of a `PackSpec` without a `Size`. A mounted pack reads each file as it decompresses, growing its buffer with the bytes that arrive rather than allocating the size the header claims, and fails with `ErrPackTooLarge` past `MaxFileBytes`.
- A spec that could never verify (a malformed `sha256` or `signature`, or a negative `size`) is refused before the cache is touched, so it cannot delete a good cached pack. A cached pack is deleted only when its bytes do not match the spec's size or hash; one whose bytes match but whose signature or contents are refused stays, since downloading it again would fetch the same bytes.
- A resumed download that gets `206 Partial Content` without a `Content-Range` header starts over from the first byte rather than appending a body whose range it cannot check.
- `ParsePackManifest` refuses fields it does not define, such as a misspelt `sha265`, a key given twice in one object, and anything after the manifest, with the line and a fix.

## Image collision and named points

An atlas source PNG may carry a `*.import.yaml` sidecar configuring baked 2D collision shapes (`image.collision`) and named image points (`image.points`, with optional per-cell overrides in `image.cellPoints` for sliced sheets):

```yaml
version: 1
image:
  slice: {width: 16, height: 16}
  collision:
    mode: alpha        # alpha, box, ellipse, polygon, or none
    tolerance: 0       # alpha cutoff [0..255]; pixels with alpha > tolerance are solid
    maxVertices: 8     # max vertices per convex polygon (3..8, default 8)
    cells:
      - cell: 1
        mode: box
  points:
    muzzle: [14, 6]
    feet: [8, 16]
  cellPoints:
    - cell: 1
      points:
        muzzle: [15, 5]
```

When `go tool game assets` or `go tool game pack` builds the atlas, it traces pixel alpha silhouettes with marching squares, simplifies contours, splits concave outlines into convex polygons of at most `maxVertices` vertices, and writes `shapes` and `points` into each frame of the packed JSON. Sidecars refuse unknown fields and validate against `import.v1.json`. In the editor, selecting the image in the file browser overlays the traced collision outline and named points over the sprite and lets you drag points directly on the preview canvas.

## Remote assets

For loose files hosted on a CDN (such as music tracks or per-level art kept out of mini-game or web bundles), configure [`assets.remote`](cli.md#assets) in `game.yaml` so `go tool game assets` copies them to content-hashed paths (`<name>.<hash8>.<ext>`) and writes a sorted, versioned `remote.yaml` manifest, then pass `assets.WithRemote` when creating the `Manager`:

```go
manifest, err := assets.ParseRemoteManifest(remoteManifestYAML)
if err != nil {
	return err
}
cache := assets.NewDirRemoteCache(cacheDir) // default 200 MiB quota
mgr := assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
	Manifest: manifest,
	Cache:    cache,
}))
```

- Any URI listed in `Manifest` (bytes, custom assets, images, atlas JSON/images, and sounds) is fetched from `Base` (defaulting to `Manifest.Base`) using its content-hashed path, checked against exact byte size and SHA-256 (`ErrSizeMismatch` / `ErrHashMismatch`), and stored in `Cache`. URIs not in `Manifest` fall through to local sources (`WithReadAsset`, `WithFS`, or packaged `resources/`).
- A cached file whose bytes still match the manifest's size and SHA-256 is served without a network request, allowing offline startups once cached. A corrupted or truncated cache entry is removed and re-fetched automatically.
- Concurrent loads for the same content digest share a single in-flight request; cancelling one waiter leaves sibling waiters running, and cancelling the last waiter aborts the underlying fetch.
- `RemoteOptions.Fetch` (`func(ctx context.Context, url string) (io.ReadCloser, int64, error)`) is the transport seam, defaulting to `net/http`. Targets without `net/http` (such as mini-program runtimes) can plug in their host request API directly.
- `DirRemoteCache` enforces `Quota` (200 MiB by default, matching mini-game filesystem caps) with least-recently-used eviction that never removes files pinned by a loaded or loading `Group`, writes files atomically via temporary files, persists access times in a small `index.yaml` text index, and exposes `Usage()` (`Bytes`, `Quota`, `Files`, `PinnedBytes`) and `Clear()` for a settings screen. Use `MemoryRemoteCache` on targets without local disk storage (such as the web).

## Delivery per export preset

An export preset in `game.yaml` can say where each asset group ships: `delivery.main` lists the groups in the main package, `delivery.subpackages` maps each subpackage name to its groups, and `delivery.remote` lists the groups served from the remote origin. `delivery.caps` sets the largest size of the main package (`main`) and of all packages together (`total`) in bytes. A group may appear in only one of these places, and a subpackage name has no spaces; see [Asset delivery](project.md#asset-delivery) for the format. `game.yaml` records and checks this placement only: `game release` does not yet lay files out per package or enforce the caps, and `Manager.LoadGroup` does not yet load a subpackage through the host.

### Shared reference counts

`assets.RefCache` shares a loaded value by URI and closes it after the last handle releases it. Managers can use this primitive to keep shared atlases, sounds and custom resources alive while any group still refers to them.
