package flow

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

var ErrUnknownCommand = errors.New("flow: unknown map-event command")

// MapEventDoc is a strict YAML map event definition. Pages are checked in
// order, so the first page whose When conditions match is active.
type MapEventDoc struct {
	Version int         `yaml:"version"`
	Pages   []EventPage `yaml:"pages"`
}
type EventPage struct {
	When     EventConditions `yaml:"when,omitempty"`
	Trigger  string          `yaml:"trigger,omitempty"`
	Commands []EventCommand  `yaml:"commands,omitempty"`
}
type EventConditions struct {
	Self      map[string]bool `yaml:"self,omitempty"`
	Switches  map[string]bool `yaml:"switches,omitempty"`
	Variables map[string]int  `yaml:"variables,omitempty"`
}
type EventCommand struct {
	Say  *SayCommand  `yaml:"say,omitempty"`
	Wait *WaitCommand `yaml:"wait,omitempty"`
	Set  *SetCommand  `yaml:"set,omitempty"`
	Self *SelfCommand `yaml:"self,omitempty"`
}
type SayCommand struct {
	Text string `yaml:"text"`
}
type WaitCommand struct {
	Seconds float64 `yaml:"seconds"`
}
type SetCommand struct {
	Name  string `yaml:"name"`
	Value int    `yaml:"value"`
}
type SelfCommand struct {
	Name  string `yaml:"name"`
	Value bool   `yaml:"value"`
}

func ParseMapEvent(data []byte) (MapEventDoc, error) {
	var doc MapEventDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return MapEventDoc{}, err
	}
	if doc.Version != 1 {
		return MapEventDoc{}, fmt.Errorf("flow: unsupported map event version %d", doc.Version)
	}
	if len(doc.Pages) == 0 {
		return MapEventDoc{}, errors.New("flow: map event has no pages")
	}
	for pi, page := range doc.Pages {
		if page.Trigger == "" {
			page.Trigger = "action"
		}
		switch page.Trigger {
		case "action", "touch", "autorun", "parallel":
		default:
			return MapEventDoc{}, fmt.Errorf("flow: page %d: invalid trigger %q", pi, page.Trigger)
		}
		for ci, command := range page.Commands {
			n := 0
			if command.Say != nil {
				n++
			}
			if command.Wait != nil {
				n++
			}
			if command.Set != nil {
				n++
			}
			if command.Self != nil {
				n++
			}
			if n != 1 {
				return MapEventDoc{}, fmt.Errorf("flow: page %d command %d: exactly one command is required", pi, ci)
			}
			if command.Wait != nil && command.Wait.Seconds < 0 {
				return MapEventDoc{}, fmt.Errorf("flow: page %d command %d: negative wait", pi, ci)
			}
		}
	}
	return doc, nil
}

// EventSnapshot is a text-friendly save point for a running event.
type EventSnapshot struct {
	Version   int             `json:"version" yaml:"version"`
	Page      int             `json:"page" yaml:"page"`
	Command   int             `json:"command" yaml:"command"`
	Waiting   time.Duration   `json:"waiting,omitempty" yaml:"waiting,omitempty"`
	Self      map[string]bool `json:"self,omitempty" yaml:"self,omitempty"`
	Variables map[string]int  `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// EventRunner executes a selected page without blocking the game loop.
type EventRunner struct {
	doc           MapEventDoc
	page, command int
	waiting       time.Duration
	Self          map[string]bool
	Variables     map[string]int
	Say           func(string)
}

func NewEventRunner(doc MapEventDoc) *EventRunner {
	return &EventRunner{doc: doc, Self: map[string]bool{}, Variables: map[string]int{}}
}
func (r *EventRunner) Update(dt time.Duration) bool {
	if r == nil || r.page >= len(r.doc.Pages) {
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
			r.waiting = time.Duration(c.Wait.Seconds * float64(time.Second))
			if r.waiting > 0 {
				return false
			}
		case c.Set != nil:
			r.Variables[c.Set.Name] = c.Set.Value
		case c.Self != nil:
			r.Self[c.Self.Name] = c.Self.Value
		}
	}
	return true
}
func (r *EventRunner) Snapshot() EventSnapshot {
	return EventSnapshot{Version: 1, Page: r.page, Command: r.command, Waiting: r.waiting, Self: cloneBoolMap(r.Self), Variables: cloneIntMap(r.Variables)}
}
func (r *EventRunner) Restore(s EventSnapshot) error {
	if s.Version != 1 || s.Page < 0 || s.Command < 0 || s.Page > len(r.doc.Pages) {
		return errors.New("flow: invalid event snapshot")
	}
	r.page, r.command, r.waiting, r.Self, r.Variables = s.Page, s.Command, s.Waiting, cloneBoolMap(s.Self), cloneIntMap(s.Variables)
	return nil
}
func cloneBoolMap(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func cloneIntMap(m map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
