package gamecli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
)

// projectVerbs are what follows game project's flags.
var projectVerbs = []string{"check", "fmt", "sync", "targets"}

// projectCommand checks, formats and applies a game's project file, game.yaml.
var projectCommand = &command{
	name:    "project",
	args:    "check|fmt|sync|targets [dir]",
	summary: "Check a project's game.yaml, rewrite it in canonical form, write the fields it owns into nsgo.yaml, or print its export presets' build targets",
	define:  defineProject,
	complete: map[string]completion{
		"": oneOf(projectVerbs...),
	},
}

func defineProject(a *app, fs *flag.FlagSet) func() error {
	check := fs.Bool("check", false, "with fmt, fail unless game.yaml is in canonical form, and with sync, fail unless nsgo.yaml agrees with game.yaml, without rewriting either")

	return func() error {
		if fs.NArg() == 0 {
			return usagef("expected one of %s", strings.Join(projectVerbs, ", "))
		}
		verb, args := fs.Arg(0), fs.Args()[1:]
		if !slices.Contains(projectVerbs, verb) {
			return usagef("unknown verb %q; use %s", verb, strings.Join(projectVerbs, ", "))
		}
		if *check && verb != "fmt" && verb != "sync" {
			return usagef("-check is for fmt and sync")
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				return usagef("%s after %s: flags go before the verb, as in game project -check %s", arg, verb, verb)
			}
		}
		if len(args) > 1 {
			return usagef("%s takes at most one directory", verb)
		}
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}

		switch verb {
		case "check":
			if _, err := project.Check(os.DirFS(dir)); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "%s: ok\n", filepath.Join(dir, project.FileName))
			return nil
		case "fmt":
			return projectFormat(dir, *check, a.stdout)
		case "sync":
			return projectSync(dir, *check, a.stdout)
		default: // targets
			f, err := project.ReadFS(os.DirFS(dir))
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, strings.Join(f.BuildTargets(), ","))
			return nil
		}
	}
}

// projectFormat rewrites dir's game.yaml as project.Encode writes it, or with check reports whether it already is.
func projectFormat(dir string, check bool, stdout io.Writer) error {
	path := filepath.Join(dir, project.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := project.Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	canonical, err := project.Encode(f)
	if err != nil {
		return err
	}
	if bytes.Equal(canonical, data) {
		return nil
	}
	if check {
		return fmt.Errorf("%s is not in canonical form; run game project fmt", path)
	}
	if err := replaceFile(path, canonical); err != nil {
		return err
	}
	fmt.Fprintln(stdout, path)
	return nil
}

// projectSync writes the fields game.yaml owns into dir's nsgo.yaml, creating it when missing, or with check reports whether they agree.
func projectSync(dir string, check bool, stdout io.Writer) error {
	f, err := project.ReadFS(os.DirFS(dir))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, project.NSGoFileName)
	old, err := os.ReadFile(path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	if check {
		if missing {
			return fmt.Errorf("%s is missing; run game project sync", path)
		}
		if err := f.CheckNSGo(old); err != nil {
			return fmt.Errorf("%w; run game project sync", err)
		}
		return nil
	}
	synced, err := f.SyncNSGo(old)
	if err != nil {
		return err
	}
	if !missing && bytes.Equal(synced, old) {
		return nil
	}
	if missing {
		err = os.WriteFile(path, synced, 0o644)
	} else {
		err = replaceFile(path, synced)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, path)
	return nil
}
