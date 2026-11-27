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

Paths are relative to `github.com/nsgo-dev`. The table is checked by a test in the module, so every package has exactly one row.

| Package | Status | Guide |
| --- | --- | --- |
| `game` | Stable | [Loop](loop.md) |
| `game/cmd/accountsd` | Experimental | [accounts](accounts.md) |
| `game/cmd/cloudsaved` | Experimental | [cloudsave](cloudsave.md) |
| `game/cmd/editor` | Experimental | [Scene Editor](editor.md) |
| `game/cmd/game` | Experimental | [Command Line](cli.md) |
| `game/cmd/gamewire` | Experimental | [netplay/wire](netplay-wire.md) |
| `game/cmd/leaderboardd` | Experimental | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/cmd/nsgo-game` | Experimental | [Command Line](cli.md) |
| `game/pkg/a11y` | Experimental | [a11y](a11y.md) |
| `game/pkg/ability` | Experimental | [ability](ability.md) |
| `game/pkg/ai` | Stable | [ai](ai.md) |
| `game/pkg/ai/navbake` | Experimental | [navbake](navbake.md) |
| `game/pkg/ai/navbake3d` | Experimental | [navbake3d](navbake3d.md) |
| `game/pkg/ai/perception3d` | Experimental | [ai](ai.md) |
| `game/pkg/anim3d` | Experimental | [anim3d](anim3d.md) |
| `game/pkg/animclip` | Experimental | [animclip](animclip.md) |
| `game/pkg/assets` | Experimental | [assets](assets.md) |
| `game/pkg/bullets` | Experimental | [bullets](bullets.md) |
| `game/pkg/cards` | Experimental | [cards](cards.md) |
| `game/pkg/cutscene` | Experimental | [cutscene](cutscene.md) |
| `game/pkg/datatable` | Experimental | |
| `game/pkg/datatable/datatablewatch` | Experimental | |
| `game/pkg/editor` | Experimental | [editor](editor.md) |
| `game/pkg/editor/editorcmd` | Experimental | [editor](editor.md) |
| `game/pkg/editor/editormcp` | Experimental | [mcp](mcp.md) |
| `game/pkg/entities` | Experimental | [entities](entities.md) |
| `game/pkg/fighting` | Experimental | [fighting](fighting.md) |
| `game/pkg/flow` | Stable | [flow](flow.md) |
| `game/pkg/fog` | Experimental | [fog](fog.md) |
| `game/pkg/framedata` | Experimental | [framedata](framedata.md) |
| `game/pkg/gametest` | Experimental | [gametest](gametest.md) |
| `game/pkg/gameui` | Experimental | [gameui](gameui.md) |
| `game/pkg/gltf` | Experimental | [3D Models from Blender](3d-models-blender.md) |
| `game/pkg/idle` | Experimental | [idle](idle.md) |
| `game/pkg/importsettings` | Experimental | [Command Line](cli.md) |
| `game/pkg/input` | Stable | [input](input.md) |
| `game/pkg/inventory` | Experimental | [inventory](inventory.md) |
| `game/pkg/jsonschema` | Experimental | [Schemas for YAML files](schemas.md) |
| `game/pkg/ktx2` | Experimental | |
| `game/pkg/l10n` | Experimental | [l10n](l10n.md) |
| `game/pkg/lights2d` | Experimental | [lights2d](lights2d.md) |
| `game/pkg/loaderr` | Stable | [Errors](errors.md) |
| `game/pkg/loot` | Experimental | [loot](loot.md) |
| `game/pkg/math2d` | Stable | [math2d](math2d.md) |
| `game/pkg/math3d` | Experimental | [math3d](math3d.md) |
| `game/pkg/mcp` | Experimental | [mcp](mcp.md) |
| `game/pkg/mempool` | Experimental | [mempool](mempool.md) |
| `game/pkg/modding` | Experimental | [modding](modding.md) |
| `game/pkg/online/accounts` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/accounts/authtoken` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/accounts/pgstore` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/accounts/sqlitestore` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/accounts/sqlstore` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/accounts/storetest` | Experimental | [accounts](accounts.md) |
| `game/pkg/online/achievements` | Experimental | [achievements](achievements.md) |
| `game/pkg/online/audit` | Experimental | [gameserver](gameserver.md) |
| `game/pkg/online/cloudsave` | Experimental | [cloudsave](cloudsave.md) |
| `game/pkg/online/cloudsave/pgstore` | Experimental | [cloudsave](cloudsave.md) |
| `game/pkg/online/cloudsave/sqlitestore` | Experimental | [cloudsave](cloudsave.md) |
| `game/pkg/online/cloudsave/sqlstore` | Experimental | [cloudsave](cloudsave.md) |
| `game/pkg/online/cloudsave/storetest` | Experimental | [cloudsave](cloudsave.md) |
| `game/pkg/online/gameserver` | Experimental | [gameserver](gameserver.md) |
| `game/pkg/online/leaderboard` | Experimental | [leaderboard](leaderboard.md) |
| `game/pkg/online/leaderboard/selfhost` | Experimental | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/pkg/online/liveops` | Experimental | [liveops](liveops.md) |
| `game/pkg/online/liveops/crashreport` | Experimental | [liveops](liveops.md) |
| `game/pkg/online/netplay` | Experimental | [netplay](netplay.md) |
| `game/pkg/online/netplay/wire` | Experimental | [netplay/wire](netplay-wire.md) |
| `game/pkg/online/presence` | Experimental | [presence](presence.md) |
| `game/pkg/online/wsclient` | Experimental | [wsclient](wsclient.md) |
| `game/pkg/particles` | Experimental | [particles](particles.md) |
| `game/pkg/physics` | Stable | [physics](physics.md) |
| `game/pkg/physics3d` | Experimental | [physics3d](physics3d.md) |
| `game/pkg/pixelfx` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/arcane` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/critters` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/explosions` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/fire` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/holy` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/ice` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/impacts` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/lightning` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/melee` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/moves` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/nature` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/pickups` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/projectiles` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/scifi` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/shadow` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/spawn` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/status` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/transitions` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/ui` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/water` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/pixelfx/weather` | Experimental | [pixelfx](pixelfx.md) |
| `game/pkg/placeholder` | Experimental | [Placeholder Assets](placeholder.md) |
| `game/pkg/platformer` | Experimental | [platformer](platformer.md) |
| `game/pkg/plugin` | Experimental | [Plugins](plugins.md) |
| `game/pkg/plugin/devtools` | Experimental | [Plugins](plugins.md) |
| `game/pkg/plugin/manifest` | Experimental | [Plugins](plugins.md) |
| `game/pkg/postfx` | Experimental | [postfx](postfx.md) |
| `game/pkg/procgen` | Experimental | [procgen](procgen.md) |
| `game/pkg/project` | Experimental | [Project File](project.md) |
| `game/pkg/quality` | Experimental | [quality](quality.md) |
| `game/pkg/quest` | Experimental | [quest](quest.md) |
| `game/pkg/random` | Stable | [Determinism](determinism.md) |
| `game/pkg/render3d` | Experimental | [render3d](render3d.md) |
| `game/pkg/replay` | Experimental | [replay](replay.md) |
| `game/pkg/resource` | Experimental | [resources](resources.md) |
| `game/pkg/rhythm` | Experimental | [rhythm](rhythm.md) |
| `game/pkg/rooms` | Experimental | [rooms](rooms.md) |
| `game/pkg/rts` | Experimental | [rts](rts.md) |
| `game/pkg/save` | Stable | [save](save.md) |
| `game/pkg/scene` | Stable | [scene](scene.md) |
| `game/pkg/scene3d` | Experimental | [scene3d](scene3d.md) |
| `game/pkg/sceneformat` | Experimental | [editor](editor.md) |
| `game/pkg/scenemodel` | Experimental | [scenemodel](scenemodel.md) |
| `game/pkg/screen` | Stable | [screen](screen.md) |
| `game/pkg/skeletal` | Experimental | [skeletal](skeletal.md) |
| `game/pkg/sound` | Stable | [sound](sound.md) |
| `game/pkg/spatial` | Experimental | [spatial](spatial.md) |
| `game/pkg/sprite` | Stable | [sprite](sprite.md) |
| `game/pkg/statedump` | Experimental | [statedump](statedump.md) |
| `game/pkg/stats` | Experimental | [stats](stats.md) |
| `game/pkg/text` | Experimental | [text](text.md) |
| `game/pkg/tilemap` | Stable | [tilemap](tilemap.md) |
| `game/pkg/tilemap/tileshape` | Experimental | [tileshape](tileshape.md) |
| `game/pkg/tooling/assetbuild` | Experimental | [Command Line](cli.md) |
| `game/pkg/tooling/assetcheck` | Experimental | [Command Line](cli.md) |
| `game/pkg/tooling/atlaspack` | Experimental | |
| `game/pkg/tooling/audioconv` | Experimental | |
| `game/pkg/tooling/bench` | Experimental | [Command Line](cli.md) |
| `game/pkg/tooling/credits` | Experimental | [Command Line](cli.md) |
| `game/pkg/tooling/devstate` | Experimental | |
| `game/pkg/tooling/gamedebug` | Experimental | [gamedebug](gamedebug.md) |
| `game/pkg/tooling/hotreload` | Experimental | |
| `game/pkg/tooling/library` | Experimental | [Asset and Plugin Library](library.md) |
| `game/pkg/tooling/manifest` | Experimental | |
| `game/pkg/tooling/playtest` | Experimental | [playtest](playtest.md) |
| `game/pkg/tooling/refs` | Experimental | [Project File](project.md) |
| `game/pkg/tooling/sizecheck` | Experimental | [Package Size Budgets](package-size-budgets.md) |
| `game/pkg/tween` | Stable | [tween](tween.md) |
| `game/pkg/undo` | Experimental | [undo](undo.md) |
| `game/pkg/vehicle3d` | Experimental | [vehicle3d](vehicle3d.md) |
| `game/pkg/vn` | Experimental | [vn](vn.md) |
| `game/pkg/voxel` | Experimental | [voxel](voxel.md) |
| `game/pkg/world3d` | Experimental | [world3d](world3d.md) |
| `game/pkg/worldmap` | Experimental | [worldmap](worldmap.md) |
