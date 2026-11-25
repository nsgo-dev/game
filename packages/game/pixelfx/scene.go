// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/scene.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Preview backdrops: what tools draw behind an effect. Sprite sheets stay transparent.

var (
	sky, skyTop, skyLow                     = Hex("#15131d"), Hex("#0d0b14"), Hex("#221d33")
	floorEdge, floorCol, floorDark, mortarC = Hex("#4a4263"), Hex("#2b2639"), Hex("#211d2d"), Hex("#1a1724")
	starTint                                = Hex("#cfc8ff")
	bayerInts                               = [16]int{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5}
)

// Backdrops are the kinds Backdrop draws.
var Backdrops = []string{"dark", "ground", "sky", "light", "black"}

// Backdrop returns a backdrop as w x h colors. kind is "dark", "ground" (a stone floor from groundY down), "sky" (a night gradient with stars), "light" or "black"; any other kind is "dark".
func Backdrop(kind string, w, h, groundY int) []uint32 {
	out := make([]uint32, w*h)
	switch kind {
	case "light":
		fill(out, Hex("#ebe8f0"))
		return out
	case "black":
		fill(out, Hex("#000000"))
		return out
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := sky
			if kind == "sky" {
				t := float64(y) / math.Max(1, float64(h-1))
				step := float64(ToInt32(t*5 + float64(bayerInts[(y&3)<<2|(x&3)])/16))
				c = Mix(skyTop, skyLow, math.Min(1, step/4))
				if r := Rand(float64(x), float64(y), 77); r > 0.994 {
					c = Mix(c, starTint, 0.35+(r-0.994)*90)
				}
			}
			if kind == "ground" && y >= groundY {
				gy := y - groundY
				if gy == 0 {
					c = floorEdge
				} else {
					row := (gy - 1) / 5
					off := 0
					if row%2 != 0 {
						off = 6
					}
					switch {
					case (gy-1)%5 == 4 || (x+off)%12 == 0:
						c = mortarC
					case gy < 3:
						c = floorCol
					default:
						c = floorDark
					}
				}
			}
			out[y*w+x] = c
		}
	}
	return out
}

func fill(s []uint32, c uint32) {
	for i := range s {
		s[i] = c
	}
}
