package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type connFormMode int

const (
	connFormClosed connFormMode = iota
	connFormNew
	connFormEdit
)

// connectionsSection builds the inspector's Connections section for objID: the events declared by objID's registered components, an inline Connect/Edit form with target picking (dropdown, scene tree, or viewport), handler selection, typed registry param fields and a once toggle, and the object's outgoing and incoming connections with jump, edit and remove controls.
func connectionsSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	objID string,
	disabled func() bool,
	sectionHeader func(string) core.Node,
	describedRow func(label, labelID, tip string, control core.Node) core.Node,
) core.Node {
	colors := ui.ColorsOf(cx)
	mode := core.NewSignal(cx, connFormClosed)
	editIndex := core.NewSignal(cx, -1)
	fromID := core.NewSignal(cx, objID)
	eventRef := core.NewSignal(cx, "")
	targetID := core.NewSignal(cx, "")
	handlerRef := core.NewSignal(cx, "")
	onceVal := core.NewSignal(cx, false)
	var draftArgs map[string]any
	var editedConn sceneformat.ConnectionDoc
	argsRev := core.NewSignal(cx, uint64(0))
	setDraftArgs := func(m map[string]any) {
		draftArgs = m
		argsRev.Set(argsRev.GetUntracked() + 1)
	}

	cancelPick := func() {
		if ed.PickingConnectionTarget() {
			ed.CancelConnectionPick()
		}
	}
	cx.Cleanup(func() {
		if ed.connPick != nil && ed.connPick.sourceID == objID {
			ed.connPick = nil
		}
	})

	// Only bind Escape while target picking is active so it does not swallow Escape from viewport drags or other panels.
	pickEscape := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.PickingConnectionTarget()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.PickingConnectionTarget() {
					ed.CancelConnectionPick()
				}
			})
			return nil
		},
	}

	closeForm := func() {
		cancelPick()
		editedConn = sceneformat.ConnectionDoc{}
		mode.Set(connFormClosed)
		editIndex.Set(-1)
	}

	// Close a stale edit form if undo, redo, or another edit removed, replaced, or shifted the connection being edited.
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			if mode.GetUntracked() != connFormEdit {
				return
			}
			idx := editIndex.GetUntracked()
			if idx < 0 || ed.Doc == nil || idx >= len(ed.Doc.Connections) {
				closeForm()
				return
			}
			c := ed.Doc.Connections[idx]
			if (c.From != objID && c.To != objID) || !connectionsEqual(c, editedConn) {
				closeForm()
			}
		})
	})

	openNew := func(ev string) {
		cancelPick()
		editedConn = sceneformat.ConnectionDoc{}
		fromID.Set(objID)
		if ev == "" {
			if events := ed.ObjectEvents(objID); len(events) > 0 {
				ev = events[0].Ref
			}
		}
		eventRef.Set(ev)
		targetID.Set("")
		handlerRef.Set("")
		onceVal.Set(false)
		setDraftArgs(nil)
		editIndex.Set(-1)
		mode.Set(connFormNew)
	}

	openEdit := func(entry ConnectionEntry) {
		cancelPick()
		editedConn = cloneConnectionDoc(entry.Conn)
		fromID.Set(entry.Conn.From)
		eventRef.Set(entry.Conn.Event)
		targetID.Set(entry.Conn.To)
		handlerRef.Set(entry.Conn.Handler)
		onceVal.Set(entry.Conn.Once)
		setDraftArgs(cloneMap(entry.Conn.Args))
		editIndex.Set(entry.Index)
		mode.Set(connFormEdit)
	}

	// pruneArgsFor keeps only keys declared in the resolved registered handler's Params so switching targets or handlers never traps the form with stale hidden args.
	pruneArgsFor := func(tID, hRef string, args map[string]any) map[string]any {
		if len(args) == 0 {
			return nil
		}
		h, ok := ed.resolveObjectHandler(strings.TrimSpace(tID), strings.TrimSpace(hRef))
		if !ok {
			return cloneMap(args)
		}
		var kept map[string]any
		for _, p := range h.Params {
			if v, set := args[p.Name]; set {
				if kept == nil {
					kept = make(map[string]any)
				}
				kept[p.Name] = v
			}
		}
		return kept
	}

	setTarget := func(id string) {
		if id == targetID.GetUntracked() {
			return
		}
		targetID.Set(id)
		if ed.ComponentRegistry() != nil && len(draftArgs) > 0 {
			hRef := handlerRef.GetUntracked()
			if _, ok := ed.resolveObjectHandler(strings.TrimSpace(id), strings.TrimSpace(hRef)); ok {
				setDraftArgs(pruneArgsFor(id, hRef, draftArgs))
			} else {
				setDraftArgs(nil)
			}
		}
	}

	setHandler := func(hRef string) {
		handlerRef.Set(hRef)
		if ed.ComponentRegistry() != nil && len(draftArgs) > 0 {
			tID := targetID.GetUntracked()
			if _, ok := ed.resolveObjectHandler(strings.TrimSpace(tID), strings.TrimSpace(hRef)); ok {
				setDraftArgs(pruneArgsFor(tID, hRef, draftArgs))
			}
		}
	}

	submitForm := func() {
		m := mode.GetUntracked()
		if m == connFormClosed {
			return
		}
		to := strings.TrimSpace(targetID.GetUntracked())
		handler := strings.TrimSpace(handlerRef.GetUntracked())
		args := cloneMap(draftArgs)
		if ed.ComponentRegistry() != nil {
			args = pruneArgsFor(to, handler, args)
		}
		conn := sceneformat.ConnectionDoc{
			From:    strings.TrimSpace(fromID.GetUntracked()),
			Event:   strings.TrimSpace(eventRef.GetUntracked()),
			To:      to,
			Handler: handler,
			Args:    args,
			Once:    onceVal.GetUntracked(),
			// The form has no field for When yet, so an edit keeps the connection's condition.
			When: editedConn.When,
		}
		var err error
		if m == connFormNew {
			_, err = ed.AddConnection(conn)
		} else {
			err = ed.UpdateConnection(editIndex.GetUntracked(), conn)
		}
		if err != nil {
			return
		}
		closeForm()
	}

	// Keep the params control block keyed on handler/target schema so editing param values never remounts inputs or steals focus.
	paramSchema := core.NewMemo(cx, func() string {
		_ = rev.Get()
		m := mode.Get()
		if m == connFormClosed {
			return ""
		}
		tID := targetID.Get()
		hRef := handlerRef.Get()
		var b strings.Builder
		fmt.Fprintf(&b, "%d:%d:%s:%s", m, editIndex.Get(), tID, hRef)
		if h, ok := ed.resolveObjectHandler(tID, hRef); ok {
			b.WriteString("|R:")
			for _, p := range h.Params {
				b.WriteString(p.Name)
				b.WriteByte('=')
				b.WriteString(p.Kind.String())
				b.WriteByte(',')
			}
		} else if ed.ComponentRegistry() == nil {
			b.WriteString("|U:")
			for _, k := range sortedMapKeys(draftArgs) {
				b.WriteString(k)
				b.WriteByte('=')
				b.WriteString(valueKindTag(draftArgs[k]))
				b.WriteByte(',')
			}
		}
		return b.String()
	})

	// Keep the event control keyed on whether source has registered events.
	eventSchema := core.NewMemo(cx, func() string {
		_ = rev.Get()
		m := mode.Get()
		if m == connFormClosed {
			return ""
		}
		src := fromID.Get()
		events := ed.ObjectEvents(src)
		var b strings.Builder
		fmt.Fprintf(&b, "%d:%s:", m, src)
		for _, ev := range events {
			b.WriteString(ev.Ref)
			b.WriteByte(',')
		}
		return b.String()
	})

	headerRow := core.View{
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            6,
		},
		Children: []core.Node{
			sectionHeader(ed.T("inspector.section.connections")),
			ui.Button{
				TestID:   "inspector-conn-new",
				Text:     ed.T("inspector.connect"),
				Label:    ed.T("inspector.connect"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonSecondary,
				Disabled: disabled,
				OnPress:  func() { openNew("") },
			},
		},
	}

	declaredEventsView := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		if ed.ComponentRegistry() == nil {
			return core.Text{
				TestID:  "inspector-conn-no-registry",
				Content: ed.T("inspectorConn.noRegistry"),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		events := ed.ObjectEvents(objID)
		if len(events) == 0 {
			return core.Text{
				TestID:  "inspector-conn-no-events",
				Content: ed.T("inspectorConn.noEvents"),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		rows := make([]core.Node, 0, len(events)+1)
		rows = append(rows, core.Text{
			Content: ed.T("inspectorConn.events"),
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted()},
		})
		for _, ev := range events {
			e := ev
			tip := e.Component + "." + e.Event.Name
			if e.Event.Doc != "" {
				tip += "\n" + e.Event.Doc
			}
			label := withTooltip(core.Text{
				Content: e.Ref,
				Style:   core.Style{FontSize: 12, Color: colors.Text()},
			}, tip)
			connectBtn := ui.Button{
				TestID:   "inspector-conn-event-" + e.Component + "-" + e.Event.Name + "-connect",
				Text:     ed.T("inspectorConn.connect"),
				Label:    ed.T("inspectorConn.connectEvent", l10n.Param("event", e.Ref)),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress:  func() { openNew(e.Ref) },
			}
			rows = append(rows, core.View{
				TestID: "inspector-conn-event-" + e.Component + "-" + e.Event.Name,
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            6,
				},
				Children: []core.Node{label, connectBtn},
			})
		}
		return core.View{Style: core.Style{Gap: 4}, Children: rows}
	})

	formView := core.Show{
		When: func() bool { return mode.Get() != connFormClosed },
		Then: func(cx *core.Scope) core.Node {
			eventControl := core.Dynamic(func(cx *core.Scope) core.Node {
				_ = eventSchema.Get()
				src := fromID.GetUntracked()
				events := ed.ObjectEvents(src)
				if len(events) == 0 {
					return ui.Input{
						TestID:      "inspector-conn-event",
						Label:       ed.T("inspectorConn.event"),
						Placeholder: ed.T("inspectorConn.eventPlaceholder"),
						Disabled:    disabled,
						Value:       eventRef.Get,
						OnChange:    eventRef.Set,
					}
				}
				return ui.Select[string]{
					TestID:      "inspector-conn-event",
					Label:       ed.T("inspectorConn.event"),
					Placeholder: ed.T("inspectorConn.pickEvent"),
					Searchable:  true,
					Disabled:    disabled,
					Style:       core.Style{Width: core.Pct(100)},
					OptionsFunc: func() []ui.Option[string] {
						_ = rev.Get()
						cur := eventRef.Get()
						evs := ed.ObjectEvents(fromID.Get())
						opts := make([]ui.Option[string], 0, len(evs)+1)
						listed := false
						for _, ev := range evs {
							listed = listed || ev.Ref == cur
							opts = append(opts, ui.Option[string]{Value: ev.Ref, Label: ev.Ref, Group: ev.Component})
						}
						if cur != "" && !listed {
							opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
						}
						return opts
					},
					Value:    eventRef.Get,
					OnChange: eventRef.Set,
				}
			})

			targetSelect := ui.Select[string]{
				TestID:      "inspector-conn-target",
				Label:       ed.T("inspectorConn.target"),
				Placeholder: ed.T("inspectorConn.pickTarget"),
				Searchable:  true,
				Disabled:    disabled,
				Style:       core.Style{Width: core.Pct(100)},
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					cur := targetID.Get()
					ids := ed.SceneObjectIDs()
					opts := make([]ui.Option[string], 0, len(ids)+1)
					listed := false
					for _, id := range ids {
						listed = listed || id == cur
						opts = append(opts, ui.Option[string]{Value: id, Label: id})
					}
					if cur != "" && !listed {
						opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
					}
					return opts
				},
				Value:    targetID.Get,
				OnChange: setTarget,
			}

			pickBtn := ui.IconButton{
				TestID: "inspector-conn-pick-target",
				Icon:   lucide.Crosshair,
				Label:  ed.T("inspectorConn.pickTargetLabel"),
				Tooltip: func() string {
					_ = rev.Get()
					if ed.PickingConnectionTarget() {
						return ed.T("inspectorConn.pickTargetActiveTip")
					}
					return ed.T("inspectorConn.pickTargetTip")
				},
				Selected: func() bool {
					_ = rev.Get()
					return ed.PickingConnectionTarget()
				},
				Disabled: disabled,
				OnPress: func() {
					if ed.PickingConnectionTarget() {
						ed.CancelConnectionPick()
						return
					}
					ed.BeginConnectionPick(setTarget)
				},
			}

			handlerInput := ui.Input{
				TestID:      "inspector-conn-handler",
				Label:       ed.T("inspectorConn.handler"),
				Placeholder: ed.T("inspectorConn.handlerPlaceholder"),
				Disabled:    disabled,
				Value:       handlerRef.Get,
				OnChange:    setHandler,
				OnSubmit:    func(string) { submitForm() },
			}

			handlerChips := core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				tID := targetID.Get()
				if tID == "" || ed.ComponentRegistry() == nil {
					return nil
				}
				handlers := ed.ObjectHandlers(tID)
				if len(handlers) == 0 {
					return core.Text{
						TestID:  "inspector-conn-no-handlers",
						Content: ed.T("inspectorConn.noHandlersOn", l10n.Param("id", tID)),
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					}
				}
				btns := make([]core.Node, len(handlers))
				for i, h := range handlers {
					ho := h
					tip := ho.Component + "." + ho.Handler.Name
					if ho.Handler.Doc != "" {
						tip += "\n" + ho.Handler.Doc
					}
					variant := ui.ButtonSecondary
					if handlerRef.Get() == ho.Ref {
						variant = ui.ButtonPrimary
					}
					btn := core.Node(ui.Button{
						TestID:   "inspector-conn-handler-opt-" + ho.Ref,
						Text:     ho.Ref,
						Size:     ui.SizeSmall,
						Variant:  variant,
						Disabled: disabled,
						OnPress:  func() { setHandler(ho.Ref) },
					})
					btns[i] = withTooltip(btn, tip)
				}
				return ui.Wrap{Children: btns}
			})

			paramsView := core.Dynamic(func(cx *core.Scope) core.Node {
				if paramSchema.Get() == "" {
					return nil
				}
				tID := targetID.GetUntracked()
				hRef := handlerRef.GetUntracked()
				setArg := func(name string, val any) {
					next := cloneMap(draftArgs)
					if next == nil {
						next = make(map[string]any)
					}
					next[name] = val
					setDraftArgs(next)
				}
				if h, ok := ed.resolveObjectHandler(tID, hRef); ok {
					if len(h.Params) == 0 {
						return nil
					}
					rows := make([]core.Node, 0, len(h.Params)+1)
					rows = append(rows, core.Text{
						Content: ed.T("inspectorConn.params"),
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted()},
					})
					for _, p := range h.Params {
						param := p
						get := func() any {
							_ = argsRev.Get()
							if draftArgs != nil {
								if v, set := draftArgs[param.Name]; set {
									return v
								}
							}
							return param.Default
						}
						ctrl := registeredFieldControlFor(
							ed,
							"inspector-conn-param-"+param.Name,
							param.Name,
							param,
							disabled,
							get,
							func(val any) { setArg(param.Name, val) },
						)
						rows = append(rows, describedRow(param.Name, "inspector-conn-param-"+param.Name+"-label", localizedFieldTooltip(ed, param), ctrl))
					}
					return core.View{Style: core.Style{Gap: 6}, Children: rows}
				}
				// Without a registry, keep existing args on an edited connection editable by their inferred kind.
				if ed.ComponentRegistry() == nil {
					if len(draftArgs) == 0 {
						return nil
					}
					keys := sortedMapKeys(draftArgs)
					rows := make([]core.Node, 0, len(keys)+1)
					rows = append(rows, core.Text{
						Content: ed.T("inspectorConn.args"),
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted()},
					})
					for _, k := range keys {
						key := k
						sample := draftArgs[key]
						ctrl := typedFieldControl(
							"inspector-conn-param-"+key,
							key,
							sample,
							disabled,
							func() any {
								_ = argsRev.Get()
								if draftArgs != nil {
									return draftArgs[key]
								}
								return nil
							},
							func(val any) { setArg(key, val) },
						)
						rows = append(rows, describedRow(key, "", "", ctrl))
					}
					return core.View{Style: core.Style{Gap: 6}, Children: rows}
				}
				return nil
			})

			formChildren := []core.Node{
				core.Text{
					ContentFunc: func() string {
						if mode.Get() == connFormEdit {
							return ed.T("inspectorConn.editTitle")
						}
						return ed.T("inspectorConn.newTitle")
					},
					Style: core.Style{FontSize: 11, FontWeight: 700, Color: colors.Text()},
				},
				core.Show{
					When: func() bool { return fromID.Get() != objID },
					Then: func(cx *core.Scope) core.Node {
						return describedRow(ed.T("inspectorConn.from"), "", "", core.Text{
							ContentFunc: fromID.Get,
							Style:       core.Style{FontSize: 12, Color: colors.Text()},
						})
					},
				},
				describedRow(ed.T("inspectorConn.event"), "", "", eventControl),
				describedRow(ed.T("inspectorConn.target"), "", "", withTrailing(targetSelect, pickBtn)),
				describedRow(ed.T("inspectorConn.handler"), "", "", handlerInput),
				handlerChips,
				paramsView,
				describedRow(ed.T("inspectorConn.once"), "", ed.T("inspectorConn.onceTip"), ui.Checkbox{
					TestID:   "inspector-conn-once",
					Label:    ed.T("inspectorConn.once"),
					Disabled: disabled,
					Checked:  onceVal.Get,
					OnChange: onceVal.Set,
				}),
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyEnd,
						Gap:            6,
					},
					Children: []core.Node{
						ui.Button{
							TestID:   "inspector-conn-cancel",
							Text:     ed.T("dialog.cancel"),
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonOutline,
							Disabled: disabled,
							OnPress:  closeForm,
						},
						core.Dynamic(func(cx *core.Scope) core.Node {
							text := ed.T("inspector.connect")
							if mode.Get() == connFormEdit {
								text = ed.T("inspector.save")
							}
							return ui.Button{
								TestID:   "inspector-conn-save",
								Text:     text,
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonPrimary,
								Disabled: disabled,
								OnPress:  submitForm,
							}
						}),
					},
				},
			}
			return core.View{
				TestID: "inspector-conn-form",
				Style: core.Style{
					Gap:          6,
					Padding:      core.All(8),
					Background:   colors.Raised(),
					Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
					BorderColor:  colors.Line(),
					CornerRadius: 4,
				},
				Children: formChildren,
			}
		},
	}

	connectionRow := func(prefix string, entry ConnectionEntry, incoming bool) core.Node {
		en := entry
		otherID := en.Conn.To
		summary := fmt.Sprintf("%s -> %s.%s", en.Conn.Event, en.Conn.To, en.Conn.Handler)
		if incoming {
			otherID = en.Conn.From
			summary = fmt.Sprintf("%s.%s -> %s", en.Conn.From, en.Conn.Event, en.Conn.Handler)
		}
		argsText := formatConnectionArgs(en.Conn.Args)
		rowID := fmt.Sprintf("inspector-conn-%s-%d", prefix, en.Index)

		info := []core.Node{
			core.Text{
				Content: summary,
				Style:   core.Style{FontSize: 11, Color: colors.Text()},
			},
		}
		if argsText != "" || en.Conn.Once {
			var meta []core.Node
			if argsText != "" {
				meta = append(meta, core.Text{
					Content: argsText,
					Style:   core.Style{FontSize: 10, Color: colors.Muted()},
				})
			}
			if en.Conn.Once {
				meta = append(meta, ui.Badge{Text: ed.T("inspector.once"), Variant: ui.BadgeNeutral})
			}
			info = append(info, core.View{
				Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, Wrap: core.Wrap},
				Children: meta,
			})
		}

		actions := core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2},
			Children: []core.Node{
				ui.IconButton{
					TestID:   rowID + "-jump",
					Icon:     lucide.ArrowUpRight,
					Label:    ed.T("inspector.jumpTo", l10n.Param("id", otherID)),
					Size:     20,
					IconSize: 12,
					OnPress: func() {
						ed.jumpToConnectionObject(otherID)
					},
				},
				ui.IconButton{
					TestID:   rowID + "-edit",
					Icon:     lucide.Pencil,
					Label:    ed.T("inspector.editConnection"),
					Size:     20,
					IconSize: 12,
					Disabled: disabled,
					OnPress:  func() { openEdit(en) },
				},
				ui.IconButton{
					TestID:   rowID + "-remove",
					Icon:     lucide.X,
					Label:    ed.T("inspector.removeConnection"),
					Size:     20,
					IconSize: 12,
					Disabled: disabled,
					OnPress: func() {
						if mode.GetUntracked() == connFormEdit && editIndex.GetUntracked() == en.Index {
							closeForm()
						}
						_ = ed.RemoveConnection(en.Index)
					},
				},
			},
		}

		return core.View{
			TestID: rowID,
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            4,
				Padding:        core.Symmetric(4, 6),
				Background:     colors.Raised(),
				CornerRadius:   4,
			},
			Children: []core.Node{
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
					Children: info,
				},
				actions,
			},
		}
	}

	listsView := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		outgoing, incoming := ed.ObjectConnections(objID)
		if len(outgoing) == 0 && len(incoming) == 0 {
			return core.Text{
				TestID:  "inspector-conn-empty",
				Content: ed.T("inspector.noConnections"),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		var children []core.Node
		if len(outgoing) > 0 {
			children = append(children, core.Text{
				Content: ed.T("inspector.outgoing", l10n.Param("count", len(outgoing))),
				Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted()},
			})
			for _, en := range outgoing {
				children = append(children, connectionRow("out", en, false))
			}
		}
		if len(incoming) > 0 {
			children = append(children, core.Text{
				Content: ed.T("inspector.incoming", l10n.Param("count", len(incoming))),
				Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Muted()},
			})
			for _, en := range incoming {
				children = append(children, connectionRow("in", en, true))
			}
		}
		return core.View{Style: core.Style{Gap: 4}, Children: children}
	})

	return core.View{
		TestID: "inspector-connections",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			pickEscape,
			headerRow,
			declaredEventsView,
			formView,
			listsView,
		},
	}
}
