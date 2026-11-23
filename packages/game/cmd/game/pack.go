package main

import (
	"flag"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

// packCommand packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata for sprite.LoadAtlas.
var packCommand = &command{
	name:    "pack",
	summary: "Pack a folder of PNG images into sprite atlas pages and TexturePacker JSON for sprite.LoadAtlas",
	define:  definePack,
}

func definePack(a *app, fs *flag.FlagSet) func() error {
	defaults := pack.DefaultOptions()
	inDir := inFlag(fs, "", "input directory of PNG images (required)")
	outDir := outFlag(fs, ".", "output directory for packed atlas PNG and JSON files")
	name := fs.String("name", defaults.Name, "base file name for atlas pages")
	padding := fs.Int("padding", defaults.Padding, "transparent pixels between adjacent frames")
	extrude := fs.Int("extrude", defaults.Extrude, "edge pixels extruded around each frame")
	maxWidth := fs.Int("max-width", defaults.MaxWidth, "maximum atlas page width in pixels")
	maxHeight := fs.Int("max-height", defaults.MaxHeight, "maximum atlas page height in pixels")
	scale := fs.Int("scale", defaults.Scale, "atlas scale written to meta.scale")
	trim := fs.Bool("trim", defaults.Trim, "trim transparent borders around input images")

	return func() error {
		if *inDir == "" {
			return usagef("-in directory is required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		res, err := pack.PackDir(*inDir, pack.Options{
			Name:      *name,
			Padding:   *padding,
			Extrude:   *extrude,
			MaxWidth:  *maxWidth,
			MaxHeight: *maxHeight,
			Scale:     *scale,
			Trim:      *trim,
		})
		if err != nil {
			return err
		}
		written, err := res.Write(*outDir)
		if err != nil {
			return err
		}
		for _, p := range written {
			fmt.Fprintln(a.stdout, p)
		}
		return nil
	}
}
