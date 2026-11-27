package repocheck_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// onlinePackageRow is one row of the "without Google Play Services" table in docs/<lang>/shipping-china.md.
type onlinePackageRow struct {
	pkg      string // such as pkg/online/accounts
	status   string // normalized: "works", "partial", or "unavailable"
	behavior string // non-empty behavior and fallback note
}

var (
	shippingChinaRowPattern = regexp.MustCompile("^\\| `(pkg/online/[^`]+)` \\| ([^|]+?) \\| (.*?) ?\\|$")
	chinaOnlineStatusWords  = map[string]map[string]string{
		"en": {
			"Works":       "works",
			"Partial":     "partial",
			"Unavailable": "unavailable",
		},
		"zh": {
			"可用":   "works",
			"部分可用": "partial",
			"不可用":  "unavailable",
		},
	}
)

// onlineHelperSubpackages lists companion subpackages under pkg/online whose Google-free behavior is documented in their parent package's row of docs/<lang>/shipping-china.md. Any new package under pkg/online/ must either get its own row in both EN and ZH shipping-china.md or be added here with its parent package.
var onlineHelperSubpackages = map[string]string{
	"pkg/online/accounts/pgstore":     "pkg/online/accounts",
	"pkg/online/accounts/sqlitestore": "pkg/online/accounts",
	"pkg/online/accounts/sqlstore":    "pkg/online/accounts",
	"pkg/online/accounts/storetest":   "pkg/online/accounts",
	"pkg/online/accounts/token":       "pkg/online/accounts",
	"pkg/online/cloudsave/sqlstore":   "pkg/online/cloudsave",
	"pkg/online/cloudsave/storetest":  "pkg/online/cloudsave",
	"pkg/online/liveops/crashreport":  "pkg/online/liveops",
	"pkg/online/netplay/wire":         "pkg/online/netplay",
}

func parseShippingChinaOnlineTable(lang string, content string) ([]onlinePackageRow, []string) {
	var rows []onlinePackageRow
	var problems []string
	words := chinaOnlineStatusWords[lang]
	for i, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "| `pkg/online/") {
			continue
		}
		m := shippingChinaRowPattern.FindStringSubmatch(line)
		if m == nil {
			problems = append(problems, fmt.Sprintf("docs/%s/shipping-china.md:%d: malformed online package row %q", lang, i+1, line))
			continue
		}
		status, ok := words[strings.TrimSpace(m[2])]
		if !ok {
			problems = append(problems, fmt.Sprintf("docs/%s/shipping-china.md:%d: %s has unknown status %q", lang, i+1, m[1], strings.TrimSpace(m[2])))
		}
		behavior := strings.TrimSpace(m[3])
		if behavior == "" {
			problems = append(problems, fmt.Sprintf("docs/%s/shipping-china.md:%d: %s has empty behavior description", lang, i+1, m[1]))
		}
		rows = append(rows, onlinePackageRow{
			pkg:      m[1],
			status:   status,
			behavior: behavior,
		})
	}
	if len(rows) == 0 {
		problems = append(problems, fmt.Sprintf("docs/%s/shipping-china.md: missing pkg/online/* table rows", lang))
	}
	return rows, problems
}

func checkShippingChinaDocs(root string, onlinePkgs []string) ([]string, error) {
	var problems []string
	parsed := map[string][]onlinePackageRow{}
	for _, lang := range []string{"en", "zh"} {
		p := filepath.Join(root, "docs", lang, "shipping-china.md")
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rows, errs := parseShippingChinaOnlineTable(lang, string(data))
		problems = append(problems, errs...)
		parsed[lang] = rows
		for i := 1; i < len(rows); i++ {
			if rows[i-1].pkg >= rows[i].pkg {
				problems = append(problems, fmt.Sprintf("docs/%s/shipping-china.md: %s should come after %s (keep rows sorted and unique)", lang, rows[i-1].pkg, rows[i].pkg))
			}
		}
	}

	// Verify EN and ZH list the exact same packages with the exact same normalized statuses.
	enRows, zhRows := parsed["en"], parsed["zh"]
	if len(enRows) != len(zhRows) {
		problems = append(problems, fmt.Sprintf("docs/en/shipping-china.md lists %d online packages, docs/zh/shipping-china.md lists %d", len(enRows), len(zhRows)))
	} else {
		for i := range enRows {
			if enRows[i].pkg != zhRows[i].pkg || enRows[i].status != zhRows[i].status {
				problems = append(problems, fmt.Sprintf("shipping-china.md mismatch at row %d: en=(%s, %s), zh=(%s, %s)", i+1, enRows[i].pkg, enRows[i].status, zhRows[i].pkg, zhRows[i].status))
			}
		}
	}

	// Build expected top-level / standalone pkg/online rows from onlinePkgs minus helper subpackages.
	actualSet := map[string]bool{}
	for _, pkg := range onlinePkgs {
		actualSet[pkg] = true
	}
	for sub, parent := range onlineHelperSubpackages {
		if !actualSet[sub] {
			problems = append(problems, fmt.Sprintf("onlineHelperSubpackages lists %s, which does not exist in the module", sub))
		}
		if !actualSet[parent] {
			problems = append(problems, fmt.Sprintf("onlineHelperSubpackages maps %s to missing parent %s", sub, parent))
		}
	}

	documented := map[string]bool{}
	for _, row := range enRows {
		documented[row.pkg] = true
		if !actualSet[row.pkg] {
			problems = append(problems, fmt.Sprintf("docs/en/shipping-china.md lists %s, which is not a package under pkg/online/", row.pkg))
		}
	}
	for _, pkg := range onlinePkgs {
		if _, isHelper := onlineHelperSubpackages[pkg]; isHelper {
			continue
		}
		if !documented[pkg] {
			problems = append(problems, fmt.Sprintf("package %s is missing from docs/en/shipping-china.md and docs/zh/shipping-china.md; document its behavior without Google Play Services or classify it in onlineHelperSubpackages", pkg))
		}
	}

	// Verify shipping.md and liveops.md link to shipping-china.md in both languages.
	for _, lang := range []string{"en", "zh"} {
		for _, page := range []string{"shipping.md", "liveops.md"} {
			rel := filepath.Join("docs", lang, page)
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				return nil, err
			}
			if !strings.Contains(string(data), "(shipping-china.md)") {
				problems = append(problems, fmt.Sprintf("%s: missing link to shipping-china.md", filepath.ToSlash(rel)))
			}
		}
	}

	return problems, nil
}

// TestShippingChinaOnlinePackages verifies that docs/en/shipping-china.md and docs/zh/shipping-china.md document every pkg/online package's behavior without Google Play Services, agree with each other, and are linked from shipping.md and liveops.md.
func TestShippingChinaOnlinePackages(t *testing.T) {
	pkgs := modulePackages(t)
	var onlinePkgs []string
	for pkg := range pkgs {
		if rel, ok := strings.CutPrefix(pkg, "game/"); ok && strings.HasPrefix(rel, "pkg/online/") {
			onlinePkgs = append(onlinePkgs, rel)
		}
	}
	slices.Sort(onlinePkgs)
	if len(onlinePkgs) == 0 {
		t.Fatal("found no packages under pkg/online/; scan is broken")
	}
	problems, err := checkShippingChinaDocs(".", onlinePkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestShippingChinaOnlineTableDetectsDrift(t *testing.T) {
	write := func(root, rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	allPkgs := []string{
		"pkg/online/accounts",
		"pkg/online/accounts/pgstore",
		"pkg/online/accounts/sqlitestore",
		"pkg/online/accounts/sqlstore",
		"pkg/online/accounts/storetest",
		"pkg/online/accounts/token",
		"pkg/online/cloudsave",
		"pkg/online/cloudsave/sqlstore",
		"pkg/online/cloudsave/storetest",
		"pkg/online/liveops",
		"pkg/online/liveops/crashreport",
		"pkg/online/netplay",
		"pkg/online/netplay/wire",
	}

	makeClean := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		write(dir, "docs/en/shipping-china.md", strings.Join([]string{
			"# Releasing in Mainland China",
			"",
			"| Package | Without Google services | Behavior |",
			"| --- | --- | --- |",
			"| `pkg/online/accounts` | Works | Self-hosted. |",
			"| `pkg/online/cloudsave` | Works | Self-hosted. |",
			"| `pkg/online/liveops` | Partial | Mixed. |",
			"| `pkg/online/netplay` | Works | Pure Go. |",
			"",
		}, "\n"))
		write(dir, "docs/zh/shipping-china.md", strings.Join([]string{
			"# 在中国大陆发布",
			"",
			"| 包 | 无 Google 服务状态 | 行为 |",
			"| --- | --- | --- |",
			"| `pkg/online/accounts` | 可用 | 自部署。 |",
			"| `pkg/online/cloudsave` | 可用 | 自部署。 |",
			"| `pkg/online/liveops` | 部分可用 | 混合。 |",
			"| `pkg/online/netplay` | 可用 | 纯 Go。 |",
			"",
		}, "\n"))
		write(dir, "docs/en/shipping.md", "See [China](shipping-china.md).\n")
		write(dir, "docs/zh/shipping.md", "见[中国大陆](shipping-china.md)。\n")
		write(dir, "docs/en/liveops.md", "See [China](shipping-china.md).\n")
		write(dir, "docs/zh/liveops.md", "见[中国大陆](shipping-china.md)。\n")
		return dir
	}

	t.Run("clean tree passes", func(t *testing.T) {
		dir := makeClean(t)
		problems, err := checkShippingChinaDocs(dir, allPkgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %v", problems)
		}
	})

	t.Run("missing online package fails", func(t *testing.T) {
		dir := makeClean(t)
		extra := append(slices.Clone(allPkgs), "pkg/online/newsvc")
		problems, err := checkShippingChinaDocs(dir, extra)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "pkg/online/newsvc") {
			t.Fatalf("problems = %v, want missing pkg/online/newsvc error", problems)
		}
	})

	t.Run("en/zh status mismatch fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "docs/zh/shipping-china.md", strings.Join([]string{
			"# 在中国大陆发布",
			"",
			"| `pkg/online/accounts` | 不可用 | 自部署。 |",
			"| `pkg/online/cloudsave` | 可用 | 自部署。 |",
			"| `pkg/online/liveops` | 部分可用 | 混合。 |",
			"| `pkg/online/netplay` | 可用 | 纯 Go。 |",
			"",
		}, "\n"))
		problems, err := checkShippingChinaDocs(dir, allPkgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "mismatch at row 1") {
			t.Fatalf("problems = %v, want mismatch at row 1", problems)
		}
	})

	t.Run("missing link from liveops.md fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "docs/en/liveops.md", "# Live operations\n")
		problems, err := checkShippingChinaDocs(dir, allPkgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "docs/en/liveops.md") {
			t.Fatalf("problems = %v, want docs/en/liveops.md link error", problems)
		}
	})
}
