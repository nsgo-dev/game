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

func TestVerifyRelease(t *testing.T) {
	released := strings.Replace(sample, "## Unreleased\n\n### Added\n\n- one (#1).\n\n### Fixed\n\n- two (#2).\n\n- three (#3).\n\n", "## Unreleased\n\n", 1)
	for name, c := range map[string]struct {
		changelog string
		fragment  string
		version   string
		ok        bool
	}{
		"released":             {changelog: released, version: "0.1.0", ok: true},
		"tag name":             {changelog: released, version: "v0.1.0", ok: true},
		"fragment left":        {changelog: released, fragment: "### Added\n\n- late (#9).\n", version: "0.1.0"},
		"unreleased not empty": {changelog: sample, version: "0.1.0"},
		"no section":           {changelog: released, version: "0.2.0"},
		"empty section":        {changelog: released + "\n## [0.0.1] - 2025-01-01\n", version: "0.0.1"},
	} {
		root := t.TempDir()
		write(t, root, changelogFile, c.changelog)
		write(t, root, "changelog.d/README.md", "Not a fragment.\n")
		if c.fragment != "" {
			write(t, root, "changelog.d/late.md", c.fragment)
		}
		err := run(root, []string{"verify-release", c.version}, time.Now())
		if c.ok && err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: verify-release succeeded", name)
		}
	}
}

func TestNotesPrintsTheWholeSection(t *testing.T) {
	text := sample + "\n[0.1.0]: https://example.com/0.1.0\n"
	got, err := notes(text, "v0.1.0", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "### Added\n\n- old.\n"; got != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

// TestNotesShortensToWholeSubsections checks that a section over the limit keeps Changed and Removed whole, drops the long Added list as a whole rather than partway through, and links to the section's anchor.
func TestNotesShortensToWholeSubsections(t *testing.T) {
	var added strings.Builder
	for range 500 {
		added.WriteString("- an addition long enough to fill the release body (#1).\n")
	}
	text := "# Changelog\n\n## Unreleased\n\n## [1.2.0] - 2026-12-01\n\nIntro.\n\n### Added\n\n" + added.String() + "\n### Changed\n\n- a change (#2).\n\n### Removed\n\n- a removal (#3).\n\n### Fixed\n\n- a fix (#4).\n"
	link := "https://github.com/nsgo-dev/game/blob/v1.2.0/CHANGELOG.md"
	const limit = 2000
	got, err := notes(text, "1.2.0", limit, link)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got)); n > limit {
		t.Errorf("notes are %d characters, over %d", n, limit)
	}
	for _, want := range []string{
		"Intro.\n\n### Added\n\n_500 entries, listed in the [full notes](" + link + "#120---2026-12-01)._\n",
		"### Changed\n\n- a change (#2).\n",
		"### Removed\n\n- a removal (#3).\n",
		"### Fixed\n\n- a fix (#4).\n",
		"the full notes are in [CHANGELOG.md](" + link + "#120---2026-12-01).",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notes lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "- an addition") {
		t.Errorf("notes kept part of Added:\n%s", got)
	}
	if _, err := notes(text, "1.2.0", limit, ""); err == nil {
		t.Error("shortening without -link succeeded")
	}
}

// TestRepositoryNotesFitGitHub checks the repository's newest release against the limit release.yml passes.
func TestRepositoryNotesFitGitHub(t *testing.T) {
	text := read(t, "../../CHANGELOG.md")
	got, err := notes(text, "0.1.0", 120000, "https://github.com/nsgo-dev/game/blob/v0.1.0/CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got)); n > 120000 {
		t.Fatalf("notes are %d characters", n)
	}
	for _, want := range []string{"### Changed\n\n- ", "### Removed\n\n- ", "### Security\n\n- "} {
		if !strings.Contains(got, want) {
			t.Errorf("notes lack %q", want)
		}
	}
}
