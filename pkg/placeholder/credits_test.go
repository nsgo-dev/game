package placeholder_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/placeholder"
)

// repoRoot is the repository's root, from this package's folder.
var repoRoot = filepath.Join("..", "..")

// creditsFile is the file that lists the assets in its folder and the folders below it, each with its license.
const creditsFile = "CREDITS.md"

// mediaExts are the extensions of the files that need a credits entry: images, sounds, fonts, models, and the maps, tilesets and font descriptors other tools export. An atlas exported as JSON needs one too; isMedia finds it by the image beside it.
var mediaExts = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ktx2",
	".wav", ".ogg", ".mp3", ".aac", ".m4a", ".flac",
	".ttf", ".otf", ".woff", ".woff2", ".fnt",
	".glb", ".gltf", ".bin", ".fbx", ".obj", ".blend",
	".tmj", ".tmx", ".tsj", ".tsx", ".ldtk", ".aseprite", ".ase",
}

// atlasImageExts are the extensions of the image an atlas JSON export, such as Aseprite's or TexturePacker's, sits beside under the same name.
var atlasImageExts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ktx2"}

// isMedia reports whether the file at p needs a credits entry: a file with one of mediaExts, or a JSON file beside an image of the same name, which is that image's atlas.
func isMedia(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	if slices.Contains(mediaExts, ext) {
		return true
	}
	if ext != ".json" {
		return false
	}
	base := strings.TrimSuffix(p, filepath.Ext(p))
	for _, img := range atlasImageExts {
		if _, err := os.Stat(base + img); err == nil {
			return true
		}
	}
	return false
}

// licenses are the SPDX identifiers a credits entry may give: the licenses an asset in this repository may be under.
var licenses = []string{
	"CC0-1.0", "Apache-2.0", "MIT", "BSD-2-Clause", "BSD-3-Clause", "Unlicense", "OFL-1.1",
	"CC-BY-3.0", "CC-BY-4.0", "CC-BY-SA-3.0", "CC-BY-SA-4.0",
}

// licenseLinkPattern matches a license cell written as a Markdown link, such as "[Apache-2.0](../../LICENSE)", capturing its text.
var licenseLinkPattern = regexp.MustCompile(`^\[([^\]]+)\]\([^)]*\)$`)

// creditRowPattern matches a row of a credits table that names a file, such as "| `assets/hero.png` | ... |".
var creditRowPattern = regexp.MustCompile("^\\| `([^`]+)` \\|")

// readCredits parses the credits tables of the CREDITS.md at file: each listed path, relative to the file's folder, and its license, the SPDX identifier in the column headed "License", written as it is or as the text of a Markdown link. It reports a listed path without a license, or with a license that is not one of licenses, as a problem.
func readCredits(data []byte, file string) (entries map[string]string, problems []string) {
	entries = map[string]string{}
	licenseCol := -1
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "|") {
			licenseCol = -1
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if i := slices.Index(cells, "License"); i >= 0 {
			licenseCol = i
			continue
		}
		m := creditRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if licenseCol < 0 || licenseCol >= len(cells) || cells[licenseCol] == "" {
			problems = append(problems, fmt.Sprintf("%s:%d: %s has no license; list it in a table with a License column", file, n, m[1]))
			continue
		}
		license := cells[licenseCol]
		if l := licenseLinkPattern.FindStringSubmatch(license); l != nil {
			license = l[1]
		}
		if !slices.Contains(licenses, license) {
			problems = append(problems, fmt.Sprintf("%s:%d: %s has the license %q; give one of the SPDX identifiers %s", file, n, m[1], cells[licenseCol], strings.Join(licenses, ", ")))
			continue
		}
		entries[m[1]] = license
	}
	return entries, problems
}

func TestReadCredits(t *testing.T) {
	entries, problems := readCredits([]byte("# Credits\n\n| File | Author | License |\n| --- | --- | --- |\n| `a.png` | Someone | CC0-1.0 |\n| `b.wav` | Someone |  |\n| `d.png` | Someone | [Apache-2.0](../LICENSE) |\n| `e.png` | Someone | TBD |\n\n| `c.png` | no table header |\n"), "CREDITS.md")
	if len(entries) != 2 || entries["a.png"] != "CC0-1.0" || entries["d.png"] != "Apache-2.0" {
		t.Errorf("entries = %v, want a.png under CC0-1.0 and d.png under Apache-2.0", entries)
	}
	if len(problems) != 3 || !strings.Contains(problems[0], "b.wav") || !strings.Contains(problems[1], `e.png has the license "TBD"`) || !strings.Contains(problems[2], "c.png") {
		t.Errorf("problems = %q, want b.wav and c.png without a license and e.png with no SPDX identifier", problems)
	}
}

func TestIsMedia(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"hero.png", "hero.json", "data.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for f, want := range map[string]bool{"hero.png": true, "hero.json": true, "data.json": false, "main.go": false, "level.TMJ": true} {
		if got := isMedia(filepath.Join(dir, f)); got != want {
			t.Errorf("isMedia(%s) = %v, want %v", f, got, want)
		}
	}
}

// TestPackCredits checks that the pack's CREDITS.md lists every asset in the pack under CC0, and nothing else.
func TestPackCredits(t *testing.T) {
	data, err := fs.ReadFile(placeholder.FS(), creditsFile)
	if err != nil {
		t.Fatal(err)
	}
	listed, problems := readCredits(data, "pack/"+creditsFile)
	for _, p := range problems {
		t.Error(p)
	}
	var assets []string
	for _, p := range packFiles(t) {
		if importsettings.IsSidecar(p) {
			continue
		}
		assets = append(assets, p)
		if lic, ok := listed[p]; !ok {
			t.Errorf("pack/%s lists no %s", creditsFile, p)
		} else if lic != placeholder.License {
			t.Errorf("pack/%s gives %s the license %q, want %s", creditsFile, p, lic, placeholder.License)
		}
	}
	for p := range listed {
		if !slices.Contains(assets, p) {
			t.Errorf("pack/%s lists %s, which the pack does not hold", creditsFile, p)
		}
	}
}

// TestExamplesUseCreditedAssets keeps the promise that the examples, the tutorial, the docs and the project template use only the placeholder pack or assets of their own whose license is written down: every image, sound, font, model, exported map or tileset and atlas JSON export under them must be listed, with an SPDX license, in a CREDITS.md in its folder or a folder above it, below the folder scanned, and every file a CREDITS.md lists must exist.
func TestExamplesUseCreditedAssets(t *testing.T) {
	for _, dir := range []string{"examples", "docs", "template"} {
		root := filepath.Join(repoRoot, dir)
		credits := map[string]map[string]string{} // by the slash-separated folder of each CREDITS.md, relative to root
		// load parses the CREDITS.md in folder once, reporting its problems and the files it lists that do not exist.
		load := func(folder string) (map[string]string, error) {
			if entries, ok := credits[folder]; ok {
				return entries, nil
			}
			entries := map[string]string{}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(folder), creditsFile))
			if err == nil {
				var problems []string
				entries, problems = readCredits(data, path.Join(dir, folder, creditsFile))
				for _, p := range problems {
					t.Error(p)
				}
				for listed := range entries {
					if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(folder), filepath.FromSlash(listed))); err != nil {
						t.Errorf("%s lists %s, which does not exist", path.Join(dir, folder, creditsFile), listed)
					}
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			credits[folder] = entries
			return entries, nil
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "dist" || d.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.Name() == creditsFile {
				_, err := load(path.Dir(rel))
				return err
			}
			if !isMedia(p) {
				return nil
			}
			for folder := path.Dir(rel); ; folder = path.Dir(folder) {
				entries, err := load(folder)
				if err != nil {
					return err
				}
				name := strings.TrimPrefix(rel, folder+"/")
				if folder == "." {
					name = rel
				}
				if _, ok := entries[name]; ok {
					return nil
				}
				if folder == "." {
					break
				}
			}
			t.Errorf("%s/%s has no credits entry: use the placeholder pack (pkg/placeholder) instead, or list the file with its license in a %s beside it", dir, rel, creditsFile)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
