package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `# Changelog

Intro.

## Unreleased

### Added

- one (#1).

### Fixed

- two (#2).

- three (#3).

## [0.1.0] - 2026-01-01

### Added

- old.
`

func write(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestFoldWithoutFragmentsKeepsTheFile checks that folding nothing rewrites the sample and the repository's own changelog byte for byte, so collect never reformats what is already there.
func TestFoldWithoutFragmentsKeepsTheFile(t *testing.T) {
	for name, text := range map[string]string{"sample": sample, "CHANGELOG.md": read(t, "../../CHANGELOG.md")} {
		got, err := fold(text, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != text {
			t.Errorf("%s changed:\n%s", name, got)
		}
	}
}

func TestCollectAppendsToSubsections(t *testing.T) {
	root := t.TempDir()
	write(t, root, changelogFile, sample)
	write(t, root, "changelog.d/README.md", "Not a fragment.\n")
	write(t, root, "changelog.d/b-change.md", "### Fixed\n\n- four (#4).\n\n### Changed\n\n- five (#5).\n")
	write(t, root, "changelog.d/a-change.md", "### Fixed\n\n- six (#6).\n")
	if err := run(root, []string{"collect"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sample, "- three (#3).\n", "- three (#3).\n- six (#6).\n- four (#4).\n", 1)
	want = strings.Replace(want, "### Fixed\n\n- two", "### Changed\n\n- five (#5).\n\n### Fixed\n\n- two", 1)
	if got := read(t, filepath.Join(root, changelogFile)); got != want {
		t.Fatalf("collect wrote:\n%s\nwant:\n%s", got, want)
	}
	entries, _ := os.ReadDir(filepath.Join(root, fragmentDir))
	if len(entries) != 1 || entries[0].Name() != readme {
		t.Fatalf("changelog.d kept %v, want only its README", entries)
	}
}

func TestReleaseCutsTheVersion(t *testing.T) {
	root := t.TempDir()
	write(t, root, changelogFile, sample)
	write(t, root, "changelog.d/new.md", "### Added\n\n- seven (#7).\n")
	if err := run(root, []string{"release", "-date", "2026-11-23", "0.2.0"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, changelogFile))
	want := strings.Replace(sample, "## Unreleased\n\n### Added\n\n- one (#1).\n", "## Unreleased\n\n## [0.2.0] - 2026-11-23\n\n### Added\n\n- one (#1).\n- seven (#7).\n", 1)
	if got != want {
		t.Fatalf("release wrote:\n%s\nwant:\n%s", got, want)
	}
	if err := run(root, []string{"release", "-date", "2026-11-24", "0.2.0"}, time.Now()); err == nil {
		t.Fatal("releasing 0.2.0 twice succeeded")
	}
}

func TestCheckRefusesBadFragments(t *testing.T) {
	for name, text := range map[string]string{
		"no-heading.md":  "- bullet\n",
		"bad-kind.md":    "### Improved\n\n- bullet\n",
		"prose.md":       "### Added\n\nSome prose.\n",
		"empty.md":       "### Added\n",
		"Upper_Case.md":  "### Added\n\n- bullet\n",
		"sub-heading.md": "## Added\n\n- bullet\n",
	} {
		root := t.TempDir()
		write(t, root, changelogFile, sample)
		write(t, root, filepath.Join(fragmentDir, name), text)
		if err := run(root, []string{"check"}, time.Now()); err == nil {
			t.Errorf("check accepted %s", name)
		}
	}
}
