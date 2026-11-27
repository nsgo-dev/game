package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gameYAML = `version: 1
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

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String(), err
}

func TestSyncWritesNSGoAndCheckAgrees(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML, "nsgo.yaml": "name: Old\n# kept\ndev:\n  target: native\n"})

	if _, err := runCLI(t, "check", dir); err == nil || !strings.Contains(err.Error(), `name is "Old"`) {
		t.Fatalf("check of a stale nsgo.yaml = %v", err)
	}
	if _, err := runCLI(t, "sync", "-check", dir); err == nil {
		t.Fatal("sync -check passed a stale nsgo.yaml")
	}
	if out, err := runCLI(t, "sync", dir); err != nil || !strings.Contains(out, "nsgo.yaml") {
		t.Fatalf("sync = %q, %v", out, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "nsgo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "name: Demo\n# kept\ndev:\n  target: native\nid: dev.example.demo\nversion: 1.2.0\nbuild:\n  targets: [web]\n"
	if string(got) != want {
		t.Fatalf("nsgo.yaml = %q, want %q", got, want)
	}
	if _, err := runCLI(t, "sync", "-check", dir); err != nil {
		t.Fatalf("sync -check after sync: %v", err)
	}
	if out, err := runCLI(t, "sync", dir); err != nil || out != "" {
		t.Fatalf("a second sync = %q, %v; want no change", out, err)
	}
	if out, err := runCLI(t, "check", dir); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("check after sync = %q, %v", out, err)
	}
}

func TestSyncCreatesAMissingNSGo(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML})
	if _, err := runCLI(t, "sync", "-check", dir); err == nil {
		t.Fatal("sync -check passed without nsgo.yaml")
	}
	if _, err := runCLI(t, "sync", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nsgo.yaml")); err != nil {
		t.Fatalf("sync did not create nsgo.yaml: %v", err)
	}
}

func TestFmtRewritesToCanonicalForm(t *testing.T) {
	messy := "# A comment.\nversion: 1\nid: dev.example.demo\nname: Demo\nappVersion: 1.2.0\nwindow: {height: 360, width: 640}\nexport: {presets: [{platform: macos, targets: [macos_arm64]}, {platform: web}]}\n"
	dir := writeProject(t, map[string]string{"game.yaml": messy})
	if _, err := runCLI(t, "fmt", "-check", dir); err == nil {
		t.Fatal("fmt -check passed a file out of canonical form")
	}
	if _, err := runCLI(t, "fmt", dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != gameYAML {
		t.Fatalf("fmt wrote %q, want %q", got, gameYAML)
	}
	if _, err := runCLI(t, "fmt", "-check", dir); err != nil {
		t.Fatalf("fmt -check after fmt: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("fmt left %d files, want only game.yaml", len(entries))
	}
}

func TestTargetsPrintsTheBuildTargets(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML})
	if out, err := runCLI(t, "targets", dir); err != nil || out != "macos_arm64,web\n" {
		t.Fatalf("targets = %q, %v", out, err)
	}
}

func TestCheckReportsAMissingAutoload(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML + "autoload:\n  resources:\n    - name: table\n      path: data/table.csv\n"})
	if _, err := runCLI(t, "check", dir); err == nil || !strings.Contains(err.Error(), "table") {
		t.Fatalf("check = %v, want the missing table reported", err)
	}
}

func TestUsageErrors(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML})
	for _, args := range [][]string{nil, {"bogus"}, {"check", dir, dir}, {"fmt", "-nope", dir}} {
		if _, err := runCLI(t, args...); err == nil {
			t.Fatalf("run(%q) succeeded", args)
		}
	}
	if out, err := runCLI(t, "help"); err != nil || !strings.Contains(out, "usage") {
		t.Fatalf("help = %q, %v", out, err)
	}
	if _, err := runCLI(t, "check", t.TempDir()); err == nil {
		t.Fatal("check of a directory without game.yaml succeeded")
	}
	bad := writeProject(t, map[string]string{"game.yaml": "version: 1\n"})
	for _, cmd := range []string{"fmt", "sync", "targets"} {
		if _, err := runCLI(t, cmd, bad); err == nil {
			t.Fatalf("%s of an invalid game.yaml succeeded", cmd)
		}
	}
}
