package repocheck_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// uncatalogedGroups are the grouped directories under pkg/ whose packages need no row of their own in README.md and docs/en/index.md, with the reason. Every other group's packages, like pkg/online's, are catalogued one by one.
var uncatalogedGroups = map[string]string{
	"tooling": "build, editor and dev-time tooling, listed on the site's API reference; tooling/library has a row because games use it",
}

// stdNameExemptions are the module's packages allowed, for now, to share their name with a standard library package, by package name, with the reason. A rename that ends the clash must delete its entry, which the test enforces.
var stdNameExemptions = map[string]string{
	"plugin": "pkg/plugin, renamed in a later commit",
	"token":  "pkg/online/accounts/token, renamed in a later commit",
}

var (
	readmePackageRowPattern = regexp.MustCompile(`^\| \*\*[^*]+\*\* \| (.+) \|$`)
	backtickedPattern       = regexp.MustCompile("`([^`]+)`")
	indexImportPathPattern  = regexp.MustCompile("^\\| `[^`]+` \\| `(github\\.com/nsgo-dev/game(?:/[a-z0-9/]+)?)` \\|")
)

// labelerRule is one label's rules in .github/labeler.yml, as far as the catalog test reads them.
type labelerRule struct {
	ChangedFiles []struct {
		AnyGlobToAnyFile []string `yaml:"any-glob-to-any-file"`
	} `yaml:"changed-files"`
}

// hasGoFiles reports whether dir holds a non-test Go file, that is, whether it is a package directory.
func hasGoFiles(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

// isDir reports whether p is an existing directory.
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// subdirs returns the names of dir's subdirectories, sorted.
func subdirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// catalogedPackages returns the packages README.md and docs/en/index.md must each list, by their path under pkg/: every flat package and every package directly under a group not in uncatalogedGroups.
func catalogedPackages(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	for _, name := range subdirs(t, "pkg") {
		dir := path.Join("pkg", name)
		if hasGoFiles(t, dir) {
			pkgs = append(pkgs, name)
			continue
		}
		if _, ok := uncatalogedGroups[name]; ok {
			continue
		}
		for _, sub := range subdirs(t, dir) {
			if hasGoFiles(t, path.Join(dir, sub)) {
				pkgs = append(pkgs, path.Join(name, sub))
			}
		}
	}
	for group := range uncatalogedGroups {
		if _, err := os.Stat(path.Join("pkg", group)); err != nil {
			t.Errorf("uncatalogedGroups names pkg/%s, which does not exist: delete its entry", group)
		}
	}
	return pkgs
}

// readmePackages returns the package names in the rows of README.md's package table.
func readmePackages(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## Package Overview\n")
	if !ok {
		t.Fatal("README.md has no Package Overview section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	names := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		m := readmePackageRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, b := range backtickedPattern.FindAllStringSubmatch(m[1], -1) {
			names[b[1]] = true
		}
	}
	if len(names) == 0 {
		t.Fatal("README.md's package table has no rows")
	}
	return names
}

// indexImportPaths returns the import paths of the package table in docs/<lang>/index.md, in order.
func indexImportPaths(t *testing.T, lang string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("docs", lang, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := indexImportPathPattern.FindStringSubmatch(line); m != nil {
			paths = append(paths, m[1])
		}
	}
	if len(paths) == 0 {
		t.Fatalf("docs/%s/index.md has no package rows", lang)
	}
	return paths
}

// TestLabelerCoversEveryPackage checks that every directory under pkg/, a flat package or a group such as pkg/online, has a pkg/<name>/** glob in .github/labeler.yml, so a pull request touching it gets a label.
func TestLabelerCoversEveryPackage(t *testing.T) {
	data, err := os.ReadFile(".github/labeler.yml")
	if err != nil {
		t.Fatal(err)
	}
	var rules map[string][]labelerRule
	if err := yaml.Unmarshal(data, &rules); err != nil {
		t.Fatalf(".github/labeler.yml: %v", err)
	}
	globs := map[string]bool{}
	for _, label := range rules {
		for _, rule := range label {
			for _, cf := range rule.ChangedFiles {
				for _, g := range cf.AnyGlobToAnyFile {
					globs[g] = true
				}
			}
		}
	}
	for _, name := range subdirs(t, "pkg") {
		if glob := "pkg/" + name + "/**"; !globs[glob] {
			t.Errorf(".github/labeler.yml has no %s glob: add a pkg/%s label for it", glob, name)
		}
	}
}

// TestPackageCatalogsComplete checks that README.md's package table and docs/en/index.md list every catalogued package, and that docs/en/index.md and docs/zh/index.md list the same import paths, each an existing package.
func TestPackageCatalogsComplete(t *testing.T) {
	readme := readmePackages(t)
	en := indexImportPaths(t, "en")
	inIndex := map[string]bool{}
	for _, p := range en {
		inIndex[p] = true
	}
	for _, pkg := range catalogedPackages(t) {
		if !readme[pkg] {
			t.Errorf("README.md's package table does not list `%s`", pkg)
		}
		if ip := "github.com/nsgo-dev/game/pkg/" + pkg; !inIndex[ip] {
			t.Errorf("docs/en/index.md has no row for %s: add one to docs/en/index.md and docs/zh/index.md", ip)
		}
	}
	for name := range readme {
		if dir := path.Join("pkg", name); name != "game" && (!isDir(dir) || !hasGoFiles(t, dir)) {
			t.Errorf("README.md's package table lists `%s`, which is not a package of the module", name)
		}
	}
	for _, ip := range en {
		dir := strings.TrimPrefix(strings.TrimPrefix(ip, "github.com/nsgo-dev/game"), "/")
		if dir == "" {
			dir = "."
		}
		if !isDir(dir) || !hasGoFiles(t, dir) {
			t.Errorf("docs/en/index.md lists %s, which is not a package of the module", ip)
		}
	}
	if zh := indexImportPaths(t, "zh"); !slices.Equal(en, zh) {
		t.Errorf("docs/en/index.md and docs/zh/index.md list different packages or orders:\nen: %v\nzh: %v", en, zh)
	}
}

// TestNoStdPackageNames checks that no package of the module, other than a main package, takes the name of a standard library package (the last element of its import path), which would shadow it in every file that imports both.
func TestNoStdPackageNames(t *testing.T) {
	out, err := exec.Command("go", "list", "std").Output()
	if err != nil {
		t.Fatalf("go list std: %v", err)
	}
	std := map[string]string{}
	for _, ip := range strings.Fields(string(out)) {
		elems := strings.Split(ip, "/")
		if slices.Contains(elems, "internal") || slices.Contains(elems, "vendor") {
			continue
		}
		std[elems[len(elems)-1]] = ip
	}

	names := map[string][]string{} // package name -> directories
	fset := token.NewFileSet()
	err = filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p == "." {
				return nil
			}
			if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "node_modules" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir // another module
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.PackageClauseOnly)
		if err != nil {
			return err
		}
		pkg := f.Name.Name
		dir := filepath.ToSlash(filepath.Dir(p))
		if pkg != "main" && !slices.Contains(names[pkg], dir) {
			names[pkg] = append(names[pkg], dir)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for pkg, dirs := range names {
		ip, ok := std[pkg]
		if !ok {
			continue
		}
		if _, exempt := stdNameExemptions[pkg]; exempt {
			continue
		}
		slices.Sort(dirs)
		t.Errorf("package %s in %s has the name of the standard library's %s: rename it", pkg, strings.Join(dirs, ", "), ip)
	}
	for pkg, reason := range stdNameExemptions {
		if _, ok := std[pkg]; !ok || len(names[pkg]) == 0 {
			t.Errorf("stdNameExemptions exempts package %s (%s), but no package of the module clashes with the standard library under that name any more: delete the entry", pkg, reason)
		}
	}
}
