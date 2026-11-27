package gamecli

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/tooling/atlaspack"
)

// packCommand packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata for sprite.LoadAtlas, applying any slice and pivot settings in *.import.yaml sidecars beside the input PNGs.
var packCommand = &command{
	name:    "pack",
	summary: "Pack a folder of PNG images into sprite atlas pages and TexturePacker JSON for sprite.LoadAtlas",
	define:  definePack,
}

func definePack(a *app, fs *flag.FlagSet) func() error {
	defaults := atlaspack.DefaultOptions()
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
		res, err := atlaspack.PackImages(inputs, atlaspack.Options{
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

func loadPackInputs(inDir string) ([]atlaspack.Input, error) {
	relPaths, err := atlaspack.ScanDir(inDir)
	if err != nil {
		return nil, err
	}
	inputs := make([]atlaspack.Input, 0, len(relPaths))
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
		var im *importsettings.Image
		if side != nil {
			im = side.Image
		}
		if im == nil || im.Slice == nil {
			var framePivot *atlaspack.Pivot
			var shapes []atlaspack.Polygon
			var pts map[string]atlaspack.Point
			if im != nil {
				if pt, ok := im.Pivot.ForImage(); ok {
					framePivot = &atlaspack.Pivot{X: pt[0], Y: pt[1]}
				}
				if spec, ok := im.Collision.ForImage(); ok {
					shapes, err = atlaspack.TraceCollision(img, packCollisionOptions(spec))
					if err != nil {
						return nil, fmt.Errorf("%s: %w", importsettings.Path(full), err)
					}
				}
				pts = packPointsMap(im.PointsForImage())
			}
			inputs = append(inputs, atlaspack.Input{Name: rel, Image: img, Pivot: framePivot, Shapes: shapes, Points: pts})
			continue
		}
		sub, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		})
		if !ok {
			return nil, fmt.Errorf("%s: cannot cut a %T image into frames", importsettings.Path(full), img)
		}
		bounds := img.Bounds()
		rects, _, _, err := im.Slice.Cells(bounds)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", importsettings.Path(full), err)
		}
		if im.Pivot != nil {
			for _, c := range im.Pivot.Cells {
				if c.Cell >= len(rects) {
					return nil, fmt.Errorf("%s: pivot: cell %d is out of range for the %d cells that fit the %dx%d image", importsettings.Path(full), c.Cell, len(rects), bounds.Dx(), bounds.Dy())
				}
			}
		}
		if im.Collision != nil {
			for _, c := range im.Collision.Cells {
				if c.Cell >= len(rects) {
					return nil, fmt.Errorf("%s: collision: cell %d is out of range for the %d cells that fit the %dx%d image", importsettings.Path(full), c.Cell, len(rects), bounds.Dx(), bounds.Dy())
				}
			}
		}
		for _, cp := range im.CellPoints {
			if cp.Cell >= len(rects) {
				return nil, fmt.Errorf("%s: cellPoints: cell %d is out of range for the %d cells that fit the %dx%d image", importsettings.Path(full), cp.Cell, len(rects), bounds.Dx(), bounds.Dy())
			}
		}
		ext := path.Ext(rel)
		stem := strings.TrimSuffix(rel, ext)
		for i, r := range rects {
			cellImg := sub.SubImage(r)
			var framePivot *atlaspack.Pivot
			if pt, ok := im.Pivot.ForCell(i); ok {
				framePivot = &atlaspack.Pivot{X: pt[0], Y: pt[1]}
			}
			var shapes []atlaspack.Polygon
			if spec, ok := im.Collision.ForCell(i); ok {
				shapes, err = atlaspack.TraceCollision(cellImg, packCollisionOptions(spec))
				if err != nil {
					return nil, fmt.Errorf("%s: cell %d: %w", importsettings.Path(full), i, err)
				}
			}
			pts := packPointsMap(im.PointsForCell(i))
			inputs = append(inputs, atlaspack.Input{Name: fmt.Sprintf("%s_%d%s", stem, i, ext), Image: cellImg, Pivot: framePivot, Shapes: shapes, Points: pts})
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

func packCollisionOptions(spec importsettings.CollisionSpec) atlaspack.CollisionOptions {
	opts := atlaspack.CollisionOptions{
		Mode:        atlaspack.CollisionMode(spec.Mode),
		Tolerance:   spec.Tolerance,
		MaxVertices: spec.MaxVertices,
	}
	if len(spec.Points) > 0 {
		opts.Points = make([]atlaspack.Point, len(spec.Points))
		for i, p := range spec.Points {
			opts.Points[i] = atlaspack.Point{X: p[0], Y: p[1]}
		}
	}
	return opts
}

func packPointsMap(pts map[string]importsettings.Point2D) map[string]atlaspack.Point {
	if len(pts) == 0 {
		return nil
	}
	out := make(map[string]atlaspack.Point, len(pts))
	for name, p := range pts {
		out[name] = atlaspack.Point{X: p[0], Y: p[1]}
	}
	return out
}
