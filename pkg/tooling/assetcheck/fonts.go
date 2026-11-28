package assetcheck

import (
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"go.yaml.in/yaml/v3"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// checkFonts checks every font target in each game.yaml that is meant for one
// or more locales (via an explicit locale field or by referencing l10n YAML
// string tables in charsFrom) and reports every localized string or plural form
// containing characters that the font target cannot draw.
func (c *checker) checkFonts(gameYAMLs []string) {
	for _, p := range gameYAMLs {
		data, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			continue
		}
		projFile, err := project.Parse(data)
		if err != nil || projFile.Assets == nil || len(projFile.Assets.Fonts) == 0 {
			continue
		}
		projRoot := path.Dir(p)
		pl := c.localeTables[projRoot]
		if pl == nil || len(pl.files) == 0 {
			continue
		}
		projFS := c.fsys
		if projRoot != "." {
			sub, err := fs.Sub(c.fsys, projRoot)
			if err != nil {
				continue
			}
			projFS = sub
		}

		tableByRel := make(map[string]*localeTableFile, len(pl.files))
		for _, tf := range pl.files {
			tableByRel[path.Clean(tf.projRel)] = tf
		}

		type resolvedTarget struct {
			target       project.FontTarget
			sidecar      *importsettings.Font
			charsFrom    []string
			explicitTabs []*localeTableFile
			inferredLocs map[string]bool
		}

		resolved := make([]resolvedTarget, 0, len(projFile.Assets.Fonts))
		claimedTables := make(map[string]bool)
		for _, ft := range projFile.Assets.Fonts {
			scFont := readFontSidecar(projFS, ft.In)
			effCharsFrom := ft.CharsFrom
			if scFont != nil && len(scFont.CharsFrom) > 0 {
				effCharsFrom = scFont.CharsFrom
			}
			rt := resolvedTarget{
				target:       ft,
				sidecar:      scFont,
				charsFrom:    effCharsFrom,
				inferredLocs: make(map[string]bool),
			}
			for _, src := range effCharsFrom {
				cleanSrc := path.Clean(src)
				if tf, ok := tableByRel[cleanSrc]; ok {
					rt.explicitTabs = append(rt.explicitTabs, tf)
					rt.inferredLocs[tf.table.Locale] = true
					claimedTables[cleanSrc] = true
				}
			}
			resolved = append(resolved, rt)
		}

		for _, rt := range resolved {
			if rt.target.Locale == "" && len(rt.inferredLocs) == 0 {
				continue
			}
			// Collect every string table this font target is responsible for.
			tables := selectFontLocaleTables(pl.files, rt.target.Locale, rt.explicitTabs, rt.inferredLocs, claimedTables)
			if len(tables) == 0 {
				continue
			}
			drawable, ok := c.fontDrawableRunes(projRoot, projFS, rt.target, rt.sidecar)
			if !ok {
				continue
			}
			for _, tf := range tables {
				c.checkTableAgainstFont(tf, rt.target.Out, drawable)
			}
		}
	}
}

func selectFontLocaleTables(
	all []*localeTableFile,
	explicitLocale string,
	explicitTabs []*localeTableFile,
	inferredLocs map[string]bool,
	claimedTables map[string]bool,
) []*localeTableFile {
	seen := make(map[string]bool)
	var out []*localeTableFile
	add := func(tf *localeTableFile) {
		if !seen[tf.path] {
			seen[tf.path] = true
			out = append(out, tf)
		}
	}
	for _, tf := range explicitTabs {
		add(tf)
	}
	for _, tf := range all {
		if explicitLocale != "" && tf.table.Locale == explicitLocale {
			add(tf)
			continue
		}
		if inferredLocs[tf.table.Locale] && !claimedTables[path.Clean(tf.projRel)] {
			add(tf)
		}
	}
	return out
}

func readFontSidecar(projFS fs.FS, fontIn string) *importsettings.Font {
	data, err := fs.ReadFile(projFS, importsettings.Path(fontIn))
	if err != nil {
		return nil
	}
	f, err := importsettings.Parse(data)
	if err != nil {
		return nil
	}
	return f.Font
}

// fontDrawableRunes returns the set of runes the font target can draw. When the
// baked .fnt output already exists on disk, it reads that .fnt file so a stale
// build or a missing glyph in the source font is caught directly. Otherwise it
// computes the target's effective character set intersected with the source
// font's glyphs (or scans a glyph-image folder).
func (c *checker) fontDrawableRunes(projRoot string, projFS fs.FS, ft project.FontTarget, sidecar *importsettings.Font) (map[rune]bool, bool) {
	outRel := path.Clean(ft.Out)
	fntData, err := fs.ReadFile(projFS, outRel)
	if err == nil {
		f, err := text.LoadBMFont(fntData, nil)
		if err != nil {
			outPath := outRel
			if projRoot != "." {
				outPath = path.Join(projRoot, outRel)
			}
			c.problem(outPath, 0, "baked font %q: %v", ft.Out, err)
			return nil, false
		}
		drawable := make(map[rune]bool, len(f.Glyphs))
		for r := range f.Glyphs {
			drawable[r] = true
		}
		return drawable, true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}

	// Baked .fnt does not exist yet: derive drawable runes from the target's inputs.
	if ft.GlyphFolder() {
		return glyphFolderDrawableRunes(projFS, ft)
	}
	chars, err := assetbuild.CollectFontChars(projFS, ft, sidecar)
	if err != nil {
		return nil, false
	}
	fontData, err := fs.ReadFile(projFS, path.Clean(ft.In))
	if err != nil {
		// Missing ft.In is already reported by checkRefs.
		return nil, false
	}
	otf, err := opentype.Parse(fontData)
	if err != nil {
		return nil, false
	}
	var buf sfnt.Buffer
	drawable := make(map[rune]bool)
	for _, r := range chars {
		gi, err := otf.GlyphIndex(&buf, r)
		if err == nil && gi != 0 {
			drawable[r] = true
		}
	}
	return drawable, true
}

func glyphFolderDrawableRunes(projFS fs.FS, ft project.FontTarget) (map[rune]bool, bool) {
	inDir := path.Clean(ft.In)
	info, err := fs.Stat(projFS, inDir)
	if err != nil || !info.IsDir() {
		return nil, false
	}
	drawable := make(map[rune]bool)
	err = fs.WalkDir(projFS, inDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if p != inDir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || importsettings.IsSidecar(d.Name()) || !strings.EqualFold(path.Ext(d.Name()), ".png") {
			return nil
		}
		rel := strings.TrimPrefix(p, inDir+"/")
		if inDir == "." {
			rel = p
		}
		if mapped, ok := ft.Glyphs[rel]; ok {
			if rs := []rune(mapped); len(rs) == 1 && !unicode.IsControl(rs[0]) {
				drawable[rs[0]] = true
			}
			return nil
		}
		stem := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		if rs := []rune(stem); len(rs) == 1 && !unicode.IsControl(rs[0]) {
			drawable[rs[0]] = true
		}
		return nil
	})
	if err != nil {
		return nil, false
	}
	return drawable, true
}

func (c *checker) checkTableAgainstFont(tf *localeTableFile, fontOut string, drawable map[rune]bool) {
	var root yaml.Node
	_ = yaml.Unmarshal(tf.data, &root)
	lines := yamlLineCount(tf.data)

	for _, key := range slices.Sorted(maps.Keys(tf.table.Strings)) {
		missing := missingRunesForText(tf.table.Strings[key], drawable)
		if len(missing) == 0 {
			continue
		}
		line := min(yamlLine(&root, []string{"strings", key}), lines)
		c.problem(tf.path, line, "string %q (locale %q): font %q lacks glyph(s) for %s", key, tf.table.Locale, fontOut, assetbuild.FormatMissingRunes(missing))
	}

	for _, key := range slices.Sorted(maps.Keys(tf.table.Plurals)) {
		tf.table.Plurals[key].EachForm(func(cat l10n.PluralCategory, tmpl string) {
			missing := missingRunesForText(tmpl, drawable)
			if len(missing) == 0 {
				return
			}
			line := min(yamlLine(&root, []string{"plurals", key, cat.String()}), lines)
			c.problem(tf.path, line, "plural %q form %q (locale %q): font %q lacks glyph(s) for %s", key, cat.String(), tf.table.Locale, fontOut, assetbuild.FormatMissingRunes(missing))
		})
	}
}

func missingRunesForText(s string, drawable map[rune]bool) []rune {
	plain := assetbuild.ExtractTextChars(s)
	var seen map[rune]struct{}
	for _, r := range plain {
		if drawable[r] {
			continue
		}
		if seen == nil {
			seen = make(map[rune]struct{})
		}
		seen[r] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]rune, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}
