package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

const (
	// slotCount is how many save slots the save and load panels offer.
	slotCount = 3
	// storagePrefix keys the game's save slots and settings in the app's storage.
	storagePrefix = "visualnovel"
	// titleBackground is the background behind the title screen.
	titleBackground = "river"
	// fadeDuration is how long a new background takes to fade in over the last.
	fadeDuration = 600 * time.Millisecond
	// statusDuration is how long a status message, such as a save's, stays.
	statusDuration = 2 * time.Second
	// logBarHeight is the band at the top of the backlog that its buttons stand in, so no entry is drawn under them.
	logBarHeight = 56
	// maxReplaySteps bounds the lines and choices a load replays, so a save that no longer matches the story fails instead of running on.
	maxReplaySteps = 10000
)

// The presenter's pace: how long auto mode waits after a line, plus a little per character, and how long skip mode shows each line.
const (
	autoDelay        = 1200 * time.Millisecond
	autoDelayPerRune = 25 * time.Millisecond
	skipInterval     = 70 * time.Millisecond
)

// Action names, which game.yaml's input map binds to keys and controller buttons. Taps and clicks reach the stage and the buttons directly.
const (
	actionAdvance = "advance"
	actionUp      = "up"
	actionDown    = "down"
	actionAuto    = "auto"
	actionSkip    = "skip"
	actionLog     = "log"
	actionMenu    = "menu"
	actionBack    = "back"
)

// Keys of save.Settings.Custom, which keeps what the player has seen across save slots.
const (
	settingRead    = "read"
	settingEndings = "endings"
)

// phase is where the player is.
type phase uint8

const (
	phaseTitle phase = iota
	phasePlaying
	phaseEnded
)

// panel is what covers the stage.
type panel uint8

const (
	panelNone panel = iota
	panelLog
	panelPause
	panelSave
	panelLoad
)

// errSaveMismatch reports a save slot that no longer leads to a line of the story, as after the script changed under it.
var errSaveMismatch = errors.New("the save does not match the story")

// saveData is what a save slot holds. The dialogue runner's state is its variables and visits but not where in a node it is, so a slot keeps the path to the line instead: the options chosen since the start, in order, and the line shown, which loading replays.
type saveData struct {
	// Node is the node of the line shown, which names the slot's chapter.
	Node string `json:"node"`
	// Line is the #line: id of the line shown.
	Line string `json:"line"`
	// Occurrence counts the times Line had been shown since the last option chosen, the line shown included, as a line reached by two jumps shows twice; 0, from a slot written before the field, counts as 1.
	Occurrence int `json:"occurrence"`
	// Choices are the Index of each option chosen, in order.
	Choices []int `json:"choices"`
}

// check refuses a slot that cannot be a saveData the game wrote, so save.Store.LoadChecked falls back to its backup.
func (s *saveData) check() error {
	if s.Line == "" || s.Node == "" {
		return errors.New("save slot names no line")
	}
	if s.Occurrence < 0 {
		return fmt.Errorf("save slot has occurrence %d", s.Occurrence)
	}
	for _, c := range s.Choices {
		if c < 0 {
			return fmt.Errorf("save slot has option index %d", c)
		}
	}
	return nil
}

// run is one playthrough: its presenter, the backgrounds it shows and the path it took.
type run struct {
	p *vn.Presenter
	// background is the background shown, previous the one it fades in over for fade more.
	background, previous string
	fade                 time.Duration
	// ending is the ending <<ending>> named, empty before the story reaches one.
	ending string
	// choices are the Index of each option chosen, which a save slot keeps.
	choices []int
}

// novel is the game: the title screen, the story with its menus, and the endings.
type novel struct {
	story    *story
	actions  *input.Actions
	saves    *save.Store
	settings *save.DefaultSettingsStore
	prefs    save.Settings
	style    vn.Style

	run       run
	replaying bool
	// waiting is whether choices waited at the last step, so the highlight moves to the first one when they appear.
	waiting bool
	// read are the keys of the lines read in earlier playthroughs, for skip mode.
	read []string
	// found are the endings the player has reached.
	found map[string]bool

	clock      time.Duration
	statusLeft time.Duration
	size       core.Size
	// insets are what the stage keeps its text clear of, such as a phone's notch and home indicator.
	insets func() core.Edges

	phase    *core.Signal[phase]
	panel    *core.Signal[panel]
	selected *core.Signal[int]
	locale   *core.Signal[string]
	auto     *core.Signal[bool]
	skip     *core.Signal[bool]
	status   *core.Signal[string]
	// logScroll is how many of the newest backlog entries the backlog skips, scrolled back.
	logScroll *core.Signal[int]
	// overwrite is the used save slot, from 1, that a press on the save panel asked to overwrite and a second press overwrites; 0 for none.
	overwrite *core.Signal[int]
	// slots changes after each save, so the slot list reads the slots again.
	slots *core.Signal[int]
	frame *core.Signal[uint64]
}

// Root is the game's root NSGo component.
func Root(cx *core.Scope) core.Node {
	return newNovel(cx).view()
}

// newNovel loads the story and the player's settings and starts the game loop on the title screen.
func newNovel(cx *core.Scope) *novel {
	desktop.SetTitle(cx, Project.Title())
	st, err := loadStory()
	if err != nil {
		// The story is embedded in the binary, and story_test.go loads it, so this only fails in a build whose tests fail.
		panic(err)
	}
	atlas := portraitAtlas(st.cast)
	if err := st.cast.CheckFrames(atlas); err != nil {
		panic(err)
	}
	n := &novel{
		story:    st,
		actions:  input.NewActions(),
		saves:    save.NewStorageStore(cx.Storage(), storagePrefix),
		settings: save.NewStorageSettingsStore(cx.Storage(), storagePrefix),
		style: vn.Style{
			Atlas:          atlas,
			InactiveAlpha:  0.6,
			NamePlateColor: core.RGB(0x3b4b6b),
		},
		found:     map[string]bool{},
		phase:     core.NewSignal(cx, phaseTitle),
		panel:     core.NewSignal(cx, panelNone),
		selected:  core.NewSignal(cx, 0),
		locale:    core.NewSignal(cx, baseLocale),
		auto:      core.NewSignal(cx, false),
		skip:      core.NewSignal(cx, false),
		status:    core.NewSignal(cx, ""),
		logScroll: core.NewSignal(cx, 0),
		overwrite: core.NewSignal(cx, 0),
		slots:     core.NewSignal(cx, 0),
		frame:     core.NewSignal(cx, uint64(0)),
		insets:    core.InsetsFunc(cx),
	}
	n.loadSettings(cx.Env().Locale())

	Project.BindInput(n.actions)
	// Keys count wherever the keyboard focus is, and every connected controller plays.
	n.actions.WatchKeys(cx)
	n.actions.WatchGamepads(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: n.update,
		Draw:   func(float64) { n.frame.Update(func(f uint64) uint64 { return f + 1 }) },
	}
	loop.Run(cx)

	// The lines read since the last choice reach the settings only when they are saved: save them when the app leaves the front, where a phone may stop it without warning, and when it quits.
	cx.Effect(func() {
		if cx.Env().Lifecycle() != core.LifecycleActive {
			n.saveSettings()
		}
	})
	cx.OnQuit(func(context.Context) { n.saveSettings() })
	return n
}

// loadSettings reads the player's language, the lines read and the endings found, choosing a language from the device's locale tag when none was saved.
func (n *novel) loadSettings(deviceLocale string) {
	prefs, err := n.settings.Load()
	if err != nil {
		log.Printf("settings: %v", err)
		prefs = save.DefaultSettings()
	}
	n.prefs = prefs
	locale := pickLanguage(deviceLocale)
	if slices.Contains(languages, prefs.Locale) {
		locale = prefs.Locale
	}
	n.setLocale(locale)
	if raw, ok := prefs.Custom[settingRead]; ok {
		if err := json.Unmarshal(raw, &n.read); err != nil {
			log.Printf("settings: lines read: %v", err)
		}
	}
	if raw, ok := prefs.Custom[settingEndings]; ok {
		var found []string
		if err := json.Unmarshal(raw, &found); err != nil {
			log.Printf("settings: endings: %v", err)
		}
		for _, e := range found {
			n.found[e] = true
		}
	}
}

// saveSettings writes the player's language, the lines read and the endings found.
func (n *novel) saveSettings() {
	if n.run.p != nil {
		n.read = n.run.p.ReadKeys()
	}
	found := make([]string, 0, len(n.found))
	for _, e := range endings {
		if n.found[e] {
			found = append(found, e)
		}
	}
	if n.prefs.Custom == nil {
		n.prefs.Custom = map[string]json.RawMessage{}
	}
	n.prefs.Locale = n.locale.GetUntracked()
	for key, v := range map[string]any{settingRead: n.read, settingEndings: found} {
		raw, err := json.Marshal(v)
		if err != nil {
			log.Printf("settings: %v", err)
			return
		}
		n.prefs.Custom[key] = raw
	}
	if err := n.settings.Save(n.prefs); err != nil {
		log.Printf("settings: %v", err)
	}
}

// setLocale shows the interface, the lines and the name plates in locale.
func (n *novel) setLocale(locale string) {
	n.locale.Set(locale)
	n.story.localizeNames(locale)
}

// t returns the string key in the player's language; read in a binding, it follows the language.
func (n *novel) t(key string, args ...l10n.Arg) string {
	return n.story.catalog.T(n.locale.Get(), key, args...)
}

// newPresenter returns a presenter for a new playthrough that knows the lines read before.
func (n *novel) newPresenter() *vn.Presenter {
	p := vn.New(n.story.script, n.story.cast, vn.Options{
		Dialogue: flow.DialogueOptions{
			OnCommand:      n.command,
			StringResolver: n.story.resolver(n.locale.GetUntracked),
		},
		AutoDelay:        autoDelay,
		AutoDelayPerRune: autoDelayPerRune,
		SkipInterval:     skipInterval,
	})
	p.MarkRead(n.read...)
	return p
}

// newGame starts the story from the beginning.
func (n *novel) newGame() {
	n.run = run{p: n.newPresenter()}
	if err := n.run.p.Start(startNode); err != nil {
		log.Print(err)
		return
	}
	n.waiting = false
	n.phase.Set(phasePlaying)
	n.openPanel(panelNone)
}

// toTitle leaves the story for the title screen.
func (n *novel) toTitle() {
	n.saveSettings()
	n.run = run{}
	n.phase.Set(phaseTitle)
	n.openPanel(panelNone)
}

// command runs the script's commands other than vn's own.
func (n *novel) command(ev flow.DialogueEvent) {
	switch ev.Command {
	case commandBackground:
		if len(ev.Args) != 1 || backgrounds[ev.Args[0]] == nil {
			log.Printf("%s: <<%s %v>> names no background of stage.go", ev.Node, ev.Command, ev.Args)
			return
		}
		n.run.previous, n.run.background = n.run.background, ev.Args[0]
		n.run.fade = fadeDuration
		if n.replaying || n.run.previous == "" {
			n.run.fade = 0
		}
	case commandEnding:
		if len(ev.Args) != 1 || !slices.Contains(endings, ev.Args[0]) {
			log.Printf("%s: <<%s %v>> names no ending of story.go", ev.Node, ev.Command, ev.Args)
			return
		}
		n.run.ending = ev.Args[0]
	default:
		log.Printf("%s: unknown command <<%s>>", ev.Node, ev.Command)
	}
}

// update is one fixed step of the game loop.
func (n *novel) update(dt time.Duration) {
	n.actions.Step()
	n.clock += dt
	n.run.fade = max(0, n.run.fade-dt)
	if n.statusLeft > 0 {
		if n.statusLeft -= dt; n.statusLeft <= 0 {
			n.status.Set("")
		}
	}
	n.handleActions()
	if n.phase.GetUntracked() == phasePlaying && n.panel.GetUntracked() == panelNone {
		if err := n.run.p.Update(dt); err != nil {
			log.Print(err)
		}
	}
	n.afterStep()
}

// afterStep follows the presenter: it shows the ending once the story ends, moves the highlight to the first choice when choices appear, and mirrors auto and skip modes for the buttons.
func (n *novel) afterStep() {
	p := n.run.p
	if p == nil || n.phase.GetUntracked() != phasePlaying {
		return
	}
	if p.Ended() {
		n.finish()
		return
	}
	waiting := len(p.Choices()) > 0
	if waiting && !n.waiting {
		n.selected.Set(firstAvailable(p.Choices()))
	}
	n.waiting = waiting
	n.auto.Set(p.Auto())
	n.skip.Set(p.Skipping())
}

// finish shows the ending the story reached and remembers it.
func (n *novel) finish() {
	if n.run.ending != "" {
		n.found[n.run.ending] = true
	}
	n.run.p.SetAuto(false)
	n.auto.Set(false)
	n.skip.Set(false)
	n.saveSettings()
	n.phase.Set(phaseEnded)
	n.openPanel(panelNone)
}

// handleActions acts on the step's keys and controller buttons.
func (n *novel) handleActions() {
	a := n.actions
	switch {
	case n.panel.GetUntracked() == panelLog:
		n.handleLog()
	case n.menuOpen():
		switch {
		case a.JustPressed(actionUp):
			n.selected.Set(step(n.menuItems(), n.selected.GetUntracked(), -1))
		case a.JustPressed(actionDown):
			n.selected.Set(step(n.menuItems(), n.selected.GetUntracked(), 1))
		case a.JustPressed(actionAdvance):
			n.activate(n.selected.GetUntracked())
		case a.JustPressed(actionBack), a.JustPressed(actionMenu) && n.panel.GetUntracked() != panelNone:
			n.back()
		}
	case n.phase.GetUntracked() == phasePlaying:
		p := n.run.p
		switch {
		case a.JustPressed(actionMenu):
			n.openPanel(panelPause)
		case a.JustPressed(actionLog):
			n.openPanel(panelLog)
		case a.JustPressed(actionAuto):
			p.SetAuto(!p.Auto())
		case a.JustPressed(actionSkip):
			p.SetSkip(!p.Skipping())
		case len(p.Choices()) > 0:
			choices := p.Choices()
			sel := n.selected.GetUntracked()
			switch {
			case a.JustPressed(actionUp):
				n.selected.Set(stepChoice(choices, sel, -1))
			case a.JustPressed(actionDown):
				n.selected.Set(stepChoice(choices, sel, 1))
			case a.JustPressed(actionAdvance) && sel >= 0 && sel < len(choices):
				n.choose(choices[sel].Index)
			}
		case a.JustPressed(actionAdvance):
			n.advance()
		}
	}
}

// handleLog scrolls and closes the backlog.
func (n *novel) handleLog() {
	a := n.actions
	switch {
	case a.JustPressed(actionUp):
		n.scrollLog(1)
	case a.JustPressed(actionDown):
		n.scrollLog(-1)
	case a.JustPressed(actionAdvance), a.JustPressed(actionBack), a.JustPressed(actionLog), a.JustPressed(actionMenu):
		n.openPanel(panelNone)
	}
}

// scrollLog scrolls the backlog back by dir entries, or forward for a negative dir, staying between the newest entry and the oldest.
func (n *novel) scrollLog(dir int) {
	n.logScroll.Set(min(max(n.logScroll.GetUntracked()+dir, 0), n.logScrollMax()))
}

// logScrollMax is the furthest the backlog scrolls back, which shows the oldest entry at its bottom.
func (n *novel) logScrollMax() int {
	if n.run.p == nil {
		return 0
	}
	return max(len(n.run.p.Backlog())-1, 0)
}

// advance goes on to the rest of the line or the next one.
func (n *novel) advance() {
	if err := n.run.p.Advance(); err != nil && !errors.Is(err, flow.ErrChoiceRequired) {
		log.Print(err)
	}
}

// choose picks the option whose Index is index and keeps it on the run's path.
func (n *novel) choose(index int) {
	if err := n.run.p.Choose(index); err != nil {
		log.Print(err)
		return
	}
	n.run.choices = append(n.run.choices, index)
	n.waiting = false
	if !n.replaying {
		n.saveSettings()
	}
}

// press is a tap or click on the stage: it closes the backlog, picks the choice under it, or goes on.
func (n *novel) press(at core.Point) {
	if n.phase.GetUntracked() != phasePlaying {
		return
	}
	switch n.panel.GetUntracked() {
	case panelLog:
		n.openPanel(panelNone)
		return
	case panelNone:
	default:
		return
	}
	p := n.run.p
	if len(p.Choices()) > 0 {
		if i, ok := p.ChoiceAt(n.size, n.styleFor(n.size), at); ok {
			n.choose(i)
		}
		return
	}
	n.advance()
}

// openPanel shows panel over the stage and highlights its first item.
func (n *novel) openPanel(pn panel) {
	n.panel.Set(pn)
	n.logScroll.Set(0)
	n.overwrite.Set(0)
	n.waiting = false
	if n.menuOpen() {
		n.selected.Set(step(n.menuItems(), -1, 1))
	}
}

// back closes the panel shown, going back to the one it opened from.
func (n *novel) back() {
	switch {
	case n.phase.GetUntracked() == phasePlaying && n.panel.GetUntracked() != panelPause:
		n.openPanel(panelPause)
	default:
		n.openPanel(panelNone)
	}
}

// say shows a status message for a moment.
func (n *novel) say(msg string) {
	n.status.Set(msg)
	n.statusLeft = statusDuration
}

// slotName is the save.Store slot of save slot i, from 1.
func slotName(i int) string { return fmt.Sprintf("slot%d", i) }

// saveTo saves the run to slot i and goes back to the story.
func (n *novel) saveTo(i int) {
	p := n.run.p
	line, ok := p.Line()
	if !ok || line.LineID == "" {
		log.Printf("save %s: the line shown has no #line: id", slotName(i))
		n.say(n.t("status.saveFailed", l10n.Param("slot", i)))
		return
	}
	data := saveData{Node: p.Runner().CurrentNode(), Line: line.LineID, Occurrence: occurrence(p, line.LineID), Choices: slices.Clone(n.run.choices)}
	if data.Choices == nil {
		data.Choices = []int{}
	}
	if err := n.saves.Save(slotName(i), data); err != nil {
		log.Printf("save %s: %v", slotName(i), err)
		n.say(n.t("status.saveFailed", l10n.Param("slot", i)))
		return
	}
	n.saveSettings()
	n.slots.Update(func(v int) int { return v + 1 })
	n.openPanel(panelNone)
	n.say(n.t("status.saved", l10n.Param("slot", i)))
}

// loadFrom loads slot i, replaying its path, and plays on from its line. A slot that fails to load leaves the game as it was.
func (n *novel) loadFrom(i int) {
	var data saveData
	err := n.saves.LoadChecked(slotName(i), &data, data.check)
	if err == nil {
		err = n.replay(data)
	}
	if err != nil {
		log.Printf("load %s: %v", slotName(i), err)
		n.say(n.t("status.loadFailed", l10n.Param("slot", i)))
		return
	}
	n.phase.Set(phasePlaying)
	n.openPanel(panelNone)
}

// replay plays a new run from the start along data's choices to its line, at once: commands run, the backlog fills and the lines count as read, as they did when the player first saw them.
func (n *novel) replay(data saveData) error {
	before, beforeWaiting := n.run, n.waiting
	// The new presenter starts from n.read: take in the lines the run it replaces read since the settings were last saved.
	if n.run.p != nil {
		n.read = n.run.p.ReadKeys()
	}
	n.run = run{p: n.newPresenter()}
	n.replaying = true
	defer func() { n.replaying = false }()
	err := n.replayTo(data)
	if err != nil {
		n.run, n.waiting = before, beforeWaiting
		return err
	}
	n.run.fade = 0
	n.waiting = false
	return nil
}

// replayTo starts the run's presenter and plays it along data's choices until it shows data's line for the data.Occurrence-th time since the last choice.
func (n *novel) replayTo(data saveData) error {
	p := n.run.p
	if err := p.Start(startNode); err != nil {
		return err
	}
	want := max(data.Occurrence, 1)
	k := 0
	for range maxReplaySteps {
		if p.Ended() {
			return errSaveMismatch
		}
		if line, ok := p.Line(); ok && k == len(data.Choices) && line.LineID == data.Line && occurrence(p, data.Line) == want {
			return nil
		}
		if len(p.Choices()) > 0 {
			if k == len(data.Choices) {
				return errSaveMismatch
			}
			if err := p.Choose(data.Choices[k]); err != nil {
				return fmt.Errorf("%w: %w", errSaveMismatch, err)
			}
			n.run.choices = append(n.run.choices, data.Choices[k])
			k++
			continue
		}
		if tw := p.Typewriter(); tw != nil {
			tw.Skip()
		}
		if err := p.Advance(); err != nil {
			return err
		}
	}
	return errSaveMismatch
}

// occurrence counts the backlog entries since the last option chosen that show the line with id, the times a save slot's line had been shown when it was saved. Replaying the same path builds the same backlog, so a load counts the same way.
func occurrence(p *vn.Presenter, id string) int {
	count := 0
	backlog := p.Backlog()
	for i := len(backlog) - 1; i >= 0 && !backlog[i].Choice; i-- {
		if backlog[i].LineID == id {
			count++
		}
	}
	return count
}

// styleFor returns the style the stage draws in at size: portraits stand most of its height, and the text box keeps clear of the insets on its sides and below it.
func (n *novel) styleFor(size core.Size) vn.Style {
	st := n.style
	st.PortraitHeight = float32(size.Height) * 0.72
	in := n.insets()
	st.Margin = max(16, float32(in.Left), float32(in.Right), float32(in.Bottom))
	return st
}

// firstAvailable returns the position of the first available choice, 0 when none is.
func firstAvailable(choices []flow.DialogueChoice) int {
	return max(stepChoice(choices, -1, 1), 0)
}

// stepChoice moves the highlight from position from by dir to the next available choice, staying put at either end.
func stepChoice(choices []flow.DialogueChoice, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(choices); i += dir {
		if choices[i].Available {
			return i
		}
	}
	return from
}
