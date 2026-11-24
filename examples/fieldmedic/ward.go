package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// StepRate is the fixed update rate of the ward's game.Loop.
const StepRate = 60

const (
	// BleedSteps is how many fixed steps a patient takes to lose one hit point.
	BleedSteps = 90
	// HealAmount is how many hit points one press heals, up to the patient's max.
	HealAmount = 3
	// PatientSize is the side of a patient's square in scene points, centered on its position.
	PatientSize = 24
)

// Patient is an object of the ward scene with a health component.
type Patient struct {
	// ID is the object's ID in the scene file.
	ID string
	// Name is the object's display name, or its ID when the scene gives none.
	Name string
	// X and Y are the center of the patient's square in scene points.
	X, Y float32
	// HP and Max are the patient's hit points and the most healing brings it to.
	HP, Max int
}

// Lost reports whether the patient has no hit points left.
func (p Patient) Lost() bool { return p.HP <= 0 }

// Ward is the game's state: the patients of the scene, who lose a hit point every BleedSteps, staggered so they do not all bleed on the same step, and the steps the player has kept at least one of them alive.
type Ward struct {
	// Width and Height are the scene's virtual size in points.
	Width, Height float32
	// Background is the scene's background color.
	Background core.Color
	// Patients are the scene's top-level objects with a health component, in document order.
	Patients []Patient
	// Steps counts the fixed steps run since the ward was loaded or reset; it stops once every patient is lost.
	Steps int

	start []Patient
}

// LoadWard parses a scene file, checking its components against health.Components, and returns the ward its patients make. file names the scene in errors. A scene without patients is refused.
func LoadWard(data []byte, file string) (*Ward, error) {
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: file, Components: health.Components()})
	if err != nil {
		return nil, err
	}
	w := &Ward{Width: doc.VirtualWidth, Height: doc.VirtualHeight, Background: core.RGB(0x10141F)}
	if doc.BackgroundColor != "" {
		if c, err := core.ParseColor(doc.BackgroundColor); err == nil {
			w.Background = c
		}
	}
	if w.Width <= 0 || w.Height <= 0 {
		return nil, fmt.Errorf("%s: the scene needs a virtualWidth and virtualHeight", file)
	}
	for _, layer := range doc.Layers {
		for i := range layer.Objects {
			o := &layer.Objects[i]
			s, ok, err := health.Read(o)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if !ok {
				continue
			}
			name := o.Name
			if name == "" {
				name = o.ID
			}
			w.start = append(w.start, Patient{ID: o.ID, Name: name, X: o.X, Y: o.Y, HP: s.HP, Max: s.Max})
		}
	}
	if len(w.start) == 0 {
		return nil, fmt.Errorf("%s: no object has a %s component", file, health.Name)
	}
	w.Reset()
	return w, nil
}

// Reset puts every patient back to the hit points the scene gives it and the step count to 0.
func (w *Ward) Reset() {
	w.Patients = append(w.Patients[:0], w.start...)
	w.Steps = 0
}

// Over reports whether every patient is lost.
func (w *Ward) Over() bool {
	for _, p := range w.Patients {
		if !p.Lost() {
			return false
		}
	}
	return true
}

// LostCount returns how many patients are lost.
func (w *Ward) LostCount() int {
	n := 0
	for _, p := range w.Patients {
		if p.Lost() {
			n++
		}
	}
	return n
}

// Survived returns how long the player has kept a patient alive: the steps run, at StepRate.
func (w *Ward) Survived() time.Duration {
	return time.Duration(w.Steps) * time.Second / StepRate
}

// Step runs one fixed update: patient i loses a hit point on the steps where Steps+i*BleedSteps/len(Patients) is a multiple of BleedSteps. It does nothing once the ward is over.
func (w *Ward) Step(time.Duration) {
	if w.Over() {
		return
	}
	w.Steps++
	n := len(w.Patients)
	for i := range w.Patients {
		p := &w.Patients[i]
		if !p.Lost() && (w.Steps+i*BleedSteps/n)%BleedSteps == 0 {
			p.HP--
		}
	}
}

// PatientAt returns the index of the patient whose square holds the scene point (x, y), the last in document order when squares overlap, and -1 when none does.
func (w *Ward) PatientAt(x, y float32) int {
	const half = PatientSize / 2
	for i := len(w.Patients) - 1; i >= 0; i-- {
		p := w.Patients[i]
		if x >= p.X-half && x <= p.X+half && y >= p.Y-half && y <= p.Y+half {
			return i
		}
	}
	return -1
}

// HealAt heals the patient at the scene point (x, y) by HealAmount, up to its max, and reports whether it healed one. A lost patient cannot be healed, and nothing heals once the ward is over.
func (w *Ward) HealAt(x, y float32) bool {
	i := w.PatientAt(x, y)
	if i < 0 || w.Patients[i].Lost() {
		return false
	}
	p := &w.Patients[i]
	p.HP = min(p.Max, p.HP+HealAmount)
	return true
}

// Fit returns the scale and offset that fit the scene, letterboxed, into a canvas of size.
func (w *Ward) Fit(size core.Size) (scale, ox, oy float32) {
	scale = min(float32(size.Width)/w.Width, float32(size.Height)/w.Height)
	if scale <= 0 {
		return 0, 0, 0
	}
	return scale, (float32(size.Width) - w.Width*scale) / 2, (float32(size.Height) - w.Height*scale) / 2
}

// ToScene converts a point on a canvas of size to scene points.
func (w *Ward) ToScene(size core.Size, at core.Point) (float32, float32) {
	scale, ox, oy := w.Fit(size)
	if scale == 0 {
		return -1, -1
	}
	return (float32(at.X) - ox) / scale, (float32(at.Y) - oy) / scale
}

// Patient colors by how many hit points are left.
var (
	colorWell     = core.RGB(0x22C55E)
	colorHurt     = core.RGB(0xF59E0B)
	colorCritical = core.RGB(0xEF4444)
	colorLost     = core.RGB(0x475569)
	colorBarBack  = core.RGB(0x1E293B)
	colorLabel    = core.RGB(0xE2E8F0)
)

// PatientColor returns the color a patient is drawn in: green above half its max, amber above a quarter, red below and grey once lost.
func PatientColor(p Patient) core.Color {
	switch {
	case p.Lost():
		return colorLost
	case p.HP*2 > p.Max:
		return colorWell
	case p.HP*4 > p.Max:
		return colorHurt
	default:
		return colorCritical
	}
}

// Draw draws the ward letterboxed into a canvas of size: each patient as a square in its color, a hit point bar above it and its name below.
func (w *Ward) Draw(d *core.Drawing, size core.Size) {
	d.SetFillColor(core.RGB(0x05070D)).FillRect(0, 0, float32(size.Width), float32(size.Height))
	scale, ox, oy := w.Fit(size)
	if scale == 0 {
		return
	}
	d.Save().Translate(ox, oy).Scale(scale, scale)
	d.SetFillColor(w.Background).FillRect(0, 0, w.Width, w.Height)
	const half = PatientSize / 2
	font := core.Font{Size: 7}
	d.SetTextAlign(core.TextAlignCenter)
	for _, p := range w.Patients {
		d.SetFillColor(PatientColor(p)).FillRect(p.X-half, p.Y-half, PatientSize, PatientSize)
		d.SetFillColor(colorBarBack).FillRect(p.X-half, p.Y-half-6, PatientSize, 3)
		if p.Max > 0 && p.HP > 0 {
			d.SetFillColor(PatientColor(p)).FillRect(p.X-half, p.Y-half-6, PatientSize*float32(p.HP)/float32(p.Max), 3)
		}
		d.SetFillColor(colorLabel).FillText(p.Name, p.X, p.Y+half+9, font)
	}
	d.Restore()
}
