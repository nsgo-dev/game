package assetbuild

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/text"
	"go.yaml.in/yaml/v3"
)

// ExtractTextChars strips l10n {placeholders} and rich-text markup from s and
// returns its non-control characters in their original order.
func ExtractTextChars(s string) string {
	plain := text.ParseRich(l10n.StripPlaceholders(s)).PlainText()
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, plain)
}

// CollectFontChars collects, merges, sorts and deduplicates the characters a
// TrueType/OpenType font target bakes from its chars, charsFrom and charsFile
// settings (after applying sidecar overrides). When all three are empty, it
// returns project.DefaultFontChars sorted by code point.
func CollectFontChars(fsys fs.FS, target project.FontTarget, sidecar *importsettings.Font) (string, error) {
	chars := target.Chars
	charsFrom := target.CharsFrom
	charsFile := target.CharsFile
	if sidecar != nil {
		if sidecar.Chars != "" {
			chars = sidecar.Chars
		}
		if len(sidecar.CharsFrom) > 0 {
			charsFrom = sidecar.CharsFrom
		}
		if sidecar.CharsFile != "" {
			charsFile = sidecar.CharsFile
		}
	}
	if chars == "" && len(charsFrom) == 0 && charsFile == "" {
		return sortedChars(project.DefaultFontChars), nil
	}
	var buf strings.Builder
	buf.WriteString(chars)
	for _, src := range charsFrom {
		got, err := CollectCharsFromFile(fsys, src)
		if err != nil {
			return "", err
		}
		buf.WriteString(got)
	}
	if charsFile != "" {
		got, err := CollectCharsFile(fsys, charsFile)
		if err != nil {
			return "", err
		}
		buf.WriteString(got)
	}
	result := sortedChars(buf.String())
	if result == "" {
		return "", errors.New("no characters collected from chars, charsFrom and charsFile")
	}
	return result, nil
}

// CollectCharsFile reads a UTF-8 plain-text character list at rel in fsys,
// strips an optional leading UTF-8 BOM and control characters (including line
// breaks), and returns the sorted, deduplicated characters.
func CollectCharsFile(fsys fs.FS, rel string) (string, error) {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return "", fmt.Errorf("charsFile %s: %w", rel, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return "", fmt.Errorf("charsFile %s: file must be valid UTF-8", rel)
	}
	filtered := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, string(data))
	return sortedChars(filtered), nil
}

// CollectCharsFromFile reads a supported source file at rel in fsys (a YAML or
// JSON l10n string table, a YAML data table, or a scene YAML file), strips
// {placeholders} and rich-text markup from its text values, and returns the
// sorted, deduplicated non-control characters.
func CollectCharsFromFile(fsys fs.FS, rel string) (string, error) {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return "", fmt.Errorf("charsFrom %s: %w", rel, err)
	}
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".scene.yaml"), strings.HasSuffix(lower, ".scene.yml"):
		return collectSceneChars(rel, data)
	case strings.HasSuffix(lower, ".table.yaml"), strings.HasSuffix(lower, ".table.yml"):
		return collectDataTableChars(rel, data)
	case strings.HasSuffix(lower, ".json"):
		return collectJSONL10nChars(rel, data)
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		if isYAMLDataTable(data) {
			return collectDataTableChars(rel, data)
		}
		return collectYAMLL10nChars(rel, data)
	default:
		return "", fmt.Errorf("charsFrom %s: unsupported file type %q (expected an l10n .yaml/.json string table, a .table.yaml data table, or a .scene.yaml scene)", rel, path.Ext(rel))
	}
}

func collectYAMLL10nChars(rel string, data []byte) (string, error) {
	tbl, err := l10n.ParseYAML(data)
	if err != nil {
		return "", loaderr.WithFile(err, rel)
	}
	var buf strings.Builder
	for _, v := range tbl.Strings {
		buf.WriteString(ExtractTextChars(v))
	}
	for _, forms := range tbl.Plurals {
		forms.EachForm(func(_ l10n.PluralCategory, tmpl string) {
			buf.WriteString(ExtractTextChars(tmpl))
		})
	}
	return sortedChars(buf.String()), nil
}

func collectJSONL10nChars(rel string, data []byte) (string, error) {
	cat := l10n.NewCatalog("en")
	if err := cat.LoadJSON("en", data); err != nil {
		return "", loaderr.WithFile(err, rel)
	}
	tbl, ok := cat.StringTable("en")
	if !ok {
		return "", nil
	}
	var buf strings.Builder
	for _, v := range tbl.Strings {
		buf.WriteString(ExtractTextChars(v))
	}
	for _, forms := range tbl.Plurals {
		forms.EachForm(func(_ l10n.PluralCategory, tmpl string) {
			buf.WriteString(ExtractTextChars(tmpl))
		})
	}
	return sortedChars(buf.String()), nil
}

// isYAMLDataTable reports whether data is a top-level YAML mapping holding a
// "rows" key and no "locale" key, distinguishing YAML data tables from l10n
// string tables when the filename does not end in .table.yaml.
func isYAMLDataTable(data []byte) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return false
	}
	hasRows, hasLocale := false, false
	for i := 0; i+1 < len(m.Content); i += 2 {
		switch m.Content[i].Value {
		case "rows":
			hasRows = true
		case "locale":
			hasLocale = true
		}
	}
	return hasRows && !hasLocale
}

// collectDataTableChars parses a versioned YAML data table strictly (version: 1
// and rows: list of mappings, refusing unknown top-level keys, merge keys, and
// trailing documents) and collects every string cell's characters.
func collectDataTableChars(rel string, data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return "", loaderr.WithFile(loaderr.Errorf("", fmt.Sprintf("write the data table with version: %d and rows", datatable.FormatVersion), "empty YAML data table"), rel)
		}
		return "", loaderr.Locate(err, rel, data, "fix the YAML syntax in the data table")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return "", loaderr.Locate(err, rel, data, "keep a single YAML document in the data table")
		}
		e := loaderr.Errorf("", "keep a single YAML document in the data table", "unexpected second YAML document in data table")
		e.File, e.Line, e.Column = rel, extra.Line, extra.Column
		return "", e
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", loaderr.WithFile(loaderr.Errorf("", fmt.Sprintf("write the data table as a mapping with version: %d and rows", datatable.FormatVersion), "YAML data table must be a mapping"), rel)
	}
	root := doc.Content[0]
	var versionNode, rowsNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		kNode, vNode := root.Content[i], root.Content[i+1]
		if kNode.Tag == "!!merge" {
			e := loaderr.Errorf("", "write version and rows out in full", "merge keys (<<) are not supported in data tables")
			e.File, e.Line, e.Column = rel, kNode.Line, kNode.Column
			return "", e
		}
		switch kNode.Value {
		case "version":
			versionNode = vNode
		case "rows":
			rowsNode = vNode
		default:
			e := loaderr.Errorf(kNode.Value, "remove the unknown key; a data table has only version and rows", "unknown top-level key %q in data table", kNode.Value)
			e.File, e.Line, e.Column = rel, kNode.Line, kNode.Column
			return "", e
		}
	}
	if versionNode == nil || versionNode.Kind != yaml.ScalarNode || versionNode.Tag != "!!int" {
		return "", loaderr.WithFile(loaderr.Errorf("version", fmt.Sprintf("set version: %d", datatable.FormatVersion), "data table missing integer version (want %d)", datatable.FormatVersion), rel)
	}
	if v, err := strconv.Atoi(versionNode.Value); err != nil || v != datatable.FormatVersion {
		e := loaderr.Errorf("version", fmt.Sprintf("set version: %d", datatable.FormatVersion), "unsupported data table version %s (want %d)", versionNode.Value, datatable.FormatVersion)
		e.File, e.Line, e.Column = rel, versionNode.Line, versionNode.Column
		return "", e
	}
	if rowsNode == nil || rowsNode.Tag == "!!null" {
		return "", nil
	}
	if rowsNode.Kind != yaml.SequenceNode {
		e := loaderr.Errorf("rows", "write rows as a list of mappings", "data table rows must be a list of mappings")
		e.File, e.Line, e.Column = rel, rowsNode.Line, rowsNode.Column
		return "", e
	}
	var buf strings.Builder
	var visitCell func(n *yaml.Node, depth int)
	visitCell = func(n *yaml.Node, depth int) {
		if n == nil || depth > 16 {
			return
		}
		if n.Kind == yaml.AliasNode {
			visitCell(n.Alias, depth+1)
			return
		}
		switch n.Kind {
		case yaml.ScalarNode:
			if n.ShortTag() == "!!str" {
				buf.WriteString(ExtractTextChars(n.Value))
			}
		case yaml.SequenceNode:
			for _, item := range n.Content {
				visitCell(item, depth+1)
			}
		}
	}
	for i, item := range rowsNode.Content {
		if item.Kind == yaml.AliasNode {
			item = item.Alias
		}
		if item == nil || item.Kind != yaml.MappingNode {
			e := loaderr.Errorf(loaderr.Path("rows", i), "make each row a mapping of column names to values", "data table row %d must be a mapping", i+1)
			e.File = rel
			if item != nil {
				e.Line, e.Column = item.Line, item.Column
			}
			return "", e
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			kNode, vNode := item.Content[j], item.Content[j+1]
			if kNode.Tag == "!!merge" {
				e := loaderr.Errorf(loaderr.Path("rows", i), "write each column in the row", "merge keys (<<) are not supported in data tables")
				e.File, e.Line, e.Column = rel, kNode.Line, kNode.Column
				return "", e
			}
			visitCell(vNode, 0)
		}
	}
	return sortedChars(buf.String()), nil
}

// builtinSceneComponents are engine-defined scene components whose string
// fields hold enum names, layer names, or asset paths rather than player-facing
// display text.
var builtinSceneComponents = map[string]bool{
	"animator":    true,
	"body2d":      true,
	"body3d":      true,
	"collider2d":  true,
	"collider3d":  true,
	"cover":       true,
	"light2d":     true,
	"navLink2d":   true,
	"navRegion2d": true,
	"occluder2d":  true,
	"onScreen":    true,
	"skeleton":    true,
	"timer":       true,
	"trigger2d":   true,
	"trigger3d":   true,
}

func collectSceneChars(rel string, data []byte) (string, error) {
	doc, err := sceneformat.Parse(data)
	if err != nil {
		return "", loaderr.WithFile(err, rel)
	}
	var buf strings.Builder
	var visitAny func(v any)
	visitAny = func(v any) {
		switch x := v.(type) {
		case string:
			buf.WriteString(ExtractTextChars(x))
		case []string:
			for _, s := range x {
				buf.WriteString(ExtractTextChars(s))
			}
		case []any:
			for _, item := range x {
				visitAny(item)
			}
		case map[string]any:
			for _, item := range x {
				visitAny(item)
			}
		}
	}
	visitComponents := func(comps map[string]any) {
		for name, val := range comps {
			if builtinSceneComponents[name] {
				continue
			}
			visitAny(val)
		}
	}
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			visitComponents(o.Components)
			for _, v := range o.Properties {
				visitAny(v)
			}
			for _, ov := range o.Overrides {
				visitComponents(ov.Components)
			}
			return true
		})
	}
	for _, ov := range doc.Overrides {
		visitComponents(ov.Components)
	}
	return sortedChars(buf.String()), nil
}

// FormatMissingRunes formats a sorted slice of missing runes for warnings and
// check diagnostics (such as `"龙" (U+9F99), "凤" (U+51E4)`), truncating after
// 20 runes with a count of the remainder.
func FormatMissingRunes(runes []rune) string {
	const limit = 20
	n := min(len(runes), limit)
	parts := make([]string, 0, n)
	for _, r := range runes[:n] {
		parts = append(parts, fmt.Sprintf("%q (U+%04X)", r, r))
	}
	s := strings.Join(parts, ", ")
	if len(runes) > limit {
		s += fmt.Sprintf(", ... (+%d more)", len(runes)-limit)
	}
	return s
}
