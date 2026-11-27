package sound

import (
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
)

const (
	// sectionProgressInterval is how often PlaySections polls an intro player's position to schedule its handoff ahead of OnEnd. NSGo clamps ProgressInterval to at least 16 ms.
	sectionProgressInterval = 16 * time.Millisecond
	// sectionHandoffLead starts a preloaded two-file loop slightly ahead of the intro's known end so playback does not wait a full engine turn after OnEnd.
	sectionHandoffLead = 20 * time.Millisecond
)

// SectionedTrack pairs an intro section played once with a loop section that repeats indefinitely after the intro ends.
//
// Prefer Music.PlayLooped with a single audio file and a loop point when you control the asset: two-file playback coordinates two audio.Players across an engine turn and may still leave a slight seam depending on the backend and load.
type SectionedTrack struct {
	Intro audio.Source
	Loop  audio.Source
}

// PlayLooped plays src from the beginning on the first pass and loops back to loopStart each time it reaches its end, fading in over fade as any previous music fades out. It opens one audio.Player with audio.PlayerOptions.LoopStart so the loop point runs inside the native player with no two-player handoff seam.
//
// Playback always loops regardless of mu.Loop, without changing mu.Loop for subsequent Play calls. A loopStart <= 0 (or >= the track's duration) loops the full track from the start. If src is empty, PlayLooped stops the music over fade.
//
// As with Play, a src holding in-memory 16-bit PCM WAV data is processed through the music's group bus effect chain (SetGroupEffects) before playing.
func (mu *Music) PlayLooped(src audio.Source, loopStart, fade time.Duration) {
	if sourceEmpty(src) {
		mu.Stop(fade)
		return
	}
	src = mu.m.process(mu.Group, src)
	mu.playSingle(src, true, max(0, loopStart), fade)
}

// PlayWithIntro plays track's intro once and hands off to its looping section, fading in over fade as any previous music fades out.
//
// Prefer PlayLooped with a single file and a loop point for a sample-accurate loop seam; PlayWithIntro is for assets split across two files.
func (mu *Music) PlayWithIntro(track SectionedTrack, fade time.Duration) {
	mu.PlaySections(track.Intro, track.Loop, fade)
}

// PlaySections plays intro once without looping, preloads loop with looping enabled, and hands off to loop at the end of intro, fading in over fade as any previous music fades out. When the backend reports intro's duration and progress, loop is scheduled slightly ahead of intro's end rather than waiting for OnEnd, with OnEnd kept as a fallback when duration is unknown; intro finishes its remaining samples before closing. Because this coordinates two audio.Players across the engine turn, it may still leave a slight seam under load or on the web; prefer PlayLooped with a single file and a loop point where possible. Stopping or starting another track before intro ends cancels loop so it never plays.
//
// If intro is empty, loop plays directly with looping enabled; if loop is empty, intro plays once without looping; if both are empty, PlaySections stops the music over fade.
//
// Each source is processed through the music's group bus effect chain (SetGroupEffects) before playing, as Play does.
func (mu *Music) PlaySections(intro, loop audio.Source, fade time.Duration) {
	intro = mu.m.process(mu.Group, intro)
	loop = mu.m.process(mu.Group, loop)
	hasIntro := !sourceEmpty(intro)
	hasLoop := !sourceEmpty(loop)
	if !hasIntro && !hasLoop {
		mu.Stop(fade)
		return
	}
	if !hasIntro {
		mu.playSingle(loop, true, 0, fade)
		return
	}
	if !hasLoop {
		mu.playSingle(intro, false, 0, fade)
		return
	}

	var introTrack, loopTrack *track
	mu.m.cx.Run(func() {
		loopTrack = &track{}
		endLoop := func() {
			loopTrack.playing, loopTrack.blocked, loopTrack.done = false, false, true
			mu.syncBlocked()
			mu.m.syncBusDucking()
		}
		loopTrack.p = audio.NewPlayer(mu.m.cx, loop, audio.PlayerOptions{
			OnEnd: endLoop,
			OnError: func(err error) {
				mu.handleError(loopTrack, err, false)
			},
		})

		introTrack = &track{next: loopTrack}
		introTrack.p = audio.NewPlayer(mu.m.cx, intro, audio.PlayerOptions{
			ProgressInterval: sectionProgressInterval,
			OnLoad: func(dur time.Duration) {
				mu.scheduleHandoff(introTrack, introTrack.p.Position(), dur)
			},
			OnProgress: func(pos, dur time.Duration) {
				mu.scheduleHandoff(introTrack, pos, dur)
			},
			OnBuffer: func(buffering bool) {
				if buffering {
					introTrack.cancelHandoff()
					return
				}
				mu.scheduleHandoff(introTrack, introTrack.p.Position(), introTrack.p.Duration())
			},
			OnEnd: func() {
				introTrack.playing, introTrack.blocked, introTrack.done = false, false, true
				introTrack.cancelHandoff()
				if introTrack.next != nil {
					mu.handoff(introTrack)
				} else {
					mu.finishPrev(introTrack)
					mu.syncBlocked()
					mu.m.syncBusDucking()
				}
			},
			OnError: func(err error) {
				mu.handleError(introTrack, err, true)
			},
		})
	})
	loopTrack.p.SetLoop(true)
	introTrack.p.SetLoop(false)
	mu.fadeTo(introTrack, fade)
}

func (mu *Music) playSingle(src audio.Source, loop bool, loopStart, fade time.Duration) {
	var t *track
	mu.m.cx.Run(func() {
		t = &track{}
		end := func() {
			t.playing, t.blocked, t.done = false, false, true
			mu.syncBlocked()
			mu.m.syncBusDucking()
		}
		t.p = audio.NewPlayer(mu.m.cx, src, audio.PlayerOptions{
			LoopStart: max(0, loopStart),
			OnEnd:     end,
			OnError: func(err error) {
				mu.handleError(t, err, false)
			},
		})
	})
	t.p.SetLoop(loop)
	mu.fadeTo(t, fade)
}

// scheduleHandoff arms (or tightens) a timer to start intro's armed loop slightly ahead of intro's known end.
func (mu *Music) scheduleHandoff(intro *track, pos, dur time.Duration) {
	if dur <= 0 || intro.done || !intro.playing || mu.current != intro || intro.next == nil {
		return
	}
	lead := min(sectionHandoffLead, dur/2)
	delay := dur - pos - lead
	if delay <= 0 {
		intro.cancelHandoff()
		mu.handoff(intro)
		return
	}
	intro.cancelHandoff()
	mu.m.cx.Run(func() {
		intro.stopHandoff = mu.m.cx.AfterFunc(delay, func() {
			intro.stopHandoff = nil
			if !intro.playing || intro.done {
				return
			}
			mu.handoff(intro)
		})
	})
}

// handoff replaces intro with its armed loop track if intro is still current, or closes the loop track if intro was stopped or superseded.
func (mu *Music) handoff(intro *track) {
	intro.cancelHandoff()
	next := intro.next
	intro.next = nil
	if next == nil {
		return
	}
	if mu.current != intro {
		next.p.Close()
		next.done = true
		return
	}
	next.level = intro.level
	mu.current = next
	mu.syncBlocked()
	if intro.done {
		intro.p.Close()
	} else {
		// Early handoff: keep intro alive for its remaining samples so OnEnd closes it cleanly, while linking it to next so volume, pause and Stop affect both.
		next.prev = intro
	}
	mu.apply()
}

// finishPrev closes an intro track that already handed off early and detaches it from whichever track still references it.
func (mu *Music) finishPrev(intro *track) {
	detached := false
	if mu.current != nil && mu.current.prev == intro {
		mu.current.prev = nil
		detached = true
	}
	for _, o := range mu.out {
		if o.prev == intro {
			o.prev = nil
			detached = true
		}
	}
	if detached || slices.Contains(mu.out, intro) {
		intro.p.Close()
	}
}

// cancelHandoff stops any armed pre-end handoff timer on t.
func (t *track) cancelHandoff() {
	if t.stopHandoff != nil {
		t.stopHandoff()
		t.stopHandoff = nil
	}
}

// cancelNext closes and detaches an armed continuation track so it never starts.
func (t *track) cancelNext() {
	t.cancelHandoff()
	if t.next == nil {
		return
	}
	t.next.p.Close()
	t.next.done = true
	t.next = nil
}

func sourceEmpty(src audio.Source) bool {
	return src.URI == "" && len(src.Data) == 0
}
