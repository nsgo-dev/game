package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const (
	// sceneMergeDriverName is the gitattributes merge driver identifier for NSGo scene files.
	sceneMergeDriverName = "nsgo-scene"
	// sceneMergeDriverTitle is the human-readable description written to git config.
	sceneMergeDriverTitle = "NSGo scene merge driver"
	// sceneMergeDriverCmd is the command Git invokes with ancestor (%O), current (%A), other (%B) and path (%P).
	sceneMergeDriverCmd = "go tool game merge %O %A %B %P"
	// sceneGitAttributesContent is the default .gitattributes written by templates and game merge -setup.
	sceneGitAttributesContent = "# Merge NSGo scene files object by object instead of line by line.\n# Enable the driver once per clone with: go tool game merge -setup\n*.scene.yaml merge=nsgo-scene\n*.scene.yml merge=nsgo-scene\n"
)

// mergeCommand runs the object-by-object scene Git merge driver or registers it in a project's Git config and .gitattributes.
var mergeCommand = &command{
	name:    "merge",
	args:    "<base> <ours> <theirs> [path] | -setup [dir]",
	summary: "Merge scene YAML files object by object for Git, or set up the Git merge driver in a project",
	define:  defineMerge,
	complete: map[string]completion{
		"conflict-out": filePaths,
		"":             filePaths,
	},
}

// runGit runs git with args in dir and returns its trimmed combined output. A test may replace it.
var runGit = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func defineMerge(a *app, fs *flag.FlagSet) func() error {
	out := outFlag(fs, "", "file to write the merged scene or conflict report to (default: overwrite <ours> in place, as Git expects)")
	conflictOut := fs.String("conflict-out", "", "file to write the YAML conflict report to instead of overwriting the output file on conflict")
	setup := fs.Bool("setup", false, "register the nsgo-scene merge driver in local Git config and .gitattributes")

	return func() error {
		if *setup {
			if *out != "" || *conflictOut != "" {
				return usagef("-setup cannot be combined with -out or -conflict-out")
			}
			if fs.NArg() > 1 {
				return usagef("-setup takes at most one directory")
			}
			dir := "."
			if fs.NArg() == 1 {
				dir = fs.Arg(0)
			}
			return setupSceneMergeDriver(dir, a.stdout)
		}

		if fs.NArg() < 3 || fs.NArg() > 4 {
			return usagef("expected <base> <ours> <theirs> [path]")
		}
		basePath, oursPath, theirsPath := fs.Arg(0), fs.Arg(1), fs.Arg(2)
		scenePath := ""
		if fs.NArg() == 4 {
			scenePath = filepath.ToSlash(fs.Arg(3))
		}

		baseData, err := os.ReadFile(basePath)
		if err != nil {
			return fmt.Errorf("read base %s: %w", basePath, err)
		}
		oursData, err := os.ReadFile(oursPath)
		if err != nil {
			return fmt.Errorf("read ours %s: %w", oursPath, err)
		}
		theirsData, err := os.ReadFile(theirsPath)
		if err != nil {
			return fmt.Errorf("read theirs %s: %w", theirsPath, err)
		}

		var baseDoc *sceneformat.SceneDoc
		if len(bytes.TrimSpace(baseData)) > 0 {
			baseDoc, err = sceneformat.ParseWith(baseData, sceneformat.ParseOptions{File: basePath})
			if err != nil {
				return fmt.Errorf("parse base %s: %w", basePath, err)
			}
		}
		oursDoc, err := sceneformat.ParseWith(oursData, sceneformat.ParseOptions{File: oursPath})
		if err != nil {
			return fmt.Errorf("parse ours %s: %w", oursPath, err)
		}
		theirsDoc, err := sceneformat.ParseWith(theirsData, sceneformat.ParseOptions{File: theirsPath})
		if err != nil {
			return fmt.Errorf("parse theirs %s: %w", theirsPath, err)
		}

		res, err := sceneformat.Merge(baseDoc, oursDoc, theirsDoc)
		if err != nil {
			return err
		}

		dest := oursPath
		if *out != "" {
			dest = *out
		}

		if !res.HasConflicts() {
			encoded, err := sceneformat.Encode(res.Doc)
			if err != nil {
				return fmt.Errorf("encode merged scene: %w", err)
			}
			if err := writeMergeOutput(dest, encoded); err != nil {
				return err
			}
			return nil
		}

		cdoc := res.ConflictDoc(scenePath)
		encoded, err := sceneformat.EncodeConflict(cdoc)
		if err != nil {
			return fmt.Errorf("encode conflict report: %w", err)
		}
		conflictDest := dest
		if *conflictOut != "" {
			conflictDest = *conflictOut
		}
		if err := writeMergeOutput(conflictDest, encoded); err != nil {
			return err
		}
		label := scenePath
		if label == "" {
			label = oursPath
		}
		for _, c := range res.Conflicts {
			fmt.Fprintf(a.stderr, "game merge: conflict in %s: %s\n", label, c.Summary())
		}
		return fmt.Errorf("%s: %d merge conflict(s); conflict details written to %s", label, len(res.Conflicts), conflictDest)
	}
}

func writeMergeOutput(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// setupSceneMergeDriver registers merge.nsgo-scene in the Git repository at dir
// and ensures .gitattributes routes *.scene.yaml and *.scene.yml to it.
func setupSceneMergeDriver(dir string, stdout io.Writer) error {
	if err := configureGitMergeDriver(dir); err != nil {
		return err
	}
	attrPath := filepath.Join(dir, ".gitattributes")
	changed, err := ensureSceneGitAttributes(attrPath)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(stdout, "updated %s\n", attrPath)
	}
	fmt.Fprintf(stdout, "configured git merge.%s.driver = %s\n", sceneMergeDriverName, sceneMergeDriverCmd)
	return nil
}

// configureGitMergeDriver writes merge.nsgo-scene.name and
// merge.nsgo-scene.driver into the local Git repository containing dir.
func configureGitMergeDriver(dir string) error {
	if out, err := runGit(dir, "rev-parse", "--show-toplevel"); err != nil {
		if out != "" {
			return fmt.Errorf("git rev-parse in %s: %w (%s); run git init first", dir, err, out)
		}
		return fmt.Errorf("git rev-parse in %s: %w; run git init first", dir, err)
	}
	for _, kv := range [][2]string{
		{"merge." + sceneMergeDriverName + ".name", sceneMergeDriverTitle},
		{"merge." + sceneMergeDriverName + ".driver", sceneMergeDriverCmd},
	} {
		if out, err := runGit(dir, "config", kv[0], kv[1]); err != nil {
			if out != "" {
				return fmt.Errorf("git config %s: %w (%s)", kv[0], err, out)
			}
			return fmt.Errorf("git config %s: %w", kv[0], err)
		}
	}
	return nil
}

// ensureSceneGitAttributes ensures path contains merge=nsgo-scene rules for
// *.scene.yaml and *.scene.yml without duplicating existing entries.
func ensureSceneGitAttributes(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(sceneGitAttributesContent), 0o644); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	haveYAML, haveYML := false, false
	for _, line := range strings.Split(string(existing), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for _, attr := range fields[1:] {
			if attr == "merge="+sceneMergeDriverName {
				switch fields[0] {
				case "*.scene.yaml":
					haveYAML = true
				case "*.scene.yml":
					haveYML = true
				}
			}
		}
	}
	if haveYAML && haveYML {
		return false, nil
	}

	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		buf.WriteByte('\n')
	}
	if !haveYAML {
		buf.WriteString("*.scene.yaml merge=" + sceneMergeDriverName + "\n")
	}
	if !haveYML {
		buf.WriteString("*.scene.yml merge=" + sceneMergeDriverName + "\n")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
