package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type remoteTreeNodeKind int

const (
	remoteNodeLayer remoteTreeNodeKind = iota
	remoteNodeSpawnedHeader
	remoteNodeEntity
	remoteNodeTaken
)

type remoteTreeKey struct {
	kind  remoteTreeNodeKind
	id    string
	layer string
}

// remotePlayBarView renders the live Play Mode control bar below the main toolbar while connected to (or connecting to) a running game over MCP.
// It provides the live/paused status pill, Pause/Resume, Step 1, Step N, Time Scale segmented controls, Refresh, and Disconnect.
func remotePlayBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.RemoteConnected() || ed.RemoteConnecting()
		},
		Then: func(cx *core.Scope) core.Node {
			scaleBtn := func(scale float64, label string) core.Node {
				id := fmt.Sprintf("remote-timescale-%g", scale)
				return ui.ToggleButton{
					TestID: id,
					Text:   label,
					Label:  fmt.Sprintf("Set remote time scale to %s", label),
					Disabled: func() bool {
						_ = rev.Get()
						snap := ed.RemoteSnapshot()
						return !snap.Connected || snap.Loop == nil
					},
					On: func() bool {
						_ = rev.Get()
						snap := ed.RemoteSnapshot()
						return snap.Loop != nil && math.Abs(snap.Loop.TimeScale-scale) < 1e-6
					},
					OnChange: func(on bool) {
						if on {
							ed.report(ed.RemoteSetTimeScale(scale))
						}
					},
				}
			}

			return core.View{
				TestID: "remote-play-bar",
				Style: core.Style{
					Direction:      core.Row,
					Wrap:           core.Wrap,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(6, 12),
					Gap:            8,
					Borders:        core.Edges{Bottom: 1},
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					snap := ed.RemoteSnapshot()
					if snap.Loop != nil && snap.Loop.Paused {
						return core.Style{
							Background:  ui.Alpha(colors.Warn(), 0.08),
							BorderColor: ui.Alpha(colors.Warn(), 0.4),
						}
					}
					return core.Style{
						Background:  ui.Alpha(colors.Up(), 0.08),
						BorderColor: ui.Alpha(colors.Up(), 0.4),
					}
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							core.View{
								TestID: "remote-status-pill",
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          6,
									Padding:      core.Symmetric(3, 10),
									CornerRadius: 12,
									Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
								},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									if snap.Loop != nil && snap.Loop.Paused {
										return core.Style{
											Background:  ui.Alpha(colors.Warn(), 0.14),
											BorderColor: ui.Alpha(colors.Warn(), 0.5),
										}
									}
									return core.Style{
										Background:  ui.Alpha(colors.Up(), 0.14),
										BorderColor: ui.Alpha(colors.Up(), 0.5),
									}
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3},
										StyleFunc: func() core.Style {
											_ = rev.Get()
											snap := ed.RemoteSnapshot()
											if snap.Loop != nil && snap.Loop.Paused {
												return core.Style{Background: colors.Warn()}
											}
											return core.Style{Background: colors.Up()}
										},
									},
									core.Text{
										TestID: "remote-status-pill-text",
										ContentFunc: func() string {
											_ = rev.Get()
											snap := ed.RemoteSnapshot()
											if snap.Connecting && !snap.Connected {
												return "Connecting…"
											}
											if snap.Loop == nil {
												return "Connected · live"
											}
											if snap.Loop.Paused {
												return fmt.Sprintf("Paused · frame %d", snap.Loop.Steps)
											}
											if math.Abs(snap.Loop.TimeScale-1) > 1e-6 {
												return fmt.Sprintf("Playing · live · %g×", snap.Loop.TimeScale)
											}
											return "Playing · live"
										},
										Style: core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
									},
								},
							},
						},
					},
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							ui.Button{
								TestID: "remote-pause-resume",
								TextFunc: func() string {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									if snap.Loop != nil && snap.Loop.Paused {
										return "Resume"
									}
									return "Pause"
								},
								LabelFunc: func() string {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									if snap.Loop != nil && snap.Loop.Paused {
										return "Resume running game (" + cx.KeyLabel("mod+shift+p") + ")"
									}
									return "Pause running game (" + cx.KeyLabel("mod+shift+p") + ")"
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									snap := ed.RemoteSnapshot()
									if snap.Loop != nil && snap.Loop.Paused {
										ed.report(ed.RemoteResume())
									} else {
										ed.report(ed.RemotePause())
									}
								},
							},
							ui.Button{
								TestID:  "remote-step-1",
								Text:    "Step",
								Label:   "Step one fixed update (.)",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									_, err := ed.RemoteStep(1)
									ed.report(err)
								},
							},
							ui.NumberInput{
								TestID: "remote-step-count",
								Label:  "Step Count",
								Min:    1,
								Max:    float64(gamemcp.MaxLoopSteps),
								Step:   1,
								Style:  core.Style{Width: core.Pt(86)},
								Disabled: func() bool {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									return !snap.Connected || snap.Loop == nil
								},
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.remoteStepCount())
								},
								OnChange: func(v float64) {
									ed.setRemoteStepCount(int(math.Round(v)))
								},
							},
							ui.Button{
								TestID:  "remote-step-n",
								Text:    "Step N",
								Label:   "Step N fixed updates",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									_, err := ed.RemoteStep(ed.remoteStepCount())
									ed.report(err)
								},
							},
							ui.ToggleGroup{
								TestID: "remote-timescale-group",
								Label:  "Time Scale",
								Children: []core.Node{
									scaleBtn(0.25, "0.25×"),
									scaleBtn(0.5, "0.5×"),
									scaleBtn(1, "1×"),
									scaleBtn(2, "2×"),
								},
							},
						},
					},
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							ui.Button{
								TestID:  "remote-refresh",
								Text:    "Refresh",
								Icon:    lucide.RefreshCw,
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.RemoteConnected()
								},
								OnPress: func() {
									ed.report(ed.RefreshRemote())
								},
							},
							ui.Button{
								TestID:  "remote-disconnect",
								Text:    "Disconnect",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonOutline,
								OnPress: func() {
									ed.report(ed.DisconnectRemote())
								},
							},
						},
					},
				},
			}
		},
	}
}

// remoteSceneTreeView renders the Remote tab beside the local Scene Tree:
// active scenes, layers in draw order, live entity hierarchy (with Spawned at Runtime and Taken scene entities).
func remoteSceneTreeView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	selection := ui.NewSelection(cx, false)
	open := map[remoteTreeKey]bool{}
	openRev := core.NewSignal(cx, 0)

	expanded := func(k remoteTreeKey) bool {
		_ = openRev.Get()
		if v, ok := open[k]; ok {
			return v
		}
		// Expand layers, spawned group, and entities with children by default so live entities are immediately visible.
		return true
	}
	setOpen := func(k remoteTreeKey, on bool) {
		if core.Untracked(cx, func() bool { return expanded(k) }) == on {
			return
		}
		open[k] = on
		openRev.Set(openRev.GetUntracked() + 1)
	}

	classifyEntities := func(snap RemoteSnapshot) (byLayer map[string][]gamemcp.EntitySummary, spawned []gamemcp.EntitySummary, childrenByParent map[string][]gamemcp.EntitySummary, byRef map[string]gamemcp.EntitySummary, layerOrder []string) {
		byLayer = make(map[string][]gamemcp.EntitySummary)
		childrenByParent = make(map[string][]gamemcp.EntitySummary)
		byRef = make(map[string]gamemcp.EntitySummary, len(snap.Entities))
		for _, ent := range snap.Entities {
			byRef[ent.Ref] = ent
		}
		seenLayer := make(map[string]bool)
		for _, l := range snap.Layers {
			if l.Name != "" && !seenLayer[l.Name] {
				seenLayer[l.Name] = true
				layerOrder = append(layerOrder, l.Name)
			}
		}
		for _, ent := range snap.Entities {
			if ent.Parent != "" {
				if _, ok := byRef[ent.Parent]; ok {
					childrenByParent[ent.Parent] = append(childrenByParent[ent.Parent], ent)
					continue
				}
			}
			isSceneEntity := ent.Layer != "" || slices.Contains(ent.Components, "object_ref") || ed.docHasObjectID(ent.Name)
			if !isSceneEntity && len(snap.Layers) > 0 {
				spawned = append(spawned, ent)
				continue
			}
			layerName := ent.Layer
			if layerName == "" {
				layerName = "world"
			}
			if !seenLayer[layerName] {
				seenLayer[layerName] = true
				layerOrder = append(layerOrder, layerName)
			}
			byLayer[layerName] = append(byLayer[layerName], ent)
		}
		for _, t := range snap.Taken {
			layerName := t.Layer
			if layerName == "" {
				layerName = "world"
			}
			if !seenLayer[layerName] {
				seenLayer[layerName] = true
				layerOrder = append(layerOrder, layerName)
			}
		}
		return
	}

	roots := func() []any {
		_ = rev.Get()
		snap := ed.RemoteSnapshot()
		if !snap.Connected {
			return nil
		}
		_, spawned, _, _, layerOrder := classifyEntities(snap)
		out := make([]any, 0, len(layerOrder)+1)
		for _, name := range layerOrder {
			out = append(out, remoteTreeKey{kind: remoteNodeLayer, id: name, layer: name})
		}
		if len(spawned) > 0 {
			out = append(out, remoteTreeKey{kind: remoteNodeSpawnedHeader, id: "__spawned__"})
		}
		return out
	}

	children := func(k remoteTreeKey) []any {
		_ = rev.Get()
		snap := ed.RemoteSnapshot()
		if !snap.Connected {
			return nil
		}
		byLayer, spawned, childrenByParent, _, _ := classifyEntities(snap)
		switch k.kind {
		case remoteNodeLayer:
			ents := byLayer[k.layer]
			var out []any
			for _, ent := range ents {
				out = append(out, remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref, layer: k.layer})
			}
			for _, t := range snap.Taken {
				tLayer := t.Layer
				if tLayer == "" {
					tLayer = "world"
				}
				if tLayer == k.layer {
					out = append(out, remoteTreeKey{kind: remoteNodeTaken, id: t.ObjectID, layer: k.layer})
				}
			}
			return out
		case remoteNodeSpawnedHeader:
			out := make([]any, len(spawned))
			for i, ent := range spawned {
				out[i] = remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref}
			}
			return out
		case remoteNodeEntity:
			kids := childrenByParent[k.id]
			out := make([]any, len(kids))
			for i, ent := range kids {
				out[i] = remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref, layer: k.layer}
			}
			return out
		default:
			return nil
		}
	}

	// Sync tree cursor -> Editor.SelectRemoteEntity.
	cx.Effect(func() {
		cur, _ := selection.Cursor().(remoteTreeKey)
		cx.Untrack(func() {
			if !ed.RemoteConnected() {
				return
			}
			if cur.kind == remoteNodeEntity && cur.id != "" && selection.Has(cur) {
				if ed.SelectedRemoteEntity() != cur.id {
					ed.report(ed.SelectRemoteEntity(cur.id))
				}
			}
		})
	})

	// Sync Editor.SelectedRemoteEntity -> tree selection.
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			sel := ed.SelectedRemoteEntity()
			cur, _ := selection.Cursor().(remoteTreeKey)
			if sel == "" {
				if selection.Len() > 0 {
					selection.Clear()
				}
				if selection.Cursor() != nil {
					selection.SetCursor(nil)
				}
				return
			}
			if cur.kind == remoteNodeEntity && cur.id == sel && selection.Has(cur) {
				return
			}
			snap := ed.RemoteSnapshot()
			layerName := ""
			for _, ent := range snap.Entities {
				if ent.Ref == sel {
					layerName = ent.Layer
					if layerName == "" && (slices.Contains(ent.Components, "object_ref") || ed.docHasObjectID(ent.Name) || len(snap.Layers) == 0) {
						layerName = "world"
					}
					break
				}
			}
			selection.Set(remoteTreeKey{kind: remoteNodeEntity, id: sel, layer: layerName})
		})
	})

	row := func(cx *core.Scope, item ui.TreeItem) core.Node {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			k, _ := item.Key().(remoteTreeKey)
			switch k.kind {
			case remoteNodeLayer:
				return core.View{
					TestID: "remote-layer-row-" + k.layer,
					Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: k.layer,
							Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
						},
						core.Text{
							ContentFunc: func() string {
								_ = rev.Get()
								snap := ed.RemoteSnapshot()
								for _, l := range snap.Layers {
									if l.Name == k.layer {
										if !l.Visible {
											return l.Space + " · hidden"
										}
										return l.Space
									}
								}
								return ""
							},
							Style: core.Style{FontSize: 10, Color: colors.Muted()},
						},
					},
				}
			case remoteNodeSpawnedHeader:
				return core.Text{
					TestID:  "remote-spawned-header",
					Content: "SPAWNED AT RUNTIME",
					Style:   core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
				}
			case remoteNodeTaken:
				return core.View{
					TestID: "remote-taken-row-" + k.id,
					Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, Gap: 4},
					Children: []core.Node{
						core.Text{
							Content: k.id,
							Style:   core.Style{FontSize: 12, Color: colors.Muted(), FlexGrow: 1},
						},
						core.Text{
							ContentFunc: func() string {
								_ = rev.Get()
								snap := ed.RemoteSnapshot()
								for _, t := range snap.Taken {
									if t.ObjectID == k.id {
										return fmt.Sprintf("taken @%d", t.TakenAtStep)
									}
								}
								return "taken"
							},
							Style: core.Style{FontSize: 10, Color: colors.Muted()},
						},
					},
				}
			default:
				ref := k.id
				return core.View{
					TestID: "remote-entity-row-" + ref,
					Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, Gap: 4, MinWidth: core.Pt(0)},
					Children: []core.Node{
						core.Text{
							TestID: "remote-entity-label-" + ref,
							ContentFunc: func() string {
								_ = rev.Get()
								snap := ed.RemoteSnapshot()
								for _, ent := range snap.Entities {
									if ent.Ref == ref {
										if ent.Name != "" {
											return ent.Name
										}
										return ent.Ref
									}
								}
								return ref
							},
							Style: core.Style{FontSize: 12, Color: colors.Text(), FlexGrow: 1, FlexShrink: 1},
						},
						core.Text{
							ContentFunc: func() string {
								_ = rev.Get()
								snap := ed.RemoteSnapshot()
								for _, ent := range snap.Entities {
									if ent.Ref == ref {
										if len(ent.Tags) > 0 {
											return ent.Tags[0]
										}
										return ent.Ref
									}
								}
								return ""
							},
							Style: core.Style{FontSize: 10, Color: colors.Muted()},
						},
					},
				}
			}
		})
	}

	return core.View{
		TestID: "remote-scene-tree-panel",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 6, MinHeight: core.Pt(0)},
		Children: []core.Node{
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return !ed.RemoteConnected()
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "remote-disconnected-hint",
						Style:  core.Style{Gap: 8, Padding: core.Symmetric(8, 4)},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									if snap.Connecting {
										return "Connecting to running game over MCP…"
									}
									if snap.Error != "" {
										return "Remote error: " + snap.Error
									}
									return "Run the game with Automation enabled, or connect to a running game over nsgo mcp."
								},
								Style: core.Style{FontSize: 11, Color: colors.Muted()},
							},
							ui.ToggleButton{
								TestID: "remote-run-automation",
								Text:   "Auto-connect on Run",
								Label:  "Pass -automation to go tool game dev and connect automatically",
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Project == nil || ed.GameRunning()
								},
								On: func() bool {
									_ = rev.Get()
									return ed.RunWithAutomation()
								},
								OnChange: func(on bool) {
									ed.SetRunWithAutomation(on)
								},
							},
							ui.Button{
								TestID:  "remote-connect-btn",
								Text:    "Connect",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Project == nil || ed.RemoteConnecting()
								},
								OnPress: func() {
									go func() {
										if err := ed.ConnectRemoteCommand(context.Background(), ""); err != nil {
											ed.postRemoteStatus("Remote connect failed: " + err.Error())
										}
									}()
								},
							},
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 6, MinHeight: core.Pt(0)},
						Children: []core.Node{
							// Scenes strip
							core.View{
								TestID: "remote-scenes-list",
								Style:  core.Style{Gap: 3},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											snap := ed.RemoteSnapshot()
											if snap.ActiveScene != "" {
												return fmt.Sprintf("SCENE · %s (%d)", snap.ActiveScene, len(snap.Scenes.Scenes))
											}
											return fmt.Sprintf("SCENES · %d", len(snap.Scenes.Scenes))
										},
										Style: core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
									},
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = rev.Get()
										snap := ed.RemoteSnapshot()
										if len(snap.Scenes.Scenes) <= 1 {
											return nil
										}
										var btns []core.Node
										for _, sc := range snap.Scenes.Scenes {
											name := sc.Name
											label := name
											if sc.Current {
												label += " *"
											}
											btns = append(btns, ui.ToggleButton{
												TestID: "remote-scene-btn-" + name,
												Text:   label,
												On: func() bool {
													_ = rev.Get()
													return ed.RemoteSnapshot().ActiveScene == name
												},
												OnChange: func(on bool) {
													if on {
														ed.report(ed.SelectRemoteScene(name))
													}
												},
											})
										}
										return ui.HStack{Gap: 4, Children: btns}
									}),
								},
							},
							// Live entities count header
							core.Text{
								TestID: "remote-entities-header",
								ContentFunc: func() string {
									_ = rev.Get()
									snap := ed.RemoteSnapshot()
									_, spawned, _, _, _ := classifyEntities(snap)
									if len(spawned) > 0 {
										return fmt.Sprintf("LIVE ENTITIES · %d · %d spawned", len(snap.Entities), len(spawned))
									}
									return fmt.Sprintf("LIVE ENTITIES · %d", len(snap.Entities))
								},
								Style: core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
							},
							ui.Tree{
								TestID:   "remote-entity-tree",
								Label:    "Remote Entities",
								Roots:    roots,
								Children: func(key any) []any { k, _ := key.(remoteTreeKey); return children(k) },
								Expanded: func(key any) bool { k, _ := key.(remoteTreeKey); return expanded(k) },
								OnExpand: func(key any, on bool) {
									if k, ok := key.(remoteTreeKey); ok {
										setOpen(k, on)
									}
								},
								RowLabel: func(key any) string {
									k, _ := key.(remoteTreeKey)
									switch k.kind {
									case remoteNodeLayer:
										return "Layer " + k.layer
									case remoteNodeSpawnedHeader:
										return "Spawned at runtime"
									case remoteNodeTaken:
										return "Taken " + k.id
									default:
										return k.id
									}
								},
								ToggleLabel: "Expand",
								Selection:   selection,
								Row:         row,
								Style:       core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
							},
						},
					}
				},
			},
		},
	}
}

func shouldShowRemoteInspector(ed *Editor) bool {
	if ed == nil {
		return false
	}
	if ed.SceneTreeTab() != SceneTreeRemote {
		return false
	}
	return ed.RemoteConnected() || ed.RemoteConnecting() || len(ed.RemoteLiveChanges()) > 0
}

// remoteInspectorSchemaKey computes a stable structural signature for the Remote Inspector so editing values does not remount inputs and steal focus.
func remoteInspectorSchemaKey(ed *Editor) string {
	snap := ed.RemoteSnapshot()
	var b strings.Builder
	b.WriteString("remote:")
	if snap.Connected {
		b.WriteByte('1')
	} else {
		b.WriteByte('0')
	}
	b.WriteByte('|')
	b.WriteString(snap.SelectedRef)
	if snap.Inspected != nil {
		names := make([]string, 0, len(snap.Inspected.Components))
		for k := range snap.Inspected.Components {
			names = append(names, k)
		}
		slices.Sort(names)
		b.WriteString("|C:")
		for _, cn := range names {
			b.WriteString(cn)
			b.WriteByte('{')
			var m map[string]any
			if json.Unmarshal(snap.Inspected.Components[cn], &m) == nil && m != nil {
				for _, fk := range sortedMapKeys(m) {
					b.WriteString(fk)
					b.WriteByte('=')
					b.WriteString(remoteValueKindTag(m[fk]))
					b.WriteByte(',')
				}
			} else {
				var scalar any
				if json.Unmarshal(snap.Inspected.Components[cn], &scalar) == nil {
					b.WriteString(remoteValueKindTag(scalar))
				}
			}
			b.WriteByte('}')
		}
	}
	if len(snap.Tunables) > 0 {
		b.WriteString("|T:")
		for _, t := range snap.Tunables {
			b.WriteString(t.Name)
			b.WriteByte('=')
			b.WriteString(t.Type)
			b.WriteByte(',')
		}
	}
	changes := ed.RemoteLiveChanges()
	fmt.Fprintf(&b, "|L:%d", len(changes))
	for _, c := range changes {
		b.WriteString(c.EntityRef)
		b.WriteByte('.')
		b.WriteString(c.Component)
		b.WriteByte('.')
		b.WriteString(c.Field)
		b.WriteByte(',')
	}
	return b.String()
}

func remoteValueKindTag(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case map[string]any, []any:
		return "readonly"
	default:
		return "readonly"
	}
}

func isRemoteComponentReadOnly(name string) bool {
	return name == "world_transform2d" || name == "object_ref"
}

// remoteInspectorView renders the live Remote Inspector inside InspectorView:
// selected live entity components (with game_component_set editing and "was X" indicators),
// LIVE CHANGES tray (per-row revert, Keep in scene, Discard),
// TUNABLES (game_tunable_set), and CONSOLE (game_console_exec).
func remoteInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	schema := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return remoteInspectorSchemaKey(ed)
	})
	consoleDraft := core.NewSignal(cx, "")

	runConsole := func() {
		cmd := strings.TrimSpace(consoleDraft.GetUntracked())
		if cmd == "" {
			return
		}
		consoleDraft.Set("")
		_, err := ed.RemoteExecConsole(cmd)
		ed.report(err)
	}

	return core.View{
		TestID: "remote-inspector",
		Style:  core.Style{Gap: 10},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = schema.Get()
				return remoteInspectorDynamicBody(cx, ed, rev)
			}),
			// Console section stays outside the dynamic schema block so typing never loses focus on refresh.
			core.Show{
				When: func() bool {
					_ = rev.Get()
					snap := ed.RemoteSnapshot()
					return snap.Connected && snap.HasConsole
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "remote-console-section",
						Style: core.Style{
							Gap:         6,
							Padding:     core.Edges{Top: 8},
							Borders:     core.Edges{Top: 1},
							BorderColor: colors.Line(),
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
								Children: []core.Node{
									core.Text{
										Content: "CONSOLE",
										Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
									},
									ui.Button{
										TestID:  "remote-console-clear",
										Text:    "Clear",
										Size:    ui.SizeSmall,
										Variant: ui.ButtonGhost,
										OnPress: func() { ed.ClearRemoteConsole() },
									},
								},
							},
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
								Children: []core.Node{
									ui.Input{
										TestID:      "remote-console-input",
										Label:       "Console command",
										Placeholder: "set gravity 1200",
										Value:       consoleDraft.Get,
										OnChange:    func(s string) { consoleDraft.Set(s) },
										OnSubmit:    func(s string) { consoleDraft.Set(s); runConsole() },
										Style:       core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
									},
									ui.Button{
										TestID:  "remote-console-run",
										Text:    "Run",
										Size:    ui.SizeSmall,
										Variant: ui.ButtonSecondary,
										OnPress: runConsole,
									},
								},
							},
							core.View{
								TestID: "remote-console-output",
								Style: core.Style{
									Gap:          3,
									Padding:      core.All(6),
									Background:   colors.Surface(),
									CornerRadius: 6,
									Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
									BorderColor:  colors.Line(),
								},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = rev.Get()
										hist := ed.RemoteConsoleHistory()
										if len(hist) == 0 {
											return core.Text{
												Content: "Run a debug console command",
												Style:   core.Style{FontSize: 11, Color: colors.Muted()},
											}
										}
										start := 0
										if len(hist) > 12 {
											start = len(hist) - 12
										}
										var rows []core.Node
										for i := start; i < len(hist); i++ {
											entry := hist[i]
											rows = append(rows, core.Text{
												Content: "> " + entry.Command,
												Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
											})
											if entry.Output != "" {
												rows = append(rows, core.Text{
													Content: entry.Output,
													Style:   core.Style{FontSize: 11, Color: colors.Muted()},
												})
											}
											if !entry.OK && entry.Error != "" {
												rows = append(rows, core.Text{
													Content: entry.Error,
													Style:   core.Style{FontSize: 11, Color: colors.Danger()},
												})
											}
										}
										return core.View{Style: core.Style{Gap: 2}, Children: rows}
									}),
								},
							},
						},
					}
				},
			},
		},
	}
}

func remoteInspectorDynamicBody(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	snap := ed.RemoteSnapshot()

	sectionHeader := func(title, tag string) core.Node {
		children := []core.Node{
			core.Text{
				Content: title,
				Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
			},
		}
		if tag != "" {
			children = append(children, core.Text{
				Content: tag,
				Style:   core.Style{FontSize: 10, Color: colors.Muted()},
			})
		}
		return core.View{
			Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 6},
			Children: children,
		}
	}

	var nodes []core.Node

	if snap.Connected {
		if snap.Inspected == nil {
			nodes = append(nodes, core.Text{
				TestID:  "remote-inspector-empty",
				Content: "Select a live entity in the Remote tab to inspect and edit its components.",
				Style:   core.Style{FontSize: 12, Color: colors.Muted()},
			})
		} else {
			ins := snap.Inspected
			title := ins.Name
			if title == "" {
				title = ins.Ref
			}
			_, objectID := remoteEntityIdentity(ins, ins.Ref)
			isSpawned := objectID == "" && !ed.docHasObjectID(ins.Name)
			badgeText := "live"
			if isSpawned {
				badgeText = "spawned"
			}
			sub := ins.Ref
			if ins.Layer != "" {
				sub = ins.Layer + " · " + ins.Ref
			}
			if isSpawned {
				sub = "Spawned at runtime · " + ins.Ref
			}
			nodes = append(nodes, core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 6},
				Children: []core.Node{
					core.View{
						Style: core.Style{Gap: 2, FlexGrow: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							core.Text{
								TestID:  "remote-inspector-entity-name",
								Content: title,
								Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
							},
							core.Text{
								Content: sub,
								Style:   core.Style{FontSize: 11, Color: colors.Muted()},
							},
						},
					},
					ui.Badge{
						Text:    badgeText,
						Variant: ui.BadgeNeutral,
					},
				},
			})

			compNames := make([]string, 0, len(ins.Components))
			for cn := range ins.Components {
				compNames = append(compNames, cn)
			}
			slices.Sort(compNames)

			for _, cn := range compNames {
				compName := cn
				raw := ins.Components[compName]
				readOnlyComp := isRemoteComponentReadOnly(compName)
				tag := "game_component_set"
				if readOnlyComp {
					tag = "live · read only"
				}
				nodes = append(nodes, sectionHeader(strings.ToUpper(compName), tag))

				var fieldMap map[string]any
				if err := json.Unmarshal(raw, &fieldMap); err == nil && fieldMap != nil {
					for _, fk := range sortedMapKeys(fieldMap) {
						fieldKey := fk
						sample := fieldMap[fieldKey]
						nodes = append(nodes, remoteFieldRow(cx, ed, rev, ins.Ref, compName, fieldKey, sample, readOnlyComp))
					}
				} else {
					var scalar any
					_ = json.Unmarshal(raw, &scalar)
					nodes = append(nodes, remoteScalarComponentRow(cx, ed, rev, ins.Ref, compName, scalar, readOnlyComp))
				}
			}

			if isSpawned {
				nodes = append(nodes, core.Text{
					Content: "Spawned while running; not in the open scene file.",
					Style:   core.Style{FontSize: 11, Color: colors.Muted()},
				})
			}
		}
	}

	// LIVE CHANGES tray (kept even after stopping/disconnecting so the author can Keep in scene or Discard).
	nodes = append(nodes, remoteLiveChangesSection(cx, ed, rev))

	// TUNABLES section.
	if snap.Connected && len(snap.Tunables) > 0 {
		nodes = append(nodes, remoteTunablesSection(cx, ed, rev, snap.Tunables))
	}

	return core.View{
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func remoteFieldRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], ref, compName, fieldKey string, sample any, readOnly bool) core.Node {
	colors := ui.ColorsOf(cx)
	testID := "remote-comp-" + compName + "-" + fieldKey

	getVal := func() any {
		_ = rev.Get()
		snap := ed.RemoteSnapshot()
		if snap.Inspected == nil || snap.Inspected.Ref != ref {
			return sample
		}
		return extractComponentFieldValue(snap.Inspected, compName, fieldKey)
	}

	wasText := func() string {
		_ = rev.Get()
		for _, c := range ed.RemoteLiveChanges() {
			if c.EntityRef == ref && c.Component == compName && c.Field == fieldKey {
				return "was " + formatRemoteValue(c.From)
			}
		}
		return ""
	}

	var ctrl core.Node
	if readOnly {
		ctrl = core.Text{
			TestID: testID,
			ContentFunc: func() string {
				return formatRemoteValue(getVal())
			},
			Style: core.Style{FontSize: 11, Color: colors.Text()},
		}
	} else {
		switch sample.(type) {
		case bool:
			ctrl = ui.Checkbox{
				TestID: testID,
				Label:  compName + "." + fieldKey,
				Checked: func() bool {
					b, _ := getVal().(bool)
					return b
				},
				OnChange: func(checked bool) {
					ed.report(ed.SetRemoteComponentField(ref, compName, fieldKey, checked))
				},
			}
		case float64, int, int64:
			step := 1.0
			if f, ok := asFloat64(sample); ok && math.Trunc(f) != f {
				step = 0.1
			}
			// Check component registry for step/min/max hints when available.
			var minVal, maxVal float64
			isIntField := false
			if t, ok := ed.lookupComponentType(compName); ok {
				for _, f := range t.Fields {
					if f.Name == fieldKey {
						if f.Kind == sceneformat.FieldInt {
							isIntField = true
						}
						if f.Step > 0 {
							step = f.Step
						}
						if f.Min != nil && f.Max != nil && *f.Max > *f.Min {
							minVal, maxVal = *f.Min, *f.Max
						}
						break
					}
				}
			}
			ctrl = ui.NumberInput{
				TestID: testID,
				Label:  compName + "." + fieldKey,
				Min:    minVal,
				Max:    maxVal,
				Step:   step,
				Format: func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Value: func() float64 {
					f, _ := asFloat64(getVal())
					return f
				},
				OnChange: func(v float64) {
					if isIntField {
						ed.report(ed.SetRemoteComponentField(ref, compName, fieldKey, int(math.Round(v))))
						return
					}
					ed.report(ed.SetRemoteComponentField(ref, compName, fieldKey, v))
				},
			}
		case string:
			ctrl = ui.Input{
				TestID: testID,
				Label:  compName + "." + fieldKey,
				Value: func() string {
					if v := getVal(); v != nil {
						return fmt.Sprint(v)
					}
					return ""
				},
				OnChange: func(text string) {
					ed.report(ed.SetRemoteComponentField(ref, compName, fieldKey, text))
				},
			}
		default:
			ctrl = core.Text{
				TestID: testID,
				ContentFunc: func() string {
					return formatRemoteValue(getVal())
				},
				Style: core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
	}

	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
		Children: []core.Node{
			core.View{
				Style: core.Style{Width: core.Pt(88), Gap: 1},
				Children: []core.Node{
					core.Text{
						Content: fieldKey,
						Style:   core.Style{FontSize: 11, Color: colors.Text()},
					},
					core.Text{
						ContentFunc: wasText,
						Style:       core.Style{FontSize: 10, Color: colors.Warn()},
					},
				},
			},
			core.View{
				Style:    core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{ctrl},
			},
		},
	}
}

func remoteScalarComponentRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], ref, compName string, sample any, readOnly bool) core.Node {
	colors := ui.ColorsOf(cx)
	testID := "remote-comp-" + compName
	getVal := func() any {
		_ = rev.Get()
		snap := ed.RemoteSnapshot()
		if snap.Inspected == nil || snap.Inspected.Ref != ref {
			return sample
		}
		var v any
		_ = json.Unmarshal(snap.Inspected.Components[compName], &v)
		return v
	}
	if readOnly {
		return core.Text{
			TestID:      testID,
			ContentFunc: func() string { return formatRemoteValue(getVal()) },
			Style:       core.Style{FontSize: 11, Color: colors.Text()},
		}
	}
	switch sample.(type) {
	case bool:
		return ui.Checkbox{
			TestID:  testID,
			Label:   compName,
			Checked: func() bool { b, _ := getVal().(bool); return b },
			OnChange: func(checked bool) {
				ed.report(ed.SetRemoteComponentValue(ref, compName, checked, true))
			},
		}
	case float64, int, int64:
		return ui.NumberInput{
			TestID: testID,
			Label:  compName,
			Step:   1,
			Value:  func() float64 { f, _ := asFloat64(getVal()); return f },
			OnChange: func(v float64) {
				ed.report(ed.SetRemoteComponentValue(ref, compName, v, true))
			},
		}
	case string:
		return ui.Input{
			TestID: testID,
			Label:  compName,
			Value:  func() string { return fmt.Sprint(getVal()) },
			OnChange: func(text string) {
				ed.report(ed.SetRemoteComponentValue(ref, compName, text, true))
			},
		}
	default:
		return core.Text{
			TestID:      testID,
			ContentFunc: func() string { return formatRemoteValue(getVal()) },
			Style:       core.Style{FontSize: 11, Color: colors.Muted()},
		}
	}
}

func remoteLiveChangesSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	changes := ed.RemoteLiveChanges()

	children := []core.Node{
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 6},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("LIVE CHANGES (%d)", len(changes)),
					Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
				},
				core.Text{
					Content: "applied without restart",
					Style:   core.Style{FontSize: 10, Color: colors.Muted()},
				},
			},
		},
	}

	if len(changes) == 0 {
		children = append(children, core.Text{
			TestID:  "remote-live-changes-empty",
			Content: "Change a value in the inspector and the running game picks it up at once.",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		})
	} else {
		for i, c := range changes {
			idx := i
			path := c.Component
			if c.Field != "" {
				path += "." + c.Field
			}
			children = append(children, core.View{
				TestID: fmt.Sprintf("remote-change-%d", idx),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          6,
					Padding:      core.Symmetric(4, 6),
					CornerRadius: 6,
					Background:   colors.Surface(),
					Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
					BorderColor:  colors.Line(),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Gap: 1},
						Children: []core.Node{
							core.Text{
								Content: fmt.Sprintf("%s · %s", c.EntityName, path),
								Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
							},
							core.Text{
								Content: fmt.Sprintf("%s → %s", formatRemoteValue(c.From), formatRemoteValue(c.To)),
								Style:   core.Style{FontSize: 10, Color: colors.Warn()},
							},
						},
					},
					ui.Button{
						TestID:  fmt.Sprintf("remote-change-revert-%d", idx),
						Text:    "Undo",
						Label:   "Put back the scene value",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						OnPress: func() {
							ed.report(ed.RevertRemoteLiveChange(idx))
						},
					},
				},
			})
		}
		children = append(children, core.View{
			Style: core.Style{Direction: core.Row, Gap: 6},
			Children: []core.Node{
				ui.Button{
					TestID:  "remote-changes-keep",
					Text:    "Keep in scene",
					Size:    ui.SizeSmall,
					Variant: ui.ButtonPrimary,
					Style:   core.Style{FlexGrow: 1},
					OnPress: func() {
						_, err := ed.KeepRemoteLiveChangesInScene()
						ed.report(err)
					},
				},
				ui.Button{
					TestID:  "remote-changes-discard",
					Text:    "Discard",
					Size:    ui.SizeSmall,
					Variant: ui.ButtonOutline,
					OnPress: func() {
						ed.report(ed.DiscardRemoteLiveChanges())
					},
				},
			},
		})
		children = append(children, core.Text{
			Content: "Stopping keeps this list. Nothing is written until you keep it.",
			Style:   core.Style{FontSize: 10, Color: colors.Muted()},
		})
	}

	return core.View{
		TestID: "remote-live-changes",
		Style: core.Style{
			Gap:         6,
			Padding:     core.Edges{Top: 8},
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: children,
	}
}

func remoteTunablesSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], tunables []gamemcp.TunableResult) core.Node {
	colors := ui.ColorsOf(cx)
	rows := []core.Node{
		core.Text{
			Content: fmt.Sprintf("TUNABLES (%d)", len(tunables)),
			Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
		},
	}

	for _, t := range tunables {
		tun := t
		testID := "remote-tunable-" + tun.Name
		getVal := func() any {
			_ = rev.Get()
			snap := ed.RemoteSnapshot()
			for _, cur := range snap.Tunables {
				if cur.Name == tun.Name {
					return cur.Value
				}
			}
			return tun.Value
		}

		var ctrl core.Node
		switch tun.Type {
		case "bool":
			ctrl = ui.Checkbox{
				TestID: testID,
				Label:  tun.Name,
				Checked: func() bool {
					b, _ := getVal().(bool)
					return b
				},
				OnChange: func(checked bool) {
					_, err := ed.RemoteSetTunable(tun.Name, checked, false)
					ed.report(err)
				},
			}
		case "int":
			lo, hi := tunableBounds(tun)
			ctrl = ui.NumberInput{
				TestID: testID,
				Label:  tun.Name,
				Min:    lo,
				Max:    hi,
				Step:   1,
				Value: func() float64 {
					f, _ := asFloat64(getVal())
					return f
				},
				OnChange: func(v float64) {
					_, err := ed.RemoteSetTunable(tun.Name, int(math.Round(v)), true)
					ed.report(err)
				},
			}
		case "float":
			lo, hi := tunableBounds(tun)
			ctrl = ui.NumberInput{
				TestID: testID,
				Label:  tun.Name,
				Min:    lo,
				Max:    hi,
				Step:   0.1,
				Format: func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Value: func() float64 {
					f, _ := asFloat64(getVal())
					return f
				},
				OnChange: func(v float64) {
					_, err := ed.RemoteSetTunable(tun.Name, v, true)
					ed.report(err)
				},
			}
		default:
			ctrl = ui.Input{
				TestID: testID,
				Label:  tun.Name,
				Value: func() string {
					if v := getVal(); v != nil {
						return fmt.Sprint(v)
					}
					return ""
				},
				OnChange: func(text string) {
					_, err := ed.RemoteSetTunable(tun.Name, text, false)
					ed.report(err)
				},
			}
		}

		rows = append(rows, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
			Children: []core.Node{
				core.Text{
					Content: tun.Name,
					Style:   core.Style{Width: core.Pt(92), FontSize: 11, Color: colors.Text()},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{ctrl},
				},
			},
		})
	}

	return core.View{
		TestID: "remote-tunables-section",
		Style: core.Style{
			Gap:         6,
			Padding:     core.Edges{Top: 8},
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: rows,
	}
}

func tunableBounds(t gamemcp.TunableResult) (lo, hi float64) {
	minV, okMin := asFloat64(t.Min)
	maxV, okMax := asFloat64(t.Max)
	if okMin && okMax && maxV > minV {
		return minV, maxV
	}
	return 0, 0
}

func formatRemoteValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	default:
		b, err := json.Marshal(x)
		if err == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	}
}

func remoteStatusView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.RemoteConnected()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID: "status-remote",
				ContentFunc: func() string {
					_ = rev.Get()
					snap := ed.RemoteSnapshot()
					var parts []string
					if snap.Loop != nil {
						if snap.Loop.Paused {
							parts = append(parts, "paused")
						} else if snap.HasFPS {
							parts = append(parts, fmt.Sprintf("%.0f fps", snap.FPS))
						} else {
							parts = append(parts, "live")
						}
						parts = append(parts, fmt.Sprintf("frame %d", snap.Loop.Steps))
						parts = append(parts, fmt.Sprintf("%.2fs", snap.Loop.GameTimeSeconds))
						parts = append(parts, fmt.Sprintf("%g×", snap.Loop.TimeScale))
					} else {
						parts = append(parts, "remote")
					}
					parts = append(parts, fmt.Sprintf("%d entities", len(snap.Entities)))
					return "Remote: " + strings.Join(parts, " · ")
				},
				Style: core.Style{FontSize: 11, Color: colors.Muted()},
			}
		},
	}
}
