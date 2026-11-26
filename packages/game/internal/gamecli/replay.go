package gamecli

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/replay"
)

// recordingExt is the extension game replay finds recordings by, the one replay.SaveFile's documentation names.
const recordingExt = ".replay.yaml"

// replayCommand checks the input recordings under its directories and plays them headless through the tests that replay them.
var replayCommand = &command{
	name:    "replay",
	args:    "[directory ...]",
	summary: "Check the input recordings under the directories and play them headless through their replay tests, failing on a mismatch",
	define:  defineReplay,
	complete: map[string]completion{
		"": filePaths,
	},
}

func defineReplay(a *app, fs *flag.FlagSet) func() error {
	pattern := fs.String("run", "Replay", "run the tests matching this `regexp`, as go test's -run: the tests that play the recordings")
	return func() error {
		if *pattern == "" {
			return usagef("-run must name the tests that play the recordings")
		}
		dirs := fs.Args()
		if len(dirs) == 0 {
			dirs = []string{"./..."}
		}
		var invalid []error
		total := 0
		for _, dir := range dirs {
			paths, err := findRecordings(dir)
			if err != nil {
				return err
			}
			for _, path := range paths {
				if _, err := replay.LoadFile(path); err != nil {
					invalid = append(invalid, err)
				}
			}
			total += len(paths)
		}
		if len(invalid) > 0 {
			return fmt.Errorf("%d of %d recordings cannot be played:\n%w", len(invalid), total, errors.Join(invalid...))
		}
		fmt.Fprintf(a.stdout, "game replay: %d recordings, playing them through the tests matching %q\n", total, *pattern)
		for _, dir := range dirs {
			var out nsgoOutput
			if err := out.run(a, []string{"test", "-run", *pattern, dir, "-count=1"}); err != nil {
				return fmt.Errorf("replay tests in %s failed: %w", dir, err)
			}
			// nsgo test, like go test, passes a run in which -run matched no test, so a run that passed no test is refused: otherwise a misnamed test or a mistyped -run would pass without playing a recording.
			passed, ok := passedTests(out.bytes())
			switch {
			case !ok:
				return fmt.Errorf("replay tests in %s printed no tally of passed tests", dir)
			case passed == 0:
				return fmt.Errorf("no test in %s matching -run %q passed, so no recording was played", dir, *pattern)
			}
		}
		return nil
	}
}

// findRecordings returns the paths of the recordings under dir, in lexical order. dir may end in /..., as a Go package pattern does and as nsgo test reads it; either way the whole tree is searched, as nsgo test tests it. Like the go command, it skips directories whose names start with . or _ and nested modules, but not testdata, where recordings usually live. A directory holding no recording is an error, so a mistyped path cannot pass.
func findRecordings(dir string) ([]string, error) {
	root := dir
	if dir == "..." {
		root = "."
	} else if t, ok := strings.CutSuffix(filepath.ToSlash(dir), "/..."); ok {
		root = filepath.FromSlash(t)
		if root == "" {
			root = string(filepath.Separator)
		}
	}
	if fi, err := os.Stat(root); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if name := d.Name(); strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), recordingExt) && d.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no *%s recordings under %s", recordingExt, root)
	}
	return paths, nil
}
