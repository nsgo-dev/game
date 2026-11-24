package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
)

// mvCommand moves a file or directory of a project and rewrites every reference to it, as refs.PlanMove plans.
var mvCommand = &command{
	name:    "mv",
	args:    "<from> <to>",
	summary: "Move or rename a file or directory of a project and rewrite every reference to it, keeping the project index up to date",
	define:  defineMv,
	complete: map[string]completion{
		"": filePaths,
	},
}

// indexCommand assigns uids, follows files moved without the tools and writes the project index, as refs.PlanIndex plans.
var indexCommand = &command{
	name:    "index",
	args:    "[dir]",
	summary: "Give every scene and resource file a uid, point references at files moved without the tools, and write the project index",
	define:  defineIndex,
	complete: map[string]completion{
		"": filePaths,
	},
}

func defineMv(a *app, fs *flag.FlagSet) func() error {
	inArg := inFlag(fs, "", "project directory (defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod)")
	dryRun := fs.Bool("dry-run", false, "print what would change without changing anything")

	return func() error {
		if fs.NArg() != 2 {
			return usagef("expected a file to move and where to move it, got %d arguments", fs.NArg())
		}
		dir, err := projectDir(*inArg)
		if err != nil {
			return err
		}
		from, err := projectPath(dir, fs.Arg(0))
		if err != nil {
			return err
		}
		to, err := projectPath(dir, fs.Arg(1))
		if err != nil {
			return err
		}
		plan, err := refs.PlanMove(os.DirFS(dir), from, to)
		if err != nil {
			return err
		}
		return finishPlan(a.stdout, dir, plan, *dryRun)
	}
}

func defineIndex(a *app, fs *flag.FlagSet) func() error {
	inArg := inFlag(fs, "", "project directory (alternative to the positional argument; defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod)")
	dryRun := fs.Bool("dry-run", false, "print what would change without changing anything")

	return func() error {
		in := strings.TrimSpace(*inArg)
		switch {
		case in != "" && fs.NArg() > 0:
			return usagef("specify either -in or a positional directory, not both")
		case fs.NArg() > 1:
			return usagef("expected at most one directory, got %d", fs.NArg())
		case fs.NArg() == 1:
			in = fs.Arg(0)
		}
		dir, err := projectDir(in)
		if err != nil {
			return err
		}
		plan, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{})
		if err != nil {
			return err
		}
		if plan.Empty() {
			fmt.Fprintln(a.stdout, "the index is up to date")
			return nil
		}
		return finishPlan(a.stdout, dir, plan, *dryRun)
	}
}

// finishPlan prints what plan changes in dir, naming files from the working directory, and makes the changes unless dryRun is set.
func finishPlan(w io.Writer, dir string, plan *refs.Plan, dryRun bool) error {
	wd, wdErr := os.Getwd()
	name := func(p string) string {
		n := filepath.Join(dir, filepath.FromSlash(p))
		if abs, err := filepath.Abs(n); err == nil && wdErr == nil {
			if rel, err := filepath.Rel(wd, abs); err == nil && !filepath.IsAbs(dir) {
				return rel
			}
		}
		return n
	}
	for _, m := range plan.Moves {
		fmt.Fprintf(w, "move %s to %s\n", name(m.From), name(m.To))
	}
	for _, r := range plan.Updated {
		fmt.Fprintf(w, "%s:%d: update %s %q\n", name(r.File), r.Line, r.Field, r.Value)
	}
	for _, p := range plan.Assigned {
		fmt.Fprintf(w, "%s: add a uid\n", name(p))
	}
	for _, wr := range plan.Writes {
		fmt.Fprintf(w, "write %s\n", name(wr.Path))
	}
	if dryRun {
		return nil
	}
	return plan.Apply(dir)
}

// projectDir returns in, when given, after checking it is a directory, or else the nearest directory at or above the working directory that holds game.yaml, nsgo.yaml or go.mod, or the working directory itself when none does.
func projectDir(in string) (string, error) {
	if in != "" {
		info, err := os.Stat(in)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", in)
		}
		return in, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; {
		for _, marker := range []string{project.FileName, project.NSGoFileName, "go.mod"} {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return relToWD(wd, d), nil
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ".", nil
		}
		d = parent
	}
}

// relToWD returns dir relative to the working directory wd when it is wd or above it, so messages name files the way the person typed them.
func relToWD(wd, dir string) string {
	if rel, err := filepath.Rel(wd, dir); err == nil {
		return rel
	}
	return dir
}

// projectPath returns arg, a path from the working directory, as a slash-separated path from the project directory dir, or an error when it lies outside dir.
func projectPath(dir, arg string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New(arg + " is not inside the project directory " + dir)
	}
	return filepath.ToSlash(rel), nil
}
