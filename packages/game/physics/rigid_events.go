package physics

import "github.com/nsgo-dev/nsgo/core"

// ContactEventKind says whether a ContactEvent or SensorEvent reports a touch that began or one that ended.
type ContactEventKind uint8

const (
	// ContactBegin reports two bodies, or a body and Tiles, that touch after a Step and did not before it.
	ContactBegin ContactEventKind = iota
	// ContactEnd reports two bodies, or a body and Tiles, that touched before a Step and are now more than 0.25 px apart, or one of which left RigidWorld.Bodies, became a sensor or can no longer collide with the other.
	ContactEnd
)

// String returns "begin" or "end".
func (k ContactEventKind) String() string {
	switch k {
	case ContactBegin:
		return "begin"
	case ContactEnd:
		return "end"
	}
	return "unknown"
}

// ContactEvent reports a contact between two bodies of a RigidWorld, or between a body and its Tiles, that began or ended in the last Step. It is the 2D counterpart of physics3d.ContactEvent, which also reports contacts that stay and whose normal points the other way, from BodyB toward BodyA.
//
// BodyA and BodyB are in RigidWorld.Bodies order. A contact with Tiles has Tile set, BodyB nil and the body in BodyA, and the bodies touching any number of tiles count as one contact, so a crate sliding across a tiled floor begins touching it once. Col, Row and TileFlags name the tile the event's point is on.
//
// A ContactBegin event carries the point of the contact that was approaching fastest, the unit normal there pointing from BodyA toward BodyB (or the tile), and ApproachSpeed, how fast in pixels per second the two closed along the normal before the step resolved the contact, which is the impact speed for playing a sound or dealing damage. A ContactEnd event carries the bodies and, for Tiles, the last tile touched, with no point, normal or speed.
type ContactEvent struct {
	Kind         ContactEventKind
	BodyA, BodyB *RigidBody
	Tile         bool
	Col, Row     int
	TileFlags    TileFlags
	Point        core.Point
	// NormalX and NormalY form the unit contact normal from BodyA toward BodyB, or toward the tile.
	NormalX, NormalY float32
	ApproachSpeed    float32
}

// SensorEvent reports that a body that is not a sensor began or ended overlapping a sensor in the last Step, or left RigidWorld.Bodies while overlapping it.
type SensorEvent struct {
	Kind    ContactEventKind
	Sensor  *RigidBody
	Visitor *RigidBody
}

// ContactEvents returns the contacts that began or ended in the most recent Step, in deterministic order: the ContactBegin events in the order the step found the contacts (pairs of bodies in RigidWorld.Bodies order, then each body with Tiles), then the ContactEnd events in the order the previous Step listed those contacts, then, in RigidWorld.Bodies order, a ContactBegin for each fast body continuous collision stopped at a surface it was not touching, which carries the speed it struck at. A contact the step did not find carries over while its bodies may still collide and are both asleep or not dynamic, or are still within 0.25 px of each other; it ends once they are farther apart, a body leaves RigidWorld.Bodies or becomes a sensor, or both become static or kinematic. The slice is reused by the next Step.
func (w *RigidWorld) ContactEvents() []ContactEvent {
	if w == nil {
		return nil
	}
	return w.contactEvents
}

// SensorEvents returns the sensor overlaps that began or ended in the most recent Step, tested at the bodies' poses at the end of the step: the ContactBegin events in pair order (by RigidWorld.Bodies index), then the ContactEnd events in the order the previous Step listed those overlaps. The slice is reused by the next Step.
func (w *RigidWorld) SensorEvents() []SensorEvent {
	if w == nil {
		return nil
	}
	return w.sensorEvents
}

// rigidTouch is a pair of bodies, or a body and the body that stands for Tiles in b, in contact, with the last tile touched.
type rigidTouch struct {
	a, b     *RigidBody
	col, row int
	flags    TileFlags
}

// rigidTouchKey names a contact between two bodies, or a body and Tiles, across steps.
type rigidTouchKey struct {
	a, b *RigidBody
}

// rigidSense is a sensor and a body overlapping it.
type rigidSense struct {
	sensor, visitor *RigidBody
}

// updateContactEvents compares this step's manifolds, before solving, with the contacts of the last step and records the contacts that began and ended. A contact this step's manifolds do not name lasts while touchLasts says it does. ignoredPairs are the pairs joints keep from colliding.
func (w *RigidWorld) updateContactEvents(manifolds []rigidManifold, ignoredPairs map[[2]*RigidBody]struct{}) {
	events := w.contactEvents[:0]
	index := w.scratch.touchIndex
	if index == nil {
		index = make(map[rigidTouchKey]int, len(w.touching))
		w.scratch.touchIndex = index
	}
	clear(index)
	for i, t := range w.touching {
		index[rigidTouchKey{a: t.a, b: t.b}] = i
	}
	matched := w.scratch.touchMatched[:0]
	for range w.touching {
		matched = append(matched, false)
	}
	next := w.scratch.touching[:0]
	for i := 0; i < len(manifolds); {
		a, b := manifolds[i].a, manifolds[i].b
		j := i + 1
		for j < len(manifolds) && manifolds[j].a == a && manifolds[j].b == b {
			j++
		}
		// The manifold approaching fastest, and its deepest point, name the contact; ties keep the first.
		best := i
		for k := i + 1; k < j; k++ {
			if manifolds[k].approach > manifolds[best].approach {
				best = k
			}
		}
		m := &manifolds[best]
		t := rigidTouch{a: a, b: b}
		if b == w.tileBody {
			t.col, t.row = w.tileGeom.cellAt(m.sub)
			t.flags = w.tileFlags(t.col, t.row)
		}
		if pi, ok := index[rigidTouchKey{a: a, b: b}]; ok {
			matched[pi] = true
		} else {
			p := 0
			for k := 1; k < m.count; k++ {
				if m.points[k].depth > m.points[p].depth {
					p = k
				}
			}
			events = append(events, ContactEvent{
				Kind:          ContactBegin,
				BodyA:         a,
				BodyB:         t.bodyB(w),
				Tile:          b == w.tileBody,
				Col:           t.col,
				Row:           t.row,
				TileFlags:     t.flags,
				Point:         core.Point{X: core.Points(m.points[p].px), Y: core.Points(m.points[p].py)},
				NormalX:       float32(m.nx),
				NormalY:       float32(m.ny),
				ApproachSpeed: float32(max(0, m.approach)),
			})
		}
		next = append(next, t)
		i = j
	}
	for pi, t := range w.touching {
		if matched[pi] {
			continue
		}
		if w.touchLasts(&t, ignoredPairs) {
			next = append(next, t)
			continue
		}
		events = append(events, ContactEvent{
			Kind:      ContactEnd,
			BodyA:     t.a,
			BodyB:     t.bodyB(w),
			Tile:      t.b == w.tileBody,
			Col:       t.col,
			Row:       t.row,
			TileFlags: t.flags,
		})
	}
	w.scratch.touchMatched = matched
	w.scratch.touching = w.touching
	w.touching = next
	w.contactEvents = events
}

// touchLasts reports whether contact t, which this step's manifolds do not name, still holds: both bodies are in the world and may still collide (neither is a sensor, they are not both static or kinematic, their layers interact, no joint keeps them apart, and a body touching Tiles is dynamic), and either neither is awake and dynamic, so the step did not test them and a sleeping contact carries over, or they are still within contactEndGap of each other, so a contact the solver lets drift a hair apart, or one continuous collision began at a gap, does not end and begin again.
func (w *RigidWorld) touchLasts(t *rigidTouch, ignoredPairs map[[2]*RigidBody]struct{}) bool {
	a, b := t.a, t.b
	if !w.inWorld(a) || !w.inWorld(b) || a.Sensor || b.Sensor || !rigidPairCandidate(a, b) || !CanCollideRigid(a, b) {
		return false
	}
	if b == w.tileBody && (a.fixed() || !w.tilesActive()) {
		return false
	}
	if ignoredPairs != nil {
		if _, skip := ignoredPairs[[2]*RigidBody{a, b}]; skip {
			return false
		}
	}
	if !a.awakeDynamic() && !b.awakeDynamic() {
		return true
	}
	return w.touchNear(a, b)
}

// contactEndGap is how far apart two bodies, or a body and Tiles, may drift without a manifold before their contact ends: a little more than the gap continuous collision leaves (ccdImpactGap), so a contact it began at that gap lasts until the regular contacts name it.
const contactEndGap = 0.25

// touchNear reports whether body a is within contactEndGap of body b, or of a solid tile or the top of a one-way tile when b stands for Tiles, without their cores overlapping.
func (w *RigidWorld) touchNear(a, b *RigidBody) bool {
	if a.Shape.Kind == ShapeChain {
		a, b = b, a
	}
	ha, ok := a.rigidHull()
	if !ok || !ha.finite() {
		return false
	}
	near := func(h *hull) bool {
		sep, _, _, _, ok := hullSeparation(&ha, h)
		return ok && sep <= contactEndGap
	}
	switch {
	case b == w.tileBody:
		g := &w.tileGeom
		minX, minY, maxX, maxY := ha.bounds()
		minCol, minRow, maxCol, maxRow, ok := g.cellRange(minX-contactEndGap, minY-contactEndGap, maxX+contactEndGap, maxY+contactEndGap)
		if !ok {
			return false
		}
		for row := minRow; row <= maxRow; row++ {
			for col := minCol; col <= maxCol; col++ {
				flags := w.Tiles.TileFlagsAt(col, row)
				if flags&TileSolid == 0 && flags&TileOneWay == 0 {
					continue
				}
				th := g.cellHull(col, row)
				sep, n, _, _, ok := hullSeparation(&ha, &th)
				if !ok || sep > contactEndGap {
					continue
				}
				// A one-way tile holds only a body above it, whose normal toward the tile points down.
				if flags&TileSolid != 0 || n.y > 1-tileNormalTolerance {
					return true
				}
			}
		}
		return false
	case b.Shape.Kind == ShapeChain:
		if !b.hasLocal {
			return false
		}
		s, _ := b.poseShape()
		c := s.Chain
		if c.Empty() {
			return false
		}
		for i := range c.SegmentCount() {
			p0, p1 := c.Points[i], c.Points[(i+1)%len(c.Points)]
			if seg := segmentHull(vec2{float64(p0.X), float64(p0.Y)}, vec2{float64(p1.X), float64(p1.Y)}, 0); seg.finite() && near(&seg) {
				return true
			}
		}
		return false
	}
	hb, ok := b.rigidHull()
	return ok && hb.finite() && near(&hb)
}

// bodyB returns t's second body, or nil when it is the body that stands for Tiles.
func (t *rigidTouch) bodyB(w *RigidWorld) *RigidBody {
	if t.b == w.tileBody {
		return nil
	}
	return t.b
}

// updateSensorEvents finds every sensor and body that is not a sensor overlapping at the bodies' current poses and records the overlaps that began and ended since the last Step.
func (w *RigidWorld) updateSensorEvents() {
	events := w.sensorEvents[:0]
	anySensor := false
	for _, b := range w.Bodies {
		if b != nil && b.Sensor {
			anySensor = true
			break
		}
	}
	if !anySensor && len(w.sensing) == 0 {
		w.sensorEvents = events
		return
	}
	index := w.scratch.senseIndex
	if index == nil {
		index = make(map[rigidSense]int, len(w.sensing))
		w.scratch.senseIndex = index
	}
	clear(index)
	for i, s := range w.sensing {
		index[s] = i
	}
	matched := w.scratch.senseMatched[:0]
	for range w.sensing {
		matched = append(matched, false)
	}
	next := w.scratch.sensing[:0]
	if anySensor {
		buf := w.scratch.pairManifolds[:0]
		for _, key := range w.broadphase() {
			a, b := w.Bodies[int(key>>32)], w.Bodies[int(uint32(key))]
			if a.Sensor == b.Sensor {
				continue
			}
			if buf = collideRigidPair(a, b, buf[:0]); len(buf) == 0 {
				continue
			}
			s := rigidSense{sensor: a, visitor: b}
			if b.Sensor {
				s = rigidSense{sensor: b, visitor: a}
			}
			if pi, ok := index[s]; ok {
				matched[pi] = true
			} else {
				events = append(events, SensorEvent{Kind: ContactBegin, Sensor: s.sensor, Visitor: s.visitor})
			}
			next = append(next, s)
		}
		w.scratch.pairManifolds = buf
	}
	for pi, s := range w.sensing {
		if !matched[pi] {
			events = append(events, SensorEvent{Kind: ContactEnd, Sensor: s.sensor, Visitor: s.visitor})
		}
	}
	w.scratch.senseMatched = matched
	w.scratch.sensing = w.sensing
	w.sensing = next
	w.sensorEvents = events
}
