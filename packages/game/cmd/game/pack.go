package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

// packCommand packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata for sprite.LoadAtlas, applying any slice and pivot settings in *.import.yaml sidecars beside the input PNGs.
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
		inputs, err := loadPackInputs(*inDir)
		if err != nil {
			return err
		}
		res, err := pack.PackImages(inputs, pack.Options{
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

func loadPackInputs(inDir string) ([]pack.Input, error) {
	relPaths, err := pack.ScanDir(inDir)
	if err != nil {
		return nil, err
	}
	inputs := make([]pack.Input, 0, len(relPaths))
	for _, rel := range relPaths {
		full := filepath.Join(inDir, filepath.FromSlash(rel))
		f, err := os.Open(full)
		if err != nil {
			return nil, fmt.Errorf("pack: open %q: %w", rel, err)
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("pack: decode %q: %w", rel, err)
		}
		side, err := readSidecar(full, "image")
		if err != nil {
			return nil, err
		}
		var sl *importsettings.Slice
		var pv *importsettings.Pivot
		if side != nil && side.Image != nil {
			sl = side.Image.Slice
			pv = side.Image.Pivot
		}
		if sl == nil {
			var framePivot *pack.Pivot
			if pt, ok := pv.ForImage(); ok {
				framePivot = &pack.Pivot{X: pt[0], Y: pt[1]}
			}
			inputs = append(inputs, pack.Input{Name: rel, Image: img, Pivot: framePivot})
			continue
		}
		sub, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		})
		if !ok {
			return nil, fmt.Errorf("%s: cannot cut a %T image into frames", importsettings.Path(full), img)
		}
		bounds := img.Bounds()
		rects, _, _, err := sl.Cells(bounds)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", importsettings.Path(full), err)
		}
		if pv != nil {
			for _, c := range pv.Cells {
				if c.Cell >= len(rects) {
					return nil, fmt.Errorf("%s: pivot: cell %d is out of range for the %d cells that fit the %dx%d image", importsettings.Path(full), c.Cell, len(rects), bounds.Dx(), bounds.Dy())
				}
			}
		}
		ext := path.Ext(rel)
		stem := strings.TrimSuffix(rel, ext)
		for i, r := range rects {
			var framePivot *pack.Pivot
			if pt, ok := pv.ForCell(i); ok {
				framePivot = &pack.Pivot{X: pt[0], Y: pt[1]}
			}
			inputs = append(inputs, pack.Input{Name: fmt.Sprintf("%s_%d%s", stem, i, ext), Image: sub.SubImage(r), Pivot: framePivot})
		}
	}
	seen := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		if seen[in.Name] {
			return nil, fmt.Errorf("two frames of %s are named %s; rename the image or the sheet sliced into that frame", inDir, in.Name)
		}
		seen[in.Name] = true
	}
	return inputs, nil
}
