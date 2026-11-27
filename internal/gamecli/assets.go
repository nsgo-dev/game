package gamecli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
)

// assetsCommand builds every asset target game.yaml's assets section declares, reading game.yaml through package project, rebuilding only those whose inputs or settings changed.
var assetsCommand = &command{
	name:    "assets",
	summary: "Pack atlases, encode textures, convert audio, bake fonts, pack models and generate the manifest from game.yaml, rebuilding only what changed",
	define:  defineAssets,
}

func defineAssets(a *app, fs *flag.FlagSet) func() error {
	inPath := inFlag(fs, project.FileName, "project file whose assets section declares the targets")
	force := fs.Bool("force", false, "rebuild every target, even those the cache says are up to date")
	check := fs.Bool("check", false, "list the targets that are out of date without building them, and fail if there are any")
	resources := fs.Bool("resources", false, "write asset outputs into resources/ and emit \"asset:\" URIs in the manifest instead of //go:embed")

	return func() error {
		if *inPath == "" {
			return usagef("-in is required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		if *force && *check {
			return usagef("-force and -check are mutually exclusive")
		}
		data, err := os.ReadFile(*inPath)
		if err != nil {
			return err
		}
		f, err := project.Parse(data)
		if err != nil {
			return loaderr.WithFile(err, *inPath)
		}
		if f.Assets == nil {
			return fmt.Errorf("%s has no assets section", *inPath)
		}
		b := &assetbuild.Builder{
			Root:    filepath.Dir(*inPath),
			Audio:   a.audio,
			Version: assetsToolVersion(),
			Force:   *force,
			Check:   *check,
		}
		if *resources {
			b.ResourcesDir = "resources"
		}
		r, err := b.Build(context.Background(), f.Assets)
		if r != nil {
			for _, t := range r.Targets {
				if t.Status != assetbuild.UpToDate {
					fmt.Fprintf(a.stdout, "%s %s\n", t.Status, t.ID)
				}
			}
		}
		if err != nil {
			return err
		}
		if *check {
			if n := r.Count(assetbuild.Stale); n > 0 {
				return fmt.Errorf("%d of %d targets are out of date; rerun game assets", n, len(r.Targets))
			}
			fmt.Fprintf(a.stdout, "%d up to date\n", len(r.Targets))
			return nil
		}
		fmt.Fprintf(a.stdout, "%d built, %d up to date\n", r.Count(assetbuild.Built), r.Count(assetbuild.UpToDate))
		return nil
	}
}

// assetsToolVersion returns the version of the game layer this command was built from, so upgrading it rebuilds every asset target; it is "(devel)" in a build of the module itself and names the replacement when the module is replaced, unlike toolVersion, which gives the version a new project can require.
func assetsToolVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if bi.Main.Path == gameModule {
		return bi.Main.Version
	}
	for _, d := range bi.Deps {
		if d.Path == gameModule {
			if d.Replace != nil {
				return d.Replace.Path + "@" + d.Replace.Version
			}
			return d.Version
		}
	}
	return ""
}
