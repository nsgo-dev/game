package vn_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/vn"
)

const step = 10 * time.Millisecond

const sceneYarn = `
title: Start
---
<<show aiko>>
<<show ren right doubt>>
Aiko: Welcome to the observatory. #line:l1 #voice:v1
Ren: It is smaller than I pictured. #line:l2
Aiko: But the sky is the same size. #expr:smile #line:l3 #voice:v3
The dome creaks open. #line:l4
<<play_sfx creak>>
-> Look through the telescope #line:c1
    Aiko: Careful, the lens is old. #expr:worry #line:l5
-> Ask about the logbook #line:c2
    Ren: Who kept it? #at:center #line:l6
<<hide aiko>>
Aiko: I will get the tea. #offstage #line:l7
Ren: Thank you. #expr:smile #line:l8
===
`

type fakeVoices struct {
	lengths map[string]time.Duration
	played  []string
	stops   int
}

func (v *fakeVoices) PlayVoice(id string) (time.Duration, bool) {
	l, ok := v.lengths[id]
	if ok {
		v.played = append(v.played, id)
	}
	return l, ok
}

func (v *fakeVoices) StopVoice() { v.stops++ }

func newPresenter(t testing.TB, src string, opts vn.Options) *vn.Presenter {
	t.Helper()
	script, err := flow.ParseYarn(src)
	if err != nil {
		t.Fatal(err)
	}
	p := vn.New(script, mustCast(t), opts)
	if err := p.Start("Start"); err != nil {
		t.Fatal(err)
	}
	return p
}

func lineText(t testing.TB, p *vn.Presenter) string {
	t.Helper()
	l, ok := p.Line()
	if !ok {
		t.Fatal("no line shown")
	}
	return l.Text
}

// next completes the line shown and goes on.
func next(t testing.TB, p *vn.Presenter) {
	t.Helper()
	if !p.Typewriter().Done() {
		if err := p.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
}

type stage map[string]string

func stageOf(p *vn.Presenter) stage {
	s := stage{}
	for _, pt := range p.Portraits() {
		s[pt.Character] = pt.Slot + "/" + pt.Expression
	}
	return s
}

func sameStage(a, b stage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestPortraitsFollowCommandsAndTags(t *testing.T) {
	var commands []string
	p := newPresenter(t, sceneYarn, vn.Options{Dialogue: flow.DialogueOptions{OnCommand: func(ev flow.DialogueEvent) {
		commands = append(commands, ev.Command)
	}}})

	if got, want := stageOf(p), (stage{"aiko": "left/neutral", "ren": "right/doubt"}); !sameStage(got, want) {
		t.Fatalf("stage = %v, want %v", got, want)
	}
	l, _ := p.Line()
	if l.Character != "aiko" || l.Name != "Aiko" || l.Color != "#e07a5f" || l.Voice != "v1" {
		t.Fatalf("line = %+v", l)
	}
	for _, pt := range p.Portraits() {
		if pt.Speaking != (pt.Character == "aiko") {
			t.Fatalf("portrait %s speaking = %v", pt.Character, pt.Speaking)
		}
	}

	next(t, p) // Ren keeps doubt.
	if got := stageOf(p)["ren"]; got != "right/doubt" {
		t.Fatalf("ren = %q", got)
	}
	next(t, p) // #expr:smile
	if got := stageOf(p)["aiko"]; got != "left/smile" {
		t.Fatalf("aiko = %q", got)
	}
	next(t, p) // narration
	if l, _ := p.Line(); l.Name != "" || l.Character != "" {
		t.Fatalf("narration line = %+v", l)
	}
	for _, pt := range p.Portraits() {
		if pt.Speaking {
			t.Fatalf("%s speaks during narration", pt.Character)
		}
	}
	next(t, p) // choices
	if got := p.Choices(); len(got) != 2 {
		t.Fatalf("choices = %v", got)
	}
	if !slices.Equal(commands, []string{"play_sfx"}) {
		t.Fatalf("user commands = %v, want only play_sfx", commands)
	}
	if err := p.Advance(); !errors.Is(err, flow.ErrChoiceRequired) {
		t.Fatalf("Advance at choices = %v", err)
	}
	if err := p.Choose(1); err != nil {
		t.Fatal(err)
	}
	// #at:center moves Ren to the center.
	if got, want := stageOf(p), (stage{"aiko": "left/smile", "ren": "center/doubt"}); !sameStage(got, want) {
		t.Fatalf("stage = %v, want %v", got, want)
	}
	next(t, p) // <<hide aiko>>, then an offstage line by Aiko.
	if got, want := stageOf(p), (stage{"ren": "center/doubt"}); !sameStage(got, want) {
		t.Fatalf("stage = %v, want %v", got, want)
	}
	if l, _ := p.Line(); l.Name != "Aiko" {
		t.Fatalf("offstage line keeps its name plate, got %+v", l)
	}
	next(t, p)
	if got := stageOf(p)["ren"]; got != "center/smile" {
		t.Fatalf("ren = %q", got)
	}
	next(t, p)
	if !p.Ended() {
		t.Fatal("scene did not end")
	}
}

func TestShowInOccupiedSlotReplacesPortrait(t *testing.T) {
	p := newPresenter(t, "title: Start\n---\n<<show aiko left>>\n<<show ren left smile>>\nRen: Mine now.\n===\n", vn.Options{})
	if got, want := stageOf(p), (stage{"ren": "left/smile"}); !sameStage(got, want) {
		t.Fatalf("stage = %v, want %v", got, want)
	}
}

func TestStageCommandErrors(t *testing.T) {
	for name, src := range map[string]string{
		"unknown character": "<<show kai>>",
		"unknown slot":      "<<show aiko nowhere>>",
		"bad expression":    "<<expr aiko angry>>",
		"hide two":          "<<hide aiko ren>>",
		"tag off cast":      "Narrator: Hi. #expr:smile",
		"bad tag slot":      "Aiko: Hi. #at:roof",
	} {
		t.Run(name, func(t *testing.T) {
			script, err := flow.ParseYarn("title: Start\n---\n" + src + "\nAiko: Next.\n===\n")
			if err != nil {
				t.Fatal(err)
			}
			p := vn.New(script, mustCast(t), vn.Options{})
			if err := p.Start("Start"); err == nil {
				t.Fatal("Start reported no error")
			}
		})
	}
}

func TestTypewriterRevealsAndAdvanceCompletes(t *testing.T) {
	p := newPresenter(t, sceneYarn, vn.Options{CharsPerSecond: 100})
	tw := p.Typewriter()
	if tw.VisibleCount() != 0 {
		t.Fatalf("visible = %d at start", tw.VisibleCount())
	}
	for range 10 {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
	}
	if got := tw.VisibleCount(); got != 10 {
		t.Fatalf("visible after 10 steps at 100/s = %d, want 10", got)
	}
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	if !tw.Done() || lineText(t, p) != "Welcome to the observatory." {
		t.Fatal("Advance did not complete the line in place")
	}
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	if lineText(t, p) != "It is smaller than I pictured." {
		t.Fatalf("line = %q", lineText(t, p))
	}
}

func TestAutoWaitsForTextVoiceAndDelay(t *testing.T) {
	voices := &fakeVoices{lengths: map[string]time.Duration{"v1": time.Second, "v3": time.Second}}
	p := newPresenter(t, sceneYarn, vn.Options{
		Voices:         voices,
		CharsPerSecond: -1,
		AutoDelay:      500 * time.Millisecond,
	})
	if !slices.Equal(voices.played, []string{"v1"}) {
		t.Fatalf("played = %v", voices.played)
	}
	p.SetAuto(true)
	steps := 0
	for lineText(t, p) == "Welcome to the observatory." {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
		steps++
		if steps > 1000 {
			t.Fatal("auto never went on")
		}
	}
	// One second of voice, then half a second of delay, at 100 steps a second.
	if steps != 150 {
		t.Fatalf("auto went on after %d steps, want 150", steps)
	}
	// Without a voice line, only the delay.
	steps = 0
	for lineText(t, p) == "It is smaller than I pictured." {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
		steps++
	}
	if steps != 50 {
		t.Fatalf("auto went on after %d steps, want 50", steps)
	}
	// Auto runs on to the choices and waits there.
	for range 600 {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Choices()) != 2 || !p.Auto() {
		t.Fatalf("auto should wait at the choices, choices = %v", p.Choices())
	}
	if !slices.Equal(voices.played, []string{"v1", "v3"}) {
		t.Fatalf("played = %v", voices.played)
	}
}

func TestAutoDelayPerRune(t *testing.T) {
	p := newPresenter(t, "title: Start\n---\nAiko: Four\nAiko: Next\n===\n", vn.Options{
		CharsPerSecond:   -1,
		AutoDelay:        -1,
		AutoDelayPerRune: 100 * time.Millisecond,
	})
	p.SetAuto(true)
	steps := 0
	for lineText(t, p) == "Four" {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
		steps++
	}
	if steps != 40 {
		t.Fatalf("went on after %d steps, want 40 (four runes at 100ms)", steps)
	}
}

func TestSkipStopsOnUnreadLines(t *testing.T) {
	voices := &fakeVoices{lengths: map[string]time.Duration{"v1": time.Second, "v3": time.Second}}
	p := newPresenter(t, sceneYarn, vn.Options{Voices: voices})
	// The line shown was never read before, so skip does not pass it.
	p.SetSkip(true)
	if err := p.Update(step); err != nil {
		t.Fatal(err)
	}
	if p.Skipping() || lineText(t, p) != "Welcome to the observatory." {
		t.Fatal("skip went past an unread line")
	}

	// From a read line, skip stops on the next unread one and shows it as usual.
	p.MarkRead("l1")
	if err := p.Start("Start"); err != nil {
		t.Fatal(err)
	}
	p.SetSkip(true)
	if err := p.Update(step); err != nil {
		t.Fatal(err)
	}
	if p.Skipping() {
		t.Fatal("skip went on into unread lines")
	}
	if lineText(t, p) != "It is smaller than I pictured." || p.Typewriter().Done() {
		t.Fatal("skip should stop on the unread line and reveal it as usual")
	}
}

func TestSkipRunsThroughReadLinesToChoices(t *testing.T) {
	first := newPresenter(t, sceneYarn, vn.Options{})
	for len(first.Choices()) == 0 {
		next(t, first)
	}
	read := first.ReadKeys()
	if !slices.Equal(read, []string{"l1", "l2", "l3", "l4"}) {
		t.Fatalf("read = %v", read)
	}

	voices := &fakeVoices{lengths: map[string]time.Duration{"v1": time.Second, "v3": time.Second}}
	script, err := flow.ParseYarn(sceneYarn)
	if err != nil {
		t.Fatal(err)
	}
	p := vn.New(script, mustCast(t), vn.Options{Voices: voices, SkipInterval: 50 * time.Millisecond})
	p.MarkRead(read...)
	if err := p.Start("Start"); err != nil {
		t.Fatal(err)
	}
	p.SetSkip(true)
	updates := 0
	for p.Skipping() {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
		if l, ok := p.Line(); ok && l.Text != "Welcome to the observatory." && !p.Typewriter().Done() {
			t.Fatalf("skipped line %q is not shown whole", l.Text)
		}
		updates++
	}
	if len(p.Choices()) != 2 {
		t.Fatal("skip should stop at the choices")
	}
	// Three lines and the choices at one per 50ms, a skip step every five updates.
	if updates != 20 {
		t.Fatalf("skip took %d updates, want 20", updates)
	}
	if !slices.Equal(voices.played, []string{"v1"}) {
		t.Fatalf("skip played voices: %v", voices.played)
	}
}

func TestSkipUnreadRunsThroughEverything(t *testing.T) {
	p := newPresenter(t, sceneYarn, vn.Options{SkipUnread: true})
	p.SetSkip(true)
	for range 10 {
		if err := p.Update(step); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Choices()) != 2 || p.Skipping() {
		t.Fatal("skip should run to the choices and stop")
	}
	if err := p.Choose(0); err != nil {
		t.Fatal(err)
	}
	p.SetSkip(true)
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	if p.Skipping() {
		t.Fatal("Advance should stop skip mode")
	}
}

func TestBacklog(t *testing.T) {
	voices := &fakeVoices{lengths: map[string]time.Duration{"v1": time.Second, "v3": time.Second}}
	p := newPresenter(t, sceneYarn, vn.Options{Voices: voices})
	for len(p.Choices()) == 0 {
		next(t, p)
	}
	if err := p.Choose(0); err != nil {
		t.Fatal(err)
	}
	log := p.Backlog()
	want := []vn.BacklogEntry{
		{Character: "aiko", Name: "Aiko", Color: "#e07a5f", Text: "Welcome to the observatory.", LineID: "l1", Voice: "v1"},
		{Character: "ren", Name: "Ren", Color: "#81b29a", Text: "It is smaller than I pictured.", LineID: "l2"},
		{Character: "aiko", Name: "Aiko", Color: "#e07a5f", Text: "But the sky is the same size.", LineID: "l3", Voice: "v3"},
		{Text: "The dome creaks open.", LineID: "l4"},
		{Text: "Look through the telescope", LineID: "c1", Choice: true},
		{Character: "aiko", Name: "Aiko", Color: "#e07a5f", Text: "Careful, the lens is old.", LineID: "l5"},
	}
	if !slices.Equal(log, want) {
		t.Fatalf("backlog =\n%+v\nwant\n%+v", log, want)
	}
	voices.played = nil
	if !p.ReplayVoice(2) || !slices.Equal(voices.played, []string{"v3"}) {
		t.Fatalf("ReplayVoice(2) played %v", voices.played)
	}
	if p.ReplayVoice(1) || p.ReplayVoice(99) {
		t.Fatal("ReplayVoice played an entry without a voice line")
	}
}

func TestBacklogSizeDropsOldest(t *testing.T) {
	p := newPresenter(t, sceneYarn, vn.Options{BacklogSize: 2})
	next(t, p)
	next(t, p)
	log := p.Backlog()
	if len(log) != 2 || log[0].LineID != "l2" || log[1].LineID != "l3" {
		t.Fatalf("backlog = %+v", log)
	}
	none := newPresenter(t, sceneYarn, vn.Options{BacklogSize: -1})
	if len(none.Backlog()) != 0 {
		t.Fatal("a negative size keeps entries")
	}
}

func TestVoiceFromLineIDAndStopOnAdvance(t *testing.T) {
	voices := &fakeVoices{lengths: map[string]time.Duration{"l2": time.Second}}
	p := newPresenter(t, sceneYarn, vn.Options{Voices: voices, VoiceFromLineID: true})
	stops := voices.stops
	next(t, p)
	if voices.stops == stops {
		t.Fatal("going on did not stop the voice")
	}
	if !slices.Equal(voices.played, []string{"l2"}) {
		t.Fatalf("played = %v; the #voice: tag wins over the line id", voices.played)
	}
}

func TestStartKeepsBacklogAndReadLines(t *testing.T) {
	p := newPresenter(t, sceneYarn, vn.Options{})
	next(t, p)
	if err := p.Start("Start"); err != nil {
		t.Fatal(err)
	}
	if l, _ := p.Line(); !l.Read {
		t.Fatal("the first line should read as read on the second play")
	}
	if len(p.Backlog()) != 3 {
		t.Fatalf("backlog = %d entries", len(p.Backlog()))
	}
}

func TestStartUnknownNodeKeepsState(t *testing.T) {
	p := newPresenter(t, sceneYarn, vn.Options{CharsPerSecond: -1})
	before := stageOf(p)
	if err := p.Start("Typo"); !errors.Is(err, flow.ErrUnknownNode) {
		t.Fatalf("Start(Typo) = %v, want ErrUnknownNode", err)
	}
	if got := lineText(t, p); got != "Welcome to the observatory." {
		t.Fatalf("line = %q, want the first line still shown", got)
	}
	if !sameStage(stageOf(p), before) {
		t.Fatalf("stage = %v, want %v", stageOf(p), before)
	}
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	if got := lineText(t, p); got != "It is smaller than I pictured." {
		t.Fatalf("line = %q, want the second line", got)
	}
}

func TestEffectsKeepAnimatingAfterReveal(t *testing.T) {
	src := "title: Start\n---\nAiko: [wave]Hi[/wave] #line:a\n-> Go #line:c\n    Ren: Ok. #line:b\n===\n"
	p := newPresenter(t, src, vn.Options{CharsPerSecond: -1})
	tw := p.Typewriter()
	if !tw.Done() {
		t.Fatal("a negative rate should show the line at once")
	}
	e0 := tw.Elapsed()
	if err := p.Update(step); err != nil {
		t.Fatal(err)
	}
	if tw.Elapsed() <= e0 {
		t.Fatal("the typewriter's clock stopped once the line had appeared")
	}
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	if len(p.Choices()) != 1 {
		t.Fatalf("choices = %v", p.Choices())
	}
	e1 := tw.Elapsed()
	if err := p.Update(step); err != nil {
		t.Fatal(err)
	}
	if tw.Elapsed() <= e1 {
		t.Fatal("the typewriter's clock stopped while choices wait")
	}
}

func TestChooseStopsVoice(t *testing.T) {
	src := "title: Start\n---\nAiko: Pick one. #line:q #voice:vq\n-> A #line:ca\n    Ren: Fine. #line:a\n-> B #line:cb\n    Ren: Also fine. #line:b\n===\n"
	voices := &fakeVoices{lengths: map[string]time.Duration{"vq": time.Second}}
	p := newPresenter(t, src, vn.Options{Voices: voices, CharsPerSecond: -1, AutoDelay: -1})
	if err := p.Advance(); err != nil {
		t.Fatal(err)
	}
	// The player replays the question's voice from the backlog while the choices wait.
	if !p.ReplayVoice(0) {
		t.Fatal("ReplayVoice(0) did not play")
	}
	stops := voices.stops
	if err := p.Choose(0); err != nil {
		t.Fatal(err)
	}
	if voices.stops == stops {
		t.Fatal("choosing did not stop the voice playing")
	}
	if got := lineText(t, p); got != "Fine." {
		t.Fatalf("line = %q", got)
	}
	p.SetAuto(true)
	if err := p.Update(step); err != nil {
		t.Fatal(err)
	}
	if !p.Ended() {
		t.Fatal("auto mode waited on a voice from before the choice")
	}
}

func BenchmarkPresenterUpdate(b *testing.B) {
	script, err := flow.ParseYarn(sceneYarn)
	if err != nil {
		b.Fatal(err)
	}
	cast := mustCast(b)
	b.ReportAllocs()
	for b.Loop() {
		p := vn.New(script, cast, vn.Options{SkipUnread: true})
		if err := p.Start("Start"); err != nil {
			b.Fatal(err)
		}
		p.SetSkip(true)
		for p.Skipping() {
			if err := p.Update(step); err != nil {
				b.Fatal(err)
			}
		}
	}
}
