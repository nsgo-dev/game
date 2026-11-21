// Command gametex encodes PNG and JPEG game textures into pure-Go KTX2 v2.0 containers
// with optional mipmap generation, sRGB color space metadata, and BC1/BC3/ETC2/RGBA8 encoding.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gametex", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: gametex [flags] <input.png|input.jpg>\n")
		fs.PrintDefaults()
	}

	formatFlag := fs.String("format", "rgba8", "output format: bc1, bc3, etc2, or rgba8")
	mipmaps := fs.Bool("mipmaps", false, "generate full mipmap chain down to 1x1")
	srgb := fs.Bool("srgb", false, "encode as sRGB with linear-light mipmap filtering")
	outPath := fs.String("o", "", "output .ktx2 path (defaults to input path with .ktx2 extension)")
	inFlag := fs.String("in", "", "input PNG/JPEG file path (alternative to positional argument)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	inPath := strings.TrimSpace(*inFlag)
	if inPath == "" {
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "gametex: expected exactly one input image path")
			fs.Usage()
			return 2
		}
		inPath = fs.Arg(0)
	} else if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "gametex: specify either -in or a positional input path, not both")
		fs.Usage()
		return 2
	}

	format, err := ktx2.ParseFormat(*formatFlag)
	if err != nil {
		fmt.Fprintf(stderr, "gametex: %v\n", err)
		return 2
	}

	dest := strings.TrimSpace(*outPath)
	if dest == "" {
		ext := filepath.Ext(inPath)
		dest = strings.TrimSuffix(inPath, ext) + ".ktx2"
	}

	opts := ktx2.Options{
		Format:  format,
		Mipmaps: *mipmaps,
		SRGB:    *srgb,
	}
	if err := ktx2.EncodeFile(inPath, dest, opts); err != nil {
		fmt.Fprintf(stderr, "gametex: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote %s (format=%s mipmaps=%t srgb=%t)\n", dest, format, *mipmaps, *srgb)
	return 0
}
