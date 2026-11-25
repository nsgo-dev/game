package editor

import (
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// roomInspectorView renders the Property Inspector for the currently selected room in the
// open *.rooms.yaml map: ID, Name, world-space rectangle (X, Y, Width, Height), per-room
// transition override (Inherit / Cut / Slide / Fade with Duration, Easing, Fade Color),
// and room-map controls (Snap to Tile Grid, Draw Room, Save Rooms, Close Rooms).
func roomInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	sectionHeader := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		}
	}

	fieldRow := func(label string, control core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						Width:    core.Pt(76),
						FontSize: 12,
						Color:    colors.Text(),
					},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{control},
				},
			},
		}
	}

	currentRoom := func() *rooms.Room {
		_ = rev.Get()
		return ed.SelectedRoom()
	}

	// ID draft committed on Enter / Blur so typing an intermediate duplicate or empty string
	// doesn't fight the user mid-keystroke.
	idDraft := core.NewSignal(cx, "")
	idDirty := core.NewSignal(cx, false)
	idSynced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.SelectedRoomID()
	})
	cx.Effect(func() {
		cur := idSynced.Get()
		cx.Untrack(func() {
			idDraft.Set(cur)
			idDirty.Set(false)
		})
	})
	commitRoomID := func(raw string) {
		idDirty.Set(false)
		curID := ed.SelectedRoomID()
		if curID == "" {
			return
		}
		if err := ed.SetRoomID(curID, raw); err != nil {
			ed.report(err)
			idDraft.Set(ed.SelectedRoomID())
			return
		}
		idDraft.Set(ed.SelectedRoomID())
	}

	// Transition duration & color drafts committed on Enter / Blur.
	// Keyed by selected room ID as well as value so switching between two rooms with
	// identical duration or color resets any uncommitted draft cleanly.
	type roomFieldSync struct {
		id  string
		val string
	}
	durDraft := core.NewSignal(cx, "")
	durDirty := core.NewSignal(cx, false)
	durSynced := core.NewMemo(cx, func() roomFieldSync {
		r := currentRoom()
		if r == nil {
			return roomFieldSync{}
		}
		val := ""
		if r.Transition != nil && r.Transition.Duration > 0 {
			val = time.Duration(r.Transition.Duration).String()
		}
		return roomFieldSync{id: r.ID, val: val}
	})
	cx.Effect(func() {
		cur := durSynced.Get()
		cx.Untrack(func() {
			durDraft.Set(cur.val)
			durDirty.Set(false)
		})
	})

	colorDraft := core.NewSignal(cx, "")
	colorDirty := core.NewSignal(cx, false)
	colorSynced := core.NewMemo(cx, func() roomFieldSync {
		r := currentRoom()
		if r == nil {
			return roomFieldSync{}
		}
		val := ""
		if r.Transition != nil {
			val = r.Transition.Color
		}
		return roomFieldSync{id: r.ID, val: val}
	})
	cx.Effect(func() {
		cur := colorSynced.Get()
		cx.Untrack(func() {
			colorDraft.Set(cur.val)
			colorDirty.Set(false)
		})
	})

	updateRoomTransition := func(mutate func(cur *rooms.Transition) *rooms.Transition) {
		r := ed.SelectedRoom()
		if r == nil {
			return
		}
		next := mutate(cloneTransition(r.Transition))
		ed.report(ed.SetRoomTransition(r.ID, next))
	}

	commitDuration := func(raw string) {
		durDirty.Set(false)
		r := ed.SelectedRoom()
		if r == nil || r.Transition == nil {
			return
		}
		d, err := parseRoomDuration(raw)
		if err != nil {
			ed.report(err)
			durDraft.Set(durSynced.Get().val)
			return
		}
		next := *r.Transition
		next.Duration = d
		if err := ed.SetRoomTransition(r.ID, &next); err != nil {
			ed.report(err)
			durDraft.Set(durSynced.Get().val)
		}
	}

	commitColor := func(raw string) {
		colorDirty.Set(false)
		r := ed.SelectedRoom()
		if r == nil || r.Transition == nil {
			return
		}
		next := *r.Transition
		next.Color = strings.TrimSpace(raw)
		if err := ed.SetRoomTransition(r.ID, &next); err != nil {
			ed.report(err)
			colorDraft.Set(colorSynced.Get().val)
		}
	}

	floatFormat := func(v float64) string {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}

	updateRect := func(mutate func(r *rooms.Room)) {
		r := ed.SelectedRoom()
		if r == nil {
			return
		}
		mutate(r)
		ed.report(ed.SetRoomRect(r.ID, r.X, r.Y, r.Width, r.Height))
	}

	transKindBtn := func(testID, label string, kind rooms.Kind, inherit bool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: disabled,
			On: func() bool {
				r := currentRoom()
				if r == nil {
					return false
				}
				if inherit {
					return r.Transition == nil
				}
				return r.Transition != nil && r.Transition.Kind == kind
			},
			OnChange: func(on bool) {
				if !on {
					return
				}
				updateRoomTransition(func(cur *rooms.Transition) *rooms.Transition {
					if inherit {
						return nil
					}
					next := rooms.Transition{Kind: kind}
					if cur != nil && kind != rooms.KindCut {
						next.Duration = cur.Duration
						next.Easing = cur.Easing
						if kind == rooms.KindFade {
							next.Color = cur.Color
						}
					}
					return &next
				})
			},
		}
	}

	isTimed := func() bool {
		r := currentRoom()
		return r != nil && r.Transition != nil && (r.Transition.Kind == rooms.KindSlide || r.Transition.Kind == rooms.KindFade)
	}
	isFade := func() bool {
		r := currentRoom()
		return r != nil && r.Transition != nil && r.Transition.Kind == rooms.KindFade
	}

	easingOptions := []ui.Option[rooms.Easing]{
		{Value: "", Label: "Default"},
		{Value: rooms.EasingLinear, Label: "linear"},
		{Value: rooms.EasingEase, Label: "ease"},
		{Value: rooms.EasingEaseIn, Label: "easeIn"},
		{Value: rooms.EasingEaseOut, Label: "easeOut"},
		{Value: rooms.EasingEaseInOut, Label: "easeInOut"},
	}

	return core.View{
		TestID: "inspector-room",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							core.Text{
								TestID: "inspector-room-header-id",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.SelectedRoomID()
								},
								MaxLines: 1,
								Style: core.Style{
									FontSize:   13,
									FontWeight: 700,
									Color:      colors.Text(),
								},
							},
							ui.Badge{
								Text:    "room",
								Variant: ui.BadgeNeutral,
							},
						},
					},
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							ui.Button{
								TestID:  "inspector-room-back",
								Text:    "Scene",
								Label:   "Return to Scene Settings",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								OnPress: func() {
									ed.SelectRoom("")
								},
							},
							ui.Button{
								TestID:   "inspector-room-delete",
								Text:     "Delete",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonOutline,
								Disabled: disabled,
								OnPress: func() {
									if id := ed.SelectedRoomID(); id != "" {
										ed.report(ed.DeleteRoom(id))
									}
								},
							},
						},
					},
				},
			},

			sectionHeader("ROOM"),
			fieldRow("ID", ui.Input{
				TestID:   "inspector-room-id",
				Label:    "Room ID",
				Disabled: disabled,
				Value: func() string {
					_ = rev.Get()
					if idDirty.Get() {
						return idDraft.Get()
					}
					return ed.SelectedRoomID()
				},
				OnFocus: func() {
					if !idDirty.Get() {
						idDraft.Set(ed.SelectedRoomID())
					}
				},
				OnChange: func(text string) {
					idDraft.Set(text)
					idDirty.Set(true)
				},
				OnSubmit: func(text string) {
					idDraft.Set(text)
					commitRoomID(text)
				},
				OnBlur: func() {
					if idDirty.Get() {
						commitRoomID(idDraft.Get())
					}
				},
			}),
			fieldRow("Name", ui.Input{
				TestID:      "inspector-room-name",
				Label:       "Room Name",
				Placeholder: "Display name or key",
				Disabled:    disabled,
				Value: func() string {
					if r := currentRoom(); r != nil {
						return r.Name
					}
					return ""
				},
				OnChange: func(text string) {
					if id := ed.SelectedRoomID(); id != "" {
						ed.report(ed.SetRoomName(id, text))
					}
				},
			}),

			sectionHeader("BOUNDS"),
			fieldRow("X", ui.NumberInput{
				TestID:   "inspector-room-x",
				Label:    "Room X",
				Step:     1,
				Format:   floatFormat,
				Disabled: disabled,
				Value: func() float64 {
					if r := currentRoom(); r != nil {
						return float64(r.X)
					}
					return 0
				},
				OnChange: func(v float64) {
					updateRect(func(r *rooms.Room) { r.X = float32(v) })
				},
			}),
			fieldRow("Y", ui.NumberInput{
				TestID:   "inspector-room-y",
				Label:    "Room Y",
				Step:     1,
				Format:   floatFormat,
				Disabled: disabled,
				Value: func() float64 {
					if r := currentRoom(); r != nil {
						return float64(r.Y)
					}
					return 0
				},
				OnChange: func(v float64) {
					updateRect(func(r *rooms.Room) { r.Y = float32(v) })
				},
			}),
			fieldRow("Width", ui.NumberInput{
				TestID:   "inspector-room-width",
				Label:    "Room Width",
				Step:     1,
				Format:   floatFormat,
				Disabled: disabled,
				Value: func() float64 {
					if r := currentRoom(); r != nil {
						return float64(r.Width)
					}
					return 0
				},
				OnChange: func(v float64) {
					updateRect(func(r *rooms.Room) { r.Width = float32(v) })
				},
			}),
			fieldRow("Height", ui.NumberInput{
				TestID:   "inspector-room-height",
				Label:    "Room Height",
				Step:     1,
				Format:   floatFormat,
				Disabled: disabled,
				Value: func() float64 {
					if r := currentRoom(); r != nil {
						return float64(r.Height)
					}
					return 0
				},
				OnChange: func(v float64) {
					updateRect(func(r *rooms.Room) { r.Height = float32(v) })
				},
			}),

			sectionHeader("TRANSITION"),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Wrap:      core.Wrap,
					Gap:       4,
				},
				Children: []core.Node{
					transKindBtn("inspector-room-transition-inherit", "Inherit", "", true),
					transKindBtn("inspector-room-transition-cut", "Cut", rooms.KindCut, false),
					transKindBtn("inspector-room-transition-slide", "Slide", rooms.KindSlide, false),
					transKindBtn("inspector-room-transition-fade", "Fade", rooms.KindFade, false),
				},
			},
			core.Show{
				When: isTimed,
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Gap: 8},
						Children: []core.Node{
							fieldRow("Duration", ui.Input{
								TestID:      "inspector-room-transition-duration",
								Label:       "Transition Duration",
								Placeholder: "e.g. 400ms",
								Disabled:    disabled,
								Value: func() string {
									_ = rev.Get()
									if durDirty.Get() {
										return durDraft.Get()
									}
									return durSynced.Get().val
								},
								OnFocus: func() {
									if !durDirty.Get() {
										durDraft.Set(durSynced.Get().val)
									}
								},
								OnChange: func(text string) {
									durDraft.Set(text)
									durDirty.Set(true)
								},
								OnSubmit: func(text string) {
									durDraft.Set(text)
									commitDuration(text)
								},
								OnBlur: func() {
									if durDirty.Get() {
										commitDuration(durDraft.Get())
									}
								},
							}),
							fieldRow("Easing", ui.Select[rooms.Easing]{
								TestID:   "inspector-room-transition-easing",
								Label:    "Transition Easing",
								Disabled: disabled,
								Style:    core.Style{Width: core.Pct(100)},
								Options:  easingOptions,
								Value: func() rooms.Easing {
									if r := currentRoom(); r != nil && r.Transition != nil {
										return r.Transition.Easing
									}
									return ""
								},
								OnChange: func(e rooms.Easing) {
									updateRoomTransition(func(cur *rooms.Transition) *rooms.Transition {
										if cur == nil {
											return nil
										}
										cur.Easing = e
										return cur
									})
								},
							}),
						},
					}
				},
			},
			core.Show{
				When: isFade,
				Then: func(cx *core.Scope) core.Node {
					return fieldRow("Color", ui.Input{
						TestID:      "inspector-room-transition-color",
						Label:       "Fade Color",
						Placeholder: "#000000",
						Disabled:    disabled,
						Value: func() string {
							_ = rev.Get()
							if colorDirty.Get() {
								return colorDraft.Get()
							}
							return colorSynced.Get().val
						},
						OnFocus: func() {
							if !colorDirty.Get() {
								colorDraft.Set(colorSynced.Get().val)
							}
						},
						OnChange: func(text string) {
							colorDraft.Set(text)
							colorDirty.Set(true)
						},
						OnSubmit: func(text string) {
							colorDraft.Set(text)
							commitColor(text)
						},
						OnBlur: func() {
							if colorDirty.Get() {
								commitColor(colorDraft.Get())
							}
						},
					})
				},
			},

			sectionHeader("ROOM MAP"),
			fieldRow("Grid Snap", ui.Switch{
				TestID:   "inspector-room-snap",
				Label:    "Snap Room Edges to Tile Grid",
				Disabled: disabled,
				Checked: func() bool {
					_ = rev.Get()
					return ed.RoomSnapToGrid()
				},
				OnChange: func(on bool) {
					ed.SetRoomSnapToGrid(on)
				},
			}),
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					ui.ToggleButton{
						TestID:   "inspector-room-draw-mode",
						Text:     "Draw Room",
						Disabled: disabled,
						On: func() bool {
							_ = rev.Get()
							return ed.RoomCreateMode()
						},
						OnChange: func(on bool) {
							ed.SetRoomCreateMode(on)
						},
					},
					ui.Button{
						TestID:   "inspector-room-save",
						Text:     "Save Rooms",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							ed.report(ed.SaveRooms())
						},
					},
				},
			},
		},
	}
}

// roomsSceneSection renders the ROOMS section inside the Scene Settings inspector, letting
// authors create or open a *.rooms.yaml beside the scene, toggle grid snapping and Draw Room
// mode, add/select/save/close rooms, and edit the room map's default transition.
func roomsSceneSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	disabled func() bool,
	sectionHeader func(string) core.Node,
	fieldRow func(string, core.Node) core.Node,
) core.Node {
	colors := ui.ColorsOf(cx)
	hasRooms := func() bool {
		_ = rev.Get()
		return ed.rooms != nil && ed.rooms.doc != nil
	}

	roomListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.rooms == nil || ed.rooms.doc == nil {
			return ""
		}
		var b strings.Builder
		for i, r := range ed.rooms.doc.Rooms {
			if i > 0 {
				b.WriteByte('\x00')
			}
			b.WriteString(r.ID)
			b.WriteByte(':')
			b.WriteString(r.Name)
		}
		return b.String()
	})

	currentMapTransition := func() *rooms.Transition {
		_ = rev.Get()
		if ed.rooms == nil || ed.rooms.doc == nil {
			return nil
		}
		return ed.rooms.doc.Transition
	}

	type mapFieldSync struct {
		target *roomMapState
		val    string
	}
	mapDurDraft := core.NewSignal(cx, "")
	mapDurDirty := core.NewSignal(cx, false)
	mapDurSynced := core.NewMemo(cx, func() mapFieldSync {
		_ = rev.Get()
		val := ""
		if ed.rooms != nil && ed.rooms.doc != nil && ed.rooms.doc.Transition != nil && ed.rooms.doc.Transition.Duration > 0 {
			val = time.Duration(ed.rooms.doc.Transition.Duration).String()
		}
		return mapFieldSync{target: ed.rooms, val: val}
	})
	cx.Effect(func() {
		cur := mapDurSynced.Get()
		cx.Untrack(func() {
			mapDurDraft.Set(cur.val)
			mapDurDirty.Set(false)
		})
	})

	mapColorDraft := core.NewSignal(cx, "")
	mapColorDirty := core.NewSignal(cx, false)
	mapColorSynced := core.NewMemo(cx, func() mapFieldSync {
		_ = rev.Get()
		val := ""
		if ed.rooms != nil && ed.rooms.doc != nil && ed.rooms.doc.Transition != nil {
			val = ed.rooms.doc.Transition.Color
		}
		return mapFieldSync{target: ed.rooms, val: val}
	})
	cx.Effect(func() {
		cur := mapColorSynced.Get()
		cx.Untrack(func() {
			mapColorDraft.Set(cur.val)
			mapColorDirty.Set(false)
		})
	})

	updateMapTransition := func(mutate func(cur *rooms.Transition) *rooms.Transition) {
		if ed.rooms == nil || ed.rooms.doc == nil {
			return
		}
		next := mutate(cloneTransition(ed.rooms.doc.Transition))
		ed.report(ed.SetRoomMapTransition(next))
	}

	commitMapDuration := func(raw string) {
		mapDurDirty.Set(false)
		if ed.rooms == nil || ed.rooms.doc == nil || ed.rooms.doc.Transition == nil {
			return
		}
		d, err := parseRoomDuration(raw)
		if err != nil {
			ed.report(err)
			mapDurDraft.Set(mapDurSynced.Get().val)
			return
		}
		next := *ed.rooms.doc.Transition
		next.Duration = d
		if err := ed.SetRoomMapTransition(&next); err != nil {
			ed.report(err)
			mapDurDraft.Set(mapDurSynced.Get().val)
		}
	}

	commitMapColor := func(raw string) {
		mapColorDirty.Set(false)
		if ed.rooms == nil || ed.rooms.doc == nil || ed.rooms.doc.Transition == nil {
			return
		}
		next := *ed.rooms.doc.Transition
		next.Color = strings.TrimSpace(raw)
		if err := ed.SetRoomMapTransition(&next); err != nil {
			ed.report(err)
			mapColorDraft.Set(mapColorSynced.Get().val)
		}
	}

	mapTransKindBtn := func(testID, label string, kind rooms.Kind, clear bool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: disabled,
			On: func() bool {
				tr := currentMapTransition()
				if clear {
					return tr == nil
				}
				return tr != nil && tr.Kind == kind
			},
			OnChange: func(on bool) {
				if !on {
					return
				}
				updateMapTransition(func(cur *rooms.Transition) *rooms.Transition {
					if clear {
						return nil
					}
					next := rooms.Transition{Kind: kind}
					if cur != nil && kind != rooms.KindCut {
						next.Duration = cur.Duration
						next.Easing = cur.Easing
						if kind == rooms.KindFade {
							next.Color = cur.Color
						}
					}
					return &next
				})
			},
		}
	}

	mapTransTimed := func() bool {
		tr := currentMapTransition()
		return tr != nil && (tr.Kind == rooms.KindSlide || tr.Kind == rooms.KindFade)
	}
	mapTransFade := func() bool {
		tr := currentMapTransition()
		return tr != nil && tr.Kind == rooms.KindFade
	}
	mapEasingOptions := []ui.Option[rooms.Easing]{
		{Value: "", Label: "Default"},
		{Value: rooms.EasingLinear, Label: "linear"},
		{Value: rooms.EasingEase, Label: "ease"},
		{Value: rooms.EasingEaseIn, Label: "easeIn"},
		{Value: rooms.EasingEaseOut, Label: "easeOut"},
		{Value: rooms.EasingEaseInOut, Label: "easeInOut"},
	}

	return core.View{
		TestID: "inspector-rooms-section",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			sectionHeader("ROOMS"),
			core.Show{
				When: hasRooms,
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Gap: 8},
						Children: []core.Node{
							core.Text{
								TestID: "inspector-rooms-label",
								ContentFunc: func() string {
									_ = rev.Get()
									label := ed.RoomsLabel()
									if ed.RoomsDirty() {
										label += "*"
									}
									return label
								},
								MaxLines: 1,
								Style:    core.Style{FontSize: 11, Color: colors.Muted()},
							},
							fieldRow("Grid Snap", ui.Switch{
								TestID:   "inspector-rooms-snap",
								Label:    "Snap Room Edges to Tile Grid",
								Disabled: disabled,
								Checked: func() bool {
									_ = rev.Get()
									return ed.RoomSnapToGrid()
								},
								OnChange: func(on bool) {
									ed.SetRoomSnapToGrid(on)
								},
							}),
							core.View{
								Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
								Children: []core.Node{
									ui.Button{
										TestID:   "inspector-rooms-add",
										Text:     "Add Room",
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonSecondary,
										Disabled: disabled,
										OnPress: func() {
											_, err := ed.AddRoomAtViewCenter()
											ed.report(err)
										},
									},
									ui.ToggleButton{
										TestID:   "inspector-rooms-draw",
										Text:     "Draw Room",
										Disabled: disabled,
										On: func() bool {
											_ = rev.Get()
											return ed.RoomCreateMode()
										},
										OnChange: func(on bool) {
											ed.SetRoomCreateMode(on)
										},
									},
									ui.Button{
										TestID:   "inspector-rooms-save",
										Text:     "Save",
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonSecondary,
										Disabled: disabled,
										OnPress: func() {
											ed.report(ed.SaveRooms())
										},
									},
									ui.Button{
										TestID:   "inspector-rooms-close",
										Text:     "Close",
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonOutline,
										Disabled: disabled,
										OnPress: func() {
											ed.CloseRooms()
										},
									},
								},
							},
							fieldRow("Default", core.View{
								Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
								Children: []core.Node{
									mapTransKindBtn("inspector-rooms-transition-none", "None", "", true),
									mapTransKindBtn("inspector-rooms-transition-cut", "Cut", rooms.KindCut, false),
									mapTransKindBtn("inspector-rooms-transition-slide", "Slide", rooms.KindSlide, false),
									mapTransKindBtn("inspector-rooms-transition-fade", "Fade", rooms.KindFade, false),
								},
							}),
							core.Show{
								When: mapTransTimed,
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										Style: core.Style{Gap: 8},
										Children: []core.Node{
											fieldRow("Duration", ui.Input{
												TestID:      "inspector-rooms-transition-duration",
												Label:       "Default Transition Duration",
												Placeholder: "e.g. 400ms",
												Disabled:    disabled,
												Value: func() string {
													_ = rev.Get()
													if mapDurDirty.Get() {
														return mapDurDraft.Get()
													}
													return mapDurSynced.Get().val
												},
												OnFocus: func() {
													if !mapDurDirty.Get() {
														mapDurDraft.Set(mapDurSynced.Get().val)
													}
												},
												OnChange: func(text string) {
													mapDurDraft.Set(text)
													mapDurDirty.Set(true)
												},
												OnSubmit: func(text string) {
													mapDurDraft.Set(text)
													commitMapDuration(text)
												},
												OnBlur: func() {
													if mapDurDirty.Get() {
														commitMapDuration(mapDurDraft.Get())
													}
												},
											}),
											fieldRow("Easing", ui.Select[rooms.Easing]{
												TestID:   "inspector-rooms-transition-easing",
												Label:    "Default Transition Easing",
												Disabled: disabled,
												Style:    core.Style{Width: core.Pct(100)},
												Options:  mapEasingOptions,
												Value: func() rooms.Easing {
													if tr := currentMapTransition(); tr != nil {
														return tr.Easing
													}
													return ""
												},
												OnChange: func(e rooms.Easing) {
													updateMapTransition(func(cur *rooms.Transition) *rooms.Transition {
														if cur == nil {
															return nil
														}
														cur.Easing = e
														return cur
													})
												},
											}),
										},
									}
								},
							},
							core.Show{
								When: mapTransFade,
								Then: func(cx *core.Scope) core.Node {
									return fieldRow("Color", ui.Input{
										TestID:      "inspector-rooms-transition-color",
										Label:       "Default Fade Color",
										Placeholder: "#000000",
										Disabled:    disabled,
										Value: func() string {
											_ = rev.Get()
											if mapColorDirty.Get() {
												return mapColorDraft.Get()
											}
											return mapColorSynced.Get().val
										},
										OnFocus: func() {
											if !mapColorDirty.Get() {
												mapColorDraft.Set(mapColorSynced.Get().val)
											}
										},
										OnChange: func(text string) {
											mapColorDraft.Set(text)
											mapColorDirty.Set(true)
										},
										OnSubmit: func(text string) {
											mapColorDraft.Set(text)
											commitMapColor(text)
										},
										OnBlur: func() {
											if mapColorDirty.Get() {
												commitMapColor(mapColorDraft.Get())
											}
										},
									})
								},
							},
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = roomListKey.Get()
								m := ed.Rooms()
								if m == nil || len(m.Rooms) == 0 {
									return core.Text{
										TestID:  "inspector-rooms-empty",
										Content: "No rooms — add or draw a room before saving.",
										Style:   core.Style{FontSize: 11, Color: colors.Muted()},
									}
								}
								rows := make([]core.Node, 0, len(m.Rooms))
								for _, r := range m.Rooms {
									rid := r.ID
									title := rid
									if r.Name != "" && r.Name != rid {
										title += " · " + r.Name
									}
									rows = append(rows, ui.Button{
										TestID:  "inspector-rooms-item-" + rid,
										Text:    title,
										Size:    ui.SizeSmall,
										Variant: ui.ButtonGhost,
										OnPress: func() {
											ed.SelectRoom(rid)
										},
									})
								}
								return core.View{Style: core.Style{Gap: 2}, Children: rows}
							}),
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							core.Text{
								Content: "No room map open beside this scene.",
								Style:   core.Style{FontSize: 11, Color: colors.Muted()},
							},
							ui.Button{
								TestID:   "inspector-rooms-new",
								Text:     "New Room Map",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonSecondary,
								Disabled: disabled,
								OnPress: func() {
									defaultPath := ""
									if ed.FilePath != "" || ed.ProjectPath != "" {
										defaultPath = ed.roomDefaultPath()
									}
									ed.report(ed.NewRoomMap(defaultPath))
								},
							},
						},
					}
				},
			},
		},
	}
}
