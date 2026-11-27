package main

import (
	"github.com/nsgo-dev/game/pkg/fighting"
	"github.com/nsgo-dev/game/pkg/framedata"
)

// control is one player's input on one step: an absolute numpad direction, 4 for left and 6 for right, and the held buttons, bit i for the move set's Buttons()[i].
type control struct {
	dir  fighting.Direction
	held uint32
}

// neutral is a control with nothing held.
var neutral = control{dir: fighting.Neutral}

// phase is where a match is in its rounds.
type phase int

const (
	// phaseIntro announces the round; the fighters stand.
	phaseIntro phase = iota
	// phaseFight is the round being fought.
	phaseFight
	// phaseRoundOver shows how the round ended.
	phaseRoundOver
	// phaseMatchOver is the end of the match.
	phaseMatchOver
)

// eventKind is what an event reports.
type eventKind int

const (
	evRound eventKind = iota
	evFight
	evHit
	evBlock
	evCombo
	evKO
	evTimeUp
	evMatchOver
)

// event is something that happened on a step, for the game to play a sound or show a banner.
type event struct {
	kind eventKind
	// player is the index of the player it concerns: the attacker of a hit, block or combo, and the round's or match's winner, -1 for a draw.
	player int
	// name is a combo's name.
	name string
	// x and y are where a hit or a block landed.
	x, y float32
}

// spark is a hit or block flash on screen.
type spark struct {
	x, y    float32
	life    int
	blocked bool
}

// sparkLife is how many steps a spark shows.
const sparkLife = 12

// match is a one-on-one match: two players, their projectiles, the round, the timer and the frame data world that resolves their hits. It knows nothing of input devices or the screen, so tests drive it step by step.
type match struct {
	rules   *rules
	players [2]*player
	shots   []*projectile
	world   framedata.World

	phase phase
	// phaseLeft is the steps left in phaseIntro or phaseRoundOver.
	phaseLeft int
	round     int
	// clock is the round's steps left.
	clock int
	// hitstop is the steps left of the freeze after a hit.
	hitstop int
	// winner is the round's winner once it ends and the match's once it is over: 0 or 1, or -1 for a draw.
	winner int
	// steps counts the match's steps.
	steps  int
	events []event
	sparks []spark
	hits   []framedata.Hit
}

// newMatch starts a match between the roster's fighters a and b, first round's intro.
func newMatch(r *roster, a, b *fighter) *match {
	m := &match{rules: &r.file.Rules}
	m.players[0] = newPlayer(0, a)
	m.players[1] = newPlayer(1, b)
	m.startRound()
	return m
}

// startRound resets both fighters and the timer for the next round.
func (m *match) startRound() {
	m.round++
	r := m.rules
	mid := (r.StageLeft + r.StageRight) / 2
	m.players[0].reset(mid-r.StartGap/2, r.Floor, false)
	m.players[1].reset(mid+r.StartGap/2, r.Floor, true)
	m.shots = m.shots[:0]
	m.sparks = m.sparks[:0]
	m.hitstop = 0
	m.clock = r.RoundSeconds * stepRate
	m.phase, m.phaseLeft = phaseIntro, r.IntroSteps
	m.emit(event{kind: evRound, player: -1})
}

// seconds returns the timer as the HUD shows it, rounded up.
func (m *match) seconds() int { return (m.clock + stepRate - 1) / stepRate }

func (m *match) emit(e event) { m.events = append(m.events, e) }

// opponent returns the other player.
func (m *match) opponent(p *player) *player { return m.players[1-p.idx] }

// step advances the match one fixed step with each player's control and returns what happened on it.
func (m *match) step(in [2]control) []event {
	m.events = m.events[:0]
	m.steps++
	m.ageSparks()
	switch m.phase {
	case phaseIntro:
		for i, p := range m.players {
			// The recognizers listen through the intro, so a charge held before the round starts counts.
			p.see(in[i])
		}
		m.settle()
		if m.phaseLeft--; m.phaseLeft <= 0 {
			m.phase = phaseFight
			m.emit(event{kind: evFight, player: -1})
		}
	case phaseFight:
		m.fight(in)
	case phaseRoundOver:
		m.settle()
		if m.phaseLeft--; m.phaseLeft <= 0 {
			m.endRound()
		}
	}
	return m.events
}

// fight is one step of a round being fought.
func (m *match) fight(in [2]control) {
	if m.hitstop > 0 {
		m.hitstop--
		for i, p := range m.players {
			p.bufferDuringHitstop(in[i])
		}
		return
	}
	m.clock--
	for _, p := range m.players {
		if o := m.opponent(p); o.f.Stun() == 0 || o.blocking() {
			p.combo = 0
		}
	}
	for i, p := range m.players {
		p.control(m, in[i])
	}
	m.world.Fighters = append(m.world.Fighters[:0], &m.players[0].f, &m.players[1].f)
	for _, s := range m.shots {
		m.world.Fighters = append(m.world.Fighters, &s.f)
	}
	m.hits = append(m.hits[:0], m.world.Step()...)
	for i := range m.hits {
		m.applyHit(&m.hits[i])
	}
	for _, p := range m.players {
		p.move(m)
		p.throw(m)
	}
	m.moveShots()
	m.pushApart()
	m.checkRoundEnd()
}

// owner returns the player whose fighter or projectile f is.
func (m *match) owner(f *framedata.Fighter) *player {
	for _, p := range m.players {
		if &p.f == f {
			return p
		}
	}
	for _, s := range m.shots {
		if &s.f == f {
			return s.owner
		}
	}
	return nil
}

// applyHit deals a hit's damage, puts the victim in its hurt or guard pose, pushes it back and freezes the action.
func (m *match) applyHit(h *framedata.Hit) {
	a, v := m.owner(h.Attacker), m.owner(h.Victim)
	if a == nil || v == nil || v.health <= 0 {
		return
	}
	extra := a.fighter.extra(h.Move.Name)
	damage := 0
	if h.Blocked {
		if extra.Special {
			damage = h.Damage / m.rules.ChipDivisor
		}
		v.guardPose(v.crouching())
		m.emit(event{kind: evBlock, player: a.idx, x: h.X, y: h.Y})
	} else {
		a.combo++
		scale := max(m.rules.MinScaling, 100-m.rules.ComboScaling*(a.combo-1))
		damage = h.Damage * scale / 100
		v.pose("hurt")
		if v.airborne {
			v.vy = min(v.vy, -3)
			v.drift = 0
		}
		m.emit(event{kind: evHit, player: a.idx, x: h.X, y: h.Y})
		if a.combo >= 2 && a.pendingCombo != "" {
			m.emit(event{kind: evCombo, player: a.idx, name: a.pendingCombo})
			a.pendingCombo = ""
		}
	}
	v.health = max(0, v.health-damage)
	v.push = h.KnockbackX
	// A victim against the wall cannot give ground, so a melee attacker is pushed away instead.
	if h.Attacker == &a.f && v.atWall(m.rules) {
		a.push = -h.KnockbackX
	}
	m.hitstop = m.rules.Hitstop
	m.sparks = append(m.sparks, spark{x: h.X, y: h.Y, life: sparkLife, blocked: h.Blocked})
}

// moveShots flies the projectiles and removes those that struck, met another or ran out.
func (m *match) moveShots() {
	for _, s := range m.shots {
		hit, blocked := s.f.Contact()
		s.life--
		s.f.X += s.vx
		if hit || blocked || s.life <= 0 || s.f.X < m.rules.StageLeft-40 || s.f.X > m.rules.StageRight+40 {
			s.dead = true
		}
	}
	// Two projectiles thrown at each other cancel out.
	for i, a := range m.shots {
		for _, b := range m.shots[i+1:] {
			if a.owner != b.owner && !a.dead && !b.dead && overlapping(&a.f, &b.f) {
				a.dead, b.dead = true, true
				m.sparks = append(m.sparks, spark{x: (a.f.X + b.f.X) / 2, y: a.f.Y - 50, life: sparkLife})
			}
		}
	}
	live := m.shots[:0]
	for _, s := range m.shots {
		if s.dead {
			s.owner.shot = nil
			continue
		}
		live = append(live, s)
	}
	clear(m.shots[len(live):])
	m.shots = live
}

// overlapping reports whether any active box of a overlaps one of b.
func overlapping(a, b *framedata.Fighter) bool {
	var ba, bb [8]framedata.WorldBox
	for _, x := range a.Boxes(ba[:0]) {
		for _, y := range b.Boxes(bb[:0]) {
			if _, ok := x.Rect.Intersection(y.Rect); ok {
				return true
			}
		}
	}
	return false
}

// pushApart keeps the fighters from standing inside each other and inside the stage. A fighter in the air passes over the other.
func (m *match) pushApart() {
	r := m.rules
	a, b := m.players[0], m.players[1]
	if abs32(a.f.Y-b.f.Y) < 60 {
		left, right := a, b
		if b.f.X < a.f.X || (b.f.X == a.f.X && a.f.FacingLeft) {
			left, right = b, a
		}
		if gap := right.f.X - left.f.X; gap < r.PushWidth {
			d := (r.PushWidth - gap) / 2
			left.f.X -= d
			right.f.X += d
			if left.f.X < r.StageLeft {
				right.f.X += r.StageLeft - left.f.X
				left.f.X = r.StageLeft
			}
			if right.f.X > r.StageRight {
				left.f.X -= right.f.X - r.StageRight
				right.f.X = r.StageRight
			}
		}
	}
	for _, p := range m.players {
		p.f.X = clamp32(p.f.X, r.StageLeft, r.StageRight)
	}
}

// checkRoundEnd ends the round on a knockout or when time runs out.
func (m *match) checkRoundEnd() {
	a, b := m.players[0], m.players[1]
	ko := a.health == 0 || b.health == 0
	if !ko && m.clock > 0 {
		return
	}
	switch {
	case ko && a.health == 0 && b.health == 0:
		m.winner = -1
	case ko && a.health == 0:
		m.winner = 1
	case ko:
		m.winner = 0
	default:
		// On time, the fighter with more of its health left wins; health is compared as a share so a sturdier fighter gains nothing from it.
		sa, sb := a.health*b.fighter.def.Health, b.health*a.fighter.def.Health
		switch {
		case sa > sb:
			m.winner = 0
		case sb > sa:
			m.winner = 1
		default:
			m.winner = -1
		}
	}
	for _, p := range m.players {
		p.f.Blocking = false
		if p.health == 0 {
			p.pose("ko")
		}
	}
	if m.winner >= 0 {
		m.players[m.winner].wins++
	} else {
		// A drawn round counts for both.
		a.wins++
		b.wins++
	}
	if ko {
		m.emit(event{kind: evKO, player: m.winner})
	} else {
		m.emit(event{kind: evTimeUp, player: m.winner})
	}
	m.phase, m.phaseLeft = phaseRoundOver, m.rules.OutroSteps
}

// endRound starts the next round or ends the match.
func (m *match) endRound() {
	a, b := m.players[0], m.players[1]
	need := m.rules.RoundsToWin
	if a.wins < need && b.wins < need {
		m.startRound()
		return
	}
	switch {
	case a.wins >= need && b.wins >= need:
		m.winner = -1
	case a.wins >= need:
		m.winner = 0
	default:
		m.winner = 1
	}
	m.phase = phaseMatchOver
	m.emit(event{kind: evMatchOver, player: m.winner})
}

// settle lets fighters in the air come down and knockback die out outside the fight, where no move starts and nothing hits.
func (m *match) settle() {
	for _, p := range m.players {
		p.fall(m.rules)
		p.f.X = clamp32(p.f.X+p.push, m.rules.StageLeft, m.rules.StageRight)
		p.push = decay(p.push)
	}
}

func (m *match) ageSparks() {
	live := m.sparks[:0]
	for _, s := range m.sparks {
		if s.life--; s.life > 0 {
			live = append(live, s)
		}
	}
	m.sparks = live
}

// threatened reports whether p's opponent is attacking within reach of p or has a projectile closing in: what makes holding back guard instead of walk away.
func (m *match) threatened(p *player) bool {
	o := m.opponent(p)
	if attacking(&o.f) && abs32(o.f.X-p.f.X) < 150 {
		return true
	}
	if s := o.shot; s != nil {
		d := p.f.X - s.f.X
		return abs32(d) < 170 && d*s.vx > 0
	}
	return false
}

// attacking reports whether f's move has a hitbox still to come or active.
func attacking(f *framedata.Fighter) bool {
	mv := f.Move()
	if mv == nil {
		return false
	}
	for _, b := range mv.Boxes {
		if b.Kind == framedata.KindHit && f.Frame() <= b.End {
			return true
		}
	}
	return false
}

// guardsAgainst reports whether p's guard stops what its opponent is attacking with: a crouching guard stops lows and mids and a standing one overheads and mids.
func (m *match) guardsAgainst(p *player, crouching bool) bool {
	o := m.opponent(p)
	check := func(f *framedata.Fighter) bool {
		mv := f.Move()
		if mv == nil {
			return true
		}
		switch o.fighter.extra(mv.Name).Level {
		case levelLow:
			return crouching
		case levelOverhead:
			return !crouching
		}
		return true
	}
	if !check(&o.f) {
		return false
	}
	if o.shot != nil && !check(&o.shot.f) {
		return false
	}
	return true
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp32(v, lo, hi float32) float32 {
	return max(lo, min(hi, v))
}

// decay slows knockback down and stops it once it is slight.
func decay(v float32) float32 {
	v *= 0.8
	if abs32(v) < 0.1 {
		return 0
	}
	return v
}
