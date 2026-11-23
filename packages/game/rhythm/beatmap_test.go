package rhythm_test

import (
	"math"
	"math/big"
	"math/rand/v2"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/rhythm"
)

const sampleMap = `version: 1
title: Opening
offsetMs: 120
tempos:
  - beat: 0
    bpm: 120
  - beat: 62.5
    bpm: 140
  - beat: 128
    bpm: 87.25
meters:
  - beat: 0
    beatsPerBar: 4
  - beat: 64
    beatsPerBar: 3
`

func mustTiming(t testing.TB, text string) *rhythm.Timing {
	t.Helper()
	m, err := rhythm.ParseBeatMap([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tm, err := m.Timing()
	if err != nil {
		t.Fatalf("timing: %v", err)
	}
	return tm
}

func TestBeatMapRoundTripIsLossless(t *testing.T) {
	m, err := rhythm.ParseBeatMap([]byte(sampleMap))
	if err != nil {
		t.Fatal(err)
	}
	out, err := rhythm.EncodeBeatMap(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != sampleMap {
		t.Fatalf("encode changed the file:\n%s\nwant:\n%s", out, sampleMap)
	}
	again, err := rhythm.EncodeBeatMap(m)
	if err != nil || string(again) != string(out) {
		t.Fatalf("a second encode differs: %v\n%s", err, again)
	}
}

func TestBeatMapWritesTheMinimalFields(t *testing.T) {
	out, err := rhythm.EncodeBeatMap(&rhythm.BeatMap{Version: 1, Tempos: []rhythm.Tempo{{BPM: 128}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\ntempos:\n  - beat: 0\n    bpm: 128\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestBeatMapRefusesInvalidFiles(t *testing.T) {
	cases := map[string]string{
		"empty":            "  \n",
		"unknown field":    "version: 1\ntempos: [{beat: 0, bpm: 120}]\nswing: 0.6\n",
		"unknown nested":   "version: 1\ntempos: [{beat: 0, bpm: 120, feel: 1}]\n",
		"second document":  "version: 1\ntempos: [{beat: 0, bpm: 120}]\n---\nversion: 1\n",
		"wrong version":    "version: 2\ntempos: [{beat: 0, bpm: 120}]\n",
		"no version":       "tempos: [{beat: 0, bpm: 120}]\n",
		"no tempo":         "version: 1\n",
		"late first":       "version: 1\ntempos: [{beat: 1, bpm: 120}]\n",
		"out of order":     "version: 1\ntempos: [{beat: 0, bpm: 120}, {beat: 8, bpm: 90}, {beat: 8, bpm: 100}]\n",
		"zero bpm":         "version: 1\ntempos: [{beat: 0, bpm: 0}]\n",
		"too fast":         "version: 1\ntempos: [{beat: 0, bpm: 2001}]\n",
		"nan bpm":          "version: 1\ntempos: [{beat: 0, bpm: .nan}]\n",
		"inf beat":         "version: 1\ntempos: [{beat: 0, bpm: 120}, {beat: .inf, bpm: 90}]\n",
		"far offset":       "version: 1\noffsetMs: 3600001\ntempos: [{beat: 0, bpm: 120}]\n",
		"meter late":       "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 4, beatsPerBar: 4}]\n",
		"meter off bar":    "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 0, beatsPerBar: 4}, {beat: 6, beatsPerBar: 3}]\n",
		"meter zero":       "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 0, beatsPerBar: 0}]\n",
		"meter long":       "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 0, beatsPerBar: 65}]\n",
		"fractional bar":   "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 0.5, beatsPerBar: 4}]\n",
		"not a map":        "- 1\n- 2\n",
		"fractional meter": "version: 1\ntempos: [{beat: 0, bpm: 120}]\nmeters: [{beat: 0, beatsPerBar: 3.5}]\n",
		"float version":    "version: 1.5\ntempos: [{beat: 0, bpm: 120}]\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if m, err := rhythm.ParseBeatMap([]byte(text)); err == nil {
				t.Fatalf("parsed %+v, want an error", m)
			}
		})
	}
	if _, err := rhythm.EncodeBeatMap(nil); err == nil {
		t.Fatal("encoded a nil map")
	}
	if _, err := rhythm.EncodeBeatMap(&rhythm.BeatMap{Version: 1}); err == nil {
		t.Fatal("encoded a map without a tempo")
	}
}

func TestTimingConvertsAcrossTempoChanges(t *testing.T) {
	tm := mustTiming(t, sampleMap)
	// 120 bpm: half a second a beat after 0.12 s.
	if got := tm.Time(0); got != 120*time.Millisecond {
		t.Fatalf("beat 0 at %v, want 120ms", got)
	}
	if got := tm.Time(62.5); got != 120*time.Millisecond+31250*time.Millisecond {
		t.Fatalf("beat 62.5 at %v", got)
	}
	// 140 bpm from 62.5: 65.5 beats later at 60/140 s each.
	want := 0.12 + 62.5*0.5 + 65.5*60.0/140
	if got := tm.Seconds(128); math.Abs(got-want) > 1e-12 {
		t.Fatalf("beat 128 at %v s, want %v", got, want)
	}
	if got := tm.BPM(100); got != 140 {
		t.Fatalf("bpm at 100 = %v", got)
	}
	if got := tm.BPM(-3); got != 120 {
		t.Fatalf("bpm before 0 = %v", got)
	}
	// Before beat 0 the first tempo holds: a beat before 0 is half a second before it.
	if got := tm.Time(-1); got != -380*time.Millisecond {
		t.Fatalf("beat -1 at %v", got)
	}
	for _, beat := range []float64{-4, -0.25, 0, 3.75, 62.5, 64, 100.125, 128, 1000} {
		if got := tm.Beat(tm.Time(beat)); math.Abs(got-beat) > 1e-6 {
			t.Fatalf("Beat(Time(%v)) = %v", beat, got)
		}
	}
}

func TestTimingCountsBarsAcrossMeterChanges(t *testing.T) {
	tm := mustTiming(t, sampleMap)
	cases := []struct {
		beat  float64
		bar   int
		inBar float64
		per   int
	}{
		{0, 0, 0, 4},
		{3.5, 0, 3.5, 4},
		{4, 1, 0, 4},
		{63, 15, 3, 4},
		{64, 16, 0, 3},
		{66.5, 16, 2.5, 3},
		{67, 17, 0, 3},
		{-1, -1, 3, 4},
		{-4.5, -2, 3.5, 4},
	}
	for _, c := range cases {
		bar, in := tm.Bar(c.beat)
		if bar != c.bar || math.Abs(in-c.inBar) > 1e-12 || tm.BeatsPerBar(c.beat) != c.per {
			t.Errorf("beat %v: bar %d +%v of %d, want bar %d +%v of %d", c.beat, bar, in, tm.BeatsPerBar(c.beat), c.bar, c.inBar, c.per)
		}
	}
	plain := mustTiming(t, "version: 1\ntempos: [{beat: 0, bpm: 90}]\n")
	if bar, in := plain.Bar(9); bar != 2 || in != 1 || plain.BeatsPerBar(9) != 4 {
		t.Fatalf("without meters beat 9 is bar %d +%v", bar, in)
	}
}

func TestTimingIsUnaffectedByLaterEdits(t *testing.T) {
	m, err := rhythm.ParseBeatMap([]byte(sampleMap))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := m.Timing()
	if err != nil {
		t.Fatal(err)
	}
	before := tm.Seconds(200)
	m.Tempos[1].BPM = 60
	m.OffsetMs = 0
	if tm.Seconds(200) != before {
		t.Fatal("editing the map changed a timing made from it")
	}
	m.Tempos = nil
	if _, err := m.Timing(); err == nil {
		t.Fatal("made a timing from an invalid map")
	}
}

// reference is a beat map's times computed at 256 bits from the map's float64 values: the reference the float64 Timing is held to.
type reference struct {
	m      *rhythm.BeatMap
	starts []*big.Float
}

const refPrec = 256

func bf(v float64) *big.Float { return new(big.Float).SetPrec(refPrec).SetFloat64(v) }

func newReference(m *rhythm.BeatMap) *reference {
	r := &reference{m: m, starts: make([]*big.Float, len(m.Tempos))}
	r.starts[0] = new(big.Float).Quo(bf(m.OffsetMs), bf(1000))
	for i := 1; i < len(m.Tempos); i++ {
		r.starts[i] = r.span(i-1, m.Tempos[i].Beat)
	}
	return r
}

// span is the time of beat counted at tempo i from its start.
func (r *reference) span(i int, beat float64) *big.Float {
	d := new(big.Float).Sub(bf(beat), bf(r.m.Tempos[i].Beat))
	d.Mul(d, new(big.Float).Quo(bf(60), bf(r.m.Tempos[i].BPM)))
	return d.Add(d, r.starts[i])
}

// seconds is beat's time in seconds.
func (r *reference) seconds(beat float64) float64 {
	i := sort.Search(len(r.m.Tempos), func(i int) bool { return r.m.Tempos[i].Beat > beat }) - 1
	f, _ := r.span(max(i, 0), beat).Float64()
	return f
}

// longMap is a seeded beat map of n tempo changes between 40 and 400 bpm, some between beats, over about n*16 beats.
func longMap(n int) *rhythm.BeatMap {
	rng := rand.New(rand.NewPCG(527, 1))
	m := &rhythm.BeatMap{Version: 1, OffsetMs: -1234.567}
	beat := 0.0
	for range n {
		m.Tempos = append(m.Tempos, rhythm.Tempo{Beat: beat, BPM: 40 + float64(rng.IntN(36000))/100})
		beat += float64(1+rng.IntN(32)) + float64(rng.IntN(4))/4
	}
	return m
}

func TestBeatTimesStayWithinAMillisecondOfTheMap(t *testing.T) {
	m := longMap(4000)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	tm, err := m.Timing()
	if err != nil {
		t.Fatal(err)
	}
	last := m.Tempos[len(m.Tempos)-1].Beat
	rng := rand.New(rand.NewPCG(1, 527))
	beats := []float64{0, last, last + 500}
	for _, tp := range m.Tempos {
		beats = append(beats, tp.Beat, tp.Beat+0.5)
	}
	for range 4000 {
		beats = append(beats, rng.Float64()*(last+100))
	}
	ref := newReference(m)
	worst := 0.0
	for _, beat := range beats {
		want := ref.seconds(beat)
		got := tm.Seconds(beat)
		worst = max(worst, math.Abs(got-want))
		if d := tm.Time(beat) - time.Duration(math.Round(want*1e9)); d > time.Millisecond || d < -time.Millisecond {
			t.Fatalf("beat %v at %v, the map puts it at %v s", beat, tm.Time(beat), want)
		}
	}
	// The float64 error stays far inside the millisecond the acceptance allows, after hours of music.
	if worst > 1e-6 {
		t.Fatalf("worst error %v s over %v s of music", worst, tm.Seconds(last))
	}
	t.Logf("worst error %.3g s over %.0f s of music", worst, tm.Seconds(last))
}

func FuzzParseBeatMap(f *testing.F) {
	f.Add([]byte(sampleMap))
	f.Add([]byte("version: 1\ntempos: [{beat: 0, bpm: 120}]\n"))
	f.Add([]byte("version: 1\ntempos: [{bpm: 1}]\nmeters: []"))
	f.Add([]byte("version: 1\noffsetMs: -50.5\ntempos: [{beat: 0, bpm: 1e-3}, {beat: 0.1, bpm: 2000}]\nmeters: [{beat: 0, beatsPerBar: 1}, {beat: 3, beatsPerBar: 64}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := rhythm.ParseBeatMap(data)
		if err != nil {
			return
		}
		out, err := rhythm.EncodeBeatMap(m)
		if err != nil {
			t.Fatalf("a parsed map does not encode: %v", err)
		}
		back, err := rhythm.ParseBeatMap(out)
		if err != nil {
			t.Fatalf("an encoded map does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(m, back) {
			t.Fatalf("round trip changed the map: %+v -> %+v", m, back)
		}
		again, _ := rhythm.EncodeBeatMap(back)
		if string(again) != string(out) {
			t.Fatalf("encoding is not deterministic:\n%s\n%s", out, again)
		}
		tm, err := m.Timing()
		if err != nil {
			t.Fatalf("a valid map has no timing: %v", err)
		}
		for _, tp := range m.Tempos {
			if s := tm.Seconds(tp.Beat); math.IsNaN(s) || math.IsInf(s, 0) {
				t.Fatalf("beat %v at %v", tp.Beat, s)
			}
		}
	})
}

func BenchmarkTimingBeat(b *testing.B) {
	tm, err := longMap(4000).Timing()
	if err != nil {
		b.Fatal(err)
	}
	end := tm.Time(60000)
	b.ReportAllocs()
	var sink float64
	for i := 0; b.Loop(); i++ {
		sink += tm.Beat(time.Duration(i%1000) * end / 1000)
	}
	_ = sink
}

func BenchmarkTimingTime(b *testing.B) {
	tm, err := longMap(4000).Timing()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	var sink time.Duration
	for i := 0; b.Loop(); i++ {
		sink += tm.Time(float64(i % 60000))
	}
	_ = sink
}

func BenchmarkParseBeatMap(b *testing.B) {
	out, err := rhythm.EncodeBeatMap(longMap(1000))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(out)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rhythm.ParseBeatMap(out); err != nil {
			b.Fatal(err)
		}
	}
}
