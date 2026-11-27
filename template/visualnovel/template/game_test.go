package main

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

const frame = time.Second / 60

// startNovel starts the game on a 960 by 540 window and returns it on its title screen.
func startNovel(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *novel) {
	t.Helper()
	var n *novel
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		n = newNovel(cx)
		return n.view()
	}, append([]nsgotest.Option{nsgotest.Size(960, 540), nsgotest.Environment(core.Environment{Locale: "en"})}, opts...)...)
	app.Advance(frame)
	if n == nil || n.phase.Get() != phaseTitle {
		t.Fatal("the game did not start on its title screen")
	}
	return app, n
}

// restart starts the game again with what the last run kept in its storage, as the player opening it again does.
func restart(t *testing.T, app *nsgotest.App) (*nsgotest.App, *novel) {
	t.Helper()
	store := app.Storage()
	entries := map[string]string{}
	for _, k := range store.Keys() {
		entries[k], _ = store.GetItem(k)
	}
	return startNovel(t, nsgotest.Storage(entries))
}

func lineID(n *novel) string {
	if n.run.p == nil {
		return ""
	}
	l, _ := n.run.p.Line()
	return l.LineID
}

// backlogIDs returns the line ids of the backlog, the chosen options marked with a > before them.
func backlogIDs(n *novel) []string {
	var ids []string
	for _, e := range n.run.p.Backlog() {
		if e.Choice {
			ids = append(ids, ">"+e.LineID)
		} else {
			ids = append(ids, e.LineID)
		}
	}
	return ids
}

// playUntilChoices goes on with press, a frame apart, until choices wait or the story ends.
func playUntilChoices(t *testing.T, app *nsgotest.App, n *novel, press func()) {
	t.Helper()
	for range 200 {
		if n.phase.Get() != phasePlaying || len(n.run.p.Choices()) > 0 {
			return
		}
		press()
		app.Advance(frame)
	}
	t.Fatalf("no choices after 200 presses, at line %s", lineID(n))
}

// key presses a key and lets two frames pass, so that the press reaches a step of the game loop of its own even when a frame runs none.
func key(app *nsgotest.App, k string) func() {
	return func() {
		app.Key(k)
		app.Advance(2 * frame)
	}
}

// pad presses a controller button for two frames and releases it for two.
func pad(app *nsgotest.App, b gamepad.Button) func() {
	return func() {
		app.SetGamepadButton(1, b, 1)
		app.Advance(2 * frame)
		app.SetGamepadButton(1, b, 0)
		app.Advance(2 * frame)
	}
}

// tap taps the stage at a point with a finger.
func tap(app *nsgotest.App, at core.Point) {
	app.Touch("stage", at).Lift()
	app.Advance(frame)
}

// choicePoint returns the middle of the choice button at position i.
func choicePoint(n *novel, i int) core.Point {
	b := n.styleFor(n.size).Layout(n.size, len(n.run.p.Choices())).Choices[i]
	return core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

func wantEnding(t *testing.T, app *nsgotest.App, n *novel, ending, title string) {
	t.Helper()
	if n.phase.Get() != phaseEnded || n.run.ending != ending || !n.found[ending] {
		t.Fatalf("phase %v, ending %q, found %v; want the %s ending", n.phase.Get(), n.run.ending, n.found, ending)
	}
	if got := app.Find("heading").Text; got != title {
		t.Fatalf("ending heading = %q, want %q", got, title)
	}
}

// TestKeyboardReachesTheDawnEnding plays the story with the keyboard: Return starts it and goes on, the arrow keys move between options and Return picks one. Trusting Tobin twice and taking the marsh reaches the good ending.
func TestKeyboardReachesTheDawnEnding(t *testing.T) {
	app, n := startNovel(t)
	key(app, "enter")()
	if n.phase.Get() != phasePlaying || lineID(n) != "dock_01" || n.run.background != "dock" {
		t.Fatalf("Return on the title screen: phase %v, line %q, background %q", n.phase.Get(), lineID(n), n.run.background)
	}
	for _, pick := range []int{0, 0, 1} {
		playUntilChoices(t, app, n, key(app, "space"))
		for range pick {
			key(app, "down")()
		}
		if n.selected.Get() != pick {
			t.Fatalf("highlight at %d after %d downs", n.selected.Get(), pick)
		}
		key(app, "enter")()
	}
	playUntilChoices(t, app, n, key(app, "enter"))
	if ids := backlogIDs(n); !slices.Contains(ids, "fork_03") || slices.Contains(ids, "fork_04") {
		t.Fatalf("Tobin did not trust Mira at the fork: %v", ids)
	}
	wantEnding(t, app, n, "dawn", "Dawn Harbor")

	key(app, "enter")()
	if n.phase.Get() != phaseTitle || app.Find("endings-found").Text != "Endings found: 1 of 2" {
		t.Fatal("Return on the ending did not go back to the title screen counting the ending")
	}
}

// TestGamepadReachesTheLostEnding plays the story with a controller: South goes on and picks, the D-pad moves between options. Keeping the map hidden loses Tobin's trust, so he refuses the marsh and the story is lost.
func TestGamepadReachesTheLostEnding(t *testing.T) {
	app, n := startNovel(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(frame)
	south := pad(app, gamepad.ButtonSouth)
	south()
	if n.phase.Get() != phasePlaying {
		t.Fatal("South on the title screen did not start the story")
	}
	for _, pick := range []int{1, 0, 1} {
		playUntilChoices(t, app, n, south)
		for range pick {
			pad(app, gamepad.ButtonDPadDown)()
		}
		south()
	}
	if lineID(n) != "fork_05" {
		t.Fatalf("line after taking the marsh without trust = %q, want fork_05", lineID(n))
	}
	playUntilChoices(t, app, n, south)
	wantEnding(t, app, n, "lost", "Lost in the Fog")

	// Start opens the pause menu during the story, and East goes back.
	south()
	south()
	pad(app, gamepad.ButtonStart)()
	if n.panel.Get() != panelPause || !app.Exists("menu-resume") {
		t.Fatal("Start did not open the pause menu")
	}
	pad(app, gamepad.ButtonEast)()
	if n.panel.Get() != panelNone {
		t.Fatal("East did not close the pause menu")
	}
}

// TestTouchPlaysWithTheButtons plays the story by tapping: the stage goes on and picks options, and the buttons turn on auto mode, open the backlog and reach an ending.
func TestTouchPlaysWithTheButtons(t *testing.T) {
	app, n := startNovel(t)
	app.Press("menu-new")
	app.Advance(frame)
	middle := core.Point{X: 480, Y: 270}
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	tap(app, choicePoint(n, 1))
	if lineID(n) != "dock_07" {
		t.Fatalf("tapping the second option showed %q, want dock_07", lineID(n))
	}

	// Auto goes on by itself up to the next choices.
	app.Press("auto")
	if !n.run.p.Auto() || !n.auto.Get() {
		t.Fatal("Auto did not turn auto mode on")
	}
	for i := 0; len(n.run.p.Choices()) == 0; i++ {
		if i > 60*60 {
			t.Fatal("auto mode did not reach the river's choices in a minute")
		}
		app.Advance(frame)
	}
	app.Press("auto")

	// Log shows the lines read so far, the newest at the bottom; a tap closes it.
	backlog := n.run.p.Backlog()
	app.Press("log")
	app.Advance(frame)
	if n.panel.Get() != panelLog || !canvasTexts(app)[backlog[len(backlog)-1].Text] {
		t.Fatal("Log did not draw the backlog")
	}
	tap(app, middle)
	if n.panel.Get() != panelNone {
		t.Fatal("a tap did not close the backlog")
	}

	tap(app, choicePoint(n, 0))
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	tap(app, choicePoint(n, 0))
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	wantEnding(t, app, n, "lost", "Lost in the Fog")
	app.Press("menu-title")
	app.Advance(frame)
	if n.phase.Get() != phaseTitle {
		t.Fatal("Title screen did not go back to the title")
	}
}

// TestSaveSlotsRestoreTheStory saves in the middle of a scene, plays on to another ending, and loads the slot again, in the same run and after the game starts again: the line, the background, the backlog and the variables the choices set come back.
func TestSaveSlotsRestoreTheStory(t *testing.T) {
	app, n := startNovel(t)
	if !app.Find("menu-load").Props.Disabled {
		t.Fatal("Load is enabled before any save")
	}
	key(app, "enter")()
	space := key(app, "space")
	playUntilChoices(t, app, n, space)
	key(app, "enter")()
	playUntilChoices(t, app, n, space)
	key(app, "enter")()
	if lineID(n) != "river_03" {
		t.Fatalf("line = %q, want river_03", lineID(n))
	}
	space()
	space()
	saved, savedLog := lineID(n), backlogIDs(n)

	// Escape, down to Save, Return, Return on slot 1.
	key(app, "escape")()
	key(app, "down")()
	key(app, "enter")()
	if n.panel.Get() != panelSave || n.selected.Get() != 0 {
		t.Fatalf("panel %v, highlight %d; want the save panel on slot 1", n.panel.Get(), n.selected.Get())
	}
	key(app, "enter")()
	if n.panel.Get() != panelNone || app.Find("status").Text != "Saved to slot 1" {
		t.Fatal("saving to slot 1 did not go back to the story with a message")
	}

	// Play on and lose by following the light.
	playUntilChoices(t, app, n, space)
	key(app, "enter")()
	playUntilChoices(t, app, n, space)
	wantEnding(t, app, n, "lost", "Lost in the Fog")
	key(app, "enter")()

	load := func(app *nsgotest.App, n *novel) {
		t.Helper()
		app.Press("menu-load")
		app.Advance(frame)
		if got := app.Find("menu-slot1").Props.Label; got != "Slot 1 · The River" {
			t.Fatalf("slot 1 = %q", got)
		}
		app.Press("menu-slot1")
		app.Advance(frame)
		if n.phase.Get() != phasePlaying || lineID(n) != saved || n.run.background != "river" {
			t.Fatalf("loading slot 1: phase %v, line %q, background %q; want %s by the river", n.phase.Get(), lineID(n), n.run.background, saved)
		}
		if got := backlogIDs(n); !slices.Equal(got, savedLog) {
			t.Fatalf("backlog after loading = %v, want %v", got, savedLog)
		}
	}
	load(app, n)
	// The trust the two choices before the save earned came back with them: the marsh now leads to the harbor.
	playUntilChoices(t, app, n, space)
	key(app, "down")()
	key(app, "enter")()
	playUntilChoices(t, app, n, space)
	wantEnding(t, app, n, "dawn", "Dawn Harbor")
	key(app, "enter")()

	// The slot and the endings found outlast the app.
	app, n = restart(t, app)
	if got := app.Find("endings-found").Text; got != "Endings found: 2 of 2" {
		t.Fatalf("after a restart the title screen says %q", got)
	}
	load(app, n)
}

// TestLoadRefusesASlotThatNoLongerMatches loads a slot whose line the story no longer reaches, as after the script changed: the game stays where it was and says so.
func TestLoadRefusesASlotThatNoLongerMatches(t *testing.T) {
	app, n := startNovel(t)
	var saveErr error
	app.Post(func() {
		saveErr = n.saves.Save(slotName(2), saveData{Node: "River", Line: "river_99", Choices: []int{0}})
		n.slots.Set(1)
	})
	if saveErr != nil {
		t.Fatal(saveErr)
	}
	app.Advance(frame)
	app.Press("menu-load")
	app.Advance(frame)
	app.Press("menu-slot2")
	app.Advance(frame)
	if n.phase.Get() != phaseTitle || n.run.p != nil || app.Find("status").Text != "Slot 2 could not be loaded" {
		t.Fatalf("phase %v, status %q after loading a slot that does not match", n.phase.Get(), n.status.Get())
	}
	// Advance in chunks under game.Loop's MaxSteps per-frame cap (100ms at 60 Hz) so a single display frame on a live target does not drop the rest of statusDuration.
	for i := 0; i < 40 && app.Exists("status"); i++ {
		app.Advance(5 * frame)
	}
	if app.Exists("status") {
		t.Fatal("the status message did not go away")
	}
}

// TestSkipRunsThroughReadLinesOnly plays the story once, then skips through it again: skip mode runs on through the lines read before, without a press, and stops on the first line not read.
func TestSkipRunsThroughReadLinesOnly(t *testing.T) {
	app, n := startNovel(t)
	key(app, "enter")()
	for _, pick := range []int{1, 1, 0} {
		playUntilChoices(t, app, n, key(app, "space"))
		for range pick {
			key(app, "down")()
		}
		key(app, "enter")()
	}
	playUntilChoices(t, app, n, key(app, "space"))
	wantEnding(t, app, n, "lost", "Lost in the Fog")
	key(app, "enter")()

	key(app, "enter")()
	key(app, "s")()
	if !n.run.p.Skipping() || !n.skip.Get() {
		t.Fatal("S did not turn skip mode on")
	}
	for i := 0; len(n.run.p.Choices()) == 0; i++ {
		if i > 60 {
			t.Fatal("skip mode took over a second through four lines read before")
		}
		app.Advance(frame)
	}
	key(app, "enter")()
	key(app, "s")()
	app.Advance(frame)
	if n.run.p.Skipping() || lineID(n) != "dock_05" {
		t.Fatalf("skip went past the unread line dock_05 to %q", lineID(n))
	}
}

// TestChineseShowsTranslatedLinesAndNames switches the language on the title screen: the menus, the lines and the name plates follow, and the choice outlasts the app.
func TestChineseShowsTranslatedLinesAndNames(t *testing.T) {
	app, n := startNovel(t)
	app.Press("menu-language")
	app.Advance(frame)
	if got := app.Find("menu-language").Props.Label; got != "语言：中文" {
		t.Fatalf("language item = %q", got)
	}
	app.Press("menu-new")
	app.Advance(frame)
	if l, _ := n.run.p.Line(); l.Text != "午夜时分，最后一班渡船驶离东码头。" {
		t.Fatalf("first line = %q", l.Text)
	}
	key(app, "space")()
	key(app, "space")()
	if l, _ := n.run.p.Line(); l.Name != "托宾" {
		t.Fatalf("name plate = %q, want 托宾", l.Name)
	}
	for range 120 {
		app.Advance(frame)
	}
	if !canvasTexts(app)["托宾"] {
		t.Fatal("the name plate was not drawn in Chinese")
	}
	if got := app.Find("auto").Props.Label; got != "自动" {
		t.Fatalf("auto button = %q", got)
	}

	app, n = restart(t, app)
	if n.locale.Get() != "zh-Hans" || app.Find("menu-new").Props.Label != "新游戏" {
		t.Fatalf("after a restart the language is %s", n.locale.Get())
	}
}

func TestTitleScreenTitlesTheWindowFromTheProject(t *testing.T) {
	app, _ := startNovel(t)
	if app.Title != Project.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, Project.Title())
	}
	if got := app.Find("heading").Text; got != Project.Title() {
		t.Fatalf("title screen heading = %q, want %q", got, Project.Title())
	}
	if app.Exists("endings-found") {
		t.Fatal("the title screen counts endings before any was found")
	}
}

// canvasTexts returns the texts the stage canvas drew last.
func canvasTexts(app *nsgotest.App) map[string]bool {
	texts := map[string]bool{}
	for _, op := range app.Find("canvas").Ops {
		if op.Kind == core.DrawFillText {
			texts[op.Text] = true
		}
	}
	return texts
}

// TestGamepadSavesLoadsAndReachesTheDawnEnding saves and loads with a controller alone: Start opens the pause menu, the D-pad and South pick Save and a slot, and after losing, the title screen's Load brings back the trust the choices earned, so the marsh reaches the good ending.
func TestGamepadSavesLoadsAndReachesTheDawnEnding(t *testing.T) {
	app, n := startNovel(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(frame)
	south, down := pad(app, gamepad.ButtonSouth), pad(app, gamepad.ButtonDPadDown)
	south()
	for range 2 {
		playUntilChoices(t, app, n, south)
		south()
	}
	playUntilChoices(t, app, n, south)

	pad(app, gamepad.ButtonStart)()
	down()
	south()
	if n.panel.Get() != panelSave {
		t.Fatalf("panel %v after Start, down and South; want the save panel", n.panel.Get())
	}
	south()
	if !n.saves.Exists(slotName(1)) || n.panel.Get() != panelNone {
		t.Fatal("South on slot 1 did not save to it")
	}

	// Back at the choices the highlight is on the first, following the light, which loses the story.
	south()
	playUntilChoices(t, app, n, south)
	wantEnding(t, app, n, "lost", "Lost in the Fog")
	south()
	down()
	south()
	if n.panel.Get() != panelLoad {
		t.Fatalf("panel %v after down and South on the title screen; want the load panel", n.panel.Get())
	}
	south()
	if n.phase.Get() != phasePlaying {
		t.Fatal("South on slot 1 did not load it")
	}
	playUntilChoices(t, app, n, south)
	down()
	south()
	playUntilChoices(t, app, n, south)
	wantEnding(t, app, n, "dawn", "Dawn Harbor")
}

// TestTouchSavesLoadsAndReachesTheDawnEnding saves and loads by tapping the buttons of the pause menu, the save panel and the title screen, and reaches the good ending after a load.
func TestTouchSavesLoadsAndReachesTheDawnEnding(t *testing.T) {
	app, n := startNovel(t)
	press := func(id string) {
		t.Helper()
		app.Press(id)
		app.Advance(frame)
	}
	middle := core.Point{X: 480, Y: 270}
	press("menu-new")
	for range 2 {
		playUntilChoices(t, app, n, func() { tap(app, middle) })
		tap(app, choicePoint(n, 0))
	}
	playUntilChoices(t, app, n, func() { tap(app, middle) })

	press("menu")
	press("menu-save")
	press("menu-slot1")
	if app.Find("status").Text != "Saved to slot 1" {
		t.Fatal("tapping slot 1 did not save to it")
	}
	tap(app, choicePoint(n, 0))
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	wantEnding(t, app, n, "lost", "Lost in the Fog")

	press("menu-title")
	press("menu-load")
	press("menu-slot1")
	if n.phase.Get() != phasePlaying {
		t.Fatal("tapping slot 1 did not load it")
	}
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	tap(app, choicePoint(n, 1))
	playUntilChoices(t, app, n, func() { tap(app, middle) })
	wantEnding(t, app, n, "dawn", "Dawn Harbor")
}

// TestTouchScrollsTheBacklog opens the backlog after more lines than fit on the stage: Older scrolls back to the first line, where it stops, Newer comes forward again, and Close goes back to the story.
func TestTouchScrollsTheBacklog(t *testing.T) {
	app, n := startNovel(t)
	app.Press("menu-new")
	app.Advance(frame)
	middle := core.Point{X: 480, Y: 270}
	for range 2 {
		playUntilChoices(t, app, n, func() { tap(app, middle) })
		tap(app, choicePoint(n, 0))
	}
	playUntilChoices(t, app, n, func() { tap(app, middle) })

	const first = "The last ferry leaves the east dock at midnight."
	app.Press("log")
	app.Advance(frame)
	if n.panel.Get() != panelLog || canvasTexts(app)[first] {
		t.Fatal("the backlog's first page already shows the first line: play on further before scrolling")
	}
	if !app.Find("log-newer").Props.Disabled {
		t.Fatal("Newer is enabled at the newest line")
	}
	for i := 0; !canvasTexts(app)[first]; i++ {
		if i > len(n.run.p.Backlog()) {
			t.Fatal("Older did not scroll back to the first line")
		}
		app.Press("log-older")
		app.Advance(frame)
	}
	for !app.Find("log-older").Props.Disabled {
		app.Press("log-older")
		app.Advance(frame)
	}
	if got, want := n.logScroll.Get(), len(n.run.p.Backlog())-1; got != want {
		t.Fatalf("Older stopped at %d, want %d", got, want)
	}
	app.Press("log-newer")
	app.Advance(frame)
	if n.logScroll.Get() != len(n.run.p.Backlog())-2 || app.Find("log-older").Props.Disabled {
		t.Fatal("Newer did not scroll forward")
	}
	app.Press("log-close")
	app.Advance(frame)
	if n.panel.Get() != panelNone || len(n.run.p.Choices()) == 0 {
		t.Fatal("Close did not go back to the choices")
	}
}

// TestSavingOverAUsedSlotAsksFirst saves twice to slot 1: the second time, the first Return only asks, and a second Return overwrites the slot.
func TestSavingOverAUsedSlotAsksFirst(t *testing.T) {
	app, n := startNovel(t)
	key(app, "enter")()
	saveSlot1 := func() {
		key(app, "escape")()
		key(app, "down")()
		key(app, "enter")()
		key(app, "enter")()
	}
	saveSlot1()
	var data saveData
	if err := n.saves.Load(slotName(1), &data); err != nil || data.Line != "dock_01" {
		t.Fatalf("slot 1 = %+v, %v; want dock_01", data, err)
	}
	for lineID(n) == "dock_01" {
		key(app, "space")()
	}
	saveSlot1()
	if n.panel.Get() != panelSave || app.Find("menu-slot1").Props.Label != "Overwrite slot 1?" {
		t.Fatalf("panel %v, slot 1 %q; want the save panel asking to overwrite", n.panel.Get(), app.Find("menu-slot1").Props.Label)
	}
	if err := n.saves.Load(slotName(1), &data); err != nil || data.Line != "dock_01" {
		t.Fatalf("slot 1 changed before the second Return: %+v, %v", data, err)
	}
	key(app, "enter")()
	if err := n.saves.Load(slotName(1), &data); err != nil || data.Line != "dock_02" || n.panel.Get() != panelNone {
		t.Fatalf("slot 1 = %+v, %v after overwriting; want dock_02", data, err)
	}
}

// TestEscapeClosesTheTitleScreensLoadPanel opens Load on the title screen: Escape closes it as it does during the story.
func TestEscapeClosesTheTitleScreensLoadPanel(t *testing.T) {
	app, n := startNovel(t)
	var saveErr error
	app.Post(func() {
		saveErr = n.saves.Save(slotName(1), saveData{Node: "Start", Line: "dock_01", Choices: []int{}})
		n.slots.Set(1)
	})
	if saveErr != nil {
		t.Fatal(saveErr)
	}
	app.Advance(frame)
	app.Press("menu-load")
	app.Advance(frame)
	key(app, "escape")()
	if n.panel.Get() != panelNone || n.phase.Get() != phaseTitle || !app.Exists("menu-new") {
		t.Fatalf("panel %v, phase %v after Escape on the title screen's load panel", n.panel.Get(), n.phase.Get())
	}
}

// TestLinesReadOutlastALoadAndTheBackground reads lines after the last save and choice and then loads an earlier slot, which keeps them as read, and then moves the app to the background, which saves them, so they outlast the app.
func TestLinesReadOutlastALoadAndTheBackground(t *testing.T) {
	app, n := startNovel(t)
	space := key(app, "space")
	key(app, "enter")()
	playUntilChoices(t, app, n, space)
	key(app, "enter")()
	key(app, "escape")()
	key(app, "down")()
	key(app, "enter")()
	key(app, "enter")()
	for lineID(n) != "river_02" {
		space()
	}
	app.Press("menu")
	app.Advance(frame)
	app.Press("menu-load")
	app.Advance(frame)
	app.Press("menu-slot1")
	app.Advance(frame)
	if lineID(n) != "dock_05" || !slices.Contains(n.run.p.ReadKeys(), "river_02") {
		t.Fatalf("after loading at %s, the lines read are %v; want river_02 among them", lineID(n), n.run.p.ReadKeys())
	}

	if nsgotest.Target() != "" {
		// On a live target, SetEnvironment does not override Lifecycle; headless tests verify the background save hook.
		return
	}
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	_, n = restart(t, app)
	if !slices.Contains(n.read, "river_02") {
		t.Fatalf("after the background and a restart, the lines read are %v; want river_02 among them", n.read)
	}
}

// TestLoadFindsALineShownTwice saves on the second showing of a line a jump shows again with no choice between: loading comes back to the second showing, with the variable the first one's pass set.
func TestLoadFindsALineShownTwice(t *testing.T) {
	app, n := startNovel(t)
	script, err := dialogue.ParseYarn(`title: Start
chapter: dock
---
<<declare $n = 0>>
<<bg dock>>
First. #line:t_first
<<jump Again>>
===
title: Again
chapter: river
---
Again. #line:t_again
<<if $n < 1>>
<<set $n += 1>>
<<jump Again>>
<<endif>>
Done. #line:t_done
===
`)
	if err != nil {
		t.Fatal(err)
	}
	n.story.script = script
	space := key(app, "space")
	key(app, "enter")()
	for i := 0; occurrence(n.run.p, "t_again") < 2; i++ {
		if i > 10 {
			t.Fatalf("t_again was not shown twice, at %s", lineID(n))
		}
		space()
	}
	if lineID(n) != "t_again" || occurrence(n.run.p, "t_again") != 2 {
		t.Fatalf("at %s, shown %d times; want t_again shown twice", lineID(n), occurrence(n.run.p, "t_again"))
	}
	app.Post(func() {
		n.saveTo(1)
		n.toTitle()
		n.loadFrom(1)
	})
	app.Advance(frame)
	if n.phase.Get() != phasePlaying || lineID(n) != "t_again" || occurrence(n.run.p, "t_again") != 2 {
		t.Fatalf("loaded at %s, shown %d times; want the second t_again", lineID(n), occurrence(n.run.p, "t_again"))
	}
	for lineID(n) == "t_again" {
		space()
	}
	if lineID(n) != "t_done" {
		t.Fatalf("after the loaded line came %s, want t_done", lineID(n))
	}
}

// TestTextBoxKeepsClearOfTheSafeArea plays on a phone held sideways, whose notch and home indicator take the stage's sides and bottom: the text box stays inside them.
func TestTextBoxKeepsClearOfTheSafeArea(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("safe-area environment pinning is headless-only")
	}
	app, n := startNovel(t, nsgotest.Environment(core.Environment{Locale: "en", SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21}}))
	key(app, "enter")()
	box := n.styleFor(n.size).Layout(n.size, 0).Box
	if box.Origin.X < 47 || box.Origin.X+box.Size.Width > n.size.Width-47 || box.Origin.Y+box.Size.Height > n.size.Height-21 {
		t.Fatalf("text box %+v on a %+v stage reaches into the safe area", box, n.size)
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 540))
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}
