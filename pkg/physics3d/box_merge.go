package physics3d

import (
	"cmp"
	"slices"
)

type mergeBox struct {
	lo [3]float32
	hi [3]float32
}

const defaultBoxMergeEps float32 = 1e-4

// MergeBoxes greedily merges adjacent axis-aligned boxes along X, then Z, then
// Y whenever their perpendicular faces align within eps (defaults to 1e-4 when
// <= 0). Degenerate boxes with non-positive extent on any axis are dropped.
//
// The point-set union of the returned boxes equals the union of the valid input
// boxes, and the result order is deterministic regardless of input slice order.
func MergeBoxes(boxes []Box, eps float32) []Box {
	if len(boxes) == 0 {
		return nil
	}
	if eps <= 0 {
		eps = defaultBoxMergeEps
	}

	work := make([]mergeBox, 0, len(boxes))
	for _, b := range boxes {
		mn, mx := b.Min(), b.Max()
		if mx.X <= mn.X || mx.Y <= mn.Y || mx.Z <= mn.Z {
			continue
		}
		if !isFiniteVec(mn) || !isFiniteVec(mx) {
			continue
		}
		work = append(work, mergeBox{
			lo: [3]float32{mn.X, mn.Y, mn.Z},
			hi: [3]float32{mx.X, mx.Y, mx.Z},
		})
	}
	if len(work) == 0 {
		return nil
	}

	// Pass 1: merge along X (axis 0), requiring Y (1) and Z (2) faces to match.
	work = mergeBoxesAlongAxis(work, 0, 1, 2, eps)
	// Pass 2: merge along Z (axis 2), requiring Y (1) and X (0) faces to match.
	work = mergeBoxesAlongAxis(work, 2, 1, 0, eps)
	// Pass 3: merge along Y (axis 1), requiring X (0) and Z (2) faces to match.
	work = mergeBoxesAlongAxis(work, 1, 0, 2, eps)

	// Canonical deterministic output order: ascending (minY, minZ, minX, maxY, maxZ, maxX).
	slices.SortFunc(work, func(a, b mergeBox) int {
		if c := cmp.Compare(a.lo[1], b.lo[1]); c != 0 {
			return c
		}
		if c := cmp.Compare(a.lo[2], b.lo[2]); c != 0 {
			return c
		}
		if c := cmp.Compare(a.lo[0], b.lo[0]); c != 0 {
			return c
		}
		if c := cmp.Compare(a.hi[1], b.hi[1]); c != 0 {
			return c
		}
		if c := cmp.Compare(a.hi[2], b.hi[2]); c != 0 {
			return c
		}
		return cmp.Compare(a.hi[0], b.hi[0])
	})

	out := make([]Box, len(work))
	for i, mb := range work {
		out[i] = BoxFromMinMax(
			Vec3{X: mb.lo[0], Y: mb.lo[1], Z: mb.lo[2]},
			Vec3{X: mb.hi[0], Y: mb.hi[1], Z: mb.hi[2]},
		)
	}
	return out
}

// mergeBoxesAlongAxis partitions boxes into clusters whose perpendicular faces
// (p1, p2) match within eps without quantization boundary misses, then merges
// touching or overlapping runs along axis.
func mergeBoxesAlongAxis(work []mergeBox, axis, p1, p2 int, eps float32) []mergeBox {
	if len(work) <= 1 {
		return work
	}

	// Cluster by lo[p1], then hi[p1], then lo[p2], then hi[p2], then merge along axis.
	out := make([]mergeBox, 0, len(work))

	// Sort by lo[p1] first.
	slices.SortFunc(work, func(a, b mergeBox) int {
		return cmp.Compare(a.lo[p1], b.lo[p1])
	})
	for i := 0; i < len(work); {
		refLo1 := work[i].lo[p1]
		j := i + 1
		for j < len(work) && work[j].lo[p1]-refLo1 <= eps {
			j++
		}
		c1 := work[i:j]
		i = j

		// Within c1, sort and cluster by hi[p1].
		slices.SortFunc(c1, func(a, b mergeBox) int {
			return cmp.Compare(a.hi[p1], b.hi[p1])
		})
		for i2 := 0; i2 < len(c1); {
			refHi1 := c1[i2].hi[p1]
			j2 := i2 + 1
			for j2 < len(c1) && c1[j2].hi[p1]-refHi1 <= eps {
				j2++
			}
			c2 := c1[i2:j2]
			i2 = j2

			// Within c2, sort and cluster by lo[p2].
			slices.SortFunc(c2, func(a, b mergeBox) int {
				return cmp.Compare(a.lo[p2], b.lo[p2])
			})
			for i3 := 0; i3 < len(c2); {
				refLo2 := c2[i3].lo[p2]
				j3 := i3 + 1
				for j3 < len(c2) && c2[j3].lo[p2]-refLo2 <= eps {
					j3++
				}
				c3 := c2[i3:j3]
				i3 = j3

				// Within c3, sort and cluster by hi[p2].
				slices.SortFunc(c3, func(a, b mergeBox) int {
					return cmp.Compare(a.hi[p2], b.hi[p2])
				})
				for i4 := 0; i4 < len(c3); {
					refHi2 := c3[i4].hi[p2]
					j4 := i4 + 1
					for j4 < len(c3) && c3[j4].hi[p2]-refHi2 <= eps {
						j4++
					}
					run := c3[i4:j4]
					i4 = j4

					// Sort run along merge axis and greedily combine touching intervals.
					slices.SortFunc(run, func(a, b mergeBox) int {
						if c := cmp.Compare(a.lo[axis], b.lo[axis]); c != 0 {
							return c
						}
						return cmp.Compare(a.hi[axis], b.hi[axis])
					})

					cur := run[0]
					for k := 1; k < len(run); k++ {
						next := run[k]
						if next.lo[axis] <= cur.hi[axis]+eps &&
							abs32(next.lo[p1]-cur.lo[p1]) <= eps &&
							abs32(next.hi[p1]-cur.hi[p1]) <= eps &&
							abs32(next.lo[p2]-cur.lo[p2]) <= eps &&
							abs32(next.hi[p2]-cur.hi[p2]) <= eps {
							if next.hi[axis] > cur.hi[axis] {
								cur.hi[axis] = next.hi[axis]
							}
						} else {
							out = append(out, cur)
							cur = next
						}
					}
					out = append(out, cur)
				}
			}
		}
	}
	return out
}
