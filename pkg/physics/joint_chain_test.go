package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

type rigidChain2D struct {
	bodies []*physics.RigidBody
	joints []*physics.RevoluteJoint
	tail   core.Point
}

func makeRigidChain2D(links int, horizontal bool, endMass float32) (physics.RigidWorld, rigidChain2D) {
	const (
		linkLen   = float32(10)
		thickness = float32(2)
	)
	rw := physics.RigidWorld{GravityY: 600}
	anchor := physics.NewStaticRigidBox(0, 0, 2, 2)
	anchor.Layer, anchor.Mask = 2, 1
	rw.AddBody(anchor)
	chain := rigidChain2D{
		bodies: make([]*physics.RigidBody, links),
		joints: make([]*physics.RevoluteJoint, links),
	}
	prev := anchor
	prevAnchor := core.Point{}
	for i := range links {
		mass := float32(1)
		if i == links-1 {
			mass = endMass
		}
		x, y := float32(0), (float32(i)+0.5)*linkLen
		w, h := thickness, linkLen
		chain.tail = core.Point{Y: core.Points(linkLen / 2)}
		far := core.Point{Y: core.Points(linkLen / 2)}
		if horizontal {
			x, y = (float32(i)+0.5)*linkLen, 0
			w, h = linkLen, thickness
			chain.tail = core.Point{X: core.Points(linkLen / 2)}
			far = chain.tail
		}
		body := physics.NewRigidBox(x, y, w, h, mass)
		body.Layer, body.Mask = 1, 2
		rw.AddBody(body)
		pivot := prev.LocalToWorld(prevAnchor)
		joint := physics.NewRevoluteJoint(prev, body, pivot)
		rw.AddJoint(joint)
		chain.bodies[i] = body
		chain.joints[i] = joint
		prev = body
		prevAnchor = far
	}
	return rw, chain
}

func TestRigidJointChainsDefaultSettings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		links      int
		horizontal bool
		endMass    float32
		tolerance  float64
	}{
		{name: "hanging20", links: 20, endMass: 1, tolerance: 0.02},
		{name: "hanging40", links: 40, endMass: 1, tolerance: 0.02},
		{name: "swinging20", links: 20, horizontal: true, endMass: 1, tolerance: 0.05},
		{name: "swinging40", links: 40, horizontal: true, endMass: 1, tolerance: 0.05},
		{name: "heavyEnd20", links: 20, endMass: 100, tolerance: 0.05},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rw, chain := makeRigidChain2D(tc.links, tc.horizontal, tc.endMass)
			rest := float64(tc.links * 10)
			for range 600 {
				rw.Step(time.Second / 60)
			}
			for i, joint := range chain.joints {
				pa := joint.A.LocalToWorld(joint.AnchorA)
				pb := joint.B.LocalToWorld(joint.AnchorB)
				if separation := math.Hypot(float64(pb.X-pa.X), float64(pb.Y-pa.Y)); separation > 0.1 {
					t.Fatalf("joint %d separation = %.4f px", i, separation)
				}
			}
			var got float64
			for i, joint := range chain.joints {
				near := joint.B.LocalToWorld(joint.AnchorB)
				far := near
				if i+1 < len(chain.joints) {
					far = chain.joints[i+1].A.LocalToWorld(chain.joints[i+1].AnchorA)
				}
				got += math.Hypot(float64(far.X-near.X), float64(far.Y-near.Y))
			}
			last := chain.joints[len(chain.joints)-1]
			near := last.B.LocalToWorld(last.AnchorB)
			far := chain.bodies[len(chain.bodies)-1].LocalToWorld(chain.tail)
			got += math.Hypot(float64(far.X-near.X), float64(far.Y-near.Y))
			if stretch := math.Abs(got-rest) / rest; stretch >= tc.tolerance {
				t.Fatalf("chain length = %.4f, rest = %.1f, stretch = %.3f%% (want < %.1f%%), sleeping=%v", got, rest, stretch*100, tc.tolerance*100, chain.bodies[tc.links-1].Sleeping())
			}
		})
	}
}

func BenchmarkRigidJointChain40(b *testing.B) {
	for b.Loop() {
		rw, _ := makeRigidChain2D(40, false, 1)
		for range 600 {
			rw.Step(time.Second / 60)
		}
	}
}
