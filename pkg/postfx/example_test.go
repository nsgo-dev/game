package postfx_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/postfx"
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
	post := postfx.MustCompile(
		postfx.Effect{Name: "invert", Source: guideInvert},
		postfx.Effect{Name: "smear", Source: guideSmear},
	)
	u := post.Uniforms([]core.Uniforms{{"Amount": 0.6}, {"Offset": 1}})
	fmt.Println(post.Len(), len(u))
	// Output: 2 2
}

func ExampleBloom() {
	post := postfx.MustCompile(postfx.Bloom())
	u := post.Uniforms([]core.Uniforms{{"Threshold": 0.6, "Intensity": 1.5, "Radius": 4}})
	fmt.Println(len(u), post.HasUniform(0, "Radius"))
	// Output: 3 true
}
func ExampleCRT() {
	post := postfx.MustCompile(postfx.CRT())
	u := post.Uniforms([]core.Uniforms{{"Curvature": 0.25, "Scanlines": 0.35}})
	fmt.Println(len(u), post.HasUniform(0, "Curvature"))
	// Output: 2 true
}

func ExamplePaletteSwap() {
	pal := postfx.PaletteImage(
		[]core.Color{core.RGB(0xd03030), core.RGB(0x3060d0)},
		[]core.Color{core.RGB(0x40c8f0), core.RGB(0x8838c8)},
	)
	post := postfx.MustCompile(postfx.PaletteSwap(core.ImageSource{Pixels: pal, Version: 1}))
	u := post.Uniforms([]core.Uniforms{{"Amount": 1, "Row": 1}})
	fmt.Println(len(u), post.HasTexture(0))
	// Output: 2 true
}
