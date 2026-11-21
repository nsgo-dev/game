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
	return 3, 0
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
// meshes, and heightfields whose Layer matches mask, returning hits sorted by
// ascending T in [0, 1] with deterministic tie-breaking.
func (w *World) RaycastLayer(from, to Vec3, mask LayerMask) []RaycastHit {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 || mask == 0 {
		return nil
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}

	var hits []RaycastHit
	for _, b := range w.bodies {
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		if h, ok := RaycastShape(ray, b.WorldShape()); ok {
			h.Body = b
			hits = append(hits, h)
		}
	}
	for _, tm := range w.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if h, ok := tm.Raycast(ray); ok {
			h.TriMesh = tm
			hits = append(hits, h)
		}
	}
	for _, hf := range w.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if h, ok := hf.Raycast(ray); ok {
			h.Heightfield = hf
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, compareRaycastHits)
	return hits
}

// RaycastFirstLayer casts a segment from from to to against all bodies, triangle
// meshes, and heightfields whose Layer matches mask, returning the closest hit.
func (w *World) RaycastFirstLayer(from, to Vec3, mask LayerMask) (RaycastHit, bool) {
	delta := to.Sub(from)
	maxDist := delta.Len()
	if maxDist <= 0 || mask == 0 {
		return RaycastHit{}, false
	}
	ray := Ray{Origin: from, Dir: delta, MaxDist: maxDist}

	var best RaycastHit
	found := false
	consider := func(h RaycastHit) {
		if !found || compareRaycastHits(h, best) < 0 {
			best = h
			found = true
		}
	}

	for _, b := range w.bodies {
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		if h, ok := RaycastShape(ray, b.WorldShape()); ok {
			h.Body = b
			consider(h)
		}
	}
	for _, tm := range w.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if h, ok := tm.Raycast(ray); ok {
			h.TriMesh = tm
			consider(h)
		}
	}
	for _, hf := range w.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if h, ok := hf.Raycast(ray); ok {
			h.Heightfield = hf
			consider(h)
		}
	}
	return best, found
}

// OverlapLayer returns every body, triangle mesh, and heightfield in w whose
// Layer matches mask and overlaps shape, in deterministic insertion order.
// Each hit's Contact.Normal points from the collider toward shape.
func (w *World) OverlapLayer(shape Shape, mask LayerMask) []OverlapHit {
	if mask == 0 {
		return nil
	}
	queryBounds := shape.Bounds()
	var hits []OverlapHit
	for _, b := range w.bodies {
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		ws := b.WorldShape()
		if !aabbOverlap(queryBounds, ws.Bounds()) {
			continue
		}
		if c := TestShapes(shape, ws); c.Hit {
			hits = append(hits, OverlapHit{Body: b, Contact: c})
		}
	}
	for _, tm := range w.triMeshes {
		if (effectiveLayer(tm.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(queryBounds, tm.Bounds) {
			continue
		}
		if c := tm.TestShape(shape); c.Hit {
			hits = append(hits, OverlapHit{TriMesh: tm, Contact: c})
		}
	}
	for _, hf := range w.heightfields {
		if (effectiveLayer(hf.Layer) & mask) == 0 {
			continue
		}
		if !aabbOverlap(queryBounds, hf.Bounds) {
			continue
		}
		if c := hf.TestShape(shape); c.Hit {
			hits = append(hits, OverlapHit{Heightfield: hf, Contact: c})
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

	curKeys := make([]pairKey, 0, len(current))
	for k := range current {
		curKeys = append(curKeys, k)
	}
	slices.SortFunc(curKeys, comparePairKeys)

	var exitKeys []pairKey
	for k := range w.activeContacts {
		if _, still := current[k]; !still {
			exitKeys = append(exitKeys, k)
		}
	}
	slices.SortFunc(exitKeys, comparePairKeys)

	events := make([]ContactEvent, 0, len(curKeys)+len(exitKeys))
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

	w.activeContacts = current
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
