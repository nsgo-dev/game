# Small downloads and fast starts

Put files needed after startup in `resources/` and load them by `asset:` URI. They are fetched on demand on the web. `//go:embed` is appropriate for tiny build-time files, but embedded runtime data is included before the first frame and remains resident.

Register a small first group with `assets.Manager` and show `assets.LoadingScreen`. Load later groups in the background after the first interactive frame. Release groups when scenes end, and put optional content in DLC packs instead of the initial package.

For web builds, `web.offline` caches a release after its first launch. The loading view receives reactive progress and can be replaced with a branded `View`. `nsgo build` writes Brotli and gzip copies of the web module for hosts that negotiate compression.

Use `mobile.exclude_resources` for desktop-only files. Choose compressed audio and appropriate sample rates, and run `go run github.com/nsgo-dev/game/cmd/sizecheck -kind wasm path/to/game.wasm` from your game module. Set `-budget-mb` for a raw wasm limit while retaining the budget file's compressed threshold.
