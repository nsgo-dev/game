// Command genicon writes Star Catcher's app icon, 1024 pixels square, to the PNG file its argument names.
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"

	"github.com/nsgo-dev/game/examples/tutorial/ch09-ship/internal/icon"
)

// size is the icon's width and height in pixels, as nsgo build wants a PNG icon.
const size = 1024

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genicon <output.png>")
		os.Exit(2)
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, icon.Render(size)); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
}
