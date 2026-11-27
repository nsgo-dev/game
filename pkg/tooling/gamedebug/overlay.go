//go:build !release

package gamedebug

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Enabled reports whether debug overlays are compiled into this build. It is true in default builds and false with -tags release.
func Enabled() bool { return true }

// Overlay coordinates the development debug HUD: a key-toggled panel with a frame-time graph and scene inspector, plus world-space collision and sprite-bounds wireframes.
type Overlay struct {
	opts   Options
	stats  FrameStats
	shapes ShapeOverlay

	visible      bool
	frameTick    uint64
	scenesTick   uint64
	scenes       []SceneSnapshot
	lastScenes   []SceneSnapshot
	aiTick       uint64
	aiSnap       AISnapshot
	hasAI        bool
	lastAI       AISnapshot
	netTick      uint64
	netSnap      NetSnapshot
	hasNet       bool
	lastNet      NetSnapshot
	drawStats    DrawStats
	hasDrawStats bool
	pendingCause string

	attached  *core.Scope
	visSig    *core.Signal[bool]
	frameSig  *core.Signal[uint64]
	scenesSig *core.Signal[uint64]
	aiSig     *core.Signal[uint64]
	netSig    *core.Signal[uint64]
}

// New returns a configured debug overlay.
func New(opts Options) *Overlay {
	return &Overlay{
		opts:    opts,
		shapes:  ShapeOverlay{LineWidth: opts.LineWidth},
		visible: opts.Visible,
	}
}

// Attach wires the overlay's reactive signals and toggle key onto cx. Calling Attach more than once on the same scope is a no-op; View calls Attach automatically.
func (o *Overlay) Attach(cx *core.Scope) {
	if o == nil || cx == nil || o.attached == cx {
		return
	}
	o.attached = cx
	o.visSig = core.NewSignal(cx, o.visible)
	o.frameSig = core.NewSignal(cx, o.frameTick)
	o.scenesSig = core.NewSignal(cx, o.scenesTick)
	o.aiSig = core.NewSignal(cx, o.aiTick)
	o.netSig = core.NewSignal(cx, o.netTick)

	key := strings.ToLower(strings.TrimSpace(o.opts.ToggleKey))
	if key == "" {
		key = "f3"
	}
	cx.OnKey(key, o.Toggle)
	cx.Cleanup(func() {
		if o.attached == cx {
			o.attached = nil
			o.visSig = nil
			o.frameSig = nil
			o.scenesSig = nil
			o.aiSig = nil
			o.netSig = nil
		}
	})
}

// Visible reports whether the overlay is currently shown. Read inside a reactive NSGo view or drawing, it subscribes to visibility toggles.
func (o *Overlay) Visible() bool {
	if o == nil {
		return false
	}
	if o.visSig != nil {
		return o.visSig.Get()
	}
	return o.visible
}

// SetVisible shows or hides the overlay and its world-space wireframes.
func (o *Overlay) SetVisible(v bool) {
	if o == nil || o.visible == v {
		return
	}
	o.visible = v
	if o.visSig != nil {
		o.visSig.Set(v)
	}
	if v && o.opts.Scenes != nil && o.scenesSig != nil {
		o.lastScenes = cloneScenes(o.opts.Scenes())
	}
	if v && o.opts.AI != nil && o.aiSig != nil {
		o.lastAI = cloneAISnapshot(o.opts.AI())
	}
	if v && o.opts.Net != nil && o.netSig != nil {
		copyNetSnapshot(&o.lastNet, o.opts.Net())
	}
}

// Toggle flips the overlay's visibility.
func (o *Overlay) Toggle() {
	if o == nil {
		return
	}
	o.SetVisible(!o.visible)
}

// NoteFrameCause attaches a short explanation (for example "first use of torch_glow compiled its shader") to the next frame recorded by RecordFrame. Calling it with an empty string is a no-op; calling it again before RecordFrame replaces the pending cause.
func (o *Overlay) NoteFrameCause(cause string) {
	if o == nil || cause == "" {
		return
	}
	o.pendingCause = cause
}

// RecordFrame records one frame's total, update and draw durations into the ring buffer, samples the attached GCMonitor (if any), and refreshes the visible overlay graph.
func (o *Overlay) RecordFrame(total, update, draw time.Duration) {
	if o == nil {
		return
	}
	var gcHitch bool
	if o.opts.GC != nil {
		gcHitch = o.opts.GC.SampleFrame(total).Hitched
	}
	cause := o.pendingCause
	o.pendingCause = ""
	o.stats.recordWithCause(total, update, draw, gcHitch, cause)
	if !o.visible {
		return
	}
	o.frameTick++
	if o.frameSig != nil {
		o.frameSig.Set(o.frameTick)
	}
	if o.opts.Scenes != nil && o.scenesSig != nil {
		snap := o.opts.Scenes()
		if !scenesEqual(o.lastScenes, snap) {
			o.lastScenes = cloneScenes(snap)
			o.scenesTick++
			o.scenesSig.Set(o.scenesTick)
		}
	}
	if o.opts.AI != nil && o.aiSig != nil {
		snap := o.opts.AI()
		if !aiSnapshotsEqual(o.lastAI, snap) {
			o.lastAI = cloneAISnapshot(snap)
			o.aiTick++
			o.aiSig.Set(o.aiTick)
		}
	}
	if o.opts.Net != nil {
		snap := o.opts.Net()
		if !netSnapshotsEqual(o.lastNet, snap) {
			copyNetSnapshot(&o.lastNet, snap)
			o.netTick++
			if o.netSig != nil {
				o.netSig.Set(o.netTick)
			}
		}
	}
}

// WrapLoop wraps l.Update and l.Draw so every frame automatically times Update and Draw and calls RecordFrame.
func (o *Overlay) WrapLoop(l *game.Loop) {
	if o == nil || l == nil {
		return
	}
	origUpdate := l.Update
	origDraw := l.Draw
	nowFn := o.opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	var updateCost time.Duration
	var lastEnd time.Time

	l.Update = func(dt time.Duration) {
		start := nowFn()
		if origUpdate != nil {
			origUpdate(dt)
		}
		if elapsed := nowFn().Sub(start); elapsed > 0 {
			updateCost += elapsed
		}
	}

	l.Draw = func(alpha float64) {
		start := nowFn()
		if origDraw != nil {
			origDraw(alpha)
		}
		end := nowFn()
		drawCost := max(time.Duration(0), end.Sub(start))
		total := updateCost + drawCost
		if !lastEnd.IsZero() {
			if delta := end.Sub(lastEnd); delta > total {
				total = delta
			}
		}
		lastEnd = end
		u := updateCost
		updateCost = 0
		o.RecordFrame(total, u, drawCost)
	}
}

// Stats returns the overlay's frame-time ring buffer.
func (o *Overlay) Stats() *FrameStats {
	if o == nil {
		return nil
	}
	return &o.stats
}

// Console returns the Console attached to the overlay via Options.Console, or nil.
func (o *Overlay) Console() *Console {
	if o == nil {
		return nil
	}
	return o.opts.Console
}

// Profiler returns the Profiler attached to the overlay via Options.Profiler, or nil.
func (o *Overlay) Profiler() *Profiler {
	if o == nil {
		return nil
	}
	return o.opts.Profiler
}

// Memory returns the MemoryTracker attached to the overlay via Options.Memory, or nil.
func (o *Overlay) Memory() *MemoryTracker {
	if o == nil {
		return nil
	}
	return o.opts.Memory
}

// GC returns the GCMonitor attached to the overlay via Options.GC, or nil.
func (o *Overlay) GC() *GCMonitor {
	if o == nil {
		return nil
	}
	return o.opts.GC
}

// Sound returns the sound.Mixer attached to the overlay via Options.Sound, or nil.
func (o *Overlay) Sound() *sound.Mixer {
	if o == nil {
		return nil
	}
	return o.opts.Sound
}

// SetDrawStats updates the overlay's pushed per-frame draw statistics and refreshes the visible overlay. It is used when Options.DrawStats is nil.
func (o *Overlay) SetDrawStats(s DrawStats) {
	if o == nil {
		return
	}
	o.drawStats = s
	o.hasDrawStats = true
	if o.visible {
		o.frameTick++
		if o.frameSig != nil {
			o.frameSig.Set(o.frameTick)
		}
	}
}

// DrawStats returns the current per-frame draw statistics from Options.DrawStats if configured, or the most recent SetDrawStats value.
func (o *Overlay) DrawStats() DrawStats {
	if o == nil {
		return DrawStats{}
	}
	if o.opts.DrawStats != nil {
		return o.opts.DrawStats()
	}
	return o.drawStats
}

// HasDrawStats reports whether draw statistics have been configured via Options.DrawStats or pushed via SetDrawStats.
func (o *Overlay) HasDrawStats() bool {
	if o == nil {
		return false
	}
	return o.opts.DrawStats != nil || o.hasDrawStats
}

// ResetStats clears the overlay's recorded frame timings and pushed draw statistics and refreshes the visible overlay.
func (o *Overlay) ResetStats() {
	if o == nil {
		return
	}
	o.stats.Reset()
	o.drawStats = DrawStats{}
	o.hasDrawStats = false
	o.pendingCause = ""
	if o.visible {
		o.frameTick++
		if o.frameSig != nil {
			o.frameSig.Set(o.frameTick)
		}
	}
}

// SetScenes updates the scene inspector's snapshot list and immediately refreshes the inspector view. It is used when Options.Scenes is nil.
func (o *Overlay) SetScenes(scenes []SceneSnapshot) {
	if o == nil {
		return
	}
	o.scenes = cloneScenes(scenes)
	o.scenesTick++
	if o.scenesSig != nil {
		o.scenesSig.Set(o.scenesTick)
	}
}

// RefreshScenes tells the scene inspector to re-read Options.Scenes immediately.
func (o *Overlay) RefreshScenes() {
	if o == nil {
		return
	}
	if o.opts.Scenes != nil {
		o.lastScenes = cloneScenes(o.opts.Scenes())
	}
	o.scenesTick++
	if o.scenesSig != nil {
		o.scenesSig.Set(o.scenesTick)
	}
}

// DrawShapes calls fn with the overlay's ShapeOverlay only while the overlay is visible. When called inside a core.Canvas.Draw callback, toggling the overlay automatically redraws the canvas.
func (o *Overlay) DrawShapes(d *core.Drawing, fn func(*ShapeOverlay)) {
	if o == nil || d == nil || fn == nil || !o.Visible() {
		return
	}
	fn(&o.shapes)
}

// DrawBox draws a wireframe box under d's active transform while the overlay is visible.
func (o *Overlay) DrawBox(d *core.Drawing, b core.Bounds, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawBox(d, b, col)
}

// DrawCircle draws a wireframe circle under d's active transform while the overlay is visible.
func (o *Overlay) DrawCircle(d *core.Drawing, x, y, r float32, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawCircle(d, x, y, r, col)
}

// DrawSpriteBounds draws the untrimmed bounding wireframe of s under d's active transform while the overlay is visible.
func (o *Overlay) DrawSpriteBounds(d *core.Drawing, s *sprite.Sprite, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawSpriteBounds(d, s, col)
}

// DrawBatchBounds draws the untrimmed bounding wireframes of all sprites in batch under d's active transform while the overlay is visible.
func (o *Overlay) DrawBatchBounds(d *core.Drawing, batch *sprite.Batch, col core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawBatchBounds(d, batch, col)
}

var (
	overlayPanelStyle = core.Style{
		Position:     core.Absolute,
		Top:          core.Pt(8),
		Left:         core.Pt(8),
		MinWidth:     core.Pt(256),
		MaxWidth:     core.Pt(320),
		ZIndex:       1000,
		Background:   core.RGBA(0x0B0F17, 0.88),
		CornerRadius: 6,
		Padding:      core.All(8),
		BorderWidth:  1,
		BorderColor:  core.RGBA(0x334155, 0.85),
		Gap:          6,
	}
	readoutTextStyle = core.Style{
		FontFamily:  core.FontMonospace,
		FontSize:    11,
		FontVariant: core.FontTabularNums,
		Color:       core.RGB(0xE2E8F0),
	}
	sectionHeaderStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   10,
		FontWeight: core.FontWeightSemibold,
		Color:      core.RGB(0x94A3B8),
	}
	sceneNameStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		FontWeight: core.FontWeightSemibold,
		Color:      core.RGB(0xF8FAFC),
	}
	layerLabelStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0xCBD5E1),
	}
	layerCountStyle = core.Style{
		FontFamily:  core.FontMonospace,
		FontSize:    11,
		FontVariant: core.FontTabularNums,
		Color:       core.RGB(0x38BDF8),
	}
	mutedTextStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0x64748B),
	}
	sectionDividerStyle = core.Style{
		Gap:         4,
		Padding:     core.Edges{Top: 6},
		Borders:     core.Edges{Top: 1},
		BorderColor: core.RGBA(0x334155, 0.7),
	}
	alertTextStyle = core.Style{
		FontFamily:  core.FontMonospace,
		FontSize:    11,
		FontVariant: core.FontTabularNums,
		Color:       core.RGB(0xF87171),
	}
	gcHitchTextStyle = core.Style{
		FontFamily:  core.FontMonospace,
		FontSize:    11,
		FontVariant: core.FontTabularNums,
		Color:       core.RGB(0xE879F9),
	}
	consoleOverlayAnchorStyle = core.Style{
		Position: core.Absolute,
		Bottom:   core.Pt(8),
		Left:     core.Pt(8),
		ZIndex:   1001,
	}
)

// View attaches the overlay to cx and returns a non-intrusive native NSGo overlay node positioned over the game view.
func (o *Overlay) View(cx *core.Scope) core.Node {
	if o == nil {
		return nil
	}
	o.Attach(cx)
	return core.Show{
		When: o.Visible,
		Then: func(cx *core.Scope) core.Node {
			children := []core.Node{
				core.Text{
					TestID:      "debug-frame-stats",
					Style:       readoutTextStyle,
					ContentFunc: o.frameReadout,
				},
				core.Canvas{
					TestID: "debug-frame-graph",
					Style:  core.Style{Width: core.Pt(240), Height: core.Pt(64)},
					Draw: func(d *core.Drawing, size core.Size) {
						if o.frameSig != nil {
							_ = o.frameSig.Get()
						}
						o.stats.DrawGraph(d, size, o.targetFPS())
					},
				},
			}
			if o.opts.GC != nil {
				children = append(children, o.gcSummaryView())
			}
			children = append(children, o.drawStatsView())
			if o.opts.Profiler != nil {
				children = append(children, o.profilerScopesView())
			}
			if o.opts.Memory != nil {
				children = append(children, o.memorySummaryView())
			}
			if o.opts.Sound != nil {
				children = append(children, o.soundSummaryView())
			}
			children = append(children, o.aiInspectorView())
			children = append(children, o.netInspectorView())
			children = append(children, o.sceneInspectorView())

			hud := core.View{
				TestID:        "debug-overlay",
				PointerEvents: core.PointerEventsNone,
				Style:         overlayPanelStyle,
				Children:      children,
			}
			if o.opts.Console == nil {
				return hud
			}
			return core.Group([]core.Node{
				hud,
				core.View{
					Style:    consoleOverlayAnchorStyle,
					Children: []core.Node{ConsoleView(cx, o.opts.Console)},
				},
			})
		},
	}
}

func (o *Overlay) targetFPS() float64 {
	if o.opts.TargetFPS > 0 {
		return o.opts.TargetFPS
	}
	return 60
}

func (o *Overlay) frameReadout() string {
	if o.frameSig != nil {
		_ = o.frameSig.Get()
	}
	if o.stats.Len() == 0 {
		return "0.0 fps · 0.00 ms"
	}
	avg := o.stats.Average()
	fps := o.stats.FPS()
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return fmt.Sprintf("%.1f fps · %.2f ms (upd %.2f / drw %.2f)", fps, ms(avg.Total), ms(avg.Update), ms(avg.Draw))
}

func (o *Overlay) currentScenes() []SceneSnapshot {
	if o.scenesSig != nil {
		_ = o.scenesSig.Get()
	}
	if o.opts.Scenes != nil {
		return o.opts.Scenes()
	}
	return o.scenes
}

func (o *Overlay) gcSummaryView() core.Node {
	return core.View{
		TestID: "debug-gc",
		Style:  sectionDividerStyle,
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if o.frameSig != nil {
					_ = o.frameSig.Get()
				}
				gc := o.opts.GC
				ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
				hitches := gc.RecentHitches()
				header := fmt.Sprintf("GC (%d cycles · %s heap)", gc.TotalCycles(), formatBytes(int64(gc.HeapObjectsBytes())))
				line := fmt.Sprintf("last %.2fms · max %.2fms · hitches %d", ms(gc.LastPause()), ms(gc.MaxPause()), hitches)
				allocLine := fmt.Sprintf("allocs %.1f/f (%s/f)", gc.AllocsPerFrame(), formatBytes(int64(gc.AllocBytesPerFrame())))
				lineStyle := layerLabelStyle
				if hitches > 0 {
					lineStyle = gcHitchTextStyle
				}
				return core.Group([]core.Node{
					core.Text{Content: header, Style: sectionHeaderStyle},
					core.Text{Content: line, Style: lineStyle},
					core.Text{Content: allocLine, Style: layerLabelStyle},
				})
			}),
		},
	}
}

func (o *Overlay) drawStatsView() core.Node {
	return core.Show{
		When: func() bool {
			if o.frameSig != nil {
				_ = o.frameSig.Get()
			}
			return o.HasDrawStats()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "debug-draw-stats",
				Style:  sectionDividerStyle,
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if o.frameSig != nil {
							_ = o.frameSig.Get()
						}
						ds := o.DrawStats()
						line := fmt.Sprintf("Draw: %d calls · %d sprites · %d batches", ds.DrawCalls, ds.Sprites, ds.Batches)
						if ds.Triangles > 0 || ds.Vertices > 0 {
							line += fmt.Sprintf(" · %d tris · %d verts", ds.Triangles, ds.Vertices)
						}
						return core.Text{Content: line, Style: layerLabelStyle}
					}),
				},
			}
		},
	}
}

func (o *Overlay) profilerScopesView() core.Node {
	return core.View{
		TestID: "debug-profiler",
		Style:  sectionDividerStyle,
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if o.frameSig != nil {
					_ = o.frameSig.Get()
				}
				scopes := o.opts.Profiler.TopScopes(6)
				if len(scopes) == 0 {
					return core.Text{
						Content: "Scopes: none",
						Style:   mutedTextStyle,
					}
				}
				ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
				nodes := make([]core.Node, 0, len(scopes)+1)
				nodes = append(nodes, core.Text{
					Content: fmt.Sprintf("Scopes (%d)", len(scopes)),
					Style:   sectionHeaderStyle,
				})
				for _, sc := range scopes {
					val := fmt.Sprintf("%.2fms (avg %.2f, x%d)", ms(sc.Total), ms(sc.Avg), sc.Calls)
					nodes = append(nodes, core.View{
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{Content: sc.Name, Style: layerLabelStyle},
							core.Text{Content: val, Style: layerCountStyle},
						},
					})
				}
				return core.Group(nodes)
			}),
		},
	}
}

func (o *Overlay) memorySummaryView() core.Node {
	return core.View{
		TestID: "debug-memory",
		Style:  sectionDividerStyle,
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if o.frameSig != nil {
					_ = o.frameSig.Get()
				}
				mem := o.opts.Memory
				total := mem.TotalBytes()
				budget := mem.TotalBudget()
				header := "Memory: " + formatBytes(total)
				headerStyle := sectionHeaderStyle
				if budget > 0 {
					header += " / " + formatBytes(budget)
					if total > budget {
						headerStyle = alertTextStyle
					}
				}
				groups := mem.Groups()
				if len(groups) == 0 {
					return core.Text{
						Content: header,
						Style:   headerStyle,
					}
				}
				nodes := make([]core.Node, 0, len(groups)+1)
				nodes = append(nodes, core.Text{
					Content: header,
					Style:   headerStyle,
				})
				for _, g := range groups {
					val := formatBytes(g.TotalBytes)
					valStyle := layerCountStyle
					if g.Budget > 0 {
						val += " / " + formatBytes(g.Budget)
						if g.OverBudget {
							valStyle = alertTextStyle
						}
					}
					nodes = append(nodes, core.View{
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{Content: g.Group, Style: layerLabelStyle},
							core.Text{Content: val, Style: valStyle},
						},
					})
				}
				return core.Group(nodes)
			}),
		},
	}
}

func (o *Overlay) soundSummaryView() core.Node {
	return core.View{
		TestID: "debug-sound",
		Style:  sectionDividerStyle,
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if o.frameSig != nil {
					_ = o.frameSig.Get()
				}
				snap := o.opts.Sound.DebugSnapshot()
				header := fmt.Sprintf("Sound: %d / %d voices", snap.ActiveVoices, snap.MaxVoices)
				if snap.FadingVoices > 0 {
					header += fmt.Sprintf(" (+%d fading)", snap.FadingVoices)
				}
				headerStyle := sectionHeaderStyle
				if snap.MaxVoices > 0 && snap.ActiveVoices >= snap.MaxVoices {
					headerStyle = alertTextStyle
				}
				nodes := []core.Node{
					core.Text{Content: header, Style: headerStyle},
				}
				for _, g := range snap.Groups {
					var val string
					if g.Max > 0 {
						val = fmt.Sprintf("%d / %d", g.Active, g.Max)
					} else {
						val = strconv.Itoa(g.Active)
					}
					valStyle := layerCountStyle
					if g.Max > 0 && g.Active >= g.Max {
						valStyle = alertTextStyle
					}
					nodes = append(nodes, core.View{
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{Content: g.Name, Style: layerLabelStyle},
							core.Text{Content: val, Style: valStyle},
						},
					})
				}
				for _, b := range snap.Buses {
					if b.DuckGain < 0.999 {
						nodes = append(nodes, core.View{
							Style: core.Style{
								Direction:      core.Row,
								JustifyContent: core.JustifySpaceBetween,
								Gap:            8,
							},
							Children: []core.Node{
								core.Text{Content: "duck:" + b.Name, Style: layerLabelStyle},
								core.Text{Content: fmt.Sprintf("%.0f%%", b.DuckGain*100), Style: layerCountStyle},
							},
						})
					}
				}
				return core.Group(nodes)
			}),
		},
	}
}

func (o *Overlay) sceneInspectorView() core.Node {
	return core.View{
		TestID: "debug-scene-inspector",
		Style:  sectionDividerStyle,
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				scenes := o.currentScenes()
				if len(scenes) == 0 {
					return core.Text{
						Content: "Scenes: none",
						Style:   mutedTextStyle,
					}
				}
				nodes := make([]core.Node, 0, len(scenes)+1)
				nodes = append(nodes, core.Text{
					Content: fmt.Sprintf("Scenes (%d)", len(scenes)),
					Style:   sectionHeaderStyle,
				})
				for _, sc := range scenes {
					rows := make([]core.Node, 0, len(sc.Layers)+1)
					rows = append(rows, core.Text{
						Content: sc.Name,
						Style:   sceneNameStyle,
					})
					for _, ly := range sc.Layers {
						label := ly.Name
						if ly.Space != "" {
							label = ly.Name + " (" + ly.Space + ")"
						}
						rows = append(rows, core.View{
							Style: core.Style{
								Direction:      core.Row,
								JustifyContent: core.JustifySpaceBetween,
								Gap:            12,
								Padding:        core.Edges{Left: 8},
							},
							Children: []core.Node{
								core.Text{Content: label, Style: layerLabelStyle},
								core.Text{Content: strconv.Itoa(ly.Objects), Style: layerCountStyle},
							},
						})
					}
					nodes = append(nodes, core.View{
						Style:    core.Style{Gap: 2},
						Children: rows,
					})
				}
				return core.Group(nodes)
			}),
		},
	}
}

func cloneScenes(src []SceneSnapshot) []SceneSnapshot {
	if len(src) == 0 {
		return nil
	}
	dst := make([]SceneSnapshot, len(src))
	for i, sc := range src {
		dst[i].Name = sc.Name
		if len(sc.Layers) > 0 {
			dst[i].Layers = make([]LayerSnapshot, len(sc.Layers))
			copy(dst[i].Layers, sc.Layers)
		}
	}
	return dst
}

func scenesEqual(a, b []SceneSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || len(a[i].Layers) != len(b[i].Layers) {
			return false
		}
		for j := range a[i].Layers {
			if a[i].Layers[j] != b[i].Layers[j] {
				return false
			}
		}
	}
	return true
}
