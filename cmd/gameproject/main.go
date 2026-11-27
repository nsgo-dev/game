// Command gameproject checks, formats and applies a game's project file, game.yaml.
//
//	gameproject check [dir]          report what is wrong with dir's game.yaml, its nsgo.yaml and its autoloads
//	gameproject fmt [-check] [dir]   rewrite dir's game.yaml in canonical form, or with -check fail unless it is
//	gameproject sync [-check] [dir]  write the fields game.yaml owns into dir's nsgo.yaml, or with -check fail unless they agree
//	gameproject targets [dir]        print the NSGo build targets of dir's export presets, comma-separated
//
// dir is the project root and defaults to the current directory.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gameproject: %v\n", err)
		os.Exit(1)
	}
}

const usage = `usage:
  gameproject check [dir]
  gameproject fmt [-check] [dir]
  gameproject sync [-check] [dir]
  gameproject targets [dir]`

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, args := args[0], args[1:]
	fset := flag.NewFlagSet("gameproject "+cmd, flag.ContinueOnError)
	fset.SetOutput(stderr)
	var check *bool
	switch cmd {
	case "check", "targets":
	case "fmt":
		check = fset.Bool("check", false, "fail unless game.yaml is in canonical form, without rewriting it")
	case "sync":
		check = fset.Bool("check", false, "fail unless nsgo.yaml agrees with game.yaml, without rewriting it")
	case "help", "-h", "-help", "--help":
		fmt.Fprintln(stdout, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() > 1 {
		return fmt.Errorf("%s takes at most one directory\n%s", cmd, usage)
	}
	dir := "."
	if fset.NArg() == 1 {
		dir = fset.Arg(0)
	}

	switch cmd {
	case "check":
		if _, err := project.Check(os.DirFS(dir)); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: ok\n", filepath.Join(dir, project.FileName))
		return nil
	case "fmt":
		return format(dir, *check, stdout)
	case "sync":
		return sync(dir, *check, stdout)
	default: // targets
		f, err := project.ReadFS(os.DirFS(dir))
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, strings.Join(f.BuildTargets(), ","))
		return nil
	}
}

// format rewrites dir's game.yaml as project.Encode writes it, or with check reports whether it already is.
func format(dir string, check bool, stdout io.Writer) error {
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
		return fmt.Errorf("%s is not in canonical form; run gameproject fmt", path)
	}
	if err := writeFile(path, canonical); err != nil {
		return err
	}
	fmt.Fprintln(stdout, path)
	return nil
}

// sync writes the fields game.yaml owns into dir's nsgo.yaml, creating it when missing, or with check reports whether they agree.
func sync(dir string, check bool, stdout io.Writer) error {
	f, err := project.ReadFS(os.DirFS(dir))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, project.NSGoFileName)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if check {
		if err != nil {
			return fmt.Errorf("%s is missing; run gameproject sync", path)
		}
		if err := f.CheckNSGo(old); err != nil {
			return fmt.Errorf("%w; run gameproject sync", err)
		}
		return nil
	}
	synced, err := f.SyncNSGo(old)
	if err != nil {
		return err
	}
	if bytes.Equal(synced, old) {
		return nil
	}
	if err := writeFile(path, synced); err != nil {
		return err
	}
	fmt.Fprintln(stdout, path)
	return nil
}

// writeFile replaces path with data through a temporary file beside it, so a failed write never truncates the old file.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
