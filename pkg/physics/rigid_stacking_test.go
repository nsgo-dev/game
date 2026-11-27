package physics_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
)

func TestRigidColumn20StandsTwentySeconds(t *testing.T) {
	for _, gravity := range []float32{600, 980} {
		for _, disableSleep := range []bool{false, true} {
			name := "sleep"
			if disableSleep {
				name = "no_sleep"
			}
			if gravity == 980 {
				name = "g980/" + name
			} else {
				name = "g600/" + name
			}
			t.Run(name, func(t *testing.T) {
				w := &physics.RigidWorld{GravityY: gravity, DisableSleep: disableSleep}
				const (
					n    = 20
					size = 20
				)
				w.AddBody(physics.NewStaticRigidBox(200, 610, 800, 20)) // top at y = 600
				boxes := make([]*physics.RigidBody, n)
				for i := range n {
					y := float32(600 - size*0.5 - float32(i)*size)
					b := physics.NewRigidBox(200, y, size, size, 1)
					b.Friction = 0.4
					boxes[i] = b
					w.AddBody(b)
				}
				top := boxes[n-1]
				startX, startY := top.X, top.Y

				for range 20 * 60 {
					w.Step(step)
				}

				dx := math.Abs(float64(top.X - startX))
				dy := math.Abs(float64(top.Y - startY))
				if dx > 1 || dy > 1 {
					t.Fatalf("20-box column top drifted by (%.4f, %.4f) px to (%v, %v), want within 1 px of (%v, %v)", dx, dy, top.X, top.Y, startX, startY)
				}
				if !disableSleep && !allSleeping(boxes) {
					t.Fatalf("20-box column did not fall asleep within 20 s")
				}
			})
		}
	}
}

func TestRigidPyramid20BaseSleepsWithinFiveSeconds(t *testing.T) {
	for _, disableSleep := range []bool{false, true} {
		name := "sleep"
		if disableSleep {
			name = "no_sleep"
		}
		t.Run(name, func(t *testing.T) {
			w := &physics.RigidWorld{GravityY: 600, DisableSleep: disableSleep}
			const (
				base = 20
				size = 20
			)
			w.AddBody(physics.NewStaticRigidBox(500, 610, 1200, 20)) // top at y = 600
			var boxes []*physics.RigidBody
			var startX, startY []float32
			for row := range base {
				count := base - row
				y := float32(600 - size*0.5 - float32(row)*size)
				x0 := 500 - float32(count-1)*size*0.5
				for i := range count {
					x := x0 + float32(i)*size
					b := physics.NewRigidBox(x, y, size, size, 1)
					b.Friction = 0.4
					boxes = append(boxes, b)
					startX = append(startX, x)
					startY = append(startY, y)
					w.AddBody(b)
				}
			}

			sleptAt := -1
			for s := range 5 * 60 {
				w.Step(step)
				if !disableSleep && sleptAt < 0 && allSleeping(boxes) {
					sleptAt = s + 1
					break
				}
			}
			if !disableSleep && sleptAt < 0 {
				awake := 0
				for _, b := range boxes {
					if !b.Sleeping() {
						awake++
					}
				}
				t.Fatalf("20-base pyramid (%d boxes) still has %d awake boxes after 5 s", len(boxes), awake)
			}
			for i, b := range boxes {
				dx := math.Abs(float64(b.X - startX[i]))
				dy := math.Abs(float64(b.Y - startY[i]))
				if dx > 5 || dy > 1 {
					t.Fatalf("box %d drifted by (%.3f, %.3f) px to (%v, %v), started at (%v, %v)", i, dx, dy, b.X, b.Y, startX[i], startY[i])
				}
			}
		})
	}
}

func TestRigidHeavyOnLightMassRatios(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ratio float32
	}{
		{"100:1", 100},
		{"1000:1", 1000},
	} {
		for _, disableSleep := range []bool{false, true} {
			mode := "sleep"
			if disableSleep {
				mode = "no_sleep"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				w := &physics.RigidWorld{GravityY: 600, DisableSleep: disableSleep}
				const size = 20
				w.AddBody(physics.NewStaticRigidBox(200, 410, 600, 20)) // top at y = 400
				light := physics.NewRigidBox(200, 390, size, size, 1)
				heavy := physics.NewRigidBox(200, 370, size, size, tc.ratio)
				light.Friction = 0.4
				heavy.Friction = 0.4
				w.AddBody(light, heavy)

				maxFloorOverlap := 0.0
				maxPairOverlap := 0.0
				for range 5 * 60 {
					w.Step(step)
					floorOverlap := float64(light.Y+size*0.5) - 400.0
					pairOverlap := float64(heavy.Y+size*0.5) - float64(light.Y-size*0.5)
					if floorOverlap > maxFloorOverlap {
						maxFloorOverlap = floorOverlap
					}
					if pairOverlap > maxPairOverlap {
						maxPairOverlap = pairOverlap
					}
				}
				finalFloorOverlap := float64(light.Y+size*0.5) - 400.0
				finalPairOverlap := float64(heavy.Y+size*0.5) - float64(light.Y-size*0.5)
				if finalFloorOverlap >= 1.0 || finalPairOverlap >= 1.0 {
					t.Fatalf("overlap >= 1 px: floor=%.4f pair=%.4f (max floor=%.4f pair=%.4f)",
						finalFloorOverlap, finalPairOverlap, maxFloorOverlap, maxPairOverlap)
				}
				if maxFloorOverlap >= 1.0 || maxPairOverlap >= 1.0 {
					t.Fatalf("transient overlap >= 1 px: max floor=%.4f pair=%.4f", maxFloorOverlap, maxPairOverlap)
				}
			})
		}
	}
}

func TestRigidSoftPushOutTurnsOffCenterBody(t *testing.T) {
	// A box overlapping a ledge only on one side must rotate as soft contact push-out acts at the contact point, rather than only translating upward.
	w := &physics.RigidWorld{GravityY: 0, DisableSleep: true}
	ledge := physics.NewStaticRigidBox(100, 210, 40, 20) // x in [80, 120], top at y = 200
	plank := physics.NewRigidBox(130, 198, 60, 20, 1)    // x in [100, 160], bottom at y = 208 (overlaps ledge on left side only)
	w.AddBody(ledge, plank)

	for range 10 {
		w.Step(step)
	}
	if !(plank.Angle > 0.01) {
		t.Fatalf("off-center overlapping plank angle = %v, want positive clockwise rotation from contact-point push-out", plank.Angle)
	}
}

func TestRigidSubstepsSnapshotRoundTrip(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600, Substeps: 6}
	w.AddBody(
		physics.NewStaticRigidBox(200, 410, 400, 20),
		physics.NewRigidBox(200, 380, 20, 20, 1),
		physics.NewRigidBox(202, 355, 20, 20, 1),
	)
	for range 30 {
		w.Step(step)
	}
	raw, err := json.Marshal(w.Save())
	if err != nil {
		t.Fatal(err)
	}
	var snap physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Substeps != 6 {
		t.Fatalf("snap.Substeps = %d, want 6", snap.Substeps)
	}
	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if fresh.Substeps != 6 {
		t.Fatalf("restored Substeps = %d, want 6", fresh.Substeps)
	}
	if fresh.Checksum() != w.Checksum() {
		t.Fatalf("restored checksum %#x != original %#x", fresh.Checksum(), w.Checksum())
	}
	for range 60 {
		w.Step(step)
		fresh.Step(step)
	}
	if fresh.Checksum() != w.Checksum() {
		t.Fatalf("continued checksum %#x != original %#x", fresh.Checksum(), w.Checksum())
	}
}
