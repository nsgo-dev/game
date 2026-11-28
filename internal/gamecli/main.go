// Package gamecli implements the game layer's command-line tool, shared by cmd/game (for `go tool game`, `go run`, and `nsgo game` inside a module) and cmd/nsgo-game (the `nsgo-game` extension binary on PATH for `nsgo game` outside a module).
package gamecli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
)

// app is what the subcommands write to, and the parts a test replaces: the audio encoder and how the editor is shown.
type app struct {
	// stdin is what the mcp subcommand reads its client's messages from.
	stdin          io.Reader
	stdout, stderr io.Writer
	// audio converts audio files for the audio subcommand.
	audio *audioconv.Encoder
	// launch shows the editor and returns once it closes.
	launch func(ed *editor.Editor)
	// nsgo runs `go tool nsgo` with args, for the replay and bench subcommands.
	nsgo func(args []string, stdout, stderr io.Writer) error
	// nsgoEnv runs `go tool nsgo` with extra KEY=VALUE environment entries, for the playtest subcommand.
	nsgoEnv func(env []string, args []string, stdout, stderr io.Writer) error
	// goRun runs the go command with args in dir, for the editor subcommand to run a project's own editor command.
	goRun func(dir string, args []string, stdout, stderr io.Writer) error
	// recentProjectsPath overrides the per-user recent projects YAML file path used by `game editor` (tests set it inside t.TempDir()).
	recentProjectsPath string
	// layoutPath overrides the per-user editor dock layout YAML file path used by `game editor` (tests set it inside t.TempDir()).
	layoutPath string
	// readBuildInfo reads the running binary's build info to warn if the CLI was built from a module other than github.com/nsgo-dev/game.
	readBuildInfo func() (*debug.BuildInfo, bool)
}

// command is one subcommand of game.
type command struct {
	// name is the word after game that runs it.
	name string
	// args is what follows its flags in its usage line, if anything.
	args string
	// summary is the one line game help shows for it.
	summary string
	// define adds the command's flags to fs and returns what runs once they are parsed, which reads the arguments left from fs.Args.
	define func(a *app, fs *flag.FlagSet) func() error
	// complete is what shell completion offers for a flag's value, keyed by the flag's name, and for the arguments after the flags, keyed by "". The shared -in and -out flags complete file paths without an entry; any other flag, or the arguments, with no entry complete nothing.
	complete map[string]completion
}

// commands are game's subcommands in the order game help lists them, help itself last.
var commands = []*command{packCommand, audioCommand, manifestCommand, texCommand, editorCommand, mcpCommand, releaseCommand, captureCommand, replayCommand, playtestCommand, benchCommand, sizeCommand, newCommand, projectCommand, devCommand, checkCommand, assetsCommand, generateCommand, mvCommand, indexCommand, libraryCommand, creditsCommand, pixelfxCommand, navmeshCommand, mergeCommand, schemaCommand, netCommand, wireCommand}

// helpSummary is the help subcommand's line in the listing.
const helpSummary = "Show the commands, or one command's flags"

// usageError is a mistake in how a command was called: it exits 2 and shows the command's usage.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// errUsageReported is returned when a subcommand already printed its flag parse error and usage and only needs exit code 2.
var errUsageReported = errors.New("usage reported")

// usagef returns a usageError with a formatted message.
func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// Main runs the game CLI with os.Args[1:] and exits with its exit code.
func Main() {
	os.Exit(Run(os.Args[1:]))
}

// Run runs the game CLI with args (the arguments after the command name) using the standard OS streams and returns the exit code: 0 on success or help, 1 when a command fails, 2 on usage errors.
func Run(args []string) int {
	return run(args, &app{
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		audio:         &audioconv.Encoder{},
		launch:        editorcmd.Show,
		nsgo:          runGoToolNSGo,
		nsgoEnv:       runGoToolNSGoEnv,
		goRun:         runGoIn,
		readBuildInfo: debug.ReadBuildInfo,
	})
}

// run runs game with args, the arguments after its name, and returns the exit code: 0 on success or help asked for, 1 when a command fails, 2 when it is called wrongly.
func run(args []string, a *app) int {
	if a.readBuildInfo != nil {
		if info, ok := a.readBuildInfo(); ok {
			warnUnexpectedModule(a.stderr, info)
		}
	}
	if len(args) == 0 {
		printUsage(a.stderr)
		return 2
	}
	name, rest := args[0], args[1:]
	switch name {
	case "help", "-h", "-help", "--help":
		return help(rest, a)
	}
	c := lookup(name)
	if c == nil {
		fmt.Fprintf(a.stderr, "game: unknown command %q\nRun 'game help' for the commands.\n", name)
		return 2
	}
	fs := newFlagSet(c, a.stderr)
	exec := c.define(a, fs)
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := exec(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if errors.Is(err, errUsageReported) {
			return 2
		}
		fmt.Fprintf(a.stderr, "game %s: %v\n", c.name, err)
		if _, ok := errors.AsType[*usageError](err); ok {
			fs.Usage()
			return 2
		}
		return 1
	}
	return 0
}

// help runs game help: the listing with no arguments, else the usage of the one command named.
func help(args []string, a *app) int {
	switch len(args) {
	case 0:
		printUsage(a.stdout)
		return 0
	case 1:
		if args[0] == "help" {
			fmt.Fprintf(a.stdout, "usage: game help [command]\n\n%s.\n", helpSummary)
			return 0
		}
		c := lookup(args[0])
		if c == nil {
			fmt.Fprintf(a.stderr, "game help: unknown command %q\nRun 'game help' for the commands.\n", args[0])
			return 2
		}
		fs := newFlagSet(c, a.stdout)
		c.define(a, fs)
		fs.Usage()
		return 0
	default:
		fmt.Fprintln(a.stderr, "usage: game help [command]")
		return 2
	}
}

// lookup returns the command called name, or nil.
func lookup(name string) *command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return nil
}

// printUsage writes game's usage and the list of its commands to w.
func printUsage(w io.Writer) {
	fmt.Fprint(w, "usage: game <command> [flags] [arguments]\n\nCommands:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	fmt.Fprintf(tw, "  help\t%s\n", helpSummary)
	_ = tw.Flush()
	fmt.Fprint(w, "\nRun 'game help <command>' for a command's flags.\n")
}

// newFlagSet returns the flag set for c, writing errors and usage to w: every command's usage is its usage line, its summary and its flags.
func newFlagSet(c *command, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("game "+c.name, flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Usage = func() {
		fmt.Fprintf(w, "usage: %s\n\n%s.\n", c.usage(), c.summary)
		hasFlags := false
		fs.VisitAll(func(*flag.Flag) { hasFlags = true })
		if hasFlags {
			fmt.Fprint(w, "\nFlags:\n")
			fs.PrintDefaults()
		}
	}
	return fs
}

// usage returns how c is called, its usage line without the "usage: " in front.
func (c *command) usage() string {
	line := "game " + c.name + " [flags]"
	if c.args != "" {
		line += " " + c.args
	}
	return line
}

// inFlag adds the -in flag the commands share for what they read.
func inFlag(fs *flag.FlagSet, value, usage string) *string {
	return fs.String("in", value, usage)
}

// outFlag adds the -out flag the commands share for what they write.
func outFlag(fs *flag.FlagSet, value, usage string) *string {
	return fs.String("out", value, usage)
}

// cliModulePath returns the module path that contains the CLI main package recorded in info, matching info.Path against info.Main and info.Deps by longest module-path prefix on a '/' boundary, or info.Main.Path when info.Path is empty. It returns "" when no module path can be determined.
func cliModulePath(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	if info.Path == "command-line-arguments" {
		return ""
	}
	if info.Path == "" {
		if info.Main.Path == "" || info.Main.Path == "command-line-arguments" {
			return ""
		}
		return info.Main.Path
	}
	best := ""
	consider := func(modPath string) {
		if modPath == "" || modPath == "command-line-arguments" {
			return
		}
		if info.Path == modPath || strings.HasPrefix(info.Path, modPath+"/") {
			if len(modPath) > len(best) {
				best = modPath
			}
		}
	}
	consider(info.Main.Path)
	for _, dep := range info.Deps {
		if dep != nil {
			consider(dep.Path)
		}
	}
	return best
}

// warnUnexpectedModule writes a one-line warning to w when info shows this CLI was built from a module path other than github.com/nsgo-dev/game (for example, the pre-migration github.com/nsgo-dev/game/packages/game module).
func warnUnexpectedModule(w io.Writer, info *debug.BuildInfo) {
	if w == nil {
		return
	}
	modPath := cliModulePath(info)
	if modPath == "" || modPath == gameModule {
		return
	}
	fmt.Fprintf(w, "game: warning: running from module %s instead of %s (use go run %s@latest or go tool game)\n", modPath, gameModule, gameTool)
}
