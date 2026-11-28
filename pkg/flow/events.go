package flow

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// Trigger controls when a map event page starts.
type Trigger string

const (
	TriggerAction   Trigger = "action"
	TriggerTouch    Trigger = "touch"
	TriggerAutorun  Trigger = "autorun"
	TriggerParallel Trigger = "parallel"
)

// Condition selects an event page from saved switch state.
type Condition struct {
	Self string `yaml:"self,omitempty"`
	Is   *bool  `yaml:"is,omitempty"`
}

// Command is one supported data command. Exactly one command key is required.
type Command struct {
	Say  map[string]any `yaml:"say,omitempty"`
	Give map[string]any `yaml:"give,omitempty"`
	Wait map[string]any `yaml:"wait,omitempty"`
	Self map[string]any `yaml:"self,omitempty"`
}

// Page is one ordered page of a map event.
type Page struct {
	When     Condition `yaml:"when,omitempty"`
	Trigger  Trigger   `yaml:"trigger"`
	Sprite   string    `yaml:"sprite,omitempty"`
	Commands []Command `yaml:"commands,omitempty"`
}

// Event is a reusable map event definition.
type Event struct {
	Version int    `yaml:"version"`
	Pages   []Page `yaml:"pages"`
}

// ParseEvent strictly parses and validates a map event YAML document.
func ParseEvent(data []byte) (*Event, error) {
	var e Event
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&e); err != nil {
		return nil, fmt.Errorf("flow: decode event: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("flow: event: trailing document")
	}
	if err := ValidateEvent(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ValidateEvent checks page order, triggers, conditions and command vocabulary.
func ValidateEvent(e *Event) error {
	if e == nil || e.Version != 1 {
		return errors.New("flow: event: version must be 1")
	}
	if len(e.Pages) == 0 {
		return errors.New("flow: event: pages must not be empty")
	}
	for i, p := range e.Pages {
		if p.Trigger != TriggerAction && p.Trigger != TriggerTouch && p.Trigger != TriggerAutorun && p.Trigger != TriggerParallel {
			return fmt.Errorf("flow: event: pages[%d]: unknown trigger %q", i, p.Trigger)
		}
		if p.When.Self != "" && p.When.Is == nil {
			return fmt.Errorf("flow: event: pages[%d].when.is is required", i)
		}
		for j, c := range p.Commands {
			n := 0
			if c.Say != nil {
				n++
			}
			if c.Give != nil {
				n++
			}
			if c.Wait != nil {
				n++
			}
			if c.Self != nil {
				n++
			}
			if n != 1 {
				return fmt.Errorf("flow: event: pages[%d].commands[%d]: command must contain exactly one known key", i, j)
			}
		}
	}
	return nil
}

// SelectPage returns the last page whose self-switch condition matches, preserving RPG-style page precedence.
func (e *Event) SelectPage(self map[string]bool) (Page, bool) {
	if e == nil {
		return Page{}, false
	}
	for i := len(e.Pages) - 1; i >= 0; i-- {
		p := e.Pages[i]
		if p.When.Self == "" || self[p.When.Self] == *p.When.Is {
			return p, true
		}
	}
	return Page{}, false
}

// EncodeEvent writes deterministic two-space YAML.
func EncodeEvent(e *Event) ([]byte, error) {
	if err := ValidateEvent(e); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return b.Bytes(), nil
}
