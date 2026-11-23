package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"go.yaml.in/yaml/v3"
)

// devCommand runs the game under nsgo dev and points it at its asset directory, which the game watches to reload assets while it runs.
var devCommand = &command{
	name:    "dev",
	args:    "[dir] [nsgo dev flags]",
	summary: "Run the game with nsgo dev, reloading its assets in the running game when they are saved",
	define:  defineDev,
}

// devRun is one nsgo dev that game dev starts: the command line and what it adds to the environment.
type devRun struct {
	// args are the arguments after go: tool nsgo dev and its flags.
	args []string
	// env is added to game dev's own environment.
	env []string
	// stdout and stderr are where nsgo dev's output goes.
	stdout, stderr io.Writer
}

// startNSGoDev runs nsgo dev as run says and returns once it exits; tests replace it.
var startNSGoDev = runNSGoDev

// nativeTargets are the nsgo dev targets whose app runs on this machine and can read the asset directory; the rest run in a browser or on a device.
var nativeTargets = map[string]bool{"native": true, "tui": true}

func defineDev(a *app, fs *flag.FlagSet) func() error {
	assetsDir := fs.String("assets", "resources", "the directory the game's \"asset:\" URIs name, relative to the project directory, which the running game watches")
	target := fs.String("target", "", "the nsgo dev target, nsgo.yaml's dev.target when empty; assets reload on native and tui")
	fps := fs.Bool("fps", false, "show NSGo's performance monitor in the running game")

	return func() error {
		dir, passthrough := splitDevArgs(fs.Args())
		project := dir
		if project == "" {
			project = "."
		}
		info, err := os.Stat(project)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return usagef("%s is not a directory", project)
		}

		args := []string{"tool", "nsgo", "dev"}
		if *target != "" {
			args = append(args, "-target", *target)
		}
		if *fps {
			args = append(args, "-fps")
		}
		args = append(args, passthrough...)
		if dir != "" {
			args = append(args, dir)
		}

		var env []string
		assets := *assetsDir
		if !filepath.IsAbs(assets) {
			assets = filepath.Join(project, assets)
		}
		assets, err = filepath.Abs(assets)
		if err != nil {
			return err
		}
		if st, err := os.Stat(assets); err != nil || !st.IsDir() {
			fmt.Fprintf(a.stderr, "game dev: no asset directory at %s; assets will not reload\n", assets)
		} else {
			env = append(env, hotreload.AssetsDirEnv+"="+assets)
			t := *target
			if t == "" {
				t = projectDevTarget(project)
			}
			if nativeTargets[t] {
				fmt.Fprintf(a.stderr, "game dev: reloading assets from %s\n", assets)
			} else {
				fmt.Fprintf(a.stderr, "game dev: assets reload on the native and tui targets, not %s, whose app cannot read %s\n", t, assets)
			}
		}

		return startNSGoDev(context.Background(), devRun{args: args, env: env, stdout: a.stdout, stderr: a.stderr})
	}
}

// splitDevArgs splits what follows game dev's flags into the project directory, the first argument when it is not a flag, and the flags passed on to nsgo dev.
func splitDevArgs(args []string) (dir string, passthrough []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// projectDevTarget returns the dev.target of the nsgo.yaml in dir, "native" (nsgo dev's default) when it names none or cannot be read.
func projectDevTarget(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "nsgo.yaml"))
	if err != nil {
		return "native"
	}
	var cfg struct {
		Dev struct {
			Target string `yaml:"target"`
		} `yaml:"dev"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil || cfg.Dev.Target == "" {
		return "native"
	}
	return cfg.Dev.Target
}

// runNSGoDev runs go with run's arguments in this terminal until it exits. An interrupt, Control+C, reaches nsgo dev from the terminal, which stops the app and exits, so game dev waits for it instead of exiting first.
func runNSGoDev(ctx context.Context, run devRun) error {
	cmd := exec.CommandContext(ctx, "go", run.args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = run.stdout, run.stderr
	cmd.Env = append(os.Environ(), run.env...)

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go tool nsgo dev: %w", err)
	}
	return nil
}
