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
type ContactEvent struct {
	Kind      ContactEventKind
	BodyA     *Body
	BodyB     *Body
	Point     Vec3
	Normal    Vec3
	Depth     float32
	IsTrigger bool
}

type activeContactRecord struct {
	a, b      *Body
	point     Vec3
	normal    Vec3
	depth     float32
	isTrigger bool
}

func canonicalPairKey(idA, idB int) (pairKey, bool) {
	if idA <= idB {
		return pairKey{a: idA, b: idB}, false
	}
	return pairKey{a: idB, b: idA}, true
}

func comparePairKeys(x, y pairKey) int {
	if c := cmp.Compare(x.a, y.a); c != 0 {
		return c
	}
	return cmp.Compare(x.b, y.b)
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
	return cmp.Compare(aid, bid)
}

// RaycastLayer casts a segment from from to to against all bodies, triangle
// meshes, heightfields, and voxel colliders whose Layer matches mask, returning
// hits sorted by ascending T in [0, 1] with deterministic tie-breaking.
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
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := RaycastShape(ray, p.worldShape); hit {
				h.Body = b
				hits = append(hits, h)
			}
		case bpKindTriMesh:
			tm := p.triMesh
			if (effectiveLayer(tm.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := tm.Raycast(ray); hit {
				h.TriMesh = tm
				hits = append(hits, h)
			}
		case bpKindHeightfield:
			hf := p.heightfield
			if (effectiveLayer(hf.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := hf.Raycast(ray); hit {
				h.Heightfield = hf
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
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, compareRaycastHits)
	return hits
}

// RaycastFirstLayer casts a segment from from to to against all bodies, triangle
// meshes, heightfields, and voxel colliders whose Layer matches mask, returning
// the closest hit.
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
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return curMax, true
			}
			if h, hit := RaycastShape(ray, p.worldShape); hit {
				h.Body = b
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
			consider(h)
		}
	}
	return best, found
}

// OverlapLayer returns every body, triangle mesh, heightfield, and voxel
// collider in w whose Layer matches mask and overlaps shape, in deterministic
// insertion order. Each hit's Contact.Normal points from the collider toward shape.
func (w *World) OverlapLayer(shape Shape, mask LayerMask) []OverlapHit {
	if mask == 0 {
		return nil
	}
	w.bp.ensureInit(w)
	queryBounds := shape.Bounds()
	type orderedHit struct {
		kind  bpKind
		order int32
		hit   OverlapHit
	}
	var ordered []orderedHit
	w.bp.queryAABB(queryBounds, func(p *bpProxy) bool {
		switch p.kind {
		case bpKindBody:
			b := p.body
			if (effectiveLayer(b.Layer) & mask) == 0 {
				return true
			}
			if !aabbOverlap(queryBounds, p.worldBounds) {
				return true
			}
			if c := TestShapes(shape, p.worldShape); c.Hit {
				ordered = append(ordered, orderedHit{
					kind:  bpKindBody,
					order: p.order,
					hit:   OverlapHit{Body: b, Contact: c},
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
					hit:   OverlapHit{TriMesh: tm, Contact: c},
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
					hit:   OverlapHit{Heightfield: hf, Contact: c},
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
			return cmp.Compare(a.order, b.order)
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
			hits = append(hits, OverlapHit{Voxel: vc, Contact: c})
		}
	}
	return hits
}

// ContactEvents returns the deterministic slice of ContactEvents produced by
// the most recent World.Step (and any subsequent RemoveBody exit events).
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

func (w *World) reconcileContactEvents(current map[pairKey]activeContactRecord) {
	if w.activeContacts == nil {
		w.activeContacts = make(map[pairKey]activeContactRecord)
	}

	curKeys := w.curKeysBuf[:0]
	for k := range current {
		curKeys = append(curKeys, k)
	}
	slices.SortFunc(curKeys, comparePairKeys)
	w.curKeysBuf = curKeys

	exitKeys := w.exitKeysBuf[:0]
	for k := range w.activeContacts {
		if _, still := current[k]; !still {
			exitKeys = append(exitKeys, k)
		}
	}
	slices.SortFunc(exitKeys, comparePairKeys)
	w.exitKeysBuf = exitKeys

	clear(w.contactEvents)
	events := w.contactEvents[:0]
	for _, k := range curKeys {
		cur := current[k]
		if prev, existed := w.activeContacts[k]; existed {
			if prev.isTrigger != cur.isTrigger {
				events = append(events, ContactEvent{
					Kind:      ContactExit,
					BodyA:     prev.a,
					BodyB:     prev.b,
					Point:     prev.point,
					Normal:    prev.normal,
					Depth:     0,
					IsTrigger: prev.isTrigger,
				})
				events = append(events, ContactEvent{
					Kind:      ContactEnter,
					BodyA:     cur.a,
					BodyB:     cur.b,
					Point:     cur.point,
					Normal:    cur.normal,
					Depth:     cur.depth,
					IsTrigger: cur.isTrigger,
				})
			} else {
				events = append(events, ContactEvent{
					Kind:      ContactStay,
					BodyA:     cur.a,
					BodyB:     cur.b,
					Point:     cur.point,
					Normal:    cur.normal,
					Depth:     cur.depth,
					IsTrigger: cur.isTrigger,
				})
			}
		} else {
			events = append(events, ContactEvent{
				Kind:      ContactEnter,
				BodyA:     cur.a,
				BodyB:     cur.b,
				Point:     cur.point,
				Normal:    cur.normal,
				Depth:     cur.depth,
				IsTrigger: cur.isTrigger,
			})
		}
	}

	for _, k := range exitKeys {
		prev := w.activeContacts[k]
		events = append(events, ContactEvent{
			Kind:      ContactExit,
			BodyA:     prev.a,
			BodyB:     prev.b,
			Point:     prev.point,
			Normal:    prev.normal,
			Depth:     0,
			IsTrigger: prev.isTrigger,
		})
	}

	old := w.activeContacts
	w.activeContacts = current
	if old != nil {
		clear(old)
		w.nextActiveContacts = old
	} else {
		w.nextActiveContacts = nil
	}
	w.contactEvents = events

	for _, ev := range events {
		w.emitContactEvent(ev)
	}
}

func (w *World) emitRemovedBodyExits(removedID int) {
	if len(w.activeContacts) == 0 {
		return
	}
	var exitKeys []pairKey
	for k := range w.activeContacts {
		if k.a == removedID || k.b == removedID {
			exitKeys = append(exitKeys, k)
		}
	}
	if len(exitKeys) == 0 {
		return
	}
	slices.SortFunc(exitKeys, comparePairKeys)

	exits := make([]ContactEvent, 0, len(exitKeys))
	for _, k := range exitKeys {
		prev := w.activeContacts[k]
		delete(w.activeContacts, k)
		exits = append(exits, ContactEvent{
			Kind:      ContactExit,
			BodyA:     prev.a,
			BodyB:     prev.b,
			Point:     prev.point,
			Normal:    prev.normal,
			Depth:     0,
			IsTrigger: prev.isTrigger,
		})
	}
	w.contactEvents = append(w.contactEvents, exits...)
	for _, ev := range exits {
		w.emitContactEvent(ev)
	}
}
