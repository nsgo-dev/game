package sceneformat

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ConflictVersion is the schema version of the YAML conflict file written by
// the scene merge driver when a three-way merge encounters conflicts.
const ConflictVersion = 1

// ConflictKind classifies why a three-way scene merge entry conflicted.
type ConflictKind string

const (
	// ConflictField means both branches changed the same field to different values.
	ConflictField ConflictKind = "field"
	// ConflictDeleteModify means one branch deleted an item while the other modified it.
	ConflictDeleteModify ConflictKind = "delete-modify"
	// ConflictAddAdd means both branches added an item with the same identity and different contents.
	ConflictAddAdd ConflictKind = "add-add"
	// ConflictMove means both branches moved the same object to different parents or layers, or combined moves formed a cycle.
	ConflictMove ConflictKind = "move"
	// ConflictValidation means the merged scene failed SceneDoc validation.
	ConflictValidation ConflictKind = "validation"
)

// ConflictDoc is the versioned, deterministic YAML document left behind when a
// three-way scene merge cannot finish cleanly.
type ConflictDoc struct {
	// Version is ConflictVersion (1).
	Version int `yaml:"version"`
	// Path is the scene's slash-separated path in the repository (git's %P), when known.
	Path string `yaml:"path,omitempty"`
	// Merged lists items the merge driver joined cleanly by identity from one or both sides.
	Merged []MergedItem `yaml:"merged,omitempty"`
	// Conflicts lists every unresolved conflict in deterministic order.
	Conflicts []MergeConflict `yaml:"conflicts"`
}

// MergedItem records one scene item that the merge driver combined cleanly by
// identity without a conflict.
type MergedItem struct {
	// Scope is "scene", "layer", "object", "connection", "tilemap", "override" or "layerOverride".
	Scope string `yaml:"scope"`
	// Target identifies the item: object ID, layer name, connection signature, or map key.
	Target string `yaml:"target,omitempty"`
	// Side is "ours", "theirs" or "both".
	Side string `yaml:"side"`
	// Action is "added", "changed", "deleted" or "moved".
	Action string `yaml:"action"`
}

// MergeConflict describes one unresolved conflict in a three-way scene merge.
type MergeConflict struct {
	// Kind is ConflictField, ConflictDeleteModify, ConflictAddAdd, ConflictMove or ConflictValidation.
	Kind ConflictKind `yaml:"kind"`
	// Scope is "scene", "layer", "object", "connection", "tilemap", "override" or "layerOverride".
	Scope string `yaml:"scope"`
	// Layer names the layer when the conflict is on a layer or an object inside a layer.
	Layer string `yaml:"layer,omitempty"`
	// Object is the object's ID when Scope is "object".
	Object string `yaml:"object,omitempty"`
	// Target identifies non-object items (such as a connection signature, tilemap name or override key).
	Target string `yaml:"target,omitempty"`
	// Field names the conflicting field path (such as "x", "components.patrol.speed", or "parent").
	Field string `yaml:"field,omitempty"`
	// Message describes the conflict in plain text when needed (such as delete/modify or validation errors).
	Message string `yaml:"message,omitempty"`
	// Base is the ancestor value, or nil when absent.
	Base any `yaml:"base"`
	// Ours is the current branch's value, or nil when absent or deleted.
	Ours any `yaml:"ours"`
	// Theirs is the other branch's value, or nil when absent or deleted.
	Theirs any `yaml:"theirs"`
}

// ParseConflict decodes and validates a YAML scene merge conflict document.
// Unknown fields, unsupported versions and trailing YAML documents are refused.
func ParseConflict(data []byte) (*ConflictDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("sceneformat: empty conflict document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc ConflictDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("sceneformat: decode conflict: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("sceneformat: conflict document has an unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("sceneformat: decode conflict: %w", err)
	}
	if err := ValidateConflict(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// ValidateConflict checks that doc conforms to ConflictVersion's schema.
func ValidateConflict(doc *ConflictDoc) error {
	if doc == nil {
		return fmt.Errorf("sceneformat: nil conflict document")
	}
	if doc.Version != ConflictVersion {
		return fmt.Errorf("sceneformat: unsupported conflict version %d (want %d)", doc.Version, ConflictVersion)
	}
	if len(doc.Conflicts) == 0 {
		return fmt.Errorf("sceneformat: conflict document must list at least one conflict")
	}
	for i, m := range doc.Merged {
		if !validConflictScope(m.Scope) {
			return fmt.Errorf("sceneformat: merged[%d] has invalid scope %q", i, m.Scope)
		}
		switch m.Side {
		case "ours", "theirs", "both":
		default:
			return fmt.Errorf("sceneformat: merged[%d] has invalid side %q", i, m.Side)
		}
		switch m.Action {
		case "added", "changed", "deleted", "moved":
		default:
			return fmt.Errorf("sceneformat: merged[%d] has invalid action %q", i, m.Action)
		}
	}
	for i, c := range doc.Conflicts {
		switch c.Kind {
		case ConflictField, ConflictDeleteModify, ConflictAddAdd, ConflictMove, ConflictValidation:
		default:
			return fmt.Errorf("sceneformat: conflicts[%d] has invalid kind %q", i, c.Kind)
		}
		if !validConflictScope(c.Scope) {
			return fmt.Errorf("sceneformat: conflicts[%d] has invalid scope %q", i, c.Scope)
		}
	}
	return nil
}

func validConflictScope(s string) bool {
	switch s {
	case "scene", "layer", "object", "connection", "tilemap", "override", "layerOverride":
		return true
	default:
		return false
	}
}

// EncodeConflict validates doc and serializes it as deterministic 2-space YAML.
func EncodeConflict(doc *ConflictDoc) ([]byte, error) {
	if err := ValidateConflict(doc); err != nil {
		return nil, err
	}
	norm := normalizeConflictDoc(doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(norm); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("sceneformat: encode conflict: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("sceneformat: encode conflict: %w", err)
	}
	return buf.Bytes(), nil
}

func normalizeConflictDoc(doc *ConflictDoc) *ConflictDoc {
	out := &ConflictDoc{
		Version: doc.Version,
		Path:    doc.Path,
	}
	if len(doc.Merged) > 0 {
		out.Merged = slices.Clone(doc.Merged)
		sortMergedItems(out.Merged)
	}
	if len(doc.Conflicts) > 0 {
		out.Conflicts = make([]MergeConflict, len(doc.Conflicts))
		for i, c := range doc.Conflicts {
			c.Base = floatValue(c.Base)
			c.Ours = floatValue(c.Ours)
			c.Theirs = floatValue(c.Theirs)
			out.Conflicts[i] = c
		}
		sortConflicts(out.Conflicts)
	}
	return out
}

func sortMergedItems(items []MergedItem) {
	slices.SortFunc(items, func(a, b MergedItem) int {
		if c := cmp.Compare(scopeRank(a.Scope), scopeRank(b.Scope)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Scope, b.Scope); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Action, b.Action); c != 0 {
			return c
		}
		return cmp.Compare(a.Side, b.Side)
	})
}

func sortConflicts(conflicts []MergeConflict) {
	slices.SortFunc(conflicts, func(a, b MergeConflict) int {
		if c := cmp.Compare(scopeRank(a.Scope), scopeRank(b.Scope)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Scope, b.Scope); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Layer, b.Layer); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Field, b.Field); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
}

func scopeRank(scope string) int {
	switch scope {
	case "scene":
		return 0
	case "tilemap":
		return 1
	case "layerOverride":
		return 2
	case "override":
		return 3
	case "layer":
		return 4
	case "object":
		return 5
	case "connection":
		return 6
	default:
		return 7
	}
}

// formatConflictSummary returns a single-line human-readable description of c for CLI diagnostics.
func (c MergeConflict) Summary() string {
	var where strings.Builder
	where.WriteString(c.Scope)
	switch {
	case c.Object != "":
		where.WriteString(" ")
		where.WriteString(c.Object)
	case c.Target != "":
		where.WriteString(" ")
		where.WriteString(c.Target)
	case c.Layer != "":
		where.WriteString(" ")
		where.WriteString(c.Layer)
	}
	if c.Field != "" {
		where.WriteString(".")
		where.WriteString(c.Field)
	}
	if c.Message != "" {
		return fmt.Sprintf("%s (%s): %s", where.String(), c.Kind, c.Message)
	}
	return fmt.Sprintf("%s (%s)", where.String(), c.Kind)
}
