package main

import (
	"strings"

	"github.com/nsgo-dev/game/pkg/fighting"
	"github.com/nsgo-dev/game/pkg/framedata"
)

// stepRate is the fixed steps per second the game runs at, which frame data is counted in.
const stepRate = 60

// comboWindow is how many steps a recognized combo waits for its hits to land before it is forgotten.
const comboWindow = 40

// player is one side of a match: a fighter's frame data state, its input recognizer and what frame data leaves to the game, its position in the air, knockback, health and rounds won.
type player struct {
	idx     int
	fighter *fighter
	f       framedata.Fighter
	rec     *fighting.Recognizer

	health int
	wins   int
	// raw and input are this step's direction, absolute and relative to the side the fighter faces.
	raw, input fighting.Direction
	// walk is -1, 0 or 1 while the fighter walks back, stands or walks forward.
	walk int
	// airborne is set from a jump or a lifting motion until the fighter lands; vy is its vertical speed and drift its sideways speed in a jump.
	airborne    bool
	vy, drift   float32
	airAttacked bool
	// push is the knockback still moving the fighter, in points per step.
	push float32
	// shot is the fighter's projectile in flight.
	shot *projectile
	// combo counts the hits in a row the fighter has landed while its opponent could not act.
	combo int
	// pendingCombo is a combo of the move set whose inputs were just completed; it is announced once its hits land.
	pendingCombo string
	pendingLeft  int
	// queued are the moves recognized during hitstop, tried first once it ends.
	queued []string
}

// projectile is a projectile in flight: a frame data fighter of its own, on its thrower's team so it never hits the thrower.
type projectile struct {
	owner *player
	f     framedata.Fighter
	vx    float32
	life  int
	dead  bool
}

func newPlayer(idx int, ft *fighter) *player {
	rec, err := fighting.NewRecognizer(ft.moves)
	if err != nil {
		// loadRoster parsed the move set, which validates it, so this cannot fail.
		panic(err)
	}
	return &player{idx: idx, fighter: ft, rec: rec}
}

// reset puts the fighter at x on the floor at full health for a new round.
func (p *player) reset(x, floor float32, faceLeft bool) {
	p.f = framedata.Fighter{Moves: p.fighter.frames, Neutral: "idle", X: x, Y: floor, FacingLeft: faceLeft, Team: p.idx + 1}
	_ = p.f.Force("idle")
	p.rec.Reset()
	p.health = p.fighter.def.Health
	p.raw, p.input, p.walk = fighting.Neutral, fighting.Neutral, 0
	p.airborne, p.vy, p.drift, p.airAttacked, p.push = false, 0, 0, false, 0
	p.shot, p.combo, p.pendingCombo, p.pendingLeft = nil, 0, "", 0
	p.queued = p.queued[:0]
}

// facing returns the side the fighter faces, for the recognizer.
func (p *player) facing() fighting.Facing {
	if p.f.FacingLeft {
		return fighting.FaceLeft
	}
	return fighting.FaceRight
}

// forward returns 1 when the fighter faces right and -1 when it faces left.
func (p *player) forward() float32 {
	if p.f.FacingLeft {
		return -1
	}
	return 1
}

// moveName returns the name of the frame data move the fighter plays.
func (p *player) moveName() string {
	if mv := p.f.Move(); mv != nil {
		return mv.Name
	}
	return ""
}

// crouching reports whether the fighter's input holds down.
func (p *player) crouching() bool { return p.input.Y() < 0 }

// blocking reports whether the fighter is in a guard pose.
func (p *player) blocking() bool {
	name := p.moveName()
	return name == "guard" || name == "crouch_guard"
}

// pose plays the looping move name unless the fighter already plays it.
func (p *player) pose(name string) {
	if p.moveName() != name {
		// Every pose is a stance move, which loadRoster checked the frame data has.
		_ = p.f.Force(name)
	}
}

// guardPose plays the standing or crouching guard.
func (p *player) guardPose(crouch bool) {
	if crouch {
		p.pose("crouch_guard")
	} else {
		p.pose("guard")
	}
}

// atWall reports whether the fighter stands against a wall of the stage.
func (p *player) atWall(r *rules) bool {
	return p.f.X <= r.StageLeft+1 || p.f.X >= r.StageRight-1
}

// see feeds this step's input to the recognizer and keeps track of combos it completes.
func (p *player) see(in control) fighting.Result {
	res := p.rec.Feed(in.dir, in.held, p.facing())
	p.raw, p.input = in.dir, p.facing().Relative(in.dir)
	if p.pendingLeft > 0 {
		if p.pendingLeft--; p.pendingLeft == 0 {
			p.pendingCombo = ""
		}
	}
	for _, c := range res.Combos {
		p.pendingCombo, p.pendingLeft = c.Name, comboWindow
	}
	return res
}

// bufferDuringHitstop reads input while the action is frozen and keeps the moves it completes for when it moves again, so a cancel input during hitstop is not lost.
func (p *player) bufferDuringHitstop(in control) {
	for _, m := range p.see(in).Moves {
		if len(p.queued) < 4 {
			p.queued = append(p.queued, m.Name)
		}
	}
}

// control acts on this step's input before the frame data world steps: it starts a recognized move when the fighter may, or else picks the stance, guard included, from the direction held.
func (p *player) control(m *match, in control) {
	res := p.see(in)
	p.walk = 0
	p.f.Blocking = false
	if p.health == 0 {
		return
	}
	started := false
	for _, name := range p.queued {
		if started = p.tryStart(name); started {
			break
		}
	}
	p.queued = p.queued[:0]
	for i := 0; !started && i < len(res.Moves); i++ {
		started = p.tryStart(res.Moves[i].Name)
	}
	if !started {
		p.stance(m)
	}
}

// tryStart starts the frame data move for the recognized move name, as the fighter stands, crouches or jumps, if its current move allows it.
func (p *player) tryStart(name string) bool {
	mv, ok := p.fighter.variant(name, p.airborne, p.crouching())
	if !ok || p.airborne && p.airAttacked {
		return false
	}
	if p.fighter.extra(mv).Projectile != nil && p.shot != nil {
		return false
	}
	if !p.f.Start(mv) {
		return false
	}
	p.airAttacked = p.airborne
	return true
}

// stance picks what the fighter does when it starts no move: it keeps a guard through blockstun, turns to face its opponent, jumps, guards a threat while holding back, crouches, walks or stands.
func (p *player) stance(m *match) {
	if p.airborne {
		if p.f.Actionable() {
			p.pose("jump")
		}
		return
	}
	if p.f.Stun() > 0 {
		if p.blocking() {
			p.guardPose(p.crouching())
			p.f.Blocking = m.guardsAgainst(p, p.crouching())
		}
		return
	}
	if !p.f.Actionable() {
		return
	}
	if o := m.opponent(p); o.f.X != p.f.X {
		p.f.FacingLeft = o.f.X < p.f.X
	}
	p.input = p.facing().Relative(p.raw)
	switch in := p.input; {
	case in.Y() > 0:
		p.airborne = true
		p.vy = -p.fighter.def.JumpSpeed
		p.drift = float32(p.raw.X()) * p.fighter.def.JumpDrift
		p.pose("jump")
	case in.X() < 0 && m.threatened(p):
		p.guardPose(p.crouching())
		p.f.Blocking = m.guardsAgainst(p, p.crouching())
	case in.Y() < 0:
		p.pose("crouch")
	case in.X() != 0:
		p.pose("walk")
		p.walk = in.X()
	default:
		p.pose("idle")
	}
}

// move moves the fighter after the world stepped: by its move's motion, by walking, by falling and by knockback.
func (p *player) move(m *match) {
	lifted := false
	if mv := p.f.Move(); mv != nil {
		frame := p.f.Frame()
		for _, mo := range p.fighter.extra(mv.Name).Motion {
			if frame < mo.Start || frame > mo.End {
				continue
			}
			p.f.X += mo.DX * p.forward()
			if mo.DY != 0 {
				p.f.Y += mo.DY
				p.airborne, p.vy, p.drift, lifted = true, 0, 0, true
			}
		}
	}
	if p.walk != 0 && !p.airborne && p.moveName() == "walk" {
		speed := p.fighter.def.WalkSpeed
		if p.walk < 0 {
			speed = p.fighter.def.BackSpeed
		}
		p.f.X += float32(p.walk) * speed * p.forward()
	}
	if !lifted {
		p.fall(m.rules)
	}
	p.f.X += p.push
	p.push = decay(p.push)
}

// fall applies gravity to a fighter in the air and lands it on the floor, where a jump or an air attack ends.
func (p *player) fall(r *rules) {
	if !p.airborne {
		return
	}
	p.vy += r.Gravity
	p.f.Y += p.vy
	p.f.X += p.drift
	if p.f.Y < r.Floor {
		return
	}
	p.f.Y = r.Floor
	p.airborne, p.vy, p.drift, p.airAttacked = false, 0, 0, false
	if name := p.moveName(); p.health > 0 && p.f.Stun() == 0 && (name == "jump" || strings.HasPrefix(name, "air_")) {
		p.pose("idle")
	}
}

// throw launches the projectile of the fighter's move on the frame it names.
func (p *player) throw(m *match) {
	mv := p.f.Move()
	if mv == nil || p.shot != nil {
		return
	}
	def := p.fighter.extra(mv.Name).Projectile
	if def == nil || p.f.Frame() != def.Frame {
		return
	}
	s := &projectile{owner: p, vx: p.forward() * def.Speed, life: def.Life}
	s.f = framedata.Fighter{Moves: p.fighter.frames, X: p.f.X + p.forward()*def.X, Y: p.f.Y + def.Y, FacingLeft: p.f.FacingLeft, Team: p.idx + 1}
	// loadRoster checked that the projectile's move is in the frame data.
	_ = s.f.Force(def.Move)
	p.shot = s
	m.shots = append(m.shots, s)
}
