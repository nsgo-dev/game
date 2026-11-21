//go:build !release

package debug

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// FrameStats keeps the last FrameHistory frame timings in a fixed-size ring buffer without per-frame heap allocations.
type FrameStats struct {
	samples [FrameHistory]FrameSample
	head    int
	count   int
}

// Record appends one frame's timings to the ring buffer, overwriting the oldest sample once full.
//
// Negative durations are clamped to zero. When total is zero and update or draw is positive, total defaults to update+draw.
func (s *FrameStats) Record(total, update, draw time.Duration) {
	if s == nil {
		return
	}
	if total < 0 {
		total = 0
	}
	if update < 0 {
		update = 0
	}
	if draw < 0 {
		draw = 0
	}
	if total == 0 && update+draw > 0 {
		total = update + draw
	}
	s.samples[s.head] = FrameSample{Total: total, Update: update, Draw: draw}
	s.head = (s.head + 1) % FrameHistory
	if s.count < FrameHistory {
		s.count++
	}
}

// Reset clears all recorded samples.
func (s *FrameStats) Reset() {
	if s == nil {
		return
	}
	s.head = 0
	s.count = 0
}

// Len returns how many samples are currently stored, up to FrameHistory.
func (s *FrameStats) Len() int {
	if s == nil {
		return 0
	}
	return s.count
}

// At returns sample i in chronological order, where 0 is the oldest retained frame and Len()-1 is the newest. Out-of-range indices return the zero FrameSample.
func (s *FrameStats) At(i int) FrameSample {
	if s == nil || i < 0 || i >= s.count {
		return FrameSample{}
	}
	start := (s.head - s.count + FrameHistory) % FrameHistory
	return s.samples[(start+i)%FrameHistory]
}

// Latest returns the most recently recorded sample, or the zero FrameSample when empty.
func (s *FrameStats) Latest() FrameSample {
	if s == nil || s.count == 0 {
		return FrameSample{}
	}
	return s.samples[(s.head-1+FrameHistory)%FrameHistory]
}

// Samples appends the retained samples in chronological order to dst and returns the resulting slice.
func (s *FrameStats) Samples(dst []FrameSample) []FrameSample {
	if s == nil || s.count == 0 {
		return dst
	}
	start := (s.head - s.count + FrameHistory) % FrameHistory
	for i := range s.count {
		dst = append(dst, s.samples[(start+i)%FrameHistory])
	}
	return dst
}

// Average returns the mean Total, Update and Draw durations across the retained samples.
func (s *FrameStats) Average() FrameSample {
	if s == nil || s.count == 0 {
		return FrameSample{}
	}
	var sumT, sumU, sumD time.Duration
	for i := range s.count {
		sample := s.At(i)
		sumT += sample.Total
		sumU += sample.Update
		sumD += sample.Draw
	}
	n := time.Duration(s.count)
	return FrameSample{
		Total:  sumT / n,
		Update: sumU / n,
		Draw:   sumD / n,
	}
}

// FPS returns the average frame rate in frames per second across the retained samples, or 0 when empty.
func (s *FrameStats) FPS() float64 {
	avg := s.Average()
	if avg.Total <= 0 {
		return 0
	}
	return float64(time.Second) / float64(avg.Total)
}

// MaxTotal returns the longest Total frame duration among the retained samples.
func (s *FrameStats) MaxTotal() time.Duration {
	if s == nil || s.count == 0 {
		return 0
	}
	var peak time.Duration
	for i := range s.count {
		if t := s.At(i).Total; t > peak {
			peak = t
		}
	}
	return peak
}

var (
	graphBGColor    = core.RGBA(0x0F172A, 0.9)
	graphIdleColor  = core.RGBA(0x94A3B8, 0.25)
	graphUpdColor   = core.RGB(0x38BDF8)
	graphDrawColor  = core.RGB(0xF59E0B)
	graphOverColor  = core.RGB(0xEF4444)
	graphRefColor   = core.RGBA(0x22C55E, 0.85)
	graphLabelColor = core.RGBA(0x86EFAC, 0.95)
	graphRefDash    = []float32{4, 3}
)

// DrawGraph records a stacked update+draw bar graph of the retained frame samples into d, filling size, with a reference line at targetFPS (60 when 0 or less).
func (s *FrameStats) DrawGraph(d *core.Drawing, size core.Size, targetFPS float64) {
	if s == nil || d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	if targetFPS <= 0 {
		targetFPS = 60
	}
	w := float32(size.Width)
	h := float32(size.Height)
	budget := float64(time.Second) / targetFPS
	// Scale the vertical axis to 2x the target frame budget (33.33 ms at 60 fps) so the reference line sits cleanly across the center.
	maxDur := 2 * budget

	d.Save()
	d.SetFillColor(graphBGColor).FillRect(0, 0, w, h)

	barW := w / float32(FrameHistory)
	gap := float32(0.5)
	if barW <= 1.5 {
		gap = 0
	}
	drawW := max(float32(1), barW-gap)
	startSlot := FrameHistory - s.count

	for i := range s.count {
		sample := s.At(i)
		x := float32(startSlot+i) * barW

		updH := barHeight(sample.Update, maxDur, h)
		drwH := barHeight(sample.Draw, maxDur, h-updH)
		totH := barHeight(sample.Total, maxDur, h)
		workH := updH + drwH

		if totH > workH {
			idleColor := graphIdleColor
			if float64(sample.Total) > budget {
				idleColor = core.RGBA(0xEF4444, 0.28)
			}
			d.SetFillColor(idleColor).FillRect(x, h-totH, drawW, totH-workH)
		}
		if updH > 0 {
			d.SetFillColor(graphUpdColor).FillRect(x, h-updH, drawW, updH)
		}
		if drwH > 0 {
			col := graphDrawColor
			if float64(sample.Update+sample.Draw) > budget {
				col = graphOverColor
			}
			d.SetFillColor(col).FillRect(x, h-updH-drwH, drawW, drwH)
		}
	}

	// Draw the 16.67 ms (or 1/targetFPS) budget reference line.
	refY := h - float32(budget/maxDur)*h
	d.Save()
	d.SetStrokeColor(graphRefColor).
		SetLineWidth(1).
		SetLineDash(graphRefDash...).
		BeginPath().
		MoveTo(0, refY).
		LineTo(w, refY).
		Stroke()
	d.Restore()

	budgetMs := budget / float64(time.Millisecond)
	d.SetFillColor(graphLabelColor).FillText(
		fmt.Sprintf("%.1fms", budgetMs),
		3,
		max(float32(10), refY-3),
		core.Font{Family: core.FontMonospace, Size: 9},
	)
	d.Restore()
}

func barHeight(dur time.Duration, maxDur float64, maxH float32) float32 {
	if dur <= 0 || maxDur <= 0 || maxH <= 0 {
		return 0
	}
	v := float32(float64(dur)/maxDur) * maxH
	if v > maxH {
		return maxH
	}
	return v
}
