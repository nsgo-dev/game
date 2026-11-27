package gamecli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/ktx2"
)

// texCommand encodes PNG and JPEG textures into KTX2 v2.0 containers, with optional mipmaps, sRGB metadata and BC1, BC3, ETC2 or RGBA8 encoding, taking the settings of the import settings sidecar beside the input under the flags given.
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

		// The sidecar beside the input sets what the flags given leave alone, as game assets applies it to a texture target. slice is an atlas's setting, and a texture holds the whole image, so it is not read here.
		side, err := readSidecar(inPath, "image")
		if err != nil {
			return err
		}
		set := setFlags(fs)
		var filter string
		if side != nil {
			im := side.Image
			if im.Format != "" && !set["format"] {
				*formatFlag = im.Format
			}
			if im.Mipmaps != nil && !set["mipmaps"] {
				*mipmaps = *im.Mipmaps
			}
			if im.SRGB != nil && !set["srgb"] {
				*srgb = *im.SRGB
			}
			filter = im.Filter
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
		summary := fmt.Sprintf("format=%s mipmaps=%t srgb=%t", format, *mipmaps, *srgb)
		if filter != "" {
			opts.KVD = map[string]string{importsettings.FilterKey: filter}
			summary += " filter=" + filter
		}
		if err := ktx2.EncodeFile(inPath, dest, opts); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s (%s)\n", dest, summary)
		return nil
	}
}
