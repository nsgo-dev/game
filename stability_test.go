package game_test

import (
	"cmp"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// modulePrefix is what the stability table's package paths are relative to.
const modulePrefix = "github.com/nsgo-dev/"

// stabilityRow is one row of the package table in docs/<lang>/stability.md.
type stabilityRow struct {
	pkg    string // such as game/sprite
	status string // "stable" or "experimental"
	guide  string // the linked page, such as sprite.md, or empty
}

// statusWords maps each language's status word to the status it names.
var statusWords = map[string]map[string]string{
	"en": {"Stable": "stable", "Experimental": "experimental"},
	"zh": {"稳定": "stable", "实验性": "experimental"},
}

var (
	stabilityRowPattern = regexp.MustCompile("^\\| `([^`]+)` \\| ([^|]+?) \\| (.*?) ?\\|$")
	guideLinkPattern    = regexp.MustCompile(`^\[[^\]]+\]\(([a-z0-9-]+\.md)\)$`)
)

// readStabilityTable parses the package table of docs/<lang>/stability.md.
func readStabilityTable(t *testing.T, lang string) []stabilityRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("docs", lang, "stability.md"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []stabilityRow
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		m := stabilityRowPattern.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("docs/%s/stability.md:%d: malformed package row %q", lang, i+1, line)
			continue
		}
		status, ok := statusWords[lang][m[2]]
		if !ok {
			t.Errorf("docs/%s/stability.md:%d: %s has unknown status %q", lang, i+1, m[1], m[2])
		}
		row := stabilityRow{pkg: m[1], status: status}
		if m[3] != "" {
			g := guideLinkPattern.FindStringSubmatch(m[3])
			if g == nil {
				t.Errorf("docs/%s/stability.md:%d: %s has a guide that is not a link to a page: %q", lang, i+1, m[1], m[3])
			} else {
				row.guide = g[1]
				if _, err := os.Stat(filepath.Join("docs", lang, row.guide)); err != nil {
					t.Errorf("docs/%s/stability.md:%d: %s links to a missing page: %v", lang, i+1, m[1], err)
				}
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatalf("docs/%s/stability.md has no package rows", lang)
	}
	return rows
}

// modulePackages returns the module's packages outside internal/, by their path relative to modulePrefix, with the module paths each one's non-test files import.
func modulePackages(t *testing.T) map[string][]string {
	t.Helper()
	pkgs := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != "." && (name == "testdata" || name == "internal" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if p != "." {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir // another module
				}
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		pkg := path.Join("game", filepath.ToSlash(filepath.Dir(p)))
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		imports := pkgs[pkg]
		for _, spec := range f.Imports {
			ip, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if (ip == "github.com/nsgo-dev/game" || strings.HasPrefix(ip, "github.com/nsgo-dev/game/")) && !slices.Contains(imports, strings.TrimPrefix(ip, modulePrefix)) {
				imports = append(imports, strings.TrimPrefix(ip, modulePrefix))
			}
		}
		pkgs[pkg] = imports
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

// TestStabilityTable checks that docs/en/stability.md and docs/zh/stability.md give every package of the module exactly one status, agree with each other, and keep stable packages off experimental ones.
func TestStabilityTable(t *testing.T) {
	en := readStabilityTable(t, "en")
	zh := readStabilityTable(t, "zh")

	for i := 1; i < len(en); i++ {
		if en[i-1].pkg >= en[i].pkg {
			t.Errorf("docs/en/stability.md: %s should come after %s; rows are sorted by package and listed once", en[i-1].pkg, en[i].pkg)
		}
	}
	if !slices.Equal(en, zh) {
		t.Errorf("docs/en/stability.md and docs/zh/stability.md list different packages, statuses or guides:\nen: %v\nzh: %v", en, zh)
	}

	pkgs := modulePackages(t)
	status := map[string]string{}
	for _, row := range en {
		status[row.pkg] = row.status
		if _, ok := pkgs[row.pkg]; !ok {
			t.Errorf("docs/en/stability.md lists %s, which is not a package of the module", row.pkg)
		}
	}
	var missing []string
	for pkg := range pkgs {
		if _, ok := status[pkg]; !ok {
			missing = append(missing, pkg)
		}
	}
	slices.Sort(missing)
	for _, pkg := range missing {
		t.Errorf("package %s has no status: add a row to docs/en/stability.md and docs/zh/stability.md", pkg)
	}

	for _, row := range en {
		if row.status != "stable" {
			continue
		}
		for _, imp := range pkgs[row.pkg] {
			if strings.Contains("/"+imp+"/", "/internal/") {
				continue
			}
			if status[imp] != "stable" {
				t.Errorf("stable package %s imports %s, which is %s", row.pkg, imp, cmp.Or(status[imp], "not listed"))
			}
		}
	}
}
