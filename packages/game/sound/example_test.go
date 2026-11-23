package sound_test

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// tone returns n frames of a mono 440 Hz sine at 8 kHz.
func tone(n int) sound.PCM {
	s := make([]float64, n)
	for i := range s {
		s[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/8000)
	}
	return sound.PCM{Samples: s, SampleRate: 8000, Channels: 1}
}

// A Cue picks one of several variations of a sound with random pitch and volume, never repeating the last pick, so frequent sounds such as footsteps do not grate.
func ExampleCue() {
	cue := sound.NewCue(1, sound.CueOptions{
		Clips: []sound.Clip{
			{Name: "step1", PCM: tone(800)},
			{Name: "step2", PCM: tone(800)},
			{Name: "step3", PCM: tone(800)},
		},
		PitchMin: 0.9, PitchMax: 1.1,
		NoRepeatWindow: 1,
	})
	last := ""
	repeats := 0
	for range 20 {
		p := cue.Pick()
		if p.Clip.Name == last {
			repeats++
		}
		last = p.Clip.Name
		if p.Pitch < 0.9 || p.Pitch > 1.1 {
			fmt.Println("pitch out of range")
		}
	}
	fmt.Println("repeats:", repeats)
	// Output:
	// repeats: 0
}

// EncodeWAV and DecodePCM convert between samples and WAV files, and ResamplePitch shifts a sound's pitch by changing its length.
func ExampleDecodePCM() {
	wav, err := tone(8000).EncodeWAV()
	if err != nil {
		fmt.Println(err)
		return
	}
	pcm, err := sound.DecodePCM(wav)
	fmt.Println(err, pcm.SampleRate, pcm.Channels, pcm.Frames())
	fmt.Println(sound.ResamplePitch(pcm, 2).Frames())
	// Output:
	// <nil> 8000 1 8000
	// 4000
}

// ProcessSamples runs a chain of bus effects over samples, as the Mixer does for a group.
func ExampleProcessSamples() {
	pcm := tone(800)
	peak := func(s []float64) float64 {
		m := 0.0
		for _, v := range s {
			m = max(m, math.Abs(v))
		}
		return m
	}
	before := peak(pcm.Samples)
	sound.ProcessSamples(pcm.Samples, pcm.SampleRate, pcm.Channels, sound.Gain(0.5), sound.LowPass(2000, 0.7))
	fmt.Printf("%.2f -> %.2f\n", before, peak(pcm.Samples))
	// Output:
	// 0.50 -> 0.25
}

// A Mixer keeps group volumes in the app's storage, so the player's settings survive a restart.
func ExampleMixer() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	m := sound.NewMixer(root, "mixer")
	m.SetVolume("music", 0.3)
	m.SetMaster(0.8)

	again := sound.NewMixer(root, "mixer")
	fmt.Println(again.Volume("music"), again.Master(), again.Volume("sfx"))
	// Output:
	// 0.3 0.8 1
}

// Music plays one long track at a time through the mixer's "music" group, crossfading into the next. PlayWithIntro plays a SectionedTrack's intro once and then loops its loop section. Playing needs the platform's audio, so this example is compiled but not run.
func ExampleMusic() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	mixer := sound.NewMixer(root, "mixer")
	music := mixer.Music("music")

	// The title screen's theme loops; the first level's track crossfades in over two seconds.
	music.Loop = true
	music.Play(audio.Source{URI: "asset:music/title.m4a"}, 0)
	music.PlayWithIntro(sound.SectionedTrack{
		Intro: audio.Source{URI: "asset:music/level1-intro.m4a"},
		Loop:  audio.Source{URI: "asset:music/level1-loop.m4a"},
	}, 2*time.Second)

	// Pausing the game fades the music out.
	music.Stop(500 * time.Millisecond)
}
