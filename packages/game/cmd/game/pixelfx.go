package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	// The categories whose effects game pixelfx renders; each category's port adds its import here.
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/critters"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/explosions"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/fire"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/ice"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/moves"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/nature"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/pickups"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/projectiles"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/scifi"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/shadow"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/spawn"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/status"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/transitions"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/ui"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/water"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/weather"
)

// pixelfxCommand renders a pixelfx effect to a PNG sprite sheet and its Aseprite JSON, which sprite.LoadAtlas reads, as pixelfx's own sheets are laid out.
var pixelfxCommand = &command{
	name:    "pixelfx",
	args:    "<category/name>",
	summary: "Render a pixelfx effect, or the effect acted out, to a PNG sprite sheet and Aseprite JSON for sprite.LoadAtlas",
	define:  definePixelfx,
	complete: map[string]completion{
		"": oneOf(pixelfx.IDs()...),
	},
}

// optionSets is the -set flag: name=value pairs, in the order given.
type optionSets []string

func (s *optionSets) String() string { return strings.Join(*s, " ") }

func (s *optionSets) Set(v string) error {
	if !strings.Contains(v, "=") {
		return fmt.Errorf("%q is not name=value", v)
	}
	*s = append(*s, v)
	return nil
}

func definePixelfx(a *app, fs *flag.FlagSet) func() error {
	outDir := outFlag(fs, ".", "the `directory` to write <name>.png and <name>.json into")
	name := fs.String("name", "", "base file name and animation tag (default: the effect's name, such as torch)")
	fps := fs.Int("fps", 0, "frames per second (default: the effect's own, or 60 acted out)")
	act := fs.Bool("act", false, "render the effect acted out: the stock character performing it, with its target")
	flip := fs.Bool("flip", false, "mirror the effect")
	list := fs.Bool("list", false, "list the effects and their options instead of rendering one")
	var sets optionSets
	fs.Var(&sets, "set", "set one of the effect's options as `name=value`, such as seed=4 or palette=#102,#215,#fff; repeat it for more")

	return func() error {
		if *list {
			if fs.NArg() > 0 {
				return usagef("-list takes no arguments")
			}
			return listEffects(a)
		}
		if fs.NArg() != 1 {
			return usagef("expected one effect, such as fire/torch; game pixelfx -list lists them")
		}
		if *fps < 0 {
			return usagef("-fps must not be negative (0 uses the effect's own)")
		}
		id := fs.Arg(0)
		e, ok := pixelfx.Lookup(id)
		if !ok {
			return usagef("no effect %q; game pixelfx -list lists them", id)
		}
		o := pixelfx.Options{Flip: *flip}
		for _, s := range sets {
			k, v, _ := strings.Cut(s, "=")
			def, ok := e.Meta.Default(k)
			if !ok {
				return usagef("%s has no option %q; it has %s", id, k, optionNames(e))
			}
			val, err := pixelfx.ParseValue(def.Kind(), v)
			if err != nil {
				return usagef("-set %s: %v (the option is a %s)", k, err, def.Kind())
			}
			o = o.Set(k, val)
		}
		if *act && e.Act == nil {
			fmt.Fprintf(a.stderr, "%s has no act of its own: the character stands by while it plays\n", id)
		}
		base := *name
		if base == "" {
			_, base, _ = strings.Cut(id, "/")
		} else if base == "." || base == ".." || strings.ContainsAny(base, `/\`) || filepath.Base(base) != base {
			return usagef("-name %q must be a file name, without a directory", base)
		}
		var fr *pixelfx.Frames
		if *act {
			fr = pixelfx.RenderAct(e, o, *fps)
		} else {
			fr = pixelfx.Render(e, o, *fps)
		}
		img, js, err := fr.SheetPNG(base, base+".png")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		pngPath, jsonPath := filepath.Join(*outDir, base+".png"), filepath.Join(*outDir, base+".json")
		if err := os.WriteFile(pngPath, img, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(jsonPath, js, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s and %s (%d frames of %dx%d at %d fps)\n", pngPath, jsonPath, len(fr.Pixels), fr.W, fr.H, fr.FPS)
		return nil
	}
}

// listEffects writes every registered effect with its size, length and options.
func listEffects(a *app) error {
	tw := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, id := range pixelfx.IDs() {
		e, _ := pixelfx.Lookup(id)
		m := &e.Meta
		kind := "one-shot"
		if m.Loop {
			kind = "loop"
		}
		if e.Act != nil {
			kind += ", acted"
		}
		fmt.Fprintf(tw, "%s\t%dx%d\t%gs %s\t%s\n", id, m.Size[0], m.Size[1], m.Duration, kind, m.Name)
		for _, opt := range m.Options {
			fmt.Fprintf(tw, "\t\t-set %s=%s\t(%s)\n", opt.Name, opt.Default, opt.Default.Kind())
		}
	}
	return tw.Flush()
}

func optionNames(e *pixelfx.Effect) string {
	names := make([]string, len(e.Meta.Options))
	for i, o := range e.Meta.Options {
		names[i] = o.Name
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
