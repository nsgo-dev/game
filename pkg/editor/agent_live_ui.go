package editor

import (
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type canvasRect struct {
	x, y, w, h float32
}

// drawAgentLiveOverlays draws the live Spec change and agent-working overlays in the 2D viewport
// (design/editor/Spec.dc.html, Working.dc.html, Main.dc.html):
//   - Added: 1.5 px dashed added outline + "+ name" pill
//   - Changed: 1.5 px dashed changed outline + "field old → new" pill
//   - Removed: grey 25% ghost + 1.5 px solid removed outline + struck-through "name removed" pill
//   - Agent working: pulsing 1.5 px solid violet outline (no handles; sits 3 px further out when
//     coexisting with Selection on the same object) + dark pill with violet sparkle glyph and activity
//   - Problem: 2 px solid problem outline + filled pill with alert glyph and message
func (e *Editor) drawAgentLiveOverlays(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing || e.HistoryPreviewActive() {
		return
	}
	spec := e.SpecPalette(nil)
	overlays := e.VisibleViewportLiveOverlays()
	if len(overlays) == 0 {
		return
	}

	// Track all placed pills across the viewport so pills on neighboring or same objects never overlap.
	var placedPills []canvasRect

	d.Save()
	for _, ov := range overlays {
		if ov.Kind == OverlayLegendSelection {
			// Selection outline and handles are drawn by drawSelectionOverlay / drawGizmo2D.
			continue
		}
		rect, ok := e.liveOverlayCanvasRect(scene, ov)
		if !ok {
			continue
		}

		// Anchor pill above the top edge of this overlay's outline.
		outlineTop := rect.y - ov.OutsetPx
		pillBottom := outlineTop - 6

		switch ov.Kind {
		case OverlayLegendAdded:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Added.Edge, ui.Alpha(spec.Added.Fg, 0.16), true)
			placedPills = append(placedPills, drawMonoOverlayPill(d, rect.x+rect.w*0.5, pillBottom, e.canvasW, ov.Label, spec.Added.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Added.Edge, 0.65), placedPills))

		case OverlayLegendChanged:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Changed.Edge, core.Transparent, true)
			placedPills = append(placedPills, drawMonoOverlayPill(d, rect.x+rect.w*0.5, pillBottom, e.canvasW, ov.Label, spec.Changed.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Changed.Edge, 0.65), placedPills))

		case OverlayLegendRemoved:
			// Grey ghost at 25% opacity with 1.5 px solid red outline.
			ghostFill := ui.Alpha(spec.TextFaint, 0.25)
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Removed.Edge, ghostFill, false)
			placedPills = append(placedPills, drawRemovedOverlayPill(d, rect.x+rect.w*0.5, pillBottom, e.canvasW, ov.Detail, "removed", spec.Removed.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Removed.Edge, 0.65), placedPills))

		case OverlayLegendAgent:
			alpha := e.AgentPulseOpacity()
			fill := core.Transparent
			if !e.IsSelected(ov.ObjectID) {
				fill = ui.Alpha(spec.Agent.Fg, 0.18*alpha)
			}
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, ui.Alpha(spec.Agent.Edge, alpha), fill, false)
			placedPills = append(placedPills, drawAgentWorkingPill(d, rect.x+rect.w*0.5, pillBottom, e.canvasW, ov.Label, spec.Agent.Fg, spec.TextSecondary, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Agent.Edge, 0.7), placedPills))

		case OverlayLegendProblem:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 2.0, spec.Problem().Edge, core.Transparent, false)
			placedPills = append(placedPills, drawProblemOverlayPill(d, rect.x+rect.w*0.5, pillBottom, e.canvasW, ov.Label, spec.Problem().OnFill, spec.Problem().Fg, placedPills))
		}
	}
	d.Restore()
}

// drawAgentLiveOverlays3D projects live agent overlays onto the 3D viewport canvas using the active 3D camera.
func (e *Editor) drawAgentLiveOverlays3D(d *core.Drawing, w, h float32) {
	if e == nil || d == nil || e.playing || e.HistoryPreviewActive() || w <= 0 || h <= 0 {
		return
	}
	overlays := e.VisibleViewportLiveOverlays()
	if len(overlays) == 0 {
		return
	}
	spec := e.SpecPalette(nil)
	proj := e.Projection3D(w, h)
	var placedPills []canvasRect

	d.Save()
	for _, ov := range overlays {
		if ov.Kind == OverlayLegendSelection {
			continue
		}
		rect, ok := e.liveOverlay3DCanvasRect(proj, ov)
		if !ok {
			continue
		}
		outlineTop := rect.y - ov.OutsetPx
		pillBottom := outlineTop - 6
		switch ov.Kind {
		case OverlayLegendAdded:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Added.Edge, ui.Alpha(spec.Added.Fg, 0.14), true)
			placedPills = append(placedPills, drawMonoOverlayPill(d, rect.x+rect.w*0.5, pillBottom, w, ov.Label, spec.Added.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Added.Edge, 0.65), placedPills))
		case OverlayLegendChanged:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Changed.Edge, core.Transparent, true)
			placedPills = append(placedPills, drawMonoOverlayPill(d, rect.x+rect.w*0.5, pillBottom, w, ov.Label, spec.Changed.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Changed.Edge, 0.65), placedPills))
		case OverlayLegendRemoved:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, spec.Removed.Edge, ui.Alpha(spec.TextFaint, 0.25), false)
			placedPills = append(placedPills, drawRemovedOverlayPill(d, rect.x+rect.w*0.5, pillBottom, w, ov.Detail, "removed", spec.Removed.Fg, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Removed.Edge, 0.65), placedPills))
		case OverlayLegendAgent:
			alpha := e.AgentPulseOpacity()
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 1.5, ui.Alpha(spec.Agent.Edge, alpha), core.Transparent, false)
			placedPills = append(placedPills, drawAgentWorkingPill(d, rect.x+rect.w*0.5, pillBottom, w, ov.Label, spec.Agent.Fg, spec.TextSecondary, ui.Alpha(spec.PillBg, 0.94), ui.Alpha(spec.Agent.Edge, 0.7), placedPills))
		case OverlayLegendProblem:
			drawLiveOutlineBox(d, rect, ov.OutsetPx, 2.0, spec.Problem().Edge, core.Transparent, false)
			placedPills = append(placedPills, drawProblemOverlayPill(d, rect.x+rect.w*0.5, pillBottom, w, ov.Label, spec.Problem().OnFill, spec.Problem().Fg, placedPills))
		}
	}
	d.Restore()
}

func (e *Editor) liveOverlayCanvasRect(scene *sceneformat.BuiltScene, ov ViewportLiveOverlay) (canvasRect, bool) {
	if ov.Kind == OverlayLegendRemoved {
		return e.removedOverlayCanvasRect(ov.ObjectID)
	}
	bo := scene.Object(ov.ObjectID)
	if bo == nil || bo.Doc == nil || viewFlagged(e.hidden, bo) {
		return canvasRect{}, false
	}
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return canvasRect{}, false
		}
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	m := e.layerAffineFor(space, px, py).Mul(bo.World)
	rx, ry, rw, rh := objectOutline(bo)
	return transformedBoundsRect(m, rx, ry, rw, rh)
}

func (e *Editor) removedOverlayCanvasRect(objectID string) (canvasRect, bool) {
	if e == nil || e.agentLive.turn == nil {
		return canvasRect{}, false
	}
	ch, ok := e.agentLive.turn.byID[objectID]
	if !ok || ch.Kind != AgentChangeRemoved {
		return canvasRect{}, false
	}
	if e.agentLive.turn.baseBuilt != nil {
		if bo := e.agentLive.turn.baseBuilt.Object(objectID); bo != nil && bo.Doc != nil {
			space := sceneformat.SpaceWorld
			var px, py float32 = 1, 1
			if layer := e.agentLive.turn.baseBuilt.Layer(bo.Layer); layer != nil {
				space = layer.Space
				px, py = layer.ParallaxX, layer.ParallaxY
			}
			m := e.layerAffineFor(space, px, py).Mul(bo.World)
			rx, ry, rw, rh := objectOutline(bo)
			return transformedBoundsRect(m, rx, ry, rw, rh)
		}
	}
	if ch.RemovedDoc != nil {
		space, px, py := e.layerSpaceAndParallax(ch.Layer)
		m := e.layerAffineFor(space, px, py)
		pt := m.TransformPoint(math2d.Vec2{X: ch.RemovedDoc.X, Y: ch.RemovedDoc.Y})
		if !isFiniteVec(pt) {
			return canvasRect{}, false
		}
		half := defaultMarkerHalfSize
		return canvasRect{x: pt.X - half, y: pt.Y - half, w: half * 2, h: half * 2}, true
	}
	return canvasRect{}, false
}

func (e *Editor) liveOverlay3DCanvasRect(proj Projection3D, ov ViewportLiveOverlay) (canvasRect, bool) {
	var obj *sceneformat.ObjectDoc
	if ov.Kind == OverlayLegendRemoved {
		if e.agentLive.turn != nil {
			if ch, ok := e.agentLive.turn.byID[ov.ObjectID]; ok {
				obj = ch.RemovedDoc
			}
		}
	} else {
		obj, _ = e.FindResolvedObject(ov.ObjectID)
	}
	if obj == nil {
		return canvasRect{}, false
	}
	minB, maxB := e.ObjectAABB3D(obj)
	corners := [8][3]float32{
		{minB[0], minB[1], minB[2]},
		{maxB[0], minB[1], minB[2]},
		{maxB[0], maxB[1], minB[2]},
		{minB[0], maxB[1], minB[2]},
		{minB[0], minB[1], maxB[2]},
		{maxB[0], minB[1], maxB[2]},
		{maxB[0], maxB[1], maxB[2]},
		{minB[0], maxB[1], maxB[2]},
	}
	var minX, maxX, minY, maxY float32
	anyVis := false
	for _, c := range corners {
		sx, sy, _, vis := proj.Project(math3d.Vec3{X: c[0], Y: c[1], Z: c[2]})
		if !vis || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
			continue
		}
		if !anyVis {
			minX, maxX, minY, maxY = sx, sx, sy, sy
			anyVis = true
		} else {
			minX, maxX = min(minX, sx), max(maxX, sx)
			minY, maxY = min(minY, sy), max(maxY, sy)
		}
	}
	if !anyVis {
		return canvasRect{}, false
	}
	return canvasRect{x: minX, y: minY, w: max(maxX-minX, 12), h: max(maxY-minY, 12)}, true
}

func transformedBoundsRect(m math2d.Affine, rx, ry, rw, rh float32) (canvasRect, bool) {
	pts := [4]math2d.Vec2{
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh}),
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh}),
	}
	for _, p := range pts {
		if !isFiniteVec(p) {
			return canvasRect{}, false
		}
	}
	minX, maxX := pts[0].X, pts[0].X
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts[1:] {
		minX, maxX = min(minX, p.X), max(maxX, p.X)
		minY, maxY = min(minY, p.Y), max(maxY, p.Y)
	}
	return canvasRect{x: minX, y: minY, w: max(maxX-minX, 8), h: max(maxY-minY, 8)}, true
}

func drawLiveOutlineBox(d *core.Drawing, r canvasRect, outset, lineWidth float32, stroke, fill core.Color, dashed bool) {
	d.Save()
	if fill != core.Transparent {
		d.SetFillColor(fill).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 4).Fill()
	}
	x := r.x - outset
	y := r.y - outset
	w := r.w + outset*2
	h := r.h + outset*2
	d.SetStrokeColor(stroke).SetLineWidth(lineWidth)
	if dashed {
		d.SetLineDash(5, 3)
	} else {
		d.SetLineDash()
	}
	d.BeginPath().RoundRect(x, y, w, h, 6).Stroke()
	d.SetLineDash()
	d.Restore()
}

func resolvePillRect(centerX, bottomY, canvasW, pillW, pillH float32, placed []canvasRect) canvasRect {
	const gap float32 = 4
	const margin float32 = 6
	px := centerX - pillW*0.5
	if canvasW > 0 {
		if px+pillW > canvasW-margin {
			px = canvasW - pillW - margin
		}
		if px < margin {
			px = margin
		}
	}
	py := max(bottomY-pillH, margin)
	hit := func(y float32) (canvasRect, bool) {
		for _, other := range placed {
			if px < other.x+other.w+gap && px+pillW+gap > other.x &&
				y < other.y+other.h+gap && y+pillH+gap > other.y {
				return other, true
			}
		}
		return canvasRect{}, false
	}
	// Prefer stacking upward; if top margin is reached, step downward so pills never overlap.
	stackDown := false
	for iter := 0; iter < 16; iter++ {
		other, collided := hit(py)
		if !collided {
			break
		}
		if !stackDown {
			nextY := other.y - pillH - gap
			if nextY >= margin {
				py = nextY
				continue
			}
			stackDown = true
			py = max(bottomY-pillH, margin)
		}
		other, collided = hit(py)
		if !collided {
			break
		}
		py = other.y + other.h + gap
	}
	return canvasRect{x: px, y: py, w: pillW, h: pillH}
}

func drawMonoOverlayPill(d *core.Drawing, centerX, bottomY, canvasW float32, text string, fg, bg, border core.Color, placed []canvasRect) canvasRect {
	const pillH float32 = 22
	const padX float32 = 9
	font := core.Font{Family: core.FontMonospace, Size: 11}
	tw := float32(len([]rune(text))) * 6.8
	pillW := tw + padX*2
	r := resolvePillRect(centerX, bottomY, canvasW, pillW, pillH, placed)

	d.Save()
	d.SetFillColor(bg).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Fill()
	d.SetStrokeColor(border).SetLineWidth(1).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Stroke()
	d.SetFillColor(fg).FillText(text, r.x+padX, r.y+14.5, font)
	d.Restore()
	return r
}

func drawRemovedOverlayPill(d *core.Drawing, centerX, bottomY, canvasW float32, name, suffix string, fg, bg, border core.Color, placed []canvasRect) canvasRect {
	const pillH float32 = 22
	const padX float32 = 9
	const gap float32 = 6
	mono := core.Font{Family: core.FontMonospace, Size: 11}
	sans := core.Font{Size: 11}
	nameW := float32(len([]rune(name))) * 6.8
	sufW := float32(len([]rune(suffix))) * 6.3
	pillW := padX*2 + nameW + gap + sufW
	r := resolvePillRect(centerX, bottomY, canvasW, pillW, pillH, placed)

	d.Save()
	d.SetFillColor(bg).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Fill()
	d.SetStrokeColor(border).SetLineWidth(1).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Stroke()
	d.SetFillColor(fg).FillText(name, r.x+padX, r.y+14.5, mono)
	// Strike through the removed object's name per Spec.dc.html.
	strikeY := r.y + pillH*0.54
	d.SetStrokeColor(fg).SetLineWidth(1).BeginPath().MoveTo(r.x+padX, strikeY).LineTo(r.x+padX+nameW, strikeY).Stroke()
	d.SetFillColor(fg).FillText(suffix, r.x+padX+nameW+gap, r.y+14.5, sans)
	d.Restore()
	return r
}

func drawAgentWorkingPill(d *core.Drawing, centerX, bottomY, canvasW float32, text string, glyphColor, fg, bg, border core.Color, placed []canvasRect) canvasRect {
	const pillH float32 = 22
	const padX float32 = 10
	const glyphW float32 = 10
	const gap float32 = 6
	font := core.Font{Size: 11}
	tw := float32(len([]rune(text))) * 6.5
	pillW := padX*2 + glyphW + gap + tw
	r := resolvePillRect(centerX, bottomY, canvasW, pillW, pillH, placed)

	d.Save()
	d.SetFillColor(bg).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Fill()
	d.SetStrokeColor(border).SetLineWidth(1).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Stroke()
	// 4-point Spec agent sparkle glyph.
	gx := r.x + padX + glyphW*0.5
	gy := r.y + pillH*0.5
	d.SetFillColor(glyphColor).BeginPath().
		MoveTo(gx, gy-4.5).
		LineTo(gx+1.5, gy-1.5).
		LineTo(gx+4.5, gy).
		LineTo(gx+1.5, gy+1.5).
		LineTo(gx, gy+4.5).
		LineTo(gx-1.5, gy+1.5).
		LineTo(gx-4.5, gy).
		LineTo(gx-1.5, gy-1.5).
		ClosePath().Fill()
	d.SetFillColor(fg).FillText(text, r.x+padX+glyphW+gap, r.y+14.5, font)
	d.Restore()
	return r
}

func drawProblemOverlayPill(d *core.Drawing, centerX, bottomY, canvasW float32, text string, fg, bg core.Color, placed []canvasRect) canvasRect {
	const pillH float32 = 22
	const padX float32 = 9
	const iconW float32 = 10
	const gap float32 = 6
	font := core.Font{Size: 11, Weight: core.FontWeightSemibold}
	tw := float32(len([]rune(text))) * 6.5
	pillW := padX*2 + iconW + gap + tw
	r := resolvePillRect(centerX, bottomY, canvasW, pillW, pillH, placed)

	d.Save()
	d.SetFillColor(bg).BeginPath().RoundRect(r.x, r.y, r.w, r.h, 11).Fill()
	// Alert triangle icon in high-contrast OnFill ink (#101216 on dark #FF6B6B).
	ix := r.x + padX + iconW*0.5
	iy := r.y + pillH*0.5
	d.SetStrokeColor(fg).SetLineWidth(1.5).SetLineJoin(core.JoinRound).BeginPath().
		MoveTo(ix, iy-4).LineTo(ix+4.5, iy+3.8).LineTo(ix-4.5, iy+3.8).ClosePath().Stroke()
	d.SetFillColor(fg).FillText(text, r.x+padX+iconW+gap, r.y+14.5, font)
	d.Restore()
	return r
}

// toolbarAgentPillView renders the header pill following the agent's live state
// (design/editor/Working.dc.html):
//   - Working: violet pill with pulsing dot and "Agent is <activity> · manual edits stay available"
//   - Waiting: warning pill with clock icon and "Waiting for you · <detail>"
//   - Pending review: raised pill showing change count with Keep and Revert buttons
//   - Idle: hidden
func toolbarAgentPillView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type pillKey struct {
		dark   bool
		lang   string
		state  AgentState
		detail string
		count  int
	}
	keyMemo := core.NewMemo(cx, func() pillKey {
		_ = rev.Get()
		st, detail := ed.AgentState()
		n := 0
		if ed.agentLive.turn != nil {
			n = len(ed.agentLive.turn.changes)
		}
		return pillKey{
			dark:   ed.SpecPalette(cx).Dark,
			lang:   ed.ResolvedLanguage(),
			state:  st,
			detail: detail,
			count:  n,
		}
	})

	return core.Show{
		When: func() bool {
			k := keyMemo.Get()
			if k.state == AgentStateIdle {
				return false
			}
			if k.state == AgentStatePendingReview && k.count == 0 {
				return false
			}
			return true
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				p := SpecPaletteFor(k.dark)
				text := ed.AgentHeaderPillText()
				if text == "" {
					return core.View{}
				}

				switch k.state {
				case AgentStateWaiting:
					return core.View{
						TestID:     "header-agent-pill",
						Role:       core.RoleStatus,
						LiveRegion: core.LiveRegionPolite,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 14,
							Background:   p.Warning.Fill,
							BorderWidth:  1,
							BorderColor:  p.Warning.Edge,
						},
						Children: []core.Node{
							ui.Icon(lucide.Clock, 13, func() core.Color { return p.Warning.Fg }),
							core.Text{
								TestID:   "header-agent-pill-text",
								Content:  text,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.Warning.Fg,
								},
							},
						},
					}

				case AgentStatePendingReview:
					return core.View{
						TestID:     "header-agent-pill",
						Role:       core.RoleStatus,
						LiveRegion: core.LiveRegionPolite,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Height:       core.Pt(28),
							Padding:      core.Edges{Left: 12, Right: 4},
							CornerRadius: 14,
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(7),
									Height:       core.Pt(7),
									CornerRadius: 4,
									Background:   p.Added.Fg,
								},
							},
							core.Text{
								TestID:   "header-agent-pill-text",
								Content:  text,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextSecondary,
								},
							},
							core.View{
								TestID:    "header-agent-keep",
								Role:      core.RoleButton,
								Label:     ed.T("agent.keep"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Height:         core.Pt(22),
									Padding:        core.Symmetric(0, 10),
									CornerRadius:   11,
									Background:     p.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FocusVisible:   p.FocusRing(2),
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.KeepAgentTurn())
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("agent.keep"),
										Style: core.Style{
											FontSize:   11.5,
											FontWeight: 600,
											Color:      p.Accent.OnFill,
										},
									},
								},
							},
							core.View{
								TestID:    "header-agent-revert",
								Role:      core.RoleButton,
								Label:     ed.T("agent.revert"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Height:         core.Pt(22),
									Padding:        core.Symmetric(0, 10),
									CornerRadius:   11,
									Background:     p.LineRaised,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FocusVisible:   p.FocusRing(2),
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.RevertAgentTurn())
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("agent.revert"),
										Style: core.Style{
											FontSize:   11.5,
											FontWeight: 500,
											Color:      p.TextSecondary,
										},
									},
								},
							},
						},
					}

				default: // AgentStateWorking
					return core.View{
						TestID:     "header-agent-pill",
						Role:       core.RoleStatus,
						LiveRegion: core.LiveRegionPolite,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 14,
							Background:   p.Agent.Fill,
							BorderWidth:  1,
							BorderColor:  ui.Alpha(p.Agent.Edge, 0.55),
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(7),
									Height:       core.Pt(7),
									CornerRadius: 4,
									Background:   p.Agent.Fg,
								},
							},
							core.Text{
								TestID:   "header-agent-pill-text",
								Content:  text,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextSecondary,
								},
							},
						},
					}
				}
			})
		},
	}
}

// inspectorAgentWorkingBannerView renders a read-only violet Spec notice at the top of the
// object inspector while the agent is touching objID.
func inspectorAgentWorkingBannerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], objID string) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.AgentTouchingObject(objID)
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "inspector-agent-working-banner",
				Role:   core.RoleStatus,
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 8,
					BorderWidth:  1,
				},
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Background:  p.Agent.Fill,
						BorderColor: ui.Alpha(p.Agent.Edge, 0.55),
					}
				},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), FlexShrink: 0},
						Draw: func(d *core.Drawing, _ core.Size) {
							p := ed.SpecPalette(cx)
							d.SetFillColor(p.Agent.Fg).BeginPath().
								MoveTo(6, 1).LineTo(7.6, 4.4).LineTo(11, 6).LineTo(7.6, 7.6).
								LineTo(6, 11).LineTo(4.4, 7.6).LineTo(1, 6).LineTo(4.4, 4.4).ClosePath().Fill()
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.T("agent.inspectorReadOnly")
						},
						Style: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							FlexShrink: 1,
						},
						StyleFunc: func() core.Style {
							return core.Style{Color: ed.SpecPalette(cx).TextSecondary}
						},
					},
				},
			}
		},
	}
}
