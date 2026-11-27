# save

`game/save` 将游戏存档槽以带版本号的 JSON 信封存储在 `core.Storage` 或本地目录（如 `cx.DataDir()`）中，支持逐版本数据结构迁移与原子备份恢复。

```go
import "github.com/nsgo-dev/game/pkg/save"
```

## 选择存储方式

根据目标平台选择对应的 `Store`：

- **`NewStorageStore(cx.Storage(), prefix, opts...)`**：将每个槽位存入 `core.Storage` 的 `<prefix>.slot.<slot>` 与 `<prefix>.bak.<slot>` 键下，兼容包括 Web 在内的所有平台。
- **`NewFileStore(cx.DataDir(), opts...)`**：将每个槽位保存为目录下的 `<slot>.json` 与 `<slot>.json.bak`，适合桌面端和移动端的较大存档（Web 端 `cx.DataDir()` 为空，应改用 `NewStorageStore`）。
- **`NewStore(backend, opts...)`**：接收任意自定义 `Backend` 实现（例如云存档）。

```go
type Progress struct {
	Level int    `json:"level"`
	Coins int    `json:"coins"`
	Name  string `json:"name"`
}

store := save.NewStorageStore(cx.Storage(), "mygame", save.WithVersion(1))

if err := store.Save("slot1", Progress{Level: 3, Coins: 120, Name: "Hero"}); err != nil {
	return err
}

var p Progress
if err := store.Load("slot1", &p); errors.Is(err, save.ErrNotFound) {
	p = Progress{Level: 1}
} else if err != nil {
	return err
}
```

使用 `store.Exists(slot)`、`store.Slots()`（返回有序槽位名列表）和 `store.Delete(slot)`（先删除备份再删除主存档，中途中断也不会留下让 `Load` 复活该槽位的备份；删除不存在的槽位返回 `nil`）管理存档槽。来自游戏外部的槽位名（例如云端列表）可用 `save.ValidateSlot(name)` 按同样的规则检查后再交给 `Backend`。包装另一个后端的 `Backend`（例如 `liveops.CloudSyncer.Backend()`）可以通过 `Unwrap() Backend` 方法暴露被包装的后端；基于包装器创建的设置存储会把设置保存在与被包装后端完全相同的位置。

## 版本信封与数据迁移

每个槽位都以 `{"version": N, "data": ...}` 信封格式写入。存档结构升级时，提高 `WithVersion(N)` 并通过 `WithMigration`、`WithMigrations` 或 `RegisterMigration` 注册逐级迁移函数（`v -> v+1`）：

```go
store := save.NewFileStore(cx.DataDir(),
	save.WithVersion(2),
	save.WithMigration(1, func(raw json.RawMessage) (json.RawMessage, error) {
		var v1 struct {
			Level int `json:"level"`
			Gold  int `json:"gold"`
		}
		if err := json.Unmarshal(raw, &v1); err != nil {
			return nil, err
		}
		return json.Marshal(Progress{
			Level: v1.Level,
			Coins: v1.Gold, // 字段重命名
			Name:  "Adventurer",
		})
	}),
)
```

`Load` 读取到旧版本存档时，会按 `1 -> 2 -> 3 ...` 逐步迁移 JSON，再反序列化到 `dst`。`Load` 不会把迁移结果写回，因此在该槽位再次保存之前，每次 `Load` 都会重新运行迁移：每个迁移函数都必须是只依赖输入的纯函数，每次运行结果相同。

信封中出现 `version` 与 `data` 以外的字段，或任一字段重复出现，均视为 `ErrCorrupt`。`save.EnvelopeVersion(payload)` 可以在不解码正文的情况下读取存档信封的模式版本（包括较新构建写入、带有额外未知字段的信封）。

搭配 `liveops.CloudSyncer` 跨设备同步槽位时，请将存储的 `Version` 传入 `CloudSyncOptions.SaveVersion`。在把云端副本写入本地之前，同步器会检查 `save.EnvelopeVersion`；若云端副本版本更新，它会保持本地槽位不变，并报告 `liveops.OutcomeNeedsUpdate`（附带 `LocalVersion` 和 `CloudVersion`），让游戏提示玩家更新，而不是把会导致 `Load` 返回 `ErrFutureVersion` 的未来版本信封写入本地、进而冒险让新存档覆盖云端副本。详见 [cloudsave](cloudsave.md#混合客户端版本)。

可用 `errors.Is` 判断的哨兵错误：

| 错误 | 含义 |
| --- | --- |
| `ErrNotFound` | 指定槽位既无主存档也无备份。 |
| `ErrInvalidSlot` | 槽位名为空或包含不安全的路径/键字符。 |
| `ErrFutureVersion` | 存档版本号高于当前 `Store.Version`。 |
| `ErrMissingMigration` | 缺少升级到 `Store.Version` 所需的迁移步骤。 |
| `ErrCorrupt` | 存储内容不是合法的存档信封。 |

## 原子写入与备份恢复

`Save` 会先完成 JSON 编码再接触底层存储，因此序列化失败不会破坏原有存档。在替换主存档之前，两种内置后端都会先保留上一次有效存档作为备份：

- **`FileBackend`**：写入同目录临时文件并同步落盘，将原有 `<slot>.json` 保存到 `<slot>.json.bak`，原子重命名临时文件，最后同步目录；目录同步失败会作为写入错误返回，但文件系统本身不支持同步目录时（部分网络挂载和 FUSE 挂载会返回 `EINVAL` 或 `ENOTSUP`）跳过这一步。每个后端的首次写入会清理被中断的写入在一小时前留下的 `.save-*.tmp` 文件。
- **`StorageBackend`**：先写入备份键再更新主键，即便遇到 `*core.StorageFullError` 也能保住上一次完好存档。

调用 `Load` 时，若主存档缺失、不可读或损坏，会自动回退到备份恢复。只有主存档缺失、或根本不是 JSON 对象（为空、被截断或是乱码）时，才会用备份重写主存档。主存档因其他原因读取失败（如权限或 I/O 错误），或者是 `Load` 无法使用的 JSON 对象时，保持原样不动，因为它可能比备份更新。若主存档的版本号高于 `Store.Version`，即使信封里有当前版本不认识的字段，也会直接返回 `ErrFutureVersion`，不会静默回退到旧版本备份。

## 读取错误与检查

关于副本内容的错误是 `*loaderr.Error`（见[错误](errors.md)），它给出副本、在存储的 JSON 中的行号、字段（例如 `data.items[1]`）和修复方法。`FileBackend` 的副本以其文件表示，例如 `saves/slot1.json`，备份为 `saves/slot1.json.bak`；`StorageBackend` 的副本以其存储键表示；自定义 `Backend` 的副本以槽位名表示，备份为 `slot1 (backup)`。上文的哨兵错误仍可用 `errors.Is` 匹配它。

`LoadChecked` 是带有解码后检查的 `Load`。检查拒绝的副本会被视为损坏，因此会尝试备份，备份会解码到调用前 `dst` 的值上，而不是被拒绝的副本留下的值上；所有副本都失败时，`dst` 保持该值；检查返回针对某个字段的 `*loaderr.Error` 时，错误会给出该字段在存储副本中 `data` 之下的行号。`entities.LoadWorld` 就用这种方式检查世界快照：

```go
var snap Progress
err := store.LoadChecked("slot1", &snap, func() error {
	if snap.Level < 1 {
		return loaderr.Errorf("level", "start levels at 1", "level %d is below 1", snap.Level)
	}
	return nil
})
```

## 玩家设置（`SettingsStore` 与 `AccessibilitySettings`）

`NewDefaultSettingsStore`（`NewFileSettingsStore` / `NewStorageSettingsStore`）在存档槽之外独立保存 `Settings`（包含 `Audio`、`Bindings`、`Display`、`Controls`、`Camera`、`Locale`、`Accessibility`、`Custom`），同样具备版本信封与备份恢复。调用 `store.Subscribe(fn)` 可在每次 `Save`、`Reset` 或 `Delete` 成功时收到通知（`a11y.NewAssistFromSettings` 借此与 `gameui.SettingsMenu` 实时同步字幕、菜单与特效）。

`Controls` 保存各设备的视角灵敏度（倍率，范围 [`MinLookSensitivity`, `MaxLookSensitivity`]，默认 1）、轴反转、摇杆死区（[0, `MaxStickDeadZone`]，默认 0.15，0 表示关闭）、扳机阈值（默认 0.5，供自行读取模拟扳机的游戏使用）和震动强度（[0, 1]，默认 1）。`Camera` 保存以角度计的垂直视野（[`MinFieldOfView`, `MaxFieldOfView`]，默认 90）、镜头摇晃与摆动强度（[0, 1]，默认 1）以及运动模糊开关。加载时两部分都会校验，在它们出现之前写入的设置文件会加载默认值，并通过 `SettingsTargets.ApplyControls` 和 `SettingsTargets.ApplyCamera` 分发。

`AccessibilitySettings` 是无障碍与玩法辅助偏好的唯一数据模型：

- **统一范围与规范名称**：`CaptionScale` 裁剪在 `[MinCaptionScale, MaxCaptionScale]`（`0.5`–`3.0`），`GameSpeed` 裁剪在 `[MinGameSpeed, MaxGameSpeed]`（`0.25`–`2.0`），`ScreenShake` 裁剪在 `[0, 1]`。`NormalizeColorBlindMode` 同时接受 `"none"` 与 `"normal"`（统一规范为 `"none"`），以及 `"protanopia"`、`"deuteranopia"`、`"tritanopia"` 和 `"high-contrast"`（含 `"highcontrast"` / `"high_contrast"`）。
- **辅助字段与旧文件迁移**：提供 `Captions`、`SFXCaptions`、`Caption`（`CaptionSettings`）、`ReduceFlashes`、`Invincibility`、`AutoAim` 与 `HoldToToggle` 强类型字段；加载包含旧版 `Custom["reduce_flashes"]` 的设置文件时会自动迁移到 `Accessibility.ReduceFlashes`。
- **字幕样式与分类型开关（`CaptionSettings`）**：配置 `Background`（`black`、`gray`、`blue`、`white` 或 `#rrggbb`）、`BackgroundOpacity`（`0`–`1`，默认 `0.9`；调用 `SetBackgroundOpacity(0)` 可显式设为透明底）、`TextColor`（`white`、`yellow`、`cyan`、`green`、`black` 或 `#rrggbb`）、`SpeakerColors`（默认 `true`）、`Dialogue`（默认 `true`）、`Chatter`、`Sounds`（通过 `SetSoundCaptions` / `EffectiveSoundCaptions` 与 `SFXCaptions` 保持同步）及 `Size`（`small`、`medium`、`large`、`xlarge`）。
- **跟随系统默认值**：未显式覆盖时，`EffectiveCaptions(env)`、`EffectiveReduceMotion(env)`、`EffectiveHighContrast(env)` 与 `EffectiveCaptionScale(env)` 会跟随系统环境设置；调用 `SetCaptions`、`SetReduceMotion`、`SetHighContrast` 或 `SetCaptionScale` 可记录显式覆盖值（包括 `false` 或 `1.0`），调用对应的 `Clear*` 方法可恢复跟随系统。

## 整局存档注册表（`save.Game`）

无需为每个子系统手写合并结构体和成对的快照/恢复代码，只需把可存档的子系统一次性注册到 `save.Game`，即可一次调用完成整局存档与读档：

```go
saver := save.NewGame()
saver.Register("bag", 1, bag.SavePart())
saver.Register("gear", 1, gear.SavePart())
saver.Register("quests", 1, quests.SavePart())
saver.Register("dialogue", 1, dialogueRunner.SavePart())
saver.Register("persistence", 1, persistence)
saver.Register("player", 1, save.ValuePart(&player))

if err := saver.Save(store, "slot1"); err != nil {
	return err
}
if err := saver.Load(store, "slot1"); err != nil {
	return err
}
```

- **槽位布局**：`Game.Save` 在 `Store` 信封的 `data` 内写入 `{"parts": {"<name>": {"version": N, "data": ...}}}`，各部件键按确定性顺序写出。
- **内置 `SavePart()` 适配器**：`inventory.Container`、`inventory.Equipment`、`quest.Log`、`stats.Sheet`、`ability.Caster`、`flow.Machine[S]`、`dialogue.Runner` 与 `entities.World` 均提供 `SavePart() save.Part`。`save.Persistence` 直接实现了 `save.Part`、`save.StagedPart` 与 `save.Resetter`。
- **自定义部件**：使用 `save.ValuePart(&state)` 包装值指针，使用 `save.FuncPart(saveFn, loadFn, resetFn...)` 包装读写回调，或使用 `save.StagedFuncPart(saveFn, prepareFn, resetFn...)` 实现先校验后提交的两阶段恢复。
- **全有或全无读档**：`Game.Load` 通过 `Store.LoadChecked` 执行，在修改任何运行中状态前先完成所有部件的解码、迁移与校验，再按注册顺序统一提交。内置适配器都实现了 `save.StagedPart`（`PrepareLoad`），失败不会改动任何子系统；普通 `save.Part` 会在提交前拍摄快照，任一部件失败时自动回滚。若主存档在部件校验阶段失败，`Store.LoadChecked` 会自动回退到备份副本。
- **分部件版本、迁移与新增部件**：升级单个部件的结构时，只需提高该部件版本号并传入 `save.WithPartMigration(from, fn)`（或调用 `saver.RegisterMigration(name, from, fn)`）。当旧槽位中缺少新版本才加入的部件时，`Game.Load` 会按顺序使用 `save.WithPartDefault(v)`、调用 `save.Resetter` 的 `ResetState()`（所有内置适配器均已实现），或保持原样。部件版本号高于当前注册版本返回 `ErrFutureVersion`，缺少迁移步骤返回 `ErrMissingMigration`，且均不会静默回退到旧备份。
- **迁移旧版扁平存档与读取摘要**：在 `Store` 级 `save.Migration` 中调用 `save.EncodeParts`，可将旧版扁平结构体无缝迁移为 `Game` 的分部件槽位；使用 `saver.PeekPart(store, slot, "player", &summary)` 可在不改动运行中游戏的情况下读取单个部件（含迁移）用于存档菜单摘要。

## 按场景持久化的对象状态（`save.Persistence`）

`save.Persistence` 按规范化的场景路径与对象 ID 记录对象的持久化状态，包括已销毁标记（`MarkDestroyed` / `IsDestroyed`）、移动后的 2D/3D 位置与旋转（`SetPosition` / `SetPosition3D` / `SetRotation`）、组件字段覆盖（`SetComponentField` / `ComponentField`）、RPG Maker 风格的独立开关（`SetSwitch` / `Switch`）以及每对象变量（`SetVar` / `Var`）。

将其注册到 `save.Game`，并在进入和离开场景时与 `sceneformat` 配合使用：

```go
// 进入场景时：
spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{
	Components:  reg,
	Persistence: persistence,
	ScenePath:   "scenes/cave.scene.yaml",
})

// 离开场景前：
sceneformat.CapturePersistence(doc, world, spawned, persistence, "scenes/cave.scene.yaml")
```

传给 `CapturePersistence` 的应是从文件解析出的场景本身：它把每个对象与文件中的位置和旋转比较，记录不同的那些；对象回到文件所放的位置后，之前记录的覆盖会被清除。

`sceneformat.ApplyPersistence` 与 `sceneformat.BuildWithPersistence` 会在克隆后的 `SceneDoc` 上应用相同的持久化变更（跳过已销毁对象并清理指向它们的信号连接），不会修改原始已解析的场景文档。`Components` 与 `Vars` 中的整数在 JSON 往返后会保持为 Go `int`，重新加载后仍能通过 `sceneformat.FieldInt` 校验。

## 往返测试辅助（`pkg/save/savetest`）

在测试中使用 `savetest.RoundTrip` 可尽早发现遗漏字段或未能正确恢复的部件：

```go
import "github.com/nsgo-dev/game/pkg/save/savetest"

func TestSaveRoundTrip(t *testing.T) {
	g := newTestGame(t)
	// ……推进游戏，使各子系统状态偏离默认值……
	savetest.RoundTrip(t, g.saver, func() *save.Game {
		return newTestGame(t).saver
	})
}
```

传入新鲜实例工厂时，`RoundTrip` 会把 `g` 写入临时存储、加载到全新的 `*save.Game`、校验两边注册的部件名与版本号完全一致，并逐一比对每个部件加载前后的 JSON 状态。省略工厂直接调用 `savetest.RoundTrip(t, g)` 时，会在重新加载前调用所有 `save.Resetter` 部件的 `ResetState()`。
