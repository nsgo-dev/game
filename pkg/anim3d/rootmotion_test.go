package anim3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/math3d"
)

// pitchYawClip is a 1 s looping clip whose root yaws by 90 degrees and pitches by 60 degrees per loop, so the order in which step rotations compose is observable.
func pitchYawClip() *anim3d.Clip {
	const steps = 10
	rots := make([]anim3d.QuatKeyframe, steps+1)
	for i := range rots {
		u := float32(i) / steps
		yaw := math3d.QuatFromAxisAngle(math3d.Vec3Up(), u*math.Pi/2)
		pitch := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, u*math.Pi/3)
		rots[i] = anim3d.QuatKeyframe{Time: time.Duration(i) * time.Second / steps, Value: yaw.Mul(pitch).Norm()}
	}
	return &anim3d.Clip{
		Name:       "pitch_yaw",
		Duration:   time.Second,
		Loop:       true,
		JointCount: 1,
		Tracks:     []anim3d.JointTrack{{JointIndex: 0, Rotations: rots}},
	}
}

func TestRootMotionWrapComposesInPlaybackOrder(t *testing.T) {
	clip := pitchYawClip()
	opts := anim3d.RootMotionOptions{}
	start := math3d.Transform{Rot: math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, 0.3), Scale: math3d.Vec3One()}

	stepped := func(cuts ...time.Duration) math3d.Quat {
		w := start
		for i := 0; i+1 < len(cuts); i += 2 {
			w = anim3d.ApplyRootMotion(w, anim3d.ExtractRootMotion(clip, cuts[i], cuts[i+1], opts))
		}
		return w.Rot
	}

	cases := []struct {
		name       string
		prev, curr time.Duration
		want       math3d.Quat
	}{
		{"wrapped", 950 * time.Millisecond, 50 * time.Millisecond, stepped(950*time.Millisecond, time.Second, 0, 50*time.Millisecond)},
		{"unwrapped", 950 * time.Millisecond, 1050 * time.Millisecond, stepped(950*time.Millisecond, time.Second, 0, 50*time.Millisecond)},
		{"multi-loop", 950 * time.Millisecond, 3050 * time.Millisecond, stepped(950*time.Millisecond, time.Second, 0, time.Second, 0, time.Second, 0, 50*time.Millisecond)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := anim3d.ApplyRootMotion(start, anim3d.ExtractRootMotion(clip, tc.prev, tc.curr, opts)).Rot
			if !got.ApproxEqual(tc.want, 1e-4) && !got.ApproxEqual(math3d.Quat{X: -tc.want.X, Y: -tc.want.Y, Z: -tc.want.Z, W: -tc.want.W}, 1e-4) {
				t.Fatalf("rotation after one step = %v, want %v (two steps)", got, tc.want)
			}
		})
	}
}

func TestRootMotionWrapYawOnlyMatchesSteppedYaw(t *testing.T) {
	clip := pitchYawClip()
	opts := anim3d.RootMotionOptions{ExtractYawOnly: true}
	one := anim3d.ExtractRootMotion(clip, 950*time.Millisecond, 50*time.Millisecond, opts)
	a := anim3d.ExtractRootMotion(clip, 950*time.Millisecond, time.Second, opts)
	b := anim3d.ExtractRootMotion(clip, 0, 50*time.Millisecond, opts)
	want := a.DeltaRot.Mul(b.DeltaRot).Norm()
	if !one.DeltaRot.ApproxEqual(want, 1e-5) {
		t.Fatalf("yaw-only wrapped DeltaRot = %v, want %v", one.DeltaRot, want)
	}
	if math.Abs(float64(one.DeltaRot.X)) > 1e-6 || math.Abs(float64(one.DeltaRot.Z)) > 1e-6 {
		t.Fatalf("yaw-only DeltaRot has pitch or roll: %v", one.DeltaRot)
	}
}
