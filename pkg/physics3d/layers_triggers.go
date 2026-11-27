package physics3d

import (
	"cmp"
	"slices"
)

// LayerMask is a 32-bit collision layer bitmask.
type LayerMask uint32

// LayerAll matches every collision layer.
const LayerAll LayerMask = ^LayerMask(0)

// LayerBit returns a LayerMask with only bit i set (0 <= i < 32).
// Values >= 32 return 0.
func LayerBit(i uint) LayerMask {
	if i >= 32 {
		return 0
	}
	return LayerMask(1) << i
}

func effectiveLayer(m LayerMask) LayerMask {
	if m == 0 {
		return LayerAll
	}
	return m
}

func canCollideMasks(aLayer, aMask, bLayer, bMask LayerMask) bool {
	al := effectiveLayer(aLayer)
	am := effectiveLayer(aMask)
	bl := effectiveLayer(bLayer)
	bm := effectiveLayer(bMask)
	return (al&bm) != 0 && (bl&am) != 0
}

// CanCollide reports whether bodies a and b are permitted to collide or overlap
// based on their Layer and Mask bitmasks. Zero Layer or Mask fields default to
// LayerAll for backwards compatibility.
func CanCollide(a, b *Body) bool {
	if a == nil || b == nil {
		return false
	}
	return canCollideMasks(a.Layer, a.Mask, b.Layer, b.Mask)
}

// ContactEventKind identifies the lifecycle transition of a contact or trigger pair.
type ContactEventKind int

const (
	// ContactEnter fires once on the first step two bodies begin overlapping.
	ContactEnter ContactEventKind = iota
	// ContactStay fires on each subsequent step the two bodies remain overlapping.
	ContactStay
	// ContactExit fires once when two previously overlapping bodies separate or
	// either body is removed from the World.
	ContactExit
)

// ContactEvent describes a deterministic contact or trigger transition between
// BodyA and BodyB (canonicalized so BodyA.ID <= BodyB.ID).
//
// Normal points from BodyB toward BodyA (matching TestShapes(BodyA, BodyB)).
// ChildIndexA and ChildIndexB report the zero-based compound child indices on
// BodyA and BodyB, or -1 for non-compound bodies.
type ContactEvent struct {
	Kind        ContactEventKind
	BodyA       *Body
	BodyB       *Body
	ChildIndexA int
	ChildIndexB int
	Point       Vec3
	Normal      Vec3
	Depth       float32
	IsTrigger   bool
}

type activeContactRecord struct {
	a, b           *Body
	childA, childB int
	point          Vec3
	normal         Vec3
	depth          float32
	isTrigger      bool
}

type contactKey struct {
	a, b           int
	childA, childB int
}

type pendingContactEvent struct {
	key contactKey
	rec activeContactRecord
	ev  ContactEvent
}

func canonicalPairKey(idA, idB int) (pairKey, bool) {
	if idA <= idB {
		return pairKey{a: idA, b: idB}, false
	}
	return pairKey{a: idB, b: idA}, true
}

func canonicalContactKey(idA, childA, idB, childB int) (contactKey, bool) {
	if idA < idB || (idA == idB && childA <= childB) {
		return contactKey{a: idA, b: idB, childA: childA, childB: childB}, false
	}
	return contactKey{a: idB, b: idA, childA: childB, childB: childA}, true
}

func comparePairKeys(x, y pairKey) int {
	if c := cmp.Compare(x.a, y.a); c != 0 {
		return c
	}
	return cmp.Compare(x.b, y.b)
}

func compareContactKeys(x, y contactKey) int {
	if c := cmp.Compare(x.a, y.a); c != 0 {
		return c
	}
	if c := cmp.Compare(x.b, y.b); c != 0 {
		return c
	}
	if c := cmp.Compare(x.childA, y.childA); c != 0 {
		return c
	}
	return cmp.Compare(x.childB, y.childB)
}

func hitTieOrder(h RaycastHit) (int, int) {
	if h.Body != nil {
		return 0, h.Body.ID
	}
	if h.TriMesh != nil {
		return 1, h.TriMesh.ID
	}
	if h.Heightfield != nil {
		return 2, h.Heightfield.ID
	}
	if h.Voxel != nil {
		return 3, h.Voxel.ID
	}
	return 4, 0
}

func compareRaycastHits(a, b RaycastHit) int {
	if c := cmp.Compare(a.T, b.T); c != 0 {
		return c
	}
	ak, aid := hitTieOrder(a)
	bk, bid := hitTieOrder(b)
	if c := cmp.Compare(ak, bk); c != 0 {
		return c
	}
	if c := cmp.Compare(aid, bid); c != 0 {
		return c
	}
	return cmp.Compare(a.ChildIndex, b.ChildIndex)
}

// RaycastLayer casts a segment from from to to against all bodies, triangle
// meshes, heightfields, and voxel colliders whose Layer matches mask, returning
// hits sorted by ascending T in [0, 1] with deterministic tie-breaking.
// For compound bodies, every child matching mask is tested and reported with
// its zero-based ChildIndex.
func (w *World) RaycastLayer(from, to Vec3, mask LayerMask) []RaycastHit {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 || mask == 0 {
		return nil
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}
	slab, ok := newRaySlab(from, delta)
	if !ok {
		return nil
	}
	w.bp.ensureInit(w)

	var hits []RaycastHit
	limit := maxDist + max(float32(1e-4), float32(maxDist*1e-5))
	w.bp.raycastOrdered(&slab, limit, func(p *bpProxy, curMax float32) (float32, bool) {
		switch p.kind {
		case bpKindBody:
			b := p.body
			if b.Shape.Kind == ShapeCompound {
				ws := p.worldShape
				for ci, c := range ws.Children {
					if (c.EffectiveLayer(b.Layer) & mask) == 0 {
						continue
					}
					if h, hit := RaycastShape(ray, c.LocalShape()); hit {
						h.Body = b
						h.ChildIndex = ci
						hits = append(hits, h)
					}
				}
				return curMax, true
			}
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := RaycastShape(ray, p.worldShape); hit {
				h.Body = b
				h.ChildIndex = -1
				hits = append(hits, h)
			}
		case bpKindTriMesh:
			tm := p.triMesh
			if (effectiveLayer(tm.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := tm.Raycast(ray); hit {
				h.TriMesh = tm
				h.ChildIndex = -1
				hits = append(hits, h)
			}
		case bpKindHeightfield:
			hf := p.heightfield
			if (effectiveLayer(hf.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := hf.Raycast(ray); hit {
				h.Heightfield = hf
				h.ChildIndex = -1
				hits = append(hits, h)
			}
		}
		return curMax, true
	})
	for _, vc := range w.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if h, hit := vc.Raycast(ray); hit {
			h.Voxel = vc
			h.ChildIndex = -1
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, compareRaycastHits)
	return hits
}

// RaycastFirstLayer casts a segment from from to to against all bodies, triangle
// meshes, heightfields, and voxel colliders whose Layer matches mask, returning
// the closest hit. For compound bodies, ChildIndex reports the struck child.
func (w *World) RaycastFirstLayer(from, to Vec3, mask LayerMask) (RaycastHit, bool) {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 || mask == 0 {
		return RaycastHit{}, false
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}
	slab, ok := newRaySlab(from, delta)
	if !ok {
		return RaycastHit{}, false
	}
	w.bp.ensureInit(w)

	var best RaycastHit
	found := false
	consider := func(h RaycastHit) {
		if !found || compareRaycastHits(h, best) < 0 {
			best = h
			found = true
		}
	}

	limit := maxDist + max(float32(1e-4), float32(maxDist*1e-5))
	w.bp.raycastOrdered(&slab, limit, func(p *bpProxy, curMax float32) (float32, bool) {
		switch p.kind {
		case bpKindBody:
			b := p.body
			if b.Shape.Kind == ShapeCompound {
				ws := p.worldShape
				for ci, c := range ws.Children {
					if (c.EffectiveLayer(b.Layer) & mask) == 0 {
						continue
					}
					if h, hit := RaycastShape(ray, c.LocalShape()); hit {
						h.Body = b
						h.ChildIndex = ci
						consider(h)
						next := best.Distance + max(float32(1e-5), float32(best.Distance*1e-6))
						if next < curMax {
							curMax = next
						}
					}
				}
				return curMax, true
			}
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := RaycastShape(ray, p.worldShape); hit {
				h.Body = b
				h.ChildIndex = -1
				consider(h)
				next := best.Distance + max(float32(1e-5), float32(best.Distance*1e-6))
				if next < curMax {
					curMax = next
				}
			}
		case bpKindTriMesh:
			tm := p.triMesh
			if (effectiveLayer(tm.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := tm.Raycast(ray); hit {
				h.TriMesh = tm
				h.ChildIndex = -1
				consider(h)
				next := best.Distance + max(float32(1e-5), float32(best.Distance*1e-6))
				if next < curMax {
					curMax = next
				}
			}
		case bpKindHeightfield:
			hf := p.heightfield
			if (effectiveLayer(hf.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := hf.Raycast(ray); hit {
				h.Heightfield = hf
				h.ChildIndex = -1
				consider(h)
				next := best.Distance + max(float32(1e-5), float32(best.Distance*1e-6))
				if next < curMax {
					curMax = next
				}
			}
		}
		return curMax, true
	})
	for _, vc := range w.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if h, hit := vc.Raycast(ray); hit {
			h.Voxel = vc
			h.ChildIndex = -1
			consider(h)
		}
	}
	return best, found
}

// OverlapLayer returns every body, triangle mesh, heightfield, and voxel
// collider in w whose Layer matches mask and overlaps shape, in deterministic
// insertion order. Each hit's Contact.Normal points from the collider toward shape.
// For compound bodies, every overlapping child matching mask is reported in
// ascending child-index order with its zero-based ChildIndex.
func (w *World) OverlapLayer(shape Shape, mask LayerMask) []OverlapHit {
	if mask == 0 {
		return nil
	}
	w.bp.ensureInit(w)
	queryBounds := shape.Bounds()
	type orderedHit struct {
		kind  bpKind
		order int32
		child int
		hit   OverlapHit
	}
	var ordered []orderedHit
	w.bp.queryAABB(queryBounds, func(p *bpProxy) bool {
		switch p.kind {
		case bpKindBody:
			b := p.body
			if !aabbOverlap(queryBounds, p.worldBounds) {
				return true
			}
			if b.Shape.Kind == ShapeCompound {
				ws := p.worldShape
				for ci, c := range ws.Children {
					if (c.EffectiveLayer(b.Layer) & mask) == 0 {
						continue
					}
					cs := c.LocalShape()
					if !aabbOverlap(queryBounds, cs.Bounds()) {
						continue
					}
					if ct := TestShapes(shape, cs); ct.Hit {
						ordered = append(ordered, orderedHit{
							kind:  bpKindBody,
							order: p.order,
							child: ci,
							hit:   OverlapHit{Body: b, ChildIndex: ci, Contact: ct},
						})
					}
				}
				return true
			}
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return true
			}
			if c := TestShapes(shape, p.worldShape); c.Hit {
				ordered = append(ordered, orderedHit{
					kind:  bpKindBody,
					order: p.order,
					child: -1,
					hit:   OverlapHit{Body: b, ChildIndex: -1, Contact: c},
				})
			}
		case bpKindTriMesh:
			tm := p.triMesh
			if (effectiveLayer(tm.Layer) & mask) == 0 {
				return true
			}
			if !aabbOverlap(queryBounds, tm.Bounds) {
				return true
			}
			if c := tm.TestShape(shape); c.Hit {
				ordered = append(ordered, orderedHit{
					kind:  bpKindTriMesh,
					order: p.order,
					child: -1,
					hit:   OverlapHit{TriMesh: tm, ChildIndex: -1, Contact: c},
				})
			}
		case bpKindHeightfield:
			hf := p.heightfield
			if (effectiveLayer(hf.Layer) & mask) == 0 {
				return true
			}
			if !aabbOverlap(queryBounds, hf.Bounds) {
				return true
			}
			if c := hf.TestShape(shape); c.Hit {
				ordered = append(ordered, orderedHit{
					kind:  bpKindHeightfield,
					order: p.order,
					child: -1,
					hit:   OverlapHit{Heightfield: hf, ChildIndex: -1, Contact: c},
				})
			}
		}
		return true
	})
	if len(ordered) > 1 {
		slices.SortFunc(ordered, func(a, b orderedHit) int {
			if a.kind != b.kind {
				return cmp.Compare(a.kind, b.kind)
			}
			if a.order != b.order {
				return cmp.Compare(a.order, b.order)
			}
			return cmp.Compare(a.child, b.child)
		})
	}
	var hits []OverlapHit
	if len(ordered) > 0 {
		hits = make([]OverlapHit, len(ordered))
		for i, oh := range ordered {
			hits[i] = oh.hit
		}
	}
	for _, vc := range w.voxels {
		if (effectiveLayer(vc.Layer) & mask) == 0 {
			continue
		}
		if c := vc.TestShape(shape); c.Hit {
			hits = append(hits, OverlapHit{Voxel: vc, ChildIndex: -1, Contact: c})
		}
	}
	return hits
}

// ContactEvents returns the deterministic slice of ContactEvents produced by
// the most recent World.Step (and any subsequent RemoveBody exit events), in
// the exact order delivered to OnContact and OnTrigger.
func (w *World) ContactEvents() []ContactEvent {
	return w.contactEvents
}

// IgnoreCollision disables collision and trigger detection between bodies a and b.
func (w *World) IgnoreCollision(a, b *Body) {
	if a == nil || b == nil || a.ID == b.ID {
		return
	}
	if w.ignoredPairs == nil {
		w.ignoredPairs = make(map[pairKey]struct{})
	}
	key, _ := canonicalPairKey(a.ID, b.ID)
	w.ignoredPairs[key] = struct{}{}
}

// AllowCollision re-enables collision and trigger detection between bodies a and b
// if previously disabled with IgnoreCollision.
func (w *World) AllowCollision(a, b *Body) {
	if a == nil || b == nil || w.ignoredPairs == nil {
		return
	}
	key, _ := canonicalPairKey(a.ID, b.ID)
	delete(w.ignoredPairs, key)
}

func (w *World) isPairIgnored(idA, idB int) bool {
	if len(w.ignoredPairs) == 0 {
		return false
	}
	key, _ := canonicalPairKey(idA, idB)
	_, ok := w.ignoredPairs[key]
	return ok
}

func (w *World) emitContactEvent(ev ContactEvent) {
	if ev.IsTrigger {
		if w.OnTrigger != nil {
			w.OnTrigger(ev)
		}
	} else {
		if w.OnContact != nil {
			w.OnContact(ev)
		}
	}
}

func (w *World) bodyRemovedDuringDispatch(key contactKey) bool {
	if len(w.removedDuringDispatch) == 0 {
		return false
	}
	if _, ok := w.removedDuringDispatch[key.a]; ok {
		return true
	}
	if _, ok := w.removedDuringDispatch[key.b]; ok {
		return true
	}
	return false
}

func (w *World) reconcileContactEvents(current map[contactKey]activeContactRecord) {
	if w.activeContacts == nil {
		w.activeContacts = make(map[contactKey]activeContactRecord)
	}

	curKeys := w.curKeysBuf[:0]
	for k := range current {
		curKeys = append(curKeys, k)
	}
	slices.SortFunc(curKeys, compareContactKeys)
	w.curKeysBuf = curKeys

	exitKeys := w.exitKeysBuf[:0]
	for k := range w.activeContacts {
		if _, still := current[k]; !still {
			exitKeys = append(exitKeys, k)
		}
	}
	slices.SortFunc(exitKeys, compareContactKeys)
	w.exitKeysBuf = exitKeys

	pending := w.pendingEventsBuf[:0]
	for _, k := range curKeys {
		cur := current[k]
		if prev, existed := w.activeContacts[k]; existed {
			if prev.isTrigger != cur.isTrigger {
				pending = append(pending, pendingContactEvent{
					key: k,
					ev: ContactEvent{
						Kind:        ContactExit,
						BodyA:       prev.a,
						BodyB:       prev.b,
						ChildIndexA: prev.childA,
						ChildIndexB: prev.childB,
						Point:       prev.point,
						Normal:      prev.normal,
						Depth:       0,
						IsTrigger:   prev.isTrigger,
					},
				})
				pending = append(pending, pendingContactEvent{
					key: k,
					rec: cur,
					ev: ContactEvent{
						Kind:        ContactEnter,
						BodyA:       cur.a,
						BodyB:       cur.b,
						ChildIndexA: cur.childA,
						ChildIndexB: cur.childB,
						Point:       cur.point,
						Normal:      cur.normal,
						Depth:       cur.depth,
						IsTrigger:   cur.isTrigger,
					},
				})
			} else {
				pending = append(pending, pendingContactEvent{
					key: k,
					rec: cur,
					ev: ContactEvent{
						Kind:        ContactStay,
						BodyA:       cur.a,
						BodyB:       cur.b,
						ChildIndexA: cur.childA,
						ChildIndexB: cur.childB,
						Point:       cur.point,
						Normal:      cur.normal,
						Depth:       cur.depth,
						IsTrigger:   cur.isTrigger,
					},
				})
			}
		} else {
			pending = append(pending, pendingContactEvent{
				key: k,
				rec: cur,
				ev: ContactEvent{
					Kind:        ContactEnter,
					BodyA:       cur.a,
					BodyB:       cur.b,
					ChildIndexA: cur.childA,
					ChildIndexB: cur.childB,
					Point:       cur.point,
					Normal:      cur.normal,
					Depth:       cur.depth,
					IsTrigger:   cur.isTrigger,
				},
			})
		}
	}

	for _, k := range exitKeys {
		prev := w.activeContacts[k]
		pending = append(pending, pendingContactEvent{
			key: k,
			ev: ContactEvent{
				Kind:        ContactExit,
				BodyA:       prev.a,
				BodyB:       prev.b,
				ChildIndexA: prev.childA,
				ChildIndexB: prev.childB,
				Point:       prev.point,
				Normal:      prev.normal,
				Depth:       0,
				IsTrigger:   prev.isTrigger,
			},
		})
	}

	// current is w.nextActiveContacts; clear it now that pending holds this
	// step's contact records so stale Body pointers are not retained.
	clear(current)

	clear(w.contactEvents)
	w.contactEvents = w.contactEvents[:0]

	w.dispatchingContacts = true
	if len(w.removedDuringDispatch) > 0 {
		clear(w.removedDuringDispatch)
	}
	defer func() {
		w.dispatchingContacts = false
		if len(w.removedDuringDispatch) > 0 {
			clear(w.removedDuringDispatch)
		}
		clear(pending)
		w.pendingEventsBuf = pending[:0]
	}()

	for _, item := range pending {
		if w.bodyRemovedDuringDispatch(item.key) {
			continue
		}
		switch item.ev.Kind {
		case ContactEnter, ContactStay:
			w.activeContacts[item.key] = item.rec
		case ContactExit:
			delete(w.activeContacts, item.key)
		}
		w.contactEvents = append(w.contactEvents, item.ev)
		w.emitContactEvent(item.ev)
	}
}

func (w *World) emitRemovedBodyExits(removedID int) {
	if len(w.activeContacts) == 0 {
		return
	}
	var exitKeys []contactKey
	for k := range w.activeContacts {
		if k.a == removedID || k.b == removedID {
			exitKeys = append(exitKeys, k)
		}
	}
	if len(exitKeys) == 0 {
		return
	}
	slices.SortFunc(exitKeys, compareContactKeys)

	for _, k := range exitKeys {
		prev, ok := w.activeContacts[k]
		if !ok {
			// A nested RemoveBody inside an earlier Exit callback already
			// emitted and removed this pair.
			continue
		}
		delete(w.activeContacts, k)
		ev := ContactEvent{
			Kind:        ContactExit,
			BodyA:       prev.a,
			BodyB:       prev.b,
			ChildIndexA: prev.childA,
			ChildIndexB: prev.childB,
			Point:       prev.point,
			Normal:      prev.normal,
			Depth:       0,
			IsTrigger:   prev.isTrigger,
		}
		w.contactEvents = append(w.contactEvents, ev)
		w.emitContactEvent(ev)
	}
}
