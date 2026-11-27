// Command gameapi writes the exported API of a Go module's packages, read from their source and doc comments, as the JSON of package apiref: the reference the website renders as its API pages and publishes at /api.json for agents.
//
//	go run ./tools/gameapi [-root dir] [-html] [-out file]
//
// It reads the module whose go.mod is in -root, the current directory by default, and writes to -out, or to standard output without it. -html adds each doc comment rendered as HTML, which the website's pages show; the published JSON leaves it out.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nsgo-dev/game/pkg/tooling/apiref"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gameapi: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gameapi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "directory of the module's go.mod")
	out := fs.String("out", "", "file to write the JSON to (standard output when empty)")
	html := fs.Bool("html", false, "add each doc comment rendered as HTML")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	ref, err := apiref.Load(*root, apiref.Options{HTML: *html})
	if err != nil {
		return err
	}
	data, err := apiref.Marshal(ref)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(*out, data, 0o644)
}
