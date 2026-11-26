package editor

import (
	"fmt"
	"math"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Additional Spec color tokens for docs/design/editor/Animation.dc.html (shared specPanel/specRaised/etc. live in profiler_ui.go).
var (
	specBG          = darkSpecPalette.Bg
	specActiveNode  = CompositeOver(specAlpha(darkSpecPalette.Accent.Fg, 0.12), darkSpecPalette.Raised)
	specViewportBG  = darkSpecPalette.Viewport
	specViewportDot = darkSpecPalette.ViewportDot
	specEdgeLine    = CompositeOver(specAlpha(darkSpecPalette.TextFaint, 0.45), darkSpecPalette.Viewport)
	specAccentText  = CompositeOver(specAlpha(darkSpecPalette.TextPrimary, 0.62), darkSpecPalette.Accent.Fg)
	specFloorFill   = darkSpecPalette.LineRaised
)

const (
	stateNodeW float32 = 148
	stateNodeH float32 = 56
	anyNodeW   float32 = 88
	anyNodeH   float32 = 28
)

// StatesPanelView renders the sprite state machine graph editor panel (matching Animation.dc.html Screen 1) whenever a *.states.yaml document is open:
// header with file info, Play/Pause, Drive with keys toggle + interactive key pills, Save and Close;
// state graph canvas with state nodes (sprite thumbnail, name, clip meta), Any state badge, directed transition arrows and condition labels;
// right sidebar with live animated sprite preview card and LEAVING <STATE> outgoing transition list.
func StatesPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.states.doc != nil
		},
		Then: func(cx *core.Scope) core.Node { return statesPanel(cx, ed, rev) },
	}
}

func statesPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		doc := ed.states.doc
		if doc == nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "e=%s|s=%d|t=%d|", doc.Entry, len(doc.States), len(doc.Transitions))
		for _, st := range doc.States {
			px, py := stateNodePos(doc, st.Name)
			fmt.Fprintf(&b, "%s:%.0f,%.0f:%s|", st.Name, px, py, st.Clip)
		}
		for _, tr := range doc.Transitions {
			fmt.Fprintf(&b, "%s->%s:%s:%t|", tr.From, tr.To, tr.When, tr.OnEnd)
		}
		return b.String()
	})

	header := core.View{
		TestID: "states-header",
		Style: core.Style{
			Height:         core.Pt(44),
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(0, 12),
			Gap:            12,
			Background:     specPanel,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    specLine,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID: "states-title",
						ContentFunc: func() string {
							_ = rev.Get()
							if p := ed.StatesPath(); p != "" {
								return path.Base(strings.ReplaceAll(p, `\`, "/"))
							}
							if ed.states.doc != nil && ed.states.doc.Name != "" {
								return ed.states.doc.Name
							}
							return "states.yaml"
						},
						Style: core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
					},
					core.Text{
						TestID: "states-meta",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.states.doc == nil {
								return ""
							}
							return fmt.Sprintf("· %d states · %d transitions", len(ed.states.doc.States), len(ed.states.doc.Transitions))
						},
						Style: core.Style{FontSize: 12, Color: specTextMuted},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.StatesDirty()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "states-dirty-dot",
								Style: core.Style{
									Width:        core.Pt(6),
									Height:       core.Pt(6),
									CornerRadius: 3,
									Background:   specChanged,
								},
							}
						},
					},
				},
			},
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					// Interactive key pills visible while Drive with keys is active (Animation.dc.html lines 88-94).
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.StatesDriveWithKeys() && !ed.Playing()
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.HStack{
								Gap: 4,
								Children: []core.Node{
									driveKeyPill("states-key-move", "← →", "Hold left or right: run", func() bool {
										_ = rev.Get()
										return ed.StatesActionHeld("move_right") || ed.StatesActionHeld("move_left")
									}, func() {
										if ed.Playing() {
											return
										}
										if ed.StatesActionHeld("move_right") {
											ed.ReleaseStatesAction("move_right")
										} else {
											ed.PressStatesAction("move_right")
										}
									}),
									driveKeyPill("states-key-jump", "Space", "Space: jump", func() bool { return false }, func() {
										if ed.Playing() {
											return
										}
										ed.TriggerStatesAction("jump")
									}),
									driveKeyPill("states-key-attack", "J", "J: attack", func() bool { return false }, func() {
										if ed.Playing() {
											return
										}
										ed.TriggerStatesAction("attack")
									}),
									driveKeyPill("states-key-hurt", "H", "H: hurt", func() bool { return false }, func() {
										if ed.Playing() {
											return
										}
										ed.TriggerStatesAction("hurt")
									}),
									driveKeyPill("states-key-release", "Release", "Release all keys: idle", func() bool { return false }, func() {
										if ed.Playing() {
											return
										}
										ed.ReleaseAllStatesActions()
									}),
								},
							}
						},
					},
					// Drive with keys switch (Animation.dc.html line 95).
					core.View{
						TestID: "states-drive-toggle",
						Role:   core.RoleSwitch,
						Label:  "Drive with keys",
						OnPress: func(core.PressEvent) {
							if ed.Playing() {
								return
							}
							ed.SetStatesDriveWithKeys(!ed.StatesDriveWithKeys())
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							var op core.Opacity
							if ed.Playing() {
								op = core.Alpha(0.45)
							}
							return core.Style{
								Height:       core.Pt(28),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          8,
								Padding:      core.Edges{Left: 6, Right: 10},
								CornerRadius: 8,
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
								Opacity:      op,
							}
						},
						Children: []core.Node{
							core.View{
								StyleFunc: func() core.Style {
									_ = rev.Get()
									bg := specLineRaised
									if ed.StatesDriveWithKeys() {
										bg = specAccent
									}
									return core.Style{
										Width:        core.Pt(28),
										Height:       core.Pt(16),
										CornerRadius: 8,
										Position:     core.Relative,
										Background:   bg,
									}
								},
								Children: []core.Node{
									core.View{
										StyleFunc: func() core.Style {
											_ = rev.Get()
											left := core.Pt(2)
											if ed.StatesDriveWithKeys() {
												left = core.Pt(14)
											}
											return core.Style{
												Position:     core.Absolute,
												Top:          core.Pt(2),
												Left:         left,
												Width:        core.Pt(12),
												Height:       core.Pt(12),
												CornerRadius: 6,
												Background:   specTextStrong,
											}
										},
									},
								},
							},
							core.Text{
								Content: "Drive with keys",
								Style:   core.Style{FontSize: 12, Color: specText},
							},
						},
					},
					specButton("states-play", func() string {
						_ = rev.Get()
						if ed.StatesPlaying() {
							return "Pause"
						}
						return "Play"
					}, true, func() bool {
						_ = rev.Get()
						return ed.Playing()
					}, func() {
						if ed.StatesPlaying() {
							ed.PauseStates()
							return
						}
						ed.report(ed.PlayStates())
					}),
					specButton("states-save", func() string { return "Save" }, false, func() bool {
						_ = rev.Get()
						return ed.Playing()
					}, func() {
						ed.report(ed.saveStatesFromPanel())
					}),
					specButton("states-close", func() string { return "Close" }, false, func() bool {
						_ = rev.Get()
						return ed.Playing()
					}, func() {
						ed.report(ed.CloseStates())
					}),
				},
			},
		},
	}

	graphSize := core.NewSignal(cx, core.Size{})
	graphArea := core.View{
		TestID: "states-graph",
		OnLayout: func(b core.Bounds) {
			if b.Size != graphSize.GetUntracked() {
				graphSize.Set(b.Size)
			}
		},
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Position:   core.Relative,
			Background: specViewportBG,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			// Background dot grid + directed transition arrows canvas.
			core.Canvas{
				TestID: "states-graph-canvas",
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Left:     core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					drawStatesGraphCanvas(d, size, ed)
				},
			},
			// Interactive state node cards + transition edge label pills + Any state pill.
			core.Dynamic(func(cx *core.Scope) core.Node {
				sz := graphSize.Get()
				if structure.Get() == "" || ed.states.doc == nil {
					return nil
				}
				return statesGraphOverlay(cx, ed, rev, sz)
			}),
		},
	}

	sidebar := statesSidebar(cx, ed, rev)

	return core.View{
		TestID: "states-panel",
		OnKeyDown: func(ev core.KeyEvent) bool {
			return ed.StatesHandleKeyEvent(ev, true)
		},
		OnKeyUp: func(ev core.KeyEvent) {
			ed.StatesHandleKeyEvent(ev, false)
		},
		Style: dockedStyle(cx, core.Style{
			Height:      core.Pt(380),
			Background:  specBG,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
		}),
		Children: []core.Node{
			header,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.states.err != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{
							Padding:     core.Symmetric(6, 12),
							Background:  specPanel,
							Borders:     core.Edges{Bottom: 1},
							BorderColor: specLine,
						},
						Children: []core.Node{
							core.Text{
								TestID: "states-error",
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.states.err == nil {
										return ""
									}
									return "Preview: " + ed.states.err.Error()
								},
								Style: core.Style{FontSize: 11.5, Color: specProblem},
							},
						},
					}
				},
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					graphArea,
					sidebar,
				},
			},
		},
	}
}

func driveKeyPill(testID, text, label string, active func() bool, onPress func()) core.Node {
	return core.View{
		TestID:  testID,
		Role:    core.RoleButton,
		Label:   label,
		OnPress: func(core.PressEvent) { onPress() },
		StyleFunc: func() core.Style {
			border := specLineRaised
			bg := specRaised
			if active() {
				border = specAccent
				bg = specActiveNode
			}
			return core.Style{
				Height:         core.Pt(28),
				MinWidth:       core.Pt(28),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     bg,
				BorderWidth:    1,
				BorderColor:    border,
			}
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				Style:   core.Style{FontSize: 11.5, FontWeight: 500, Color: specTextStrong},
			},
		},
	}
}

func specButton(testID string, textFunc func() string, primary bool, disabled func() bool, onPress func()) core.Node {
	bg := specLineRaised
	fg := specText
	weight := core.FontWeight(500)
	if primary {
		bg = specAccent
		fg = specTextStrong
		weight = 600
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		OnPress: func(core.PressEvent) {
			if disabled != nil && disabled() {
				return
			}
			onPress()
		},
		StyleFunc: func() core.Style {
			var op core.Opacity
			if disabled != nil && disabled() {
				op = core.Alpha(0.45)
			}
			return core.Style{
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     bg,
				Opacity:        op,
			}
		},
		Children: []core.Node{
			core.Text{
				ContentFunc: textFunc,
				Style:       core.Style{FontSize: 12, FontWeight: weight, Color: fg},
			},
		},
	}
}

// stateNodePos returns the graph position of state name (using saved X/Y when present, or a clean deterministic grid fallback).
func stateNodePos(doc *animclip.StateMachineDoc, name string) (float32, float32) {
	if doc == nil {
		return 24, 24
	}
	for i, st := range doc.States {
		if st.Name == name {
			if st.X != nil && st.Y != nil {
				return *st.X, *st.Y
			}
			col := i % 3
			row := i / 3
			return 24 + float32(col)*210, 28 + float32(row)*110
		}
	}
	return 24, 24
}

// graphBoundsTransformFor maps document graph coordinates into the docked graph canvas so horizontal transitions have generous room for arrows and labels while all nodes and the Any state badge stay visible when the dock is resized.
func graphBoundsTransformFor(doc *animclip.StateMachineDoc, avail core.Size) (scaleX, scaleY, offX, offY float32) {
	if doc == nil || len(doc.States) == 0 {
		return 1, 1, 0, 0
	}
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := float32(-math.MaxFloat32), float32(-math.MaxFloat32)
	for _, st := range doc.States {
		x, y := stateNodePos(doc, st.Name)
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x)
		maxY = max(maxY, y)
	}
	spanX := maxX - minX
	spanY := maxY - minY
	targetSpanX := float32(660)
	if w := float32(avail.Width); w > 0 {
		hasAny := false
		for _, tr := range doc.Transitions {
			if tr.From == animclip.AnyState {
				hasAny = true
				break
			}
		}
		reserve := float32(24) + stateNodeW + 16
		if hasAny {
			reserve += 56 + anyNodeW
		}
		if fit := w - reserve; fit < targetSpanX {
			targetSpanX = max(fit, 120)
		}
	}
	scaleX = 1
	if spanX > 0 {
		scaleX = targetSpanX / spanX
		if scaleX > 1.75 {
			scaleX = 1.75
		}
	}
	targetH := float32(292)
	if h := float32(avail.Height); h > 0 && h-36 < targetH {
		targetH = max(h-36, stateNodeH+16)
	}
	scaleY = 1
	if spanY > 0 && spanY+stateNodeH > targetH {
		scaleY = (targetH - stateNodeH) / spanY
	}
	offX = 24 - minX*scaleX
	offY = 18 - minY*scaleY
	return scaleX, scaleY, offX, offY
}

func stateNodeScreenPos(doc *animclip.StateMachineDoc, name string, avail core.Size) (x, y float32) {
	gx, gy := stateNodePos(doc, name)
	sx, sy, ox, oy := graphBoundsTransformFor(doc, avail)
	return gx*sx + ox, gy*sy + oy
}

func anyNodeScreenPos(doc *animclip.StateMachineDoc, avail core.Size) (x, y float32) {
	var maxRight float32
	for _, st := range doc.States {
		nx, _ := stateNodeScreenPos(doc, st.Name, avail)
		maxRight = max(maxRight, nx+stateNodeW)
	}
	// Vertically align with the first wildcard transition's target state when present.
	y = 18
	for _, tr := range doc.Transitions {
		if tr.From == animclip.AnyState {
			_, ty := stateNodeScreenPos(doc, tr.To, avail)
			y = ty + (stateNodeH-anyNodeH)/2
			break
		}
	}
	gap := float32(110)
	if w := float32(avail.Width); w > 0 && maxRight+gap+anyNodeW > w-12 {
		gap = max(w-12-anyNodeW-maxRight, 16)
	}
	return maxRight + gap, y
}

func drawStatesGraphCanvas(d *core.Drawing, size core.Size, ed *Editor) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	// Spec dot-grid background (.vp: #15181d with #2a2f38 1px dots every 16px).
	d.SetFillColor(specViewportBG).FillRect(0, 0, w, h)
	d.SetFillColor(specViewportDot)
	for gy := float32(8); gy < h; gy += 16 {
		for gx := float32(8); gx < w; gx += 16 {
			d.FillRect(gx, gy, 1.5, 1.5)
		}
	}

	doc := ed.states.doc
	if doc == nil {
		return
	}
	cur := ed.StatesCurrentState()
	prev := ed.StatesPreviousState()

	// Draw directed edges between states.
	for _, tr := range doc.Transitions {
		var sx, sy, sw, sh float32
		if tr.From == animclip.AnyState {
			sx, sy = anyNodeScreenPos(doc, size)
			sw, sh = anyNodeW, anyNodeH
		} else {
			sx, sy = stateNodeScreenPos(doc, tr.From, size)
			sw, sh = stateNodeW, stateNodeH
		}
		tx, ty := stateNodeScreenPos(doc, tr.To, size)
		tw, th := stateNodeW, stateNodeH

		x1, y1 := sx+sw/2, sy+sh/2
		x2, y2 := tx+tw/2, ty+th/2
		// Offset parallel reverse transitions so bidirectional edges (idle <-> run) never overlap.
		dx, dy := x2-x1, y2-y1
		dist := float32(math.Hypot(float64(dx), float64(dy)))
		if dist < 1 {
			continue
		}
		nx, ny := -dy/dist, dx/dist
		hasReverse := hasTransitionBetween(doc, tr.To, tr.From)
		if hasReverse {
			x1 += nx * 10
			y1 += ny * 10
			x2 += nx * 10
			y2 += ny * 10
		}
		// Clip line endpoints to node bounding boxes.
		p1x, p1y := clipRayToRect(x1, y1, x2, y2, sx, sy, sw, sh)
		p2x, p2y := clipRayToRect(x2, y2, x1, y1, tx, ty, tw, th)

		hot := (tr.From == prev && tr.To == cur) || (tr.From == animclip.AnyState && tr.To == cur)
		col := specEdgeLine
		lw := float32(1.5)
		if hot {
			col = specAccent
			lw = 2
		}
		d.SetStrokeColor(col).SetLineWidth(lw)
		strokeLine(d, p1x, p1y, p2x, p2y)
		drawArrowHead(d, p1x, p1y, p2x, p2y, col)
	}
}

func strokeLine(d *core.Drawing, x1, y1, x2, y2 float32) {
	d.BeginPath().MoveTo(x1, y1).LineTo(x2, y2).Stroke()
}

func hasTransitionBetween(doc *animclip.StateMachineDoc, from, to string) bool {
	for _, tr := range doc.Transitions {
		if tr.From == from && tr.To == to {
			return true
		}
	}
	return false
}

func clipRayToRect(cx, cy, tx, ty, rx, ry, rw, rh float32) (float32, float32) {
	dx, dy := tx-cx, ty-cy
	if dx == 0 && dy == 0 {
		return cx, cy
	}
	t := float32(1)
	if dx > 0 {
		t = min(t, (rx+rw-cx)/dx)
	} else if dx < 0 {
		t = min(t, (rx-cx)/dx)
	}
	if dy > 0 {
		t = min(t, (ry+rh-cy)/dy)
	} else if dy < 0 {
		t = min(t, (ry-cy)/dy)
	}
	t = max(t, 0)
	return cx + dx*t, cy + dy*t
}

func drawArrowHead(d *core.Drawing, x1, y1, x2, y2 float32, col core.Color) {
	dx, dy := x2-x1, y2-y1
	dist := float32(math.Hypot(float64(dx), float64(dy)))
	if dist < 1 {
		return
	}
	ux, uy := dx/dist, dy/dist
	px, py := -uy, ux
	const sz float32 = 7
	const half float32 = 4
	bx, by := x2-ux*sz, y2-uy*sz
	d.SetStrokeColor(col).SetLineWidth(1.8)
	strokeLine(d, x2, y2, bx+px*half, by+py*half)
	strokeLine(d, x2, y2, bx-px*half, by-py*half)
}

func statesGraphOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], avail core.Size) core.Node {
	doc := ed.states.doc
	if doc == nil {
		return nil
	}
	var children []core.Node

	// Transition edge label pills.
	for i, tr := range doc.Transitions {
		labelText := tr.Label
		if labelText == "" {
			if tr.When != "" {
				labelText = tr.When
			} else if tr.OnEnd {
				labelText = "clip ends"
			}
		}
		if labelText == "" {
			continue
		}
		var sx, sy, sw, sh float32
		if tr.From == animclip.AnyState {
			sx, sy = anyNodeScreenPos(doc, avail)
			sw, sh = anyNodeW, anyNodeH
		} else {
			sx, sy = stateNodeScreenPos(doc, tr.From, avail)
			sw, sh = stateNodeW, stateNodeH
		}
		tx, ty := stateNodeScreenPos(doc, tr.To, avail)
		x1, y1 := sx+sw/2, sy+sh/2
		x2, y2 := tx+stateNodeW/2, ty+stateNodeH/2
		dx, dy := x2-x1, y2-y1
		dist := float32(math.Hypot(float64(dx), float64(dy)))
		if dist < 1 {
			continue
		}
		hasReverse := hasTransitionBetween(doc, tr.To, tr.From)
		if hasReverse {
			nx, ny := -dy/dist, dx/dist
			x1 += nx * 10
			y1 += ny * 10
			x2 += nx * 10
			y2 += ny * 10
		}
		p1x, p1y := clipRayToRect(x1, y1, x2, y2, sx, sy, sw, sh)
		p2x, p2y := clipRayToRect(x2, y2, x1, y1, tx, ty, stateNodeW, stateNodeH)
		mx, my := (p1x+p2x)/2, (p1y+p2y)/2
		if hasReverse {
			nx, ny := -dy/dist, dx/dist
			mx += nx * 8
			my += ny * 8
		}
		estW := float32(len(labelText))*6.2 + 12
		trCopy := tr
		children = append(children, core.View{
			TestID: fmt.Sprintf("states-edge-label-%d", i),
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(core.Points(max(mx-estW/2, 4))),
				Top:            core.Pt(core.Points(max(my-10, 4))),
				Height:         core.Pt(20),
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     specBG,
			},
			Children: []core.Node{
				core.Text{
					Content: labelText,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						cur := ed.StatesCurrentState()
						prev := ed.StatesPreviousState()
						hot := (trCopy.From == prev && trCopy.To == cur) || (trCopy.From == animclip.AnyState && trCopy.To == cur)
						col := specTextSecondary
						if hot {
							col = specAccentText
						}
						return core.Style{FontSize: 11, Color: col}
					},
				},
			},
		})
	}

	// Any state badge when any transition uses "*".
	hasAny := false
	for _, tr := range doc.Transitions {
		if tr.From == animclip.AnyState {
			hasAny = true
			break
		}
	}
	if hasAny {
		ax, ay := anyNodeScreenPos(doc, avail)
		children = append(children, core.View{
			TestID: "states-node-any",
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(core.Points(ax)),
				Top:            core.Pt(core.Points(ay)),
				Width:          core.Pt(core.Points(anyNodeW)),
				Height:         core.Pt(core.Points(anyNodeH)),
				CornerRadius:   14,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     specBG,
				BorderWidth:    1,
				BorderColor:    specEdgeLine,
			},
			Children: []core.Node{
				core.Text{
					Content: "Any state",
					Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
				},
			},
		})
	}

	// State node buttons (Animation.dc.html lines 124-132).
	for _, st := range doc.States {
		stateName := st.Name
		nx, ny := stateNodeScreenPos(doc, stateName, avail)
		meta, _ := ed.StatesClipMetaFor(stateName)
		metaSummary := meta.Summary
		if metaSummary == "" {
			metaSummary = path.Base(st.Clip)
		}
		isEntry := stateName == doc.Entry

		thumb := core.Canvas{
			TestID: "states-thumb-" + stateName,
			Style: core.Style{
				Width:        core.Pt(40),
				Height:       core.Pt(40),
				CornerRadius: 8,
				Background:   specRaised,
			},
			Draw: func(d *core.Drawing, size core.Size) {
				_ = rev.Get()
				frameName := meta.FirstFrame
				if ed.StatesCurrentState() == stateName {
					if active := ed.StatesActiveFrame(); active != "" {
						frameName = active
					}
				}
				drawSpriteThumbnail(d, size, ed, ed.states.meta.target, frameName)
			},
		}

		children = append(children, core.View{
			TestID: "states-node-" + stateName,
			Role:   core.RoleButton,
			Label:  "State " + stateName,
			OnPress: func(core.PressEvent) {
				ed.SelectStatesNode(stateName)
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				active := ed.StatesCurrentState() == stateName
				bg := specRaised
				border := specLineRaised
				bw := core.Points(1)
				if active {
					bg = specActiveNode
					border = specAccent
					bw = 2
				}
				return core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(core.Points(nx)),
					Top:          core.Pt(core.Points(ny)),
					Width:        core.Pt(core.Points(stateNodeW)),
					Height:       core.Pt(core.Points(stateNodeH)),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.All(8),
					CornerRadius: 12,
					Background:   bg,
					BorderWidth:  bw,
					BorderColor:  border,
				}
			},
			Children: []core.Node{
				thumb,
				core.View{
					Style: core.Style{FlexGrow: 1, Gap: 2},
					Children: []core.Node{
						ui.HStack{
							Gap: 4,
							Children: []core.Node{
								core.Text{
									Content: stateName,
									StyleFunc: func() core.Style {
										_ = rev.Get()
										col := specText
										if ed.StatesCurrentState() == stateName {
											col = specTextStrong
										}
										return core.Style{FontSize: 12.5, FontWeight: 600, Color: col}
									},
								},
								core.Show{
									When: func() bool { return isEntry },
									Then: func(cx *core.Scope) core.Node {
										return core.Text{
											Content: "ENTRY",
											Style:   core.Style{FontSize: 9, FontWeight: 600, Color: specAdded},
										}
									},
								},
							},
						},
						core.Text{
							Content: metaSummary,
							Style:   core.Style{FontSize: 11, Color: specTextMuted},
						},
					},
				},
			},
		})
	}

	return core.View{
		Style: core.Style{
			Position: core.Absolute,
			Top:      core.Pt(0),
			Left:     core.Pt(0),
			Right:    core.Pt(0),
			Bottom:   core.Pt(0),
		},
		Children: children,
	}
}

func statesSidebar(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	// Live preview card (Animation.dc.html lines 134-144).
	previewCard := core.View{
		TestID: "states-preview-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   specPanel,
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "states-preview-canvas",
				Style: core.Style{
					Height: core.Pt(110),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					drawStatesPreviewCard(d, size, ed)
				},
			},
			core.View{
				Style: core.Style{
					Height:      core.Pt(30),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(0, 12),
					Background:  specRaised,
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   specAccent,
						},
					},
					core.Text{
						TestID: "states-current-state",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.StatesCurrentState()
						},
						Style: core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong, FlexGrow: 1},
					},
					core.Text{
						TestID: "states-preview-target",
						ContentFunc: func() string {
							_ = rev.Get()
							if t := ed.StatesTarget(); t != "" {
								return t
							}
							return "preview"
						},
						Style: core.Style{FontSize: 11.5, Color: specTextMuted},
					},
				},
			},
		},
	}

	// LEAVING <STATE> section (Animation.dc.html lines 258-267).
	leavingKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		cur := ed.StatesCurrentState()
		outs := ed.StatesOutgoingFor(cur)
		var b strings.Builder
		b.WriteString(cur)
		for _, o := range outs {
			fmt.Fprintf(&b, "|%s:%s", o.To, o.Detail)
		}
		return b.String()
	})

	leavingList := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = leavingKey.Get()
		cur := ed.StatesCurrentState()
		outs := ed.StatesOutgoingFor(cur)
		if len(outs) == 0 {
			return core.Text{
				TestID:  "states-leaving-empty",
				Content: "No outgoing transitions",
				Style:   core.Style{FontSize: 12, Color: specTextMuted},
			}
		}
		rows := make([]core.Node, 0, len(outs))
		for i, o := range outs {
			toState := o.To
			rows = append(rows, core.View{
				TestID: fmt.Sprintf("states-leaving-%d", i),
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("Transition to %s: %s", o.To, o.Detail),
				OnPress: func(core.PressEvent) {
					ed.SelectStatesNode(toState)
				},
				Style: core.Style{
					MinHeight:    core.Pt(28),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(2, 6),
					CornerRadius: 6,
					Background:   specRaised,
				},
				Children: []core.Node{
					core.Text{Content: "→", Style: core.Style{FontSize: 12, Color: specTextMuted}},
					core.Text{Content: o.To, Style: core.Style{Width: core.Pt(56), FontSize: 12, FontWeight: 600, Color: specTextStrong}},
					core.Text{Content: o.Detail, Style: core.Style{FlexGrow: 1, FontSize: 11.5, Color: specTextSecondary}},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 4}, Children: rows}
	})

	return core.View{
		TestID: "states-sidebar",
		Style: core.Style{
			Width:       core.Pt(360),
			Padding:     core.All(12),
			Gap:         12,
			Background:  specPanel,
			Borders:     core.Edges{Left: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			previewCard,
			core.Text{
				TestID: "states-leaving-header",
				ContentFunc: func() string {
					_ = rev.Get()
					return "LEAVING " + strings.ToUpper(ed.StatesCurrentState())
				},
				Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
			},
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1},
				Children: []core.Node{leavingList},
			},
		},
	}
}

func drawSpriteThumbnail(d *core.Drawing, size core.Size, ed *Editor, target, frameName string) {
	sw, sh := float32(size.Width), float32(size.Height)
	if sw <= 0 || sh <= 0 {
		return
	}
	// Subtle checkerboard background matching .ck in Animation.dc.html.
	d.SetFillColor(specRaised).FillRect(0, 0, sw, sh)
	d.SetFillColor(specLine)
	for y := float32(0); y < sh; y += 8 {
		for x := float32(0); x < sw; x += 8 {
			if (int(x/8)+int(y/8))%2 == 0 {
				d.FillRect(x, y, 8, 8)
			}
		}
	}
	entry, ok := ed.ClipFrameEntry(target, frameName)
	if !ok {
		return
	}
	f := entry.Rect
	if f.W <= 0 || f.H <= 0 {
		return
	}
	scale := min((sw-4)/f.W, (sh-4)/f.H)
	dw, dh := f.W*scale, f.H*scale
	d.DrawImageFrom(entry.Image, f.X, f.Y, f.W, f.H, (sw-dw)/2, (sh-dh)/2, dw, dh)
}

func drawStatesPreviewCard(d *core.Drawing, size core.Size, ed *Editor) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(specActiveNode).FillRect(0, 0, w, h)
	// Floor strip (Animation.dc.html line 136).
	floorH := float32(24)
	floorY := h - floorH
	d.SetFillColor(specFloorFill).FillRect(0, floorY, w, floorH)
	d.SetStrokeColor(specLine).SetLineWidth(1.5)
	strokeLine(d, 0, floorY, w, floorY)
	for gx := float32(24); gx < w; gx += 24 {
		strokeLine(d, gx, floorY, gx, h)
	}

	frameName := ed.StatesActiveFrame()
	if frameName == "" {
		if m, ok := ed.StatesClipMetaFor(ed.StatesCurrentState()); ok {
			frameName = m.FirstFrame
		}
	}
	entry, ok := ed.ClipFrameEntry(ed.states.meta.target, frameName)
	if !ok {
		return
	}
	f := entry.Rect
	if f.W <= 0 || f.H <= 0 {
		return
	}
	scale := min(48/f.W, 48/f.H)
	dw, dh := f.W*scale, f.H*scale
	// Reflect simulated vertical jump offset from InputDriver.
	jumpOffset := float32(ed.StatesDriverPosY() * 0.4)
	if jumpOffset < -(floorY - dh - 4) {
		jumpOffset = -(floorY - dh - 4)
	}
	dx := (w - dw) / 2
	dy := floorY - dh + jumpOffset
	d.DrawImageFrom(entry.Image, f.X, f.Y, f.W, f.H, dx, dy, dw, dh)
}
