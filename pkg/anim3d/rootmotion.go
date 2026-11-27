package anim3d

import (
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// RootMotion holds the per-step root displacement extracted from an animation clip.
type RootMotion struct {
	DeltaPos math3d.Vec3
	DeltaRot math3d.Quat
}

// RootMotionOptions configures root motion extraction and pose stripping.
type RootMotionOptions struct {
	// RootJoint is the joint index carrying root motion (default 0).
	RootJoint int
	// ExtractYawOnly projects DeltaRot onto the world +Y yaw axis so pitch and
	// roll remain purely visual on the animated skeleton.
	ExtractYawOnly bool
	// StripRootXZ zeroes the root joint's X and Z translation in StripRootMotion
	// (keeping Y vertical motion on the skeleton) so characters can drive their
	// world transform from DeltaPos without double-translating.
	StripRootXZ bool
	// StripRootY zeroes the root joint's Y translation in StripRootMotion.
	StripRootY bool
	// StripRootRot removes the extracted root rotation from the root joint in
	// StripRootMotion.
	StripRootRot bool
}

// ExtractRootMotion extracts the root joint's translation delta (DeltaPos) and
// rotation delta (DeltaRot) across the interval [prevTime, currTime].
//
// For looping clips, loop wrap-around is handled cleanly without backward jumps whether the caller passes already-wrapped playback times (currTime < prevTime) or monotonic unwrapped times (currTime >= clip.Duration). A step that crosses one or more loop boundaries is split at each boundary and the pieces are composed in playback order, so its DeltaRot equals the rotation of applying the pieces one after another with ApplyRootMotion, also for clips that pitch and yaw at once.
func ExtractRootMotion(clip *Clip, prevTime, currTime time.Duration, opts RootMotionOptions) RootMotion {
	if clip == nil || clip.Duration <= 0 {
		return RootMotion{DeltaRot: math3d.QuatIdentity()}
	}

	root := opts.RootJoint
	// segment returns the motion between two in-range clip times. Yaw-only extraction runs per segment so that a step crossing a loop boundary composes exactly like the two steps a caller would take on either side of it.
	segment := func(from, to time.Duration) RootMotion {
		a := clip.sampleJointClamped(root, from)
		b := clip.sampleJointClamped(root, to)
		rot := b.Rot.Mul(a.Rot.Inverse()).Norm()
		if opts.ExtractYawOnly {
			rot = extractYawQuat(rot)
		}
		return RootMotion{DeltaPos: b.Pos.Sub(a.Pos), DeltaRot: rot}
	}

	if !clip.Loop {
		return segment(clip.NormalizeTime(prevTime), clip.NormalizeTime(currTime))
	}
	if currTime < prevTime {
		// Caller passed wrapped playback timestamps across a single loop boundary.
		p0 := prevTime
		if p0 != clip.Duration {
			p0 = modDuration(p0, clip.Duration)
		}
		p1 := modDuration(currTime, clip.Duration)
		return composeRootMotion(segment(p0, clip.Duration), segment(0, p1))
	}
	span := currTime - prevTime
	if span == 0 {
		return RootMotion{DeltaRot: math3d.QuatIdentity()}
	}
	p0 := modDuration(prevTime, clip.Duration)
	p1 := modDuration(currTime, clip.Duration)
	loops := int((p0 + span) / clip.Duration)
	if loops == 0 {
		return segment(p0, p1)
	}
	rm := segment(p0, clip.Duration)
	if loops > 1 {
		whole := segment(0, clip.Duration)
		for range loops - 1 {
			rm = composeRootMotion(rm, whole)
		}
	}
	return composeRootMotion(rm, segment(0, p1))
}

// composeRootMotion joins two consecutive segments, a first. Rotations compose in the order ApplyRootMotion applies successive steps (world.Rot * a * b), so a step across a loop boundary turns the character exactly as the two steps on either side of it would; positions add because DeltaPos is measured in the clip's root parent space.
func composeRootMotion(a, b RootMotion) RootMotion {
	return RootMotion{
		DeltaPos: a.DeltaPos.Add(b.DeltaPos),
		DeltaRot: a.DeltaRot.Mul(b.DeltaRot).Norm(),
	}
}

// StripRootMotion returns a copy of pose with the configured root translation
// and/or rotation channels removed from opts.RootJoint so world-space movement
// is driven exclusively by the extracted RootMotion.
func StripRootMotion(pose Pose, opts RootMotionOptions) Pose {
	out := pose.Clone()
	if opts.RootJoint < 0 || opts.RootJoint >= len(out.Joints) {
		return out
	}
	jt := out.Joints[opts.RootJoint]
	if opts.StripRootXZ {
		jt.Pos.X = 0
		jt.Pos.Z = 0
	}
	if opts.StripRootY {
		jt.Pos.Y = 0
	}
	if opts.StripRootRot {
		if opts.ExtractYawOnly {
			yaw := extractYawQuat(jt.Rot)
			jt.Rot = yaw.Inverse().Mul(jt.Rot).Norm()
		} else {
			jt.Rot = math3d.QuatIdentity()
		}
	}
	out.Joints[opts.RootJoint] = jt
	return out
}

// ApplyRootMotion advances a character's world transform by rm, transforming
// rm.DeltaPos by world's current orientation and scale and composing rm.DeltaRot.
func ApplyRootMotion(world math3d.Transform, rm RootMotion) math3d.Transform {
	world.Pos = world.Pos.Add(world.Rot.RotateVec3(world.Scale.Mul(rm.DeltaPos)))
	world.Rot = world.Rot.Mul(rm.DeltaRot).Norm()
	return world
}

// extractYawQuat extracts the Y-axis twist (yaw) component of q as a unit quaternion.
func extractYawQuat(q math3d.Quat) math3d.Quat {
	yaw := math3d.Quat{X: 0, Y: q.Y, Z: 0, W: q.W}
	if yaw.LenSq() < 1e-12 {
		return math3d.QuatIdentity()
	}
	return yaw.Norm()
}
