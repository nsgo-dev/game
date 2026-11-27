package postfx

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
)

// vector returns the n lanes, 3 or 4, that uniform u sets name to, filled as core.Uniforms fills a vector's lanes in order: a number, a bool or a core.Constant fills one lane; a core.Color fills its red, green and blue, not premultiplied, and its alpha too when n is 4; a slice fills the lanes of its elements in turn, a []any or []core.AnimatedNode spreading the elements it holds. Lanes left out are 0. A value with more lanes than n, or one that is not numbers, constants or a core.Color, panics, as the GPU path refuses it. Chain.Reference refuses animated nodes before an effect reads its uniforms.
func vector(u core.Uniforms, name string, n int) [4]float64 {
	w := laneFiller{name: name, n: min(n, 4), vec3: n == 3}
	w.put(u[name])
	return w.lanes
}

// laneFiller fills a vector uniform's lanes as nsgo's core fills them for the GPU (core/shader.go, laneWriter.put).
type laneFiller struct {
	lanes [4]float64
	at, n int
	name  string
	vec3  bool
}

func (w *laneFiller) lane(x float64) {
	if w.at >= w.n {
		panic(fmt.Sprintf("postfx: the shader's uniform %s has fewer lanes than its value", w.name))
	}
	w.lanes[w.at] = x
	w.at++
}

func (w *laneFiller) put(v any) {
	switch x := v.(type) {
	case nil:
	case core.Color:
		r, g, b, a := x.Components()
		w.lane(r)
		w.lane(g)
		w.lane(b)
		if !w.vec3 {
			w.lane(a)
		}
	case float64, float32, int, int32, int64, uint, uint32, uint64, bool, core.Constant:
		w.lane(Float(core.Uniforms{w.name: x}, w.name))
	case []float64:
		for _, e := range x {
			w.lane(e)
		}
	case []float32:
		for _, e := range x {
			w.lane(float64(e))
		}
	case []int:
		for _, e := range x {
			w.lane(float64(e))
		}
	case []core.AnimatedNode:
		for _, e := range x {
			w.put(e)
		}
	case []any:
		for _, e := range x {
			w.put(e)
		}
	default:
		panic(fmt.Sprintf("postfx: the shader's uniform %s is set to a %T, not numbers, constants or a Color", w.name, v))
	}
}
