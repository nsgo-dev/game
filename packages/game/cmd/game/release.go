package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// releaseCommand builds a game's release packages for every platform in one run of nsgo build.
var releaseCommand = &command{
	name:    "release",
	args:    "[dir]",
	summary: "Build the game's release packages for macOS, Windows, Linux, web, iOS and Android, or game.yaml's export presets, in one run of nsgo build",
	define:  defineRelease,
}

// releasePlatforms are what game release builds when neither -target nor game.yaml's export presets say otherwise, as nsgo build -target names them: macos, windows, linux and android expand to their architectures, and ios and web are one target each.
var releasePlatforms = []string{"macos", "windows", "linux", "web", "ios", "android"}

// steamPlatforms are what game release builds by default for the steam channel, whose depots nsgo build makes only of desktop targets.
var steamPlatforms = []string{"macos", "windows", "linux"}

// gameFileName is the project file at a game's root whose export section game release reads.
const gameFileName = "game.yaml"

// gameFileVersion is the game.yaml schema version game release reads.
const gameFileVersion = 1

// exportPlatforms are the platforms a game.yaml export preset may name, each a name nsgo build -target accepts.
var exportPlatforms = []string{"macos", "windows", "linux", "ios", "tvos", "watchos", "android", "harmonyos", "web", "miniprogram"}

// exportTargets are the NSGo v0.3.0 build targets a preset may list, from the unexported targets list in its cmd/nsgo/build.go, since NSGo exports none; each belongs to the platform before its first underscore (nsgo-dev/nsgo#2259).
var exportTargets = []string{
	"macos_arm64", "macos_x64", "macos_universal",
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

// runNSGo runs the go command with args, writing its output to stdout and stderr; tests replace it.
var runNSGo = func(args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// releaseSettings are the parts of game.yaml that game release reads: the build output directory and the build targets of the export presets.
type releaseSettings struct {
	// Output is export.output, a slash-separated directory inside the project; empty leaves nsgo.yaml's build.output.
	Output string
	// Targets are the presets' targets in order, a preset without targets giving its platform name; nil without presets.
	Targets []string
}

// gameFileKeys are game.yaml's top-level fields, those of the project file's schema (#488), so that a misspelt section such as exprot is refused rather than ignored.
var gameFileKeys = []string{"version", "name", "id", "appVersion", "window", "display", "input", "audio", "autoload", "export"}

// gameFileHead is game.yaml's version and export section, decoded on their own so the rest of the file, whose schema is package project's (#488), is left to it.
type gameFileHead struct {
	Version yaml.Node `yaml:"version"`
	Export  yaml.Node `yaml:"export"`
}

// exportSection is game.yaml's export section, decoded once knownFields has refused unknown fields.
type exportSection struct {
	Output  string         `yaml:"output"`
	Presets []exportPreset `yaml:"presets"`
}

// exportPreset is one export preset: a platform and the NSGo targets of it to build.
type exportPreset struct {
	Platform string   `yaml:"platform"`
	Targets  []string `yaml:"targets"`
}

// parseReleaseSettings reads the version and export section of a game.yaml, refusing unknown top-level fields, a version other than the integer 1, unknown fields in export, an empty preset list, platforms and targets nsgo build does not know, a target of another platform, duplicates and an output outside the project.
func parseReleaseSettings(data []byte) (releaseSettings, error) {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return releaseSettings{}, errors.New("empty file")
		}
		return releaseSettings{}, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return releaseSettings{}, errors.New("unexpected trailing YAML document")
		}
		return releaseSettings{}, err
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return releaseSettings{}, fmt.Errorf("line %d: the file must be a mapping of fields", root.Line)
	}
	if err := knownFields(root, "game.yaml", gameFileKeys...); err != nil {
		return releaseSettings{}, err
	}
	var head gameFileHead
	if err := root.Decode(&head); err != nil {
		return releaseSettings{}, err
	}
	version := resolveAlias(&head.Version)
	if version.Kind == 0 {
		return releaseSettings{}, errors.New("version is required")
	}
	// The tag is checked before decoding, since yaml.v3 would truncate a float such as 1.9 to the int 1.
	var v int
	if version.Kind != yaml.ScalarNode || version.Tag != "!!int" || version.Decode(&v) != nil {
		return releaseSettings{}, fmt.Errorf("line %d: version %q must be the integer %d", version.Line, version.Value, gameFileVersion)
	}
	if v != gameFileVersion {
		return releaseSettings{}, fmt.Errorf("unsupported version %d (want %d)", v, gameFileVersion)
	}
	if head.Export.Kind == 0 || head.Export.Tag == "!!null" {
		return releaseSettings{}, nil
	}
	// Node.Decode cannot refuse unknown fields, so the section's keys are checked first, keeping the file's line numbers in the errors.
	if err := knownFields(&head.Export, "export", "output", "presets"); err != nil {
		return releaseSettings{}, err
	}
	if presets := field(&head.Export, "presets"); presets != nil && presets.Kind == yaml.SequenceNode {
		if len(presets.Content) == 0 {
			return releaseSettings{}, fmt.Errorf("line %d: export presets must name at least one platform; leave presets out to build the default platforms", presets.Line)
		}
		for _, p := range presets.Content {
			if err := knownFields(p, "export preset", "platform", "targets"); err != nil {
				return releaseSettings{}, err
			}
		}
	}
	var export exportSection
	if err := head.Export.Decode(&export); err != nil {
		return releaseSettings{}, fmt.Errorf("export: %w", err)
	}
	// A colon is refused as well as fs.ValidPath's rejects, since C:/builds is a valid slash path that Windows reads as a drive outside the project.
	if o := export.Output; o != "" && (o == "." || !fs.ValidPath(o) || strings.ContainsAny(o, `\:`)) {
		return releaseSettings{}, fmt.Errorf("export output %q must be a slash-separated directory inside the project", o)
	}
	settings := releaseSettings{Output: export.Output}
	seenPlatforms := make(map[string]bool, len(export.Presets))
	for _, p := range export.Presets {
		if !slices.Contains(exportPlatforms, p.Platform) {
			return releaseSettings{}, fmt.Errorf("export platform %q must be one of %s", p.Platform, strings.Join(exportPlatforms, ", "))
		}
		if seenPlatforms[p.Platform] {
			return releaseSettings{}, fmt.Errorf("duplicate export preset for platform %q", p.Platform)
		}
		seenPlatforms[p.Platform] = true
		if len(p.Targets) == 0 {
			settings.Targets = append(settings.Targets, p.Platform)
			continue
		}
		seenTargets := make(map[string]bool, len(p.Targets))
		for _, t := range p.Targets {
			if !slices.Contains(exportTargets, t) || platformOf(t) != p.Platform {
				return releaseSettings{}, fmt.Errorf("export platform %q target %q must be one of %s", p.Platform, t, strings.Join(targetsOf(p.Platform), ", "))
			}
			if seenTargets[t] {
				return releaseSettings{}, fmt.Errorf("export platform %q lists target %q twice", p.Platform, t)
			}
			seenTargets[t] = true
			settings.Targets = append(settings.Targets, t)
		}
	}
	return settings, nil
}

// resolveAlias returns the node an alias node points to, or n itself.
func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// knownFields refuses a key of the mapping n that is not one of names; a node of another kind is left for Decode to refuse.
func knownFields(n *yaml.Node, where string, names ...string) error {
	n = resolveAlias(n)
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := resolveAlias(n.Content[i]); !slices.Contains(names, k.Value) {
			return fmt.Errorf("line %d: unknown field %q in %s", k.Line, k.Value, where)
		}
	}
	return nil
}

// field returns the value of key in the mapping n, or nil.
func field(n *yaml.Node, key string) *yaml.Node {
	n = resolveAlias(n)
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if resolveAlias(n.Content[i]).Value == key {
			return resolveAlias(n.Content[i+1])
		}
	}
	return nil
}

// platformOf is the platform an NSGo build target belongs to.
func platformOf(target string) string {
	platform, _, _ := strings.Cut(target, "_")
	return platform
}

// targetsOf returns the build targets a preset for platform may list.
func targetsOf(platform string) []string {
	var out []string
	for _, t := range exportTargets {
		if platformOf(t) == platform {
			out = append(out, t)
		}
	}
	return out
}

// readReleaseSettings reads dir's game.yaml, or returns no settings when it has none.
func readReleaseSettings(dir string) (releaseSettings, error) {
	path := filepath.Join(dir, gameFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return releaseSettings{}, nil
	}
	if err != nil {
		return releaseSettings{}, err
	}
	settings, err := parseReleaseSettings(data)
	if err != nil {
		return releaseSettings{}, fmt.Errorf("%s: %w", path, err)
	}
	return settings, nil
}

// nsgoSettings are the parts of nsgo.yaml that game release reads.
type nsgoSettings struct {
	// Channel is updates.channel, the channel nsgo build hands the packages out through when -channel does not say.
	Channel string
}

// readNSGoSettings reads dir's nsgo.yaml. The file is nsgo's, which validates it when it builds, so fields game release does not read are left alone rather than refused.
func readNSGoSettings(dir string) (nsgoSettings, error) {
	path := filepath.Join(dir, "nsgo.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nsgoSettings{}, fmt.Errorf("%s has no nsgo.yaml; run game release in the game's root or name it", dir)
	}
	if err != nil {
		return nsgoSettings{}, err
	}
	var file struct {
		Updates struct {
			Channel string `yaml:"channel"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nsgoSettings{}, fmt.Errorf("%s: %w", path, err)
	}
	return nsgoSettings{Channel: file.Updates.Channel}, nil
}

// releaseOptions are game release's flags.
type releaseOptions struct {
	// targets is -target, comma-separated; empty takes game.yaml's presets, else steamPlatforms for the steam channel, else releasePlatforms.
	targets string
	// output is -output; empty takes game.yaml's export.output, else nsgo.yaml's build.output.
	output string
	// channel is -channel, which nsgo build takes as the channel the packages are handed out through.
	channel string
	// notes is -release-notes, a Markdown file nsgo build puts in the appcasts.
	notes string
	// jobs is -jobs, how many targets build at once; 0 leaves nsgo's default.
	jobs int
}

// releaseArgs returns the arguments of the go command that builds the project in dir: go -C dir tool nsgo build with the targets and output resolved from opts, then game.yaml's settings, then the defaults, the channel coming from opts, then nsgo.yaml. nsgo.yaml's build.targets is not a default: it is plain nsgo build's everyday list, which in the starter holds a simulator build and leaves out Linux. Paths in opts are relative to the working directory, and game.yaml's output to dir.
func releaseArgs(dir string, opts releaseOptions, settings releaseSettings, nsgo nsgoSettings) ([]string, error) {
	targets := releasePlatforms
	channel := opts.channel
	if channel == "" {
		channel = nsgo.Channel
	}
	if channel == "steam" {
		targets = steamPlatforms
	}
	if settings.Targets != nil {
		targets = settings.Targets
	}
	if opts.targets != "" {
		targets = strings.Split(opts.targets, ",")
		for _, t := range targets {
			if t == "" {
				return nil, usagef("-target %q names an empty target", opts.targets)
			}
		}
	}
	args := []string{"-C", dir, "tool", "nsgo", "build", "-target", strings.Join(targets, ",")}
	switch {
	case opts.output != "":
		abs, err := filepath.Abs(opts.output)
		if err != nil {
			return nil, err
		}
		args = append(args, "-output", abs)
	case settings.Output != "":
		args = append(args, "-output", filepath.FromSlash(settings.Output))
	}
	if opts.channel != "" {
		args = append(args, "-channel", opts.channel)
	}
	if opts.notes != "" {
		abs, err := filepath.Abs(opts.notes)
		if err != nil {
			return nil, err
		}
		args = append(args, "-release-notes", abs)
	}
	if opts.jobs != 0 {
		args = append(args, "-jobs", strconv.Itoa(opts.jobs))
	}
	return args, nil
}

// shellQuote writes args as one line a POSIX shell reads back as the same arguments.
func shellQuote(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./,=:+@%", r))
		}) < 0 {
			quoted[i] = a
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func defineRelease(a *app, fs *flag.FlagSet) func() error {
	var opts releaseOptions
	fs.StringVar(&opts.targets, "target", "", "comma-separated nsgo build `targets` (default: game.yaml's export presets, else "+strings.Join(releasePlatforms, ",")+", or "+strings.Join(steamPlatforms, ",")+" for the steam channel)")
	fs.StringVar(&opts.output, "output", "", "the output `directory` (default: game.yaml's export.output, else nsgo.yaml's build.output)")
	fs.StringVar(&opts.channel, "channel", "", "the `channel` the packages are handed out through: direct, store, steam or none (default: nsgo.yaml's updates.channel)")
	fs.StringVar(&opts.notes, "release-notes", "", "a Markdown `file` of the release's notes for the update prompts")
	fs.IntVar(&opts.jobs, "jobs", 0, "how many targets build at once (default: nsgo's)")
	dryRun := fs.Bool("dry-run", false, "print the nsgo build command instead of running it")

	return func() error {
		if fs.NArg() > 1 {
			return usagef("takes one directory, not %q as well", fs.Args()[1:])
		}
		if opts.jobs < 0 {
			return usagef("-jobs %d must not be negative", opts.jobs)
		}
		dir := "."
		if fs.NArg() == 1 {
			dir = fs.Arg(0)
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		nsgo, err := readNSGoSettings(dir)
		if err != nil {
			return err
		}
		settings, err := readReleaseSettings(dir)
		if err != nil {
			return err
		}
		args, err := releaseArgs(dir, opts, settings, nsgo)
		if err != nil {
			return err
		}
		if *dryRun {
			fmt.Fprintln(a.stdout, shellQuote(append([]string{"go"}, args...)))
			return nil
		}
		if err := runNSGo(args, a.stdout, a.stderr); err != nil {
			return fmt.Errorf("nsgo build: %w", err)
		}
		return nil
	}
}
