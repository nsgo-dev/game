package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/vn"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	frameStep   = time.Second / 60
	voiceLength = 600 * time.Millisecond
)

func startSample(t *testing.T) (*nsgotest.App, *sample) {
	t.Helper()
	durations := map[string]time.Duration{}
	for i := range voiceIDs {
		durations[string(voiceWAV(i))] = voiceLength
	}
	var s *sample
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		s, node = newSample(cx)
		return node
	},
		nsgotest.Size(width, height),
		nsgotest.Environment(core.Environment{Audio: true}),
		func(a *nsgotest.App) { a.AudioDurations = durations },
	)
	app.Advance(frameStep)
	return app, s
}

// runUntil advances the app a frame at a time until cond holds, failing after limit frames, and returns the frames it took.
func runUntil(t *testing.T, app *nsgotest.App, limit int, cond func() bool) int {
	t.Helper()
	for n := 0; n < limit; n++ {
		if cond() {
			return n
		}
		app.Advance(frameStep)
	}
	t.Fatalf("condition not met after %d frames", limit)
	return limit
}

func lineText(s *sample) string {
	l, _ := s.p.Line()
	return l.Text
}

func portraitsOf(s *sample) map[string]string {
	m := map[string]string{}
	for _, p := range s.p.Portraits() {
		m[p.Character] = p.Slot + "/" + p.Expression
	}
	return m
}

func pressChoice(t *testing.T, app *nsgotest.App, s *sample, i int) {
	t.Helper()
	b := s.style.Layout(s.size, len(s.p.Choices())).Choices[i]
	app.PressAt("stage", core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2})
}

func canvasTexts(app *nsgotest.App) map[string]bool {
	texts := map[string]bool{}
	for _, op := range app.Find("canvas").Ops {
		if op.Kind == core.DrawFillText {
			texts[op.Text] = true
		}
	}
	return texts
}

func TestSampleScenePlaysWithPortraitsBacklogAutoAndSkip(t *testing.T) {
	app, s := startSample(t)

	// Portraits enter with their expressions and the speaker's name plate shows.
	if got := portraitsOf(s); got["aiko"] != "left/neutral" || got["ren"] != "right/doubt" || len(got) != 2 {
		t.Fatalf("portraits = %v", got)
	}
	runUntil(t, app, 600, func() bool { return s.p.Typewriter().Done() })
	if !canvasTexts(app)["Aiko"] {
		t.Fatal("no name plate for Aiko")
	}

	// A tap goes on; a tap during the typewriter shows the whole line.
	app.Press("stage")
	if lineText(s) != "It is smaller than I pictured." || s.p.Typewriter().Done() {
		t.Fatalf("line = %q", lineText(s))
	}
	app.Press("stage")
	if !s.p.Typewriter().Done() {
		t.Fatal("a tap did not show the whole line")
	}

	// Auto mode plays on, waiting for each voice line, up to the choices.
	app.Press("auto")
	if !s.p.Auto() {
		t.Fatal("Auto did not turn auto mode on")
	}
	frames := runUntil(t, app, 60*30, func() bool { return len(s.p.Choices()) > 0 })
	// Two more lines, one voiced: at least the voice and two delays.
	if min := int((voiceLength + 2*time.Second) / frameStep); frames < min {
		t.Fatalf("auto reached the choices in %d frames, under the %d its waits take", frames, min)
	}
	if got := portraitsOf(s)["aiko"]; got != "left/smile" {
		t.Fatalf("aiko = %q after #expr:smile", got)
	}

	// Pick the logbook: Ren steps to the center.
	pressChoice(t, app, s, 1)
	if got := portraitsOf(s)["ren"]; got != "center/doubt" {
		t.Fatalf("ren = %q after #at:center", got)
	}

	// The backlog holds every line and the choice, and the Log button shows it.
	log := s.p.Backlog()
	if len(log) != 6 || !log[4].Choice || log[4].Text != "Ask about the logbook" {
		t.Fatalf("backlog = %+v", log)
	}
	app.Press("log")
	app.Advance(frameStep)
	if texts := canvasTexts(app); !texts["Welcome to the old observatory."] || !texts["> Ask about the logbook"] {
		t.Fatalf("backlog drew %v", texts)
	}
	app.Press("stage")
	app.Advance(frameStep)
	if canvasTexts(app)["> Ask about the logbook"] {
		t.Fatal("a tap did not close the backlog")
	}

	// Auto runs on to the last choice; watch the scene again.
	runUntil(t, app, 60*60, func() bool { return len(s.p.Choices()) > 0 })
	if got := portraitsOf(s); got["aiko"] != "left/smile" || got["ren"] != "center/smile" {
		t.Fatalf("portraits = %v", got)
	}
	app.Press("auto")
	pressChoice(t, app, s, 0)
	if l, _ := s.p.Line(); !l.Read || lineText(s) != "Welcome to the old observatory." {
		t.Fatalf("replay starts at %+v", l)
	}
	if got := portraitsOf(s); got["aiko"] != "left/neutral" || got["ren"] != "right/doubt" {
		t.Fatalf("portraits = %v after <<hide>> and <<show>>", got)
	}

	// Skip runs through the lines already read, without waiting, to the first choice.
	app.Press("skip")
	if !s.p.Skipping() {
		t.Fatal("Skip did not turn skip mode on")
	}
	frames = runUntil(t, app, 60*5, func() bool { return len(s.p.Choices()) > 0 })
	if frames > 30 {
		t.Fatalf("skip took %d frames over three lines", frames)
	}
	if s.p.Skipping() {
		t.Fatal("skip should stop at the choices")
	}

	// The telescope branch was never read, so skip stops on its first line.
	pressChoice(t, app, s, 0)
	app.Press("skip")
	app.Advance(frameStep)
	if s.p.Skipping() || lineText(s) != "Careful, the lens is older than both of us." {
		t.Fatalf("skip went past an unread line to %q", lineText(s))
	}

	// Play to the end and leave.
	runUntil(t, app, 60*60, func() bool {
		if len(s.p.Choices()) > 0 {
			return true
		}
		app.Press("stage")
		return false
	})
	pressChoice(t, app, s, 1)
	runUntil(t, app, 600, func() bool {
		if s.p.Ended() {
			return true
		}
		app.Press("stage")
		return false
	})
	if len(s.p.Portraits()) != 0 {
		t.Fatalf("portraits = %v after <<hide>>", s.p.Portraits())
	}
	app.Advance(frameStep)
	if !canvasTexts(app)["The end"] {
		t.Fatal("the end was not drawn")
	}
}

func TestSampleFilesRoundTrip(t *testing.T) {
	cast, err := vn.ParseCast(castYAML)
	if err != nil {
		t.Fatal(err)
	}
	out, err := vn.EncodeCast(cast)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(castYAML) {
		t.Fatalf("cast.yaml is not in canonical form:\n%s", out)
	}
}
