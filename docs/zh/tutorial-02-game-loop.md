# 教程 2：游戏循环

给游戏一个按固定步长前进的世界，用 `game.Loop` 驱动它，以 `screen.Viewport` 缩放到任意窗口的虚拟分辨率绘制它，并在没有窗口的情况下测试这个世界。

完成后的代码在 [`examples/tutorial/ch02-game-loop`](../../examples/tutorial/ch02-game-loop/)。

## 只由步进改变的世界

把游戏状态放进一个单独的类型，并且只在一个方法 `step` 里改变它，每次推进一段固定时间：

```go
type world struct {
	basketX float32

	starLane     int
	starY, prevY float32
}

func (w *world) step(dt time.Duration) {
	w.prevY = w.starY
	w.starY += fallSpeed * float32(dt.Seconds())
	if w.starY > virtualHeight+itemSize {
		w.respawn((w.starLane + 2) % laneCount)
	}
}
```

世界对窗口和帧一无所知，这有三个好处：测试一毫秒内就能推进它上千次；游戏在 60 Hz 和 120 Hz 的显示器上玩起来一样；到了[第 5 章](tutorial-05-rules.md)，同样的种子和操作总会玩出同样的一局。

`prevY` 记录星星上一步的位置。绘制时用它把星星放在两步之间：

```go
func (w *world) starAt(alpha float32) float32 { return w.prevY + (w.starY-w.prevY)*alpha }
```

## 用 game.Loop 驱动

`game.Loop` 在显示帧上以固定频率运行 `Update`，并在两次之间每帧运行一次 `Draw`：

```go
func newStarCatcher(cx *core.Scope) *starCatcher {
	s := &starCatcher{world: newWorld(), frame: core.NewSignal(cx, uint64(0))}
	loop := &game.Loop{
		Rate:   60,
		Update: s.update,
		Draw: func(alpha float64) {
			s.alpha = alpha
			s.frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)
	return s
}

func (s *starCatcher) update(dt time.Duration) { s.world.step(dt) }
```

- 每个显示帧，循环累加距上一帧的时间，其中每包含一个完整的六十分之一秒就运行一次 `Update`，所以 `dt` 总是 1/60 秒。
- `Draw` 得到 `alpha`，即这一帧位于上一步和下一步之间的位置，范围从 0 到 1（不含 1）。
- 画布在它的 `Draw` 里读取 `frame`，所以每帧设置它只会重绘画布，而不会重建任何视图。
- 组件销毁时循环停止；应用在后台时不运行任何更新。

## 虚拟分辨率

手机、平板、桌面窗口和浏览器标签页的尺寸和比例都不一样。Star Catcher 不为每一种单独布局，而是在一块宽 240 点、高 320 点、分成五条通道的虚拟画布上进行，由 `screen.Viewport` 把它适配到窗口：

```go
var viewport = screen.Viewport{
	Width:    virtualWidth,
	Height:   virtualHeight,
	Mode:     screen.ModeFit,
	BarColor: barColor,
}

func (s *starCatcher) draw(d *core.Drawing, size core.Size) {
	_ = s.frame.Get()
	viewport.Draw(d, size, s.drawWorld)
}
```

`ModeFit` 在保持比例的前提下把场地放到最大，其余部分用 `BarColor` 颜色的边条填充。`viewport.Draw` 缩放并裁剪绘制内容，然后调用 `drawWorld`，后者只使用虚拟坐标绘制：

```go
func (s *starCatcher) drawWorld(d *core.Drawing, _ core.Size) {
	w := s.world
	d.SetFillColor(skyColor).FillRect(0, 0, virtualWidth, virtualHeight)
	// The lanes and the ground…
	d.SetFillColor(basketColor).FillRect(w.basketX-basketWidth/2, basketY, basketWidth, basketHeight)

	y := w.starAt(float32(s.alpha))
	d.SetFillColor(starColor).BeginPath().Ellipse(laneX(w.starLane), y, itemSize/2, itemSize/2).Fill()
}
```

运行这一章。一颗星星沿一条通道落下，越过地面后从两条通道外重新开始。把窗口拉宽，场地两侧会出现边条。

## 测试世界和循环

世界的测试手动推进它。推进一秒，星星必须下落 `fallSpeed` 点：

```go
func TestStarFallsAtFallSpeed(t *testing.T) {
	w := newWorld()
	start := w.starY
	for range 60 {
		w.step(step)
	}
	if got, want := w.starY-start, float32(fallSpeed); !near(got, want) {
		t.Fatalf("star fell %v points in a second, want %v", got, want)
	}
}
```

一个应用测试检查循环确实在运行：`app.Advance` 推进测试时钟，并运行这段时间里的显示帧，所以一秒后画布画出的星星更低了：

```go
func TestLoopMovesTheStarOnScreen(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(240, 320))
	before := starEllipse(t, app.Find("game-canvas").Ops)

	app.Advance(time.Second)

	after := starEllipse(t, app.Find("game-canvas").Ops)
	if after.Y <= before.Y {
		t.Fatalf("star at y %v after a second, want below %v", after.Y, before.Y)
	}
}
```

两个测试都不睡眠：测试时钟只在测试要求时才前进，所以测试和世界一样快、一样可重复。

## 下一章

篮子还不会动。[第 3 章](tutorial-03-input.md)让玩家移动它。
