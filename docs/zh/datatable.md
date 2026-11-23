# datatable

`game/tooling/datatable` 把敌人、道具、关卡、任务等类型化游戏数据加载为你自己的 Go 结构体切片，并在游戏运行时、策划保存文件后热重载。数据表使用 YAML，策划和 AI 工具可以直接编辑，每处改动在评审中都是一行可读的差异；电子表格等工具导出的 CSV 和 JSON 也能加载。

```go
import "github.com/nsgo-dev/game/packages/game/tooling/datatable"
```

## 数据表与类型

行类型是一个结构体。每个字段是一列，列名取 `table` 标签，没有则取 `json` 标签，再没有则取 Go 字段名；标签为 `-` 的字段不参与。字段可以是字符串、布尔、有符号整数、无符号整数、浮点数、`time.Duration`、指向这些类型的指针（缺省或 `null` 时保持 nil）以及它们的切片。

```go
type Enemy struct {
	ID       string        `table:"id"`
	Name     string        `table:"name"`
	HP       int           `table:"hp"`
	Speed    float64       `table:"speed"`
	Boss     bool          `table:"boss"`
	Tags     []string      `table:"tags"`
	Cooldown time.Duration `table:"cooldown"`
}
```

YAML 文件是一个映射，包含 `version`（即 `datatable.FormatVersion`，值为 1）和 `rows` 列表。每一行是列名到值的映射；某行省略的列保持零值。

```yaml
version: 1
rows:
  - id: goblin
    name: Cave Goblin
    hp: 45
    speed: 3.5
    boss: false
    tags: [melee, scout]
    cooldown: 500ms
  - id: dragon
    name: Ancient Wyrm
    hp: 1200
    speed: 6.25
    boss: true
    tags: [flying, fire, boss]
    cooldown: 2s
```

`LoadFile[T](path)` 按扩展名（`.yaml`、`.yml`、`.csv` 或 `.json`）选择读取方式；`LoadYAML[T](name, data)` 读取已在内存中的字节，例如嵌入的文件。

```go
enemies, err := datatable.LoadFile[Enemy]("data/enemies.yaml")
```

## YAML 数据表拒绝的内容

YAML 数据表是严格的，拼写错误会在加载时报错，而不是悄悄丢掉一个值：

- 缺少 `version`、版本不是 1，或顶层出现 `version` 和 `rows` 以外的键；
- 没有 `version`/`rows` 映射、直接写成行列表的文件；
- 行中的键不对应任何列（`datatable.ErrUnknownField`），包括标签为 `-` 的字段；
- 同一行把同一列设置两次，例如同时写 `hp` 和 `HP`（`datatable.ErrDuplicateField`）；
- 字段类型容纳不了的值，例如 `speed: fast` 或给 `int8` 写 300。

行内的错误是 `*datatable.CellError`，带有文件、从 1 开始的行号、列名和值：

```go
var ce *datatable.CellError
if errors.As(err, &ce) {
	log.Printf("%s row %d, %s = %q: %v", ce.File, ce.Row, ce.Field, ce.Value, ce.Err)
}
```

列名先精确匹配，再忽略大小写匹配，所以 `Armor` 和 `armor` 都会填入名为 `Armor` 的字段。CSV 和 JSON 保持各自工具导出的形式：它们没有版本号，类型中不存在的列会被跳过。

## 保存数据表

`MarshalYAML(rows)` 和 `SaveYAML(path, rows)` 写出同一格式，供编辑器或生成数据的脚本使用。输出是确定的：先写 `version`，再按结构体声明顺序写出每一行的每一列，两空格缩进，列表写在一行内，字符串只在 YAML 需要时加引号，nil 指针写为 `null`，时长写为其 `String` 形式。`SaveYAML` 保存的文件经加载再保存后逐字节不变。nil 列表和空列表都写为 `[]`。非有限浮点数以及加载器无法读取的字段类型（map、结构体、接口）会报错。

```go
if err := datatable.SaveYAML("data/enemies.yaml", enemies); err != nil {
	return err
}
```

## 热重载

`NewTable[T](path)` 加载文件并保存其行。`Watch` 把它注册到 `hotreload.Watcher`（来自 `game/tooling/hotreload`），每次有效保存后在引擎 goroutine 上用新行回调：

```go
func Game(cx *core.Scope) core.Node {
	enemies, err := datatable.NewTable[Enemy]("data/enemies.yaml")
	if err != nil {
		panic(err)
	}
	w := hotreload.NewWatcher(cx, "data")
	enemies.WatchWithError(w, func(rows []Enemy, err error) {
		if err != nil {
			log.Printf("enemies.yaml: %v", err) // rows are still the last valid ones
			return
		}
		spawner.SetEnemies(rows)
	})
	// ...
}
```

加载失败的编辑（例如列名拼错）会保留之前的行，`Version` 不变，错误记录在 `LastError` 中；下一次有效保存会替换行并增加 `Version`。`Rows` 返回副本，调用方无法改动数据表内部的行。在发布构建（`-tags release`）中监视器不做任何事，数据表保留启动时加载的行。
