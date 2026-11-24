# API 稳定性

模块中的每个包要么是稳定的，要么是实验性的。状态说明一个包在两次发布之间可能怎样变化、变化如何公布，以及应用有多长时间迁移掉它弃用的部分。

## 状态

**稳定**的包只会以应用能够预先安排的方式变化：

- 从 v1.0.0 起，稳定包在同一个主版本内保持导出 API 兼容，与[语义化版本](https://semver.org/lang/zh-CN/)和 Go 自身的[兼容性承诺](https://go.dev/doc/go1compat)所说的一致：不删除或重命名标识符，不改变签名，文档写明的行为保持不变。新增方法、字段、函数和类型属于兼容的变化。
- 在 v1.0.0 之前，语义化版本允许任何变化，而稳定包只在次版本（v0.x.0，从不在补丁版本）中破坏 API，并且要先把它删除或改变的部分弃用至少一个次版本，做法见[弃用](#弃用)。
- 稳定包不导入本模块的任何实验性包，所以实验性包的变化不会破坏它。

**实验性**的包仍在定型，通常是因为在等待 NSGo 的某个基础能力、某个格式决定，或者示例游戏中更多的使用：

- 实验性包可以在任何次版本中改变或删除 API 的任何部分，没有弃用期。补丁版本仍然只修复缺陷。
- 它的每一处 API 变化都会列在 `CHANGELOG.md` 中做出这一变化的版本下，所以升级时不会有意外。
- 应用可以使用实验性包；应当固定模块的版本，并在升级前阅读变更日志。

## 承诺涵盖的范围

- 包的导出 Go API：标识符、签名，以及文档注释写明的行为。
- 包自己定义的文本格式，例如它的 YAML 文件。每种格式都有 `version` 字段；稳定包会一直能加载它成为稳定包以来写出过的每个格式版本，并在加载时迁移。
- 包读取的其他工具的格式，例如 Tiled 和 LDtk 的地图、Aseprite 和 TexturePacker 的图集。稳定包会一直能读取它成为稳定包以来读取过的这些导出格式的每个版本。
- 对命令（`cmd` 目录）而言，是它的参数、选项、退出码和输出文件的格式。

不涵盖：

- `internal/` 下的包，其他模块无法导入它们。
- 文档注释说明为未指定的行为，以及依赖缺陷的行为：修复缺陷不算破坏性变化。
- 性能、内存分配次数和二进制体积，这些由[预算](package-size-budgets.md)跟踪，而不是承诺。
- 最低 Go 和 NSGo 版本，次版本可以提高它们；README 的兼容性表格列出了每个版本的要求。

## 弃用

稳定包替换或删除 API 的某一部分时：

1. 弃用它的版本在该标识符的文档注释中加入以 `Deprecated:` 开头的段落，写明改用什么，遵循 Go 的[弃用约定](https://go.dev/wiki/Deprecated)，这样 gopls、staticcheck 和 pkg.go.dev 都会标出对它的使用：

   ```go
   // DrawAll draws every sprite of the batch.
   //
   // Deprecated: Use Batch.Draw, which also orders the sprites by Z.
   func (b *Batch) DrawAll(d core.Drawing)
   ```

2. 同一个版本在 `CHANGELOG.md` 的 `### Deprecated` 下列出它，并附上跟踪它的 issue。
3. 被弃用的标识符在弃用它的版本之后，至少再完整保留一个次版本，行为不变：在 v0.4.0 中弃用的，最早在 v0.6.0 中删除。
4. 删除它的版本在 `CHANGELOG.md` 的 `### Removed` 下列出它。从 v1.0.0 起，稳定包在下一个主版本之前不删除任何东西。

发布本身，包括标签、变更日志章节以及每个版本需要的 NSGo 版本，见 [#39](https://github.com/nsgo-dev/game/issues/39)。

## 改变状态

- 包通过一个修改下表中对应行、在变更日志中写明、并且已有该包指南的拉取请求，在某个次版本中成为稳定包。它应当至少被 `examples` 中的一个示例游戏使用，并且只依赖稳定包。
- 稳定包不会退回实验性。设计不再适用时，由新的包，或者从 v1.0.0 起由新的主版本取代。
- 新的包从实验性开始。

## 包列表

路径相对于 `github.com/nsgo-dev/game/packages`。模块中的一个测试会检查这张表，确保每个包恰好有一行。

| 包 | 状态 | 指南 |
| --- | --- | --- |
| `game` | 稳定 | [Loop](loop.md) |
| `game/a11y` | 实验性 | [a11y](a11y.md) |
| `game/ability` | 实验性 | [ability](ability.md) |
| `game/accounts` | 实验性 | [accounts](accounts.md) |
| `game/accounts/cmd/accountsd` | 实验性 | [accounts](accounts.md) |
| `game/accounts/pgstore` | 实验性 | [accounts](accounts.md) |
| `game/accounts/sqlitestore` | 实验性 | [accounts](accounts.md) |
| `game/accounts/sqlstore` | 实验性 | [accounts](accounts.md) |
| `game/accounts/storetest` | 实验性 | [accounts](accounts.md) |
| `game/accounts/token` | 实验性 | [accounts](accounts.md) |
| `game/achievements` | 实验性 | [achievements](achievements.md) |
| `game/ai` | 稳定 | [ai](ai.md) |
| `game/anim3d` | 实验性 | [anim3d](anim3d.md) |
| `game/animclip` | 实验性 | [animclip](animclip.md) |
| `game/assets` | 实验性 | [assets](assets.md) |
| `game/bullets` | 实验性 | [bullets](bullets.md) |
| `game/cards` | 实验性 | [cards](cards.md) |
| `game/cloudsave` | 实验性 | [cloudsave](cloudsave.md) |
| `game/cloudsave/cmd/cloudsaved` | 实验性 | [cloudsave](cloudsave.md) |
| `game/cloudsave/sqlstore` | 实验性 | [cloudsave](cloudsave.md) |
| `game/cloudsave/storetest` | 实验性 | [cloudsave](cloudsave.md) |
| `game/cmd/game` | 实验性 | [命令行](cli.md) |
| `game/cmd/gameapi` | 实验性 | |
| `game/cmd/gameproject` | 实验性 | [项目文件](project.md) |
| `game/cmd/gameschema` | 实验性 | [YAML 文件的 Schema](schemas.md) |
| `game/cmd/sizecheck` | 实验性 | [包体积预算](package-size-budgets.md) |
| `game/cmd/wasmprobe` | 实验性 | [包体积预算](package-size-budgets.md) |
| `game/cutscene` | 实验性 | [cutscene](cutscene.md) |
| `game/debug` | 实验性 | [debug](debug.md) |
| `game/editor` | 实验性 | [editor](editor.md) |
| `game/effects` | 实验性 | [effects](effects.md) |
| `game/entities` | 实验性 | [entities](entities.md) |
| `game/fighting` | 实验性 | [fighting](fighting.md) |
| `game/flow` | 稳定 | [flow](flow.md) |
| `game/fog` | 实验性 | [fog](fog.md) |
| `game/framedata` | 实验性 | [framedata](framedata.md) |
| `game/gametest` | 实验性 | |
| `game/gameui` | 实验性 | [gameui](gameui.md) |
| `game/gltf` | 实验性 | [从 Blender 导入 3D 模型](3d-models-blender.md) |
| `game/idle` | 实验性 | [idle](idle.md) |
| `game/input` | 稳定 | [input](input.md) |
| `game/inventory` | 实验性 | [inventory](inventory.md) |
| `game/l10n` | 实验性 | [l10n](l10n.md) |
| `game/leaderboard` | 实验性 | [leaderboard](leaderboard.md) |
| `game/leaderboard/selfhost` | 实验性 | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/leaderboard/selfhost/cmd/leaderboardd` | 实验性 | [leaderboard/selfhost](leaderboard-selfhost.md) |
| `game/lights2d` | 实验性 | [lights2d](lights2d.md) |
| `game/liveops` | 实验性 | [liveops](liveops.md) |
| `game/loaderr` | 稳定 | [可据以行动的错误](errors.md) |
| `game/loot` | 实验性 | [loot](loot.md) |
| `game/math2d` | 稳定 | [math2d](math2d.md) |
| `game/math3d` | 实验性 | [math3d](math3d.md) |
| `game/modding` | 实验性 | [modding](modding.md) |
| `game/net` | 实验性 | [net](net.md) |
| `game/particles` | 实验性 | [particles](particles.md) |
| `game/perf` | 实验性 | [perf](perf.md) |
| `game/physics` | 稳定 | [physics](physics.md) |
| `game/physics3d` | 实验性 | [physics3d](physics3d.md) |
| `game/placeholder` | 实验性 | [占位素材](placeholder.md) |
| `game/platformer` | 实验性 | [platformer](platformer.md) |
| `game/procgen` | 实验性 | [procgen](procgen.md) |
| `game/project` | 实验性 | [项目文件](project.md) |
| `game/quality` | 实验性 | [quality](quality.md) |
| `game/quest` | 实验性 | [quest](quest.md) |
| `game/random` | 稳定 | [确定性指南](determinism.md) |
| `game/refs` | 实验性 | [项目文件](project.md) |
| `game/render3d` | 实验性 | [render3d](render3d.md) |
| `game/replay` | 实验性 | [replay](replay.md) |
| `game/resource` | 实验性 | [resources](resources.md) |
| `game/rhythm` | 实验性 | [rhythm](rhythm.md) |
| `game/rooms` | 实验性 | [rooms](rooms.md) |
| `game/rts` | 实验性 | [rts](rts.md) |
| `game/save` | 稳定 | [save](save.md) |
| `game/scene` | 稳定 | [scene](scene.md) |
| `game/scene3d` | 实验性 | [scene3d](scene3d.md) |
| `game/sceneformat` | 实验性 | [editor](editor.md) |
| `game/screen` | 稳定 | [screen](screen.md) |
| `game/server` | 实验性 | [server](server.md) |
| `game/server/cmd/dedicatedserver` | 实验性 | [server](server.md) |
| `game/skeletal` | 实验性 | [skeletal](skeletal.md) |
| `game/sound` | 稳定 | [sound](sound.md) |
| `game/sprite` | 稳定 | [sprite](sprite.md) |
| `game/statedump` | 实验性 | [statedump](statedump.md) |
| `game/stats` | 实验性 | [stats](stats.md) |
| `game/text` | 实验性 | [text](text.md) |
| `game/tilemap` | 稳定 | [tilemap](tilemap.md) |
| `game/tooling/apiref` | 实验性 | |
| `game/tooling/assetbuild` | 实验性 | [命令行](cli.md) |
| `game/tooling/assetcheck` | 实验性 | [命令行](cli.md) |
| `game/tooling/audioconv` | 实验性 | |
| `game/tooling/bench` | 实验性 | [命令行](cli.md) |
| `game/tooling/datatable` | 实验性 | |
| `game/tooling/devstate` | 实验性 | |
| `game/tooling/hotreload` | 实验性 | |
| `game/tooling/importsettings` | 实验性 | [命令行](cli.md) |
| `game/tooling/jsonschema` | 实验性 | [YAML 文件的 Schema](schemas.md) |
| `game/tooling/ktx2` | 实验性 | |
| `game/tooling/library` | 实验性 | [资源与插件库](library.md) |
| `game/tooling/manifest` | 实验性 | |
| `game/tooling/pack` | 实验性 | |
| `game/tween` | 稳定 | [tween](tween.md) |
| `game/undo` | 实验性 | [undo](undo.md) |
| `game/vehicle` | 实验性 | [vehicle](vehicle.md) |
| `game/vn` | 实验性 | [vn](vn.md) |
| `game/vn/cmd/vnsample` | 实验性 | [vn](vn.md) |
| `game/world3d` | 实验性 | [world3d](world3d.md) |
| `game/worldmap` | 实验性 | [worldmap](worldmap.md) |
