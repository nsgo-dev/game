package credits

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/manifest"
	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

// FileName is the name of the credits file game release writes beside a release's packages.
const FileName = "CREDITS.md"

// Entry is one asset and its credit.
type Entry struct {
	// Asset is the asset's slash-separated path relative to the project root.
	Asset string
	// Credit is the credit section of the asset's import settings sidecar.
	Credit importsettings.Credit
}

// MissingError is returned by Collect when some assets have no credit: no import settings sidecar, or one without a credit section.
type MissingError struct {
	// Assets are the uncredited assets' slash-separated paths, sorted.
	Assets []string
}

// Error names every uncredited asset and the sidecar that would credit it.
func (e *MissingError) Error() string {
	var b strings.Builder
	n := len(e.Assets)
	if n == 1 {
		b.WriteString("credits: 1 asset has no credit; ")
	} else {
		fmt.Fprintf(&b, "credits: %d assets have no credit; ", n)
	}
	b.WriteString("record each one's author and license in the credit section of its import settings sidecar:")
	for _, a := range e.Assets {
		b.WriteString("\n\t")
		b.WriteString(importsettings.Path(a))
	}
	b.WriteString("\na file in the manifest's folder that a target no longer writes is a leftover of an earlier build, which game assets removes")
	return b.String()
}

// Assets returns the slash-separated paths, relative to root, of the assets a reaches, sorted: the PNG images each atlas packs, the input of each texture, audio, font and model target, the buffers and images each model names by relative path (assetbuild.ModelFiles), which its GLB embeds, and every file the manifest lists in its folder, leaving out what a target writes (Assets.IsOutput), since that is made from the targets' inputs. A file a target wrote before its output changed counts as an asset until game assets removes it. A nil a reaches none. It fails when an input a target names is missing; a manifest folder that is not there yet holds no asset.
func Assets(root string, a *project.Assets) ([]string, error) {
	if a == nil {
		return nil, nil
	}
	abs := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = path.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, at := range a.Atlases {
		names, err := pack.ScanDir(abs(at.In))
		if err != nil {
			return nil, fmt.Errorf("credits: atlas %s: %w", at.Name, err)
		}
		for _, name := range names {
			add(path.Join(at.In, name))
		}
	}
	var files []string
	for _, t := range a.Textures {
		files = append(files, t.In)
	}
	for _, au := range a.Audio {
		files = append(files, au.In)
	}
	for _, f := range a.Fonts {
		files = append(files, f.In)
	}
	for _, m := range a.Models {
		files = append(files, m.In)
		data, err := os.ReadFile(abs(m.In))
		if err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		// The buffers and images a model embeds ship inside its GLB, so each is credited as an asset of its own.
		embedded, err := assetbuild.ModelFiles(m.In, data)
		if err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		files = append(files, embedded...)
	}
	for _, f := range files {
		info, err := os.Stat(abs(f))
		if err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("credits: %s is a directory, not an asset", f)
		}
		add(f)
	}
	// The manifest's folder is often where the targets write, so before the first game assets run it may not be there; it then holds no asset.
	if m := a.Manifest; m != nil {
		entries, err := manifest.Scan(abs(m.In), manifest.DefaultOptions())
		if errors.Is(err, fs.ErrNotExist) {
			entries, err = nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		for _, e := range entries {
			if p := path.Join(m.In, e.Path); !a.IsOutput(p) {
				add(p)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// Collect returns the credit of every asset Assets returns, in its order, read from the credit section of the import settings sidecar beside each one. A sidecar counts only under exactly its asset's name and the suffix, so a case-insensitive file system, which would open Hero.PNG.import.yaml for hero.png, credits the same assets as a case-sensitive one. When some assets have none, it returns the entries of the others with a *MissingError naming them; a sidecar that fails to parse is an error of its own.
func Collect(root string, a *project.Assets) ([]Entry, error) {
	assets, err := Assets(root, a)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(assets))
	var missing []string
	// names caches each folder's file names, for the exact-name check.
	names := map[string]map[string]bool{}
	for _, asset := range assets {
		sidecar := importsettings.Path(asset)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sidecar)))
		if errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, asset)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		dir := path.Dir(sidecar)
		if names[dir] == nil {
			list, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
			if err != nil {
				return nil, fmt.Errorf("credits: %w", err)
			}
			names[dir] = map[string]bool{}
			for _, e := range list {
				names[dir][e.Name()] = true
			}
		}
		if !names[dir][path.Base(sidecar)] {
			missing = append(missing, asset)
			continue
		}
		f, err := importsettings.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("credits: %s: %w", sidecar, err)
		}
		if f.Credit == nil {
			missing = append(missing, asset)
			continue
		}
		entries = append(entries, Entry{Asset: asset, Credit: *f.Credit})
	}
	if missing != nil {
		return entries, &MissingError{Assets: missing}
	}
	return entries, nil
}

// Markdown writes entries as the credits file, sorted by asset: a table of each asset's author, license and source, a table of how many assets each license covers, linking each single SPDX identifier to its page in the SPDX license list, and each asset's notice word for word in a fenced block. The output depends only on entries, so the same credits always give the same bytes.
func Markdown(entries []Entry) []byte {
	entries = slices.Clone(entries)
	slices.SortStableFunc(entries, func(a, b Entry) int { return strings.Compare(a.Asset, b.Asset) })
	var b bytes.Buffer
	b.WriteString("# Credits\n\n<!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->\n\n")
	if len(entries) == 0 {
		b.WriteString("The game uses no assets that need crediting.\n")
		return b.Bytes()
	}
	b.WriteString("| Asset | Author | License | Source |\n| --- | --- | --- | --- |\n")
	counts := map[string]int{}
	var licenses []string
	for _, e := range entries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", codeSpan(e.Asset, true), escape(e.Credit.Author), escape(e.Credit.License), source(e.Credit.Source))
		if counts[e.Credit.License] == 0 {
			licenses = append(licenses, e.Credit.License)
		}
		counts[e.Credit.License]++
	}
	slices.Sort(licenses)
	b.WriteString("\n## Licenses\n\n| License | Assets |\n| --- | --- |\n")
	for _, l := range licenses {
		fmt.Fprintf(&b, "| %s | %d |\n", licenseLink(l), counts[l])
	}
	first := true
	for _, e := range entries {
		if e.Credit.Notice == "" {
			continue
		}
		if first {
			b.WriteString("\n## Notices\n")
			first = false
		}
		fence := strings.Repeat("`", max(3, longestRun(e.Credit.Notice, '`')+1))
		fmt.Fprintf(&b, "\n### %s\n\n%stext\n%s\n%s\n", codeSpan(e.Asset, false), fence, e.Credit.Notice, fence)
	}
	return b.Bytes()
}

// licenseLink returns the license expression l as table text, linked to its page in the SPDX license list when it is one listed identifier rather than an expression or a LicenseRef.
func licenseLink(l string) string {
	id := strings.TrimSuffix(l, "+")
	if strings.ContainsAny(id, " ():+") || strings.HasPrefix(id, "LicenseRef-") {
		return escape(l)
	}
	return "[" + escape(l) + "](https://spdx.org/licenses/" + id + ".html)"
}

// source returns a credit's source as table text: an autolink for a URL, its pipes percent-encoded since a table cell ends at one even inside an autolink, else the text escaped.
func source(s string) string {
	if (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")) && !strings.ContainsAny(s, " <>") {
		return "<" + strings.ReplaceAll(s, "|", "%7C") + ">"
	}
	return escape(s)
}

// escape backslash-escapes the ASCII punctuation that Markdown or a table cell would read as markup, so the text shows as written.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>|~&#!", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codeSpan returns s as a Markdown code span, delimited by more backticks than any run in it and, when inTable, with its pipes escaped, since a table cell ends at an unescaped pipe even in a code span, while elsewhere the backslash would show.
func codeSpan(s string, inTable bool) string {
	ticks := strings.Repeat("`", longestRun(s, '`')+1)
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	if inTable {
		s = strings.ReplaceAll(s, "|", `\|`)
	}
	return ticks + pad + s + pad + ticks
}

// longestRun returns the length of the longest run of c in s.
func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := range len(s) {
		if s[i] == c {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// Write writes the credits file of entries, as Markdown makes it, to FileName in dir, creating dir when it is missing, and returns the file's path.
func Write(dir string, entries []Entry) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("credits: %w", err)
	}
	name := filepath.Join(dir, FileName)
	if err := os.WriteFile(name, Markdown(entries), 0o644); err != nil {
		return "", fmt.Errorf("credits: %w", err)
	}
	return name, nil
}
