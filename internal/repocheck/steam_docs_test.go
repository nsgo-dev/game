package repocheck_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vdfEntry is a single key/value or key/section item in a Valve KeyValues (VDF) document.
// Repeated keys (such as multiple FileExclusion entries in a DepotBuild) are preserved in order.
type vdfEntry struct {
	Key      string
	Value    string
	Children []vdfEntry
	IsBlock  bool
}

func (e vdfEntry) find(key string) (vdfEntry, bool) {
	for _, c := range e.Children {
		if c.Key == key {
			return c, true
		}
	}
	return vdfEntry{}, false
}

func (e vdfEntry) findAll(key string) []vdfEntry {
	var out []vdfEntry
	for _, c := range e.Children {
		if c.Key == key {
			out = append(out, c)
		}
	}
	return out
}

type vdfTokenKind int

const (
	vdfTokEOF vdfTokenKind = iota
	vdfTokString
	vdfTokLBrace
	vdfTokRBrace
)

type vdfToken struct {
	kind vdfTokenKind
	val  string
	line int
}

func lexVDF(src string) ([]vdfToken, error) {
	var toks []vdfToken
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '{' {
			toks = append(toks, vdfToken{kind: vdfTokLBrace, val: "{", line: line})
			i++
			continue
		}
		if c == '}' {
			toks = append(toks, vdfToken{kind: vdfTokRBrace, val: "}", line: line})
			i++
			continue
		}
		if c == '"' {
			startLine := line
			i++
			var b strings.Builder
			closed := false
			for i < len(src) {
				ch := src[i]
				if ch == '\n' {
					return nil, fmt.Errorf("line %d: unclosed quoted string", startLine)
				}
				if ch == '\\' {
					if i+1 >= len(src) {
						return nil, fmt.Errorf("line %d: trailing backslash in quoted string", startLine)
					}
					next := src[i+1]
					switch next {
					case '"', '\\':
						b.WriteByte(next)
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte('\\')
						b.WriteByte(next)
					}
					i += 2
					continue
				}
				if ch == '"' {
					i++
					closed = true
					break
				}
				b.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("line %d: unclosed quoted string", startLine)
			}
			toks = append(toks, vdfToken{kind: vdfTokString, val: b.String(), line: startLine})
			continue
		}
		return nil, fmt.Errorf("line %d: unexpected character %q", line, c)
	}
	toks = append(toks, vdfToken{kind: vdfTokEOF, line: line})
	return toks, nil
}

type vdfParser struct {
	toks []vdfToken
	pos  int
}

func (p *vdfParser) peek() vdfToken {
	return p.toks[p.pos]
}

func (p *vdfParser) next() vdfToken {
	t := p.toks[p.pos]
	if p.pos+1 < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *vdfParser) parseEntries(stopAtRBrace bool) ([]vdfEntry, error) {
	var out []vdfEntry
	for {
		tok := p.peek()
		if tok.kind == vdfTokEOF {
			if stopAtRBrace {
				return nil, fmt.Errorf("line %d: unclosed '{' block", tok.line)
			}
			return out, nil
		}
		if tok.kind == vdfTokRBrace {
			if !stopAtRBrace {
				return nil, fmt.Errorf("line %d: unexpected '}'", tok.line)
			}
			p.next()
			return out, nil
		}
		if tok.kind != vdfTokString {
			return nil, fmt.Errorf("line %d: expected quoted key, got %q", tok.line, tok.val)
		}
		key := p.next()
		valTok := p.peek()
		switch valTok.kind {
		case vdfTokString:
			p.next()
			out = append(out, vdfEntry{Key: key.val, Value: valTok.val})
		case vdfTokLBrace:
			p.next()
			children, err := p.parseEntries(true)
			if err != nil {
				return nil, err
			}
			out = append(out, vdfEntry{Key: key.val, Children: children, IsBlock: true})
		default:
			return nil, fmt.Errorf("line %d: key %q missing quoted value or '{' block", key.line, key.val)
		}
	}
}

func parseVDF(src string) (vdfEntry, error) {
	toks, err := lexVDF(src)
	if err != nil {
		return vdfEntry{}, err
	}
	p := &vdfParser{toks: toks}
	entries, err := p.parseEntries(false)
	if err != nil {
		return vdfEntry{}, err
	}
	if len(entries) != 1 || !entries[0].IsBlock {
		return vdfEntry{}, fmt.Errorf("expected a single top-level VDF block, got %d entries", len(entries))
	}
	return entries[0], nil
}

type docVDFBlock struct {
	File    string
	Line    int
	Header  string
	Content string
}

// extractVDFBlocks extracts every fenced code block in a Markdown file that is tagged as vdf or starts with a `// ... .vdf` header comment.
func extractVDFBlocks(path string) ([]docVDFBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var (
		blocks    []docVDFBlock
		inFence   bool
		fenceLang string
		startLine int
		buf       []string
	)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
				fenceLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				startLine = i + 2
				buf = buf[:0]
			} else {
				content := strings.Join(buf, "\n")
				firstLine := ""
				if len(buf) > 0 {
					firstLine = strings.TrimSpace(buf[0])
				}
				isVDF := fenceLang == "vdf" || (strings.HasPrefix(firstLine, "//") && strings.HasSuffix(firstLine, ".vdf"))
				if isVDF {
					blocks = append(blocks, docVDFBlock{
						File:    filepath.ToSlash(path),
						Line:    startLine,
						Header:  firstLine,
						Content: content,
					})
				}
				inFence = false
				fenceLang = ""
			}
			continue
		}
		if inFence {
			buf = append(buf, line)
		}
	}
	return blocks, nil
}

func TestSteamDocsVDFSamples(t *testing.T) {
	docPaths := []string{
		"docs/en/steam.md",
		"docs/zh/steam.md",
	}

	var blockCounts []int
	for _, docPath := range docPaths {
		blocks, err := extractVDFBlocks(docPath)
		if err != nil {
			t.Fatalf("%s: %v", docPath, err)
		}
		if len(blocks) < 5 {
			t.Fatalf("%s: found %d VDF blocks, want at least 5 (app_build, 3 depots, rich_presence_loc)", docPath, len(blocks))
		}
		blockCounts = append(blockCounts, len(blocks))

		var (
			appBuild        *vdfEntry
			depots          []vdfEntry
			richPresenceLoc *vdfEntry
		)
		for _, blk := range blocks {
			root, err := parseVDF(blk.Content)
			if err != nil {
				t.Errorf("%s:%d (%s): invalid VDF: %v", blk.File, blk.Line, blk.Header, err)
				continue
			}
			switch root.Key {
			case "AppBuild":
				cp := root
				appBuild = &cp
			case "DepotBuild":
				depots = append(depots, root)
			case "lang":
				cp := root
				richPresenceLoc = &cp
			default:
				t.Errorf("%s:%d: unexpected top-level VDF key %q", blk.File, blk.Line, root.Key)
			}
		}

		if appBuild == nil {
			t.Fatalf("%s: missing AppBuild VDF sample", docPath)
		}
		for _, reqKey := range []string{"AppID", "Desc", "BuildOutput", "ContentRoot", "SetLive"} {
			v, ok := appBuild.find(reqKey)
			if !ok || v.IsBlock || strings.TrimSpace(v.Value) == "" {
				t.Errorf("%s: AppBuild missing string field %q", docPath, reqKey)
			}
		}
		if setLive, ok := appBuild.find("SetLive"); ok && strings.EqualFold(strings.TrimSpace(setLive.Value), "default") {
			t.Errorf("%s: AppBuild SetLive must not target %q directly; use a non-default staging/beta branch", docPath, setLive.Value)
		}
		depotsBlock, ok := appBuild.find("Depots")
		if !ok || !depotsBlock.IsBlock || len(depotsBlock.Children) != 3 {
			t.Errorf("%s: AppBuild Depots block should list 3 desktop depots, got %d", docPath, len(depotsBlock.Children))
		}

		if len(depots) != 3 {
			t.Errorf("%s: got %d DepotBuild samples, want 3 (macOS, Windows, Linux)", docPath, len(depots))
		}
		referencedDepotIDs := make(map[string]bool, len(depotsBlock.Children))
		for _, d := range depotsBlock.Children {
			referencedDepotIDs[d.Key] = true
		}
		for i, dep := range depots {
			idEntry, ok := dep.find("DepotID")
			if !ok || idEntry.IsBlock || idEntry.Value == "" {
				t.Errorf("%s: DepotBuild[%d] missing DepotID", docPath, i)
			} else if !referencedDepotIDs[idEntry.Value] {
				t.Errorf("%s: DepotBuild[%d] DepotID %q not referenced in AppBuild Depots", docPath, i, idEntry.Value)
			}
			fm, ok := dep.find("FileMapping")
			if !ok || !fm.IsBlock {
				t.Errorf("%s: DepotBuild[%d] missing FileMapping block", docPath, i)
			} else {
				for _, k := range []string{"LocalPath", "DepotPath", "recursive"} {
					if v, ok := fm.find(k); !ok || v.IsBlock || v.Value == "" {
						t.Errorf("%s: DepotBuild[%d] FileMapping missing %q", docPath, i, k)
					}
				}
			}
			excludesSteamAppID := false
			for _, ex := range dep.findAll("FileExclusion") {
				if ex.Value == "steam_appid.txt" {
					excludesSteamAppID = true
				}
			}
			if !excludesSteamAppID {
				t.Errorf("%s: DepotBuild[%d] should exclude steam_appid.txt", docPath, i)
			}
		}

		if richPresenceLoc == nil {
			t.Fatalf("%s: missing lang rich presence localization VDF sample", docPath)
		}
		for _, lang := range []string{"english", "schinese"} {
			langSec, ok := richPresenceLoc.find(lang)
			if !ok || !langSec.IsBlock {
				t.Errorf("%s: rich presence VDF missing %q section", docPath, lang)
				continue
			}
			tokens, ok := langSec.find("tokens")
			if !ok || !tokens.IsBlock {
				t.Errorf("%s: rich presence VDF %q missing tokens block", docPath, lang)
				continue
			}
			if tok, ok := tokens.find("#Status_InLevel"); !ok || !strings.Contains(tok.Value, "%level%") {
				t.Errorf("%s: rich presence VDF %q tokens missing #Status_InLevel with %%level%%", docPath, lang)
			}
		}
	}

	if blockCounts[0] != blockCounts[1] {
		t.Errorf("VDF block count mismatch between docs/en/steam.md (%d) and docs/zh/steam.md (%d)", blockCounts[0], blockCounts[1])
	}
}

func TestSteamDocsCrossLinks(t *testing.T) {
	linkedFrom := []string{
		"docs/en/shipping.md",
		"docs/zh/shipping.md",
		"docs/en/liveops.md",
		"docs/zh/liveops.md",
		"docs/en/presence.md",
		"docs/zh/presence.md",
		"docs/en/modding.md",
		"docs/zh/modding.md",
	}
	for _, p := range linkedFrom {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !strings.Contains(string(data), "steam.md") {
			t.Errorf("%s: expected a link to steam.md", p)
		}
	}
}

func TestParseVDFErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"unclosed quote", `"AppBuild" { "AppID" "1000 }`},
		{"unclosed brace", `"AppBuild" { "AppID" "1000"`},
		{"extra closing brace", `"AppBuild" { "AppID" "1000" } }`},
		{"missing value", `"AppBuild" { "AppID" }`},
		{"bare token", `AppBuild { "AppID" "1000" }`},
		{"multiple roots", `"A" {} "B" {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseVDF(tc.src); err == nil {
				t.Fatalf("parseVDF(%q) succeeded, want error", tc.src)
			}
		})
	}
}
