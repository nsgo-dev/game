package physics3d

import (
	"cmp"
	"slices"
	"time"
)

const (
	defaultSolverIterations = 8
	restitutionVelThreshold = 1.0
	positionSlop            = 0.0002
	positionPercent         = 0.8
	sleepVelThreshold       = 0.05
	sleepTimeThreshold      = 0.4
	wakeVelThreshold        = 0.2
)

// Body is a translational rigid body or static collider in a World.
type Body struct {
	// ID uniquely identifies the body inside its World. AddBody assigns one
	// sequentially when ID is 0.
	ID int
	// Tag is an optional user label for identifying bodies in query results.
	Tag string
	// Shape is the body's collision primitive in local coordinates relative to Pos.
	Shape Shape
	// Pos is the body's world-space position.
	Pos Vec3
	// Vel is the body's linear velocity in units per second.
	Vel Vec3
	// Mass is the body's mass in kilograms. Zero mass marks a static body.
	Mass float32
	// InvMass is 1/Mass for dynamic bodies and 0 for static bodies.
	InvMass float32
	// Restitution is the bounciness in [0, 1].
	Restitution float32
	// Friction is the Coulomb friction coefficient (>= 0).
	Friction float32
	// LinearDamping reduces velocity smoothly over time (1 / (1 + LinearDamping*dt)).
	LinearDamping float32
	// Static marks an immovable body with infinite mass (InvMass == 0).
	Static bool
	// Sleeping marks a dynamic body currently at rest; the solver skips
	// integrating it until woken by Wake, ApplyImpulse, or an active collision.
	Sleeping bool

	sleepTimer float32
}

// NewStaticBody returns a static Body placed at pos with default friction.
func NewStaticBody(shape Shape, pos Vec3) Body {
	return Body{
		Shape:    shape,
		Pos:      pos,
		Static:   true,
		Friction: 0.5,
	}
}

// NewDynamicBody returns a dynamic Body placed at pos with the given mass and
// default friction (0.5) and linear damping (0.01). If mass <= 0, the returned
// body is static.
func NewDynamicBody(shape Shape, pos Vec3, mass float32) Body {
	if mass <= 0 {
		return NewStaticBody(shape, pos)
	}
	return Body{
		Shape:         shape,
		Pos:           pos,
		Mass:          mass,
		InvMass:       1 / mass,
		Friction:      0.5,
		LinearDamping: 0.01,
	}
}

// WorldShape returns the body's collision shape translated to Pos.
func (b *Body) WorldShape() Shape {
	return b.Shape.Translated(b.Pos)
}

// Wake clears b's sleeping state so it participates in subsequent steps.
func (b *Body) Wake() {
	b.Sleeping = false
	b.sleepTimer = 0
}

// ApplyImpulse adds impulse / Mass to a dynamic body's velocity and wakes it.
func (b *Body) ApplyImpulse(impulse Vec3) {
	if b.Static || b.InvMass == 0 {
		return
	}
	b.Wake()
	b.Vel = b.Vel.Add(impulse.Scale(b.InvMass))
}

// OverlapHit reports a body overlapped by a World.QueryOverlap shape.
// Contact.Normal points from Body toward the query shape.
type OverlapHit struct {
	Body    *Body
	Contact Contact
}

type pairKey struct {
	a, b int
}

type cachedImpulse struct {
	normal         Vec3
	normalImpulse  float32
	tangentImpulse Vec3
}

type solverContact struct {
	a, b           *Body
	normal         Vec3
	depth          float32
	point          Vec3
	restitution    float32
	friction       float32
	bias           float32
	normalImpulse  float32
	tangentImpulse Vec3
}

// World holds static and dynamic bodies and steps them forward deterministically.
type World struct {
	// Gravity is the constant acceleration applied to dynamic bodies each step.
	// NewWorld initializes it to (0, -9.81, 0).
	Gravity Vec3
	// SolverIterations is the number of velocity and positional solver passes
	// per Step; values <= 0 default to 8.
	SolverIterations int

	bodies []*Body
	nextID int
	cache  map[pairKey]cachedImpulse
}

// NewWorld returns a World with standard Y-up gravity (0, -9.81, 0) and 8
// solver iterations.
func NewWorld() *World {
	return &World{
		Gravity:          Vec3{Y: -9.81},
		SolverIterations: defaultSolverIterations,
		cache:            make(map[pairKey]cachedImpulse),
	}
}

// AddBody copies b into w, normalizes its ID and mass fields, and returns a
// pointer to the stored body.
func (w *World) AddBody(b Body) *Body {
	if b.ID == 0 {
		w.nextID++
		b.ID = w.nextID
	} else if b.ID >= w.nextID {
		w.nextID = b.ID
	}

	if b.Static || (b.Mass <= 0 && b.InvMass <= 0) {
		b.Static = true
		b.Mass = 0
		b.InvMass = 0
		b.Vel = Vec3{}
		b.Sleeping = false
	} else if b.InvMass <= 0 {
		b.InvMass = 1 / b.Mass
	} else if b.Mass <= 0 {
		b.Mass = 1 / b.InvMass
	}

	if b.Friction < 0 {
		b.Friction = 0
	}
	if b.Restitution < 0 {
		b.Restitution = 0
	}
	if b.Restitution > 1 {
		b.Restitution = 1
	}

	stored := new(Body)
	*stored = b
	w.bodies = append(w.bodies, stored)
	return stored
}

// RemoveBody removes the body with the given ID, preserving deterministic order
// of the remaining bodies, and wakes dynamic bodies so supported stacks react.
func (w *World) RemoveBody(id int) bool {
	idx := -1
	for i, b := range w.bodies {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	w.bodies = append(w.bodies[:idx], w.bodies[idx+1:]...)
	for _, b := range w.bodies {
		if !b.Static {
			b.Wake()
		}
	}
	return true
}

// Bodies returns the bodies currently in w in deterministic insertion order.
func (w *World) Bodies() []*Body {
	return w.bodies
}

// Raycast casts a segment from from to to against all bodies in w and returns
// all hits sorted by ascending T in [0, 1] (tie-broken by Body.ID).
func (w *World) Raycast(from, to Vec3) []RaycastHit {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 {
		return nil
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}

	var hits []RaycastHit
	for _, b := range w.bodies {
		if h, ok := RaycastShape(ray, b.WorldShape()); ok {
			h.Body = b
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(a, b RaycastHit) int {
		if c := cmp.Compare(a.T, b.T); c != 0 {
			return c
		}
		return cmp.Compare(a.Body.ID, b.Body.ID)
	})
	return hits
}

// RaycastFirst casts a segment from from to to against all bodies in w and
// returns the closest hit, if any.
func (w *World) RaycastFirst(from, to Vec3) (RaycastHit, bool) {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 {
		return RaycastHit{}, false
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}

	var best RaycastHit
	found := false
	for _, b := range w.bodies {
		if h, ok := RaycastShape(ray, b.WorldShape()); ok {
			h.Body = b
			if !found || h.T < best.T || (h.T == best.T && h.Body.ID < best.Body.ID) {
				best = h
				found = true
			}
		}
	}
	return best, found
}

// QueryOverlap returns every body in w overlapping shape, in deterministic body
// order. Each hit's Contact.Normal points from the body toward shape.
func (w *World) QueryOverlap(shape Shape) []OverlapHit {
	queryBounds := shape.Bounds()
	var hits []OverlapHit
	for _, b := range w.bodies {
		ws := b.WorldShape()
		if !aabbOverlap(queryBounds, ws.Bounds()) {
			continue
		}
		if c := TestShapes(shape, ws); c.Hit {
			hits = append(hits, OverlapHit{Body: b, Contact: c})
		}
	}
	return hits
}

// Step advances the rigid-body simulation by dt using symplectic Euler
// integration, deterministic collision detection, warm-started iterative
// impulse resolution with Coulomb friction, and positional correction.
func (w *World) Step(dt time.Duration) {
	if dt <= 0 || len(w.bodies) == 0 {
		return
	}
	dtSec := float32(dt.Seconds())
	iters := w.SolverIterations
	if iters <= 0 {
		iters = defaultSolverIterations
	}
	if w.cache == nil {
		w.cache = make(map[pairKey]cachedImpulse)
	}

	n := len(w.bodies)
	prevPos := make([]Vec3, n)

	// 1. Symplectic Euler integration: update velocities, then tentative positions.
	for i, b := range w.bodies {
		prevPos[i] = b.Pos
		if b.Static || b.Sleeping {
			continue
		}
		b.Vel = b.Vel.Add(w.Gravity.Scale(dtSec))
		if b.LinearDamping > 0 {
			b.Vel = b.Vel.Scale(1 / (1 + b.LinearDamping*dtSec))
		}
		b.Pos = b.Pos.Add(b.Vel.Scale(dtSec))
	}

	// 2. Deterministic broadphase + narrowphase pair detection.
	shapes := make([]Shape, n)
	bounds := make([]Box, n)
	for i, b := range w.bodies {
		shapes[i] = b.WorldShape()
		bounds[i] = shapes[i].Bounds()
	}

	var contacts []solverContact
	const wakeVelSq = wakeVelThreshold * wakeVelThreshold
	for i := 0; i < n; i++ {
		a := w.bodies[i]
		for j := i + 1; j < n; j++ {
			b := w.bodies[j]
			if (a.Static || a.Sleeping) && (b.Static || b.Sleeping) {
				continue
			}
			if !aabbOverlap(bounds[i], bounds[j]) {
				continue
			}
			c := TestShapes(shapes[i], shapes[j])
			if !c.Hit {
				continue
			}

			// Wake a sleeping dynamic body if struck by a moving body.
			if a.Sleeping && !b.Static && b.Vel.LenSq() > wakeVelSq {
				a.Wake()
			}
			if b.Sleeping && !a.Static && a.Vel.LenSq() > wakeVelSq {
				b.Wake()
			}

			// Compute restitution bias once from pre-solver closing velocity.
			rv := a.Vel.Sub(b.Vel)
			vn := rv.Dot(c.Normal)
			restitution := max(a.Restitution, b.Restitution)
			var bias float32
			if vn < -restitutionVelThreshold {
				bias = -restitution * vn
			}
			friction := sqrt32(a.Friction * b.Friction)

			sc := solverContact{
				a:           a,
				b:           b,
				normal:      c.Normal,
				depth:       c.Depth,
				point:       c.Point,
				restitution: restitution,
				friction:    friction,
				bias:        bias,
			}

			// Warm-start from the previous step's accumulated impulse when the
			// contact normal remains aligned.
			key := pairKey{a: a.ID, b: b.ID}
			if cached, ok := w.cache[key]; ok && cached.normal.Dot(c.Normal) > 0.95 {
				sc.normalImpulse = cached.normalImpulse
				sc.tangentImpulse = cached.tangentImpulse.Sub(c.Normal.Scale(cached.tangentImpulse.Dot(c.Normal)))
			}
			contacts = append(contacts, sc)
		}
	}

	// Sort contacts bottom-up (ascending Y, then body IDs) for fast, deterministic
	// convergence on vertical stacks.
	slices.SortStableFunc(contacts, func(x, y solverContact) int {
		if c := cmp.Compare(x.point.Y, y.point.Y); c != 0 {
			return c
		}
		if c := cmp.Compare(x.a.ID, y.a.ID); c != 0 {
			return c
		}
		return cmp.Compare(x.b.ID, y.b.ID)
	})

	// 3. Apply warm-start impulses.
	for i := range contacts {
		c := &contacts[i]
		total := c.normal.Scale(c.normalImpulse).Add(c.tangentImpulse)
		if !c.a.Static && !c.a.Sleeping {
			c.a.Vel = c.a.Vel.Add(total.Scale(c.a.InvMass))
		}
		if !c.b.Static && !c.b.Sleeping {
			c.b.Vel = c.b.Vel.Sub(total.Scale(c.b.InvMass))
		}
	}

	// 4. Iterative sequential-impulse velocity solver (normal + 3D Coulomb friction).
	for iter := 0; iter < iters; iter++ {
		for i := range contacts {
			c := &contacts[i]
			invA := activeInvMass(c.a)
			invB := activeInvMass(c.b)
			invSum := invA + invB
			if invSum == 0 {
				continue
			}

			// Normal impulse with accumulated clamping >= 0.
			rv := c.a.Vel.Sub(c.b.Vel)
			vn := rv.Dot(c.normal)
			dj := -(vn - c.bias) / invSum
			newJ := c.normalImpulse + dj
			if newJ < 0 {
				newJ = 0
			}
			appliedJ := newJ - c.normalImpulse
			c.normalImpulse = newJ

			impN := c.normal.Scale(appliedJ)
			if invA > 0 {
				c.a.Vel = c.a.Vel.Add(impN.Scale(invA))
			}
			if invB > 0 {
				c.b.Vel = c.b.Vel.Sub(impN.Scale(invB))
			}

			// Isotropic 3D Coulomb friction impulse clamped to friction * normalImpulse.
			if c.friction > 0 && c.normalImpulse > 0 {
				rv = c.a.Vel.Sub(c.b.Vel)
				vt := rv.Sub(c.normal.Scale(rv.Dot(c.normal)))
				deltaT := vt.Scale(-1 / invSum)
				newT := c.tangentImpulse.Add(deltaT)
				newT = newT.Sub(c.normal.Scale(newT.Dot(c.normal)))
				maxF := c.friction * c.normalImpulse
				if tLen := newT.Len(); tLen > maxF && tLen > 0 {
					newT = newT.Scale(maxF / tLen)
				}
				appliedT := newT.Sub(c.tangentImpulse)
				c.tangentImpulse = newT

				if invA > 0 {
					c.a.Vel = c.a.Vel.Add(appliedT.Scale(invA))
				}
				if invB > 0 {
					c.b.Vel = c.b.Vel.Sub(appliedT.Scale(invB))
				}
			}
		}
	}

	// Refresh warm-start cache with active contacts only.
	clear(w.cache)
	for i := range contacts {
		c := &contacts[i]
		w.cache[pairKey{a: c.a.ID, b: c.b.ID}] = cachedImpulse{
			normal:         c.normal,
			normalImpulse:  c.normalImpulse,
			tangentImpulse: c.tangentImpulse,
		}
	}

	// 5. Non-linear Gauss-Seidel positional correction (zero velocity injection).
	for iter := 0; iter < iters; iter++ {
		for i := range contacts {
			c := &contacts[i]
			invA := activeInvMass(c.a)
			invB := activeInvMass(c.b)
			invSum := invA + invB
			if invSum == 0 {
				continue
			}
			cur := TestShapes(c.a.WorldShape(), c.b.WorldShape())
			if !cur.Hit || cur.Depth <= positionSlop {
				continue
			}
			corr := (cur.Depth - positionSlop) * positionPercent / invSum
			if invA > 0 {
				c.a.Pos = c.a.Pos.Add(cur.Normal.Scale(corr * invA))
			}
			if invB > 0 {
				c.b.Pos = c.b.Pos.Sub(cur.Normal.Scale(corr * invB))
			}
		}
	}

	// 6. Put settled bodies to sleep so resting stacks have zero drift.
	const sleepVelSq = sleepVelThreshold * sleepVelThreshold
	invDtSq := 1 / (dtSec * dtSec)
	for i, b := range w.bodies {
		if b.Static || b.Sleeping {
			continue
		}
		dispSq := b.Pos.Sub(prevPos[i]).LenSq() * invDtSq
		if b.Vel.LenSq() < sleepVelSq && dispSq < sleepVelSq {
			b.sleepTimer += dtSec
			if b.sleepTimer >= sleepTimeThreshold {
				b.Sleeping = true
				b.Vel = Vec3{}
			}
		} else {
			b.sleepTimer = 0
		}
	}
}

func activeInvMass(b *Body) float32 {
	if b.Static || b.Sleeping {
		return 0
	}
	return b.InvMass
}
