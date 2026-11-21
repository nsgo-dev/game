// Command gamepack packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata for sprite.LoadAtlas.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gamepack: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	defaults := pack.DefaultOptions()
	fs := flag.NewFlagSet("gamepack", flag.ContinueOnError)
	fs.SetOutput(stderr)

	inDir := fs.String("in", "", "input directory of PNG images")
	outDir := fs.String("out", ".", "output directory for packed atlas PNG and JSON files")
	name := fs.String("name", defaults.Name, "base file name for atlas pages")
	padding := fs.Int("padding", defaults.Padding, "transparent pixels between adjacent frames")
	extrude := fs.Int("extrude", defaults.Extrude, "edge pixels extruded around each frame")
	maxWidth := fs.Int("max-width", defaults.MaxWidth, "maximum atlas page width in pixels")
	maxHeight := fs.Int("max-height", defaults.MaxHeight, "maximum atlas page height in pixels")
	scale := fs.Int("scale", defaults.Scale, "atlas scale written to meta.scale")
	trim := fs.Bool("trim", defaults.Trim, "trim transparent borders around input images")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inDir == "" {
		return errors.New("-in directory is required")
	}

	opts := pack.Options{
		Name:      *name,
		Padding:   *padding,
		Extrude:   *extrude,
		MaxWidth:  *maxWidth,
		MaxHeight: *maxHeight,
		Scale:     *scale,
		Trim:      *trim,
	}
	res, err := pack.PackDir(*inDir, opts)
	if err != nil {
		return err
	}
	written, err := res.Write(*outDir)
	if err != nil {
		return err
	}
	for _, p := range written {
		fmt.Fprintln(stdout, p)
	}
	return nil
}
