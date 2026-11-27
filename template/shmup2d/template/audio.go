package main

import (
	"time"

	"github.com/nsgo-dev/game/pkg/placeholder"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// The mixer groups, which game.yaml's audio buses configure.
const (
	sfxGroup   = "sfx"
	musicGroup = "music"
)

// musicFade is how long the music takes to fade in or out.
const musicFade = 400 * time.Millisecond

// cue is how one event sounds: the placeholder sound it plays, at what volume and rate, and how many of it may overlap.
type cue struct {
	sound  string
	volume float32
	rate   float32
	limit  int
}

// cues are the sounds of the world's events, from the placeholder pack. Put your own WAV files under resources/ and load them by "asset:" URI to replace them.
var cues = map[event]cue{
	eventShot:      {"shoot", 0.25, 1.3, 2},
	eventHit:       {"hit", 0.35, 1.4, 3},
	eventExplosion: {"explosion", 0.6, 1, 4},
	eventPowerUp:   {"powerup", 0.8, 1, 1},
	eventBomb:      {"explosion", 1, 0.6, 1},
	eventLifeLost:  {"hit", 1, 0.6, 1},
	eventBossPhase: {"select", 0.8, 0.8, 1},
}

// sounds plays the world's events and the stage music through a mixer whose volumes persist and whose buses game.yaml sets.
type sounds struct {
	mixer   *sound.Mixer
	music   *sound.Music
	track   audio.Source
	effects map[event]*sound.Effect
	// shots counts the ship's volleys, of which every other one is heard, so the guns do not drown out the rest.
	shots int
}

// newSounds loads every cue's sound and the music under cx. A sound that fails to load stays silent; the game plays on without it.
func newSounds(cx *core.Scope) *sounds {
	s := &sounds{mixer: sound.NewMixer(cx, "shmup.sound"), effects: map[event]*sound.Effect{}}
	Project.ConfigureMixer(s.mixer)
	s.music = s.mixer.Music(musicGroup)
	s.music.Loop = true
	s.track, _ = placeholder.Sound("music_loop")
	for ev, c := range cues {
		src, err := placeholder.Sound(c.sound)
		if err != nil {
			continue
		}
		audio.LoadSound(cx, src, func(snd *audio.Sound, err error) {
			if err == nil {
				s.effects[ev] = s.mixer.Effect(snd, sfxGroup, c.limit)
			}
		})
	}
	return s
}

// play plays the sound of each event.
func (s *sounds) play(events []event) {
	for _, ev := range events {
		if ev == eventShot {
			s.shots++
			if s.shots%2 == 0 {
				continue
			}
		}
		if fx := s.effects[ev]; fx != nil {
			c := cues[ev]
			fx.Play(audio.Voice{Volume: c.volume, Rate: c.rate})
		}
	}
}

// startMusic fades the stage music in, and stopMusic fades it out.
func (s *sounds) startMusic() {
	if s.track.Data != nil || s.track.URI != "" {
		s.music.Play(s.track, musicFade)
	}
}

func (s *sounds) stopMusic() { s.music.Stop(musicFade) }

// pause pauses or resumes the gameplay sounds with the loop.
func (s *sounds) pause(paused bool) { s.mixer.SetPaused(paused) }
