package vehicle

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/physics3d"
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
	// LapSteps is the recorded lap's duration in steps, or 0 when unknown.
	LapSteps int `yaml:"lapSteps,omitempty"`
	// Frames holds the sampled poses; frame i is at step i * Interval.
	Frames []GhostFrame `yaml:"frames"`
}

// GhostFrame is one sampled chassis pose.
type GhostFrame struct {
	// P is the position (x, y, z).
	P [3]float32 `yaml:"p,flow"`
	// R is the orientation quaternion (x, y, z, w).
	R [4]float32 `yaml:"r,flow"`
}

// Steps returns the number of steps the ghost's frames span.
func (g *Ghost) Steps() int {
	if len(g.Frames) == 0 {
		return 0
	}
	return (len(g.Frames) - 1) * g.Interval
}

// Pose returns the ghost's pose at step, which may be fractional to interpolate between fixed steps (pass the step plus game.Loop's draw alpha). Positions are interpolated linearly and orientations spherically; steps outside the recording clamp to its first or last frame. An empty ghost returns the origin and identity.
func (g *Ghost) Pose(step float64) (physics3d.Vec3, physics3d.Quat) {
	if len(g.Frames) == 0 {
		return physics3d.Vec3{}, physics3d.QuatIdentity()
	}
	interval := max(g.Interval, 1)
	pos := step / float64(interval)
	if !(pos > 0) {
		return frameVec(g.Frames[0]), frameQuat(g.Frames[0])
	}
	last := len(g.Frames) - 1
	if pos >= float64(last) {
		return frameVec(g.Frames[last]), frameQuat(g.Frames[last])
	}
	i := int(math.Floor(pos))
	t := float32(pos - float64(i))
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

// Validate checks that g uses GhostVersion, has a positive rate and interval, at most an hour of frames at 60 Hz, and finite poses with non-zero quaternions.
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
			lenSq += c * c
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

// Record samples pos and rot if the current step falls on Interval, then advances one step. Call it once per fixed step, after World.Step.
func (r *GhostRecorder) Record(pos physics3d.Vec3, rot physics3d.Quat) {
	if r.steps%max(r.Interval, 1) == 0 {
		rot = rot.Norm()
		r.frames = append(r.frames, GhostFrame{
			P: [3]float32{pos.X, pos.Y, pos.Z},
			R: [4]float32{rot.X, rot.Y, rot.Z, rot.W},
		})
	}
	r.steps++
}

// Steps returns how many steps were recorded since the last Reset.
func (r *GhostRecorder) Steps() int { return r.steps }

// Reset discards the recorded frames, such as when a new lap starts.
func (r *GhostRecorder) Reset() {
	r.frames = r.frames[:0]
	r.steps = 0
}

// Ghost returns a copy of the recorded frames as a Ghost at rate steps per second, with LapSteps set to lapSteps.
func (r *GhostRecorder) Ghost(rate, lapSteps int) *Ghost {
	return &Ghost{
		Version:  GhostVersion,
		Rate:     rate,
		Interval: max(r.Interval, 1),
		LapSteps: lapSteps,
		Frames:   append([]GhostFrame(nil), r.frames...),
	}
}
