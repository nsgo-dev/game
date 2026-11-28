# CLI Commands and Text File Formats

## `go tool game` CLI

Outside a module (such as scaffolding a new project), run `go run github.com/nsgo-dev/game/cmd/game@latest <command>`. Inside a module whose `go.mod` declares the `game` tool, run `go tool game <command>` to execute that module's pinned version:

```sh
go get -tool github.com/nsgo-dev/game/cmd/game@latest
```

### Project and development workflow

| Command | Usage | Purpose |
| --- | --- | --- |
| `game new` | `game new [-list -name -module -template -version] <dir>` | Scaffold a game project (`go.mod`, `game.yaml`, `nsgo.yaml`, code and tests) from a template, `starter` by default; `-list` lists the templates |
| `game project` | `game project [-check] check\|fmt\|sync\|targets [dir]` | Check `game.yaml` and its autoloads, rewrite it in canonical form (`fmt`), write the fields it owns into `nsgo.yaml` (`sync`), or print the export presets' build targets; `-check` makes `fmt` and `sync` fail instead of rewriting |
| `game dev` | `game dev [-assets -automation -fps -scene -target] [dir] [nsgo dev flags]` | Run `nsgo dev` with live asset hot reload (`hotreload.DevAssetsDir`) |
| `game generate` | `game generate [-in -autoload -main] (component\|system\|scene\|prefab <name> \| editor)` | Generate Go code, tests, `.scene.yaml` files and registrations, or the project's component-aware editor command |
| `game editor` | `game editor [-library] [project-dir \| game.yaml \| scene.yaml] [clip.animation.yaml [object-id]]` | Open the scene and animation clip editor |
| `game mcp` | `game mcp [-project dir] [install]` | Serve all of the open editor's MCP tools and resources on stdio, forwarding to the editor that has the project open; `install` writes the project-level `.mcp.json` and `.codex/config.toml` for Claude Code and Codex |
| `game check` | `game check [-in] [dir]` | Validate scenes, Tiled/LDtk maps, sprite atlases and every referenced file |
| `game index` | `game index [-dry-run -in] [dir]` | Assign stable UIDs to scene/resource files and update the project index |
| `game mv` | `game mv [-dry-run -in -text-key] <from> <to>` | Move/rename a file, folder or localization text key and rewrite every reference in the project |
| `game library` | `game library [-dir -index -kind -project] list\|install\|remove\|digest ...` | Browse and install digest-checked asset, plugin or template packs into `game.yaml` |
| `game merge` | `game merge [-out -conflict-out] <base> <ours> <theirs> [path]` / `game merge -setup [dir]` / `game merge -check [dir]` | Git merge driver that merges `*.scene.yaml` files object by object by ID, sets up `.gitattributes` and local Git config, or checks that the driver is configured |
| `game schema` | `game schema [-out file] [project-dir \| game.yaml]` | Write the project's deterministic draft-07 scene JSON Schema (including registered components and plugins via `editorcmd -schema`, or the built-in scene schema) |
| `game net` | `game net decode\|stats [-schema -format -resolve-deltas] <capture.jsonl \| ->` | Decode `pkg/online/netplay` packet captures layer by layer using wire schemas, or print traffic statistics per message type and peer |

### Asset pipeline

| Command | Usage | Purpose |
| --- | --- | --- |
| `game assets` | `game assets [-in game.yaml -check -force]` | Incrementally build all atlases, textures, audio and manifests declared in `game.yaml` |
| `game pack` | `game pack -in <dir> [-out -name -padding -extrude -trim -max-width -max-height -scale]` | Pack a folder of PNGs into sprite atlas pages + TexturePacker JSON |
| `game audio` | `game audio -in <wav> -out <file> [-format wav\|aac\|mp3 -rate -channels -bitrate -peak-db -rms-db]` | Normalize, resample and convert audio |
| `game tex` | `game tex [-format bc1\|bc3\|etc2\|rgba8 -mipmaps -srgb -o] <input.png\|jpg>` | Encode textures into KTX2 containers |
| `game manifest` | `game manifest [-in assets -out -pkg -prefix -embed -embed-dir -check]` | Generate a typed Go asset manifest so missing assets fail compilation |
| `game credits` | `game credits [-in game.yaml -out file]` | Generate the asset author/license credits file from `.import.yaml` sidecars |
| `game pixelfx` | `game pixelfx [-out -name -fps -act -flip -set name=value -list] <category/name>` | Render a pixelfx effect (or it acted out) to a PNG sprite sheet + Aseprite JSON for `sprite.LoadAtlas` |
| `game navmesh` | `game navmesh [-in project-dir -out file] <scene.yaml>` | Bake a scene's glTF level geometry into a `*.navmesh.yaml` navigation mesh per agent type of `game.yaml`'s `navigation` section |

### Testing, performance, shipping and shell support

| Command | Usage | Purpose |
| --- | --- | --- |
| `game replay` | `game replay [-run Replay] [dir ...]` | Validate YAML input recordings and run their headless replay tests |
| `game capture` | `game capture [-plan file] [-shot name] [-out directory] [project]` | Check the store screenshots and covers `store/capture.yaml` describes: scenes and recordings exist, images are PNGs at their target sizes |
| `game playtest` | `game playtest [-config -out -recordings -run -count -seeds -check] [dir ...]` | Validate `playtest.yaml`, run headless bot playtests with replays, write a YAML report and fail on missed goals |
| `game bench` | `game bench [-baseline -bench -benchtime -count -in -update] [dir ...]` | Run headless benchmarks and fail on regressions against a YAML baseline |
| `game wire` | `game wire [-in dir\|file -out dir\|file -pkg -lang go\|ts -self -check] [schema.wire.yaml ...]` | Generate deterministic Go or TypeScript codecs from `*.wire.yaml` schemas, or with `-check` fail when the committed ones are stale |
| `game size` | `game size [-kind aab\|apk\|ipa\|wasm -budget -budget-mb -abi-budget-mb -wasm -file -json -markdown] [package]` | Check a WebAssembly, AAB, APK or IPA package against its size budget, with a category and largest-file breakdown |
| `game release` | `game release [-target -channel -output -jobs -release-notes -dry-run] [dir]` | Build release packages for `game.yaml` export presets via `nsgo build` with credits |
| `game completion` | `game completion zsh\|bash\|fish` | Output shell completion scripts |
| `game help` | `game help [command]` | Show commands or a subcommand's flags |

## Text file formats contract

Every file authored by a human or coding agent is plain-text YAML:

1. **Top-level `version` integer** (or `manifestVersion` in `mod.yaml`). Missing `version`, `version: 0`, or an unsupported future version is rejected.
2. **Strict schema validation.** Unknown keys, duplicate keys, and multi-document YAML streams are refused with `loaderr` errors naming file, line, field and fix.
3. **Canonical deterministic round-trip.** Encoders write fixed key order, sort unordered collections, and omit timestamps or random IDs so load -> save produces identical bytes.
4. **Editor & agent JSON Schemas.** Add `# yaml-language-server: $schema=...` headers (`https://game.nsgo.dev/schemas/scene.v1.json`, `scene.v2.json`, `scene.v3.json`, `scene.v4.json`, `tileset.v1.json`, `import.v1.json` and `voxel.v1.json`) or validate programmatically via `jsonschema`.

### Common authored YAML formats

| File / pattern | Package | Purpose |
| --- | --- | --- |
| `game.yaml` | `project` | Project name/ID, window, virtual resolution, input map, audio buses, autoloads, asset targets, export presets, navigation bake settings and agent types (`version: 1`) |
| `<asset>.import.yaml` | `importsettings` | Per-asset texture filter, slicing, audio settings and license/author credits (`version: 1`) |
| `*.scene.yaml` | `sceneformat` | Scenes and prefabs (`version: 1`); version 2 adds scene instancing and inheritance, glTF models, particle emitters, layered tilemaps and LDtk levels; version 3 adds parallax and repeating layers and tilemap offsets; version 4 (current) adds 3D lights, cameras, reflection probes, light probe grids and scene environment settings |
| `*.navmesh.yaml`, named by a scene's `navMesh` | `navbake3d` | Baked 3D navigation meshes, one per agent type, written by `game navmesh` (`version: 1`) |
| `*.tileset.yaml` | `tilemap` | Tilesets: grid image or atlas frames, per-tile properties, collision flags and shapes, animations and terrains, placed by scenes' layered tilemaps (`version: 1`) |
| `<name>.<type>.yaml`, such as `stone.material.yaml` | `resource` | Shared resources; `type` is `material`, `curve`, `gradient`, `animation`, `theme`, `busLayout` or `physicsMaterial` (`version: 1`) |
| `*.replay.yaml` | `replay` | Editable deterministic input recordings (seed, step rate, per-step action/axis transitions; `version: 2`) |
| `playtest.yaml` | `tooling/playtest` | Headless bot playtest configuration (scope, seeded runs, bot profile/parameters, metrics and sourced goals; `version: 1`, schema `playtest.v1.json`) |
| `vars.yaml` / `*.vars.yaml` | `flow` | Project-wide game variables and switches (`bool`, `int`, `float`, `string`), defaults, ranges and docs (`version: 1`, schema `vars.v1.json`) |
| Any `.yaml` or `.yml` file, such as `weapons.yaml` | `datatable` | Typed game data tables (`version: 1`); `.csv` and `.json` exports load too, without a version |
| `mod.yaml` | `modding` | Mod metadata, capabilities and dependencies (`manifestVersion: 1`) |
| Gameplay YAML configs | `particles`, `skeletal`, `cutscene`, `rooms`, `worldmap`, `cards`, `stats`, `ability`, `inventory`, `quest`, `loot`, `online/achievements`, `online/leaderboard`, `fighting`, `framedata`, `platformer`, `vehicle3d`, `bullets`, `rts`, `procgen`, `vn`, `rhythm`, `idle`, `quality`, `voxel` | Domain-specific versioned YAML assets validated by each package's `Parse`/`Encode` |

### Program-written text formats (YAML / JSON)

- **YAML**: asset build cache (`tooling/assetbuild`), benchmark baselines (`tooling/bench`), achievement progress (`online/achievements`), latency calibrations (`rhythm`), ghost laps (`vehicle3d`), undo logs (`undo`), headless state dumps (`statedump`), project index (`tooling/refs`).
- **JSON**: versioned save slots & player settings (`save`), world map discovery (`worldmap`), quest snapshots (`quest`), fog state (`fog`), ECS world snapshots (`entities`), persisted input bindings (`input`), debug console tunables (`tooling/gamedebug`), dev session state (`tooling/devstate`), cloud sync (`online/liveops`), string tables (`l10n`).

### External tool exports read directly

- **Tiled** `.tmj` maps and **LDtk** `.ldtk` projects (`tilemap`), which scenes name as tilemap sources and `sceneformat.FSAssets` loads with no game code
- **TexturePacker** and **Aseprite** JSON sprite atlases (`sprite`)
- **AngelCode BMFont** and **MSDF** fonts (`text`)
- **glTF 2.0** `.gltf` / `.glb` 3D models (`gltf`)
- **Yarn Spinner** `.yarn` dialogue scripts (`dialogue`)
- **Chrome trace** JSON profiler exports (`tooling/gamedebug#trace`)

Binary files are used only for inherently binary media (PNG/KTX2 images, WAV/AAC/MP3 audio, fonts, glTF buffers) and network packets (`online/netplay`).
