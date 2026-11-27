package modding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/internal/semver"
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

const (
	// MinManifestVersion is the oldest mod manifest schema version this package reads and writes (1).
	MinManifestVersion = 1
	// ManifestVersion is the newest mod manifest schema version this package reads and writes (2), adding optionalDependencies, conflicts, platforms, and contributes.
	ManifestVersion = 2

	// SchemaURL is where the JSON Schema of version 1 mod.yaml manifests is published.
	SchemaURL = "https://game.nsgo.dev/schemas/mod.v1.json"
	// SchemaURLV2 is where the JSON Schema of version 2 mod.yaml manifests is published.
	SchemaURLV2 = "https://game.nsgo.dev/schemas/mod.v2.json"
)

// validPlatforms are the canonical NSGo target platform names accepted in a v2 manifest's platforms field.
var validPlatforms = []string{
	"macos",
	"windows",
	"linux",
	"ios",
	"tvos",
	"watchos",
	"android",
	"harmonyos",
	"web",
	"miniprogram",
}

// SupportedPlatforms returns a copy of the canonical target platform names accepted in a v2 manifest's platforms field.
func SupportedPlatforms() []string {
	return slices.Clone(validPlatforms)
}

// Fixes the manifest's errors give.
const (
	fixManifestVersion = "set manifestVersion: 1 or manifestVersion: 2"
	fixManifestV2      = "set manifestVersion: 2 to use version 2 manifest fields"
	fixCapabilities    = "list built-in capability names such as [readState, audio] or dotted host names such as [inventory.read]"
	fixPlatforms       = "use supported lowercase platform names such as [windows, macos, linux]"
	fixContributesDir  = "name a relative slash-separated directory inside the mod, such as \"assets\" or \"locales\""
)

// ValidHostCapability reports whether name is a valid host capability name:
// two or more dot-separated lower camel case ASCII segments (such as "inventory.read" or "editor.contextAction").
func ValidHostCapability(name string) bool {
	if len(name) == 0 || len(name) > 128 || !strings.Contains(name, ".") {
		return false
	}
	for seg := range strings.SplitSeq(name, ".") {
		if len(seg) == 0 {
			return false
		}
		if seg[0] < 'a' || seg[0] > 'z' {
			return false
		}
		for i := 1; i < len(seg); i++ {
			c := seg[i]
			isLower := c >= 'a' && c <= 'z'
			isUpper := c >= 'A' && c <= 'Z'
			isDigit := c >= '0' && c <= '9'
			if !isLower && !isUpper && !isDigit {
				return false
			}
		}
	}
	return true
}

// ModContributes declares read-only content directories that a manifestVersion 2 mod overlays onto the host game via [Manager.ContentFS]. Each non-empty field is a slash-separated directory path relative to the mod's directory.
type ModContributes struct {
	// Assets is the mod-relative directory of asset files (images, audio, fonts, models) overlaid under "assets/" in ContentFS.
	Assets string `yaml:"assets,omitempty" json:"assets,omitempty"`
	// Scenes is the mod-relative directory of scene files overlaid under "scenes/" in ContentFS.
	Scenes string `yaml:"scenes,omitempty" json:"scenes,omitempty"`
	// L10n is the mod-relative directory of localization string tables overlaid under "l10n/" in ContentFS.
	L10n string `yaml:"l10n,omitempty" json:"l10n,omitempty"`
	// DataTables is the mod-relative directory of data tables overlaid under "datatables/" in ContentFS.
	DataTables string `yaml:"datatables,omitempty" json:"datatables,omitempty"`
}

// IsEmpty reports whether c declares no content directories.
func (c ModContributes) IsEmpty() bool {
	return c.Assets == "" && c.Scenes == "" && c.L10n == "" && c.DataTables == ""
}

// ModManifest describes a mod. Mod authors write it as YAML in a [ManifestFile] (mod.yaml) in the mod's directory; [ParseManifest] reads it and [EncodeManifest] writes it.
type ModManifest struct {
	// ManifestVersion is the schema version of the manifest file (1 or 2). A mod.yaml must state [MinManifestVersion] or [ManifestVersion]; a manifest built in code may leave it 0, and [EncodeManifest] writes [ManifestVersion].
	ManifestVersion int `yaml:"manifestVersion" json:"manifestVersion" schema:"required"`
	// ID uniquely identifies the mod, such as com.example.boss-rush.
	ID string `yaml:"id" json:"id" schema:"required,minLength=1"`
	// Name is the mod's human-readable name.
	Name string `yaml:"name" json:"name" schema:"required,minLength=1"`
	// Version is the mod's own semantic version, which dependency constraints are checked against.
	Version string `yaml:"version" json:"version" schema:"required,minLength=1"`
	// Author optionally names the mod's author.
	Author string `yaml:"author,omitempty" json:"author,omitempty"`
	// Description optionally describes the mod.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Entry is the entry script's slash-separated path relative to the mod's directory. Required in manifestVersion 1; optional in manifestVersion 2 when contributes declares at least one content directory.
	Entry string `yaml:"entry,omitempty" json:"entry,omitempty"`
	// GameVersion optionally constrains the host game versions the mod runs on, such as ">=0.2.0".
	GameVersion string `yaml:"gameVersion,omitempty" json:"gameVersion,omitempty"`
	// Capabilities lists the built-in game API capabilities the mod requests. In mod.yaml, the capabilities sequence may also contain dotted host capability names (<namespace>.<name>), which [ParseManifest] decodes into [ModManifest.HostCapabilities] and [EncodeManifest] writes after the built-in names in sorted order.
	Capabilities Capability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	// HostCapabilities lists the dotted host capability names (such as "inventory.read") requested in mod.yaml's capabilities sequence, sorted in byte order.
	HostCapabilities []string `yaml:"-" json:"hostCapabilities,omitempty"`
	// Dependencies maps the IDs of mods this mod requires to version constraints.
	Dependencies map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	// OptionalDependencies maps the IDs of optional mods to version constraints (manifestVersion 2). When present, an optional dependency loads before this mod and its version must satisfy the constraint; when absent, it is ignored.
	OptionalDependencies map[string]string `yaml:"optionalDependencies,omitempty" json:"optionalDependencies,omitempty"`
	// Conflicts maps the IDs of incompatible mods to version constraints (manifestVersion 2). A conflicting pair is refused together in either direction.
	Conflicts map[string]string `yaml:"conflicts,omitempty" json:"conflicts,omitempty"`
	// Platforms lists the target platforms the mod runs on, such as [windows, macos, linux] (manifestVersion 2). When omitted, the mod runs on all platforms.
	Platforms []string `yaml:"platforms,omitempty" json:"platforms,omitempty"`
	// Contributes declares mod-relative content directories overlaid into [Manager.ContentFS] (manifestVersion 2).
	Contributes ModContributes `yaml:"contributes,omitempty" json:"contributes,omitempty"`
}

type manifestCaps struct {
	caps Capability
	host []string
}

func (c manifestCaps) IsZero() bool {
	return c.caps == CapNone && len(c.host) == 0
}

func (c manifestCaps) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, name := range c.caps.Names() {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	}
	host := slices.Clone(c.host)
	slices.Sort(host)
	for _, name := range host {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	}
	return node, nil
}

func (c *manifestCaps) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode {
		return capabilityError(value, "%w: line %d: capabilities must be a sequence of names", ErrInvalidManifest, value.Line)
	}
	var mask Capability
	var host []string
	var seenHost map[string]struct{}
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return capabilityError(item, "%w: line %d: capability must be a name", ErrInvalidManifest, item.Line)
		}
		if strings.Contains(item.Value, ".") {
			if !ValidHostCapability(item.Value) {
				return capabilityError(item, "%w: line %d: invalid host capability %q", ErrInvalidManifest, item.Line, item.Value)
			}
			if seenHost == nil {
				seenHost = map[string]struct{}{}
			}
			if _, dup := seenHost[item.Value]; dup {
				return capabilityError(item, "%w: line %d: capability %q listed twice", ErrInvalidManifest, item.Line, item.Value)
			}
			seenHost[item.Value] = struct{}{}
			host = append(host, item.Value)
			continue
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
	slices.Sort(host)
	c.caps = mask
	c.host = host
	return nil
}

type manifestWire struct {
	ManifestVersion      int               `yaml:"manifestVersion"`
	ID                   string            `yaml:"id"`
	Name                 string            `yaml:"name"`
	Version              string            `yaml:"version"`
	Author               string            `yaml:"author,omitempty"`
	Description          string            `yaml:"description,omitempty"`
	Entry                string            `yaml:"entry,omitempty"`
	GameVersion          string            `yaml:"gameVersion,omitempty"`
	Capabilities         manifestCaps      `yaml:"capabilities,omitempty"`
	Dependencies         map[string]string `yaml:"dependencies,omitempty"`
	OptionalDependencies map[string]string `yaml:"optionalDependencies,omitempty"`
	Conflicts            map[string]string `yaml:"conflicts,omitempty"`
	Platforms            []string          `yaml:"platforms,omitempty"`
	Contributes          ModContributes    `yaml:"contributes,omitempty"`
}

// ParseManifest parses and validates a mod.yaml manifest (version 1 or 2). Unknown fields, duplicate keys, trailing YAML documents, version 2 fields in a version 1 file, and a missing or unsupported manifestVersion are refused.
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
	var wire manifestWire
	if err := dec.Decode(&wire); err != nil {
		if errors.Is(err, ErrInvalidManifest) {
			// manifestCaps.UnmarshalYAML refused the list and its *loaderr.Error gives the line.
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
	m := ModManifest{
		ManifestVersion:      wire.ManifestVersion,
		ID:                   wire.ID,
		Name:                 wire.Name,
		Version:              wire.Version,
		Author:               wire.Author,
		Description:          wire.Description,
		Entry:                wire.Entry,
		GameVersion:          wire.GameVersion,
		Capabilities:         wire.Capabilities.caps,
		HostCapabilities:     wire.Capabilities.host,
		Dependencies:         wire.Dependencies,
		OptionalDependencies: wire.OptionalDependencies,
		Conflicts:            wire.Conflicts,
		Platforms:            wire.Platforms,
		Contributes:          wire.Contributes,
	}
	if m.ManifestVersion < MinManifestVersion || m.ManifestVersion > ManifestVersion {
		return ModManifest{}, loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d..%d)", ErrInvalidManifest, m.ManifestVersion, MinManifestVersion, ManifestVersion)
	}
	if err := checkManifestYAML(data, m.ManifestVersion); err != nil {
		return ModManifest{}, err
	}
	if err := m.Validate(); err != nil {
		return ModManifest{}, err
	}
	return m, nil
}

// v2ManifestKeys are the top-level keys introduced in manifestVersion 2.
var v2ManifestKeys = []string{"optionalDependencies", "conflicts", "platforms", "contributes"}

// checkManifestYAML refuses a manifestVersion that decodes to version but is not written as that plain integer (such as 1.0 or 0x2), and refuses any version 2 top-level key in a version 1 file (even when empty, such as platforms: []).
func checkManifestYAML(data []byte, version int) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return loaderr.Errorf("", fixManifest, "%w: manifest must be a mapping", ErrInvalidManifest)
	}
	root := doc.Content[0]
	wantLit := strconv.Itoa(version)
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		value := root.Content[i+1]
		if key == "manifestVersion" {
			if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || value.Value != wantLit {
				return loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: line %d: manifestVersion must be the integer %d, not %q", ErrInvalidManifest, value.Line, version, value.Value)
			}
		}
		if version == MinManifestVersion && slices.Contains(v2ManifestKeys, key) {
			e := loaderr.Errorf(key, fixManifestV2, "%w: line %d: %s requires manifestVersion %d", ErrInvalidManifest, root.Content[i].Line, key, ManifestVersion)
			e.Line, e.Column = root.Content[i].Line, root.Content[i].Column
			return e
		}
	}
	return nil
}

// EncodeManifest validates m and writes it as deterministic 2-space YAML: fields in a fixed order, built-in capabilities in canonical order followed by host capabilities sorted in byte order, maps (dependencies, optionalDependencies, conflicts) sorted by ID in byte order, and platforms as a flow sequence sorted in byte order. A ManifestVersion of 0 (a manifest built in code) is written as [ManifestVersion]; version 1 and version 2 manifests preserve their ManifestVersion so existing v1 files round-trip byte for byte.
func EncodeManifest(m ModManifest) ([]byte, error) {
	if m.ManifestVersion == 0 {
		m.ManifestVersion = ManifestVersion
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	platforms := m.Platforms
	if len(platforms) > 0 {
		platforms = slices.Clone(platforms)
		slices.Sort(platforms)
	}
	wire := manifestWire{
		ManifestVersion: m.ManifestVersion,
		ID:              m.ID,
		Name:            m.Name,
		Version:         m.Version,
		Author:          m.Author,
		Description:     m.Description,
		Entry:           m.Entry,
		GameVersion:     m.GameVersion,
		Capabilities: manifestCaps{
			caps: m.Capabilities,
			host: m.HostCapabilities,
		},
		Dependencies:         m.Dependencies,
		OptionalDependencies: m.OptionalDependencies,
		Conflicts:            m.Conflicts,
		Platforms:            platforms,
		Contributes:          m.Contributes,
	}
	var node yaml.Node
	if err := node.Encode(wire); err != nil {
		return nil, fmt.Errorf("modding: encode manifest: %w", err)
	}
	formatManifestNode(&node)
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

// formatManifestNode orders the dependencies, optionalDependencies, and conflicts mappings of an encoded manifest by ID in byte order (matching ResolveOrder; yaml.v3 sorts map keys with digit runs compared as numbers) and formats platforms as a flow sequence.
func formatManifestNode(doc *yaml.Node) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]
		switch key {
		case "dependencies", "optionalDependencies", "conflicts":
			if val.Kind != yaml.MappingNode {
				continue
			}
			pairs := make([][2]*yaml.Node, 0, len(val.Content)/2)
			for j := 0; j+1 < len(val.Content); j += 2 {
				pairs = append(pairs, [2]*yaml.Node{val.Content[j], val.Content[j+1]})
			}
			slices.SortFunc(pairs, func(a, b [2]*yaml.Node) int { return strings.Compare(a[0].Value, b[0].Value) })
			val.Content = val.Content[:0]
			for _, p := range pairs {
				val.Content = append(val.Content, p[0], p[1])
			}
		case "platforms":
			if val.Kind == yaml.SequenceNode {
				val.Style = yaml.FlowStyle
			}
		}
	}
}

// Validate checks that m has a valid schema version, ID, name, version, entry path (or contributes in v2), capabilities, version constraints, platforms, and content directory paths. ManifestVersion may be 0 for a manifest built in code, or [MinManifestVersion]..[ManifestVersion]; [ParseManifest] additionally requires a file to state [MinManifestVersion] or [ManifestVersion]. Its errors are *loaderr.Error values that name the field at fault and how to fix it, and match ErrInvalidManifest or ErrCyclicDependency with errors.Is.
func (m ModManifest) Validate() error {
	const (
		fixID         = "use 1 to 128 letters, digits, '.', '-' and '_', starting with a letter or digit and without \"..\", such as \"com.example.more-enemies\""
		fixConstraint = "write a constraint such as \">=1.2.0\", \"^1.2\" or \">=1.0.0 <2.0.0\""
	)
	if m.ManifestVersion != 0 && (m.ManifestVersion < MinManifestVersion || m.ManifestVersion > ManifestVersion) {
		return loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d..%d)", ErrInvalidManifest, m.ManifestVersion, MinManifestVersion, ManifestVersion)
	}
	if m.ManifestVersion == MinManifestVersion {
		if len(m.OptionalDependencies) > 0 {
			return loaderr.Errorf("optionalDependencies", fixManifestV2, "%w: optionalDependencies requires manifestVersion %d", ErrInvalidManifest, ManifestVersion)
		}
		if len(m.Conflicts) > 0 {
			return loaderr.Errorf("conflicts", fixManifestV2, "%w: conflicts requires manifestVersion %d", ErrInvalidManifest, ManifestVersion)
		}
		if len(m.Platforms) > 0 {
			return loaderr.Errorf("platforms", fixManifestV2, "%w: platforms requires manifestVersion %d", ErrInvalidManifest, ManifestVersion)
		}
		if !m.Contributes.IsEmpty() {
			return loaderr.Errorf("contributes", fixManifestV2, "%w: contributes requires manifestVersion %d", ErrInvalidManifest, ManifestVersion)
		}
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
	if m.Entry != "" {
		if err := validateEntryPath(m.Entry); err != nil {
			return loaderr.Errorf("entry", "name the mod's script as a path inside its directory ending in .tsx, .ts, .jsx, .js or .mjs, such as \"main.ts\"", "%w: mod %q entry %q: %v", ErrInvalidManifest, m.ID, m.Entry, err)
		}
	} else if m.ManifestVersion == MinManifestVersion || m.Contributes.IsEmpty() {
		fix := "name the mod's script as a path inside its directory ending in .tsx, .ts, .jsx, .js or .mjs, such as \"main.ts\""
		if m.ManifestVersion != MinManifestVersion {
			fix = "set entry to a script path, or declare at least one directory under contributes for a content-only mod"
		}
		return loaderr.Errorf("entry", fix, "%w: mod %q entry %q: entry is empty", ErrInvalidManifest, m.ID, m.Entry)
	}
	if m.Capabilities&^CapAll != 0 {
		return loaderr.Errorf("capabilities", fixCapabilities, "%w: mod %q has undefined capability bits %#x", ErrInvalidManifest, m.ID, uint32(m.Capabilities&^CapAll))
	}
	if len(m.HostCapabilities) > 0 {
		seen := make(map[string]struct{}, len(m.HostCapabilities))
		for _, hc := range m.HostCapabilities {
			if !ValidHostCapability(hc) {
				return loaderr.Errorf("capabilities", fixCapabilities, "%w: mod %q has invalid host capability %q", ErrInvalidManifest, m.ID, hc)
			}
			if _, dup := seen[hc]; dup {
				return loaderr.Errorf("capabilities", fixCapabilities, "%w: mod %q lists host capability %q twice", ErrInvalidManifest, m.ID, hc)
			}
			seen[hc] = struct{}{}
		}
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
	for _, depID := range slices.Sorted(maps.Keys(m.OptionalDependencies)) {
		constraint := m.OptionalDependencies[depID]
		field := loaderr.Path("optionalDependencies", depID)
		if !validModID(depID) {
			return loaderr.Errorf(field, fixID, "%w: mod %q has invalid optional dependency id %q", ErrInvalidManifest, m.ID, depID)
		}
		if depID == m.ID {
			return loaderr.Errorf(field, "remove the mod's own id from optionalDependencies", "%w: mod %q cannot depend on itself", ErrCyclicDependency, m.ID)
		}
		if _, dup := m.Dependencies[depID]; dup {
			return loaderr.Errorf(field, "keep each dependency in either dependencies or optionalDependencies, not both", "%w: mod %q lists %q in both dependencies and optionalDependencies", ErrInvalidManifest, m.ID, depID)
		}
		if err := ValidateConstraint(constraint); err != nil {
			return loaderr.Errorf(field, fixConstraint, "%w: mod %q optional dependency %q constraint %q: %v", ErrInvalidManifest, m.ID, depID, constraint, err)
		}
	}
	for _, conflictID := range slices.Sorted(maps.Keys(m.Conflicts)) {
		constraint := m.Conflicts[conflictID]
		field := loaderr.Path("conflicts", conflictID)
		if !validModID(conflictID) {
			return loaderr.Errorf(field, fixID, "%w: mod %q has invalid conflict id %q", ErrInvalidManifest, m.ID, conflictID)
		}
		if conflictID == m.ID {
			return loaderr.Errorf(field, "remove the mod's own id from conflicts", "%w: mod %q cannot conflict with itself", ErrInvalidManifest, m.ID)
		}
		if err := ValidateConstraint(constraint); err != nil {
			return loaderr.Errorf(field, fixConstraint, "%w: mod %q conflict %q constraint %q: %v", ErrInvalidManifest, m.ID, conflictID, constraint, err)
		}
	}
	seenPlatforms := make(map[string]struct{}, len(m.Platforms))
	for _, p := range m.Platforms {
		if !slices.Contains(validPlatforms, p) {
			return loaderr.Errorf("platforms", fixPlatforms, "%w: mod %q unknown platform %q", ErrInvalidManifest, m.ID, p)
		}
		if _, dup := seenPlatforms[p]; dup {
			return loaderr.Errorf("platforms", "remove duplicate platform names", "%w: mod %q platform %q listed twice", ErrInvalidManifest, m.ID, p)
		}
		seenPlatforms[p] = struct{}{}
	}
	for _, item := range []struct {
		key string
		dir string
	}{
		{"assets", m.Contributes.Assets},
		{"scenes", m.Contributes.Scenes},
		{"l10n", m.Contributes.L10n},
		{"datatables", m.Contributes.DataTables},
	} {
		if item.dir == "" {
			continue
		}
		if err := validateContributesDir(item.dir); err != nil {
			return loaderr.Errorf(loaderr.Path("contributes", item.key), fixContributesDir, "%w: mod %q contributes.%s %q: %v", ErrInvalidManifest, m.ID, item.key, item.dir, err)
		}
	}
	return nil
}

func validateContributesDir(dir string) error {
	if strings.TrimSpace(dir) == "" || dir != strings.TrimSpace(dir) {
		return errors.New("path must not be blank or have surrounding whitespace")
	}
	if strings.HasPrefix(dir, "/") || strings.Contains(dir, "\\") {
		return errors.New("must be a relative slash-separated path")
	}
	if slices.Contains(strings.Split(dir, "/"), "..") {
		return errors.New("path escapes mod directory")
	}
	cleaned := path.Clean(dir)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || !fs.ValidPath(cleaned) {
		return errors.New("path escapes mod directory")
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
type SemVer = semver.Version

// ParseSemVer parses a semantic version string with an optional leading "v", such as "1.2.3", "v1.2.3", or "0.4.0-beta.1".
func ParseSemVer(raw string) (SemVer, error) {
	return semver.Parse(raw)
}

// ValidateConstraint checks that constraint is a syntactically valid version constraint.
func ValidateConstraint(constraint string) error {
	_, err := semver.ParseConstraint(constraint)
	return err
}

// CheckConstraint reports whether version satisfies constraint.
// Supported operators: *, =, >=, >, <=, <, ^, ~, partial versions such as ^1.2, and space/comma-separated conjunctions.
func CheckConstraint(version, constraint string) (bool, error) {
	return semver.Check(version, constraint)
}

// ResolveOrder validates manifests and returns them in deterministic topological load order
// (every required dependency and every present optional dependency precedes every mod that
// depends on it, with lexicographical ID tie-breaking).
// It rejects duplicate IDs, conflicting pairs ([ErrConflictingMods]), missing required
// dependencies, version mismatches (on required or present optional dependencies), and cycles.
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

	// Check conflicts in either direction in deterministic ID order.
	for _, id := range ids {
		m := byID[id]
		for _, conflictID := range slices.Sorted(maps.Keys(m.Conflicts)) {
			other, present := byID[conflictID]
			if !present {
				continue
			}
			constraint := m.Conflicts[conflictID]
			matches, err := CheckConstraint(other.Version, constraint)
			if err != nil {
				return nil, fmt.Errorf("%w: mod %q conflict %q: %v", ErrInvalidManifest, m.ID, conflictID, err)
			}
			if matches {
				return nil, fmt.Errorf("%w: mod %q conflicts with %q (%s, found %s)", ErrConflictingMods, m.ID, other.ID, constraint, other.Version)
			}
		}
	}

	inDegree := make(map[string]int, len(manifests))
	dependents := make(map[string][]string, len(manifests))
	for _, id := range ids {
		m := byID[id]
		for _, depID := range slices.Sorted(maps.Keys(m.Dependencies)) {
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
		for _, depID := range slices.Sorted(maps.Keys(m.OptionalDependencies)) {
			dep, ok := byID[depID]
			if !ok {
				continue
			}
			constraint := m.OptionalDependencies[depID]
			okVer, err := CheckConstraint(dep.Version, constraint)
			if err != nil {
				return nil, fmt.Errorf("%w: mod %q optional dependency %q: %v", ErrInvalidManifest, m.ID, depID, err)
			}
			if !okVer {
				return nil, fmt.Errorf("%w: mod %q optionally requires %q %s, found %s", ErrVersionMismatch, m.ID, depID, constraint, dep.Version)
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
