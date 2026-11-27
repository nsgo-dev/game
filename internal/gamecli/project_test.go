package gamecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projectTestYAML = `version: 1
name: Demo
id: dev.example.demo
appVersion: 1.2.0
window:
  width: 640
  height: 360
export:
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: web
`

// writeProjectTestDir writes files, keyed by their slash-separated paths, into a new directory and returns it.
func writeProjectTestDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runProject runs game project with args and returns its exit code, standard output and standard error.
func runProject(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	a, stdout, stderr := newTestApp(t)
	code := run(append([]string{"project"}, args...), a)
	return code, stdout.String(), stderr.String()
}

func TestProjectSyncWritesNSGoAndCheckAgrees(t *testing.T) {
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": projectTestYAML, "nsgo.yaml": "name: Old\n# kept\ndev:\n  target: native\n"})

	if code, _, stderr := runProject(t, "check", dir); code != 1 || !strings.Contains(stderr, `name is "Old"`) {
		t.Fatalf("check of a stale nsgo.yaml = %d, %q", code, stderr)
	}
	if code, _, stderr := runProject(t, "-check", "sync", dir); code != 1 || !strings.Contains(stderr, "run game project sync") {
		t.Fatalf("-check sync of a stale nsgo.yaml = %d, %q", code, stderr)
	}
	if code, out, stderr := runProject(t, "sync", dir); code != 0 || !strings.Contains(out, "nsgo.yaml") {
		t.Fatalf("sync = %d, %q (stderr: %s)", code, out, stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "nsgo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "name: Demo\n# kept\ndev:\n  target: native\nid: dev.example.demo\nversion: 1.2.0\nbuild:\n  targets: [web]\n"
	if string(got) != want {
		t.Fatalf("nsgo.yaml = %q, want %q", got, want)
	}
	if code, _, stderr := runProject(t, "-check", "sync", dir); code != 0 {
		t.Fatalf("-check sync after sync = %d: %s", code, stderr)
	}
	if code, out, stderr := runProject(t, "sync", dir); code != 0 || out != "" {
		t.Fatalf("a second sync = %d, %q (stderr: %s); want no change", code, out, stderr)
	}
	if code, out, stderr := runProject(t, "check", dir); code != 0 || !strings.Contains(out, "ok") {
		t.Fatalf("check after sync = %d, %q (stderr: %s)", code, out, stderr)
	}
}

func TestProjectSyncCreatesAMissingNSGo(t *testing.T) {
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": projectTestYAML})
	if code, _, stderr := runProject(t, "-check", "sync", dir); code != 1 || !strings.Contains(stderr, "is missing") {
		t.Fatalf("-check sync without nsgo.yaml = %d, %q", code, stderr)
	}
	if code, _, stderr := runProject(t, "sync", dir); code != 0 {
		t.Fatalf("sync = %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "nsgo.yaml")); err != nil {
		t.Fatalf("sync did not create nsgo.yaml: %v", err)
	}
}

func TestProjectFmtRewritesToCanonicalForm(t *testing.T) {
	messy := "# A comment.\nversion: 1\nid: dev.example.demo\nname: Demo\nappVersion: 1.2.0\nwindow: {height: 360, width: 640}\nexport: {presets: [{platform: macos, targets: [macos_arm64]}, {platform: web}]}\n"
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": messy})
	if code, _, stderr := runProject(t, "-check", "fmt", dir); code != 1 || !strings.Contains(stderr, "game project: ") || !strings.Contains(stderr, "run game project fmt") {
		t.Fatalf("-check fmt of a file out of canonical form = %d, %q", code, stderr)
	}
	if code, _, stderr := runProject(t, "fmt", dir); code != 0 {
		t.Fatalf("fmt = %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != projectTestYAML {
		t.Fatalf("fmt wrote %q, want %q", got, projectTestYAML)
	}
	if code, _, stderr := runProject(t, "-check", "fmt", dir); code != 0 {
		t.Fatalf("-check fmt after fmt = %d: %s", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("fmt left %d files, want only game.yaml", len(entries))
	}
}

func TestProjectTargetsPrintsTheBuildTargets(t *testing.T) {
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": projectTestYAML})
	if code, out, stderr := runProject(t, "targets", dir); code != 0 || out != "macos_arm64,web\n" {
		t.Fatalf("targets = %d, %q (stderr: %s)", code, out, stderr)
	}
}

func TestProjectCheckReportsAMissingAutoload(t *testing.T) {
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": projectTestYAML + "autoload:\n  resources:\n    - name: table\n      path: data/table.csv\n"})
	if code, _, stderr := runProject(t, "check", dir); code != 1 || !strings.Contains(stderr, "table") {
		t.Fatalf("check = %d, %q; want the missing table reported", code, stderr)
	}
}

func TestProjectUsageErrors(t *testing.T) {
	dir := writeProjectTestDir(t, map[string]string{"game.yaml": projectTestYAML})
	for _, args := range [][]string{nil, {"bogus"}, {"check", dir, dir}, {"-nope", "fmt", dir}, {"-check", "check", dir}, {"-check", "targets", dir}, {"fmt", "-check", dir}} {
		code, _, stderr := runProject(t, args...)
		if code != 2 {
			t.Fatalf("game project %q exit %d, want 2 (stderr: %s)", args, code, stderr)
		}
		if !strings.Contains(stderr, "usage: game project") && !strings.Contains(stderr, "flag provided but not defined") {
			t.Errorf("game project %q printed no usage: %s", args, stderr)
		}
	}
	a, stdout, _ := newTestApp(t)
	if code := run([]string{"help", "project"}, a); code != 0 || !strings.Contains(stdout.String(), "usage: game project") {
		t.Fatalf("help project = %d, %q", code, stdout)
	}
	if code, _, _ := runProject(t, "check", t.TempDir()); code != 1 {
		t.Fatalf("check of a directory without game.yaml exit %d, want 1", code)
	}
	bad := writeProjectTestDir(t, map[string]string{"game.yaml": "version: 1\n"})
	for _, verb := range []string{"fmt", "sync", "targets"} {
		if code, _, _ := runProject(t, verb, bad); code != 1 {
			t.Fatalf("%s of an invalid game.yaml exit %d, want 1", verb, code)
		}
	}
}
