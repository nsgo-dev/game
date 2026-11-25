package physics3d

import (
	"math"
	"strings"
)

// RagdollMode controls how a Ragdoll's bodies respond to animation vs physics.
type RagdollMode int

const (
	// RagdollKinematic drives every bone body directly from SyncFromPose,
	// bypassing gravity and physics impulses while remaining collidable.
	RagdollKinematic RagdollMode = iota
	// RagdollDynamic simulates all bone bodies under full rigid-body dynamics
	// and skeleton joint constraints.
	RagdollDynamic
	// RagdollBlended blends between the animated pose provided to SyncFromPose
	// and the physics simulation using BlendWeight in [0, 1]
	// (0 = pure animation, 1 = pure physics).
	RagdollBlended
)

// SkeletonJoint describes one bone in a skeletal hierarchy used by BuildRagdoll.
type SkeletonJoint struct {
	Name       string
	Parent     int     // parent joint index, or < 0 for root
	LocalPos   Vec3    // joint offset in parent space
	LocalRot   Quat    // joint orientation in parent space (zero => identity)
	Radius     float32 // capsule radius (defaults to 0.08 when <= 0)
	Length     float32 // bone length (defaults to 0.25 when <= 0)
	Mass       float32 // bone mass in kg (defaults to 1.0 when <= 0)
	SwingLimit float32 // optional symmetric cone/hinge swing limit in radians (> 0 enables)
	HingeAxis  Vec3    // optional explicit hinge axis (non-zero selects HingeJoint)
	// Optional asymmetric swing limits in radians for SwingTwistJoint.
	// When any of these four fields is non-zero, they override symmetric SwingLimit
	// on their respective axis (or all four when both axes are specified).
	SwingMin1  float32
	SwingMax1  float32
	SwingMin2  float32
	SwingMax2  float32
	TwistLimit float32 // optional symmetric twist half-angle in radians (> 0 sets [-TwistLimit, +TwistLimit])
	MinTwist   float32 // optional asymmetric lower twist limit in radians
	MaxTwist   float32 // optional asymmetric upper twist limit in radians
}

// Ragdoll manages the rigid bodies and joint constraints generated from a
// skeletal hierarchy, supporting kinematic animation following, dynamic ragdoll
// simulation, and smooth animation-to-ragdoll blending.
type Ragdoll struct {
	World       *World
	Skeleton    []SkeletonJoint
	Bodies      []*Body
	Joints      []Joint3D
	Mode        RagdollMode
	BlendWeight float32 // [0, 1]: 0 = animated pose, 1 = full physics (used in RagdollBlended)

	localCenterOffsets []Vec3
	animPos            []Vec3
	animRot            []Quat
	hasAnimPose        bool
}

// BuildRagdoll spawns per-bone capsule/box bodies and connecting SwingTwistJoint /
// HingeJoint constraints in world matching the skeleton hierarchy rooted at
// (rootPos, rootRot).
//
// The returned Ragdoll starts in RagdollDynamic mode with BlendWeight = 1.
func BuildRagdoll(world *World, joints []SkeletonJoint, rootPos Vec3, rootRot Quat) *Ragdoll {
	r := &Ragdoll{
		World:       world,
		Mode:        RagdollDynamic,
		BlendWeight: 1,
	}
	n := len(joints)
	if world == nil || n == 0 {
		return r
	}

	r.Skeleton = make([]SkeletonJoint, n)
	copy(r.Skeleton, joints)
	r.Bodies = make([]*Body, n)
	r.localCenterOffsets = make([]Vec3, n)
	r.animPos = make([]Vec3, n)
	r.animRot = make([]Quat, n)

	rRot := normalizedQuat(rootRot)

	// 1. Forward kinematics (handles arbitrary parent index ordering safely).
	worldPos := make([]Vec3, n)
	worldRot := make([]Quat, n)
	resolved := make([]uint8, n) // 0=unvisited, 1=visiting, 2=done

	var resolve func(i int)
	resolve = func(i int) {
		if resolved[i] == 2 {
			return
		}
		if resolved[i] == 1 {
			// Cycle guard: fall back to root transform.
			worldRot[i] = rRot
			worldPos[i] = rootPos
			resolved[i] = 2
			return
		}
		resolved[i] = 1
		sj := r.Skeleton[i]
		lRot := normalizedQuat(sj.LocalRot)
		if sj.Parent < 0 || sj.Parent >= n || sj.Parent == i {
			worldRot[i] = rRot.Mul(lRot).Norm()
			worldPos[i] = rootPos.Add(rRot.RotateVec(sj.LocalPos))
		} else {
			resolve(sj.Parent)
			p := sj.Parent
			worldRot[i] = worldRot[p].Mul(lRot).Norm()
			worldPos[i] = worldPos[p].Add(worldRot[p].RotateVec(sj.LocalPos))
		}
		resolved[i] = 2
	}
	for i := range n {
		resolve(i)
	}

	// 2. Count children per joint to pick sensible bone directions and center offsets.
	childIndices := make([][]int, n)
	for i, sj := range r.Skeleton {
		if sj.Parent >= 0 && sj.Parent < n && sj.Parent != i {
			childIndices[sj.Parent] = append(childIndices[sj.Parent], i)
		}
	}

	boneDirs := make([]Vec3, n)
	bodyPos := make([]Vec3, n)
	for i, sj := range r.Skeleton {
		radius := sj.Radius
		if radius <= 0 {
			radius = 0.08
		}
		length := sj.Length
		if length <= 0 {
			if len(childIndices[i]) == 1 {
				if d := r.Skeleton[childIndices[i][0]].LocalPos.Len(); d > 1e-3 {
					length = d
				}
			}
			if length <= 0 {
				length = 0.25
			}
		}
		mass := sj.Mass
		if mass <= 0 {
			mass = 1.0
		}

		var dir Vec3
		switch {
		case len(childIndices[i]) == 1 && r.Skeleton[childIndices[i][0]].LocalPos.LenSq() > 1e-6:
			dir = r.Skeleton[childIndices[i][0]].LocalPos.Norm()
		case sj.Parent >= 0 && sj.Parent < n && sj.LocalPos.LenSq() > 1e-6:
			dir = normalizedQuat(sj.LocalRot).Conjugate().RotateVec(sj.LocalPos).Norm()
		default:
			dir = Vec3{Y: 1}
		}
		if dir.LenSq() == 0 {
			dir = Vec3{Y: 1}
		}
		boneDirs[i] = dir

		var shape Shape
		var centerOffset Vec3
		if sj.Parent < 0 && len(childIndices[i]) > 1 {
			// Multi-branch root (e.g. pelvis/torso hub): center a compact box at root joint.
			shape = BoxShape(Box{HalfExtents: Vec3{X: radius, Y: max(radius, length*0.5), Z: radius}})
			centerOffset = Vec3{}
		} else {
			halfSeg := float32(length*0.5) - radius
			if halfSeg < 0 {
				halfSeg = 0
			}
			shape = CapsuleShape(Capsule{
				A:      dir.Scale(-halfSeg),
				B:      dir.Scale(halfSeg),
				Radius: radius,
			})
			centerOffset = dir.Scale(length * 0.5)
		}

		r.localCenterOffsets[i] = centerOffset
		bPos := worldPos[i].Add(worldRot[i].RotateVec(centerOffset))
		bodyPos[i] = bPos

		b := NewDynamicBody(shape, bPos, mass)
		b.Tag = sj.Name
		b.Rot = worldRot[i]
		b.LinearDamping = 0.05
		b.AngularDamping = 0.15
		r.Bodies[i] = world.AddBody(b)
	}

	// Disable intra-ragdoll self-collisions so overlapping capsules at joints
	// never fight the skeleton constraints.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			world.IgnoreCollision(r.Bodies[i], r.Bodies[j])
		}
	}

	// 3. Connect each child bone to its parent at the child's world joint pivot.
	for i, sj := range r.Skeleton {
		if sj.Parent < 0 || sj.Parent >= n || sj.Parent == i {
			continue
		}
		p := sj.Parent
		bodyP := r.Bodies[p]
		bodyC := r.Bodies[i]
		pivot := worldPos[i]

		localAnchorP := worldRot[p].Conjugate().RotateVec(pivot.Sub(bodyPos[p]))
		localAnchorC := worldRot[i].Conjugate().RotateVec(pivot.Sub(bodyPos[i]))

		lowerName := strings.ToLower(sj.Name)
		useHinge := sj.HingeAxis.LenSq() > 1e-6 ||
			strings.Contains(lowerName, "knee") ||
			strings.Contains(lowerName, "elbow")

		if useHinge {
			hingeAxis := sj.HingeAxis.Norm()
			if hingeAxis.LenSq() == 0 {
				hingeAxis = Vec3{X: 1}
			}
			hj := NewHingeJoint(bodyP, bodyC, localAnchorP, localAnchorC, hingeAxis)
			// Align B's local axis with the same world axis as A's hinge axis.
			worldAxis := worldRot[p].RotateVec(hingeAxis)
			hj.LocalAxisB = worldRot[i].Conjugate().RotateVec(worldAxis).Norm()
			hj.refInitialized = false
			hj.initReference()
			if sj.SwingLimit > 0 {
				hj.EnableLimits = true
				hj.MinAngle = -sj.SwingLimit
				hj.MaxAngle = sj.SwingLimit
			}
			r.Joints = append(r.Joints, world.AddJoint(hj))
		} else {
			worldAxisB := worldRot[i].RotateVec(boneDirs[i])
			twistAxisA := worldRot[p].Conjugate().RotateVec(worldAxisB).Norm()
			st := NewSwingTwistJoint(bodyP, bodyC, localAnchorP, localAnchorC, twistAxisA)

			hasAsym1 := sj.SwingMin1 != 0 || sj.SwingMax1 != 0
			hasAsym2 := sj.SwingMin2 != 0 || sj.SwingMax2 != 0
			if hasAsym1 || hasAsym2 {
				st.EnableSwingLimits = true
				if hasAsym1 {
					st.SwingMin1 = sj.SwingMin1
					st.SwingMax1 = sj.SwingMax1
				} else if sj.SwingLimit > 0 {
					st.SwingMin1 = -sj.SwingLimit
					st.SwingMax1 = sj.SwingLimit
				}
				if hasAsym2 {
					st.SwingMin2 = sj.SwingMin2
					st.SwingMax2 = sj.SwingMax2
				} else if sj.SwingLimit > 0 {
					st.SwingMin2 = -sj.SwingLimit
					st.SwingMax2 = sj.SwingLimit
				}
			} else if sj.SwingLimit > 0 {
				st.EnableSwingLimits = true
				st.SwingMin1 = -sj.SwingLimit
				st.SwingMax1 = sj.SwingLimit
				st.SwingMin2 = -sj.SwingLimit
				st.SwingMax2 = sj.SwingLimit
			}

			switch {
			case sj.MinTwist != 0 || sj.MaxTwist != 0:
				st.EnableTwistLimits = true
				st.MinTwist = sj.MinTwist
				st.MaxTwist = sj.MaxTwist
			case sj.TwistLimit > 0:
				st.EnableTwistLimits = true
				st.MinTwist = -sj.TwistLimit
				st.MaxTwist = sj.TwistLimit
			case st.EnableSwingLimits:
				// Default anatomically plausible twist range when swing limits are enabled
				// so shoulders and hips cannot spin freely around their own bone axis.
				const defaultTwist = float32(math.Pi / 4)
				st.EnableTwistLimits = true
				st.MinTwist = -defaultTwist
				st.MaxTwist = defaultTwist
			}
			r.Joints = append(r.Joints, world.AddJoint(st))
		}
	}

	return r
}

// SetMode switches the ragdoll between RagdollKinematic, RagdollDynamic, and
// RagdollBlended modes.
func (r *Ragdoll) SetMode(mode RagdollMode) {
	if r == nil {
		return
	}
	r.Mode = mode
	for _, b := range r.Bodies {
		if b == nil {
			continue
		}
		switch mode {
		case RagdollKinematic:
			b.Kinematic = true
			b.Sleeping = false
			b.Vel = Vec3{}
			b.AngVel = Vec3{}
		case RagdollDynamic:
			b.Kinematic = false
			b.Wake()
		case RagdollBlended:
			b.Kinematic = false
			b.Wake()
		}
	}
}

// SyncFromPose updates bone bodies from an animated world-space pose.
//
// In RagdollKinematic mode, bones snap directly to the animated pose.
// In RagdollBlended mode, bones smoothly blend between the animated pose and
// their simulated physics pose according to BlendWeight in [0, 1]
// (0 = pure animation, 1 = pure physics).
// In RagdollDynamic mode, the animated pose is recorded without overriding
// physics bodies.
func (r *Ragdoll) SyncFromPose(worldPositions []Vec3, worldRotations []Quat) {
	if r == nil || len(r.Bodies) == 0 {
		return
	}
	count := min(len(r.Bodies), min(len(worldPositions), len(worldRotations)))
	if count == 0 {
		return
	}

	w := clamp32(r.BlendWeight, 0, 1)
	r.hasAnimPose = true

	for i := 0; i < count; i++ {
		b := r.Bodies[i]
		if b == nil {
			continue
		}
		targetRot := normalizedQuat(worldRotations[i])
		targetJointPos := worldPositions[i]
		r.animPos[i] = targetJointPos
		r.animRot[i] = targetRot
		targetBodyPos := targetJointPos.Add(targetRot.RotateVec(r.localCenterOffsets[i]))

		switch r.Mode {
		case RagdollKinematic:
			b.Pos = targetBodyPos
			b.Rot = targetRot
			b.Vel = Vec3{}
			b.AngVel = Vec3{}
		case RagdollBlended:
			if w <= 0 {
				b.Pos = targetBodyPos
				b.Rot = targetRot
				b.Vel = Vec3{}
				b.AngVel = Vec3{}
			} else if w < 1 {
				curRot := normalizedQuat(b.Rot)
				curJointPos := b.Pos.Sub(curRot.RotateVec(r.localCenterOffsets[i]))
				blendedJointPos := lerpVec3(targetJointPos, curJointPos, w)
				blendedRot := targetRot.Slerp(curRot, w)
				b.Rot = blendedRot
				b.Pos = blendedJointPos.Add(blendedRot.RotateVec(r.localCenterOffsets[i]))
				b.Vel = b.Vel.Scale(w)
				b.AngVel = b.AngVel.Scale(w)
				b.Wake()
			}
		case RagdollDynamic:
			// Full physics: do not override simulated bodies.
		}
	}
}

// WritePose reads back the current world-space joint positions and rotations
// from the ragdoll's bodies for driving skinned-mesh rendering.
func (r *Ragdoll) WritePose() ([]Vec3, []Quat) {
	if r == nil || len(r.Bodies) == 0 {
		return nil, nil
	}
	n := len(r.Bodies)
	positions := make([]Vec3, n)
	rotations := make([]Quat, n)

	w := clamp32(r.BlendWeight, 0, 1)
	exactAnim := r.Mode == RagdollBlended && r.hasAnimPose && w <= 0

	for i, b := range r.Bodies {
		if b == nil {
			rotations[i] = QuatIdentity()
			continue
		}
		if exactAnim {
			positions[i] = r.animPos[i]
			rotations[i] = r.animRot[i]
			continue
		}
		rot := normalizedQuat(b.Rot)
		rotations[i] = rot
		positions[i] = b.Pos.Sub(rot.RotateVec(r.localCenterOffsets[i]))
	}
	return positions, rotations
}

// Destroy removes all ragdoll joints and bone bodies from r.World.
func (r *Ragdoll) Destroy() {
	if r == nil || r.World == nil {
		return
	}
	for _, j := range r.Joints {
		r.World.RemoveJoint(j)
	}
	for _, b := range r.Bodies {
		if b != nil {
			r.World.RemoveBody(b.ID)
		}
	}
	r.Joints = nil
	r.Bodies = nil
}
