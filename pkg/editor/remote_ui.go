package editor

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	gamemcp "github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func newRemoteSnapMemo(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) (*core.Memo[uint64], *core.Memo[*RemoteSnapshot]) {
	tick := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.remoteSeq()
	})
	snap := core.NewMemo(cx, func() *RemoteSnapshot {
		_ = tick.Get()
		return ed.remoteSnapshotShared()
	})
	return tick, snap
}

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
	remoteTick, snapMemo := newRemoteSnapMemo(cx, ed, rev)
	return core.Show{
		When: func() bool {
			snap := snapMemo.Get()
			return snap.Connected || snap.Connecting
		},
		Then: func(cx *core.Scope) core.Node {
			scaleBtn := func(scale float64, label string) core.Node {
				id := fmt.Sprintf("remote-timescale-%g", scale)
				return ui.ToggleButton{
					TestID: id,
					Text:   label,
					Label:  ed.T("remote.setTimeScaleLabel", l10n.Param("scale", label)),
					Disabled: func() bool {
						snap := snapMemo.Get()
						return !snap.Connected || snap.Loop == nil
					},
					On: func() bool {
						snap := snapMemo.Get()
						return snap.Loop != nil && math.Abs(snap.Loop.TimeScale-scale) < 1e-6
					},
					OnChange: func(on bool) {
						if on {
							ed.dispatchRemoteAction(cx, func() error {
								return ed.RemoteSetTimeScale(scale)
							})
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
					snap := snapMemo.Get()
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
									snap := snapMemo.Get()
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
											snap := snapMemo.Get()
											if snap.Loop != nil && snap.Loop.Paused {
												return core.Style{Background: colors.Warn()}
											}
											return core.Style{Background: colors.Up()}
										},
									},
									core.Text{
										TestID: "remote-status-pill-text",
										ContentFunc: func() string {
											snap := snapMemo.Get()
											if snap.Connecting && !snap.Connected {
												return ed.T("remote.status.connecting")
											}
											if snap.Loop == nil {
												return ed.T("remote.status.connectedLive")
											}
											if snap.Loop.Paused {
												return ed.T("remote.status.pausedFrame", l10n.Param("frame", snap.Loop.Steps))
											}
											if math.Abs(snap.Loop.TimeScale-1) > 1e-6 {
												return ed.T("remote.status.playingLiveScale", l10n.Param("scale", fmt.Sprintf("%g", snap.Loop.TimeScale)))
											}
											return ed.T("remote.status.playingLive")
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
									snap := snapMemo.Get()
									if snap.Loop != nil && snap.Loop.Paused {
										return ed.T("remote.resume")
									}
									return ed.T("remote.pause")
								},
								LabelFunc: func() string {
									snap := snapMemo.Get()
									if snap.Loop != nil && snap.Loop.Paused {
										return ed.T("remote.resumeLabel", l10n.Param("key", cx.KeyLabel("mod+shift+p")))
									}
									return ed.T("remote.pauseLabel", l10n.Param("key", cx.KeyLabel("mod+shift+p")))
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									snap := snapMemo.Get()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									snap := ed.remoteSnapshotShared()
									paused := snap.Loop != nil && snap.Loop.Paused
									ed.dispatchRemoteAction(cx, func() error {
										if paused {
											return ed.RemoteResume()
										}
										return ed.RemotePause()
									})
								},
							},
							ui.Button{
								TestID:  "remote-step-1",
								Text:    ed.T("remote.step"),
								Label:   ed.T("remote.stepLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									snap := snapMemo.Get()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									ed.dispatchRemoteAction(cx, func() error {
										_, err := ed.RemoteStep(1)
										return err
									})
								},
							},
							ui.NumberInput{
								TestID: "remote-step-count",
								Label:  ed.T("remote.stepCountLabel"),
								Min:    1,
								Max:    float64(gamemcp.MaxLoopSteps),
								Step:   1,
								Style:  core.Style{Width: core.Pt(86)},
								Disabled: func() bool {
									snap := snapMemo.Get()
									return !snap.Connected || snap.Loop == nil
								},
								Value: func() float64 {
									_ = remoteTick.Get()
									return float64(ed.remoteStepCount())
								},
								OnChange: func(v float64) {
									ed.setRemoteStepCount(int(math.Round(v)))
								},
							},
							ui.Button{
								TestID:  "remote-step-n",
								Text:    ed.T("remote.stepN"),
								Label:   ed.T("remote.stepNLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									snap := snapMemo.Get()
									return !snap.Connected || snap.Loop == nil
								},
								OnPress: func() {
									n := ed.remoteStepCount()
									ed.dispatchRemoteAction(cx, func() error {
										_, err := ed.RemoteStep(n)
										return err
									})
								},
							},
							ui.ToggleGroup{
								TestID: "remote-timescale-group",
								Label:  ed.T("remote.timeScaleLabel"),
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
								TestID:  "remote-mark-btn",
								Icon:    lucide.Flag,
								Label:   ed.T("remote.markLabel", l10n.Param("key", cx.KeyLabel("alt+f"))),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								TextFunc: func() string {
									_ = rev.Get()
									if n := len(ed.PlayMarks()); n > 0 {
										return ed.T("remote.markCount", l10n.Param("count", n))
									}
									return ed.T("remote.mark")
								},
								Disabled: func() bool {
									return !snapMemo.Get().Connected
								},
								OnPress: func() {
									_, err := ed.AddPlayMark()
									ed.report(err)
								},
							},
							ui.Button{
								TestID:  "remote-ask-btn",
								Icon:    lucide.Sparkles,
								Text:    ed.T("remote.askButton"),
								Label:   ed.T("remote.askButtonLabel", l10n.Param("key", cx.KeyLabel("mod+l"))),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									return !snapMemo.Get().Connected
								},
								OnPress: func() {
									ed.report(ed.OpenAskFrozenFrame())
								},
							},
							ui.Button{
								TestID:  "remote-refresh",
								Text:    ed.T("remote.refresh"),
								Icon:    lucide.RefreshCw,
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									return !snapMemo.Get().Connected
								},
								OnPress: func() {
									ed.dispatchRemoteAction(cx, func() error {
										return ed.RefreshRemote()
									})
								},
							},
							ui.Button{
								TestID:  "remote-disconnect",
								Text:    ed.T("remote.disconnect"),
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

type remoteTreeIndex struct {
	snap             *RemoteSnapshot
	byLayer          map[string][]gamemcp.EntitySummary
	spawned          []gamemcp.EntitySummary
	childrenByParent map[string][]gamemcp.EntitySummary
	byRef            map[string]gamemcp.EntitySummary
	layerByName      map[string]gamemcp.LayerInfo
	takenByLayer     map[string][]RemoteTakenEntity
	takenByID        map[string]RemoteTakenEntity
	entityLayer      map[string]string
	layerOrder       []string
}

func buildRemoteTreeIndex(snap *RemoteSnapshot, docIDs map[string]struct{}) *remoteTreeIndex {
	idx := &remoteTreeIndex{
		snap:             snap,
		byLayer:          make(map[string][]gamemcp.EntitySummary),
		childrenByParent: make(map[string][]gamemcp.EntitySummary),
		byRef:            make(map[string]gamemcp.EntitySummary, len(snap.Entities)),
		layerByName:      make(map[string]gamemcp.LayerInfo, len(snap.Layers)),
		takenByLayer:     make(map[string][]RemoteTakenEntity),
		takenByID:        make(map[string]RemoteTakenEntity, len(snap.Taken)),
		entityLayer:      make(map[string]string, len(snap.Entities)),
	}
	if !snap.Connected {
		return idx
	}
	for _, ent := range snap.Entities {
		idx.byRef[ent.Ref] = ent
	}
	seenLayer := make(map[string]bool, len(snap.Layers))
	for _, l := range snap.Layers {
		if l.Name != "" {
			idx.layerByName[l.Name] = l
			if !seenLayer[l.Name] {
				seenLayer[l.Name] = true
				idx.layerOrder = append(idx.layerOrder, l.Name)
			}
		}
	}
	for _, ent := range snap.Entities {
		if ent.Parent != "" {
			if _, ok := idx.byRef[ent.Parent]; ok {
				idx.childrenByParent[ent.Parent] = append(idx.childrenByParent[ent.Parent], ent)
				continue
			}
		}
		_, inDoc := docIDs[ent.Name]
		isSceneEntity := ent.Layer != "" || slices.Contains(ent.Components, "object_ref") || inDoc
		if !isSceneEntity && len(snap.Layers) > 0 {
			idx.spawned = append(idx.spawned, ent)
			idx.entityLayer[ent.Ref] = ""
			continue
		}
		layerName := ent.Layer
		if layerName == "" {
			layerName = "world"
		}
		if !seenLayer[layerName] {
			seenLayer[layerName] = true
			idx.layerOrder = append(idx.layerOrder, layerName)
		}
		idx.byLayer[layerName] = append(idx.byLayer[layerName], ent)
		idx.entityLayer[ent.Ref] = layerName
	}
	// Propagate root ancestor layer down to child entities so tree key lookup is O(1).
	var assignChildLayers func(parentRef, layerName string)
	assignChildLayers = func(parentRef, layerName string) {
		for _, child := range idx.childrenByParent[parentRef] {
			idx.entityLayer[child.Ref] = layerName
			assignChildLayers(child.Ref, layerName)
		}
	}
	for layerName, roots := range idx.byLayer {
		for _, ent := range roots {
			assignChildLayers(ent.Ref, layerName)
		}
	}
	for _, ent := range idx.spawned {
		assignChildLayers(ent.Ref, "")
	}
	for _, t := range snap.Taken {
		idx.takenByID[t.ObjectID] = t
		layerName := t.Layer
		if layerName == "" {
			layerName = "world"
		}
		if !seenLayer[layerName] {
			seenLayer[layerName] = true
			idx.layerOrder = append(idx.layerOrder, layerName)
		}
		idx.takenByLayer[layerName] = append(idx.takenByLayer[layerName], t)
	}
	return idx
}

// remoteSceneTreeView renders the Remote tab beside the local Scene Tree:
// active scenes, layers in draw order, live entity hierarchy (with Spawned at Runtime and Taken scene entities).
func remoteSceneTreeView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	selection := ui.NewSelection(cx, false)
	open := map[remoteTreeKey]bool{}
	openRev := core.NewSignal(cx, 0)
	_, snapMemo := newRemoteSnapMemo(cx, ed, rev)
	treeIdx := core.NewMemo(cx, func() *remoteTreeIndex {
		return buildRemoteTreeIndex(snapMemo.Get(), ed.remoteDocObjectIDs())
	})
	pendingSelectRef := ""

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

	roots := func() []any {
		idx := treeIdx.Get()
		if !idx.snap.Connected {
			return nil
		}
		out := make([]any, 0, len(idx.layerOrder)+1)
		for _, name := range idx.layerOrder {
			out = append(out, remoteTreeKey{kind: remoteNodeLayer, id: name, layer: name})
		}
		if len(idx.spawned) > 0 {
			out = append(out, remoteTreeKey{kind: remoteNodeSpawnedHeader, id: "__spawned__"})
		}
		return out
	}

	children := func(k remoteTreeKey) []any {
		idx := treeIdx.Get()
		if !idx.snap.Connected {
			return nil
		}
		switch k.kind {
		case remoteNodeLayer:
			ents := idx.byLayer[k.layer]
			taken := idx.takenByLayer[k.layer]
			out := make([]any, 0, len(ents)+len(taken))
			for _, ent := range ents {
				out = append(out, remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref, layer: k.layer})
			}
			for _, t := range taken {
				out = append(out, remoteTreeKey{kind: remoteNodeTaken, id: t.ObjectID, layer: k.layer})
			}
			return out
		case remoteNodeSpawnedHeader:
			out := make([]any, len(idx.spawned))
			for i, ent := range idx.spawned {
				out[i] = remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref}
			}
			return out
		case remoteNodeEntity:
			kids := idx.childrenByParent[k.id]
			out := make([]any, len(kids))
			for i, ent := range kids {
				out[i] = remoteTreeKey{kind: remoteNodeEntity, id: ent.Ref, layer: k.layer}
			}
			return out
		default:
			return nil
		}
	}

	// Sync tree cursor -> Editor.SelectRemoteEntity (dispatched off the UI goroutine).
	cx.Effect(func() {
		cur, _ := selection.Cursor().(remoteTreeKey)
		cx.Untrack(func() {
			if !ed.RemoteConnected() {
				pendingSelectRef = ""
				return
			}
			if cur.kind == remoteNodeEntity && cur.id != "" && selection.Has(cur) {
				if ed.SelectedRemoteEntity() != cur.id && pendingSelectRef != cur.id {
					targetRef := cur.id
					pendingSelectRef = targetRef
					ed.dispatchRemoteAction(cx, func() error {
						return ed.SelectRemoteEntity(targetRef)
					})
				}
			}
		})
	})

	// Sync Editor.SelectedRemoteEntity -> tree selection.
	cx.Effect(func() {
		idx := treeIdx.Get()
		cx.Untrack(func() {
			sel := idx.snap.SelectedRef
			cur, _ := selection.Cursor().(remoteTreeKey)
			if !idx.snap.Connected {
				pendingSelectRef = ""
			}
			if sel != "" && sel == pendingSelectRef {
				pendingSelectRef = ""
			}
			if sel == "" {
				if pendingSelectRef != "" && cur.kind == remoteNodeEntity && cur.id == pendingSelectRef && selection.Has(cur) {
					return
				}
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
			layerName := idx.entityLayer[sel]
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
								if l, ok := treeIdx.Get().layerByName[k.layer]; ok {
									if !l.Visible {
										return ed.T("remote.layerHidden", l10n.Param("space", l.Space))
									}
									return l.Space
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
					Content: ed.T("remote.spawnedHeader"),
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
								if t, ok := treeIdx.Get().takenByID[k.id]; ok {
									return ed.T("remote.takenAt", l10n.Param("step", t.TakenAtStep))
								}
								return ed.T("remote.taken")
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
								if ent, ok := treeIdx.Get().byRef[ref]; ok {
									if ent.Name != "" {
										return ent.Name
									}
									return ent.Ref
								}
								return ref
							},
							Style: core.Style{FontSize: 12, Color: colors.Text(), FlexGrow: 1, FlexShrink: 1},
						},
						core.Text{
							ContentFunc: func() string {
								if ent, ok := treeIdx.Get().byRef[ref]; ok {
									if len(ent.Tags) > 0 {
										return ent.Tags[0]
									}
									return ent.Ref
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
					return !snapMemo.Get().Connected
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "remote-disconnected-hint",
						Style:  core.Style{Gap: 8, Padding: core.Symmetric(8, 4)},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									snap := snapMemo.Get()
									if snap.Connecting {
										return ed.T("remote.connectingHint")
									}
									if snap.Error != "" {
										return ed.T("remote.errorPrefix", l10n.Param("error", snap.Error))
									}
									return ed.T("remote.disconnectedHint")
								},
								Style: core.Style{FontSize: 11, Color: colors.Muted()},
							},
							ui.ToggleButton{
								TestID: "remote-run-automation",
								Text:   ed.T("remote.autoConnect"),
								Label:  ed.T("remote.autoConnectLabel"),
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
								Text:    ed.T("remote.connect"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Project == nil || snapMemo.Get().Connecting
								},
								OnPress: func() {
									ed.startRemoteCommandConnect()
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
											snap := snapMemo.Get()
											if snap.ActiveScene != "" {
												return ed.T("remote.sceneActiveHeader",
													l10n.Param("scene", snap.ActiveScene),
													l10n.Param("count", len(snap.Scenes.Scenes)),
												)
											}
											return ed.T("remote.scenesHeader", l10n.Param("count", len(snap.Scenes.Scenes)))
										},
										Style: core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
									},
									core.Dynamic(func(cx *core.Scope) core.Node {
										snap := snapMemo.Get()
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
													return snapMemo.Get().ActiveScene == name
												},
												OnChange: func(on bool) {
													if on {
														ed.dispatchRemoteAction(cx, func() error {
															return ed.SelectRemoteScene(name)
														})
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
									idx := treeIdx.Get()
									if len(idx.spawned) > 0 {
										return ed.T("remote.entitiesHeaderSpawned",
											l10n.Param("count", len(idx.snap.Entities)),
											l10n.Param("spawned", len(idx.spawned)),
										)
									}
									return ed.T("remote.entitiesHeader", l10n.Param("count", len(idx.snap.Entities)))
								},
								Style: core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
							},
							ui.Tree{
								TestID:   "remote-entity-tree",
								Label:    ed.T("remote.entitiesTreeLabel"),
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
										return ed.T("remote.rowLabel.layer", l10n.Param("layer", k.layer))
									case remoteNodeSpawnedHeader:
										return ed.T("remote.rowLabel.spawned")
									case remoteNodeTaken:
										return ed.T("remote.rowLabel.taken", l10n.Param("id", k.id))
									default:
										return k.id
									}
								},
								ToggleLabel: ed.T("remote.expand"),
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
	return ed.RemoteConnected() || ed.RemoteConnecting() || ed.hasRemoteLiveChanges()
}

// remoteInspectorSchemaKey computes a stable structural signature for the Remote Inspector so editing values does not remount inputs and steal focus.
func remoteInspectorSchemaKey(ed *Editor, snap *RemoteSnapshot) string {
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

type remoteInspectorData struct {
	snap            *RemoteSnapshot
	changesByKey    map[remoteChangeKey]RemoteLiveChange
	inspectedFields map[string]map[string]any
	tunablesByName  map[string]any
}

// remoteInspectorView renders the live Remote Inspector inside InspectorView:
// selected live entity components (with game_component_set editing and "was X" indicators),
// LIVE CHANGES tray (per-row revert, Keep in scene, Discard),
// TUNABLES (game_tunable_set), and CONSOLE (game_console_exec).
func remoteInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	remoteTick, snapMemo := newRemoteSnapMemo(cx, ed, rev)
	dataMemo := core.NewMemo(cx, func() *remoteInspectorData {
		_ = remoteTick.Get()
		snap := snapMemo.Get()
		d := &remoteInspectorData{
			snap:         snap,
			changesByKey: ed.remoteLiveChangesByKey(),
		}
		if snap.Inspected != nil && len(snap.Inspected.Components) > 0 {
			fields := make(map[string]map[string]any, len(snap.Inspected.Components))
			for cn, raw := range snap.Inspected.Components {
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil && m != nil {
					fields[cn] = m
				}
			}
			d.inspectedFields = fields
		}
		if len(snap.Tunables) > 0 {
			tuns := make(map[string]any, len(snap.Tunables))
			for _, t := range snap.Tunables {
				tuns[t.Name] = t.Value
			}
			d.tunablesByName = tuns
		}
		return d
	})
	schema := core.NewMemo(cx, func() string {
		_ = remoteTick.Get()
		return remoteInspectorSchemaKey(ed, snapMemo.Get())
	})
	consoleDraft := core.NewSignal(cx, "")

	runConsole := func() {
		cmd := strings.TrimSpace(consoleDraft.GetUntracked())
		if cmd == "" {
			return
		}
		consoleDraft.Set("")
		ed.dispatchRemoteAction(cx, func() error {
			_, err := ed.RemoteExecConsole(cmd)
			return err
		})
	}

	return core.View{
		TestID: "remote-inspector",
		Style:  core.Style{Gap: 10},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = schema.Get()
				return remoteInspectorDynamicBody(cx, ed, snapMemo, dataMemo)
			}),
			// Console section stays outside the dynamic schema block so typing never loses focus on refresh.
			core.Show{
				When: func() bool {
					snap := snapMemo.Get()
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
										Content: ed.T("remote.consoleTitle"),
										Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
									},
									ui.Button{
										TestID:  "remote-console-clear",
										Text:    ed.T("remote.consoleClear"),
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
										Label:       ed.T("remote.consoleInputLabel"),
										Placeholder: ed.T("remote.consolePlaceholder"),
										Value:       consoleDraft.Get,
										OnChange:    func(s string) { consoleDraft.Set(s) },
										OnSubmit:    func(s string) { consoleDraft.Set(s); runConsole() },
										Style:       core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
									},
									ui.Button{
										TestID:  "remote-console-run",
										Text:    ed.T("remote.consoleRun"),
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
										_ = remoteTick.Get()
										hist := ed.RemoteConsoleHistory()
										if len(hist) == 0 {
											return core.Text{
												Content: ed.T("remote.consoleEmpty"),
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

func remoteInspectorDynamicBody(
	cx *core.Scope,
	ed *Editor,
	snapMemo *core.Memo[*RemoteSnapshot],
	dataMemo *core.Memo[*remoteInspectorData],
) core.Node {
	colors := ui.ColorsOf(cx)
	snap := ed.remoteSnapshotShared()

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
				Content: ed.T("remote.inspectorEmpty"),
				Style:   core.Style{FontSize: 12, Color: colors.Muted()},
			})
		} else {
			ins := snap.Inspected
			title := ins.Name
			if title == "" {
				title = ins.Ref
			}
			_, objectID := remoteEntityIdentity(ins, ins.Ref)
			docIDs := ed.remoteDocObjectIDs()
			_, inDoc := docIDs[ins.Name]
			isSpawned := objectID == "" && !inDoc
			badgeText := ed.T("remote.badge.live")
			if isSpawned {
				badgeText = ed.T("remote.badge.spawned")
			}
			sub := ins.Ref
			if ins.Layer != "" {
				sub = ins.Layer + " · " + ins.Ref
			}
			if isSpawned {
				sub = ed.T("remote.spawnedSubtitle", l10n.Param("ref", ins.Ref))
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
					tag = ed.T("remote.readOnlyTag")
				}
				nodes = append(nodes, sectionHeader(strings.ToUpper(compName), tag))

				var fieldMap map[string]any
				if err := json.Unmarshal(raw, &fieldMap); err == nil && fieldMap != nil {
					for _, fk := range sortedMapKeys(fieldMap) {
						fieldKey := fk
						sample := fieldMap[fieldKey]
						nodes = append(nodes, remoteFieldRow(cx, ed, dataMemo, ins.Ref, compName, fieldKey, sample, readOnlyComp))
					}
				} else {
					var scalar any
					_ = json.Unmarshal(raw, &scalar)
					nodes = append(nodes, remoteScalarComponentRow(cx, ed, snapMemo, ins.Ref, compName, scalar, readOnlyComp))
				}
			}

			if isSpawned {
				nodes = append(nodes, core.Text{
					Content: ed.T("remote.spawnedNote"),
					Style:   core.Style{FontSize: 11, Color: colors.Muted()},
				})
			}
		}
	}

	// LIVE CHANGES tray (kept even after stopping/disconnecting so the author can Keep in scene or Discard).
	nodes = append(nodes, remoteLiveChangesSection(cx, ed))

	// TUNABLES section.
	if snap.Connected && len(snap.Tunables) > 0 {
		nodes = append(nodes, remoteTunablesSection(cx, ed, dataMemo, snap.Tunables))
	}

	return core.View{
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func remoteFieldRow(
	cx *core.Scope,
	ed *Editor,
	dataMemo *core.Memo[*remoteInspectorData],
	ref, compName, fieldKey string,
	sample any,
	readOnly bool,
) core.Node {
	colors := ui.ColorsOf(cx)
	testID := "remote-comp-" + compName + "-" + fieldKey
	changeKey := remoteChangeKey{entityRef: ref, component: compName, field: fieldKey}

	getVal := func() any {
		d := dataMemo.Get()
		if d.snap.Inspected == nil || d.snap.Inspected.Ref != ref {
			return sample
		}
		if d.inspectedFields != nil {
			if m := d.inspectedFields[compName]; m != nil {
				return m[fieldKey]
			}
		}
		return nil
	}

	wasText := func() string {
		if d := dataMemo.Get(); d.changesByKey != nil {
			if c, ok := d.changesByKey[changeKey]; ok {
				return ed.T("remote.wasValue", l10n.Param("value", formatRemoteValue(c.From)))
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
					ed.dispatchRemoteAction(cx, func() error {
						return ed.SetRemoteComponentField(ref, compName, fieldKey, checked)
					})
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
					val := any(v)
					if isIntField {
						val = int(math.Round(v))
					}
					ed.dispatchRemoteAction(cx, func() error {
						return ed.SetRemoteComponentField(ref, compName, fieldKey, val)
					})
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
					ed.dispatchRemoteAction(cx, func() error {
						return ed.SetRemoteComponentField(ref, compName, fieldKey, text)
					})
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

func remoteScalarComponentRow(cx *core.Scope, ed *Editor, snapMemo *core.Memo[*RemoteSnapshot], ref, compName string, sample any, readOnly bool) core.Node {
	colors := ui.ColorsOf(cx)
	testID := "remote-comp-" + compName
	getVal := func() any {
		snap := snapMemo.Get()
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
				ed.dispatchRemoteAction(cx, func() error {
					return ed.SetRemoteComponentValue(ref, compName, checked, true)
				})
			},
		}
	case float64, int, int64:
		return ui.NumberInput{
			TestID: testID,
			Label:  compName,
			Step:   1,
			Value:  func() float64 { f, _ := asFloat64(getVal()); return f },
			OnChange: func(v float64) {
				ed.dispatchRemoteAction(cx, func() error {
					return ed.SetRemoteComponentValue(ref, compName, v, true)
				})
			},
		}
	case string:
		return ui.Input{
			TestID: testID,
			Label:  compName,
			Value:  func() string { return fmt.Sprint(getVal()) },
			OnChange: func(text string) {
				ed.dispatchRemoteAction(cx, func() error {
					return ed.SetRemoteComponentValue(ref, compName, text, true)
				})
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

func remoteLiveChangesSection(cx *core.Scope, ed *Editor) core.Node {
	colors := ui.ColorsOf(cx)
	changes := ed.RemoteLiveChanges()

	children := []core.Node{
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 6},
			Children: []core.Node{
				core.Text{
					Content: ed.T("remote.liveChangesTitle", l10n.Param("count", len(changes))),
					Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
				},
				core.Text{
					Content: ed.T("remote.appliedWithoutRestart"),
					Style:   core.Style{FontSize: 10, Color: colors.Muted()},
				},
			},
		},
	}

	if len(changes) == 0 {
		children = append(children, core.Text{
			TestID:  "remote-live-changes-empty",
			Content: ed.T("remote.liveChangesEmpty"),
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		})
	} else {
		for i, c := range changes {
			idx := i
			ref, comp, field := c.EntityRef, c.Component, c.Field
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
						Text:    ed.T("remote.undo"),
						Label:   ed.T("remote.undoLabel"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						OnPress: func() {
							ed.dispatchRemoteAction(cx, func() error {
								return ed.revertRemoteLiveChangeByKey(ref, comp, field)
							})
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
					Text:    ed.T("remote.keepInScene"),
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
					Text:    ed.T("remote.discard"),
					Size:    ui.SizeSmall,
					Variant: ui.ButtonOutline,
					OnPress: func() {
						ed.dispatchRemoteAction(cx, func() error {
							return ed.DiscardRemoteLiveChanges()
						})
					},
				},
			},
		})
		children = append(children, core.Text{
			Content: ed.T("remote.liveChangesNote"),
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

func remoteTunablesSection(cx *core.Scope, ed *Editor, dataMemo *core.Memo[*remoteInspectorData], tunables []gamemcp.TunableResult) core.Node {
	colors := ui.ColorsOf(cx)
	rows := []core.Node{
		core.Text{
			Content: ed.T("remote.tunablesTitle", l10n.Param("count", len(tunables))),
			Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
		},
	}

	for _, t := range tunables {
		tun := t
		testID := "remote-tunable-" + tun.Name
		getVal := func() any {
			if d := dataMemo.Get(); d.tunablesByName != nil {
				if v, ok := d.tunablesByName[tun.Name]; ok {
					return v
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
					ed.dispatchRemoteAction(cx, func() error {
						_, err := ed.RemoteSetTunable(tun.Name, checked, false)
						return err
					})
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
					iv := int(math.Round(v))
					ed.dispatchRemoteAction(cx, func() error {
						_, err := ed.RemoteSetTunable(tun.Name, iv, true)
						return err
					})
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
					ed.dispatchRemoteAction(cx, func() error {
						_, err := ed.RemoteSetTunable(tun.Name, v, true)
						return err
					})
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
					ed.dispatchRemoteAction(cx, func() error {
						_, err := ed.RemoteSetTunable(tun.Name, text, false)
						return err
					})
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
	_, snapMemo := newRemoteSnapMemo(cx, ed, rev)
	return core.Show{
		When: func() bool {
			return snapMemo.Get().Connected
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID: "status-remote",
				ContentFunc: func() string {
					snap := snapMemo.Get()
					var parts []string
					if snap.Loop != nil {
						if snap.Loop.Paused {
							parts = append(parts, ed.T("remote.bar.paused"))
						} else if snap.HasFPS {
							parts = append(parts, fmt.Sprintf("%.0f fps", snap.FPS))
						} else {
							parts = append(parts, ed.T("remote.bar.live"))
						}
						parts = append(parts, ed.T("remote.bar.frame", l10n.Param("frame", snap.Loop.Steps)))
						parts = append(parts, fmt.Sprintf("%.2fs", snap.Loop.GameTimeSeconds))
						parts = append(parts, fmt.Sprintf("%g×", snap.Loop.TimeScale))
					} else {
						parts = append(parts, ed.T("remote.bar.remote"))
					}
					parts = append(parts, ed.T("remote.bar.entities", l10n.Param("count", len(snap.Entities))))
					return ed.T("remote.bar.summary", l10n.Param("details", strings.Join(parts, " · ")))
				},
				Style: core.Style{FontSize: 11, Color: colors.Muted()},
			}
		},
	}
}
