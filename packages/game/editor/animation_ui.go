package editor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// AnimationPanelView renders the animation editor below the viewport while a clip is open: the clip's name, length, loop, playback and Save and Close; a scrubber for the playhead; a property picker that keys the chosen property at the playhead; one lane per track with its keys, which select a key to edit its time, value and interpolation; and a lane of the clip's events.
func AnimationPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.clip != nil
		},
		Then: func(cx *core.Scope) core.Node { return animationPanel(cx, ed, rev) },
	}
}

// clipLengthSeconds returns the open clip's length in seconds, or 1 with none open.
func (e *Editor) clipLengthSeconds() float64 {
	if e.clip == nil {
		return 1
	}
	return animclip.KeyValue(e.clip.Length)
}

// report shows err in the status bar and appends it to the Output panel history, so an edit the panel's controls make and the editor refuses is never silently dropped or overwritten by the next error.
func (e *Editor) report(err error) {
	if err == nil {
		return
	}
	e.reportStatusError("Error: " + err.Error())
}

func durationOfSeconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

func animationPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	property := core.NewSignal(cx, "")
	frameChoice := core.NewSignal(cx, "")
	asepriteTag := core.NewSignal(cx, "")
	runFPS := core.NewSignal(cx, 12.0)
	eventName := core.NewSignal(cx, "")
	selEvent := core.NewSignal(cx, -1)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	keySelected := func() bool {
		_ = rev.Get()
		_, _, ok := ed.SelectedClipKey()
		return ok
	}
	// structure changes when tracks or keys are added or removed, frame thumbnails change, or events change, so the lanes remount then and not on every numeric key value edit.
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.clip == nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "len=%v|", ed.clip.Length)
		for _, tr := range ed.clip.Tracks {
			fmt.Fprintf(&b, "%s/%s:k%d:f%d|", tr.Target, tr.Property, len(tr.Keys), len(tr.Frames))
			for _, fk := range tr.Frames {
				fmt.Fprintf(&b, "%v=%q|", fk.Time, fk.Frame)
			}
		}
		// The event markers' labels and tooltips are fixed when they mount, so each event's time and name are part of the key.
		for _, ev := range ed.clip.Events {
			fmt.Fprintf(&b, "e%v:%q|", ev.Time, ev.Name)
		}
		return b.String()
	})
	label := func(text string) core.Node {
		return core.Text{Content: text, Style: core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()}}
	}

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{Content: "Animation", Style: core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()}},
			core.Text{
				TestID: "anim-target",
				ContentFunc: func() string {
					_ = rev.Get()
					return "on " + ed.ClipTarget()
				},
				Style: core.Style{FontSize: 12, Color: colors.Muted()},
			},
			core.View{Style: core.Style{Width: core.Pt(140)}, Children: []core.Node{ui.Input{
				TestID:   "anim-name",
				Label:    "Clip Name",
				Disabled: disabled,
				Value: func() string {
					_ = rev.Get()
					if ed.clip == nil {
						return ""
					}
					return ed.clip.Name
				},
				OnChange: func(text string) { ed.report(ed.SetClipName(text)) },
			}}},
			label("LENGTH"),
			core.View{Style: core.Style{Width: core.Pt(90)}, Children: []core.Node{ui.NumberInput{
				TestID:   "anim-length",
				Label:    "Clip Length (seconds)",
				Min:      0.01,
				Step:     0.1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					return ed.clipLengthSeconds()
				},
				OnChange: func(v float64) { ed.report(ed.SetClipLength(durationOfSeconds(v))) },
			}}},
			ui.ToggleButton{
				TestID:   "anim-loop",
				Text:     "Loop",
				Disabled: disabled,
				On: func() bool {
					_ = rev.Get()
					return ed.clip != nil && ed.clip.Loop
				},
				OnChange: func(on bool) { ed.report(ed.SetClipLoop(on)) },
			},
			ui.Button{
				TestID:   "anim-play",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonPrimary,
				Disabled: disabled,
				TextFunc: func() string {
					_ = rev.Get()
					if ed.ClipPlaying() {
						return "Pause"
					}
					return "Play"
				},
				OnPress: func() {
					if ed.ClipPlaying() {
						ed.PauseClip()
						return
					}
					ed.report(ed.PlayClip())
				},
			},
			core.Text{
				TestID: "anim-time",
				ContentFunc: func() string {
					_ = rev.Get()
					return fmt.Sprintf("%.2f / %.2f s", ed.ClipTime().Seconds(), ed.clipLengthSeconds())
				},
				Style: core.Style{FontSize: 12, Color: colors.Text()},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:  "anim-save",
				Text:    "Save Clip",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonOutline,
				OnPress: func() { ed.report(ed.saveClipFromPanel()) },
			},
			ui.Button{
				TestID:   "anim-close",
				Text:     "Close",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress:  func() { ed.report(ed.CloseClip()) },
			},
		},
	}

	scrubber := ui.Slider{
		TestID:  "anim-scrub",
		Label:   "Playhead",
		MinFunc: func() float64 { return 0 },
		MaxFunc: func() float64 {
			_ = rev.Get()
			return ed.clipLengthSeconds()
		},
		StepFunc: func() float64 { return 1 / 60.0 },
		Value: func() float64 {
			_ = rev.Get()
			return ed.ClipTime().Seconds()
		},
		ValueText: func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) + " seconds" },
		OnChange:  func(v float64) { ed.SeekClip(durationOfSeconds(v)) },
	}

	keyBar := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(160)}, Children: []core.Node{ui.Select[string]{
				TestID:      "anim-property",
				Label:       "Property to key",
				Placeholder: "Property",
				Searchable:  true,
				Disabled:    disabled,
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					props := animclip.Properties(ed.components)
					opts := make([]ui.Option[string], len(props))
					for i, p := range props {
						opts[i] = ui.Option[string]{Value: p.Name, Label: p.Name}
					}
					return opts
				},
				Value:    property.Get,
				OnChange: property.Set,
			}}},
			core.Show{
				When: func() bool { return property.Get() == animclip.PropertySpriteFrame },
				Then: func(cx *core.Scope) core.Node {
					return core.View{Style: core.Style{Width: core.Pt(150)}, Children: []core.Node{ui.Select[string]{
						TestID:      "anim-frame-picker",
						Label:       "Sprite frame to key",
						Placeholder: "Current frame",
						Searchable:  true,
						Disabled:    disabled,
						OptionsFunc: func() []ui.Option[string] {
							_ = rev.Get()
							entries := ed.ClipTargetFrames(ed.keyTarget())
							opts := make([]ui.Option[string], len(entries))
							for i, ent := range entries {
								opts[i] = ui.Option[string]{Value: ent.Frame, Label: ent.Frame}
							}
							return opts
						},
						Value:    frameChoice.Get,
						OnChange: frameChoice.Set,
						OnClear:  func() { frameChoice.Set("") },
					}}}
				},
			},
			ui.Button{
				TestID:  "anim-key",
				Text:    "Key",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || property.Get() == ""
				},
				OnPress: func() {
					if property.Get() == animclip.PropertySpriteFrame && frameChoice.Get() != "" {
						_, _, err := ed.KeySpriteFrame(ed.keyTarget(), frameChoice.Get(), ed.ClipTime())
						ed.report(err)
						return
					}
					_, _, err := ed.KeyProperty(ed.keyTarget(), property.Get(), ed.ClipTime())
					ed.report(err)
				},
			},
			core.Show{
				When: keySelected,
				Then: func(cx *core.Scope) core.Node { return keyEditor(cx, ed, rev, disabled) },
			},
		},
	}

	flipbookBar := core.Show{
		When: func() bool {
			_ = rev.Get()
			return len(ed.ClipTargetFrames("")) > 0
		},
		Then: func(cx *core.Scope) core.Node {
			return ui.HStack{
				Gap: 8,
				Children: []core.Node{
					label("FLIPBOOK"),
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return len(ed.ClipTargetAsepriteTags("")) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.HStack{
								Gap: 6,
								Children: []core.Node{
									core.View{Style: core.Style{Width: core.Pt(140)}, Children: []core.Node{ui.Select[string]{
										TestID:      "anim-aseprite-tag",
										Label:       "Aseprite tag",
										Placeholder: "Aseprite tag",
										Searchable:  true,
										Disabled:    disabled,
										OptionsFunc: func() []ui.Option[string] {
											_ = rev.Get()
											tags := ed.ClipTargetAsepriteTags("")
											opts := make([]ui.Option[string], len(tags))
											for i, tag := range tags {
												opts[i] = ui.Option[string]{Value: tag, Label: tag}
											}
											return opts
										},
										Value:    asepriteTag.Get,
										OnChange: asepriteTag.Set,
									}}},
									ui.Button{
										TestID:  "anim-from-tag",
										Text:    "From Tag",
										Size:    ui.SizeSmall,
										Variant: ui.ButtonOutline,
										Disabled: func() bool {
											_ = rev.Get()
											return ed.Playing() || asepriteTag.Get() == ""
										},
										OnPress: func() {
											ed.report(ed.NewClipFromAsepriteTag(ed.ClipTarget(), asepriteTag.Get()))
										},
									},
								},
							}
						},
					},
					label("FPS"),
					core.View{Style: core.Style{Width: core.Pt(70)}, Children: []core.Node{ui.NumberInput{
						TestID:   "anim-run-fps",
						Label:    "Frame rate (FPS)",
						Min:      1,
						Step:     1,
						Disabled: disabled,
						Value:    runFPS.Get,
						OnChange: runFPS.Set,
					}}},
					ui.Button{
						TestID:   "anim-from-frames",
						Text:     "From Frames",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonOutline,
						Disabled: disabled,
						OnPress: func() {
							entries := ed.ClipTargetFrames("")
							names := make([]string, len(entries))
							for i, ent := range entries {
								names[i] = ent.Frame
							}
							name := "Flipbook"
							if ed.clip != nil && ed.clip.Name != "" {
								name = ed.clip.Name
							}
							ed.report(ed.NewClipFromFrames(ed.ClipTarget(), name, names, runFPS.Get(), true))
						},
					},
				},
			}
		},
	}

	lanes := ui.ScrollView{
		Style:        core.Style{FlexGrow: 1},
		ContentStyle: core.Style{Gap: 4},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if structure.Get() == "" {
				return nil
			}
			var rows []core.Node
			for ti, tr := range ed.clip.Tracks {
				rows = append(rows, trackLane(cx, ed, rev, ti, tr, disabled))
			}
			rows = append(rows, eventLane(cx, ed, rev, selEvent, eventName, disabled))
			return core.View{Style: core.Style{Gap: 4}, Children: rows}
		})},
	}

	return core.View{
		TestID: "anim-panel",
		Style: core.Style{
			Height:      core.Pt(260),
			Padding:     core.Symmetric(6, 12),
			Gap:         6,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			header,
			scrubber,
			keyBar,
			flipbookBar,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.clipErr != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "anim-error",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.clipErr == nil {
								return ""
							}
							return "Preview: " + ed.clipErr.Error()
						},
						Style: core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
			lanes,
		},
	}
}

// keyTarget is the track target the Key button writes: the selected object when it is not the one the clip plays on, and empty (the clip's target) otherwise.
func (e *Editor) keyTarget() string {
	if e.SelectedID == "" || e.SelectedID == e.clipMeta.target {
		return ""
	}
	return e.SelectedID
}

func (e *Editor) saveClipFromPanel() error {
	if e.OnSaveClip != nil {
		data, err := e.SaveClipYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveClip(data); err != nil {
			return err
		}
		e.Status = "Saved clip"
		e.notify()
		return nil
	}
	if e.clipMeta.path == "" {
		return fmt.Errorf("editor: no clip path; open the clip from a file or set OnSaveClip")
	}
	return e.SaveClipFile(e.clipMeta.path)
}

// keyEditor edits the selected key's time, value/frame and interpolation (or frame duration for discrete sprite.frame keys), and deletes it.
func keyEditor(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	selectedTrack := func() (*resource.Track, int, int, bool) {
		_ = rev.Get()
		ti, ki, ok := ed.SelectedClipKey()
		if !ok {
			return nil, -1, -1, false
		}
		return &ed.clip.Tracks[ti], ti, ki, true
	}
	selectedIsFrame := func() bool {
		tr, _, _, ok := selectedTrack()
		return ok && tr.IsFrameTrack()
	}
	selectedNumeric := func() (resource.Key, int, int, bool) {
		tr, ti, ki, ok := selectedTrack()
		if !ok || tr.IsFrameTrack() || ki >= len(tr.Keys) {
			return resource.Key{}, -1, -1, false
		}
		return tr.Keys[ki], ti, ki, true
	}
	selectedFrame := func() (resource.FrameKey, string, int, int, bool) {
		tr, ti, ki, ok := selectedTrack()
		if !ok || !tr.IsFrameTrack() || ki >= len(tr.Frames) {
			return resource.FrameKey{}, "", -1, -1, false
		}
		return tr.Frames[ki], tr.Target, ti, ki, true
	}
	interpBtn := func(id, text string, interp resource.Interp) core.Node {
		return ui.ToggleButton{
			TestID:   id,
			Text:     text,
			Disabled: disabled,
			On: func() bool {
				k, _, _, ok := selectedNumeric()
				got := k.Interp
				if got == "" {
					got = resource.InterpLinear
				}
				return ok && got == interp
			},
			OnChange: func(on bool) {
				if _, ti, ki, ok := selectedNumeric(); ok && on {
					ed.report(ed.SetKeyInterp(ti, ki, interp))
				}
			},
		}
	}
	return ui.HStack{
		Gap: 6,
		Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(80)}, Children: []core.Node{ui.NumberInput{
				TestID:   "anim-key-time",
				Label:    "Key Time (seconds)",
				Min:      0,
				Step:     1 / 60.0,
				Disabled: disabled,
				Value: func() float64 {
					tr, _, ki, ok := selectedTrack()
					if !ok {
						return 0
					}
					return animclip.KeyValue(trackKeyTime(*tr, ki))
				},
				OnChange: func(v float64) {
					if _, ti, ki, ok := selectedTrack(); ok {
						_, err := ed.MoveKey(ti, ki, durationOfSeconds(v))
						ed.report(err)
					}
				},
			}}},
			core.Show{
				When: selectedIsFrame,
				Then: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.View{Style: core.Style{Width: core.Pt(140)}, Children: []core.Node{ui.Select[string]{
								TestID:     "anim-key-frame",
								Label:      "Key Frame",
								Searchable: true,
								Disabled:   disabled,
								OptionsFunc: func() []ui.Option[string] {
									fk, target, _, _, ok := selectedFrame()
									if !ok {
										return nil
									}
									entries := ed.ClipTargetFrames(target)
									opts := make([]ui.Option[string], 0, len(entries)+1)
									listed := false
									for _, ent := range entries {
										listed = listed || ent.Frame == fk.Frame
										opts = append(opts, ui.Option[string]{Value: ent.Frame, Label: ent.Frame})
									}
									if fk.Frame != "" && !listed {
										opts = append([]ui.Option[string]{{Value: fk.Frame, Label: fk.Frame}}, opts...)
									}
									return opts
								},
								Value: func() string {
									fk, _, _, _, _ := selectedFrame()
									return fk.Frame
								},
								OnChange: func(frame string) {
									if _, _, ti, ki, ok := selectedFrame(); ok {
										ed.report(ed.SetKeyFrame(ti, ki, frame))
									}
								},
							}}},
							ui.Button{
								TestID:   "anim-frame-shorter",
								Text:     "-10ms",
								Label:    "Shorter by 10 ms",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonOutline,
								Disabled: disabled,
								OnPress: func() {
									if _, _, ti, ki, ok := selectedFrame(); ok {
										if dur, ok := ed.FrameDuration(ti, ki); ok && dur > 10*time.Millisecond {
											ed.report(ed.SetFrameDuration(ti, ki, dur-10*time.Millisecond))
										}
									}
								},
							},
							core.Text{
								TestID: "anim-frame-duration",
								ContentFunc: func() string {
									_, _, ti, ki, ok := selectedFrame()
									if !ok {
										return ""
									}
									dur, ok := ed.FrameDuration(ti, ki)
									if !ok {
										return ""
									}
									return fmt.Sprintf("%d ms", dur.Milliseconds())
								},
								Style: core.Style{FontSize: 11, Color: colors.Muted()},
							},
							ui.Button{
								TestID:   "anim-frame-longer",
								Text:     "+10ms",
								Label:    "Longer by 10 ms",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonOutline,
								Disabled: disabled,
								OnPress: func() {
									if _, _, ti, ki, ok := selectedFrame(); ok {
										if dur, ok := ed.FrameDuration(ti, ki); ok {
											ed.report(ed.SetFrameDuration(ti, ki, dur+10*time.Millisecond))
										}
									}
								},
							},
						},
					}
				},
			},
			core.Show{
				When: func() bool { return !selectedIsFrame() },
				Then: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.View{Style: core.Style{Width: core.Pt(90)}, Children: []core.Node{ui.NumberInput{
								TestID:   "anim-key-value",
								Label:    "Key Value",
								Step:     0.1,
								Disabled: disabled,
								Value: func() float64 {
									k, _, _, _ := selectedNumeric()
									return animclip.KeyValue(k.Value)
								},
								OnChange: func(v float64) {
									if _, ti, ki, ok := selectedNumeric(); ok {
										ed.report(ed.SetKeyValue(ti, ki, v))
									}
								},
							}}},
							ui.ToolbarGroup{
								Label: "Interpolation",
								Children: []core.Node{
									interpBtn("anim-interp-linear", "Linear", resource.InterpLinear),
									interpBtn("anim-interp-smooth", "Smooth", resource.InterpSmooth),
									interpBtn("anim-interp-step", "Step", resource.InterpStep),
								},
							},
						},
					}
				},
			},
			ui.Button{
				TestID:   "anim-key-delete",
				Text:     "Delete Key",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					if _, ti, ki, ok := selectedTrack(); ok {
						ed.report(ed.DeleteKey(ti, ki))
					}
				},
			},
		},
	}
}

// laneLeft places a marker at seconds along a lane as long as the clip.
func (e *Editor) laneLeft(seconds float64) core.Length {
	return core.Pct(float32(min(max(seconds/e.clipLengthSeconds(), 0), 1) * 100))
}

func playheadLine(ed *Editor, rev *core.Signal[uint64], color core.Color) core.Node {
	return core.View{StyleFunc: func() core.Style {
		_ = rev.Get()
		return core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Width: core.Pt(1), Left: ed.laneLeft(ed.ClipTime().Seconds()), Background: color}
	}}
}

func trackLane(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], ti int, tr resource.Track, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	name := tr.Property
	if tr.Target != "" {
		name = tr.Target + " · " + tr.Property
	}
	if tr.IsFrameTrack() {
		return frameTrackLane(cx, ed, rev, ti, tr, name, disabled)
	}
	markers := []core.Node{playheadLine(ed, rev, colors.Accent())}
	for ki := range tr.Keys {
		markers = append(markers, core.View{
			TestID: fmt.Sprintf("anim-key-%d-%d", ti, ki),
			Role:   core.RoleButton,
			Label:  fmt.Sprintf("%s key %d", name, ki+1),
			OnPress: func(core.PressEvent) {
				ed.SelectClipKey(ti, ki)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && ki < len(ed.clip.Tracks[ti].Keys) {
					ed.SeekClip(animclip.Seconds(ed.clip.Tracks[ti].Keys[ki].Time))
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				fill := colors.Muted()
				left := core.Pct(0)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && ki < len(ed.clip.Tracks[ti].Keys) {
					left = ed.laneLeft(animclip.KeyValue(ed.clip.Tracks[ti].Keys[ki].Time))
				}
				if t, k, ok := ed.SelectedClipKey(); ok && t == ti && k == ki {
					fill = colors.Accent()
				}
				return core.Style{Position: core.Absolute, Top: core.Pt(2), Width: core.Pt(8), Height: core.Pt(14), Left: left, Margin: core.Edges{Left: -4}, Background: fill, CornerRadius: 2}
			},
		})
	}
	return ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{TestID: fmt.Sprintf("anim-track-%d-label", ti), Content: name, Style: core.Style{Width: core.Pt(150), FontSize: 12, Color: colors.Text()}},
			core.View{
				Style:    core.Style{FlexGrow: 1, Height: core.Pt(18), Position: core.Relative, Background: colors.Raised(), CornerRadius: 3},
				Children: markers,
			},
			ui.Button{
				TestID:   fmt.Sprintf("anim-track-%d-remove", ti),
				Text:     "Remove",
				Label:    "Remove track " + name,
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress:  func() { ed.report(ed.RemoveClipTrack(ti)) },
			},
		},
	}
}

func frameTrackLane(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], ti int, tr resource.Track, name string, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	totalSec := ed.clipLengthSeconds()
	if totalSec <= 0 {
		totalSec = 1
	}
	cells := make([]core.Node, 0, len(tr.Frames)+1)
	for ki, fk := range tr.Frames {
		frameIdx := ki
		frameKey := fk
		startSec := animclip.KeyValue(frameKey.Time)
		endSec := totalSec
		if frameIdx+1 < len(tr.Frames) {
			endSec = animclip.KeyValue(tr.Frames[frameIdx+1].Time)
		}
		spanPct := float32(min(max((endSec-startSec)/totalSec, 0), 1) * 100)
		entry, hasEntry := ed.ClipFrameEntry(tr.Target, frameKey.Frame)
		durMs := max(int64(animclip.Seconds(float32(endSec-startSec)).Milliseconds()), 0)
		tip := fmt.Sprintf("%s (%d ms)", frameKey.Frame, durMs)
		thumb := core.Canvas{
			TestID: fmt.Sprintf("anim-frame-thumb-%d-%d", ti, frameIdx),
			Style: core.Style{
				Width:        core.Pt(30),
				Height:       core.Pt(30),
				Background:   colors.Inset(),
				CornerRadius: 3,
			},
			Draw: func(d *core.Drawing, size core.Size) {
				if !hasEntry {
					return
				}
				f := entry.Rect
				sw, sh := float32(size.Width), float32(size.Height)
				if f.W <= 0 || f.H <= 0 || sw <= 0 || sh <= 0 {
					return
				}
				scale := min(sw/f.W, sh/f.H)
				dw, dh := f.W*scale, f.H*scale
				dx, dy := (sw-dw)/2, (sh-dh)/2
				d.DrawImageFrom(entry.Image, f.X, f.Y, f.W, f.H, dx, dy, dw, dh)
			},
		}
		cells = append(cells, withTooltip(core.View{
			TestID: fmt.Sprintf("anim-key-%d-%d", ti, frameIdx),
			Role:   core.RoleButton,
			Label:  fmt.Sprintf("%s frame %s", name, frameKey.Frame),
			OnPress: func(core.PressEvent) {
				ed.SelectClipKey(ti, frameIdx)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && frameIdx < len(ed.clip.Tracks[ti].Frames) {
					ed.SeekClip(animclip.Seconds(ed.clip.Tracks[ti].Frames[frameIdx].Time))
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				border := colors.Line()
				bg := colors.Surface()
				if t, k, ok := ed.SelectedClipKey(); ok && t == ti && k == frameIdx {
					border = colors.Accent()
					bg = colors.Raised()
				}
				return core.Style{
					Position:     core.Absolute,
					Top:          core.Pt(2),
					Bottom:       core.Pt(2),
					Left:         ed.laneLeft(startSec),
					Width:        core.Pct(spanPct),
					MinWidth:     core.Pt(36),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          4,
					Padding:      core.Symmetric(2, 3),
					Background:   bg,
					BorderWidth:  1,
					BorderColor:  border,
					CornerRadius: 4,
					Overflow:     core.Hidden,
				}
			},
			Children: []core.Node{
				thumb,
				core.Text{
					Content: frameKey.Frame,
					Style:   core.Style{FontSize: 10, Color: colors.Text()},
				},
			},
		}, tip))
	}
	cells = append(cells, playheadLine(ed, rev, colors.Accent()))
	return ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{TestID: fmt.Sprintf("anim-track-%d-label", ti), Content: name, Style: core.Style{Width: core.Pt(150), FontSize: 12, Color: colors.Text()}},
			core.View{
				Style:    core.Style{FlexGrow: 1, Height: core.Pt(40), Position: core.Relative, Background: colors.Raised(), CornerRadius: 4, Overflow: core.Hidden},
				Children: cells,
			},
			ui.Button{
				TestID:   fmt.Sprintf("anim-track-%d-remove", ti),
				Text:     "Remove",
				Label:    "Remove track " + name,
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress:  func() { ed.report(ed.RemoveClipTrack(ti)) },
			},
		},
	}
}

func eventLane(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], selEvent *core.Signal[int], eventName *core.Signal[string], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	markers := []core.Node{playheadLine(ed, rev, colors.Accent())}
	for i, ev := range ed.clip.Events {
		markers = append(markers, withTooltip(core.View{
			TestID:  fmt.Sprintf("anim-event-%d", i),
			Role:    core.RoleButton,
			Label:   "Event " + ev.Name,
			OnPress: func(core.PressEvent) { selEvent.Set(i) },
			StyleFunc: func() core.Style {
				_ = rev.Get()
				fill := colors.Warn()
				if selEvent.Get() == i {
					fill = colors.Accent()
				}
				return core.Style{Position: core.Absolute, Top: core.Pt(3), Width: core.Pt(10), Height: core.Pt(10), Left: ed.laneLeft(animclip.KeyValue(ev.Time)), Margin: core.Edges{Left: -5}, Background: fill, CornerRadius: 5}
			},
		}, ev.Name))
	}
	return ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{Content: "events", Style: core.Style{Width: core.Pt(150), FontSize: 12, Color: colors.Muted()}},
			core.View{
				Style:    core.Style{FlexGrow: 1, Height: core.Pt(18), Position: core.Relative, Background: colors.Raised(), CornerRadius: 3},
				Children: markers,
			},
			core.View{Style: core.Style{Width: core.Pt(110)}, Children: []core.Node{ui.Input{
				TestID:   "anim-event-name",
				Label:    "Event Name",
				Disabled: disabled,
				Value:    eventName.Get,
				OnChange: eventName.Set,
			}}},
			ui.Button{
				TestID:  "anim-event-add",
				Text:    "Add Event",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || eventName.Get() == ""
				},
				OnPress: func() { ed.report(ed.AddClipEvent(ed.ClipTime(), eventName.Get())) },
			},
			ui.Button{
				TestID:  "anim-event-delete",
				Text:    "Delete Event",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || selEvent.Get() < 0 || ed.clip == nil || selEvent.Get() >= len(ed.clip.Events)
				},
				OnPress: func() {
					ed.report(ed.DeleteClipEvent(selEvent.Get()))
					selEvent.Set(-1)
				},
			},
		},
	}
}
