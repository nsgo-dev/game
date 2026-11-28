package entities

// Each4 visits entities carrying all four components in the smallest-store order.
func Each4[A, B, C, D any](w *World, fn func(Entity, *A, *B, *C, *D) bool) {
	sa, sb, sc, sd := existingStore[A](w), existingStore[B](w), existingStore[C](w), existingStore[D](w)
	if sa == nil || sb == nil || sc == nil || sd == nil {
		return
	}
	stores := []int{len(sa.dense), len(sb.dense), len(sc.dense), len(sd.dense)}
	driver := 0
	for i := 1; i < 4; i++ {
		if stores[i] < stores[driver] {
			driver = i
		}
	}
	for i := 0; ; i++ {
		var e Entity
		switch driver {
		case 0:
			if i >= len(sa.dense) {
				return
			}
			e = sa.dense[i]
		case 1:
			if i >= len(sb.dense) {
				return
			}
			e = sb.dense[i]
		case 2:
			if i >= len(sc.dense) {
				return
			}
			e = sc.dense[i]
		default:
			if i >= len(sd.dense) {
				return
			}
			e = sd.dense[i]
		}
		ja, oka := sa.denseIndex(e)
		jb, okb := sb.denseIndex(e)
		jc, okc := sc.denseIndex(e)
		jd, okd := sd.denseIndex(e)
		if oka && okb && okc && okd && !fn(e, &sa.values[ja], &sb.values[jb], &sc.values[jc], &sd.values[jd]) {
			return
		}
	}
}
