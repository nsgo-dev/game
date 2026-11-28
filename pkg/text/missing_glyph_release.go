//go:build release

package text

type missingGlyphState struct{}

// SetLogMissingGlyphs is a no-op in release builds so missing-glyph logging
// compiles out completely.
func SetLogMissingGlyphs(bool) {}

// LogMissingGlyphsEnabled reports whether debug missing-glyph logging is
// enabled; it is always false in release builds.
func LogMissingGlyphsEnabled() bool { return false }

// SetMissingGlyphLogger is a no-op in release builds.
func SetMissingGlyphLogger(func(face string, r rune)) {}

func (*Font) logMissingGlyph(rune) {}
