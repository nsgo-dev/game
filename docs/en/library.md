# Asset and Plugin Library

`github.com/nsgo-dev/game/pkg/tooling/library` finds shared asset packs, editor plugin packs and templates in a library and installs them into a game project. A library is a text index anyone can add a pack to with a pull request; installing a pack writes its files into the project and records them, and the references they add, in `game.yaml`. `go tool game library` runs it from the command line and the scene editor's library panel runs it from the editor.

## Mods, plugins and packs (#1268)

The engine has three ways to extend a game, and they are not interchangeable:

| | Editor (Go) plugin | Script mod | Library pack |
| --- | --- | --- | --- |
| Written in | Go | TSX | YAML and media |
| Who writes it | the game's developers, third-party tool authors | players | anyone |
| When it loads | compiled into the editor binary | at run time, with hot reload (`Manager.Reload`) | installed into the project |
| Trust | full | sandboxed, capability-gated | carries no code |
| Platforms | all, iOS and the web included | native; the web only with `ManagerOptions.WebUnisolated` | all |
| Extends the editor | yes | no (today) | turns on built-in plugins only |

An editor plugin is an `editor.Plugin` the editor binary registers with `Editor.RegisterPlugin` and a project turns on by name under `editor.plugins` in `game.yaml`; see [Editor plugins](editor.md#editor-plugins-492-898). A script mod is a `mod.yaml` manifest plus a TSX entry script that `modding.Manager` runs in a sandbox limited to the capabilities the manifest asks for and the host grants; see [modding](modding.md). A library pack is a set of files `library.Install` writes into the project and records in `game.yaml`, as this page describes. A pack may not hold Go source or module files (`.go`, `go.mod` and the like are refused), so a plugin pack only turns on editor plugins the editor binary was already built with — it never ships their code.

Which one do you need?

- A new panel, inspector, viewport tool or 3D gizmo in the editor, or component types the editor checks scenes against: an editor plugin — see [Editor plugins](editor.md#editor-plugins-492-898).
- Let players change the shipped game, within limits the game sets: a script mod — see [modding](modding.md).
- Share sprites, scenes, sounds or a starter template: a library pack, below.

A Go plugin is compiled into the binary rather than loaded at run time because Go has no portable way to load code on every platform — iOS and the web cannot load native code at run time — so a plugin ships inside the editor command it extends.

## Finding and installing packs

```sh
go tool game library list
go tool game library list tiles
go tool game library -kind plugin list
go tool game library install dungeon-tiles
go tool game library remove dungeon-tiles
```

`list` prints the packs whose name, description or tags hold every word given, with the version the project in the working directory (or `-project`) has installed:

```text
PACK             KIND   VERSION  INSTALLED  DESCRIPTION
cc0-adventurers  asset  1.0.0    -          Two placeholder one-joint rigged glTF characters sharing a twelve-clip animation inventory.
dungeon-tiles    asset  1.0.0    -          A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
```

`install` fetches each pack named, checks it against the index and installs it into the project; `-dir` puts a pack's files in another directory of the project than the one its manifest names; installing the pack again later, to update it, keeps it in that directory unless `-dir` names another. `remove` takes an installed pack out again. The library is the shared one in this repository's [`library`](https://github.com/nsgo-dev/game/tree/main/library) directory unless `-index` names another: an https URL of an index file, an index file, or a directory holding `index.yaml`.

The shared library holds two packs. `dungeon-tiles` is a 2D tile set. `cc0-adventurers` holds two placeholder characters for the 3D templates, `mage.gltf` and `knight.gltf`: one triangle skinned to one joint each, with the twelve clips `Attack`, `AttackHeavy`, `Block`, `Death`, `Defeat`, `Dodge`, `Hit`, `Idle`, `Jump`, `Run`, `Victory` and `Walk`, so a game can wire its animations to those names before the KayKit Adventurers models (CC0) replace the placeholders. The pack's [`README.md`](https://github.com/nsgo-dev/game/tree/main/library/packs/cc0-adventurers/README.md) lists each model's meshes, skins, bones and clip durations; `go run ./tools/clipinventory` generates it from the models, and the tools module's tests fail when it is stale.

## What installing does

Installing the sample asset pack into a project writes its files under `packs/dungeon-tiles` (the pack's `pack.yaml` included, so its license travels with its files) and adds this to `game.yaml`:

```yaml
autoload:
  resources:
    - name: dungeon-tiles
      path: packs/dungeon-tiles/tiles.json
packs:
  - name: dungeon-tiles
    kind: asset
    packVersion: 1.0.0
    sha256: 69d89c016fc8eaf19a83ea7bf3f14800fc4b6fff1384cd4d9f2db29f6e5ab177
    dir: packs/dungeon-tiles
    files:
      - pack.yaml
      - tiles.json
      - tiles.png
    resources: [dungeon-tiles]
```

The `packs` entry is the [project file](project.md)'s record of the install: the pack's version and digest, the directory and files it wrote, and the autoload `scenes` and `resources` and editor `plugins` it added. A plugin pack adds the editor plugins it names to `editor.plugins`, which turns them on in an editor built with them (see [Editor plugins](editor.md#editor-plugins-492-898)); a plugin the project already turned on stays the project's and is not recorded as the pack's.

An install never overwrites a file the project already has, unless the pack wrote it or it holds what the pack would write, and never a file another pack wrote; it refuses an autoload scene or resource whose name the project already uses. It checks everything before it writes anything, and when a write fails it restores the files it had changed.

A pack can only write where an asset belongs. The directory its files go to and every file it lists must stay inside the project and outside hidden directories (no path element may start with `.`, so a pack cannot write into `.github` or `.git`), with names every platform can hold: no control characters or any of `<>:"|?*`, no element ending in a dot or a space, no Windows device name such as `con` or `aux`, and no two files whose names differ only in case. A pack may not hold Go source or any other file the Go toolchain builds or reads (`.go`, `.s`, `.c`, `.h`, `.cc`, `.cpp`, `.m`, `.syso` and the like, `go.mod`, `go.sum`, `go.work`), so installing one never changes what compiles into the game; an editor plugin pack turns on plugins the editor is built with rather than shipping their code. `game.yaml` is then written in its canonical form, which keeps no comments, as the editor saves it.

## Updating and removing

Installing a pack the project already has updates it, in the directory the installed release went to: the files the new release lacks are removed, the others replaced (a file may become a directory between releases, or a directory a file), and the autoload entries and plugins it added are replaced by the new release's, keeping their places in `game.yaml`. Installing the same release again changes nothing and writes nothing. Removing a pack removes the files it wrote, edited or not, the directories that leaves empty inside the pack's directory (and `packs` itself, for a pack in its default `packs/<name>`), never a directory above it that the project may have had before, and the autoload entries and plugins it added, and unsets a main scene that was one of its scenes. `game project check` and `project.Check` report a file an installed pack wrote that is missing.

## In the editor

`go tool game editor -library <index> <project>` shows a library panel beside the asset browser listing every pack in the library with its kind, version and description and the version the open project has installed. Install, Update and Remove do what the command does and then save `game.yaml`, so they are refused while the project has unsaved edits; save or undo them first. Installing writes files the undo history cannot take back, so it empties the history, as opening a project does. A pack from a library over https is fetched without blocking the editor; the panel's buttons wait until it arrives, and opening another project cancels the fetch. In Go, set `Editor.Library` to a `*library.Library` and call `Editor.InstallPack` and `Editor.RemovePack`.

## The index

```yaml
version: 1
packs:
  - name: dungeon-tiles
    kind: asset
    packVersion: 1.0.0
    description: A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
    tags: [2d, tiles, dungeon, pixel-art]
    license: CC0-1.0
    source: packs/dungeon-tiles
    sha256: 69d89c016fc8eaf19a83ea7bf3f14800fc4b6fff1384cd4d9f2db29f6e5ab177
```

| Field | What it holds |
| --- | --- |
| `version` | The index schema version, `1`. |
| `name` | The pack's name: 1 to 64 lower-case ASCII letters, digits and `-`, starting with a letter or digit; unique in the index. |
| `kind` | `asset`, `plugin` or `template`. |
| `packVersion` | The version of the pack the index offers: a strict semantic version (`MAJOR.MINOR.PATCH[-prerelease][+build]`) without a leading `v`, such as `1.0.0`. |
| `description` | One line saying what the pack holds. |
| `tags` | Lower-case words a search also matches. |
| `license` | The license of the pack's files, as an SPDX expression such as `CC0-1.0` or `MIT OR Apache-2.0`. |
| `source` | The pack's directory: a slash-separated path relative to the index that stays inside the library, or an https URL (plain http only to a loopback host). |
| `sha256` | The pack's digest (below). |

`library.ParseIndex` refuses unknown fields, other versions and duplicate names, and `EncodeIndex` writes the form `ParseIndex` reads back unchanged. A fetched pack whose manifest's name, kind or version differs from its entry, or whose digest differs from `sha256`, is refused.

## A pack

A pack is a directory holding its manifest, `pack.yaml`, and the files the manifest lists:

```yaml
version: 1
name: dungeon-tiles
kind: asset
packVersion: 1.0.0
description: A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
license: CC0-1.0
files:
  - tiles.json
  - tiles.png
autoload:
  resources:
    - name: dungeon-tiles
      path: tiles.json
```

| Field | What it holds |
| --- | --- |
| `version` | The manifest schema version, `1`. |
| `name`, `kind`, `packVersion`, `description`, `license` | As in the index, which repeats them. |
| `into` | The project directory the pack's files go to; left out, `packs/<name>`. |
| `files` | The pack's files other than `pack.yaml`, as slash-separated paths inside its directory. A file not listed is not part of the pack. |
| `autoload` | The `scenes` (`.yaml` or `.yml`) and `resources` among `files` that installing adds to `game.yaml`'s autoload section, each with the `name` it is autoloaded under. |
| `plugins` | The editor plugins a plugin pack turns on; a plugin pack names at least one, and the other kinds none. |

A pack's digest is the SHA-256 of the listing `sha256sum` prints for its files, `pack.yaml` included, named in byte order: one line per file of its SHA-256 in lower-case hexadecimal, two spaces and its path. `go tool game library digest <pack-dir>` prints it.

## Adding a pack to the shared library

1. Add the pack's directory under `library/packs/<name>` with its `pack.yaml` and files. Every file must be yours to share under the license the manifest names.
2. From the repository root, run `go run ./cmd/game library digest library/packs/<name>` and add an entry with that `sha256` to `library/index.yaml`.
3. Open a pull request. The library's tests fetch every pack of the index, so a digest or a manifest that does not match its entry fails CI.

A team can keep a library of its own anywhere it can serve files over https, or in a directory of its repository, and point `-index` at it. Plain http is accepted only for a loopback host, such as a local test server, for the index and for a pack's `source` alike, and every file is limited to 64 MiB. A server that does not connect within 30 seconds or answer within a minute, or a file that takes longer than 10 minutes, is given up on.

## In Go

`library.Open(ctx, location)` reads an index and `Library.Fetch(ctx, name)` fetches and checks a pack; `Index.Search(kind, query)` finds packs. `library.Install(projectDir, pack, dir)` and `library.Remove(projectDir, name)` change a project directory. `PlanInstall` and `PlanRemove` work out the same change without writing, from an `fs.FS` and a `project.File`, and `Change.Apply` writes it.
