package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

// texCommand encodes PNG and JPEG textures into KTX2 v2.0 containers, with optional mipmaps, sRGB metadata and BC1, BC3, ETC2 or RGBA8 encoding.
var texCommand = &command{
	name:    "tex",
	args:    "[input.png|input.jpg]",
	summary: "Encode a PNG or JPEG texture into a KTX2 container as BC1, BC3, ETC2 or RGBA8, with optional mipmaps",
	define:  defineTex,
	complete: map[string]completion{
		"format": oneOf(string(ktx2.FormatBC1), string(ktx2.FormatBC3), string(ktx2.FormatETC2), string(ktx2.FormatRGBA8)),
		"o":      filePaths,
		"":       filePaths,
	},
}

func defineTex(a *app, fs *flag.FlagSet) func() error {
	formatFlag := fs.String("format", "rgba8", "output format: bc1, bc3, etc2, or rgba8")
	mipmaps := fs.Bool("mipmaps", false, "generate full mipmap chain down to 1x1")
	srgb := fs.Bool("srgb", false, "encode as sRGB with linear-light mipmap filtering")
	inArg := inFlag(fs, "", "input PNG/JPEG file path (alternative to the positional argument)")
	outArg := outFlag(fs, "", "output .ktx2 path (defaults to the input path with a .ktx2 extension)")
	fs.StringVar(outArg, "o", "", "shorthand for -out")

	return func() error {
		inPath := strings.TrimSpace(*inArg)
		switch {
		case inPath == "" && fs.NArg() != 1:
			return usagef("expected exactly one input image path")
		case inPath == "":
			inPath = fs.Arg(0)
		case fs.NArg() > 0:
			return usagef("specify either -in or a positional input path, not both")
		}

		format, err := ktx2.ParseFormat(*formatFlag)
		if err != nil {
			return usagef("%v", err)
		}

		dest := strings.TrimSpace(*outArg)
		if dest == "" {
			dest = strings.TrimSuffix(inPath, filepath.Ext(inPath)) + ".ktx2"
		}
		opts := ktx2.Options{
			Format:  format,
			Mipmaps: *mipmaps,
			SRGB:    *srgb,
		}
		if err := ktx2.EncodeFile(inPath, dest, opts); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s (format=%s mipmaps=%t srgb=%t)\n", dest, format, *mipmaps, *srgb)
		return nil
	}
}
