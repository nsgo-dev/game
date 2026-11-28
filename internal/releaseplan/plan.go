// Package releaseplan plans and formats `nsgo build` release invocations and credits checks shared by `cmd/game release` and the scene editor's Export panel.
package releaseplan

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
)

// ReleaseTag is the Go build tag passed to nsgo build so development-only code (pkg/tooling/gamedebug, pkg/tooling/hotreload, pkg/tooling/devstate, and physics/AI debug drawing) compiles its release stubs.
// Native release-only build tags in nsgo.yaml are tracked upstream in https://github.com/nsgo-dev/nsgo/issues/2695.
const ReleaseTag = "release"

// DefaultPlatforms are what game release builds when neither -target nor game.yaml's export presets say otherwise, as nsgo build -target names them: macos, windows, linux and android expand to their architectures, and ios and web are one target each.
var DefaultPlatforms = []string{"macos", "windows", "linux", "web", "ios", "android"}

// PlatformSupportURL is the English documentation anchor describing experimental targets and per-feature platform support.
const PlatformSupportURL = "https://game.nsgo.dev/docs/platforms/#experimental-targets"

// PlatformSupportURLZH is the Simplified Chinese documentation anchor describing experimental targets and per-feature platform support.
const PlatformSupportURLZH = "https://game.nsgo.dev/zh/docs/platforms/#实验性目标平台"

// ExperimentalInfo describes why an NSGo platform is marked experimental and where its support status and upstream tracking issue are documented.
type ExperimentalInfo struct {
	// Platform is the canonical NSGo platform name (such as "tvos", "watchos", "harmonyos", or "miniprogram").
	Platform string
	// Missing summarizes the core game capabilities unavailable on this platform in the current NSGo release.
	Missing string
	// IssueURL is the upstream nsgo-dev/nsgo tracking issue for this platform.
	IssueURL string
}

// Experimental is the single source of truth for NSGo platforms accepted as experimental by game.yaml export presets, game release, and the scene editor. None of these platforms are built or vetted in CI, and NSGo disables audio and GPU views on all four. When NSGo enables audio and a GPU view on one of them, remove it from this set and add it to .github/workflows/check.yml in the same pull request.
var Experimental = map[string]ExperimentalInfo{
	"tvos": {
		Platform: "tvos",
		Missing:  "no audio, no GPU view, and not tested in CI",
		IssueURL: "https://github.com/nsgo-dev/nsgo/issues/1317",
	},
	"watchos": {
		Platform: "watchos",
		Missing:  "no audio, no GPU view, no gamepad, and not tested in CI",
		IssueURL: "https://github.com/nsgo-dev/nsgo/issues/1318",
	},
	"harmonyos": {
		Platform: "harmonyos",
		Missing:  "no audio, no GPU view, no gamepad, and not tested in CI",
		IssueURL: "https://github.com/nsgo-dev/nsgo/issues/1521",
	},
	"miniprogram": {
		Platform: "miniprogram",
		Missing:  "no audio, no GPU view, no gamepad, and not tested in CI",
		IssueURL: "https://github.com/nsgo-dev/nsgo/issues/1505",
	},
}

// PlatformOf returns the NSGo platform name that target belongs to (the segment before the first underscore, or target itself when there is none).
func PlatformOf(target string) string {
	platform, _, _ := strings.Cut(target, "_")
	return platform
}

// IsExperimental reports whether targetOrPlatform belongs to an experimental platform.
func IsExperimental(targetOrPlatform string) bool {
	_, ok := Experimental[PlatformOf(targetOrPlatform)]
	return ok
}

// ExperimentalInfoFor returns the experimental metadata for targetOrPlatform, or false when it is not experimental.
func ExperimentalInfoFor(targetOrPlatform string) (ExperimentalInfo, bool) {
	info, ok := Experimental[PlatformOf(targetOrPlatform)]
	return info, ok
}

// ExperimentalPlatforms returns the unique experimental platforms referenced by targets, preserving first-seen order.
func ExperimentalPlatforms(targets []string) []ExperimentalInfo {
	var out []ExperimentalInfo
	seen := make(map[string]bool)
	for _, t := range targets {
		plat := PlatformOf(t)
		if seen[plat] {
			continue
		}
		if info, ok := Experimental[plat]; ok {
			seen[plat] = true
			out = append(out, info)
		}
	}
	return out
}

// ExperimentalDocsURL returns the platform support documentation URL for lang ("zh" for Simplified Chinese, English otherwise).
func ExperimentalDocsURL(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return PlatformSupportURLZH
	}
	return PlatformSupportURL
}

// FormatExperimentalWarning formats the single-line warning printed by game release and the editor's export log for an experimental platform.
func FormatExperimentalWarning(info ExperimentalInfo) string {
	return fmt.Sprintf("game release: warning: target platform %q is experimental (%s); see %s and %s", info.Platform, info.Missing, PlatformSupportURL, info.IssueURL)
}

// SteamPlatforms are what game release builds by default for the steam channel, whose depots nsgo build makes only of desktop targets.
var SteamPlatforms = []string{"macos", "windows", "linux"}

// Channels are the distribution channels nsgo build accepts for -channel.
var Channels = []string{"direct", "store", "steam", "none"}

// DefaultOutput is the directory nsgo build writes to when nsgo.yaml's build.output is empty, from NSGo v0.3.0's cmd/nsgo/project.go.
const DefaultOutput = "dist"

// NoCreditsNote is what game release prints when game.yaml has no assets section, and so no credits file is written.
const NoCreditsNote = "game release: note: game.yaml has no assets section, so no " + credits.FileName + " is written; credit the assets the game ships yourself"

// ErrInvalidTarget is wrapped when Options.Targets contains an empty target entry.
var ErrInvalidTarget = errors.New("invalid target")

// ErrExperimentalTarget is wrapped when Options.Strict refuses one or more experimental target platforms.
var ErrExperimentalTarget = errors.New("experimental target platform")

// ErrHostUnavailableTarget is wrapped when an explicitly requested -target cannot be built on Options.Host.
var ErrHostUnavailableTarget = errors.New("target cannot be built on this host")

// ErrNoBuildableTargets is wrapped when every resolved preset or default target is skipped on Options.Host.
var ErrNoBuildableTargets = errors.New("no buildable targets on this host")

// Host describes a machine's ability to build host-restricted NSGo targets (macOS and Apple mobile SDKs).
type Host struct {
	// GOOS is the host operating system (such as "darwin", "linux", or "windows").
	GOOS string
	// HasAppleSDK reports whether Xcode provides the Apple SDK named sdk (such as "iphoneos", "iphonesimulator", "appletvos", "appletvsimulator", "watchos", or "watchsimulator").
	HasAppleSDK func(sdk string) bool
}

// CurrentHost returns a Host describing the running machine. On darwin, Apple mobile SDK availability is probed lazily via `xcrun --sdk <sdk> --show-sdk-path` and cached per SDK name.
func CurrentHost() *Host {
	cache := make(map[string]bool)
	return &Host{
		GOOS: runtime.GOOS,
		HasAppleSDK: func(sdk string) bool {
			if ok, cached := cache[sdk]; cached {
				return ok
			}
			ok := exec.Command("xcrun", "--sdk", sdk, "--show-sdk-path").Run() == nil
			cache[sdk] = ok
			return ok
		},
	}
}

// AllTargetsHost returns a Host that reports every target as buildable, useful in tests that mock nsgo build across platforms.
func AllTargetsHost() *Host {
	return &Host{
		GOOS:        "darwin",
		HasAppleSDK: func(string) bool { return true },
	}
}

// SkippedTarget describes a target that cannot be built on the current host and why.
type SkippedTarget struct {
	Target string
	Reason string
}

// FormatSkippedTarget formats the single-line message printed when game release skips an unavailable preset or default target.
func FormatSkippedTarget(s SkippedTarget) string {
	return fmt.Sprintf("game release: skipped %s: %s", s.Target, s.Reason)
}

// UnsignedTarget describes a target built without store/device signing configuration in nsgo.yaml.
type UnsignedTarget struct {
	Target string
	Detail string
}

// FormatUnsignedNote formats the single-line note printed when a release target such as ios builds unsigned because nsgo.yaml lacks signing configuration.
func FormatUnsignedNote(u UnsignedTarget) string {
	return fmt.Sprintf("game release: note: %s is unsigned (%s)", u.Target, u.Detail)
}

// UnavailableTargetsError is returned when explicit -target arguments cannot be built on Options.Host.
type UnavailableTargetsError struct {
	Targets []SkippedTarget
}

func (e *UnavailableTargetsError) Error() string {
	if len(e.Targets) == 1 {
		t := e.Targets[0]
		return fmt.Sprintf("cannot build requested target %q on this host: %s", t.Target, t.Reason)
	}
	var b strings.Builder
	b.WriteString("cannot build requested targets on this host:")
	for _, t := range e.Targets {
		fmt.Fprintf(&b, "\n  - %s: %s", t.Target, t.Reason)
	}
	return b.String()
}

func (e *UnavailableTargetsError) Unwrap() error {
	return ErrHostUnavailableTarget
}

func appleSDKFor(target string) (string, bool) {
	switch target {
	case "ios":
		return "iphoneos", true
	case "ios_simulator":
		return "iphonesimulator", true
	case "tvos":
		return "appletvos", true
	case "tvos_simulator":
		return "appletvsimulator", true
	case "watchos":
		return "watchos", true
	case "watchos_simulator":
		return "watchsimulator", true
	}
	return "", false
}

func hostUnavailableReason(h *Host, target string) string {
	if h == nil {
		return ""
	}
	if PlatformOf(target) == "macos" {
		if h.GOOS != "darwin" {
			return "needs macOS"
		}
		return ""
	}
	if sdk, ok := appleSDKFor(target); ok {
		if h.GOOS != "darwin" {
			return "needs macOS with Xcode"
		}
		if h.HasAppleSDK == nil || !h.HasAppleSDK(sdk) {
			return fmt.Sprintf("needs Xcode (%s SDK unavailable)", sdk)
		}
	}
	return ""
}

// ExperimentalTargetsError is returned by Build when Options.Strict is true and the resolved build targets include one or more experimental platforms.
type ExperimentalTargetsError struct {
	Platforms []ExperimentalInfo
}

func (e *ExperimentalTargetsError) Error() string {
	if len(e.Platforms) == 1 {
		p := e.Platforms[0]
		return fmt.Sprintf("experimental target platform %q refused by -strict (%s); see %s and %s", p.Platform, p.Missing, PlatformSupportURL, p.IssueURL)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "experimental target platforms refused by -strict (see %s):", PlatformSupportURL)
	for _, p := range e.Platforms {
		fmt.Fprintf(&b, "\n  - %s (%s; %s)", p.Platform, p.Missing, p.IssueURL)
	}
	return b.String()
}

func (e *ExperimentalTargetsError) Unwrap() error {
	return ErrExperimentalTarget
}

// NSGoIconConfig holds the icon settings in nsgo.yaml read by release readiness checks.
type NSGoIconConfig struct {
	// Apple is icon.apple, an Icon Composer .icon (macOS/iOS) or .icns (macOS only).
	Apple string `yaml:"apple"`
	// PNG is icon.png, the square PNG icon fallback used across targets including iOS.
	PNG string `yaml:"png"`
}

// NSGoSignConfig holds the signing settings in nsgo.yaml read by release readiness checks.
type NSGoSignConfig struct {
	Identity     string `yaml:"identity"`
	Profile      string `yaml:"profile"`
	MacOSProfile string `yaml:"macos_profile"`
	Keystore     string `yaml:"keystore"`
	KeyAlias     string `yaml:"key_alias"`
	Certificate  string `yaml:"certificate"`
	SignCommand  string `yaml:"sign_command"`
	Notarize     string `yaml:"notarize"`
}

// NSGoPrivacyConfig holds apple.privacy in nsgo.yaml, which nsgo build merges into PrivacyInfo.xcprivacy.
type NSGoPrivacyConfig struct {
	AccessedAPIs    map[string][]string `yaml:"accessed_apis"`
	CollectedData   []map[string]any    `yaml:"collected_data"`
	Tracking        *bool               `yaml:"tracking"`
	TrackingDomains []string            `yaml:"tracking_domains"`
}

// NSGoAppleConfig holds the Apple-specific settings in nsgo.yaml read by release readiness checks.
type NSGoAppleConfig struct {
	Privacy NSGoPrivacyConfig `yaml:"privacy"`
}

// NSGoSettings are the parts of nsgo.yaml that game release, readiness checks, and the editor's publish flow read.
type NSGoSettings struct {
	// Name is nsgo.yaml's name, used by nsgo build to name package directories and desktop executables.
	Name string
	// ID is nsgo.yaml's id, the reverse-DNS bundle identifier / application ID.
	ID string
	// Version is nsgo.yaml's version, used by nsgo build to name package directories.
	Version string
	// BuildNumber is nsgo.yaml's build_number (CFBundleVersion, versionCode) as written; empty when the file leaves it to derive from version.
	BuildNumber string
	// Channel is updates.channel, the channel nsgo build hands the packages out through when -channel does not say.
	Channel string
	// Output is build.output, the directory nsgo build writes to when neither -output nor game.yaml's export.output says, relative to the project unless absolute; empty is nsgo's default, dist.
	Output string
	// ItchProject is itch.project, the itch.io owner/game slug when configured.
	ItchProject string
	// ItchChannels is itch.channels, mapping NSGo build target names to custom itch.io channel names.
	ItchChannels map[string]string
	// Icon is nsgo.yaml's icon section.
	Icon NSGoIconConfig
	// Sign is nsgo.yaml's sign section.
	Sign NSGoSignConfig
	// SignProfile is sign.profile (trimmed), the iOS provisioning profile path; empty means ios device builds are unsigned.
	SignProfile string
	// Apple is nsgo.yaml's apple section.
	Apple NSGoAppleConfig
}

// ReadNSGoSettings reads dir's nsgo.yaml. The file is nsgo's, which validates it when it builds, so fields game release does not read are left alone rather than refused.
func ReadNSGoSettings(dir string) (NSGoSettings, error) {
	path := filepath.Join(dir, project.NSGoFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NSGoSettings{}, fmt.Errorf("%s has no %s; run game release in the game's root or name it", dir, project.NSGoFileName)
	}
	if err != nil {
		return NSGoSettings{}, err
	}
	return parseNSGoSettings(path, data)
}

func parseNSGoSettings(path string, data []byte) (NSGoSettings, error) {
	var file struct {
		Name        string `yaml:"name"`
		ID          string `yaml:"id"`
		Version     string `yaml:"version"`
		BuildNumber string `yaml:"build_number"`
		Updates     struct {
			Channel string `yaml:"channel"`
		} `yaml:"updates"`
		Build struct {
			Output string `yaml:"output"`
		} `yaml:"build"`
		Itch struct {
			Project  string            `yaml:"project"`
			Channels map[string]string `yaml:"channels"`
		} `yaml:"itch"`
		Icon  NSGoIconConfig  `yaml:"icon"`
		Sign  NSGoSignConfig  `yaml:"sign"`
		Apple NSGoAppleConfig `yaml:"apple"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return NSGoSettings{}, fmt.Errorf("%s: %w", path, err)
	}
	var itchChannels map[string]string
	if len(file.Itch.Channels) > 0 {
		itchChannels = make(map[string]string, len(file.Itch.Channels))
		for k, v := range file.Itch.Channels {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k != "" && v != "" {
				itchChannels[k] = v
			}
		}
		if len(itchChannels) == 0 {
			itchChannels = nil
		}
	}
	return NSGoSettings{
		Name:         file.Name,
		ID:           file.ID,
		Version:      file.Version,
		BuildNumber:  strings.TrimSpace(file.BuildNumber),
		Channel:      strings.TrimSpace(file.Updates.Channel),
		Output:       strings.TrimSpace(file.Build.Output),
		ItchProject:  strings.TrimSpace(file.Itch.Project),
		ItchChannels: itchChannels,
		Icon:         file.Icon,
		Sign:         file.Sign,
		SignProfile:  strings.TrimSpace(file.Sign.Profile),
		Apple:        file.Apple,
	}, nil
}

// Settings are the parts of game.yaml that game release reads: the build output directory, the build targets of the export presets and the assets section, whose assets the credits file lists.
type Settings struct {
	// Output is export.output, a slash-separated directory inside the project; empty leaves nsgo.yaml's build.output.
	Output string
	// Targets are the presets' targets in order, a preset without targets giving its platform name; nil without presets.
	Targets []string
	// Assets is the assets section as package project parses it with the whole file; nil when game.yaml has none, and then no credits file is written.
	Assets *project.Assets
}

// SettingsFromProject extracts release Settings from an in-memory project.File.
func SettingsFromProject(f *project.File) Settings {
	if f == nil {
		return Settings{}
	}
	var s Settings
	if f.Export != nil {
		s.Output = f.Export.Output
		if len(f.Export.Presets) > 0 {
			s.Targets = f.BuildTargets()
		}
	}
	s.Assets = f.Assets
	return s
}

// Options are game release's build flags.
type Options struct {
	// Targets is -target, comma-separated; empty takes game.yaml's presets, else SteamPlatforms for the steam channel, else DefaultPlatforms.
	Targets string
	// Output is -output; empty takes game.yaml's export.output, else nsgo.yaml's build.output.
	Output string
	// Channel is -channel, which nsgo build takes as the channel the packages are handed out through.
	Channel string
	// Notes is -release-notes, a Markdown file nsgo build puts in the appcasts.
	Notes string
	// Jobs is -jobs, how many targets build at once; 0 leaves nsgo's default.
	Jobs int
	// Strict is -strict, refusing experimental target platforms with *ExperimentalTargetsError instead of printing warnings.
	Strict bool
	// Host, when non-nil, filters preset/default targets that this host cannot build into Plan.Skipped and fails with *UnavailableTargetsError if an explicit Targets entry cannot build.
	Host *Host
}

// ResolveTargets resolves the ordered list of nsgo build targets from opts, settings, and nsgo.
func ResolveTargets(opts Options, settings Settings, nsgo NSGoSettings) ([]string, error) {
	targets := DefaultPlatforms
	channel := opts.Channel
	if channel == "" {
		channel = nsgo.Channel
	}
	if channel == "steam" {
		targets = SteamPlatforms
	}
	if settings.Targets != nil {
		targets = settings.Targets
	}
	if opts.Targets != "" {
		targets = strings.Split(opts.Targets, ",")
		for _, t := range targets {
			if t == "" {
				return nil, fmt.Errorf("%w: -target %q names an empty target", ErrInvalidTarget, opts.Targets)
			}
		}
	}
	return append([]string(nil), targets...), nil
}

func resolvePlanTargets(opts Options, settings Settings, nsgo NSGoSettings) (targets []string, skipped []SkippedTarget, err error) {
	raw, err := ResolveTargets(opts, settings, nsgo)
	if err != nil {
		return nil, nil, err
	}
	if opts.Host == nil {
		return raw, nil, nil
	}
	if opts.Targets != "" {
		var unavailable []SkippedTarget
		for _, t := range raw {
			if why := hostUnavailableReason(opts.Host, t); why != "" {
				unavailable = append(unavailable, SkippedTarget{Target: t, Reason: why})
			}
		}
		if len(unavailable) > 0 {
			return nil, nil, &UnavailableTargetsError{Targets: unavailable}
		}
		return raw, nil, nil
	}
	kept := make([]string, 0, len(raw))
	for _, t := range raw {
		if why := hostUnavailableReason(opts.Host, t); why != "" {
			skipped = append(skipped, SkippedTarget{Target: t, Reason: why})
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		parts := make([]string, len(skipped))
		for i, s := range skipped {
			parts[i] = fmt.Sprintf("%s (%s)", s.Target, s.Reason)
		}
		return nil, skipped, fmt.Errorf("%w: skipped %s", ErrNoBuildableTargets, strings.Join(parts, ", "))
	}
	return kept, skipped, nil
}

// OutputDir returns the absolute directory nsgo build writes the release to, as Args passes it and nsgo resolves it: -output, relative to the working directory, else game.yaml's export.output, else nsgo.yaml's build.output, else dist, these relative to dir.
func OutputDir(dir string, opts Options, settings Settings, nsgo NSGoSettings) (string, error) {
	switch {
	case opts.Output != "":
		return filepath.Abs(opts.Output)
	case settings.Output != "":
		return filepath.Join(dir, filepath.FromSlash(settings.Output)), nil
	case nsgo.Output != "":
		if filepath.IsAbs(nsgo.Output) {
			return filepath.Clean(nsgo.Output), nil
		}
		return filepath.Join(dir, filepath.FromSlash(nsgo.Output)), nil
	}
	return filepath.Join(dir, DefaultOutput), nil
}

// Args returns the arguments of the go command that builds the project in dir: go -C dir tool nsgo build with the targets and output resolved from opts, then game.yaml's settings, then the defaults, the channel coming from opts, then nsgo.yaml. nsgo.yaml's build.targets is not a default: it holds the portable cross-host list for a plain nsgo build, while game.yaml's export presets hold the full release list (including macOS and iOS). Paths in opts are relative to the working directory, and game.yaml's output to dir.
func Args(dir string, opts Options, settings Settings, nsgo NSGoSettings) ([]string, error) {
	targets, _, err := resolvePlanTargets(opts, settings, nsgo)
	if err != nil {
		return nil, err
	}
	return buildArgs(dir, opts, settings, targets)
}

func buildArgs(dir string, opts Options, settings Settings, targets []string) ([]string, error) {
	args := []string{"-C", dir, "tool", "nsgo", "build", "-target", strings.Join(targets, ",")}
	switch {
	case opts.Output != "":
		abs, err := filepath.Abs(opts.Output)
		if err != nil {
			return nil, err
		}
		args = append(args, "-output", abs)
	case settings.Output != "":
		args = append(args, "-output", filepath.FromSlash(settings.Output))
	}
	if opts.Channel != "" {
		args = append(args, "-channel", opts.Channel)
	}
	if opts.Notes != "" {
		abs, err := filepath.Abs(opts.Notes)
		if err != nil {
			return nil, err
		}
		args = append(args, "-release-notes", abs)
	}
	if opts.Jobs != 0 {
		args = append(args, "-jobs", strconv.Itoa(opts.Jobs))
	}
	return args, nil
}

// ShellQuote writes args as one line a POSIX shell reads back as the same arguments.
func ShellQuote(args []string) string {
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

// ReleaseEnv returns the default environment overrides applied to release invocations of nsgo build.
func ReleaseEnv() []string {
	return []string{"GOFLAGS=-tags=" + ReleaseTag}
}

// GOFLAGSWithReleaseTag returns a GOFLAGS value equivalent to existing with ReleaseTag merged into its -tags setting. Non-tag flags and any existing tags are preserved without duplicating ReleaseTag.
func GOFLAGSWithReleaseTag(existing string) string {
	return MergeGOFLAGS(existing, "-tags="+ReleaseTag)
}

// MergeGOFLAGS combines base and extra GOFLAGS values, keeping all non-tag flags and merging the effective -tags lists of both without duplicates. Within each input, a later -tags= replaces an earlier one, matching Go's own GOFLAGS parsing.
func MergeGOFLAGS(base, extra string) string {
	baseOther, baseTags := splitGOFLAGS(base)
	extraOther, extraTags := splitGOFLAGS(extra)

	out := make([]string, 0, len(baseOther)+len(extraOther)+1)
	out = append(out, baseOther...)
	out = append(out, extraOther...)

	mergedTags := make([]string, 0, len(baseTags)+len(extraTags))
	for _, tag := range baseTags {
		if !slices.Contains(mergedTags, tag) {
			mergedTags = append(mergedTags, tag)
		}
	}
	for _, tag := range extraTags {
		if !slices.Contains(mergedTags, tag) {
			mergedTags = append(mergedTags, tag)
		}
	}
	if len(mergedTags) > 0 {
		out = append(out, "-tags="+strings.Join(mergedTags, ","))
	}
	return strings.Join(out, " ")
}

func splitGOFLAGS(flags string) (other, tags []string) {
	for _, field := range strings.Fields(flags) {
		norm := field
		if strings.HasPrefix(norm, "--") {
			norm = norm[1:]
		}
		if value, ok := strings.CutPrefix(norm, "-tags="); ok {
			tags = tags[:0]
			for _, raw := range strings.Split(value, ",") {
				if tag := strings.TrimSpace(raw); tag != "" && !slices.Contains(tags, tag) {
					tags = append(tags, tag)
				}
			}
			continue
		}
		other = append(other, field)
	}
	return other, tags
}

// WithReleaseEnv returns a copy of baseEnv with GOFLAGS updated so nsgo build compiles with ReleaseTag. When baseEnv is nil, os.Environ() is used.
func WithReleaseEnv(baseEnv []string) []string {
	return ApplyEnv(baseEnv, ReleaseEnv())
}

// ApplyEnv returns a copy of baseEnv with overrides ("KEY=VALUE") applied. When baseEnv is nil, os.Environ() is used. A GOFLAGS override is merged into any existing GOFLAGS entry via MergeGOFLAGS so caller flags such as -mod=mod or custom build tags are preserved.
func ApplyEnv(baseEnv, overrides []string) []string {
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	if len(overrides) == 0 {
		return slices.Clone(baseEnv)
	}
	overrideKeys := make(map[string]string, len(overrides))
	overrideOrder := make([]string, 0, len(overrides))
	for _, entry := range overrides {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || k == "" {
			continue
		}
		upper := strings.ToUpper(k)
		if _, seen := overrideKeys[upper]; !seen {
			overrideOrder = append(overrideOrder, upper)
		}
		if upper == "GOFLAGS" {
			overrideKeys[upper] = MergeGOFLAGS(overrideKeys[upper], v)
		} else {
			overrideKeys[upper] = v
		}
	}

	var existingGOFLAGS string
	out := make([]string, 0, len(baseEnv)+len(overrideOrder))
	for _, entry := range baseEnv {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		upper := strings.ToUpper(k)
		if _, overridden := overrideKeys[upper]; overridden {
			if upper == "GOFLAGS" {
				existingGOFLAGS = v
			}
			continue
		}
		out = append(out, entry)
	}

	for _, upper := range overrideOrder {
		v := overrideKeys[upper]
		if upper == "GOFLAGS" {
			out = append(out, "GOFLAGS="+MergeGOFLAGS(existingGOFLAGS, v))
			continue
		}
		// Preserve original casing from the last override with this key.
		key := upper
		for _, entry := range overrides {
			if k, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(k, upper) {
				key = k
			}
		}
		out = append(out, key+"="+v)
	}
	return out
}

// formatEnvCommand formats env assignments followed by cmdArgs as a POSIX shell line.
func formatEnvCommand(env, cmdArgs []string) string {
	parts := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			parts = append(parts, k+"="+ShellQuote([]string{v}))
		}
	}
	parts = append(parts, ShellQuote(cmdArgs))
	return strings.Join(parts, " ")
}

// Plan is a validated release build plan ready for dry-run printing or execution.
type Plan struct {
	// Dir is the absolute project directory.
	Dir string
	// AppName is nsgo.yaml's name, used to locate built desktop binaries under OutputDir.
	AppName string
	// AppVersion is nsgo.yaml's version, used to locate built desktop binaries under OutputDir.
	AppVersion string
	// Targets are the resolved nsgo build targets in build order.
	Targets []string
	// Skipped lists preset or default targets omitted because Options.Host cannot build them.
	Skipped []SkippedTarget
	// Unsigned lists build targets (such as ios without sign.profile) that will be produced unsigned rather than dropped.
	Unsigned []UnsignedTarget
	// Experimental lists the unique experimental platforms referenced by Targets, in first-seen order.
	Experimental []ExperimentalInfo
	// Env are the environment variable overrides (such as GOFLAGS=-tags=release) applied when executing Args.
	Env []string
	// Args are the arguments passed to the `go` command.
	Args []string
	// CommandLine is the POSIX-quoted command (with Env prefix) printed by -dry-run.
	CommandLine string
	// OutputDir is the resolved absolute directory where release packages are written.
	OutputDir string
	// AssetsConfigured reports whether game.yaml declared an assets section.
	AssetsConfigured bool
	// Credits are the collected asset credits when AssetsConfigured is true.
	Credits []credits.Entry
	// CreditsPath is the full path where CREDITS.md will be written, or empty when AssetsConfigured is false.
	CreditsPath string
}

// Build resolves nsgo.yaml, command arguments, output directory, experimental target warnings, and asset credits for a release build in dir. It fails before building when nsgo.yaml is missing, options are invalid, Options.Strict refuses an experimental target (*ExperimentalTargetsError), or any declared asset has no credit (*credits.MissingError).
func Build(dir string, opts Options, settings Settings) (*Plan, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	nsgo, err := ReadNSGoSettings(absDir)
	if err != nil {
		return nil, err
	}
	targets, skipped, err := resolvePlanTargets(opts, settings, nsgo)
	if err != nil {
		return nil, err
	}
	exp := ExperimentalPlatforms(targets)
	if opts.Strict && len(exp) > 0 {
		return nil, &ExperimentalTargetsError{Platforms: exp}
	}
	var unsigned []UnsignedTarget
	if slices.Contains(targets, "ios") && strings.TrimSpace(nsgo.SignProfile) == "" {
		unsigned = append(unsigned, UnsignedTarget{
			Target: "ios",
			Detail: "no sign.profile in nsgo.yaml; building an unsigned IPA — set sign.profile to install on devices or upload to App Store Connect",
		})
	}
	args, err := buildArgs(absDir, opts, settings, targets)
	if err != nil {
		return nil, err
	}
	outDir, err := OutputDir(absDir, opts, settings, nsgo)
	if err != nil {
		return nil, err
	}
	env := ReleaseEnv()
	p := &Plan{
		Dir:              absDir,
		AppName:          nsgo.Name,
		AppVersion:       nsgo.Version,
		Targets:          targets,
		Skipped:          skipped,
		Unsigned:         unsigned,
		Experimental:     exp,
		Env:              env,
		Args:             args,
		CommandLine:      formatEnvCommand(env, append([]string{"go"}, args...)),
		OutputDir:        outDir,
		AssetsConfigured: settings.Assets != nil,
	}
	if settings.Assets != nil {
		entries, err := credits.Collect(absDir, settings.Assets)
		if err != nil {
			return nil, err
		}
		p.Credits = entries
		p.CreditsPath = filepath.Join(outDir, credits.FileName)
	} else {
		if _, err := credits.CheckGeneratedLicenses(absDir); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// WriteDryRun writes the exact dry-run lines printed by `game release -dry-run` for p.
func WriteDryRun(w io.Writer, p *Plan) {
	fmt.Fprintln(w, p.CommandLine)
	if p.CreditsPath != "" {
		fmt.Fprintf(w, "# then writes the credits of %d assets to %s\n", len(p.Credits), p.CreditsPath)
	}
}
