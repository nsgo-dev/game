package pathfind_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
)

func uCorridorMesh() *pathfind.NavMesh {
	return pathfind.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 2, 10),
		rectPoly(2, 8, 8, 10),
		rectPoly(8, 0, 10, 10),
	})
}

func velocityAgentConfig() pathfind.NavAgentConfig {
	return pathfind.NavAgentConfig{
		MaxSpeed:         10,
		MaxAcceleration:  40,
		Radius:           0.3,
		ArrivalRadius:    2.5,
		StoppingDistance: 0.25,
	}
}

func TestNavAgentStepWithDesiredVelocityMatchesStep(t *testing.T) {
	mesh := uCorridorMesh()
	ref := pathfind.NewNavAgent(mesh, pt(1, 1), velocityAgentConfig())
	ext := pathfind.NewNavAgent(mesh, pt(1, 1), velocityAgentConfig())
	goal := pt(9, 1)
	ref.SetTarget(goal)
	ext.SetTarget(goal)

	const dt = time.Second / 60
	for step := range 600 {
		ref.Step(dt)
		ext.StepWithVelocity(ext.DesiredVelocity(), dt)
		if d := dist2D(ref.Pos(), ext.Pos()); d > 1e-3 {
			t.Fatalf("step %d: StepWithVelocity(DesiredVelocity()) at %v diverged from Step at %v by %.5f", step, ext.Pos(), ref.Pos(), d)
		}
		if ref.State() != ext.State() {
			t.Fatalf("step %d: state %v, want %v", step, ext.State(), ref.State())
		}
		if ref.State() == pathfind.NavAgentArrived {
			break
		}
	}
	if ext.State() != pathfind.NavAgentArrived {
		t.Fatalf("external-velocity agent did not arrive: state=%v pos=%v", ext.State(), ext.Pos())
	}
	if v := ext.DesiredVelocity(); v != (core.Point{}) {
		t.Fatalf("DesiredVelocity after arrival = %v, want zero", v)
	}
}

func TestNavAgentDesiredVelocityPointsAtNextCorner(t *testing.T) {
	agent := pathfind.NewNavAgent(uCorridorMesh(), pt(1, 1), velocityAgentConfig())
	if v := agent.DesiredVelocity(); v != (core.Point{}) {
		t.Fatalf("idle DesiredVelocity = %v, want zero", v)
	}
	agent.SetTarget(pt(9, 1))
	v := agent.DesiredVelocity()
	corner, _ := agent.NextCorner()
	if s := speed2D(v); math.Abs(s-10) > 1e-3 {
		t.Fatalf("DesiredVelocity speed = %.4f, want MaxSpeed 10", s)
	}
	dx, dy := float64(corner.X-agent.Pos().X), float64(corner.Y-agent.Pos().Y)
	vx, vy := float64(v.X), float64(v.Y)
	cross := (vx*dy - vy*dx) / (math.Hypot(vx, vy) * math.Hypot(dx, dy))
	dot := vx*dx + vy*dy
	if math.Abs(cross) > 1e-3 || dot <= 0 {
		t.Fatalf("DesiredVelocity %v does not point at next corner %v", v, corner)
	}
}

func TestNavAgentStepWithVelocityClampsAndStaysOnMesh(t *testing.T) {
	mesh := uCorridorMesh()
	cfg := velocityAgentConfig()
	agent := pathfind.NewNavAgent(mesh, pt(1, 1), cfg)
	agent.SetTarget(pt(9, 1))

	const dt = time.Second / 60
	// An avoidance result far above MaxSpeed pointing out of the mesh through the left wall.
	push := pt(-1000, 0)
	agent.StepWithVelocity(push, dt)
	if s, maxS := speed2D(agent.Vel()), float64(cfg.MaxAcceleration)*dt.Seconds()+1e-4; s > maxS {
		t.Fatalf("speed after one step = %.4f, want <= MaxAcceleration*dt %.4f", s, maxS)
	}
	for step := range 120 {
		agent.StepWithVelocity(push, dt)
		if !mesh.Contains(agent.Pos()) {
			t.Fatalf("step %d: agent left navmesh at %v", step, agent.Pos())
		}
		if s := speed2D(agent.Vel()); s > float64(cfg.MaxSpeed)+1e-4 {
			t.Fatalf("step %d: speed %.4f exceeds MaxSpeed", step, s)
		}
	}

	// Non-finite velocities are treated as zero: the agent decelerates instead of jumping.
	before := agent.Pos()
	agent.StepWithVelocity(pt(float32(math.NaN()), float32(math.Inf(1))), dt)
	if p := agent.Pos(); math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || dist2D(p, before) > float64(cfg.MaxSpeed)*dt.Seconds()+1e-4 {
		t.Fatalf("non-finite velocity moved agent from %v to %v", before, p)
	}

	// Handing control back to the corridor velocity still reaches the goal.
	for range 600 {
		agent.StepWithVelocity(agent.DesiredVelocity(), dt)
		if agent.State() == pathfind.NavAgentArrived {
			break
		}
	}
	if agent.State() != pathfind.NavAgentArrived {
		t.Fatalf("agent did not arrive after avoidance push: state=%v pos=%v", agent.State(), agent.Pos())
	}
}

func TestNavAgentStepWithVelocityTraversesLinks(t *testing.T) {
	mesh := pathfind.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),
		rectPoly(10, 0, 20, 10),
		rectPoly(30, 0, 40, 10),
	})
	if !mesh.AddOffMeshLink(pathfind.NavOffMeshLink{Start: pt(19, 5), End: pt(31, 5), ID: 7}) {
		t.Fatal("AddOffMeshLink failed")
	}
	var jumps int
	agent := pathfind.NewNavAgent(mesh, pt(2, 5), pathfind.NavAgentConfig{
		MaxSpeed:         20,
		MaxAcceleration:  100,
		StoppingDistance: 0.4,
		OnLink: func(a *pathfind.NavAgent, ev pathfind.NavLinkEvent, dt time.Duration) bool {
			jumps++
			a.SetPosition(ev.End)
			return true
		},
	})
	agent.SetTarget(pt(38, 5))

	const dt = time.Second / 60
	for range 400 {
		if _, active := agent.TraversingLink(); active {
			if v := agent.DesiredVelocity(); v != (core.Point{}) {
				t.Fatalf("DesiredVelocity during link traversal = %v, want zero", v)
			}
		}
		agent.StepWithVelocity(agent.DesiredVelocity(), dt)
		if agent.State() == pathfind.NavAgentArrived {
			break
		}
	}
	if agent.State() != pathfind.NavAgentArrived || jumps != 1 {
		t.Fatalf("agent did not cross the off-mesh link: state=%v pos=%v jumps=%d", agent.State(), agent.Pos(), jumps)
	}
}
