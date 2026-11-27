package scenemerge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestCheckOutsideGitRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(GitAttributesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.InRepo || status.NeedsSetup() {
		t.Errorf("status = %+v, want InRepo=false NeedsSetup=false", status)
	}
}

func TestCheckWithoutAttributes(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runTestGit(t, dir, "init")

	status, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !status.InRepo || status.AttributesConfigured || status.DriverConfigured || status.NeedsSetup() {
		t.Errorf("status = %+v, want InRepo=true AttributesConfigured=false NeedsSetup=false", status)
	}
}

func TestCheckNeedsSetupAndPassesAfterConfigure(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runTestGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(GitAttributesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("Check before configure: %v", err)
	}
	if !status.InRepo || !status.AttributesConfigured || status.DriverConfigured || !status.NeedsSetup() {
		t.Fatalf("before configure: status = %+v, want NeedsSetup=true", status)
	}
	if !strings.HasSuffix(status.AttributesPath, ".gitattributes") {
		t.Errorf("AttributesPath = %q, want .gitattributes path", status.AttributesPath)
	}

	if err := ConfigureDriver(dir, nil); err != nil {
		t.Fatalf("ConfigureDriver: %v", err)
	}

	status, err = Check(dir, nil)
	if err != nil {
		t.Fatalf("Check after configure: %v", err)
	}
	if !status.InRepo || !status.AttributesConfigured || !status.DriverConfigured || status.NeedsSetup() {
		t.Fatalf("after configure: status = %+v, want DriverConfigured=true NeedsSetup=false", status)
	}
	if status.DriverCommand != DriverCommand {
		t.Errorf("DriverCommand = %q, want %q", status.DriverCommand, DriverCommand)
	}
}

func TestCheckNestedProjectInParentRepo(t *testing.T) {
	requireGit(t)

	t.Run("attributes_in_child_project", func(t *testing.T) {
		repo := t.TempDir()
		runTestGit(t, repo, "init")
		nested := filepath.Join(repo, "games", "dungeon")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, ".gitattributes"), []byte(GitAttributesContent), 0o644); err != nil {
			t.Fatal(err)
		}

		status, err := Check(nested, nil)
		if err != nil {
			t.Fatalf("Check nested: %v", err)
		}
		if !status.NeedsSetup() {
			t.Fatalf("nested status = %+v, want NeedsSetup=true", status)
		}

		if err := ConfigureDriver(nested, nil); err != nil {
			t.Fatalf("ConfigureDriver nested: %v", err)
		}
		if got := runTestGit(t, repo, "config", "--local", "merge.nsgo-scene.driver"); got != DriverCommand {
			t.Errorf("parent repo driver = %q, want %q", got, DriverCommand)
		}
		status, err = Check(nested, nil)
		if err != nil {
			t.Fatalf("Check nested after configure: %v", err)
		}
		if status.NeedsSetup() || !status.DriverConfigured {
			t.Fatalf("after configure nested status = %+v, want DriverConfigured=true", status)
		}
	})

	t.Run("attributes_in_parent_repo", func(t *testing.T) {
		repo := t.TempDir()
		runTestGit(t, repo, "init")
		if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(GitAttributesContent), 0o644); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(repo, "games", "dungeon")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}

		status, err := Check(nested, nil)
		if err != nil {
			t.Fatalf("Check nested with parent attrs: %v", err)
		}
		if !status.NeedsSetup() {
			t.Fatalf("nested status = %+v, want NeedsSetup=true", status)
		}
	})
}

func TestCheckAttributesEdgeCases(t *testing.T) {
	requireGit(t)

	t.Run("commented_rule_ignored", func(t *testing.T) {
		dir := t.TempDir()
		runTestGit(t, dir, "init")
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("# *.scene.yaml merge=nsgo-scene\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, err := Check(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if status.AttributesConfigured || status.NeedsSetup() {
			t.Errorf("commented rule should not trigger: %+v", status)
		}
	})

	t.Run("unset_in_child_overrides_parent", func(t *testing.T) {
		repo := t.TempDir()
		runTestGit(t, repo, "init")
		if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(GitAttributesContent), 0o644); err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(repo, "sub")
		if err := os.MkdirAll(child, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(child, ".gitattributes"), []byte("*.scene.yaml -merge\n*.scene.yml -merge\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, err := Check(child, nil)
		if err != nil {
			t.Fatal(err)
		}
		if status.AttributesConfigured || status.NeedsSetup() {
			t.Errorf("child unset should override parent: %+v", status)
		}
	})

	t.Run("scoped_scene_pattern", func(t *testing.T) {
		dir := t.TempDir()
		runTestGit(t, dir, "init")
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("scenes/*.scene.yaml merge=nsgo-scene\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, err := Check(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !status.NeedsSetup() {
			t.Errorf("scoped pattern should require setup: %+v", status)
		}
	})

	t.Run("git_info_attributes", func(t *testing.T) {
		dir := t.TempDir()
		runTestGit(t, dir, "init")
		infoAttrs := filepath.Join(dir, ".git", "info", "attributes")
		if err := os.MkdirAll(filepath.Dir(infoAttrs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(infoAttrs, []byte("*.scene.yaml merge=nsgo-scene\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, err := Check(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !status.NeedsSetup() {
			t.Errorf(".git/info/attributes should require setup: %+v", status)
		}
	})
}

func TestEnsureGitAttributes(t *testing.T) {
	t.Run("creates_full_default_idempotently", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), ".gitattributes")
		changed, err := EnsureGitAttributes(p)
		if err != nil || !changed {
			t.Fatalf("first EnsureGitAttributes: changed=%v err=%v, want true, nil", changed, err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != GitAttributesContent {
			t.Errorf("got:\n%s\nwant:\n%s", got, GitAttributesContent)
		}
		changed, err = EnsureGitAttributes(p)
		if err != nil || changed {
			t.Fatalf("second EnsureGitAttributes: changed=%v err=%v, want false, nil", changed, err)
		}
	})

	t.Run("upgrades_legacy_and_prepends_lf_before_custom_rules", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), ".gitattributes")
		if err := os.WriteFile(p, []byte("*.png binary\n*.bat text eol=crlf\n*.scene.yaml merge=nsgo-scene\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		changed, err := EnsureGitAttributes(p)
		if err != nil || !changed {
			t.Fatalf("EnsureGitAttributes: changed=%v err=%v", changed, err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		s := string(got)
		if strings.Index(s, "* text=auto eol=lf\n") > strings.Index(s, "*.png binary\n") {
			t.Errorf("expected * text=auto eol=lf before *.png binary:\n%s", s)
		}
		if strings.Count(s, "*.png binary") != 1 || strings.Count(s, "*.scene.yaml merge=nsgo-scene") != 1 {
			t.Errorf("duplicate rules in:\n%s", s)
		}
		if !strings.Contains(s, "*.scene.yml merge=nsgo-scene\n") {
			t.Errorf("missing *.scene.yml rule in:\n%s", s)
		}
		if changed, err := EnsureGitAttributes(p); err != nil || changed {
			t.Fatalf("second EnsureGitAttributes: changed=%v err=%v, want false, nil", changed, err)
		}
	})
}
