package gamecli

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/animclip"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/tooling/atlaspack"
	"github.com/nsgo-dev/game/pkg/tooling/spriteframes"
)

var spriteFramesCommand = &command{
	name:    "sprite",
	args:    "frames",
	summary: "Turn PNG or GIF frames into a keyed sprite atlas and animation clip",
	define:  defineSpriteFrames,
}

func defineSpriteFrames(a *app, fs *flag.FlagSet) func() error {
	in := inFlag(fs, "", "input directory of PNG frames or a GIF file (required)")
	out := outFlag(fs, ".", "output directory for atlas PNG, JSON and animation YAML")
	name := fs.String("name", "animation", "base name for atlas and animation clip")
	fps := fs.Float64("fps", 12, "animation frame rate")
	key := fs.String("key", "", "chroma key colour as #rrggbb or #rrggbbaa")
	tolerance := fs.Int("tolerance", 0, "chroma key RGB tolerance")
	softness := fs.Int("softness", 0, "chroma key alpha softness")
	despill := fs.Float64("despill", 0, "green spill removal from keyed edges")
	holes := fs.Bool("fill-holes", false, "fill one-pixel transparent holes")
	loop := fs.Bool("loop", true, "loop the animation")
	seam := fs.Bool("seam", false, "rotate frames to choose the least-visible loop seam")
	anchor := fs.String("anchor", "bottom-center", "shared anchor: bottom-center")
	width := fs.Int("width", 0, "nearest-neighbor output width (0 keeps source size)")
	height := fs.Int("height", 0, "nearest-neighbor output height (0 keeps source size)")
	padding := fs.Int("padding", 1, "transparent pixels between atlas frames")

	return func() error {
		if *in == "" {
			return usagef("-in is required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		if *tolerance < 0 || *tolerance > 255 || *softness < 0 || *softness > 255 {
			return usagef("-tolerance and -softness must be between 0 and 255")
		}
		frames, err := readSpriteFrames(*in)
		if err != nil {
			return err
		}
		if *key != "" {
			c, err := parseHexColor(*key)
			if err != nil {
				return usagef("-key: %v", err)
			}
			frames, err = spriteframes.ChromaKey(frames, spriteframes.KeyOptions{Color: c, Tolerance: uint8(*tolerance), Softness: uint8(*softness), Despill: *despill, FillHoles: *holes})
			if err != nil {
				return err
			}
		}
		if (*width == 0) != (*height == 0) {
			return usagef("-width and -height must be set together")
		}
		if *width > 0 {
			frames, err = spriteframes.ResizeNearest(frames, *width, *height)
			if err != nil {
				return err
			}
		}
		if *seam {
			frames, err = spriteframes.LoopSeam(frames)
			if err != nil {
				return err
			}
		}
		aligned, _, err := spriteframes.Align(frames, spriteframes.AnchorMode(*anchor))
		if err != nil {
			return err
		}
		inputs := make([]atlaspack.Input, len(aligned))
		names := make([]string, len(aligned))
		for i, frame := range aligned {
			names[i] = fmt.Sprintf("%s_%03d", *name, i)
			inputs[i] = atlaspack.Input{Name: names[i], Image: frame, Pivot: &atlaspack.Pivot{X: .5, Y: 1}}
		}
		packed, err := atlaspack.PackImages(inputs, atlaspack.Options{Name: *name, Padding: *padding, Trim: false})
		if err != nil {
			return err
		}
		if _, err := packed.Write(*out); err != nil {
			return err
		}
		clip, err := animclip.FromFrames(*name, names, *fps, *loop)
		if err != nil {
			return err
		}
		encoded, err := resource.Encode(clip)
		if err != nil {
			return err
		}
		clipPath := filepath.Join(*out, *name+".animation.yaml")
		if err := os.MkdirAll(*out, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(clipPath, encoded, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s and %s (%d frames at %g fps)\n", filepath.Join(*out, *name+".png"), clipPath, len(names), *fps)
		return nil
	}
}

func readSpriteFrames(input string) ([]*image.NRGBA, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("sprite frames: %w", err)
	}
	if info.IsDir() {
		ents, err := os.ReadDir(input)
		if err != nil {
			return nil, err
		}
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
		var out []*image.NRGBA
		for _, ent := range ents {
			if ent.IsDir() || !strings.EqualFold(filepath.Ext(ent.Name()), ".png") {
				continue
			}
			f, err := os.Open(filepath.Join(input, ent.Name()))
			if err != nil {
				return nil, err
			}
			img, err := png.Decode(f)
			_ = f.Close()
			if err != nil {
				return nil, fmt.Errorf("sprite frames: decode %s: %w", ent.Name(), err)
			}
			out = append(out, toNRGBA(img))
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("sprite frames: no PNG files found in %q", input)
		}
		return out, nil
	}
	f, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, fmt.Errorf("sprite frames: decode GIF: %w", err)
	}
	out := make([]*image.NRGBA, len(g.Image))
	for i, img := range g.Image {
		out[i] = toNRGBA(img)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sprite frames: GIF has no frames")
	}
	return out, nil
}

func toNRGBA(src image.Image) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			dst.Set(x, y, src.At(x, y))
		}
	}
	return dst
}

func parseHexColor(s string) (color.NRGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return color.NRGBA{}, fmt.Errorf("want #rrggbb or #rrggbbaa")
	}
	b, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("invalid hex colour")
	}
	if len(s) == 6 {
		b = b<<8 | 0xff
	}
	return color.NRGBA{R: uint8(b >> 24), G: uint8(b >> 16), B: uint8(b >> 8), A: uint8(b)}, nil
}
