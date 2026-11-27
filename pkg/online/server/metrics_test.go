package server_test

import (
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/server"
)

type fakeInbox struct{ stats netplay.InboxStats }

func (f fakeInbox) SetInboxLimits(netplay.InboxLimits) {}
func (f fakeInbox) InboxStats() netplay.InboxStats     { return f.stats }

func scrapeMetrics(t *testing.T, h *server.Health) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func bucketValue(t *testing.T, samples map[string][]sample, match, le string) float64 {
	t.Helper()
	for _, s := range samples["nsgo_server_match_tick_seconds_bucket"] {
		if s.labels["match"] == match && s.labels["le"] == le {
			return s.value
		}
	}
	t.Fatalf("no bucket for match=%q le=%q", match, le)
	return 0
}

func labeledValue(t *testing.T, samples map[string][]sample, name string, labels map[string]string) float64 {
	t.Helper()
	for _, s := range samples[name] {
		if maps.Equal(s.labels, labels) {
			return s.value
		}
	}
	t.Fatalf("no sample %s%v", name, labels)
	return 0
}

func TestMatchTickHistogram(t *testing.T) {
	h := server.NewHealth(nil, nil)
	h.ObserveMatchTick("beta", 300*time.Microsecond)
	h.ObserveMatchTick("beta", 2*time.Millisecond)
	h.ObserveMatchTick("beta", 500*time.Millisecond) // past the last bucket
	h.ObserveMatchTick("alpha", time.Millisecond)

	body := scrapeMetrics(t, h)
	samples := parseExposition(t, body)

	for le, want := range map[string]float64{
		"0.0005": 1,
		"0.001":  1,
		"0.002":  2,
		"0.005":  2,
		"0.1":    2,
		"+Inf":   3,
	} {
		if got := bucketValue(t, samples, "beta", le); got != want {
			t.Fatalf("beta bucket le=%s = %v, want %v", le, got, want)
		}
	}
	if got := bucketValue(t, samples, "alpha", "+Inf"); got != 1 {
		t.Fatalf("alpha +Inf bucket = %v, want 1", got)
	}
	sum := labeledValue(t, samples, "nsgo_server_match_tick_seconds_sum", map[string]string{"match": "beta"})
	if want := (502300 * time.Microsecond).Seconds(); math.Abs(sum-want) > 1e-9 {
		t.Fatalf("beta _sum = %v, want %v", sum, want)
	}
	if got := labeledValue(t, samples, "nsgo_server_match_tick_seconds_count", map[string]string{"match": "beta"}); got != 3 {
		t.Fatalf("beta _count = %v, want 3", got)
	}
	if strings.Index(body, `match="alpha"`) > strings.Index(body, `match="beta"`) {
		t.Fatal("matches are not exported in name order")
	}
}

func TestRunnerRecordsMatchTicksAndOverruns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := server.NewHealth(nil, nil)
		var ticks, nextCalls int
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := &server.Runner{
			Health:    health,
			OnOverrun: health.OnOverrun(func(*server.Match, time.Duration, int) { nextCalls++ }),
			Matches: []*server.Match{{
				Rate: 20, // 50ms step; each update takes 80ms, an overrun missing 1 tick
				Update: func(time.Duration) {
					ticks++
					time.Sleep(80 * time.Millisecond)
					if ticks == 2 {
						cancel()
					}
				},
			}},
		}
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}

		body := scrapeMetrics(t, health)
		samples := parseExposition(t, body)
		labels := map[string]string{"match": "match_0"} // an unnamed match is match_<index>
		if got := labeledValue(t, samples, "nsgo_server_match_overruns_total", labels); got != 2 {
			t.Fatalf("overruns = %v, want 2", got)
		}
		if got := labeledValue(t, samples, "nsgo_server_match_missed_ticks_total", labels); got != 2 {
			t.Fatalf("missed ticks = %v, want 2", got)
		}
		if got := labeledValue(t, samples, "nsgo_server_match_tick_seconds_count", labels); got != 2 {
			t.Fatalf("tick count = %v, want 2", got)
		}
		if got := bucketValue(t, samples, "match_0", "0.1"); got != 2 {
			t.Fatalf("le=0.1 bucket = %v, want 2", got)
		}
		if nextCalls != 2 {
			t.Fatalf("wrapped OnOverrun calls = %d, want 2", nextCalls)
		}
	})
}

func TestGuardTotalsAcrossPeersLeaving(t *testing.T) {
	g := server.NewGuard(server.Limits{PacketsPerSecond: 100, BurstPackets: 100, MaxMessageSize: 64}, server.LimitThrottle)
	packet := netplay.Packet{Payload: make([]byte, 10)}
	for i := 0; i < 3; i++ {
		g.Check(1, packet)
	}
	g.Check(2, packet)
	g.Check(2, netplay.Packet{Payload: make([]byte, 100)}) // oversized: dropped and a violation
	if err := g.ReserveReliable(1, 5); err != nil {
		t.Fatal(err)
	}
	want := server.PeerStats{Packets: 4, Bytes: 40, Dropped: 1, Violations: 1, PendingBytes: 5}
	if got := g.Totals(); got != want {
		t.Fatalf("totals = %+v, want %+v", got, want)
	}

	g.Reset(1)
	want.PendingBytes = 0 // a forgotten peer keeps its counters but not its pending bytes
	if got := g.Totals(); got != want {
		t.Fatalf("totals after Reset = %+v, want %+v", got, want)
	}
	if got := g.Stats(1); got != (server.PeerStats{}) {
		t.Fatalf("reset peer stats = %+v, want zero", got)
	}
	g.Reset(2)
	if got := g.Totals(); got != want {
		t.Fatalf("totals after both left = %+v, want %+v", got, want)
	}
}

func TestHealthCollectors(t *testing.T) {
	health := server.NewHealth(nil, nil)
	g := server.NewGuard(server.Limits{}, server.LimitThrottle)
	g.Check(1, netplay.Packet{Payload: []byte("hello")})
	health.AddCollector(server.GuardCollector(g))
	health.AddCollector(server.InboxCollector("loop", fakeInbox{netplay.InboxStats{
		UnreliableDropped: 2, ReliableRefused: 3, ReliableWarnings: 1,
	}}))
	health.AddCollector(func(w io.Writer) {
		fmt.Fprintf(w, "# HELP game_things Things the game counts.\n# TYPE game_things gauge\ngame_things 7\n")
	})

	body := scrapeMetrics(t, health)
	samples := parseExposition(t, body)
	if got := labeledValue(t, samples, "nsgo_server_guard_packets_total", nil); got != 1 {
		t.Fatalf("guard packets = %v, want 1", got)
	}
	if got := labeledValue(t, samples, "nsgo_server_guard_bytes_total", nil); got != 5 {
		t.Fatalf("guard bytes = %v, want 5", got)
	}
	loop := map[string]string{"transport": "loop"}
	if got := labeledValue(t, samples, "nsgo_net_inbox_unreliable_dropped_total", loop); got != 2 {
		t.Fatalf("inbox unreliable dropped = %v, want 2", got)
	}
	if got := labeledValue(t, samples, "nsgo_net_inbox_reliable_refused_total", loop); got != 3 {
		t.Fatalf("inbox reliable refused = %v, want 3", got)
	}
	if got := labeledValue(t, samples, "nsgo_net_inbox_reliable_warnings_total", loop); got != 1 {
		t.Fatalf("inbox reliable warnings = %v, want 1", got)
	}
	if got := labeledValue(t, samples, "game_things", nil); got != 7 {
		t.Fatalf("game_things = %v, want 7", got)
	}
	if strings.Index(body, "game_things") < strings.Index(body, "nsgo_server_match_tick_seconds") {
		t.Fatal("collector output is not appended after the built-in series")
	}
}

func TestMetricsLabelEscaping(t *testing.T) {
	h := server.NewHealth(nil, nil)
	name := "a\"b\\c\nd"
	h.ObserveMatchTick(name, time.Millisecond)
	body := scrapeMetrics(t, h)
	if !strings.Contains(body, `match="a\"b\\c\nd"`) {
		t.Fatalf("match name is not escaped in:\n%s", body)
	}
	samples := parseExposition(t, body) // the strict parser round-trips the escapes
	if got := labeledValue(t, samples, "nsgo_server_match_tick_seconds_count", map[string]string{"match": name}); got != 1 {
		t.Fatalf("escaped match count = %v, want 1", got)
	}
}

// sample is one parsed exposition line.
type sample struct {
	name   string
	labels map[string]string
	value  float64
}

// parseExposition parses body as the Prometheus text exposition format,
// strictly: every line is a HELP or TYPE comment naming a valid metric or a
// sample with a parseable value and well-formed, escaped labels; every sample
// belongs to a TYPE'd family; histogram buckets are cumulative per label set
// and their +Inf bucket equals the family's count.
func parseExposition(t *testing.T, body string) map[string][]sample {
	t.Helper()
	if !strings.HasSuffix(body, "\n") {
		t.Fatal("output does not end with a newline")
	}
	types := map[string]string{}
	samples := map[string][]sample{}
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if line == "" {
			t.Fatal("empty line in output")
		}
		if strings.HasPrefix(line, "# ") {
			fields := strings.SplitN(line, " ", 4)
			if len(fields) < 4 || (fields[1] != "HELP" && fields[1] != "TYPE") {
				t.Fatalf("malformed comment line %q", line)
			}
			if !validMetricName(fields[2]) {
				t.Fatalf("invalid metric name in %q", line)
			}
			if fields[1] == "TYPE" {
				switch fields[3] {
				case "counter", "gauge", "histogram", "summary", "untyped":
				default:
					t.Fatalf("unknown metric type in %q", line)
				}
				types[fields[2]] = fields[3]
			}
			continue
		}
		s := parseSample(t, line)
		base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s.name, "_bucket"), "_sum"), "_count")
		if _, ok := types[s.name]; !ok {
			if _, ok := types[base]; !ok {
				t.Fatalf("sample %q has no TYPE", s.name)
			}
		}
		samples[s.name] = append(samples[s.name], s)
	}
	for name, typ := range types {
		if typ == "histogram" {
			checkHistogram(t, samples, name)
		}
	}
	return samples
}

// checkHistogram verifies one histogram family's buckets, sum and count.
func checkHistogram(t *testing.T, samples map[string][]sample, name string) {
	t.Helper()
	groups := map[string][]sample{}
	for _, s := range samples[name+"_bucket"] {
		if _, ok := s.labels["le"]; !ok {
			t.Fatalf("%s bucket without le: %v", name, s.labels)
		}
		key := labelSetKey(s.labels, "le")
		groups[key] = append(groups[key], s)
	}
	for _, bs := range groups {
		slices.SortFunc(bs, func(a, b sample) int {
			return compareLE(a.labels["le"], b.labels["le"])
		})
		var prev float64
		for _, s := range bs {
			if s.value < prev {
				t.Fatalf("%s buckets are not cumulative", name)
			}
			prev = s.value
		}
		last := bs[len(bs)-1]
		if last.labels["le"] != "+Inf" {
			t.Fatalf("%s has no +Inf bucket", name)
		}
		labels := maps.Clone(last.labels)
		delete(labels, "le")
		if got := labeledValue(t, samples, name+"_count", labels); got != last.value {
			t.Fatalf("%s_count = %v, +Inf bucket = %v", name, got, last.value)
		}
		labeledValue(t, samples, name+"_sum", labels)
	}
}

// labelSetKey renders a label set, minus drop, for grouping.
func labelSetKey(labels map[string]string, drop string) string {
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		if k != drop {
			pairs = append(pairs, k+"="+v)
		}
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

// compareLE orders bucket bounds numerically, +Inf last.
func compareLE(a, b string) int {
	fa, fb := leValue(a), leValue(b)
	switch {
	case fa < fb:
		return -1
	case fa > fb:
		return 1
	}
	return 0
}

func leValue(le string) float64 {
	if le == "+Inf" {
		return math.Inf(1)
	}
	v, err := strconv.ParseFloat(le, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// parseSample parses one sample line: a metric name, optional labels and a
// value.
func parseSample(t *testing.T, line string) sample {
	t.Helper()
	var s sample
	i := strings.IndexAny(line, "{ ")
	if i < 0 {
		t.Fatalf("malformed sample %q", line)
	}
	s.name = line[:i]
	rest := line[i:]
	if !validMetricName(s.name) {
		t.Fatalf("invalid metric name in %q", line)
	}
	if strings.HasPrefix(rest, "{") {
		end := closingBrace(t, rest)
		s.labels = parseLabels(t, rest[1:end])
		rest = rest[end+1:]
	}
	if !strings.HasPrefix(rest, " ") || strings.TrimSpace(rest) == "" {
		t.Fatalf("sample %q has no value", line)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	if err != nil {
		t.Fatalf("bad value in %q: %v", line, err)
	}
	s.value = v
	return s
}

// closingBrace finds the } that ends the label set at the start of s,
// skipping quoted, escaped characters.
func closingBrace(t *testing.T, s string) int {
	t.Helper()
	quoted, escaped := false, false
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == '}' && !quoted:
			return i
		}
	}
	t.Fatalf("unterminated label set in %q", s)
	return -1
}

// parseLabels parses k="v" pairs, allowing any character inside quotes and
// accepting only the \\, \" and \n escapes.
func parseLabels(t *testing.T, s string) map[string]string {
	t.Helper()
	labels := map[string]string{}
	for s != "" {
		i := strings.IndexByte(s, '=')
		if i < 1 || !validLabelName(s[:i]) || i+1 >= len(s) || s[i+1] != '"' {
			t.Fatalf("malformed label in %q", s)
		}
		name, rest := s[:i], s[i+2:]
		var value strings.Builder
		for {
			j := strings.IndexAny(rest, `"\`)
			if j < 0 {
				t.Fatalf("unterminated label value in %q", s)
			}
			value.WriteString(rest[:j])
			if rest[j] == '"' {
				rest = rest[j+1:]
				break
			}
			if j+1 >= len(rest) {
				t.Fatalf("unterminated escape in %q", s)
			}
			switch rest[j+1] {
			case '\\':
				value.WriteByte('\\')
			case '"':
				value.WriteByte('"')
			case 'n':
				value.WriteByte('\n')
			default:
				t.Fatalf("invalid escape \\%c in %q", rest[j+1], s)
			}
			rest = rest[j+2:]
		}
		labels[name] = value.String()
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, ",") {
			t.Fatalf("missing comma after label in %q", s)
		}
		s = rest[1:]
	}
	return labels
}

func validMetricName(s string) bool {
	if s == "" || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_:", rune(s[0])) {
		return false
	}
	return strings.IndexFunc(s[1:], func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_:", r)
	}) < 0
}

func validLabelName(s string) bool {
	if s == "" || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_", rune(s[0])) {
		return false
	}
	return strings.IndexFunc(s[1:], func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", r)
	}) < 0
}
