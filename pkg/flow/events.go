package flow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// MapEventVersion is the map-event YAML schema version [ParseMapEvent] reads and [EncodeMapEvent] writes.
const MapEventVersion = 1

// EventSnapshotVersion is the [EventSnapshot] schema version written by [EventRunner.Snapshot] and read by [EventRunner.Restore].
const EventSnapshotVersion = 1

// ErrInvalidMapEvent is wrapped by every map-event validation error.
var ErrInvalidMapEvent = errors.New("flow: invalid map event")

// ErrInvalidEventSnapshot is wrapped by every error [EventRunner.Restore] returns.
var ErrInvalidEventSnapshot = errors.New("flow: invalid event snapshot")

// Map-event page triggers. The game decides when a trigger fires (the action button facing the event, the player stepping on it, every step for autorun and parallel) and then calls [EventRunner.Start] with the page [MapEventDoc.ActivePage] picks.
const (
	TriggerAction   = "action"
	TriggerTouch    = "touch"
	TriggerAutorun  = "autorun"
	TriggerParallel = "parallel"
)

const fixMapEvent = "write a map-event mapping with version: 1 and a pages list; docs/en/flow.md documents the format"

// MapEventDoc is a strict YAML map event: an ordered list of pages, of which the last whose conditions hold is active, as in RPG Maker.
type MapEventDoc struct {
	Version int         `yaml:"version"`
	Pages   []EventPage `yaml:"pages"`
}

// EventPage is one page of a map event: the conditions that make it eligible, the trigger that starts it and the commands it runs. An empty trigger in a file reads as [TriggerAction].
type EventPage struct {
	When     EventConditions `yaml:"when,omitempty"`
	Trigger  string          `yaml:"trigger"`
	Commands []EventCommand  `yaml:"commands,omitempty"`
}

// EventConditions must all hold for a page to be eligible: each self switch and switch equals its value, and each variable is at least its value. No conditions always hold.
type EventConditions struct {
	Self      map[string]bool `yaml:"self,omitempty"`
	Switches  map[string]bool `yaml:"switches,omitempty"`
	Variables map[string]int  `yaml:"variables,omitempty"`
}

// EventCommand is one command of a page; exactly one field is set.
type EventCommand struct {
	Say  *SayCommand  `yaml:"say,omitempty"`
	Wait *WaitCommand `yaml:"wait,omitempty"`
	Set  *SetCommand  `yaml:"set,omitempty"`
	Self *SelfCommand `yaml:"self,omitempty"`
}

// SayCommand passes Text, usually a localization key, to [EventRunner.Say].
type SayCommand struct {
	Text string `yaml:"text"`
}

// WaitCommand pauses the page for Seconds of game time.
type WaitCommand struct {
	Seconds float64 `yaml:"seconds"`
}

// SetCommand sets the runner's integer variable Name to Value.
type SetCommand struct {
	Name  string `yaml:"name"`
	Value int    `yaml:"value"`
}

// SelfCommand sets the runner's self switch Name to Value.
type SelfCommand struct {
	Name  string `yaml:"name"`
	Value bool   `yaml:"value"`
}

// maxWaitSeconds is the longest wait a time.Duration holds.
const maxWaitSeconds = float64(math.MaxInt64) / float64(time.Second)

// ParseMapEvent decodes and validates a versioned map-event document. Unknown fields and commands, duplicate keys, missing or unsupported versions and trailing YAML documents are refused. Every error is a *loaderr.Error pointing to the offending field and line with a fix.
func ParseMapEvent(data []byte) (MapEventDoc, error) {
	doc, err := parseMapEvent(data)
	if err != nil {
		return MapEventDoc{}, loaderr.Locate(err, "", data, fixMapEvent)
	}
	return doc, nil
}

func parseMapEvent(data []byte) (MapEventDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		e := loaderr.Errorf("", fmt.Sprintf("add version: %d and a pages list", MapEventVersion), "%w: empty document", ErrInvalidMapEvent)
		e.Line = 1
		return MapEventDoc{}, e
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc MapEventDoc
	if err := dec.Decode(&doc); err != nil {
		return MapEventDoc{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return MapEventDoc{}, loaderr.Errorf("", "keep a single YAML document in the file; remove the trailing --- document", "%w: unexpected trailing YAML document", ErrInvalidMapEvent)
		}
		return MapEventDoc{}, err
	}
	for i := range doc.Pages {
		if doc.Pages[i].Trigger == "" {
			doc.Pages[i].Trigger = TriggerAction
		}
	}
	if err := ValidateMapEvent(doc); err != nil {
		return MapEventDoc{}, err
	}
	return doc, nil
}

// ValidateMapEvent checks doc against the map-event schema: the version, at least one page, a known trigger on every page, exactly one command per list entry, named set and self commands, and finite non-negative waits. Errors are *loaderr.Error values wrapping [ErrInvalidMapEvent].
func ValidateMapEvent(doc MapEventDoc) error {
	if doc.Version != MapEventVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d", MapEventVersion), "%w: unsupported version %d (want %d)", ErrInvalidMapEvent, doc.Version, MapEventVersion)
	}
	if len(doc.Pages) == 0 {
		return loaderr.Errorf("pages", "add at least one page", "%w: no pages", ErrInvalidMapEvent)
	}
	for pi, page := range doc.Pages {
		base := loaderr.Path("pages", pi)
		switch page.Trigger {
		case TriggerAction, TriggerTouch, TriggerAutorun, TriggerParallel:
		default:
			return loaderr.Errorf(loaderr.Join(base, "trigger"), "set trigger to action, touch, autorun or parallel", "%w: page %d has unknown trigger %q", ErrInvalidMapEvent, pi, page.Trigger)
		}
		for ci, c := range page.Commands {
			field := loaderr.Join(base, "commands", ci)
			n := 0
			for _, set := range []bool{c.Say != nil, c.Wait != nil, c.Set != nil, c.Self != nil} {
				if set {
					n++
				}
			}
			if n != 1 {
				return loaderr.Errorf(field, "give each list entry exactly one of say, wait, set or self", "%w: page %d command %d has %d commands", ErrInvalidMapEvent, pi, ci, n)
			}
			switch {
			case c.Wait != nil:
				if s := c.Wait.Seconds; math.IsNaN(s) || s < 0 || s > maxWaitSeconds {
					return loaderr.Errorf(loaderr.Join(field, "wait", "seconds"), "use a finite number of seconds, 0 or more", "%w: page %d command %d waits %v seconds", ErrInvalidMapEvent, pi, ci, s)
				}
			case c.Set != nil:
				if c.Set.Name == "" {
					return loaderr.Errorf(loaderr.Join(field, "set", "name"), "name the variable to set", "%w: page %d command %d sets an unnamed variable", ErrInvalidMapEvent, pi, ci)
				}
			case c.Self != nil:
				if c.Self.Name == "" {
					return loaderr.Errorf(loaderr.Join(field, "self", "name"), "name the self switch to set", "%w: page %d command %d sets an unnamed self switch", ErrInvalidMapEvent, pi, ci)
				}
			}
		}
	}
	return nil
}

// EncodeMapEvent validates doc and writes it in canonical YAML form: 2-space indentation, fields in declaration order, map keys sorted and every page's trigger written.
func EncodeMapEvent(doc MapEventDoc) ([]byte, error) {
	if err := ValidateMapEvent(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("flow: encode map event: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("flow: encode map event: %w", err)
	}
	return buf.Bytes(), nil
}

// ActivePage returns the index of the last page whose conditions hold against the event's self switches, the game's switches and variables, or -1 when none does. Missing names read as false and 0.
func (d MapEventDoc) ActivePage(self, switches map[string]bool, variables map[string]int) int {
	for i := len(d.Pages) - 1; i >= 0; i-- {
		if d.Pages[i].When.hold(self, switches, variables) {
			return i
		}
	}
	return -1
}

func (c EventConditions) hold(self, switches map[string]bool, variables map[string]int) bool {
	for k, v := range c.Self {
		if self[k] != v {
			return false
		}
	}
	for k, v := range c.Switches {
		if switches[k] != v {
			return false
		}
	}
	for k, v := range c.Variables {
		if variables[k] < v {
			return false
		}
	}
	return true
}

// EventSnapshot is a text save point of an [EventRunner]: the running page (-1 when idle), the index of the next command, the time left on a wait, and the runner's self switches and variables.
type EventSnapshot struct {
	Version   int             `json:"version" yaml:"version"`
	Page      int             `json:"page" yaml:"page"`
	Command   int             `json:"command" yaml:"command"`
	Waiting   time.Duration   `json:"waiting,omitempty" yaml:"waiting,omitempty"`
	Self      map[string]bool `json:"self,omitempty" yaml:"self,omitempty"`
	Variables map[string]int  `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// EventRunner runs one page of a map event on the loop's fixed steps without blocking it: a wait returns from [EventRunner.Update] and resumes on a later step. Self and Variables hold what self and set commands write; Say receives say commands.
type EventRunner struct {
	doc       MapEventDoc
	page      int
	command   int
	waiting   time.Duration
	Self      map[string]bool
	Variables map[string]int
	Say       func(string)
}

// NewEventRunner returns an idle runner for doc; call [EventRunner.Start] to run a page.
func NewEventRunner(doc MapEventDoc) *EventRunner {
	return &EventRunner{doc: doc, page: -1, Self: map[string]bool{}, Variables: map[string]int{}}
}

// Start runs page from its first command, replacing any page in progress.
func (r *EventRunner) Start(page int) error {
	if page < 0 || page >= len(r.doc.Pages) {
		return fmt.Errorf("flow: map event has no page %d (it has %d)", page, len(r.doc.Pages))
	}
	r.page, r.command, r.waiting = page, 0, 0
	return nil
}

// Running reports whether a page is in progress.
func (r *EventRunner) Running() bool { return r != nil && r.page >= 0 }

// Update advances the running page by dt, running commands until one waits or the page ends, and reports whether the runner is idle afterwards. Time left over when a wait ends carries into the commands after it.
func (r *EventRunner) Update(dt time.Duration) bool {
	if !r.Running() {
		return true
	}
	if r.waiting > 0 {
		if dt < r.waiting {
			r.waiting -= dt
			return false
		}
		dt -= r.waiting
		r.waiting = 0
	}
	p := r.doc.Pages[r.page]
	for r.command < len(p.Commands) {
		c := p.Commands[r.command]
		r.command++
		switch {
		case c.Say != nil:
			if r.Say != nil {
				r.Say(c.Say.Text)
			}
		case c.Wait != nil:
			w := time.Duration(c.Wait.Seconds * float64(time.Second))
			if w > dt {
				r.waiting = w - dt
				return false
			}
			dt -= w
		case c.Set != nil:
			r.Variables[c.Set.Name] = c.Set.Value
		case c.Self != nil:
			r.Self[c.Self.Name] = c.Self.Value
		}
	}
	r.page, r.command = -1, 0
	return true
}

// Snapshot returns the runner's state for a save slot.
func (r *EventRunner) Snapshot() EventSnapshot {
	return EventSnapshot{Version: EventSnapshotVersion, Page: r.page, Command: r.command, Waiting: r.waiting, Self: cloneBoolMap(r.Self), Variables: cloneIntMap(r.Variables)}
}

// Restore replaces the runner's state with s, which must fit the runner's document; on error the runner is unchanged.
func (r *EventRunner) Restore(s EventSnapshot) error {
	switch {
	case s.Version != EventSnapshotVersion:
		return fmt.Errorf("%w: version %d (want %d)", ErrInvalidEventSnapshot, s.Version, EventSnapshotVersion)
	case s.Page < -1 || s.Page >= len(r.doc.Pages):
		return fmt.Errorf("%w: page %d of %d", ErrInvalidEventSnapshot, s.Page, len(r.doc.Pages))
	case s.Page == -1 && (s.Command != 0 || s.Waiting != 0):
		return fmt.Errorf("%w: an idle runner has no command or wait", ErrInvalidEventSnapshot)
	case s.Page >= 0 && (s.Command < 0 || s.Command > len(r.doc.Pages[s.Page].Commands)):
		return fmt.Errorf("%w: command %d of %d on page %d", ErrInvalidEventSnapshot, s.Command, len(r.doc.Pages[s.Page].Commands), s.Page)
	case s.Waiting < 0:
		return fmt.Errorf("%w: negative wait %v", ErrInvalidEventSnapshot, s.Waiting)
	case s.Waiting > 0 && (s.Command == 0 || r.doc.Pages[s.Page].Commands[s.Command-1].Wait == nil):
		return fmt.Errorf("%w: a wait is left but command %d is not a wait", ErrInvalidEventSnapshot, s.Command-1)
	}
	r.page, r.command, r.waiting = s.Page, s.Command, s.Waiting
	r.Self, r.Variables = cloneBoolMap(s.Self), cloneIntMap(s.Variables)
	return nil
}

func cloneBoolMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
