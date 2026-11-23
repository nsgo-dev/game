package main

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

//go:embed cast.yaml
var castYAML []byte

//go:embed scene.yarn
var sceneYarn string

const (
	width  = 720
	height = 480

	portraitW = 120
	portraitH = 220
)

// voiceIDs are the scene's voice lines, each synthesized as a short murmur.
var voiceIDs = []string{"aiko_01", "aiko_02", "aiko_03", "aiko_04", "aiko_05", "aiko_06", "ren_01", "ren_02", "ren_03", "ren_04"}

// sample is the running scene: the presenter, the style it draws in and whether the backlog is open.
type sample struct {
	p       *vn.Presenter
	style   vn.Style
	size    core.Size
	backlog *core.Signal[bool]
	auto    *core.Signal[bool]
	skip    *core.Signal[bool]
	frame   *core.Signal[uint64]
}

// Root is the sample app's root view.
func Root(cx *core.Scope) core.Node {
	_, node := newSample(cx)
	return node
}

func newSample(cx *core.Scope) (*sample, core.Node) {
	cast, err := vn.ParseCast(castYAML)
	if err != nil {
		panic(err)
	}
	script, err := flow.ParseYarn(sceneYarn)
	if err != nil {
		panic(err)
	}
	atlas := portraitAtlas(cast)
	if err := cast.CheckFrames(atlas); err != nil {
		panic(err)
	}

	mixer := sound.NewMixer(cx, "vnsample.sound")
	voices := &vn.EffectVoices{Clips: map[string]vn.VoiceClip{}}
	for i, id := range voiceIDs {
		audio.LoadSound(cx, audio.Source{Data: voiceWAV(i)}, func(snd *audio.Sound, err error) {
			if err == nil && snd != nil {
				voices.Clips[id] = vn.VoiceClip{Effect: mixer.Effect(snd, "voice", 1), Length: snd.Duration()}
			}
		})
	}

	s := &sample{
		p: vn.New(script, cast, vn.Options{
			Voices:       voices,
			AutoDelay:    time.Second,
			SkipInterval: 80 * time.Millisecond,
		}),
		style: vn.Style{
			Atlas:          atlas,
			PortraitHeight: 300,
			InactiveAlpha:  0.55,
		},
		size:    core.Size{Width: width, Height: height},
		backlog: core.NewSignal(cx, false),
		auto:    core.NewSignal(cx, false),
		skip:    core.NewSignal(cx, false),
		frame:   core.NewSignal(cx, uint64(0)),
	}
	if err := s.p.Start("Start"); err != nil {
		panic(err)
	}

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			if err := s.p.Update(dt); err != nil {
				log.Print(err)
			}
			s.sync()
		},
		Draw: func(float64) { s.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	return s, s.tree()
}

// sync mirrors the presenter's modes into the signals the buttons read.
func (s *sample) sync() {
	s.auto.Set(s.p.Auto())
	s.skip.Set(s.p.Skipping())
}

func (s *sample) tree() core.Node {
	return core.View{
		Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Background: core.RGB(0x1d2540)},
		Children: []core.Node{
			core.View{
				TestID:  "stage",
				Style:   core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pct(100), Height: core.Pct(100)},
				OnPress: func(ev core.PressEvent) { s.press(ev.Position) },
				Children: []core.Node{
					core.Canvas{
						TestID: "canvas",
						Label:  "Visual novel stage",
						Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw:   s.draw,
					},
				},
			},
			core.View{
				Style: core.Style{Position: core.Absolute, Top: core.Pt(12), Right: core.Pt(12), Direction: core.Row, Gap: 8},
				Children: []core.Node{
					s.toggle("auto", "Auto", s.auto, func() { s.p.SetAuto(!s.p.Auto()) }),
					s.toggle("skip", "Skip", s.skip, func() { s.p.SetSkip(!s.p.Skipping()) }),
					s.toggle("log", "Log", s.backlog, func() { s.backlog.Set(!s.backlog.GetUntracked()) }),
				},
			},
		},
	}
}

func (s *sample) toggle(id, label string, on *core.Signal[bool], press func()) core.Node {
	return ui.Button{
		Text:    label,
		Variant: ui.ButtonSecondary,
		Size:    ui.SizeSmall,
		TestID:  id,
		Color: func() core.Color {
			if on.Get() {
				return core.RGB(0xe07a5f)
			}
			return core.RGB(0x3b4b6b)
		},
		OnPress: func() {
			press()
			s.sync()
		},
	}
}

// press is a tap on the stage: it closes the backlog, picks a choice, or goes on.
func (s *sample) press(at core.Point) {
	defer s.sync()
	if s.backlog.GetUntracked() {
		s.backlog.Set(false)
		return
	}
	if len(s.p.Choices()) > 0 {
		if i, ok := s.p.ChoiceAt(s.size, s.style, at); ok {
			if err := s.p.Choose(i); err != nil {
				log.Print(err)
			}
		}
		return
	}
	if err := s.p.Advance(); err != nil {
		log.Print(err)
	}
}

func (s *sample) draw(d *core.Drawing, size core.Size) {
	_ = s.frame.Get()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	s.size = size
	w, h := float32(size.Width), float32(size.Height)
	// A night sky behind the stage.
	d.Save().SetFillColor(core.RGB(0x1d2540)).FillRect(0, 0, w, h)
	d.SetFillColor(core.RGB(0x2c365a)).FillRect(0, h*0.62, w, h*0.38).Restore()
	if s.backlog.Get() {
		s.p.DrawBacklog(d, size, s.style, 0)
		return
	}
	s.p.Draw(d, size, s.style)
	if s.p.Ended() {
		d.Save().SetFillColor(core.RGB(0xffffff)).SetTextAlign(core.TextAlignCenter).
			FillText("The end", w/2, h/2, core.Font{Size: 24}).Restore()
	}
}

// portraitAtlas paints each character's expressions as frames of one atlas: a coat in the character's color under a face whose brows and mouth show the expression.
func portraitAtlas(cast *vn.Cast) *sprite.Atlas {
	coats := map[string]color.RGBA{"aiko": {0xe0, 0x7a, 0x5f, 0xff}, "ren": {0x3d, 0x8f, 0x7a, 0xff}}
	hair := map[string]color.RGBA{"aiko": {0x3a, 0x26, 0x1e, 0xff}, "ren": {0x1c, 0x22, 0x33, 0xff}}
	n := 0
	for _, ch := range cast.Characters {
		n += len(ch.Expressions)
	}
	img := image.NewRGBA(image.Rect(0, 0, portraitW*n, portraitH))
	atlas := &sprite.Atlas{Frames: map[string]sprite.Frame{}}
	i := 0
	for _, ch := range cast.Characters {
		for _, e := range ch.Expressions {
			x0 := i * portraitW
			paintPortrait(img, x0, coats[ch.ID], hair[ch.ID], e.Name)
			atlas.Frames[e.Frame] = sprite.Frame{Name: e.Frame, X: float32(x0), W: portraitW, H: portraitH, Width: portraitW, Height: portraitH}
			i++
		}
	}
	atlas.Image = core.ImageSource{Pixels: img, Version: 1}
	return atlas
}

func paintPortrait(img *image.RGBA, x0 int, coat, hair color.RGBA, expr string) {
	skin := color.RGBA{0xf2, 0xd0, 0xb5, 0xff}
	ink := color.RGBA{0x22, 0x1a, 0x18, 0xff}
	cx := x0 + portraitW/2
	// Coat: a rounded trapezoid from the shoulders down.
	for y := 130; y < portraitH; y++ {
		half := 30 + (y-130)/2
		for x := cx - half; x < cx+half; x++ {
			img.SetRGBA(x, y, coat)
		}
	}
	// Head and hair.
	disc(img, cx, 78, 44, hair)
	disc(img, cx, 86, 38, skin)
	for y := 40; y < 62; y++ {
		for x := cx - 40; x < cx+40; x++ {
			if dx, dy := x-cx, y-78; dx*dx+dy*dy <= 44*44 {
				img.SetRGBA(x, y, hair)
			}
		}
	}
	// Eyes.
	disc(img, cx-14, 86, 4, ink)
	disc(img, cx+14, 86, 4, ink)
	// Brows and mouth by expression.
	for d := -8; d <= 8; d++ {
		left, right := 74, 74
		switch expr {
		case "worry": // Inner ends raised.
			left, right = 74-d/3, 74+d/3
		case "doubt": // One brow raised.
			right = 70
		}
		img.SetRGBA(cx-14+d, left, ink)
		img.SetRGBA(cx+14+d, right, ink)
	}
	for d := -10; d <= 10; d++ {
		y := 106
		switch expr {
		case "smile":
			y += 4 - d*d/25
		case "worry":
			y += d * d / 25
		case "doubt":
			y += d / 4
		}
		img.SetRGBA(cx+d, y, ink)
		img.SetRGBA(cx+d, y+1, ink)
	}
}

func disc(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// voiceWAV synthesizes voice line i: a murmur of a few syllables, longer and lower for later lines.
func voiceWAV(i int) []byte {
	const rate = 22050
	syllables := 3 + i%4
	length := syllables * rate / 6
	samples := make([]float64, length)
	base := 220.0 + float64(i%5)*30
	for n := range samples {
		t := float64(n) / rate
		syl := n * syllables / length
		f := base * (1 + 0.08*float64(syl%3))
		env := math.Sin(math.Pi * float64(n%(length/syllables)) / float64(length/syllables))
		samples[n] = 0.25 * env * math.Sin(2*math.Pi*f*t)
	}
	wav, err := sound.EncodeWAV(samples, rate, 1)
	if err != nil {
		panic(fmt.Sprintf("vnsample: voice %d: %v", i, err))
	}
	return wav
}
