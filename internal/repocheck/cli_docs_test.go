package repocheck_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	nsgoRequirePattern = regexp.MustCompile(`(?m)^\s*github\.com/nsgo-dev/nsgo\s+(v\d+\.\d+\.\d+\S*)\s*$`)
	semverCorePattern  = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)
)

// isNSGoAtMost034 reports whether version is at or below NSGo v0.3.4, where the built-in `nsgo game` command hard-codes the legacy github.com/nsgo-dev/game/packages/game module path.
func isNSGoAtMost034(version string) bool {
	m := semverCorePattern.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if major != 0 {
		return major < 0
	}
	if minor != 3 {
		return minor < 3
	}
	return patch <= 4
}

// templatesPinLegacyNSGo reports whether any template's go.mod.tmpl pins github.com/nsgo-dev/nsgo at v0.3.4 or older.
func templatesPinLegacyNSGo(root string) (bool, []string, error) {
	var pins []string
	legacy := false
	err := filepath.WalkDir(filepath.Join(root, "template"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "go.mod.tmpl" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		m := nsgoRequirePattern.FindSubmatch(data)
		if m == nil {
			return fmt.Errorf("%s: missing github.com/nsgo-dev/nsgo requirement", filepath.ToSlash(p))
		}
		ver := string(m[1])
		pins = append(pins, fmt.Sprintf("%s=%s", filepath.ToSlash(p), ver))
		if isNSGoAtMost034(ver) {
			legacy = true
		}
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	if len(pins) == 0 {
		return false, nil, fmt.Errorf("found no template/**/go.mod.tmpl under %s", filepath.ToSlash(root))
	}
	return legacy, pins, nil
}

// cliDocNoteSections lists the only markdown sections permitted to mention "nsgo game" in prose (never in fenced code blocks) while templates pin NSGo v0.3.4.
var cliDocNoteSections = map[string]map[string]bool{
	"docs/en/cli.md": {
		"## Install":     true,
		"### completion": true,
	},
	"docs/zh/cli.md": {
		"## 安装":          true,
		"### completion": true,
	},
}

const newcomerNewCommand = "go run github.com/nsgo-dev/game/cmd/game@latest new"

var newcomerEntryPoints = []string{
	"README.md",
	"docs/en/getting-started.md",
	"docs/zh/getting-started.md",
	"docs/en/cli.md",
	"docs/zh/cli.md",
	"docs/en/templates.md",
	"docs/zh/templates.md",
	"skills/nsgo-game/SKILL.md",
	"apps/website/src/pages/Home.tsx",
}

// checkCLIDocs scans README, docs/en, docs/zh, skills/nsgo-game, and apps/website/src for "nsgo game" commands and verifies every newcomer entry point leads with `go run github.com/nsgo-dev/game/cmd/game@latest new`.
func checkCLIDocs(root string) ([]string, error) {
	var files []string
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); err == nil {
		files = append(files, readme)
	}
	walkRoots := []struct {
		dir  string
		exts []string
	}{
		{filepath.Join(root, "docs", "en"), []string{".md"}},
		{filepath.Join(root, "docs", "zh"), []string{".md"}},
		{filepath.Join(root, "skills", "nsgo-game"), []string{".md"}},
		{filepath.Join(root, "apps", "website", "src"), []string{".ts", ".tsx"}},
	}
	for _, wr := range walkRoots {
		if _, err := os.Stat(wr.dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(wr.dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			for _, ext := range wr.exts {
				if strings.HasSuffix(d.Name(), ext) {
					files = append(files, p)
					break
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var problems []string
	for _, p := range files {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		isMD := strings.HasSuffix(rel, ".md")
		allowedSections := cliDocNoteSections[rel]
		inFence := false
		section := ""
		for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if isMD && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
				inFence = !inFence
				continue
			}
			if isMD && !inFence && (strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")) {
				section = strings.TrimSpace(line)
			}
			if !strings.Contains(line, "nsgo game") {
				continue
			}
			if inFence {
				problems = append(problems, fmt.Sprintf(`%s:%d: fenced code block contains "nsgo game"; use "go run github.com/nsgo-dev/game/cmd/game@latest" or "go tool game" while templates pin NSGo v0.3.4`, rel, i+1))
				continue
			}
			if allowedSections != nil && allowedSections[section] {
				continue
			}
			problems = append(problems, fmt.Sprintf(`%s:%d: contains "nsgo game" (section %q); use "go run github.com/nsgo-dev/game/cmd/game@latest" or "go tool game" while templates pin NSGo v0.3.4`, rel, i+1, section))
		}
	}

	for _, rel := range newcomerEntryPoints {
		p := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				problems = append(problems, fmt.Sprintf("%s: missing expected newcomer entry point", rel))
				continue
			}
			return nil, err
		}
		if !strings.Contains(string(data), newcomerNewCommand) {
			problems = append(problems, fmt.Sprintf("%s: missing newcomer command %q", rel, newcomerNewCommand))
		}
	}

	return problems, nil
}

// TestCLIDocsAvoidLegacyNSGoGame checks that while project templates pin NSGo v0.3.4 (whose built-in `nsgo game` hard-codes the pre-migration github.com/nsgo-dev/game/packages/game module), docs, README, website and the agent skill lead with `go run github.com/nsgo-dev/game/cmd/game@latest` and `go tool game` rather than `nsgo game`.
func TestCLIDocsAvoidLegacyNSGoGame(t *testing.T) {
	legacy, pins, err := templatesPinLegacyNSGo(".")
	if err != nil {
		t.Fatal(err)
	}
	if !legacy {
		t.Skipf("templates no longer pin NSGo <= v0.3.4 (%v); remove or invert this guard when switching docs back to `nsgo game`", pins)
	}
	problems, err := checkCLIDocs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestIsNSGoAtMost034(t *testing.T) {
	cases := map[string]bool{
		"v0.3.3":                true,
		"v0.3.4":                true,
		"v0.3.5":                false,
		"v0.4.0":                false,
		"v1.0.0":                false,
		"v0.3.4-0.20261125-abc": true,
		"v0.3.5-0.20261128-abc": false,
	}
	for ver, want := range cases {
		if got := isNSGoAtMost034(ver); got != want {
			t.Errorf("isNSGoAtMost034(%q) = %v, want %v", ver, got, want)
		}
	}
}

func TestCheckCLIDocsDetectsDrift(t *testing.T) {
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

	makeClean := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		for _, rel := range newcomerEntryPoints {
			write(dir, rel, newcomerNewCommand+" mygame\n")
		}
		write(dir, "docs/en/cli.md", "# CLI\n\n## Install\n\n```sh\n"+newcomerNewCommand+" mygame\n```\n\nRunning as `nsgo game` requires NSGo newer than v0.3.4.\n\n### check\n\n```sh\ngo tool game check\n```\n")
		write(dir, "docs/zh/cli.md", "# 命令行\n\n## 安装\n\n```sh\n"+newcomerNewCommand+" mygame\n```\n\n以 `nsgo game` 运行需要高于 v0.3.4 的 NSGo。\n")
		return dir
	}

	t.Run("clean tree passes", func(t *testing.T) {
		dir := makeClean(t)
		problems, err := checkCLIDocs(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %v", problems)
		}
	})

	t.Run("nsgo game in getting-started fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "docs/en/getting-started.md", newcomerNewCommand+" mygame\nnsgo game check\n")
		problems, err := checkCLIDocs(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "docs/en/getting-started.md:2:") {
			t.Fatalf("problems = %v, want getting-started.md:2 error", problems)
		}
	})

	t.Run("nsgo game in cli.md Install code block fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "docs/en/cli.md", "# CLI\n\n## Install\n\n```sh\n"+newcomerNewCommand+" mygame\nnsgo game help\n```\n")
		problems, err := checkCLIDocs(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "fenced code block") {
			t.Fatalf("problems = %v, want fenced code block error", problems)
		}
	})

	t.Run("nsgo game in non-note section of cli.md fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "docs/en/cli.md", "# CLI\n\n## Install\n\n"+newcomerNewCommand+" mygame\n\n### check\n\nRun `nsgo game check`.\n")
		problems, err := checkCLIDocs(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "### check") {
			t.Fatalf("problems = %v, want ### check error", problems)
		}
	})

	t.Run("nsgo game in skill or website fails", func(t *testing.T) {
		dir := makeClean(t)
		write(dir, "skills/nsgo-game/SKILL.md", newcomerNewCommand+" mygame\nnsgo game dev\n")
		write(dir, "apps/website/src/pages/Home.tsx", "const NEW_GAME = '"+newcomerNewCommand+" mygame'\nconst ALT = 'nsgo game dev'\n")
		problems, err := checkCLIDocs(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 2 {
			t.Fatalf("got %d problems (%v), want 2", len(problems), problems)
		}
	})
}
