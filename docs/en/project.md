# Project File

`github.com/nsgo-dev/game/pkg/project` reads, checks, writes and applies `game.yaml`, the one file at a game's root that holds its settings: its name, id and version, its window and virtual resolution, its input map, its audio buses, the scenes and resources it loads at startup, its export presets per platform, the editor plugins it turns on, its asset pipeline and the library packs installed into it. The game reads it at startup, the [`game project`](cli.md#project) command checks and applies it, `game release` reads its export presets, `game assets` builds its asset targets, and the editor opens, edits and saves it.

## The file

```yaml
version: 1
name: Cave Run
id: dev.nsgo.game.examples.caverun
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
        - kind: key
          key: left
        - kind: keyCode
          code: KeyA
        - kind: gamepadButton
          button: dpadLeft
        - kind: touchRegion
          region: {"y": 116, width: 72, height: 64}
        - kind: mouseRegion
          button: left
          region: {"y": 116, width: 72, height: 64}
    - name: right
      bindings:
        - kind: key
          key: right
        - kind: keyCode
          code: KeyD
        - kind: gamepadButton
          button: dpadRight
        - kind: touchRegion
          region: {x: 72, "y": 116, width: 72, height: 64}
    - name: fire
      bindings:
        - kind: mouseButton
          button: right
        - kind: gamepadButton
          button: south
  axes:
    - name: move
      negative: left
      positive: right
  motionAxes:
    - name: steer
      axis: tiltX
      deadZone: 0.1
  gamepadAxes:
    - name: move
      axis: leftX
      deadZone: 0.15
audio:
  master: 0.9
  maxVoices: 28
  buses:
    - name: music
      volume: 0.8
      effects:
        - lowPass: {cutoffHz: 1200, q: 0.7}
        - reverb: {roomSize: 0.5, damping: 0.4, wet: 0.25}
    - name: sfx
      effects:
        - delay: {time: 250ms, feedback: 0.3, wet: 0.2}
  concurrency:
    - name: impacts
      max: 4
      resolve: quietest
      scope: global
      retrigger: 40ms
      stealFade: 30ms
      duck: {amount: 0.25, attack: 20ms, release: 150ms}
  duck:
    - bus: music
      by: dialogue
      amount: 0.4
      attack: 100ms
      release: 500ms
physics:
  gravity2D:
    "y": 980
  gravity3D:
    "y": -9.81
  solverIterations2D: 8
  solverIterations3D: 8
  layers:
    - name: player
      bit: 0
    - name: walls
      bit: 1
    - name: coins
      bit: 2
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
| `origin` | Where the project was copied from: `kind` (`template` for a template `game new` embeds, or `example`, `directory` or `pack`), `source` (the template's Go module, or the directory copied) and, for a template, `path`, its name, such as `cards2d`. `keep` lists the [template residue warnings](cli.md#check) `game check` leaves out for a deliberate as-is copy: `rules`, `data`, `name`, `readme` and `credits`; any other value is refused with its line. |
| `window` | The main window's starting content size in points (`width`, `height`, both required), its resize limits (`minWidth`, `minHeight`, `maxWidth`, `maxHeight`; 0 is no limit), `resizable` (true when left out), an optional `title` and `appearance` (`light` or `dark`; left out, the app follows the system). |
| `display` | The virtual resolution a `screen.Viewport` maps onto the window: `width`, `height`, `mode` (`fit` when left out, `fill`, `stretch` or `expand`), `integerScale`, `pixelSnap` and the CSS `barColor` of letterbox bars. Left out, the game draws at the window's size. |
| `input` | Named `actions`, each with `bindings` of kind `touchRegion` (a `region`), `mouseButton` (a mouse `button`), `mouseRegion` (both), `key` (a layout key name `key`, such as `space` or `left`), `keyCode` (a physical key position `code`, such as `KeyW` or `ArrowLeft`), or `gamepadButton` (a W3C Standard Gamepad `button`: `south`, `east`, `west`, `north`, `leftShoulder`, `rightShoulder`, `leftTrigger`, `rightTrigger`, `back`, `start`, `leftStick`, `rightStick`, `dpadUp`, `dpadDown`, `dpadLeft`, `dpadRight` or `home`), and an optional `assist` block (`toggle`, `mash`, `mashRate` in presses per second, defaulting to 10; `mashRate` requires `mash: true`) declaring which player motor assists apply so menu actions are never latched. Mouse `button` is `left`, `right`, `middle`, `back` or `forward` and regions are in virtual coordinates. `axes` read -1 for their `negative` action and +1 for their `positive` one; `motionAxes` read `tiltX`, `tiltY`, `tiltZ`, `gyroX`, `gyroY` or `gyroZ` with `input.MotionAxisOptions`' fields; `gamepadAxes` read controller sticks (`leftX`, `leftY`, `rightX` or `rightY`) with `input.GamepadAxisOptions`' fields (`deadZone`, `axial`, `sensitivity`, `invert`). A motion axis or gamepad axis may share its name with an axis, and `Actions.Axis` sums all of them and clamps to `[-1, 1]`. An action without bindings is declared for code to bind or an axis to read. |
| `audio` | The default `master` volume, optional global voice budget `maxVoices` (0 uses `sound.DefaultMaxVoices`), the `buses` (each a `sound.Mixer` group with a default `volume` and its DSP `effects` in order: `lowPass` and `highPass` (`cutoffHz`, `q`), `delay` (`time`, `feedback`, `wet`), `reverb` (`roomSize`, `damping`, `wet`), `gain` (`factor`) and `bitcrush` (`bits`, `downsample`)), shared `concurrency` groups (`name`, `max`, `resolve` `refuse`/`oldest`/`farthest`/`quietest`/`lowestPriority`, `scope` `global`/`owner`, `retrigger`, `stealFade`, and older-voice `duck` `{amount, attack, release}`), and bus `duck` rules (`bus`, `by`, `amount`, `attack`, `release`). |
| `physics` | Project-wide 2D and 3D physics defaults: `gravity2D` (`x`, `y`), `gravity3D` (`x`, `y`, `z`), `solverIterations2D`, `solverIterations3D`, named collision `layers` mapping human-readable `name`s (such as `player`, `walls`, `coins`) to unique bit indices `0..31` (`LayerMask`), and `ldtkIntGrid` rules mapping LDtk IntGrid identifiers/group names (`name`) or numeric values (`value`) to `solid`, `oneWay`, and custom `flags` bits (`File.LDtkOptions`, applied automatically by `project.Resolver` when `sceneformat.Build` loads `.ldtk` sources). |
| `autoload` | The `scenes` (scene format YAML files) and other `resources` loaded at startup, each with a unique `name` and a slash-separated `path` inside the project, and the `mainScene` the game starts in. |
| `export` | The build `output` directory and one preset per platform: primary targets (`macos`, `windows`, `linux`, `ios`, `android`, `web`) and experimental targets (`tvos`, `watchos`, `harmonyos`, `miniprogram`; see [Platform support](platforms.md#experimental-targets)), each with the NSGo build `targets` of that platform to build; a preset without targets builds the platform name, which NSGo expands to its default targets. `game release` prints one warning per experimental platform built (and `-strict` turns that warning into an error for CI), while the scene editor marks experimental presets with an Experimental badge without blocking the build. |
| `editor` | The scene editor's settings: `plugins`, the [editor plugins](editor.md#editor-plugins-492-898) it turns on for this project, in the order their panels and tools appear. A plugin is Go code built into the editor binary; this list only turns it on. |
| `assets` | The targets `game assets` builds, each list optional: `atlases` packed from a folder of PNG images (`name`, `in`, `out` and `padding`, `extrude`, `maxWidth`, `maxHeight`, `scale`, `trim`), `textures` encoded into KTX2 (`in`, `out`, `format` `rgba8`, `bc1`, `bc3` or `etc2`, `mipmaps`, `srgb`), `audio` converted from WAV (`in`, `out`, `format` `wav`, `aac` or `mp3`, `rate`, `channels`, `peakDB` or `rmsDB`, `bitrate`), `fonts` baked from TrueType or OpenType, or from a folder of PNG glyph images, into AngelCode BMFonts (`in`, `out` a `.fnt` file, `size`, `chars`, `padding`, and for a glyph folder `glyphs`, `spacing`, `align`), `models` packed from glTF into single GLB files (`in`, `out`, `scale`, `animations`), the `manifest` generated last (`in`, `out`, `package`, `prefix`, `embed`, `embedDir`), and the build `cache` path, `.game/assets-cache.yaml` when left out. Paths are slash-separated and inside the project. An image, a sound, a font or a model may also carry its own settings in an import settings sidecar beside it (`hero.png.import.yaml`), which override its target's, and any asset its author and license, which `game release` lists in the credits file it ships. [Command Line](cli.md#assets) says what each setting does. |
| `navigation` | The settings and agent types [`game navmesh`](cli.md#navmesh) bakes a scene's [navigation meshes](navbake3d.md) with: the voxel `cellSize` and `cellHeight`, `minRegionArea`, `maxEdgeLength`, `maxSimplificationError`, `maxVertsPerPoly` (3 to 12), `detailSampleDistance` (at least `cellSize`) and `detailSampleMaxError`, and the `agents`, each with a unique `name` and its `radius`, `height`, `maxSlope` (degrees, below 90) and `maxClimb`. A size left out takes navbake3d's default, and without agents one called `default` is baked. |
| `packs` | The packs installed from an [asset and plugin library](library.md), each with its `name`, `kind` (`asset`, `plugin` or `template`), `packVersion` (a strict semantic version without a leading `v`, such as `1.0.0`), `sha256` digest, the `dir` its `files` were written to, and the autoload `scenes` and `resources` and editor `plugins` the install added, which must still be in the file. `game library` and the editor's library panel write it; no two packs may claim the same file, entry or plugin. |

`Parse` refuses unknown fields, other versions, a trailing YAML document, duplicate names, actions an axis reads but nobody declares, a main scene that is not autoloaded, paths that leave the project, targets of another platform, values out of range, and asset targets that write the same file, write over `game.yaml` or a file a target reads, or write where another target's scan reads, and says which field is wrong. `Encode` validates a `File` and writes 2-space YAML in a fixed key order that `Parse` reads back unchanged. A person may write the file in any YAML style and with comments; `game project fmt` rewrites it in the canonical form, which keeps no comments.

### Asset delivery

A preset's `delivery` says where its asset groups ship, for platforms that split a game into packages, such as mini games:

```yaml
export:
  presets:
    - platform: miniprogram
      targets: [miniprogram_wechat]
      delivery:
        main: [boot, ui]
        subpackages:
          chapter1: [level1, level2]
        remote: [music]
        caps:
          main: 4194304
          total: 31457280
```

`main` lists the groups in the main package, `subpackages` maps each subpackage name (no spaces or control characters) to its groups, and `remote` lists the groups served from the remote origin. `caps` sets the largest size in bytes of the main package and of all packages together; 0 or an omitted cap leaves it unset. `Parse` refuses an empty group name, a group in more than one place, and a negative cap. A preset without `delivery` builds a single package as before. Only the placement is recorded and checked so far: `game release` does not yet lay the files out per package or fail on a cap.

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
- `ConfigureMixer(mixer)` gives a `sound.Mixer` the default master volume and each bus's default volume (`SetDefaultMaster`, `SetDefaultVolume`) and DSP chain. A volume the player set and the mixer kept in storage wins over the default. A bus without effects leaves the chain the game set in code, as an action without bindings leaves its bindings. At run time the chain applies to in-memory 16-bit PCM WAV sources played with `Music.Play` or loaded with `Mixer.LoadEffect`; other sources play without it, with one log line per group (see [sound](sound.md#bus-effect-chains)).
- `ConfigurePhysicsWorld(world)`, `ConfigureRigidWorld(world)`, and `ConfigurePhysics3DWorld(world)` apply `physics.gravity2D`, `physics.gravity3D`, and solver iterations to `physics.World`, `physics.RigidWorld`, and `physics3d.World`. `PhysicsLayerTable()` and `PhysicsSettings()` convert `physics:` for `sceneformat.ParseWith` and `sceneformat.BuildPhysics`, while `Physics.LayerBit(name)`, `Physics.LayerMask2D(names...)`, and `Physics.LayerMask3D(names...)` resolve named collision layers in Go.
- `NewResolver(fsys)` returns a `Resolver` over an `fs.FS` rooted at the project, which loads what a scene names from the project's own files: its sprite atlases, scene files, materials, physics materials, and the YAML tilesets and Tiled and LDtk maps its tilemaps name. It is the `sceneformat.AssetResolver`, `SceneResolver`, `MaterialResolver`, `PhysicsMaterialResolver` and `FileResolver` (`ReadFile`) that `sceneformat.Build` and `sceneformat.BuildPhysics` take, and lists the project's atlases for the editor's asset browser (`AtlasNames`). A scene names an atlas as `game check` finds it (`NearestAtlases`): the path of an atlas JSON file without `.json`, from the project root or as its base name, the one nearest the scene winning among several (`ForScene` returns a resolver for one scene), with the image its `meta.image` names read from the project beside it. Tilesets and maps are read through `ReadFile`, as `sceneformat.FSAssets` reads them. Atlases are listed and loaded once, until `Refresh`, and scene files and the files `ReadFile` reads are read on every request. `AtlasNamed`, `SkipDir` and `IsAtlasJSON` are the rules the walk and the name lookup share with `game check`: which names name an atlas file, which directories are skipped, and which JSON files are atlases.
- `LoadAutoloads(fsys)` reads every autoloaded scene, parsing and validating it as a scene document (including named physics layers against `physics.layers`), and every resource from an `fs.FS` rooted at the project, such as an `embed.FS`, and fails on the first file that is missing or invalid. `Loaded.Main()` is the main scene.

## game.yaml and nsgo.yaml

NSGo's own `nsgo.yaml` sits beside `game.yaml` and repeats some of its settings. `game.yaml` owns them, and `nsgo.yaml` follows:

| nsgo.yaml | game.yaml |
| --- | --- |
| `name`, `id`, `version` | `name`, `id`, `appVersion` |
| `appearance` | `window.appearance` |
| `build.output`, `build.targets` | `export.output`, the cross-host subset of the presets' targets in order (or all targets for Apple-only projects) |

`SyncNSGo` writes these fields into an `nsgo.yaml`—filtering `build.targets` to targets every host can build (omitting Apple targets and `android_x64`) when any preset is portable so plain `nsgo build` succeeds on Linux and Windows—while keeping its other keys (`dev`, `icon` and the rest) and its comments. `CheckNSGo` reports each field that disagrees and accepts either the portable subset or the full preset target list for `build.targets`. Both read an empty `build:` key as no build settings, as NSGo does. Everything else in `nsgo.yaml` stays NSGo's.

## The game project command

```sh
go tool game project check [dir]
go tool game project [-check] fmt [dir]
go tool game project [-check] sync [dir]
go tool game project targets [dir]
```

- `check` reports every problem of a project at once (`project.Check`): a `game.yaml` that does not parse, an `nsgo.yaml` that disagrees with it, an autoload that is missing or invalid, and a file an installed pack wrote that is missing.
- `fmt` rewrites `game.yaml` in canonical form; with `-check` it fails instead when the file is not.
- `sync` writes the fields `game.yaml` owns into `nsgo.yaml`, creating it when missing; with `-check` it fails instead when they disagree. Run it after editing `game.yaml`.
- `targets` prints the export presets' build targets, comma-separated, for `nsgo build -target`.

`dir` is the project root, the current directory when left out. Inside a project, `go tool game project` runs the same command without an installed `nsgo`. A failed check exits with 1, and a wrong call, such as `-check` with `check` or `targets`, or `-check` after the verb, with 2.

## In the editor

`Editor.OpenProject(path)` opens a `game.yaml`, or the directory holding one, and the main scene it names, and empties the undo history. `EditProject(label, edit)` changes a copy of the project and keeps it when it validates, as one undoable step. `SaveProject()` writes the project in canonical form and syncs the `nsgo.yaml` beside it. The toolbar's Save button saves the project when it was edited, before the scene and even when the scene has no file to save to, and leaves an unedited `game.yaml` as written, comments included. `SetPluginEnabled(name, on)` adds a plugin to `editor.plugins` or removes it as such a step. With `Editor.ProjectAssets` set, opening a project makes the editor's resolver a `project.Resolver` over its directory, so its scenes draw its own atlases, and `OpenProjectScene(path, scene)` opens the project with another of its scenes in place of the main one. `go tool game editor <dir>` opens a project from the command line that way, through the project's own editor command when it has one (`game generate editor`, see [Command Line](cli.md#generate)). `MoveFile(from, to)` moves a file of the project and rewrites the references to it (see Stable references below).

## Stable references

Files refer to each other by path: `game.yaml` autoloads scenes and resources, builds assets from files and folders, and records installed library packs' folders (`packs[].dir`), a scene inherits or instances other scenes and names materials, glTF models, Tiled and LDtk maps and YAML tilesets, a YAML tileset names its image, a material names its texture, Tiled and LDtk files name their tilesets and images (a Tiled tileset may be `.tsj` or `.json`), a `.gltf` model names the buffers and images it keeps in separate files (percent-encoded, as glTF writes them), and a sprite atlas names its page image in `meta.image`. Package `game/tooling/refs` keeps those references working when files are renamed or moved. It reads `game.yaml`'s paths through package `project` (`project.PathRefs`), so a `game.yaml` that does not validate is reported and nothing is moved until it is fixed.

Every scene and resource file carries a stable ID in its header, a `uid` written once and never changed, so it travels with the file:

```yaml
version: 2
uid: 4kq0w8m2c7d9x
name: Cave
```

A uid is 13 characters of `0`-`9` and `a`-`z` without `i`, `l`, `o` and `u` (`resource.NewUID`), and `sceneformat` and `resource` refuse any other value; an empty or null `uid` is read as none, and `game index` fills it in where it is. A uid is unique within one project: two projects, such as two games made from one template, may share one, since each keeps its own index. The project index, `game.index.yaml` beside `game.yaml`, records where each file was the last time a tool looked: each scene and resource file's path with its uid, and every other file a reference names, such as an image or a Tiled map, with the SHA-256 of its contents. The tools keep it, in path order so that it changes only when files do; keep it in version control with the files it describes.

```yaml
# The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.
version: 1
files:
  - path: art/stone.png
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - path: materials/stone.material.yaml
    uid: 4kq0w8m2c7d9x
```

- `go tool game mv <from> <to>` moves or renames a file or a directory and rewrites every reference to what it moves, including the paths a moved Tiled or LDtk file writes relative to itself, and updates the index. `-dry-run` prints the changes without making them. The editor does the same with `Editor.MoveFile(from, to)`, which also updates the open scene, project and clip, unsaved edits included, and with the Move button of its file browser, which calls it. A move in the editor is one undoable step: Undo moves the files back, puts each file the move rewrote back byte for byte, the index included, and removes an index the move created and the directories it created when they are left empty, and Redo moves them once more. A file changed since the move has only its references rewritten back, and a file the move left alone, such as one that already named the destination, stays as it is; a step that a file now on disk blocks stays where it was, and the status bar says why.
- `go tool game index` gives every scene and resource file without a uid one, as a line after its `version`; points each reference to a file that was moved without the tools at the file's new path; gives a copied file that kept its original's uid a new one; and writes the index. Run it once to start, and again after moving files by hand or with Git.
- `go tool game check` fails on a reference to a file that is gone, at the file and line that holds it, and says where the file went when the index finds it again: the scene or resource file that now holds the uid the index lists for the old path, or the one other file with the same extension and contents.

```
scenes/room.scene.yaml:10: material "materials/stone.material.yaml": not found; it moved to "materials/rock.material.yaml"; run game index to update the reference
```

A reference is rewritten in place: its quotes are kept, and the rest of the file, comments and formatting included, is left byte for byte, so a move shows in review as the lines it changes. A move that would leave a reference broken is refused before anything changes: one that would lead out of the project root, one written over several lines, one in a scene that does not parse, or moving an individual tracked file or subfolder inside an installed pack (`packs[].files`) without moving the pack's folder (`packs[].dir`). Atlas, tileset and prefab names in scenes are names the game's resolver looks up rather than paths, so they are not rewritten, and neither is an object's `mesh`, which names a built-in primitive, nor its `modelNode` or `modelMesh`, which name parts inside its model file ([scenemodel](scenemodel.md)). Nor are the shader and texture paths of a hot-reloaded shader material (`hotreload.MaterialDef`), which are relative to the asset directory the game passes its reloaders at run time, `resources` under `game dev` by default, rather than to the project root. What `game assets` writes (an `assets` target's `out`, the `cache`, and the manifest's `in` folder) is rewritten when it moves but need not exist, and is not listed in the index; nor are folders (including installed pack folders). A rename that changes only the case of a name, such as `a.png` to `A.png`, works on file systems that ignore case too.

## The starter and the examples

The starter template and the four example games (`caverun`, `cryptquest`, `slidegem`, `starburst`) keep their settings in `game.yaml`: each its window, and where it has them its virtual resolution, input map, audio buses and export presets. Cave Run also autoloads its level from it. Each has a test that its `game.yaml` is canonical and its `nsgo.yaml` agrees with it. They are indexed too: Cave Run, whose `game.yaml` and maps name files, carries a `game.index.yaml`, and a test in `game/tooling/refs` fails when `game index` would change anything in the examples or the starter, so run `go run ./cmd/game index <dir>` from the repository root after moving or editing a file they name. The others, and the starter, have no scene or resource file and name no file from one, so they have nothing to index until a game adds one.
`brief.yaml` accepts `basis: locked | target | assumption` on sections and feel targets. Explicit target edits may be recorded in the deterministic `changes` list with a date, field, before/after values, and reason.
