package sound_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func namedClips(names ...string) []sound.Clip {
	clips := make([]sound.Clip, len(names))
	for i, name := range names {
		clips[i] = sound.Clip{Name: name}
	}
	return clips
}

func TestCueThreeClipsNoRepeat500PicksAndRanges(t *testing.T) {
	clips := namedClips("step_a", "step_b", "step_c")
	cue := sound.NewCue(12345, sound.CueOptions{
		Clips:     clips,
		PitchMin:  0.9,
		PitchMax:  1.15,
		VolumeMin: 0.7,
		VolumeMax: 0.95,
	})

	if cue.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", cue.Len())
	}
	if cue.LastIndex() != -1 {
		t.Fatalf("initial LastIndex() = %d, want -1", cue.LastIndex())
	}
	if cue.NoRepeatWindow() != 1 {
		t.Fatalf("default NoRepeatWindow() = %d, want 1", cue.NoRepeatWindow())
	}

	var counts [3]int
	prev := -1
	minPitch, maxPitch := math.Inf(1), math.Inf(-1)
	minVol, maxVol := math.Inf(1), math.Inf(-1)

	for i := range 500 {
		p := cue.Pick()
		if !p.Valid() {
			t.Fatalf("pick %d invalid: %+v", i, p)
		}
		if p.Index < 0 || p.Index >= len(clips) {
			t.Fatalf("pick %d index out of range: %d", i, p.Index)
		}
		if p.Clip.Name != clips[p.Index].Name {
			t.Fatalf("pick %d clip name = %q, want %q", i, p.Clip.Name, clips[p.Index].Name)
		}
		if cue.LastIndex() != p.Index {
			t.Fatalf("pick %d LastIndex() = %d, want %d", i, cue.LastIndex(), p.Index)
		}
		if p.Index == prev {
			t.Fatalf("pick %d repeated index %d twice in a row", i, p.Index)
		}
		if p.Pitch < 0.9 || p.Pitch > 1.15 {
			t.Fatalf("pick %d pitch = %v, want in [0.9, 1.15]", i, p.Pitch)
		}
		if p.Volume < 0.7 || p.Volume > 0.95 {
			t.Fatalf("pick %d volume = %v, want in [0.7, 0.95]", i, p.Volume)
		}
		if p.Pitch < minPitch {
			minPitch = p.Pitch
		}
		if p.Pitch > maxPitch {
			maxPitch = p.Pitch
		}
		if p.Volume < minVol {
			minVol = p.Volume
		}
		if p.Volume > maxVol {
			maxVol = p.Volume
		}
		counts[p.Index]++
		prev = p.Index
	}

	if !(minPitch < maxPitch) {
		t.Fatalf("pitch did not vary: min=%v max=%v", minPitch, maxPitch)
	}
	if !(minVol < maxVol) {
		t.Fatalf("volume did not vary: min=%v max=%v", minVol, maxVol)
	}
	for i, n := range counts {
		if n < 100 {
			t.Fatalf("clip %d picked only %d/500 times (counts=%v)", i, n, counts)
		}
	}
}

func TestCueDeterministicSeedAndReset(t *testing.T) {
	opts := sound.CueOptions{
		Clips:     namedClips("a", "b", "c", "d"),
		PitchMin:  0.85,
		PitchMax:  1.2,
		VolumeMin: 0.6,
		VolumeMax: 1.0,
	}

	c1 := sound.NewCue(42, opts)
	c2 := sound.NewCue(42, opts)
	cDiff := sound.NewCue(43, opts)

	const n = 200
	recorded := make([]sound.CuePick, n)
	different := false

	for i := range n {
		p1 := c1.Pick()
		p2 := c2.Pick()
		pd := cDiff.Pick()
		recorded[i] = p1

		if p1.Index != p2.Index ||
			math.Float64bits(p1.Pitch) != math.Float64bits(p2.Pitch) ||
			math.Float64bits(p1.Volume) != math.Float64bits(p2.Volume) {
			t.Fatalf("pick %d mismatch for identical seed: %+v vs %+v", i, p1, p2)
		}
		if pd.Index != p1.Index || math.Float64bits(pd.Pitch) != math.Float64bits(p1.Pitch) {
			different = true
		}
	}
	if !different {
		t.Fatal("different seeds produced identical sequences")
	}

	// Reset reproduces the exact same bit-identical sequence and clears LastIndex.
	c1.Reset(42)
	if c1.LastIndex() != -1 {
		t.Fatalf("LastIndex after Reset = %d, want -1", c1.LastIndex())
	}
	for i := range n {
		p := c1.Pick()
		want := recorded[i]
		if p.Index != want.Index ||
			math.Float64bits(p.Pitch) != math.Float64bits(want.Pitch) ||
			math.Float64bits(p.Volume) != math.Float64bits(want.Volume) {
			t.Fatalf("after Reset pick %d = %+v, want %+v", i, p, want)
		}
	}

	// Seed 0 is also well-seeded via SplitMix64 and replays identically.
	zeroCue := sound.NewCue(0, opts)
	first := make([]sound.CuePick, 50)
	for i := range first {
		first[i] = zeroCue.Pick()
	}
	zeroCue.Reset(0)
	for i := range first {
		got := zeroCue.Pick()
		if got.Index != first[i].Index ||
			math.Float64bits(got.Pitch) != math.Float64bits(first[i].Pitch) ||
			math.Float64bits(got.Volume) != math.Float64bits(first[i].Volume) {
			t.Fatalf("seed 0 replay mismatch at %d: %+v vs %+v", i, got, first[i])
		}
	}
}

func TestCuePluggableRandSource(t *testing.T) {
	clips := namedClips("x", "y", "z")
	c1 := sound.NewCue(0, sound.CueOptions{
		Clips:     clips,
		PitchMin:  0.8,
		PitchMax:  1.2,
		VolumeMin: 0.5,
		VolumeMax: 1.0,
		Source:    rand.NewPCG(7, 13),
	})
	c2 := sound.NewCue(999, sound.CueOptions{
		Clips:     clips,
		PitchMin:  0.8,
		PitchMax:  1.2,
		VolumeMin: 0.5,
		VolumeMax: 1.0,
		Source:    rand.NewPCG(7, 13),
	})

	for i := range 100 {
		p1 := c1.Pick()
		p2 := c2.Pick()
		if p1.Index != p2.Index ||
			math.Float64bits(p1.Pitch) != math.Float64bits(p2.Pitch) ||
			math.Float64bits(p1.Volume) != math.Float64bits(p2.Volume) {
			t.Fatalf("pluggable source pick %d mismatch: %+v vs %+v", i, p1, p2)
		}
	}

	// SetSource replaces the stream; Reset restores the internal seeded stream.
	c1.SetSource(rand.NewPCG(100, 200))
	c2.SetSource(rand.NewPCG(100, 200))
	for i := range 20 {
		p1 := c1.Pick()
		p2 := c2.Pick()
		if p1.Index != p2.Index || math.Float64bits(p1.Pitch) != math.Float64bits(p2.Pitch) {
			t.Fatalf("SetSource pick %d mismatch: %+v vs %+v", i, p1, p2)
		}
	}

	c1.Reset(77)
	c3 := sound.NewCue(77, sound.CueOptions{
		Clips:     clips,
		PitchMin:  0.8,
		PitchMax:  1.2,
		VolumeMin: 0.5,
		VolumeMax: 1.0,
	})
	for i := range 20 {
		p1 := c1.Pick()
		p3 := c3.Pick()
		if p1.Index != p3.Index ||
			math.Float64bits(p1.Pitch) != math.Float64bits(p3.Pitch) ||
			math.Float64bits(p1.Volume) != math.Float64bits(p3.Volume) {
			t.Fatalf("Reset after SetSource pick %d mismatch: %+v vs %+v", i, p1, p3)
		}
	}
}

func TestCueEdgeCasesEmptyOneClipTwoClipsAndWindows(t *testing.T) {
	// Nil cue is safe.
	var nilCue *sound.Cue
	if p := nilCue.Pick(); p.Valid() || p.Index != -1 {
		t.Fatalf("nil Cue.Pick() = %+v, want Index -1", p)
	}
	if p, ok := nilCue.Play(); ok || p.Valid() {
		t.Fatalf("nil Cue.Play() = (%+v, %v), want invalid/false", p, ok)
	}
	if p, pcm := nilCue.PlayPCM(); p.Valid() || pcm.Frames() != 0 {
		t.Fatalf("nil Cue.PlayPCM() = (%+v, frames=%d), want invalid/empty", p, pcm.Frames())
	}
	nilCue.Stop()

	// Empty cue.
	empty := sound.NewCue(1, sound.CueOptions{})
	if empty.Len() != 0 || empty.LastIndex() != -1 || empty.NoRepeatWindow() != 0 {
		t.Fatalf("empty cue state: len=%d last=%d window=%d", empty.Len(), empty.LastIndex(), empty.NoRepeatWindow())
	}
	if p := empty.Pick(); p.Valid() || p.Index != -1 {
		t.Fatalf("empty Pick() = %+v, want Index -1", p)
	}
	if p, ok := empty.Play(); ok || p.Valid() {
		t.Fatalf("empty Play() = (%+v, %v), want invalid/false", p, ok)
	}
	if _, err := empty.Pick().RenderWAV(); err == nil {
		t.Fatal("empty RenderWAV() should return error")
	}

	// 1-clip cue always picks 0 without hanging, still randomizes pitch/volume.
	one := sound.NewCue(2, sound.CueOptions{
		Clips:     namedClips("solo"),
		PitchMin:  0.9,
		PitchMax:  1.1,
		VolumeMin: 0.8,
		VolumeMax: 1.0,
	})
	if one.NoRepeatWindow() != 0 {
		t.Fatalf("1-clip NoRepeatWindow() = %d, want 0", one.NoRepeatWindow())
	}
	for i := range 50 {
		p := one.Pick()
		if !p.Valid() || p.Index != 0 || p.Clip.Name != "solo" {
			t.Fatalf("1-clip pick %d = %+v, want index 0", i, p)
		}
		if p.Pitch < 0.9 || p.Pitch > 1.1 || p.Volume < 0.8 || p.Volume > 1.0 {
			t.Fatalf("1-clip pick %d out of range: pitch=%v vol=%v", i, p.Pitch, p.Volume)
		}
	}

	// 2-clip cue with default no-repeat strictly alternates.
	two := sound.NewCue(3, sound.CueOptions{Clips: namedClips("left", "right")})
	prev := -1
	for i := range 100 {
		p := two.Pick()
		if p.Index == prev {
			t.Fatalf("2-clip cue repeated index %d at pick %d", p.Index, i)
		}
		prev = p.Index
	}

	// AllowRepeat enables repeats on 2-clip cue.
	twoRepeat := sound.NewCue(4, sound.CueOptions{
		Clips:       namedClips("left", "right"),
		AllowRepeat: true,
	})
	if twoRepeat.NoRepeatWindow() != 0 {
		t.Fatalf("AllowRepeat NoRepeatWindow() = %d, want 0", twoRepeat.NoRepeatWindow())
	}
	repeats := 0
	prev = -1
	for range 200 {
		p := twoRepeat.Pick()
		if p.Index == prev {
			repeats++
		}
		prev = p.Index
	}
	if repeats == 0 {
		t.Fatal("AllowRepeat cue never repeated over 200 picks")
	}

	// NoRepeatWindow clamped to len(Clips)-1; window=2 on 3 clips means every 3 consecutive picks are distinct.
	win := sound.NewCue(5, sound.CueOptions{
		Clips:          namedClips("a", "b", "c"),
		NoRepeatWindow: 10,
	})
	if win.NoRepeatWindow() != 2 {
		t.Fatalf("clamped NoRepeatWindow() = %d, want 2", win.NoRepeatWindow())
	}
	var last2 [2]int
	last2[0], last2[1] = -1, -1
	for i := range 150 {
		p := win.Pick()
		if p.Index == last2[0] || p.Index == last2[1] {
			t.Fatalf("window=2 pick %d chose %d which is in recent [%d, %d]", i, p.Index, last2[0], last2[1])
		}
		last2[0], last2[1] = last2[1], p.Index
	}

	// Negative NoRepeatWindow disables repeat avoidance.
	negWin := sound.NewCue(6, sound.CueOptions{
		Clips:          namedClips("a", "b"),
		NoRepeatWindow: -1,
	})
	if negWin.NoRepeatWindow() != 0 {
		t.Fatalf("negative NoRepeatWindow() = %d, want 0", negWin.NoRepeatWindow())
	}

	// Range normalization: swapped, defaults, NaN/Inf, out-of-bounds clamping.
	swapped := sound.NewCue(7, sound.CueOptions{
		Clips:     namedClips("a"),
		PitchMin:  1.3,
		PitchMax:  0.8,
		VolumeMin: 1.5,
		VolumeMax: -0.2,
	})
	pMin, pMax := swapped.PitchRange()
	vMin, vMax := swapped.VolumeRange()
	if pMin != 0.8 || pMax != 1.3 {
		t.Fatalf("swapped PitchRange = [%v, %v], want [0.8, 1.3]", pMin, pMax)
	}
	if vMin != 0 || vMax != 1 {
		t.Fatalf("clamped VolumeRange = [%v, %v], want [0, 1]", vMin, vMax)
	}

	defaults := sound.NewCue(8, sound.CueOptions{
		Clips:     namedClips("a"),
		PitchMin:  math.NaN(),
		PitchMax:  math.Inf(1),
		VolumeMin: math.NaN(),
		VolumeMax: math.Inf(-1),
	})
	pMin, pMax = defaults.PitchRange()
	vMin, vMax = defaults.VolumeRange()
	if pMin != 1 || pMax != 1 || vMin != 1 || vMax != 1 {
		t.Fatalf("NaN/Inf normalized ranges = pitch[%v,%v] vol[%v,%v], want [1,1] [1,1]", pMin, pMax, vMin, vMax)
	}
}

func TestCueWeightsAndNoRepeat(t *testing.T) {
	// Weighted distribution with AllowRepeat: clip 0 has weight 10 vs 1 and 1 (~83.3%).
	wCue := sound.NewCue(101, sound.CueOptions{
		Clips:       namedClips("heavy", "light1", "light2"),
		Weights:     []float64{10, 1, 1},
		AllowRepeat: true,
	})
	var counts [3]int
	for range 2000 {
		counts[wCue.Pick().Index]++
	}
	if counts[0] < 1400 || counts[1] < 50 || counts[2] < 50 {
		t.Fatalf("unexpected weighted counts over 2000 picks: %v", counts)
	}

	// Zero-weight clip is never selected, and remaining positive-weight clips respect no-repeat.
	zCue := sound.NewCue(102, sound.CueOptions{
		Clips:          namedClips("a", "b", "disabled"),
		Weights:        []float64{1, 2, 0},
		NoRepeatWindow: 2,
	})
	prev := -1
	for i := range 200 {
		p := zCue.Pick()
		if p.Index == 2 {
			t.Fatalf("pick %d selected zero-weight clip 2", i)
		}
		if p.Index == prev {
			t.Fatalf("pick %d repeated clip %d despite another positive-weight clip available", i, p.Index)
		}
		prev = p.Index
	}

	// Single positive-weight clip always selects that clip.
	singlePos := sound.NewCue(103, sound.CueOptions{
		Clips:   namedClips("off0", "only", "off2"),
		Weights: []float64{0, 5, -1},
	})
	for i := range 30 {
		if idx := singlePos.Pick().Index; idx != 1 {
			t.Fatalf("single positive weight pick %d = %d, want 1", i, idx)
		}
	}

	// All-zero / invalid weights fall back cleanly to uniform no-repeat selection.
	allZero := sound.NewCue(104, sound.CueOptions{
		Clips:   namedClips("a", "b", "c"),
		Weights: []float64{0, -2, math.NaN()},
	})
	prev = -1
	for i := range 100 {
		p := allZero.Pick()
		if p.Index == prev {
			t.Fatalf("all-zero fallback repeated %d at pick %d", p.Index, i)
		}
		prev = p.Index
	}

	// Short Weights slice defaults missing trailing weights to 1.
	shortW := sound.NewCue(105, sound.CueOptions{
		Clips:       namedClips("heavy", "def1", "def2"),
		Weights:     []float64{8},
		AllowRepeat: true,
	})
	var sc [3]int
	for range 1000 {
		sc[shortW.Pick().Index]++
	}
	if sc[0] < 650 || sc[1] < 30 || sc[2] < 30 {
		t.Fatalf("short Weights counts = %v, want clip 0 dominant and clips 1,2 active", sc)
	}
}

// estimateFreqHz estimates the fundamental frequency of a mono sine signal via upward zero crossings.
func estimateFreqHz(samples []float64, sampleRate int) float64 {
	var crossings []float64
	for i := 1; i < len(samples); i++ {
		if samples[i-1] <= 0 && samples[i] > 0 {
			frac := -samples[i-1] / (samples[i] - samples[i-1])
			crossings = append(crossings, float64(i-1)+frac)
		}
	}
	if len(crossings) < 2 {
		return 0
	}
	avgPeriodSamples := (crossings[len(crossings)-1] - crossings[0]) / float64(len(crossings)-1)
	return float64(sampleRate) / avgPeriodSamples
}

func TestResamplePitchAndApplyGain(t *testing.T) {
	const sr = 44100
	const baseFreq = 440.0
	const inFrames = sr / 5 // 200ms = 8820 frames

	sine := makeSine(baseFreq, sr, inFrames)
	src := sound.PCM{Samples: sine, SampleRate: sr, Channels: 1}

	// Identity pitch returns independent copy.
	id := sound.ResamplePitch(src, 1.0)
	if id.Frames() != inFrames || id.SampleRate != sr || id.Channels != 1 {
		t.Fatalf("identity resample metadata: frames=%d sr=%d ch=%d", id.Frames(), id.SampleRate, id.Channels)
	}
	id.Samples[0] = 999
	if src.Samples[0] == 999 {
		t.Fatal("ResamplePitch(1.0) aliased input slice")
	}

	// Invalid pitches default to 1.0.
	for _, bad := range []float64{0, -1.5, math.NaN(), math.Inf(1)} {
		got := sound.ResamplePitch(src, bad)
		if got.Frames() != inFrames {
			t.Fatalf("ResamplePitch(%v) frames = %d, want %d", bad, got.Frames(), inFrames)
		}
	}

	// Pitch shifts adjust frame count and fundamental frequency proportionally.
	for _, pitch := range []float64{0.5, 0.8, 1.25, 2.0} {
		out := sound.ResamplePitch(src, pitch)
		wantFrames := int(math.Round(float64(inFrames) / pitch))
		if out.Frames() != wantFrames {
			t.Fatalf("pitch=%v frames=%d, want %d", pitch, out.Frames(), wantFrames)
		}
		for i, s := range out.Samples {
			if math.IsNaN(s) || math.IsInf(s, 0) {
				t.Fatalf("pitch=%v invalid sample at %d: %v", pitch, i, s)
			}
		}
		gotFreq := estimateFreqHz(out.Samples, sr)
		wantFreq := baseFreq * pitch
		if math.Abs(gotFreq-wantFreq) > 5.0 {
			t.Fatalf("pitch=%v estimated freq = %.2f Hz, want %.2f Hz", pitch, gotFreq, wantFreq)
		}
	}

	// Stereo channel isolation during resampling.
	stereoSamples := make([]float64, inFrames*2)
	for f := range inFrames {
		stereoSamples[f*2] = sine[f]
		stereoSamples[f*2+1] = 0.25
	}
	stereoOut := sound.ResamplePitch(sound.PCM{Samples: stereoSamples, SampleRate: sr, Channels: 2}, 1.5)
	if stereoOut.Channels != 2 || stereoOut.Frames() != int(math.Round(float64(inFrames)/1.5)) {
		t.Fatalf("stereo resample shape: ch=%d frames=%d", stereoOut.Channels, stereoOut.Frames())
	}
	left := make([]float64, stereoOut.Frames())
	for f := range stereoOut.Frames() {
		left[f] = stereoOut.Samples[f*2]
		if math.Abs(stereoOut.Samples[f*2+1]-0.25) > 1e-9 {
			t.Fatalf("stereo right channel drifted at frame %d: %v", f, stereoOut.Samples[f*2+1])
		}
	}
	if fL := estimateFreqHz(left, sr); math.Abs(fL-baseFreq*1.5) > 5.0 {
		t.Fatalf("stereo left freq = %.2f Hz, want %.2f Hz", fL, baseFreq*1.5)
	}

	// Single-frame and empty PCM edge cases.
	oneFrame := sound.ResamplePitch(sound.PCM{Samples: []float64{0.3, -0.4}, SampleRate: sr, Channels: 2}, 0.5)
	if oneFrame.Frames() != 2 || oneFrame.Samples[2] != 0.3 || oneFrame.Samples[3] != -0.4 {
		t.Fatalf("1-frame resample = %+v", oneFrame)
	}
	if empty := sound.ResamplePitch(sound.PCM{}, 1.2); empty.Frames() != 0 {
		t.Fatalf("empty resample frames = %d, want 0", empty.Frames())
	}

	// ApplyGain scaling, clamping, and non-mutation.
	scaled := sound.ApplyGain(src, 0.5)
	if math.Abs(scaled.Samples[10]-src.Samples[10]*0.5) > 1e-12 {
		t.Fatalf("ApplyGain(0.5) sample mismatch: %v vs %v", scaled.Samples[10], src.Samples[10]*0.5)
	}
	muted := sound.ApplyGain(src, -0.5)
	for i, s := range muted.Samples {
		if s != 0 {
			t.Fatalf("ApplyGain(-0.5) sample %d = %v, want 0", i, s)
		}
	}
	nanGain := sound.ApplyGain(src, math.NaN())
	if nanGain.Samples[10] != src.Samples[10] {
		t.Fatalf("ApplyGain(NaN) altered sample: %v vs %v", nanGain.Samples[10], src.Samples[10])
	}

	// DecodePCM / EncodeWAV / CuePick.RenderWAV round-trip.
	wav, err := src.EncodeWAV()
	if err != nil {
		t.Fatalf("EncodeWAV: %v", err)
	}
	decoded, err := sound.DecodePCM(wav)
	if err != nil {
		t.Fatalf("DecodePCM: %v", err)
	}
	pcmCue := sound.NewCue(99, sound.CueOptions{
		Clips:     []sound.Clip{sound.PCMClip(decoded)},
		PitchMin:  1.2,
		PitchMax:  1.2,
		VolumeMin: 0.5,
		VolumeMax: 0.5,
	})
	pick, rendered := pcmCue.PlayPCM()
	if !pick.Valid() || rendered.Frames() != int(math.Round(float64(inFrames)/1.2)) {
		t.Fatalf("PlayPCM: pick=%+v frames=%d", pick, rendered.Frames())
	}
	if gotRMS, origRMS := rms(rendered.Samples), rms(decoded.Samples); math.Abs(gotRMS/origRMS-0.5) > 0.02 {
		t.Fatalf("PlayPCM RMS ratio = %v, want ~0.5", gotRMS/origRMS)
	}
	if renderedWAV, err := pick.RenderWAV(); err != nil || len(renderedWAV) <= 44 {
		t.Fatalf("RenderWAV: len=%d err=%v", len(renderedWAV), err)
	}
}

func TestCueMixerPlaybackWithNsgotest(t *testing.T) {
	const (
		step0 = "asset:sounds/step0.wav"
		step1 = "asset:sounds/step1.wav"
		step2 = "asset:sounds/step2.wav"
	)

	var (
		mixer  *sound.Mixer
		sounds [3]*audio.Sound
		cue    *sound.Cue
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "variants")
		uris := [3]string{step0, step1, step2}
		loaded := 0
		for i, uri := range uris {
			audio.LoadSound(cx, audio.Source{URI: uri}, func(s *audio.Sound, err error) {
				if err != nil {
					t.Errorf("load %s: %v", uri, err)
					return
				}
				sounds[i] = s
				loaded++
				if loaded == 3 {
					// Include a nil sound to verify Mixer.Cue skips nil entries cleanly.
					cue = mixer.Cue("effects", 2, 2026, sound.CueOptions{
						PitchMin:  0.85,
						PitchMax:  1.2,
						VolumeMin: 0.6,
						VolumeMax: 0.95,
					}, sounds[0], nil, sounds[1], sounds[2])
				}
			})
		}
		return core.View{}
	}, nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			step0: 100 * time.Millisecond,
			step1: 100 * time.Millisecond,
			step2: 100 * time.Millisecond,
		}
	})

	if cue == nil || cue.Len() != 3 {
		t.Fatalf("cue not initialized properly: %v", cue)
	}

	mixer.SetMaster(0.8)
	mixer.SetVolume("effects", 0.5) // effective group gain = 0.4

	prev := -1
	var expectedPlays [3][]audio.Voice
	for i := range 15 {
		pick, ok := mixer.PlayCue(cue)
		if !ok || !pick.Valid() {
			t.Fatalf("PlayCue %d failed: pick=%+v ok=%v", i, pick, ok)
		}
		if pick.Index == prev {
			t.Fatalf("PlayCue %d repeated index %d", i, pick.Index)
		}
		prev = pick.Index
		expectedPlays[pick.Index] = append(expectedPlays[pick.Index], audio.Voice{
			Volume: float32(0.4) * float32(pick.Volume),
			Rate:   float32(pick.Pitch),
		})
		app.Advance(150 * time.Millisecond)
	}

	app.Settle()
	if len(app.Sounds) != 3 {
		t.Fatalf("got %d platform sounds, want 3", len(app.Sounds))
	}
	byURI := map[string]*nsgotest.Sound{}
	for _, s := range app.Sounds {
		byURI[s.Source.URI] = s
	}
	uris := [3]string{step0, step1, step2}
	for idx, uri := range uris {
		got := byURI[uri].Plays
		want := expectedPlays[idx]
		if len(got) != len(want) {
			t.Fatalf("%s play count = %d, want %d", uri, len(got), len(want))
		}
		for j := range want {
			if !near(got[j].Volume, want[j].Volume) || !near(got[j].Rate, want[j].Rate) {
				t.Fatalf("%s play %d = %+v, want %+v", uri, j, got[j], want[j])
			}
		}
	}

	// The voice limit of 2 per clip holds on rapid playback without advancing the clock: each new play stops its clip's oldest, so at most 6 play at once.
	for i := range 12 {
		if _, ok := cue.Play(); !ok {
			t.Fatalf("rapid play %d did not play", i)
		}
	}
	app.Settle()
	for _, uri := range uris {
		live := 0
		for _, p := range byURI[uri].Playbacks {
			if !p.Ended {
				live++
			}
		}
		if live > 2 {
			t.Fatalf("%s has %d playbacks playing, want at most 2", uri, live)
		}
	}

	// Stop calls Stop on all clip effects.
	cue.Stop()
	app.Settle()
	for _, uri := range uris {
		if byURI[uri].Stops < 1 {
			t.Fatalf("%s was not stopped by cue.Stop()", uri)
		}
	}

	// Backgrounding prevents playback.
	app.Advance(200 * time.Millisecond)
	app.SetEnvironment(background)
	app.Settle()
	if _, ok := cue.Play(); ok {
		t.Fatal("cue played while app was in background")
	}
	app.SetEnvironment(playsAudio)
	app.Settle()

	// Group volume 0 prevents playback.
	mixer.SetVolume("effects", 0)
	if _, ok := cue.Play(); ok {
		t.Fatal("cue played while effects group volume was 0")
	}
	mixer.SetVolume("effects", 1)

	// Sampled volume of 0 must not fall through as audio.Voice{Volume: 0} (full volume).
	silentCue := mixer.Cue("effects", 4, 1, sound.CueOptions{
		VolumeMin: -1,
		VolumeMax: -0.5, // clamps to [0, 0]
	}, sounds[0])
	if _, ok := silentCue.Play(); ok {
		t.Fatal("cue with [0, 0] volume range should not play")
	}
}

func TestMixerRenderCuePCMWithDSPAndNsgotestPlayback(t *testing.T) {
	const sr = 44100
	frames := sr / 10 // 100ms

	highWAV, err := sound.EncodeWAV(makeSine(6000, sr, frames), sr, 1)
	if err != nil {
		t.Fatalf("EncodeWAV high: %v", err)
	}
	lowWAV, err := sound.EncodeWAV(makeSine(300, sr, frames), sr, 1)
	if err != nil {
		t.Fatalf("EncodeWAV low: %v", err)
	}
	highPCM, err := sound.DecodePCM(highWAV)
	if err != nil {
		t.Fatalf("DecodePCM high: %v", err)
	}
	lowPCM, err := sound.DecodePCM(lowWAV)
	if err != nil {
		t.Fatalf("DecodePCM low: %v", err)
	}

	m := startMixed(t)
	m.mixer.SetMaster(0.8)
	m.mixer.SetVolume("sfx", 0.5) // effective gain = 0.4
	m.mixer.SetGroupEffects("sfx", sound.LowPass(800, 1/math.Sqrt2))

	cue := sound.NewCue(55, sound.CueOptions{
		Clips: []sound.Clip{
			{Name: "high", PCM: highPCM},
			{Name: "low", PCM: lowPCM},
		},
		PitchMin:  0.9,
		PitchMax:  1.1,
		VolumeMin: 0.8,
		VolumeMax: 0.8,
	})

	// Pick both clips (2-clip no-repeat alternates between high and low).
	var highOut, lowOut sound.PCM
	for range 2 {
		pick, out := m.mixer.RenderCuePCM("sfx", cue)
		if !pick.Valid() {
			t.Fatal("RenderCuePCM returned invalid pick")
		}
		wantFrames := int(math.Round(float64(frames) / pick.Pitch))
		if out.Frames() != wantFrames {
			t.Fatalf("RenderCuePCM(%s) frames = %d, want %d", pick.Clip.Name, out.Frames(), wantFrames)
		}
		if pick.Clip.Name == "high" {
			highOut = out
		} else {
			lowOut = out
		}
	}

	// High-frequency clip is strongly attenuated by LowPass; low-frequency clip passes at ~0.4*0.8 = 0.32x RMS.
	origRMS := rms(lowPCM.Samples[frames/2:])
	highRatio := rms(highOut.Samples[len(highOut.Samples)/2:]) / origRMS
	lowRatio := rms(lowOut.Samples[len(lowOut.Samples)/2:]) / origRMS
	if highRatio > 0.05 {
		t.Fatalf("RenderCuePCM high-tone RMS ratio = %v, want <= 0.05", highRatio)
	}
	if math.Abs(lowRatio-0.32) > 0.04 {
		t.Fatalf("RenderCuePCM low-tone RMS ratio = %v, want ~0.32", lowRatio)
	}

	// Backgrounding silences RenderCuePCM output.
	m.app.SetEnvironment(background)
	m.app.Settle()
	_, silent := m.mixer.RenderCuePCM("sfx", cue)
	if rms(silent.Samples) != 0 {
		t.Fatalf("RenderCuePCM in background had non-zero RMS: %v", rms(silent.Samples))
	}

	// Rendered WAV is playable via audio.LoadSound and audio.NewPlayer in nsgotest.
	renderedWAV, err := lowOut.EncodeWAV()
	if err != nil {
		t.Fatalf("EncodeWAV(lowOut): %v", err)
	}
	var (
		loaded *audio.Sound
		effect *sound.Effect
		player *audio.Player
	)
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mx := sound.NewMixer(cx, "rendered")
		src := audio.Source{Data: renderedWAV}
		audio.LoadSound(cx, src, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("LoadSound rendered WAV: %v", err)
				return
			}
			loaded = s
			effect = mx.Effect(s, "sfx", 2)
		})
		player = audio.NewPlayer(cx, src, audio.PlayerOptions{})
		return core.View{}
	}, nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			string(renderedWAV): 100 * time.Millisecond,
		}
	})
	if loaded == nil || effect == nil {
		t.Fatal("rendered WAV failed to load as audio.Sound")
	}
	if !effect.Play(audio.Voice{}) {
		t.Fatal("rendered WAV effect did not play")
	}
	player.Play(nil)
	app2.Settle()
	if len(app2.Sounds) != 1 || len(app2.Sounds[0].Plays) != 1 {
		t.Fatalf("expected 1 rendered sound play, got %+v", app2.Sounds)
	}
	if len(app2.Players) != 1 || !app2.Players[0].Playing {
		t.Fatalf("expected rendered WAV player to be playing, got %+v", app2.Players)
	}
}
