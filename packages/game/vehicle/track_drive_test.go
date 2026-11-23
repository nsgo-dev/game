package vehicle_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/vehicle"
)

const maxRaceSteps = 90 * rate

// runReferenceLaps drives the reference car around the test track with the reference driver until it finishes laps, failing t if it leaves the road upside down or runs out of time.
func runReferenceLaps(t *testing.T, laps int) (*testTrack, *driver) {
	t.Helper()
	tt := newTestTrack(laps)
	d := newDriver(tt.car, tt.waypoints)
	for i := 0; !tt.racer.Finished; i++ {
		if i >= maxRaceSteps {
			t.Fatalf("car did not finish %d laps in %d steps: %+v", laps, maxRaceSteps, tt.pose())
		}
		tt.step(d)
		if up := tt.car.Up(); up.Y < 0.95 {
			t.Fatalf("step %d: car tilted to up %+v", i, up)
		}
	}
	return tt, d
}

// TestReferenceCarDrivesTestTrackDeterministically is the issue's acceptance test: the reference car laps the test track, and two runs end in bit-identical poses and lap times.
func TestReferenceCarDrivesTestTrackDeterministically(t *testing.T) {
	a, _ := runReferenceLaps(t, 2)
	b, _ := runReferenceLaps(t, 2)
	if a.pose() != b.pose() {
		t.Fatalf("runs differ:\n%+v\n%+v", a.pose(), b.pose())
	}
	p := a.pose()
	if p.Laps != 2 || !a.racer.Finished {
		t.Fatalf("laps = %d finished = %v", p.Laps, a.racer.Finished)
	}
	// A lap is about 155 m; at 11-20 m/s it takes 8-16 s.
	if p.Best < 8*rate || p.Best > 16*rate {
		t.Fatalf("best lap = %d steps (%v), want 8-16 s", p.Best, time.Duration(p.Best)*dt)
	}
	if p.Finish != a.race.Step() {
		t.Fatalf("finish step %d, race step %d", p.Finish, a.race.Step())
	}
	t.Logf("best lap %v, finish %v", time.Duration(p.Best)*dt, time.Duration(p.Finish)*dt)
}

// TestRecordedDriveReplays records the reference driver's inputs with replay.Recorder, round-trips the recording through its file format, and plays it back with replay.Player into a fresh world: the replayed car must end bit-identical to the recorded one.
func TestRecordedDriveReplays(t *testing.T) {
	controls := vehicle.DefaultControls()
	tt := newTestTrack(1)
	rec := replay.NewRecorder(newDriver(tt.car, tt.waypoints), 7, rate, controls.Actions(), controls.Axes())
	for i := 0; !tt.racer.Finished; i++ {
		if i >= maxRaceSteps {
			t.Fatal("recorded run did not finish")
		}
		tt.step(rec)
	}
	want := tt.pose()

	data, err := rec.Recording().Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	replay.AssertEndState(t, loaded, want, nil, func(_ uint64, src replay.ActionSource) (func(time.Duration), func() pose) {
		pt := newTestTrack(1)
		return func(time.Duration) { pt.step(src) }, pt.pose
	})
}

// TestBestLapGhost records a ghost per lap, keeps the fastest, and checks it round-trips through YAML and replays the lap's poses.
func TestBestLapGhost(t *testing.T) {
	tt := newTestTrack(2)
	d := newDriver(tt.car, tt.waypoints)
	gr := &vehicle.GhostRecorder{Interval: 1}
	var best *vehicle.Ghost
	bestLap := 0
	var lapStarts []physics3d.Vec3
	record := func() { gr.Record(tt.car.Body.Pos, tt.car.Body.Rot) }
	tt.race.OnLap = func(ev vehicle.LapEvent) {
		record()
		if ev.Best {
			best = gr.Ghost(rate, ev.Steps)
			bestLap = ev.Lap
		}
		gr.Reset()
		lapStarts = append(lapStarts, tt.car.Body.Pos)
	}
	lapStarts = append(lapStarts, tt.car.Body.Pos)
	record()
	for i := 0; !tt.racer.Finished; i++ {
		if i >= maxRaceSteps {
			t.Fatal("ghost run did not finish")
		}
		tt.step(d)
		record()
	}
	if best == nil {
		t.Fatal("no best lap ghost")
	}
	if best.LapSteps != tt.racer.BestLap || best.Steps() != best.LapSteps {
		t.Fatalf("ghost lap steps = %d, frames span %d, best lap %d", best.LapSteps, best.Steps(), tt.racer.BestLap)
	}
	data, err := vehicle.EncodeGhost(best)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := vehicle.ParseGhost(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := vehicle.EncodeGhost(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatal("ghost YAML is not stable across a round trip")
	}
	if parsed.Frames[len(parsed.Frames)-1] != best.Frames[len(best.Frames)-1] || len(parsed.Frames) != len(best.Frames) {
		t.Fatal("ghost frames changed across a round trip")
	}

	// The ghost starts where its lap started and ends at the finish line.
	lap := bestLap - 1
	start, _ := parsed.Pose(0)
	if start != lapStarts[lap] {
		t.Fatalf("ghost start %+v, lap %d started at %+v", start, lap+1, lapStarts[lap])
	}
	end, _ := parsed.Pose(float64(parsed.Steps()) + 10)
	if end != lapStarts[lap+1] {
		t.Fatalf("ghost end %+v, lap %d ended at %+v", end, lap+1, lapStarts[lap+1])
	}
}

func BenchmarkReferenceLap(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		tt := newTestTrack(1)
		d := newDriver(tt.car, tt.waypoints)
		for i := 0; !tt.racer.Finished && i < maxRaceSteps; i++ {
			tt.step(d)
		}
	}
}
