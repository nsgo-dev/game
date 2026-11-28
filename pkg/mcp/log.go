package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/automation"
)

// ToolLogRead reads recent log lines from the Bridge's bounded slog ring buffer
// together with the last panic recorded by pkg/tooling/gamedebug.
const ToolLogRead = "game_log_read"

const (
	// DefaultLogCapacity is the default ring buffer capacity for LogBuffer.
	DefaultLogCapacity = 512
	// MaxLogCapacity is the upper bound for LogBuffer ring capacity.
	MaxLogCapacity = 16384
	// DefaultLogLineBytes is the default byte limit for each captured log message.
	DefaultLogLineBytes = 4096
	// DefaultLogReadLimit is the default number of entries returned by game_log_read when limit is omitted.
	DefaultLogReadLimit = 100
	// MaxLogReadLimit is the maximum number of entries returned by a single game_log_read call.
	MaxLogReadLimit = 1000
)

// LogConfig configures bounded slog capture and panic reporting for game_log_read.
// Set Config.Log = &mcp.LogConfig{} to install a tee slog.Handler around slog.Default() while the Bridge is attached (restoring the previous default logger on Detach). Output of the standard log package (log.Print and friends) keeps its previous destination and is not captured.
type LogConfig struct {
	// Buffer is an optional pre-created LogBuffer. When nil, the Bridge creates one using Capacity, MinLevel and MaxLineBytes.
	Buffer *LogBuffer
	// Capacity is the maximum number of log entries retained in the ring buffer when Buffer is nil (default DefaultLogCapacity, capped at MaxLogCapacity).
	Capacity int
	// MinLevel is the minimum slog.Level recorded into the ring buffer when Buffer is nil (default slog.LevelDebug).
	MinLevel slog.Leveler
	// MaxLineBytes bounds the byte length of each captured log message when Buffer is nil (default DefaultLogLineBytes).
	MaxLineBytes int
	// NoDefault prevents Attach from wrapping slog.Default() when Buffer is nil. Use Bridge.LogBuffer() to wire the handler into a custom *slog.Logger instead.
	NoDefault bool
	// InstallDefault wraps slog.Default() into Buffer on Attach even when Buffer is pre-created.
	InstallDefault bool
	// LastPanic optionally overrides gamedebug.LastPanic for reporting the last recovered panic in game_log_read.
	LastPanic func() (gamedebug.PanicRecord, bool)
}

// LogBufferOptions configures a standalone LogBuffer created with NewLogBuffer.
type LogBufferOptions struct {
	// Capacity is the maximum number of entries retained in the ring buffer (default DefaultLogCapacity, capped at MaxLogCapacity).
	Capacity int
	// MinLevel is the minimum slog.Level recorded into the ring buffer (default slog.LevelDebug).
	MinLevel slog.Leveler
	// MaxLineBytes bounds each captured message's length in bytes (default DefaultLogLineBytes).
	MaxLineBytes int
	// Next is an optional downstream slog.Handler that also receives every log record (tee).
	Next slog.Handler
}

// LogEntry is one structured log record stored in LogBuffer and returned by game_log_read.
type LogEntry struct {
	// Seq is the monotonically increasing 1-based cursor sequence assigned to this entry.
	Seq uint64 `json:"seq"`
	// Time is the UTC timestamp of the log record.
	Time time.Time `json:"time"`
	// Level is the slog level name (such as "DEBUG", "INFO", "WARN", "ERROR").
	Level string `json:"level"`
	// LevelNum is the numeric slog.Level value.
	LevelNum int `json:"level_num"`
	// Message is the log message (bounded to MaxLineBytes).
	Message string `json:"message"`
	// Attrs holds flattened structured attributes when present.
	Attrs map[string]any `json:"attrs,omitempty"`
	// Source holds "file:line" when the slog record carried a program counter.
	Source string `json:"source,omitempty"`
}

// LogReadOptions filters entries returned by LogBuffer.Read.
type LogReadOptions struct {
	// Since returns only entries with Seq > Since. When 0, returns the latest Limit matching entries; when > 0, returns the earliest Limit matching entries after Since so callers can resume without gaps.
	Since uint64
	// MinLevel filters entries to LevelNum >= int(MinLevel) when HasMinLevel is true.
	MinLevel    slog.Level
	HasMinLevel bool
	// Contains filters entries case-insensitively across Message, Source and Attrs.
	Contains string
	// Limit bounds the number of returned entries (default DefaultLogReadLimit, capped at MaxLogReadLimit).
	Limit int
}

// LogReadResult is the JSON result of game_log_read and LogBuffer.Read.
type LogReadResult struct {
	Configured bool                   `json:"configured"`
	Entries    []LogEntry             `json:"entries"`
	Returned   int                    `json:"returned"`
	NextCursor uint64                 `json:"next_cursor"`
	OldestSeq  uint64                 `json:"oldest_seq,omitempty"`
	LatestSeq  uint64                 `json:"latest_seq,omitempty"`
	Capacity   int                    `json:"capacity,omitempty"`
	Buffered   int                    `json:"buffered,omitempty"`
	Dropped    uint64                 `json:"dropped,omitempty"`
	Missed     uint64                 `json:"missed,omitempty"`
	HasMore    bool                   `json:"has_more,omitempty"`
	Truncated  bool                   `json:"truncated,omitempty"`
	LastPanic  *gamedebug.PanicRecord `json:"last_panic,omitempty"`
}

type logRing struct {
	mu           sync.RWMutex
	entries      []LogEntry
	head         int
	count        int
	nextSeq      uint64
	dropped      uint64
	minLevel     slog.Leveler
	maxLineBytes int
}

// LogBuffer is a concurrency-safe bounded ring buffer implementing slog.Handler.
// It can be used directly with slog.New or wrapped around an existing handler via Tee / LogBufferOptions.Next.
type LogBuffer struct {
	ring   *logRing
	next   slog.Handler
	attrs  []boundAttr
	groups []string
}

type boundAttr struct {
	groups []string
	attr   slog.Attr
}

// NewLogBuffer creates a concurrency-safe bounded slog ring buffer.
func NewLogBuffer(opts LogBufferOptions) *LogBuffer {
	capN := opts.Capacity
	if capN <= 0 {
		capN = DefaultLogCapacity
	}
	if capN > MaxLogCapacity {
		capN = MaxLogCapacity
	}
	maxLine := opts.MaxLineBytes
	if maxLine <= 0 {
		maxLine = DefaultLogLineBytes
	}
	minLvl := opts.MinLevel
	if minLvl == nil {
		minLvl = slog.LevelDebug
	}
	return &LogBuffer{
		ring: &logRing{
			entries:      make([]LogEntry, capN),
			nextSeq:      1,
			minLevel:     minLvl,
			maxLineBytes: maxLine,
		},
		next: opts.Next,
	}
}

// Tee returns a new LogBuffer handler sharing the same underlying ring buffer
// that also forwards every record to next.
func (b *LogBuffer) Tee(next slog.Handler) *LogBuffer {
	if b == nil {
		return nil
	}
	return &LogBuffer{
		ring:   b.ring,
		next:   next,
		attrs:  slices.Clone(b.attrs),
		groups: slices.Clone(b.groups),
	}
}

// Enabled implements slog.Handler.
func (b *LogBuffer) Enabled(ctx context.Context, level slog.Level) bool {
	if b == nil || b.ring == nil {
		return false
	}
	if level >= b.ring.minLevel.Level() {
		return true
	}
	if b.next != nil {
		return b.next.Enabled(ctx, level)
	}
	return false
}

// Handle implements slog.Handler.
func (b *LogBuffer) Handle(ctx context.Context, r slog.Record) error {
	if b == nil || b.ring == nil {
		return nil
	}
	if r.Level >= b.ring.minLevel.Level() {
		ts := r.Time
		if ts.IsZero() {
			ts = time.Now().UTC()
		} else {
			ts = ts.UTC()
		}
		var attrs map[string]any
		addAttr := func(prefix []string, a slog.Attr) {
			collectSlogAttr(&attrs, prefix, a, 0)
		}
		for _, ba := range b.attrs {
			addAttr(ba.groups, ba.attr)
		}
		r.Attrs(func(a slog.Attr) bool {
			addAttr(b.groups, a)
			return true
		})

		var source string
		if r.PC != 0 {
			frames := runtime.CallersFrames([]uintptr{r.PC})
			frame, _ := frames.Next()
			if frame.File != "" {
				source = fmt.Sprintf("%s:%d", shortSourceFile(frame.File), frame.Line)
			}
		}

		entry := LogEntry{
			Time:     ts,
			Level:    r.Level.String(),
			LevelNum: int(r.Level),
			Message:  truncateUTF8(r.Message, b.ring.maxLineBytes),
			Attrs:    attrs,
			Source:   source,
		}
		b.ring.append(entry)
	}
	if b.next != nil && b.next.Enabled(ctx, r.Level) {
		return b.next.Handle(ctx, r)
	}
	return nil
}

// WithAttrs implements slog.Handler.
func (b *LogBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	if b == nil || len(attrs) == 0 {
		return b
	}
	nextAttrs := make([]boundAttr, len(b.attrs), len(b.attrs)+len(attrs))
	copy(nextAttrs, b.attrs)
	grpCopy := slices.Clone(b.groups)
	for _, a := range attrs {
		nextAttrs = append(nextAttrs, boundAttr{groups: grpCopy, attr: a})
	}
	var nextH slog.Handler
	if b.next != nil {
		nextH = b.next.WithAttrs(attrs)
	}
	return &LogBuffer{
		ring:   b.ring,
		next:   nextH,
		attrs:  nextAttrs,
		groups: grpCopy,
	}
}

// WithGroup implements slog.Handler.
func (b *LogBuffer) WithGroup(name string) slog.Handler {
	if b == nil || name == "" {
		return b
	}
	nextGroups := make([]string, len(b.groups)+1)
	copy(nextGroups, b.groups)
	nextGroups[len(b.groups)] = name
	var nextH slog.Handler
	if b.next != nil {
		nextH = b.next.WithGroup(name)
	}
	return &LogBuffer{
		ring:   b.ring,
		next:   nextH,
		attrs:  slices.Clone(b.attrs),
		groups: nextGroups,
	}
}

func (r *logRing) append(e LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e.Seq = r.nextSeq
	r.nextSeq++
	capN := len(r.entries)
	if r.count < capN {
		idx := (r.head + r.count) % capN
		r.entries[idx] = e
		r.count++
		return
	}
	r.entries[r.head] = e
	r.head = (r.head + 1) % capN
	r.dropped++
}

// Len returns the number of log entries currently buffered.
func (b *LogBuffer) Len() int {
	if b == nil || b.ring == nil {
		return 0
	}
	b.ring.mu.RLock()
	defer b.ring.mu.RUnlock()
	return b.ring.count
}

// Cap returns the maximum capacity of the ring buffer.
func (b *LogBuffer) Cap() int {
	if b == nil || b.ring == nil {
		return 0
	}
	return len(b.ring.entries)
}

// Dropped returns the total number of oldest entries evicted because the buffer was full.
func (b *LogBuffer) Dropped() uint64 {
	if b == nil || b.ring == nil {
		return 0
	}
	b.ring.mu.RLock()
	defer b.ring.mu.RUnlock()
	return b.ring.dropped
}

// LatestSeq returns the sequence cursor of the most recently recorded entry, or 0 if none have been recorded.
func (b *LogBuffer) LatestSeq() uint64 {
	if b == nil || b.ring == nil {
		return 0
	}
	b.ring.mu.RLock()
	defer b.ring.mu.RUnlock()
	if b.ring.nextSeq <= 1 {
		return 0
	}
	return b.ring.nextSeq - 1
}

// Clear empties buffered entries while preserving the monotonic sequence cursor.
func (b *LogBuffer) Clear() {
	if b == nil || b.ring == nil {
		return
	}
	b.ring.mu.Lock()
	defer b.ring.mu.Unlock()
	clear(b.ring.entries)
	b.ring.head = 0
	b.ring.count = 0
}

// Read returns buffered log entries matching opts along with pagination and drop metadata.
func (b *LogBuffer) Read(opts LogReadOptions) LogReadResult {
	if b == nil || b.ring == nil {
		return LogReadResult{Configured: false, Entries: []LogEntry{}}
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLogReadLimit
	}
	if limit > MaxLogReadLimit {
		limit = MaxLogReadLimit
	}
	needle := strings.ToLower(opts.Contains)

	b.ring.mu.RLock()
	capN := len(b.ring.entries)
	count := b.ring.count
	head := b.ring.head
	dropped := b.ring.dropped
	var latestSeq uint64
	if b.ring.nextSeq > 1 {
		latestSeq = b.ring.nextSeq - 1
	}
	var oldestSeq uint64
	if count > 0 {
		oldestSeq = b.ring.entries[head].Seq
	}

	var missed uint64
	if opts.Since > 0 && count > 0 && opts.Since+1 < oldestSeq {
		missed = oldestSeq - (opts.Since + 1)
	}

	matched := make([]LogEntry, 0, min(count, limit))
	for i := range count {
		e := b.ring.entries[(head+i)%capN]
		if e.Seq <= opts.Since {
			continue
		}
		if opts.HasMinLevel && e.LevelNum < int(opts.MinLevel) {
			continue
		}
		if needle != "" && !logEntryContains(e, needle) {
			continue
		}
		matched = append(matched, cloneLogEntry(e))
	}
	b.ring.mu.RUnlock()

	var (
		hasMore   bool
		truncated bool
		nextCur   = max(opts.Since, latestSeq)
	)
	if len(matched) > limit {
		if opts.Since > 0 {
			// Forward cursor pagination: return earliest page and advance cursor only to last returned entry.
			matched = matched[:limit]
			nextCur = matched[len(matched)-1].Seq
			hasMore = true
		} else {
			// Tail read: return most recent limit entries.
			matched = matched[len(matched)-limit:]
			truncated = true
		}
	}
	if matched == nil {
		matched = []LogEntry{}
	}
	return LogReadResult{
		Configured: true,
		Entries:    matched,
		Returned:   len(matched),
		NextCursor: nextCur,
		OldestSeq:  oldestSeq,
		LatestSeq:  latestSeq,
		Capacity:   capN,
		Buffered:   count,
		Dropped:    dropped,
		Missed:     missed,
		HasMore:    hasMore,
		Truncated:  truncated,
	}
}

func cloneLogEntry(e LogEntry) LogEntry {
	if len(e.Attrs) == 0 {
		return e
	}
	cp := make(map[string]any, len(e.Attrs))
	for k, v := range e.Attrs {
		cp[k] = v
	}
	e.Attrs = cp
	return e
}

func logEntryContains(e LogEntry, lowerNeedle string) bool {
	if strings.Contains(strings.ToLower(e.Message), lowerNeedle) {
		return true
	}
	if e.Source != "" && strings.Contains(strings.ToLower(e.Source), lowerNeedle) {
		return true
	}
	if len(e.Attrs) > 0 {
		if raw, err := json.Marshal(e.Attrs); err == nil {
			if strings.Contains(strings.ToLower(string(raw)), lowerNeedle) {
				return true
			}
		}
	}
	return false
}

func collectSlogAttr(dst *map[string]any, groups []string, a slog.Attr, depth int) {
	if depth >= 16 {
		return
	}
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		sub := a.Value.Group()
		if len(sub) == 0 {
			return
		}
		nextGroups := groups
		if a.Key != "" {
			nextGroups = append(slices.Clone(groups), a.Key)
		}
		for _, child := range sub {
			collectSlogAttr(dst, nextGroups, child, depth+1)
		}
		return
	}
	if a.Key == "" {
		return
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + a.Key
	}
	if *dst == nil {
		*dst = make(map[string]any)
	}
	(*dst)[key] = sanitizeSlogValue(a.Value)
}

func sanitizeSlogValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindBool:
		return v.Bool()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		f := v.Float64()
		switch {
		case math.IsNaN(f):
			return "NaN"
		case math.IsInf(f, 1):
			return "+Inf"
		case math.IsInf(f, -1):
			return "-Inf"
		default:
			return f
		}
	case slog.KindString:
		return v.String()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	default:
		anyVal := v.Any()
		if err, ok := anyVal.(error); ok {
			return err.Error()
		}
		if raw, err := encodeValue(anyVal); err == nil {
			var decoded any
			if json.Unmarshal(raw, &decoded) == nil {
				return decoded
			}
		}
		return fmt.Sprint(anyVal)
	}
}

func shortSourceFile(file string) string {
	idx := strings.LastIndexByte(file, '/')
	if idx <= 0 {
		return file
	}
	prev := strings.LastIndexByte(file[:idx], '/')
	if prev < 0 {
		return file
	}
	return file[prev+1:]
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}

var defaultLoggerMu sync.Mutex

func (b *Bridge) initLog() {
	if b == nil || b.cfg.Log == nil {
		return
	}
	if b.cfg.Log.Buffer != nil {
		b.logBuf = b.cfg.Log.Buffer
		return
	}
	b.logBuf = NewLogBuffer(LogBufferOptions{
		Capacity:     b.cfg.Log.Capacity,
		MinLevel:     b.cfg.Log.MinLevel,
		MaxLineBytes: b.cfg.Log.MaxLineBytes,
	})
}

func (b *Bridge) attachDefaultLogger() func() {
	if b == nil || b.cfg.Log == nil || b.logBuf == nil {
		return func() {}
	}
	shouldInstall := (b.cfg.Log.Buffer == nil && !b.cfg.Log.NoDefault) || b.cfg.Log.InstallDefault
	if !shouldInstall {
		return func() {}
	}
	defaultLoggerMu.Lock()
	prev := slog.Default()
	installed := slog.New(b.logBuf.Tee(prev.Handler()))
	// slog.SetDefault points the log package's output at the new handler. When prev is slog's built-in handler, the tee forwards each record back into the log package, whose output lock is then taken twice and every slog call deadlocks; put the log package's writer and flags back so log.Print keeps its previous destination and the tee never re-enters it.
	prevWriter, prevFlags := log.Writer(), log.Flags()
	slog.SetDefault(installed)
	log.SetOutput(prevWriter)
	log.SetFlags(prevFlags)
	defaultLoggerMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			defaultLoggerMu.Lock()
			if slog.Default() == installed {
				slog.SetDefault(prev)
			}
			defaultLoggerMu.Unlock()
		})
	}
}

// LogBuffer returns the Bridge's configured LogBuffer, or nil when Config.Log is nil.
// Games using a dedicated *slog.Logger can wire slog.New(bridge.LogBuffer().Tee(handler)).
func (b *Bridge) LogBuffer() *LogBuffer {
	if b == nil {
		return nil
	}
	return b.logBuf
}

var schemaLogRead = json.RawMessage(`{
	"type": "object",
	"additionalProperties": false,
	"properties": {
		"since": {
			"type": "integer",
			"minimum": 0,
			"description": "Cursor from a previous game_log_read response's next_cursor; returns entries with seq > since in order so polling resumes without gaps."
		},
		"level": {
			"type": "string",
			"description": "Minimum log level filter (\"debug\", \"info\", \"warn\", \"error\", or slog level syntax such as \"INFO+2\")."
		},
		"contains": {
			"type": "string",
			"description": "Optional case-insensitive substring filter matched against message, source, and structured attributes."
		},
		"limit": {
			"type": "integer",
			"minimum": 1,
			"maximum": 1000,
			"description": "Maximum number of entries to return (default 100, max 1000)."
		}
	}
}`)

func (b *Bridge) logTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolLogRead,
			Description: "Read recent structured log entries from the game's bounded slog ring buffer (with cursor, level, substring, and limit filtering) plus the last panic recorded by pkg/tooling/gamedebug.",
			InputSchema: schemaLogRead,
			Handler:     b.handleLogRead,
		},
	}
}

type logReadArgs struct {
	Since    uint64 `json:"since"`
	Level    string `json:"level"`
	Contains string `json:"contains"`
	Limit    int    `json:"limit"`
}

func parseLogLevel(raw string) (slog.Level, error) {
	s := strings.TrimSpace(raw)
	if strings.EqualFold(s, "warning") {
		return slog.LevelWarn, nil
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		return 0, fmt.Errorf("mcp: invalid log level %q (want debug, info, warn, error, or slog level syntax)", raw)
	}
	return lvl, nil
}

func (b *Bridge) handleLogRead(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[logReadArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Limit < 0 || args.Limit > MaxLogReadLimit {
		return nil, fmt.Errorf("mcp: limit must be between 1 and %d (got %d)", MaxLogReadLimit, args.Limit)
	}
	opts := LogReadOptions{
		Since:    args.Since,
		Contains: args.Contains,
		Limit:    args.Limit,
	}
	if strings.TrimSpace(args.Level) != "" {
		lvl, err := parseLogLevel(args.Level)
		if err != nil {
			return nil, err
		}
		opts.MinLevel = lvl
		opts.HasMinLevel = true
	}

	var res LogReadResult
	if b.logBuf != nil {
		res = b.logBuf.Read(opts)
	} else {
		res = LogReadResult{
			Configured: false,
			Entries:    []LogEntry{},
		}
	}

	lastPanicFn := gamedebug.LastPanic
	if b.cfg.Log != nil && b.cfg.Log.LastPanic != nil {
		lastPanicFn = b.cfg.Log.LastPanic
	}
	if rec, ok := lastPanicFn(); ok {
		cp := rec
		res.LastPanic = &cp
	}
	return res, nil
}
