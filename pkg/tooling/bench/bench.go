// Package bench keeps Go benchmark baselines in a versioned YAML text file, reads the results `go test -bench` and `nsgo test -bench` print, and reports the benchmarks that regressed past the baseline's limits. The game command's bench subcommand is built on it.
package bench

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// FormatVersion is the version written to every Baseline and the only one Decode accepts.
const FormatVersion = 1

// DefaultLimit is the regression allowed for each metric, in percent, in a Baseline made by NewBaseline.
const DefaultLimit = 25

// Metrics are one benchmark's per-operation costs: the median of the runs measured, or the stored baseline.
type Metrics struct {
	// NsPerOp is the time per operation in nanoseconds.
	NsPerOp float64
	// BytesPerOp is the memory allocated per operation in bytes.
	BytesPerOp float64
	// AllocsPerOp is the number of allocations per operation.
	AllocsPerOp float64
}

// Limits are how much each metric may grow over its baseline, in percent, before a benchmark counts as regressed.
type Limits struct {
	// NsPerOp is the allowed growth of the time per operation.
	NsPerOp float64
	// BytesPerOp is the allowed growth of the bytes allocated per operation.
	BytesPerOp float64
	// AllocsPerOp is the allowed growth of the allocations per operation.
	AllocsPerOp float64
}

// Baseline is the stored file a benchmark run is checked against: the regression limits and each benchmark's metrics, by benchmark name without its -GOMAXPROCS suffix.
type Baseline struct {
	// Version is the format version; it must equal FormatVersion.
	Version int
	// Note is free text kept with the file, such as how to update it.
	Note string
	// Limits are the allowed regressions.
	Limits Limits
	// Benchmarks are the baseline metrics by benchmark name.
	Benchmarks map[string]Metrics
}

// NewBaseline returns an empty Baseline of the current version with DefaultLimit for every metric.
func NewBaseline() *Baseline {
	return &Baseline{
		Version:    FormatVersion,
		Limits:     Limits{NsPerOp: DefaultLimit, BytesPerOp: DefaultLimit, AllocsPerOp: DefaultLimit},
		Benchmarks: map[string]Metrics{},
	}
}

// Validate checks that b has the supported version, positive finite limits, and benchmark names starting with "Benchmark" whose metrics are finite and not negative.
func (b *Baseline) Validate() error {
	if b == nil {
		return loaderr.Errorf("", "pass a non-nil *bench.Baseline to Validate", "bench: baseline is nil")
	}
	if b.Version != FormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the baseline (or regenerate with game bench -update)", FormatVersion), "bench: unsupported version %d (want %d)", b.Version, FormatVersion)
	}
	for _, l := range []struct {
		name  string
		value float64
	}{{"ns_per_op", b.Limits.NsPerOp}, {"bytes_per_op", b.Limits.BytesPerOp}, {"allocs_per_op", b.Limits.AllocsPerOp}} {
		if !(l.value > 0) || math.IsInf(l.value, 0) {
			return loaderr.Errorf("max_regression_pct."+l.name, fmt.Sprintf("set max_regression_pct.%s to a finite positive percentage such as %d", l.name, DefaultLimit), "bench: max_regression_pct %s must be a positive number, not %v", l.name, l.value)
		}
	}
	names := make([]string, 0, len(b.Benchmarks))
	for name := range b.Benchmarks {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		m := b.Benchmarks[name]
		if !strings.HasPrefix(name, "Benchmark") || strings.ContainsAny(name, " \t\r\n") {
			return loaderr.Errorf("benchmarks."+name, "use a Go benchmark function name starting with Benchmark and no whitespace", "bench: %q is not a benchmark name", name)
		}
		for _, f := range []struct {
			key string
			v   float64
		}{{"ns_per_op", m.NsPerOp}, {"bytes_per_op", m.BytesPerOp}, {"allocs_per_op", m.AllocsPerOp}} {
			if !(f.v >= 0) || math.IsInf(f.v, 0) {
				return loaderr.Errorf("benchmarks."+name+"."+f.key, "set each benchmark metric to a finite number >= 0", "bench: benchmark %s has a metric that is not a finite number of at least 0", name)
			}
		}
	}
	return nil
}

// Update sets the baseline of every benchmark in measured to its measured metrics, leaving the others as they were.
func (b *Baseline) Update(measured map[string]Metrics) {
	if b.Benchmarks == nil {
		b.Benchmarks = make(map[string]Metrics, len(measured))
	}
	for name, m := range measured {
		b.Benchmarks[name] = m
	}
}

// baselineDoc is the YAML form of a Baseline. Field order is the key order Encode writes. Limits and every metric are pointers so a missing key is refused instead of read as 0.
type baselineDoc struct {
	Version    int                    `yaml:"version"`
	Note       string                 `yaml:"note,omitempty"`
	Limits     *metricsDoc            `yaml:"max_regression_pct"`
	Benchmarks map[string]*metricsDoc `yaml:"benchmarks"`
}

// metricsDoc is the YAML form of Metrics and of Limits, which name the same three metrics.
type metricsDoc struct {
	NsPerOp     *float64 `yaml:"ns_per_op"`
	BytesPerOp  *float64 `yaml:"bytes_per_op"`
	AllocsPerOp *float64 `yaml:"allocs_per_op"`
}

// MarshalYAML writes the metrics as a one-line flow mapping with each number in plain decimal, so 3500000 is not written as 3.5e+06 and a hand-written file survives a load and save unchanged.
func (m metricsDoc) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	for _, f := range []struct {
		key   string
		value *float64
	}{{"ns_per_op", m.NsPerOp}, {"bytes_per_op", m.BytesPerOp}, {"allocs_per_op", m.AllocsPerOp}} {
		v := *f.value
		if v == 0 {
			v = 0 // negative zero reads back as 0
		}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatFloat(v, 'f', -1, 64)})
	}
	return n, nil
}

func docOf(m Metrics) *metricsDoc {
	ns, bytes, allocs := m.NsPerOp, m.BytesPerOp, m.AllocsPerOp
	return &metricsDoc{NsPerOp: &ns, BytesPerOp: &bytes, AllocsPerOp: &allocs}
}

func (m *metricsDoc) metrics(fieldPrefix, where string) (Metrics, error) {
	if m == nil {
		return Metrics{}, loaderr.Errorf(fieldPrefix, fmt.Sprintf("set %s to {ns_per_op, bytes_per_op, allocs_per_op}", fieldPrefix), "bench: decode: %s is empty", where)
	}
	switch {
	case m.NsPerOp == nil:
		return Metrics{}, loaderr.Errorf(fieldPrefix, fmt.Sprintf("add ns_per_op to %s", fieldPrefix), "bench: decode: %s has no ns_per_op", where)
	case m.BytesPerOp == nil:
		return Metrics{}, loaderr.Errorf(fieldPrefix, fmt.Sprintf("add bytes_per_op to %s", fieldPrefix), "bench: decode: %s has no bytes_per_op", where)
	case m.AllocsPerOp == nil:
		return Metrics{}, loaderr.Errorf(fieldPrefix, fmt.Sprintf("add allocs_per_op to %s", fieldPrefix), "bench: decode: %s has no allocs_per_op", where)
	}
	return Metrics{NsPerOp: *m.NsPerOp, BytesPerOp: *m.BytesPerOp, AllocsPerOp: *m.AllocsPerOp}, nil
}

// Encode validates b and writes it as deterministic YAML text: the version, the note, the limits, then one line per benchmark in name order.
//
//	version: 1
//	note: Update with go tool game bench -update.
//	max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
//	benchmarks:
//	  BenchmarkLoadAtlas: {ns_per_op: 300000, bytes_per_op: 7200, allocs_per_op: 59}
//	  BenchmarkRaycast: {ns_per_op: 15000, bytes_per_op: 0, allocs_per_op: 0}
//
// The same Baseline always encodes to the same bytes, and Decode followed by Encode reproduces a file Encode wrote byte for byte.
func (b *Baseline) Encode() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	doc := baselineDoc{
		Version:    b.Version,
		Note:       b.Note,
		Limits:     docOf(Metrics(b.Limits)),
		Benchmarks: make(map[string]*metricsDoc, len(b.Benchmarks)),
	}
	for name, m := range b.Benchmarks {
		doc.Benchmarks[name] = docOf(m)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("bench: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("bench: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode reads a Baseline from the YAML text Encode writes, including text a person has edited, and validates it. Unknown keys, a missing limit or metric, and trailing YAML documents are refused, and a file of another version is refused with an error naming that version.
func Decode(data []byte) (*Baseline, error) {
	const fixBaseline = "write the baseline again with game bench -update, or correct the YAML"
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fixBaseline, "bench: decode: empty baseline"), "", data, "")
	}
	// Check the version before the strict decode so a file of another version reports its version instead of its first unknown key.
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("bench: decode: %w", err), "", data, fixBaseline)
	}
	if probe.Version != FormatVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the baseline (or regenerate with game bench -update)", FormatVersion), "bench: unsupported version %d (want %d)", probe.Version, FormatVersion), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc baselineDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("bench: decode: %w", err), "", data, fixBaseline)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the benchmark baseline and remove any --- separator", "bench: decode: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("bench: decode: %w", err), "", data, fixBaseline)
	}
	limits, err := doc.Limits.metrics("max_regression_pct", "max_regression_pct")
	if err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	b := &Baseline{
		Version:    doc.Version,
		Note:       doc.Note,
		Limits:     Limits(limits),
		Benchmarks: make(map[string]Metrics, len(doc.Benchmarks)),
	}
	names := make([]string, 0, len(doc.Benchmarks))
	for name := range doc.Benchmarks {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		md := doc.Benchmarks[name]
		m, err := md.metrics("benchmarks."+name, "benchmark "+name)
		if err != nil {
			return nil, loaderr.Locate(err, "", data, "")
		}
		b.Benchmarks[name] = m
	}
	if err := b.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return b, nil
}

// LoadFile reads and decodes the Baseline at path. Its errors are *loaderr.Error values that name path, with the line of the problem and how to fix it.
func LoadFile(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("bench: load: %w", err), path, nil, "check the baseline path, or write a baseline with game bench -update")
	}
	b, err := Decode(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return b, nil
}

// SaveFile writes b to path as the YAML text Encode produces, creating path's directory if needed.
func SaveFile(path string, b *Baseline) error {
	data, err := b.Encode()
	if err != nil {
		return fmt.Errorf("bench: save %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("bench: save %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("bench: save %q: %w", path, err)
	}
	return nil
}

// ParseOutput reads benchmark result lines and returns the median metrics of each benchmark, by name without its trailing -GOMAXPROCS suffix.
//
// It reads the lines `go test -bench -benchmem` prints, such as "BenchmarkRaycast-8  2000000  420.5 ns/op  0 B/op  0 allocs/op", and the same lines `nsgo test -bench` prints after a ✓ mark, and skips every other line. A line without ns/op is skipped; one without B/op or allocs/op counts them as 0.
//
// A trailing -N suffix (N >= 2, as Go's testing package formats GOMAXPROCS > 1) is stripped only when every parsed benchmark line in the input ends with the same -N suffix and verbose output does not declare those raw names verbatim. Lines with different full names are never merged.
func ParseOutput(r io.Reader) (map[string]Metrics, error) {
	type series struct{ ns, bytes, allocs []float64 }
	grouped := make(map[string]*series)
	var declared map[string]struct{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 1 && line == fields[0] && strings.HasPrefix(line, "Benchmark") {
			if declared == nil {
				declared = make(map[string]struct{})
			}
			declared[line] = struct{}{}
			continue
		}
		if len(fields) == 3 && fields[0] == "===" && fields[1] == "RUN" && strings.HasPrefix(fields[2], "Benchmark") {
			if declared == nil {
				declared = make(map[string]struct{})
			}
			declared[fields[2]] = struct{}{}
			continue
		}
		if fields[0] == "✓" {
			fields = fields[1:]
		}
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		if _, err := strconv.ParseUint(fields[1], 10, 64); err != nil {
			continue
		}
		var m Metrics
		hasNs := false
		for i := 2; i+1 < len(fields); i += 2 {
			v, err := strconv.ParseFloat(fields[i], 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				continue
			}
			switch fields[i+1] {
			case "ns/op":
				m.NsPerOp, hasNs = v, true
			case "B/op":
				m.BytesPerOp = v
			case "allocs/op":
				m.AllocsPerOp = v
			}
		}
		if !hasNs {
			continue
		}
		rawName := fields[0]
		s := grouped[rawName]
		if s == nil {
			s = &series{}
			grouped[rawName] = s
		}
		s.ns = append(s.ns, m.NsPerOp)
		s.bytes = append(s.bytes, m.BytesPerOp)
		s.allocs = append(s.allocs, m.AllocsPerOp)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("bench: read results: %w", err)
	}

	strip := commonCPUSuffix(grouped, declared)
	out := make(map[string]Metrics, len(grouped))
	for rawName, s := range grouped {
		name := rawName
		if strip != "" {
			name = strings.TrimSuffix(rawName, strip)
		}
		out[name] = Metrics{NsPerOp: median(s.ns), BytesPerOp: median(s.bytes), AllocsPerOp: median(s.allocs)}
	}
	return out, nil
}

// commonCPUSuffix returns the uniform "-N" GOMAXPROCS suffix shared by every parsed benchmark name in grouped, or "" when no suffix should be stripped.
func commonCPUSuffix[T any](grouped map[string]T, declared map[string]struct{}) string {
	if len(grouped) == 0 {
		return ""
	}
	// When verbose output declared every parsed raw name verbatim, the run ran without a -GOMAXPROCS suffix (GOMAXPROCS == 1).
	if len(declared) > 0 {
		allDeclared := true
		for rawName := range grouped {
			if _, ok := declared[rawName]; !ok {
				allDeclared = false
				break
			}
		}
		if allDeclared {
			return ""
		}
	}
	common := ""
	for rawName := range grouped {
		suffix, ok := cpuSuffix(rawName)
		if !ok {
			return ""
		}
		if common == "" {
			common = suffix
		} else if suffix != common {
			return ""
		}
	}
	return common
}

// cpuSuffix returns the trailing "-N" suffix of raw when N is a decimal integer >= 2 with no leading zero, matching Go testing's benchmarkName formatting when GOMAXPROCS > 1.
func cpuSuffix(raw string) (string, bool) {
	i := strings.LastIndexByte(raw, '-')
	if i < len("Benchmark") || i == len(raw)-1 {
		return "", false
	}
	suf := raw[i+1:]
	if suf[0] == '0' {
		return "", false
	}
	for j := range len(suf) {
		if suf[j] < '0' || suf[j] > '9' {
			return "", false
		}
	}
	n, err := strconv.Atoi(suf)
	if err != nil || n < 2 {
		return "", false
	}
	return raw[i:], true
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// Result is one baseline benchmark compared with its measurement.
type Result struct {
	// Name is the benchmark's name.
	Name string
	// Missing reports that the benchmark is in the baseline but was not measured.
	Missing bool
	// Baseline and Measured are the stored and the measured metrics.
	Baseline, Measured Metrics
	// Delta is each metric's change from the baseline in percent; +Inf when the baseline is 0 and the measurement is not.
	Delta Metrics
	// Regressions describe each metric that grew past its limit.
	Regressions []string
}

// Passed reports whether the benchmark was measured and no metric grew past its limit.
func (r Result) Passed() bool { return !r.Missing && len(r.Regressions) == 0 }

// Report is a benchmark run compared with a Baseline.
type Report struct {
	// Limits are the limits the run was held to.
	Limits Limits
	// Results are the baseline's benchmarks in name order.
	Results []Result
	// New are the measured benchmarks the baseline does not hold, in name order; they neither pass nor fail.
	New []string
}

// Passed reports whether the baseline holds at least one benchmark and every one of them passed.
func (r Report) Passed() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, res := range r.Results {
		if !res.Passed() {
			return false
		}
	}
	return true
}

// Failed returns the number of baseline benchmarks that regressed or were not measured.
func (r Report) Failed() int {
	n := 0
	for _, res := range r.Results {
		if !res.Passed() {
			n++
		}
	}
	return n
}

// Evaluate compares measured with b's benchmarks: a benchmark fails when it was not measured, when a measured metric is NaN, infinite or negative, or when a metric grew past b.Limits.
func Evaluate(b *Baseline, measured map[string]Metrics) Report {
	rep := Report{Limits: b.Limits}
	names := make([]string, 0, len(b.Benchmarks))
	for name := range b.Benchmarks {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		base := b.Benchmarks[name]
		got, ok := measured[name]
		if !ok {
			rep.Results = append(rep.Results, Result{Name: name, Missing: true, Baseline: base})
			continue
		}
		res := Result{
			Name:     name,
			Baseline: base,
			Measured: got,
			Delta: Metrics{
				NsPerOp:     deltaPct(base.NsPerOp, got.NsPerOp),
				BytesPerOp:  deltaPct(base.BytesPerOp, got.BytesPerOp),
				AllocsPerOp: deltaPct(base.AllocsPerOp, got.AllocsPerOp),
			},
		}
		for _, c := range []struct {
			unit             string
			delta, limit     float64
			got, baselineVal float64
		}{
			{"ns/op", res.Delta.NsPerOp, b.Limits.NsPerOp, got.NsPerOp, base.NsPerOp},
			{"B/op", res.Delta.BytesPerOp, b.Limits.BytesPerOp, got.BytesPerOp, base.BytesPerOp},
			{"allocs/op", res.Delta.AllocsPerOp, b.Limits.AllocsPerOp, got.AllocsPerOp, base.AllocsPerOp},
		} {
			if math.IsNaN(c.got) || math.IsInf(c.got, 0) || c.got < 0 {
				// ParseOutput never yields such a value, but a caller that builds measured itself may, and a NaN delta compares as no regression.
				res.Regressions = append(res.Regressions, fmt.Sprintf("%s is not a finite, non-negative number (got %s)", c.unit, formatNum(c.got)))
				continue
			}
			if c.delta > c.limit {
				res.Regressions = append(res.Regressions, fmt.Sprintf("%s regressed by %s (got %s, baseline %s, limit +%s%%)",
					c.unit, formatPct(c.delta), formatNum(c.got), formatNum(c.baselineVal), formatNum(c.limit)))
			}
		}
		rep.Results = append(rep.Results, res)
	}
	for name := range measured {
		if _, ok := b.Benchmarks[name]; !ok {
			rep.New = append(rep.New, name)
		}
	}
	slices.Sort(rep.New)
	return rep
}

func deltaPct(base, got float64) float64 {
	if base == 0 {
		if got == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (got - base) / base * 100
}

func formatPct(p float64) string {
	if math.IsInf(p, 1) {
		return "+inf%"
	}
	return fmt.Sprintf("%+.1f%%", p)
}

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// String renders r as a table with a PASS or FAIL line per baseline benchmark, the reason under each failure, and the measured benchmarks the baseline lacks.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Benchmark baseline check (limits: ns/op +%s%%, B/op +%s%%, allocs/op +%s%%)\n",
		formatNum(r.Limits.NsPerOp), formatNum(r.Limits.BytesPerOp), formatNum(r.Limits.AllocsPerOp))
	for _, res := range r.Results {
		status := "PASS"
		if !res.Passed() {
			status = "FAIL"
		}
		if res.Missing {
			fmt.Fprintf(&b, "  [%s] %s: not measured\n", status, res.Name)
			continue
		}
		fmt.Fprintf(&b, "  [%s] %-40s ns/op: %11.1f (base %11.1f, %7s) | B/op: %9.1f (base %9.1f, %7s) | allocs/op: %7.1f (base %7.1f, %7s)\n",
			status, res.Name,
			res.Measured.NsPerOp, res.Baseline.NsPerOp, formatPct(res.Delta.NsPerOp),
			res.Measured.BytesPerOp, res.Baseline.BytesPerOp, formatPct(res.Delta.BytesPerOp),
			res.Measured.AllocsPerOp, res.Baseline.AllocsPerOp, formatPct(res.Delta.AllocsPerOp))
		for _, msg := range res.Regressions {
			fmt.Fprintf(&b, "         -> %s\n", msg)
		}
	}
	for _, name := range r.New {
		fmt.Fprintf(&b, "  [NEW ] %s: not in the baseline\n", name)
	}
	return b.String()
}
