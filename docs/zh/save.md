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

`NewDefaultSettingsStore`（`NewFileSettingsStore` / `NewStorageSettingsStore`）在存档槽之外独立保存 `Settings`（包含 `Audio`、`Bindings`、`Display`、`Locale`、`Accessibility`、`Custom`），同样具备版本信封与备份恢复。调用 `store.Subscribe(fn)` 可在每次 `Save`、`Reset` 或 `Delete` 成功时收到通知（`a11y.NewAssistFromSettings` 借此与 `gameui.SettingsMenu` 实时同步字幕、菜单与特效）。

`AccessibilitySettings` 是无障碍与玩法辅助偏好的唯一数据模型：

- **统一范围与规范名称**：`CaptionScale` 裁剪在 `[MinCaptionScale, MaxCaptionScale]`（`0.5`–`3.0`），`GameSpeed` 裁剪在 `[MinGameSpeed, MaxGameSpeed]`（`0.25`–`2.0`），`ScreenShake` 裁剪在 `[0, 1]`。`NormalizeColorBlindMode` 同时接受 `"none"` 与 `"normal"`（统一规范为 `"none"`），以及 `"protanopia"`、`"deuteranopia"`、`"tritanopia"` 和 `"high-contrast"`（含 `"highcontrast"` / `"high_contrast"`）。
- **辅助字段与旧文件迁移**：提供 `Captions`、`SFXCaptions`、`ReduceFlashes`、`Invincibility`、`AutoAim` 与 `HoldToToggle` 强类型字段；加载包含旧版 `Custom["reduce_flashes"]` 的设置文件时会自动迁移到 `Accessibility.ReduceFlashes`。
- **跟随系统默认值**：未显式覆盖时，`EffectiveReduceMotion(env)`、`EffectiveHighContrast(env)` 与 `EffectiveCaptionScale(env)` 会跟随 `env.ReduceMotion()`、`env.IncreaseContrast()` 和 `env.FontScale()`；调用 `SetReduceMotion`、`SetHighContrast` 或 `SetCaptionScale` 可记录显式覆盖值（包括 `false` 或 `1.0`），调用对应的 `Clear*` 方法可恢复跟随系统。
## 控制与相机设置

`save.Settings` 包含经过验证的 `ControlSettings` 和 `CameraSettings`。旧设置文件加载时会为这些区块使用默认值。相机控制器可通过 `SettingsTargets.ApplyCamera` 使用视野、镜头摆动和运动模糊偏好。
