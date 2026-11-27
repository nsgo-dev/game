// Command changelog keeps CHANGELOG.md's Unreleased section in fragments: each pull request adds a file to changelog.d instead of a line in the section, so pull requests do not conflict with each other there, and a release folds the fragments into CHANGELOG.md.
//
//	go run ./tools/changelog check
//	go run ./tools/changelog collect
//	go run ./tools/changelog release [-date YYYY-MM-DD] X.Y.Z
//
// check validates the fragments; collect moves their bullets to the end of the matching "### Kind" subsections of Unreleased and deletes them; release collects them and turns Unreleased into the version's section above a fresh one. It works on the repository the current directory is in.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	changelogFile = "CHANGELOG.md"
	fragmentDir   = "changelog.d"
	// readme explains the directory to contributors; it is not a fragment and keeps the directory in Git when no fragment is left.
	readme            = "README.md"
	unreleasedHeading = "## Unreleased"
)

// kinds are Keep a Changelog's types, in the order CHANGELOG.md lists their subsections.
var kinds = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

var (
	fragmentName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.md$`)
	versionRE    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

// fragment is one file of changelog.d: its bullets by kind, each a whole line starting with "- ".
type fragment struct {
	name    string
	bullets [][]string
}

func main() {
	root, err := repoRoot()
	if err == nil {
		err = run(root, os.Args[1:], time.Now())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// repoRoot is the nearest directory up from the current one that holds CHANGELOG.md.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, changelogFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s in this directory or above it", changelogFile)
		}
		dir = parent
	}
}

func usage() error {
	return errors.New("usage: changelog check | collect | release [-date YYYY-MM-DD] X.Y.Z")
}

func run(root string, args []string, now time.Time) error {
	if len(args) == 0 {
		return usage()
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "check":
		if len(rest) != 0 {
			return usage()
		}
		frags, err := readFragments(root)
		if err != nil {
			return err
		}
		fmt.Printf("%d changelog fragments\n", len(frags))
		return nil
	case "collect":
		if len(rest) != 0 {
			return usage()
		}
		n, err := collect(root)
		if err != nil {
			return err
		}
		fmt.Printf("collected %d fragments into %s\n", n, changelogFile)
		return nil
	case "release":
		fs := flag.NewFlagSet("release", flag.ContinueOnError)
		date := fs.String("date", now.UTC().Format(time.DateOnly), "the release's date")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return usage()
		}
		return release(root, fs.Arg(0), *date)
	}
	return usage()
}

func kindIndex(name string) int {
	return slices.IndexFunc(kinds, func(k string) bool { return strings.EqualFold(k, name) })
}

// parseFragment reads a fragment: "### Kind" headings, each followed by one-line bullets.
func parseFragment(name string, data []byte) (fragment, error) {
	f := fragment{name: name, bullets: make([][]string, len(kinds))}
	kind, count := -1, 0
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, " \t\r")
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			title, ok := strings.CutPrefix(line, "### ")
			kind = kindIndex(strings.TrimSpace(title))
			if !ok || kind < 0 {
				return f, fmt.Errorf("%s:%d: a heading is ### and one of %s", name, i+1, strings.Join(kinds, ", "))
			}
		case strings.HasPrefix(line, "- ") && strings.TrimSpace(line[2:]) != "":
			if kind < 0 {
				return f, fmt.Errorf("%s:%d: a bullet goes under a ### heading naming its type", name, i+1)
			}
			f.bullets[kind] = append(f.bullets[kind], line)
			count++
		default:
			return f, fmt.Errorf("%s:%d: a fragment holds only ### headings and one-line \"- \" bullets", name, i+1)
		}
	}
	if count == 0 {
		return f, fmt.Errorf("%s: no bullets", name)
	}
	return f, nil
}

// readFragments reads changelog.d in name order, reporting every invalid file at once.
func readFragments(root string) ([]fragment, error) {
	entries, err := os.ReadDir(filepath.Join(root, fragmentDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var frags []fragment
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if name == readme {
			continue
		}
		if e.IsDir() || !fragmentName.MatchString(name) {
			errs = append(errs, fmt.Errorf("%s/%s: a fragment is a file named in kebab case ending in .md, such as keep-keys-held-through-reset.md", fragmentDir, name))
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, fragmentDir, name))
		if err != nil {
			return nil, err
		}
		f, err := parseFragment(fragmentDir+"/"+name, data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		frags = append(frags, f)
	}
	return frags, errors.Join(errs...)
}

// unreleased splits a changelog around its Unreleased section's body: what comes before and including the heading, the body's lines, and the next section on.
func unreleased(text string) (head string, body []string, tail string, err error) {
	lines := strings.SplitAfter(text, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t\r\n") == unreleasedHeading })
	if start < 0 {
		return "", nil, "", fmt.Errorf("%s has no %q heading", changelogFile, unreleasedHeading)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	for _, l := range lines[start+1 : end] {
		body = append(body, strings.TrimRight(l, "\r\n"))
	}
	return strings.Join(lines[:start+1], ""), body, strings.Join(lines[end:], ""), nil
}

// subsections splits Unreleased's body into its "### Kind" subsections' bullet lines, keeping every other line of a subsection as it is. A line before the first subsection, or a subsection that names no kind, is refused, as a fold could not keep it in place.
func subsections(body []string) ([][]string, error) {
	subs := make([][]string, len(kinds))
	kind := -1
	for _, l := range body {
		if title, ok := strings.CutPrefix(l, "### "); ok {
			kind = kindIndex(strings.TrimSpace(title))
			if kind < 0 {
				return nil, fmt.Errorf("%s's Unreleased has a %q subsection; use one of %s", changelogFile, l, strings.Join(kinds, ", "))
			}
			continue
		}
		if kind < 0 {
			if strings.TrimSpace(l) != "" {
				return nil, fmt.Errorf("%s's Unreleased has %q before its first ### subsection", changelogFile, l)
			}
			continue
		}
		subs[kind] = append(subs[kind], l)
	}
	for k := range subs {
		subs[k] = trimBlank(subs[k])
	}
	return subs, nil
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// render writes a heading, its subsections in kind order with a blank line around each, and the rest of the file.
func render(head string, subs [][]string, tail string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(head, "\r\n"))
	b.WriteString("\n")
	for k, lines := range subs {
		if len(lines) == 0 {
			continue
		}
		b.WriteString("\n### " + kinds[k] + "\n\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	if tail != "" {
		b.WriteString("\n")
	}
	b.WriteString(tail)
	return b.String()
}

// fold appends the fragments' bullets to the end of their kinds' subsections of Unreleased, by fragment name, creating a subsection a kind lacks.
func fold(text string, frags []fragment) (string, error) {
	head, body, tail, err := unreleased(text)
	if err != nil {
		return "", err
	}
	subs, err := subsections(body)
	if err != nil {
		return "", err
	}
	for k := range kinds {
		for _, f := range frags {
			subs[k] = append(subs[k], f.bullets[k]...)
		}
	}
	return render(head, subs, tail), nil
}

// collect folds changelog.d into CHANGELOG.md, writing the changelog before deleting any fragment.
func collect(root string) (int, error) {
	frags, err := readFragments(root)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(root, changelogFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	text, err := fold(string(data), frags)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return 0, err
	}
	for _, f := range frags {
		if err := os.Remove(filepath.Join(root, f.name)); err != nil {
			return 0, err
		}
	}
	return len(frags), nil
}

// cut turns Unreleased into the version's section, "## [X.Y.Z] - date" as release.yml reads it, above a fresh, empty Unreleased.
func cut(text, version, date string) (string, error) {
	if !versionRE.MatchString(version) {
		return "", fmt.Errorf("version %q is not X.Y.Z or X.Y.Z-suffix", version)
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return "", fmt.Errorf("date %q is not YYYY-MM-DD", date)
	}
	heading := "## [" + version + "]"
	for _, l := range strings.Split(text, "\n") {
		if l == heading || strings.HasPrefix(l, heading+" ") {
			return "", fmt.Errorf("%s already has a %s section", changelogFile, heading)
		}
	}
	head, body, tail, err := unreleased(text)
	if err != nil {
		return "", err
	}
	subs, err := subsections(body)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(subs, func(lines []string) bool { return len(lines) > 0 }) {
		return "", fmt.Errorf("%s's Unreleased section is empty: nothing to release", changelogFile)
	}
	return render(head, make([][]string, len(kinds)), render(heading+" - "+date, subs, tail)), nil
}

func release(root, version, date string) error {
	path := filepath.Join(root, changelogFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Cut a copy first, so a bad version or an existing section leaves the fragments in place.
	frags, err := readFragments(root)
	if err != nil {
		return err
	}
	folded, err := fold(string(data), frags)
	if err != nil {
		return err
	}
	if _, err := cut(folded, version, date); err != nil {
		return err
	}
	n, err := collect(root)
	if err != nil {
		return err
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return err
	}
	text, err := cut(string(data), version, date)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("collected %d fragments and wrote the %s section of %s; review it, then merge it and tag v%s\n", n, version, changelogFile, version)
	return nil
}
