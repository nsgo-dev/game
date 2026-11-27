package modding

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/tsx/pkg"
	"go.yaml.in/yaml/v3"
)

// Capability is a bitmask of permissions granted to a mod.
// A mod may only call game API operations covered by both its manifest and the host's grant.
type Capability uint32

const (
	// CapNone grants no privileged game API capabilities (hooks and logging remain available).
	CapNone Capability = 0
	// CapReadState allows reading scoped game state via getState.
	CapReadState Capability = 1 << iota
	// CapWriteState allows writing scoped game state via setState.
	CapWriteState
	// CapSpawnEntity allows spawning entities via spawn.
	CapSpawnEntity
	// CapEmitEvent allows emitting game events via emit.
	CapEmitEvent
	// CapUI allows mounting and unmounting TSX UI panels and HUD widgets.
	CapUI
	// CapAudio allows playing sounds through the game bridge and NSGo audio.
	CapAudio
	// CapStorage allows reading/writing persistent per-mod storage (and scoped state writes).
	CapStorage

	// CapAll includes every defined game modding capability.
	CapAll = CapReadState | CapWriteState | CapSpawnEntity | CapEmitEvent | CapUI | CapAudio | CapStorage
)

var capabilityTable = []struct {
	flag Capability
	name string
}{
	{CapReadState, "readState"},
	{CapWriteState, "writeState"},
	{CapSpawnEntity, "spawnEntity"},
	{CapEmitEvent, "emitEvent"},
	{CapUI, "ui"},
	{CapAudio, "audio"},
	{CapStorage, "storage"},
}

// Has reports whether c contains all capabilities in req.
func (c Capability) Has(req Capability) bool {
	return c&req == req
}

// Names returns the canonical names of the capabilities set in c, in deterministic order.
func (c Capability) Names() []string {
	var out []string
	for _, entry := range capabilityTable {
		if c&entry.flag != 0 {
			out = append(out, entry.name)
		}
	}
	return out
}

// String returns a comma-separated list of capability names, or "none" when empty.
func (c Capability) String() string {
	names := c.Names()
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ",")
}

// ParseCapability parses a capability name (camelCase, snake_case, or kebab-case).
func ParseCapability(name string) (Capability, error) {
	norm := strings.ToLower(strings.TrimSpace(name))
	norm = strings.ReplaceAll(norm, "_", "")
	norm = strings.ReplaceAll(norm, "-", "")
	switch norm {
	case "readstate":
		return CapReadState, nil
	case "writestate":
		return CapWriteState, nil
	case "spawnentity":
		return CapSpawnEntity, nil
	case "emitevent":
		return CapEmitEvent, nil
	case "ui":
		return CapUI, nil
	case "audio":
		return CapAudio, nil
	case "storage":
		return CapStorage, nil
	default:
		return CapNone, fmt.Errorf("%w: unknown capability %q", ErrInvalidManifest, name)
	}
}

// MarshalJSON encodes c as a JSON array of canonical capability names.
func (c Capability) MarshalJSON() ([]byte, error) {
	names := c.Names()
	if names == nil {
		names = []string{}
	}
	return json.Marshal(names)
}

// UnmarshalJSON decodes a JSON array of capability names into c.
func (c *Capability) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*c = CapNone
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return fmt.Errorf("%w: capabilities must be an array of strings: %v", ErrInvalidManifest, err)
	}
	var mask Capability
	for _, n := range names {
		flag, err := ParseCapability(n)
		if err != nil {
			return err
		}
		mask |= flag
	}
	*c = mask
	return nil
}

// MarshalYAML encodes c as a flow sequence of canonical capability names in table order, so a manifest's capabilities always serialize the same way regardless of how they were listed.
func (c Capability) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, name := range c.Names() {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	}
	return node, nil
}

// capabilityError returns the *loaderr.Error for a capabilities list that n, its node or one of its items, makes invalid. It matches ErrInvalidManifest with errors.Is.
func capabilityError(n *yaml.Node, format string, args ...any) error {
	e := loaderr.Errorf("capabilities", fixCapabilities, format, args...)
	e.Line, e.Column = n.Line, n.Column
	return e
}

// UnmarshalYAML decodes a YAML sequence of canonical capability names (such as readState) into c. Unknown, non-canonical and repeated names are refused.
func (c *Capability) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode {
		return capabilityError(value, "%w: line %d: capabilities must be a sequence of names", ErrInvalidManifest, value.Line)
	}
	var mask Capability
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return capabilityError(item, "%w: line %d: capability must be a name", ErrInvalidManifest, item.Line)
		}
		flag, ok := capabilityByName(item.Value)
		if !ok {
			return capabilityError(item, "%w: line %d: unknown capability %q", ErrInvalidManifest, item.Line, item.Value)
		}
		if mask&flag != 0 {
			return capabilityError(item, "%w: line %d: capability %q listed twice", ErrInvalidManifest, item.Line, item.Value)
		}
		mask |= flag
	}
	*c = mask
	return nil
}

// capabilityByName looks up a capability by its exact canonical name.
func capabilityByName(name string) (Capability, bool) {
	for _, entry := range capabilityTable {
		if entry.name == name {
			return entry.flag, true
		}
	}
	return CapNone, false
}

// pkgCapabilities translates modding capabilities into NSGo's package sandbox capabilities.
// Network, clipboard, secrets, and other system permissions remain closed by default.
func (c Capability) pkgCapabilities() pkg.Capabilities {
	return pkg.Capabilities{
		Audio: c.Has(CapAudio),
	}
}

// ManifestFile is the name of the manifest file [Manager] reads from each mod's directory.
const ManifestFile = "mod.yaml"

// legacyManifestFile is the JSON manifest name read before mod.yaml; DiscoverFS refuses a mod that has only this file.
const legacyManifestFile = "mod.json"

// ManifestVersion is the mod manifest schema version this package reads and writes.
const ManifestVersion = 1

// Fixes the manifest's errors give.
const (
	fixManifestVersion = "set manifestVersion: 1"
	fixCapabilities    = "list capability names such as [readState, audio]; see Capability for the names"
)

// ModManifest describes a mod. Mod authors write it as YAML in a [ManifestFile] (mod.yaml) in the mod's directory; [ParseManifest] reads it and [EncodeManifest] writes it.
type ModManifest struct {
	// ManifestVersion is the schema version of the manifest file. A mod.yaml must state [ManifestVersion]; a manifest built in code may leave it 0, and [EncodeManifest] writes [ManifestVersion].
	ManifestVersion int `yaml:"manifestVersion" json:"manifestVersion"`
	// ID uniquely identifies the mod, such as com.example.boss-rush.
	ID string `yaml:"id" json:"id"`
	// Name is the mod's human-readable name.
	Name string `yaml:"name" json:"name"`
	// Version is the mod's own semantic version, which dependency constraints are checked against.
	Version string `yaml:"version" json:"version"`
	// Author optionally names the mod's author.
	Author string `yaml:"author,omitempty" json:"author,omitempty"`
	// Description optionally describes the mod.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Entry is the entry script's slash-separated path relative to the mod's directory.
	Entry string `yaml:"entry" json:"entry"`
	// GameVersion optionally constrains the host game versions the mod runs on, such as ">=0.2.0".
	GameVersion string `yaml:"gameVersion,omitempty" json:"gameVersion,omitempty"`
	// Capabilities lists the game API capabilities the mod requests.
	Capabilities Capability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	// Dependencies maps the IDs of mods this mod requires to version constraints.
	Dependencies map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// ParseManifest parses and validates a mod.yaml manifest. Unknown fields, duplicate keys, trailing YAML documents and a missing or unsupported manifestVersion are refused.
//
// Its errors match ErrInvalidManifest (or ErrCyclicDependency) with errors.Is, and are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile, as Manager does.
func ParseManifest(data []byte) (ModManifest, error) {
	m, err := parseManifest(data)
	return m, loaderr.Locate(err, "", data, fixManifest)
}

// fixManifest is the fix for a manifest error that has no more specific one.
const fixManifest = "correct mod.yaml as docs/en/modding.md describes it"

func parseManifest(data []byte) (ModManifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return ModManifest{}, loaderr.Errorf("", fixManifest, "%w: empty manifest", ErrInvalidManifest)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m ModManifest
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, ErrInvalidManifest) {
			// Capability.UnmarshalYAML refused the list and its *loaderr.Error gives the line.
			return ModManifest{}, err
		}
		// Locate finds the decode error in the chain for its line, and keeps the chain, so errors.Is still sees ErrInvalidManifest.
		return ModManifest{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return ModManifest{}, loaderr.Errorf("", "keep one YAML document in mod.yaml; remove the --- and what follows it", "%w: unexpected trailing YAML document", ErrInvalidManifest)
		}
		return ModManifest{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	if m.ManifestVersion != ManifestVersion {
		return ModManifest{}, loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d)", ErrInvalidManifest, m.ManifestVersion, ManifestVersion)
	}
	if err := checkManifestVersionLiteral(data); err != nil {
		return ModManifest{}, err
	}
	if err := m.Validate(); err != nil {
		return ModManifest{}, err
	}
	return m, nil
}

// checkManifestVersionLiteral refuses a manifestVersion that decodes to the supported number but is not written as that plain integer, such as 1.0 or 0x1, so a strict file cannot carry a value that EncodeManifest would silently rewrite.
func checkManifestVersionLiteral(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return loaderr.Errorf("", fixManifest, "%w: manifest must be a mapping", ErrInvalidManifest)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "manifestVersion" {
			continue
		}
		value := root.Content[i+1]
		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || value.Value != strconv.Itoa(ManifestVersion) {
			return loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: line %d: manifestVersion must be the integer %d, not %q", ErrInvalidManifest, value.Line, ManifestVersion, value.Value)
		}
	}
	return nil
}

// EncodeManifest validates m and writes it as deterministic 2-space YAML: fields in a fixed order, capabilities in canonical order and dependencies sorted by ID in byte order. A ManifestVersion of 0 (a manifest built in code) is written as [ManifestVersion]. [ParseManifest] reads the output back as an equal manifest, and encoding that again gives the same bytes.
func EncodeManifest(m ModManifest) ([]byte, error) {
	if m.ManifestVersion == 0 {
		m.ManifestVersion = ManifestVersion
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := node.Encode(m); err != nil {
		return nil, fmt.Errorf("modding: encode manifest: %w", err)
	}
	sortDependencyKeys(&node)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("modding: encode manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("modding: encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// validEntryExtensions are the script extensions supported for a mod's entry file.
var validEntryExtensions = []string{".tsx", ".ts", ".jsx", ".js", ".mjs"}

// sortDependencyKeys orders the dependencies mapping of an encoded manifest by ID in byte order, the same order [ResolveOrder] uses; yaml.v3 itself sorts map keys with digit runs compared as numbers.
func sortDependencyKeys(doc *yaml.Node) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "dependencies" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		deps := root.Content[i+1]
		pairs := make([][2]*yaml.Node, 0, len(deps.Content)/2)
		for j := 0; j+1 < len(deps.Content); j += 2 {
			pairs = append(pairs, [2]*yaml.Node{deps.Content[j], deps.Content[j+1]})
		}
		slices.SortFunc(pairs, func(a, b [2]*yaml.Node) int { return strings.Compare(a[0].Value, b[0].Value) })
		deps.Content = deps.Content[:0]
		for _, p := range pairs {
			deps.Content = append(deps.Content, p[0], p[1])
		}
	}
}

// Validate checks that m has a valid ID, name, version, entry path, capabilities, and version constraints. ManifestVersion may be 0 for a manifest built in code, or [ManifestVersion]; [ParseManifest] additionally requires a file to state [ManifestVersion]. Its errors are *loaderr.Error values that name the field at fault and how to fix it, and match ErrInvalidManifest or ErrCyclicDependency with errors.Is.
func (m ModManifest) Validate() error {
	const (
		fixID         = "use 1 to 128 letters, digits, '.', '-' and '_', starting with a letter or digit and without \"..\", such as \"com.example.more-enemies\""
		fixConstraint = "write a constraint such as \">=1.2.0\", \"^1.2\" or \">=1.0.0 <2.0.0\""
	)
	if m.ManifestVersion != 0 && m.ManifestVersion != ManifestVersion {
		return loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d)", ErrInvalidManifest, m.ManifestVersion, ManifestVersion)
	}
	if !validModID(m.ID) {
		return loaderr.Errorf("id", fixID, "%w: invalid mod id %q", ErrInvalidManifest, m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return loaderr.Errorf("name", "give the mod a name players see", "%w: mod %q requires a non-empty name", ErrInvalidManifest, m.ID)
	}
	if _, err := ParseSemVer(m.Version); err != nil {
		return loaderr.Errorf("version", "write the version as MAJOR.MINOR.PATCH, such as \"1.0.0\"", "%w: mod %q version %q: %v", ErrInvalidManifest, m.ID, m.Version, err)
	}
	if err := validateEntryPath(m.Entry); err != nil {
		return loaderr.Errorf("entry", "name the mod's script as a path inside its directory ending in .tsx, .ts, .jsx, .js or .mjs, such as \"main.ts\"", "%w: mod %q entry %q: %v", ErrInvalidManifest, m.ID, m.Entry, err)
	}
	if m.Capabilities&^CapAll != 0 {
		return loaderr.Errorf("capabilities", fixCapabilities, "%w: mod %q has undefined capability bits %#x", ErrInvalidManifest, m.ID, uint32(m.Capabilities&^CapAll))
	}
	if m.GameVersion != "" {
		if err := ValidateConstraint(m.GameVersion); err != nil {
			return loaderr.Errorf("gameVersion", fixConstraint, "%w: mod %q gameVersion %q: %v", ErrInvalidManifest, m.ID, m.GameVersion, err)
		}
	}
	// Sorted, so the same dependency is reported every time when several are wrong.
	for _, depID := range slices.Sorted(maps.Keys(m.Dependencies)) {
		constraint := m.Dependencies[depID]
		field := loaderr.Path("dependencies", depID)
		if !validModID(depID) {
			return loaderr.Errorf(field, fixID, "%w: mod %q has invalid dependency id %q", ErrInvalidManifest, m.ID, depID)
		}
		if depID == m.ID {
			return loaderr.Errorf(field, "remove the mod's own id from dependencies", "%w: mod %q cannot depend on itself", ErrCyclicDependency, m.ID)
		}
		if err := ValidateConstraint(constraint); err != nil {
			return loaderr.Errorf(field, fixConstraint, "%w: mod %q dependency %q constraint %q: %v", ErrInvalidManifest, m.ID, depID, constraint, err)
		}
	}
	return nil
}

func validModID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	if id[0] == '.' || id[len(id)-1] == '.' || id[0] == '-' || id[0] == '_' {
		return false
	}
	for _, r := range id {
		isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		isPunct := r == '.' || r == '-' || r == '_'
		if !isAlpha && !isDigit && !isPunct {
			return false
		}
	}
	return true
}

func validateEntryPath(entry string) error {
	if strings.TrimSpace(entry) == "" {
		return errors.New("entry is empty")
	}
	if strings.HasPrefix(entry, "/") || strings.Contains(entry, "\\") {
		return errors.New("entry must be a relative slash-separated path")
	}
	cleaned := path.Clean(entry)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("entry escapes mod directory")
	}
	ext := strings.ToLower(path.Ext(cleaned))
	if !slices.Contains(validEntryExtensions, ext) {
		return fmt.Errorf("unsupported entry extension %q (want one of %s)", ext, strings.Join(validEntryExtensions, ", "))
	}
	return nil
}

// SemVer is a parsed semantic version (MAJOR.MINOR.PATCH[-prerelease]).
type SemVer struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// ParseSemVer parses a semantic version string such as "1.2.3" or "0.4.0-beta.1".
func ParseSemVer(raw string) (SemVer, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return SemVer{}, errors.New("empty version")
	}
	// Strip optional build metadata (+...).
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		if pre == "" {
			return SemVer{}, fmt.Errorf("empty prerelease in %q", raw)
		}
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("expected MAJOR.MINOR.PATCH, got %q", raw)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return SemVer{}, fmt.Errorf("invalid numeric segment %q in %q", p, raw)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return SemVer{}, fmt.Errorf("invalid numeric segment %q in %q", p, raw)
		}
		nums[i] = n
	}
	return SemVer{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}, nil
}

// String formats v as MAJOR.MINOR.PATCH[-prerelease].
func (v SemVer) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		return base + "-" + v.Pre
	}
	return base
}

// Compare compares v and other according to SemVer 2.0.0 precedence.
func (v SemVer) Compare(other SemVer) int {
	if c := cmp.Compare(v.Major, other.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, other.Minor); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Patch, other.Patch); c != 0 {
		return c
	}
	if v.Pre == other.Pre {
		return 0
	}
	if v.Pre == "" {
		return 1
	}
	if other.Pre == "" {
		return -1
	}
	a := strings.Split(v.Pre, ".")
	b := strings.Split(other.Pre, ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		ai, aErr := strconv.Atoi(a[i])
		bi, bErr := strconv.Atoi(b[i])
		switch {
		case aErr == nil && bErr == nil:
			return cmp.Compare(ai, bi)
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			return cmp.Compare(a[i], b[i])
		}
	}
	return cmp.Compare(len(a), len(b))
}

// ValidateConstraint checks that constraint is a syntactically valid version constraint.
func ValidateConstraint(constraint string) error {
	_, err := parseConstraintClauses(constraint)
	return err
}

// CheckConstraint reports whether version satisfies constraint.
// Supported operators: *, =, >=, >, <=, <, ^, ~, and space/comma-separated conjunctions.
func CheckConstraint(version, constraint string) (bool, error) {
	v, err := ParseSemVer(version)
	if err != nil {
		return false, err
	}
	clauses, err := parseConstraintClauses(constraint)
	if err != nil {
		return false, err
	}
	for _, cl := range clauses {
		if !cl.matches(v) {
			return false, nil
		}
	}
	return true, nil
}

type versionClause struct {
	op  string
	ver SemVer
}

func parseConstraintClauses(raw string) ([]versionClause, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "*" {
		return nil, nil
	}
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	clauses := make([]versionClause, 0, len(fields))
	for _, f := range fields {
		if f == "*" {
			continue
		}
		op := "="
		rest := f
		for _, prefix := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
			if strings.HasPrefix(f, prefix) {
				op = prefix
				rest = strings.TrimPrefix(f, prefix)
				break
			}
		}
		ver, err := ParseSemVer(rest)
		if err != nil {
			return nil, fmt.Errorf("invalid constraint clause %q: %v", f, err)
		}
		clauses = append(clauses, versionClause{op: op, ver: ver})
	}
	return clauses, nil
}

func (c versionClause) matches(v SemVer) bool {
	switch c.op {
	case "=":
		return v.Compare(c.ver) == 0
	case ">=":
		return v.Compare(c.ver) >= 0
	case ">":
		return v.Compare(c.ver) > 0
	case "<=":
		return v.Compare(c.ver) <= 0
	case "<":
		return v.Compare(c.ver) < 0
	case "^":
		if v.Compare(c.ver) < 0 {
			return false
		}
		var upper SemVer
		switch {
		case c.ver.Major > 0:
			upper = SemVer{Major: c.ver.Major + 1}
		case c.ver.Minor > 0:
			upper = SemVer{Major: 0, Minor: c.ver.Minor + 1}
		default:
			upper = SemVer{Major: 0, Minor: 0, Patch: c.ver.Patch + 1}
		}
		return v.Compare(upper) < 0
	case "~":
		if v.Compare(c.ver) < 0 {
			return false
		}
		upper := SemVer{Major: c.ver.Major, Minor: c.ver.Minor + 1}
		return v.Compare(upper) < 0
	default:
		return false
	}
}

// ResolveOrder validates manifests and returns them in deterministic topological load order
// (every dependency precedes every mod that depends on it, with lexicographical ID tie-breaking).
// It rejects duplicate IDs, missing dependencies, version mismatches, and dependency cycles.
func ResolveOrder(manifests []ModManifest) ([]ModManifest, error) {
	byID := make(map[string]ModManifest, len(manifests))
	ids := make([]string, 0, len(manifests))
	for _, m := range manifests {
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if _, dup := byID[m.ID]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateMod, m.ID)
		}
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)

	inDegree := make(map[string]int, len(manifests))
	dependents := make(map[string][]string, len(manifests))
	for _, id := range ids {
		m := byID[id]
		depIDs := make([]string, 0, len(m.Dependencies))
		for depID := range m.Dependencies {
			depIDs = append(depIDs, depID)
		}
		slices.Sort(depIDs)
		for _, depID := range depIDs {
			dep, ok := byID[depID]
			if !ok {
				return nil, fmt.Errorf("%w: mod %q requires missing dependency %q", ErrMissingDependency, m.ID, depID)
			}
			constraint := m.Dependencies[depID]
			okVer, err := CheckConstraint(dep.Version, constraint)
			if err != nil {
				return nil, fmt.Errorf("%w: mod %q dependency %q: %v", ErrInvalidManifest, m.ID, depID, err)
			}
			if !okVer {
				return nil, fmt.Errorf("%w: mod %q requires %q %s, found %s", ErrVersionMismatch, m.ID, depID, constraint, dep.Version)
			}
			dependents[depID] = append(dependents[depID], id)
			inDegree[id]++
		}
	}

	var ready []string
	for _, id := range ids {
		if inDegree[id] == 0 {
			ready = append(ready, id)
		}
	}

	ordered := make([]ModManifest, 0, len(manifests))
	for len(ready) > 0 {
		// Always pop the lexicographically smallest ready ID for deterministic order.
		curr := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[curr])
		for _, next := range dependents[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				// Keep ready sorted.
				idx, _ := slices.BinarySearch(ready, next)
				ready = slices.Insert(ready, idx, next)
			}
		}
	}

	if len(ordered) != len(manifests) {
		var cyclic []string
		for _, id := range ids {
			if inDegree[id] > 0 {
				cyclic = append(cyclic, id)
			}
		}
		return nil, fmt.Errorf("%w: cycle involving [%s]", ErrCyclicDependency, strings.Join(cyclic, ", "))
	}

	return ordered, nil
}
