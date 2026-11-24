# YAML 文件的 Schema

游戏作者编写的每个文件都是 YAML 文本，每种格式都有一份 JSON Schema，编辑器和 AI 工具可借此在编写文件时补全键名、悬停显示字段说明并标出错误，而不必等到游戏加载时才发现。

```go
import "github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
```

## 已发布的 Schema

| 格式 | Schema | 文件 |
| --- | --- | --- |
| 场景，版本 1 | `https://game.nsgo.dev/schemas/scene.v1.json` | `*.scene.yaml`，由 `sceneformat.Parse` 读取 |
| 场景，版本 2 | `https://game.nsgo.dev/schemas/scene.v2.json` | 实例化或继承其他场景文件的 `*.scene.yaml`，由 `sceneformat.Parse` 读取 |
| 导入设置，版本 1 | `https://game.nsgo.dev/schemas/import.v1.json` | 资源旁的 `*.import.yaml`，由 `importsettings.Parse` 和 `game assets` 读取，其中的署名由 `game release` 和 `game credits` 读取（见[命令行](cli.md#assets)） |
| 数据表 | 用 `datatable.Schema[T]` 按行类型生成 | `*.yaml`，由 `datatable.LoadYAML` 读取 |

其余格式在各自的 YAML 加载器落地时补上 Schema：模组清单（#474）、项目文件（#488）、共享资源（#485）、粒子（#477）、骨骼绑定（#475）和本地化表（#473）。

Schema 的 URL 带有格式版本号。`scene.v2.json` 已在 `scene.v1.json` 旁发布，写着 `version: 1` 的文件仍按 `scene.v1.json` 校验，它拒绝版本 2 新增的键；`Encode` 写入的是文档自身版本的 Schema。

## 让编辑器使用 Schema

场景文件在第一行写明自己的 Schema，`sceneformat.Encode` 每次保存都会写入这一行：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v1.json
version: 1
name: Cave Room
```

YAML 语言服务器（`yaml-language-server`）会读取这条注释，VS Code 的 YAML 扩展（`redhat.vscode-yaml`）运行的就是它，配置了它的 Neovim、Helix 和 Zed 也一样，因此文件无需其他配置即可校验。`Parse` 会像对待其他注释一样忽略它。`importsettings.Encode` 也会在导入设置附属文件的开头写一行同样的注释，指向 `import.v1.json`。

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

导入设置的 Schema 同样只逐个检查字段：已知的键、允许的 `filter`、`format` 和音频 `format` 取值，不为负的尺寸和采样率，以及署名必填的 `author` 和 `license`。它不描述字段之间的组合规则，因为 Schema 生成器不输出 `oneOf`、`not` 或依赖关键字，而循环的终点与起点的大小关系也无法用 JSON Schema 表达；因此没有任何部分或同时含有 `image` 和 `audio` 的附属文件、同时设置 `peakDB` 和 `rmsDB`、循环终点不在起点之后、输出为 `aac` 或 `mp3` 却带循环、以及 `license` 不是 SPDX 许可证表达式的附属文件，仍由 `importsettings.Parse` 和 `game assets` 拒绝，而编辑器会把这样的文件显示为有效。

## 在 CI 中检查 Schema

`cmd/gameschema` 把已发布的 Schema 写入 `apps/website/public/schemas`，网站在 `https://game.nsgo.dev/schemas/` 提供这些文件。修改某种格式的 Go 类型或文档注释后，在 `packages/game` 下重新生成：

```sh
go run ./cmd/gameschema
```

Check 工作流会运行 `go run ./cmd/gameschema -check`：提交的 Schema 与 Go 类型生成的结果不一致、缺少某个 Schema，或目录里有不再生成的 Schema 时都会失败。`cmd/gameschema` 中的一个测试在 `go tool nsgo test` 下做同样的检查。

## 在 CI 中检查格式

Schema 检查的是有人编写的文件，格式自身的测试检查的是读写它的代码。每个读取 YAML 格式的包都在 `conformance_test.go` 中对规范形式（即编码器写出的字节）的 testdata 文件运行 `internal/formattest` 提供的共享检查：

```go
func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*quest.Book]{
		Decode: quest.Parse,
		Encode: quest.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "quest",
	})
}
```

对每个文件，这些检查要求：

- 加载后再保存，得到的字节与原文件相同；
- 再次保存同一个值、保存对该文件的第二次加载、保存对已保存输出的加载，得到的字节都相同，因此输出不依赖 map 顺序、时间或随机数；
- 加载时拒绝多出一个顶层键、缺少 `version`、`version: 0`，以及比格式能读取的最新版本还新一版的文件，且错误信息中写出该键名。

`Doc` 指明该格式在[文件格式](formats.md)页中的条目，即 `docs/en/formats.md` 和 `docs/zh/formats.md` 中条目标题前的 `<!-- format: quest -->` 一行；任一页缺少该条目，或条目中的 YAML 示例加载再保存后字节改变时，`Run` 都会失败，并打印格式为该示例保存出的内容。可选的 `#名称` 之前的路径必须是该包的路径，有两种格式的包为第二种命名，如 `achievements#progress`。`packages/game` 中的 `TestFormatsPage` 检查该页的其余部分：两种语言列出相同的条目且顺序一致；不是任何 `Doc` 的条目都登记在它的 `codeFormats` 列表中，并写明读取该格式的声明，该包必须仍声明它，且条目中的 JSON 或 Yarn 示例能被这个读取函数加载；每个解码 JSON 的包都有条目，除非 `notJSONFormats` 写明了理由；每个名称以 `Version` 结尾的导出整数常量都在它的 `formatVersions` 列表中对应一个条目。`Doc` 可以是字符串字面量或字符串常量，以便该测试读取。

`packages/game` 中的 `TestFormatPackagesRunConformance` 会在某个包用 `go.yaml.in/yaml/v3` 解码 YAML（无论是否宽松）、却没有任何测试调用 `formattest.Run` 时失败，因此跳过检查的新格式会让 CI 中的 `go tool nsgo test` 失败。解码 YAML 但不定义自己格式的包（例如 `loaderr`）列在该测试的 `notFormats` 中并写明理由。该测试以包为单位检查，调用一次即可满足，所以包含第二种格式的包必须自己为它也运行这些检查。其他测试依赖其注释或排版的手写 fixture 保持原样，它的规范形式另存为单独的文件（例如放在 `testdata/canonical/` 下）供这些检查使用。版本号放在其他键下的格式（例如 mod 清单的 `manifestVersion`）设置 `VersionField`，能读取多个版本的格式设置 `MaxVersion`。新增 testdata 文件时，先写好文档，再用 `FORMATTEST_UPDATE=1` 运行一次测试：它会在检查前用格式为每个文件保存出的内容重写该文件，编码器不会写出的注释会被丢弃。

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
