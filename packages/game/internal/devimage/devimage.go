// Package devimage builds the image sources that asset hot reload hands a running game: an image's bytes read from disk in place of where it was loaded from.
package devimage

import "github.com/nsgo-dev/nsgo/core"

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
