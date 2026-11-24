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

	"github.com/nsgo-dev/game/packages/game/placeholder"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// repoRoot is the repository's root, from this package's folder.
var repoRoot = filepath.Join("..", "..", "..")

// creditsFile is the file that lists the assets in its folder and the folders below it, each with its license.
const creditsFile = "CREDITS.md"

// mediaExts are the extensions of the files that need a credits entry: images, sounds, fonts, models, and the maps, tilesets, atlases and font descriptors other tools export.
var mediaExts = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ktx2",
	".wav", ".ogg", ".mp3", ".aac", ".m4a", ".flac",
	".ttf", ".otf", ".woff", ".woff2", ".fnt",
	".glb", ".gltf", ".bin", ".fbx", ".obj", ".blend",
	".tmj", ".tmx", ".tsj", ".tsx", ".ldtk", ".aseprite", ".ase",
}

// creditRowPattern matches a row of a credits table that names a file, such as "| `assets/hero.png` | ... |".
var creditRowPattern = regexp.MustCompile("^\\| `([^`]+)` \\|")

// readCredits parses the credits tables of the CREDITS.md at file: each listed path, relative to the file's folder, and its license, the column headed "License". It reports a listed path without a license as a problem.
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
		entries[m[1]] = cells[licenseCol]
	}
	return entries, problems
}

func TestReadCredits(t *testing.T) {
	entries, problems := readCredits([]byte("# Credits\n\n| File | Author | License |\n| --- | --- | --- |\n| `a.png` | Someone | CC0-1.0 |\n| `b.wav` | Someone |  |\n\n| `c.png` | no table header |\n"), "CREDITS.md")
	if len(entries) != 1 || entries["a.png"] != "CC0-1.0" {
		t.Errorf("entries = %v, want a.png under CC0-1.0", entries)
	}
	if len(problems) != 2 || !strings.Contains(problems[0], "b.wav") || !strings.Contains(problems[1], "c.png") {
		t.Errorf("problems = %q, want b.wav and c.png without a license", problems)
	}
}

// TestPackCredits checks that the pack's CREDITS.md lists every asset in the pack under CC0, and nothing else.
func TestPackCredits(t *testing.T) {
	data, err := fs.ReadFile(placeholder.FS, creditsFile)
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

// TestExamplesUseCreditedAssets keeps the promise that the examples, the tutorial, the docs and the project template use only the placeholder pack or assets of their own whose license is written down: every image, sound, font, model and exported map under them must be listed, with its license, in a CREDITS.md in its folder or a folder above it, below the folder scanned.
func TestExamplesUseCreditedAssets(t *testing.T) {
	for _, dir := range []string{"examples", "docs", "template"} {
		root := filepath.Join(repoRoot, dir)
		credits := map[string]map[string]string{} // by the slash-separated folder of each CREDITS.md, relative to root
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
			if !slices.Contains(mediaExts, strings.ToLower(filepath.Ext(p))) {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			for folder := path.Dir(rel); ; folder = path.Dir(folder) {
				entries, ok := credits[folder]
				if !ok {
					entries = map[string]string{}
					data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(folder), creditsFile))
					if err == nil {
						var problems []string
						entries, problems = readCredits(data, path.Join(dir, folder, creditsFile))
						for _, p := range problems {
							t.Error(p)
						}
					} else if !os.IsNotExist(err) {
						return err
					}
					credits[folder] = entries
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
			t.Errorf("%s/%s has no credits entry: use the placeholder pack (packages/game/placeholder) instead, or list the file with its license in a %s beside it", dir, rel, creditsFile)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
