package ai

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// StateTreeFormatVersion is the version of the YAML state-tree schema.
const StateTreeFormatVersion = 1

// StateTreeSchemaComment is the schema hint written by EncodeStateTree.
const StateTreeSchemaComment = "# yaml-language-server: $schema=https://game.nsgo.dev/schemas/statetree.v1.json"

// StateTree is a validated, designer-authored state tree. Runtime stepping and
// task bindings are separate so the format can be inspected without executing game code.
type StateTree struct {
	Version    int               `yaml:"version"`
	Parameters map[string]string `yaml:"parameters,omitempty"`
	States     []State           `yaml:"states"`
}

// State is one node in a state tree. Children retain file order for deterministic selection tie-breaks.
type State struct {
	ID           string           `yaml:"id"`
	Enter        []StateCondition `yaml:"enter,omitempty"`
	Tasks        []Task           `yaml:"tasks,omitempty"`
	CompleteWhen string           `yaml:"completeWhen,omitempty"`
	Transitions  []Transition     `yaml:"transitions,omitempty"`
	Children     []State          `yaml:"children,omitempty"`
}

// StateCondition checks a blackboard key before a state can be entered.
type StateCondition struct {
	Key   string `yaml:"key"`
	Op    string `yaml:"op"`
	Value any    `yaml:"value,omitempty"`
}

// Task names a behavior-tree action and its typed parameters.
type Task struct {
	Name   string         `yaml:"name"`
	Params map[string]any `yaml:"params,omitempty"`
}

// Transition selects a destination when a state reaches its trigger.
type Transition struct {
	On       string `yaml:"on"`
	To       string `yaml:"to"`
	Priority int    `yaml:"priority,omitempty"`
	Delay    int    `yaml:"delay,omitempty"`
	Restart  bool   `yaml:"restart,omitempty"`
}

const stateTreeFix = "correct the state tree document to match version 1 in docs/en/ai.md"

// ParseStateTree decodes and validates one version 1 state-tree document.
// Unknown fields, unsupported versions, invalid conditions, duplicate IDs and trailing YAML documents are refused.
func ParseStateTree(data []byte) (StateTree, error) {
	var tree StateTree
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&tree); err != nil {
		return tree, loaderr.Locate(fmt.Errorf("ai: decode state tree: %w", err), "", data, stateTreeFix)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return tree, loaderr.Locate(errors.New("ai: unexpected trailing YAML document in state tree"), "", data, stateTreeFix)
		}
		return tree, loaderr.Locate(fmt.Errorf("ai: decode trailing state tree YAML: %w", err), "", data, stateTreeFix)
	}
	if err := ValidateStateTree(&tree); err != nil {
		return tree, loaderr.Locate(err, "", data, stateTreeFix)
	}
	return tree, nil
}

// LoadStateTreeFS reads name from fsys and parses it as a state tree.
func LoadStateTreeFS(fsys fs.FS, name string) (StateTree, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return StateTree{}, fmt.Errorf("ai: read state tree %q: %w", name, err)
	}
	return ParseStateTree(data)
}

// EncodeStateTree validates tree and writes deterministic two-space YAML.
func EncodeStateTree(tree *StateTree) ([]byte, error) {
	if err := ValidateStateTree(tree); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(StateTreeSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(tree); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("ai: encode state tree: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ai: encode state tree: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateStateTree checks the schema and deterministic authoring constraints.
func ValidateStateTree(tree *StateTree) error {
	if tree == nil {
		return loaderr.Errorf("", "pass a non-nil state tree", "ai: nil state tree")
	}
	if tree.Version != StateTreeFormatVersion {
		return loaderr.Errorf("version", "set version: 1", "ai: unsupported state tree version %d", tree.Version)
	}
	if len(tree.States) == 0 {
		return loaderr.Errorf("states", "add at least one root state", "ai: state tree has no states")
	}
	for key := range tree.Parameters {
		if strings.TrimSpace(key) == "" {
			return loaderr.Errorf("parameters", "use non-empty parameter names", "ai: state tree has an empty parameter name")
		}
	}
	seen := map[string]bool{}
	var walk func([]State, string) error
	walk = func(states []State, parentPath string) error {
		for i, state := range states {
			path := state.ID
			if parentPath != "" {
				path = parentPath + "/" + state.ID
			}
			field := fmt.Sprintf("states[%d]", i)
			if strings.TrimSpace(state.ID) == "" {
				return loaderr.Errorf(field+".id", "set a non-empty state id", "ai: state id is empty")
			}
			if strings.Contains(state.ID, "/") {
				return loaderr.Errorf(field+".id", "remove / from state ids and use state paths only in transition targets", "ai: state %q contains /", state.ID)
			}
			if seen[state.ID] {
				return loaderr.Errorf(field+".id", "give every state a unique id", "ai: duplicate state id %q", state.ID)
			}
			seen[state.ID] = true
			for j, condition := range state.Enter {
				if err := validateStateCondition(condition, fmt.Sprintf("%s.enter[%d]", field, j)); err != nil {
					return err
				}
			}
			for j, task := range state.Tasks {
				if strings.TrimSpace(task.Name) == "" {
					return loaderr.Errorf(fmt.Sprintf("%s.tasks[%d].name", field, j), "set a task name", "ai: task name is empty")
				}
				for key := range task.Params {
					if strings.TrimSpace(key) == "" {
						return loaderr.Errorf(fmt.Sprintf("%s.tasks[%d].params", field, j), "use non-empty parameter names", "ai: task parameter name is empty")
					}
				}
			}
			if state.CompleteWhen != "" && state.CompleteWhen != "any" && state.CompleteWhen != "all" {
				return loaderr.Errorf(field+".completeWhen", "use any or all", "ai: state %q has invalid completeWhen %q", path, state.CompleteWhen)
			}
			for j, transition := range state.Transitions {
				if err := validateTransition(transition, fmt.Sprintf("%s.transitions[%d]", field, j)); err != nil {
					return err
				}
			}
			if err := walk(state.Children, path); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(tree.States, "")
}

func validateStateCondition(condition StateCondition, field string) error {
	if strings.TrimSpace(condition.Key) == "" {
		return loaderr.Errorf(field+".key", "set a blackboard key", "ai: condition key is empty")
	}
	switch condition.Op {
	case "set", "unset":
		if condition.Value != nil {
			return loaderr.Errorf(field+".value", "omit value for set and unset", "ai: condition %q does not take a value", condition.Op)
		}
	case "eq", "ne", "lt", "lte", "gt", "gte":
		if condition.Value == nil {
			return loaderr.Errorf(field+".value", "set a comparison value", "ai: condition %q needs a value", condition.Op)
		}
	default:
		return loaderr.Errorf(field+".op", "use eq, ne, lt, lte, gt, gte, set or unset", "ai: invalid state condition operator %q", condition.Op)
	}
	return nil
}

func validateTransition(transition Transition, field string) error {
	switch transition.On {
	case "completed", "succeeded", "failed", "condition", "event":
	default:
		return loaderr.Errorf(field+".on", "use completed, succeeded, failed, condition or event", "ai: invalid transition trigger %q", transition.On)
	}
	if strings.TrimSpace(transition.To) == "" {
		return loaderr.Errorf(field+".to", "set a state path or a control target", "ai: transition target is empty")
	}
	if transition.Delay < 0 {
		return loaderr.Errorf(field+".delay", "use a non-negative number of steps", "ai: transition delay %d is negative", transition.Delay)
	}
	return nil
}
