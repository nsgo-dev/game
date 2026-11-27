# Contributing

Contributions are welcome: bug fixes, new packages, examples, documentation, and website improvements.

## Repository layout

```
game/
├─ go.mod / go.work  Go module github.com/nsgo-dev/game (root package game: loop.go and its tests)
├─ cmd/              binaries shipped to users: game, nsgo-game, and the service daemons accountsd, cloudsaved, leaderboardd
├─ internal/         shared helpers and the CLI implementation, repocheck/ (repo-wide checks) and sizeprobe/ (the CI size fixture)
├─ pkg/              every other library package (github.com/nsgo-dev/game/pkg/<name>)
│  ├─ online/        service and netcode packages (accounts, cloudsave, leaderboard, liveops, netplay, server, ...)
│  └─ tooling/       build, editor and dev-time tooling that a release build does not link
├─ examples/         playable sample games, one Go module each, including dedicatedserver and vnsample
├─ template/         Go modules publishing the game new templates: starter from template/template/, the others from template/<name>/template/
├─ docs/
│  ├─ en/            site documentation in English
│  └─ zh/            the same pages in Simplified Chinese
├─ design/editor/    the editor's design artboards (Spec.dc.html and the per-panel .dc.html files) that pkg/editor implements
├─ website/          the site at https://game.nsgo.dev
├─ changelog.d/      changelog fragments, folded into CHANGELOG.md at release
└─ tools/            one Go module of contributor tooling: changelog/ checks and folds the fragments, agenteval/ is the agent evaluation suite (its README is the contributor guide), gameschema/ writes and checks the published JSON Schemas, gameapi/ generates the API reference; pixelfxref/ is a Node script that renders the pixelfx reference images
```

The root `go.work` links the root module, starter templates, examples and the tools module together. The `nsgo` CLI is declared as a `tool` dependency in the root `go.mod`, so run `go tool nsgo ...` directly from the repository root (just as CI does).

## Development setup

- **Go 1.27 or later** (see `go.mod` for the version used in CI).
- **Bun 1.4.4** if you are working on `website/` or `docs/`.
- **Xcode** on macOS if you want to vet the iOS targets locally.

### Running Go checks and tests

From the repository root:

```sh
# Format Go source files
gofmt -w .

# Run the headless test suite
go tool nsgo test ./...

# Build and vet all non-iOS targets (macOS, Windows, Linux, Android, Web)
go tool nsgo check -target macos,windows,linux,android,web

# Build and vet iOS targets (macOS with Xcode only)
go tool nsgo check -target ios,ios_simulator
```

CI runs `gofmt -l`, `go tool nsgo test ./...` on Linux and macOS, `go tool nsgo check -target macos,windows,linux,android,web` on Linux, and `go tool nsgo check -target ios,ios_simulator` on macOS.

### Running the website

From the repository root:

```sh
bun install --frozen-lockfile
bun run dev        # local dev server
bun run typecheck  # TypeScript check
bun run test       # the build plugins' and the copy's tests
bun run build      # Vite build, prerender, tsc --noEmit and the API reference's anchor check
```

The site is [TanStack Start](https://tanstack.com/start) and React, every page prerendered: `website/plugins/content.ts` and `plugins/site.ts` read the docs, the examples and the Go packages at build time, `src/pages/` holds the pages, `src/copy/` their words in ten languages and `src/styles.css` the look. `bun run playground` builds the playground's examples for the web, which a build in CI requires.

## Coding conventions

- **Standard library first.** Add no third-party Go dependencies where the standard library suffices. Packages should depend only on Go's standard library and NSGo's public API (`core`, `audio`, `gamepad`, etc.), so an app that does not import `game` links none of it.
- **No hand-written C or `cgo`.** Keep all packages pure Go. Platform bindings belong in [nsgo-dev/nsgo](https://github.com/nsgo-dev/nsgo), not here.
- **Deterministic tests.** Drive tests through `game.Loop` and `nsgotest.Main(m)` (`nsgotest.Start`, `app.Advance`, `app.FastForward`). Do not rely on `time.Sleep` or wall-clock timing.
- **Runnable examples.** Every library package (not commands, which are `package main`, or internal packages) keeps an `example_test.go` with `Example` functions that end in an `// Output:` comment, so `go tool nsgo test` runs and checks them; an example that needs an outside service or a running test leaves out `// Output:` and is only compiled. Cover the main use of each exported type, keep the output deterministic (fixed seeds, manual clocks, sorted results, floats rounded with negative zero printed as 0), avoid printing lines indented by four or more spaces, and update the examples with the API they show.
- **Concise doc comments.** Document every exported package, type, function, and method directly—state its contract and edge cases without repeating the signature.
- **A status for every package.** A new package starts experimental: add its row to the table in both `docs/en/stability.md` and `docs/zh/stability.md`, which `TestStabilityTable` in `internal/repocheck` checks. Deprecate and remove parts of a stable package as the [API stability policy](docs/en/stability.md#deprecation) describes.
- **A catalog entry for every package.** A new flat package under `pkg/` gets a `pkg/<name>` label in `.github/labeler.yml`, a name in `README.md`'s package table and a row in both `docs/en/index.md` and `docs/zh/index.md` (a package under `pkg/online` too, without the label), and takes no standard library package's name; `internal/repocheck/catalog_test.go` checks all of it.
- **Conforming YAML formats.** A package that reads a YAML format runs `formattest.Run` from `internal/formattest` on testdata in the format's canonical form, which checks that it refuses unknown fields and unsupported versions, saves deterministically and round-trips each file unchanged ([Checking formats in CI](docs/en/schemas.md#checking-formats-in-ci)); `TestFormatPackagesRunConformance` in `internal/repocheck` fails a package that decodes YAML without it, unless the package reads no format of its own and its `notFormats` list says why. Each call names the format's entry on [File Formats](docs/en/formats.md) in `Doc`, and the checks load and save that entry's YAML examples too; a new format adds its entry to `docs/en/formats.md` and `docs/zh/formats.md`, and a format that is not YAML also adds its entry to `codeFormats` in `internal/repocheck/formats_test.go`, with a reader that loads the entry's examples.
- **Bilingual docs and site copy.** When changing documentation or website UI text, update both `docs/en/` and `docs/zh/` (and both `en` and `zh` in `website/src/copy/`). In Chinese copy, omit trailing `。` on headings, titles, and labels, keep `。` on body sentences, and keep package, type, and code names in English.

## Pull requests

1. **Keep PRs focused.** One issue or cohesive change per pull request. Before starting a large feature, open an issue to align on the API.
2. **Verify locally before opening.** Ensure `gofmt -l` outputs nothing, `go tool nsgo test ./...` passes, and `go tool nsgo check` passes for your targets. For `docs/` or `website/` changes, ensure `bun run build` exits `0`.
3. **Link the issue.** Include `Fixes #<number>` (or `Refs #<number>` for partial work) in the pull request description.
4. **Add a changelog fragment.** A change to the root Go module (`*.go`, `cmd/`, `internal/`, `pkg/`), `examples/` or `template/` adds a file to `changelog.d/` instead of editing `CHANGELOG.md`, as [changelog.d/README.md](changelog.d/README.md) describes. An internal change takes the `skip-changelog` label instead.
5. **Show visual changes.** Attach screenshots or a short recording for website, example, or rendering changes.

## Engine-level issues

Bugs or missing primitives in NSGo itself (such as display frame times, input primitives, audio backends, or the GPU view) belong in [nsgo-dev/nsgo](https://github.com/nsgo-dev/nsgo/issues).
