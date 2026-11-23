# save

`game/save` 将游戏存档槽以带版本号的 JSON 信封存储在 `core.Storage` 或本地目录（如 `cx.DataDir()`）中，支持逐版本数据结构迁移与原子备份恢复。

```go
import "github.com/nsgo-dev/game/packages/game/save"
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

使用 `store.Exists(slot)`、`store.Slots()`（返回有序槽位名列表）和 `store.Delete(slot)`（同时删除主存档与备份，删除不存在的槽位返回 `nil`）管理存档槽。来自游戏外部的槽位名（例如云端列表）可用 `save.ValidateSlot(name)` 按同样的规则检查后再交给 `Backend`。

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

`Load` 读取到旧版本存档时，会按 `1 -> 2 -> 3 ...` 逐步迁移 JSON，再反序列化到 `dst`。

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

- **`FileBackend`**：写入同目录临时文件并同步落盘，将原有 `<slot>.json` 保存到 `<slot>.json.bak`，再原子重命名临时文件。
- **`StorageBackend`**：先写入备份键再更新主键，即便遇到 `*core.StorageFullError` 也能保住上一次完好存档。

调用 `Load` 时，若主存档缺失、不可读或损坏，会自动回退到备份恢复。但若主存档本身是合法信封且版本号高于 `Store.Version`，则会直接返回 `ErrFutureVersion`，不会静默回退到旧版本备份。
