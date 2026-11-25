// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/actor.js and src/core/poses.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// A stock character to try effects on: status effects wrap its silhouette, hits land on it, auras sit behind it. Previews draw it; effects only read its mask through Options.Actor.

// Hero is the stock character at 12 x 19 px.
var Hero = []string{
	"....kkkkk...",
	"..kkhhhhhk..",
	".khhhhhhhhk.",
	".khhhhhhhhhk",
	".khhssssshhk",
	".khsessesshk",
	".khsessesshk",
	"..ksssssssk.",
	"...kkSSSkk..",
	"..kttttttk..",
	".kttTttTttk.",
	".ksttTTttsk.",
	".kstttttttk.",
	"..kbbbbbbk..",
	"..kppkkppk..",
	"..kppk.kppk.",
	"..kppk.kppk.",
	"..koook.kook",
	"..kkkkk.kkkk",
}

// HeroPal holds Hero's colors, and those of the larger character's extra detail and scarf.
var HeroPal = palette(map[rune]string{
	'k': "#17121f", 'h': "#7a4128", 's': "#f3c49d", 'S': "#c98b6c", 'e': "#17121f",
	't': "#5f6f94", 'T': "#46536f", 'b': "#8a5a32", 'p': "#38334a", 'o': "#6a4030",
	'H': "#a35d34", 'w': "#ffffff", 'm': "#a3564a", 'g': "#e8c060", 'L': "#7f90b6", 'O': "#8f5a3f", 'P': "#4b4560",
	'c': "#f53cd1", 'C': "#ff8fe6", 'D': "#a31f8a",
})

// DummyPal holds the dummy's colors: a neutral shell that lets any effect's colors do the talking, near and far limbs, dark bands at the joints, a small mark.
var DummyPal = palette(map[rune]string{
	'k': "#0e0c14", 'A': "#e4e6ec", 'B': "#b6b9c3", 'C': "#868995", 'D': "#5c5e6a", 'E': "#6e707c", 'F': "#4b4d58", 'G': "#34353f", 'J': "#2a2b33", 'm': "#f53cd1",
})

// Hero2 is the same character at twice the size (24 x 38), standing easy: the rest pose of the rig, baked.
var Hero2 = []string{
	"...........kkk..........",
	"..........kAABk.........",
	".........kAAABJk........",
	".........kABBJJk........",
	".........kBBBJCDk.......",
	".........kBCJDDDk.......",
	".........kkJJJDk........",
	".......kkkkABBJCkk......",
	"......kAABCJBCCCkDk.....",
	"......kABBCJCCCmDkDk....",
	"......kBBCDDJCDDDkDk....",
	"......kABCDJDDDDDkk.....",
	".....kAABDkDDDDJkk......",
	".....kABCDkDDJJGkk......",
	".....kABCkJJJGGGkk......",
	"....kJJCDkFFFGGGk.......",
	"....kAJJDkJJJGGGk.......",
	"....kJCJJkABBJGGk.......",
	".....kJCCkBBCDJJJk......",
	".....kCCCJBBCDJJDk......",
	".....kCCCDJCCDkJDDk.....",
	"......kCDDJBCDkDDDk.....",
	".......kDJJJCDkDDDkk....",
	".......kJBCJCDkDDDkk....",
	".......kBDJBCDkCDDkk....",
	"........kJJBCDkCDDk.....",
	"........kBBBCDkCCDDk....",
	"........kBBBCDkCCDDk....",
	".........kBBCDkJDDDk....",
	"........kBJJJJkCJJJk....",
	"........kBBCCDkCCDDk....",
	"........kBBCDDkCDDk.....",
	"........kBBCDkCCDDk.....",
	"........kBCCDkCCDDk.....",
	"........kBCDDkCDDDk.....",
	".......kJBCJkkkDJkkkk...",
	".......kAJJAAAAkCCCCCk..",
	"........kkkkkkkkkkkkk...",
}

func palette(m map[rune]string) map[rune]uint32 {
	out := make(map[rune]uint32, len(m))
	for k, v := range m {
		out[k] = Hex(v)
	}
	return out
}

func maskOf(rows []string, k int) *Mask {
	h, w := len(rows)*k, len([]rune(rows[0]))*k
	m := &Mask{W: w, H: h, Data: make([]uint8, w*h)}
	for y, r := range rows {
		x := -1
		for _, ch := range r {
			x++
			if ch == '.' {
				continue
			}
			for j := 0; j < k*k; j++ {
				m.Data[(y*k+j/k)*w+x*k+j%k] = 1
			}
		}
	}
	return m
}

// pixelsOf returns the character's own colors, row by row (0 where empty), for effects that recolor it: frozen, poisoned, stone.
func pixelsOf(rows []string, pal map[rune]uint32) []uint32 {
	w := len([]rune(rows[0]))
	data := make([]uint32, w*len(rows))
	for y, r := range rows {
		x := -1
		for _, ch := range r {
			x++
			if ch != '.' {
				data[y*w+x] = pal[ch]
			}
		}
	}
	return data
}

var (
	actorMasks  = [3]*Mask{nil, maskOf(Hero, 1), maskOf(Hero2, 1)}
	actorPixels = [3][]uint32{nil, pixelsOf(Hero, HeroPal), pixelsOf(Hero2, DummyPal)}
)

// Actor is a character an effect wraps, hits or sits behind, in local space with its feet at (0, 0): its Mask and its own Pixels (W x H, top-left at X, Y), its scale K, the top of its Head, its Chest, and, at scale 2, its Eyes (in the mask).
type Actor struct {
	Mask   *Mask
	Pixels []uint32
	X, Y   float64
	W, H   int
	K      int
	Head   [2]float64
	Chest  [2]float64
	Eyes   [][2]float64
}

// StockActor returns the stock actor at scale k: 1 is 12 x 19 px, 2 is 24 x 38 px (any other k is 1).
func StockActor(k int) *Actor {
	if k != 2 {
		k = 1
	}
	m := actorMasks[k]
	x, y := -math.Floor(float64(m.W)/2), -float64(m.H)
	a := &Actor{Mask: m, Pixels: actorPixels[k], X: x, Y: y, W: m.W, H: m.H, K: k, Head: [2]float64{0, y}, Chest: [2]float64{0, y + 11}}
	if k == 2 {
		a.Chest = [2]float64{0, -26}
		a.Eyes = [][2]float64{{13, 3}, {15, 3}}
	}
	return a
}

// DrawActor draws the stock actor with its feet at the painter's origin, at alpha a (dithered); nil draws the 12 x 19 one.
func DrawActor(p *Painter, actor *Actor, a float64) {
	if actor == nil {
		actor = StockActor(1)
	}
	if a <= 0 {
		return
	}
	if actor.K == 2 {
		p.Sprite(Hero2, DummyPal, actor.X, actor.Y, a, SpriteOpts{})
		return
	}
	p.Sprite(Hero, HeroPal, actor.X, actor.Y, a, SpriteOpts{})
}

// Ready is the stance acts start from. A pose only says what differs from standing easy (Stand), in screen terms, px from the feet, y negative up; Pose says what each key does.
var Ready = Pose{}.With(Crouch.Is(3), FX.Is(11), FY.Is(-21), BX.Is(9), BY.Is(-21), FFX.Is(7), FBX.Is(-7))
