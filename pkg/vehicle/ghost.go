package vehicle

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

// GhostVersion is the Ghost schema version written by EncodeGhost and accepted by ParseGhost.
const GhostVersion = 1

// maxGhostFrames bounds a decoded Ghost: an hour of racing sampled every step at 60 Hz.
const maxGhostFrames = 60 * 60 * 60

// Ghost is a recorded lap: the chassis pose sampled every Interval steps, played back as a translucent rival. It is pose data, so playing it needs no physics and costs nothing per frame beyond drawing.
type Ghost struct {
	// Version is the schema version; it must equal GhostVersion.
	Version int `yaml:"version"`
	// Name optionally labels the ghost, such as the racer or track.
	Name string `yaml:"name,omitempty"`
	// Rate is the fixed step rate the ghost was recorded at, in steps per second (> 0).
	Rate int `yaml:"rate"`
	// Interval is the number of steps between frames (>= 1).
	Interval int `yaml:"interval"`
	// LapSteps is the recorded lap's duration in steps, or 0 when unknown. When set, the last frame is at step LapSteps, so a lap whose length is not a multiple of Interval still ends on the finish line.
	LapSteps int `yaml:"lapSteps,omitempty"`
	// Frames holds the sampled poses; frame i is at step i * Interval, except that the last frame is at step LapSteps when LapSteps is set.
	Frames []GhostFrame `yaml:"frames"`
}

// GhostFrame is one sampled chassis pose.
type GhostFrame struct {
	// P is the position (x, y, z).
	P [3]float32 `yaml:"p,flow"`
	// R is the orientation quaternion (x, y, z, w).
	R [4]float32 `yaml:"r,flow"`
}

// Steps returns the number of steps the ghost's frames span: LapSteps when it is set, otherwise (len(Frames) - 1) * Interval.
func (g *Ghost) Steps() int {
	if len(g.Frames) < 2 {
		return 0
	}
	if g.LapSteps > 0 {
		return g.LapSteps
	}
	return (len(g.Frames) - 1) * g.Interval
}

// Pose returns the ghost's pose at step, which may be fractional to interpolate between fixed steps (pass the step plus game.Loop's draw alpha). Positions are interpolated linearly and orientations spherically; steps outside the recording clamp to its first or last frame. An empty ghost returns the origin and identity.
func (g *Ghost) Pose(step float64) (physics3d.Vec3, physics3d.Quat) {
	if len(g.Frames) == 0 {
		return physics3d.Vec3{}, physics3d.QuatIdentity()
	}
	if !(step > 0) {
		return frameVec(g.Frames[0]), frameQuat(g.Frames[0])
	}
	last := len(g.Frames) - 1
	end := float64(g.Steps())
	if last == 0 || step >= end {
		return frameVec(g.Frames[last]), frameQuat(g.Frames[last])
	}
	interval := float64(max(g.Interval, 1))
	i := min(int(math.Floor(step/interval)), last-1)
	t0, t1 := float64(float64(i)*interval), float64(float64(i+1)*interval)
	if i+1 == last {
		// The last frame is at LapSteps, which may fall between Interval samples.
		t1 = end
	}
	t := float32(1)
	if t1 > t0 {
		t = float32(min(max((step-t0)/(t1-t0), 0), 1))
	}
	a, b := g.Frames[i], g.Frames[i+1]
	pa, pb := frameVec(a), frameVec(b)
	return pa.Add(pb.Sub(pa).Scale(t)), frameQuat(a).Slerp(frameQuat(b), t)
}

func frameVec(f GhostFrame) physics3d.Vec3 {
	return physics3d.Vec3{X: f.P[0], Y: f.P[1], Z: f.P[2]}
}

func frameQuat(f GhostFrame) physics3d.Quat {
	return physics3d.Quat{X: f.R[0], Y: f.R[1], Z: f.R[2], W: f.R[3]}
}

// Validate checks that g uses GhostVersion, has a positive rate and interval, at most an hour of frames at 60 Hz spanning at most math.MaxInt32 steps, a LapSteps that falls within the last Interval before the last frame when it is set, and finite poses with non-zero quaternions.
func (g *Ghost) Validate() error {
	if g == nil {
		return errors.New("vehicle: nil ghost")
	}
	if g.Version != GhostVersion {
		return fmt.Errorf("vehicle: unsupported ghost version %d (want %d)", g.Version, GhostVersion)
	}
	if g.Rate <= 0 {
		return fmt.Errorf("vehicle: ghost rate %d must be > 0", g.Rate)
	}
	if g.Interval < 1 {
		return fmt.Errorf("vehicle: ghost interval %d must be >= 1", g.Interval)
	}
	if g.LapSteps < 0 {
		return fmt.Errorf("vehicle: ghost lapSteps %d must be >= 0", g.LapSteps)
	}
	if len(g.Frames) > maxGhostFrames {
		return fmt.Errorf("vehicle: ghost has %d frames (max %d)", len(g.Frames), maxGhostFrames)
	}
	// Divide rather than multiply so a huge interval cannot overflow the check itself.
	if len(g.Frames) > 1 && g.Interval > math.MaxInt32/(len(g.Frames)-1) {
		return fmt.Errorf("vehicle: ghost frames every %d steps span more than %d steps", g.Interval, math.MaxInt32)
	}
	if g.LapSteps > 0 {
		// The last frame is at LapSteps, so it must lie after the second-to-last frame's step and no later than the next Interval sample.
		n := len(g.Frames)
		if n < 2 || g.LapSteps <= (n-2)*g.Interval || g.LapSteps > (n-1)*g.Interval {
			return fmt.Errorf("vehicle: ghost lapSteps %d does not match %d frames every %d steps", g.LapSteps, n, g.Interval)
		}
	}
	for i, f := range g.Frames {
		for _, c := range f.P {
			if !finite(c) {
				return fmt.Errorf("vehicle: ghost frame %d has a non-finite position", i)
			}
		}
		var lenSq float32
		for _, c := range f.R {
			if !finite(c) {
				return fmt.Errorf("vehicle: ghost frame %d has a non-finite rotation", i)
			}
			lenSq += float32(c * c)
		}
		if lenSq < 1e-6 {
			return fmt.Errorf("vehicle: ghost frame %d has a zero rotation", i)
		}
	}
	return nil
}

// ParseGhost decodes a YAML Ghost and validates it. Unknown fields and trailing YAML documents are rejected.
func ParseGhost(data []byte) (*Ghost, error) {
	var g Ghost
	if err := decodeStrict(data, &g); err != nil {
		return nil, fmt.Errorf("vehicle: ghost: %w", err)
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	// `frames: []` and `frames:` (null) both mean no frames; keep one in-memory form so a parse, encode and parse round trip is lossless.
	if len(g.Frames) == 0 {
		g.Frames = nil
	}
	return &g, nil
}

// EncodeGhost validates g and serializes it as deterministic 2-space YAML, each frame's position and rotation on a flow-style line of its own, that ParseGhost reads back unchanged.
func EncodeGhost(g *Ghost) ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	data, err := encodeYAML(g)
	if err != nil {
		return nil, fmt.Errorf("vehicle: ghost: %w", err)
	}
	return data, nil
}

// GhostRecorder samples a body's pose every Interval steps into a Ghost.
type GhostRecorder struct {
	// Interval is the number of steps between frames; values < 1 record every step.
	Interval int

	frames []GhostFrame
	steps  int
}

// Record samples pos and rot if the current step falls on Interval, then advances one step. Call it once per fixed step, after World.Step, and once when the lap starts.
func (r *GhostRecorder) Record(pos physics3d.Vec3, rot physics3d.Quat) {
	if r.steps%max(r.Interval, 1) == 0 {
		r.sample(pos, rot)
	}
	r.steps++
}

func (r *GhostRecorder) sample(pos physics3d.Vec3, rot physics3d.Quat) {
	rot = rot.Norm()
	r.frames = append(r.frames, GhostFrame{
		P: [3]float32{pos.X, pos.Y, pos.Z},
		R: [4]float32{rot.X, rot.Y, rot.Z, rot.W},
	})
}

// Finish samples pos and rot as the recording's last frame, even when the current step falls between Interval samples, then advances one step. Call it instead of Record on the step the lap ends, such as from Race.OnLap, and pass the lap's step count to Ghost, so the ghost ends exactly on the finish line.
func (r *GhostRecorder) Finish(pos physics3d.Vec3, rot physics3d.Quat) {
	if r.steps%max(r.Interval, 1) != 0 {
		r.sample(pos, rot)
	}
	r.Record(pos, rot)
}

// Steps returns how many steps were recorded since the last Reset.
func (r *GhostRecorder) Steps() int { return r.steps }

// Reset discards the recorded frames, such as when a new lap starts.
func (r *GhostRecorder) Reset() {
	r.frames = r.frames[:0]
	r.steps = 0
}

// Ghost returns a copy of the recorded frames as a Ghost at rate steps per second, with LapSteps set to lapSteps: the step of the last frame, which is Steps() - 1 after Finish, or 0 when unknown.
func (r *GhostRecorder) Ghost(rate, lapSteps int) *Ghost {
	return &Ghost{
		Version:  GhostVersion,
		Rate:     rate,
		Interval: max(r.Interval, 1),
		LapSteps: lapSteps,
		Frames:   append([]GhostFrame(nil), r.frames...),
	}
}
