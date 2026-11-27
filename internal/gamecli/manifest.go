package gamecli

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/nsgo-dev/game/pkg/tooling/manifest"
)

// manifestCommand generates a typed Go asset manifest from a directory of asset files, so a missing asset fails the build.
var manifestCommand = &command{
	name:    "manifest",
	summary: "Generate a typed Go asset manifest from a directory of assets, so a missing asset fails the build",
	define:  defineManifest,
}

func defineManifest(a *app, fs *flag.FlagSet) func() error {
	defaults := manifest.DefaultOptions()
	inDir := inFlag(fs, "assets", "input assets directory")
	outPath := outFlag(fs, "", "output Go file path (writes to stdout when empty)")
	pkg := fs.String("pkg", defaults.Package, "Go package name for the generated file")
	prefix := fs.String("prefix", defaults.Prefix, "URI prefix prepended to each asset path")
	embed := fs.Bool("embed", false, "emit //go:embed directives instead of URI references")
	embedDir := fs.String("embed-dir", "", "optional directory prefix prepended to //go:embed patterns")
	resources := fs.Bool("resources", false, "scan resources/ (unless -in is set) and emit \"asset:\" URI references for app resources instead of //go:embed")
	check := fs.Bool("check", false, "verify that -out is up to date without modifying it")

	return func() error {
		if *resources {
			if *embed || *embedDir != "" {
				return usagef("-resources cannot be used with -embed or -embed-dir")
			}
			inSet := false
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "in" || f.Name == "i" {
					inSet = true
				}
			})
			if !inSet {
				*inDir = "resources"
			}
		}
		if *inDir == "" {
			return usagef("-in directory is required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		opts := manifest.Options{
			Package:  *pkg,
			Prefix:   *prefix,
			Embed:    *embed,
			EmbedDir: *embedDir,
		}

		if *check {
			if *outPath == "" {
				return usagef("-out is required with -check")
			}
			want, err := manifest.Generate(*inDir, opts)
			if err != nil {
				return err
			}
			got, err := os.ReadFile(*outPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", *outPath, err)
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("%s is out of date; rerun game manifest", *outPath)
			}
			return nil
		}

		if *outPath == "" {
			src, err := manifest.Generate(*inDir, opts)
			if err != nil {
				return err
			}
			_, err = a.stdout.Write(src)
			return err
		}
		if err := manifest.WriteFile(*inDir, *outPath, opts); err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, *outPath)
		return nil
	}
}
