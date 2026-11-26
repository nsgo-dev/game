package editor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	gamedebug "github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Playing reports whether Play mode is currently active.
func (e *Editor) Playing() bool {
	return e != nil && e.playing
}

// PlayPaused reports whether Play mode is currently paused (frozen).
func (e *Editor) PlayPaused() bool {
	return e != nil && e.playing && e.playPaused
}

// SetPlayPaused pauses or resumes local Play mode. Resuming clears the viewport agent stepping pill.
func (e *Editor) SetPlayPaused(paused bool) {
	if e == nil || !e.playing {
		return
	}
	if e.playPaused == paused {
		return
	}
	e.playPaused = paused
	if e.playLoop != nil {
		e.playLoop.SetPaused(paused)
	}
	if paused {
		e.Status = fmt.Sprintf("Paused · frame %s", formatPlayFrameNumber(e.playSteps))
	} else {
		e.playMarks.askOpen = false
		e.playMarks.agentPill = ""
		e.Status = "Playing"
	}
	e.notify()
}

// PausePlay pauses local Play mode at the current frame.
func (e *Editor) PausePlay() {
	e.SetPlayPaused(true)
}

// ResumePlay resumes local Play mode from a paused state.
func (e *Editor) ResumePlay() {
	e.SetPlayPaused(false)
}

// PlayTimeScale returns the current Play mode time scale (1 when unset).
func (e *Editor) PlayTimeScale() float64 {
	if e == nil || e.playTimeScale <= 0 || math.IsNaN(e.playTimeScale) || math.IsInf(e.playTimeScale, 0) {
		return 1
	}
	return e.playTimeScale
}

// SetPlayTimeScale updates the Play mode simulation time scale (such as 0.25, 0.5, 1, or 2).
func (e *Editor) SetPlayTimeScale(scale float64) {
	if e == nil || scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return
	}
	e.playTimeScale = scale
	if e.playLoop != nil {
		e.playLoop.SetTimeScale(scale)
	}
	e.notify()
}

// PlayRateHz returns the fixed update rate in Hz used by Play mode (60 when PlayRate <= 0).
func (e *Editor) PlayRateHz() int {
	if e == nil || e.PlayRate <= 0 {
		return 60
	}
	return e.PlayRate
}

// PlayActiveInput returns the active replay.ActionSource for the running Play session:
// the replay.Player while replaying a recording, otherwise the 60-second rolling recorder wrapping PlayInputSource.
func (e *Editor) PlayActiveInput() replay.ActionSource {
	if e == nil {
		return nil
	}
	if e.playReplayPlayer != nil {
		return e.playReplayPlayer
	}
	if e.playRolling != nil {
		return e.playRolling
	}
	return e.PlayInputSource
}

// StepPlay synchronously advances local Play mode by n fixed simulation steps (even while paused)
// and returns the number of steps actually executed.
func (e *Editor) StepPlay(n int) int {
	if e == nil || !e.playing || n <= 0 {
		return 0
	}
	if e.playLoop != nil {
		return e.playLoop.StepNow(n)
	}
	dt := time.Second / time.Duration(e.PlayRateHz())
	ran := 0
	for range n {
		if !e.playing {
			break
		}
		e.PlayTick(dt)
		ran++
	}
	return ran
}

// PlayTick advances local Play mode by one fixed simulation step of duration dt, recording input into
// the 60-second rolling buffer and recovering panics into the Output panel.
func (e *Editor) PlayTick(dt time.Duration) {
	if e == nil || !e.playing {
		return
	}
	e.runPlayStep(dt, nil)
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
// Calling Stop stops the loop, dispatches any unsent Play marks as one thread, and restores the exact pre-play SceneDoc snapshot.
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
	return e.startPlaySession(cx, nil, false)
}

func (e *Editor) startPlaySession(cx *core.Scope, playback *replay.Recording, startPaused bool) error {
	if e.Doc == nil {
		return errors.New("editor: cannot play nil scene document")
	}
	e.CloseAskInPlace()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	e.exitTileReviewBeforePreview()
	e.resetViewport3DAgentPrompt(false)
	e.resetFirstRunFlow()
	if e.ReviewOpen() {
		e.PauseReview()
	}
	e.cancelAutoSaveTimer()
	e.stopRunningTasksForPlay()
	e.ExitHistoryPreview()

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
	e.ClosePermissionsSettings()
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
	if e.EditorSettingsOpen() {
		e.settingsOpen = false
	}
	e.shortcutsSheetOpen = false
	e.stopReleaseBuild()
	e.closeBuildReleaseScreenSilent()
	e.pauseDataTableReviewSilent()
	e.contextMenu.open = false
	e.contextMenu.targetID = ""
	e.contextMenu.renameReqID = ""
	e.resetTextInputFocus()
	e.cancelBackgroundPreview()
	e.cancelMaterialColorPreview()

	if playback == nil && e.playReplay != nil {
		playback = e.playReplay
	}
	seed := e.PlaySeed
	rate := e.PlayRateHz()
	actions := e.playWatchedActions()
	axes := uniquePlayNames(e.PlayInputAxes)
	if playback != nil {
		seed = playback.Seed
		if playback.Rate > 0 {
			rate = playback.Rate
		}
		actions = playback.Actions
		axes = playback.Axes
		if e.playReplayPlayer == nil {
			e.playReplayPlayer = replay.NewPlayer(playback)
		}
		e.playRolling = newPlayRollingRecorder(e.playReplayPlayer, seed, rate, actions, axes)
		e.playMarks.activeReplay = playback.Clone()
	} else {
		e.playReplayPlayer = nil
		e.playRolling = newPlayRollingRecorder(e.PlayInputSource, seed, rate, actions, axes)
		e.playMarks.marks = nil
		e.playMarks.askOpen = false
		e.playMarks.agentPill = ""
		e.playMarks.agentActive = false
		e.playMarks.agentSteps = nil
		e.playMarks.sentQuestion = ""
		e.playMarks.sentMeta = ""
		e.playMarks.activeAsk = nil
		e.playMarks.activeReplay = nil
	}

	if e.OnPlayReset != nil {
		if err := e.OnPlayReset(seed); err != nil {
			return fmt.Errorf("editor: play reset: %w", err)
		}
	}

	e.output.mu.Lock()
	e.output.runCount++
	e.output.runLines = nil
	e.output.mu.Unlock()

	e.playSnapshot = snap
	e.Doc = playDoc
	e.Built = playBuilt
	e.playScene = playBuilt
	e.playPhysics = playPhys
	e.playTriggerEvents = nil
	e.playSteps = 0
	e.playFrames = 0
	e.playPaused = startPaused
	if e.playTimeScale <= 0 {
		e.playTimeScale = 1
	}
	e.playing = true
	e.refreshClipPreview()
	if startPaused {
		e.Status = "Paused · frame 0"
	} else {
		e.Status = "Playing"
	}

	if cx == nil {
		e.playLoop = nil
		e.playStop = nil
		e.notify()
		return nil
	}

	playProf := gamedebug.NewProfiler(300)
	playProf.BeginFrame()
	playGC := gamedebug.NewGCMonitor(gamedebug.FrameHistory)
	playOverlay := gamedebug.New(gamedebug.Options{
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

	loopRate := e.PlayRate
	if loopRate <= 0 && e.playReplayPlayer != nil && e.playReplayPlayer.Rate() > 0 {
		loopRate = e.playReplayPlayer.Rate()
	}

	loop := &game.Loop{
		Rate:      loopRate,
		Paused:    startPaused,
		TimeScale: e.playTimeScale,
		Update: func(dt time.Duration) {
			if !e.playing {
				return
			}
			tStart := time.Now()
			e.runPlayStep(dt, playProf)
			if elapsed := time.Since(tStart); elapsed > 0 {
				updateCost += elapsed
			}
		},
		Draw: func(alpha float64) {
			if !e.playing {
				return
			}
			tStart := time.Now()
			e.playFrames++
			if e.OnPlayDraw != nil {
				panicked := false
				func() {
					defer func() {
						if r := recover(); r != nil {
							panicked = true
							e.recordPlayPanic(r, debug.Stack())
						}
					}()
					tok := playProf.Begin("draw")
					e.OnPlayDraw(e.playScene, alpha)
					playProf.End(tok)
				}()
				if panicked || !e.playing {
					return
				}
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
	e.playLoop = loop
	cx.Run(func() {
		e.playStop = loop.Run(cx)
		cx.Cleanup(func() {
			e.detachPlayProfiler()
		})
	})
	e.notify()
	return nil
}

func (e *Editor) runPlayStep(dt time.Duration, playProf *gamedebug.Profiler) {
	if !e.playing {
		return
	}
	e.playSteps++
	if e.playRolling != nil {
		e.playRolling.Step()
	} else if e.playReplayPlayer != nil {
		e.playReplayPlayer.Step()
	}

	defer func() {
		if r := recover(); r != nil {
			e.recordPlayPanic(r, debug.Stack())
		}
	}()

	if e.playReplayPlayer != nil && e.OnPlayReplayStep != nil {
		e.OnPlayReplayStep(e.playReplayPlayer, e.playScene, dt)
	}

	if e.playPhysics != nil && e.playPhysics.HasPhysics() {
		if playProf != nil {
			tok := playProf.Begin("physics")
			e.playPhysics.Step(dt)
			playProf.End(tok)
		} else {
			e.playPhysics.Step(dt)
		}
	}
	// Animated tiles of the scene's tilemaps advance as they do in the game.
	e.playScene.Update(dt)
	if e.playScene.Lights2D != nil {
		_ = e.playScene.SyncLights2D()
	}
	if e.OnPlayUpdate != nil {
		if playProf != nil {
			tok := playProf.Begin("update")
			e.OnPlayUpdate(e.playScene, dt)
			playProf.End(tok)
		} else {
			e.OnPlayUpdate(e.playScene, dt)
		}
	}
	e.notify()
}

// Stop halts the running Play loop, sends any unsent Play marks as one thread,
// and restores the exact pre-play SceneDoc snapshot, discarding any runtime mutations made during Play mode.
func (e *Editor) Stop() error {
	return e.stopInternal(true)
}

func (e *Editor) stopInternal(sendMarks bool) error {
	if e == nil || !e.playing {
		return nil
	}
	if sendMarks {
		e.SendPlayMarks()
	}
	if e.playStop != nil {
		e.playStop()
		e.playStop = nil
	}
	e.playLoop = nil
	e.playing = false
	e.playPaused = false
	e.playScene = nil
	e.playPhysics = nil
	e.playReplay = nil
	e.playReplayPlayer = nil
	e.playReplayPath = ""
	e.playReplayRunIndex = 0
	e.playMarks.askOpen = false
	e.playMarks.agentPill = ""
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
	e.scheduleAutoSaveIfNeeded()
	e.Status = "Stopped"
	e.notify()
	return nil
}
