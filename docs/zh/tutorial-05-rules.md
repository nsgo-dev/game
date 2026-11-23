# 教程 5：规则

把下落的星星变成游戏：用带种子的 `random.Stream` 落下星星和石头，用 `physics` 的盒子判断篮子接到了什么，记录分数和生命，失去最后一条命时结束游戏，并证明同样的种子总是玩出同样的一局。

完成后的代码在 [`examples/tutorial/ch05-rules`](../../examples/tutorial/ch05-rules/)。

## 规则

所有规则都是 `world.go` 顶部的常量，所以调整游戏只需要改一处的数字：

```go
const (
	startLives   = 3
	rockChance   = 0.25
	fallSpeed    = 90  // points a second, at a score of 0
	fallPerScore = 6   // points a second more for each star caught
	maxFallSpeed = 220 // points a second
	firstSpawn   = 600 * time.Millisecond
	slowSpawn    = 1100 * time.Millisecond
	fastSpawn    = 400 * time.Millisecond
	spawnPerStar = 25 * time.Millisecond
)
```

世界现在保存一列下落物（每个是星星或石头），以及分数、生命和游戏是否结束：

```go
type item struct {
	kind     itemKind
	lane     int
	y, prevY float32
	speed    float32
}

type world struct {
	basketLane     int
	basketX, prevX float32

	items   []item
	rng     *random.Stream
	spawnIn time.Duration

	score, lives int
	over         bool
	// events are what happened in the last step, oldest first.
	events []event
}
```

## 带种子的生成

`random.Stream` 是游戏库的随机数生成器。它的数字只取决于种子，在所有操作系统、架构和 Go 版本上都相同，而 `math/rand` 不保证这一点。每次生成都从它取数：

```go
func (w *world) spawn() {
	lane := w.rng.Intn(laneCount)
	kind := kindStar
	if w.rng.Chance(rockChance) {
		kind = kindRock
	}
	speed := min(float32(fallSpeed+fallPerScore*w.score), maxFallSpeed)
	w.items = append(w.items, item{kind: kind, lane: lane, y: -itemSize, prevY: -itemSize, speed: speed})
}

func (w *world) spawnInterval() time.Duration {
	return max(slowSpawn-spawnPerStar*time.Duration(w.score), fastSpawn)
}
```

游戏用时钟给世界播种，所以每次启动玩起来都不同；测试则传入自己的种子：

```go
func Root(cx *core.Scope) core.Node {
	return newStarCatcher(cx, uint64(cx.WallTime().UnixNano())).view()
}
```

## 接住、漏掉与结束

每一步先移动所有下落物，再处理它们碰到了什么。`physics.TestBoxBox` 报告两个盒子是否重叠；下落物的盒子比它的图片小一点，所以只擦到一个角不算接住：

```go
basket := w.basketBox()
kept := w.items[:0]
for _, it := range w.items {
	it.y += it.speed * secs
	switch {
	case physics.TestBoxBox(basket, it.box()).Hit:
		if it.kind == kindStar {
			w.score++
			w.events = append(w.events, eventCaught)
		} else {
			w.lives--
			w.events = append(w.events, eventHit)
		}
		continue
	case it.y-itemSize/2 >= groundY:
		if it.kind == kindStar {
			w.lives--
			w.events = append(w.events, eventMissed)
		}
		continue
	}
	kept = append(kept, it)
}
clear(w.items[len(kept):])
w.items = kept

if w.lives <= 0 {
	w.lives = 0
	w.over = true
	w.events = append(w.events, eventOver)
}
```

- 过滤到 `w.items[:0]` 会复用切片的底层数组，所以列表长到一定大小后，每一步都不再分配内存。
- 下落物每一步最多移动不到四个点，比篮子的高度小，所以星星不可能在两步之间穿过篮子。
- `events` 记录这一步发生的事。界面现在读取它们来更新分数，[第 7 章](tutorial-07-sound.md)还会为每个事件播放一个声音。

游戏结束后，`step` 不再移动任何东西，直到 `reset` 开始下一局。

## 屏幕上的分数

分数和生命是画布上方的原生 `core.Text` 视图。每个视图读取一个信号，游戏在发生了事件的一步之后用世界的值设置这些信号：

```go
core.Text{TestID: "score", ContentFunc: func() string { return fmt.Sprintf("Score: %d", s.score.Get()) }, Style: label},
core.Text{TestID: "lives", ContentFunc: func() string { return fmt.Sprintf("Lives: %d", s.lives.Get()) }, Style: label},
```

每个下落物需要一个精灵来绘制：闪烁的星星，或者石头（图集里新画的一帧）。游戏保存一个精灵切片，在各帧之间复用。游戏结束时，`core.Show` 显示一个面板，直到玩家轻点或按 Enter 开始下一局。

## 测试规则

规则的测试精确地搭建它要测的场景。`quiet` 创建一个不会自己生成下落物的世界，`drop` 放下一个下落物：

```go
func TestCatchingARockCostsALife(t *testing.T) {
	w := quiet()
	w.drop(kindRock, w.basketLane, basketY-itemSize)
	if got := stepUntil(t, w); !slices.Equal(got, []event{eventHit}) {
		t.Fatalf("events = %v, want a hit", got)
	}
	if w.score != 0 || w.lives != startLives-1 {
		t.Fatalf("score %d, lives %d; want 0, %d", w.score, w.lives, startLives-1)
	}
}
```

`random.AssertDeterministic` 从头运行一整分钟的游戏三次，只要有一次结果和第一次不同就让测试失败：

```go
func TestSameSeedPlaysTheSameGame(t *testing.T) {
	random.AssertDeterministic(t, 3, func() snapshot { return play(42, 3600) })
}
```

模糊测试 `FuzzWorldKeepsItsRules` 用任意种子和任意操作玩二十秒，每一步之后检查：生命在 0 到 3 之间，分数从不减少，篮子留在场地内，没有东西穿过地面。`nsgo test` 会运行它的种子用例；要搜索更多输入，在 `--` 之后传入 Go 的模糊测试参数：

```sh
go tool nsgo test ./ch05-rules -- -run '^$' -fuzz FuzzWorldKeepsItsRules -fuzztime 1m
```

## 下一章

应用一打开游戏就开始，而且无法暂停。[第 6 章](tutorial-06-screens.md)加入标题界面、暂停菜单和像样的结束界面。
