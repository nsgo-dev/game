package pathfind3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
)

func velocityAgentConfig3D() pathfind3d.NavAgentConfig {
	return pathfind3d.NavAgentConfig{
		MaxSpeed:         6,
		MaxAcceleration:  30,
		Radius:           0.35,
		ArrivalRadius:    1.5,
		StoppingDistance: 0.3,
	}
}

func speedXZ(v math3d.Vec3) float64 { return math.Hypot(float64(v.X), float64(v.Z)) }

func TestNavAgent3DStepWithDesiredVelocityMatchesStep(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()
	start := math3d.Vec3{X: 6, Y: 0, Z: 0}
	goal := math3d.Vec3{X: 6, Y: 3, Z: 0}
	ref := pathfind3d.NewNavAgent(nm, start, velocityAgentConfig3D())
	ext := pathfind3d.NewNavAgent(nm, start, velocityAgentConfig3D())
	ref.SetTarget(goal)
	ext.SetTarget(goal)

	const dt = time.Second / 60
	for step := range 900 {
		ref.Step(dt)
		v := ext.DesiredVelocity()
		if v.Y != 0 {
			t.Fatalf("step %d: DesiredVelocity %v has a vertical component", step, v)
		}
		ext.StepWithVelocity(v, dt)
		if d := ref.Pos().Dist(ext.Pos()); d > 1e-3 {
			t.Fatalf("step %d: StepWithVelocity(DesiredVelocity()) at %v diverged from Step at %v by %.5f", step, ext.Pos(), ref.Pos(), d)
		}
		if ref.State() != ext.State() {
			t.Fatalf("step %d: state %v, want %v", step, ext.State(), ref.State())
		}
		if ref.State() == pathfind3d.NavAgentArrived {
			break
		}
	}
	if ext.State() != pathfind3d.NavAgentArrived {
		t.Fatalf("external-velocity agent did not arrive on the bridge: state=%v pos=%v", ext.State(), ext.Pos())
	}
	if v := ext.DesiredVelocity(); v != (math3d.Vec3{}) {
		t.Fatalf("DesiredVelocity after arrival = %v, want zero", v)
	}
}

func TestNavAgent3DStepWithVelocityClampsAndTracksSurface(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()
	cfg := velocityAgentConfig3D()
	agent := pathfind3d.NewNavAgent(nm, math3d.Vec3{X: 14, Y: 0, Z: 0}, cfg)
	if v := agent.DesiredVelocity(); v != (math3d.Vec3{}) {
		t.Fatalf("idle DesiredVelocity = %v, want zero", v)
	}
	agent.SetTarget(math3d.Vec3{X: 6, Y: 3, Z: 0})

	const dt = time.Second / 60
	// An avoidance result far above MaxSpeed, with a vertical component that must be ignored, pushing south out of the mesh.
	push := math3d.Vec3{X: 0, Y: 500, Z: -1000}
	agent.StepWithVelocity(push, dt)
	if s, maxS := speedXZ(agent.Vel()), float64(cfg.MaxAcceleration)*dt.Seconds()+1e-4; s > maxS {
		t.Fatalf("speed after one step = %.4f, want <= MaxAcceleration*dt %.4f", s, maxS)
	}
	for step := range 120 {
		agent.StepWithVelocity(push, dt)
		p := agent.Pos()
		poly, ok := nm.FindPolygon(p)
		if !ok {
			t.Fatalf("step %d: agent left 3D navmesh at %v", step, p)
		}
		if surfY := nm.Polys[poly].HeightAt(p.X, p.Z); !math3d.ApproxEqual(p.Y, surfY, 1e-2) {
			t.Fatalf("step %d: Y = %v, want surface %v", step, p.Y, surfY)
		}
		if s := speedXZ(agent.Vel()); s > float64(cfg.MaxSpeed)+1e-4 {
			t.Fatalf("step %d: speed %.4f exceeds MaxSpeed", step, s)
		}
	}

	before := agent.Pos()
	nan := float32(math.NaN())
	agent.StepWithVelocity(math3d.Vec3{X: nan, Z: float32(math.Inf(-1))}, dt)
	if p := agent.Pos(); math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Z)) || float64(p.Dist(before)) > float64(cfg.MaxSpeed)*dt.Seconds()+1e-3 {
		t.Fatalf("non-finite velocity moved agent from %v to %v", before, p)
	}

	for range 900 {
		agent.StepWithVelocity(agent.DesiredVelocity(), dt)
		if agent.State() == pathfind3d.NavAgentArrived {
			break
		}
	}
	if agent.State() != pathfind3d.NavAgentArrived || !math3d.ApproxEqual(agent.Pos().Y, 3, 1e-2) {
		t.Fatalf("agent did not arrive on the bridge after avoidance push: state=%v pos=%v", agent.State(), agent.Pos())
	}
}

func TestNavAgent3DStepWithVelocityTraversesOffMeshLink(t *testing.T) {
	nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 6, 6, 0),
		quadXZ(6, 0, 12, 6, 0),
		quadXZ(18, 0, 24, 6, 4),
	}, math3d.DegToRad(45))
	if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start: math3d.Vec3{X: 11, Y: 0, Z: 3},
		End:   math3d.Vec3{X: 19, Y: 4, Z: 3},
		ID:    42,
	}) {
		t.Fatal("AddOffMeshLink failed")
	}
	var jumps int
	agent := pathfind3d.NewNavAgent(nm, math3d.Vec3{X: 2, Y: 0, Z: 3}, pathfind3d.NavAgentConfig{
		MaxSpeed:         10,
		MaxAcceleration:  60,
		StoppingDistance: 0.35,
		OnLink: func(a *pathfind3d.NavAgent, ev pathfind3d.NavLinkEvent, dt time.Duration) bool {
			jumps++
			a.SetPosition(ev.End)
			return true
		},
	})
	agent.SetTarget(math3d.Vec3{X: 22, Y: 4, Z: 3})

	const dt = time.Second / 60
	for range 400 {
		if _, active := agent.TraversingLink(); active {
			if v := agent.DesiredVelocity(); v != (math3d.Vec3{}) {
				t.Fatalf("DesiredVelocity during link traversal = %v, want zero", v)
			}
		}
		agent.StepWithVelocity(agent.DesiredVelocity(), dt)
		if agent.State() == pathfind3d.NavAgentArrived {
			break
		}
	}
	if agent.State() != pathfind3d.NavAgentArrived || jumps != 1 {
		t.Fatalf("agent did not cross the off-mesh link: state=%v pos=%v jumps=%d", agent.State(), agent.Pos(), jumps)
	}
}
