# undo

`game/undo` 为解谜和回合制游戏记录可撤销的操作，支持撤销与重做，并为游戏内编辑器提供操作上限、合并和保存标记。

```go
import "github.com/nsgo-dev/game/pkg/undo"
```

`undo` 中没有任何代码读取时钟、启动 goroutine 或保存包级状态。

## 操作

`Move[S]` 是对游戏状态 `S`（通常是指向棋盘的指针）的一次可撤销修改：

```go
type Move[S any] interface {
	Apply(state S)
	Revert(state S)
}
```

`Apply(s)` 之后再 `Revert(s)` 必须让 `s` 与之前完全相同。操作可以把 `Apply` 改动了什么记在自己的字段里（指针接收者的操作），在 `Revert` 时读回，例如这一步是否推动了箱子：

```go
type Push struct {
	Dir string `yaml:"dir"`

	moved, pushed bool // 由 Apply 记录，供 Revert 读取
}

func (p *Push) Apply(b *Board) {
	p.moved, p.pushed = b.Step(p.Dir)
}

func (p *Push) Revert(b *Board) {
	b.Unstep(p.Dir, p.moved, p.pushed)
}
```

两个辅助类型无需单独定义类型即可构造操作：

- `Func[S]{Do: ..., Undo: ...}` 是由两个函数组成的操作。
- `Group[S]{a, b, c}` 把多个操作作为一个整体撤销和重做，例如一个回合里既移动棋子又吃掉另一个棋子。它按顺序应用，按相反顺序撤销。nil 或空的组是不做任何修改的操作；组里含有 nil 操作时，`Do` 在应用任何操作之前就会 panic。

## History

`New[S, M](state)` 返回一个以游戏初始状态为起点的空 `History`。`M` 是操作的类型：多种操作时用 `Move[S]`，只有一种具体类型（如 `*Push`）时用该类型，这样历史还可以保存。

```go
board := level.Board()
h := undo.New[*Board, *Push](board)

h.Do(&Push{Dir: "left"}) // 应用操作并记录
h.Undo()                 // 撤销最近应用的操作
h.Redo()                 // 重新应用最近撤销的操作
h.Seek(0)                // 回到初始状态
```

- `Do(m)` 应用 `m` 并记录它。撤销之后做出的新操作会丢弃原本可以重做的操作。
- `Undo` 和 `Redo` 报告是否有可撤销或可重做的操作，`CanUndo` 和 `CanRedo` 可以事先判断，供游戏的按钮使用。
- `Seek(n)` 撤销或重做操作，直到恰好应用了 `n` 个：`Seek(0)` 重新开始关卡，`Seek(h.Len())` 重做全部操作。
- `Len` 是记录的操作数，`Applied` 是已应用的操作数，`Moves` 按顺序返回它们的副本。
- `Clear` 忘掉所有操作并保留当前状态，当前状态成为新的初始状态。`Clear` 之后保存的日志只含之后的操作，所以要把 `Clear` 时的状态一起保存，并恢复到那个状态上。
- 设置了 `OnChange` 时，每次应用、撤销或忘掉操作的调用之后都会调用它。

历史默认没有上限：在 `Clear` 之前每个操作都会保留。由于每个操作都撤销自己的修改，撤销全部操作会让状态精确回到初始状态，重做全部操作则精确回到最新状态。包内测试在数百步的随机行走上验证这一点，并对任意的 `Do`、`Undo`、`Redo`、`Seek`、`Clear` 序列做模糊测试，与每一步之后状态的副本逐一比对。

操作的方法不能调用持有它的 `History`，`History` 也不能并发使用。

## 游戏内编辑器

关卡编辑器、基地建造或角色创建器会连续编辑很长时间，三件工具让这种编辑保持可用。

`Limit` 为正数时限制保留的操作数：记录新操作超出上限时，最旧的已应用操作被遗忘。被遗忘操作的修改留在状态中，但它不能再被撤销，`Seek` 和保存的日志都从保留的最早操作开始计数。

`DoMerge` 是面向一连串同类小修改的 `Do`，例如拖动滑块或物体的每一步：应用操作后，如果最近应用的操作是 `Merger` 且其 `Merge` 接受，就把它折叠进去，于是整次拖动只需撤销一次。`EndMerge` 结束这一轮合并，下一次拖动同一个滑块也会开始新操作：

```go
type Slide struct {
	To  int `yaml:"to"`
	old int // 由 Apply 记录
}

func (s *Slide) Apply(v *int)  { s.old = *v; *v = s.To }
func (s *Slide) Revert(v *int) { *v = s.old }

// Merge 把拖动的下一步折叠进这一步：保留旧值，采用新目标值。
func (s *Slide) Merge(next *Slide) bool { s.To = next.To; return true }
```

`MarkSaved` 记录当前位置，`Dirty` 报告状态是否与保存时不同，用于"有未保存修改"的标记。`Dirty` 比较的是位置，所以撤销回保存点时报告干净；一旦 `Limit` 把保存点之前的操作遗忘，或新操作丢弃了保存点所在的分支，历史在下次 `MarkSaved` 之前一直保持脏。`MarkSaved` 同时结束合并，这样保存后的第一次 `DoMerge` 不会改写保存点所依赖的操作。`Version` 标识当前位置，每次 `Do`、`Undo`、`Redo`、`Seek` 和 `Clear` 都会改变；折叠进最近操作的 `DoMerge` 不改变它。

## 保存历史

操作只有一种具体类型的历史可以保存为 `Log` 并恢复，这样玩家在关卡中途退出、重新载入后仍然可以撤销。日志是 YAML 文本：

```yaml
version: 2
applied: 2
forgotten: 0
moves:
  - dir: right
  - dir: down
  - dir: down
```

- `version` 是 `LogVersion`（2）；`applied` 是 `moves` 中已应用的个数，其余是 `Redo` 可以恢复的操作。`forgotten` 是 `Limit` 在保留的最早操作之前遗忘的操作数。`Limit` 出现之前的版本 1 日志仍可读取，再次保存时写成当前格式。
- `h.Log()` 返回日志，`EncodeLog` 按上面的字段顺序以 2 空格缩进写出，同一份历史总是写出相同的文本。
- `DecodeLog[M](data)` 读回日志，拒绝缺少 `version`、`applied` 或 `moves` 键、`moves` 为 null、未知字段（日志或操作中的）、更新的版本、超出操作数的 `applied`、负的 `forgotten`、空操作以及多余的 YAML 文档。
- `Restore(start, log)` 返回以日志记录时的初始状态为起点的 `History`，即传给 `New` 的状态，或在 `Clear` 之后为 `Clear` 时的状态：按顺序应用前 `applied` 个操作，其余留给 `Redo`。它拒绝有遗忘操作的日志，因为初始状态缺少这些操作的修改：先 `Clear` 历史，把 `Clear` 时的状态和日志一起保存，下一份日志就没有遗忘的操作。

操作在 `Apply` 时记录的内容要放在未导出字段或标记为 `yaml:"-"` 的字段里，这样日志只保存玩家的选择；`Restore` 会重新应用这些操作，重新记录它们。把日志和游戏存档放在一起，例如放进 [save](save.md) 存档槽或单独的文件。

```go
data, err := undo.EncodeLog(h.Log())
// ……之后，用历史开始时的棋盘（关卡的初始棋盘，或上次 Clear 时保存的棋盘）：
log, err := undo.DecodeLog[*Push](data)
h, err = undo.Restore(level.Board(), log)
```
