package effects

import "github.com/nsgo-dev/nsgo/core"

// vector returns the first n lanes uniform u sets name to, as core.Uniforms fills a vector's lanes: a core.Color's red, green, blue and alpha, not premultiplied, or a slice of numbers or constants. Lanes left out, and lanes set to anything else, are 0, as on the GPU.
func vector(u core.Uniforms, name string, n int) [4]float64 {
	var out [4]float64
	put := func(i int, v float64) {
		if i < n && i < len(out) {
			out[i] = v
		}
	}
	switch x := u[name].(type) {
	case core.Color:
		r, g, b, a := x.Components()
		for i, v := range [4]float64{r, g, b, a} {
			put(i, v)
		}
	case []float64:
		for i, v := range x {
			put(i, v)
		}
	case []float32:
		for i, v := range x {
			put(i, float64(v))
		}
	case []int:
		for i, v := range x {
			put(i, float64(v))
		}
	case []core.AnimatedNode:
		for i, v := range x {
			if c, ok := v.(core.Constant); ok {
				put(i, float64(c))
			}
		}
	case []any:
		for i, v := range x {
			put(i, Float(core.Uniforms{name: v}, name))
		}
	}
	return out
}
