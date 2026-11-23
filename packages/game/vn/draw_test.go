package vn_test

import (
	"image"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func testAtlas(c *vn.Cast) *sprite.Atlas {
	atlas := &sprite.Atlas{
		Image:  core.ImageSource{Pixels: image.NewRGBA(image.Rect(0, 0, 20, 40)), Version: 1},
		Frames: map[string]sprite.Frame{},
	}
	for _, ch := range c.Characters {
		for _, e := range ch.Expressions {
			atlas.Frames[e.Frame] = sprite.Frame{Name: e.Frame, W: 20, H: 40, Width: 20, Height: 40}
		}
	}
	return atlas
}

func TestLayoutAndChoiceAt(t *testing.T) {
	size := core.Size{Width: 400, Height: 300}
	st := vn.Style{BoxHeight: 80, Margin: 10, Padding: 10, ChoiceWidth: 200, ChoiceHeight: 40, ChoiceGap: 10}
	l := st.Layout(size, 2)
	if l.Box.Origin.Y != 210 || l.Box.Size.Width != 380 || l.Text.Origin.X != 20 || l.Text.Size.Height != 60 {
		t.Fatalf("box = %+v, text = %+v", l.Box, l.Text)
	}
	if len(l.Choices) != 2 || l.Choices[0].Origin.X != 100 || l.Choices[0].Origin.Y != 60 || l.Choices[1].Origin.Y != 110 {
		t.Fatalf("choices = %+v", l.Choices)
	}

	p := newPresenter(t, sceneYarn, vn.Options{})
	for len(p.Choices()) == 0 {
		next(t, p)
	}
	if i, ok := p.ChoiceAt(size, st, core.Point{X: 150, Y: 120}); !ok || i != 1 {
		t.Fatalf("ChoiceAt(second) = %d, %v", i, ok)
	}
	if _, ok := p.ChoiceAt(size, st, core.Point{X: 10, Y: 10}); ok {
		t.Fatal("ChoiceAt found a choice off every button")
	}
}

func TestDrawRecordsStage(t *testing.T) {
	cast := mustCast(t)
	p := newPresenter(t, sceneYarn, vn.Options{CharsPerSecond: -1})
	st := vn.Style{Atlas: testAtlas(cast), PortraitHeight: 80, InactiveAlpha: 0.5}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "stage",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Draw:   func(d *core.Drawing, size core.Size) { p.Draw(d, size, st) },
			},
			core.Canvas{
				TestID: "log",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Draw:   func(d *core.Drawing, size core.Size) { p.DrawBacklog(d, size, st, 0) },
			},
		}}
	})
	ops := app.Find("stage").Ops
	images, texts, dimmed := 0, map[string]bool{}, false
	for _, op := range ops {
		switch op.Kind {
		case core.DrawImage:
			images++
		case core.DrawFillText:
			texts[op.Text] = true
		case core.DrawAlpha:
			dimmed = dimmed || op.A == 0.5
		}
	}
	if images != 2 {
		t.Fatalf("drew %d portraits, want 2", images)
	}
	if !dimmed {
		t.Fatal("the portrait not speaking was not dimmed")
	}
	if !texts["Aiko"] {
		t.Fatalf("no name plate among %v", texts)
	}
	if !texts["Welcome to the observatory."] && len(texts) < 2 {
		t.Fatalf("no line text among %v", texts)
	}
	logged := false
	for _, op := range app.Find("log").Ops {
		logged = logged || (op.Kind == core.DrawFillText && op.Text == "Aiko")
	}
	if !logged {
		t.Fatal("the backlog drew no entry")
	}
}
