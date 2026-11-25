package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
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

// clipLengthSeconds returns the open clip's length in seconds, or 1 with none open or non-positive/non-finite length.
func (e *Editor) clipLengthSeconds() float64 {
	if e.clip == nil {
		return 1
	}
	v := animclip.KeyValue(e.clip.Length)
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 1
	}
	return v
}

// report shows err in the status bar and appends it to the Output panel history, so an edit the panel's controls make and the editor refuses is never silently dropped or overwritten by the next error.
func (e *Editor) report(err error) {
	if err == nil {
		return
	}
	e.reportStatusError("Error: " + err.Error())
}

func durationOfSeconds(s float64) time.Duration {
	if math.IsNaN(s) || math.IsInf(s, 0) || s <= 0 {
		return 0
	}
	const maxSec = float64(math.MaxInt64/time.Second) - 1
	if s >= maxSec {
		return time.Duration(maxSec) * time.Second
	}
	return time.Duration(s * float64(time.Second))
}

func animationPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	property := core.NewSignal(cx, "")
	frameChoice := core.NewSignal(cx, "")
	asepriteTag := core.NewSignal(cx, "")
	runFPS := core.NewSignal(cx, 12.0)
	eventName := core.NewSignal(cx, "")
	selEvent := core.NewSignal(cx, -1)
	saveShortcut := FormatShortcut(ed.EffectiveShortcut(CmdSave), cx.Env().OS())
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	// Memoize clip playback state so 60 Hz rev ticks during preview playback do not remount anim-play's icon.
	clipPlaying := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ClipPlaying()
	})
	keySelected := func() bool {
		_ = rev.Get()
		_, _, ok := ed.SelectedClipKey()
		return ok
	}
	// structure changes when tracks or keys are added or removed, frame thumbnails change, loading toggles, theme changes, or events change, so the lanes remount then and not on every numeric key value edit.
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.clip == nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "len=%v|load=%t|dark=%t|", ed.clip.Length, ed.clipLoading, cx.Dark())
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

	header := specPanelHeader(p,
		specPanelTitle(p, "", lucide.Film, "Animation", nil),
		core.Text{
			TestID: "anim-target",
			ContentFunc: func() string {
				_ = rev.Get()
				return "on " + ed.ClipTarget()
			},
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextMuted,
			},
		},
		core.View{
			Style: core.Style{Width: core.Pt(144)},
			Children: []core.Node{ui.Input{
				TestID:      "anim-name",
				Label:       "Clip Name",
				Placeholder: "Clip name",
				Style:       specInputStyle(p),
				Disabled:    disabled,
				Value: func() string {
					_ = rev.Get()
					if ed.clip == nil {
						return ""
					}
					return ed.clip.Name
				},
				OnChange: func(text string) { ed.report(ed.SetClipName(text)) },
			}},
		},
		specSectionLabel(p, "LENGTH"),
		core.View{
			Style: core.Style{Width: core.Pt(104)},
			Children: []core.Node{ui.NumberInput{
				TestID:   "anim-length",
				Label:    "Clip Length (seconds)",
				Min:      0.01,
				Step:     0.1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
				Style:    specNumberInputStyle(p),
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					return ed.clipLengthSeconds()
				},
				OnChange: func(v float64) { ed.report(ed.SetClipLength(durationOfSeconds(v))) },
			}},
		},
		specPanelToggle(p, specPanelToggleSpec{
			TestID:   "anim-loop",
			Text:     "Loop",
			Icon:     lucide.Repeat,
			Label:    "Loop clip playback",
			Tooltip:  "Loop clip playback",
			Disabled: disabled,
			On: func() bool {
				_ = rev.Get()
				return ed.clip != nil && ed.clip.Loop
			},
			OnChange: func(on bool) { ed.report(ed.SetClipLoop(on)) },
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:  "anim-play",
			Variant: specBtnPrimary,
			Compact: true,
			IconFunc: func() *core.SVG {
				if clipPlaying.Get() {
					return lucide.Pause()
				}
				return lucide.Play()
			},
			TextFunc: func() string {
				if clipPlaying.Get() {
					return "Pause"
				}
				return "Play"
			},
			LabelFunc: func() string {
				if clipPlaying.Get() {
					return "Pause clip preview"
				}
				return "Play clip preview"
			},
			Tooltip:  "Play or pause clip preview",
			Disabled: disabled,
			OnPress: func() {
				if ed.ClipPlaying() {
					ed.PauseClip()
					return
				}
				ed.report(ed.PlayClip())
			},
		}),
		core.Text{
			TestID: "anim-time",
			ContentFunc: func() string {
				_ = rev.Get()
				return fmt.Sprintf("%.2f / %.2f s", ed.ClipTime().Seconds(), ed.clipLengthSeconds())
			},
			Style: core.Style{
				FontFamily:  core.FontMonospace,
				FontSize:    11.5,
				FontVariant: core.FontTabularNums,
				Color:       p.TextMuted,
				FlexShrink:  0,
			},
		},
		core.View{Style: core.Style{FlexGrow: 1}},
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "anim-save",
			Text:     "Save Clip",
			Icon:     lucide.Save,
			Label:    "Save Clip",
			Tooltip:  specShortcutTip("Save Clip", saveShortcut),
			Variant:  specBtnQuiet,
			Compact:  true,
			Disabled: disabled,
			OnPress:  func() { ed.report(ed.saveClipFromPanel()) },
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "anim-close",
			Icon:     lucide.X,
			Label:    "Close Animation",
			Tooltip:  specShortcutTip("Close Animation", "Esc"),
			Variant:  specBtnGhost,
			Compact:  true,
			Disabled: disabled,
			OnPress:  func() { ed.report(ed.CloseClip()) },
		}),
	)

	scrubber := ui.Slider{
		TestID:   "anim-scrub",
		Label:    "Playhead",
		Style:    core.Style{FocusVisible: p.FocusRing(-2)},
		Disabled: disabled,
		MinFunc:  func() float64 { return 0 },
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

	keyBar := core.View{
		Style: core.Style{
			Direction:  core.Row,
			Wrap:       core.Wrap,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Width: core.Pt(180)},
				Children: []core.Node{ui.Select[string]{
					TestID:      "anim-property",
					Label:       "Property to key",
					Placeholder: "Property",
					Searchable:  true,
					Style:       specSelectStyle(p),
					Disabled:    disabled,
					OptionsFunc: func() []ui.Option[string] {
						_ = rev.Get()
						props := animclip.Properties(ed.components)
						opts := make([]ui.Option[string], len(props))
						for i, prop := range props {
							opts[i] = ui.Option[string]{Value: prop.Name, Label: prop.Name}
						}
						return opts
					},
					Value:    property.Get,
					OnChange: property.Set,
				}},
			},
			core.Show{
				When: func() bool { return property.Get() == animclip.PropertySpriteFrame },
				Then: func(cx *core.Scope) core.Node {
					p := ed.SpecPalette(cx)
					return core.View{
						Style: core.Style{Width: core.Pt(160)},
						Children: []core.Node{ui.Select[string]{
							TestID:      "anim-frame-picker",
							Label:       "Sprite frame to key",
							Placeholder: "Current frame",
							Searchable:  true,
							Style:       specSelectStyle(p),
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
						}},
					}
				},
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "anim-key",
				Text:    "Key",
				Icon:    lucide.KeyRound,
				Label:   "Key property at playhead",
				Tooltip: "Insert a keyframe for the chosen property at the playhead",
				Variant: specBtnSecondary,
				Compact: true,
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
			}),
			core.Show{
				When: keySelected,
				Then: func(cx *core.Scope) core.Node { return keyEditor(cx, ed.SpecPalette(cx), ed, rev, disabled) },
			},
		},
	}

	flipbookBar := core.Show{
		When: func() bool {
			_ = rev.Get()
			return len(ed.ClipTargetFrames("")) > 0
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
				},
				Children: []core.Node{
					specSectionLabel(p, "FLIPBOOK"),
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return len(ed.ClipTargetAsepriteTags("")) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							p := ed.SpecPalette(cx)
							return core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
								Children: []core.Node{
									core.View{
										Style: core.Style{Width: core.Pt(152)},
										Children: []core.Node{ui.Select[string]{
											TestID:      "anim-aseprite-tag",
											Label:       "Aseprite tag",
											Placeholder: "Aseprite tag",
											Searchable:  true,
											Style:       specSelectStyle(p),
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
										}},
									},
									specPanelButton(p, specPanelButtonSpec{
										TestID:  "anim-from-tag",
										Text:    "From Tag",
										Icon:    lucide.Tag,
										Label:   "Create clip from Aseprite tag",
										Tooltip: "Build flipbook frames from the selected Aseprite tag",
										Variant: specBtnQuiet,
										Compact: true,
										Disabled: func() bool {
											_ = rev.Get()
											return ed.Playing() || asepriteTag.Get() == ""
										},
										OnPress: func() {
											ed.report(ed.NewClipFromAsepriteTag(ed.ClipTarget(), asepriteTag.Get()))
										},
									}),
								},
							}
						},
					},
					specSectionLabel(p, "FPS"),
					core.View{
						Style: core.Style{Width: core.Pt(84)},
						Children: []core.Node{ui.NumberInput{
							TestID:   "anim-run-fps",
							Label:    "Frame rate (FPS)",
							Min:      1,
							Step:     1,
							Format:   func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) },
							Style:    specNumberInputStyle(p),
							Disabled: disabled,
							Value:    runFPS.Get,
							OnChange: runFPS.Set,
						}},
					},
					specPanelButton(p, specPanelButtonSpec{
						TestID:   "anim-from-frames",
						Text:     "From Frames",
						Icon:     lucide.Film,
						Label:    "Create clip from atlas frames",
						Tooltip:  "Create a flipbook clip from all atlas frames at the chosen FPS",
						Variant:  specBtnQuiet,
						Compact:  true,
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
					}),
				},
			}
		},
	}

	errorBanner := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.clipErr != nil
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			return specStatusBanner(p, "anim-error", false, "", func() string {
				_ = rev.Get()
				if ed.clipErr == nil {
					return ""
				}
				return "Preview: " + ed.clipErr.Error()
			}, nil)
		},
	}

	lanes := ui.ScrollView{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
		ContentStyle: core.Style{
			Padding: core.Edges{Left: specSpace16, Right: specSpace16, Bottom: specSpace8},
			Gap:     specSpace8,
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if structure.Get() == "" {
				return nil
			}
			p := ed.SpecPalette(cx)
			if ed.ClipLoading() {
				return specLoadingState(p, "anim-loading", "Loading animation clip…")
			}
			var rows []core.Node
			if len(ed.clip.Tracks) == 0 && len(ed.clip.Events) == 0 {
				rows = append(rows, specEmptyState(p, "anim-empty", lucide.KeyRound, p.TextFaint, "No tracks yet", "Choose a property above and press Key to add keyframes at the playhead.", nil))
			}
			for ti, tr := range ed.clip.Tracks {
				rows = append(rows, trackLane(cx, p, ed, rev, ti, tr, disabled))
			}
			rows = append(rows, eventLane(cx, p, ed, rev, selEvent, eventName, disabled))
			return core.View{Style: core.Style{Gap: specSpace4}, Children: rows}
		})},
	}

	controls := core.View{
		Style: core.Style{
			Padding:    core.Symmetric(specSpace8, specSpace16),
			Gap:        specSpace8,
			FlexShrink: 0,
		},
		Children: []core.Node{
			scrubber,
			keyBar,
			flipbookBar,
			errorBanner,
		},
	}

	return specPanelContainer(cx, p, "anim-panel", "Animation", 284, header, controls, lanes)
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
		e.clipSaved = append([]byte(nil), data...)
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
func keyEditor(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
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
	interpBtn := func(p SpecPalette, id, text string, interp resource.Interp) core.Node {
		return specPanelToggle(p, specPanelToggleSpec{
			TestID:   id,
			Text:     text,
			Label:    "Interpolation " + text,
			Tooltip:  "Set keyframe interpolation to " + text,
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
		})
	}
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			Wrap:       core.Wrap,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			specSectionLabel(p, "TIME"),
			core.View{
				Style: core.Style{Width: core.Pt(96)},
				Children: []core.Node{ui.NumberInput{
					TestID:   "anim-key-time",
					Label:    "Key Time (seconds)",
					Min:      0,
					Step:     1 / 60.0,
					Format:   func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
					Style:    specNumberInputStyle(p),
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
				}},
			},
			core.Show{
				When: selectedIsFrame,
				Then: func(cx *core.Scope) core.Node {
					p := ed.SpecPalette(cx)
					return core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							core.View{
								Style: core.Style{Width: core.Pt(152)},
								Children: []core.Node{ui.Select[string]{
									TestID:     "anim-key-frame",
									Label:      "Key Frame",
									Searchable: true,
									Style:      specSelectStyle(p),
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
								}},
							},
							specPanelButton(p, specPanelButtonSpec{
								TestID:   "anim-frame-shorter",
								Text:     "-10ms",
								Icon:     lucide.Minus,
								Label:    "Shorter by 10 ms",
								Tooltip:  "Shorten frame duration by 10 ms",
								Variant:  specBtnQuiet,
								Compact:  true,
								Disabled: disabled,
								OnPress: func() {
									if _, _, ti, ki, ok := selectedFrame(); ok {
										if dur, ok := ed.FrameDuration(ti, ki); ok && dur > 10*time.Millisecond {
											ed.report(ed.SetFrameDuration(ti, ki, dur-10*time.Millisecond))
										}
									}
								},
							}),
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
								Style: core.Style{
									FontFamily:  core.FontMonospace,
									FontSize:    11.5,
									FontVariant: core.FontTabularNums,
									Color:       p.TextMuted,
								},
							},
							specPanelButton(p, specPanelButtonSpec{
								TestID:   "anim-frame-longer",
								Text:     "+10ms",
								Icon:     lucide.Plus,
								Label:    "Longer by 10 ms",
								Tooltip:  "Extend frame duration by 10 ms",
								Variant:  specBtnQuiet,
								Compact:  true,
								Disabled: disabled,
								OnPress: func() {
									if _, _, ti, ki, ok := selectedFrame(); ok {
										if dur, ok := ed.FrameDuration(ti, ki); ok {
											ed.report(ed.SetFrameDuration(ti, ki, dur+10*time.Millisecond))
										}
									}
								},
							}),
						},
					}
				},
			},
			core.Show{
				When: func() bool { return !selectedIsFrame() },
				Then: func(cx *core.Scope) core.Node {
					p := ed.SpecPalette(cx)
					return core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							specSectionLabel(p, "VALUE"),
							core.View{
								Style: core.Style{Width: core.Pt(96)},
								Children: []core.Node{ui.NumberInput{
									TestID:   "anim-key-value",
									Label:    "Key Value",
									Step:     0.1,
									Format:   func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) },
									Style:    specNumberInputStyle(p),
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
								}},
							},
							specPanelToggleGroup(p,
								"Interpolation",
								interpBtn(p, "anim-interp-linear", "Linear", resource.InterpLinear),
								interpBtn(p, "anim-interp-smooth", "Smooth", resource.InterpSmooth),
								interpBtn(p, "anim-interp-step", "Step", resource.InterpStep),
							),
						},
					}
				},
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:   "anim-key-delete",
				Text:     "Delete Key",
				Icon:     lucide.Trash,
				Label:    "Delete Key",
				Tooltip:  "Delete the selected keyframe",
				Variant:  specBtnDanger,
				Compact:  true,
				Disabled: disabled,
				OnPress: func() {
					if _, ti, ki, ok := selectedTrack(); ok {
						ed.report(ed.DeleteKey(ti, ki))
					}
				},
			}),
		},
	}
}

// laneLeft places a marker at seconds along a lane as long as the clip, always returning a finite percentage in [0, 100].
func (e *Editor) laneLeft(seconds float64) core.Length {
	length := e.clipLengthSeconds()
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || math.IsNaN(length) || math.IsInf(length, 0) || length <= 0 {
		return core.Pct(0)
	}
	ratio := seconds / length
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return core.Pct(0)
	}
	return core.Pct(float32(min(max(ratio, 0), 1) * 100))
}

func playheadLine(ed *Editor, rev *core.Signal[uint64], color core.Color) core.Node {
	return core.View{StyleFunc: func() core.Style {
		_ = rev.Get()
		return core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Width: core.Pt(2), Left: ed.laneLeft(ed.ClipTime().Seconds()), Background: color}
	}}
}

func trackLane(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], ti int, tr resource.Track, disabled func() bool) core.Node {
	name := tr.Property
	if tr.Target != "" {
		name = tr.Target + " · " + tr.Property
	}
	if tr.IsFrameTrack() {
		return frameTrackLane(cx, p, ed, rev, ti, tr, name, disabled)
	}
	markers := []core.Node{playheadLine(ed, rev, p.Accent.Fg)}
	hoverFill := specHoverOverlay(p)
	focusRing := p.FocusRing(-2)
	for ki, k := range tr.Keys {
		keyIdx := ki
		tip := fmt.Sprintf("%s @ %.2fs = %g", name, animclip.KeyValue(k.Time), animclip.KeyValue(k.Value))
		markers = append(markers, base.Button{
			TestID:  fmt.Sprintf("anim-key-%d-%d", ti, keyIdx),
			Label:   fmt.Sprintf("%s key %d", name, keyIdx+1),
			Tooltip: specTooltipNode(p, tip),
			Selected: func() bool {
				_ = rev.Get()
				t, kk, ok := ed.SelectedClipKey()
				return ok && t == ti && kk == keyIdx
			},
			OnPress: func(core.PressEvent) {
				ed.SelectClipKey(ti, keyIdx)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && keyIdx < len(ed.clip.Tracks[ti].Keys) {
					ed.SeekClip(animclip.Seconds(ed.clip.Tracks[ti].Keys[keyIdx].Time))
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				left := core.Pct(0)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && keyIdx < len(ed.clip.Tracks[ti].Keys) {
					left = ed.laneLeft(animclip.KeyValue(ed.clip.Tracks[ti].Keys[keyIdx].Time))
				}
				return core.Style{
					Position:       core.Absolute,
					Top:            core.Pt(0),
					Width:          core.Pt(specRowHeight),
					Height:         core.Pt(specRowHeight),
					Left:           left,
					Margin:         core.Edges{Left: -14},
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					CornerRadius:   specRadiusControl,
					Cursor:         core.CursorPointer,
					Hover:          &core.Style{Background: hoverFill},
					FocusVisible:   focusRing,
				}
			},
			Children: []core.Node{
				core.View{
					StyleFunc: func() core.Style {
						_ = rev.Get()
						fill := p.TextMuted
						if t, kk, ok := ed.SelectedClipKey(); ok && t == ti && kk == keyIdx {
							fill = p.Accent.Fg
						}
						return core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(14),
							CornerRadius: 3,
							Background:   fill,
						}
					},
				},
			},
		})
	}
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("anim-track-%d-label", ti),
				Content: name,
				Style: core.Style{
					Width:      core.Pt(152),
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextSecondary,
					FlexShrink: 0,
				},
			},
			core.View{
				Style: core.Style{
					FlexGrow:     1,
					Height:       core.Pt(specRowHeight),
					Position:     core.Relative,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					CornerRadius: specRadiusControl,
				},
				Children: markers,
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:     fmt.Sprintf("anim-track-%d-remove", ti),
				Text:       "Remove",
				Icon:       lucide.Trash,
				Label:      "Remove track " + name,
				Tooltip:    "Remove track " + name,
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled:   disabled,
				OnPress:    func() { ed.report(ed.RemoveClipTrack(ti)) },
			}),
		},
	}
}

func frameTrackLane(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], ti int, tr resource.Track, name string, disabled func() bool) core.Node {
	totalSec := ed.clipLengthSeconds()
	if math.IsNaN(totalSec) || math.IsInf(totalSec, 0) || totalSec <= 0 {
		totalSec = 1
	}
	focusRing := p.FocusRing(-2)
	cells := make([]core.Node, 0, len(tr.Frames)+1)
	for ki, fk := range tr.Frames {
		frameIdx := ki
		frameKey := fk
		startSec := animclip.KeyValue(frameKey.Time)
		endSec := totalSec
		if frameIdx+1 < len(tr.Frames) {
			endSec = animclip.KeyValue(tr.Frames[frameIdx+1].Time)
		}
		rawSpan := (endSec - startSec) / totalSec
		if math.IsNaN(rawSpan) || math.IsInf(rawSpan, 0) {
			rawSpan = 0
		}
		spanPct := float32(min(max(rawSpan, 0), 1) * 100)
		entry, hasEntry := ed.ClipFrameEntry(tr.Target, frameKey.Frame)
		durMs := durationOfSeconds(endSec - startSec).Milliseconds()
		tip := fmt.Sprintf("%s (%d ms)", frameKey.Frame, durMs)
		thumb := core.Canvas{
			TestID: fmt.Sprintf("anim-frame-thumb-%d-%d", ti, frameIdx),
			Style: core.Style{
				Width:        core.Pt(28),
				Height:       core.Pt(28),
				Background:   p.Bg,
				CornerRadius: 4,
				FlexShrink:   0,
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
		cells = append(cells, base.Button{
			TestID:  fmt.Sprintf("anim-key-%d-%d", ti, frameIdx),
			Label:   fmt.Sprintf("%s frame %s", name, frameKey.Frame),
			Tooltip: specTooltipNode(p, tip),
			Selected: func() bool {
				_ = rev.Get()
				t, k, ok := ed.SelectedClipKey()
				return ok && t == ti && k == frameIdx
			},
			OnPress: func(core.PressEvent) {
				ed.SelectClipKey(ti, frameIdx)
				if ed.clip != nil && ti < len(ed.clip.Tracks) && frameIdx < len(ed.clip.Tracks[ti].Frames) {
					ed.SeekClip(animclip.Seconds(ed.clip.Tracks[ti].Frames[frameIdx].Time))
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				border := p.LineRaised
				bg := p.Panel
				if t, k, ok := ed.SelectedClipKey(); ok && t == ti && k == frameIdx {
					border = p.Accent.Edge
					bg = p.Accent.Fill
				}
				return core.Style{
					Position:     core.Absolute,
					Top:          core.Pt(4),
					Bottom:       core.Pt(4),
					Left:         ed.laneLeft(startSec),
					Width:        core.Pct(spanPct),
					MinWidth:     core.Pt(36),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace4,
					Padding:      core.Symmetric(0, specSpace4),
					Background:   bg,
					BorderWidth:  1,
					BorderColor:  border,
					CornerRadius: specRadiusControl,
					Overflow:     core.Hidden,
					Cursor:       core.CursorPointer,
					Hover:        &core.Style{Background: p.LineRaised},
					FocusVisible: focusRing,
				}
			},
			Children: []core.Node{
				thumb,
				core.Text{
					Content:  frameKey.Frame,
					MaxLines: 1,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextSecondary,
					},
				},
			},
		})
	}
	cells = append(cells, playheadLine(ed, rev, p.Accent.Fg))
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("anim-track-%d-label", ti),
				Content: name,
				Style: core.Style{
					Width:      core.Pt(152),
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextSecondary,
					FlexShrink: 0,
				},
			},
			core.View{
				Style: core.Style{
					FlexGrow:     1,
					Height:       core.Pt(44),
					Position:     core.Relative,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					CornerRadius: specRadiusControl,
					Overflow:     core.Hidden,
				},
				Children: cells,
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:     fmt.Sprintf("anim-track-%d-remove", ti),
				Text:       "Remove",
				Icon:       lucide.Trash,
				Label:      "Remove track " + name,
				Tooltip:    "Remove track " + name,
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled:   disabled,
				OnPress:    func() { ed.report(ed.RemoveClipTrack(ti)) },
			}),
		},
	}
}

func eventLane(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], selEvent *core.Signal[int], eventName *core.Signal[string], disabled func() bool) core.Node {
	markers := []core.Node{playheadLine(ed, rev, p.Accent.Fg)}
	hoverFill := specHoverOverlay(p)
	focusRing := p.FocusRing(-2)
	for i, ev := range ed.clip.Events {
		evIdx := i
		evCopy := ev
		tip := fmt.Sprintf("Event %s @ %.2fs", evCopy.Name, animclip.KeyValue(evCopy.Time))
		markers = append(markers, base.Button{
			TestID:  fmt.Sprintf("anim-event-%d", evIdx),
			Label:   "Event " + evCopy.Name,
			Tooltip: specTooltipNode(p, tip),
			Selected: func() bool {
				return selEvent.Get() == evIdx
			},
			OnPress: func(core.PressEvent) { selEvent.Set(evIdx) },
			StyleFunc: func() core.Style {
				_ = rev.Get()
				return core.Style{
					Position:       core.Absolute,
					Top:            core.Pt(0),
					Width:          core.Pt(specRowHeight),
					Height:         core.Pt(specRowHeight),
					Left:           ed.laneLeft(animclip.KeyValue(evCopy.Time)),
					Margin:         core.Edges{Left: -14},
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					CornerRadius:   specRadiusControl,
					Cursor:         core.CursorPointer,
					Hover:          &core.Style{Background: hoverFill},
					FocusVisible:   focusRing,
				}
			},
			Children: []core.Node{
				core.View{
					StyleFunc: func() core.Style {
						fill := p.Warning.Fg
						if selEvent.Get() == evIdx {
							fill = p.Accent.Fg
						}
						return core.Style{
							Width:        core.Pt(10),
							Height:       core.Pt(10),
							CornerRadius: 5,
							Background:   fill,
						}
					},
				},
			},
		})
	}
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:      core.Pt(152),
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					FlexShrink: 0,
				},
				Children: []core.Node{
					specIcon(lucide.Sparkles, 13, p.Warning.Fg),
					specSectionLabel(p, "EVENTS"),
				},
			},
			core.View{
				Style: core.Style{
					FlexGrow:     1,
					Height:       core.Pt(specRowHeight),
					Position:     core.Relative,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					CornerRadius: specRadiusControl,
				},
				Children: markers,
			},
			core.View{
				Style: core.Style{Width: core.Pt(120)},
				Children: []core.Node{ui.Input{
					TestID:      "anim-event-name",
					Label:       "Event Name",
					Placeholder: "Event name",
					Style:       specInputStyle(p),
					Disabled:    disabled,
					Value:       eventName.Get,
					OnChange:    eventName.Set,
				}},
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:     "anim-event-add",
				Text:       "Add Event",
				Icon:       lucide.Plus,
				Label:      "Add Event",
				Tooltip:    "Add named event at the current playhead time",
				Variant:    specBtnSecondary,
				Compact:    true,
				InsetFocus: true,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || eventName.Get() == ""
				},
				OnPress: func() { ed.report(ed.AddClipEvent(ed.ClipTime(), eventName.Get())) },
			}),
			specPanelButton(p, specPanelButtonSpec{
				TestID:     "anim-event-delete",
				Text:       "Delete Event",
				Icon:       lucide.Trash,
				Label:      "Delete Event",
				Tooltip:    "Delete the selected event marker",
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || selEvent.Get() < 0 || ed.clip == nil || selEvent.Get() >= len(ed.clip.Events)
				},
				OnPress: func() {
					ed.report(ed.DeleteClipEvent(selEvent.Get()))
					selEvent.Set(-1)
				},
			}),
		},
	}
}
