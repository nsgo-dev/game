package project_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
)

const templateNSGo = `name: Starter Game
id: dev.nsgo.startergame
version: 0.1.0
game: true
appearance: light

# A comment above dev.
dev:
  # native: this machine's backend.
  target: native

build:
  output: dist
  # true opens the output directory after building.
  open: false
  targets: [macos_arm64, web] # what to build
`

func TestSyncNSGoSetsOwnedFieldsAndKeepsTheRest(t *testing.T) {
	f := project.MustParse(readGolden(t))
	if err := f.CheckNSGo([]byte(templateNSGo)); err == nil {
		t.Fatal("CheckNSGo accepted an nsgo.yaml that disagrees")
	} else {
		for _, key := range []string{"name", "id", "appearance", "build.targets"} {
			if !strings.Contains(err.Error(), key+" is") {
				t.Fatalf("CheckNSGo error %q does not name %s", err, key)
			}
		}
		if strings.Contains(err.Error(), "build.output is") {
			t.Fatalf("CheckNSGo error %q names build.output, which agrees", err)
		}
	}

	out, err := f.SyncNSGo([]byte(templateNSGo))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.CheckNSGo(out); err != nil {
		t.Fatalf("CheckNSGo after SyncNSGo: %v\n%s", err, out)
	}
	for _, want := range []string{
		"name: Cave Run\n",
		"id: dev.nsgo.caverun\n",
		"game: true\n",
		"appearance: dark\n",
		"# A comment above dev.\n",
		"# native: this machine's backend.\n",
		"open: false\n",
		"targets: [macos, windows_x64, web] # what to build\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("SyncNSGo output lacks %q:\n%s", want, out)
		}
	}
	again, err := f.SyncNSGo(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, out) {
		t.Fatalf("SyncNSGo is not idempotent:\n%s\nthen:\n%s", out, again)
	}
}

func TestSyncNSGoRemovesFieldsTheProjectLeavesEmpty(t *testing.T) {
	f := project.MustParse([]byte("version: 1\nname: Starter\nid: dev.example.starter\nappVersion: 1.0.0\nwindow: {width: 480, height: 640}\n"))
	out, err := f.SyncNSGo([]byte("name: Old\nappearance: dark\nbuild:\n  output: dist\n  targets: [web]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "name: Starter\nid: dev.example.starter\nversion: 1.0.0\n"; string(out) != want {
		t.Fatalf("SyncNSGo = %q, want %q", out, want)
	}
	kept, err := f.SyncNSGo([]byte("build:\n  open: true\n  output: dist\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "build:\n  open: true\nname: Starter\nid: dev.example.starter\nversion: 1.0.0\n"; string(kept) != want {
		t.Fatalf("SyncNSGo = %q, want %q", kept, want)
	}
}

func TestSyncNSGoWritesANewFile(t *testing.T) {
	f := project.MustParse(readGolden(t))
	out, err := f.SyncNSGo(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "name: Cave Run\nid: dev.nsgo.caverun\nversion: 0.1.0\nappearance: dark\nbuild:\n  output: dist\n  targets: [macos, windows_x64, web]\n"
	if string(out) != want {
		t.Fatalf("SyncNSGo(nil) = %q, want %q", out, want)
	}
}

func TestSyncNSGoRefusesMalformedFiles(t *testing.T) {
	f := project.MustParse(readGolden(t))
	for _, src := range []string{"- a\n- b\n", "build: [web]\n", "name: [\n"} {
		if _, err := f.SyncNSGo([]byte(src)); err == nil {
			t.Fatalf("SyncNSGo accepted %q", src)
		}
	}
	if err := f.CheckNSGo([]byte("name: [\n")); err == nil {
		t.Fatal("CheckNSGo accepted malformed YAML")
	}
	if _, err := (&project.File{}).SyncNSGo(nil); err == nil {
		t.Fatal("SyncNSGo of an invalid File succeeded")
	}
	if err := (&project.File{}).CheckNSGo(nil); err == nil {
		t.Fatal("CheckNSGo of an invalid File succeeded")
	}
}
