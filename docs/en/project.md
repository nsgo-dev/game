# Project File

`github.com/nsgo-dev/game/packages/game/project` reads, checks, writes and applies `game.yaml`, the one file at a game's root that holds its settings: its name, id and version, its window and virtual resolution, its input map, its audio buses, the scenes and resources it loads at startup, its export presets per platform, the editor plugins it turns on and its asset pipeline. The game reads it at startup, the `gameproject` command checks and applies it, `game release` reads its export presets, `game assets` builds its asset targets, and the editor opens, edits and saves it.

## The file

```yaml
version: 1
name: Cave Run
id: dev.nsgo.caverun
appVersion: 0.1.0
window:
  width: 960
  height: 540
  minWidth: 480
  minHeight: 270
  appearance: dark
display:
  width: 320
  height: 180
  mode: fit
  integerScale: true
  pixelSnap: true
  barColor: '#0b0d17'
input:
  actions:
    - name: left
      bindings:
        - kind: touchRegion
          region: {"y": 116, width: 72, height: 64}
        - kind: mouseRegion
          button: left
          region: {"y": 116, width: 72, height: 64}
    - name: right
      bindings:
        - kind: touchRegion
          region: {x: 72, "y": 116, width: 72, height: 64}
    - name: fire
      bindings:
        - kind: mouseButton
          button: right
  axes:
    - name: move
      negative: left
      positive: right
  motionAxes:
    - name: steer
      axis: tiltX
      deadZone: 0.1
audio:
  master: 0.9
  buses:
    - name: music
      volume: 0.8
      effects:
        - lowPass: {cutoffHz: 1200, q: 0.7}
        - reverb: {roomSize: 0.5, damping: 0.4, wet: 0.25}
    - name: sfx
      effects:
        - delay: {time: 250ms, feedback: 0.3, wet: 0.2}
autoload:
  mainScene: title
  scenes:
    - name: title
      path: scenes/title.yaml
  resources:
    - name: level1
      path: assets/level1.tmj
export:
  output: dist
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64]
    - platform: web
editor:
  plugins: [health]
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets/atlases
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1
      mipmaps: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      rate: 22050
      peakDB: -1
  manifest:
    in: assets
    out: assets/manifest_gen.go
```

| Field | What it holds |
| --- | --- |
| `version` | The schema version, `1`. |
| `name`, `id`, `appVersion` | The game's display name, its reverse-DNS identifier and its release version. |
| `window` | The main window's starting content size in points (`width`, `height`, both required), its resize limits (`minWidth`, `minHeight`, `maxWidth`, `maxHeight`; 0 is no limit), `resizable` (true when left out), an optional `title` and `appearance` (`light` or `dark`; left out, the app follows the system). |
| `display` | The virtual resolution a `screen.Viewport` maps onto the window: `width`, `height`, `mode` (`fit` when left out, `fill`, `stretch` or `expand`), `integerScale`, `pixelSnap` and the CSS `barColor` of letterbox bars. Left out, the game draws at the window's size. |
| `input` | Named `actions`, each with `bindings` of kind `touchRegion` (a `region`), `mouseButton` (a `button`) or `mouseRegion` (both), where `button` is `left`, `right`, `middle`, `back` or `forward` and regions are in virtual coordinates. `axes` read -1 for their `negative` action and +1 for their `positive` one; `motionAxes` read `tiltX`, `tiltY`, `tiltZ`, `gyroX`, `gyroY` or `gyroZ` with `input.MotionAxisOptions`' fields; a motion axis may share its name with an axis, and `Actions.Axis` sums both. An action without bindings is declared for code to bind or an axis to read. |
| `audio` | The default `master` volume and the `buses`, each a `sound.Mixer` group with a default `volume` and its DSP `effects` in order: `lowPass` and `highPass` (`cutoffHz`, `q`), `delay` (`time`, `feedback`, `wet`), `reverb` (`roomSize`, `damping`, `wet`), `gain` (`factor`) and `bitcrush` (`bits`, `downsample`). |
| `autoload` | The `scenes` (scene format YAML files) and other `resources` loaded at startup, each with a unique `name` and a slash-separated `path` inside the project, and the `mainScene` the game starts in. |
| `export` | The build `output` directory and one preset per platform (`macos`, `windows`, `linux`, `ios`, `tvos`, `watchos`, `android`, `harmonyos`, `web`, `miniprogram`), each with the NSGo build `targets` of that platform to build; a preset without targets builds the platform name, which NSGo expands to its default targets. |
| `editor` | The scene editor's settings: `plugins`, the [editor plugins](editor.md#editor-plugins-492) it turns on for this project, in the order their panels and tools appear. A plugin is Go code built into the editor binary; this list only turns it on. |
| `assets` | The targets `game assets` builds, each list optional: `atlases` packed from a folder of PNG images (`name`, `in`, `out` and `padding`, `extrude`, `maxWidth`, `maxHeight`, `scale`, `trim`), `textures` encoded into KTX2 (`in`, `out`, `format` `rgba8`, `bc1`, `bc3` or `etc2`, `mipmaps`, `srgb`), `audio` converted from WAV (`in`, `out`, `format` `wav`, `aac` or `mp3`, `rate`, `channels`, `peakDB` or `rmsDB`, `bitrate`), the `manifest` generated last (`in`, `out`, `package`, `prefix`, `embed`, `embedDir`), and the build `cache` path, `.game/assets-cache.yaml` when left out. Paths are slash-separated and inside the project. [Command Line](cli.md#assets) says what each setting does. |

`Parse` refuses unknown fields, other versions, a trailing YAML document, duplicate names, actions an axis reads but nobody declares, a main scene that is not autoloaded, paths that leave the project, targets of another platform, values out of range, and asset targets that write the same file or write where another target's scan reads, and says which field is wrong. `Encode` validates a `File` and writes 2-space YAML in a fixed key order that `Parse` reads back unchanged. A person may write the file in any YAML style and with comments; `gameproject fmt` rewrites it in the canonical form, which keeps no comments.

## Reading it in a game

A game embeds its `game.yaml` and parses it once:

```go
//go:embed game.yaml
var gameYAML []byte

var gameProject = project.MustParse(gameYAML)

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, gameProject.Title())
	// ...
}
```

`nsgo.Run` has no title option, so the root component sets the title with `desktop.SetTitle`: `Title()` is `window.title`, or `name` without one.

The rest of the file applies to the game's own objects:

- `Viewport()` returns the `screen.Viewport` of `display`, and false without one.
- `BindInput(actions)` binds the input map into an `input.Actions`, replacing bindings of the same names and keeping others, such as ones that follow a layout computed in code. `Region(action)` returns the first region bound to an action, where the game draws its on-screen control.
- `ConfigureMixer(mixer)` gives a `sound.Mixer` the default master volume and each bus's default volume (`SetDefaultMaster`, `SetDefaultVolume`) and DSP chain. A volume the player set and the mixer kept in storage wins over the default. A bus without effects leaves the chain the game set in code, as an action without bindings leaves its bindings.
- `LoadAutoloads(fsys)` reads every autoloaded scene, parsing and validating it as a scene document, and every resource from an `fs.FS` rooted at the project, such as an `embed.FS`, and fails on the first file that is missing or invalid. `Loaded.Main()` is the main scene.

## game.yaml and nsgo.yaml

NSGo's own `nsgo.yaml` sits beside `game.yaml` and repeats some of its settings. `game.yaml` owns them, and `nsgo.yaml` follows:

| nsgo.yaml | game.yaml |
| --- | --- |
| `name`, `id`, `version` | `name`, `id`, `appVersion` |
| `appearance` | `window.appearance` |
| `build.output`, `build.targets` | `export.output`, the presets' targets in order |

`SyncNSGo` writes these fields into an `nsgo.yaml`, keeping its other keys (`dev`, `icon` and the rest) and its comments, and `CheckNSGo` reports each field that disagrees. Both read an empty `build:` key as no build settings, as NSGo does. Everything else in `nsgo.yaml` stays NSGo's.

## The gameproject command

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject fmt [-check] [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync [-check] [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject targets [dir]
```

- `check` reports every problem of a project at once (`project.Check`): a `game.yaml` that does not parse, an `nsgo.yaml` that disagrees with it and an autoload that is missing or invalid.
- `fmt` rewrites `game.yaml` in canonical form; with `-check` it fails instead when the file is not.
- `sync` writes the fields `game.yaml` owns into `nsgo.yaml`, creating it when missing; with `-check` it fails instead when they disagree. Run it after editing `game.yaml`.
- `targets` prints the export presets' build targets, comma-separated, for `nsgo build -target`.

`dir` is the project root, the current directory when left out.

## In the editor

`Editor.OpenProject(path)` opens a `game.yaml`, or the directory holding one, and the main scene it names, and empties the undo history. `EditProject(label, edit)` changes a copy of the project and keeps it when it validates, as one undoable step. `SaveProject()` writes the project in canonical form and syncs the `nsgo.yaml` beside it. The toolbar's Save button saves the project when it was edited, before the scene and even when the scene has no file to save to, and leaves an unedited `game.yaml` as written, comments included. `SetPluginEnabled(name, on)` adds a plugin to `editor.plugins` or removes it as such a step. `gameeditor <dir>` opens a project from the command line.

## The starter and the examples

The starter template and the four example games (`caverun`, `cryptquest`, `slidegem`, `starburst`) keep their settings in `game.yaml`: each its window, and where it has them its virtual resolution, input map, audio buses and export presets. Cave Run also autoloads its level from it. Each has a test that its `game.yaml` is canonical and its `nsgo.yaml` agrees with it.
