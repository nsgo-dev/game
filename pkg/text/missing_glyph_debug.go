//go:build !release

package text

import (
	"log"
	"sync"
	"sync/atomic"
)

var (
	logMissingEnabled atomic.Bool
	missingMu         sync.Mutex
	missingLogger     func(face string, r rune)
)

type missingGlyphState struct {
	seen map[rune]struct{}
}

// SetLogMissingGlyphs enables or disables logging each rune skipped by Font,
// SDFFont, or rich-text layout because the font has no glyph for it. Each
// missing rune is logged at most once per font instance.
//
// Under -tags release, SetLogMissingGlyphs is a no-op and missing-glyph
// logging is compiled out.
func SetLogMissingGlyphs(enabled bool) {
	logMissingEnabled.Store(enabled)
}

// LogMissingGlyphsEnabled reports whether debug missing-glyph logging is
// currently enabled. It always returns false under -tags release.
func LogMissingGlyphsEnabled() bool {
	return logMissingEnabled.Load()
}

// SetMissingGlyphLogger sets a custom handler called once per missing rune and
// font when SetLogMissingGlyphs(true) is active. Passing nil restores the
// default log.Print output. Under -tags release, this is a no-op.
func SetMissingGlyphLogger(fn func(face string, r rune)) {
	missingMu.Lock()
	missingLogger = fn
	missingMu.Unlock()
}

func (f *Font) logMissingGlyph(r rune) {
	if f == nil || !logMissingEnabled.Load() {
		return
	}
	missingMu.Lock()
	if f.missing == nil {
		f.missing = &missingGlyphState{}
	}
	if f.missing.seen == nil {
		f.missing.seen = make(map[rune]struct{})
	}
	if _, dup := f.missing.seen[r]; dup {
		missingMu.Unlock()
		return
	}
	f.missing.seen[r] = struct{}{}
	logger := missingLogger
	face := f.Face
	missingMu.Unlock()

	if logger != nil {
		logger(face, r)
		return
	}
	if face != "" {
		log.Printf("text: font %q has no glyph for %q (U+%04X)", face, r, r)
	} else {
		log.Printf("text: font has no glyph for %q (U+%04X)", r, r)
	}
}
