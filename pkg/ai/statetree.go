package ai

import (
	"bytes"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
)

type StateTree struct {
	Version    int               `yaml:"version"`
	Parameters map[string]string `yaml:"parameters,omitempty"`
	States     []State           `yaml:"states"`
}
type State struct {
	ID           string           `yaml:"id"`
	Enter        []StateCondition `yaml:"enter,omitempty"`
	Tasks        []Task           `yaml:"tasks,omitempty"`
	CompleteWhen string           `yaml:"completeWhen,omitempty"`
	Transitions  []Transition     `yaml:"transitions,omitempty"`
	Children     []State          `yaml:"children,omitempty"`
}
type StateCondition struct {
	Key, Op string
	Value   any `yaml:"value,omitempty"`
}
type Task struct {
	Name   string         `yaml:"name"`
	Params map[string]any `yaml:"params,omitempty"`
}
type Transition struct {
	On, To   string
	Priority int  `yaml:"priority,omitempty"`
	Delay    int  `yaml:"delay,omitempty"`
	Restart  bool `yaml:"restart,omitempty"`
}

func ParseStateTree(data []byte) (StateTree, error) {
	var t StateTree
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&t); err != nil {
		return t, err
	}
	if t.Version != 1 || len(t.States) == 0 {
		return t, errors.New("ai: statetree requires version 1 and states")
	}
	seen := map[string]bool{}
	var walk func([]State, string) error
	walk = func(xs []State, prefix string) error {
		for _, s := range xs {
			if s.ID == "" || seen[s.ID] {
				return fmt.Errorf("ai: duplicate or empty state %q", s.ID)
			}
			seen[s.ID] = true
			if s.CompleteWhen != "" && s.CompleteWhen != "any" && s.CompleteWhen != "all" {
				return fmt.Errorf("ai: state %q invalid completeWhen", s.ID)
			}
			for _, tr := range s.Transitions {
				switch tr.On {
				case "completed", "succeeded", "failed", "condition", "event":
				default:
					return fmt.Errorf("ai: state %q invalid transition trigger %q", s.ID, tr.On)
				}
				if tr.To == "" {
					return fmt.Errorf("ai: state %q transition target empty", s.ID)
				}
			}
			if err := walk(s.Children, prefix+s.ID+"/"); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(t.States, ""); err != nil {
		return t, err
	}
	return t, nil
}
