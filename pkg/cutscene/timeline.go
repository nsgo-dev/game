package cutscene

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

// DialogueLine is the dialogue cue active at the current timeline position.
type DialogueLine struct {
	// ID is the localization line ID (for example "intro.knight.01"), empty when no dialogue clip is active.
	ID string
	// Speaker is the clip's optional speaker name.
	Speaker string
	// Text is the localized text resolved from the l10n catalog, or ID when no catalog is configured.
	Text string
	// Active reports whether a dialogue clip is active.
	Active bool
}

// TimelineTarget connects a TimelinePlayer to a scene, localization catalog, audio callbacks and mixer.
type TimelineTarget struct {
	// Scene, when non-nil, is the built scene whose objects and camera the player drives directly.
	// Actor clips update BuiltObject.Doc.X/Y and call BuiltObject.Sync; camera clips update Scene.Camera.
	Scene *sceneformat.BuiltScene
	// Doc, when non-nil and Scene is nil, is a SceneDoc whose objects and camera the player drives directly.
	Doc *sceneformat.SceneDoc
	// Catalog, when non-nil, resolves dialogue clip line IDs into localized text for ActiveDialogue and OnDialogue.
	Catalog *l10n.Catalog
	// Locale is the BCP-47 locale tag passed to Catalog.T; when empty, Catalog.FallbackLocale is used.
	Locale string
	// Mixer, when non-nil, plays music clips through sound.Mixer when OnMusic is nil.
	Mixer *sound.Mixer
	// AudioSource maps a clip's Source path to an audio.Source when playing through Mixer; when nil, audio.Source{URI: source} is used.
	AudioSource func(source string) audio.Source
	// OnActorState is called whenever an actor track's active state changes (and with "" when leaving a state clip).
	OnActorState func(actorID, state string)
	// OnDialogue is called whenever the active dialogue line changes.
	OnDialogue func(line DialogueLine)
	// OnSound is called once when Update crosses a sound clip's Start (never during Seek).
	OnSound func(clip Clip, group string, volume float32)
	// OnMusic is called once when Update crosses a music clip's Start (never during Seek).
	OnMusic func(clip Clip, group string, volume float32)
	// OnMusicStop is called when an active music clip ends or the cutscene ends, skips or stops.
	OnMusicStop func(clip Clip, fadeOut time.Duration)
	// OnSignal is called when playback crosses a signal marker.
	OnSignal func(name string, args map[string]any)
	// OnDone is called once when the cutscene finishes, is skipped or is stopped.
	OnDone func(Result)
}

type vec2 struct {
	x, y float32
}

type camPose struct {
	x, y, zoom float32
	hasZoom    bool
}

type activeMusic struct {
	clip  Clip
	end   time.Duration
	music *sound.Music
}

// TimelinePlayer runs a version 2 timeline cutscene deterministically on game.Loop's fixed steps against a scene.
//
// Call Play to begin, Update(dt) on each fixed loop step, Seek(at) to scrub to any timestamp without firing audio or completion callbacks, and Skip or Stop to end early.
type TimelinePlayer struct {
	c      Cutscene
	target TimelineTarget
	length time.Duration

	// Scene objects indexed by ID for O(1) updates.
	builtObjects map[string]*sceneformat.BuiltObject
	docObjects   map[string]*sceneformat.ObjectDoc

	// Initial scene state captured at construction so Seek and Reset are exact and repeatable.
	initActors map[string]vec2
	initCam    camPose
	hadCam     bool

	// Tracks grouped by kind for fast evaluation.
	actorTracks map[string]*Track
	actorOrder  []string
	camTracks   []*Track

	// Live playback state.
	playing     bool
	started     bool
	done        bool
	steppedOnce bool
	elapsed     time.Duration
	skippable   bool

	curActors   map[string]vec2
	actorStates map[string]string
	curCam      camPose
	hasCamPose  bool
	dialogue    DialogueLine

	// Audio trigger tracking indexed by [trackIndex][clipIndex].
	fired       [][]bool
	playingMus  []*activeMusic
	onStateHook func()
}

// NewTimelinePlayer validates c as a timeline cutscene, resolves its scene objects against target.Scene or target.Doc (when provided), snapshots initial poses and returns a player positioned at time 0.
func NewTimelinePlayer(c Cutscene, target TimelineTarget) (*TimelinePlayer, error) {
	if err := check(c); err != nil {
		return nil, err
	}
	if c.EffectiveKind() != KindTimeline {
		return nil, errors.New("cutscene: video cutscene cannot be played with TimelinePlayer")
	}
	p := &TimelinePlayer{
		c:            c,
		target:       target,
		length:       time.Duration(c.Length),
		builtObjects: make(map[string]*sceneformat.BuiltObject),
		docObjects:   make(map[string]*sceneformat.ObjectDoc),
		initActors:   make(map[string]vec2),
		actorTracks:  make(map[string]*Track),
		curActors:    make(map[string]vec2),
		actorStates:  make(map[string]string),
		skippable:    c.Skippable && c.SkipAfter <= 0,
		fired:        make([][]bool, len(c.Tracks)),
	}

	// Index scene objects if a scene or document was provided.
	hasScene := target.Scene != nil || target.Doc != nil
	if target.Scene != nil {
		for id, bo := range target.Scene.Objects {
			if bo != nil && bo.Doc != nil {
				p.builtObjects[id] = bo
				p.docObjects[id] = bo.Doc
				p.initActors[id] = vec2{x: bo.Doc.X, y: bo.Doc.Y}
				p.curActors[id] = vec2{x: bo.Doc.X, y: bo.Doc.Y}
			}
		}
		if target.Scene.Camera != nil {
			p.hadCam = true
			p.initCam = camPose{
				x:       target.Scene.Camera.X,
				y:       target.Scene.Camera.Y,
				zoom:    target.Scene.Camera.EffectiveZoom(),
				hasZoom: target.Scene.Camera.Zoom != nil,
			}
		} else {
			p.initCam = camPose{zoom: 1}
		}
	} else if target.Doc != nil {
		for li := range target.Doc.Layers {
			sceneformat.WalkObjects(target.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				p.docObjects[o.ID] = o
				p.initActors[o.ID] = vec2{x: o.X, y: o.Y}
				p.curActors[o.ID] = vec2{x: o.X, y: o.Y}
				return true
			})
		}
		if target.Doc.Camera != nil {
			p.hadCam = true
			p.initCam = camPose{
				x:       target.Doc.Camera.X,
				y:       target.Doc.Camera.Y,
				zoom:    target.Doc.Camera.EffectiveZoom(),
				hasZoom: target.Doc.Camera.Zoom != nil,
			}
		} else {
			p.initCam = camPose{zoom: 1}
		}
	} else {
		p.initCam = camPose{zoom: 1}
	}
	p.curCam = p.initCam

	// Validate object targets against the scene and group tracks.
	for ti := range p.c.Tracks {
		tr := &p.c.Tracks[ti]
		p.fired[ti] = make([]bool, len(tr.Clips))
		switch tr.Kind {
		case TrackActor:
			if hasScene && p.docObjects[tr.Target] == nil {
				return nil, fmt.Errorf("cutscene: track %d: scene has no actor object %q", ti, tr.Target)
			}
			if _, dup := p.actorTracks[tr.Target]; dup {
				return nil, fmt.Errorf("cutscene: track %d: duplicate actor track for target %q", ti, tr.Target)
			}
			p.actorTracks[tr.Target] = tr
			p.actorOrder = append(p.actorOrder, tr.Target)
		case TrackCamera:
			for ci := range tr.Clips {
				cl := &tr.Clips[ci]
				if cl.Target != "" && hasScene && p.docObjects[cl.Target] == nil {
					return nil, fmt.Errorf("cutscene: track %d clip %d: scene has no camera target %q", ti, ci, cl.Target)
				}
			}
			p.camTracks = append(p.camTracks, tr)
		}
	}
	return p, nil
}

// Cutscene returns the cutscene definition p plays.
func (p *TimelinePlayer) Cutscene() Cutscene { return p.c }

// Length returns the cutscene's total duration.
func (p *TimelinePlayer) Length() time.Duration { return p.length }

// Time returns the current playback position in [0, Length].
func (p *TimelinePlayer) Time() time.Duration { return p.elapsed }

// Playing reports whether playback is active.
func (p *TimelinePlayer) Playing() bool { return p.playing }

// Done reports whether playback has ended (finished, skipped or stopped).
func (p *TimelinePlayer) Done() bool { return p.done }

// CanSkip reports whether the cutscene is currently playing and can be skipped now.
func (p *TimelinePlayer) CanSkip() bool { return p.playing && p.skippable }

// ActiveDialogue returns the dialogue line active at the current position.
func (p *TimelinePlayer) ActiveDialogue() DialogueLine { return p.dialogue }

// ActorState returns the active animation state of actorID, or "" when none is active.
func (p *TimelinePlayer) ActorState(actorID string) string { return p.actorStates[actorID] }

// ActorPosition returns the current (x, y) position of actorID and whether it is tracked.
func (p *TimelinePlayer) ActorPosition(actorID string) (x, y float32, ok bool) {
	v, ok := p.curActors[actorID]
	return v.x, v.y, ok
}

// CameraState returns the camera's current (x, y, zoom) and whether the timeline has set the camera.
func (p *TimelinePlayer) CameraState() (x, y, zoom float32, ok bool) {
	return p.curCam.x, p.curCam.y, p.curCam.zoom, p.hasCamPose || p.hadCam
}

// Play starts or resumes playback from the current position. A finished cutscene stays finished until Reset is called.
func (p *TimelinePlayer) Play() {
	if p.done || p.playing {
		return
	}
	p.playing = true
	p.started = true
	p.applyPose(p.elapsed, true)
	p.notifyHook()
}

// Pause pauses playback at the current position without ending the cutscene.
func (p *TimelinePlayer) Pause() {
	if !p.playing {
		return
	}
	p.playing = false
	p.notifyHook()
}

// Reset rewinds to time 0, restores initial actor and camera poses, stops active cutscene music and clears done state.
func (p *TimelinePlayer) Reset() {
	p.stopAllMusic(0)
	p.playing = false
	p.started = false
	p.done = false
	p.steppedOnce = false
	p.elapsed = 0
	p.skippable = p.c.Skippable && p.c.SkipAfter <= 0
	for ti := range p.fired {
		clear(p.fired[ti])
	}
	for _, id := range p.actorOrder {
		initPos := p.initActors[id]
		p.curActors[id] = initPos
		p.writeActorPos(id, initPos)
		if p.actorStates[id] != "" {
			p.actorStates[id] = ""
			if p.target.OnActorState != nil {
				p.target.OnActorState(id, "")
			}
		}
	}
	p.curCam = p.initCam
	p.hasCamPose = false
	if p.hadCam {
		p.writeCamera(p.initCam)
	}
	if p.dialogue.Active {
		p.dialogue = DialogueLine{}
		if p.target.OnDialogue != nil {
			p.target.OnDialogue(DialogueLine{})
		}
	}
	p.notifyHook()
}

// Update advances a playing timeline cutscene by dt on a fixed game.Loop step.
func (p *TimelinePlayer) Update(dt time.Duration) {
	if !p.playing || p.done || dt <= 0 {
		return
	}
	prev := p.elapsed
	next := prev + dt
	if next < prev || next > p.length {
		next = p.length
	}
	firstStep := !p.steppedOnce
	p.steppedOnce = true
	p.elapsed = next
	if p.c.Skippable && p.elapsed >= time.Duration(p.c.SkipAfter) {
		p.skippable = true
	}

	p.applyPose(p.elapsed, true)
	p.fireAudio(prev, p.elapsed, firstStep)
	p.fireSignals(prev, p.elapsed, firstStep)
	p.notifyHook()

	if p.elapsed >= p.length {
		p.finish(Finished, true)
	}
}

func (p *TimelinePlayer) fireSignals(prev, cur time.Duration, first bool) {
	if p.target.OnSignal == nil {
		return
	}
	for _, tr := range p.c.Tracks {
		if tr.Kind != TrackSignal {
			continue
		}
		for _, cl := range tr.Clips {
			at := time.Duration(cl.Start)
			if (first && at == 0) || (at > prev && at <= cur) {
				args := map[string]any{}
				for k, v := range cl.Args {
					args[k] = v
				}
				p.target.OnSignal(cl.Signal, args)
			}
		}
	}
}

// Seek jumps directly to timestamp at (clamped to [0, Length]) and applies all actor, camera and dialogue states deterministically without firing sound, music or completion callbacks.
func (p *TimelinePlayer) Seek(at time.Duration) {
	if at < 0 {
		at = 0
	}
	if at > p.length {
		at = p.length
	}
	p.elapsed = at
	p.skippable = p.c.Skippable && at >= time.Duration(p.c.SkipAfter)
	p.applyPose(at, true)
	p.notifyHook()
}

// Skip ends playback with Skipped if the cutscene is playing and CanSkip reports true.
func (p *TimelinePlayer) Skip() bool {
	if !p.CanSkip() {
		return false
	}
	p.finish(Skipped, true)
	return true
}

// Stop ends playback with Stopped regardless of Skippable, reporting whether the cutscene was active (playing or paused mid-cutscene).
func (p *TimelinePlayer) Stop() bool {
	if !p.playing && (!p.started || p.done) {
		return false
	}
	p.finish(Stopped, true)
	return true
}

// stopQuiet ends playback without invoking OnDone, used when a parent scope or scene exits.
func (p *TimelinePlayer) stopQuiet() {
	if !p.playing && p.done {
		return
	}
	p.finish(Stopped, false)
}

func (p *TimelinePlayer) finish(reason Reason, notify bool) {
	p.playing = false
	p.started = false
	p.done = true
	p.stopAllMusic(0)
	if p.dialogue.Active {
		p.dialogue = DialogueLine{}
		if p.target.OnDialogue != nil {
			p.target.OnDialogue(DialogueLine{})
		}
	}
	p.notifyHook()
	if notify && p.target.OnDone != nil {
		p.target.OnDone(Result{Reason: reason, Position: p.elapsed})
	}
}

func (p *TimelinePlayer) notifyHook() {
	if p.onStateHook != nil {
		p.onStateHook()
	}
}

// applyPose evaluates all actor, camera and dialogue tracks purely at timestamp at.
func (p *TimelinePlayer) applyPose(at time.Duration, notify bool) {
	// 1. Evaluate actors first so camera tracks can follow/pan to their live positions.
	for _, id := range p.actorOrder {
		pos, state := p.evalActor(id, at)
		p.curActors[id] = pos
		p.writeActorPos(id, pos)
		if prev := p.actorStates[id]; prev != state {
			p.actorStates[id] = state
			if notify && p.target.OnActorState != nil {
				p.target.OnActorState(id, state)
			}
		}
	}

	// 2. Evaluate camera tracks.
	if len(p.camTracks) > 0 {
		cam := p.evalCamera(at)
		p.curCam = cam
		p.hasCamPose = true
		p.writeCamera(cam)
	}

	// 3. Evaluate dialogue tracks.
	dl := p.evalDialogue(at)
	if dl != p.dialogue {
		p.dialogue = dl
		if notify && p.target.OnDialogue != nil {
			p.target.OnDialogue(dl)
		}
	}
}

func (p *TimelinePlayer) evalActor(id string, at time.Duration) (vec2, string) {
	pos := p.initActors[id]
	tr := p.actorTracks[id]
	if tr == nil {
		return pos, ""
	}
	activeState := ""
	for i := range tr.Clips {
		cl := &tr.Clips[i]
		start := time.Duration(cl.Start)
		dur := time.Duration(cl.Duration)
		end := start + dur
		if at < start {
			break
		}
		u := 1.0
		if at < end && dur > 0 {
			u = float64(at-start) / float64(dur)
		}
		active := at >= start && (at < end || (at == p.length && end == p.length))
		switch cl.Action {
		case ActionState:
			if active {
				activeState = cl.State
			}
		case ActionWalk, ActionDrop:
			fromX, fromY := pos.x, pos.y
			if cl.FromX != nil {
				fromX = *cl.FromX
			}
			if cl.FromY != nil {
				fromY = *cl.FromY
			}
			toX, toY := fromX, fromY
			if cl.ToX != nil {
				toX = *cl.ToX
			}
			if cl.ToY != nil {
				toY = *cl.ToY
			}
			e := cl.Easing.At(u)
			pos.x = lerpFloat32(fromX, toX, e)
			pos.y = lerpFloat32(fromY, toY, e)
			if active && cl.State != "" {
				activeState = cl.State
			}
		case ActionPatrol:
			fromX, fromY := pos.x, pos.y
			if cl.FromX != nil {
				fromX = *cl.FromX
			}
			if cl.FromY != nil {
				fromY = *cl.FromY
			}
			toX, toY := fromX, fromY
			if cl.ToX != nil {
				toX = *cl.ToX
			}
			if cl.ToY != nil {
				toY = *cl.ToY
			}
			elapsed := min(at, end) - start
			factor := patrolFactor(fromX, fromY, toX, toY, elapsed, u, cl.Speed, cl.Easing)
			pos.x = lerpFloat32(fromX, toX, factor)
			pos.y = lerpFloat32(fromY, toY, factor)
			if active && cl.State != "" {
				activeState = cl.State
			}
		}
	}
	return pos, activeState
}

func patrolFactor(fromX, fromY, toX, toY float32, elapsed time.Duration, u float64, speed *float32, easing Easing) float64 {
	dist := math.Hypot(float64(toX-fromX), float64(toY-fromY))
	if dist == 0 {
		return 0
	}
	if speed != nil && *speed > 0 {
		phase := (float64(*speed) * elapsed.Seconds()) / dist
		if math.IsNaN(phase) || math.IsInf(phase, 0) {
			return 0
		}
		cycle := math.Mod(phase, 2)
		if cycle < 0 {
			cycle += 2
		}
		if cycle >= 1 {
			return easing.At(2 - cycle)
		}
		return easing.At(cycle)
	}
	// Default: one full round-trip (from -> to -> from) over the clip duration.
	phase := 2 * u
	if phase <= 1 {
		return easing.At(phase)
	}
	return easing.At(2 - phase)
}

func (p *TimelinePlayer) evalCamera(at time.Duration) camPose {
	cam := p.initCam
	for _, tr := range p.camTracks {
		for i := range tr.Clips {
			cl := &tr.Clips[i]
			start := time.Duration(cl.Start)
			dur := time.Duration(cl.Duration)
			end := start + dur
			if at < start {
				break
			}
			evalT := min(at, end)
			u := 1.0
			if at < end && dur > 0 {
				u = float64(at-start) / float64(dur)
			}
			switch cl.Action {
			case ActionHold:
				if cl.Target != "" {
					pos, _ := p.evalActor(cl.Target, evalT)
					cam.x = pos.x + cl.OffsetX
					cam.y = pos.y + cl.OffsetY
				} else {
					if cl.ToX != nil {
						cam.x = *cl.ToX
					}
					if cl.ToY != nil {
						cam.y = *cl.ToY
					}
				}
				if cl.Zoom != nil {
					cam.zoom = *cl.Zoom
					cam.hasZoom = true
				}
			case ActionFollow:
				pos, _ := p.evalActor(cl.Target, evalT)
				cam.x = pos.x + cl.OffsetX
				cam.y = pos.y + cl.OffsetY
				if cl.Zoom != nil {
					cam.zoom = *cl.Zoom
					cam.hasZoom = true
				}
			case ActionPan:
				startX, startY, startZoom := cam.x, cam.y, cam.zoom
				if cl.FromX != nil {
					startX = *cl.FromX
				}
				if cl.FromY != nil {
					startY = *cl.FromY
				}
				endX, endY, endZoom := startX, startY, startZoom
				if cl.Target != "" {
					pos, _ := p.evalActor(cl.Target, evalT)
					endX = pos.x + cl.OffsetX
					endY = pos.y + cl.OffsetY
				} else {
					if cl.ToX != nil {
						endX = *cl.ToX
					}
					if cl.ToY != nil {
						endY = *cl.ToY
					}
				}
				if cl.Zoom != nil {
					endZoom = *cl.Zoom
					cam.hasZoom = true
				}
				e := cl.Easing.At(u)
				cam.x = lerpFloat32(startX, endX, e)
				cam.y = lerpFloat32(startY, endY, e)
				cam.zoom = lerpFloat32(startZoom, endZoom, e)
			}
		}
	}
	return cam
}

func (p *TimelinePlayer) evalDialogue(at time.Duration) DialogueLine {
	for ti := range p.c.Tracks {
		tr := &p.c.Tracks[ti]
		if tr.Kind != TrackDialogue {
			continue
		}
		for ci := range tr.Clips {
			cl := &tr.Clips[ci]
			start := time.Duration(cl.Start)
			end := start + time.Duration(cl.Duration)
			if at >= start && (at < end || (at == p.length && end == p.length)) {
				text := cl.Line
				if p.target.Catalog != nil {
					loc := p.target.Locale
					if loc == "" {
						loc = p.target.Catalog.FallbackLocale()
					}
					text = p.target.Catalog.T(loc, cl.Line)
				}
				return DialogueLine{
					ID:      cl.Line,
					Speaker: cl.Speaker,
					Text:    text,
					Active:  true,
				}
			}
		}
	}
	return DialogueLine{}
}

func (p *TimelinePlayer) fireAudio(prev, cur time.Duration, firstStep bool) {
	// Stop any music clips whose end time was reached.
	if len(p.playingMus) > 0 {
		remaining := p.playingMus[:0]
		for _, am := range p.playingMus {
			if cur >= am.end {
				p.stopMusicEntry(am, time.Duration(am.clip.FadeOut))
				continue
			}
			remaining = append(remaining, am)
		}
		p.playingMus = remaining
	}

	for ti := range p.c.Tracks {
		tr := &p.c.Tracks[ti]
		if tr.Kind != TrackSound && tr.Kind != TrackMusic {
			continue
		}
		for ci := range tr.Clips {
			if p.fired[ti][ci] {
				continue
			}
			cl := tr.Clips[ci]
			start := time.Duration(cl.Start)
			triggered := (firstStep && prev == 0 && start == 0) || (start > prev && start <= cur)
			if !triggered {
				continue
			}
			p.fired[ti][ci] = true
			vol := float32(1)
			if cl.Volume != nil {
				vol = *cl.Volume
			}
			if tr.Kind == TrackSound {
				group := effectiveAudioGroup(cl.Group, tr.Group, p.c.Group, "sfx")
				if p.target.OnSound != nil {
					p.target.OnSound(cl, group, vol)
				}
				continue
			}
			group := effectiveAudioGroup(cl.Group, tr.Group, p.c.Group, "music")
			am := &activeMusic{clip: cl, end: start + time.Duration(cl.Duration)}
			if p.target.OnMusic != nil {
				p.target.OnMusic(cl, group, vol)
			} else if p.target.Mixer != nil {
				src := audio.Source{URI: cl.Source}
				if p.target.AudioSource != nil {
					src = p.target.AudioSource(cl.Source)
				}
				mu := p.target.Mixer.Music(group)
				mu.Loop = cl.Loop
				mu.Play(src, time.Duration(cl.FadeIn))
				am.music = mu
			}
			if cur >= am.end {
				p.stopMusicEntry(am, time.Duration(cl.FadeOut))
			} else {
				p.playingMus = append(p.playingMus, am)
			}
		}
	}
}

func (p *TimelinePlayer) stopMusicEntry(am *activeMusic, fadeOut time.Duration) {
	if am.music != nil {
		am.music.Stop(fadeOut)
		am.music = nil
	}
	if p.target.OnMusicStop != nil {
		p.target.OnMusicStop(am.clip, fadeOut)
	}
}

func (p *TimelinePlayer) stopAllMusic(fadeOut time.Duration) {
	for _, am := range p.playingMus {
		p.stopMusicEntry(am, fadeOut)
	}
	clear(p.playingMus)
	p.playingMus = p.playingMus[:0]
}

func effectiveAudioGroup(clipGroup, trackGroup, cutsceneGroup, fallback string) string {
	switch {
	case clipGroup != "":
		return clipGroup
	case trackGroup != "":
		return trackGroup
	case cutsceneGroup != "":
		return cutsceneGroup
	default:
		return fallback
	}
}

func (p *TimelinePlayer) writeActorPos(id string, pos vec2) {
	if bo := p.builtObjects[id]; bo != nil && bo.Doc != nil {
		bo.Doc.X = pos.x
		bo.Doc.Y = pos.y
		bo.Sync()
		return
	}
	if o := p.docObjects[id]; o != nil {
		o.X = pos.x
		o.Y = pos.y
	}
}

func (p *TimelinePlayer) writeCamera(cam camPose) {
	var dst *sceneformat.CameraDoc
	if p.target.Scene != nil {
		if p.target.Scene.Camera == nil {
			p.target.Scene.Camera = &sceneformat.CameraDoc{}
		}
		dst = p.target.Scene.Camera
	} else if p.target.Doc != nil {
		if p.target.Doc.Camera == nil {
			p.target.Doc.Camera = &sceneformat.CameraDoc{}
		}
		dst = p.target.Doc.Camera
	}
	if dst == nil {
		return
	}
	dst.X = cam.x
	dst.Y = cam.y
	if cam.hasZoom {
		z := cam.zoom
		dst.Zoom = &z
	}
}

func lerpFloat32(a, b float32, t float64) float32 {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return float32(float64(a) + float64((float64(b)-float64(a))*t))
}
