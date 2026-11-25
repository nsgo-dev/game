# Command Line

`game` is the game layer's command-line tool: one command whose subcommands pack sprite atlases, convert audio, generate asset manifests, encode textures, open the scene editor, build release packages, play input recordings headless, check benchmarks against a stored baseline, create projects, run the game with asset hot reload, check a game's scenes, maps and atlases, build all of a game's assets from `game.yaml`, rebuilding only what changed, write new components, systems, scenes and prefabs into a project, move files while keeping the references to them working, keep the project index of stable file IDs, render pixelfx effects to sprite sheets and write shell completion scripts. They share how they are called: flags before arguments, `-in` for what a command reads and `-out` for what it writes, and the same help.

## Install

Add the command to your module as a tool, then run it with `go tool game`:

```sh
go get -tool github.com/nsgo-dev/game/packages/game/cmd/game
go tool game help
```

The `tool` line in `go.mod` pins the command to the module's version of the game layer, so everyone working on the game runs the same one. A project created with [`game new`](#new) has it already.

## Commands

| Command | What it does |
| --- | --- |
| `pack` | Packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata for `sprite.LoadAtlas` |
| `audio` | Normalizes, resamples and converts a WAV file to WAV, AAC or MP3 |
| `manifest` | Generates a typed Go asset manifest from a directory of assets, so a missing asset fails the build |
| `tex` | Encodes a PNG or JPEG texture into a KTX2 container as BC1, BC3, ETC2 or RGBA8, with optional mipmaps |
| `editor` | Opens the scene editor on a project or a YAML scene, or on a demo scene when none is named |
| `release` | Builds the game's release packages for macOS, Windows, Linux, web, iOS and Android, or for `game.yaml`'s export presets, in one run of `nsgo build`, with a credits file of its assets' licenses |
| `replay` | Checks the input recordings under the directories and plays them headless through their replay tests, failing on a mismatch |
| `bench` | Runs the benchmarks headless and fails when one regresses past its stored baseline, or updates the baseline |
| `new` | Creates a game project in a directory from a template, with its `go.mod` requiring the game layer and the `game` and `nsgo` tools |
| `dev` | Runs the game with `nsgo dev`, reloading its assets in the running game when they are saved |
| `check` | Checks the scenes, Tiled and LDtk maps and sprite atlases under a directory, and the files they refer to, reporting each problem with its file and line |
| `assets` | Packs atlases, encodes textures, converts audio, bakes fonts, packs models and generates the manifest from `game.yaml`, rebuilding only what changed |
| `generate` | Writes a new component, system, scene or prefab into a game project: its Go code and a test, the scene file of a scene or prefab, and its registration |
| `mv` | Moves or renames a file or directory of a project and rewrites every reference to it, keeping the project index up to date |
| `index` | Gives every scene and resource file a uid, points references at files moved without the tools, and writes the project index |
| `library` | Finds asset, editor plugin and template packs in a library and installs them into a project, updates or removes them, or prints a pack's digest |
| `credits` | Writes the credits file `release` ships: every asset's author and license, from the import settings sidecar beside it, failing when an asset has none |
| `pixelfx` | Renders a pixelfx effect, or the effect acted out, to a PNG sprite sheet and Aseprite JSON for `sprite.LoadAtlas` |
| `completion` | Writes a completion script for zsh, bash or fish |
| `help` | Lists the commands, or shows one command's flags |

`go tool game help` lists the commands, and `go tool game help <command>` or `go tool game <command> -h` shows a command's usage line, what it does and its flags. [Command Reference](cli-reference.md) lists every command's flags and their defaults on one page, generated from the commands' definitions.

A command exits with 0 when it succeeds or shows help, 1 when it fails (a file it cannot read, an encoder it cannot find, a manifest or asset that is out of date, a build that fails, a replay whose end state does not match, a benchmark that regressed, a broken reference `check` found, a file `generate` would overwrite, an asset without a credit) and 2 when it is called wrongly (an unknown flag, a missing required flag, flags that conflict, an argument it does not take), in which case it also prints its usage. Its errors start with its name, such as `game pack: -in directory is required`, except an unknown or malformed flag, which Go's flag package reports in its own words, such as `flag provided but not defined: -nope`. An unknown command also exits with 2, and points to `game help` instead of printing a usage.

### pack

```sh
go tool game pack -in art/ui -out assets -name ui -padding 2
```

Writes `ui.png` and `ui.json` to `-out` (`ui-0.png`, `ui-0.json`, `ui-1.png` and so on when the frames need more than one page) and prints each path. `-padding` and `-extrude` set the transparent gap and the repeated edge pixels around each frame, `-max-width` and `-max-height` bound a page, `-scale` is written to `meta.scale`, and transparent borders are trimmed, each frame keeping its offset in its untrimmed size, unless `-trim=false`.

### audio

```sh
go tool game audio -in sfx/jump.wav -out assets/jump.wav -rate 22050 -channels 1 -peak-db -1
go tool game audio -in music/theme.wav -out assets/theme.m4a -format aac -rms-db -14 -bitrate 192
```

Reads a WAV file, resamples it to `-rate`, mixes it to `-channels`, and normalizes either its peak (`-peak-db`) or its loudness (`-rms-db`, with soft clipping), not both. Passing both is a usage error. WAV output is written in Go; MP3 needs `ffmpeg` on `PATH`, and AAC uses `ffmpeg`, or `afconvert` on macOS when `ffmpeg` is missing; otherwise the command fails with an error naming the missing encoder.

When an [import settings sidecar](#import-settings-beside-each-asset) sits beside the input, such as `sfx/jump.wav.import.yaml`, its `audio` settings apply as they do in `assets`, loop points included, and a flag given on the command line wins over the sidecar's setting of the same name: `-peak-db` or `-rms-db` replaces the sidecar's level, whichever kind it is. A loop needs `wav` output, and a sidecar holding settings for another kind of asset is refused.

### manifest

```sh
go tool game manifest -in assets -out assets/manifest_gen.go -pkg assets
go tool game manifest -in assets -out assets/manifest_gen.go -pkg assets -check
```

Writes a Go file declaring a typed reference for every asset under `-in`, skipping files and folders whose names start with a dot, Go files and import settings sidecars (`*.import.yaml`), or prints it when `-out` is empty. `-embed` emits `//go:embed` directives instead of `asset:` URIs, with `-embed-dir` prefixed to their patterns. `-check` leaves `-out` alone and fails when it differs from what would be generated, for CI; a `go:generate` line running the first form keeps the file current. The generated file's header names the command that wrote it (`// Code generated by game manifest. DO NOT EDIT.`), so a manifest generated by the earlier `gamemanifest` command must be regenerated once before `-check` passes.

### tex

```sh
go tool game tex -format bc1 -mipmaps -srgb -out assets/albedo.ktx2 art/albedo.png
```

Encodes the image named by the argument (or `-in`) into a KTX2 file at `-out` (`-o` for short), by default the input's path with a `.ktx2` extension. `-format` is `bc1`, `bc3`, `etc2` or `rgba8`, `-mipmaps` writes the whole chain down to 1x1, and `-srgb` marks the texture sRGB and filters its mipmaps in linear light.

When an [import settings sidecar](#import-settings-beside-each-asset) sits beside the input, such as `art/albedo.png.import.yaml`, its `image` settings apply as they do for a texture target of `assets`: `format`, `mipmaps` and `srgb`, under the flags given, which win, and `filter`, which is written into the KTX2 file as `game.filter`. `slice` is an atlas's setting and is not read, since a texture holds the whole image, and a sidecar holding settings for another kind of asset is refused.

### editor

```sh
go tool game editor scenes/level1.yaml
go tool game editor .
```

Opens the NSGo Scene Editor in a window on the scene file named, or on a small demo room when none is. A directory or a `game.yaml` opens the game's [project file](project.md) and its main scene. `-library` adds a panel that installs packs from that [library](library.md) into the open project. It has no [editor plugins](editor.md) built in: a game with its own builds its own editor command, as [Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic) does. The scene format is described in [Editor & Scene Format](editor.md).

### release

```sh
go tool game release
go tool game release -channel store -release-notes notes/0.2.0.md
go tool game release -target macos_arm64,web -output build -dry-run
```

Builds the game in the directory named, the current one by default, which holds its `nsgo.yaml`, for every platform in one run of `nsgo build`: macOS, Windows and Linux on both architectures, web, iOS and Android on arm64 unless told otherwise. Each target's package goes to `<output>/<name>-<version>-<target>`, and the files a release uploads (disk images, installers, Debian and Arch packages, AppImages, the IPA, the APK and the site's zip, each with a `.sha256`) to `<output>/releases`, as `nsgo build` lays them out. A target that fails stops the run and exits with 1. The game's module must have NSGo as a tool (`tool github.com/nsgo-dev/nsgo/cmd/nsgo` in `go.mod`, as the starter has), since the command runs `go -C <dir> tool nsgo build`, so the build uses the module's own NSGo version; `-dry-run` prints that command instead of running it.

The targets come from `-target`, else from the `export` presets of the `game.yaml` beside `nsgo.yaml`, in order, a preset without `targets` building its platform name, else the six platforms above, or only macOS, Windows and Linux when the channel (`-channel`, else `nsgo.yaml`'s `updates.channel`) is `steam`, whose depots `nsgo build` makes of desktop targets alone. `nsgo.yaml`'s `build.targets` is not used: it is the everyday list of a plain `nsgo build`, which in the starter holds a simulator build and leaves out Linux, so a release's own list belongs in the presets. The output directory comes from `-output`, else from `game.yaml`'s `export.output`, else from `nsgo.yaml`'s `build.output`:

```yaml
version: 1
export:
  output: dist
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64]
    - platform: linux
    - platform: web
    - platform: ios
    - platform: android
```

The command reads `game.yaml`'s `version`, which must be the integer 1, its `export` section and, for the credits file, its `assets` section. It refuses top-level fields outside the project file's (`version`, `name`, `id`, `appVersion`, `window`, `display`, `input`, `audio`, `autoload`, `export`, `editor` and `assets`), so a misspelt `export` is not skipped, and in `export` it refuses unknown fields, an empty `presets` list, unknown platforms and targets, a target of another platform, duplicates and an output outside the project; the rest of the file is the project file's. `-channel` is `nsgo build`'s: `store` builds the packages stores take (a Mac App Store package, universal when both macOS architectures are built, a Windows MSIX, a Flatpak bundle and an Android App Bundle) in place of the disk images, installers and APKs handed out directly, and `-release-notes` and `-jobs` pass through too. Signing, notarizing and store uploads follow `nsgo.yaml` and `nsgo publish`.

When `game.yaml` has an `assets` section, which the whole file must then pass [project file](project.md) validation for, the release ships a credits file: after `nsgo build` succeeds, the command writes `CREDITS.md` into the output directory, beside the packages, listing the author, license and source of every asset that section reaches and each notice a license asks to be shipped, from the `credit` section of each asset's [import settings sidecar](#import-settings-beside-each-asset), as [`game credits`](#credits) writes it. The assets are the images each atlas packs, the input of each texture, audio, font and model target, the buffers and images each model names by relative path, which its GLB embeds, and every file the manifest lists in its folder, such as fonts, maps and models, leaving out the files the targets write, which are made from those inputs. An asset without a credit stops the release before anything is built, with the list of the sidecars to write; a file the manifest lists that a target wrote before its output changed counts as an asset until `game assets` removes it. `-dry-run` checks the credits too and names the file it would write. A game without an `assets` section gets no credits file, and the command notes it on standard error. Files the game loads from elsewhere, such as `autoload` resources or files it embeds outside the manifest's folder, are not listed, so credit those yourself.

The credits file is written beside the packages, not into them, since `nsgo build` decides what a package holds: it is for the publisher to put on the store page or download page. To ship it inside every package as well, write it into NSGo's `resources` folder, which `nsgo build` copies into each package, before the release: `go tool game credits -out resources/CREDITS.md`.

Which targets a machine can build is what `nsgo doctor` shows: iOS needs a Mac with Xcode, and the macOS disk image needs `hdiutil` in a logged-in session, so a release of every platform runs on a Mac.

### replay

```sh
go tool game replay ./...
go tool game replay -run '^TestBossReplay$' ./levels
```

Plays a game's [input recordings](replay.md) headless, for CI. A recording needs the game's own simulation to play, so the command plays them through the game's tests: it first loads every `*.replay.yaml` file under each directory and fails, without running any test, when one cannot be played (an unknown key, a name missing from the header, a step out of order) or when a directory holds none, so a mistyped path cannot pass. It then runs the tests whose names match `-run`, by default every test with `Replay` in its name, headless with `go tool nsgo test`, and fails when one fails, which is what `replay.AssertEndState` does when a run ends in another state than the one the test expects. It also fails when no test in a directory passed, as when `-run` matches no test, so a misnamed test or a mistyped pattern cannot pass without playing a recording. Name the tests that play recordings accordingly, such as `TestLevel1ClearReplay`.

Each directory is a tree, as `nsgo test` reads it: with or without a trailing `/...`, its subdirectories are searched and tested too, skipping directories whose names start with `.` or `_` and nested modules but not `testdata`. The default is `./...`. The command runs `go tool nsgo`, so the module needs NSGo's command as a tool, as an NSGo app has.

### bench

```sh
go tool game bench ./sprite ./tilemap
go tool game bench -update ./sprite ./tilemap
go tool game bench -in bench.txt
```

Runs the benchmarks under each directory headless with `go tool nsgo test -bench`, `-count` times (3 by default) for `-benchtime` each (200ms by default), takes each benchmark's median time, bytes and allocations per operation, and compares them with the baseline file named by `-baseline`, by default `benchmarks/baseline.yaml`. It prints a line per benchmark in the baseline and fails when one grew past its limit or was not measured, or when the baseline holds no benchmark; a measured benchmark the baseline lacks is listed as new and does not fail. `-bench` narrows the benchmarks run, as `go test -bench` does. `-in` reads the results from a file of `go test -bench -benchmem` output instead of running anything.

`-update` writes the measured medians to the baseline instead, creating it when it does not exist, and keeps the benchmarks it did not measure. The baseline is a YAML text file to review and edit like any other: the format version, a note, the regression allowed for each metric in percent, and one line per benchmark in name order. Loading and saving it changes nothing, and unknown keys and missing metrics are refused.

```yaml
version: 1
note: Update with go tool game bench -update ./sprite ./tilemap
max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
benchmarks:
  BenchmarkBatchDraw1000: {ns_per_op: 155412, bytes_per_op: 72232, allocs_per_op: 754}
  BenchmarkRaycast: {ns_per_op: 827.3, bytes_per_op: 0, allocs_per_op: 0}
```

Benchmarks are named without their `-GOMAXPROCS` suffix, so a baseline recorded on one machine is read on another, but time per operation depends on the machine: record the baseline on the kind of machine CI checks it on. The format is read and written by the `tooling/bench` package.

### new

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new mygame
go tool game new -name "Space Rocks" -module example.com/rocks rocks
go tool game new -list
```

Writes a project from a template into the directory named, creating it if needed, and prints each file's path. It then runs `go get -tool github.com/nsgo-dev/game/packages/game/cmd/game@<version>` and `go mod tidy` there, which need the Go module proxy, so the project requires that version of the game layer and has the `game` tool beside the `nsgo` tool, and builds and runs with `go tool nsgo dev` without edits.

The game is named after the directory unless `-name` gives a name (letters, digits, spaces, `-` and `_`, starting with a letter), which `game.yaml` and `nsgo.yaml` hold as `name`, with `id` derived from it as `dev.nsgo.<name in lower case without spaces or punctuation>`. The module path is the name in lower case with its words joined by `-` unless `-module` gives one. A module path that is a standard-library package's, such as `time` for a game called Time, is refused because the project could not build, so such a game needs `-module`, such as `-module example.com/time`. `-version` is the version of the game layer to require, such as `v0.4.0`, a commit or `latest`; by default it is the version of the command running, so `go run …@v0.4.0 new` creates a project on `v0.4.0`, and `latest` when the command was built from a checkout of this repository. `-template` chooses the template, `starter` by default, and an unknown name is refused with the names there are. `-list` prints each template's name, the default marked, beside a line saying what it is, and creates nothing. [Templates](templates.md) lists them with the module `nsgo init -template` takes for each, and describes how a template is laid out and how to add one.

`new` writes nothing when any file it would write already exists in the directory, and fails with 1. When `go get` or `go mod tidy` fails, such as without a network, the files stay where they are written, and the error names the commands left to run in the directory to finish the project: both `go get -tool …` and `go mod tidy` when `go get` failed, and `go mod tidy` alone when it failed.

### dev

```sh
go tool game dev
go tool game dev -target native -fps
go tool game dev ./mygame -lang fr
```

Runs `go tool nsgo dev`, which builds the game, starts it and rebuilds it when its Go code changes, and tells the running game where its assets are: the absolute path of `-assets` (`resources` in the project directory by default) in the `NSGO_GAME_ASSETS` environment variable, which `hotreload.DevAssetsDir` returns. It must be the directory the game's `assets.Manager` reads `"asset:"` URIs from: one made with `assets.WithFS(os.DirFS(hotreload.DevAssetsDir()))`, since a `Manager` without `WithFS` reads `assets/` in the directory `game dev` runs in. A game that starts a watcher on it with `hotreload.NewDevWatcher` sees an atlas, image, data file, shader or material it has loaded change within a second of the file being saved, without restarting; see [Reloading on save](assets.md#reloading-on-save) and [Hot-reloading shaders and materials](render3d.md#hot-reloading-shaders-and-materials). The first argument that is not a flag is the project directory, `.` when there is none, and the arguments after it go to `nsgo dev` unchanged; `-target` and `-fps` are passed on too, and a `-target` among the arguments after the directory decides the target, as it does for `nsgo dev`. `nsgo dev`'s output is game dev's, and Control+C stops both. `go tool game dev` exits with 1 when `nsgo dev` fails.

Assets reload on the `native` and `tui` targets, and on `android` with `-android-host`, whose app's Go runs on this machine and reads the files. On the web and on phones the app cannot reach the asset directory, so game dev says so and the game picks up asset changes when `nsgo dev` rebuilds it. Without an asset directory game dev runs `nsgo dev` as it is and says that assets will not reload. An asset the game embeds with `go:embed` is part of its binary: `nsgo dev` rebuilds and restarts the game when such a file changes, and the watcher's reload shows the change before that.

### check

```sh
go tool game check
go tool game check examples/caverun
```

Checks every scene, map, tileset and atlas under the directory named (or `-in`, or the working directory when neither is given), and the references between them, so a broken reference fails before a player reaches it. Each problem is printed to standard error as `file:line: message`, with the file named from the working directory so an editor can open it, followed by a count, and the command exits with 1; with no problems it prints how many files of each kind it checked and exits with 0:

```text
examples/caverun/assets/level1.tmj:1397: tileset image "tileset.png": not found
game check: 1 problem
```

| Files | What must hold |
| --- | --- |
| Scenes (`*.scene.yaml`, `*.scene.yml`) | They parse and validate as the [scene format](editor.md), with no unknown field; a tilemap `source` is a Tiled `.tmj` map that exists and checks; each sprite's `atlas` exists and has its `frame`; an inline tilemap's `tileset` atlas has a frame for every tile ID it uses; a `material` file exists, is a valid material, and its `texture` exists; a scene that instances or inherits others resolves, and each of its connections' `from` and `to` names an object of the resolved scene ([Checking connections](editor.md#checking-connections)) |
| Tiled maps (`.tmj`) and tilesets (`.tsj`) | External tilesets are JSON tilesets (`.tsj`, or `.json` as older Tiled versions named them; not `.tsx`) that exist; tileset and image-layer images exist, also inside group layers; the map loads with `tilemap.LoadTiled` |
| LDtk projects (`.ldtk`) | Tileset images and level backgrounds exist; no level is saved in a separate `.ldtkl` file, which `tilemap.LoadLDtkAll` does not read (turn off "Save levels to separate files" in LDtk); the project loads with `tilemap.LoadLDtkAll` |
| Sprite atlases (JSON with top-level `frames` and `meta`, as TexturePacker and Aseprite export) | `meta.image` exists; every frame lies inside the image; the atlas loads with `sprite.LoadAtlas` |
| `game.yaml` and every other reference between files | `game.yaml` parses and validates as the [project file](project.md); each autoloaded scene and resource exists, as does each `assets` target's `in` (a folder for an atlas) and every other file a reference names that the rows above do not reach: the scene a scene `inherits` and the scene an object instances (`scene`), whatever its file is called, the materials and tilemap sources in overrides and in scenes not named `*.scene.yaml`, and a material's `texture` wherever the material is used. What `game assets` writes (`out`, `cache`, `manifest.in`) need not exist yet |
| Project indexes (`game.index.yaml`) | They parse, and no two scene or resource files of one project share a `uid`, as a copied file does until `game index` gives it its own; separate projects, such as two games made from one template, may |

A path a map, tileset or atlas writes is relative to that file, as Tiled, LDtk and texture packers write them. A scene's tilemap `source` and `material`, and a material's `texture`, are relative to the project root: the nearest directory above the scene holding `game.yaml`, `nsgo.yaml` or `go.mod`, or the checked directory when there is none. A scene names an atlas the way its game's `sceneformat.AssetResolver` does, so the check finds it by file name: the atlas JSON in the same project whose base name without `.json` is the name (`actors` is `art/actors.json`), or whose path from the project root is (`art/actors`). When several match, the one nearest the scene wins, counting the directories up from the scene and down to the atlas, so an atlas beside the scene beats one in a subdirectory, and two equally near are reported as ambiguous until the scene names one by its path. When no atlas matches but a JSON file of that name does not parse or is not an atlas, that file's fault is reported. A path must be relative and use forward slashes, and must not lead outside the checked directory. PNG, JPEG and GIF images must decode; other images only have to exist.

Directories whose names start with `.` or `_`, and those named `testdata`, are skipped, as the go command skips them. Other JSON and YAML files are not read. A problem the loader of a map or atlas reports names no line, so it is printed as `file: message`; the check runs that loader only once the file's references resolve, so one fault is not reported twice.

When a reference names a file that is gone and the project index knows where it went, the problem says so, and [`game index`](#index) updates the reference:

```text
scenes/room.scene.yaml:10: material "materials/stone.material.yaml": not found; it moved to "materials/rock.material.yaml"; run game index to update the reference
```

The Examples workflow runs `game check` on the sample games. To run it in a game's own CI, add the command as a tool as above and run `go tool game check` from the game's directory. The checks are also a package, `github.com/nsgo-dev/game/packages/game/tooling/assetcheck`, whose `Check` takes an `fs.FS` and returns the problems as values, and whose `CheckWith` also takes the game's component registry to check the events and handlers that connections name.

### assets

```sh
go tool game assets
go tool game assets -check
```

Builds every asset target that `game.yaml` (or the file `-in` names) declares, in one run: it packs the atlases, encodes the textures and converts the audio as `pack`, `tex` and `audio` do, bakes the fonts into bitmap fonts and packs the models into single GLB files, then regenerates the manifest as `manifest` does. Each target is rebuilt only when it changed: a second run with nothing changed writes no file, and changing one image repacks only the atlas it belongs to. It prints a line for each target it built and then the counts, such as `1 built, 4 up to date`.

The targets are the `assets` section of `game.yaml`, the project file, which the command reads through package `project` as the game and the editor do, and which `release` accepts too, so one `game.yaml` holds them beside the window, input, autoload and export settings ([Project File](project.md)). The file is versioned YAML and the command refuses a field it does not know, a file without an `assets` section, and a file the project schema refuses. The section's paths are slash-separated, relative to the file's directory, and may not leave it:

```yaml
version: 1
name: Cave Run
id: dev.nsgo.caverun
appVersion: 0.1.0
window: {width: 480, height: 640}
assets:
  atlases:
    - name: ui            # pages ui.png and ui.json
      in: art/ui          # PNG images, searched recursively
      out: assets/atlases
      padding: 2          # 1 when omitted
      extrude: 1
      maxWidth: 2048
      maxHeight: 2048
      scale: 1
      trim: true          # true when omitted
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1         # rgba8, bc1, bc3 or etc2; rgba8 when omitted
      mipmaps: true
      srgb: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      format: wav         # wav, aac or mp3; wav when omitted
      rate: 22050
      channels: 1
      peakDB: -1          # or rmsDB, not both
      bitrate: 128        # for aac and mp3
  fonts:
    - in: fonts/Inter.ttf # a TrueType or OpenType font
      out: assets/fonts/hud.fnt  # pages hud.png, or hud-0.png, hud-1.png and so on, beside it
      size: 24            # pixels; 32 when omitted
      chars: " 0123456789:" # printable ASCII when omitted
      padding: 1          # 1 when omitted
  models:
    - in: models/ship.gltf  # .gltf or .glb, with the buffers and images it names beside it
      out: assets/ship.glb
      scale: 0.01         # 1 when omitted
      animations: true    # true when omitted
  manifest:
    in: assets
    out: assets/manifest_gen.go
    package: assets
    prefix: "asset:"
    embed: false
    embedDir: ""
```

Every list and the manifest are optional, and each setting means what the flag of the same name means for the single command. Two targets may not write the same file: an atlas named `ui` writes `ui.png` and `ui.json`, or `ui-0.png`, `ui-1.png` and so on with their `.json` files when it needs several pages, so no other target in its folder may write those names, nor another atlas there be named `ui` or `ui-1`. No target, and not the cache, may write over `game.yaml` or a file a target reads, since the next run would then read its own output. An atlas may not write its pages into a folder an atlas reads images from, nor may another target or the cache write a `.png` file there, since the atlas would then pack it on the next run, and the cache may not be inside the manifest's folder, since the manifest would then list it; a folder whose name starts with a dot is skipped by both scans, so any of them may go there. A font's pages are named after its `.fnt` file as an atlas's are after its name, `hud.png` or `hud-0.png`, `hud-1.png` and so on beside `hud.fnt`, and follow the same rules. No target may read a font's page either: fonts are built after textures and models, so a texture made from one would encode the page of the run before.

A font target bakes a TrueType or OpenType font into an [AngelCode BMFont](text.md) that `text.LoadBMFont` reads: each character of `chars` the font has a glyph for is drawn at `size` pixels, at most 2048, in white with its coverage as alpha, the glyphs are packed onto PNG pages up to 2048 pixels a side, `padding` pixels apart, and the `.fnt` text file records each glyph's place, offsets and advance, the line height and baseline, and the font's kerning between the baked characters. A character the font has no glyph for is left out; a font with none of them is an error.

A model target packs a glTF model into one binary glTF file that [gltf](3d-models-blender.md) loads with nothing to resolve: the buffers the model holds or names, a `.glb` file's binary chunk among them, are merged into the GLB's binary chunk, and each image it names by file or data URI is embedded there with its media type. Every other property is kept, extensions and extras included, and the result is checked by loading it. `scale` puts each root node of the model's scenes under a new node scaled by it, one per root even when several scenes list it, such as `0.01` for a model exported in centimeters, and `animations: false` leaves the animations out. The buffers and images the model names must be relative paths inside the project; they are inputs of the target, so editing a texture beside a `.gltf` file repacks it.

What each target was built from is kept in a cache, `.game/assets-cache.yaml` unless `assets.cache` names another path. It is YAML: for each target, a SHA-256 key over its settings, the version of the game layer that built it, the path and content of every input, a model's buffers and images included, and of the import settings sidecar beside it and, for AAC and MP3, the external encoder and its version, and the path and SHA-256 of every file it wrote. A target is up to date when its key matches and its outputs are still on disk with that content, so editing or deleting an output also rebuilds it, and so does installing another encoder. When an atlas needs fewer pages than before, the pages it no longer writes are deleted, and so are other page files of its name in its folder. When a target is renamed, moved to another output or removed from `game.yaml`, the files it last wrote are deleted before the manifest is generated. The manifest needs no cache entry: it is generated each run, which reads no file's content, and written only when it differs. A missing or damaged cache only costs a full rebuild. Add the `.game` folder to `.gitignore`.

`-force` rebuilds every target. `-check` writes nothing in the project: it lists the targets that are out of date and exits with 1 if there are any, for CI. A target the cache does not show as up to date, as in a fresh checkout without the `.game` folder, is built in a temporary folder and counts as up to date when the files in the project are exactly what it writes, so CI needs no cache when the outputs are committed. AAC and MP3 are the exception: another encoder, or another version of one, writes different bytes for the same input, so without a cache entry they count as up to date when their output exists, and CI needs no encoder. An atlas whose folder holds a page file of its name that it does not write, such as `ui-1.png` left from a build that needed more pages, is out of date until a build deletes it. When a target fails, the command keeps the cache of the targets it built before it, so the next run starts from there.

#### Import settings beside each asset

An asset may have a sidecar: a YAML file beside it, named after it with `.import.yaml` appended, such as `art/albedo.png.import.yaml`, that says how it is imported, as Godot's `.import` files do, and who made it under what license. A setting in the sidecar overrides the same setting of the target that reads the file, so one asset's import is changed by editing the file beside it, and the next run rebuilds the targets that read it. A sidecar is versioned YAML that package `github.com/nsgo-dev/game/packages/game/tooling/importsettings` reads and writes; it holds one `image`, `audio`, `font` or `model` section, a `credit` section, or a credit with one of the others, refuses a field it does not know, and is validated by the [published schema](schemas.md) `https://game.nsgo.dev/schemas/import.v1.json`.

```yaml
# art/albedo.png.import.yaml, read by a texture target
version: 1
image:
  filter: nearest     # linear or nearest, written into the KTX2 key/value data as game.filter
  format: rgba8       # rgba8, bc1, bc3 or etc2
  mipmaps: false
  srgb: true
```

```yaml
# art/hero/run.png.import.yaml, read by the atlas whose folder holds run.png
version: 1
image:
  slice: {width: 16, height: 16, margin: 1, spacing: 2, count: 6}
```

```yaml
# music/theme.wav.import.yaml, read by an audio target
version: 1
audio:
  format: wav         # wav, aac or mp3
  rate: 32000
  channels: 2
  rmsDB: -14          # or peakDB; either one replaces the target's normalization
  bitrate: 128
  loop: {start: 44100, end: 1323000}  # in the source's sample frames; end 0 or left out is the end of the sound
```

```yaml
# fonts/Inter.ttf.import.yaml, read by a font target
version: 1
font:
  size: 24
  chars: " 0123456789:"  # each once, in any order
  padding: 2
```

```yaml
# models/ship.gltf.import.yaml, read by a model target
version: 1
model:
  scale: 0.01
  animations: false
```

```yaml
# assets/fonts/pixel.ttf.import.yaml, a credit beside an asset no target imports
version: 1
credit:
  author: Ana Souza
  license: OFL-1.1    # an SPDX license expression
  source: https://example.com/pixel-font
  notice: |-
    Copyright 2026 Ana Souza.
    Licensed under the SIL Open Font License, Version 1.1.
```

- A texture target reads `filter`, `format`, `mipmaps` and `srgb`. `filter` is stored under the key `game.filter` of the KTX2 file's key/value data, and [`render3d.LoadKTX2`](render3d.md) sets the sampler's filters from it when it loads the texture: `nearest` draws it sharp, as pixel art wants, and `linear` blends neighboring texels. `slice` is refused, since a texture holds the whole image, unless an atlas also reads the image.
- An atlas target reads `slice` from the sidecar of each image in its folder: the image is cut into cells of `width` by `height` pixels, `margin` pixels from its top and left edges and `spacing` pixels apart, taken row by row, and cell `i` of `run.png` is packed as the frame `run_i.png`. `count` keeps the first cells; left out, every cell that fits is a frame. The texture settings, `filter` included, are refused there, since an atlas page is one texture for every image packed into it, unless a texture target also reads the image: an image read by both may hold both kinds of settings, and each target takes its own. A frame that takes the name of another image in the folder is refused.
- A font target reads every `font` setting: `size`, `chars` and `padding`. Listing the same characters in another order changes nothing.
- A model target reads every `model` setting: `scale` and `animations`.
- `game release` and `game credits` read `credit`, which any asset's sidecar may hold, beside an `image`, `audio`, `font` or `model` section or on its own; `game assets` builds nothing from it. `author` and `license` are required. `license` is an [SPDX license expression](https://spdx.github.io/spdx-spec/v2.3/SPDX-license-expressions/): identifiers from the SPDX license list such as `CC0-1.0`, `CC-BY-4.0` or `OFL-1.1`, joined by `AND` and `OR` and grouped by parentheses, a `+` for "or any later version" and `WITH` for an exception, such as `MIT OR Apache-2.0`; a license with no identifier, such as a store's or a studio's own, is `LicenseRef-` followed by a name of its own, with its terms in `notice`. Its form is checked, not the identifier against the list, and `NONE` and `NOASSERTION` are refused. `source` is where the asset came from, usually a URL, and `notice` is text the license asks to be shipped with the asset, such as a copyright line, kept word for word and on several lines if need be.
- An audio target reads every `audio` setting. `loop` is written into the WAV output's `smpl` chunk as a forward loop that repeats forever, scaled to the output rate; AAC and MP3 carry no loop points, so a sound with a loop must be converted to `wav`. A loop already in a source WAV's `smpl` chunk is kept the same way.

A sidecar's settings are part of the key of every target that reads its asset, so adding, editing or deleting them rebuilds those targets and no others, and `-check` reports them as out of date until the next run. Its `credit` section, comments and layout are not, since they change no output: fixing an author's name rebuilds nothing. Sidecars are not assets: the manifest skips files named `*.import.yaml`, and an atlas packs only `.png` files. `game tex` and `game audio` read the sidecar beside their input too, under the flags given. A sidecar in a folder a target reads from (an atlas's folder and the folders below it, or the folder of a texture, audio, font or model input) whose asset no target reads is refused, so a mistyped name, a sidecar beside a JPEG in an atlas folder, or one named in another case than its asset, such as `Hero.PNG.import.yaml` beside `hero.png`, is an error rather than settings that silently do nothing. A sidecar that holds only a `credit` says nothing about the build, so it may sit beside any file in those folders, such as a JPEG or an image a model embeds, as long as the file is there under exactly its name.

### generate

```sh
go tool game generate component player_health
go tool game generate system movement
go tool game generate -main scene level_one
go tool game generate prefab enemy
```

Writes the code and files of a new component, system, scene or prefab into the game in `-in`, the working directory by default, laid out as [`game new`](#new) lays out a project, and prints the path of each file it writes. The Go code goes into the package in that directory, under that package's name, and every Go file comes with a test beside it. The name is ASCII letters and digits starting with a letter, in camel case or with its words joined by `-` or `_`, and is written in each form it is used in: `player_health`, `player-health` and `PlayerHealth` all give the Go type `PlayerHealth`, the key `playerHealth` and files named `player_health_*`.

| Kind | Writes | Registers it in |
| --- | --- | --- |
| `component` | `<name>_component.go`: a struct (`PlayerHealth`) and the `sceneformat.ComponentType` scene files set it by (`playerHealth`), both without fields | `Components`, the `sceneformat.ComponentRegistry` that loading a scene checks its objects' components against |
| `system` | `<name>_system.go`: a function `MovementSystem(w *entities.World, dt time.Duration)`, added to the scheduler as `movement` | `Systems`, the `entities.Scheduler` the game steps each fixed step |
| `scene` | `scenes/<name>.scene.yaml`: a scene in the scene format's current version with an empty layer named `main` and the virtual resolution of `game.yaml` (its window's size when it has none); and `<name>_scene.go`, which embeds the file and parses it with `LoadLevelOneScene`, checking its components against `Components` | `game.yaml`'s `autoload` scenes with `-autoload`, and as its `mainScene` with `-main` |
| `prefab` | `prefabs/<name>.scene.yaml`: a scene of one object whose `prefab` is the name, for other scenes to instance with `scene: prefabs/enemy.scene.yaml`; and `<name>_prefab.go`, an `entities.Prefab` (`EnemyPrefab`) of that name | `Prefabs`, the game's prefabs by name |

The registries are declared once, in `components.go`, `systems.go` and `prefabs.go`, which the first component, system, scene or prefab that needs one writes. Each generated file adds itself to its registry in an `init` function, so `generate` never edits a file that is already there. The game uses them where it loads scenes and runs its loop: `sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: Components})` refuses a component or field a scene sets that no component describes, and `Systems.Step(world, dt)` in the `game.Loop`'s `Update` runs the systems in their order, which `entities.After` and `entities.Before` set where each one is added. Fill in the generated code from there: a component's fields and the `sceneformat.Field` of each one a scene may set, a system's queries, a prefab's components with `entities.With`.

What `generate` writes builds, passes the tests `go tool nsgo test` runs and passes `game check` as it is. It writes nothing and fails with 1 when a file it would write already exists, or when the package already declares an identifier it would declare, in any of its files, tests and files for other platforms included; choose another name then. It also fails when the package declares `Components`, `Systems` or `Prefabs` as something other than a variable, or declares a name a generated file imports a package as, such as `entities` or `time`, and refuses `components`, `systems` and `prefabs` as names, since their Go names are the registries'. `-autoload` and `-main` rewrite `game.yaml` in the form `gameproject fmt` writes, so they refuse a `game.yaml` in another form, such as one with comments, which rewriting would lose, until `gameproject fmt` has rewritten it, and refuse a name `game.yaml` already autoloads. The starter's `TestAutoloadsLoad` checks the files `game.yaml` autoloads, so an autoloaded scene that goes missing fails the game's tests; it reads them from the project directory, so it is skipped under `nsgo test -target` on a phone, a simulator or the web, where the generated tests read only the scene files their code embeds.

### mv

```sh
go tool game mv materials/stone.material.yaml materials/rock.material.yaml
go tool game mv -dry-run art gfx
```

Moves or renames a file or directory and rewrites every reference to what it moves: in `game.yaml`, scenes, resource files, and Tiled and LDtk files, whose own paths relative to themselves are rewritten too when they move. Each reference is changed in place, keeping its quotes and the rest of its file byte for byte, comments included. The project index, `game.index.yaml`, is brought up to date. The paths are taken from the working directory and must lie inside the project, the directory `-in` names or else the nearest one at or above the working directory that holds `game.yaml`, `nsgo.yaml` or `go.mod`. Each change is printed; `-dry-run` prints them without making them, as it does for `release`. A move that would leave a reference broken is refused before anything changes, as are moving `game.yaml` or the index, a move onto an existing file and a move out of the project. See [Stable references](project.md#stable-references).

### index

```sh
go tool game index
go tool game index -dry-run examples/caverun
```

Gives every scene and resource file without a `uid` one, as a line after its `version`, points every reference to a file that was moved without the tools at the file's new path, gives a copied file that kept its original's uid a new one, and writes `game.index.yaml` at each project root, listing each scene and resource file's path and uid and each other referenced file's path and SHA-256. It prints each change, or that the index is up to date; `-dry-run` prints them without making them. The directory is the one named, `-in`, or else the nearest at or above the working directory holding `game.yaml`, `nsgo.yaml` or `go.mod`. Unlike `check`, which checks the working directory and what is under it, `mv` and `index` default to that whole project, since a reference to a moved file can be anywhere in it and the index sits at its root: run from a subdirectory, `check` looks at the subdirectory alone while `index` updates the whole project. Run it once to start keeping an index, and again after moving files by hand or with Git; commit the index with the files.

### library

```sh
go tool game library list tiles
go tool game library install dungeon-tiles
go tool game library remove dungeon-tiles
go tool game library digest library/packs/dungeon-tiles
```

Lists the packs of a library that match the words given (`-kind` keeps one kind), with the version the project has installed; installs packs into the project in the working directory (or `-project`), writing their files and recording them and the autoload entries and editor plugins they add in `game.yaml`; installs a newer release over an older one; removes an installed pack; and prints a pack directory's digest for the library's index. The library is the shared one in this repository unless `-index` names another. The index and pack formats, what an install may and may not overwrite, and how to add a pack are in [Asset and Plugin Library](library.md).

### credits

```sh
go tool game credits
go tool game credits -in game.yaml -out CREDITS.md
```

Writes the credits file that `game release` writes beside its packages, to `-out`, or prints it when `-out` is empty, from the `assets` section of `-in` (`game.yaml` by default), without building anything. It lists the same assets as `release`, with the author, license and source from the `credit` section of the [import settings sidecar](#import-settings-beside-each-asset) beside each one, and fails with 1, naming the sidecar each uncredited asset needs, when any has no credit, so CI can run it to keep every asset credited. The file is Markdown and depends only on the credits, so the same sidecars always give the same bytes:

````markdown
# Credits

<!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->

| Asset | Author | License | Source |
| --- | --- | --- | --- |
| `assets/fonts/pixel.ttf` | Ana Souza | OFL-1.1 | <https://example.com/pixel-font> |
| `sfx/jump.wav` | Kenney | CC0-1.0 |  |

## Licenses

| License | Assets |
| --- | --- |
| [CC0-1.0](https://spdx.org/licenses/CC0-1.0.html) | 1 |
| [OFL-1.1](https://spdx.org/licenses/OFL-1.1.html) | 1 |

## Notices

### `assets/fonts/pixel.ttf`

```text
Copyright 2026 Ana Souza.
Licensed under the SIL Open Font License, Version 1.1.
```
````

The assets are sorted by path. The licenses table counts the assets under each license expression and links each single SPDX identifier to its page in the SPDX license list, and every notice follows word for word. Package `github.com/nsgo-dev/game/packages/game/tooling/credits` collects the credits and writes the file, for a game that shows them on a screen of its own.

### pixelfx

```sh
go tool game pixelfx -out assets/fx fire/torch
go tool game pixelfx -set seed=4 -set palette=#102,#215,#43a,#8cf,#fff -flip -name frost-torch -out assets/fx fire/torch
go tool game pixelfx -act -fps 30 -out assets/fx moves/ground-pound
go tool game pixelfx -list
```

Renders one of the [pixelfx](pixelfx.md) effects the command was built with to a PNG sprite sheet and its Aseprite JSON, `<name>.png` and `<name>.json` in `-out`, which `sprite.LoadAtlas` reads: every frame at the effect's own frame rate (or `-fps`), laid out in a grid, with each frame's duration and its pivot at the effect's origin, and one animation tag that loops a loop and plays a one-shot once. The name is the effect's (`torch`) unless `-name` gives one, which must be a file name without a directory. `-set name=value` sets one of the effect's options, as a number, `true` or `false`, a color such as `#4a3f69` (`#rgb`, `#rrggbb`, or `#rrggbbaa` with its alpha ignored), colors separated by commas for a palette, or text, and `-flip` mirrors it. `-act` renders the effect acted out instead: the stock character performing it, with its straw target, at 60 frames per second unless `-fps` says otherwise, and the JSON carries the act's timing for a game with its own character. `-list` lists the effects with their size, length and options. An effect, option or value the command does not know exits with 2. The categories it renders are the ones ported so far; each category's port adds its own.

### completion

```sh
mkdir -p ~/.zfunc
go tool game completion zsh > ~/.zfunc/_game
```

Writes a completion script for `zsh`, `bash` or `fish` to standard output. It completes the subcommands with their summaries, each subcommand's flags with their descriptions, the values `-format` and `release`'s `-channel` take, the kinds `generate` writes (offered for its name too, since a command's arguments complete alike at every position), the effects `pixelfx` renders, and file paths for `-in`, `-out` and the files and directories the other commands take. The script is generated from the commands' definitions, so after upgrading the game layer, write it again to complete the subcommands and flags the upgrade added.

The script completes a command named `game`. Typed as `go tool game`, the shell completes `go` instead, so either install the command with `go install github.com/nsgo-dev/game/packages/game/cmd/game@latest`, or define `game` as a shell function that runs `go tool game`, which works inside the game's module and keeps the version its `go.mod` pins. Save the script to a file once rather than generating it each time a shell starts, since `go tool game` only runs inside the module.

zsh, with the script saved as `_game` in a directory on `$fpath`, in `~/.zshrc`:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit && compinit
game() { go tool game "$@"; }
```

bash, in bash-completion's directory, which loads it the first time `game` is completed, or sourced from `~/.bashrc` without bash-completion:

```sh
mkdir -p ~/.local/share/bash-completion/completions
go tool game completion bash > ~/.local/share/bash-completion/completions/game
```

```bash
game() { go tool game "$@"; }
```

fish, in its completions directory:

```sh
go tool game completion fish > ~/.config/fish/completions/game.fish
```

```fish
function game
    go tool game $argv
end
```
