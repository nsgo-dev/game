package liveops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// RemindersVersion is the schema version of a YAML reminders file (reminders.yaml) this package reads and writes.
const RemindersVersion = 1

// RemindersSchemaURL is where the JSON Schema of version 1 reminders files is published.
// EncodeReminders writes it in a yaml-language-server comment on the first line so editors and AI tools validate and complete reminders files as they are written.
const RemindersSchemaURL = "https://game.nsgo.dev/schemas/reminders.v1.json"

// RemindersSchemaComment is the first line EncodeReminders writes: a YAML comment pointing a YAML language server at RemindersSchemaURL. ParseReminders ignores it like any other comment.
const RemindersSchemaComment = "# yaml-language-server: $schema=" + RemindersSchemaURL

const (
	maxReminderIDLength = 128

	fixReminders        = "write reminders.yaml as version: 1 with optional quietHours and a list of reminders"
	fixRemindersVersion = "set version: 1"
	fixRemindersYAMLRef = "write values directly without YAML anchors, aliases, or merge keys"
	fixReminderTime     = `write a 24-hour "HH:MM" time such as "09:00" or "22:30"`
)

// ReminderRepeat describes how often a scheduled reminder fires.
type ReminderRepeat string

const (
	// ReminderRepeatNone fires the reminder once.
	ReminderRepeatNone ReminderRepeat = ""
	// ReminderRepeatDaily fires the reminder every day at its scheduled local wall-clock time.
	ReminderRepeatDaily ReminderRepeat = "daily"
	// ReminderRepeatWeekly fires the reminder every week on its scheduled weekday and local wall-clock time.
	ReminderRepeatWeekly ReminderRepeat = "weekly"
)

// Valid reports whether r is a supported ReminderRepeat value (empty, daily, or weekly).
func (r ReminderRepeat) Valid() bool {
	return r == ReminderRepeatNone || r == ReminderRepeatDaily || r == ReminderRepeatWeekly
}

// ReminderOnOpen controls what happens to a scheduled reminder when the app launches or returns to the foreground.
type ReminderOnOpen string

const (
	// ReminderOnOpenCancel cancels the pending reminder when the app comes to the front (the default when omitted).
	ReminderOnOpenCancel ReminderOnOpen = "cancel"
	// ReminderOnOpenReschedule reschedules the reminder from the current wall time when the app comes to the front.
	ReminderOnOpenReschedule ReminderOnOpen = "reschedule"
	// ReminderOnOpenKeep leaves the pending reminder scheduled unchanged when the app comes to the front.
	ReminderOnOpenKeep ReminderOnOpen = "keep"
)

// Valid reports whether o is a supported ReminderOnOpen value (empty, cancel, reschedule, or keep).
func (o ReminderOnOpen) Valid() bool {
	return o == "" || o == ReminderOnOpenCancel || o == ReminderOnOpenReschedule || o == ReminderOnOpenKeep
}

// QuietHours defines a daily local-time window [Start, End) during which reminders are not delivered.
// Any reminder that would fire inside QuietHours is deferred to End (on the same day, or the following morning when Start is later than End).
type QuietHours struct {
	// Start is the inclusive local start time in 24-hour "HH:MM" form, such as "22:00".
	Start string `yaml:"start" schema:"required,minLength=5"`
	// End is the exclusive local end time in 24-hour "HH:MM" form, such as "08:00".
	End string `yaml:"end" schema:"required,minLength=5"`
}

// Validate checks that q has well-formed, distinct Start and End wall-clock times.
func (q QuietHours) Validate() error {
	sh, sm, err := parseClockTime(q.Start)
	if err != nil {
		return &loaderr.Error{
			Field:   "start",
			Problem: fmt.Sprintf("liveops: invalid quietHours.start %q: %v", q.Start, err),
			Fix:     fixReminderTime,
		}
	}
	eh, em, err := parseClockTime(q.End)
	if err != nil {
		return &loaderr.Error{
			Field:   "end",
			Problem: fmt.Sprintf("liveops: invalid quietHours.end %q: %v", q.End, err),
			Fix:     fixReminderTime,
		}
	}
	if sh*60+sm == eh*60+em {
		return &loaderr.Error{
			Field:   "end",
			Problem: fmt.Sprintf("liveops: quietHours start and end must differ (both %q)", q.Start),
			Fix:     "choose distinct start and end times, such as 22:00 and 08:00",
		}
	}
	return nil
}

// Clone returns a copy of q, or nil when q is nil.
func (q *QuietHours) Clone() *QuietHours {
	if q == nil {
		return nil
	}
	cp := *q
	return &cp
}

// Contains reports whether t falls inside q's local-time window [Start, End).
// Returns false if q is not valid.
func (q QuietHours) Contains(t time.Time) bool {
	sh, sm, err := parseClockTime(q.Start)
	if err != nil {
		return false
	}
	eh, em, err := parseClockTime(q.End)
	if err != nil {
		return false
	}
	startMin := sh*60 + sm
	endMin := eh*60 + em
	if startMin == endMin {
		return false
	}
	curMin := t.Hour()*60 + t.Minute()
	if startMin < endMin {
		return curMin >= startMin && curMin < endMin
	}
	return curMin >= startMin || curMin < endMin
}

// NextAllowed returns t if t is outside QuietHours, or the local time when QuietHours ends otherwise.
// If q is not valid, t is returned unchanged.
func (q QuietHours) NextAllowed(t time.Time) time.Time {
	sh, sm, err := parseClockTime(q.Start)
	if err != nil {
		return t
	}
	eh, em, err := parseClockTime(q.End)
	if err != nil {
		return t
	}
	startMin := sh*60 + sm
	endMin := eh*60 + em
	if startMin == endMin {
		return t
	}
	curMin := t.Hour()*60 + t.Minute()
	if startMin < endMin {
		if curMin >= startMin && curMin < endMin {
			return time.Date(t.Year(), t.Month(), t.Day(), eh, em, 0, 0, t.Location())
		}
		return t
	}
	if curMin >= startMin {
		return time.Date(t.Year(), t.Month(), t.Day()+1, eh, em, 0, 0, t.Location())
	}
	if curMin < endMin {
		return time.Date(t.Year(), t.Month(), t.Day(), eh, em, 0, 0, t.Location())
	}
	return t
}

// RemindersConfig is the root of a versioned YAML reminders file (reminders.yaml).
type RemindersConfig struct {
	// Version is the schema version; it must equal RemindersVersion (1).
	Version int `yaml:"version" schema:"required"`
	// QuietHours is the optional default daily local-time window during which reminders are deferred until the window ends.
	QuietHours *QuietHours `yaml:"quietHours,omitempty"`
	// Reminders lists the game's local re-engagement reminder definitions in declaration order.
	Reminders []ReminderDef `yaml:"reminders,omitempty"`
}

// ReminderDef declares one local re-engagement reminder in a RemindersConfig file.
type ReminderDef struct {
	// ID uniquely names the reminder: 1 to 128 ASCII letters, digits, '.', '_' or '-'.
	ID string `yaml:"id" schema:"required,minLength=1"`
	// Title is the localization key for the notification title.
	Title string `yaml:"title" schema:"required,minLength=1"`
	// Body is the optional localization key for the notification body.
	Body string `yaml:"body,omitempty"`
	// Delay schedules the reminder this positive Go duration after Schedule is called, such as "4h0m0s" or "30m0s". Mutually exclusive with At.
	Delay time.Duration `yaml:"delay,omitempty"`
	// At schedules the reminder at the next occurrence of this 24-hour local wall-clock time ("HH:MM"), such as "09:00". Mutually exclusive with Delay.
	At string `yaml:"at,omitempty"`
	// Weekday restricts an At-based reminder to a specific day of the week (sunday through saturday); requires At and cannot be paired with daily repeat.
	Weekday string `yaml:"weekday,omitempty" schema:"enum=sunday|monday|tuesday|wednesday|thursday|friday|saturday"`
	// Repeat sets how often the reminder repeats after its first firing: omitted for once, "daily", or "weekly".
	Repeat ReminderRepeat `yaml:"repeat,omitempty" schema:"enum=daily|weekly"`
	// OnOpen controls what happens to this reminder when the app comes to the front: "cancel" (default when omitted), "reschedule", or "keep".
	OnOpen ReminderOnOpen `yaml:"onOpen,omitempty" schema:"enum=cancel|reschedule|keep"`
	// Silent delivers the notification without the system's sound or vibration.
	Silent bool `yaml:"silent,omitempty"`
}

// EffectiveOnOpen returns d.OnOpen, defaulting to ReminderOnOpenCancel when omitted.
func (d ReminderDef) EffectiveOnOpen() ReminderOnOpen {
	if d.OnOpen == "" {
		return ReminderOnOpenCancel
	}
	return d.OnOpen
}

// Lookup returns the reminder definition with ID id.
func (c *RemindersConfig) Lookup(id string) (ReminderDef, bool) {
	if c == nil {
		return ReminderDef{}, false
	}
	for _, r := range c.Reminders {
		if r.ID == id {
			return r, true
		}
	}
	return ReminderDef{}, false
}

// Clone returns a deep copy of c.
func (c *RemindersConfig) Clone() *RemindersConfig {
	if c == nil {
		return nil
	}
	return &RemindersConfig{
		Version:    c.Version,
		QuietHours: c.QuietHours.Clone(),
		Reminders:  slices.Clone(c.Reminders),
	}
}

// ParseReminders decodes and validates a versioned YAML reminders document.
// Unknown fields, trailing YAML documents, duplicate keys, YAML anchors/aliases/merge keys, missing or unsupported versions, and invalid schedules or quiet hours are refused with *loaderr.Error line info.
func ParseReminders(data []byte) (*RemindersConfig, error) {
	cfg, err := parseReminders(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixReminders)
	}
	return cfg, nil
}

// LoadRemindersFS reads name from fsys and parses it with ParseReminders.
func LoadRemindersFS(fsys fs.FS, name string) (*RemindersConfig, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("liveops: read reminders %s: %w", name, err)
	}
	cfg, err := parseReminders(data)
	if err != nil {
		return nil, loaderr.Locate(err, name, data, fixReminders)
	}
	return cfg, nil
}

func parseReminders(data []byte) (*RemindersConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixReminders, "liveops: empty reminders document")
	}

	var root yaml.Node
	nodeDec := yaml.NewDecoder(bytes.NewReader(data))
	if err := nodeDec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Errorf("", fixReminders, "liveops: empty reminders document")
		}
		return nil, fmt.Errorf("liveops: decode reminders: %w", err)
	}
	var extraNode yaml.Node
	if err := nodeDec.Decode(&extraNode); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    extraNode.Line,
				Column:  extraNode.Column,
				Problem: "liveops: unexpected trailing YAML document in reminders file",
				Fix:     "keep one YAML document in reminders.yaml",
			}
		}
		return nil, fmt.Errorf("liveops: decode reminders: %w", err)
	}
	if err := checkPlainRemindersNode(&root); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg RemindersConfig
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Errorf("", fixReminders, "liveops: empty reminders document")
		}
		return nil, fmt.Errorf("liveops: decode reminders: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in reminders.yaml", "liveops: unexpected trailing YAML document in reminders file")
		}
		return nil, fmt.Errorf("liveops: decode reminders: %w", err)
	}
	if err := ValidateRemindersConfig(&cfg); err != nil {
		return nil, err
	}
	if err := checkRemindersVersionScalar(&root, cfg.Version); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// EncodeReminders validates cfg and writes deterministic 2-space YAML prefixed with RemindersSchemaComment that ParseReminders reads back unchanged.
func EncodeReminders(cfg *RemindersConfig) ([]byte, error) {
	if err := ValidateRemindersConfig(cfg); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(RemindersSchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("liveops: encode reminders: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("liveops: encode reminders: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateRemindersConfig checks cfg against the version 1 reminders schema.
func ValidateRemindersConfig(cfg *RemindersConfig) error {
	if cfg == nil {
		return loaderr.Errorf("", fixReminders, "liveops: nil reminders config")
	}
	switch cfg.Version {
	case RemindersVersion:
	case 0:
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("liveops: reminders file is missing version (want %d)", RemindersVersion),
			Expected: strconv.Itoa(RemindersVersion),
			Fix:      fixRemindersVersion,
		}
	default:
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("liveops: unsupported reminders version %d (want %d)", cfg.Version, RemindersVersion),
			Expected: strconv.Itoa(RemindersVersion),
			Fix:      fixRemindersVersion,
		}
	}
	if cfg.QuietHours != nil {
		if err := cfg.QuietHours.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(cfg.Reminders))
	for i, r := range cfg.Reminders {
		where := fmt.Sprintf("reminders[%d]", i)
		if err := validateReminderID(r.ID); err != nil {
			return &loaderr.Error{
				Field:   "id",
				Problem: fmt.Sprintf("liveops: %s: %v", where, err),
				Fix:     "give each reminder a unique id of 1 to 128 ASCII letters, digits, '.', '_' or '-'",
			}
		}
		where = fmt.Sprintf("reminder %q", r.ID)
		if _, dup := seen[r.ID]; dup {
			return &loaderr.Error{
				Field:   "id",
				Problem: fmt.Sprintf("liveops: %s is declared more than once", where),
				Fix:     "remove or rename the duplicate reminder id",
			}
		}
		seen[r.ID] = struct{}{}
		if err := validateReminderKey(r.Title, true, where, "title"); err != nil {
			return err
		}
		if err := validateReminderKey(r.Body, false, where, "body"); err != nil {
			return err
		}
		if r.Delay < 0 {
			return &loaderr.Error{
				Field:   "delay",
				Problem: fmt.Sprintf("liveops: %s: delay %s must be positive", where, r.Delay),
				Fix:     `set a positive Go duration such as "30m" or "4h"`,
			}
		}
		hasDelay := r.Delay > 0
		hasAt := r.At != ""
		switch {
		case hasDelay && hasAt:
			return &loaderr.Error{
				Field:   "at",
				Problem: fmt.Sprintf("liveops: %s: delay and at cannot both be set", where),
				Fix:     "set either delay or at, not both",
			}
		case !hasDelay && !hasAt:
			return &loaderr.Error{
				Field:   "delay",
				Problem: fmt.Sprintf("liveops: %s: either delay or at must be set", where),
				Fix:     `set delay (such as "4h") or at (such as "09:00")`,
			}
		}
		if hasAt {
			if _, _, err := parseClockTime(r.At); err != nil {
				return &loaderr.Error{
					Field:   "at",
					Problem: fmt.Sprintf("liveops: %s: invalid at %q: %v", where, r.At, err),
					Fix:     fixReminderTime,
				}
			}
		}
		if r.Weekday != "" {
			if _, ok := parseWeekday(r.Weekday); !ok {
				return &loaderr.Error{
					Field:   "weekday",
					Problem: fmt.Sprintf("liveops: %s: unknown weekday %q", where, r.Weekday),
					Fix:     "use sunday, monday, tuesday, wednesday, thursday, friday, or saturday",
				}
			}
			if !hasAt {
				return &loaderr.Error{
					Field:   "weekday",
					Problem: fmt.Sprintf("liveops: %s: weekday requires at", where),
					Fix:     "set at alongside weekday, or remove weekday",
				}
			}
			if r.Repeat == ReminderRepeatDaily {
				return &loaderr.Error{
					Field:   "weekday",
					Problem: fmt.Sprintf("liveops: %s: weekday cannot be used with daily repeat", where),
					Fix:     "use repeat: weekly (or omit repeat) when weekday is set",
				}
			}
		}
		if !r.Repeat.Valid() {
			return &loaderr.Error{
				Field:   "repeat",
				Problem: fmt.Sprintf("liveops: %s: unsupported repeat %q", where, r.Repeat),
				Fix:     "omit repeat, or set repeat to daily or weekly",
			}
		}
		if !r.OnOpen.Valid() {
			return &loaderr.Error{
				Field:   "onOpen",
				Problem: fmt.Sprintf("liveops: %s: unsupported onOpen %q", where, r.OnOpen),
				Fix:     "omit onOpen, or set onOpen to cancel, reschedule, or keep",
			}
		}
	}
	return nil
}

func validateReminderID(id string) error {
	if id == "" {
		return errors.New("empty reminder id")
	}
	if len(id) > maxReminderIDLength {
		return fmt.Errorf("reminder id %q exceeds %d bytes", id, maxReminderIDLength)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
		if !ok {
			return fmt.Errorf("reminder id %q contains invalid character %q", id, c)
		}
	}
	return nil
}

func validateReminderKey(s string, required bool, where, field string) error {
	if s == "" {
		if required {
			return &loaderr.Error{
				Field:   field,
				Problem: fmt.Sprintf("liveops: %s: missing %s localization key", where, field),
				Fix:     fmt.Sprintf("set %s to a non-empty localization key", field),
			}
		}
		return nil
	}
	if !utf8.ValidString(s) {
		return &loaderr.Error{
			Field:   field,
			Problem: fmt.Sprintf("liveops: %s: %s must be valid UTF-8", where, field),
			Fix:     fmt.Sprintf("write %s as valid UTF-8 text", field),
		}
	}
	if strings.TrimSpace(s) == "" {
		return &loaderr.Error{
			Field:   field,
			Problem: fmt.Sprintf("liveops: %s: %s must not be blank", where, field),
			Fix:     fmt.Sprintf("set %s to a non-empty localization key", field),
		}
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return &loaderr.Error{
				Field:   field,
				Problem: fmt.Sprintf("liveops: %s: %s must not contain control characters", where, field),
				Fix:     fmt.Sprintf("remove control characters from %s", field),
			}
		}
	}
	return nil
}

// parseClockTime parses a strict 24-hour "HH:MM" time string (00:00 through 23:59).
func parseClockTime(s string) (hour, minute int, err error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, errors.New(`must be "HH:MM"`)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, 0, errors.New(`must be "HH:MM"`)
		}
	}
	hour = int(s[0]-'0')*10 + int(s[1]-'0')
	minute = int(s[3]-'0')*10 + int(s[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, 0, errors.New("hour must be 00-23 and minute 00-59")
	}
	return hour, minute, nil
}

func parseWeekday(s string) (time.Weekday, bool) {
	switch s {
	case "sunday":
		return time.Sunday, true
	case "monday":
		return time.Monday, true
	case "tuesday":
		return time.Tuesday, true
	case "wednesday":
		return time.Wednesday, true
	case "thursday":
		return time.Thursday, true
	case "friday":
		return time.Friday, true
	case "saturday":
		return time.Saturday, true
	default:
		return 0, false
	}
}

func checkPlainRemindersNode(n *yaml.Node) error {
	if err := checkNoRemindersYAMLReferences(n); err != nil {
		return err
	}
	return checkRemindersDuplicateKeys(n)
}

func checkNoRemindersYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("liveops: reminders line %d: YAML aliases are not allowed", n.Line),
			Fix:     fixRemindersYAMLRef,
		}
	}
	if n.Anchor != "" {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("liveops: reminders line %d: YAML anchors are not allowed", n.Line),
			Fix:     fixRemindersYAMLRef,
		}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{
				Line:    c.Line,
				Column:  c.Column,
				Problem: fmt.Sprintf("liveops: reminders line %d: YAML merge keys are not allowed", c.Line),
				Fix:     fixRemindersYAMLRef,
			}
		}
		if err := checkNoRemindersYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

func checkRemindersDuplicateKeys(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]int, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if firstLine, dup := seen[k.Value]; dup {
				return &loaderr.Error{
					Line:    k.Line,
					Column:  k.Column,
					Field:   k.Value,
					Problem: fmt.Sprintf("liveops: reminders duplicate key %q (first defined at line %d)", k.Value, firstLine),
					Fix:     fmt.Sprintf("remove the duplicate %q key", k.Value),
				}
			}
			seen[k.Value] = k.Line
		}
	}
	for _, c := range n.Content {
		if err := checkRemindersDuplicateKeys(c); err != nil {
			return err
		}
	}
	return nil
}

func checkRemindersVersionScalar(n *yaml.Node, version int) error {
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "version" {
			val := doc.Content[i+1]
			if want := strconv.Itoa(version); val.Value != want {
				return &loaderr.Error{
					Line:     val.Line,
					Column:   val.Column,
					Field:    "version",
					Problem:  fmt.Sprintf("liveops: reminders version %q must be written as %s", val.Value, want),
					Expected: want,
					Fix:      "write the version as " + want,
				}
			}
		}
	}
	return nil
}
