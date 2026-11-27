package rhythm_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var epoch = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func TestClockRunsPausesAndResumes(t *testing.T) {
	c := rhythm.NewClock(mustTiming(t, sampleMap))
	if c.Running() || c.Track(epoch) != 0 {
		t.Fatal("a new clock runs")
	}
	c.Start(epoch, 2*time.Second)
	if !c.Running() || c.Track(epoch.Add(1500*time.Millisecond)) != 3500*time.Millisecond {
		t.Fatalf("track %v", c.Track(epoch.Add(1500*time.Millisecond)))
	}
	c.Pause(epoch.Add(time.Second))
	if c.Running() || c.Track(epoch.Add(time.Hour)) != 3*time.Second {
		t.Fatalf("a paused clock moved to %v", c.Track(epoch.Add(time.Hour)))
	}
	c.Resume(epoch.Add(10 * time.Second))
	if got := c.Track(epoch.Add(11 * time.Second)); got != 4*time.Second {
		t.Fatalf("resumed at %v, want 4s", got)
	}
	// Resume while running changes nothing.
	c.Resume(epoch.Add(20 * time.Second))
	if got := c.Track(epoch.Add(11 * time.Second)); got != 4*time.Second {
		t.Fatalf("a second resume moved the clock to %v", got)
	}
}

func TestClockCorrectsByTheDevicesOffsets(t *testing.T) {
	tm := mustTiming(t, sampleMap)
	c := rhythm.NewClock(tm)
	c.SetOffsets(rhythm.Offsets{Audio: 80 * time.Millisecond, Video: 30 * time.Millisecond})
	if c.Offsets().Audio != 80*time.Millisecond || c.Timing() != tm {
		t.Fatal("the clock lost its offsets or timing")
	}
	c.Start(epoch, 0)
	now := epoch.Add(10 * time.Second)
	if c.Position(now) != 9920*time.Millisecond {
		t.Fatalf("position %v, want 9.92s", c.Position(now))
	}
	if c.DrawPosition(now) != 9950*time.Millisecond {
		t.Fatalf("draw position %v, want 9.95s", c.DrawPosition(now))
	}
	if c.Beat(now) != tm.Beat(9920*time.Millisecond) || c.DrawBeat(now) != tm.Beat(9950*time.Millisecond) {
		t.Fatal("beats do not follow the corrected positions")
	}
	// A tap 80ms after the output plays beat 20 is on time for this device.
	hit := c.BeatTime(20).Add(80 * time.Millisecond)
	if j := c.Judge(hit, 20); j != 0 {
		t.Fatalf("an on-time tap judged %v", j)
	}
	if j := c.Judge(hit.Add(-15*time.Millisecond), 20); j != -15*time.Millisecond {
		t.Fatalf("an early tap judged %v", j)
	}
}

func TestClockBeatTimesStayWithinAMillisecondOfTheMap(t *testing.T) {
	m := longMap(500)
	tm, err := m.Timing()
	if err != nil {
		t.Fatal(err)
	}
	c := rhythm.NewClock(tm)
	start := epoch.Add(3 * time.Second)
	c.Start(start, tm.Time(0))
	ref := newReference(m)
	last := m.Tempos[len(m.Tempos)-1].Beat
	for beat := 0.0; beat <= last; beat += 0.75 {
		want := ref.seconds(beat)
		at := c.BeatTime(beat)
		got := at.Sub(start) + tm.Time(0)
		if d := got - time.Duration(math.Round(want*1e9)); d > time.Millisecond || d < -time.Millisecond {
			t.Fatalf("beat %v scheduled at track %v, the map puts it at %v s", beat, got, want)
		}
		if b := c.Beat(at); math.Abs(tm.Seconds(b)-want) > 1e-3 {
			t.Fatalf("the clock hears beat %v at beat %v's time", b, beat)
		}
	}
}

func TestClockSyncFollowsOnlyRealDrift(t *testing.T) {
	c := rhythm.NewClock(mustTiming(t, sampleMap))
	if c.Sync(time.Second, epoch) {
		t.Fatal("a stopped clock synced")
	}
	c.Start(epoch, 0)
	if c.Sync(5*time.Second+29*time.Millisecond, epoch.Add(5*time.Second)) {
		t.Fatal("jitter within the tolerance moved the clock")
	}
	if c.Track(epoch.Add(5*time.Second)) != 5*time.Second {
		t.Fatal("the clock moved")
	}
	if !c.Sync(40*time.Second, epoch.Add(6*time.Second)) || c.Track(epoch.Add(7*time.Second)) != 41*time.Second {
		t.Fatalf("a seek did not move the clock: %v", c.Track(epoch.Add(7*time.Second)))
	}
	c.Tolerance = 5 * time.Millisecond
	if !c.Sync(41*time.Second+6*time.Millisecond, epoch.Add(7*time.Second)) {
		t.Fatal("a tighter tolerance did not sync")
	}
}

// TestClockFollowsAPlayer runs a clock off an engine player: started where Play reports it plays, kept by Sync from OnProgress, through a seek.
func TestClockFollowsAPlayer(t *testing.T) {
	const track = "asset:music/opening.m4a"
	tm := mustTiming(t, sampleMap)
	clock := rhythm.NewClock(tm)
	var player *audio.Player
	var cx *core.Scope
	var reports, syncs int
	var worst time.Duration
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		player = audio.NewPlayer(cx, audio.Source{URI: track}, audio.PlayerOptions{
			ProgressInterval: 100 * time.Millisecond,
			OnProgress: func(pos, _ time.Duration) {
				now := audio.Now(cx)
				reports++
				if clock.Sync(pos, now) {
					syncs++
				}
				worst = max(worst, (clock.Track(now) - pos).Abs())
			},
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{track: 3 * time.Minute}
	})
	player.Play(func(err error) {
		if err != nil {
			t.Errorf("play: %v", err)
			return
		}
		clock.Start(audio.Now(cx), player.Position())
	})
	app.Settle()
	if !clock.Running() {
		t.Fatal("the clock did not start with the player")
	}
	for range 300 {
		app.Advance(100 * time.Millisecond)
	}
	if reports < 250 || syncs != 0 || worst > time.Millisecond {
		t.Fatalf("%d reports, %d syncs, worst drift %v", reports, syncs, worst)
	}
	now := audio.Now(cx)
	if d := clock.Track(now) - player.Position(); d.Abs() > time.Millisecond {
		t.Fatalf("clock %v, player %v", clock.Track(now), player.Position())
	}
	if b, want := clock.Beat(now), tm.Beat(player.Position()); math.Abs(tm.Seconds(b)-tm.Seconds(want)) > 1e-3 {
		t.Fatalf("clock beat %v, player beat %v", b, want)
	}
	player.Seek(90 * time.Second)
	app.Advance(100 * time.Millisecond)
	if syncs != 1 {
		t.Fatalf("%d syncs after a seek, want 1", syncs)
	}
	if d := clock.Track(audio.Now(cx)) - player.Position(); d.Abs() > time.Millisecond {
		t.Fatalf("after the seek: clock %v, player %v", clock.Track(audio.Now(cx)), player.Position())
	}
}

func BenchmarkClockBeat(b *testing.B) {
	tm, err := longMap(4000).Timing()
	if err != nil {
		b.Fatal(err)
	}
	c := rhythm.NewClock(tm)
	c.Start(epoch, 0)
	b.ReportAllocs()
	var sink float64
	for i := 0; b.Loop(); i++ {
		sink += c.Beat(epoch.Add(time.Duration(i%100000) * time.Millisecond))
	}
	_ = sink
}
