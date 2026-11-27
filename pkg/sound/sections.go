package sound

import (
	"time"

	"github.com/nsgo-dev/nsgo/audio"
)

// SectionedTrack pairs an intro section played once with a loop section that repeats indefinitely after the intro ends.
type SectionedTrack struct {
	Intro audio.Source
	Loop  audio.Source
}

// PlayWithIntro plays track's intro once and hands off immediately to its looping section, fading in over fade as any previous music fades out.
func (mu *Music) PlayWithIntro(track SectionedTrack, fade time.Duration) {
	mu.PlaySections(track.Intro, track.Loop, fade)
}

// PlaySections plays intro once without looping, pre-arms loop with looping enabled, and hands off to loop as soon as intro ends, fading in over fade as any previous music fades out. Stopping or starting another track before intro ends cancels loop so it never plays.
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
		mu.playSingle(loop, true, fade)
		return
	}
	if !hasLoop {
		mu.playSingle(intro, false, fade)
		return
	}

	var introTrack, loopTrack *track
	mu.m.cx.Run(func() {
		loopTrack = &track{}
		endLoop := func() { loopTrack.playing, loopTrack.done = false, true }
		loopTrack.p = audio.NewPlayer(mu.m.cx, loop, audio.PlayerOptions{
			OnEnd:   endLoop,
			OnError: func(error) { endLoop() },
		})

		introTrack = &track{next: loopTrack}
		introTrack.p = audio.NewPlayer(mu.m.cx, intro, audio.PlayerOptions{
			OnEnd: func() {
				introTrack.playing, introTrack.done = false, true
				mu.handoff(introTrack)
			},
			OnError: func(error) {
				introTrack.playing, introTrack.done = false, true
				introTrack.cancelNext()
			},
		})
	})
	loopTrack.p.SetLoop(true)
	introTrack.p.SetLoop(false)
	mu.fadeTo(introTrack, fade)
}

func (mu *Music) playSingle(src audio.Source, loop bool, fade time.Duration) {
	var t *track
	mu.m.cx.Run(func() {
		t = &track{}
		end := func() { t.playing, t.done = false, true }
		t.p = audio.NewPlayer(mu.m.cx, src, audio.PlayerOptions{
			OnEnd:   end,
			OnError: func(error) { end() },
		})
	})
	t.p.SetLoop(loop)
	mu.fadeTo(t, fade)
}

// handoff replaces intro with its armed loop track if intro is still current, or closes the loop track if intro was stopped or superseded.
func (mu *Music) handoff(intro *track) {
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
	intro.p.Close()
	mu.apply()
}

// cancelNext closes and detaches an armed continuation track so it never starts.
func (t *track) cancelNext() {
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
