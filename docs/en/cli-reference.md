# Command Reference

<!-- Code generated from the game command's definitions by `go tool nsgo test ./internal/gamecli -- -run TestReference -update` from the repository root. DO NOT EDIT. -->

Every subcommand of `go tool game` (or `go run github.com/nsgo-dev/game/cmd/game@latest` outside a project), with its usage line and flags, generated from the commands' definitions so it always matches `game help`. [Command Line](cli.md) shows how to add the command as a tool, what each subcommand is for and how to set up shell completion.

## game pack

Pack a folder of PNG images into sprite atlas pages and TexturePacker JSON for sprite.LoadAtlas.

```sh
go tool game pack [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-extrude int` |  | edge pixels extruded around each frame |
| `-in string` |  | input directory of PNG images (required) |
| `-max-height int` | `2048` | maximum atlas page height in pixels |
| `-max-width int` | `2048` | maximum atlas page width in pixels |
| `-name string` | `atlas` | base file name for atlas pages |
| `-out string` | `.` | output directory for packed atlas PNG and JSON files |
| `-padding int` | `1` | transparent pixels between adjacent frames |
| `-scale int` | `1` | atlas scale written to meta.scale |
| `-trim` | `true` | trim transparent borders around input images |

## game audio

Normalize, resample and convert a WAV file to WAV, AAC or MP3.

```sh
go tool game audio [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-bitrate int` | `128` | target bitrate in kbps for aac and mp3 output |
| `-channels int` |  | target channel count: 1 (mono), 2 (stereo), or 0 (keep input) |
| `-format string` | `wav` | output format: wav, aac, or mp3 |
| `-in string` |  | input WAV file path (required) |
| `-out string` |  | output audio file path (required) |
| `-peak-db float` |  | normalize peak amplitude to this level in dBFS (e.g. -1) |
| `-rate int` |  | target sample rate in Hz (0 keeps input rate) |
| `-rms-db float` |  | normalize RMS loudness to this level in dBFS with soft-clip protection (e.g. -14) |

## game manifest

Generate a typed Go asset manifest from a directory of assets, so a missing asset fails the build.

```sh
go tool game manifest [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | verify that -out is up to date without modifying it |
| `-embed` |  | emit //go:embed directives instead of URI references |
| `-embed-dir string` |  | optional directory prefix prepended to //go:embed patterns |
| `-in string` | `assets` | input assets directory |
| `-out string` |  | output Go file path (writes to stdout when empty) |
| `-pkg string` | `assets` | Go package name for the generated file |
| `-prefix string` | `asset:` | URI prefix prepended to each asset path |
| `-resources` |  | scan resources/ (unless -in is set) and emit "asset:" URI references for app resources instead of //go:embed |

## game tex

Encode a PNG or JPEG texture into a KTX2 container as BC1, BC3, ETC2 or RGBA8, with optional mipmaps.

```sh
go tool game tex [flags] [input.png|input.jpg]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-format string` | `rgba8` | output format: bc1, bc3, etc2, or rgba8 |
| `-in string` |  | input PNG/JPEG file path (alternative to the positional argument) |
| `-mipmaps` |  | generate full mipmap chain down to 1x1 |
| `-o string` |  | shorthand for -out |
| `-out string` |  | output .ktx2 path (defaults to the input path with a .ktx2 extension) |
| `-srgb` |  | encode as sRGB with linear-light mipmap filtering |

## game editor

Open the scene editor on a project or a YAML scene, and an animation clip on one of its objects, with the project's own editor command when it has one, or on the start screen when none is named.

```sh
go tool game editor [flags] [project-dir | game.yaml | scene.yaml] [clip.animation.yaml [object-id]]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-library string` |  | show the library panel, which installs packs into the open project, for this library index: an https URL such as https://raw.githubusercontent.com/nsgo-dev/game/main/library/index.yaml, an index file, or a directory holding index.yaml |

## game mcp

Serve the editor's MCP tools on stdio, or run \`mcp install\` to configure Claude Code and Codex for this project.

```sh
go tool game mcp [flags] [install]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-project string` | `.` | the project: its directory, a directory inside it, or its game.yaml |

## game release

Build the game's release packages for macOS, Windows, Linux, web, iOS and Android, or game.yaml's export presets, in one run of nsgo build, with a credits file of its assets' licenses.

```sh
go tool game release [flags] [dir]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-channel channel` |  | the channel the packages are handed out through: direct, store, steam or none (default: nsgo.yaml's updates.channel) |
| `-dry-run` |  | print the nsgo build command instead of running it |
| `-jobs int` |  | how many targets build at once (default: nsgo's) |
| `-output directory` |  | the output directory (default: game.yaml's export.output, else nsgo.yaml's build.output) |
| `-release-notes file` |  | a Markdown file of the release's notes for the update prompts |
| `-strict` |  | fail with an error instead of warning when building an experimental target platform |
| `-target targets` |  | comma-separated nsgo build targets (default: game.yaml's export presets, else macos,windows,linux,web,ios,android, or macos,windows,linux for the steam channel) |

## game replay

Check the input recordings under the directories and play them headless through their replay tests, failing on a mismatch.

```sh
go tool game replay [flags] [directory ...]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-run regexp` | `Replay` | run the tests matching this regexp, as go test's -run: the tests that play the recordings |

## game playtest

Validate playtest.yaml, run headless bot playtests with replays, write a YAML report and fail on missed goals.

```sh
go tool game playtest [flags] [directory ...]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | validate playtest.yaml without running any tests |
| `-config file` | `playtest.yaml` | the playtest configuration file to validate and run |
| `-count int` |  | override runs.count in playtest.yaml when positive |
| `-out file` | `playtest.report.yaml` | write the YAML playtest report to this file |
| `-recordings directory` | `playtest-recordings` | write per-seed .replay.yaml recordings into this directory (empty skips writing files) |
| `-run regexp` | `Playtest` | run the tests matching this regexp, as go test's -run: the tests that call playtest.RunTest |
| `-seeds seeds` |  | override runs.seeds with a comma-separated list of uint64 seeds |

## game bench

Run the benchmarks headless and fail when one regresses past its stored baseline, or update the baseline.

```sh
go tool game bench [flags] [directory ...]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-baseline file` | `benchmarks/baseline.yaml` | the baseline file to check against, or to write with -update |
| `-bench regexp` | `.` | run the benchmarks matching this regexp, as go test's -bench |
| `-benchtime duration` | `200ms` | run each benchmark for this duration, or Nx times, as go test's -benchtime |
| `-count times` | `3` | run the benchmarks this many times and compare each one's median |
| `-in file` |  | read benchmark results from this file of go test -bench output instead of running the benchmarks |
| `-update` |  | write the measured medians to the baseline instead of checking them |
| `-strict` |  | fail when a measured benchmark has no baseline entry |

## game size

Measure a WebAssembly, Android AAB or APK, or iOS IPA package and fail when it exceeds its size budget.

```sh
go tool game size [flags] [package]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-abi-budget-mb MiB` |  | override the per-ABI native library budget in MiB for Android packages (> 0) |
| `-budget file` |  | path to the budget JSON file (for wasm defaults to benchmarks/web-budget.json; for mobile packages defaults to the platform's built-in budget) |
| `-budget-mb MiB` |  | override the total package size budget in MiB (> 0) |
| `-file archive` |  | path to the package archive (.aab, .apk, .ipa, or .wasm); may also be passed as the positional argument |
| `-json` |  | output the report as JSON |
| `-kind kind` |  | package kind: aab, apk, ipa, or wasm (inferred from the file extension when omitted, defaults to wasm) |
| `-markdown` |  | output the report as a Markdown summary |
| `-wasm file` |  | optional path to an already built .wasm file; if empty in wasm mode, builds the budget's target with GOOS=js GOARCH=wasm |

## game new

Create a game project in a directory from a template, with its go.mod requiring the game layer and the game and nsgo tools.

```sh
go tool game new [flags] <dir>
```

| Flag | Default | Description |
| --- | --- | --- |
| `-list` |  | list the templates, each with a line describing it, instead of creating a project |
| `-module string` |  | the module path in go.mod (default: the name in lower case with its words joined by -) |
| `-name string` |  | the game's display name in game.yaml and nsgo.yaml: letters, digits, spaces, - and \_ (default: the directory's name) |
| `-template string` | `starter` | the template to start from: starter, action3d, cards2d, fighting2d, fps3d, idle, platformer2d, puzzle2d, racing3d, rhythm, roguelike2d, rpg2d, rts2d, shmup2d, towerdefense2d, visualnovel; -list describes each |
| `-version string` |  | the version of github.com/nsgo-dev/game to require, such as v0.4.0, a commit or latest (default: this command's own version, or latest for a development build) |

## game project

Check a project's game.yaml, rewrite it in canonical form, write the fields it owns into nsgo.yaml, or print its export presets' build targets.

```sh
go tool game project [flags] check|fmt|sync|targets [dir]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | with fmt, fail unless game.yaml is in canonical form, and with sync, fail unless nsgo.yaml agrees with game.yaml, without rewriting either |

## game dev

Run the game with nsgo dev, reloading its assets in the running game when they are saved.

```sh
go tool game dev [flags] [dir] [nsgo dev flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-assets string` | `resources` | the directory, relative to the project directory, that the running game watches and its assets.Manager reads "asset:" URIs from, given to the game as hotreload.DevAssetsDir |
| `-automation` |  | enable NSGo automation so the editor and nsgo mcp can inspect and drive the running game |
| `-fps` |  | show NSGo's performance monitor in the running game |
| `-scene string` |  | start the game at this scene path relative to the project root, given to the game as hotreload.DevScene |
| `-target string` |  | the nsgo dev target, nsgo.yaml's dev.target when empty; assets reload on native and tui |

## game check

Check the scenes, Tiled and LDtk maps and sprite atlases under a directory, and the files they refer to.

```sh
go tool game check [flags] [dir]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-in string` |  | directory to check (alternative to the positional argument; defaults to the current directory) |

## game assets

Pack atlases, encode textures, convert audio, bake fonts, pack models and generate the manifest from game.yaml, rebuilding only what changed.

```sh
go tool game assets [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | list the targets that are out of date without building them, and fail if there are any |
| `-force` |  | rebuild every target, even those the cache says are up to date |
| `-in string` | `game.yaml` | project file whose assets section declares the targets |
| `-resources` |  | write asset outputs into resources/ and emit "asset:" URIs in the manifest instead of //go:embed |

## game generate

Write a new component, system, scene or prefab into a game project: its Go code and a test, the scene file of a scene or prefab, and its registration; or the project's editor command, which checks the project's components and builds in its editor plugins.

```sh
go tool game generate [flags] component|system|scene|prefab <name> | editor
```

| Flag | Default | Description |
| --- | --- | --- |
| `-autoload` |  | for a scene, also add it to the scenes game.yaml autoloads |
| `-in string` | `.` | the game's project directory, which holds its game.yaml and the Go package the code is written into |
| `-main` |  | for a scene, also add it to the scenes game.yaml autoloads and make it the main scene |

## game mv

Move or rename a file, directory, or text key (-text-key) and rewrite every reference to it, keeping the project index up to date.

```sh
go tool game mv [flags] <from> <to>
```

| Flag | Default | Description |
| --- | --- | --- |
| `-dry-run` |  | print what would change without changing anything |
| `-in string` |  | project directory (defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod) |
| `-text-key` |  | rename a localization text key across screen files and YAML string tables instead of moving a file |

## game index

Give every scene and resource file a uid, point references at files moved without the tools, and write the project index.

```sh
go tool game index [flags] [dir]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-dry-run` |  | print what would change without changing anything |
| `-in string` |  | project directory (alternative to the positional argument; defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod) |

## game library

Find asset, editor plugin and template packs in a library and install them into a project, remove them, or print a pack's digest for the library's index.

```sh
go tool game library [flags] list [words] | install <pack>... | remove <pack>... | digest <pack-dir>
```

| Flag | Default | Description |
| --- | --- | --- |
| `-dir string` |  | install the pack into this directory of the project instead of the one it is installed in or its manifest names (one pack at a time) |
| `-index string` | `https://raw.githubusercontent.com/nsgo-dev/game/main/library/index.yaml` | the library's index: an https URL, an index file, or a directory holding index.yaml |
| `-kind string` |  | list only packs of this kind: asset, plugin, template |
| `-project string` | `.` | the project directory holding game.yaml, which install and remove change and list marks installed packs from |

## game credits

List every asset's author and license from its import settings sidecar as the credits file game release ships, failing when an asset has none.

```sh
go tool game credits [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-in string` | `game.yaml` | project file whose assets section names the assets |
| `-out file` |  | the credits file to write (default: print it) |

## game pixelfx

Render a pixelfx effect, or the effect acted out, to a PNG sprite sheet and Aseprite JSON for sprite.LoadAtlas.

```sh
go tool game pixelfx [flags] <category/name>
```

| Flag | Default | Description |
| --- | --- | --- |
| `-act` |  | render the effect acted out: the stock character performing it, with its target |
| `-flip` |  | mirror the effect |
| `-fps int` |  | frames per second (default: the effect's own, or 60 acted out) |
| `-list` |  | list the effects and their options instead of rendering one |
| `-name string` |  | base file name and animation tag (default: the effect's name, such as torch) |
| `-out directory` | `.` | the directory to write \<name>.png and \<name>.json into |
| `-set name=value` |  | set one of the effect's options as name=value, such as seed=4 or palette=#102,#215,#fff; repeat it for more |

## game navmesh

Bake a scene's glTF level geometry into a navigation mesh per agent type of game.yaml's navigation section, written to the file the scene's navMesh names.

```sh
go tool game navmesh [flags] <scene.yaml>
```

| Flag | Default | Description |
| --- | --- | --- |
| `-in string` |  | project directory, whose game.yaml holds the navigation settings and from which the scene's paths start (defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod) |
| `-out file` |  | navigation mesh file to write (defaults to the file the scene's navMesh names) |

## game merge

Merge scene YAML files object by object for Git, or set up and check the Git merge driver in a project.

```sh
go tool game merge [flags] <base> <ours> <theirs> [path] | -setup [dir] | -check [dir]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | verify that merge.nsgo-scene.driver is configured when .gitattributes names nsgo-scene |
| `-conflict-out string` |  | file to write the YAML conflict report to instead of overwriting the output file on conflict |
| `-out string` |  | file to write the merged scene or conflict report to (default: overwrite \<ours> in place, as Git expects) |
| `-setup` |  | register the nsgo-scene merge driver in local Git config and ensure .gitattributes has LF line-ending, binary media, and scene merge rules |

## game schema

Write the project's scene JSON Schema, including its registered components and plugins, or the built-in scene schema when the project has no editor command of its own.

```sh
go tool game schema [flags] [project-dir | game.yaml]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-out file` |  | write the JSON Schema to file instead of stdout |

## game net

Decode packet captures layer by layer using wire schemas, or print traffic statistics per message type and peer.

```sh
go tool game net [flags] decode [flags] <capture.jsonl | -> | stats [flags] <capture.jsonl | ->
```

| Flag | Default | Description |
| --- | --- | --- |
| `-format string` |  | output format (decode: yaml or json, default yaml; stats: text, yaml, or json, default text) |
| `-resolve-deltas` |  | track per-peer replication baselines so SnapshotDelta frames also print full resulting entity states (decode only) |
| `-schema string` |  | directory or \*.wire.yaml file of game-defined wire schemas (for example, custom EntityState.Props) |

## game wire

Generate Go and TypeScript codecs from \*.wire.yaml schemas, or check that the committed ones are current.

```sh
go tool game wire [flags] [schema.wire.yaml ...]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-check` |  | verify that generated files in -out are up to date without writing |
| `-in file` | `.` | input \*.wire.yaml file or directory containing \*.wire.yaml files |
| `-lang language` | `go` | target language for code generation: go or ts |
| `-out directory` |  | output directory (or .go/.ts file for a single input); defaults to the input directory |
| `-pkg name` |  | override the Go package name (defaults to each schema's package field) |
| `-self` |  | omit the pkg/online/netplay/wire runtime import (used when generating Go inside pkg/online/netplay/wire) |

## game completion

Write a shell completion script for zsh, bash or fish.

```sh
go tool game completion [flags] zsh|bash|fish
```

No flags.

## game help

Show the commands, or one command's flags.

```sh
go tool game help [command]
```

No flags.
