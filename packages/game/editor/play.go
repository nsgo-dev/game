package editor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Playing reports whether Play mode is currently active.
func (e *Editor) Playing() bool {
	return e != nil && e.playing
}

// PlayScene returns the active runtime BuiltScene while playing, or nil when stopped.
func (e *Editor) PlayScene() *sceneformat.BuiltScene {
	if e == nil || !e.playing {
		return nil
	}
	return e.playScene
}

// PlayPhysics returns the active runtime PhysicsScene constructed from built-in physics components while playing, or nil when stopped.
func (e *Editor) PlayPhysics() *sceneformat.PhysicsScene {
	if e == nil || !e.playing {
		return nil
	}
	return e.playPhysics
}

// PlayTriggerEvents returns a copy of all built-in physics trigger events recorded during the current or last Play session.
func (e *Editor) PlayTriggerEvents() []sceneformat.PhysicsTriggerEvent {
	if e == nil || len(e.playTriggerEvents) == 0 {
		return nil
	}
	out := make([]sceneformat.PhysicsTriggerEvent, len(e.playTriggerEvents))
	copy(out, e.playTriggerEvents)
	return out
}

// PlaySteps returns the number of fixed simulation updates executed in the current or last Play session.
func (e *Editor) PlaySteps() int {
	if e == nil {
		return 0
	}
	return e.playSteps
}

// PlayFrames returns the number of draw frames executed in the current or last Play session.
func (e *Editor) PlayFrames() int {
	if e == nil {
		return 0
	}
	return e.playFrames
}

// PlayReplay returns the active replay.Recording while watching a playtest run in Play mode, or nil otherwise.
func (e *Editor) PlayReplay() *replay.Recording {
	if e == nil || !e.playing {
		return nil
	}
	return e.playReplay
}

// PlayReplayPlayer returns the active replay.Player driving Play mode while watching a recording, or nil otherwise.
func (e *Editor) PlayReplayPlayer() *replay.Player {
	if e == nil || !e.playing {
		return nil
	}
	return e.playReplayPlayer
}

// PlayReplayPath returns the slash-separated path of the recording currently playing in Play mode, or empty.
func (e *Editor) PlayReplayPath() string {
	if e == nil || !e.playing {
		return ""
	}
	return e.playReplayPath
}

// PlayReplayRunIndex returns the 1-based playtest run index being watched in Play mode, or 0.
func (e *Editor) PlayReplayRunIndex() int {
	if e == nil || !e.playing {
		return 0
	}
	return e.playReplayRunIndex
}

// PlayReplayIn starts Play mode on cx driven by rec (such as a recording opened from the Playtest Report screen's "Watch run N" button).
func (e *Editor) PlayReplayIn(cx *core.Scope, rec *replay.Recording, recordingPath string, runIndex int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if rec == nil {
		return errors.New("editor: replay recording is nil")
	}
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	e.playReplay = rec
	e.playReplayPlayer = replay.NewPlayer(rec)
	e.playReplayPath = recordingPath
	e.playReplayRunIndex = runIndex
	if err := e.PlayIn(cx); err != nil {
		e.playReplay = nil
		e.playReplayPlayer = nil
		e.playReplayPath = ""
		e.playReplayRunIndex = 0
		return err
	}
	if runIndex > 0 {
		e.Status = fmt.Sprintf("Watching run %d (%s)", runIndex, recordingPath)
	} else if recordingPath != "" {
		e.Status = fmt.Sprintf("Watching replay (%s)", recordingPath)
	}
	e.notify()
	return nil
}

// Play snapshots the current SceneDoc and starts a live game.Loop on the editor's attached core.Scope.
// Call EditorView(cx, ed) or AttachScope(cx) beforehand, or call PlayIn(cx) with an explicit scope.
func (e *Editor) Play() error {
	return e.PlayIn(e.scope)
}

// PlayIn snapshots the current SceneDoc (as deterministic YAML), builds an isolated runtime copy,
// and starts a live game.Loop on cx calling OnPlayUpdate and OnPlayDraw.
// Calling Stop stops the loop and restores the exact pre-play SceneDoc snapshot.
func (e *Editor) PlayIn(cx *core.Scope) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return nil
	}
	if cx == nil {
		return errors.New("editor: play requires a core.Scope (mount EditorView or pass cx to PlayIn)")
	}
	if e.Doc == nil {
		return errors.New("editor: cannot play nil scene document")
	}
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	e.exitTileReviewBeforePreview()

	// A 2D drag or particle curve drag in progress is put back, so Play and the scene Stop restores start from the last recorded state.
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelAssetDrag()
	e.stopAssetPreviewLoop()
	e.assetDrawer.previewPlaying = false
	e.stopAssetAudioPreview()
	e.ClearPluginHover2D()
	e.CancelGizmoDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.cancelParticleCurveDrag()
	e.CancelImagePivotDrag()
	e.overlaysPopoverOpen = false
	snap, err := sceneformat.Encode(e.Doc)
	if err != nil {
		return fmt.Errorf("editor: snapshot scene: %w", err)
	}
	playDoc, err := sceneformat.Parse(snap)
	if err != nil {
		return fmt.Errorf("editor: clone play scene: %w", err)
	}
	// A scene that instances or inherits other files is resolved first, so that the play document and the built scene share the same objects and runtime changes made through BuiltObject.Doc show in e.Doc; Stop restores the snapshot, which keeps the references.
	if playDoc.RefersToScenes() {
		scenes, _ := e.Resolver.(sceneformat.SceneResolver)
		if playDoc, err = sceneformat.Resolve(playDoc, scenes); err != nil {
			return fmt.Errorf("editor: resolve play scene: %w", err)
		}
	}
	playBuilt, err := sceneformat.Build(playDoc, e.liveResolver())
	if err != nil {
		return fmt.Errorf("editor: build play scene: %w", err)
	}
	playPhys, err := sceneformat.BuildPhysics(playBuilt, e.physicsSettings(), e.physicsResolver())
	if err != nil {
		return fmt.Errorf("editor: build play physics: %w", err)
	}
	playPhys.OnTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		e.playTriggerEvents = append(e.playTriggerEvents, ev)
		if e.OnPlayTrigger != nil {
			e.OnPlayTrigger(ev)
		}
	}
	if e.components != nil && len(playDoc.Connections) > 0 {
		if sigs, err := sceneformat.Connect(playDoc, e.components, playBuilt); err == nil {
			playPhys.Signals = sigs
		}
	}

	e.stopClipLoop()
	e.stopStatesLoop()
	e.stopCutsceneLoop()
	e.SetCutsceneReviewPeekBefore(false)
	e.stopTileAnimLoop()
	e.SetStatesDriveWithKeys(false)
	e.stopParticlePreviewLoop()
	e.stopSceneEmitterLoop()
	e.stopRigLoop()
	e.CancelRigDrag()
	e.stopModelPreviewLoop()
	e.stopAudioAudition()
	e.stopChangedAudioPlayback()
	e.resetInputMapTransient()
	e.connPick = nil
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.CancelHUDTouchDrag()
	e.hud.open = false
	e.playtest.open = false
	if e.usages.open {
		e.usages.open = false
		e.usages.rev++
	}
	e.shortcutCaptureActive = false
	e.editorSettingsOpen = false
	e.shortcutsSheetOpen = false
	e.stopReleaseBuild()
	e.closeBuildReleaseScreenSilent()
	e.contextMenu.open = false
	e.contextMenu.targetID = ""
	e.contextMenu.renameReqID = ""
	e.cancelBackgroundPreview()
	e.cancelMaterialColorPreview()
	e.playSnapshot = snap
	e.Doc = playDoc
	e.Built = playBuilt
	e.playScene = playBuilt
	e.playPhysics = playPhys
	e.playTriggerEvents = nil
	e.playSteps = 0
	e.playFrames = 0
	e.playing = true
	e.refreshClipPreview()
	e.Status = "Playing"

	playProf := debug.NewProfiler(300)
	playProf.BeginFrame()
	playGC := debug.NewGCMonitor(debug.FrameHistory)
	playOverlay := debug.New(debug.Options{
		Profiler: playProf,
		GC:       playGC,
	})
	useLocalProfiler := !e.ProfilerConnected()
	if useLocalProfiler {
		bridge := mcp.New(mcp.Config{
			Overlay:  playOverlay,
			Profiler: playProf,
			GC:       playGC,
		})
		e.attachProfilerClient(NewBridgeProfilerClient(bridge), true, "Play · live")
	}

	var updateCost time.Duration
	var lastDrawEnd time.Time

	rate := e.PlayRate
	if rate <= 0 && e.playReplayPlayer != nil && e.playReplayPlayer.Rate() > 0 {
		rate = e.playReplayPlayer.Rate()
	}

	loop := &game.Loop{
		Rate: rate,
		Update: func(dt time.Duration) {
			if !e.playing {
				return
			}
			tStart := time.Now()
			e.playSteps++
			if e.playReplayPlayer != nil {
				e.playReplayPlayer.Step()
				if e.OnPlayReplayStep != nil {
					e.OnPlayReplayStep(e.playReplayPlayer, e.playScene, dt)
				}
			}
			if e.playPhysics != nil && e.playPhysics.HasPhysics() {
				tok := playProf.Begin("physics")
				e.playPhysics.Step(dt)
				playProf.End(tok)
			}
			// Animated tiles of the scene's tilemaps advance as they do in the game.
			e.playScene.Update(dt)
			if e.playScene.Lights2D != nil {
				_ = e.playScene.SyncLights2D()
			}
			if e.OnPlayUpdate != nil {
				tok := playProf.Begin("update")
				e.OnPlayUpdate(e.playScene, dt)
				playProf.End(tok)
			}
			if elapsed := time.Since(tStart); elapsed > 0 {
				updateCost += elapsed
			}
			e.notify()
		},
		Draw: func(alpha float64) {
			if !e.playing {
				return
			}
			tStart := time.Now()
			e.playFrames++
			if e.OnPlayDraw != nil {
				tok := playProf.Begin("draw")
				e.OnPlayDraw(e.playScene, alpha)
				playProf.End(tok)
			}
			playProf.EndFrame()
			playProf.BeginFrame()
			drawEnd := time.Now()
			drawCost := max(time.Duration(0), drawEnd.Sub(tStart))
			total := updateCost + drawCost
			if !lastDrawEnd.IsZero() {
				if delta := drawEnd.Sub(lastDrawEnd); delta > total {
					total = delta
				}
			}
			lastDrawEnd = drawEnd
			u := updateCost
			updateCost = 0
			playOverlay.RecordFrame(total, u, drawCost)
			if e.isPlayProfilerActive() && (e.playFrames == 1 || e.playFrames%10 == 0) {
				_ = e.PollProfiler(context.Background())
			}
			e.notify()
		},
	}
	cx.Run(func() {
		e.playStop = loop.Run(cx)
		cx.Cleanup(func() {
			e.detachPlayProfiler()
		})
	})
	e.notify()
	return nil
}

// Stop halts the running Play loop and restores the exact pre-play SceneDoc snapshot,
// discarding any runtime mutations made during Play mode.
func (e *Editor) Stop() error {
	if e == nil || !e.playing {
		return nil
	}
	if e.playStop != nil {
		e.playStop()
		e.playStop = nil
	}
	e.playing = false
	e.playScene = nil
	e.playPhysics = nil
	e.playReplay = nil
	e.playReplayPlayer = nil
	e.playReplayPath = ""
	e.playReplayRunIndex = 0
	e.detachPlayProfiler()

	snap := e.playSnapshot
	e.playSnapshot = nil

	if len(snap) > 0 {
		restored, err := sceneformat.Parse(snap)
		if err != nil {
			return fmt.Errorf("editor: restore pre-play snapshot: %w", err)
		}
		e.Doc = restored
	}
	e.Rebuild()
	e.clearDiskWatchNotified()
	e.Status = "Stopped"
	e.notify()
	return nil
}
