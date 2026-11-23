package project

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// NSGoFileName is NSGo's own project file, beside game.yaml.
const NSGoFileName = "nsgo.yaml"

// Platforms are the export preset platforms, each a name nsgo build -target accepts.
var Platforms = []string{"macos", "windows", "linux", "ios", "tvos", "watchos", "android", "harmonyos", "web", "miniprogram"}

// nsgoTargets are the targets NSGo v0.2.0's nsgo build accepts, as cmd/nsgo/build.go lists them; NSGo exports no list of its own (reported upstream).
var nsgoTargets = []string{
	"macos_arm64", "macos_x64",
	"windows_arm64", "windows_x64",
	"linux_arm64", "linux_x64",
	"ios", "ios_simulator",
	"tvos", "tvos_simulator",
	"watchos", "watchos_simulator",
	"android_arm64", "android_x64",
	"harmonyos",
	"web",
	"miniprogram_wechat", "miniprogram_qq", "miniprogram_alipay", "miniprogram_douyin",
}

// platformOf is the platform an NSGo target belongs to.
func platformOf(target string) string {
	platform, _, _ := strings.Cut(target, "_")
	return platform
}

// TargetsOf returns the NSGo build targets a preset for platform may list.
func TargetsOf(platform string) []string {
	var out []string
	for _, t := range nsgoTargets {
		if platformOf(t) == platform {
			out = append(out, t)
		}
	}
	return out
}

// BuildTargets returns the NSGo build targets of every export preset in order: each preset's Targets, or its platform name when it lists none. It returns nil without export presets.
func (f *File) BuildTargets() []string {
	if f.Export == nil {
		return nil
	}
	var out []string
	for _, p := range f.Export.Presets {
		if len(p.Targets) == 0 {
			out = append(out, p.Platform)
			continue
		}
		out = append(out, p.Targets...)
	}
	return out
}

// Preset returns the export preset for platform.
func (f *File) Preset(platform string) (Preset, bool) {
	if f.Export != nil {
		for _, p := range f.Export.Presets {
			if p.Platform == platform {
				return p, true
			}
		}
	}
	return Preset{}, false
}

// nsgoOwned is the part of nsgo.yaml that game.yaml owns.
type nsgoOwned struct {
	Name       string `yaml:"name"`
	ID         string `yaml:"id"`
	Version    string `yaml:"version"`
	Appearance string `yaml:"appearance"`
	Build      struct {
		Output  string   `yaml:"output"`
		Targets []string `yaml:"targets"`
	} `yaml:"build"`
}

func (f *File) owned() nsgoOwned {
	var o nsgoOwned
	o.Name, o.ID, o.Version, o.Appearance = f.Name, f.ID, f.AppVersion, f.Window.Appearance
	if f.Export != nil {
		o.Build.Output = f.Export.Output
		o.Build.Targets = f.BuildTargets()
	}
	return o
}

// CheckNSGo reports the fields of nsgoYAML, an nsgo.yaml's contents, that disagree with f: name, id, version, appearance, build.output and build.targets, which game.yaml owns. It returns nil when they all agree.
func (f *File) CheckNSGo(nsgoYAML []byte) error {
	if err := Validate(f); err != nil {
		return err
	}
	var got nsgoOwned
	if err := yaml.Unmarshal(nsgoYAML, &got); err != nil {
		return fmt.Errorf("project: %s: %w", NSGoFileName, err)
	}
	want := f.owned()
	var diffs []string
	diff := func(key, got, want string) {
		if got != want {
			diffs = append(diffs, fmt.Sprintf("%s is %q, game.yaml has %q", key, got, want))
		}
	}
	diff("name", got.Name, want.Name)
	diff("id", got.ID, want.ID)
	diff("version", got.Version, want.Version)
	diff("appearance", got.Appearance, want.Appearance)
	diff("build.output", got.Build.Output, want.Build.Output)
	if !slices.Equal(got.Build.Targets, want.Build.Targets) {
		diffs = append(diffs, fmt.Sprintf("build.targets is %v, game.yaml has %v", got.Build.Targets, want.Build.Targets))
	}
	if len(diffs) > 0 {
		return fmt.Errorf("project: %s disagrees with %s: %s", NSGoFileName, FileName, strings.Join(diffs, "; "))
	}
	return nil
}

// SyncNSGo returns nsgoYAML, an nsgo.yaml's contents (empty for a new one), with the fields game.yaml owns set from f: name, id, version, appearance, build.output and build.targets. Fields f leaves empty are removed; every other key and comment is kept.
func (f *File) SyncNSGo(nsgoYAML []byte) ([]byte, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(nsgoYAML)) > 0 {
		if err := yaml.Unmarshal(nsgoYAML, &doc); err != nil {
			return nil, fmt.Errorf("project: %s: %w", NSGoFileName, err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("project: %s is not a YAML mapping", NSGoFileName)
	}
	root := doc.Content[0]
	o := f.owned()
	setScalar(root, "name", o.Name)
	setScalar(root, "id", o.ID)
	setScalar(root, "version", o.Version)
	setScalar(root, "appearance", o.Appearance)

	build := mappingValue(root, "build")
	if build == nil && (o.Build.Output != "" || len(o.Build.Targets) > 0) {
		build = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalarNode("build"), build)
	}
	if build != nil {
		if build.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("project: %s build is not a mapping", NSGoFileName)
		}
		setScalar(build, "output", o.Build.Output)
		setTargets(build, o.Build.Targets)
		if len(build.Content) == 0 {
			deleteKey(root, "build")
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("project: encode %s: %w", NSGoFileName, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("project: encode %s: %w", NSGoFileName, err)
	}
	return buf.Bytes(), nil
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// keyIndex is the index of key's key node in mapping m's content, or -1.
func keyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if i := keyIndex(m, key); i >= 0 {
		return m.Content[i+1]
	}
	return nil
}

func deleteKey(m *yaml.Node, key string) {
	if i := keyIndex(m, key); i >= 0 {
		m.Content = slices.Delete(m.Content, i, i+2)
	}
}

// setScalar sets key to the string value in mapping m, keeping the value node's comments, or removes key when value is empty.
func setScalar(m *yaml.Node, key, value string) {
	if value == "" {
		deleteKey(m, key)
		return
	}
	i := keyIndex(m, key)
	if i < 0 {
		m.Content = append(m.Content, scalarNode(key), scalarNode(value))
		return
	}
	v := m.Content[i+1]
	*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, LineComment: v.LineComment, HeadComment: v.HeadComment, FootComment: v.FootComment}
}

// setTargets sets build.targets in flow style, or removes it when targets is empty.
func setTargets(build *yaml.Node, targets []string) {
	if len(targets) == 0 {
		deleteKey(build, "targets")
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, t := range targets {
		seq.Content = append(seq.Content, scalarNode(t))
	}
	i := keyIndex(build, "targets")
	if i < 0 {
		build.Content = append(build.Content, scalarNode("targets"), seq)
		return
	}
	old := build.Content[i+1]
	seq.LineComment, seq.HeadComment, seq.FootComment = old.LineComment, old.HeadComment, old.FootComment
	build.Content[i+1] = seq
}
