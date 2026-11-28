package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

type rigidChain3D struct {
	bodies []*physics3d.Body
	joints []*physics3d.BallJoint
	tail   physics3d.Vec3
}

func makeRigidChain3D(links int, horizontal bool, endMass float32) (*physics3d.World, rigidChain3D) {
	const (
		linkLen   = float32(0.2)
		thickness = float32(0.04)
	)
	w := physics3d.NewWorld()
	anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.02}), physics3d.Vec3{Y: 10}))
	chain := rigidChain3D{
		bodies: make([]*physics3d.Body, links),
		joints: make([]*physics3d.BallJoint, links),
	}
	prev := anchor
	prevAnchor := physics3d.Vec3{}
	for i := range links {
		mass := float32(1)
		if i == links-1 {
			mass = endMass
		}
		pos := physics3d.Vec3{Y: 10 - (float32(i)+0.5)*linkLen}
		half := physics3d.Vec3{X: thickness / 2, Y: linkLen / 2, Z: thickness / 2}
		near := physics3d.Vec3{Y: linkLen / 2}
		far := physics3d.Vec3{Y: -linkLen / 2}
		chain.tail = far
		if horizontal {
			pos = physics3d.Vec3{X: (float32(i) + 0.5) * linkLen, Y: 10}
			half = physics3d.Vec3{X: linkLen / 2, Y: thickness / 2, Z: thickness / 2}
			near = physics3d.Vec3{X: -linkLen / 2}
			far = physics3d.Vec3{X: linkLen / 2}
			chain.tail = far
		}
		body := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: half}), pos, mass))
		joint := physics3d.NewBallJoint(prev, body, prevAnchor, near)
		w.AddJoint(joint)
		chain.bodies[i] = body
		chain.joints[i] = joint
		prev = body
		prevAnchor = far
	}
	return w, chain
}

func TestJointChainsDefaultSettings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		links      int
		horizontal bool
		endMass    float32
		tolerance  float32
	}{
		{name: "hanging20", links: 20, endMass: 1, tolerance: 0.02},
		{name: "hanging40", links: 40, endMass: 1, tolerance: 0.02},
		{name: "swinging20", links: 20, horizontal: true, endMass: 1, tolerance: 0.05},
		{name: "swinging40", links: 40, horizontal: true, endMass: 1, tolerance: 0.05},
		{name: "heavyEnd20", links: 20, endMass: 100, tolerance: 0.05},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, chain := makeRigidChain3D(tc.links, tc.horizontal, tc.endMass)
			rest := float32(tc.links) * 0.2
			for range 600 {
				w.Step(time.Second / 60)
			}
			var length float32
			for i, joint := range chain.joints {
				near := joint.BodyB.Pos.Add(joint.BodyB.Rot.RotateVec(joint.LocalAnchorB))
				far := near
				if i+1 < len(chain.joints) {
					next := chain.joints[i+1]
					far = next.BodyA.Pos.Add(next.BodyA.Rot.RotateVec(next.LocalAnchorA))
				}
				length += near.Dist(far)
			}
			last := chain.joints[len(chain.joints)-1]
			near := last.BodyB.Pos.Add(last.BodyB.Rot.RotateVec(last.LocalAnchorB))
			far := chain.bodies[len(chain.bodies)-1].Pos.Add(chain.bodies[len(chain.bodies)-1].Rot.RotateVec(chain.tail))
			length += near.Dist(far)
			if stretch := float32(math.Abs(float64(length-rest))) / rest; stretch >= tc.tolerance {
				t.Fatalf("chain length = %.5f, rest = %.2f, stretch = %.3f%% (want < %.1f%%)", length, rest, stretch*100, tc.tolerance*100)
			}
		})
	}
}

func jointAnchorDistance3D(joint physics3d.Joint3D) float32 {
	var a, b *physics3d.Body
	var localA, localB physics3d.Vec3
	switch j := joint.(type) {
	case *physics3d.BallJoint:
		a, b, localA, localB = j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB
	case *physics3d.HingeJoint:
		a, b, localA, localB = j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB
	case *physics3d.SwingTwistJoint:
		a, b, localA, localB = j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB
	default:
		return 0
	}
	pa := a.Pos.Add(a.Rot.RotateVec(localA))
	pb := b.Pos.Add(b.Rot.RotateVec(localB))
	return pa.Dist(pb)
}

func TestRagdollDroppedOnSlopeKeepsJoints(t *testing.T) {
	skeleton := []physics3d.SkeletonJoint{
		{Name: "pelvis", Parent: -1, Radius: 0.14, Length: 0.25, Mass: 8},
		{Name: "torso", Parent: 0, LocalPos: physics3d.Vec3{Y: 0.25}, Radius: 0.14, Length: 0.4, Mass: 10, SwingLimit: 0.6},
		{Name: "head", Parent: 1, LocalPos: physics3d.Vec3{Y: 0.42}, Radius: 0.11, Length: 0.22, Mass: 4, SwingLimit: 0.5},
		{Name: "upperArmL", Parent: 1, LocalPos: physics3d.Vec3{X: -0.22, Y: 0.32}, Radius: 0.06, Length: 0.3, Mass: 2.5, SwingLimit: 1.2},
		{Name: "elbowL", Parent: 3, LocalPos: physics3d.Vec3{X: -0.3}, Radius: 0.05, Length: 0.28, Mass: 2, SwingLimit: 1.1},
		{Name: "thighL", Parent: 0, LocalPos: physics3d.Vec3{X: -0.1, Y: -0.05}, Radius: 0.08, Length: 0.42, Mass: 5, SwingLimit: 1},
		{Name: "kneeL", Parent: 5, LocalPos: physics3d.Vec3{Y: -0.42}, Radius: 0.07, Length: 0.4, Mass: 4, SwingLimit: 1.2},
	}
	w := physics3d.NewWorld()
	slope := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}), physics3d.Vec3{Y: -0.5})
	slope.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Z: 1}, float32(math.Pi/10))
	w.AddBody(slope)
	ragdoll := physics3d.BuildRagdoll(w, skeleton, physics3d.Vec3{Y: 3}, physics3d.QuatIdentity())
	for step := range 300 {
		w.Step(time.Second / 60)
		for i, joint := range ragdoll.Joints {
			if separation := jointAnchorDistance3D(joint); separation > 0.01 {
				t.Fatalf("step %d joint %d separation = %.5f m (want <= 0.01)", step, i, separation)
			}
		}
	}
}

func BenchmarkJointChain40(b *testing.B) {
	for b.Loop() {
		w, _ := makeRigidChain3D(40, false, 1)
		for range 600 {
			w.Step(time.Second / 60)
		}
	}
}
