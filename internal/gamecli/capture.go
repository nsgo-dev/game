package gamecli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

var captureCommand = &command{
	name: "capture", args: "[project]", summary: "Check the store screenshots and covers a YAML capture plan describes against its scenes, recordings and sizes",
	define: func(a *app, fs *flag.FlagSet) func() error {
		planPath := fs.String("plan", "store/capture.yaml", "capture plan `file` relative to the project")
		shotName := fs.String("shot", "", "check only the named `shot`")
		out := fs.String("out", "store/media", "media `directory` relative to the project")
		return func() error {
			if fs.NArg() > 1 {
				return usagef("capture accepts at most one project directory")
			}
			dir := "."
			if fs.NArg() == 1 {
				dir = fs.Arg(0)
			}
			return runCapture(a.stdout, dir, *planPath, *shotName, *out)
		}
	},
}

// runCapture loads the plan at planPath and checks its images under out, both relative to the project directory dir unless absolute, keeping only the shot named shotName when it is not empty.
func runCapture(stdout io.Writer, dir, planPath, shotName, out string) error {
	planFile := planPath
	if !filepath.IsAbs(planFile) {
		planFile = filepath.Join(dir, planFile)
	}
	plan, err := storecapture.LoadFile(planFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("capture plan %s does not exist", planFile)
		}
		return err
	}
	if shotName != "" {
		plan.Shots = slices.DeleteFunc(plan.Shots, func(s storecapture.Shot) bool { return s.Name != shotName })
		if len(plan.Shots) == 0 {
			return fmt.Errorf("shot %q not found in %s", shotName, planFile)
		}
	}
	media := out
	if !filepath.IsAbs(media) {
		media = filepath.Join(dir, media)
	}
	if err := storecapture.Check(*plan, dir, media); err != nil {
		return err
	}
	n := len(plan.Images())
	noun := "images match"
	if n == 1 {
		noun = "image matches"
	}
	fmt.Fprintf(stdout, "game capture: %d %s %s in %s\n", n, noun, planFile, media)
	return nil
}
