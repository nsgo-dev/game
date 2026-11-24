# Contributing

Contributions are welcome: bug fixes, new packages from the [roadmap](https://game.nsgo.dev/roadmap/), examples, documentation, and website improvements.

## Repository layout

```
game/
├─ go.work
├─ packages/
│  └─ game/          Go module github.com/nsgo-dev/game/packages/game
├─ examples/         playable sample games built on the game layer
├─ template/         Go module publishing the starter from template/template/
├─ docs/
│  ├─ en/            site documentation in English
│  └─ zh/            the same pages in Simplified Chinese
├─ apps/website/     the site at https://game.nsgo.dev
├─ changelog.d/      changelog fragments, folded into CHANGELOG.md at release
└─ tools/changelog/  the command that checks and folds them
```

The root `go.work` links the starter and examples to `./packages/game`. The `nsgo` CLI is declared as a `tool` dependency in `packages/game/go.mod`, so run `go tool nsgo ...` from `packages/game` (just as CI does).

## Development setup

- **Go 1.27 or later** (see `packages/game/go.mod` for the version used in CI).
- **Bun 1.4.4** if you are working on `apps/website/` or `docs/`.
- **Xcode** on macOS if you want to vet the iOS targets locally.

### Running Go checks and tests

From `packages/game`:

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
bun run test       # the build plugins' tests
bun run build      # Vite build, prerender, and tsc --noEmit
```

## Coding conventions

- **Standard library first.** Add no third-party Go dependencies where the standard library suffices. Packages should depend only on Go's standard library and NSGo's public API (`core`, `audio`, `gamepad`, etc.), so an app that does not import `game` links none of it.
- **No hand-written C or `cgo`.** Keep all packages pure Go. Platform bindings belong in [nsgo-dev/nsgo](https://github.com/nsgo-dev/nsgo), not here.
- **Deterministic tests.** Drive tests through `game.Loop` and `nsgotest.Main(m)` (`nsgotest.Start`, `app.Advance`, `app.FastForward`). Do not rely on `time.Sleep` or wall-clock timing.
- **Runnable examples.** Every library package (not commands, which are `package main`, or internal packages) keeps an `example_test.go` with `Example` functions that end in an `// Output:` comment, so `go tool nsgo test` runs and checks them; an example that needs an outside service or a running test leaves out `// Output:` and is only compiled. Cover the main use of each exported type, keep the output deterministic (fixed seeds, manual clocks, sorted results, floats rounded with negative zero printed as 0), avoid printing lines indented by four or more spaces, and update the examples with the API they show.
- **Concise doc comments.** Document every exported package, type, function, and method directly—state its contract and edge cases without repeating the signature.
- **A status for every package.** A new package starts experimental: add its row to the table in both `docs/en/stability.md` and `docs/zh/stability.md`, which `TestStabilityTable` in `packages/game` checks. Deprecate and remove parts of a stable package as the [API stability policy](docs/en/stability.md#deprecation) describes.
- **Conforming YAML formats.** A package that reads a YAML format runs `formattest.Run` from `internal/formattest` on testdata in the format's canonical form, which checks that it refuses unknown fields and unsupported versions, saves deterministically and round-trips each file unchanged ([Checking formats in CI](docs/en/schemas.md#checking-formats-in-ci)); `TestFormatPackagesRunConformance` in `packages/game` fails a package that decodes YAML without it, unless the package reads no format of its own and its `notFormats` list says why.
- **Bilingual docs and site copy.** When changing documentation or website UI text, update both `docs/en/` and `docs/zh/` (and both `en` and `zh` in `apps/website/src/copy.ts`). In Chinese copy, omit trailing `。` on headings, titles, and labels, keep `。` on body sentences, and keep package, type, and code names in English.

## Pull requests

1. **Keep PRs focused.** One issue or cohesive change per pull request. Before starting a large feature, open an issue or comment on the existing roadmap issue to align on the API.
2. **Verify locally before opening.** Ensure `gofmt -l` outputs nothing, `go tool nsgo test ./...` passes, and `go tool nsgo check` passes for your targets. For `docs/` or `apps/website/` changes, ensure `bun run build` exits `0`.
3. **Link the issue.** Include `Fixes #<number>` (or `Refs #<number>` for partial work) in the pull request description.
4. **Add a changelog fragment.** A change to `packages/`, `examples/` or `template/` adds a file to `changelog.d/` instead of editing `CHANGELOG.md`, as [changelog.d/README.md](changelog.d/README.md) describes. An internal change takes the `skip-changelog` label instead.
5. **Show visual changes.** Attach screenshots or a short recording for website, example, or rendering changes.

## Engine-level issues

Bugs or missing primitives in NSGo itself (such as display frame times, input primitives, audio backends, or the GPU view) belong in [nsgo-dev/nsgo](https://github.com/nsgo-dev/nsgo/issues).
