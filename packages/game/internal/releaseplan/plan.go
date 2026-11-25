// Package releaseplan plans and formats `nsgo build` release invocations and credits checks shared by `cmd/game release` and the scene editor's Export panel.
package releaseplan

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
)

// DefaultPlatforms are what game release builds when neither -target nor game.yaml's export presets say otherwise, as nsgo build -target names them: macos, windows, linux and android expand to their architectures, and ios and web are one target each.
var DefaultPlatforms = []string{"macos", "windows", "linux", "web", "ios", "android"}

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

// NSGoSettings are the parts of nsgo.yaml that game release reads.
type NSGoSettings struct {
	// Channel is updates.channel, the channel nsgo build hands the packages out through when -channel does not say.
	Channel string
	// Output is build.output, the directory nsgo build writes to when neither -output nor game.yaml's export.output says, relative to the project unless absolute; empty is nsgo's default, dist.
	Output string
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
	var file struct {
		Updates struct {
			Channel string `yaml:"channel"`
		} `yaml:"updates"`
		Build struct {
			Output string `yaml:"output"`
		} `yaml:"build"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return NSGoSettings{}, fmt.Errorf("%s: %w", path, err)
	}
	return NSGoSettings{Channel: file.Updates.Channel, Output: file.Build.Output}, nil
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

// Args returns the arguments of the go command that builds the project in dir: go -C dir tool nsgo build with the targets and output resolved from opts, then game.yaml's settings, then the defaults, the channel coming from opts, then nsgo.yaml. nsgo.yaml's build.targets is not a default: it is plain nsgo build's everyday list, which in the starter holds a simulator build and leaves out Linux. Paths in opts are relative to the working directory, and game.yaml's output to dir.
func Args(dir string, opts Options, settings Settings, nsgo NSGoSettings) ([]string, error) {
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

// Plan is a validated release build plan ready for dry-run printing or execution.
type Plan struct {
	// Dir is the absolute project directory.
	Dir string
	// Args are the arguments passed to the `go` command.
	Args []string
	// CommandLine is the POSIX-quoted `go ...` command printed by -dry-run.
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

// Build resolves nsgo.yaml, command arguments, output directory, and asset credits for a release build in dir. It fails before building when nsgo.yaml is missing, options are invalid, or any declared asset has no credit (*credits.MissingError).
func Build(dir string, opts Options, settings Settings) (*Plan, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	nsgo, err := ReadNSGoSettings(absDir)
	if err != nil {
		return nil, err
	}
	args, err := Args(absDir, opts, settings, nsgo)
	if err != nil {
		return nil, err
	}
	outDir, err := OutputDir(absDir, opts, settings, nsgo)
	if err != nil {
		return nil, err
	}
	p := &Plan{
		Dir:              absDir,
		Args:             args,
		CommandLine:      ShellQuote(append([]string{"go"}, args...)),
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
