// Package manifest parses, validates and canonically encodes plugin.yaml manifests for Go plugins.
//
// A plugin.yaml file sits at the root of a plugin's Go module and declares its identity, Go module path, engine compatibility constraint, target plugin API version, supported platforms, Go entry packages (runtime, editor and CLI), module-keyed dependencies and conflicts, and contributed symbols.
//
// There is deliberately no version field in plugin.yaml: a plugin's version is its Go module version, resolved by the game project's go.mod, and is never duplicated in the manifest.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/internal/semver"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// FileName is the plugin manifest file's name at the root of a plugin's Go module.
const FileName = "plugin.yaml"

// ManifestVersion is the plugin manifest schema version this package reads and writes.
const ManifestVersion = 1

// SchemaURL is where the JSON Schema of version 1 plugin manifests (plugin.yaml) is published.
const SchemaURL = "https://game.nsgo.dev/schemas/plugin.v1.json"

// PlatformAll is the wildcard value in Manifest.Platforms meaning every target platform is supported.
const PlatformAll = "all"

// Platforms lists every concrete target platform a plugin manifest may name under platforms.
var Platforms = []string{"macos", "windows", "linux", "ios", "android", "web"}

// ErrInvalid reports that a plugin.yaml manifest breaks the schema or validation rules.
var ErrInvalid = errors.New("plugin manifest: invalid manifest")

const (
	fixManifest        = "correct plugin.yaml as docs/en/plugins.md describes it"
	fixManifestVersion = "set manifestVersion: 1"
	fixName            = "use 1 to 64 lower-case letters, digits and '-', starting with a letter or digit, such as \"healthkit\""
	fixModule          = "set module to the Go module path from the plugin's go.mod, such as \"example.com/healthkit\""
	fixConstraint      = "write a semantic version constraint such as \">=0.4.0 <0.5.0\" or \"^0.3.0\""
	fixAPI             = "set api to a positive integer plugin API version, such as 1"
	fixPlatforms       = "use [all] or a subset of macos, windows, linux, ios, android and web"
	fixEntries         = "set at least one of entries.runtime, entries.editor or entries.cli"
	fixRepository      = "use an http:// or https:// URL such as \"https://github.com/example/healthkit\""
	fixModuleKey       = "use a valid Go module path, such as \"example.com/navkit\""
	fixIdentifier      = "use a non-empty identifier without spaces or control characters"
)

// Manifest describes a Go plugin module. Plugin authors write it as YAML in [FileName] (plugin.yaml) at the root of the plugin's Go module; [Parse] and [ParseFS] read it and [Encode] writes it.
type Manifest struct {
	// ManifestVersion is the schema version of plugin.yaml. A file must state [ManifestVersion] (1); a manifest built in code may leave it 0, and [Encode] writes [ManifestVersion].
	ManifestVersion int `yaml:"manifestVersion" json:"manifestVersion" schema:"required"`
	// Name uniquely identifies the plugin: 1 to 64 lower-case ASCII letters, digits and '-', starting with a letter or digit.
	Name string `yaml:"name" json:"name" schema:"required,minLength=1"`
	// Module is the Go module path of the plugin, which must equal the module line of its go.mod.
	Module string `yaml:"module" json:"module" schema:"required,minLength=1"`
	// Description summarizes what the plugin provides on a single line.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// License is the plugin's license identifier, such as "MIT" or "Apache-2.0".
	License string `yaml:"license,omitempty" json:"license,omitempty"`
	// Authors names the people or organizations who maintain the plugin.
	Authors []string `yaml:"authors,omitempty,flow" json:"authors,omitempty"`
	// Repository is the HTTP or HTTPS URL of the plugin's source repository.
	Repository string `yaml:"repository,omitempty" json:"repository,omitempty"`
	// Engine is a semantic version constraint on github.com/nsgo-dev/game, such as ">=0.4.0 <0.5.0".
	Engine string `yaml:"engine" json:"engine" schema:"required,minLength=1"`
	// API is the plugin API version (gameplugin.APIVersion) the plugin targets; it must be a positive integer.
	API int `yaml:"api" json:"api" schema:"required,minimum=1"`
	// Platforms lists the target platforms the plugin supports: either ["all"] or a subset of macos, windows, linux, ios, android and web. When omitted, all platforms are supported.
	Platforms []string `yaml:"platforms,omitempty,flow" json:"platforms,omitempty"`
	// Entries names the Go packages inside Module that provide the plugin's runtime, editor and CLI integrations.
	Entries Entries `yaml:"entries" json:"entries" schema:"required"`
	// Requires maps Go module paths of plugins this plugin depends on to semantic version constraints.
	Requires map[string]string `yaml:"requires,omitempty" json:"requires,omitempty"`
	// Conflicts lists Go module paths of plugins that cannot be installed alongside this plugin.
	Conflicts []string `yaml:"conflicts,omitempty,flow" json:"conflicts,omitempty"`
	// Contributes summarizes the components, editor extensions and agent tools the plugin registers, for indexing and comparison by game plugin check.
	Contributes *Contributes `yaml:"contributes,omitempty" json:"contributes,omitempty"`
}

// Entries names the Go packages inside a plugin's module that integrate with the game runtime, the scene editor and the game CLI. At least one of Runtime, Editor or CLI must be set.
type Entries struct {
	// Runtime is the import path of the Go package that provides the plugin's runtime entry (func Plugin() gameplugin.Plugin).
	Runtime string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	// Editor is the import path of the Go package that provides the plugin's editor entry (func Plugin() editor.Plugin).
	Editor string `yaml:"editor,omitempty" json:"editor,omitempty"`
	// CLI lists command packages that extend the game CLI via go.mod tool directives.
	CLI []CLIEntry `yaml:"cli,omitempty" json:"cli,omitempty"`
}

// CLIEntry names one command package a plugin contributes to the game CLI.
type CLIEntry struct {
	// Name is the subcommand name invoked as "game <name>": 1 to 64 lower-case ASCII letters, digits and '-', starting with a letter or digit.
	Name string `yaml:"name" json:"name" schema:"required,minLength=1"`
	// Package is the Go import path of the main package implementing the command, inside the plugin's module.
	Package string `yaml:"package" json:"package" schema:"required,minLength=1"`
}

// Contributes indexes the components, editor extensions and agent tools a plugin registers. Tools such as game plugin check compare it against the plugin's code.
type Contributes struct {
	// Components lists the names of scene components the plugin registers.
	Components []string `yaml:"components,omitempty,flow" json:"components,omitempty"`
	// Editor lists the panels, tools and commands the plugin adds to the scene editor.
	Editor *EditorContributes `yaml:"editor,omitempty" json:"editor,omitempty"`
	// AgentTools lists the MCP agent tool names the plugin exposes, such as "plugin.healthkit.heal_all".
	AgentTools []string `yaml:"agentTools,omitempty,flow" json:"agentTools,omitempty"`
}

// EditorContributes lists the editor extension IDs a plugin registers.
type EditorContributes struct {
	// Panels lists the IDs of editor panels the plugin adds.
	Panels []string `yaml:"panels,omitempty,flow" json:"panels,omitempty"`
	// Tools lists the IDs of viewport tools the plugin adds.
	Tools []string `yaml:"tools,omitempty,flow" json:"tools,omitempty"`
	// Commands lists the IDs of editor commands the plugin adds.
	Commands []string `yaml:"commands,omitempty,flow" json:"commands,omitempty"`
}

// Parse decodes and validates a plugin.yaml manifest. Unknown fields, duplicate keys, trailing YAML documents and a missing, unsupported or non-integer-literal manifestVersion are refused.
//
// Its errors match [ErrInvalid] with errors.Is, and are *loaderr.Error values that give the line, field and fix. A caller that read data from a file names it with loaderr.WithFile, as [ParseFS] does.
func Parse(data []byte) (Manifest, error) {
	m, err := parse(data)
	return m, loaderr.Locate(err, "", data, fixManifest)
}

// ParseFS reads [FileName] (plugin.yaml) from dir in fsys and parses it with [Parse], naming the file in any returned *loaderr.Error.
func ParseFS(fsys fs.FS, dir string) (Manifest, error) {
	if fsys == nil {
		return Manifest{}, loaderr.Errorf("", fixManifest, "%w: nil filesystem", ErrInvalid)
	}
	filePath := FileName
	if dir != "" && dir != "." {
		filePath = path.Join(dir, FileName)
	}
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin manifest: read %s: %w", filePath, err)
	}
	m, err := Parse(data)
	if err != nil {
		return Manifest{}, loaderr.WithFile(err, filePath)
	}
	return m, nil
}

func parse(data []byte) (Manifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Manifest{}, loaderr.Errorf("", fixManifest, "%w: empty manifest", ErrInvalid)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, loaderr.Errorf("", "keep one YAML document in plugin.yaml; remove the --- and what follows it", "%w: unexpected trailing YAML document", ErrInvalid)
		}
		return Manifest{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if m.ManifestVersion != ManifestVersion {
		return Manifest{}, loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d)", ErrInvalid, m.ManifestVersion, ManifestVersion)
	}
	if err := checkIntegerLiterals(data); err != nil {
		return Manifest{}, err
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// checkIntegerLiterals refuses manifestVersion or api values that decode to an integer in YAML but are not written as a plain decimal integer, such as 1.0 or 0x1, so a strict file cannot carry a value that Encode would silently rewrite.
func checkIntegerLiterals(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return loaderr.Errorf("", fixManifest, "%w: manifest must be a mapping", ErrInvalid)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		value := root.Content[i+1]
		switch key {
		case "manifestVersion":
			if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || value.Value != strconv.Itoa(ManifestVersion) {
				e := loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: line %d: manifestVersion must be the integer %d, not %q", ErrInvalid, value.Line, ManifestVersion, value.Value)
				e.Line, e.Column = value.Line, value.Column
				return e
			}
		case "api":
			if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || !isPlainPositiveDecimal(value.Value) {
				e := loaderr.Errorf("api", fixAPI, "%w: line %d: api must be a positive integer, not %q", ErrInvalid, value.Line, value.Value)
				e.Line, e.Column = value.Line, value.Column
				return e
			}
		}
	}
	return nil
}

func isPlainPositiveDecimal(s string) bool {
	if s == "" || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Encode validates m and writes it as deterministic 2-space YAML: fields in a fixed order, every list sorted in byte order (CLI entries by Name), and requires map keys sorted in byte order. A ManifestVersion of 0 (a manifest built in code) is written as [ManifestVersion]. [Parse] reads the output back as an equal manifest, and encoding that again gives the same bytes.
func Encode(m Manifest) ([]byte, error) {
	if m.ManifestVersion == 0 {
		m.ManifestVersion = ManifestVersion
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	canon := canonicalCopy(m)
	var node yaml.Node
	if err := node.Encode(canon); err != nil {
		return nil, fmt.Errorf("plugin manifest: encode: %w", err)
	}
	sortMappingKeys(&node, "requires")
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("plugin manifest: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("plugin manifest: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// canonicalCopy returns a copy of m with every slice cloned and sorted in byte order and empty contributes blocks normalized to nil, without mutating m's slices.
func canonicalCopy(m Manifest) Manifest {
	if len(m.Authors) > 0 {
		m.Authors = slices.Clone(m.Authors)
		slices.Sort(m.Authors)
	}
	if len(m.Platforms) > 0 {
		m.Platforms = slices.Clone(m.Platforms)
		slices.Sort(m.Platforms)
	}
	if len(m.Entries.CLI) > 0 {
		m.Entries.CLI = slices.Clone(m.Entries.CLI)
		slices.SortFunc(m.Entries.CLI, func(a, b CLIEntry) int {
			return strings.Compare(a.Name, b.Name)
		})
	}
	if len(m.Conflicts) > 0 {
		m.Conflicts = slices.Clone(m.Conflicts)
		slices.Sort(m.Conflicts)
	}
	if m.Contributes != nil {
		c := *m.Contributes
		if len(c.Components) > 0 {
			c.Components = slices.Clone(c.Components)
			slices.Sort(c.Components)
		}
		if len(c.AgentTools) > 0 {
			c.AgentTools = slices.Clone(c.AgentTools)
			slices.Sort(c.AgentTools)
		}
		if c.Editor != nil {
			ed := *c.Editor
			if len(ed.Panels) > 0 {
				ed.Panels = slices.Clone(ed.Panels)
				slices.Sort(ed.Panels)
			}
			if len(ed.Tools) > 0 {
				ed.Tools = slices.Clone(ed.Tools)
				slices.Sort(ed.Tools)
			}
			if len(ed.Commands) > 0 {
				ed.Commands = slices.Clone(ed.Commands)
				slices.Sort(ed.Commands)
			}
			if len(ed.Panels) == 0 && len(ed.Tools) == 0 && len(ed.Commands) == 0 {
				c.Editor = nil
			} else {
				c.Editor = &ed
			}
		}
		if len(c.Components) == 0 && c.Editor == nil && len(c.AgentTools) == 0 {
			m.Contributes = nil
		} else {
			m.Contributes = &c
		}
	}
	return m
}

// sortMappingKeys orders the entries of the top-level mapping under key in strict byte order; yaml.v3 itself compares digit runs numerically.
func sortMappingKeys(doc *yaml.Node, key string) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		m := root.Content[i+1]
		pairs := make([][2]*yaml.Node, 0, len(m.Content)/2)
		for j := 0; j+1 < len(m.Content); j += 2 {
			pairs = append(pairs, [2]*yaml.Node{m.Content[j], m.Content[j+1]})
		}
		slices.SortFunc(pairs, func(a, b [2]*yaml.Node) int {
			return strings.Compare(a[0].Value, b[0].Value)
		})
		m.Content = m.Content[:0]
		for _, p := range pairs {
			m.Content = append(m.Content, p[0], p[1])
		}
	}
}

// SupportsPlatform reports whether m declares support for platform (one of [Platforms]). An empty Platforms list or one containing [PlatformAll] matches every known platform.
func (m Manifest) SupportsPlatform(platform string) bool {
	if !slices.Contains(Platforms, platform) {
		return false
	}
	if len(m.Platforms) == 0 {
		return true
	}
	for _, p := range m.Platforms {
		if p == PlatformAll || p == platform {
			return true
		}
	}
	return false
}

// Validate checks every field of m. ManifestVersion may be 0 for a manifest built in code, or [ManifestVersion]; [Parse] additionally requires a file to state [ManifestVersion]. Its errors are *loaderr.Error values that name the field at fault and how to fix it, and match [ErrInvalid] with errors.Is.
func (m Manifest) Validate() error {
	if m.ManifestVersion != 0 && m.ManifestVersion != ManifestVersion {
		return loaderr.Errorf("manifestVersion", fixManifestVersion, "%w: unsupported manifestVersion %d (want %d)", ErrInvalid, m.ManifestVersion, ManifestVersion)
	}
	if err := ValidateName(m.Name); err != nil {
		return loaderr.Errorf("name", fixName, "%w: %v", ErrInvalid, err)
	}
	if err := ValidateModulePath(m.Module); err != nil {
		return loaderr.Errorf("module", fixModule, "%w: %v", ErrInvalid, err)
	}
	if m.Description != "" {
		if err := validateSingleLine("description", m.Description); err != nil {
			return loaderr.Errorf("description", "write description on a single line without control characters", "%w: %v", ErrInvalid, err)
		}
	}
	if m.License != "" {
		if err := validateSingleLine("license", m.License); err != nil {
			return loaderr.Errorf("license", "set license to a single-line SPDX identifier such as \"MIT\" or \"Apache-2.0\"", "%w: %v", ErrInvalid, err)
		}
	}
	if err := validateAuthors(m.Authors); err != nil {
		return err
	}
	if m.Repository != "" {
		if err := validateRepository(m.Repository); err != nil {
			return loaderr.Errorf("repository", fixRepository, "%w: %v", ErrInvalid, err)
		}
	}
	if strings.TrimSpace(m.Engine) == "" {
		return loaderr.Errorf("engine", fixConstraint, "%w: engine version constraint is required", ErrInvalid)
	}
	if _, err := semver.ParseConstraint(m.Engine); err != nil {
		return loaderr.Errorf("engine", fixConstraint, "%w: invalid engine constraint %q: %v", ErrInvalid, m.Engine, err)
	}
	if m.API < 1 {
		return loaderr.Errorf("api", fixAPI, "%w: api must be a positive integer, got %d", ErrInvalid, m.API)
	}
	if err := validatePlatforms(m.Platforms); err != nil {
		return err
	}
	if err := validateEntries(m.Module, m.Entries); err != nil {
		return err
	}
	if err := validateRequires(m.Module, m.Requires); err != nil {
		return err
	}
	if err := validateConflicts(m.Module, m.Requires, m.Conflicts); err != nil {
		return err
	}
	if m.Contributes != nil {
		if err := validateContributes(m.Contributes); err != nil {
			return err
		}
	}
	return nil
}

// ValidateName checks that name is a valid plugin name: 1 to 64 lower-case ASCII letters, digits and '-', starting with a letter or digit (the same rule as project.ValidatePackName).
func ValidateName(name string) error {
	if name == "" || len(name) > 64 || name[0] == '-' {
		return fmt.Errorf("plugin name %q must be 1 to 64 lower-case letters, digits and '-', starting with a letter or digit", name)
	}
	for i := range len(name) {
		if c := name[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return fmt.Errorf("plugin name %q must be 1 to 64 lower-case letters, digits and '-', starting with a letter or digit", name)
		}
	}
	return nil
}

// ValidateModulePath checks that modulePath is a valid Go module path.
func ValidateModulePath(modulePath string) error {
	if modulePath == "" {
		return errors.New("module path must not be empty")
	}
	if strings.HasPrefix(modulePath, "/") || strings.HasSuffix(modulePath, "/") {
		return fmt.Errorf("module path %q must not have a leading or trailing '/'", modulePath)
	}
	elems := strings.Split(modulePath, "/")
	for i, elem := range elems {
		if err := validatePathElement(modulePath, elem, i == 0); err != nil {
			return err
		}
	}
	if len(elems) > 1 {
		last := elems[len(elems)-1]
		if isMajorVersionElem(last) {
			if last == "v0" || last == "v1" || last[1] == '0' {
				return fmt.Errorf("module path %q has invalid major version suffix %q (must be v2 or higher without leading zeros)", modulePath, last)
			}
		}
	}
	return nil
}

// ValidatePackagePath checks that pkgPath is a valid Go package import path equal to modulePath or strictly under modulePath + "/".
func ValidatePackagePath(modulePath, pkgPath string) error {
	if err := ValidateModulePath(modulePath); err != nil {
		return err
	}
	if pkgPath == "" {
		return errors.New("package path must not be empty")
	}
	if pkgPath == modulePath {
		return nil
	}
	if !strings.HasPrefix(pkgPath, modulePath+"/") {
		return fmt.Errorf("package path %q is outside module %q", pkgPath, modulePath)
	}
	sub := strings.TrimPrefix(pkgPath, modulePath+"/")
	if strings.HasSuffix(sub, "/") {
		return fmt.Errorf("package path %q must not have a trailing '/'", pkgPath)
	}
	for elem := range strings.SplitSeq(sub, "/") {
		if err := validatePathElement(pkgPath, elem, false); err != nil {
			return err
		}
	}
	return nil
}

func validatePathElement(fullPath, elem string, first bool) error {
	if elem == "" {
		return fmt.Errorf("path %q has an empty path element", fullPath)
	}
	if elem[0] == '.' || elem[len(elem)-1] == '.' {
		return fmt.Errorf("path %q element %q must not start or end with '.'", fullPath, elem)
	}
	if first && elem[0] == '-' {
		return fmt.Errorf("path %q first element %q must not start with '-'", fullPath, elem)
	}
	for i := range len(elem) {
		c := elem[i]
		if !isPathChar(c, first) {
			return fmt.Errorf("path %q contains invalid character %q in element %q", fullPath, c, elem)
		}
	}
	if isWindowsReservedName(elem) {
		return fmt.Errorf("path %q element %q is a reserved name on Windows", fullPath, elem)
	}
	if hasTildeDigits(elem) {
		return fmt.Errorf("path %q element %q contains a disallowed Windows short-name tilde suffix", fullPath, elem)
	}
	return nil
}

func isPathChar(c byte, first bool) bool {
	if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
		return true
	}
	if !first && c >= 'A' && c <= 'Z' {
		return true
	}
	return c == '-' || c == '.' || c == '_' || c == '~'
}

func isWindowsReservedName(elem string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(elem), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9'
}

func hasTildeDigits(elem string) bool {
	base, _, _ := strings.Cut(elem, ".")
	tilde := strings.LastIndexByte(base, '~')
	if tilde < 0 || tilde == len(base)-1 {
		return false
	}
	for i := tilde + 1; i < len(base); i++ {
		if base[i] < '0' || base[i] > '9' {
			return false
		}
	}
	return true
}

func isMajorVersionElem(elem string) bool {
	if len(elem) < 2 || elem[0] != 'v' {
		return false
	}
	for i := 1; i < len(elem); i++ {
		if elem[i] < '0' || elem[i] > '9' {
			return false
		}
	}
	return true
}

func validateSingleLine(name, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s must not be whitespace-only", name)
	}
	if strings.TrimSpace(s) != s {
		return fmt.Errorf("%s must not have leading or trailing whitespace", name)
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s must be a single line without control characters", name)
	}
	return nil
}

func validateAuthors(authors []string) error {
	seen := make(map[string]struct{}, len(authors))
	for i, a := range authors {
		field := loaderr.Path("authors", i)
		if err := validateSingleLine("author", a); err != nil {
			return loaderr.Errorf(field, "list non-empty single-line author names without control characters", "%w: %v", ErrInvalid, err)
		}
		if _, dup := seen[a]; dup {
			return loaderr.Errorf(field, "remove duplicate author entries", "%w: author %q is listed twice", ErrInvalid, a)
		}
		seen[a] = struct{}{}
	}
	return nil
}

func validateRepository(repo string) error {
	if strings.TrimSpace(repo) != repo || strings.IndexFunc(repo, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("repository %q must not contain spaces or control characters", repo)
	}
	u, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("repository %q: %w", repo, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("repository %q must be an http:// or https:// URL with a host", repo)
	}
	return nil
}

func validatePlatforms(platforms []string) error {
	if len(platforms) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(platforms))
	hasAll := false
	for i, p := range platforms {
		field := loaderr.Path("platforms", i)
		if p == PlatformAll {
			if hasAll {
				return loaderr.Errorf(field, "remove duplicate platform entries", "%w: platform %q is listed twice", ErrInvalid, p)
			}
			hasAll = true
			continue
		}
		if !slices.Contains(Platforms, p) {
			return loaderr.Errorf(field, fixPlatforms, "%w: unknown platform %q", ErrInvalid, p)
		}
		if _, dup := seen[p]; dup {
			return loaderr.Errorf(field, "remove duplicate platform entries", "%w: platform %q is listed twice", ErrInvalid, p)
		}
		seen[p] = struct{}{}
	}
	if hasAll && len(platforms) > 1 {
		return loaderr.Errorf("platforms", "use [all] alone, or list specific platforms from macos, windows, linux, ios, android and web", "%w: platform \"all\" cannot be combined with specific platforms", ErrInvalid)
	}
	return nil
}

func validateEntries(modulePath string, entries Entries) error {
	if entries.Runtime == "" && entries.Editor == "" && len(entries.CLI) == 0 {
		return loaderr.Errorf("entries", fixEntries, "%w: manifest must declare at least one entry under entries", ErrInvalid)
	}
	fixPkg := fmt.Sprintf("set the entry package to %q or a package path under %q", modulePath, modulePath+"/")
	if entries.Runtime != "" {
		if err := ValidatePackagePath(modulePath, entries.Runtime); err != nil {
			return loaderr.Errorf("entries.runtime", fixPkg, "%w: %v", ErrInvalid, err)
		}
	}
	if entries.Editor != "" {
		if err := ValidatePackagePath(modulePath, entries.Editor); err != nil {
			return loaderr.Errorf("entries.editor", fixPkg, "%w: %v", ErrInvalid, err)
		}
	}
	seenCLI := make(map[string]struct{}, len(entries.CLI))
	for i, cli := range entries.CLI {
		nameField := loaderr.Path("entries", "cli", i, "name")
		if err := ValidateName(cli.Name); err != nil {
			return loaderr.Errorf(nameField, fixName, "%w: cli entry %d: %v", ErrInvalid, i, err)
		}
		if _, dup := seenCLI[cli.Name]; dup {
			return loaderr.Errorf(nameField, "give each CLI entry a unique command name", "%w: cli command %q is listed twice", ErrInvalid, cli.Name)
		}
		seenCLI[cli.Name] = struct{}{}
		pkgField := loaderr.Path("entries", "cli", i, "package")
		if err := ValidatePackagePath(modulePath, cli.Package); err != nil {
			return loaderr.Errorf(pkgField, fixPkg, "%w: cli command %q: %v", ErrInvalid, cli.Name, err)
		}
	}
	return nil
}

func validateRequires(modulePath string, requires map[string]string) error {
	for _, depMod := range slices.Sorted(maps.Keys(requires)) {
		constraint := requires[depMod]
		field := loaderr.Path("requires", depMod)
		if err := ValidateModulePath(depMod); err != nil {
			return loaderr.Errorf(field, fixModuleKey, "%w: invalid required module %q: %v", ErrInvalid, depMod, err)
		}
		if depMod == modulePath {
			return loaderr.Errorf(field, "remove the plugin's own module path from requires", "%w: plugin %q cannot require itself", ErrInvalid, modulePath)
		}
		if strings.TrimSpace(constraint) == "" {
			return loaderr.Errorf(field, fixConstraint, "%w: required module %q has an empty version constraint", ErrInvalid, depMod)
		}
		if _, err := semver.ParseConstraint(constraint); err != nil {
			return loaderr.Errorf(field, fixConstraint, "%w: required module %q constraint %q: %v", ErrInvalid, depMod, constraint, err)
		}
	}
	return nil
}

func validateConflicts(modulePath string, requires map[string]string, conflicts []string) error {
	seen := make(map[string]struct{}, len(conflicts))
	for i, confMod := range conflicts {
		field := loaderr.Path("conflicts", i)
		if err := ValidateModulePath(confMod); err != nil {
			return loaderr.Errorf(field, fixModuleKey, "%w: invalid conflicting module %q: %v", ErrInvalid, confMod, err)
		}
		if confMod == modulePath {
			return loaderr.Errorf(field, "remove the plugin's own module path from conflicts", "%w: plugin %q cannot conflict with itself", ErrInvalid, modulePath)
		}
		if _, dup := seen[confMod]; dup {
			return loaderr.Errorf(field, "remove duplicate entries from conflicts", "%w: conflicting module %q is listed twice", ErrInvalid, confMod)
		}
		if _, inReq := requires[confMod]; inReq {
			return loaderr.Errorf(field, "remove the module from either requires or conflicts so they are disjoint", "%w: module %q appears in both requires and conflicts", ErrInvalid, confMod)
		}
		seen[confMod] = struct{}{}
	}
	return nil
}

func validateContributes(c *Contributes) error {
	if err := validateIdentifierList("contributes.components", c.Components); err != nil {
		return err
	}
	if c.Editor != nil {
		if err := validateIdentifierList("contributes.editor.panels", c.Editor.Panels); err != nil {
			return err
		}
		if err := validateIdentifierList("contributes.editor.tools", c.Editor.Tools); err != nil {
			return err
		}
		if err := validateIdentifierList("contributes.editor.commands", c.Editor.Commands); err != nil {
			return err
		}
	}
	if err := validateIdentifierList("contributes.agentTools", c.AgentTools); err != nil {
		return err
	}
	return nil
}

func validateIdentifierList(base string, items []string) error {
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		field := loaderr.Join(base, i)
		if item == "" || strings.IndexFunc(item, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return loaderr.Errorf(field, fixIdentifier, "%w: %s item %q must be non-empty without spaces or control characters", ErrInvalid, base, item)
		}
		if _, dup := seen[item]; dup {
			return loaderr.Errorf(field, "remove duplicate entries from "+base, "%w: %s lists %q twice", ErrInvalid, base, item)
		}
		seen[item] = struct{}{}
	}
	return nil
}
