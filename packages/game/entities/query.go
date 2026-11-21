package entities

// Each1 visits every live entity in w carrying component A in deterministic dense order until fn returns false.
// It performs no reflection and zero heap allocations.
func Each1[A any](w *World, fn func(Entity, *A) bool) {
	sa := existingStore[A](w)
	if sa == nil || len(sa.dense) == 0 {
		return
	}
	sa.Each(fn)
}

// Each2 visits every live entity in w carrying both A and B until fn returns false.
// It drives iteration from the smaller sparse set and checks membership in the other in O(1) time, with deterministic order, no reflection and zero heap allocations.
//
// A callback may destroy the entity it was handed, but must not add or remove other entities/components of the queried stores.
func Each2[A, B any](w *World, fn func(Entity, *A, *B) bool) {
	sa := existingStore[A](w)
	if sa == nil || len(sa.dense) == 0 {
		return
	}
	sb := existingStore[B](w)
	if sb == nil || len(sb.dense) == 0 {
		return
	}
	if len(sa.dense) <= len(sb.dense) {
		for i := 0; i < len(sa.dense); {
			e := sa.dense[i]
			if jb, ok := sb.denseIndex(e); ok {
				if !fn(e, &sa.values[i], &sb.values[jb]) {
					return
				}
			}
			if i < len(sa.dense) && sa.dense[i] == e {
				i++
			}
		}
		return
	}
	for i := 0; i < len(sb.dense); {
		e := sb.dense[i]
		if ja, ok := sa.denseIndex(e); ok {
			if !fn(e, &sa.values[ja], &sb.values[i]) {
				return
			}
		}
		if i < len(sb.dense) && sb.dense[i] == e {
			i++
		}
	}
}

// Each3 visits every live entity in w carrying A, B and C until fn returns false.
// It drives iteration from the smallest of the three sparse sets and checks membership in the other two in O(1) time, with deterministic order, no reflection and zero heap allocations.
func Each3[A, B, C any](w *World, fn func(Entity, *A, *B, *C) bool) {
	sa := existingStore[A](w)
	if sa == nil || len(sa.dense) == 0 {
		return
	}
	sb := existingStore[B](w)
	if sb == nil || len(sb.dense) == 0 {
		return
	}
	sc := existingStore[C](w)
	if sc == nil || len(sc.dense) == 0 {
		return
	}
	if len(sa.dense) <= len(sb.dense) && len(sa.dense) <= len(sc.dense) {
		for i := 0; i < len(sa.dense); {
			e := sa.dense[i]
			if jb, ok := sb.denseIndex(e); ok {
				if jc, ok := sc.denseIndex(e); ok {
					if !fn(e, &sa.values[i], &sb.values[jb], &sc.values[jc]) {
						return
					}
				}
			}
			if i < len(sa.dense) && sa.dense[i] == e {
				i++
			}
		}
		return
	}
	if len(sb.dense) <= len(sc.dense) {
		for i := 0; i < len(sb.dense); {
			e := sb.dense[i]
			if ja, ok := sa.denseIndex(e); ok {
				if jc, ok := sc.denseIndex(e); ok {
					if !fn(e, &sa.values[ja], &sb.values[i], &sc.values[jc]) {
						return
					}
				}
			}
			if i < len(sb.dense) && sb.dense[i] == e {
				i++
			}
		}
		return
	}
	for i := 0; i < len(sc.dense); {
		e := sc.dense[i]
		if ja, ok := sa.denseIndex(e); ok {
			if jb, ok := sb.denseIndex(e); ok {
				if !fn(e, &sa.values[ja], &sb.values[jb], &sc.values[i]) {
					return
				}
			}
		}
		if i < len(sc.dense) && sc.dense[i] == e {
			i++
		}
	}
}
