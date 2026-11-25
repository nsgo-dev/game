package editor

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/skeletal"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// RigPanelView renders the 2D skeletal rig editor panel below the viewport while a rig is open:
// an interactive canvas showing attachments, bones, bone handles and attachment-origin handles;
// bone, slot, skin and animation controls; and a keyframe timeline with play and crossfade preview
// driven directly by the runtime skeletal package.
func RigPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.RigOpen()
		},
		Then: func(cx *core.Scope) core.Node { return rigPanel(cx, ed, rev) },
	}
}

func rigPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	newBoneName := core.NewSignal(cx, "")
	newSlotName := core.NewSignal(cx, "")
	newSkinName := core.NewSignal(cx, "")
	newAnimName := core.NewSignal(cx, "")
	crossfadeTarget := core.NewSignal(cx, "")
	crossfadeSec := core.NewSignal(cx, 0.2)
	keyTransX := core.NewSignal(cx, 0.0)
	keyTransY := core.NewSignal(cx, 0.0)
	keyRotAngle := core.NewSignal(cx, 0.0)
	keyScaleX := core.NewSignal(cx, 1.0)
	keyScaleY := core.NewSignal(cx, 1.0)
	keyAttachFrame := core.NewSignal(cx, "")
	activeTab := core.NewSignal(cx, "bones")

	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	selectedBoneData := func() (skeletal.BoneData, bool) {
		if !ed.RigOpen() {
			return skeletal.BoneData{}, false
		}
		if idx := findRigBone(ed.rig.data, ed.SelectedRigBone()); idx >= 0 {
			return ed.rig.data.Bones[idx], true
		}
		return skeletal.BoneData{}, false
	}

	selectedSlotAttach := func() (skin, slot string, att skeletal.Attachment, ok bool) {
		if !ed.RigOpen() {
			return "", "", skeletal.Attachment{}, false
		}
		slot = ed.SelectedRigSlot()
		if findRigSlot(ed.rig.data, slot) < 0 {
			return "", "", skeletal.Attachment{}, false
		}
		skin = ed.RigSkin()
		if skin == "" {
			skin = "default"
		}
		att = skeletal.Attachment{ScaleX: 1, ScaleY: 1}
		if sk, hasSkin := ed.rig.data.Skins[skin]; hasSkin {
			if a, hasAtt := sk[slot]; hasAtt {
				att = a
			}
		}
		return skin, slot, att, true
	}

	bonesListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.RigOpen() {
			return ""
		}
		var b strings.Builder
		for i, bone := range ed.rig.data.Bones {
			if i > 0 {
				b.WriteByte(0)
			}
			b.WriteString(bone.Name)
		}
		return b.String()
	})

	slotsListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.RigOpen() {
			return ""
		}
		var b strings.Builder
		for i, sl := range ed.rig.data.Slots {
			if i > 0 {
				b.WriteByte(0)
			}
			b.WriteString(sl.Name)
			b.WriteByte(1)
			b.WriteString(sl.Bone)
		}
		return b.String()
	})

	timelineRowsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.RigOpen() {
			return ""
		}
		anim := ed.activeRigAnim()
		if anim == nil {
			return ""
		}
		var b strings.Builder
		b.WriteString(anim.Name)
		for _, bn := range slices.Sorted(maps.Keys(anim.Bones)) {
			tl := anim.Bones[bn]
			fmt.Fprintf(&b, "|b:%s:%d,%d,%d", bn, len(tl.Translate), len(tl.Rotate), len(tl.Scale))
		}
		for _, sn := range slices.Sorted(maps.Keys(anim.Slots)) {
			tl := anim.Slots[sn]
			fmt.Fprintf(&b, "|s:%s:%d,%d", sn, len(tl.Attachment), len(tl.Alpha))
		}
		return b.String()
	})

	label := func(text string) core.Node {
		return core.Text{Content: text, Style: core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()}}
	}

	animDurSec := func() float64 {
		a := ed.activeRigAnim()
		if a == nil || a.Duration <= 0 {
			return 1
		}
		return a.Duration.Seconds()
	}

	header := core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Wrap: core.Wrap, Gap: 6},
		Children: []core.Node{
			core.Text{Content: "Rig Editor", Style: core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()}},
			core.View{Style: core.Style{Width: core.Pt(110)}, Children: []core.Node{ui.Input{
				TestID:   "rig-name",
				Label:    "Rig Name",
				Disabled: disabled,
				Value: func() string {
					_ = rev.Get()
					if !ed.RigOpen() {
						return ""
					}
					return ed.rig.data.Name
				},
				OnChange: func(v string) { ed.report(ed.SetRigName(v)) },
			}}},
			core.View{Style: core.Style{Width: core.Pt(105)}, Children: []core.Node{ui.Select[string]{
				TestID:      "rig-atlas",
				Label:       "Rig Atlas",
				Placeholder: "Atlas",
				Disabled:    disabled,
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					seen := map[string]bool{}
					var opts []ui.Option[string]
					for _, ent := range ed.Assets() {
						if !seen[ent.Atlas] {
							seen[ent.Atlas] = true
							opts = append(opts, ui.Option[string]{Value: ent.Atlas, Label: ent.Atlas})
						}
					}
					if cur := ed.RigAtlas(); cur != "" && !seen[cur] {
						opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
					}
					return opts
				},
				Value: func() string {
					_ = rev.Get()
					return ed.RigAtlas()
				},
				OnChange: func(v string) { ed.report(ed.SetRigAtlas(v)) },
			}}},
			core.View{Style: core.Style{Width: core.Pt(105)}, Children: []core.Node{ui.Select[string]{
				TestID:      "rig-skin",
				Label:       "Preview Skin",
				Placeholder: "Skin",
				Disabled:    disabled,
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					names := ed.RigSkinNames()
					opts := make([]ui.Option[string], len(names))
					for i, n := range names {
						opts[i] = ui.Option[string]{Value: n, Label: n}
					}
					return opts
				},
				Value: func() string {
					_ = rev.Get()
					return ed.RigSkin()
				},
				OnChange: func(v string) { ed.report(ed.SelectRigSkin(v)) },
			}}},
			core.View{Style: core.Style{Width: core.Pt(115)}, Children: []core.Node{ui.Select[string]{
				TestID:      "rig-animation",
				Label:       "Active Animation",
				Placeholder: "(Bind Pose)",
				Disabled:    disabled,
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					names := ed.RigAnimationNames()
					opts := make([]ui.Option[string], 0, len(names)+1)
					opts = append(opts, ui.Option[string]{Value: "", Label: "(Bind Pose)"})
					for _, n := range names {
						opts = append(opts, ui.Option[string]{Value: n, Label: n})
					}
					return opts
				},
				Value: func() string {
					_ = rev.Get()
					return ed.RigAnimation()
				},
				OnChange: func(v string) { ed.report(ed.SelectRigAnimation(v)) },
			}}},
			ui.Button{
				TestID:  "rig-play",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonPrimary,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || ed.RigAnimation() == ""
				},
				TextFunc: func() string {
					_ = rev.Get()
					if ed.RigPlaying() {
						return "Pause"
					}
					return "Play"
				},
				OnPress: func() {
					if ed.RigPlaying() {
						ed.PauseRig()
						return
					}
					ed.report(ed.PlayRig())
				},
			},
			core.View{Style: core.Style{Width: core.Pt(105)}, Children: []core.Node{ui.Select[string]{
				TestID:      "rig-crossfade-target",
				Label:       "Crossfade Target",
				Placeholder: "Crossfade to",
				Disabled:    disabled,
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					names := ed.RigAnimationNames()
					opts := make([]ui.Option[string], len(names))
					for i, n := range names {
						opts[i] = ui.Option[string]{Value: n, Label: n}
					}
					return opts
				},
				Value:    crossfadeTarget.Get,
				OnChange: crossfadeTarget.Set,
			}}},
			core.View{Style: core.Style{Width: core.Pt(62)}, Children: []core.Node{ui.NumberInput{
				TestID:   "rig-crossfade-sec",
				Label:    "Crossfade Duration (s)",
				Min:      0,
				Step:     0.05,
				Disabled: disabled,
				Value:    crossfadeSec.Get,
				OnChange: crossfadeSec.Set,
			}}},
			ui.Button{
				TestID:  "rig-crossfade",
				Text:    "Blend",
				Label:   "Crossfade to selected animation",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || crossfadeTarget.Get() == ""
				},
				OnPress: func() {
					ed.report(ed.CrossfadeRig(crossfadeTarget.Get(), durationOfSeconds(crossfadeSec.Get())))
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:   "rig-place-scene",
				Text:     "Place in Scene",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonSecondary,
				Disabled: disabled,
				OnPress: func() {
					cxPos, cyPos := float32(64), float32(64)
					if ed.Doc != nil && ed.Doc.VirtualWidth > 0 && ed.Doc.VirtualHeight > 0 {
						cxPos = ed.Doc.VirtualWidth / 2
						cyPos = ed.Doc.VirtualHeight / 2
					}
					_, err := ed.PlaceOpenRig("", cxPos, cyPos)
					ed.report(err)
				},
			},
			ui.Button{
				TestID:   "rig-save",
				Text:     "Save Rig",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress:  func() { ed.report(ed.saveRigFromPanel()) },
			},
			ui.Button{
				TestID:   "rig-close",
				Text:     "Close",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress:  func() { ed.report(ed.CloseRig()) },
			},
		},
	}

	// Interactive rig viewport canvas.
	var panLast core.Point
	var panningView bool
	rigCanvas := core.View{
		TestID: "rig-canvas-frame",
		Style: core.Style{
			Width:        core.Pt(340),
			Background:   colors.Surface(),
			BorderWidth:  1,
			BorderColor:  colors.Line(),
			CornerRadius: 4,
			Overflow:     core.Hidden,
		},
		OnWheel: func(ev core.WheelEvent) {
			if dy := float64(ev.Delta.Y); dy != 0 {
				ed.ZoomRigView(float32(1 + dy/400))
			}
		},
		OnPan: func(ev core.PanEvent) {
			switch ev.Phase {
			case core.GestureBegan:
				panLast = ev.Translation
				sx := float32(ev.Position.X - ev.Translation.X)
				sy := float32(ev.Position.Y - ev.Translation.Y)
				if ed.BeginRigDrag(sx, sy) != RigHandleNone {
					panningView = false
					ed.UpdateRigDrag(float32(ev.Position.X), float32(ev.Position.Y))
				} else {
					panningView = true
				}
			case core.GestureChanged:
				if ed.DraggingRig() != RigHandleNone {
					ed.UpdateRigDrag(float32(ev.Position.X), float32(ev.Position.Y))
				} else if panningView {
					ed.PanRigView(float32(ev.Translation.X-panLast.X), float32(ev.Translation.Y-panLast.Y))
				}
				panLast = ev.Translation
			case core.GestureEnded:
				if ed.DraggingRig() != RigHandleNone {
					ed.UpdateRigDrag(float32(ev.Position.X), float32(ev.Position.Y))
					ed.EndRigDrag()
				}
				panningView = false
			default:
				ed.CancelRigDrag()
				panningView = false
			}
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "rig-canvas",
				Label:  "Skeletal Rig Viewport",
				Style:  core.Style{FlexGrow: 1},
				OnLayout: func(b core.Bounds) {
					ed.SetRigCanvasSize(float32(b.Size.Width), float32(b.Size.Height))
				},
				OnPress: func(ev core.PressEvent) {
					if ed.Playing() {
						return
					}
					if hit := ed.HitTestRigBone(float32(ev.Position.X), float32(ev.Position.Y)); hit != "" {
						ed.SelectRigBone(hit)
					}
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					ed.DrawRigViewport(d, size)
				},
			},
		},
	}

	tabBtn := func(id, title, key string) core.Node {
		return ui.ToggleButton{
			TestID: id,
			Text:   title,
			On:     func() bool { return activeTab.Get() == key },
			OnChange: func(on bool) {
				if on {
					activeTab.Set(key)
					if ed.RigOpen() {
						ed.rig.preferAttachOrigin = key == "slots"
					}
				}
			},
		}
	}

	// Bones tab: static inputs and memo-gated bone button list so 60 Hz playback
	// and bone value edits never remount controls or drop focus.
	bonesTab := core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(120)}, Children: []core.Node{ui.Input{
						TestID:      "rig-new-bone-name",
						Label:       "New Bone Name",
						Placeholder: "New bone",
						Disabled:    disabled,
						Value:       newBoneName.Get,
						OnChange:    newBoneName.Set,
					}}},
					ui.Button{
						TestID:   "rig-add-bone",
						Text:     "Add Bone",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							if !ed.RigOpen() {
								return
							}
							name := strings.TrimSpace(newBoneName.Get())
							if name == "" {
								name = fmt.Sprintf("bone_%d", len(ed.rig.data.Bones)+1)
							}
							if err := ed.AddRigBone(name, ed.SelectedRigBone(), 0, -16, 0, 16); err != nil {
								ed.report(err)
								return
							}
							newBoneName.Set("")
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = bonesListKey.Get()
				if !ed.RigOpen() {
					return nil
				}
				var boneButtons []core.Node
				for _, b := range ed.rig.data.Bones {
					bn := b.Name
					boneButtons = append(boneButtons, ui.ToggleButton{
						TestID: "rig-bone-" + bn,
						Text:   bn,
						On: func() bool {
							_ = rev.Get()
							return ed.SelectedRigBone() == bn
						},
						OnChange: func(on bool) {
							if on {
								ed.SelectRigBone(bn)
							}
						},
					})
				}
				return core.View{
					Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
					Children: boneButtons,
				}
			}),
			core.Show{
				When: func() bool {
					_ = rev.Get()
					_, ok := selectedBoneData()
					return ok
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Gap: 4},
						Children: []core.Node{
							ui.HStack{
								Gap: 6,
								Children: []core.Node{
									label("PARENT"),
									core.View{Style: core.Style{Width: core.Pt(110)}, Children: []core.Node{ui.Select[string]{
										TestID:   "rig-bone-parent",
										Label:    "Bone Parent",
										Disabled: disabled,
										OptionsFunc: func() []ui.Option[string] {
											_ = rev.Get()
											opts := []ui.Option[string]{{Value: "", Label: "(Root)"}}
											if !ed.RigOpen() {
												return opts
											}
											selBone := ed.SelectedRigBone()
											for _, b := range ed.rig.data.Bones {
												if b.Name != selBone {
													opts = append(opts, ui.Option[string]{Value: b.Name, Label: b.Name})
												}
											}
											return opts
										},
										Value: func() string {
											_ = rev.Get()
											if bd, ok := selectedBoneData(); ok {
												return bd.Parent
											}
											return ""
										},
										OnChange: func(p string) {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.ReparentRigBone(bd.Name, p, true))
											}
										},
									}}},
									label("X"),
									core.View{Style: core.Style{Width: core.Pt(65)}, Children: []core.Node{ui.NumberInput{
										TestID:   "rig-bone-x",
										Label:    "Bone X",
										Step:     1,
										Disabled: disabled,
										Value: func() float64 {
											_ = rev.Get()
											if bd, ok := selectedBoneData(); ok {
												return float64(bd.X)
											}
											return 0
										},
										OnChange: func(v float64) {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.MoveRigBone(bd.Name, float32(v), bd.Y))
											}
										},
									}}},
									label("Y"),
									core.View{Style: core.Style{Width: core.Pt(65)}, Children: []core.Node{ui.NumberInput{
										TestID:   "rig-bone-y",
										Label:    "Bone Y",
										Step:     1,
										Disabled: disabled,
										Value: func() float64 {
											_ = rev.Get()
											if bd, ok := selectedBoneData(); ok {
												return float64(bd.Y)
											}
											return 0
										},
										OnChange: func(v float64) {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.MoveRigBone(bd.Name, bd.X, float32(v)))
											}
										},
									}}},
									label("ROT"),
									core.View{Style: core.Style{Width: core.Pt(65)}, Children: []core.Node{ui.NumberInput{
										TestID:   "rig-bone-rot",
										Label:    "Bone Rotation",
										Step:     0.1,
										Disabled: disabled,
										Value: func() float64 {
											_ = rev.Get()
											if bd, ok := selectedBoneData(); ok {
												return float64(bd.Rotation)
											}
											return 0
										},
										OnChange: func(v float64) {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.RotateRigBone(bd.Name, float32(v)))
											}
										},
									}}},
									label("LEN"),
									core.View{Style: core.Style{Width: core.Pt(65)}, Children: []core.Node{ui.NumberInput{
										TestID:   "rig-bone-len",
										Label:    "Bone Length",
										Min:      0,
										Step:     1,
										Disabled: disabled,
										Value: func() float64 {
											_ = rev.Get()
											if bd, ok := selectedBoneData(); ok {
												return float64(bd.Length)
											}
											return 0
										},
										OnChange: func(v float64) {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.SetRigBoneLength(bd.Name, float32(v)))
											}
										},
									}}},
									ui.Button{
										TestID:   "rig-bone-delete",
										Text:     "Delete",
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonOutline,
										Disabled: disabled,
										OnPress: func() {
											if bd, ok := selectedBoneData(); ok {
												ed.report(ed.DeleteRigBone(bd.Name))
											}
										},
									},
								},
							},
						},
					}
				},
			},
		},
	}

	// Slots & Skins tab: static inputs and memo-gated slot button list so 60 Hz playback
	// and attachment edits never remount controls or show stale frame state.
	slotsTab := core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(105)}, Children: []core.Node{ui.Input{
						TestID:      "rig-new-slot-name",
						Label:       "New Slot Name",
						Placeholder: "New slot",
						Disabled:    disabled,
						Value:       newSlotName.Get,
						OnChange:    newSlotName.Set,
					}}},
					ui.Button{
						TestID:   "rig-add-slot",
						Text:     "Add Slot",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							if !ed.RigOpen() {
								return
							}
							name := strings.TrimSpace(newSlotName.Get())
							if name == "" {
								name = fmt.Sprintf("slot_%d", len(ed.rig.data.Slots)+1)
							}
							bone := ed.SelectedRigBone()
							if bone == "" && len(ed.rig.data.Bones) > 0 {
								bone = ed.rig.data.Bones[0].Name
							}
							if err := ed.AddRigSlot(name, bone, "", 1); err != nil {
								ed.report(err)
								return
							}
							newSlotName.Set("")
						},
					},
					core.View{Style: core.Style{Width: core.Pt(105)}, Children: []core.Node{ui.Input{
						TestID:      "rig-new-skin-name",
						Label:       "New Skin Name",
						Placeholder: "New skin",
						Disabled:    disabled,
						Value:       newSkinName.Get,
						OnChange:    newSkinName.Set,
					}}},
					ui.Button{
						TestID:   "rig-add-skin",
						Text:     "Add Skin",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							name := strings.TrimSpace(newSkinName.Get())
							if name == "" {
								name = "default"
							}
							if err := ed.AddRigSkin(name); err != nil {
								ed.report(err)
								return
							}
							newSkinName.Set("")
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = slotsListKey.Get()
				if !ed.RigOpen() {
					return nil
				}
				var slotButtons []core.Node
				for _, sl := range ed.rig.data.Slots {
					sn := sl.Name
					slotButtons = append(slotButtons, ui.ToggleButton{
						TestID: "rig-slot-" + sn,
						Text:   sn + " (" + sl.Bone + ")",
						On: func() bool {
							_ = rev.Get()
							return ed.SelectedRigSlot() == sn
						},
						OnChange: func(on bool) {
							if on {
								ed.SelectRigSlot(sn)
							}
						},
					})
				}
				return core.View{
					Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
					Children: slotButtons,
				}
			}),
			core.Show{
				When: func() bool {
					_ = rev.Get()
					_, _, _, ok := selectedSlotAttach()
					return ok
				},
				Then: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 6,
						Children: []core.Node{
							label("FRAME"),
							core.View{Style: core.Style{Width: core.Pt(110)}, Children: []core.Node{ui.Select[string]{
								TestID:     "rig-attach-frame",
								Label:      "Slot Frame",
								Searchable: true,
								Disabled:   disabled,
								OptionsFunc: func() []ui.Option[string] {
									_ = rev.Get()
									frames := ed.RigAtlasFrames()
									opts := make([]ui.Option[string], 0, len(frames)+1)
									opts = append(opts, ui.Option[string]{Value: "", Label: "(Hidden)"})
									for _, f := range frames {
										opts = append(opts, ui.Option[string]{Value: f, Label: f})
									}
									return opts
								},
								Value: func() string {
									_ = rev.Get()
									if _, _, att, ok := selectedSlotAttach(); ok {
										return att.Frame
									}
									return ""
								},
								OnChange: func(f string) {
									if skin, slot, att, ok := selectedSlotAttach(); ok {
										ed.report(ed.AttachRigFrame(skin, slot, f, att.OriginX, att.OriginY))
									}
								},
							}}},
							label("OX"),
							core.View{Style: core.Style{Width: core.Pt(62)}, Children: []core.Node{ui.NumberInput{
								TestID:   "rig-attach-ox",
								Label:    "Origin X",
								Step:     1,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									if _, _, att, ok := selectedSlotAttach(); ok {
										return float64(att.OriginX)
									}
									return 0
								},
								OnChange: func(v float64) {
									if skin, slot, att, ok := selectedSlotAttach(); ok {
										ed.report(ed.SetRigAttachmentOrigin(skin, slot, float32(v), att.OriginY))
									}
								},
							}}},
							label("OY"),
							core.View{Style: core.Style{Width: core.Pt(62)}, Children: []core.Node{ui.NumberInput{
								TestID:   "rig-attach-oy",
								Label:    "Origin Y",
								Step:     1,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									if _, _, att, ok := selectedSlotAttach(); ok {
										return float64(att.OriginY)
									}
									return 0
								},
								OnChange: func(v float64) {
									if skin, slot, att, ok := selectedSlotAttach(); ok {
										ed.report(ed.SetRigAttachmentOrigin(skin, slot, att.OriginX, float32(v)))
									}
								},
							}}},
							ui.Button{
								TestID:   "rig-slot-delete",
								Text:     "Delete Slot",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonOutline,
								Disabled: disabled,
								OnPress: func() {
									if _, slot, _, ok := selectedSlotAttach(); ok {
										ed.report(ed.DeleteRigSlot(slot))
									}
								},
							},
						},
					}
				},
			},
		},
	}

	// Timeline tab: static scrubber & keying controls + memo-gated track summary rows
	// so 60 Hz playback never remounts the scrubber or inputs.
	timelineTab := core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(110)}, Children: []core.Node{ui.Input{
						TestID:      "rig-new-anim-name",
						Label:       "New Animation Name",
						Placeholder: "New anim",
						Disabled:    disabled,
						Value:       newAnimName.Get,
						OnChange:    newAnimName.Set,
					}}},
					ui.Button{
						TestID:   "rig-add-anim",
						Text:     "Add Anim",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							if !ed.RigOpen() {
								return
							}
							name := strings.TrimSpace(newAnimName.Get())
							if name == "" {
								name = fmt.Sprintf("anim_%d", len(ed.rig.data.Animations)+1)
							}
							if err := ed.AddRigAnimation(name, time.Second, true); err != nil {
								ed.report(err)
								return
							}
							newAnimName.Set("")
						},
					},
					core.Text{
						TestID: "rig-time",
						ContentFunc: func() string {
							_ = rev.Get()
							return fmt.Sprintf("%.2f / %.2f s", ed.RigTime().Seconds(), animDurSec())
						},
						Style: core.Style{FontSize: 11, Color: colors.Text()},
					},
				},
			},
			ui.Slider{
				TestID:   "rig-scrub",
				Label:    "Rig Playhead",
				Disabled: disabled,
				MinFunc:  func() float64 { return 0 },
				MaxFunc: func() float64 {
					_ = rev.Get()
					return animDurSec()
				},
				StepFunc: func() float64 { return 1 / 60.0 },
				Value: func() float64 {
					_ = rev.Get()
					return ed.RigTime().Seconds()
				},
				ValueText: func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) + "s" },
				OnChange:  func(v float64) { ed.SeekRig(durationOfSeconds(v)) },
			},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(56)}, Children: []core.Node{ui.NumberInput{
						TestID: "rig-key-tx", Label: "Key TX", Step: 1, Disabled: disabled, Value: keyTransX.Get, OnChange: keyTransX.Set,
					}}},
					core.View{Style: core.Style{Width: core.Pt(56)}, Children: []core.Node{ui.NumberInput{
						TestID: "rig-key-ty", Label: "Key TY", Step: 1, Disabled: disabled, Value: keyTransY.Get, OnChange: keyTransY.Set,
					}}},
					ui.Button{
						TestID:  "rig-key-translate",
						Text:    "Key T",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.RigAnimation() == "" || ed.SelectedRigBone() == ""
						},
						OnPress: func() {
							ed.report(ed.KeyRigBoneTranslate(ed.RigAnimation(), ed.SelectedRigBone(), ed.RigTime(), float32(keyTransX.Get()), float32(keyTransY.Get()), skeletal.CurveLinear, [4]float32{}))
						},
					},
					core.View{Style: core.Style{Width: core.Pt(56)}, Children: []core.Node{ui.NumberInput{
						TestID: "rig-key-rot", Label: "Key Rotate", Step: 0.1, Disabled: disabled, Value: keyRotAngle.Get, OnChange: keyRotAngle.Set,
					}}},
					ui.Button{
						TestID:  "rig-key-rotate",
						Text:    "Key R",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.RigAnimation() == "" || ed.SelectedRigBone() == ""
						},
						OnPress: func() {
							ed.report(ed.KeyRigBoneRotate(ed.RigAnimation(), ed.SelectedRigBone(), ed.RigTime(), float32(keyRotAngle.Get()), skeletal.CurveLinear, [4]float32{}))
						},
					},
					core.View{Style: core.Style{Width: core.Pt(52)}, Children: []core.Node{ui.NumberInput{
						TestID: "rig-key-sx", Label: "Key SX", Step: 0.1, Disabled: disabled, Value: keyScaleX.Get, OnChange: keyScaleX.Set,
					}}},
					core.View{Style: core.Style{Width: core.Pt(52)}, Children: []core.Node{ui.NumberInput{
						TestID: "rig-key-sy", Label: "Key SY", Step: 0.1, Disabled: disabled, Value: keyScaleY.Get, OnChange: keyScaleY.Set,
					}}},
					ui.Button{
						TestID:  "rig-key-scale",
						Text:    "Key S",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.RigAnimation() == "" || ed.SelectedRigBone() == ""
						},
						OnPress: func() {
							ed.report(ed.KeyRigBoneScale(ed.RigAnimation(), ed.SelectedRigBone(), ed.RigTime(), float32(keyScaleX.Get()), float32(keyScaleY.Get()), skeletal.CurveLinear, [4]float32{}))
						},
					},
				},
			},
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(120)}, Children: []core.Node{ui.Input{
						TestID:      "rig-key-attach-frame",
						Label:       "Key Attachment Frame",
						Placeholder: "Slot frame",
						Disabled:    disabled,
						Value:       keyAttachFrame.Get,
						OnChange:    keyAttachFrame.Set,
					}}},
					ui.Button{
						TestID:  "rig-key-attachment",
						Text:    "Key Slot Frame",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.RigAnimation() == "" || ed.SelectedRigSlot() == ""
						},
						OnPress: func() {
							ed.report(ed.KeyRigSlotAttachment(ed.RigAnimation(), ed.SelectedRigSlot(), ed.RigTime(), strings.TrimSpace(keyAttachFrame.Get())))
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = timelineRowsKey.Get()
				if !ed.RigOpen() {
					return nil
				}
				anim := ed.activeRigAnim()
				if anim == nil {
					return nil
				}
				var rows []core.Node
				for _, bn := range slices.Sorted(maps.Keys(anim.Bones)) {
					tl := anim.Bones[bn]
					rows = append(rows, core.Text{
						TestID:  "rig-tl-bone-" + bn,
						Content: fmt.Sprintf("Bone %s: T(%d) R(%d) S(%d)", bn, len(tl.Translate), len(tl.Rotate), len(tl.Scale)),
						Style:   core.Style{FontSize: 11, Color: colors.Text()},
					})
				}
				for _, sn := range slices.Sorted(maps.Keys(anim.Slots)) {
					tl := anim.Slots[sn]
					rows = append(rows, core.Text{
						TestID:  "rig-tl-slot-" + sn,
						Content: fmt.Sprintf("Slot %s: Attach(%d) Alpha(%d)", sn, len(tl.Attachment), len(tl.Alpha)),
						Style:   core.Style{FontSize: 11, Color: colors.Text()},
					})
				}
				return core.View{Style: core.Style{Gap: 2}, Children: rows}
			}),
		},
	}

	rightPane := core.View{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					tabBtn("rig-tab-bones", "Bones", "bones"),
					tabBtn("rig-tab-slots", "Slots & Skins", "slots"),
					tabBtn("rig-tab-timeline", "Timeline", "timeline"),
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						TestID: "rig-summary",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.RigBoneSummary()
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
				},
			},
			ui.ScrollView{
				Style: core.Style{FlexGrow: 1},
				Children: []core.Node{
					core.Show{When: func() bool { return activeTab.Get() == "bones" }, Then: func(*core.Scope) core.Node { return bonesTab }},
					core.Show{When: func() bool { return activeTab.Get() == "slots" }, Then: func(*core.Scope) core.Node { return slotsTab }},
					core.Show{When: func() bool { return activeTab.Get() == "timeline" }, Then: func(*core.Scope) core.Node { return timelineTab }},
				},
			},
		},
	}

	return core.View{
		TestID: "rig-panel",
		Style: dockedStyle(cx, core.Style{
			Height:      core.Pt(320),
			Padding:     core.Symmetric(6, 12),
			Gap:         6,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		}),
		Children: []core.Node{
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.DraggingRig() != RigHandleNone
				},
				Then: func(cx *core.Scope) core.Node {
					cx.OnKey("escape", func() {
						ed.CancelRigDrag()
						panningView = false
					})
					return core.View{
						PointerEvents: core.PointerEventsNone,
					}
				},
			},
			header,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.RigError() != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "rig-error",
						ContentFunc: func() string {
							_ = rev.Get()
							if err := ed.RigError(); err != nil {
								return err.Error()
							}
							return ""
						},
						Style: core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 10},
				Children: []core.Node{
					rigCanvas,
					rightPane,
				},
			},
		},
	}
}
