# Playground

The [playground](https://game.nsgo.dev/playground/) runs this repository's examples in the browser, built for NSGo's web target, with each example's source beside it. To change an example, run it on your machine with the dev server, which rebuilds and reloads it each time you save.

## In the browser

Each example has a page of its own, such as `/playground/caverun/`, so its address is the link to share. The page shows:

- **The game**, which loads when you press **Run**: its WebAssembly is about 15 MB, so nothing is downloaded before then. Click the game to give it the keyboard. **Restart** loads it again from the start, and **Open in a new tab** runs the build on a page of its own.
- **The source**, a tab per file: the example's main package without its tests, then its `nsgo.yaml`. The tab shown is the address's fragment, so `/playground/caverun/#level.go` opens on `level.go`. Each file links to it on GitHub.

The site is static, hosted on GitHub Pages, so it compiles nothing: a page runs the build the site was deployed with, from the same commit as the source it shows.

## Edit on your machine

With Go 1.27 or later, clone the repository and start an example with the dev server for the web target:

```sh
git clone https://github.com/nsgo-dev/game.git
cd game/examples/caverun
go tool nsgo dev -target web
```

It opens the game in your browser, Chrome and Safari alike, and rebuilds and reloads it each time you save a file of the example or of `packages/game`, which the examples use through the checkout's `go.work`. A failed build shows its errors over the game until the next save builds. Without `-target web` the example runs as a native app, which reloads in the same way.

## How the builds are made

`bun run playground`, from the repository's root, builds each example with `go tool nsgo build -target web` into the example's `dist/playground/`, then copies the site it writes into `apps/website/public/play/<id>/`, which Git ignores. The website serves that directory at `/play/<id>/` and each example's page shows it in a frame. `bun run playground caverun` builds one example.

The website's workflow sets up Go and runs it before building the site, on every push to `main` and every pull request that touches the site, the examples or `packages/game`. In CI a missing build fails the site's build, so a deploy never publishes a page with no game; a local `bun run build` without the builds warns, and the example's page shows the command that makes its build.

## Add an example

1. Put the example under `examples/<id>/` with an `nsgo.yaml`, a main package that builds for the web target, and an entry for it in `go.work`.
2. Add `<id>` to `PLAYGROUND_IDS` in `apps/website/src/content/playground.ts`.
3. Give it an entry in `apps/website/src/content/showcase.ts`, whose title and description the playground shows, in English and Chinese.
