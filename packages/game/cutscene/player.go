package cutscene

import (
	"errors"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/core"
)

// Reason says how a cutscene ended.
type Reason uint8

const (
	// Finished is a cutscene that played to its end.
	Finished Reason = iota
	// Skipped is a cutscene the player skipped, by a press, a skip key, back or Player.Skip.
	Skipped
	// Stopped is a cutscene the game stopped, by Player.Stop or by playing another one over it.
	Stopped
	// Failed is a cutscene the platform could not open or play, such as a missing file or a codec it lacks.
	Failed
)

// String names a reason for logs and diagnostics.
func (r Reason) String() string {
	switch r {
	case Finished:
		return "finished"
	case Skipped:
		return "skipped"
	case Stopped:
		return "stopped"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

// Result is how a cutscene ended, which Player.Play hands its done callback.
type Result struct {
	// Reason says how it ended.
	Reason Reason
	// Position is how far into the video it played, as the platform last reported it while playing (every 250 milliseconds and as it pauses or ends); 0 when it reported nothing.
	Position time.Duration
	// Err is why it failed, a *core.MediaError from the platform, for Failed; nil otherwise.
	Err error
}

// Options are what a Player pauses while a cutscene plays and how it is skipped.
type Options struct {
	// Loop is the game's loop, paused while a cutscene plays (game.Loop.SetPaused) and set back as it was when it ends; nil for none. Draw goes on running under the video, as for any paused loop.
	Loop *game.Loop
	// Mixer is the game's mixer, suspended while a cutscene plays (sound.Mixer.Suspend) and resumed when it ends. Its master volume, times the cutscene's group's, sets how loud the video plays. Nil leaves the game's sound alone and plays the video at full volume.
	Mixer *sound.Mixer
	// SkipKeys are the keys that skip a skippable cutscene, as core.Scope.OnKey names them; nil for "enter" and "space", and an empty slice for none. A press on the video and back (Escape, the back button or gesture, the browser's back) skip it too.
	SkipKeys []string
	// TestID is the video view's test ID, "cutscene" when empty.
	TestID string
}

// Player plays one cutscene at a time over a game's view, pausing the game's loop and sound while it plays.
//
// Mount View over the game's view, as the last child of the view that holds the game's canvas, so the video covers it. Play starts a cutscene from a game's Update or anywhere else on the engine's goroutine, and Scene makes one a scene of its own to play between scenes.
//
// The video plays in the platform's player (core.Video). It pauses while the app is in the background and plays on as it returns; when the platform pauses it itself, as for a call or headphones unplugged, a press or a skip key plays it on rather than skipping it.
//
// Its methods are called on the engine's goroutine, as core's are.
type Player struct {
	cx   *core.Scope
	opts Options
	// cur is the cutscene playing, nil when none does.
	cur *core.Signal[*playback]
}

// playback is one Play of a cutscene.
type playback struct {
	c    Cutscene
	done func(Result)
	// position is how far the video played, as the platform last reported.
	position time.Duration
	// skippable says the cutscene can be skipped now: it is Skippable and played past SkipAfter.
	skippable *core.Signal[bool]
	// held says the platform paused the video itself (core.Video.OnPause), so it plays on only once Paused changes.
	held *core.Signal[bool]
	// loopPaused is the loop's Paused before the cutscene paused it; resumeSound resumes the mixer, nil without one.
	loopPaused  bool
	resumeSound func()
	// owner is the scene controller (Player.Scene) that played it, nil for Player.Play.
	owner *scene.Controller
}

// ErrReplaced is what Player.Play returns, playing nothing, when the done callback of the cutscene it stops plays another cutscene: that one plays instead.
var ErrReplaced = errors.New("cutscene: the stopped cutscene's done callback played another")

// NewPlayer returns a player that lives as long as cx. Disposing cx while a cutscene plays stops it without calling its done callback, and sets the loop and the mixer back as they were.
func NewPlayer(cx *core.Scope, opts Options) *Player {
	p := &Player{cx: cx, opts: opts}
	cx.Run(func() {
		p.cur = core.NewSignal[*playback](cx, nil)
		cx.Cleanup(func() {
			if pb := p.cur.GetUntracked(); pb != nil {
				p.restore(pb)
			}
		})
	})
	return p
}

// Play plays c over the game, pausing the loop and the mixer until it ends, and then calls done, which may be nil, with how it ended; the loop and the mixer are back as they were by then, so done may play another cutscene or switch scenes. A cutscene playing already stops first, its done callback told Stopped.
//
// It returns an error, and plays nothing, for a cutscene with no source, an unknown Fit or a negative SkipAfter, and ErrReplaced when the stopped cutscene's done callback plays another cutscene, which then plays in its place.
func (p *Player) Play(c Cutscene, done func(Result)) error {
	_, err := p.play(c, done, nil)
	return err
}

// play is Play for a cutscene owner played, nil for Play itself, returning its playback.
func (p *Player) play(c Cutscene, done func(Result), owner *scene.Controller) (*playback, error) {
	if err := check(c); err != nil {
		return nil, err
	}
	if p.cx.Disposed() {
		return nil, errors.New("cutscene: the player's scope is disposed")
	}
	p.end(Stopped, nil, true)
	// The stopped cutscene's done callback may have played another; it has paused the loop and the mixer already, so taking them here again would leave them paused for good.
	if p.cur.GetUntracked() != nil {
		return nil, ErrReplaced
	}
	pb := &playback{c: c, done: done, owner: owner}
	p.cx.Run(func() {
		pb.skippable = core.NewSignal(p.cx, c.Skippable && c.SkipAfter <= 0)
		pb.held = core.NewSignal(p.cx, false)
	})
	if l := p.opts.Loop; l != nil {
		pb.loopPaused = l.Paused
		l.SetPaused(true)
	}
	if m := p.opts.Mixer; m != nil {
		pb.resumeSound = m.Suspend()
	}
	p.cur.Set(pb)
	return pb, nil
}

// Scene returns a scene that plays c as it enters and calls done as it ends, for a cutscene between two scenes: switch to it, and switch on from done.
//
//	intro, err := player.Scene(c, func(cutscene.Result) { scenes.Switch(level1, fade) })
//	if err == nil {
//		scenes.Switch(intro, nil)
//	}
//
// Leaving the scene before the cutscene ends stops it without calling done, also when the scene switched to is another cutscene scene of the same player entered with a transition, which enters before the scene it replaces exits. It returns an error for a cutscene Play would refuse.
func (p *Player) Scene(c Cutscene, done func(Result)) (*scene.Controller, error) {
	if err := check(c); err != nil {
		return nil, err
	}
	var pb *playback
	ctrl := &scene.Controller{}
	ctrl.Enter = func(*core.Scope) {
		// A cutscene another cutscene scene played belongs to a scene being left, whose Exit comes only after a transition, so it stops quietly here as that Exit would stop it.
		if cur := p.cur.GetUntracked(); cur != nil && cur.owner != nil {
			p.end(Stopped, nil, false)
		}
		pb, _ = p.play(c, done, ctrl)
	}
	ctrl.Exit = func() {
		if pb != nil && p.cur.GetUntracked() == pb {
			p.end(Stopped, nil, false)
		}
		pb = nil
	}
	return ctrl, nil
}

// Playing reports whether a cutscene plays. Read in a computation, it subscribes it to changes, as a core.Signal does.
func (p *Player) Playing() bool { return p.cur.Get() != nil }

// CanSkip reports whether the cutscene playing can be skipped now: it is Skippable and played past SkipAfter. Read in a computation, it subscribes it to changes, so a game can show its own skip prompt over the video only while a skip would work.
func (p *Player) CanSkip() bool {
	pb := p.cur.Get()
	return pb != nil && pb.skippable.Get()
}

// Skip ends the cutscene playing with Skipped, as the player's skip input does, and reports whether it did: not while none plays or it cannot be skipped yet (CanSkip).
func (p *Player) Skip() bool {
	pb := p.cur.GetUntracked()
	if pb == nil || !pb.skippable.GetUntracked() {
		return false
	}
	p.end(Skipped, nil, true)
	return true
}

// Stop ends the cutscene playing with Stopped, skippable or not, and reports whether one played.
func (p *Player) Stop() bool {
	if p.cur.GetUntracked() == nil {
		return false
	}
	p.end(Stopped, nil, true)
	return true
}

// end ends the cutscene playing, if one does, setting the loop and the mixer back and then, when notify is true, calling its done callback.
func (p *Player) end(reason Reason, err error, notify bool) {
	pb := p.cur.GetUntracked()
	if pb == nil {
		return
	}
	p.cur.Set(nil)
	p.restore(pb)
	if notify && pb.done != nil {
		pb.done(Result{Reason: reason, Position: pb.position, Err: err})
	}
}

// restore sets the loop and the mixer back as they were before pb.
func (p *Player) restore(pb *playback) {
	if l := p.opts.Loop; l != nil {
		l.SetPaused(pb.loopPaused)
	}
	if pb.resumeSound != nil {
		pb.resumeSound()
		pb.resumeSound = nil
	}
}

// input is a press, a skip key or back on the cutscene pb: it plays on a video the platform paused, and else skips it if it can be.
func (p *Player) input(pb *playback) {
	if p.cur.GetUntracked() != pb {
		return
	}
	if pb.held.GetUntracked() {
		pb.held.Set(false)
		return
	}
	p.Skip()
}

// View is the cutscene playing, over the whole of the view it is in, and nothing while none plays. Mount it once, over the game's view.
func (p *Player) View() core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		pb := p.cur.Get()
		if pb == nil {
			return nil
		}
		return p.video(cx, pb)
	})
}

// video is pb's view, built under cx, which lives as long as pb plays: its key bindings, back handler and keep-awake hold go with it.
func (p *Player) video(cx *core.Scope, pb *playback) core.Node {
	keys := p.opts.SkipKeys
	if keys == nil {
		keys = []string{"enter", "space"}
	}
	for _, k := range keys {
		cx.OnKeyWith(k, core.KeyOptions{When: core.KeyNotEditing}, func() { p.input(pb) })
	}
	// Back is taken whether or not the cutscene can be skipped yet, so it does not navigate the game away under the video.
	cx.OnBackPress(func() bool {
		p.input(pb)
		return true
	})
	// A video the platform paused for an interruption as the app went to the background plays on as it returns from the background, and only then: a window losing focus (LifecycleInactive) leaves a video paused for headphones unplugged paused.
	wasBackground := false
	cx.Effect(func() {
		background := cx.Env().Lifecycle() == core.LifecycleBackground
		if wasBackground && !background {
			pb.held.Set(false)
		}
		wasBackground = background
	})
	cx.KeepAwake("cutscene", core.KeepAwakeOptions{Display: true})

	var volume func() float32
	if m := p.opts.Mixer; m != nil {
		volume = func() float32 {
			v := m.Master()
			if pb.c.Group != "" {
				v *= m.Volume(pb.c.Group)
			}
			return v
		}
	} else {
		volume = func() float32 { return 1 }
	}
	class := "absolute inset-0"
	if !pb.c.Transparent {
		class += " bg-black"
	}
	testID := p.opts.TestID
	if testID == "" {
		testID = "cutscene"
	}
	return core.View{
		Class: class,
		Children: []core.Node{core.Video{
			Source:     pb.c.Source,
			ResizeMode: pb.c.Fit.resizeMode(),
			Class:      "absolute inset-0",
			TestID:     testID,
			PausedFunc: func() bool {
				return cx.Env().Lifecycle() == core.LifecycleBackground || pb.held.Get()
			},
			// core.Video plays a Volume of 0 at full volume, so a silent cutscene is muted instead.
			VolumeFunc: volume,
			MutedFunc:  func() bool { return volume() <= 0 },
			OnProgress: func(position, _ time.Duration) {
				pb.position = position
				if pb.c.Skippable && position >= time.Duration(pb.c.SkipAfter) {
					pb.skippable.Set(true)
				}
			},
			OnEnd: func() {
				if p.cur.GetUntracked() == pb {
					p.end(Finished, nil, true)
				}
			},
			OnError: func(err error) {
				if p.cur.GetUntracked() == pb {
					p.end(Failed, err, true)
				}
			},
			OnPause: func(core.VideoPauseReason) { pb.held.Set(true) },
			OnPlay:  func() { pb.held.Set(false) },
			OnPress: func(core.PressEvent) { p.input(pb) },
		}},
	}
}
