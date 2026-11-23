# 教程 6：界面

给游戏加上标题界面、暂停菜单和结束界面。它们是画布上方的原生 NSGo 视图，按阶段显示，里面是真正的按钮，屏幕阅读器、键盘和游戏测试都能按下它们。

完成后的代码在 [`examples/tutorial/ch06-screens`](../../examples/tutorial/ch06-screens/)。视图移到了单独的文件 `screens.go`。

## 阶段

游戏总是处于四个阶段之一，阶段保存在信号里，视图随之变化：

```go
type phase uint8

const (
	phaseTitle phase = iota
	phasePlaying
	phasePaused
	phaseOver
)
```

所有阶段切换都经过同一个方法：

```go
func (s *starCatcher) setPhase(p phase) {
	s.phase.Set(p)
	s.loop.SetPaused(p == phasePaused)
	s.actions.Reset()
}
```

- `Loop.SetPaused` 在暂停时停止循环的更新，恢复时不补回离开的时间，所以玩家回来时不会有东西突然跳动。
- `Actions.Reset` 丢弃按住的触摸和锁存的按下，所以点在菜单按钮上的那一下不会同时移动篮子。

其余的切换都很短：

```go
func (s *starCatcher) start() {
	s.world.reset()
	s.sync()
	s.setPhase(phasePlaying)
}

func (s *starCatcher) togglePause() {
	switch s.phase.GetUntracked() {
	case phasePlaying:
		s.setPhase(phasePaused)
	case phasePaused:
		s.setPhase(phasePlaying)
	}
}
```

`GetUntracked` 读取信号而不让读取者订阅它，这正是按键和按钮处理函数需要的。

世界现在只在游戏进行时推进，一局结束时切换到 `phaseOver`：

```go
func (s *starCatcher) update(dt time.Duration) {
	s.actions.Step()
	if s.phase.GetUntracked() != phasePlaying {
		return
	}
	// Read the moves and step the world as before…
	if s.world.over {
		s.setPhase(phaseOver)
	}
}
```

## 每个阶段一个界面

`view` 把各个界面放在场地上方，每个界面只在它的阶段才构建：

```go
return core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: barColor},
	Children: []core.Node{
		field,
		s.when(phasePlaying, s.hud),
		s.when(phaseTitle, s.title),
		s.when(phasePaused, s.paused),
		s.when(phaseOver, s.gameOver),
	},
}

func (s *starCatcher) when(p phase, screen func() core.Node) core.Node {
	return core.Show{
		When: func() bool { return s.phase.Get() == p },
		Then: func(*core.Scope) core.Node { return screen() },
	}
}
```

每个菜单都是一个居中的面板，里面有标题和按钮：

```go
func (s *starCatcher) paused() core.Node {
	return panel("paused",
		heading("Paused"),
		button("Resume", s.togglePause),
		button("Quit", s.quit),
	)
}
```

`button` 是一个带标签、内含文字的 `base.Button`。因为菜单是原生视图而不是画在画布里的图，它们自然得到平台的文字渲染和无障碍支持：屏幕阅读器会读出“Resume，按钮”，Tab 在按钮之间移动，Return 或 Space 按下获得焦点的按钮。HUD 保留 `PointerEventsBoxNone`，所以落在分数、暂停按钮和生命之间的触摸仍会到达场地。

## 按键

Enter 在标题界面或一局结束后开始游戏，Escape 或 P 暂停和继续：

```go
cx.OnKey("enter", func() {
	if p := s.phase.GetUntracked(); p == phaseTitle || p == phaseOver {
		s.start()
	}
})
for _, key := range []string{"escape", "p"} {
	cx.OnKey(key, s.togglePause)
}
```

运行这一章。它从标题界面开始；Start 开始一局，Pause 或 Escape 让它停下，一局结束时会告诉你接住了多少颗星星。

## 测试界面

测试按角色和标签按下按钮，就像用户找到它们一样：

```go
func TestPauseStopsTheWorld(t *testing.T) {
	app, s := startPlaying(t)
	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, 0, 40)
	app.Advance(step)

	app.Press("role:button Pause")
	app.Find("text:Paused")
	y := s.world.items[0].y
	app.Advance(time.Second)
	if got := s.world.items[0].y; got != y {
		t.Fatalf("star fell from %v to %v while paused", y, got)
	}

	app.Press("role:button Resume")
	app.Advance(time.Second / 2)
	if got := s.world.items[0].y; got <= y {
		t.Fatalf("star at %v after resuming, want below %v", got, y)
	}
}
```

其他测试检查：标题界面上没有东西落下；Escape 和 P 切换暂停；一局结束时提供 Play again；Quit 回到标题界面且场地是空的。第 5 章的规则测试原样保留，因为世界没有变。

## 下一章

游戏还没有声音。[第 7 章](tutorial-07-sound.md)给它加上声音。
