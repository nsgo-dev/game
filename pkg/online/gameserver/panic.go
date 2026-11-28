package gameserver

import (
	"fmt"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/online/liveops/crashreport"
)

// PanicAction is the outcome Runner.OnPanic chooses for a recovered panic.
type PanicAction uint8

const (
	// StopMatch ends only the panicking match: it never ticks again (its
	// state may be half-updated), its OnStop runs once, and every other match
	// on the runner keeps going. It is the zero value and the default when
	// OnPanic is nil.
	StopMatch PanicAction = iota
	// Crash stops every match, runs every OnStop, then re-panics with the
	// recovered value from Run.
	Crash
)

// Panic describes a panic recovered from a match's Update or OnStop.
type Panic struct {
	// Match is the match whose goroutine panicked.
	Match *Match
	// Value is the value passed to panic.
	Value any
	// Stack is the goroutine stack trace captured at recovery.
	Stack []byte
	// Tick is the match's 1-based tick in which Update panicked, or 0 when
	// OnStop panicked.
	Tick uint64
	// Time is when the panic was recovered.
	Time time.Time
}

// Report builds a crashreport.CrashReport from p, so a server-side panic file
// has the same JSON schema as a client crash report. The match's Name, when
// set, is recorded as the "match" metadata key.
func (p Panic) Report() crashreport.CrashReport {
	rep := crashreport.CrashReport{
		Timestamp: p.Time,
		Panic:     p.Value,
		Message:   fmt.Sprint(p.Value),
		Stack:     string(p.Stack),
		Step:      p.Tick,
	}
	if p.Match != nil && p.Match.Name != "" {
		rep.Metadata = map[string]string{"match": p.Match.Name}
	}
	return rep
}

// panicState carries the value to re-panic from Run once every match has
// stopped after a Crash action. The first value wins.
type panicState struct {
	mu    sync.Mutex
	value any
}

func (p *panicState) set(v any) {
	p.mu.Lock()
	if p.value == nil {
		p.value = v
	}
	p.mu.Unlock()
}

func (p *panicState) crashValue() any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.value
}
