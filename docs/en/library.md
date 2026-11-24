# Asset and Plugin Library

`github.com/nsgo-dev/game/packages/game/tooling/library` finds shared asset packs, editor plugin packs and templates in a library and installs them into a game project. A library is a text index anyone can add a pack to with a pull request; installing a pack writes its files into the project and records them, and the references they add, in `game.yaml`. `go tool game library` runs it from the command line and the scene editor's library panel runs it from the editor.

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
PACK           KIND   VERSION  INSTALLED  DESCRIPTION
dungeon-tiles  asset  1.0.0    -          A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
```

`install` fetches each pack named, checks it against the index and installs it into the project; `-dir` puts a pack's files in another directory of the project than the one its manifest names. `remove` takes an installed pack out again. The library is the shared one in this repository's [`library`](https://github.com/nsgo-dev/game/tree/main/library) directory unless `-index` names another: an https URL of an index file, an index file, or a directory holding `index.yaml`.

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

The `packs` entry is the [project file](project.md)'s record of the install: the pack's version and digest, the directory and files it wrote, and the autoload `scenes` and `resources` and editor `plugins` it added. A plugin pack adds the editor plugins it names to `editor.plugins`, which turns them on in an editor built with them (see [Editor plugins](editor.md#editor-plugins-492)); a plugin the project already turned on stays the project's and is not recorded as the pack's.

An install never overwrites a file the project already has, unless the pack wrote it or it holds what the pack would write, and never a file another pack wrote; it refuses an autoload scene or resource whose name the project already uses. It checks everything before it writes anything, and when a write fails it restores the files it had changed. `game.yaml` is then written in its canonical form, which keeps no comments, as the editor saves it.

## Updating and removing

Installing a pack the project already has updates it: the files the new release lacks are removed, the others replaced, and the autoload entries and plugins it added are replaced by the new release's, keeping their places in `game.yaml`. Installing the same release again changes nothing and writes nothing. Removing a pack removes the files it wrote, edited or not, the directories that leaves empty, and the autoload entries and plugins it added, and unsets a main scene that was one of its scenes. `gameproject check` and `project.Check` report a file an installed pack wrote that is missing.

## In the editor

`go tool game editor -library <index> <project>` shows a library panel beside the asset browser listing every pack in the library with its kind, version and description and the version the open project has installed. Install, Update and Remove do what the command does and then save `game.yaml`, so they are refused while the project has unsaved edits; save or undo them first. Installing writes files the undo history cannot take back, so it empties the history, as opening a project does. A pack from a library over https is fetched without blocking the editor. In Go, set `Editor.Library` to a `*library.Library` and call `Editor.InstallPack` and `Editor.RemovePack`.

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
| `packVersion` | The version of the pack the index offers. |
| `description` | One line saying what the pack holds. |
| `tags` | Lower-case words a search also matches. |
| `license` | The license of the pack's files, as an SPDX expression such as `CC0-1.0` or `MIT OR Apache-2.0`. |
| `source` | The pack's directory: a slash-separated path relative to the index that stays inside the library, or an https URL. |
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
2. From `packages/game`, run `go run ./cmd/game library digest ../../library/packs/<name>` and add an entry with that `sha256` to `library/index.yaml`.
3. Open a pull request. The library's tests fetch every pack of the index, so a digest or a manifest that does not match its entry fails CI.

A team can keep a library of its own anywhere it can serve files over https, or in a directory of its repository, and point `-index` at it. Plain http is accepted only for a loopback host, such as a local test server, and every file is limited to 64 MiB.

## In Go

`library.Open(ctx, location)` reads an index and `Library.Fetch(ctx, name)` fetches and checks a pack; `Index.Search(kind, query)` finds packs. `library.Install(projectDir, pack, dir)` and `library.Remove(projectDir, name)` change a project directory. `PlanInstall` and `PlanRemove` work out the same change without writing, from an `fs.FS` and a `project.File`, and `Change.Apply` writes it.
