package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

// checkCommand checks the scenes, maps and atlases under a directory and the references between them, printing each problem with its file and line.
var checkCommand = &command{
	name:    "check",
	args:    "[dir]",
	summary: "Check the scenes, Tiled and LDtk maps and sprite atlases under a directory, and the files they refer to",
	define:  defineCheck,
}

func defineCheck(a *app, fs *flag.FlagSet) func() error {
	inArg := inFlag(fs, "", "directory to check (alternative to the positional argument; defaults to the current directory)")

	return func() error {
		dir := strings.TrimSpace(*inArg)
		switch {
		case dir != "" && fs.NArg() > 0:
			return usagef("specify either -in or a positional directory, not both")
		case fs.NArg() > 1:
			return usagef("expected at most one directory, got %d", fs.NArg())
		case fs.NArg() == 1:
			dir = fs.Arg(0)
		case dir == "":
			dir = "."
		}
		info, err := os.Stat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		report, err := assetcheck.Check(os.DirFS(dir))
		if err != nil {
			return err
		}
		for _, p := range report.Problems {
			// Name each file from the working directory, so editors and terminals can open it.
			p.File = filepath.Join(dir, filepath.FromSlash(p.File))
			fmt.Fprintln(a.stderr, p)
		}
		switch n := len(report.Problems); n {
		case 0:
		case 1:
			return errors.New("1 problem")
		default:
			return fmt.Errorf("%d problems", n)
		}
		fmt.Fprintln(a.stdout, report.Summary())
		return nil
	}
}
