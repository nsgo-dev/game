# 教程 3：输入

让玩家通过触摸或点击场地的一侧，或者用方向键，把篮子在通道之间移动。输入经由 `game/input` 的命名动作，每一步采样一次。

完成后的代码在 [`examples/tutorial/ch03-input`](../../examples/tutorial/ch03-input/)。


**本章修改的文件：** [`main.go`](../../examples/tutorial/ch03-input/main.go)、[`game.go`](../../examples/tutorial/ch03-input/game.go)、[`world.go`](../../examples/tutorial/ch03-input/world.go)、[`game_test.go`](../../examples/tutorial/ch03-input/game_test.go)。链接的章节目录包含每个完整文件。

## 世界里的移动

世界把玩家的移动作为 `step` 的参数：-1 表示向左一条通道，1 表示向右一条通道，0 表示不动。篮子朝它的通道前进，以 `basketSpeed` 滑过去：

```go
func (w *world) step(dt time.Duration, move int) {
	secs := float32(dt.Seconds())
	w.basketLane = min(max(w.basketLane+move, 0), laneCount-1)
	w.prevX = w.basketX
	target := laneX(w.basketLane)
	if d := target - w.basketX; d > 0 {
		w.basketX = min(w.basketX+basketSpeed*secs, target)
	} else if d < 0 {
		w.basketX = max(w.basketX-basketSpeed*secs, target)
	}
	// The star falls as before…
}
```

世界仍然对触摸和按键一无所知，所以测试通过传入移动来控制篮子，[第 9 章](tutorial-09-ship.md)里的机器人玩家也是这样。

## 来自触摸和点击的动作

`input.Actions` 把 `"left"` 这样的名字映射到会按下它的触摸和鼠标按键。Star Catcher 绑定了场地的左右两半：

```go
var (
	leftHalf  = core.Bounds{Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
	rightHalf = core.Bounds{Origin: core.Point{X: virtualWidth / 2}, Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
)

s.actions.MapPoint = func(p core.Point) (core.Point, bool) { return viewport.ViewToGame(s.size, p) }
s.actions.Bind("left", input.TouchRegion(leftHalf), input.MouseRegion(leftHalf, core.MouseButtonLeft))
s.actions.Bind("right", input.TouchRegion(rightHalf), input.MouseRegion(rightHalf, core.MouseButtonLeft))
```

- 区域使用虚拟坐标，在每块屏幕上都一样。`MapPoint` 借助视口把触摸位置从视图坐标转换过来；落在边条上的触摸返回 `false`，不会按下任何动作。
- `s.size` 是画布的尺寸，由 `draw` 每帧记录。
- `Attach` 把动作接到视图的触摸、按压和鼠标处理函数上：

```go
v := core.View{TestID: "game-view", /* … */}
s.actions.Attach(&v)
```

## 按键

硬件键盘通过作用域上的按键绑定到达游戏。每个绑定触发一个动作，动作会被锁存到下一步，所以按键和触摸移动篮子的方式完全相同：

```go
repeat := core.KeyOptions{Repeat: true}
for _, key := range []string{"left", "a"} {
	cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("left") })
}
for _, key := range []string{"right", "d"} {
	cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("right") })
}
```

`Repeat` 让绑定在键盘自动重复时再次触发，所以按住方向键，篮子会一路扫过各条通道。

## 每一步采样一次

`Step` 在每次更新开始时给动作拍一张快照，`JustPressed` 报告自上一步以来新按下的动作。每次新的按下让篮子移动一条通道：

```go
func (s *starCatcher) update(dt time.Duration) {
	s.actions.Step()
	move := 0
	if s.actions.JustPressed("left") {
		move--
	}
	if s.actions.JustPressed("right") {
		move++
	}
	s.world.step(dt, move)
}
```

在步进里而不是在触摸处理函数里采样，让输入和模拟保持同步：在两步之间开始又结束的轻点仍会被锁存一步，按住一秒的触摸只让篮子移动一次，而不是六十次。

运行这一章，点击或触摸两侧，或者用方向键移动篮子。

## 测试触摸和按键

`nsgotest` 发送的事件和平台发送的相同。`app.Key` 按下一个键；`app.Touch` 在视图上放下一根手指并返回它，测试可以在时间流逝时一直按住，之后再抬起：

```go
func TestTouchingASideMovesTheBasket(t *testing.T) {
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.Size(480, 640))

	// The 480 by 640 window shows the playfield at twice its size, so x 400 is in its right half.
	finger := app.Touch("game-view", core.Point{X: 400, Y: 300})
	app.Advance(time.Second / 2)
	finger.Lift()
	app.Advance(time.Second / 4)
	if s.world.basketLane != laneCount/2+1 {
		t.Fatalf("after a held touch the basket heads for lane %d, want one lane right, %d", s.world.basketLane, laneCount/2+1)
	}
}
```

测试自己构建游戏而不是调用 `Root`，这样它能拿到 `*starCatcher` 并查看其中的世界。手指按住半秒，篮子仍只移动一条通道，这正是 `JustPressed` 所保证的。

## 下一章

游戏现在用矩形和圆形绘制。[第 4 章](tutorial-04-sprites.md)改用精灵绘制。
