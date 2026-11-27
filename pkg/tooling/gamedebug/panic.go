//go:build !release

package gamedebug

import (
	"fmt"
	rdebug "runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	maxPanicStackBytes = 64 * 1024
	maxPanicValueBytes = 4 * 1024
)

var (
	panicMu   sync.RWMutex
	lastPanic *PanicRecord
)

// RecordPanic stores a recovered panic value and its stack trace so development
// tools such as game_log_read can surface the most recent crash or recovered
// panic. If recovered is nil, RecordPanic does nothing and returns the zero
// PanicRecord. If stack is empty, runtime/debug.Stack is captured automatically.
func RecordPanic(recovered any, stack []byte, context ...string) PanicRecord {
	if recovered == nil {
		return PanicRecord{}
	}
	if len(stack) == 0 {
		stack = rdebug.Stack()
	}
	rec := PanicRecord{
		Time:    time.Now().UTC(),
		Value:   truncatePanicString(fmt.Sprint(recovered), maxPanicValueBytes),
		Stack:   truncatePanicString(string(stack), maxPanicStackBytes),
		Context: joinPanicContext(context),
	}
	panicMu.Lock()
	cp := rec
	lastPanic = &cp
	panicMu.Unlock()
	return rec
}

// Recover calls recover() directly and records any panic via RecordPanic.
// Call it directly as a deferred function (`defer gamedebug.Recover("loop.Update")`);
// calling it indirectly from another function will not catch panics per Go's
// recover rules.
func Recover(context ...string) (PanicRecord, bool) {
	r := recover()
	if r == nil {
		return PanicRecord{}, false
	}
	return RecordPanic(r, nil, context...), true
}

// RecoverFunc executes fn and recovers any panic, recording it via RecordPanic.
// It reports the captured PanicRecord and whether fn panicked.
func RecoverFunc(context string, fn func()) (rec PanicRecord, panicked bool) {
	if fn == nil {
		return PanicRecord{}, false
	}
	defer func() {
		if r := recover(); r != nil {
			rec = RecordPanic(r, nil, context)
			panicked = true
		}
	}()
	fn()
	return PanicRecord{}, false
}

// LastPanic returns the most recent panic recorded by RecordPanic, Recover or
// RecoverFunc.
func LastPanic() (PanicRecord, bool) {
	panicMu.RLock()
	defer panicMu.RUnlock()
	if lastPanic == nil {
		return PanicRecord{}, false
	}
	return *lastPanic, true
}

// ClearPanic removes the stored last panic record.
func ClearPanic() {
	panicMu.Lock()
	lastPanic = nil
	panicMu.Unlock()
}

func joinPanicContext(parts []string) string {
	var clean []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			clean = append(clean, s)
		}
	}
	return strings.Join(clean, ": ")
}

func truncatePanicString(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const suffix = "\n... (truncated)"
	cut := maxBytes
	for cut > 0 && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return s[:cut] + suffix
}
