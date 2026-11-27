package editor

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// OverlayKind identifies one of the seven Spec viewport overlay styles defined in
// design/editor/Spec.dc.html ("Overlay legend").
type OverlayKind string

const (
	// OverlayKindSelection is the person's selection: accent, 1.5 px solid, four corner handles, name tag.
	OverlayKindSelection OverlayKind = "selection"
	// OverlayKindAgent is the agent working overlay: agent violet, 1.5 px solid, no handles,
	// pulsing 1.6 s between 100% and 45%, pill with agent glyph saying what it is doing.
	OverlayKindAgent OverlayKind = "agent"
	// OverlayKindAdded marks a newly added object: added green, 1.5 px dashed, "+ name" pill.
	OverlayKindAdded OverlayKind = "added"
	// OverlayKindChanged marks an edited object: changed amber, 1.5 px dashed, "field old → new" pill.
	OverlayKindChanged OverlayKind = "changed"
	// OverlayKindRemoved marks a removed object: removed red, 1.5 px solid around a 25% grey ghost,
	// pill with struck-through name.
	OverlayKindRemoved OverlayKind = "removed"
	// OverlayKindProblem marks a live object with a problem: problem red, 2 px solid on the live object,
	// filled pill with alert triangle icon and message (never like a removed ghost).
	OverlayKindProblem OverlayKind = "problem"
	// OverlayKindFocusedChange highlights the focused change in a Review: its own color and dash at 2.5 px
	// with an outlined pill, never accent.
	OverlayKindFocusedChange OverlayKind = "focused"
)

// Spec viewport overlay timing and geometry constants (design/editor/Spec.dc.html).
const (
	// OverlayChangeFadeDuration is the fade-in duration for Added, Changed, and Removed overlays.
	OverlayChangeFadeDuration = 240 * time.Millisecond
	// OverlayAgentPulsePeriod is the full pulse cycle duration for the Agent working outline.
	OverlayAgentPulsePeriod = 1600 * time.Millisecond
	// OverlayAgentPulseMinAlpha is the minimum opacity reached during an Agent working pulse (45%).
	OverlayAgentPulseMinAlpha float32 = 0.45

	// OverlayBaseOffset is the default gap in points between an object's bounds and its overlay outline.
	OverlayBaseOffset float32 = 3
	// OverlaySelectionAgentGap is how much further out the Agent working outline sits when
	// paired with the person's Selection on the same object.
	OverlaySelectionAgentGap float32 = 3

	// Spec overlay outline stroke widths in points.
	overlaySelectionWidth float32 = 1.5
	overlayAgentWidth     float32 = 1.5
	overlayChangeWidth    float32 = 1.5
	overlayRemovedWidth   float32 = 1.5
	overlayProblemWidth   float32 = 2.0
	overlayFocusedWidth   float32 = 2.5

	overlayHandleSize float32 = 8
)

// OverlayLegendKinds returns the six toggleable Spec overlay legend kinds in display order:
// Selection, Agent working, Added, Changed, Removed, Problem.
func OverlayLegendKinds() []OverlayKind {
	return []OverlayKind{
		OverlayKindSelection,
		OverlayKindAgent,
		OverlayKindAdded,
		OverlayKindChanged,
		OverlayKindRemoved,
		OverlayKindProblem,
	}
}

// DefaultOverlayLegendKinds returns the canonical sorted IDs of all six legend kinds enabled by default.
func DefaultOverlayLegendKinds() []string {
	return []string{
		string(OverlayKindAdded),
		string(OverlayKindAgent),
		string(OverlayKindChanged),
		string(OverlayKindProblem),
		string(OverlayKindRemoved),
		string(OverlayKindSelection),
	}
}

// IsLegendKind reports whether k is one of the six toggleable rows in the viewport overlay legend.
func (k OverlayKind) IsLegendKind() bool {
	switch k {
	case OverlayKindSelection, OverlayKindAgent, OverlayKindAdded, OverlayKindChanged, OverlayKindRemoved, OverlayKindProblem:
		return true
	default:
		return false
	}
}

// Valid reports whether k is any of the seven Spec overlay kinds.
func (k OverlayKind) Valid() bool {
	return k.IsLegendKind() || k == OverlayKindFocusedChange
}

// IsChangeKind reports whether k is a diff/review change kind (Added, Changed, or Removed).
func (k OverlayKind) IsChangeKind() bool {
	return k == OverlayKindAdded || k == OverlayKindChanged || k == OverlayKindRemoved
}

// OverlayFadeAlpha returns the opacity in [0, 1] for a change overlay (Added, Changed, Removed)
// elapsed after it appeared. With reduceMotion enabled, it returns 1 immediately without fading.
func OverlayFadeAlpha(elapsed time.Duration, reduceMotion bool) float32 {
	if reduceMotion {
		return 1
	}
	if elapsed <= 0 {
		return 0
	}
	if elapsed >= OverlayChangeFadeDuration {
		return 1
	}
	return float32(elapsed) / float32(OverlayChangeFadeDuration)
}

// OverlayAgentPulseAlpha returns the outline opacity in [0.45, 1.0] for an active Agent working
// overlay pulsing on a 1.6 s period. With reduceMotion enabled, it stays solid at 1.0.
func OverlayAgentPulseAlpha(elapsed time.Duration, reduceMotion bool) float32 {
	if reduceMotion {
		return 1
	}
	period := OverlayAgentPulsePeriod.Seconds()
	phase := math.Mod(elapsed.Seconds(), period) / period
	if phase < 0 {
		phase += 1
	}
	wave := 0.5 * (1 + math.Cos(2*math.Pi*phase))
	return OverlayAgentPulseMinAlpha + (1-OverlayAgentPulseMinAlpha)*float32(wave)
}

// ObjectOverlay describes one Spec viewport overlay drawn over an object's canvas bounds.
type ObjectOverlay struct {
	// Kind selects which of the seven Spec overlay styles to draw.
	Kind OverlayKind
	// ChangeKind, when Kind is OverlayKindFocusedChange, chooses the underlying change kind
	// (OverlayKindAdded, OverlayKindChanged, or OverlayKindRemoved; defaults to OverlayKindAdded).
	// Focused changes always keep their own semantic color and dash at 2.5 px — never Accent.
	ChangeKind OverlayKind
	// Label is the primary text shown in the overlay's tag or pill:
	//   - Selection: object name (for example "Knight")
	//   - Agent: what the agent is doing (for example "placing slime_02")
	//   - Added: object name (prefixed with "+ " automatically if absent)
	//   - Changed: field old → new value (for example "z 10 → 12")
	//   - Removed: removed object name (drawn struck-through beside SecondaryLabel)
	//   - Problem: alert message (for example "door: lever_02 is missing")
	Label string
	// SecondaryLabel optionally overrides the trailing text on Removed pills (defaults to "removed").
	SecondaryLabel string
	// TaskNumber, when > 0 on OverlayKindAgent, draws the numbered task badge inside the agent pill.
	TaskNumber int
	// Holding, when true on OverlayKindAgent, renders the holding (45% dashed + clock glyph) variant.
	Holding bool
	// Bounds is the object's axis-aligned bounding box in canvas points.
	Bounds core.Bounds
	// Corners optionally supplies the exact transformed 4-corner quad in canvas points for
	// rotated 2D selection outlines. When empty, Bounds is used.
	Corners []math2d.Vec2
	// CanvasSize optionally clamps pill placement inside the viewport canvas.
	CanvasSize core.Size
	// PairedWithSelection, when true on OverlayKindAgent, places the agent outline 3 px further
	// out than Selection's outline so Selection keeps its handles inside.
	PairedWithSelection bool
	// PillStackOffset shifts the pill upward in points to clear another pill on the same object.
	PillStackOffset float32
	// Elapsed is the time since this overlay appeared (change fade-in) or started (agent pulse).
	// When HasElapsed is false, the overlay renders at full opacity (1.0).
	Elapsed    time.Duration
	HasElapsed bool
	// ReduceMotion disables fade-in (instant 100%) and agent outline pulsing (solid 100%).
	ReduceMotion bool
}

// specHandleFill returns the crisp handle fill color from SpecPalette tokens only
// (#FFFFFF in both dark and light modes, matching Spec.dc.html).
func specHandleFill(p SpecPalette) core.Color {
	if p.Dark {
		return p.TextPrimary
	}
	return p.Raised
}

// isWideOverlayRune reports whether r is a fullwidth/CJK rune that occupies roughly twice the
// horizontal advance of an ASCII glyph in overlay pills.
func isWideOverlayRune(r rune) bool {
	return (r >= 0x2E80 && r <= 0x9FFF) ||
		(r >= 0xAC00 && r <= 0xD7AF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFF00 && r <= 0xFFEF)
}

// overlayTextWidth estimates the drawn width of s in canvas points, matching asciiAdvance per rune
// for ASCII/Latin/arrow strings while expanding CJK/fullwidth characters so localized pills never clip.
func overlayTextWidth(s string, asciiAdvance float32) float32 {
	var units float32
	for _, r := range s {
		if isWideOverlayRune(r) {
			units += 1.85
		} else {
			units += 1
		}
	}
	return units * asciiAdvance
}

// DrawObjectOverlay is the single Spec viewport overlay drawing helper in editor.
// It draws any of the seven Spec overlay kinds (Selection, Agent working, Added, Changed,
// Removed, Problem, Focused change in Review) using exclusively SpecPalette tokens at the
// Spec's exact line widths, dash patterns, offsets, handles, ghosts, and pills, and returns
// the canvas rectangle occupied by the drawn tag/pill (if any).
func DrawObjectOverlay(d *core.Drawing, p SpecPalette, ov ObjectOverlay) core.Bounds {
	if d == nil || !ov.Kind.Valid() {
		return core.Bounds{}
	}
	bx := float32(ov.Bounds.Origin.X)
	by := float32(ov.Bounds.Origin.Y)
	bw := float32(ov.Bounds.Size.Width)
	bh := float32(ov.Bounds.Size.Height)
	if bw < 0 {
		bx += bw
		bw = -bw
	}
	if bh < 0 {
		by += bh
		bh = -bh
	}

	d.Save()
	defer d.Restore()

	switch ov.Kind {
	case OverlayKindSelection:
		return drawSelectionSpecOverlay(d, p, ov, bx, by, bw, bh)
	case OverlayKindAgent:
		return drawAgentSpecOverlay(d, p, ov, bx, by, bw, bh)
	case OverlayKindAdded, OverlayKindChanged, OverlayKindRemoved:
		return drawChangeSpecOverlay(d, p, ov, ov.Kind, false, bx, by, bw, bh)
	case OverlayKindFocusedChange:
		base := ov.ChangeKind
		if !base.IsChangeKind() {
			base = OverlayKindAdded
		}
		return drawChangeSpecOverlay(d, p, ov, base, true, bx, by, bw, bh)
	case OverlayKindProblem:
		return drawProblemSpecOverlay(d, p, ov, bx, by, bw, bh)
	default:
		return core.Bounds{}
	}
}

func clampOverlayPillPos(cx, minY, maxY, pad, pillW, pillH, stackOffset float32, canvas core.Size) (float32, float32) {
	px := cx - pillW*0.5
	py := minY - pad - pillH - 8 - stackOffset
	if py < 6 {
		py = maxY + pad + 8 + stackOffset
	}
	if canvas.Width > 0 {
		cw := float32(canvas.Width)
		if px+pillW > cw-6 {
			px = cw - pillW - 6
		}
		if px < 6 {
			px = 6
		}
	}
	if canvas.Height > 0 {
		ch := float32(canvas.Height)
		if py+pillH > ch-6 {
			py = ch - pillH - 6
		}
		if py < 6 {
			py = 6
		}
	}
	return px, py
}

// expandQuadOutward expands a 4-corner quad outward from its centroid along its edge normals by pad points.
func expandQuadOutward(corners []math2d.Vec2, pad float32) []math2d.Vec2 {
	if len(corners) != 4 || pad == 0 {
		return corners
	}
	// Compute signed area to orient outward normals consistently regardless of flip.
	area := float32(0)
	for i := range 4 {
		a, b := corners[i], corners[(i+1)%4]
		area += a.X*b.Y - b.X*a.Y
	}
	sign := float32(1)
	if area < 0 {
		sign = -1
	}
	// Edge outward unit normals.
	var normals [4]math2d.Vec2
	for i := range 4 {
		a, b := corners[i], corners[(i+1)%4]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l < 1e-4 {
			continue
		}
		// For clockwise screen coords (area > 0), outward normal of (dx, dy) is (dy, -dx).
		normals[i] = math2d.Vec2{X: sign * dy / l, Y: -sign * dx / l}
	}
	out := make([]math2d.Vec2, 4)
	for i := range 4 {
		nPrev := normals[(i+3)%4]
		nCurr := normals[i]
		out[i] = math2d.Vec2{
			X: corners[i].X + (nPrev.X+nCurr.X)*pad,
			Y: corners[i].Y + (nPrev.Y+nCurr.Y)*pad,
		}
	}
	return out
}

func drawSelectionSpecOverlay(d *core.Drawing, p SpecPalette, ov ObjectOverlay, bx, by, bw, bh float32) core.Bounds {
	pad := OverlayBaseOffset
	handleHalf := overlayHandleSize * 0.5
	handleFill := specHandleFill(p)

	var handlePts [4]math2d.Vec2
	var minX, minY, maxX, maxY float32
	if len(ov.Corners) == 4 {
		expanded := expandQuadOutward(ov.Corners, pad)
		d.SetStrokeColor(p.Accent.Edge).SetLineWidth(overlaySelectionWidth).SetLineDash()
		strokePolygon(d, expanded)
		minX, maxX = expanded[0].X, expanded[0].X
		minY, maxY = expanded[0].Y, expanded[0].Y
		for i, c := range expanded {
			handlePts[i] = c
			minX, maxX = min(minX, c.X), max(maxX, c.X)
			minY, maxY = min(minY, c.Y), max(maxY, c.Y)
		}
	} else {
		ox, oy := bx-pad, by-pad
		ow, oh := bw+2*pad, bh+2*pad
		d.SetStrokeColor(p.Accent.Edge).SetLineWidth(overlaySelectionWidth).SetLineDash()
		d.BeginPath().RoundRect(ox, oy, ow, oh, 6).Stroke()
		handlePts = [4]math2d.Vec2{
			{X: ox, Y: oy},
			{X: ox + ow, Y: oy},
			{X: ox + ow, Y: oy + oh},
			{X: ox, Y: oy + oh},
		}
		minX, minY, maxX, maxY = ox, oy, ox+ow, oy+oh
	}

	// Four corner handles (8x8 rounded squares, 1.5 px accent border).
	for _, pt := range handlePts {
		d.SetFillColor(handleFill)
		d.BeginPath().RoundRect(pt.X-handleHalf, pt.Y-handleHalf, overlayHandleSize, overlayHandleSize, 2).Fill()
		d.SetStrokeColor(p.Accent.Edge).SetLineWidth(overlaySelectionWidth).SetLineDash()
		d.BeginPath().RoundRect(pt.X-handleHalf, pt.Y-handleHalf, overlayHandleSize, overlayHandleSize, 2).Stroke()
	}

	if ov.Label == "" {
		return core.Bounds{}
	}
	font := core.Font{Size: 10.5, Weight: 600}
	const tagH float32 = 18
	tagW := overlayTextWidth(ov.Label, 6.3) + 12
	tx, ty := clampOverlayPillPos((minX+maxX)*0.5, minY, maxY, handleHalf, tagW, tagH, ov.PillStackOffset, ov.CanvasSize)

	d.SetFillColor(p.Accent.Fg)
	d.BeginPath().RoundRect(tx, ty, tagW, tagH, 5).Fill()
	d.SetFillColor(p.Accent.OnFill).FillText(ov.Label, tx+6, ty+12.5, font)
	return core.Bounds{
		Origin: core.Point{X: core.Points(tx), Y: core.Points(ty)},
		Size:   core.Size{Width: core.Points(tagW), Height: core.Points(tagH)},
	}
}

func drawAgentSpecOverlay(d *core.Drawing, p SpecPalette, ov ObjectOverlay, bx, by, bw, bh float32) core.Bounds {
	pad := OverlayBaseOffset
	if ov.PairedWithSelection {
		pad += OverlaySelectionAgentGap
	}
	alpha := float32(1)
	if ov.Holding {
		alpha = OverlayAgentPulseMinAlpha
	} else if ov.HasElapsed {
		alpha = OverlayAgentPulseAlpha(ov.Elapsed, ov.ReduceMotion)
	}

	ox, oy := bx-pad, by-pad
	ow, oh := bw+2*pad, bh+2*pad
	d.SetStrokeColor(ui.Alpha(p.Agent.Edge, alpha)).SetLineWidth(overlayAgentWidth)
	if ov.Holding {
		d.SetLineDash(4, 3)
	} else {
		d.SetLineDash()
	}
	d.BeginPath().RoundRect(ox, oy, ow, oh, 6).Stroke()
	d.SetLineDash()

	if ov.Label == "" {
		return core.Bounds{}
	}

	const pillH float32 = 22
	fontLbl := core.Font{Size: 11, Weight: 500}
	textW := overlayTextWidth(ov.Label, 6.2)
	var pillW float32
	if ov.TaskNumber > 0 {
		clockW := float32(0)
		if ov.Holding {
			clockW = 16
		}
		pillW = 4 + 16 + 6 + clockW + textW + 8
	} else {
		// 8 left pad + 12 sparkle glyph + 6 gap + text + 8 right pad.
		pillW = 8 + 12 + 6 + textW + 8
	}

	px, py := clampOverlayPillPos(bx+bw*0.5, by, by+bh, pad, pillW, pillH, ov.PillStackOffset, ov.CanvasSize)

	pillBorder := ui.Alpha(p.Agent.Edge, 0.65)
	if ov.TaskNumber > 0 {
		pillBorder = p.Agent.Edge
	}
	if ov.Holding {
		pillBorder = p.LineRaised
	}
	d.SetFillColor(ui.Alpha(p.PillBg, 0.92))
	d.BeginPath().RoundRect(px, py, pillW, pillH, 11).Fill()
	d.SetStrokeColor(pillBorder).SetLineWidth(1)
	d.BeginPath().RoundRect(px, py, pillW, pillH, 11).Stroke()

	if ov.TaskNumber > 0 {
		fontNum := core.Font{Size: 10, Weight: 600}
		numStr := fmt.Sprintf("%d", ov.TaskNumber)
		numHalfW := float32(len(numStr)) * 3
		badgeCX := px + 12
		badgeCY := py + pillH*0.5
		if ov.Holding {
			d.SetStrokeColor(p.Agent.Edge).SetLineWidth(1.5)
			d.BeginPath().Ellipse(badgeCX, badgeCY, 7.5, 7.5).Stroke()
			d.SetFillColor(p.TextSecondary).FillText(numStr, badgeCX-numHalfW, badgeCY+3.5, fontNum)
		} else {
			d.SetFillColor(p.Agent.Fg).BeginPath().Ellipse(badgeCX, badgeCY, 8, 8).Fill()
			d.SetFillColor(p.Agent.OnFill).FillText(numStr, badgeCX-numHalfW, badgeCY+3.5, fontNum)
		}
		curX := px + 26
		if ov.Holding {
			ccx := curX + 5
			ccy := py + pillH*0.5
			d.SetStrokeColor(p.TextMuted).SetLineWidth(1.4).SetLineCap(core.CapRound)
			d.BeginPath().Ellipse(ccx, ccy, 5, 5).Stroke()
			d.BeginPath().MoveTo(ccx, ccy-2.5).LineTo(ccx, ccy).LineTo(ccx+2.2, ccy+1.4).Stroke()
			curX += 16
		}
		txtColor := p.TextSecondary
		if ov.Holding {
			txtColor = p.TextMuted
		}
		d.SetFillColor(txtColor).FillText(ov.Label, curX, py+14.5, fontLbl)
	} else {
		// Agent 4-point sparkle glyph in Spec Agent.Fg (never white on violet).
		drawAgentSparkleGlyph(d, px+14, py+pillH*0.5, 5, p.Agent.Fg)
		d.SetFillColor(p.TextSecondary).FillText(ov.Label, px+26, py+14.5, fontLbl)
	}

	return core.Bounds{
		Origin: core.Point{X: core.Points(px), Y: core.Points(py)},
		Size:   core.Size{Width: core.Points(pillW), Height: core.Points(pillH)},
	}
}

// drawAgentSparkleGlyph draws the 4-point star agent glyph centered at (cx, cy) with outer radius r.
func drawAgentSparkleGlyph(d *core.Drawing, cx, cy, r float32, col core.Color) {
	inner := r * 0.34
	d.SetFillColor(col)
	d.BeginPath().
		MoveTo(cx, cy-r).
		LineTo(cx+inner, cy-inner).
		LineTo(cx+r, cy).
		LineTo(cx+inner, cy+inner).
		LineTo(cx, cy+r).
		LineTo(cx-inner, cy+inner).
		LineTo(cx-r, cy).
		LineTo(cx-inner, cy-inner).
		ClosePath().
		Fill()
}

func drawChangeSpecOverlay(d *core.Drawing, p SpecPalette, ov ObjectOverlay, base OverlayKind, focused bool, bx, by, bw, bh float32) core.Bounds {
	alpha := float32(1)
	if ov.HasElapsed {
		alpha = OverlayFadeAlpha(ov.Elapsed, ov.ReduceMotion)
	}
	if alpha <= 0 {
		return core.Bounds{}
	}

	var role SpecRole
	var dashed bool
	switch base {
	case OverlayKindChanged:
		role = p.Changed
		dashed = true
	case OverlayKindRemoved:
		role = p.Removed
		dashed = false
	default: // OverlayKindAdded
		role = p.Added
		dashed = true
	}

	pad := OverlayBaseOffset
	ox, oy := bx-pad, by-pad
	ow, oh := bw+2*pad, bh+2*pad

	// Removed draws a 25% grey ghost inside the solid removed outline so it never resembles Problem.
	if base == OverlayKindRemoved {
		d.SetFillColor(ui.Alpha(p.TextFaint, 0.25*alpha))
		d.BeginPath().RoundRect(bx, by, bw, bh, 4).Fill()
	}

	lw := overlayChangeWidth
	if base == OverlayKindRemoved {
		lw = overlayRemovedWidth
	}
	if focused {
		lw = overlayFocusedWidth
	}

	d.SetStrokeColor(ui.Alpha(role.Edge, alpha)).SetLineWidth(lw)
	if dashed {
		d.SetLineDash(5, 3)
	} else {
		d.SetLineDash()
	}
	d.BeginPath().RoundRect(ox, oy, ow, oh, 6).Stroke()
	d.SetLineDash()

	label := ov.Label
	if label == "" {
		return core.Bounds{}
	}
	if base == OverlayKindAdded && !strings.HasPrefix(label, "+") {
		label = "+ " + label
	}

	const pillH float32 = 22
	fontMono := core.Font{Family: core.FontMonospace, Size: 11, Weight: 500}
	fontBody := core.Font{Size: 11, Weight: 500}
	var pillW float32
	secondary := ov.SecondaryLabel
	nameW := overlayTextWidth(label, 6.4)
	if base == OverlayKindRemoved {
		if secondary == "" {
			secondary = "removed"
		}
		secW := overlayTextWidth(secondary, 6.0)
		pillW = 8 + nameW + 6 + secW + 8
	} else {
		pillW = 8 + nameW + 8
	}

	px, py := clampOverlayPillPos(bx+bw*0.5, by, by+bh, pad, pillW, pillH, ov.PillStackOffset, ov.CanvasSize)

	pillFill := ui.Alpha(p.PillBg, 0.92*alpha)
	borderColor := ui.Alpha(role.Edge, 0.45*alpha)
	borderWidth := float32(1)
	if focused {
		pillFill = ui.Alpha(p.PillBg, 0.96*alpha)
		borderColor = ui.Alpha(role.Edge, alpha)
		borderWidth = 1.5
	}
	d.SetFillColor(pillFill)
	d.BeginPath().RoundRect(px, py, pillW, pillH, 11).Fill()
	d.SetStrokeColor(borderColor).SetLineWidth(borderWidth)
	d.BeginPath().RoundRect(px, py, pillW, pillH, 11).Stroke()

	fg := ui.Alpha(role.Fg, alpha)
	if base == OverlayKindRemoved {
		nameX := px + 8
		d.SetFillColor(fg).FillText(label, nameX, py+14.5, fontMono)
		// Explicit strikethrough line across the removed object's name (never color alone).
		strikeY := py + pillH*0.5
		d.SetStrokeColor(fg).SetLineWidth(1.2)
		d.BeginPath().MoveTo(nameX-1, strikeY).LineTo(nameX+nameW, strikeY).Stroke()
		d.SetFillColor(fg).FillText(secondary, nameX+nameW+6, py+14.5, fontBody)
	} else {
		d.SetFillColor(fg).FillText(label, px+8, py+14.5, fontMono)
	}

	return core.Bounds{
		Origin: core.Point{X: core.Points(px), Y: core.Points(py)},
		Size:   core.Size{Width: core.Points(pillW), Height: core.Points(pillH)},
	}
}

func drawProblemSpecOverlay(d *core.Drawing, p SpecPalette, ov ObjectOverlay, bx, by, bw, bh float32) core.Bounds {
	role := p.Problem()
	pad := OverlayBaseOffset
	ox, oy := bx-pad, by-pad
	ow, oh := bw+2*pad, bh+2*pad

	// 2 px solid Problem outline directly around the live object (never a ghost).
	d.SetStrokeColor(role.Edge).SetLineWidth(overlayProblemWidth).SetLineDash()
	d.BeginPath().RoundRect(ox, oy, ow, oh, 6).Stroke()

	if ov.Label == "" {
		return core.Bounds{}
	}

	const pillH float32 = 22
	const iconW float32 = 12
	font := core.Font{Size: 11, Weight: 600}
	pillW := 8 + iconW + 6 + overlayTextWidth(ov.Label, 6.3) + 8
	px, py := clampOverlayPillPos(bx+bw*0.5, by, by+bh, pad, pillW, pillH, ov.PillStackOffset, ov.CanvasSize)

	// Solid filled Problem pill with alert triangle icon + message in Problem.OnFill.
	d.SetFillColor(role.Fg)
	d.BeginPath().RoundRect(px, py, pillW, pillH, 11).Fill()

	ix, iy := px+8, py+5
	drawProblemAlertGlyph(d, ix, iy, role.OnFill)
	d.SetFillColor(role.OnFill).FillText(ov.Label, px+8+iconW+6, py+14.5, font)

	return core.Bounds{
		Origin: core.Point{X: core.Points(px), Y: core.Points(py)},
		Size:   core.Size{Width: core.Points(pillW), Height: core.Points(pillH)},
	}
}

// drawProblemAlertGlyph draws the 12x12 alert triangle icon at top-left (ix, iy).
func drawProblemAlertGlyph(d *core.Drawing, ix, iy float32, col core.Color) {
	d.SetStrokeColor(col).SetLineWidth(1.5).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
	d.BeginPath().MoveTo(ix+6, iy+1).LineTo(ix+11, iy+10.5).LineTo(ix+1, iy+10.5).ClosePath().Stroke()
	d.BeginPath().MoveTo(ix+6, iy+4.5).LineTo(ix+6, iy+7.2).Stroke()
	d.SetFillColor(col).BeginPath().Arc(ix+6, iy+9.1, 0.8, 0, float32(2*math.Pi), false).Fill()
}

// --- Live Scene Overlay State & Legend Visibility on Editor ---

// SceneOverlayEntry attaches a live Spec overlay (Agent working, Added, Changed, Removed,
// or Focused change in Review) to an object in the open scene (or world ghost bounds for Removed).
type SceneOverlayEntry struct {
	// ID optionally identifies this entry for targeted updates or removal.
	ID string
	// ObjectID names the scene object this overlay attaches to.
	ObjectID string
	// Kind selects which Spec overlay style to draw (Agent, Added, Changed, Removed, or FocusedChange).
	Kind OverlayKind
	// ChangeKind selects the base change kind when Kind is OverlayKindFocusedChange.
	ChangeKind OverlayKind
	// Label is the pill text (action text for Agent, object name for Added/Removed, "field old → new" for Changed).
	Label string
	// SecondaryLabel optionally overrides "removed" on Removed pills.
	SecondaryLabel string
	// GhostBounds optionally supplies 2D world bounds for a Removed object that is no longer in the scene.
	GhostBounds *core.Bounds
	// GhostPos3D and GhostSize3D optionally supply 3D world center & extents for a Removed object in 3D view.
	GhostPos3D  *[3]float32
	GhostSize3D *[3]float32
	// StartedAt records when the overlay appeared; automatically stamped with the editor clock when zero.
	StartedAt time.Time
}

func (e *Editor) overlayNow() time.Time {
	if e != nil && e.overlayNowFn != nil {
		return e.overlayNowFn()
	}
	return time.Now()
}

// SetOverlayClock sets a custom clock function for overlay fade-in and pulse animation timing
// (used in deterministic tests; pass nil to restore time.Now).
func (e *Editor) SetOverlayClock(fn func() time.Time) {
	if e == nil {
		return
	}
	e.overlayNowFn = fn
	e.notify()
}

// ReduceMotion reports whether reduced motion is active for viewport overlays and animations:
// it honors an explicit SetReduceMotion override when set, otherwise follows the attached
// UI scope's Environment.ReduceMotion().
func (e *Editor) ReduceMotion() bool {
	if e == nil {
		return false
	}
	if e.reduceMotionOverride != nil {
		return *e.reduceMotionOverride
	}
	if e.scope != nil {
		return e.scope.Env().ReduceMotion()
	}
	return false
}

// SetReduceMotion forces reduced motion on or off on the editor (overriding the platform environment).
func (e *Editor) SetReduceMotion(on bool) {
	if e == nil {
		return
	}
	v := on
	e.reduceMotionOverride = &v
	e.notify()
}

// ClearReduceMotion clears any explicit SetReduceMotion override so ReduceMotion follows the platform environment.
func (e *Editor) ClearReduceMotion() {
	if e == nil || e.reduceMotionOverride == nil {
		return
	}
	e.reduceMotionOverride = nil
	e.notify()
}

func cloneSceneOverlayEntry(ent SceneOverlayEntry) SceneOverlayEntry {
	if ent.GhostBounds != nil {
		r := *ent.GhostBounds
		ent.GhostBounds = &r
	}
	if ent.GhostPos3D != nil {
		v := *ent.GhostPos3D
		ent.GhostPos3D = &v
	}
	if ent.GhostSize3D != nil {
		v := *ent.GhostSize3D
		ent.GhostSize3D = &v
	}
	return ent
}

// SetSceneOverlays replaces the active live scene overlays (agent working, added, changed, removed,
// focused review change) drawn over the open scene in both 2D and 3D viewports.
func (e *Editor) SetSceneOverlays(entries []SceneOverlayEntry) {
	if e == nil {
		return
	}
	now := e.overlayNow()
	if len(entries) == 0 {
		if len(e.sceneOverlays) == 0 {
			return
		}
		e.sceneOverlays = nil
		e.notify()
		return
	}
	cp := make([]SceneOverlayEntry, len(entries))
	for i, ent := range entries {
		if ent.StartedAt.IsZero() {
			ent.StartedAt = now
		}
		cp[i] = cloneSceneOverlayEntry(ent)
	}
	e.sceneOverlays = cp
	e.notify()
}

// AddSceneOverlay appends (or replaces by matching non-empty ID) one live scene overlay entry.
func (e *Editor) AddSceneOverlay(entry SceneOverlayEntry) {
	if e == nil {
		return
	}
	if entry.StartedAt.IsZero() {
		entry.StartedAt = e.overlayNow()
	}
	entry = cloneSceneOverlayEntry(entry)
	if entry.ID != "" {
		for i := range e.sceneOverlays {
			if e.sceneOverlays[i].ID == entry.ID {
				e.sceneOverlays[i] = entry
				e.notify()
				return
			}
		}
	}
	e.sceneOverlays = append(e.sceneOverlays, entry)
	e.notify()
}

// RemoveSceneOverlay removes all live scene overlay entries matching id.
func (e *Editor) RemoveSceneOverlay(id string) {
	if e == nil || id == "" || len(e.sceneOverlays) == 0 {
		return
	}
	kept := e.sceneOverlays[:0]
	removed := false
	for _, ent := range e.sceneOverlays {
		if ent.ID == id {
			removed = true
			continue
		}
		kept = append(kept, ent)
	}
	if !removed {
		return
	}
	if len(kept) == 0 {
		e.sceneOverlays = nil
	} else {
		e.sceneOverlays = kept
	}
	e.notify()
}

// ClearSceneOverlays removes all live scene overlay entries.
func (e *Editor) ClearSceneOverlays() {
	e.SetSceneOverlays(nil)
}

// SceneOverlays returns a deep copy of the active live scene overlay entries.
func (e *Editor) SceneOverlays() []SceneOverlayEntry {
	if e == nil || len(e.sceneOverlays) == 0 {
		return nil
	}
	out := make([]SceneOverlayEntry, len(e.sceneOverlays))
	for i, ent := range e.sceneOverlays {
		out[i] = cloneSceneOverlayEntry(ent)
	}
	return out
}

// hasAnimatedOverlays reports whether any currently visible scene or task overlay needs
// active animation ticks (pulsing agent outline or fading change overlay < 240 ms).
func (e *Editor) hasAnimatedOverlays() bool {
	if e == nil || e.playing || e.HistoryPreviewActive() || e.ReduceMotion() || !e.ShowAgentChanges() {
		return false
	}
	if e.OverlayKindVisible(OverlayKindAgent) {
		for _, ov := range e.SceneTaskOverlays() {
			if !ov.Holding {
				return true
			}
		}
	}
	now := e.overlayNow()
	for _, ent := range e.sceneOverlays {
		switch ent.Kind {
		case OverlayKindAgent:
			if e.OverlayKindVisible(OverlayKindAgent) {
				return true
			}
		case OverlayKindAdded, OverlayKindChanged, OverlayKindRemoved:
			if e.OverlayKindVisible(ent.Kind) && now.Sub(ent.StartedAt) < OverlayChangeFadeDuration {
				return true
			}
		case OverlayKindFocusedChange:
			base := ent.ChangeKind
			if !base.IsChangeKind() {
				base = OverlayKindAdded
			}
			if e.OverlayKindVisible(base) && now.Sub(ent.StartedAt) < OverlayChangeFadeDuration {
				return true
			}
		}
	}
	return false
}

// --- Legend Toggle Persistence (EditorSettings.OverlayLegend) ---

func (e *Editor) effectiveOverlayLegendSet() map[OverlayKind]bool {
	set := make(map[OverlayKind]bool, 6)
	if e != nil {
		e.settingsMu.RLock()
		legend := e.settings.OverlayLegend
		hidden := e.settings.HiddenOverlayKinds
		if legend != nil {
			for _, id := range *legend {
				k := OverlayKind(id)
				if k.IsLegendKind() {
					set[k] = true
				}
			}
			e.settingsMu.RUnlock()
			return set
		}
		for _, k := range OverlayLegendKinds() {
			set[k] = true
		}
		for _, id := range hidden {
			k := OverlayKind(id)
			if k.IsLegendKind() {
				delete(set, k)
			}
		}
		e.settingsMu.RUnlock()
		return set
	}
	for _, k := range OverlayLegendKinds() {
		set[k] = true
	}
	return set
}

// OverlayKindVisible reports whether overlay kind is currently enabled in the top-left viewport legend.
// For OverlayKindFocusedChange, it reports whether any change kind is enabled (callers with a specific
// ChangeKind should pass that ChangeKind).
func (e *Editor) OverlayKindVisible(kind OverlayKind) bool {
	if e == nil {
		return false
	}
	if kind == OverlayKindFocusedChange {
		set := e.effectiveOverlayLegendSet()
		return set[OverlayKindAdded] || set[OverlayKindChanged] || set[OverlayKindRemoved]
	}
	if !kind.IsLegendKind() {
		return false
	}
	return e.effectiveOverlayLegendSet()[kind]
}

// ActiveOverlayLegendKinds returns the enabled legend kinds in Spec display order.
func (e *Editor) ActiveOverlayLegendKinds() []OverlayKind {
	if e == nil {
		return nil
	}
	set := e.effectiveOverlayLegendSet()
	var out []OverlayKind
	for _, k := range OverlayLegendKinds() {
		if set[k] {
			out = append(out, k)
		}
	}
	return out
}

// SetOverlayKindVisible shows or hides one legend kind across all viewports (2D and 3D) and
// persists the choice per person in Editor.SettingsPath when configured (never touching project files).
func (e *Editor) SetOverlayKindVisible(kind OverlayKind, visible bool) error {
	if e == nil {
		return nil
	}
	if !kind.IsLegendKind() {
		return fmt.Errorf("editor: unknown overlay legend kind %q", kind)
	}
	if e.OverlayKindVisible(kind) == visible {
		return nil
	}
	set := e.effectiveOverlayLegendSet()
	if visible {
		set[kind] = true
	} else {
		delete(set, kind)
	}
	return e.commitOverlayLegendSet(set)
}

// ToggleOverlayKind flips one legend kind between visible and hidden and persists to SettingsPath when set.
func (e *Editor) ToggleOverlayKind(kind OverlayKind) error {
	if e == nil {
		return nil
	}
	return e.SetOverlayKindVisible(kind, !e.OverlayKindVisible(kind))
}

// SetOverlayLegendKinds replaces the enabled legend kinds and persists to SettingsPath when set.
func (e *Editor) SetOverlayLegendKinds(kinds []OverlayKind) error {
	if e == nil {
		return nil
	}
	set := make(map[OverlayKind]bool, len(kinds))
	for _, k := range kinds {
		if !k.IsLegendKind() {
			return fmt.Errorf("editor: unknown overlay legend kind %q", k)
		}
		set[k] = true
	}
	return e.commitOverlayLegendSet(set)
}

func (e *Editor) commitOverlayLegendSet(set map[OverlayKind]bool) error {
	list := make([]string, 0, len(set))
	var hidden []string
	for _, k := range OverlayLegendKinds() {
		if set[k] {
			list = append(list, string(k))
		} else {
			hidden = append(hidden, string(k))
		}
	}
	slices.Sort(list)
	slices.Sort(hidden)
	e.settingsMu.Lock()
	e.normalizeSettingsLocked()
	e.settings.OverlayLegend = &list
	if len(hidden) > 0 {
		e.settings.HiddenOverlayKinds = hidden
	} else {
		e.settings.HiddenOverlayKinds = nil
	}
	e.settingsMu.Unlock()

	// Keep legacy problemOverlayHidden in sync with the Problem legend toggle.
	e.problems.mu.Lock()
	e.problems.problemOverlayHidden = !set[OverlayKindProblem]
	e.problems.mu.Unlock()

	if err := e.persistEditorSettings(); err != nil {
		e.notify()
		return err
	}
	e.notify()
	return nil
}

// --- Viewport Rendering Passes (2D & 3D) ---

func (e *Editor) builtObjectCanvasBounds(scene *sceneformat.BuiltScene, bo *sceneformat.BuiltObject) (core.Bounds, []math2d.Vec2, bool) {
	if e == nil || scene == nil || bo == nil || bo.Doc == nil || viewFlagged(e.hidden, bo) {
		return core.Bounds{}, nil, false
	}
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return core.Bounds{}, nil, false
		}
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	m := e.layerAffineFor(space, px, py).Mul(bo.World)
	rx, ry, rw, rh := objectOutline(bo)
	corners := []math2d.Vec2{
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh}),
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh}),
	}
	minX, maxX := corners[0].X, corners[0].X
	minY, maxY := corners[0].Y, corners[0].Y
	for _, c := range corners[1:] {
		minX, maxX = min(minX, c.X), max(maxX, c.X)
		minY, maxY = min(minY, c.Y), max(maxY, c.Y)
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}, corners, true
}

func (e *Editor) objectDisplayName(bo *sceneformat.BuiltObject, fallbackID string) string {
	if bo != nil && bo.Doc != nil && bo.Doc.Name != "" {
		return bo.Doc.Name
	}
	if bo != nil && bo.ID != "" {
		return bo.ID
	}
	return fallbackID
}

// drawSceneOverlays2D draws all live SceneOverlayEntry items (Agent working, Added, Changed,
// Removed, Focused change) in the 2D viewport through DrawObjectOverlay, honoring legend toggles
// and Editor.ShowAgentChanges().
func (e *Editor) drawSceneOverlays2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing || !e.ShowAgentChanges() || len(e.sceneOverlays) == 0 {
		return
	}
	spec := e.SpecPalette(nil)
	now := e.overlayNow()
	rm := e.ReduceMotion()
	canvas := core.Size{Width: core.Points(e.canvasW), Height: core.Points(e.canvasH)}

	for _, ent := range e.sceneOverlays {
		checkKind := ent.Kind
		if ent.Kind == OverlayKindFocusedChange {
			base := ent.ChangeKind
			if !base.IsChangeKind() {
				base = OverlayKindAdded
			}
			checkKind = base
		}
		if !e.OverlayKindVisible(checkKind) {
			continue
		}

		var bounds core.Bounds
		var ok bool
		if bo := scene.Object(ent.ObjectID); bo != nil {
			bounds, _, ok = e.builtObjectCanvasBounds(scene, bo)
		} else if ent.GhostBounds != nil && (ent.Kind == OverlayKindRemoved || (ent.Kind == OverlayKindFocusedChange && ent.ChangeKind == OverlayKindRemoved)) {
			r := *ent.GhostBounds
			pts := worldRectCanvasCorners(e, float32(r.Origin.X), float32(r.Origin.Y), float32(r.Size.Width), float32(r.Size.Height))
			minX, maxX := pts[0].X, pts[0].X
			minY, maxY := pts[0].Y, pts[0].Y
			for _, c := range pts[1:] {
				minX, maxX = min(minX, c.X), max(maxX, c.X)
				minY, maxY = min(minY, c.Y), max(maxY, c.Y)
			}
			bounds = core.Bounds{
				Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
				Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
			}
			ok = true
		}
		if !ok {
			continue
		}

		selOnObj := e.IsSelected(ent.ObjectID) && e.OverlayKindVisible(OverlayKindSelection)
		pairedSel := ent.Kind == OverlayKindAgent && selOnObj
		stackOff := float32(0)
		if selOnObj && ent.Kind != OverlayKindSelection {
			stackOff = 24
		}
		DrawObjectOverlay(d, spec, ObjectOverlay{
			Kind:                ent.Kind,
			ChangeKind:          ent.ChangeKind,
			Label:               ent.Label,
			SecondaryLabel:      ent.SecondaryLabel,
			Bounds:              bounds,
			CanvasSize:          canvas,
			PairedWithSelection: pairedSel,
			PillStackOffset:     stackOff,
			Elapsed:             now.Sub(ent.StartedAt),
			HasElapsed:          true,
			ReduceMotion:        rm,
		})
	}
}

// projectCorners3DBounds projects 8 world-space 3D corners onto canvas-space 2D bounds.
func projectCorners3DBounds(corners [8][3]float32, vc view3DCam, w, h float32) (core.Bounds, bool) {
	var minX, minY, maxX, maxY float32
	anyVis := false
	for _, c := range corners {
		sx, sy, _, vis := vc.project(c, w, h)
		if !vis || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
			continue
		}
		if !anyVis {
			minX, maxX = sx, sx
			minY, maxY = sy, sy
			anyVis = true
		} else {
			minX, maxX = min(minX, sx), max(maxX, sx)
			minY, maxY = min(minY, sy), max(maxY, sy)
		}
	}
	if !anyVis {
		return core.Bounds{}, false
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(max(maxX-minX, 12)), Height: core.Points(max(maxY-minY, 12))},
	}, true
}

// projectBox3DBounds projects an axis-aligned 3D box centered at pos with extents ext onto canvas bounds.
func projectBox3DBounds(pos, ext [3]float32, vc view3DCam, w, h float32) (core.Bounds, bool) {
	hx, hy, hz := ext[0]*0.5, ext[1]*0.5, ext[2]*0.5
	var corners [8][3]float32
	for i, s := range boxSigns3D {
		corners[i] = [3]float32{
			pos[0] + float32(s[0])*hx,
			pos[1] + float32(s[1])*hy,
			pos[2] + float32(s[2])*hz,
		}
	}
	return projectCorners3DBounds(corners, vc, w, h)
}

// projectObject3DBounds projects a 3D object's 8 corners onto canvas-space 2D bounds.
func (e *Editor) projectObject3DBounds(o *sceneformat.ObjectDoc, parent aff3, vc view3DCam, w, h float32) (core.Bounds, bool) {
	if o == nil {
		return core.Bounds{}, false
	}
	return projectCorners3DBounds(e.objectCorners3DUnder(o, parent), vc, w, h)
}

// drawSceneOverlays3D draws Spec Agent working, Added, Changed, Removed, Focused change, and Problem
// overlays on projected 3D object bounds in the 3D viewport through DrawObjectOverlay, sharing the
// exact same legend toggles as the 2D viewport.
func (e *Editor) drawSceneOverlays3D(d *core.Drawing, w, h float32) {
	if e == nil || d == nil || e.playing || e.HistoryPreviewActive() {
		return
	}
	spec := e.SpecPalette(nil)
	now := e.overlayNow()
	rm := e.ReduceMotion()
	vc := e.activeView3DCam(w, h)
	canvas := core.Size{Width: core.Points(w), Height: core.Points(h)}

	// Map visible 3D objects by ID for fast lookup.
	type objRef3D struct {
		obj    *sceneformat.ObjectDoc
		parent aff3
	}
	byID := make(map[string]objRef3D)
	if doc := e.resolvedDoc(); doc != nil {
		for li := range doc.Layers {
			layer := &doc.Layers[li]
			if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
				continue
			}
			var walk func(objs []sceneformat.ObjectDoc, parent aff3)
			walk = func(objs []sceneformat.ObjectDoc, parent aff3) {
				for oi := range objs {
					o := &objs[oi]
					if e.hidden[o.ID] {
						continue
					}
					posed := e.posed3D(o)
					byID[o.ID] = objRef3D{obj: posed, parent: parent}
					walk(o.Children, parent.mul(local3(posed)))
				}
			}
			walk(layer.Objects, ident3)
		}
	}

	// 1. Live agent task overlays in 3D.
	showAgentChanges := e.ShowAgentChanges()
	if showAgentChanges && e.OverlayKindVisible(OverlayKindAgent) {
		for _, ov := range e.SceneTaskOverlays() {
			ref, ok := byID[ov.ObjectID]
			if !ok {
				continue
			}
			bounds, vis := e.projectObject3DBounds(ref.obj, ref.parent, vc, w, h)
			if !vis {
				continue
			}
			pairedSel := e.IsSelected(ov.ObjectID) && e.OverlayKindVisible(OverlayKindSelection)
			stackOff := float32(0)
			if pairedSel {
				stackOff = 24
			}
			DrawObjectOverlay(d, spec, ObjectOverlay{
				Kind:                OverlayKindAgent,
				Label:               ov.Label,
				TaskNumber:          ov.TaskNumber,
				Holding:             ov.Holding,
				Bounds:              bounds,
				CanvasSize:          canvas,
				PairedWithSelection: pairedSel,
				PillStackOffset:     stackOff,
				Elapsed:             now.Sub(e.overlayEpoch),
				HasElapsed:          true,
				ReduceMotion:        rm,
			})
		}
	}

	// 2. Live scene overlay entries (Agent working, Added, Changed, Removed, Focused change).
	if showAgentChanges {
		for _, ent := range e.sceneOverlays {
			checkKind := ent.Kind
			if ent.Kind == OverlayKindFocusedChange {
				base := ent.ChangeKind
				if !base.IsChangeKind() {
					base = OverlayKindAdded
				}
				checkKind = base
			}
			if !e.OverlayKindVisible(checkKind) {
				continue
			}
			var bounds core.Bounds
			var vis bool
			if ref, ok := byID[ent.ObjectID]; ok {
				bounds, vis = e.projectObject3DBounds(ref.obj, ref.parent, vc, w, h)
			} else if ent.GhostPos3D != nil && (ent.Kind == OverlayKindRemoved || (ent.Kind == OverlayKindFocusedChange && ent.ChangeKind == OverlayKindRemoved)) {
				pos := *ent.GhostPos3D
				ext := [3]float32{16, 16, 16}
				if ent.GhostSize3D != nil {
					ext = *ent.GhostSize3D
				}
				bounds, vis = projectBox3DBounds(pos, ext, vc, w, h)
			}
			if !vis {
				continue
			}
			selOnObj := e.IsSelected(ent.ObjectID) && e.OverlayKindVisible(OverlayKindSelection)
			pairedSel := ent.Kind == OverlayKindAgent && selOnObj
			stackOff := float32(0)
			if selOnObj && ent.Kind != OverlayKindSelection {
				stackOff = 24
			}
			DrawObjectOverlay(d, spec, ObjectOverlay{
				Kind:                ent.Kind,
				ChangeKind:          ent.ChangeKind,
				Label:               ent.Label,
				SecondaryLabel:      ent.SecondaryLabel,
				Bounds:              bounds,
				CanvasSize:          canvas,
				PairedWithSelection: pairedSel,
				PillStackOffset:     stackOff,
				Elapsed:             now.Sub(ent.StartedAt),
				HasElapsed:          true,
				ReduceMotion:        rm,
			})
		}
	}

	// 3. Live Problem overlays in 3D (sorted deterministically by object ID).
	if e.OverlayKindVisible(OverlayKindProblem) {
		objProbs := e.openSceneObjectProblems()
		for _, objID := range slices.Sorted(maps.Keys(objProbs)) {
			probs := objProbs[objID]
			if len(probs) == 0 {
				continue
			}
			ref, ok := byID[objID]
			if !ok {
				continue
			}
			bounds, vis := e.projectObject3DBounds(ref.obj, ref.parent, vc, w, h)
			if !vis {
				continue
			}
			label := shortProblemLabel(probs[0])
			if len(probs) > 1 {
				label = fmt.Sprintf("%s (+%d)", label, len(probs)-1)
			}
			stackOff := float32(0)
			if e.IsSelected(objID) && e.OverlayKindVisible(OverlayKindSelection) {
				stackOff = 24
			}
			DrawObjectOverlay(d, spec, ObjectOverlay{
				Kind:            OverlayKindProblem,
				Label:           label,
				Bounds:          bounds,
				CanvasSize:      canvas,
				PillStackOffset: stackOff,
			})
		}
	}
}
