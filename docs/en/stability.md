# API Stability

Every package of the module is either stable or experimental. The status says how a package may change from one release to the next, how its changes are announced, and how long an app has to move off what it deprecates.

## Statuses

**Stable** packages change only in ways an app can plan for:

- From v1.0.0, a stable package keeps its exported API compatible within a major version, as [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and Go's own [compatibility promise](https://go.dev/doc/go1compat) describe: no identifier is removed or renamed, no signature changes, and documented behavior holds. Adding methods, fields, functions and types is compatible.
- Before v1.0.0, where Semantic Versioning allows any change, a stable package breaks its API only in a minor release (v0.x.0, never a patch release), and only after deprecating what it removes or changes for at least one minor release, as [Deprecation](#deprecation) describes.
- A stable package imports no experimental package of this module, so a change to an experimental package never breaks it.

**Experimental** packages are still finding their shape, usually because they wait on an NSGo primitive, a format decision or more use in the sample games:

- An experimental package may change or remove any part of its API in any minor release, without a deprecation period. Patch releases still only fix bugs.
- Every change to its API is listed in `CHANGELOG.md` under the release that makes it, so an upgrade is never a surprise.
- An app may use experimental packages; it should pin the module's version and read the changelog before it upgrades.

## What the promise covers

- The exported Go API of a package: its identifiers, signatures, and the behavior its doc comments document.
- The text formats a package defines, such as its YAML files. Each has a `version` field; a stable package keeps loading every version of its formats it has written since it became stable, and migrates them on load.
- The formats of other tools a package reads, such as Tiled's and LDtk's maps and Aseprite's and TexturePacker's atlases. A stable package keeps reading every version of those exports it has read since it became stable.
- For a command (a `cmd` directory), its flags, arguments, exit codes and the formats of its output files.

It does not cover:

- Packages under `internal/`, which no other module can import.
- Behavior a doc comment calls unspecified, and anything that depends on a bug: a fix to a bug is not a breaking change.
- Performance, allocation counts and binary size, which are tracked by [budgets](package-size-budgets.md) rather than promised.
- The minimum Go and NSGo versions, which a minor release may raise; the README's compatibility table lists them for each release.

## Deprecation

When a stable package replaces or removes part of its API:

1. The release that deprecates it adds a paragraph starting `Deprecated:` to the identifier's doc comment, naming what to use instead, as Go's [deprecation convention](https://go.dev/wiki/Deprecated) describes, so gopls, staticcheck and pkg.go.dev flag its uses:

   ```go
   // DrawAll draws every sprite of the batch.
   //
   // Deprecated: Use Batch.Draw, which also orders the sprites by Z.
   func (b *Batch) DrawAll(d core.Drawing)
   ```

2. The same release lists it under `### Deprecated` in `CHANGELOG.md`, with the issue that tracks it.
3. The deprecated identifier keeps working, unchanged, for at least one whole minor release after the one that deprecates it: deprecated in v0.4.0, it is removed in v0.6.0 at the earliest.
4. The release that removes it lists it under `### Removed` in `CHANGELOG.md`. From v1.0.0 nothing is removed from a stable package before the next major version.

The releases themselves, their tags, changelog sections and the NSGo version each one needs, are described in [#39](https://github.com/nsgo-dev/game/issues/39).

## Changing a status

- A package becomes stable in a minor release through a pull request that changes its row below, says so in the changelog, and has the package's guide in place. It should be used by at least one sample game in `examples` and depend only on stable packages.
- A stable package never goes back to experimental. A design it outgrows is replaced by a new package or, from v1.0.0, a new major version.
- A new package starts experimental.

## Packages

Paths are relative to `github.com/nsgo-dev/game/packages`. The table is checked by a test in the module, so every package has exactly one row.

| Package | Status | Guide |
| --- | --- | --- |
| `game` | Stable | [Loop](loop.md) |
| `game/a11y` | Experimental | [a11y](a11y.md) |
| `game/ability` | Experimental | [ability](ability.md) |
| `game/accounts` | Experimental | [accounts](accounts.md) |
| `game/accounts/cmd/accountsd` | Experimental | [accounts](accounts.md) |
| `game/accounts/pgstore` | Experimental | [accounts](accounts.md) |
| `game/accounts/sqlitestore` | Experimental | [accounts](accounts.md) |
| `game/accounts/sqlstore` | Experimental | [accounts](accounts.md) |
| `game/accounts/storetest` | Experimental | [accounts](accounts.md) |
| `game/accounts/token` | Experimental | [accounts](accounts.md) |
| `game/achievements` | Experimental | [achievements](achievements.md) |
| `game/ai` | Stable | [ai](ai.md) |
| `game/anim3d` | Experimental | [anim3d](anim3d.md) |
| `game/animclip` | Experimental | [animclip](animclip.md) |
| `game/assets` | Experimental | [assets](assets.md) |
| `game/bullets` | Experimental | [bullets](bullets.md) |
| `game/cards` | Experimental | [cards](cards.md) |
| `game/cloudsave` | Experimental | [cloudsave](cloudsave.md) |
| `game/cloudsave/cmd/cloudsaved` | Experimental | [cloudsave](cloudsave.md) |
| `game/cloudsave/sqlstore` | Experimental | [cloudsave](cloudsave.md) |
| `game/cloudsave/storetest` | Experimental | [cloudsave](cloudsave.md) |
| `game/cmd/game` | Experimental | [Command Line](cli.md) |
| `game/cmd/gameapi` | Experimental | |
| `game/cmd/gameproject` | Experimental | [Project File](project.md) |
| `game/cmd/gameschema` | Experimental | [Schemas for YAML files](schemas.md) |
| `game/cmd/sizecheck` | Experimental | [Package Size Budgets](package-size-budgets.md) |
| `game/cmd/wasmprobe` | Experimental | [Package Size Budgets](package-size-budgets.md) |
| `game/cutscene` | Experimental | [cutscene](cutscene.md) |
| `game/debug` | Experimental | [debug](debug.md) |
| `game/editor` | Experimental | [editor](editor.md) |
| `game/effects` | Experimental | [effects](effects.md) |
| `game/entities` | Experimental | [entities](entities.md) |
| `game/fighting` | Experimental | [fighting](fighting.md) |
| `game/flow` | Stable | [flow](flow.md) |
| `game/fog` | Experimental | [fog](fog.md) |
| `game/framedata` | Experimental | [framedata](framedata.md) |
| `game/gametest` | Experimental | |
| `game/gameui` | Experimental | [gameui](gameui.md) |
| `game/gltf` | Experimental | [3D Models from Blender](3d-models-blender.md) |
| `game/idle` | Experimental | [idle](idle.md) |
| `game/input` | Stable | [input](input.md) |
| `game/inventory` | Experimental | [inventory](inventory.md) |
| `game/l10n` | Experimental | [l10n](l10n.md) |
| `game/leaderboard` | Experimental | [leaderboard](leaderboard.md) |
| `game/leaderboard/selfhost` | Experimental | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/leaderboard/selfhost/cmd/leaderboardd` | Experimental | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/lights2d` | Experimental | [lights2d](lights2d.md) |
| `game/liveops` | Experimental | [liveops](liveops.md) |
| `game/loaderr` | Stable | [Errors](errors.md) |
| `game/loot` | Experimental | [loot](loot.md) |
| `game/math2d` | Stable | [math2d](math2d.md) |
| `game/math3d` | Experimental | [math3d](math3d.md) |
| `game/modding` | Experimental | [modding](modding.md) |
| `game/net` | Experimental | [net](net.md) |
| `game/particles` | Experimental | [particles](particles.md) |
| `game/perf` | Experimental | [perf](perf.md) |
| `game/physics` | Stable | [physics](physics.md) |
| `game/physics3d` | Experimental | [physics3d](physics3d.md) |
| `game/platformer` | Experimental | [platformer](platformer.md) |
| `game/procgen` | Experimental | [procgen](procgen.md) |
| `game/project` | Experimental | [Project File](project.md) |
| `game/quest` | Experimental | [quest](quest.md) |
| `game/random` | Stable | [Determinism](determinism.md) |
| `game/render3d` | Experimental | [render3d](render3d.md) |
| `game/replay` | Experimental | [replay](replay.md) |
| `game/resource` | Experimental | [resources](resources.md) |
| `game/rhythm` | Experimental | [rhythm](rhythm.md) |
| `game/rooms` | Experimental | [rooms](rooms.md) |
| `game/rts` | Experimental | [rts](rts.md) |
| `game/save` | Stable | [save](save.md) |
| `game/scene` | Stable | [scene](scene.md) |
| `game/scene3d` | Experimental | [scene3d](scene3d.md) |
| `game/sceneformat` | Experimental | [editor](editor.md) |
| `game/screen` | Stable | [screen](screen.md) |
| `game/server` | Experimental | [server](server.md) |
| `game/server/cmd/dedicatedserver` | Experimental | [server](server.md) |
| `game/skeletal` | Experimental | [skeletal](skeletal.md) |
| `game/sound` | Stable | [sound](sound.md) |
| `game/sprite` | Stable | [sprite](sprite.md) |
| `game/statedump` | Experimental | [statedump](statedump.md) |
| `game/stats` | Experimental | [stats](stats.md) |
| `game/text` | Experimental | [text](text.md) |
| `game/tilemap` | Stable | [tilemap](tilemap.md) |
| `game/tooling/apiref` | Experimental | |
| `game/tooling/assetcheck` | Experimental | [Command Line](cli.md) |
| `game/tooling/audioconv` | Experimental | |
| `game/tooling/bench` | Experimental | [Command Line](cli.md) |
| `game/tooling/datatable` | Experimental | |
| `game/tooling/devstate` | Experimental | |
| `game/tooling/hotreload` | Experimental | |
| `game/tooling/jsonschema` | Experimental | [Schemas for YAML files](schemas.md) |
| `game/tooling/ktx2` | Experimental | |
| `game/tooling/manifest` | Experimental | |
| `game/tooling/pack` | Experimental | |
| `game/tween` | Stable | [tween](tween.md) |
| `game/undo` | Experimental | [undo](undo.md) |
| `game/vehicle` | Experimental | [vehicle](vehicle.md) |
| `game/vn` | Experimental | [vn](vn.md) |
| `game/vn/cmd/vnsample` | Experimental | [vn](vn.md) |
| `game/world3d` | Experimental | [world3d](world3d.md) |
| `game/worldmap` | Experimental | [worldmap](worldmap.md) |
