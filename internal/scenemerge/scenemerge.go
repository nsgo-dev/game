// Package scenemerge inspects and configures the nsgo-scene Git merge driver
// shared by the game CLI and the scene editor.
package scenemerge

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// DriverName is the gitattributes merge driver identifier for NSGo scene files.
	DriverName = "nsgo-scene"
	// DriverTitle is the human-readable description written to Git config.
	DriverTitle = "NSGo scene merge driver"
	// DriverCommand is the command Git invokes with ancestor (%O), current (%A), other (%B), and path (%P).
	DriverCommand = "go tool game merge %O %A %B %P"
	// defaultLineEndingHeader sets LF line endings for text files in .gitattributes so canonical YAML checks and project indexes agree across operating systems.
	defaultLineEndingHeader = "# Keep text files on LF line endings across operating systems so canonical YAML\n# checks and project indexes agree regardless of Git's core.autocrlf setting.\n* text=auto eol=lf\n"
	// defaultBinaryHeader introduces the binary media rules in .gitattributes.
	defaultBinaryHeader = "# Keep binary media untouched by line-ending conversion.\n"
	// defaultSceneMergeHeader introduces the scene merge driver rules in .gitattributes.
	defaultSceneMergeHeader = "# Merge NSGo scene files object by object instead of line by line.\n# Enable the driver once per clone with: go tool game merge -setup\n"
)

// BinaryMediaPatterns are the binary media file extensions the engine loads,
// marked binary in .gitattributes so Git never alters their bytes.
var BinaryMediaPatterns = []string{
	"*.png",
	"*.jpg",
	"*.jpeg",
	"*.webp",
	"*.gif",
	"*.bmp",
	"*.ktx2",
	"*.dds",
	"*.hdr",
	"*.pic",
	"*.wav",
	"*.ogg",
	"*.mp3",
	"*.m4a",
	"*.aac",
	"*.flac",
	"*.opus",
	"*.mp4",
	"*.webm",
	"*.ttf",
	"*.otf",
	"*.woff",
	"*.woff2",
	"*.glb",
	"*.bin",
}

// GitAttributesContent is the default .gitattributes written by templates and game merge -setup.
var GitAttributesContent = buildDefaultGitAttributes()

func buildDefaultGitAttributes() string {
	var b strings.Builder
	b.WriteString(defaultLineEndingHeader)
	b.WriteByte('\n')
	b.WriteString(defaultBinaryHeader)
	for _, pat := range BinaryMediaPatterns {
		b.WriteString(pat)
		b.WriteString(" binary\n")
	}
	b.WriteByte('\n')
	b.WriteString(defaultSceneMergeHeader)
	b.WriteString("*.scene.yaml merge=" + DriverName + "\n")
	b.WriteString("*.scene.yml merge=" + DriverName + "\n")
	return b.String()
}

// GitRunner executes git with args in dir and returns its trimmed combined output.
type GitRunner func(dir string, args ...string) (string, error)

// DriverStatus reports how the nsgo-scene Git merge driver is configured for a directory.
type DriverStatus struct {
	// InRepo is true when dir lies inside a Git work tree.
	InRepo bool
	// RepoRoot is the top-level directory reported by git rev-parse --show-toplevel.
	RepoRoot string
	// AttributesConfigured is true when .gitattributes (or Git's effective attributes) routes files to merge=nsgo-scene.
	AttributesConfigured bool
	// AttributesPath is the .gitattributes file that names merge=nsgo-scene, if found on disk.
	AttributesPath string
	// DriverConfigured is true when Git config defines a non-empty merge.nsgo-scene.driver.
	DriverConfigured bool
	// DriverCommand is the configured value of merge.nsgo-scene.driver.
	DriverCommand string
}

// NeedsSetup reports whether the directory is in a Git repository whose attributes
// name merge=nsgo-scene without a configured merge.nsgo-scene.driver.
func (s DriverStatus) NeedsSetup() bool {
	return s.InRepo && s.AttributesConfigured && !s.DriverConfigured
}

func defaultGitRunner(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runnerOrDefault(runGit GitRunner) GitRunner {
	if runGit != nil {
		return runGit
	}
	return defaultGitRunner
}

// RepoRoot returns the top-level directory of the Git repository containing dir, or empty if dir is not in a Git work tree.
func RepoRoot(dir string, runGit GitRunner) (string, error) {
	run := runnerOrDefault(runGit)
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Check inspects dir's Git repository, .gitattributes chain, and effective Git config
// for the nsgo-scene merge driver.
func Check(dir string, runGit GitRunner) (DriverStatus, error) {
	run := runnerOrDefault(runGit)
	root, err := RepoRoot(dir, run)
	if err != nil || root == "" {
		return DriverStatus{}, nil
	}
	status := DriverStatus{
		InRepo:   true,
		RepoRoot: root,
	}
	status.AttributesConfigured, status.AttributesPath = detectGitAttributes(dir, root, run)

	if out, err := run(dir, "config", "--get", "merge."+DriverName+".driver"); err == nil {
		if cmd := strings.TrimSpace(out); cmd != "" {
			status.DriverConfigured = true
			status.DriverCommand = cmd
		}
	}
	return status, nil
}

// ConfigureDriver writes merge.nsgo-scene.name and merge.nsgo-scene.driver
// into the local Git repository containing dir.
func ConfigureDriver(dir string, runGit GitRunner) error {
	run := runnerOrDefault(runGit)
	if out, err := run(dir, "rev-parse", "--show-toplevel"); err != nil {
		if out != "" {
			return fmt.Errorf("git rev-parse in %s: %w (%s); run git init first", dir, err, out)
		}
		return fmt.Errorf("git rev-parse in %s: %w; run git init first", dir, err)
	}
	for _, kv := range [][2]string{
		{"merge." + DriverName + ".name", DriverTitle},
		{"merge." + DriverName + ".driver", DriverCommand},
	} {
		if out, err := run(dir, "config", kv[0], kv[1]); err != nil {
			if out != "" {
				return fmt.Errorf("git config %s: %w (%s)", kv[0], err, out)
			}
			return fmt.Errorf("git config %s: %w", kv[0], err)
		}
	}
	return nil
}

// EnsureGitAttributes ensures path contains * text=auto eol=lf (before any
// pattern-specific rules), binary rules for media extensions, and merge=nsgo-scene
// rules for *.scene.yaml and *.scene.yml without duplicating existing entries.
func EnsureGitAttributes(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(GitAttributesContent), 0o644); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	haveLF := false
	haveBinary := make(map[string]bool, len(BinaryMediaPatterns))
	haveYAML, haveYML := false, false
	for _, line := range strings.Split(string(existing), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		pat := fields[0]
		attrs := fields[1:]
		if pat == "*" && slices.Contains(attrs, "eol=lf") {
			haveLF = true
		}
		if slices.Contains(attrs, "binary") || slices.Contains(attrs, "-text") {
			haveBinary[pat] = true
		}
		if slices.Contains(attrs, "merge="+DriverName) {
			switch pat {
			case "*.scene.yaml":
				haveYAML = true
			case "*.scene.yml":
				haveYML = true
			}
		}
	}
	var missingBinary []string
	for _, pat := range BinaryMediaPatterns {
		if !haveBinary[pat] {
			missingBinary = append(missingBinary, pat)
		}
	}
	if haveLF && len(missingBinary) == 0 && haveYAML && haveYML {
		return false, nil
	}

	var buf bytes.Buffer
	if !haveLF {
		buf.WriteString(defaultLineEndingHeader)
		if len(bytes.TrimSpace(existing)) > 0 {
			buf.WriteByte('\n')
		}
	}
	buf.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		buf.WriteByte('\n')
	}
	if len(missingBinary) > 0 {
		if buf.Len() > 0 && !bytes.HasSuffix(buf.Bytes(), []byte("\n\n")) {
			buf.WriteByte('\n')
		}
		if len(missingBinary) == len(BinaryMediaPatterns) {
			buf.WriteString(defaultBinaryHeader)
		}
		for _, pat := range missingBinary {
			buf.WriteString(pat)
			buf.WriteString(" binary\n")
		}
	}
	if !haveYAML || !haveYML {
		if buf.Len() > 0 && !bytes.HasSuffix(buf.Bytes(), []byte("\n\n")) {
			buf.WriteByte('\n')
		}
		if !haveYAML && !haveYML {
			buf.WriteString(defaultSceneMergeHeader)
		}
		if !haveYAML {
			buf.WriteString("*.scene.yaml merge=" + DriverName + "\n")
		}
		if !haveYML {
			buf.WriteString("*.scene.yml merge=" + DriverName + "\n")
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// detectGitAttributes reports whether .gitattributes in dir or its ancestors up to repoRoot
// (or Git's effective attributes, such as .git/info/attributes) routes scene files to merge=nsgo-scene.
func detectGitAttributes(dir, repoRoot string, runGit GitRunner) (bool, string) {
	dirs := ancestorChain(dir, repoRoot)

	// Track each pattern's effective merge attribute from repoRoot down to dir so child files
	// and later lines override earlier entries for the same pattern.
	patternDriver := make(map[string]string)
	patternFile := make(map[string]string)
	for _, d := range dirs {
		attrPath := filepath.Join(d, ".gitattributes")
		data, err := os.ReadFile(attrPath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) < 2 {
				continue
			}
			pat := fields[0]
			for _, attr := range fields[1:] {
				switch {
				case attr == "merge="+DriverName:
					patternDriver[pat] = DriverName
					patternFile[pat] = attrPath
				case attr == "-merge" || attr == "!merge" || attr == "merge" || strings.HasPrefix(attr, "merge="):
					patternDriver[pat] = ""
					delete(patternFile, pat)
				}
			}
		}
	}

	var matchedPatterns []string
	for pat, drv := range patternDriver {
		if drv == DriverName {
			matchedPatterns = append(matchedPatterns, pat)
		}
	}
	slices.Sort(matchedPatterns)

	// Ask Git directly for root-level scene probes so .git/info/attributes, global attributes,
	// and cross-pattern overrides (such as "*.yaml -merge") are respected when git supports it.
	checkAttrMatched, checkAttrOverridden := probeGitCheckAttr(dir, runGit)
	if checkAttrMatched {
		attrPath := ""
		if len(matchedPatterns) > 0 {
			attrPath = deepestAttrPath(matchedPatterns, patternFile)
		}
		return true, attrPath
	}
	if len(matchedPatterns) == 0 {
		return false, ""
	}
	if checkAttrOverridden {
		// Root-level *.scene.yaml and *.scene.yml were explicitly unset or reassigned to another driver.
		// Only report active if a more specific path pattern (such as scenes/*.scene.yaml) still names nsgo-scene.
		hasScoped := false
		for _, pat := range matchedPatterns {
			if pat != "*.scene.yaml" && pat != "*.scene.yml" {
				hasScoped = true
				break
			}
		}
		if !hasScoped {
			return false, ""
		}
	}
	return true, deepestAttrPath(matchedPatterns, patternFile)
}

func deepestAttrPath(patterns []string, patternFile map[string]string) string {
	best := ""
	for _, pat := range patterns {
		p := patternFile[pat]
		if len(p) > len(best) || (len(p) == len(best) && p < best) {
			best = p
		}
	}
	return best
}

func probeGitCheckAttr(dir string, runGit GitRunner) (matched, overridden bool) {
	out, err := runGit(dir, "check-attr", "merge", "--", ".nsgo-scene-probe.scene.yaml", ".nsgo-scene-probe.scene.yml")
	if err != nil || out == "" {
		return false, false
	}
	lines := strings.Split(out, "\n")
	parsed := 0
	overriddenCount := 0
	for _, line := range lines {
		idx := strings.LastIndex(line, ": merge: ")
		if idx < 0 {
			continue
		}
		parsed++
		val := strings.TrimSpace(line[idx+len(": merge: "):])
		if val == DriverName {
			return true, false
		}
		if val != "unspecified" {
			overriddenCount++
		}
	}
	return false, parsed > 0 && overriddenCount == parsed
}

// ancestorChain returns directories from repoRoot down to dir (inclusive),
// resolving symlinks so macOS /var vs /private/var differences do not prevent matching.
func ancestorChain(dir, repoRoot string) []string {
	realDir := canonicalPath(dir)
	realRoot := canonicalPath(repoRoot)
	if realDir == "" {
		return nil
	}
	if realRoot == "" {
		return []string{realDir}
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []string{realDir}
	}
	if rel == "." {
		return []string{realRoot}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	chain := make([]string, 0, len(parts)+1)
	cur := realRoot
	chain = append(chain, cur)
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		cur = filepath.Join(cur, p)
		chain = append(chain, cur)
	}
	return chain
}

func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		return eval
	}
	return abs
}
