package gamecli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
)

// libraryVerbs are what follows game library's flags.
var libraryVerbs = []string{"list", "install", "remove", "digest"}

// libraryCommand finds packs in a library and installs them into a project, or removes them.
var libraryCommand = &command{
	name:    "library",
	args:    "list [words] | install <pack>... | remove <pack>... | digest <pack-dir>",
	summary: "Find asset, editor plugin and template packs in a library and install them into a project, remove them, or print a pack's digest for the library's index",
	define:  defineLibrary,
	complete: map[string]completion{
		"index":   filePaths,
		"project": filePaths,
		"dir":     filePaths,
		"kind":    oneOf(project.PackKinds...),
		"":        oneOf(libraryVerbs...),
	},
}

func defineLibrary(a *app, fs *flag.FlagSet) func() error {
	index := fs.String("index", library.DefaultIndex, "the library's index: an https URL, an index file, or a directory holding index.yaml")
	dir := fs.String("project", ".", "the project directory holding game.yaml, which install and remove change and list marks installed packs from")
	kind := fs.String("kind", "", "list only packs of this kind: "+strings.Join(project.PackKinds, ", "))
	into := fs.String("dir", "", "install the pack into this directory of the project instead of the one it is installed in or its manifest names (one pack at a time)")

	return func() error {
		if fs.NArg() == 0 {
			return usagef("expected one of %s", strings.Join(libraryVerbs, ", "))
		}
		verb, args := fs.Arg(0), fs.Args()[1:]
		if *kind != "" && verb != "list" {
			return usagef("-kind is for list")
		}
		if *into != "" && verb != "install" {
			return usagef("-dir is for install")
		}
		ctx := context.Background()
		switch verb {
		case "list":
			if *kind != "" && !slices.Contains(project.PackKinds, *kind) {
				return usagef("-kind %q must be one of %s", *kind, strings.Join(project.PackKinds, ", "))
			}
			return libraryList(ctx, a, *index, *dir, *kind, strings.Join(args, " "))
		case "install":
			if len(args) == 0 {
				return usagef("install needs the name of a pack")
			}
			if *into != "" && len(args) > 1 {
				return usagef("-dir installs one pack, not %d", len(args))
			}
			return libraryInstall(ctx, a, *index, *dir, *into, args)
		case "remove":
			if len(args) == 0 {
				return usagef("remove needs the name of an installed pack")
			}
			for _, name := range args {
				c, err := library.Remove(*dir, name)
				if err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "removed %s %s from %s (%d files)\n", c.Pack.Name, c.Pack.PackVersion, c.Pack.Dir, len(c.Remove))
			}
			return nil
		case "digest":
			if len(args) != 1 {
				return usagef("digest takes one pack directory")
			}
			p, err := library.ReadPack(os.DirFS(args[0]), ".")
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, p.Digest())
			return nil
		default:
			return usagef("unknown verb %q; use %s", verb, strings.Join(libraryVerbs, ", "))
		}
	}
}

// libraryList prints the packs of the library at index that are of kind and match query, one per line, with the version installed in the project in dir, or - when it has none installed or there is no project there.
func libraryList(ctx context.Context, a *app, index, dir, kind, query string) error {
	lib, err := library.Open(ctx, index)
	if err != nil {
		return err
	}
	proj, err := readProjectIfAny(dir)
	if err != nil {
		return err
	}
	entries := lib.Index.Search(kind, query)
	if len(entries) == 0 {
		fmt.Fprintln(a.stderr, "no packs match")
		return nil
	}
	tw := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PACK\tKIND\tVERSION\tINSTALLED\tDESCRIPTION")
	for _, e := range entries {
		installed := "-"
		if proj != nil {
			if p := proj.FindPack(e.Name); p != nil {
				installed = p.PackVersion
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Kind, e.PackVersion, installed, e.Description)
	}
	return tw.Flush()
}

// libraryInstall fetches each named pack from the library at index and installs it into the project in dir, under into when it is set.
func libraryInstall(ctx context.Context, a *app, index, dir, into string, names []string) error {
	lib, err := library.Open(ctx, index)
	if err != nil {
		return err
	}
	for _, name := range names {
		proj, err := readProjectIfAny(dir)
		if err != nil {
			return err
		}
		if proj == nil {
			return fmt.Errorf("no %s in %s; run game library install in a project, or name one with -project", project.FileName, dir)
		}
		var before *project.Pack
		if p := proj.FindPack(name); p != nil {
			before = p
		}
		p, err := lib.Fetch(ctx, name)
		if err != nil {
			return err
		}
		c, err := library.Install(dir, p, into)
		if err != nil {
			return err
		}
		switch {
		case c.NoOp():
			fmt.Fprintf(a.stdout, "%s %s is already installed in %s\n", name, c.Pack.PackVersion, c.Pack.Dir)
		case before != nil:
			fmt.Fprintf(a.stdout, "updated %s %s to %s in %s (%d files written, %d removed)\n", name, before.PackVersion, c.Pack.PackVersion, c.Pack.Dir, len(c.Write), len(c.Remove))
		default:
			fmt.Fprintf(a.stdout, "installed %s %s into %s (%d files)\n", name, c.Pack.PackVersion, c.Pack.Dir, len(c.Write))
		}
	}
	return nil
}

// readProjectIfAny reads the game.yaml in dir with project.ReadFS, returning nil when there is none.
func readProjectIfAny(dir string) (*project.File, error) {
	f, err := project.ReadFS(os.DirFS(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, project.FileName), err)
	}
	return f, nil
}
