package effects_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/effects"
	"github.com/nsgo-dev/nsgo/core"
)

// The effects of the package's guide.
const (
	guideInvert = `package main

// Amount is how far the colors turn, 0 to 1.
var Amount float

func Effect(pos vec2) vec4 {
	c := Input(pos)
	turned := vec4(c.a-c.r, c.a-c.g, c.a-c.b, c.a)
	return c + (turned-c)*Amount
}
`
	guideSmear = `package main

// Offset is how far to the right, in the frame's pixels, the second tap reads.
var Offset float

func Effect(pos vec2) vec4 {
	p := vec2(min(pos.x+Offset, InputSize().x-0.5), pos.y)
	return (Input(pos) + Input(p)) * 0.5
}
`
)

func ExampleCompile() {
	post := effects.MustCompile(
		effects.Effect{Name: "invert", Source: guideInvert},
		effects.Effect{Name: "smear", Source: guideSmear},
	)
	u := post.Uniforms([]core.Uniforms{{"Amount": 0.6}, {"Offset": 1}})
	fmt.Println(post.Len(), len(u))
	// Output: 2 2
}

func ExampleBloom() {
	post := effects.MustCompile(effects.Bloom())
	u := post.Uniforms([]core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": 4}})
	fmt.Println(len(u), post.HasUniform(0, "Radius"))
	// Output: 3 true
}
