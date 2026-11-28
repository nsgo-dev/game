package gamecli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

var captureCommand = &command{
	name: "capture", args: "[project]", summary: "Render reproducible store screenshots and covers from a YAML capture plan",
	define: func(a *app, fs *flag.FlagSet) func() error {
		planPath := fs.String("plan", "store/capture.yaml", "capture plan `file` relative to the project")
		shotName := fs.String("shot", "", "render only the named `shot`")
		out := fs.String("out", "store/media", "output `directory` relative to the project")
		return func() error {
			dir := "."
			if fs.NArg() > 1 {
				return usagef("capture accepts at most one project directory")
			}
			if fs.NArg() == 1 {
				dir = fs.Arg(0)
			}
			return runCapture(dir, *planPath, *shotName, *out)
		}
	},
}

func runCapture(dir, planPath, shotName, out string) error {
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
	output := out
	if !filepath.IsAbs(output) {
		output = filepath.Join(dir, output)
	}
	if err := storecapture.Render(context.Background(), *plan, output); err != nil {
		return err
	}
	return nil
}
