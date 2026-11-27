# cards

`game/cards` 在游戏视图中把卡牌在牌桌的槽位之间拖放，并把它们排成扇形手牌、牌堆、叠放牌列和网格。卡牌放到接受它的槽位上时会吸附到位，放到其他任何地方时会飞回原处。每次移动都是在 `game.Loop` 固定步长上推进的 `game/tween` 补间，鼠标和触摸走同一条路径。

```go
import "github.com/nsgo-dev/game/pkg/cards"
```

## 牌桌、槽位与卡牌

`Board` 按添加顺序保存 `Slot`（后添加的在上层），每个槽位保存一个有序的 `Card` 列表，第一张在最底下。槽位的 `Rect` 既是其 `Layout` 摆放卡牌的区域，也是指针放下卡牌的区域。`Board` 的零值可直接使用。

```go
board := &cards.Board{CardSize: math2d.V2(63, 88), Curve: core.EaseOut}

hand := &cards.Slot{ID: "hand", Rect: math2d.Rect{X: 40, Y: 400, W: 560, H: 120},
	Layout: cards.Fan{Arc: 24, Sag: 14}}
play := &cards.Slot{ID: "play", Rect: math2d.Rect{X: 40, Y: 160, W: 560, H: 200},
	Layout: cards.Grid{Gap: math2d.V2(8, 8)}, Accepts: []string{"spell"}, Capacity: 5}
deck := &cards.Slot{ID: "deck", Rect: math2d.Rect{X: 620, Y: 400, W: 63, H: 88},
	Layout: cards.Stack{Offset: math2d.V2(0, -0.5), Depth: 8}, Grab: cards.GrabTop}
for _, s := range []*cards.Slot{hand, play, deck} {
	board.AddSlot(s)
}

for _, c := range opening {
	board.Add(&cards.Card{ID: c.Name, Tags: []string{c.Kind}, Data: c}, hand, -1)
}
```

- `Add` 立即放置卡牌，用于开局布置；`Move` 把卡牌补间到另一个槽位，用于发牌或撤销；`Remove` 移除卡牌并合拢空位。三者都不检查槽位规则。
- `Card.Pose()` 是卡牌当前的绘制位置：中心点 `Pos` 和以弧度表示的旋转 `Rot`。`Slot.Cards()`、`Slot.Top()`、`Card.Slot()` 和 `Card.Index()` 给出它所属的位置。
- `Card.Locked` 让卡牌不能被拿起，`Slot.Grab` 规定指针可以拿起槽位中的哪些卡牌：`GrabAny`（手牌）、`GrabTop`（牌库或弃牌堆）或 `GrabNone`。

## 布局

| 布局 | 摆放方式 | 字段 |
| --- | --- | --- |
| `Fan` | 手牌在槽位中居中排成一行，越往两端越倾斜、越低 | 中心间距 `Spacing`（0 为卡牌宽度的 60%），满宽手牌的总角度 `Arc`（度），两端下沉的 `Sag`（点） |
| `Stack` | 从槽位左上角叠放的牌库或牌列 | 每张相对前一张的 `Offset`，限制可见层数的 `Depth` |
| `Grid` | 从槽位左上角开始逐行排列 | 列数 `Columns`（0 按槽位宽度自动适配），卡牌间距 `Gap` |

手牌放不下时扇形会收拢间距；每张牌按离中间的距离倾斜，所以牌少时几乎是平的，满宽时两端达到 `±Arc/2`。布局还决定放下的卡牌插到哪里：扇形插到离卡牌中心最近的位置，因此在手牌内拖动就是重新排序；网格插到卡牌下方的格子；叠放总是放到最上面。游戏也可以实现自己的 `Layout`（`Arrange` 和 `InsertIndex`）。

修改槽位的 `Rect` 或 `Layout` 后，调用 `board.Relayout(slot)` 把其中的卡牌补间到新位置。

## 用鼠标和触摸拖动

NSGo 在桌面平台上把鼠标主键报告为 `core.TouchEvent` 的 0 号手指，在 Web 上把每个指针都报告为手指，所以同一个处理函数同时处理鼠标和触摸。把牌桌挂到包含游戏 `Canvas` 的视图上，并在循环中推进它：

```go
view := core.View{Children: []core.Node{core.Canvas{Draw: draw}}}
board.Attach(&view) // 会串联视图已有的 OnTouch

loop.Update = func(dt time.Duration) { board.Update(dt) }
```

触摸坐标是视图坐标；牌桌经由相机或缩放视口绘制时，设置 `board.MapPoint` 做转换，对视口外的点返回 false。用其他方式读取指针的游戏可以用自己的指针 ID 调用 `PointerDown`、`PointerMove`、`PointerUp` 和 `PointerCancel`。

- 按下会拿起指针下最上层的卡牌，命中检测会考虑卡牌的旋转。指针移动超过 `DragSlop`（默认 6 点）后卡牌开始跟随，保持被拿住的位置并转正；在此之前松开算作点按（`OnTap`）。
- 每根手指拖动各自的卡牌，所以可以同时拖两张；已被一个指针按住的卡牌不会被另一个指针拿走。
- 松开时，卡牌落在指针下最上层的槽位；指针不在任何槽位上时，落在与卡牌重叠面积最大的槽位。`CanDrop` 允许时卡牌在 `SnapDuration` 内吸附到位：原槽位总是允许；其他槽位要求 `Capacity` 未满，设置了 `Accepts` 时卡牌须带其中一个标签，并且设置了 `Slot.Accept` 和 `Board.Accept` 时须经它们同意。否则，或松开时不在任何槽位上，或触摸被系统取消，卡牌在 `ReturnDuration` 内飞回原位。
- `OnPickUp`、`OnDrop`（带有移动前后的槽位和序号）和 `OnReturn`（带有拒绝它的槽位，如果有）报告每一步；拖动中 `DropTarget(card)` 返回卡牌下方的槽位以及它是否会接受，用于高亮提示。

```go
board.Accept = func(c *cards.Card, from, to *cards.Slot) bool {
	return to != play || mana >= c.Data.(Spell).Cost
}
board.OnDrop = func(e cards.DropEvent) {
	if e.To == play {
		cast(e.Card.Data.(Spell))
	}
}
```

## 绘制

`DrawOrder(dst)` 按从下到上的顺序列出卡牌：先按槽位顺序列出各槽位的卡牌，再按被拿起的先后列出正在拖动或飞行中的卡牌。按各自的姿态绘制：

```go
order = board.DrawOrder(order[:0])
for _, c := range order {
	p := c.Pose()
	d.Save()
	d.Translate(p.Pos.X, p.Pos.Y)
	d.Rotate(p.Rot)
	drawCard(d, c, board.Size())
	d.Restore()
}
```

牌桌内部切片扩容完成后，`DrawOrder`、`Update` 以及拖动过程中的 `PointerMove` 不再分配内存。拿起一张旋转的牌、放下一张牌或把牌送回原处时，每张开始移动的牌都会分配一个补间。

## 牌桌文件

牌桌的卡牌尺寸和槽位可以写成 YAML，用 `ParseBoardDoc` 和 `LoadBoard` 加载。未知字段会被拒绝，版本必须为 `1`，`EncodeBoardDoc` 按固定顺序、以块样式写出字段（如下例），所以这种形式的文件读入再写出后不会改变；手写的流样式文件（`rect: {x: 40, ...}`）第一次保存时会变成块样式。`board.Doc()` 把代码中构建的牌桌转回 `BoardDoc`；槽位的 `Accept` 函数以及牌桌的回调和时长留在代码中。

```yaml
version: 1
card_size:
  w: 63
  h: 88
slots:
  - id: hand
    rect:
      x: 40
      "y": 400
      w: 560
      h: 120
    layout:
      fan:
        arc: 24
        sag: 14
  - id: deck
    rect:
      x: 620
      "y": 400
      w: 63
      h: 88
    layout:
      stack:
        offset:
          x: 0
          "y": -0.5
        depth: 8
    accepts:
      - spell
    capacity: 40
    grab: top
```

每个槽位只能指定 `fan`、`stack`、`grid` 中的一个；`grab` 为 `top` 或 `none`，默认的任意牌可拿则省略该键；`grab: any` 会被拒绝，使默认值只有一种写法。编码器会给 `y` 键加引号，因为 YAML 1.1 会把它读作布尔值，这样其他工具读到的内容一致。
