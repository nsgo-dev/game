package entities

import (
	"math"
	"sort"
)

type LOD struct {
	Level, Previous int
	Steps           int
	LastDistance    float32
}
type LODBand struct {
	MaxDistance float32
	Level       int
}
type Viewer struct{ X, Y float32 }

// ResolveLOD chooses the nearest viewer band with hysteresis and deterministic
// tie behavior. Bands are evaluated in ascending MaxDistance.
func ResolveLOD(current int, distance float32, bands []LODBand, hysteresis float32) int {
	if len(bands) == 0 || math.IsNaN(float64(distance)) {
		return current
	}
	cp := append([]LODBand(nil), bands...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].MaxDistance < cp[j].MaxDistance })
	target := cp[len(cp)-1].Level
	for _, b := range cp {
		if distance <= b.MaxDistance {
			target = b.Level
			break
		}
	}
	if target > current && hysteresis > 0 {
		for _, b := range cp {
			if b.Level == current && distance <= b.MaxDistance*(1+hysteresis) {
				return current
			}
		}
	}
	if target < current && hysteresis > 0 {
		for _, b := range cp {
			if b.Level == target && distance >= b.MaxDistance*(1-hysteresis) {
				return current
			}
		}
	}
	return target
}
func NearestViewerDistance(x, y float32, viewers []Viewer) float32 {
	best := float32(math.MaxFloat32)
	for _, v := range viewers {
		d := float32(math.Hypot(float64(x-v.X), float64(y-v.Y)))
		if d < best {
			best = d
		}
	}
	return best
}
