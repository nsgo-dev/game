package gamecli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
)

// creditsCommand writes the credits file game release ships, listing the author and license of every asset game.yaml's assets section reaches, from the credit section of each one's import settings sidecar.
var creditsCommand = &command{
	name:    "credits",
	summary: "List every asset's author and license from its import settings sidecar as the credits file game release ships, failing when an asset has none",
	define:  defineCredits,
}

func defineCredits(a *app, fs *flag.FlagSet) func() error {
	inPath := inFlag(fs, project.FileName, "project file whose assets section names the assets")
	outPath := outFlag(fs, "", "the credits `file` to write (default: print it)")

	return func() error {
		if *inPath == "" {
			return usagef("-in is required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		data, err := os.ReadFile(*inPath)
		if err != nil {
			return err
		}
		f, err := project.Parse(data)
		if err != nil {
			return fmt.Errorf("%s: %w", *inPath, err)
		}
		if f.Assets == nil {
			return fmt.Errorf("%s has no assets section", *inPath)
		}
		entries, err := credits.Collect(filepath.Dir(*inPath), f.Assets)
		if err != nil {
			return err
		}
		md := credits.Markdown(entries)
		if *outPath == "" {
			_, err := a.stdout.Write(md)
			return err
		}
		if dir := filepath.Dir(*outPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		return os.WriteFile(*outPath, md, 0o644)
	}
}
