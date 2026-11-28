package assetcheck

import (
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/l10n"
	"go.yaml.in/yaml/v3"
)

type localeTableFile struct {
	path    string
	projRel string
	data    []byte
	table   *l10n.StringTable
}

type projectLocales struct {
	strings map[string]map[string]string
	plurals map[string]map[string]l10n.PluralForms
	files   []*localeTableFile
}

// loadLocaleTables discovers and parses every l10n YAML string table in the scanned directory, grouped by project root.
func (c *checker) loadLocaleTables(yamlFiles []string) {
	c.localeTables = make(map[string]*projectLocales)
	for _, p := range yamlFiles {
		data, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			continue
		}
		if !looksLikeStringTable(data) {
			continue
		}
		tbl, err := l10n.ParseYAML(data)
		if err != nil {
			c.yamlError(p, data, err)
			continue
		}
		if unused := l10n.CheckStringTablePlurals(tbl); len(unused) > 0 {
			var root yaml.Node
			_ = yaml.Unmarshal(data, &root)
			lines := yamlLineCount(data)
			for _, u := range unused {
				line := min(yamlLine(&root, []string{"plurals", u.Key, u.Category.String()}), lines)
				c.problem(p, line, "plural %q (locale %q): CLDR category %q is never selected for integer counts", u.Key, u.Locale, u.Category)
			}
		}
		proj := c.projectRoot(path.Dir(p))
		projRel := p
		if proj != "." {
			projRel = strings.TrimPrefix(p, proj+"/")
		}
		pl := c.localeTables[proj]
		if pl == nil {
			pl = &projectLocales{
				strings: make(map[string]map[string]string),
				plurals: make(map[string]map[string]l10n.PluralForms),
			}
			c.localeTables[proj] = pl
		}
		pl.files = append(pl.files, &localeTableFile{
			path:    p,
			projRel: projRel,
			data:    data,
			table:   tbl,
		})
		if pl.strings[tbl.Locale] == nil {
			pl.strings[tbl.Locale] = make(map[string]string)
		}
		for k, v := range tbl.Strings {
			pl.strings[tbl.Locale][k] = v
		}
		if len(tbl.Plurals) > 0 {
			if pl.plurals[tbl.Locale] == nil {
				pl.plurals[tbl.Locale] = make(map[string]l10n.PluralForms)
			}
			for k, v := range tbl.Plurals {
				pl.plurals[tbl.Locale][k] = v
			}
		}
	}
}

// looksLikeStringTable checks whether data is a YAML mapping holding version + locale and only keys of l10n.StringTable.
func looksLikeStringTable(data []byte) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return false
	}
	hasVersion, hasLocale, hasEntries := false, false, false
	for i := 0; i+1 < len(doc.Content); i += 2 {
		k, v := doc.Content[i].Value, doc.Content[i+1]
		switch k {
		case "version":
			if v.Kind != yaml.ScalarNode {
				return false
			}
			hasVersion = true
		case "locale":
			if v.Kind != yaml.ScalarNode || v.Value == "" {
				return false
			}
			hasLocale = true
		case "strings", "plurals":
			if v.Kind == yaml.MappingNode {
				hasEntries = true
			}
		default:
			return false
		}
	}
	return hasVersion && hasLocale && hasEntries
}

// checkScreen checks the *.screen.yaml file at p:
//   - parses and validates the file with gameui.ParseScreen;
//   - verifies every sprite path exists and decodes (and that spriteFrame exists when sprite points to a sprite atlas);
//   - runs gameui.CheckScreen for touch area size (< 44 pt), localized text overflow across reference device sizes,
//     missing localization keys, and text contrast (< 4.5:1), reporting each problem with its file, line, and node.
func (c *checker) checkScreen(p string) {
	c.report.Screens++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := gameui.ParseScreen(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err)
		return
	}
	lines := yamlLineCount(data)
	proj := c.projectRoot(path.Dir(p))

	// Check sprite file references and optional atlas frames.
	var checkSprites func(nodes []gameui.ScreenNodeDoc, prefix []string)
	checkSprites = func(nodes []gameui.ScreenNodeDoc, prefix []string) {
		for i, n := range nodes {
			np := make([]string, 0, len(prefix)+1)
			np = append(np, prefix...)
			np = append(np, strconv.Itoa(i))
			if n.Sprite != "" {
				spriteLine := min(yamlLine(&root, append(np, "sprite")), lines)
				if strings.EqualFold(path.Ext(n.Sprite), ".json") {
					resolved, ok := c.fileRef(p, spriteLine, proj, n.Sprite, "sprite")
					if ok && n.SpriteFrame != "" {
						if atlas := c.atlases[resolved]; atlas != nil && atlas.frames != nil && !atlas.frames[n.SpriteFrame] {
							frameLine := min(yamlLine(&root, append(np, "spriteFrame")), lines)
							c.problem(p, frameLine, "node %q: spriteFrame %q is not in atlas %q", n.ID, n.SpriteFrame, n.Sprite)
						}
					}
				} else {
					c.imageRef(p, spriteLine, proj, n.Sprite, "sprite")
				}
			}
			for _, layer := range []struct {
				field string
				ref   string
			}{
				{"under", n.Under},
				{"over", n.Over},
				{"progress", n.Progress},
			} {
				if layer.ref != "" {
					layerLine := min(yamlLine(&root, append(np, layer.field)), lines)
					c.imageRef(p, layerLine, proj, layer.ref, layer.field)
				}
			}
			if len(n.Children) > 0 {
				cp := make([]string, 0, len(np)+1)
				cp = append(cp, np...)
				cp = append(cp, "children")
				checkSprites(n.Children, cp)
			}
		}
	}
	checkSprites(doc.Nodes, []string{"nodes"})

	var checkOpts gameui.ScreenCheckOptions
	if pl := c.localeTables[proj]; pl != nil {
		checkOpts.Locales = pl.strings
		checkOpts.Plurals = pl.plurals
	}
	for _, issue := range gameui.CheckScreen(doc, checkOpts) {
		line := min(yamlLine(&root, issue.NodePath), lines)
		c.problem(p, line, "%s", issue.Message)
	}
}
