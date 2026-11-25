package physics

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultSoftSubsteps is the default number of XPBD substeps per SoftWorld.Step.
	DefaultSoftSubsteps = 8
	// DefaultSoftIterations is the default number of constraint solver iterations per substep.
	DefaultSoftIterations = 6
)

// Particle is a 2D point mass simulated by SoftWorld using Extended Position-Based Dynamics (XPBD).
//
// Coordinates follow NSGo's screen space (+X right, +Y down). Set Pinned to true or
// InvMass to 0 to fix a particle in place. Zero Layer or Mask values default to LayerAll.
type Particle struct {
	X, Y         float32
	PrevX, PrevY float32
	VX, VY       float32
	InvMass      float32
	Radius       float32
	Friction     float32
	Restitution  float32
	Pinned       bool
	Layer        LayerMask
	Mask         LayerMask

	forceX float64
	forceY float64
}

// IsPinned reports whether p has infinite mass (Pinned is true or InvMass <= 0).
func (p *Particle) IsPinned() bool {
	return p == nil || p.Pinned || p.InvMass <= 0
}

// SetMass sets p's inverse mass to 1/mass when mass > 0, or 0 when mass <= 0.
func (p *Particle) SetMass(mass float32) {
	if p == nil {
		return
	}
	if mass <= 0 || math.IsNaN(float64(mass)) || math.IsInf(float64(mass), 0) {
		p.InvMass = 0
		return
	}
	p.InvMass = 1 / mass
}

// ApplyForce accumulates a world-space force applied to p during the next SoftWorld.Step.
func (p *Particle) ApplyForce(fx, fy float32) {
	if p == nil || p.IsPinned() {
		return
	}
	if math.IsNaN(float64(fx)) || math.IsInf(float64(fx), 0) || math.IsNaN(float64(fy)) || math.IsInf(float64(fy), 0) {
		return
	}
	p.forceX += float64(fx)
	p.forceY += float64(fy)
}

func (p *Particle) effectiveInvMass() float64 {
	if p.Pinned || p.InvMass <= 0 {
		return 0
	}
	return float64(p.InvMass)
}

// DistanceConstraint constrains the Euclidean distance between particles A and B.
// When MaxOnly is true, the constraint acts as a unilateral tether that only pulls
// particles together when their distance exceeds RestLength.
type DistanceConstraint struct {
	A, B       int
	RestLength float32
	Compliance float32
	MaxOnly    bool

	lambda float64
}

// BendingConstraint constrains the signed angle at hinge particle B between
// edges (A - B) and (C - B) toward RestAngle (in radians).
type BendingConstraint struct {
	A, B, C    int
	RestAngle  float32
	Compliance float32

	lambda float64
}

// AreaConstraint preserves (or inflates, via Pressure) the signed area of a closed
// polygon loop of particles. Pressure <= 0 defaults to 1.0.
type AreaConstraint struct {
	Particles  []int
	RestArea   float32
	Pressure   float32
	Compliance float32

	lambda float64
}

// ShapeMatchConstraint pulls a group of particles toward the closest rigid-body
// transformation (translation + 2D rotation) of their rest offsets.
type ShapeMatchConstraint struct {
	Particles   []int
	RestOffsets [][2]float32
	Stiffness   float32
}

// RigidAttachment couples particle Particle to RigidBody Body at LocalAnchor.
// When Compliance is 0, the attachment acts as a rigid two-way pin; positive
// Compliance makes it elastic while still transferring reaction impulses back to Body.
type RigidAttachment struct {
	Particle    int
	Body        *RigidBody
	LocalAnchor core.Point
	Compliance  float32

	lambda float64
}

type rigidBodyPose2D struct {
	body  *RigidBody
	x, y  float64
	angle float64
}

type trackedBody2D struct {
	body        *RigidBody
	staticOrNil bool
	targetX     float64
	targetY     float64
	targetAngle float64
	stepDX      float64
	stepDY      float64
	stepDAngle  float64
	curX        float64
	curY        float64
	curAngle    float64
	corrX       float64
	corrY       float64
	corrAngle   float64
	invMass     float64
	invInertia  float64
}

func (tb *trackedBody2D) worldAnchor(local core.Point) (ax, ay, rx, ry float64) {
	s, c := detmath.Sincos(tb.curAngle)
	lx, ly := float64(local.X), float64(local.Y)
	rx = float64(lx*c) - float64(ly*s)
	ry = float64(lx*s) + float64(ly*c)
	return tb.curX + rx, tb.curY + ry, rx, ry
}

func (tb *trackedBody2D) generalizedInvMass(rx, ry, nx, ny float64) float64 {
	if tb.staticOrNil {
		return 0
	}
	rn := float64(rx*ny) - float64(ry*nx)
	return tb.invMass + float64(float64(tb.invInertia*rn)*rn)
}

func (tb *trackedBody2D) applyPosImpulse(px, py, rx, ry float64) {
	if tb.staticOrNil {
		return
	}
	dx := float64(px * tb.invMass)
	dy := float64(py * tb.invMass)
	da := float64((float64(rx*py) - float64(ry*px)) * tb.invInertia)
	tb.curX += dx
	tb.curY += dy
	tb.curAngle += da
	tb.corrX += dx
	tb.corrY += dy
	tb.corrAngle += da
}

// SoftWorld simulates 2D XPBD particles, constraints (distance, bending, area/pressure,
// shape matching), two-way rigid-body attachments, and collisions against World bodies,
// RigidWorld bodies, and TileGrid tiles in deterministic order.
type SoftWorld struct {
	Particles    []Particle
	Distances    []DistanceConstraint
	Bendings     []BendingConstraint
	Areas        []AreaConstraint
	ShapeMatches []ShapeMatchConstraint
	Attachments  []RigidAttachment

	// World provides optional static/kinematic bodies and a TileGrid to collide against.
	World *World
	// RigidWorld provides optional 2D rigid bodies to collide against and couple with.
	RigidWorld *RigidWorld
	// Tiles provides an optional standalone TileGrid (collided in addition to World.Tiles when distinct).
	Tiles TileGrid
	// TileLayer and TileMask filter particle collisions against tiles (zero defaults to LayerAll).
	TileLayer LayerMask
	TileMask  LayerMask

	GravityX   float32
	GravityY   float32
	Damping    float32
	Substeps   int
	Iterations int

	bodyHistory []rigidBodyPose2D
}

// NewSoftWorld returns a SoftWorld configured with sensible defaults (8 substeps,
// 6 solver iterations, and mild linear damping).
func NewSoftWorld() *SoftWorld {
	return &SoftWorld{
		Damping:    0.02,
		Substeps:   DefaultSoftSubsteps,
		Iterations: DefaultSoftIterations,
	}
}

// AddParticle appends p to sw.Particles, initializes its previous position to (p.X, p.Y),
// and returns its index.
func (sw *SoftWorld) AddParticle(p Particle) int {
	p.PrevX = p.X
	p.PrevY = p.Y
	if p.Friction < 0 {
		p.Friction = 0
	}
	if p.Restitution < 0 {
		p.Restitution = 0
	} else if p.Restitution > 1 {
		p.Restitution = 1
	}
	idx := len(sw.Particles)
	sw.Particles = append(sw.Particles, p)
	return idx
}

// PinParticle marks particle idx as pinned (immovable).
func (sw *SoftWorld) PinParticle(idx int) {
	if idx >= 0 && idx < len(sw.Particles) {
		sw.Particles[idx].Pinned = true
		sw.Particles[idx].VX = 0
		sw.Particles[idx].VY = 0
	}
}

// UnpinParticle clears particle idx's Pinned flag.
func (sw *SoftWorld) UnpinParticle(idx int) {
	if idx >= 0 && idx < len(sw.Particles) {
		sw.Particles[idx].Pinned = false
	}
}

// AddDistance adds a bilateral distance constraint between particles a and b.
// If restLength < 0, the current Euclidean distance between a and b is used.
func (sw *SoftWorld) AddDistance(a, b int, restLength, compliance float32) int {
	if restLength < 0 && sw.validParticle(a) && sw.validParticle(b) {
		pa, pb := sw.Particles[a], sw.Particles[b]
		restLength = float32(detmath.Hypot(float64(pb.X)-float64(pa.X), float64(pb.Y)-float64(pa.Y)))
	}
	if restLength < 0 {
		restLength = 0
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Distances)
	sw.Distances = append(sw.Distances, DistanceConstraint{
		A:          a,
		B:          b,
		RestLength: restLength,
		Compliance: compliance,
	})
	return idx
}

// AddTether adds a unilateral maximum-distance constraint between particles a and b
// that activates only when their separation exceeds maxLength.
func (sw *SoftWorld) AddTether(a, b int, maxLength, compliance float32) int {
	if maxLength < 0 && sw.validParticle(a) && sw.validParticle(b) {
		pa, pb := sw.Particles[a], sw.Particles[b]
		maxLength = float32(detmath.Hypot(float64(pb.X)-float64(pa.X), float64(pb.Y)-float64(pa.Y)))
	}
	if maxLength < 0 {
		maxLength = 0
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Distances)
	sw.Distances = append(sw.Distances, DistanceConstraint{
		A:          a,
		B:          b,
		RestLength: maxLength,
		Compliance: compliance,
		MaxOnly:    true,
	})
	return idx
}

// AddBending adds a 3-particle angular bending constraint centered at hinge b,
// recording the current signed angle between (a - b) and (c - b) as RestAngle.
func (sw *SoftWorld) AddBending(a, b, c int, compliance float32) int {
	var restAngle float32
	if sw.validParticle(a) && sw.validParticle(b) && sw.validParticle(c) {
		pa, pb, pc := sw.Particles[a], sw.Particles[b], sw.Particles[c]
		restAngle = float32(signedAngle2D(
			float64(pa.X)-float64(pb.X), float64(pa.Y)-float64(pb.Y),
			float64(pc.X)-float64(pb.X), float64(pc.Y)-float64(pb.Y),
		))
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Bendings)
	sw.Bendings = append(sw.Bendings, BendingConstraint{
		A:          a,
		B:          b,
		C:          c,
		RestAngle:  restAngle,
		Compliance: compliance,
	})
	return idx
}

// AddArea adds a closed-polygon area/pressure constraint over particles.
// RestArea is computed from the current particle positions, and pressure <= 0 defaults to 1.0.
func (sw *SoftWorld) AddArea(particles []int, pressure, compliance float32) int {
	copied := make([]int, len(particles))
	copy(copied, particles)
	restArea := sw.PolygonArea(copied)
	if pressure <= 0 {
		pressure = 1
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Areas)
	sw.Areas = append(sw.Areas, AreaConstraint{
		Particles:  copied,
		RestArea:   restArea,
		Pressure:   pressure,
		Compliance: compliance,
	})
	return idx
}

// AddShapeMatch adds a 2D rigid shape-matching constraint over particles, capturing their
// current positions relative to their weighted center of mass as rest offsets.
func (sw *SoftWorld) AddShapeMatch(particles []int, stiffness float32) int {
	copied := make([]int, len(particles))
	copy(copied, particles)
	if stiffness <= 0 {
		stiffness = 0.5
	} else if stiffness > 1 {
		stiffness = 1
	}
	var sumM, cx, cy float64
	for _, i := range copied {
		if !sw.validParticle(i) {
			continue
		}
		p := &sw.Particles[i]
		m := 1.0
		if p.InvMass > 0 {
			m = 1.0 / float64(p.InvMass)
		}
		sumM += m
		cx += float64(float64(p.X) * m)
		cy += float64(float64(p.Y) * m)
	}
	if sumM > 0 {
		cx /= sumM
		cy /= sumM
	}
	offsets := make([][2]float32, len(copied))
	for k, i := range copied {
		if sw.validParticle(i) {
			offsets[k] = [2]float32{
				float32(float64(sw.Particles[i].X) - cx),
				float32(float64(sw.Particles[i].Y) - cy),
			}
		}
	}
	idx := len(sw.ShapeMatches)
	sw.ShapeMatches = append(sw.ShapeMatches, ShapeMatchConstraint{
		Particles:   copied,
		RestOffsets: offsets,
		Stiffness:   stiffness,
	})
	return idx
}

// AttachRigidBody couples particleIdx to body at localAnchor with two-way reaction impulses.
// Set compliance to 0 for a rigid pin attachment or > 0 for an elastic attachment.
func (sw *SoftWorld) AttachRigidBody(particleIdx int, body *RigidBody, localAnchor core.Point, compliance float32) int {
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Attachments)
	sw.Attachments = append(sw.Attachments, RigidAttachment{
		Particle:    particleIdx,
		Body:        body,
		LocalAnchor: localAnchor,
		Compliance:  compliance,
	})
	return idx
}

// RopeLength returns the total Euclidean polyline length along the ordered particle indices.
func (sw *SoftWorld) RopeLength(particles []int) float32 {
	if len(particles) < 2 {
		return 0
	}
	var total float64
	for i := 0; i < len(particles)-1; i++ {
		a, b := particles[i], particles[i+1]
		if !sw.validParticle(a) || !sw.validParticle(b) {
			continue
		}
		pa, pb := sw.Particles[a], sw.Particles[b]
		total += detmath.Hypot(float64(pb.X)-float64(pa.X), float64(pb.Y)-float64(pa.Y))
	}
	return float32(total)
}

// PolygonArea returns the signed shoelace area of the closed polygon formed by particles.
func (sw *SoftWorld) PolygonArea(particles []int) float32 {
	n := len(particles)
	if n < 3 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		ia := particles[i]
		ib := particles[(i+1)%n]
		if !sw.validParticle(ia) || !sw.validParticle(ib) {
			continue
		}
		pa, pb := sw.Particles[ia], sw.Particles[ib]
		sum += float64(float64(pa.X)*float64(pb.Y)) - float64(float64(pb.X)*float64(pa.Y))
	}
	return float32(sum * 0.5)
}

// RopeConfig configures a 2D rope constructed by SoftWorld.AddRope.
type RopeConfig struct {
	StartX, StartY    float32
	EndX, EndY        float32
	Segments          int
	TotalMass         float32
	ParticleRadius    float32
	Compliance        float32
	EnableBending     bool
	BendingCompliance float32
	PinStart          bool
	PinEnd            bool
	Layer             LayerMask
	Mask              LayerMask
	Friction          float32
}

// AddRope constructs a chain of Segments+1 particles from (StartX, StartY) to (EndX, EndY)
// connected by distance constraints (plus unilateral tethers when Compliance == 0 so heavy
// attached loads cannot over-stretch the chain, and optional bending constraints).
func (sw *SoftWorld) AddRope(cfg RopeConfig) []int {
	segs := cfg.Segments
	if segs < 1 {
		segs = 1
	}
	totalMass := cfg.TotalMass
	if totalMass <= 0 {
		totalMass = 1
	}
	radius := cfg.ParticleRadius
	if radius <= 0 {
		radius = 2
	}
	friction := cfg.Friction
	if friction <= 0 {
		friction = 0.4
	}
	count := segs + 1
	invMass := float32(count) / totalMass

	indices := make([]int, count)
	for i := 0; i < count; i++ {
		t := float64(i) / float64(segs)
		x := float32(float64(cfg.StartX) + float64((float64(cfg.EndX)-float64(cfg.StartX))*t))
		y := float32(float64(cfg.StartY) + float64((float64(cfg.EndY)-float64(cfg.StartY))*t))
		pinned := (i == 0 && cfg.PinStart) || (i == segs && cfg.PinEnd)
		indices[i] = sw.AddParticle(Particle{
			X:        x,
			Y:        y,
			InvMass:  invMass,
			Radius:   radius,
			Friction: friction,
			Pinned:   pinned,
			Layer:    cfg.Layer,
			Mask:     cfg.Mask,
		})
	}

	totalLen := detmath.Hypot(float64(cfg.EndX)-float64(cfg.StartX), float64(cfg.EndY)-float64(cfg.StartY))
	segLen := float32(totalLen / float64(segs))
	for i := 0; i < segs; i++ {
		sw.AddDistance(indices[i], indices[i+1], segLen, cfg.Compliance)
	}
	if cfg.Compliance <= 0 && segs >= 2 {
		// Unilateral long-range tethers obey the triangle inequality even when the
		// rope wraps around obstacles and guarantee bounded stretch under large mass ratios.
		for i := 2; i <= segs; i++ {
			sw.AddTether(indices[0], indices[i], segLen*float32(i), 0)
		}
		if cfg.PinEnd {
			for i := 1; i <= segs-2; i++ {
				sw.AddTether(indices[segs], indices[i], segLen*float32(segs-i), 0)
			}
		}
	}
	if cfg.EnableBending && segs >= 2 {
		for i := 1; i < segs; i++ {
			sw.AddBending(indices[i-1], indices[i], indices[i+1], cfg.BendingCompliance)
		}
	}
	return indices
}

// SoftBodyConfig configures a 2D pressurized and/or shape-matched soft body constructed by SoftWorld.AddSoftBody.
type SoftBodyConfig struct {
	CenterX, CenterY    float32
	RadiusX, RadiusY    float32
	Segments            int
	TotalMass           float32
	ParticleRadius      float32
	EdgeCompliance      float32
	EnableBending       bool
	BendingCompliance   float32
	AreaCompliance      float32
	Pressure            float32
	UseShapeMatching    bool
	ShapeMatchStiffness float32
	Layer               LayerMask
	Mask                LayerMask
	Friction            float32
	Restitution         float32
}

// AddSoftBody builds a closed 2D soft-body loop around (CenterX, CenterY) with perimeter
// distance constraints, an AreaConstraint (pressure), and optional ShapeMatchConstraint / bending.
func (sw *SoftWorld) AddSoftBody(cfg SoftBodyConfig) []int {
	n := cfg.Segments
	if n < 3 {
		n = 12
	}
	rx := cfg.RadiusX
	if rx <= 0 {
		rx = 20
	}
	ry := cfg.RadiusY
	if ry <= 0 {
		ry = rx
	}
	totalMass := cfg.TotalMass
	if totalMass <= 0 {
		totalMass = 1
	}
	pRadius := cfg.ParticleRadius
	if pRadius <= 0 {
		pRadius = 2
	}
	friction := cfg.Friction
	if friction <= 0 {
		friction = 0.4
	}
	invMass := float32(n) / totalMass

	indices := make([]int, n)
	for i := 0; i < n; i++ {
		theta := 2 * math.Pi * float64(i) / float64(n)
		sin, cos := detmath.Sincos(theta)
		x := float32(float64(cfg.CenterX) + float64(float64(rx)*cos))
		y := float32(float64(cfg.CenterY) + float64(float64(ry)*sin))
		indices[i] = sw.AddParticle(Particle{
			X:           x,
			Y:           y,
			InvMass:     invMass,
			Radius:      pRadius,
			Friction:    friction,
			Restitution: cfg.Restitution,
			Layer:       cfg.Layer,
			Mask:        cfg.Mask,
		})
	}

	for i := 0; i < n; i++ {
		sw.AddDistance(indices[i], indices[(i+1)%n], -1, cfg.EdgeCompliance)
	}
	if cfg.EnableBending {
		for i := 0; i < n; i++ {
			sw.AddBending(indices[(i+n-1)%n], indices[i], indices[(i+1)%n], cfg.BendingCompliance)
		}
	}
	pressure := cfg.Pressure
	if pressure <= 0 {
		pressure = 1
	}
	sw.AddArea(indices, pressure, cfg.AreaCompliance)

	if cfg.UseShapeMatching || cfg.ShapeMatchStiffness > 0 {
		stiff := cfg.ShapeMatchStiffness
		if stiff <= 0 {
			stiff = 0.35
		}
		sw.AddShapeMatch(indices, stiff)
	}
	return indices
}

func (sw *SoftWorld) validParticle(idx int) bool {
	return idx >= 0 && idx < len(sw.Particles)
}

func (sw *SoftWorld) findBodyHistory(b *RigidBody) (rigidBodyPose2D, bool) {
	for _, h := range sw.bodyHistory {
		if h.body == b {
			return h, true
		}
	}
	return rigidBodyPose2D{}, false
}

func (sw *SoftWorld) saveBodyHistory(b *RigidBody, x, y, angle float64) {
	for i := range sw.bodyHistory {
		if sw.bodyHistory[i].body == b {
			sw.bodyHistory[i].x = x
			sw.bodyHistory[i].y = y
			sw.bodyHistory[i].angle = angle
			return
		}
	}
	sw.bodyHistory = append(sw.bodyHistory, rigidBodyPose2D{body: b, x: x, y: y, angle: angle})
}

type collisionHit2D struct {
	hit    bool
	nx, ny float64
	rest   float64
}

// Step advances the 2D XPBD particle and constraint simulation by dt deterministically.
// Call this on the fixed game loop after stepping RigidWorld so two-way rigid attachments
// stay synchronized without over-stretching.
func (sw *SoftWorld) Step(dt time.Duration) {
	if sw == nil || dt <= 0 || len(sw.Particles) == 0 {
		return
	}
	dtSec := dt.Seconds()
	if math.IsNaN(dtSec) || math.IsInf(dtSec, 0) || dtSec <= 0 {
		return
	}

	substeps := sw.Substeps
	if substeps <= 0 {
		substeps = DefaultSoftSubsteps
	}
	iters := sw.Iterations
	if iters <= 0 {
		iters = DefaultSoftIterations
	}
	h := dtSec / float64(substeps)
	invH := 1.0 / h
	invHSq := invH * invH

	// 1. Collect unique attached rigid bodies in deterministic attachment order.
	var tracked []trackedBody2D
	attachToTracked := make([]int, len(sw.Attachments))
	rigidAttachForParticle := make([]int, len(sw.Particles))
	for i := range rigidAttachForParticle {
		rigidAttachForParticle[i] = -1
	}
	for ai := range sw.Attachments {
		att := &sw.Attachments[ai]
		attachToTracked[ai] = -1
		if att.Body == nil || !sw.validParticle(att.Particle) {
			continue
		}
		ti := -1
		for k := range tracked {
			if tracked[k].body == att.Body {
				ti = k
				break
			}
		}
		if ti < 0 {
			b := att.Body
			b.prepareMass()
			tx, ty, ta := float64(b.X), float64(b.Y), float64(b.Angle)
			sx, sy, sa := tx, ty, ta
			if hist, ok := sw.findBodyHistory(b); ok {
				sx, sy, sa = hist.x, hist.y, hist.angle
			} else if !b.Static {
				sx = tx - float64(float64(b.VX)*dtSec)
				sy = ty - float64(float64(b.VY)*dtSec)
				sa = ta - float64(float64(b.AngularVel)*dtSec)
			}
			ti = len(tracked)
			tracked = append(tracked, trackedBody2D{
				body:        b,
				staticOrNil: b.Static || (b.InvMass <= 0 && b.InvInertia <= 0),
				targetX:     tx,
				targetY:     ty,
				targetAngle: ta,
				stepDX:      (tx - sx) / float64(substeps),
				stepDY:      (ty - sy) / float64(substeps),
				stepDAngle:  (ta - sa) / float64(substeps),
				curX:        sx,
				curY:        sy,
				curAngle:    sa,
				invMass:     float64(b.InvMass),
				invInertia:  float64(b.InvInertia),
			})
		}
		attachToTracked[ai] = ti
		if att.Compliance <= 0 && rigidAttachForParticle[att.Particle] < 0 {
			rigidAttachForParticle[att.Particle] = ai
		}
	}

	// Build fast per-particle attachment exclusion lookup (skip collision with attached rigid body).
	attachedBodyOfParticle := make([]*RigidBody, len(sw.Particles))
	for ai := range sw.Attachments {
		att := &sw.Attachments[ai]
		if att.Body != nil && sw.validParticle(att.Particle) && attachedBodyOfParticle[att.Particle] == nil {
			attachedBodyOfParticle[att.Particle] = att.Body
		}
	}

	gx, gy := float64(sw.GravityX), float64(sw.GravityY)
	dampFactor := 1.0
	if sw.Damping > 0 {
		dampFactor = 1.0 / (1.0 + float64(float64(sw.Damping)*h))
	}

	px := make([]float64, len(sw.Particles))
	py := make([]float64, len(sw.Particles))
	prevX := make([]float64, len(sw.Particles))
	prevY := make([]float64, len(sw.Particles))
	vx := make([]float64, len(sw.Particles))
	vy := make([]float64, len(sw.Particles))
	for i := range sw.Particles {
		px[i] = float64(sw.Particles[i].X)
		py[i] = float64(sw.Particles[i].Y)
		vx[i] = float64(sw.Particles[i].VX)
		vy[i] = float64(sw.Particles[i].VY)
	}

	hits := make([]collisionHit2D, len(sw.Particles))
	preHitVX := make([]float64, len(sw.Particles))
	preHitVY := make([]float64, len(sw.Particles))

	syncRigidAttached := func() {
		for ai := range sw.Attachments {
			att := &sw.Attachments[ai]
			ti := attachToTracked[ai]
			if ti < 0 || att.Compliance > 0 || sw.Particles[att.Particle].IsPinned() {
				continue
			}
			ax, ay, _, _ := tracked[ti].worldAnchor(att.LocalAnchor)
			px[att.Particle] = ax
			py[att.Particle] = ay
		}
	}

	for range substeps {
		// Advance attached rigid bodies smoothly through their per-substep motion.
		for ti := range tracked {
			tracked[ti].curX += tracked[ti].stepDX
			tracked[ti].curY += tracked[ti].stepDY
			tracked[ti].curAngle += tracked[ti].stepDAngle
		}

		// Predict particle positions.
		for i := range sw.Particles {
			p := &sw.Particles[i]
			prevX[i] = px[i]
			prevY[i] = py[i]
			hits[i] = collisionHit2D{}

			if p.IsPinned() {
				vx[i] = 0
				vy[i] = 0
				continue
			}
			if ai := rigidAttachForParticle[i]; ai >= 0 {
				ti := attachToTracked[ai]
				ax, ay, _, _ := tracked[ti].worldAnchor(sw.Attachments[ai].LocalAnchor)
				px[i] = ax
				py[i] = ay
				continue
			}
			w := p.effectiveInvMass()
			vx[i] = (vx[i] + float64((gx+float64(p.forceX*w))*h)) * dampFactor
			vy[i] = (vy[i] + float64((gy+float64(p.forceY*w))*h)) * dampFactor
			preHitVX[i] = vx[i]
			preHitVY[i] = vy[i]
			px[i] += float64(vx[i] * h)
			py[i] += float64(vy[i] * h)
		}

		// Reset XPBD Lagrange multipliers for this substep.
		for i := range sw.Distances {
			sw.Distances[i].lambda = 0
		}
		for i := range sw.Bendings {
			sw.Bendings[i].lambda = 0
		}
		for i := range sw.Areas {
			sw.Areas[i].lambda = 0
		}
		for i := range sw.Attachments {
			sw.Attachments[i].lambda = 0
		}

		// Iterative XPBD constraint & collision solver.
		for iter := 0; iter < iters; iter++ {
			// Distance constraints (alternating sweep direction for fast chain convergence).
			nDist := len(sw.Distances)
			for k := 0; k < nDist; k++ {
				di := k
				if iter&1 == 1 {
					di = nDist - 1 - k
				}
				sw.solveDistance2D(&sw.Distances[di], px, py, invHSq, rigidAttachForParticle, attachToTracked, tracked)
			}

			// Bending constraints.
			for bi := range sw.Bendings {
				sw.solveBending2D(&sw.Bendings[bi], px, py, invHSq)
			}

			// Area / pressure constraints.
			for ai := range sw.Areas {
				sw.solveArea2D(&sw.Areas[ai], px, py, invHSq)
			}

			// Shape-matching constraints.
			for si := range sw.ShapeMatches {
				sw.solveShapeMatch2D(&sw.ShapeMatches[si], px, py, iters)
			}

			// Compliant rigid attachments.
			for ai := range sw.Attachments {
				att := &sw.Attachments[ai]
				ti := attachToTracked[ai]
				if ti < 0 || att.Compliance <= 0 {
					continue
				}
				sw.solveCompliantAttachment2D(att, &tracked[ti], px, py, invHSq)
			}

			syncRigidAttached()

			// Collisions against world shapes, rigid bodies, and tile grids.
			for i := range sw.Particles {
				if sw.Particles[i].IsPinned() || rigidAttachForParticle[i] >= 0 {
					continue
				}
				sw.resolveParticleCollisions2D(i, px, py, prevX, prevY, attachedBodyOfParticle[i], &hits[i])
			}
		}

		// Derive XPBD velocities from constrained displacements.
		restThresh := max(1.0, 2.0*detmath.Hypot(gx, gy)*h)
		for i := range sw.Particles {
			p := &sw.Particles[i]
			if p.IsPinned() {
				vx[i] = 0
				vy[i] = 0
				continue
			}
			vx[i] = (px[i] - prevX[i]) * invH
			vy[i] = (py[i] - prevY[i]) * invH

			if hits[i].hit && hits[i].rest > 0 {
				preVN := float64(preHitVX[i]*hits[i].nx) + float64(preHitVY[i]*hits[i].ny)
				if -preVN > restThresh {
					curVN := float64(vx[i]*hits[i].nx) + float64(vy[i]*hits[i].ny)
					targetVN := float64(-hits[i].rest * preVN)
					if targetVN > curVN {
						dv := targetVN - curVN
						vx[i] += float64(hits[i].nx * dv)
						vy[i] += float64(hits[i].ny * dv)
					}
				}
			}
		}
	}

	// Write back particle state and clear accumulated forces.
	for i := range sw.Particles {
		p := &sw.Particles[i]
		p.PrevX = float32(prevX[i])
		p.PrevY = float32(prevY[i])
		p.X = float32(px[i])
		p.Y = float32(py[i])
		p.VX = float32(vx[i])
		p.VY = float32(vy[i])
		p.forceX = 0
		p.forceY = 0
	}

	// Write back two-way coupled rigid bodies and save pose history.
	invDt := 1.0 / dtSec
	for ti := range tracked {
		tb := &tracked[ti]
		b := tb.body
		if tb.staticOrNil {
			b.X = float32(tb.targetX)
			b.Y = float32(tb.targetY)
			b.Angle = float32(tb.targetAngle)
		} else {
			b.X = float32(tb.curX)
			b.Y = float32(tb.curY)
			b.Angle = float32(tb.curAngle)
			b.VX = float32(float64(b.VX) + float64(tb.corrX*invDt))
			b.VY = float32(float64(b.VY) + float64(tb.corrY*invDt))
			b.AngularVel = float32(float64(b.AngularVel) + float64(tb.corrAngle*invDt))
			b.syncShape()
		}
		sw.saveBodyHistory(b, float64(b.X), float64(b.Y), float64(b.Angle))
	}
}

func (sw *SoftWorld) solveDistance2D(
	c *DistanceConstraint,
	px, py []float64,
	invHSq float64,
	rigidAttachForParticle []int,
	attachToTracked []int,
	tracked []trackedBody2D,
) {
	if !sw.validParticle(c.A) || !sw.validParticle(c.B) || c.A == c.B {
		return
	}
	pa := &sw.Particles[c.A]
	pb := &sw.Particles[c.B]

	attA := rigidAttachForParticle[c.A]
	attB := rigidAttachForParticle[c.B]
	if pa.IsPinned() {
		attA = -1
	}
	if pb.IsPinned() {
		attB = -1
	}

	var ax, ay, rAx, rAy float64
	if attA >= 0 {
		ax, ay, rAx, rAy = tracked[attachToTracked[attA]].worldAnchor(sw.Attachments[attA].LocalAnchor)
		px[c.A], py[c.A] = ax, ay
	} else {
		ax, ay = px[c.A], py[c.A]
	}

	var bx, by, rBx, rBy float64
	if attB >= 0 {
		bx, by, rBx, rBy = tracked[attachToTracked[attB]].worldAnchor(sw.Attachments[attB].LocalAnchor)
		px[c.B], py[c.B] = bx, by
	} else {
		bx, by = px[c.B], py[c.B]
	}

	dx := bx - ax
	dy := by - ay
	dist := detmath.Hypot(dx, dy)
	if dist <= 1e-9 {
		return
	}
	err := dist - float64(c.RestLength)
	if c.MaxOnly && err <= 0 {
		return
	}

	nx := dx / dist
	ny := dy / dist

	var wA, wB float64
	if attA >= 0 {
		wA = tracked[attachToTracked[attA]].generalizedInvMass(rAx, rAy, nx, ny)
	} else {
		wA = pa.effectiveInvMass()
	}
	if attB >= 0 {
		wB = tracked[attachToTracked[attB]].generalizedInvMass(rBx, rBy, nx, ny)
	} else {
		wB = pb.effectiveInvMass()
	}

	alphaTilde := float64(float64(c.Compliance) * invHSq)
	denom := wA + wB + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - float64(alphaTilde*c.lambda)) / denom
	if c.MaxOnly {
		newLambda := c.lambda + dLambda
		if newLambda > 0 {
			dLambda = -c.lambda
			newLambda = 0
		}
		c.lambda = newLambda
	} else {
		c.lambda += dLambda
	}

	if attA >= 0 {
		tb := &tracked[attachToTracked[attA]]
		tb.applyPosImpulse(-dLambda*nx, -dLambda*ny, rAx, rAy)
		px[c.A], py[c.A], _, _ = tb.worldAnchor(sw.Attachments[attA].LocalAnchor)
	} else if wA > 0 {
		px[c.A] -= float64(dLambda * wA * nx)
		py[c.A] -= float64(dLambda * wA * ny)
	}

	if attB >= 0 {
		tb := &tracked[attachToTracked[attB]]
		tb.applyPosImpulse(dLambda*nx, dLambda*ny, rBx, rBy)
		px[c.B], py[c.B], _, _ = tb.worldAnchor(sw.Attachments[attB].LocalAnchor)
	} else if wB > 0 {
		px[c.B] += float64(dLambda * wB * nx)
		py[c.B] += float64(dLambda * wB * ny)
	}
}

func signedAngle2D(e1x, e1y, e2x, e2y float64) float64 {
	return detmath.Atan2(float64(e1x*e2y)-float64(e1y*e2x), float64(e1x*e2x)+float64(e1y*e2y))
}

func wrapAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func (sw *SoftWorld) solveBending2D(c *BendingConstraint, px, py []float64, invHSq float64) {
	if !sw.validParticle(c.A) || !sw.validParticle(c.B) || !sw.validParticle(c.C) {
		return
	}
	wA := sw.Particles[c.A].effectiveInvMass()
	wB := sw.Particles[c.B].effectiveInvMass()
	wC := sw.Particles[c.C].effectiveInvMass()
	if wA+wB+wC <= 0 {
		return
	}

	e1x := px[c.A] - px[c.B]
	e1y := py[c.A] - py[c.B]
	e2x := px[c.C] - px[c.B]
	e2y := py[c.C] - py[c.B]
	l1Sq := float64(e1x*e1x) + float64(e1y*e1y)
	l2Sq := float64(e2x*e2x) + float64(e2y*e2y)
	if l1Sq <= 1e-12 || l2Sq <= 1e-12 {
		return
	}

	angle := signedAngle2D(e1x, e1y, e2x, e2y)
	err := wrapAngle(angle - float64(c.RestAngle))

	// Exact gradients of signed angle phi = atan2(e1 x e2, e1 . e2).
	gAx := e1y / l1Sq
	gAy := -e1x / l1Sq
	gCx := -e2y / l2Sq
	gCy := e2x / l2Sq
	gBx := -(gAx + gCx)
	gBy := -(gAy + gCy)

	alphaTilde := float64(float64(c.Compliance) * invHSq)
	denom := float64(wA*(float64(gAx*gAx)+float64(gAy*gAy))) + float64(wB*(float64(gBx*gBx)+float64(gBy*gBy))) + float64(wC*(float64(gCx*gCx)+float64(gCy*gCy))) + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - float64(alphaTilde*c.lambda)) / denom
	c.lambda += dLambda

	if wA > 0 {
		px[c.A] += float64(dLambda * wA * gAx)
		py[c.A] += float64(dLambda * wA * gAy)
	}
	if wB > 0 {
		px[c.B] += float64(dLambda * wB * gBx)
		py[c.B] += float64(dLambda * wB * gBy)
	}
	if wC > 0 {
		px[c.C] += float64(dLambda * wC * gCx)
		py[c.C] += float64(dLambda * wC * gCy)
	}
}

func (sw *SoftWorld) solveArea2D(c *AreaConstraint, px, py []float64, invHSq float64) {
	n := len(c.Particles)
	if n < 3 {
		return
	}
	for _, idx := range c.Particles {
		if !sw.validParticle(idx) {
			return
		}
	}

	var area float64
	for i := 0; i < n; i++ {
		ia := c.Particles[i]
		ib := c.Particles[(i+1)%n]
		area += float64(px[ia]*py[ib]) - float64(px[ib]*py[ia])
	}
	area = float64(area * 0.5)

	pressure := float64(c.Pressure)
	if pressure <= 0 {
		pressure = 1
	}
	target := float64(float64(c.RestArea) * pressure)
	err := area - target

	var denom float64
	for i := 0; i < n; i++ {
		iPrev := c.Particles[(i+n-1)%n]
		iCur := c.Particles[i]
		iNext := c.Particles[(i+1)%n]
		gx := 0.5 * (py[iNext] - py[iPrev])
		gy := 0.5 * (px[iPrev] - px[iNext])
		w := sw.Particles[iCur].effectiveInvMass()
		denom += float64(w * (float64(gx*gx) + float64(gy*gy)))
	}
	alphaTilde := float64(float64(c.Compliance) * invHSq)
	denom += alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - float64(alphaTilde*c.lambda)) / denom
	c.lambda += dLambda

	// Compute gradients against pre-update coordinates for exact symmetry.
	gradX := make([]float64, n)
	gradY := make([]float64, n)
	for i := 0; i < n; i++ {
		iPrev := c.Particles[(i+n-1)%n]
		iNext := c.Particles[(i+1)%n]
		gradX[i] = 0.5 * (py[iNext] - py[iPrev])
		gradY[i] = 0.5 * (px[iPrev] - px[iNext])
	}
	for i := 0; i < n; i++ {
		idx := c.Particles[i]
		w := sw.Particles[idx].effectiveInvMass()
		if w > 0 {
			px[idx] += float64(dLambda * w * gradX[i])
			py[idx] += float64(dLambda * w * gradY[i])
		}
	}
}

func (sw *SoftWorld) solveShapeMatch2D(c *ShapeMatchConstraint, px, py []float64, iters int) {
	n := len(c.Particles)
	if n < 2 || len(c.RestOffsets) < n {
		return
	}
	var sumM, cx, cy float64
	for i := 0; i < n; i++ {
		idx := c.Particles[i]
		if !sw.validParticle(idx) {
			return
		}
		m := 1.0
		if sw.Particles[idx].InvMass > 0 {
			m = 1.0 / float64(sw.Particles[idx].InvMass)
		}
		sumM += m
		cx += float64(px[idx] * m)
		cy += float64(py[idx] * m)
	}
	if sumM <= 0 {
		return
	}
	cx /= sumM
	cy /= sumM

	// Optimal 2D rotation aligning rest offsets q to current offsets r = p - c.
	var a, b float64
	for i := 0; i < n; i++ {
		idx := c.Particles[i]
		m := 1.0
		if sw.Particles[idx].InvMass > 0 {
			m = 1.0 / float64(sw.Particles[idx].InvMass)
		}
		rx := px[idx] - cx
		ry := py[idx] - cy
		qx := float64(c.RestOffsets[i][0])
		qy := float64(c.RestOffsets[i][1])
		a += float64(m * (float64(rx*qx) + float64(ry*qy)))
		b += float64(m * (float64(ry*qx) - float64(rx*qy)))
	}
	l := detmath.Hypot(a, b)
	cosT, sinT := 1.0, 0.0
	if l > 1e-12 {
		cosT = a / l
		sinT = b / l
	}

	stiff := float64(c.Stiffness)
	if stiff <= 0 {
		stiff = 0.5
	} else if stiff > 1 {
		stiff = 1
	}
	k := shapeMatchGain(stiff, iters)
	for i := 0; i < n; i++ {
		idx := c.Particles[i]
		if sw.Particles[idx].IsPinned() {
			continue
		}
		qx := float64(c.RestOffsets[i][0])
		qy := float64(c.RestOffsets[i][1])
		gx := cx + float64(qx*cosT) - float64(qy*sinT)
		gy := cy + float64(qx*sinT) + float64(qy*cosT)
		px[idx] += float64((gx - px[idx]) * k)
		py[idx] += float64((gy - py[idx]) * k)
	}
}

// shapeMatchGain returns 1 - (1-stiff)^(1/iters), the gain per solver iteration that compounds to stiff over iters iterations. It takes the root with Newton's method in plain IEEE-754 arithmetic rather than math.Pow, whose Exp has its own assembly on amd64 and on arm64 and whose Log is assembly on amd64 only, so the two architectures round it differently.
func shapeMatchGain(stiff float64, iters int) float64 {
	x := 1.0 - stiff
	n := max(1, iters)
	if x <= 0 {
		return 1
	}
	if n == 1 || x >= 1 {
		return stiff
	}
	// Newton's method on y^n = x from y = 1 decreases monotonically toward the root because y^n is convex, so it stops at the first step that fails to decrease. Stiffness is a float32, so x >= 2^-24 and the loop takes about |ln x| + 6 steps and the cap is only a backstop.
	y := 1.0
	fn := float64(n)
	fm := float64(n - 1)
	for range 128 {
		next := (float64(fm*y) + x/powInt(y, n-1)) / fn
		if !(next < y) {
			break
		}
		y = next
	}
	return 1 - y
}

// powInt returns y^m for m >= 0 by repeated squaring.
func powInt(y float64, m int) float64 {
	r := 1.0
	for m > 0 {
		if m&1 == 1 {
			r *= y
		}
		y *= y
		m >>= 1
	}
	return r
}

func (sw *SoftWorld) solveCompliantAttachment2D(att *RigidAttachment, tb *trackedBody2D, px, py []float64, invHSq float64) {
	if !sw.validParticle(att.Particle) {
		return
	}
	p := &sw.Particles[att.Particle]
	ax, ay, rx, ry := tb.worldAnchor(att.LocalAnchor)
	dx := px[att.Particle] - ax
	dy := py[att.Particle] - ay
	dist := detmath.Hypot(dx, dy)
	if dist <= 1e-9 {
		return
	}
	nx := dx / dist
	ny := dy / dist

	wP := p.effectiveInvMass()
	wB := tb.generalizedInvMass(rx, ry, nx, ny)
	alphaTilde := float64(float64(att.Compliance) * invHSq)
	denom := wP + wB + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-dist - float64(alphaTilde*att.lambda)) / denom
	att.lambda += dLambda

	if wP > 0 {
		px[att.Particle] += float64(dLambda * wP * nx)
		py[att.Particle] += float64(dLambda * wP * ny)
	}
	if !tb.staticOrNil {
		tb.applyPosImpulse(-dLambda*nx, -dLambda*ny, rx, ry)
	}
}

func (sw *SoftWorld) resolveParticleCollisions2D(
	i int,
	px, py, prevX, prevY []float64,
	skipRigid *RigidBody,
	hitOut *collisionHit2D,
) {
	p := &sw.Particles[i]
	r := float64(p.Radius)
	if r < 1e-4 {
		r = 1e-4
	}

	applySurface := func(nx, ny, depth, surfaceFriction, surfaceRest float64) {
		if depth <= 0 {
			return
		}
		px[i] += float64(nx * depth)
		py[i] += float64(ny * depth)

		mu := math.Sqrt(max(0, float64(p.Friction)) * max(0, surfaceFriction))
		if mu > 0 {
			dx := px[i] - prevX[i]
			dy := py[i] - prevY[i]
			vn := float64(dx*nx) + float64(dy*ny)
			tx := dx - float64(vn*nx)
			ty := dy - float64(vn*ny)
			tLen := detmath.Hypot(tx, ty)
			if tLen > 1e-12 {
				maxSlide := mu * depth
				if tLen <= maxSlide {
					px[i] -= tx
					py[i] -= ty
				} else {
					s := maxSlide / tLen
					px[i] -= float64(tx * s)
					py[i] -= float64(ty * s)
				}
			}
		}

		rest := max(float64(p.Restitution), surfaceRest)
		if !hitOut.hit || rest > hitOut.rest {
			hitOut.hit = true
			hitOut.nx = nx
			hitOut.ny = ny
			hitOut.rest = rest
		}
	}

	// 1. Kinematic/static bodies in World.
	if sw.World != nil {
		for _, b := range sw.World.Bodies {
			if b == nil || b.Shape.Empty() {
				continue
			}
			if !layersInteract(p.Layer, p.Mask, b.Layer, b.Mask) {
				continue
			}
			c := TestShapes(CircleShape(Circle{X: float32(px[i]), Y: float32(py[i]), R: float32(r)}), b.Shape)
			if c.Hit {
				applySurface(float64(c.NormalX), float64(c.NormalY), float64(c.Depth), 0.5, 0)
			}
		}
	}

	// 2. Rigid bodies in RigidWorld (supports rotated boxes and circles).
	if sw.RigidWorld != nil {
		for _, rb := range sw.RigidWorld.Bodies {
			if rb == nil || rb == skipRigid || rb.Shape.Empty() {
				continue
			}
			if !layersInteract(p.Layer, p.Mask, rb.Layer, rb.Mask) {
				continue
			}
			nx, ny, depth, hit := collideParticleRigidBody2D(px[i], py[i], r, rb)
			if hit {
				applySurface(nx, ny, depth, float64(rb.Friction), float64(rb.Restitution))
			}
		}
	}

	// 3. Tile grids (World.Tiles and standalone Tiles).
	if sw.World != nil && sw.World.Tiles != nil {
		sw.resolveParticleTileGrid2D(i, px, py, prevX, prevY, r, sw.World.Tiles, applySurface)
	}
	if sw.Tiles != nil && (sw.World == nil || sw.Tiles != sw.World.Tiles) {
		sw.resolveParticleTileGrid2D(i, px, py, prevX, prevY, r, sw.Tiles, applySurface)
	}
}

func (sw *SoftWorld) resolveParticleTileGrid2D(
	i int,
	px, py, prevX, prevY []float64,
	r float64,
	grid TileGrid,
	applySurface func(nx, ny, depth, surfaceFriction, surfaceRest float64),
) {
	p := &sw.Particles[i]
	if !layersInteract(p.Layer, p.Mask, sw.TileLayer, sw.TileMask) {
		return
	}
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		return
	}
	tw, th := float64(tileW), float64(tileH)
	ox, oy := tileGridOrigin(grid)
	ox32, oy32 := float32(ox), float32(oy)
	minCol := max(0, int(math.Floor((px[i]-ox-r)/tw)))
	maxCol := min(cols-1, int(math.Ceil((px[i]-ox+r)/tw))-1)
	minRow := max(0, int(math.Floor((py[i]-oy-r)/th)))
	maxRow := min(rows-1, int(math.Ceil((py[i]-oy+r)/th))-1)

	for row := minRow; row <= maxRow; row++ {
		tileTop := oy + float64(float64(row)*th)
		for col := minCol; col <= maxCol; col++ {
			flags := grid.TileFlagsAt(col, row)
			if flags == 0 {
				continue
			}
			if flags&TileSolid != 0 {
				tileBox := Box{X: ox32 + float32(float32(col)*tileW), Y: oy32 + float32(float32(row)*tileH), W: tileW, H: tileH}
				c := TestCircleBox(Circle{X: float32(px[i]), Y: float32(py[i]), R: float32(r)}, tileBox)
				if c.Hit {
					applySurface(float64(c.NormalX), float64(c.NormalY), float64(c.Depth), 0.5, 0)
				}
			} else if flags&TileOneWay != 0 {
				// One-way platform blocks downward crossings of the top edge only.
				if prevY[i]+r <= tileTop+1e-3 && py[i]+r > tileTop {
					tileLeft := ox + float64(float64(col)*tw)
					tileRight := tileLeft + tw
					if px[i]+r > tileLeft && px[i]-r < tileRight {
						applySurface(0, -1, py[i]+r-tileTop, 0.5, 0)
					}
				}
			}
		}
	}
}

// collideParticleRigidBody2D tests particle circle (x, y, r) against rb (oriented box, circle, or a Polygon, Capsule, Segment or Chain at its pose).
// Returns normal pointing from rb toward particle and positive penetration depth.
func collideParticleRigidBody2D(x, y, r float64, rb *RigidBody) (nx, ny, depth float64, hit bool) {
	switch rb.Shape.Kind {
	case ShapeCircle:
		c := TestCircleCircle(
			Circle{X: float32(x), Y: float32(y), R: float32(r)},
			Circle{X: rb.X, Y: rb.Y, R: rb.Shape.Circle.R},
		)
		if !c.Hit {
			return 0, 0, 0, false
		}
		return float64(c.NormalX), float64(c.NormalY), float64(c.Depth), true
	case ShapeBox:
		hw := float64(float64(rb.Shape.Box.W) * 0.5)
		hh := float64(float64(rb.Shape.Box.H) * 0.5)
		if hw <= 0 || hh <= 0 {
			return 0, 0, 0, false
		}
		sinB, cosB := detmath.Sincos(float64(rb.Angle))
		relX := x - float64(rb.X)
		relY := y - float64(rb.Y)
		lx := float64(relX*cosB) + float64(relY*sinB)
		ly := float64(-relX*sinB) + float64(relY*cosB)

		cx := min(max(lx, -hw), hw)
		cy := min(max(ly, -hh), hh)
		dx := lx - cx
		dy := ly - cy
		distSq := float64(dx*dx) + float64(dy*dy)

		var lnx, lny float64
		if distSq > 0 {
			if distSq >= r*r {
				return 0, 0, 0, false
			}
			dist := math.Sqrt(distSq)
			lnx = dx / dist
			lny = dy / dist
			depth = r - dist
		} else {
			dTop := ly + hh
			dBottom := hh - ly
			dLeft := lx + hw
			dRight := hw - lx
			best := dTop
			lnx, lny = 0, -1
			if dBottom < best {
				best = dBottom
				lnx, lny = 0, 1
			}
			if dLeft < best {
				best = dLeft
				lnx, lny = -1, 0
			}
			if dRight < best {
				best = dRight
				lnx, lny = 1, 0
			}
			depth = best + r
		}
		wnx := float64(lnx*cosB) - float64(lny*sinB)
		wny := float64(lnx*sinB) + float64(lny*cosB)
		return wnx, wny, depth, true
	case ShapePolygon, ShapeCapsule, ShapeSegment, ShapeChain:
		p := pointHull(x, y, r)
		var m hullManifold
		if rb.Shape.Kind == ShapeChain {
			if !rb.hasLocal {
				return 0, 0, 0, false
			}
			s, _ := rb.poseShape()
			var buf [4]hullManifold
			for _, cm := range chainManifolds(s.Chain, &p, buf[:0], 0) {
				if cm.maxDepth() > m.maxDepth() {
					m = cm
				}
			}
		} else {
			h, ok := rb.rigidHull()
			if !ok {
				return 0, 0, 0, false
			}
			m = collideHulls(&h, &p)
		}
		if d := m.maxDepth(); d > 0 {
			return m.nx, m.ny, d, true
		}
		return 0, 0, 0, false
	}
	return 0, 0, 0, false
}
