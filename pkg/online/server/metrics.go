package server

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// tickBuckets bounds the nsgo_server_match_tick_seconds histogram, in seconds,
// from half a millisecond to a tick that took 100 ms.
var tickBuckets = [...]float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1}

// matchMetrics accumulates one match's tick histogram and overrun counters.
type matchMetrics struct {
	buckets  [len(tickBuckets)]uint64
	count    uint64
	sum      float64 // seconds
	overruns uint64
	missed   uint64
}

// ObserveMatchTick records one tick of the named match in the
// nsgo_server_match_tick_seconds histogram. A Runner records every tick
// itself when its Health field is set; a game with its own loop may call
// ObserveMatchTick directly.
func (h *Health) ObserveMatchTick(match string, d time.Duration) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	mm := h.match(match)
	sec := d.Seconds()
	for i, le := range tickBuckets {
		if sec <= le {
			mm.buckets[i]++
			break
		}
	}
	mm.count++
	mm.sum += sec
}

// OnOverrun returns a Runner.OnOverrun that counts each overrun and its missed
// ticks per match in nsgo_server_match_overruns_total and
// nsgo_server_match_missed_ticks_total, then calls next when next is set.
func (h *Health) OnOverrun(next func(*Match, time.Duration, int)) func(*Match, time.Duration, int) {
	return func(m *Match, lag time.Duration, missed int) {
		if h != nil {
			name := ""
			if m != nil {
				name = m.Name
			}
			h.mu.Lock()
			mm := h.match(name)
			mm.overruns++
			if missed > 0 {
				mm.missed += uint64(missed)
			}
			h.mu.Unlock()
		}
		if next != nil {
			next(m, lag, missed)
		}
	}
}

// AddCollector appends c to /metrics: every scrape calls c after the built-in
// series. c writes complete series in the Prometheus text format and must be
// safe to call from the HTTP handler while the server runs; GuardCollector
// and InboxCollector are ready-made collectors, and a game may append its own
// series the same way.
func (h *Health) AddCollector(c func(w io.Writer)) {
	if h == nil || c == nil {
		return
	}
	h.mu.Lock()
	h.collectors = append(h.collectors, c)
	h.mu.Unlock()
}

// GuardCollector returns a collector for Health.AddCollector that exports the
// guard's totals as nsgo_server_guard_packets_total,
// nsgo_server_guard_bytes_total, nsgo_server_guard_dropped_total and
// nsgo_server_guard_violations_total.
func GuardCollector(g *Guard) func(io.Writer) {
	return func(w io.Writer) {
		t := g.Totals()
		fmt.Fprintf(w, "# HELP nsgo_server_guard_packets_total Packets the guard admitted.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_guard_packets_total counter\nnsgo_server_guard_packets_total %d\n", t.Packets)
		fmt.Fprintf(w, "# HELP nsgo_server_guard_bytes_total Payload bytes the guard admitted.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_guard_bytes_total counter\nnsgo_server_guard_bytes_total %d\n", t.Bytes)
		fmt.Fprintf(w, "# HELP nsgo_server_guard_dropped_total Packets the guard dropped or refused.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_guard_dropped_total counter\nnsgo_server_guard_dropped_total %d\n", t.Dropped)
		fmt.Fprintf(w, "# HELP nsgo_server_guard_violations_total Packets that broke a limit or failed validation.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_guard_violations_total counter\nnsgo_server_guard_violations_total %d\n", t.Violations)
	}
}

// InboxCollector returns a collector for Health.AddCollector that exports the
// transport's inbox counters as nsgo_net_inbox_unreliable_dropped_total,
// nsgo_net_inbox_reliable_refused_total and
// nsgo_net_inbox_reliable_warnings_total, each labeled transport="name".
func InboxCollector(name string, r netplay.InboxReporter) func(io.Writer) {
	return func(w io.Writer) {
		if r == nil {
			return
		}
		s := r.InboxStats()
		label := `transport="` + escapeLabel(name) + `"`
		fmt.Fprintf(w, "# HELP nsgo_net_inbox_unreliable_dropped_total Unreliable packets dropped over the inbox cap.\n")
		fmt.Fprintf(w, "# TYPE nsgo_net_inbox_unreliable_dropped_total counter\nnsgo_net_inbox_unreliable_dropped_total{%s} %d\n", label, s.UnreliableDropped)
		fmt.Fprintf(w, "# HELP nsgo_net_inbox_reliable_refused_total Reliable packets refused over the inbox cap.\n")
		fmt.Fprintf(w, "# TYPE nsgo_net_inbox_reliable_refused_total counter\nnsgo_net_inbox_reliable_refused_total{%s} %d\n", label, s.ReliableRefused)
		fmt.Fprintf(w, "# HELP nsgo_net_inbox_reliable_warnings_total Times the reliable backlog passed its warning threshold.\n")
		fmt.Fprintf(w, "# TYPE nsgo_net_inbox_reliable_warnings_total counter\nnsgo_net_inbox_reliable_warnings_total{%s} %d\n", label, s.ReliableWarnings)
	}
}

// match returns the accumulator for name, creating it on first use. The
// caller holds h.mu.
func (h *Health) match(name string) *matchMetrics {
	if h.matches == nil {
		h.matches = make(map[string]*matchMetrics)
	}
	mm := h.matches[name]
	if mm == nil {
		mm = &matchMetrics{}
		h.matches[name] = mm
	}
	return mm
}

// writeMatchMetrics writes the per-match histogram and overrun series, one
// match at a time in name order.
func (h *Health) writeMatchMetrics(w io.Writer) {
	fmt.Fprintf(w, "# HELP nsgo_server_match_tick_seconds Fixed-step duration of one match tick.\n")
	fmt.Fprintf(w, "# TYPE nsgo_server_match_tick_seconds histogram\n")
	fmt.Fprintf(w, "# HELP nsgo_server_match_overruns_total Ticks that finished past the next tick's deadline.\n")
	fmt.Fprintf(w, "# TYPE nsgo_server_match_overruns_total counter\n")
	fmt.Fprintf(w, "# HELP nsgo_server_match_missed_ticks_total Whole tick intervals an overrun covered.\n")
	fmt.Fprintf(w, "# TYPE nsgo_server_match_missed_ticks_total counter\n")
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, name := range slices.Sorted(maps.Keys(h.matches)) {
		mm := h.matches[name]
		label := `match="` + escapeLabel(name) + `"`
		var cumulative uint64
		for i, le := range tickBuckets {
			cumulative += mm.buckets[i]
			fmt.Fprintf(w, "nsgo_server_match_tick_seconds_bucket{le=%s,%s} %d\n", strconv.Quote(formatFloat(le)), label, cumulative)
		}
		fmt.Fprintf(w, "nsgo_server_match_tick_seconds_bucket{le=\"+Inf\",%s} %d\n", label, mm.count)
		fmt.Fprintf(w, "nsgo_server_match_tick_seconds_sum{%s} %s\n", label, formatFloat(mm.sum))
		fmt.Fprintf(w, "nsgo_server_match_tick_seconds_count{%s} %d\n", label, mm.count)
		fmt.Fprintf(w, "nsgo_server_match_overruns_total{%s} %d\n", label, mm.overruns)
		fmt.Fprintf(w, "nsgo_server_match_missed_ticks_total{%s} %d\n", label, mm.missed)
	}
}

// writeCollectors runs the collectors added with AddCollector.
func (h *Health) writeCollectors(w io.Writer) {
	if h == nil {
		return
	}
	h.mu.Lock()
	collectors := slices.Clone(h.collectors)
	h.mu.Unlock()
	for _, c := range collectors {
		c(w)
	}
}

// formatFloat renders a metric value the way the exposition format expects:
// the shortest decimal that round-trips.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)

// escapeLabel escapes a Prometheus label value.
func escapeLabel(s string) string {
	return labelEscaper.Replace(s)
}
