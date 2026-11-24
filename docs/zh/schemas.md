# YAML 文件的 Schema

游戏作者编写的每个文件都是 YAML 文本，每种格式都有一份 JSON Schema，编辑器和 AI 工具可借此在编写文件时补全键名、悬停显示字段说明并标出错误，而不必等到游戏加载时才发现。

```go
import "github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
```

## 已发布的 Schema

| 格式 | Schema | 文件 |
| --- | --- | --- |
| 场景，版本 1 | `https://game.nsgo.dev/schemas/scene.v1.json` | `*.scene.yaml`，由 `sceneformat.Parse` 读取 |
| 数据表 | 用 `datatable.Schema[T]` 按行类型生成 | `*.yaml`，由 `datatable.LoadYAML` 读取 |

其余格式在各自的 YAML 加载器落地时补上 Schema：模组清单（#474）、项目文件（#488）、导入旁注文件（#487）、共享资源（#485）、粒子（#477）、骨骼绑定（#475）和本地化表（#473）。

Schema 的 URL 带有格式版本号。写着 `version: 1` 的文件，在 `scene.v1.json` 旁发布版本 2 之后仍按 `scene.v1.json` 校验。

## 让编辑器使用 Schema

场景文件在第一行写明自己的 Schema，`sceneformat.Encode` 每次保存都会写入这一行：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v1.json
version: 1
name: Cave Room
```

YAML 语言服务器（`yaml-language-server`）会读取这条注释，VS Code 的 YAML 扩展（`redhat.vscode-yaml`）运行的就是它，配置了它的 Neovim、Helix 和 Zed 也一样，因此文件无需其他配置即可校验。`Parse` 会像对待其他注释一样忽略它。

没有这条注释的文件，可以在编辑器设置里把文件模式映射到 Schema，例如 VS Code 的 `.vscode/settings.json`：

```json
{
  "yaml.schemas": {
    "https://game.nsgo.dev/schemas/scene.v1.json": "*.scene.yaml"
  }
}
```

## Schema 检查什么

场景 Schema 由 `sceneformat.Parse` 解码所用的 Go 类型生成，键名、类型和说明都与代码一致：

- `version` 必填，且必须为 `1`。
- 每个对象都拒绝未定义的键，与 `Parse` 一致，拼错的键会在写错的位置被标出。
- 每个属性的说明就是它的 Go 文档注释。
- 每个数值都限制在其 Go 类型能容纳的范围内，例如 `float32` 坐标不超过约 3.4e38，`int` 类型的 `order` 不超出 64 位，因此 `Parse` 会溢出的值会被标出。
- 类型本身表达不了的约束来自字段上的 `schema` 结构体标签：必填键（`version`、图层的 `name`、对象的 `id`、精灵的 `atlas` 和 `frame`、相机边界的 `width` 和 `height`）、`space` 的可选值，以及数值范围，例如 `alpha` 在 [0, 1] 内、`zoom` 为正数。

Schema 一次只看一个文件，因此跨越整个文档的规则仍由 `Parse` 检查：图层名和对象 ID 唯一、瓦片地图的 `ref` 指向 `tilemaps` 中的条目、瓦片地图只用一种模式、内联网格的尺寸，以及 `core.ParseColor` 能解析的颜色。个别地方 Schema 比 `Parse` 更严格，例如 `Parse` 会把 `null` 读作字段的零值，而 Schema 会拒绝它；`Encode` 写出的每个场景都能通过 Schema 校验，由模糊测试保证。

## 在 CI 中检查 Schema

`cmd/gameschema` 把已发布的 Schema 写入 `apps/website/public/schemas`，网站在 `https://game.nsgo.dev/schemas/` 提供这些文件。修改某种格式的 Go 类型或文档注释后，在 `packages/game` 下重新生成：

```sh
go run ./cmd/gameschema
```

Check 工作流会运行 `go run ./cmd/gameschema -check`：提交的 Schema 与 Go 类型生成的结果不一致、缺少某个 Schema，或目录里有不再生成的 Schema 时都会失败。`cmd/gameschema` 中的一个测试在 `go tool nsgo test` 下做同样的检查。

## 数据表的 Schema

数据表的行是游戏自己的 Go 类型，因此由 `datatable.Schema[T]` 为其构建 Schema，游戏把它写在数据表旁边：

```go
docs, err := jsonschema.PackageDocs("internal/tables") // the doc comments of the package that declares Enemy
if err != nil {
	return err
}
s, err := datatable.Schema[tables.Enemy](jsonschema.Options{
	ID:    "https://example.com/schemas/enemies.json",
	Title: "Enemies",
	Docs:  docs,
})
if err != nil {
	return err
}
data, err := s.Marshal()
```

这份 Schema 描述 [YAML 数据表格式](datatable.md)：一个映射，必须有 `version` 且其值为 `1`，`rows` 是行列表，可以省略或写 `null` 表示空表；其他顶层键都会被标出。每行的键是 `LoadYAML` 匹配的列名：字段的 `table` 标签，其次是 `json` 标签，再次是 Go 字段名，拼写与 `MarshalYAML` 写出的一致；行的 Schema 位于 `#/definitions/<行类型>`。两个字段对应同一列的行类型会报错，与 `LoadYAML` 一致。`time.Duration` 单元格是 `"1.5s"` 这样的字符串或以纳秒计的整数。Schema 会标出不对应任何字段的列和类型不符的单元格；`LoadYAML` 还会拒绝合并键（`<<`），而 YAML 语言服务器会在校验前先展开合并键，因此 Schema 无法标出它们。

## 在 Go 中构建和检查 Schema

- `jsonschema.Reflect(t, opts)` 从 Go 类型构建 draft-07 Schema：结构体字段成为属性，按 `yaml` 标签（或 `Options.FieldName`）命名；具名结构体成为 `definitions`，通过 `$ref` 共享；整数和 `float32` 以其 Go 类型的取值范围为界；除非设置 `Options.AllowUnknown`，否则拒绝未知键。两个字段对应同一个键时报错，除非设置 `Options.FirstFieldWins` 保留第一个。字段的 `schema` 标签接受 `required`、`const=V`、`enum=A|B`、`minimum=N`、`maximum=N`、`exclusiveMinimum=N`、`exclusiveMaximum=N`、`minLength=N` 和 `minItems=N`。
- `jsonschema.PackageDocs(dir)` 把一个包的文档注释读入 `Options.Docs`，每个属性都以对应字段的注释作说明。
- `Schema.Marshal()` 以固定的键顺序写出缩进的 JSON，相同的类型总是得到相同的字节。
- `Schema.Validate(v)` 和 `Schema.ValidateYAML(data)` 按 `Reflect` 写出的关键字检查值，返回 `*ValidationError`，列出每个问题及其 JSON Pointer 路径，例如 `/layers/0/objects/1/alpha: 1.5 is greater than the maximum 1`，或 `/layers/0/nmae: unknown key "nmae" (known keys: name, space, …)`。`ValidateYAML` 按语言服务器的方式读取 YAML：键是字符串，时间戳是字符串，合并键会被合并，同一映射中重复的键会报错，别名会在上限内展开。
