// Command gamemanifest generates a typed Go asset manifest from a directory of asset files so missing assets fail the build at compile time.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/manifest"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gamemanifest: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	defaults := manifest.DefaultOptions()
	fs := flag.NewFlagSet("gamemanifest", flag.ContinueOnError)
	fs.SetOutput(stderr)

	inDir := fs.String("in", "assets", "input assets directory")
	outPath := fs.String("out", "", "output Go file path (writes to stdout when empty)")
	pkg := fs.String("pkg", defaults.Package, "Go package name for the generated file")
	prefix := fs.String("prefix", defaults.Prefix, "URI prefix prepended to each asset path")
	embed := fs.Bool("embed", false, "emit //go:embed directives instead of URI references")
	embedDir := fs.String("embed-dir", "", "optional directory prefix prepended to //go:embed patterns")
	check := fs.Bool("check", false, "verify that -out is up to date without modifying it")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inDir == "" {
		return errors.New("-in directory is required")
	}

	opts := manifest.Options{
		Package:  *pkg,
		Prefix:   *prefix,
		Embed:    *embed,
		EmbedDir: *embedDir,
	}

	if *check {
		if *outPath == "" {
			return errors.New("-out is required with -check")
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
			return fmt.Errorf("%s is out of date; rerun gamemanifest", *outPath)
		}
		return nil
	}

	if *outPath == "" {
		src, err := manifest.Generate(*inDir, opts)
		if err != nil {
			return err
		}
		_, err = stdout.Write(src)
		return err
	}

	if err := manifest.WriteFile(*inDir, *outPath, opts); err != nil {
		return err
	}
	fmt.Fprintln(stdout, *outPath)
	return nil
}
