package editor

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ParticleEditorPanelView renders the Particle Emitter editor panel docked below the viewport while an emitter is open: a live preview canvas with Play/Pause, Restart, Step and Burst controls, an interactive curve widget for Size/Alpha/Rotation/Color/Trail curves, an atlas frame picker, and undoable controls for every field of particles.Config.
func ParticleEditorPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ParticleEmitterOpen()
	})
	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node { return particleEditorPanel(cx, ed, rev) },
	}
}

func particleEditorPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	// structuralKey changes only when list lengths, shape kind, trail presence, target path, or active curve kind change, preserving input focus during numeric/text edits.
	structuralKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		cfg := ed.ParticleActiveConfig()
		if cfg == nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "path=%v|shape=%s|bursts=%d|trail=%v|subs=%d|curve=%s|stop=%d|atlas=%s|frames=%s",
			ed.ParticleTargetPath(),
			cfg.Shape.Kind(),
			len(cfg.Bursts),
			cfg.Trail != nil,
			len(cfg.SubEmitters),
			ed.ParticleActiveCurve(),
			ed.ParticleSelectedStop(),
			ed.ParticlePreviewAtlas(),
			strings.Join(cfg.FrameNames, ","),
		)
		return b.String()
	})

	header := core.View{
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            8,
			Wrap:           core.Wrap,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID: "particles-title",
						ContentFunc: func() string {
							_ = rev.Get()
							title := "Particle Emitter"
							if p := ed.ParticleEmitterPath(); p != "" {
								title += " (" + filepath.Base(p) + ")"
							}
							if ed.ParticleEmitterDirty() {
								title += "*"
							}
							if tp := ed.ParticleTargetPath(); len(tp) > 0 {
								var parts []string
								for _, i := range tp {
									parts = append(parts, fmt.Sprintf("Sub #%d", i+1))
								}
								title += " > " + strings.Join(parts, " > ")
							}
							return title
						},
						Style: core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return len(ed.ParticleTargetPath()) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:   "particles-target-root",
								Text:     "Back to Root",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonSecondary,
								Disabled: disabled,
								OnPress: func() {
									ed.report(ed.SetParticleTargetPath(nil))
								},
							}
						},
					},
				},
			},
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					ui.Button{
						TestID:  "particles-preview-play",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonPrimary,
						TextFunc: func() string {
							_ = rev.Get()
							if ed.ParticlePreviewPlaying() {
								return "Pause"
							}
							return "Play"
						},
						OnPress: func() {
							if ed.ParticlePreviewPlaying() {
								ed.PauseParticlePreview()
								return
							}
							ed.report(ed.PlayParticlePreview())
						},
					},
					ui.Button{
						TestID:  "particles-preview-restart",
						Text:    "Restart",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() { ed.RestartParticlePreview() },
					},
					ui.Button{
						TestID:  "particles-preview-step",
						Text:    "Step",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() {
							rate := ed.PlayRate
							if rate <= 0 {
								rate = 60
							}
							ed.StepParticlePreview(time.Second / time.Duration(rate))
						},
					},
					ui.Button{
						TestID:  "particles-preview-burst",
						Text:    "Burst",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() { ed.BurstParticlePreview(12) },
					},
					core.Text{
						TestID: "particles-preview-stats",
						ContentFunc: func() string {
							_ = rev.Get()
							em := ed.ParticlePreview()
							count := 0
							if em != nil {
								count = em.TotalCount()
							}
							return fmt.Sprintf("%d live | %.2fs", count, ed.ParticlePreviewElapsed().Seconds())
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
					ui.Button{
						TestID:   "particles-save",
						Text:     "Save Emitter",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonOutline,
						Disabled: disabled,
						OnPress:  func() { ed.report(ed.saveParticleEmitterFromPanel()) },
					},
					ui.Button{
						TestID:   "particles-close",
						Text:     "Close",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: disabled,
						OnPress:  func() { ed.report(ed.CloseParticleEmitter()) },
					},
				},
			},
		},
	}

	previewColumn := core.View{
		TestID: "particles-preview-column",
		Style: core.Style{
			Width: core.Pt(244),
			Gap:   6,
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "particles-preview-canvas",
				Style: core.Style{
					Width:        core.Pt(244),
					Height:       core.Pt(170),
					Background:   core.RGB(0x0f172a),
					CornerRadius: 6,
					BorderWidth:  1,
					BorderColor:  colors.Line(),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					drawParticlePreviewCanvas(d, size, ed)
				},
			},
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Select[string]{
								TestID:      "particles-preview-atlas",
								Label:       "Preview Atlas",
								Placeholder: "No Atlas",
								OptionsFunc: func() []ui.Option[string] {
									_ = rev.Get()
									names := ed.ParticleAtlasNames()
									opts := make([]ui.Option[string], 0, len(names))
									for _, n := range names {
										opts = append(opts, ui.Option[string]{Value: n, Label: n})
									}
									return opts
								},
								Value: func() string {
									_ = rev.Get()
									return ed.ParticlePreviewAtlas()
								},
								OnChange: func(a string) { ed.SetParticlePreviewAtlas(a) },
								OnClear:  func() { ed.SetParticlePreviewAtlas("") },
							},
						},
					},
					ui.ToggleButton{
						TestID: "particles-preview-floor",
						Text:   "Floor",
						On: func() bool {
							_ = rev.Get()
							return ed.ParticlePreviewFloor()
						},
						OnChange: func(on bool) { ed.SetParticlePreviewFloor(on) },
					},
				},
			},
		},
	}

	fieldsArea := ui.ScrollView{
		TestID: "particles-fields-scroll",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
		},
		ContentStyle: core.Style{
			Gap:     10,
			Padding: core.Edges{Right: 6},
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = structuralKey.Get()
				if ed.ParticleActiveConfig() == nil {
					return nil
				}
				return particleFieldsView(cx, ed, rev, disabled)
			}),
		},
	}

	return core.View{
		TestID: "particles-panel",
		Style: dockedStyle(cx, core.Style{
			Height:      core.Pt(276),
			Background:  colors.Raised(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
			Padding:     core.Symmetric(8, 12),
			Gap:         8,
		}),
		Children: []core.Node{
			header,
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       12,
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					previewColumn,
					fieldsArea,
				},
			},
		},
	}
}

func drawParticlePreviewCanvas(d *core.Drawing, size core.Size, ed *Editor) {
	if d == nil {
		return
	}
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	cx, cy := w/2, h/2

	// Subtle grid & origin crosshair.
	d.Save()
	d.SetStrokeColor(core.RGBA(0x94A3B8, 0.16)).SetLineWidth(1)
	d.BeginPath().MoveTo(cx, 0).LineTo(cx, h).MoveTo(0, cy).LineTo(w, cy).Stroke()

	em := ed.ParticlePreview()
	if em == nil {
		d.Restore()
		return
	}

	// Translate so emitter's (X, Y) sits at the center of the preview canvas.
	d.Translate(cx-em.X, cy-em.Y)

	// Draw spawn shape guide.
	d.SetStrokeColor(core.RGBA(0x38BDF8, 0.45)).SetLineDash(4, 3).SetLineWidth(1)
	sh := em.Shape
	switch sh.Kind() {
	case "circle", "ring":
		if r := sh.Radius(); r > 0 {
			d.BeginPath().Arc(em.X, em.Y, r, 0, 2*math.Pi, false).Stroke()
		}
	case "rect":
		if rw, rh := sh.Width(), sh.Height(); rw > 0 || rh > 0 {
			d.BeginPath().Rect(em.X-rw/2, em.Y-rh/2, rw, rh).Stroke()
		}
	}
	d.SetLineDash()

	// Draw preview floor collider when enabled.
	if ed.ParticlePreviewFloor() {
		fy := em.Y + 60
		d.SetStrokeColor(core.RGBA(0xF59E0B, 0.7)).SetLineWidth(1.5)
		d.BeginPath().MoveTo(em.X-w, fy).LineTo(em.X+w, fy).Stroke()
	}

	// Draw emitter (sprites + trails + sub-emitters).
	em.Draw(d)

	// If no atlas or frames are bound, draw fallback colored circles so particles are always visible in the live preview.
	if em.Atlas == nil || len(em.Frames) == 0 {
		drawFallbackEmitterParticles(d, em)
	}
	d.Restore()
}

func drawFallbackEmitterParticles(d *core.Drawing, em *particles.Emitter) {
	if em == nil {
		return
	}
	for _, p := range em.Particles() {
		if p.Alpha <= 0 || p.Size <= 0 {
			continue
		}
		wx, wy := em.ParticleWorldPos(p)
		radius := max(float32(1.5), 3.5*p.Size)
		col := (p.Color & 0xffffff00) | core.Color(uint32(clamp01f32(p.Alpha)*255+0.5))
		d.SetFillColor(col)
		d.BeginPath().Arc(wx, wy, radius, 0, 2*math.Pi, false).Fill()
	}
	for _, ch := range em.Children() {
		drawFallbackEmitterParticles(d, ch)
	}
}

func particleFieldsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	activeCfg := func() particles.Config {
		_ = rev.Get()
		if c := ed.ParticleActiveConfig(); c != nil {
			return *c
		}
		return particles.Config{}
	}
	secTitle := func(text string) core.Node {
		return core.Text{
			Content: text,
			Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
		}
	}
	numField := func(testID, label string, minV, maxV, step float64, get func() float64, set func(float64)) core.Node {
		return core.View{
			Style: core.Style{Width: core.Pt(118), Gap: 2},
			Children: []core.Node{
				core.Text{Content: label, Style: core.Style{FontSize: 10, Color: colors.Muted()}},
				ui.NumberInput{
					TestID:   testID,
					Label:    label,
					Min:      minV,
					Max:      maxV,
					Step:     step,
					Disabled: disabled,
					Value:    get,
					OnChange: set,
				},
			},
		}
	}

	cfg := activeCfg()

	// ---- Shape controls ----
	shapeNodes := []core.Node{
		core.View{
			Style: core.Style{Width: core.Pt(118), Gap: 2},
			Children: []core.Node{
				core.Text{Content: "Shape", Style: core.Style{FontSize: 10, Color: colors.Muted()}},
				ui.Select[string]{
					TestID:   "particles-shape-kind",
					Label:    "Shape Kind",
					Disabled: disabled,
					Options: []ui.Option[string]{
						{Value: "point", Label: "point"},
						{Value: "circle", Label: "circle"},
						{Value: "ring", Label: "ring"},
						{Value: "rect", Label: "rect"},
					},
					Value: func() string { return activeCfg().Shape.Kind() },
					OnChange: func(kind string) {
						c := activeCfg()
						switch kind {
						case "circle":
							r := c.Shape.Radius()
							if r <= 0 {
								r = 12
							}
							ed.report(ed.SetParticleShape(particles.Circle(r)))
						case "ring":
							r := c.Shape.Radius()
							if r <= 0 {
								r = 16
							}
							ed.report(ed.SetParticleShape(particles.Ring(r)))
						case "rect":
							w, h := c.Shape.Width(), c.Shape.Height()
							if w <= 0 {
								w = 24
							}
							if h <= 0 {
								h = 16
							}
							ed.report(ed.SetParticleShape(particles.Rect(w, h)))
						default:
							ed.report(ed.SetParticleShape(particles.Point()))
						}
					},
				},
			},
		},
	}
	switch cfg.Shape.Kind() {
	case "circle", "ring":
		shapeNodes = append(shapeNodes, numField("particles-shape-radius", "Radius", 0, 10000, 1,
			func() float64 { return float64(activeCfg().Shape.Radius()) },
			func(v float64) {
				if activeCfg().Shape.Kind() == "ring" {
					ed.report(ed.SetParticleShape(particles.Ring(float32(v))))
				} else {
					ed.report(ed.SetParticleShape(particles.Circle(float32(v))))
				}
			},
		))
	case "rect":
		shapeNodes = append(shapeNodes,
			numField("particles-shape-width", "Width", 0, 10000, 1,
				func() float64 { return float64(activeCfg().Shape.Width()) },
				func(v float64) {
					ed.report(ed.SetParticleShape(particles.Rect(float32(v), activeCfg().Shape.Height())))
				},
			),
			numField("particles-shape-height", "Height", 0, 10000, 1,
				func() float64 { return float64(activeCfg().Shape.Height()) },
				func(v float64) { ed.report(ed.SetParticleShape(particles.Rect(activeCfg().Shape.Width(), float32(v)))) },
			),
		)
	}

	// ---- General & Shape section ----
	generalRow := core.View{
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignEnd, Gap: 8},
		Children: append([]core.Node{
			core.View{
				Style: core.Style{Width: core.Pt(104), Gap: 2},
				Children: []core.Node{
					core.Text{Content: "Space", Style: core.Style{FontSize: 10, Color: colors.Muted()}},
					ui.Select[string]{
						TestID:   "particles-space",
						Label:    "Simulation Space",
						Disabled: disabled,
						Options: []ui.Option[string]{
							{Value: "world", Label: "world"},
							{Value: "local", Label: "local"},
						},
						Value: func() string { return activeCfg().Space.String() },
						OnChange: func(v string) {
							if v == "local" {
								ed.report(ed.SetParticleSpace(particles.SpaceLocal))
							} else {
								ed.report(ed.SetParticleSpace(particles.SpaceWorld))
							}
						},
					},
				},
			},
			numField("particles-rate", "Rate (/s)", 0, 100000, 5,
				func() float64 { return float64(activeCfg().Rate) },
				func(v float64) { ed.report(ed.SetParticleRate(float32(v))) },
			),
			numField("particles-max-particles", "Max Particles", 0, 100000, 16,
				func() float64 { return float64(activeCfg().MaxParticles) },
				func(v float64) { ed.report(ed.SetParticleMaxParticles(int(math.Round(v)))) },
			),
			numField("particles-x", "X", -100000, 100000, 1,
				func() float64 { return float64(activeCfg().X) },
				func(v float64) { ed.report(ed.SetParticlePosition(float32(v), activeCfg().Y)) },
			),
			numField("particles-y", "Y", -100000, 100000, 1,
				func() float64 { return float64(activeCfg().Y) },
				func(v float64) { ed.report(ed.SetParticlePosition(activeCfg().X, float32(v))) },
			),
			numField("particles-rotation", "Rotation (rad)", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().Rotation) },
				func(v float64) { ed.report(ed.SetParticleRotation(float32(v))) },
			),
			numField("particles-z", "Z Order", -10000, 10000, 1,
				func() float64 { return float64(activeCfg().Z) },
				func(v float64) { ed.report(ed.SetParticleZ(int(math.Round(v)))) },
			),
			numField("particles-seed", "Seed", 0, 1e15, 1,
				func() float64 { return float64(activeCfg().Seed) },
				func(v float64) {
					if v < 0 {
						v = 0
					}
					ed.report(ed.SetParticleSeed(uint64(math.Round(v))))
				},
			),
		}, shapeNodes...),
	}

	// ---- Bursts section ----
	burstRows := make([]core.Node, 0, len(cfg.Bursts)+1)
	for i := range cfg.Bursts {
		idx := i
		burstRows = append(burstRows, ui.HStack{
			Gap: 6,
			Children: []core.Node{
				numField(fmt.Sprintf("particles-burst-time-%d", idx), "Time (s)", 0, 3600, 0.1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return 0
						}
						return c.Bursts[idx].Time.Seconds()
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return
						}
						b := c.Bursts[idx]
						b.Time = durationOfSeconds(v)
						ed.report(ed.SetParticleBurst(idx, b))
					},
				),
				numField(fmt.Sprintf("particles-burst-count-%d", idx), "Count", 0, 10000, 1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return 0
						}
						return float64(c.Bursts[idx].Count)
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return
						}
						b := c.Bursts[idx]
						b.Count = int(math.Round(v))
						ed.report(ed.SetParticleBurst(idx, b))
					},
				),
				numField(fmt.Sprintf("particles-burst-cycles-%d", idx), "Cycles", -1, 1000, 1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return 1
						}
						return float64(c.Bursts[idx].Cycles)
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return
						}
						b := c.Bursts[idx]
						b.Cycles = int(math.Round(v))
						ed.report(ed.SetParticleBurst(idx, b))
					},
				),
				numField(fmt.Sprintf("particles-burst-interval-%d", idx), "Interval (s)", 0, 3600, 0.1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return 0
						}
						return c.Bursts[idx].Interval.Seconds()
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.Bursts) {
							return
						}
						b := c.Bursts[idx]
						b.Interval = durationOfSeconds(v)
						ed.report(ed.SetParticleBurst(idx, b))
					},
				),
				ui.Button{
					TestID:   fmt.Sprintf("particles-burst-remove-%d", idx),
					Text:     "Remove",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress:  func() { ed.report(ed.RemoveParticleBurst(idx)) },
				},
			},
		})
	}
	burstRows = append(burstRows, ui.Button{
		TestID:   "particles-burst-add",
		Text:     "+ Add Burst",
		Size:     ui.SizeSmall,
		Variant:  ui.ButtonSecondary,
		Disabled: disabled,
		OnPress: func() {
			ed.report(ed.AddParticleBurst(particles.Burst{Time: 0, Count: 12, Cycles: 1, Interval: time.Second}))
		},
	})

	// ---- Lifetime & Motion section ----
	motionRow := core.View{
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignEnd, Gap: 8},
		Children: []core.Node{
			numField("particles-lifetime-min", "Life Min (s)", 0, 3600, 0.05,
				func() float64 { return activeCfg().LifetimeMin.Seconds() },
				func(v float64) { ed.report(ed.SetParticleLifetime(durationOfSeconds(v), activeCfg().LifetimeMax)) },
			),
			numField("particles-lifetime-max", "Life Max (s)", 0, 3600, 0.05,
				func() float64 { return activeCfg().LifetimeMax.Seconds() },
				func(v float64) { ed.report(ed.SetParticleLifetime(activeCfg().LifetimeMin, durationOfSeconds(v))) },
			),
			numField("particles-speed-min", "Speed Min", -10000, 10000, 5,
				func() float64 { return float64(activeCfg().SpeedMin) },
				func(v float64) { ed.report(ed.SetParticleSpeed(float32(v), activeCfg().SpeedMax)) },
			),
			numField("particles-speed-max", "Speed Max", -10000, 10000, 5,
				func() float64 { return float64(activeCfg().SpeedMax) },
				func(v float64) { ed.report(ed.SetParticleSpeed(activeCfg().SpeedMin, float32(v))) },
			),
			numField("particles-angle-min", "Angle Min", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().AngleMin) },
				func(v float64) { ed.report(ed.SetParticleAngle(float32(v), activeCfg().AngleMax)) },
			),
			numField("particles-angle-max", "Angle Max", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().AngleMax) },
				func(v float64) { ed.report(ed.SetParticleAngle(activeCfg().AngleMin, float32(v))) },
			),
			numField("particles-gravity-x", "Gravity X", -10000, 10000, 5,
				func() float64 { return float64(activeCfg().GravityX) },
				func(v float64) { ed.report(ed.SetParticleGravity(float32(v), activeCfg().GravityY)) },
			),
			numField("particles-gravity-y", "Gravity Y", -10000, 10000, 5,
				func() float64 { return float64(activeCfg().GravityY) },
				func(v float64) { ed.report(ed.SetParticleGravity(activeCfg().GravityX, float32(v))) },
			),
			numField("particles-init-rot-min", "Init Rot Min", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().InitialRotationMin) },
				func(v float64) { ed.report(ed.SetParticleInitialRotation(float32(v), activeCfg().InitialRotationMax)) },
			),
			numField("particles-init-rot-max", "Init Rot Max", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().InitialRotationMax) },
				func(v float64) { ed.report(ed.SetParticleInitialRotation(activeCfg().InitialRotationMin, float32(v))) },
			),
			numField("particles-rot-speed-min", "Rot Spd Min", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().RotationSpeedMin) },
				func(v float64) { ed.report(ed.SetParticleRotationSpeed(float32(v), activeCfg().RotationSpeedMax)) },
			),
			numField("particles-rot-speed-max", "Rot Spd Max", -100, 100, 0.1,
				func() float64 { return float64(activeCfg().RotationSpeedMax) },
				func(v float64) { ed.report(ed.SetParticleRotationSpeed(activeCfg().RotationSpeedMin, float32(v))) },
			),
		},
	}

	// ---- Curves section (interactive curve widget) ----
	curveSection := particleCurveWidgetView(cx, ed, rev, disabled)

	// ---- Frames from Atlas Picker section ----
	frameChips := make([]core.Node, 0, len(cfg.FrameNames)+8)
	atlasFrames := ed.ParticleAtlasFrames("")
	if len(atlasFrames) > 0 {
		for _, ent := range atlasFrames {
			fn := ent.Frame
			selected := slices.Contains(cfg.FrameNames, fn)
			frameChips = append(frameChips, ui.ToggleButton{
				TestID:   "particles-frame-toggle-" + fn,
				Text:     fn,
				Disabled: disabled,
				On:       func() bool { return selected },
				OnChange: func(bool) { ed.report(ed.ToggleParticleFrame(fn)) },
			})
		}
	} else if len(cfg.FrameNames) == 0 {
		frameChips = append(frameChips, core.Text{
			Content: "No atlas frames available (select a Preview Atlas or load an atlas in the resolver)",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		})
	}
	// Show any configured frame names not found in the currently selected atlas so they can still be removed.
	for _, fn := range cfg.FrameNames {
		name := fn
		if slices.ContainsFunc(atlasFrames, func(a AssetEntry) bool { return a.Frame == name }) {
			continue
		}
		frameChips = append(frameChips, ui.Button{
			TestID:   "particles-frame-remove-" + name,
			Text:     name + " ×",
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonOutline,
			Disabled: disabled,
			OnPress:  func() { ed.report(ed.ToggleParticleFrame(name)) },
		})
	}

	// ---- Collision section ----
	collisionRow := core.View{
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8},
		Children: []core.Node{
			ui.Checkbox{
				TestID:   "particles-collision-enabled",
				Label:    "Enabled",
				Disabled: disabled,
				Checked:  func() bool { return activeCfg().Collision.Enabled },
				OnChange: func(on bool) {
					col := activeCfg().Collision
					col.Enabled = on
					if on && col.Mode == particles.CollideNone {
						col.Mode = particles.CollideBounce
					}
					ed.report(ed.SetParticleCollision(col))
				},
			},
			core.View{
				Style: core.Style{Width: core.Pt(110), Gap: 2},
				Children: []core.Node{
					core.Text{Content: "Mode", Style: core.Style{FontSize: 10, Color: colors.Muted()}},
					ui.Select[string]{
						TestID:   "particles-collision-mode",
						Label:    "Collision Mode",
						Disabled: disabled,
						Options: []ui.Option[string]{
							{Value: "none", Label: "none"},
							{Value: "stop", Label: "stop"},
							{Value: "bounce", Label: "bounce"},
							{Value: "kill", Label: "kill"},
						},
						Value: func() string { return activeCfg().Collision.Mode.String() },
						OnChange: func(mode string) {
							col := activeCfg().Collision
							switch mode {
							case "stop":
								col.Mode = particles.CollideStop
							case "bounce":
								col.Mode = particles.CollideBounce
							case "kill":
								col.Mode = particles.CollideKill
							default:
								col.Mode = particles.CollideNone
							}
							ed.report(ed.SetParticleCollision(col))
						},
					},
				},
			},
			numField("particles-collision-bounce", "Bounce", 0, 2, 0.05,
				func() float64 { return float64(activeCfg().Collision.Bounce) },
				func(v float64) {
					col := activeCfg().Collision
					col.Bounce = float32(v)
					ed.report(ed.SetParticleCollision(col))
				},
			),
			numField("particles-collision-friction", "Friction", 0, 1, 0.05,
				func() float64 { return float64(activeCfg().Collision.Friction) },
				func(v float64) {
					col := activeCfg().Collision
					col.Friction = float32(v)
					ed.report(ed.SetParticleCollision(col))
				},
			),
			numField("particles-collision-radius", "Radius", 0, 500, 0.5,
				func() float64 { return float64(activeCfg().Collision.Radius) },
				func(v float64) {
					col := activeCfg().Collision
					col.Radius = float32(v)
					ed.report(ed.SetParticleCollision(col))
				},
			),
		},
	}

	// ---- Trails section ----
	trailChildren := []core.Node{
		ui.Checkbox{
			TestID:   "particles-trail-enabled",
			Label:    "Enable Ribbon Trails",
			Disabled: disabled,
			Checked:  func() bool { return activeCfg().Trail != nil },
			OnChange: func(on bool) { ed.report(ed.SetParticleTrailEnabled(on)) },
		},
	}
	if cfg.Trail != nil {
		trailChildren = append(trailChildren,
			numField("particles-trail-max-points", "Max Points", 0, 512, 4,
				func() float64 {
					if tr := activeCfg().Trail; tr != nil {
						return float64(tr.MaxPoints)
					}
					return 0
				},
				func(v float64) {
					if tr := activeCfg().Trail; tr != nil {
						tr.MaxPoints = int(math.Round(v))
						ed.report(ed.SetParticleTrail(tr))
					}
				},
			),
			numField("particles-trail-lifetime", "Lifetime (s)", 0, 60, 0.05,
				func() float64 {
					if tr := activeCfg().Trail; tr != nil {
						return float64(tr.Lifetime)
					}
					return 0
				},
				func(v float64) {
					if tr := activeCfg().Trail; tr != nil {
						tr.Lifetime = float32(v)
						ed.report(ed.SetParticleTrail(tr))
					}
				},
			),
			numField("particles-trail-min-distance", "Min Dist", 0, 500, 0.5,
				func() float64 {
					if tr := activeCfg().Trail; tr != nil {
						return float64(tr.MinDistance)
					}
					return 0
				},
				func(v float64) {
					if tr := activeCfg().Trail; tr != nil {
						tr.MinDistance = float32(v)
						ed.report(ed.SetParticleTrail(tr))
					}
				},
			),
			numField("particles-trail-width", "Width", 0, 500, 0.5,
				func() float64 {
					if tr := activeCfg().Trail; tr != nil {
						return float64(tr.Width)
					}
					return 0
				},
				func(v float64) {
					if tr := activeCfg().Trail; tr != nil {
						tr.Width = float32(v)
						ed.report(ed.SetParticleTrail(tr))
					}
				},
			),
			core.View{
				Style: core.Style{Width: core.Pt(118), Gap: 2},
				Children: []core.Node{
					core.Text{Content: "Color (#rrggbbaa)", Style: core.Style{FontSize: 10, Color: colors.Muted()}},
					ui.Input{
						TestID:   "particles-trail-color",
						Label:    "Trail Color",
						Disabled: disabled,
						Value: func() string {
							if tr := activeCfg().Trail; tr != nil && tr.Color != 0 {
								return formatParticleHexColor(tr.Color)
							}
							return ""
						},
						OnSubmit: func(text string) {
							tr := activeCfg().Trail
							if tr == nil {
								return
							}
							if strings.TrimSpace(text) == "" {
								tr.Color = 0
								ed.report(ed.SetParticleTrail(tr))
								return
							}
							col, err := parseParticleHexColor(text)
							if err != nil {
								ed.report(err)
								return
							}
							tr.Color = col
							ed.report(ed.SetParticleTrail(tr))
						},
					},
				},
			},
			core.View{
				Style: core.Style{Width: core.Pt(150), Gap: 2},
				Children: []core.Node{
					core.Text{Content: "Texture URI", Style: core.Style{FontSize: 10, Color: colors.Muted()}},
					ui.Input{
						TestID:   "particles-trail-texture",
						Label:    "Trail Texture URI",
						Disabled: disabled,
						Value: func() string {
							if tr := activeCfg().Trail; tr != nil {
								return tr.Texture.URI
							}
							return ""
						},
						OnChange: func(text string) {
							if tr := activeCfg().Trail; tr != nil {
								tr.Texture = core.ImageSource{URI: strings.TrimSpace(text)}
								ed.report(ed.SetParticleTrail(tr))
							}
						},
					},
				},
			},
		)
	}

	// ---- Sub-Emitters section ----
	subRows := make([]core.Node, 0, len(cfg.SubEmitters)+1)
	for i := range cfg.SubEmitters {
		idx := i
		subRows = append(subRows, ui.HStack{
			Gap: 6,
			Children: []core.Node{
				core.View{
					Style: core.Style{Width: core.Pt(104), Gap: 2},
					Children: []core.Node{
						core.Text{Content: fmt.Sprintf("Sub #%d Trigger", idx+1), Style: core.Style{FontSize: 10, Color: colors.Muted()}},
						ui.Select[string]{
							TestID:   fmt.Sprintf("particles-sub-trigger-%d", idx),
							Label:    "Sub-Emitter Trigger",
							Disabled: disabled,
							Options: []ui.Option[string]{
								{Value: "birth", Label: "birth"},
								{Value: "death", Label: "death"},
								{Value: "collision", Label: "collision"},
							},
							Value: func() string {
								c := activeCfg()
								if idx >= len(c.SubEmitters) {
									return "birth"
								}
								return c.SubEmitters[idx].Trigger.String()
							},
							OnChange: func(trig string) {
								c := activeCfg()
								if idx >= len(c.SubEmitters) {
									return
								}
								sub := c.SubEmitters[idx]
								t := particles.TriggerOnBirth
								switch trig {
								case "death":
									t = particles.TriggerOnDeath
								case "collision":
									t = particles.TriggerOnCollision
								}
								ed.report(ed.SetParticleSubEmitter(idx, t, sub.BurstCount, sub.InheritVelocity))
							},
						},
					},
				},
				numField(fmt.Sprintf("particles-sub-burst-%d", idx), "Burst Count", 0, 1000, 1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.SubEmitters) {
							return 1
						}
						return float64(c.SubEmitters[idx].BurstCount)
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.SubEmitters) {
							return
						}
						sub := c.SubEmitters[idx]
						ed.report(ed.SetParticleSubEmitter(idx, sub.Trigger, int(math.Round(v)), sub.InheritVelocity))
					},
				),
				numField(fmt.Sprintf("particles-sub-inherit-%d", idx), "Inherit Vel", -10, 10, 0.1,
					func() float64 {
						c := activeCfg()
						if idx >= len(c.SubEmitters) {
							return 0
						}
						return float64(c.SubEmitters[idx].InheritVelocity)
					},
					func(v float64) {
						c := activeCfg()
						if idx >= len(c.SubEmitters) {
							return
						}
						sub := c.SubEmitters[idx]
						ed.report(ed.SetParticleSubEmitter(idx, sub.Trigger, sub.BurstCount, float32(v)))
					},
				),
				ui.Button{
					TestID:   fmt.Sprintf("particles-sub-edit-%d", idx),
					Text:     "Edit Child",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						nextPath := append(ed.ParticleTargetPath(), idx)
						ed.report(ed.SetParticleTargetPath(nextPath))
					},
				},
				ui.Button{
					TestID:   fmt.Sprintf("particles-sub-remove-%d", idx),
					Text:     "Remove",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress:  func() { ed.report(ed.RemoveParticleSubEmitter(idx)) },
				},
			},
		})
	}
	subRows = append(subRows, ui.Button{
		TestID:   "particles-sub-add",
		Text:     "+ Add Sub-Emitter",
		Size:     ui.SizeSmall,
		Variant:  ui.ButtonSecondary,
		Disabled: disabled,
		OnPress: func() {
			ed.report(ed.AddParticleSubEmitter(particles.SubEmitterRule{
				Trigger:         particles.TriggerOnDeath,
				BurstCount:      4,
				InheritVelocity: 0.25,
				Config: particles.Config{
					Space:       particles.SpaceWorld,
					Shape:       particles.Point(),
					LifetimeMin: 200 * time.Millisecond,
					LifetimeMax: 400 * time.Millisecond,
					SpeedMin:    15,
					SpeedMax:    45,
					AngleMax:    float32(2 * math.Pi),
				},
			}))
		},
	})

	return core.View{
		Style: core.Style{Gap: 10},
		Children: []core.Node{
			secTitle("EMITTER & SHAPE"),
			generalRow,
			secTitle("SCHEDULED BURSTS"),
			core.View{Style: core.Style{Gap: 6}, Children: burstRows},
			secTitle("LIFETIME & MOTION"),
			motionRow,
			secTitle("CURVES"),
			curveSection,
			secTitle("FRAMES (ATLAS PICKER)"),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 6}, Children: frameChips},
			secTitle("COLLISION"),
			collisionRow,
			secTitle("TRAILS"),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignEnd, Gap: 8}, Children: trailChildren},
			secTitle("SUB-EMITTERS"),
			core.View{Style: core.Style{Gap: 6}, Children: subRows},
		},
	}
}

// ---- Interactive Curve Widget ----

func curveYBounds(kind ParticleCurveKind, curve particles.FloatCurve) (minY, maxY float32) {
	if kind == ParticleCurveAlpha {
		return 0, 1
	}
	minY, maxY = 0, 2
	if kind == ParticleCurveRotation {
		minY, maxY = -3.1416, 3.1416
	}
	for _, s := range curve {
		if s.Value < minY {
			minY = s.Value
		}
		if s.Value > maxY {
			maxY = s.Value
		}
	}
	if maxY-minY < 0.1 {
		maxY = minY + 1
	}
	return minY, maxY
}

func particleCurveWidgetView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	activeCfg := func() particles.Config {
		_ = rev.Get()
		if c := ed.ParticleActiveConfig(); c != nil {
			return *c
		}
		return particles.Config{}
	}
	kind := ed.ParticleActiveCurve()
	cfg := activeCfg()

	tabBtn := func(k ParticleCurveKind, testID, label string) core.Node {
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			On: func() bool {
				_ = rev.Get()
				return ed.ParticleActiveCurve() == k
			},
			OnChange: func(on bool) {
				if on {
					ed.SetParticleActiveCurve(k)
				}
			},
		}
	}
	tabs := []core.Node{
		tabBtn(ParticleCurveSize, "particles-curve-tab-size", "Size"),
		tabBtn(ParticleCurveAlpha, "particles-curve-tab-alpha", "Alpha"),
		tabBtn(ParticleCurveRotation, "particles-curve-tab-rotation", "Rotation"),
		tabBtn(ParticleCurveColor, "particles-curve-tab-color", "Color"),
	}
	if cfg.Trail != nil {
		tabs = append(tabs,
			tabBtn(ParticleCurveTrailWidth, "particles-curve-tab-trail-width", "Trail Width"),
			tabBtn(ParticleCurveTrailColor, "particles-curve-tab-trail-color", "Trail Color"),
		)
	}

	isColor := kind == ParticleCurveColor || kind == ParticleCurveTrailColor

	const (
		graphW = 280
		graphH = 96
		padX   = 10
		padY   = 10
	)

	var canvasNode core.Node
	if !isColor {
		currentFloatCurve := func() particles.FloatCurve {
			c := activeCfg()
			if ref, err := floatCurveRef(&c, kind); err == nil {
				return *ref
			}
			return nil
		}
		pointToTV := func(px, py float32, curve particles.FloatCurve) (float32, float32) {
			minY, maxY := curveYBounds(kind, curve)
			t := clamp01f32((px - padX) / (graphW - 2*padX))
			normY := clamp01f32(1 - (py-padY)/(graphH-2*padY))
			v := minY + normY*(maxY-minY)
			return float32(math.Round(float64(t)*100) / 100), float32(math.Round(float64(v)*100) / 100)
		}
		tvToPoint := func(t, v, minY, maxY float32) (float32, float32) {
			x := padX + clamp01f32(t)*(graphW-2*padX)
			y := padY + (1-(v-minY)/(maxY-minY))*(graphH-2*padY)
			return x, y
		}

		canvasNode = core.View{
			Style: core.Style{Width: core.Pt(graphW), Height: core.Pt(graphH)},
			OnMouseDown: func(ev core.MouseButtonEvent) {
				if disabled() || ev.Button != core.MouseButtonLeft {
					return
				}
				curve := currentFloatCurve()
				minY, maxY := curveYBounds(kind, curve)
				px, py := float32(ev.Position.X), float32(ev.Position.Y)
				bestIdx := -1
				bestDistSq := float32(100) // 10pt hit radius
				for i, s := range curve {
					sx, sy := tvToPoint(s.T, s.Value, minY, maxY)
					dx, dy := px-sx, py-sy
					if dSq := dx*dx + dy*dy; dSq <= bestDistSq {
						bestDistSq = dSq
						bestIdx = i
					}
				}
				if bestIdx >= 0 {
					ed.beginParticleCurveDrag(kind, bestIdx)
					return
				}
				t, v := pointToTV(px, py, curve)
				if idx, err := ed.AddParticleFloatStop(kind, t, v); err == nil && idx >= 0 {
					ed.beginParticleCurveDrag(kind, idx)
				}
			},
			OnMouseMove: func(ev core.MouseEvent) {
				if ed.particles == nil || ed.particles.curveDrag == nil {
					return
				}
				if ev.PressedButton == core.MouseButtonNone {
					ed.endParticleCurveDrag()
					return
				}
				t, v := pointToTV(float32(ev.Position.X), float32(ev.Position.Y), currentFloatCurve())
				ed.updateParticleCurveDrag(t, v)
			},
			OnMouseUp: func(ev core.MouseButtonEvent) {
				if ev.Button == core.MouseButtonLeft {
					ed.endParticleCurveDrag()
				}
			},
			Children: []core.Node{
				core.Canvas{
					TestID: "particles-curve-canvas",
					Style: core.Style{
						Width:        core.Pt(graphW),
						Height:       core.Pt(graphH),
						Background:   core.RGB(0x0f172a),
						CornerRadius: 6,
						BorderWidth:  1,
						BorderColor:  colors.Line(),
					},
					Draw: func(d *core.Drawing, size core.Size) {
						_ = rev.Get()
						curve := currentFloatCurve()
						minY, maxY := curveYBounds(kind, curve)
						sel := ed.ParticleSelectedStop()

						// Grid lines at t = 0, 0.25, 0.5, 0.75, 1 and mid Y.
						d.Save()
						d.SetStrokeColor(core.RGBA(0x94A3B8, 0.2)).SetLineWidth(1)
						d.BeginPath()
						for g := range 5 {
							gx := padX + (float32(g)/4)*(graphW-2*padX)
							d.MoveTo(gx, padY).LineTo(gx, graphH-padY)
						}
						for g := range 3 {
							gy := padY + (float32(g)/2)*(graphH-2*padY)
							d.MoveTo(padX, gy).LineTo(graphW-padX, gy)
						}
						d.Stroke()

						// Evaluated curve polyline.
						d.SetStrokeColor(core.RGB(0x38BDF8)).SetLineWidth(2)
						d.BeginPath()
						const samples = 40
						for i := 0; i <= samples; i++ {
							t := float32(i) / samples
							val := curve.At(t)
							if kind == ParticleCurveRotation && len(curve) == 0 {
								val = 0
							}
							x, y := tvToPoint(t, val, minY, maxY)
							if i == 0 {
								d.MoveTo(x, y)
							} else {
								d.LineTo(x, y)
							}
						}
						d.Stroke()

						// Stop handles.
						for i, s := range curve {
							sx, sy := tvToPoint(s.T, s.Value, minY, maxY)
							r := float32(4.5)
							fill := core.RGB(0xF8FAFC)
							if i == sel {
								r = 6
								fill = core.RGB(0xF59E0B)
							}
							d.SetFillColor(fill)
							d.BeginPath().Arc(sx, sy, r, 0, 2*math.Pi, false).Fill()
							d.SetStrokeColor(core.RGB(0x0f172a)).SetLineWidth(1.5)
							d.BeginPath().Arc(sx, sy, r, 0, 2*math.Pi, false).Stroke()
						}
						d.Restore()
					},
				},
			},
		}
	} else {
		currentColorCurve := func() particles.ColorCurve {
			c := activeCfg()
			if ref, err := colorCurveRef(&c, kind); err == nil {
				return *ref
			}
			return nil
		}
		const barH = 40
		canvasNode = core.View{
			Style: core.Style{Width: core.Pt(graphW), Height: core.Pt(barH)},
			OnMouseDown: func(ev core.MouseButtonEvent) {
				if disabled() || ev.Button != core.MouseButtonLeft {
					return
				}
				curve := currentColorCurve()
				px := float32(ev.Position.X)
				bestIdx := -1
				bestDist := float32(10)
				for i, s := range curve {
					sx := padX + clamp01f32(s.T)*(graphW-2*padX)
					if dist := float32(math.Abs(float64(px - sx))); dist <= bestDist {
						bestDist = dist
						bestIdx = i
					}
				}
				if bestIdx >= 0 {
					ed.SelectParticleStop(bestIdx)
					return
				}
				t := clamp01f32((px - padX) / (graphW - 2*padX))
				t = float32(math.Round(float64(t)*100) / 100)
				_, _ = ed.AddParticleColorStop(kind, t, curve.At(t))
			},
			Children: []core.Node{
				core.Canvas{
					TestID: "particles-color-canvas",
					Style: core.Style{
						Width:        core.Pt(graphW),
						Height:       core.Pt(barH),
						Background:   core.RGB(0x0f172a),
						CornerRadius: 6,
						BorderWidth:  1,
						BorderColor:  colors.Line(),
					},
					Draw: func(d *core.Drawing, size core.Size) {
						_ = rev.Get()
						curve := currentColorCurve()
						sel := ed.ParticleSelectedStop()
						d.Save()
						innerW := float32(graphW - 2*padX)
						const steps = 48
						segW := innerW/steps + 0.5
						for i := range steps {
							t := (float32(i) + 0.5) / steps
							d.SetFillColor(curve.At(t))
							d.FillRect(padX+float32(i)*(innerW/steps), 6, segW, barH-16)
						}
						for i, s := range curve {
							sx := padX + clamp01f32(s.T)*innerW
							stroke := core.RGB(0xF8FAFC)
							if i == sel {
								stroke = core.RGB(0xF59E0B)
							}
							d.SetFillColor(s.Color | 0xff)
							d.BeginPath().Arc(sx, barH-7, 5, 0, 2*math.Pi, false).Fill()
							d.SetStrokeColor(stroke).SetLineWidth(1.5)
							d.BeginPath().Arc(sx, barH-7, 5, 0, 2*math.Pi, false).Stroke()
						}
						d.Restore()
					},
				},
			},
		}
	}

	// ---- Selected stop numeric / color controls ----
	sel := ed.ParticleSelectedStop()
	stopControls := []core.Node{
		ui.Button{
			TestID:   "particles-curve-add",
			Text:     "+ Stop",
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonSecondary,
			Disabled: disabled,
			OnPress: func() {
				if isColor {
					c := activeCfg()
					col := core.Color(0xffffffff)
					if ref, err := colorCurveRef(&c, kind); err == nil && len(*ref) > 0 {
						col = ref.At(0.5)
					}
					_, err := ed.AddParticleColorStop(kind, 0.5, col)
					ed.report(err)
					return
				}
				c := activeCfg()
				val := float32(1)
				if ref, err := floatCurveRef(&c, kind); err == nil && len(*ref) > 0 {
					val = ref.At(0.5)
				}
				_, err := ed.AddParticleFloatStop(kind, 0.5, val)
				ed.report(err)
			},
		},
		ui.Button{
			TestID:  "particles-curve-remove",
			Text:    "- Stop",
			Size:    ui.SizeSmall,
			Variant: ui.ButtonGhost,
			Disabled: func() bool {
				return disabled() || ed.ParticleSelectedStop() < 0
			},
			OnPress: func() {
				idx := ed.ParticleSelectedStop()
				if idx < 0 {
					return
				}
				if isColor {
					ed.report(ed.RemoveParticleColorStop(kind, idx))
				} else {
					ed.report(ed.RemoveParticleFloatStop(kind, idx))
				}
			},
		},
	}

	if !isColor {
		if ref, err := floatCurveRef(&cfg, kind); err == nil && sel >= 0 && sel < len(*ref) {
			stopControls = append(stopControls,
				core.View{
					Style: core.Style{Width: core.Pt(92)},
					Children: []core.Node{ui.NumberInput{
						TestID:   "particles-curve-stop-t",
						Label:    "Stop T",
						Min:      0,
						Max:      1,
						Step:     0.05,
						Disabled: disabled,
						Value: func() float64 {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := floatCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								return float64((*r)[idx].T)
							}
							return 0
						},
						OnChange: func(v float64) {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := floatCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								_, err := ed.SetParticleFloatStop(kind, idx, float32(v), (*r)[idx].Value)
								ed.report(err)
							}
						},
					}},
				},
				core.View{
					Style: core.Style{Width: core.Pt(98)},
					Children: []core.Node{ui.NumberInput{
						TestID:   "particles-curve-stop-value",
						Label:    "Stop Value",
						Step:     0.1,
						Disabled: disabled,
						Value: func() float64 {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := floatCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								return float64((*r)[idx].Value)
							}
							return 0
						},
						OnChange: func(v float64) {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := floatCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								_, err := ed.SetParticleFloatStop(kind, idx, (*r)[idx].T, float32(v))
								ed.report(err)
							}
						},
					}},
				},
			)
		}
	} else {
		if ref, err := colorCurveRef(&cfg, kind); err == nil && sel >= 0 && sel < len(*ref) {
			stopControls = append(stopControls,
				core.View{
					Style: core.Style{Width: core.Pt(92)},
					Children: []core.Node{ui.NumberInput{
						TestID:   "particles-curve-stop-t",
						Label:    "Color Stop T",
						Min:      0,
						Max:      1,
						Step:     0.05,
						Disabled: disabled,
						Value: func() float64 {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := colorCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								return float64((*r)[idx].T)
							}
							return 0
						},
						OnChange: func(v float64) {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := colorCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								_, err := ed.SetParticleColorStop(kind, idx, float32(v), (*r)[idx].Color)
								ed.report(err)
							}
						},
					}},
				},
				core.View{
					Style: core.Style{Width: core.Pt(112)},
					Children: []core.Node{ui.Input{
						TestID:   "particles-curve-stop-color",
						Label:    "Color (#rrggbbaa)",
						Disabled: disabled,
						Value: func() string {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							if r, e := colorCurveRef(&c, kind); e == nil && idx >= 0 && idx < len(*r) {
								return formatParticleHexColor((*r)[idx].Color)
							}
							return ""
						},
						OnSubmit: func(text string) {
							c := activeCfg()
							idx := ed.ParticleSelectedStop()
							r, e := colorCurveRef(&c, kind)
							if e != nil || idx < 0 || idx >= len(*r) {
								return
							}
							col, err := parseParticleHexColor(text)
							if err != nil {
								ed.report(err)
								return
							}
							_, err = ed.SetParticleColorStop(kind, idx, (*r)[idx].T, col)
							ed.report(err)
						},
					}},
				},
			)
		}
	}

	return core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 6}, Children: tabs},
			ui.HStack{
				Gap: 10,
				Children: []core.Node{
					canvasNode,
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							ui.HStack{Gap: 6, Children: stopControls},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									c := activeCfg()
									if isColor {
										if r, e := colorCurveRef(&c, kind); e == nil {
											return strconv.Itoa(len(*r)) + " color stops (click bar to select/add)"
										}
									} else {
										if r, e := floatCurveRef(&c, kind); e == nil {
											return strconv.Itoa(len(*r)) + " stops (drag handles or click graph to add)"
										}
									}
									return ""
								},
								Style: core.Style{FontSize: 10, Color: colors.Muted()},
							},
						},
					},
				},
			},
		},
	}
}
