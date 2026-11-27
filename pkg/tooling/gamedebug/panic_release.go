//go:build release

package gamedebug

// RecordPanic is a no-op in release builds.
func RecordPanic(any, []byte, ...string) PanicRecord { return PanicRecord{} }

// Recover is a no-op in release builds.
func Recover(...string) (PanicRecord, bool) { return PanicRecord{}, false }

// RecoverFunc runs fn directly without recovering in release builds.
func RecoverFunc(_ string, fn func()) (PanicRecord, bool) {
	if fn != nil {
		fn()
	}
	return PanicRecord{}, false
}

// LastPanic always returns the zero PanicRecord and false in release builds.
func LastPanic() (PanicRecord, bool) { return PanicRecord{}, false }

// ClearPanic is a no-op in release builds.
func ClearPanic() {}
