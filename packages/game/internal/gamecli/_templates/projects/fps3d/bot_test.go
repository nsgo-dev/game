package main

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// bot plays the match the way a careful player does, for the playthrough tests: it turns toward the visible enemy nearest its crosshair, fires once the crosshair is on it, walks to the health pickup when hurt, and otherwise holds its ground and reloads.
type bot struct{}

// intent is what the bot wants this step: how far to turn and tilt, whether to fire or reload, and where to walk.
type intent struct {
	turn, tilt   float32
	fire, reload bool
	moveX, moveY float32
}

// decide looks at m and returns the bot's intent for the next step, turning at most maxTurn radians.
func (bot) decide(m *match, maxTurn float32) intent {
	var in intent
	p := &m.player
	if target, aimPoint, ok := visibleTarget(m); ok {
		eye := m.eye()
		to := aimPoint.Sub(eye)
		wantYaw := yawTo(eye, aimPoint)
		wantPitch := float32(math.Asin(float64(to.Y / to.Len())))
		in.turn = clamp(wrapAngle(wantYaw-p.yaw), -maxTurn, maxTurn)
		in.tilt = clamp(wantPitch-p.pitch, -maxTurn, maxTurn)
		tolerance := float32(math.Atan(float64(target.kind.Radius * 0.7 / to.Len())))
		in.fire = abs32(wrapAngle(wantYaw-p.yaw-in.turn)) < tolerance && abs32(wantPitch-p.pitch-in.tilt) < tolerance
		// Strafe from side to side to make the bolts miss, or fall back to the health pickup when hurt.
		if pk, ok := healthPickup(m); ok && p.health < m.arena.Player.Health/2 {
			in.moveX, in.moveY, _ = walkToward(m, pk, maxTurn)
		} else if int(m.clock/1.2)%2 == 0 {
			in.moveX = 1
		} else {
			in.moveX = -1
		}
		return in
	}
	// Nobody in sight: level the view toward the nearest enemy, top up the magazine and heal when hurt.
	in.tilt = clamp(-p.pitch, -maxTurn, maxTurn)
	nearest := float32(math.MaxFloat32)
	for _, e := range m.enemies {
		if d := horizontalDist(e.ctrl.Pos, p.ctrl.Pos); d < nearest {
			nearest = d
			in.turn = clamp(wrapAngle(yawTo(p.ctrl.Pos, e.ctrl.Pos)-p.yaw), -maxTurn, maxTurn)
		}
	}
	w := p.weapons[p.weapon]
	in.reload = w.mag < w.cfg.Magazine/2
	if pk, ok := healthPickup(m); ok && p.health < m.arena.Player.Health*6/10 {
		in.moveX, in.moveY, in.turn = walkToward(m, pk, maxTurn)
	}
	return in
}

// healthPickup returns where a health pickup is waiting.
func healthPickup(m *match) (physics3d.Vec3, bool) {
	for _, pk := range m.pickups {
		if pk.kind.Effect == EffectHealth && pk.respawn == 0 {
			return pk.pos, true
		}
	}
	return physics3d.Vec3{}, false
}

// visibleTarget returns the enemy closest to the crosshair that the player has a clear line of sight to, as a player turns to the threat already in front of them before one behind, and the point to aim at: its center, or its eye when only that shows over cover.
func visibleTarget(m *match) (*enemy, physics3d.Vec3, bool) {
	var best *enemy
	var bestPoint physics3d.Vec3
	bestCost := float32(math.MaxFloat32)
	eye := m.eye()
	for _, e := range m.enemies {
		// The angle off the crosshair counts most; distance breaks near ties.
		cost := abs32(wrapAngle(yawTo(eye, e.center())-m.player.yaw)) + eye.Dist(e.center())*0.01
		if cost >= bestCost {
			continue
		}
		for _, p := range []physics3d.Vec3{e.center(), e.eye()} {
			if _, blocked := m.world.RaycastFirstLayer(eye, p, layerWall); !blocked {
				best, bestPoint, bestCost = e, p, cost
				break
			}
		}
	}
	return best, bestPoint, best != nil
}

// walkToward returns the strafe and walk that move the player toward target, with the turn that faces it.
func walkToward(m *match, target physics3d.Vec3, maxTurn float32) (moveX, moveY, turn float32) {
	p := &m.player
	if horizontalDist(p.ctrl.Pos, target) < 0.3 {
		return 0, 0, 0
	}
	dir := target.Sub(p.ctrl.Pos)
	dir.Y = 0
	dir = dir.Norm()
	return dir.Dot(flatRight(p.yaw)), dir.Dot(flatForward(p.yaw)), clamp(wrapAngle(yawTo(p.ctrl.Pos, target)-p.yaw), -maxTurn, maxTurn)
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

func asin(v float32) float64 { return math.Asin(float64(v)) }
