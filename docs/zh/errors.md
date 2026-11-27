# 可据以行动的错误

本模块中每个加载器和校验器在文件无法使用时，都返回同一种错误类型：`github.com/nsgo-dev/game/pkg/loaderr` 中的 `*loaderr.Error`。它给出文件、行号与列号、字段、出了什么问题、期望的是什么以及如何修复，这样人或读测试、构建日志的 Agent 不必阅读加载器的源码，就能打开文件跳到那一行改正它。

```text
levels/cave.scene.yaml:14:7: layers[1].space: layer "hud" has invalid space "iso"; expected "world" or "screen"; fix: set space to world or screen, or remove it for world
```

消息的格式是 `file:line:column: field: problem; expected E; fix: F`，未知的部分会省略。编辑器和终端会把开头的 `file:line:column` 变成可点击的链接。

## 字段

| 字段 | 含义 |
| --- | --- |
| `File` | 文件的路径或名称。读取路径的加载器会填写它；接收字节的加载器留给调用方填写（见下文）。 |
| `Line`、`Column` | 问题所在位置，从 1 开始；问题不在某一处时为 0，例如文件无法读取。 |
| `Field` | 在文档中的位置，写成路径：`layers[2].objects[0].id`，键中含点时写作 `tilemaps["a.b"].ref`。针对整个文档时为空。 |
| `Problem` | 出了什么问题。 |
| `Expected` | 加载器在此处期望的内容，问题描述中已说明时为空。 |
| `Fix` | 怎样修改才能加载文件。加载器返回的每个错误都有它。 |
| `Err` | 底层错误：`errors.Is` 和 `errors.As` 能穿过 `Error`，所以 `save.ErrCorrupt`、`modding.ErrInvalidManifest` 等哨兵错误照样能匹配。 |

`loaderr.As(err)` 返回错误链中的 `*Error`。

## 给出文件名

读取路径的加载器，例如 `particles.LoadConfigFile`、`gltf.LoadFS`、`replay.LoadFile`、`datatable.LoadFile` 和 `editor.OpenFile`，会在错误中给出该路径。接收字节的加载器，例如 `sceneformat.Parse` 和 `tilemap.LoadTiled`，知道行号却不知道文件名，会在文件名处写 `<input>`；由读取文件的代码补上文件名：

```go
data, err := fs.ReadFile(assets, "levels/cave.scene.yaml")
if err != nil {
	return err
}
doc, err := sceneformat.Parse(data)
if err != nil {
	return loaderr.WithFile(err, "levels/cave.scene.yaml")
}
```

`WithFile` 只给本身就是 `*loaderr.Error` 的错误补上文件名，各加载器的入口返回的正是这种错误。包裹了它的错误，例如 `fmt.Errorf("level 3: %w", err)`，会原样返回，使其上下文和哨兵错误仍能匹配；请在包裹之前调用 `WithFile`。

保存在 `core.Storage` 中的值，例如输入绑定、调试参数和存档槽位，以其存储键作为文件名。`FileBackend` 中的存档槽位给出其文件，即 `<dir>/<slot>.json`，备份副本为 `<dir>/<slot>.json.bak`。

## 各加载器报告的内容

| 包 | 加载器 | 行号 |
| --- | --- | --- |
| `sceneformat` | `Parse`、`ParseWith` | 解码错误和每条模式规则，按字段；`Validate` 返回的 `*FieldError` 仍在错误链中，`ParseWith` 给出 `ParseOptions.File` |
| `particles` | `UnmarshalConfigYAML`、`UnmarshalConfigJSON`、`LoadConfigFile`、`LoadConfigFS`、`HotConfig` | 解码错误、时长、颜色和枚举值，按字段或按值所在的行 |
| `skeletal` | `Load` | 骨骼、插槽、皮肤和关键帧，按字段 |
| `anim3d` | `LoadEventsSidecar`、`LoadEventsFromGLTFExtras` | 解码错误 |
| `sprite` | `LoadAtlas` | 帧、帧标签和九宫格切片，按字段 |
| `tilemap` | `LoadTiled`、`LoadLDtk`、`LoadLDtkAll` | 地图、图块集（外部图块集给出它自己的文件）、图层、对象和属性，按字段 |
| `l10n` | `Catalog.LoadJSON`、`ParseAssetManifest`、`AssetCatalog.LoadAssetManifestYAML` | 键、资源变体和资源组，按字段 |
| `input` | `Actions.Load`、`Haptics.Load` | 存储的绑定，按字段，以存储键为文件名 |
| `modding` | `ParseManifest`、`ModManifest.Validate`、`Manager.Discover`、`Manager.DiscoverFS`、`Manager.Reload` | 清单的每个字段、模组不支持的游戏版本和重复的 id，给出 `<mod>/mod.yaml` |
| `replay` | `Decode`、`LoadFile` | 头部和各步，按字段，例如 `steps[3].press[0]` |
| `save` | `Store.Load`、`Store.LoadChecked`、`SettingsStore.Load` | 外层封装、数据和检查，给出副本的文件或存储键（自定义 `Backend` 的副本以槽位名表示，备份副本为 `slot (backup)`） |
| `entities` | `LoadWorld`、`World.Restore` | 实体索引、父实体和组件，按字段 |
| `assets` | `ParsePackManifest`、`Manager.LoadAtlas`、`Group` 中的图集 | 解码错误；图集给出其 URI |
| `world3d` | `LoadMeshLibraryFromGLTF` | 版本、节点网格和访问器，按字段 |
| `gltf` | `Load`、`LoadFS` | 出错的顶层条目，例如 `nodes[3]` |
| `text` | `LoadBMFont`、`LoadBMFontImage` | 出错的记录，文本和 XML 字体均可 |
| `flow` | `ParseYarn` | 出错的脚本行 |
| `datatable` | `LoadCSV`、`LoadJSON`、`LoadYAML`、`LoadFile` | 出错的单元格；`CellError` 仍在错误链中 |
| `tooling/devstate` | `Store.LoadSnapshot` | 快照文件中的解码错误 |
| `debug` | `Console.LoadTunables` | 出错的调试参数，以存储键为文件名 |
| `tooling/bench`、`cmd/sizecheck` | `bench.LoadFile`、`LoadBudget`、`LoadMobileBudget` | 解码错误 |

无法读取的文件没有行号：它的错误给出文件名，把读取错误保留在 `Err` 中，同样给出修复方法。

## 编写加载器

1. 用 `loaderr.Errorf(field, fix, format, args...)` 校验解码后的文档，用 `loaderr.Path("layers", i, "objects", j)` 构造 `field`，或用 `loaderr.Join(at, "alpha")` 在已有路径上延伸。校验器不知道行号，所以给出字段。
2. 在加载器的入口处，让每个错误都经过一次 `loaderr.Locate(err, file, data, fix)`（只需 JSON 而不需要 YAML 支持的加载器可使用 `loaderr.LocateJSON(err, file, data, fix)`）。它在 YAML 或 JSON 文本中找到字段所在的行和列，把 `encoding/json` 和 YAML 的解码错误（语法错误、未知字段、类型不符、重复键、提前结束）转换为带行号和修复方法的 `Error`，填入文件名，并给没有修复方法的错误补上加载器的 `fix`。
3. 对既非 YAML 也非 JSON 的按行格式，例如 BMFont 文本和 Yarn，直接设置 `Line`。
4. 把修复方法写成指令：写什么、写在哪里，或者用哪个工具重新导出。“把 space 设为 world 或 screen”有用；“值无效”没用。
5. 按排序后的键遍历映射，这样文件中有多处问题时，每次运行报告的都是同一处。
6. 用 `internal/loaderrtest` 测试：`Expect(t, err, file, line)` 检查错误给出了文件和行号且有修复方法，`ExpectField(t, err, file, data, field)` 还会检查字段。

自定义的 `UnmarshalYAML` 知道它收到的节点：返回带有该节点 `Line` 和 `Column` 的 `*loaderr.Error`，就像 `particles` 对时长和颜色所做的那样，`Locate` 会保留它们。自定义的 `UnmarshalJSON` 无法说明在哪里失败：`nsgo build` 链接的 `encoding/json` 返回它的错误时不带字段路径，所以应保留它拒绝的值，再由校验器带着字段报告。
