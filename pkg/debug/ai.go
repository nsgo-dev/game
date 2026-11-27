//go:build !release

package debug

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/nsgo/core"
)

const maxOverlayMachineHistory = 8

var (
	aiActiveRowStyle = core.Style{
		Direction:      core.Row,
		JustifyContent: core.JustifySpaceBetween,
		Gap:            8,
		Background:     core.RGBA(0x22C55E, 0.18),
		CornerRadius:   3,
		Padding:        core.Edges{Top: 1, Bottom: 1, Left: 4, Right: 4},
	}
	aiActiveTextStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		FontWeight: core.FontWeightSemibold,
		Color:      core.RGB(0x4ADE80),
	}
	aiSuccessTextStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0x38BDF8),
	}
	aiFailureTextStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0xF87171),
	}
)

// BehaviorTreeSnapshot builds an AISnapshot for agent by walking ins.
// Under -tags release, BehaviorTreeSnapshot returns a zero AISnapshot without walking ins.
func BehaviorTreeSnapshot(agent string, ins *ai.BTInspector) AISnapshot {
	if ins == nil {
		return AISnapshot{Agent: agent}
	}
	return AISnapshot{
		Agent: agent,
		Tree:  ins.Walk(),
	}
}

// StateMachineSnapshot builds an AISnapshot for agent from m's current state and transition history.
// Under -tags release, StateMachineSnapshot returns a zero AISnapshot without snapshotting m.
func StateMachineSnapshot(agent string, m flow.MachineInspector) AISnapshot {
	if m == nil {
		return AISnapshot{Agent: agent}
	}
	snap := m.Inspect()
	return AISnapshot{
		Agent:   agent,
		Machine: &snap,
	}
}

// SetAI updates the AI inspector panel's pushed snapshot and immediately refreshes the panel.
// It is used when Options.AI is nil.
func (o *Overlay) SetAI(snap AISnapshot) {
	if o == nil {
		return
	}
	o.aiSnap = cloneAISnapshot(snap)
	o.hasAI = true
	o.aiTick++
	if o.aiSig != nil {
		o.aiSig.Set(o.aiTick)
	}
}

// ClearAI clears the pushed AI snapshot and hides the AI inspector panel when Options.AI is nil.
func (o *Overlay) ClearAI() {
	if o == nil {
		return
	}
	o.aiSnap = AISnapshot{}
	o.hasAI = false
	o.aiTick++
	if o.aiSig != nil {
		o.aiSig.Set(o.aiTick)
	}
}

// RefreshAI tells the AI inspector panel to re-read Options.AI immediately.
func (o *Overlay) RefreshAI() {
	if o == nil {
		return
	}
	if o.opts.AI != nil {
		o.lastAI = cloneAISnapshot(o.opts.AI())
	}
	o.aiTick++
	if o.aiSig != nil {
		o.aiSig.Set(o.aiTick)
	}
}

// AISnapshot returns the currently displayed AI snapshot from Options.AI if configured,
// or the most recent SetAI value.
func (o *Overlay) AISnapshot() AISnapshot {
	if o == nil {
		return AISnapshot{}
	}
	if o.opts.AI != nil {
		return o.opts.AI()
	}
	return cloneAISnapshot(o.aiSnap)
}

func (o *Overlay) currentAI() (AISnapshot, bool) {
	if o.aiSig != nil {
		_ = o.aiSig.Get()
	}
	if o.opts.AI != nil {
		return o.opts.AI(), true
	}
	if o.hasAI {
		return o.aiSnap, true
	}
	return AISnapshot{}, false
}

func (o *Overlay) aiInspectorView() core.Node {
	return core.Show{
		When: func() bool {
			_, ok := o.currentAI()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "debug-ai-inspector",
				Style:  sectionDividerStyle,
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						snap, ok := o.currentAI()
						if !ok {
							return nil
						}
						header := "AI"
						if snap.Agent != "" {
							header = "AI · " + snap.Agent
						}
						nodes := []core.Node{
							core.Text{Content: header, Style: sectionHeaderStyle},
						}
						if len(snap.Tree) == 0 && snap.Machine == nil {
							nodes = append(nodes, core.Text{Content: "AI: none", Style: mutedTextStyle})
							return core.Group(nodes)
						}
						if len(snap.Tree) > 0 {
							nodes = append(nodes, buildAITreeView(snap.Tree))
						}
						if snap.Machine != nil {
							nodes = append(nodes, buildAIMachineView(*snap.Machine))
						}
						return core.Group(nodes)
					}),
				},
			}
		},
	}
}

func buildAITreeView(tree []ai.BTNodeTrace) core.Node {
	rows := make([]core.Node, 0, len(tree))
	for _, n := range tree {
		kind := n.Kind
		if kind == "" {
			kind = "custom"
		}
		label := kind
		if n.Name != "" {
			label += " " + strconv.Quote(n.Name)
		}
		prefix := "  "
		labelStyle := layerLabelStyle
		indent := core.Points(n.Depth*10 + 4)
		rowStyle := core.Style{
			Direction:      core.Row,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            8,
			Padding:        core.Edges{Left: indent, Right: 4},
		}
		if n.Active {
			prefix = "* "
			labelStyle = aiActiveTextStyle
			rowStyle = aiActiveRowStyle
			rowStyle.Padding = core.Edges{Top: 1, Bottom: 1, Left: indent, Right: 4}
		}
		statusStyle := aiStatusStyle(n)
		rows = append(rows, core.View{
			Style: rowStyle,
			Children: []core.Node{
				core.Text{Content: prefix + label, Style: labelStyle},
				core.Text{Content: n.StatusString(), Style: statusStyle},
			},
		})
	}
	return core.View{
		TestID:   "debug-ai-tree",
		Style:    core.Style{Gap: 2},
		Children: rows,
	}
}

func aiStatusStyle(n ai.BTNodeTrace) core.Style {
	if !n.Ticked {
		return mutedTextStyle
	}
	switch n.LastStatus {
	case ai.StatusRunning:
		return aiActiveTextStyle
	case ai.StatusSuccess:
		return aiSuccessTextStyle
	case ai.StatusFailure:
		return aiFailureTextStyle
	default:
		return mutedTextStyle
	}
}

func buildAIMachineView(m flow.MachineInspection) core.Node {
	rows := []core.Node{
		core.View{
			Style: aiActiveRowStyle,
			Children: []core.Node{
				core.Text{Content: "* state " + m.Current, Style: aiActiveTextStyle},
				core.Text{Content: fmt.Sprintf("step %d · %s", m.Step, m.TimeInState), Style: layerCountStyle},
			},
		},
	}
	if m.HasPrevious {
		rows = append(rows, core.Text{
			Content: "prev: " + m.Previous,
			Style:   mutedTextStyle,
		})
	}
	if len(m.History) == 0 {
		rows = append(rows, core.Text{
			Content: "History (0)",
			Style:   mutedTextStyle,
		})
	} else {
		rows = append(rows, core.Text{
			Content: fmt.Sprintf("History (%d)", len(m.History)),
			Style:   sectionHeaderStyle,
		})
		start := 0
		if len(m.History) > maxOverlayMachineHistory {
			start = len(m.History) - maxOverlayMachineHistory
		}
		for i := start; i < len(m.History); i++ {
			tr := m.History[i]
			line := fmt.Sprintf("step %d: %s -> %s", tr.Step, tr.From, tr.To)
			if tr.Transition != "" {
				line += " (" + tr.Transition + ")"
			}
			style := layerLabelStyle
			if i == len(m.History)-1 {
				style = layerCountStyle
			}
			rows = append(rows, core.Text{Content: line, Style: style})
		}
	}
	return core.View{
		TestID:   "debug-ai-machine",
		Style:    core.Style{Gap: 2},
		Children: rows,
	}
}

func cloneAISnapshot(src AISnapshot) AISnapshot {
	dst := AISnapshot{
		Agent: src.Agent,
		Tree:  slices.Clone(src.Tree),
	}
	if src.Machine != nil {
		m := *src.Machine
		m.History = slices.Clone(src.Machine.History)
		dst.Machine = &m
	}
	return dst
}

func aiSnapshotsEqual(a, b AISnapshot) bool {
	if a.Agent != b.Agent || !slices.Equal(a.Tree, b.Tree) {
		return false
	}
	if (a.Machine == nil) != (b.Machine == nil) {
		return false
	}
	if a.Machine != nil {
		if a.Machine.Current != b.Machine.Current ||
			a.Machine.Previous != b.Machine.Previous ||
			a.Machine.HasPrevious != b.Machine.HasPrevious ||
			a.Machine.Step != b.Machine.Step ||
			a.Machine.TimeInState != b.Machine.TimeInState ||
			!slices.Equal(a.Machine.History, b.Machine.History) {
			return false
		}
	}
	return true
}

// DrawVector strokes a directional arrow from origin to origin+vec in col under d's active transform,
// keeping line width and arrowhead size steady in screen points across camera zooms.
func (s *ShapeOverlay) DrawVector(d *core.Drawing, origin, vec core.Point, col core.Color) {
	if d == nil {
		return
	}
	vx, vy := float32(vec.X), float32(vec.Y)
	length := float32(math.Hypot(float64(vx), float64(vy)))
	if length <= 1e-4 {
		return
	}
	lw := s.strokeWidth(d)
	scale := drawingScale(d)
	if scale <= 1e-6 {
		scale = 1
	}
	ox, oy := float32(origin.X), float32(origin.Y)
	tipX := ox + vx
	tipY := oy + vy

	headLen := min(length*0.35, 7/scale)
	headHalfWidth := headLen * 0.55
	ux, uy := vx/length, vy/length
	px, py := -uy, ux
	baseX := tipX - ux*headLen
	baseY := tipY - uy*headLen

	d.Save().
		SetStrokeColor(col).
		SetLineWidth(lw).
		BeginPath().
		MoveTo(ox, oy).
		LineTo(tipX, tipY).
		MoveTo(baseX+px*headHalfWidth, baseY+py*headHalfWidth).
		LineTo(tipX, tipY).
		LineTo(baseX-px*headHalfWidth, baseY-py*headHalfWidth).
		Stroke().
		Restore()
}

// DrawPath strokes a waypoint polyline and small waypoint markers in col under d's active transform.
func (s *ShapeOverlay) DrawPath(d *core.Drawing, path []core.Point, col core.Color) {
	if d == nil || len(path) == 0 {
		return
	}
	lw := s.strokeWidth(d)
	scale := drawingScale(d)
	if scale <= 1e-6 {
		scale = 1
	}
	dotR := 3 / scale

	d.Save().
		SetStrokeColor(col).
		SetLineWidth(lw)
	if len(path) >= 2 {
		d.BeginPath().MoveTo(float32(path[0].X), float32(path[0].Y))
		for i := 1; i < len(path); i++ {
			d.LineTo(float32(path[i].X), float32(path[i].Y))
		}
		d.Stroke()
	}
	for _, pt := range path {
		d.BeginPath().Arc(float32(pt.X), float32(pt.Y), dotR, 0, 2*math.Pi, false).Stroke()
	}
	d.Restore()
}

// DrawTarget strokes a crosshair reticle at target in col under d's active transform.
func (s *ShapeOverlay) DrawTarget(d *core.Drawing, target core.Point, col core.Color) {
	if d == nil {
		return
	}
	lw := s.strokeWidth(d)
	scale := drawingScale(d)
	if scale <= 1e-6 {
		scale = 1
	}
	tx, ty := float32(target.X), float32(target.Y)
	r := 5 / scale
	arm := 8 / scale

	d.Save().
		SetStrokeColor(col).
		SetLineWidth(lw).
		BeginPath().
		Arc(tx, ty, r, 0, 2*math.Pi, false).
		MoveTo(tx-arm, ty).
		LineTo(tx+arm, ty).
		MoveTo(tx, ty-arm).
		LineTo(tx, ty+arm).
		Stroke().
		Restore()
}

// DrawSteering draws world-space debug guides for a 2D steering agent under d's active transform:
// its waypoint path, target reticle and sightline, body radius, desired velocity arrow, and current velocity arrow.
func (s *ShapeOverlay) DrawSteering(d *core.Drawing, info SteeringDebug) {
	if d == nil {
		return
	}
	colors := resolveSteeringColors(info.Colors)
	velScale := info.VelocityScale
	if velScale <= 0 {
		velScale = 1
	}
	lw := s.strokeWidth(d)
	scale := drawingScale(d)
	if scale <= 1e-6 {
		scale = 1
	}
	ax, ay := float32(info.Agent.Pos.X), float32(info.Agent.Pos.Y)

	if len(info.Path) > 0 {
		s.DrawPath(d, info.Path, colors.Path)
		if info.WaypointIndex >= 0 && info.WaypointIndex < len(info.Path) {
			wp := info.Path[info.WaypointIndex]
			wx, wy := float32(wp.X), float32(wp.Y)
			s.DrawCircle(d, wx, wy, 6/scale, colors.Path)
			d.Save().
				SetStrokeColor(colors.Path).
				SetLineWidth(lw).
				BeginPath().
				MoveTo(ax, ay).
				LineTo(wx, wy).
				Stroke().
				Restore()
		}
	}

	if info.HasTarget {
		d.Save().
			SetStrokeColor(colors.Target).
			SetLineWidth(lw).
			BeginPath().
			MoveTo(ax, ay).
			LineTo(float32(info.Target.X), float32(info.Target.Y)).
			Stroke().
			Restore()
		s.DrawTarget(d, info.Target, colors.Target)
	}

	if info.Agent.Radius > 0 {
		s.DrawCircle(d, ax, ay, info.Agent.Radius, colors.Agent)
	}

	if info.DesiredVelocity.X != 0 || info.DesiredVelocity.Y != 0 {
		desired := core.Point{
			X: core.Points(float32(info.DesiredVelocity.X) * velScale),
			Y: core.Points(float32(info.DesiredVelocity.Y) * velScale),
		}
		s.DrawVector(d, info.Agent.Pos, desired, colors.Desired)
	}

	if info.Agent.Vel.X != 0 || info.Agent.Vel.Y != 0 {
		vel := core.Point{
			X: core.Points(float32(info.Agent.Vel.X) * velScale),
			Y: core.Points(float32(info.Agent.Vel.Y) * velScale),
		}
		s.DrawVector(d, info.Agent.Pos, vel, colors.Velocity)
	}
}

func resolveSteeringColors(c SteeringColors) SteeringColors {
	if c.Agent == 0 {
		c.Agent = core.RGB(0xE2E8F0)
	}
	if c.Velocity == 0 {
		c.Velocity = core.RGB(0x22C55E)
	}
	if c.Desired == 0 {
		c.Desired = core.RGB(0x38BDF8)
	}
	if c.Target == 0 {
		c.Target = core.RGB(0xF59E0B)
	}
	if c.Path == 0 {
		c.Path = core.RGB(0xA78BFA)
	}
	return c
}

// DrawVector draws a directional vector arrow under d's active transform while the overlay is visible.
func (o *Overlay) DrawVector(d *core.Drawing, origin, vec core.Point, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawVector(d, origin, vec, col)
}

// DrawPath draws a waypoint polyline under d's active transform while the overlay is visible.
func (o *Overlay) DrawPath(d *core.Drawing, path []core.Point, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawPath(d, path, col)
}

// DrawTarget draws a target reticle under d's active transform while the overlay is visible.
func (o *Overlay) DrawTarget(d *core.Drawing, target core.Point, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawTarget(d, target, col)
}

// DrawSteering draws steering agent guides (velocity, desired velocity, target, path) under d's active transform while the overlay is visible.
func (o *Overlay) DrawSteering(d *core.Drawing, info SteeringDebug) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawSteering(d, info)
}
