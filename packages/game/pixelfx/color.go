// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/color.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Colors are 32-bit integers laid out like RGBA bytes in memory (R in the low byte, then G, B and A), so a frame's color buffer reads as RGBA pixels. 0 is "no color": painting it changes nothing.

// RGB returns the opaque color (r, g, b); each channel is converted as JavaScript's x & 255 converts it, so fractions truncate.
func RGB(r, g, b float64) uint32 {
	return 255<<24 | uint32(ToInt32(b)&255)<<16 | uint32(ToInt32(g)&255)<<8 | uint32(ToInt32(r)&255)
}

// Red returns c's red channel.
func Red(c uint32) uint32 { return c & 255 }

// Green returns c's green channel.
func Green(c uint32) uint32 { return (c >> 8) & 255 }

// Blue returns c's blue channel.
func Blue(c uint32) uint32 { return (c >> 16) & 255 }

// ParseHex reads "#rgb", "#rrggbb" or "#rrggbbaa" as an opaque color: the alpha of "#rrggbbaa" must be hex digits but is ignored, since every color here is opaque.
func ParseHex(h string) (uint32, error) {
	bad := fmt.Errorf("color %q is not #rgb, #rrggbb or #rrggbbaa (alpha ignored)", h)
	s, ok := strings.CutPrefix(h, "#")
	if !ok {
		return 0, bad
	}
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 && len(s) != 8 {
		return 0, bad
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, bad
	}
	if len(s) == 8 {
		v >>= 8
	}
	return RGB(float64(v>>16&255), float64(v>>8&255), float64(v&255)), nil
}

// Hex reads a color as ParseHex does ("#rgb", "#rrggbb", or "#rrggbbaa" with its alpha ignored) and panics on anything else: use it for colors written in code.
func Hex(h string) uint32 {
	c, err := ParseHex(h)
	if err != nil {
		panic("pixelfx: " + err.Error())
	}
	return c
}

// ToHex returns c as "#rrggbb", leaving out its alpha.
func ToHex(c uint32) string {
	return fmt.Sprintf("#%02x%02x%02x", Red(c), Green(c), Blue(c))
}

// Mix returns the linear mix of a and b, t (held to 0..1) of the way to b.
func Mix(a, b uint32, t float64) uint32 {
	t = Clamp(t)
	ra, ga, ba := float64(Red(a)), float64(Green(a)), float64(Blue(a))
	return RGB(ra+(float64(Red(b))-ra)*t, ga+(float64(Green(b))-ga)*t, ba+(float64(Blue(b))-ba)*t)
}

// Scale multiplies c's brightness by k.
func Scale(c uint32, k float64) uint32 {
	return RGB(math.Min(255, float64(Red(c))*k), math.Min(255, float64(Green(c))*k), math.Min(255, float64(Blue(c))*k))
}

// Luma returns c's perceived brightness, 0..1.
func Luma(c uint32) float64 {
	return (0.299*float64(Red(c)) + 0.587*float64(Green(c)) + 0.114*float64(Blue(c))) / 255
}

// Ramp returns a list of hex colors (dark to bright) as colors, panicking on a malformed one as Hex does.
func Ramp(list ...string) []uint32 {
	out := make([]uint32, len(list))
	for i, h := range list {
		out[i] = Hex(h)
	}
	return out
}

// Fit resamples ramp r to exactly n steps, keeping its ends (the darkest and brightest stay put), so an effect can index steps by number whatever length of palette a caller passes. It returns r itself when it already has n steps, and nil when r is empty, since there is nothing to resample.
func Fit(r []uint32, n int) []uint32 {
	if len(r) == n {
		return r
	}
	if len(r) == 0 {
		return nil
	}
	out := make([]uint32, n)
	d := float64(max(1, n-1))
	for i := range out {
		out[i] = r[int(Round(float64(i*(len(r)-1))/d))]
	}
	return out
}

// At returns the ramp step for t in 0..1, no blending: pixel art picks a color, it doesn't fade between two.
func At(r []uint32, t float64) uint32 {
	return r[int(ClampTo(math.Floor(t*float64(len(r))), 0, float64(len(r)-1)))]
}

// Blend returns the color blended along a ramp at t (for light, where smooth is right).
func Blend(r []uint32, t float64) uint32 {
	x := Clamp(t) * float64(len(r)-1)
	i := math.Min(float64(len(r)-2), math.Floor(x))
	return Mix(r[int(i)], r[int(i)+1], x-i)
}

// Pal holds pixelfx's hue-shifted ramps as hex colors, dark to bright: shadows lean cool or deep, highlights lean warm or white. Effects take a palette option, so any of these (or your own) can recolor them.
var Pal = map[string][]string{
	"fire":     {"#1f0b10", "#4a1118", "#8c1d1c", "#cc3a1a", "#f2701f", "#ffae35", "#ffe27a", "#fff8d6"},
	"ember":    {"#1a0a0e", "#3d1012", "#701a14", "#a8301a", "#e0561e", "#ff8a2a", "#ffc46a"},
	"lava":     {"#14060a", "#380a10", "#6e1210", "#ac2410", "#e8501a", "#ff8c2a", "#ffd25a"},
	"smoke":    {"#0e0c12", "#1d1a22", "#2e2a35", "#433e4b", "#5c5664", "#7b7484", "#a39cab"},
	"dust":     {"#1c1610", "#33281c", "#4f3f2c", "#6e5a40", "#927a58", "#b89f78", "#dcc8a2"},
	"ice":      {"#0c1430", "#162c5c", "#1f4f94", "#2f7fcc", "#58b4ec", "#9bdcff", "#d8f4ff", "#ffffff"},
	"water":    {"#081830", "#0e2e5a", "#14508c", "#1c76bc", "#36a2e0", "#78ccf4", "#c4ecff", "#ffffff"},
	"electric": {"#0a0f2e", "#172a78", "#2550d0", "#3f8cff", "#7cc8ff", "#c8f0ff", "#ffffff"},
	"volt":     {"#24160a", "#5e3a0c", "#a8680f", "#e8a218", "#ffd23c", "#fff08a", "#ffffff"},
	"poison":   {"#0d1a0c", "#173a14", "#265e18", "#3f8a1e", "#6cb82a", "#a8e04a", "#e0ff8a"},
	"holy":     {"#2a1c08", "#5a3e10", "#9a6c18", "#d8a428", "#ffd84a", "#fff09a", "#fffbe6", "#ffffff"},
	"heal":     {"#08201a", "#0f3f2c", "#17663e", "#24924e", "#3ec466", "#7ee88a", "#c8ffc4", "#ffffff"},
	"arcane":   {"#140a26", "#2b1252", "#481c88", "#6b2cc0", "#9a48ec", "#c684ff", "#ecc8ff", "#ffffff"},
	"shadow":   {"#050308", "#0f0a18", "#1c1230", "#2c1a4a", "#432468", "#5e3290", "#8048b8", "#a970e0"},
	"blood":    {"#14040a", "#3a0a14", "#6a101c", "#a01824", "#d42a30", "#ff5a50", "#ff9a8a"},
	"earth":    {"#140e0a", "#2a1e14", "#47321e", "#69492a", "#8e6638", "#b88a52", "#dcb47c"},
	"stone":    {"#0e0e12", "#1c1c24", "#2c2c38", "#42424f", "#5c5c6b", "#7d7d8c", "#a4a4b2", "#d0d0da"},
	"gold":     {"#1e1206", "#4a2c08", "#86520c", "#c48412", "#f0b424", "#ffe066", "#fff6c4", "#ffffff"},
	"leaf":     {"#0c1a0e", "#163418", "#225224", "#357530", "#4f9a3a", "#7cc048", "#b4e070", "#e4ffb0"},
	"wood":     {"#160c06", "#2e1a0c", "#4a2c14", "#6a421e", "#8e5c2c", "#b47c40", "#d8a464"},
	"metal":    {"#0c0e14", "#1c2230", "#2e3648", "#4a5468", "#6e7a90", "#9aa6ba", "#d0d8e6", "#ffffff"},
	"bone":     {"#1a1814", "#3a352c", "#5e5648", "#8a806c", "#b8ad96", "#e4dcc8", "#fffaf0"},
	"neon":     {"#1a0516", "#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff"},
	"cyber":    {"#041418", "#08303a", "#0c5260", "#108088", "#18b4b0", "#4ce8d8", "#b4fff0", "#ffffff"},
	"matrix":   {"#020a04", "#05200c", "#0a3c16", "#106624", "#1a9a34", "#3cd650", "#a4ffa8", "#e8ffe8"},
	"candy":    {"#ff4a5a", "#ff9a2a", "#ffe03c", "#5ad84a", "#3cc8ff", "#7a6cff", "#f53cd1"},
	"night":    {"#07060d", "#0c0a18", "#131026", "#1b1734", "#241f44", "#2f2956"},
}

// Ramps holds the ramps of Pal as colors.
var Ramps = func() map[string][]uint32 {
	out := make(map[string][]uint32, len(Pal))
	for k, v := range Pal {
		out[k] = Ramp(v...)
	}
	return out
}()

// Colors pixelfx's effects share.
var (
	White = Hex("#ffffff")
	Black = Hex("#000000")
	Ink   = Hex("#0b0a10")
	Patch = Hex("#f53cd1")
)
