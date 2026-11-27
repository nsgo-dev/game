package playtest

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// FileName is the default playtest configuration file name at a project's root.
const FileName = "playtest.yaml"

// CurrentVersion is the playtest.yaml schema version this package reads and writes.
const CurrentVersion = 1

// SchemaURL is where the JSON Schema of version 1 playtest files (playtest.yaml) is published.
const SchemaURL = "https://game.nsgo.dev/schemas/playtest.v1.json"

// SchemaComment is the yaml-language-server header line EncodeConfig writes at the top of playtest.yaml.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

const (
	defaultStepRate    = 60
	defaultMaxDuration = 60 * time.Second
)

// Measure kinds supported by Measure.Kind.
const (
	MeasureEvent   = "event"
	MeasureCounter = "counter"
	MeasureTimer   = "timer"
	MeasureOutcome = "outcome"
	MeasureRatio   = "ratio"
	MeasurePerf    = "perf"
)

// Aggregation modes supported by Measure.Aggregate.
const (
	AggCount  = "count"
	AggSum    = "sum"
	AggMean   = "mean"
	AggMedian = "median"
	AggMin    = "min"
	AggMax    = "max"
	AggP95    = "p95"
	AggP99    = "p99"
	AggRate   = "rate"
)

// Goal evaluation statuses reported in MetricReport.Status.
const (
	StatusMet    = "met"
	StatusMissed = "missed"
	StatusInfo   = "info"
)

var (
	validMeasureKinds = []string{MeasureEvent, MeasureCounter, MeasureTimer, MeasureOutcome, MeasureRatio, MeasurePerf}
	validAggregates   = []string{AggCount, AggSum, AggMean, AggMedian, AggMin, AggMax, AggP95, AggP99, AggRate}
	validPerfKeys     = []string{"frame_ms", "update_ms", "draw_ms", "p50_frame_ms", "p95_frame_ms", "p99_frame_ms", "max_frame_ms", "allocs_per_frame"}
)

// Config is the root of a versioned playtest.yaml document describing the scope, seeded runs, bot profile and measured metrics with goals.
type Config struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Scope identifies the project and scene this playtest exercises.
	Scope Scope `yaml:"scope" schema:"required"`
	// Runs configures the batch of headless seeded runs.
	Runs Runs `yaml:"runs" schema:"required"`
	// Bot selects the bot profile and configures its behavior parameters.
	Bot BotConfig `yaml:"bot" schema:"required"`
	// Metrics declares each measured metric, how it is aggregated, and its optional goal.
	Metrics []Metric `yaml:"metrics,omitempty"`
}

// Scope identifies the project, scene and optional template a playtest targets.
type Scope struct {
	// Project is the project name or relative path to the project root.
	Project string `yaml:"project,omitempty"`
	// Scene names the scene or level the playtest exercises, such as "level_01".
	Scene string `yaml:"scene" schema:"required,minLength=1"`
	// Template names the template the playtest originated from, such as "platformer2d".
	Template string `yaml:"template,omitempty"`
}

// Runs configures how many seeded runs execute, their seeds, fixed step rate, speed and simulated time limit.
type Runs struct {
	// Count is the number of seeded runs to execute; when 0 and Seeds is non-empty, it defaults to len(Seeds).
	Count int `yaml:"count,omitempty" schema:"minimum=0"`
	// Seeds lists explicit per-run random seeds; when fewer than Count, additional seeds are derived sequentially from SeedStart.
	Seeds []uint64 `yaml:"seeds,omitempty"`
	// SeedStart is the starting seed used when generating sequential seeds (defaults to 1 when 0).
	SeedStart uint64 `yaml:"seedStart,omitempty"`
	// Rate is the fixed simulation step rate in steps per second (defaults to 60 when 0).
	Rate int `yaml:"rate,omitempty" schema:"minimum=0"`
	// Speed is the simulation speed multiplier metadata, such as 1 or 8 for 8x speed (defaults to 1 when 0).
	Speed float64 `yaml:"speed,omitempty" schema:"minimum=0"`
	// MaxDuration is the simulated time limit per run before timing out, such as "60s" (defaults to 60s when 0).
	MaxDuration time.Duration `yaml:"maxDuration,omitempty"`
}

// BotConfig configures the bot profile and parameters driving input during playtest runs.
type BotConfig struct {
	// Profile names the bot profile, such as "platformer-runner", "topdown-explorer", "shooter", "puzzle-solver" or "card-ai".
	Profile string `yaml:"profile" schema:"required,minLength=1"`
	// Strategy is an optional one-line summary of the bot's behavior for reports.
	Strategy string `yaml:"strategy,omitempty"`
	// ReactionMin is the minimum human reaction jitter duration, such as "120ms".
	ReactionMin time.Duration `yaml:"reactionMin,omitempty"`
	// ReactionMax is the maximum human reaction jitter duration, such as "220ms".
	ReactionMax time.Duration `yaml:"reactionMax,omitempty"`
	// Params holds optional profile-specific scalar parameters (booleans, finite numbers or strings).
	Params map[string]any `yaml:"params,omitempty"`
}

// Metric defines one measured playtest metric, how it is computed across runs, and its optional goal.
type Metric struct {
	// Name uniquely identifies the metric, such as "finished", "median_time" or "frame_time".
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Label is an optional human-readable display name, such as "Finished".
	Label string `yaml:"label,omitempty"`
	// Unit is an optional display unit, such as "s", "ms", "%" or "runs".
	Unit string `yaml:"unit,omitempty"`
	// Measure specifies how per-run telemetry is read and aggregated across runs.
	Measure Measure `yaml:"measure" schema:"required"`
	// Goal is the optional target threshold or range this metric is checked against.
	Goal *Goal `yaml:"goal,omitempty"`
}

// Measure specifies which telemetry signal a Metric reads and how per-run values combine.
type Measure struct {
	// Kind selects the telemetry source: "event", "counter", "timer", "outcome", "ratio" or "perf".
	Kind string `yaml:"kind" schema:"required,enum=event|counter|timer|outcome|ratio|perf"`
	// Key names the event kind, counter name, timer name, outcome name or perf field to read.
	Key string `yaml:"key" schema:"required,minLength=1"`
	// DenomKey names the denominator counter for "ratio" metrics (required when Kind is "ratio").
	DenomKey string `yaml:"denomKey,omitempty"`
	// Aggregate chooses how per-run values combine: "count", "sum", "mean", "median", "min", "max", "p95", "p99" or "rate".
	Aggregate string `yaml:"aggregate,omitempty" schema:"enum=count|sum|mean|median|min|max|p95|p99|rate"`
	// HeatMap records positioned event coordinates for this metric in the report's heatMaps section.
	HeatMap bool `yaml:"heatMap,omitempty"`
}

// Goal defines the target range or threshold for a Metric and the file the goal comes from.
type Goal struct {
	// Summary is an optional human-readable description of the goal, such as "most first-timers clear floor 1".
	Summary string `yaml:"summary,omitempty"`
	// Source names the file this goal comes from, such as "brief.yaml", "game.yaml" or "playtest.yaml".
	Source string `yaml:"source" schema:"required,minLength=1"`
	// Min is the inclusive lower bound (value >= Min).
	Min *float64 `yaml:"min,omitempty"`
	// Max is the inclusive upper bound (value <= Max).
	Max *float64 `yaml:"max,omitempty"`
	// Equal requires value == Equal.
	Equal *float64 `yaml:"equal,omitempty"`
}

// EffectiveRate returns the fixed step rate in steps per second, defaulting to 60 when unset.
func (r Runs) EffectiveRate() int {
	if r.Rate > 0 {
		return r.Rate
	}
	return defaultStepRate
}

// EffectiveSpeed returns the speed multiplier, defaulting to 1 when unset.
func (r Runs) EffectiveSpeed() float64 {
	if r.Speed > 0 {
		return r.Speed
	}
	return 1
}

// EffectiveMaxDuration returns the per-run simulated time limit, defaulting to 60s when unset.
func (r Runs) EffectiveMaxDuration() time.Duration {
	if r.MaxDuration > 0 {
		return r.MaxDuration
	}
	return defaultMaxDuration
}

// MaxSteps returns the number of fixed steps corresponding to EffectiveMaxDuration at EffectiveRate.
func (r Runs) MaxSteps() int {
	raw := math.Ceil(r.EffectiveMaxDuration().Seconds() * float64(r.EffectiveRate()))
	if math.IsNaN(raw) || raw < 1 {
		return 1
	}
	if raw >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int(raw)
}

// ResolveSeeds returns the deterministic list of per-run seeds configured by r.
func (r Runs) ResolveSeeds() []uint64 {
	count := r.Count
	if count <= 0 {
		count = len(r.Seeds)
	}
	if count <= 0 {
		return nil
	}
	out := make([]uint64, 0, count)
	seen := make(map[uint64]struct{}, count)
	for i := 0; i < len(r.Seeds) && len(out) < count; i++ {
		out = append(out, r.Seeds[i])
		seen[r.Seeds[i]] = struct{}{}
	}
	next := r.SeedStart
	if next == 0 {
		next = 1
	}
	if len(out) > 0 && next <= out[len(out)-1] {
		next = out[len(out)-1] + 1
	}
	for len(out) < count {
		for _, dup := seen[next]; dup; _, dup = seen[next] {
			next++
		}
		out = append(out, next)
		seen[next] = struct{}{}
		next++
	}
	return out
}

// EffectiveAggregate returns m.Aggregate when set, or the default aggregate for m.Kind.
func (m Measure) EffectiveAggregate() string {
	if m.Aggregate != "" {
		return m.Aggregate
	}
	switch m.Kind {
	case MeasureOutcome:
		return AggCount
	case MeasureTimer, MeasurePerf:
		return AggMedian
	case MeasureRatio:
		return AggMean
	default:
		return AggSum
	}
}

// Evaluate checks whether value satisfies g and returns StatusMet, StatusMissed, or StatusInfo (when g is nil).
func (g *Goal) Evaluate(value float64) string {
	if g == nil {
		return StatusInfo
	}
	if !isFinite(value) {
		return StatusMissed
	}
	const eps = 1e-9
	if g.Equal != nil {
		if math.Abs(value-*g.Equal) <= eps {
			return StatusMet
		}
		return StatusMissed
	}
	if g.Min != nil && value < *g.Min-eps {
		return StatusMissed
	}
	if g.Max != nil && value > *g.Max+eps {
		return StatusMissed
	}
	return StatusMet
}

// Describe returns a concise human-readable description of g's threshold or range.
func (g *Goal) Describe(unit string) string {
	if g == nil {
		return ""
	}
	fmtNum := func(v float64) string {
		s := strconv.FormatFloat(cleanZero(v), 'f', -1, 64)
		if unit != "" {
			if unit == "%" {
				return s + "%"
			}
			return s + " " + unit
		}
		return s
	}
	var bound string
	switch {
	case g.Equal != nil:
		bound = "= " + fmtNum(*g.Equal)
	case g.Min != nil && g.Max != nil:
		bound = strconv.FormatFloat(cleanZero(*g.Min), 'f', -1, 64) + "–" + fmtNum(*g.Max)
	case g.Min != nil:
		bound = ">= " + fmtNum(*g.Min)
	case g.Max != nil:
		bound = "<= " + fmtNum(*g.Max)
	}
	if g.Summary != "" && bound != "" {
		return g.Summary + " · " + bound
	}
	if g.Summary != "" {
		return g.Summary
	}
	return bound
}

// Validate checks that c uses CurrentVersion and that its scope, runs, bot and metrics are valid.
func (c *Config) Validate() error {
	if c == nil {
		return loaderr.Errorf("", "provide a playtest configuration", "playtest config is nil")
	}
	if c.Version != CurrentVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("unsupported version %d", c.Version),
			Expected: fmt.Sprint(CurrentVersion),
			Fix:      fmt.Sprintf("set version to %d", CurrentVersion),
		}
	}
	if strings.TrimSpace(c.Scope.Scene) == "" {
		return loaderr.Errorf("scope.scene", "set scope.scene to the scene or level name to playtest", "scope.scene is required")
	}
	if c.Runs.Count < 0 {
		return loaderr.Errorf("runs.count", "set runs.count to 0 or more", "runs.count %d is negative", c.Runs.Count)
	}
	if c.Runs.Count == 0 && len(c.Runs.Seeds) == 0 {
		return loaderr.Errorf("runs.count", "set runs.count to at least 1 or list explicit runs.seeds", "runs must configure at least one run")
	}
	if c.Runs.Rate < 0 {
		return loaderr.Errorf("runs.rate", "set runs.rate to a positive step rate or omit it for 60", "runs.rate %d is negative", c.Runs.Rate)
	}
	if !isFinite(c.Runs.Speed) || c.Runs.Speed < 0 {
		return loaderr.Errorf("runs.speed", "set runs.speed to a finite non-negative multiplier", "runs.speed %v is invalid", c.Runs.Speed)
	}
	if c.Runs.MaxDuration < 0 {
		return loaderr.Errorf("runs.maxDuration", "set runs.maxDuration to a non-negative duration such as 60s", "runs.maxDuration %v is negative", c.Runs.MaxDuration)
	}
	if err := validateBot("bot", c.Bot); err != nil {
		return err
	}
	seenMetrics := make(map[string]struct{}, len(c.Metrics))
	for i, m := range c.Metrics {
		at := loaderr.Path("metrics", i)
		if err := validateMetric(at, m); err != nil {
			return err
		}
		if _, dup := seenMetrics[m.Name]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give each metric a unique name", "duplicate metric name %q", m.Name)
		}
		seenMetrics[m.Name] = struct{}{}
	}
	return nil
}

// ValidateMetric checks that m is a valid Metric definition, as used by Config.Validate and playtest_define.
func ValidateMetric(m Metric) error {
	return validateMetric("metric", m)
}

func validateBot(prefix string, b BotConfig) error {
	if strings.TrimSpace(b.Profile) == "" {
		return loaderr.Errorf(loaderr.Join(prefix, "profile"), "set bot.profile to a registered bot profile such as platformer-runner", "bot.profile is required")
	}
	if b.ReactionMin < 0 {
		return loaderr.Errorf(loaderr.Join(prefix, "reactionMin"), "set bot.reactionMin to 0 or a positive duration such as 120ms", "bot.reactionMin %v is negative", b.ReactionMin)
	}
	if b.ReactionMax < 0 {
		return loaderr.Errorf(loaderr.Join(prefix, "reactionMax"), "set bot.reactionMax to 0 or a positive duration such as 220ms", "bot.reactionMax %v is negative", b.ReactionMax)
	}
	if b.ReactionMax > 0 && b.ReactionMax < b.ReactionMin {
		return loaderr.Errorf(loaderr.Join(prefix, "reactionMax"), "set bot.reactionMax to at least bot.reactionMin", "bot.reactionMax %v is less than bot.reactionMin %v", b.ReactionMax, b.ReactionMin)
	}
	for k, v := range b.Params {
		if k == "" {
			return loaderr.Errorf(loaderr.Join(prefix, "params"), "use non-empty parameter names", "bot.params contains an empty key")
		}
		if err := validateScalar(loaderr.Join(prefix, "params", k), v); err != nil {
			return err
		}
	}
	return nil
}

func validateScalar(field string, v any) error {
	switch x := v.(type) {
	case bool, string, int, int64, uint64:
		return nil
	case float64:
		if !isFinite(x) {
			return loaderr.Errorf(field, "use a finite number", "non-finite number %v", x)
		}
		return nil
	case float32:
		if !isFinite(float64(x)) {
			return loaderr.Errorf(field, "use a finite number", "non-finite number %v", x)
		}
		return nil
	default:
		return loaderr.Errorf(field, "use a boolean, finite number or string", "unsupported parameter value of type %T", v)
	}
}

func validateMetric(at string, m Metric) error {
	if strings.TrimSpace(m.Name) == "" {
		return loaderr.Errorf(loaderr.Join(at, "name"), "give the metric a non-empty name", "metric name is required")
	}
	if !slices.Contains(validMeasureKinds, m.Measure.Kind) {
		return loaderr.Errorf(loaderr.Join(at, "measure", "kind"), "use one of: "+strings.Join(validMeasureKinds, ", "), "unknown measure kind %q", m.Measure.Kind)
	}
	if strings.TrimSpace(m.Measure.Key) == "" {
		return loaderr.Errorf(loaderr.Join(at, "measure", "key"), "set measure.key to the signal name to read", "measure.key is required")
	}
	if m.Measure.Kind == MeasureRatio && strings.TrimSpace(m.Measure.DenomKey) == "" {
		return loaderr.Errorf(loaderr.Join(at, "measure", "denomKey"), "set measure.denomKey to the denominator counter name for a ratio metric", "ratio metric requires measure.denomKey")
	}
	if m.Measure.Kind != MeasureRatio && m.Measure.DenomKey != "" {
		return loaderr.Errorf(loaderr.Join(at, "measure", "denomKey"), "remove measure.denomKey unless measure.kind is ratio", "measure.denomKey is only valid for ratio metrics")
	}
	if m.Measure.Kind == MeasurePerf && !slices.Contains(validPerfKeys, m.Measure.Key) {
		return loaderr.Errorf(loaderr.Join(at, "measure", "key"), "use one of: "+strings.Join(validPerfKeys, ", "), "unknown perf measure key %q", m.Measure.Key)
	}
	if m.Measure.Aggregate != "" && !slices.Contains(validAggregates, m.Measure.Aggregate) {
		return loaderr.Errorf(loaderr.Join(at, "measure", "aggregate"), "use one of: "+strings.Join(validAggregates, ", "), "unknown measure aggregate %q", m.Measure.Aggregate)
	}
	if m.Measure.HeatMap && m.Measure.Kind != MeasureEvent {
		return loaderr.Errorf(loaderr.Join(at, "measure", "heatMap"), "set heatMap only on event metrics", "heatMap is only supported for event metrics")
	}
	if m.Goal != nil {
		if err := validateGoal(loaderr.Join(at, "goal"), m.Goal); err != nil {
			return err
		}
	}
	return nil
}

func validateGoal(at string, g *Goal) error {
	if strings.TrimSpace(g.Source) == "" {
		return loaderr.Errorf(loaderr.Join(at, "source"), "set goal.source to the file the goal comes from, such as brief.yaml, game.yaml or playtest.yaml", "goal.source is required")
	}
	if g.Min == nil && g.Max == nil && g.Equal == nil {
		return loaderr.Errorf(at, "set at least one of min, max or equal on the goal", "goal must specify min, max or equal")
	}
	if g.Equal != nil && (g.Min != nil || g.Max != nil) {
		return loaderr.Errorf(at, "use either equal or min/max on a goal, not both", "goal cannot combine equal with min or max")
	}
	if g.Min != nil && !isFinite(*g.Min) {
		return loaderr.Errorf(loaderr.Join(at, "min"), "use a finite number", "goal.min is not finite")
	}
	if g.Max != nil && !isFinite(*g.Max) {
		return loaderr.Errorf(loaderr.Join(at, "max"), "use a finite number", "goal.max is not finite")
	}
	if g.Equal != nil && !isFinite(*g.Equal) {
		return loaderr.Errorf(loaderr.Join(at, "equal"), "use a finite number", "goal.equal is not finite")
	}
	if g.Min != nil && g.Max != nil && *g.Min > *g.Max {
		return loaderr.Errorf(loaderr.Join(at, "max"), "set goal.max to at least goal.min", "goal.max %v is less than goal.min %v", *g.Max, *g.Min)
	}
	return nil
}

// ParseConfig decodes and validates a playtest.yaml document, refusing unknown fields, missing or unsupported versions, duplicate keys and trailing YAML documents.
func ParseConfig(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", "write a versioned playtest.yaml document", "playtest config is empty"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("playtest: decode: %w", err), "", data, "check the YAML syntax and field names in playtest.yaml")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep one YAML document per playtest.yaml file", "unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("playtest: decode: %w", err), "", data, "remove trailing YAML content after playtest.yaml")
	}
	normalizeConfig(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &cfg, nil
}

func normalizeConfig(c *Config) {
	c.Runs.Speed = cleanZero(c.Runs.Speed)
	for k, v := range c.Bot.Params {
		if f, ok := v.(float64); ok {
			c.Bot.Params[k] = cleanZero(f)
		}
	}
	for i := range c.Metrics {
		if g := c.Metrics[i].Goal; g != nil {
			normalizeGoal(g)
		}
	}
}

func normalizeGoal(g *Goal) {
	if g == nil {
		return
	}
	if g.Min != nil {
		v := cleanZero(*g.Min)
		g.Min = &v
	}
	if g.Max != nil {
		v := cleanZero(*g.Max)
		g.Max = &v
	}
	if g.Equal != nil {
		v := cleanZero(*g.Equal)
		g.Equal = &v
	}
}

// EncodeConfig validates c and writes it as canonical, deterministic 2-space YAML prefixed with SchemaComment.
func EncodeConfig(c *Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	cp := cloneConfig(c)
	normalizeConfig(cp)

	var buf bytes.Buffer
	buf.WriteString(SchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cp); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("playtest: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("playtest: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadConfigFile reads, decodes and validates a playtest.yaml file from disk.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("playtest: read %q: %w", path, err), path, nil, "check that the playtest config file exists and is readable")
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return cfg, nil
}

// SaveConfigFile validates, encodes and writes c to path.
func SaveConfigFile(path string, c *Config) error {
	data, err := EncodeConfig(c)
	if err != nil {
		return fmt.Errorf("playtest: write %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("playtest: write %q: %w", path, err)
	}
	return nil
}

// Clone returns an independent deep copy of c, or nil when c is nil.
func (c *Config) Clone() *Config {
	return cloneConfig(c)
}

func cloneConfig(c *Config) *Config {
	if c == nil {
		return nil
	}
	out := &Config{
		Version: c.Version,
		Scope:   c.Scope,
		Runs: Runs{
			Count:       c.Runs.Count,
			Seeds:       slices.Clone(c.Runs.Seeds),
			SeedStart:   c.Runs.SeedStart,
			Rate:        c.Runs.Rate,
			Speed:       c.Runs.Speed,
			MaxDuration: c.Runs.MaxDuration,
		},
		Bot: cloneBot(c.Bot),
	}
	if len(c.Metrics) > 0 {
		out.Metrics = make([]Metric, len(c.Metrics))
		for i, m := range c.Metrics {
			out.Metrics[i] = cloneMetric(m)
		}
	}
	return out
}

func cloneBot(b BotConfig) BotConfig {
	out := b
	if len(b.Params) > 0 {
		out.Params = make(map[string]any, len(b.Params))
		for k, v := range b.Params {
			out.Params[k] = v
		}
	} else {
		out.Params = nil
	}
	return out
}

func cloneMetric(m Metric) Metric {
	out := m
	if m.Goal != nil {
		out.Goal = cloneGoal(m.Goal)
	}
	return out
}

func cloneGoal(g *Goal) *Goal {
	if g == nil {
		return nil
	}
	cp := *g
	if g.Min != nil {
		v := *g.Min
		cp.Min = &v
	}
	if g.Max != nil {
		v := *g.Max
		cp.Max = &v
	}
	if g.Equal != nil {
		v := *g.Equal
		cp.Equal = &v
	}
	return &cp
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func cleanZero(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

// Float64 returns a pointer to v, useful for building Goal bounds in Go code.
func Float64(v float64) *float64 {
	return &v
}
