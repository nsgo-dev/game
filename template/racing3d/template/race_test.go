package main

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/vehicle"
)

// maxRaceSteps bounds every simulated race: three minutes.
const maxRaceSteps = 180 * stepRate

func loadTestRace(t *testing.T, ghost *vehicle.Ghost) *Race {
	t.Helper()
	track, cfg, err := LoadRace()
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRace(track, cfg, ghost)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// runRace steps r until it ends, the player driven by drive, and fails t if it runs out of time.
func runRace(t *testing.T, r *Race, drive func() vehicle.Input) {
	t.Helper()
	for i := 0; r.Results() == nil; i++ {
		if i >= maxRaceSteps {
			t.Fatalf("race did not end in %d steps: player lap %d, place %d", maxRaceSteps, r.Lap(), r.Place())
		}
		r.Step(drive(), false)
	}
}

// TestFastPlayerWins drives the player's car with the racing line at full pace: it overtakes both rivals from the back of the grid and wins, every lap counted.
func TestFastPlayerWins(t *testing.T) {
	r := loadTestRace(t, nil)
	pilot := &driver{course: r.course, pace: 1}
	runRace(t, r, func() vehicle.Input { return pilot.drive(r.player.v) })
	res := r.Results()
	if !r.Won() || !res[0].Finished || res[0].Name != playerName {
		t.Fatalf("results %+v, want the player first", res)
	}
	if r.player.racer.Laps != r.track.Laps {
		t.Fatalf("player laps %d, want %d", r.player.racer.Laps, r.track.Laps)
	}
	t.Logf("results %+v", res)
}

// TestIdlePlayerLoses leaves the player on the grid: the race ends when every rival has finished, with the player last and not finished.
func TestIdlePlayerLoses(t *testing.T) {
	r := loadTestRace(t, nil)
	runRace(t, r, func() vehicle.Input { return vehicle.Input{} })
	res := r.Results()
	last := res[len(res)-1]
	if r.Won() || !last.Player || last.Finished || last.Place != len(r.cars) {
		t.Fatalf("results %+v, want the player last and not finished", res)
	}
	for _, rr := range res[:len(res)-1] {
		if !rr.Finished {
			t.Fatalf("rival %s did not finish: %+v", rr.Name, res)
		}
	}
}

// TestGhostRacesFlyingLaps checks the best lap's ghost: lap 1, a standing start from the grid, is never saved, the fastest later lap is, and the next race shows the ghost from its lap 2, starting at the line.
func TestGhostRacesFlyingLaps(t *testing.T) {
	r := loadTestRace(t, nil)
	var saved []*vehicle.Ghost
	r.onBestLap = func(g *vehicle.Ghost) { saved = append(saved, g) }
	pilot := &driver{course: r.course, pace: 1}
	for r.player.racer.Laps < 1 {
		r.Step(pilot.drive(r.player.v), false)
	}
	if r.Ghost() != nil || len(saved) != 0 {
		t.Fatalf("lap 1 from the grid was kept as the ghost")
	}
	runRace(t, r, func() vehicle.Input { return pilot.drive(r.player.v) })
	ghost := r.Ghost()
	if ghost == nil || len(saved) != 1 {
		t.Fatalf("ghost %v after %d laps, %d saved", ghost, r.player.racer.Laps, len(saved))
	}
	if ghost.Name != trackID(r.track) || ghost.Rate != stepRate || ghost.LapSteps != r.player.racer.LastLap {
		t.Fatalf("ghost %q at %d steps a second over %d steps, want %q at %d over lap 2's %d", ghost.Name, ghost.Rate, ghost.LapSteps, trackID(r.track), stepRate, r.player.racer.LastLap)
	}

	next := loadTestRace(t, ghost)
	for next.phase == phaseCountdown {
		next.Step(vehicle.Input{}, false)
	}
	if _, _, shown := next.ghostPose(0); shown {
		t.Fatal("the ghost shows on lap 1")
	}
	pilot = &driver{course: next.course, pace: 1}
	for next.player.racer.Laps < 1 {
		next.Step(pilot.drive(next.player.v), false)
	}
	pos, _, shown := next.ghostPose(0)
	if !shown {
		t.Fatal("the ghost does not show on lap 2")
	}
	line := next.course.checkpoint(0)
	if d := fromPhys(pos).Sub(line).Len(); d > next.track.Width {
		t.Fatalf("the ghost starts lap 2 %v m from the line", d)
	}
}

// TestTrackIDTracksTheLayout checks a ghost's track ID changes with any edit to the track, not only its name.
func TestTrackIDTracksTheLayout(t *testing.T) {
	track, _, err := LoadRace()
	if err != nil {
		t.Fatal(err)
	}
	id := trackID(track)
	moved := *track
	moved.Points = slices.Clone(track.Points)
	moved.Points[0].X++
	if trackID(&moved) == id {
		t.Fatal("moving a point kept the track ID")
	}
	if trackID(track) != id {
		t.Fatal("the track ID is not stable")
	}
}

// TestLargeTrackStaysOnTheGround races on the largest circuit Validate accepts: every car stays on the ground the whole race, and a car pushed off the ground's edge is put back on the road.
func TestLargeTrackStaysOnTheGround(t *testing.T) {
	track, cfg, err := LoadRace()
	if err != nil {
		t.Fatal(err)
	}
	big := *track
	big.Laps = 1
	big.Points = nil
	const n = 16
	for i := range n {
		a := 2 * math.Pi * float64(i) / n
		// Driving order runs toward -Z from the first point, clockwise seen from above.
		big.Points = append(big.Points, Point{X: float32(maxTrackRadius * math.Sin(a)), Z: float32(maxTrackRadius * math.Cos(a))})
	}
	if err := big.Validate(); err != nil {
		t.Fatal(err)
	}
	r, err := newRace(&big, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	pilot := &driver{course: r.course, pace: 1}
	for i := 0; r.Results() == nil; i++ {
		if i >= maxRaceSteps {
			t.Fatalf("race on the large track did not end in %d steps", maxRaceSteps)
		}
		r.Step(pilot.drive(r.player.v), false)
		for _, c := range r.cars {
			if p := c.v.Body.Pos; p.Y < -1 || abs(p.X) > groundHalf || abs(p.Z) > groundHalf {
				t.Fatalf("step %d: %s at %v is off the ground", i, c.name, p)
			}
		}
	}
	if !r.player.racer.Finished {
		t.Fatalf("results %+v, want the player to finish", r.Results())
	}

	// A car beyond the ground's edge falls, and is put back on the road before it has fallen far.
	off := r.cars[0]
	off.v.Reset(physics3d.Vec3{X: groundHalf + 20, Y: 1, Z: 0}, off.v.Body.Rot)
	for range 2 * stepRate {
		r.Step(vehicle.Input{}, false)
	}
	if p := off.v.Body.Pos; p.Y < 0 || abs(p.X) > groundHalf {
		t.Fatalf("a car off the ground's edge is at %v, not back on the road", p)
	}
}

// TestRaceIsDeterministic runs the same race twice and compares every car's final pose and velocities bit for bit.
func TestRaceIsDeterministic(t *testing.T) {
	run := func() []vehicle.Racer {
		r := loadTestRace(t, nil)
		pilot := &driver{course: r.course, pace: 0.95}
		runRace(t, r, func() vehicle.Input { return pilot.drive(r.player.v) })
		var out []vehicle.Racer
		for _, c := range r.cars {
			out = append(out, *c.racer)
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		ba, bb := a[i].Body, b[i].Body
		if ba.Pos != bb.Pos || ba.Rot != bb.Rot || ba.Vel != bb.Vel || ba.AngVel != bb.AngVel || a[i].BestLap != b[i].BestLap || a[i].FinishStep != b[i].FinishStep {
			t.Fatalf("car %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
