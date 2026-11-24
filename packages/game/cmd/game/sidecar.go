package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// readSidecar reads the import settings sidecar beside the file at asset, a path on disk, and refuses one that holds settings for another kind of asset than want ("image" or "audio"). It returns nil, and no error, when the asset has no sidecar or one holding only a credit, which changes no output.
func readSidecar(asset, want string) (*importsettings.File, error) {
	name := asset + importsettings.Suffix
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := importsettings.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	sections := f.Sections()
	if len(sections) == 0 {
		return nil, nil
	}
	if sections[0] != want {
		return nil, fmt.Errorf("%s holds %s settings, but %s is read as %s", name, sections[0], asset, map[string]string{"image": "an image", "audio": "a sound"}[want])
	}
	return f, nil
}

// setFlags returns the names of the flags given on the command line, whose values win over a sidecar's settings.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}
