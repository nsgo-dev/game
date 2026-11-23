// Package devimage builds the image sources that asset hot reload hands a running game: an image's bytes read from disk in place of where it was loaded from.
package devimage

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// WithData returns src showing data, the bytes of an image file read from disk, in place of what src named. It keeps src's URI, so an "asset:" name such as hero@2x.png keeps its density, and its declared Size, and drops its Scales, Variants, Pixels and Offscreen, since data is one file at one scale. The core names an image with bytes by their digest, so the result loads afresh even where src's URI was already cached.
func WithData(src core.ImageSource, data []byte) core.ImageSource {
	src.Data = data
	src.Scales = 0
	src.Variants = nil
	src.Pixels = nil
	src.Offscreen = nil
	src.Version = 0
	return src
}

// Shown returns the URI of the file the core loads for src on a screen of ratio pixels per point, as it picks among src's Scales: src's URI for a single file or the 1x pick, the variant Variants names for the scale picked, else the "asset:" file named with that scale's suffix (asset:a.png at 2 is asset:a@2x.png).
func Shown(src core.ImageSource, ratio float32) string {
	// This mirrors the core's unexported Tree.pickScale (core/image.go in NSGo v0.3.0), which a game cannot call (nsgo-dev/nsgo#2272).
	if src.Scales == 0 || src.Variants == nil && (!strings.HasPrefix(src.URI, "asset:") || strings.ContainsAny(src.URI, "?#")) {
		return src.URI
	}
	pick := src.Scales.Pick(ratio)
	if pick == 1 {
		return src.URI
	}
	if uri, ok := src.Variants[pick]; ok {
		return uri
	}
	name := strings.LastIndexByte(src.URI, '/') + 1
	if name == 0 {
		name = len("asset:")
	}
	dot := strings.LastIndexByte(src.URI[name:], '.')
	if dot > 0 {
		dot += name
	} else {
		dot = len(src.URI)
	}
	return src.URI[:dot] + "@" + strconv.FormatFloat(float64(pick), 'g', -1, 32) + "x" + src.URI[dot:]
}

// Reloaded returns src showing data, the new bytes of the file uri names, when that file is the one a screen of ratio pixels per point shows for src (Shown), and false otherwise; same reports whether two URIs name the same file. The result is named after the file shown, so an "asset:" name such as hero@2x.png keeps the density it gives, while a source with several scales is left alone when uri is a scale the screen does not show.
func Reloaded(src core.ImageSource, uri string, data []byte, ratio float32, same func(a, b string) bool) (core.ImageSource, bool) {
	if src.URI == "" {
		return src, false
	}
	shown := Shown(src, ratio)
	if !same(shown, uri) {
		return src, false
	}
	src.URI = shown
	return WithData(src, data), true
}
