package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// The playfield's colors.
var (
	colorBackground = core.RGB(0x0b1020)
	colorLane       = core.RGB(0x121a33)
	colorLaneEdge   = core.RGBA(0x94a3b8, 0.18)
	colorBeatLine   = core.RGBA(0x94a3b8, 0.12)
	colorBarLine    = core.RGBA(0x94a3b8, 0.32)
	colorHitLine    = core.RGB(0xe2e8f0)
	// laneColors are each lane's notes, left to right.
	laneColors = [Lanes]core.Color{core.RGB(0x22d3ee), core.RGB(0xa78bfa), core.RGB(0xa78bfa), core.RGB(0x22d3ee)}
	// gradeColors are the glow of a hit of each grade.
	gradeColors = [len(grades)]core.Color{Perfect: core.RGB(0xfde047), Great: core.RGB(0x4ade80), Good: core.RGB(0x60a5fa), Miss: core.RGB(0xf87171)}
)

const (
	// fieldMaxWidth is the widest the playfield grows, centered in a wider window.
	fieldMaxWidth = 400
	// fieldMargin is the space kept either side of the playfield in a narrow window.
	fieldMargin = 12
	// hitLineAt is how far down the playfield the hit line is, as a fraction of its height.
	hitLineAt = 0.82
	// noteHeight is how tall a note is drawn.
	noteHeight = 18
)

// layout is where the playfield is in a view: its lanes' left edge and width, its height and the hit line.
type layout struct {
	x, laneW, height, hitY float32
}

// layoutFor lays the playfield out in a view of size: as wide as fits up to fieldMaxWidth, centered, the full height.
func layoutFor(size core.Size) layout {
	w, h := float32(size.Width), float32(size.Height)
	fw := max(min(w-2*fieldMargin, fieldMaxWidth), 0)
	return layout{x: (w - fw) / 2, laneW: fw / Lanes, height: h, hitY: h * hitLineAt}
}

// laneBounds returns lane's column, the whole height of the playfield, which a touch or a click in hits the lane.
func (l layout) laneBounds(lane int) core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(l.x + float32(lane)*l.laneW), Y: 0},
		Size:   core.Size{Width: core.Points(l.laneW), Height: core.Points(l.height)},
	}
}

// laneAt returns the lane at x, -1 outside the playfield.
func (l layout) laneAt(x core.Points) int {
	if l.laneW <= 0 {
		return -1
	}
	i := int(math.Floor(float64((float32(x) - l.x) / l.laneW)))
	if i < 0 || i >= Lanes {
		return -1
	}
	return i
}

// noteY returns where a note due in ahead is drawn: on the hit line at 0, at the top of the playfield an approach ahead, and below the line once late.
func (l layout) noteY(ahead time.Duration) float32 {
	return l.hitY - float32(ahead.Seconds()/approach.Seconds())*l.hitY
}

// drawField draws the chart being played at the clock's draw position: the lanes, the beat lines, the notes still to hit and each lane's glow.
func (g *rhythmGame) drawField(d *core.Drawing, size core.Size) {
	g.layoutField(size)
	l := layoutFor(size)
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(colorBackground).FillRect(0, 0, w, h)
	if g.run == nil {
		return
	}
	now := audio.Now(g.cx)
	pos := g.clock.DrawPosition(now)
	fw := l.laneW * Lanes
	d.SetFillColor(colorLane).FillRect(l.x, 0, fw, h)

	// A line on each beat and a brighter one on each bar, from the beat timing so they move with the music.
	timing := g.track.Timing
	first := math.Ceil(timing.Beat(pos - approach/4))
	for b := first; ; b++ {
		y := l.noteY(timing.Time(b) - pos)
		if y < 0 {
			break
		}
		c := colorBeatLine
		if _, in := timing.Bar(b); in == 0 {
			c = colorBarLine
		}
		d.SetFillColor(c).FillRect(l.x, y-0.5, fw, 1)
	}

	// Each lane glows for a moment after it is hit: in its grade's color for a judged hit, faintly for an empty tap.
	for lane, hit := range g.hits {
		age := now.Sub(hit.at)
		if hit.at.IsZero() || age < 0 || age >= hitFlash {
			continue
		}
		fade := 1 - float32(age)/float32(hitFlash)
		c, alpha := colorHitLine, 0.12*fade
		if hit.hit {
			c, alpha = gradeColors[hit.grade], 0.35*fade
		}
		d.SetAlpha(alpha).SetFillColor(c).FillRect(l.x+float32(lane)*l.laneW, 0, l.laneW, l.hitY+noteHeight)
		d.SetAlpha(1)
	}
	for lane := 1; lane < Lanes; lane++ {
		d.SetFillColor(colorLaneEdge).FillRect(l.x+float32(lane)*l.laneW-0.5, 0, 1, h)
	}

	// The notes not yet judged that are on screen, from the latest back so the nearest draws on top.
	r := g.run
	notes := r.Chart().Notes
	for i := len(notes) - 1; i >= 0; i-- {
		if r.Judged(i) {
			continue
		}
		ahead := r.NoteTime(i) - pos
		if ahead > approach {
			continue
		}
		y := l.noteY(ahead)
		if y > h+noteHeight {
			continue
		}
		x := l.x + float32(notes[i].Lane)*l.laneW
		d.SetFillColor(laneColors[notes[i].Lane]).BeginPath().RoundRect(x+4, y-noteHeight/2, l.laneW-8, noteHeight, 6).Fill()
	}

	d.SetFillColor(colorHitLine).FillRect(l.x, l.hitY-1.5, fw, 3)
}
