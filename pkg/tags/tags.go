// Package tags defines a project-wide vocabulary of hierarchical gameplay tags.
// Tags are interned by Registry and Container keeps reference counts for grants.
package tags

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const CurrentVersion = 1
const maxTags = 16384

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)*$`)

// Tag is an interned identifier allocated by a Registry. The zero value is invalid.
type Tag uint32

func (t Tag) Valid() bool { return t != 0 }

// Def declares one tag. Parent tags are implied by dotted names.
type Def struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// Set is a versioned tag registry file.
type Set struct {
	Version int   `yaml:"version"`
	Tags    []Def `yaml:"tags"`
}

// Registry resolves names from a Set into stable interned Tags.
type Registry struct {
	set     *Set
	byName  map[string]Tag
	parents map[Tag]Tag
	names   []string
}

// NewRegistry builds a registry from declarations. It is useful for tests and generated project data.
func NewRegistry(defs ...Def) (*Registry, error) {
	s := &Set{Version: CurrentVersion, Tags: append([]Def(nil), defs...)}
	return New(s)
}

// New validates set and builds its intern table.
func New(set *Set) (*Registry, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	r := &Registry{set: set, byName: make(map[string]Tag, len(set.Tags)), parents: make(map[Tag]Tag, len(set.Tags))}
	for _, d := range set.Tags {
		parts := strings.Split(d.Name, ".")
		for i := 1; i <= len(parts); i++ {
			name := strings.Join(parts[:i], ".")
			if _, ok := r.byName[name]; !ok {
				r.names = append(r.names, name)
				r.byName[name] = Tag(len(r.names))
			}
		}
	}
	for name, t := range r.byName {
		if j := strings.LastIndexByte(name, '.'); j >= 0 {
			r.parents[t] = r.byName[name[:j]]
		}
	}
	return r, nil
}

// Set returns the immutable declarations used to build r.
func (r *Registry) Set() *Set {
	if r == nil {
		return nil
	}
	return r.set
}

// Resolve returns a tag and whether name is declared.
func (r *Registry) Resolve(name string) (Tag, bool) {
	if r == nil {
		return 0, false
	}
	t, ok := r.byName[name]
	return t, ok
}

// MustResolve resolves name or panics. Use it for static declarations.
func (r *Registry) MustResolve(name string) Tag {
	t, ok := r.Resolve(name)
	if !ok {
		panic("tags: undeclared tag " + name)
	}
	return t
}

// Name returns the declared name for t.
func (r *Registry) Name(t Tag) string {
	if r == nil || t == 0 || int(t) > len(r.names) {
		return ""
	}
	return r.names[int(t)-1]
}
func (r *Registry) parent(t Tag) Tag {
	if r == nil {
		return 0
	}
	return r.parents[t]
}

// Parse decodes and validates a version 1 registry.
func Parse(data []byte) (*Set, error) {
	s, err := parse(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, "write tags as version: 1 YAML with unique dotted names")
	}
	return s, nil
}
func parse(data []byte) (*Set, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("tags: empty registry")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s Set
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("tags: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("tags: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("tags: decode: %w", err)
	}
	if err := Validate(&s); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&s)
	sort.SliceStable(s.Tags, func(i, j int) bool { return s.Tags[i].Name < s.Tags[j].Name })
	if s.Tags == nil {
		s.Tags = []Def{}
	}
	return &s, nil
}

// Encode writes deterministic two-space YAML.
func Encode(s *Set) ([]byte, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}
	canonical := *s
	canonical.Tags = append([]Def(nil), s.Tags...)
	sort.SliceStable(canonical.Tags, func(i, j int) bool { return canonical.Tags[i].Name < canonical.Tags[j].Name })
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(yamlzero.Clean(&canonical)); err != nil {
		_ = e.Close()
		return nil, fmt.Errorf("tags: encode: %w", err)
	}
	if err := e.Close(); err != nil {
		return nil, fmt.Errorf("tags: encode: %w", err)
	}
	return b.Bytes(), nil
}

// Validate checks names, explicit parents and uniqueness.
func Validate(s *Set) error {
	if s == nil {
		return errors.New("tags: nil registry")
	}
	if s.Version != CurrentVersion {
		return fmt.Errorf("tags: unsupported version %d (want %d)", s.Version, CurrentVersion)
	}
	if len(s.Tags) > maxTags {
		return fmt.Errorf("tags: %d entries, more than %d", len(s.Tags), maxTags)
	}
	seen := make(map[string]bool, len(s.Tags))
	for i, d := range s.Tags {
		if !namePattern.MatchString(d.Name) {
			return fmt.Errorf("tags: tags[%d].name %q is not a dotted tag", i, d.Name)
		}
		if seen[d.Name] {
			return fmt.Errorf("tags: duplicate tag %q", d.Name)
		}
		seen[d.Name] = true
	}
	return nil
}

// Container holds reference-counted grants from one entity.
type Container struct {
	registry *Registry
	counts   map[Tag]uint16
}

func NewContainer(r *Registry) *Container {
	return &Container{registry: r, counts: make(map[Tag]uint16)}
}
func (c *Container) Add(t Tag) {
	if c == nil || !t.Valid() {
		return
	}
	if c.counts == nil {
		c.counts = make(map[Tag]uint16)
	}
	if c.counts[t] < ^uint16(0) {
		c.counts[t]++
	}
}
func (c *Container) Remove(t Tag) {
	if c == nil {
		return
	}
	if n := c.counts[t]; n > 1 {
		c.counts[t] = n - 1
	} else if n == 1 {
		delete(c.counts, t)
	}
}
func (c *Container) Count(t Tag) int {
	if c == nil {
		return 0
	}
	return int(c.counts[t])
}
func (c *Container) HasExact(t Tag) bool { return c != nil && c.counts[t] > 0 }

// Has reports true for t or any descendant of t.
func (c *Container) Has(t Tag) bool {
	if c == nil || !t.Valid() {
		return false
	}
	for got, n := range c.counts {
		if n == 0 {
			continue
		}
		for cur := got; cur.Valid(); cur = c.registry.parent(cur) {
			if cur == t {
				return true
			}
		}
	}
	return false
}

// Query is a tag predicate. All, Any and None use parent matching. The *Of fields compose nested predicates without allocating during Match.
type Query struct {
	All    []Tag
	Any    []Tag
	None   []Tag
	AllOf  []Query
	AnyOf  []Query
	NoneOf []Query
}

func (q Query) Match(c *Container) bool {
	for _, t := range q.All {
		if !c.Has(t) {
			return false
		}
	}
	for _, n := range q.None {
		if c.Has(n) {
			return false
		}
	}
	for _, child := range q.AllOf {
		if !child.Match(c) {
			return false
		}
	}
	for _, child := range q.NoneOf {
		if child.Match(c) {
			return false
		}
	}
	if len(q.Any) > 0 {
		matched := false
		for _, t := range q.Any {
			if c.Has(t) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(q.AnyOf) > 0 {
		matched := false
		for _, child := range q.AnyOf {
			if child.Match(c) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// QueryNames converts a name-based query into an interned Query.
type QueryNames struct {
	All  []string `yaml:"all,omitempty"`
	Any  []string `yaml:"any,omitempty"`
	None []string `yaml:"none,omitempty"`
}

func (r *Registry) QueryNames(q QueryNames) (Query, error) {
	out := Query{}
	for _, part := range []struct {
		names []string
		dst   *[]Tag
	}{{q.All, &out.All}, {q.Any, &out.Any}, {q.None, &out.None}} {
		for _, name := range part.names {
			t, ok := r.Resolve(name)
			if !ok {
				return Query{}, fmt.Errorf("tags: undeclared tag %q", name)
			}
			*part.dst = append(*part.dst, t)
		}
	}
	return out, nil
}
