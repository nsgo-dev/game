# Tutorial 6: Screens

Give the game a title screen, a pause menu and a game-over screen. They are native NSGo views over the canvas, shown by phase, with real buttons that screen readers, keyboards and game tests can all press.

The finished code is in [`examples/tutorial/ch06-screens`](../../examples/tutorial/ch06-screens/). The views move to their own file, `screens.go`.


**Files changed in this chapter:** [`main.go`](../../examples/tutorial/ch06-screens/main.go), [`game.go`](../../examples/tutorial/ch06-screens/game.go), [`world.go`](../../examples/tutorial/ch06-screens/world.go), [`art.go`](../../examples/tutorial/ch06-screens/art.go), [`screens.go`](../../examples/tutorial/ch06-screens/screens.go), [`game_test.go`](../../examples/tutorial/ch06-screens/game_test.go), [`world_test.go`](../../examples/tutorial/ch06-screens/world_test.go). The linked chapter directory contains each complete file.

## Phases

The game is always in one of four phases, held in a signal so the views follow it:

```go
type phase uint8

const (
	phaseTitle phase = iota
	phasePlaying
	phasePaused
	phaseOver
)
```

Every change of phase goes through one method:

```go
func (s *starCatcher) setPhase(p phase) {
	s.phase.Set(p)
	s.loop.SetPaused(p == phasePaused)
	s.actions.Reset()
}
```

- `Loop.SetPaused` stops the loop's updates while the game is paused, and resumes without making up the time away, so nothing jumps when the player comes back.
- `Actions.Reset` drops held touches and latched presses, so the tap on a menu's button does not also move the basket.

The other transitions are short:

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

`GetUntracked` reads a signal without subscribing whoever reads it, which is what a key or button handler wants.

The world now steps only while the game is being played, and the end of a game moves to `phaseOver`:

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

## A screen for each phase

`view` puts the screens over the playfield, each built only while its phase is on:

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

Each menu is a centered panel of a heading and buttons:

```go
func (s *starCatcher) paused() core.Node {
	return panel("paused",
		heading("Paused"),
		button("Resume", s.togglePause),
		button("Quit", s.quit),
	)
}
```

`button` is a `base.Button` with a label and a text inside. Because the menus are native views rather than pictures in the canvas, they get the platform's text rendering and accessibility for free: a screen reader reads "Resume, button", Tab moves between the buttons, and Return or Space presses the focused one. The HUD keeps `PointerEventsBoxNone`, so touches between its score, pause button and lives still reach the playfield.

## Keys

Enter starts a game from the title or the end of one, and Escape or P pause and resume:

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

Run the chapter. It opens on the title screen; Start begins a game, Pause or Escape stops it, and the end of a game says how many stars you caught.

## Test the screens

The tests press the buttons by their role and label, as a user would find them:

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

Others check that nothing falls on the title screen, that Escape and P toggle the pause, that the end of a game offers Play again, and that Quit returns to the title with an empty playfield. The rules' tests from chapter 5 carry over unchanged, because the world did not change.

## Next

The game is silent. [Chapter 7](tutorial-07-sound.md) gives it sound.
