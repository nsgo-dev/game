package project_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
)

// FuzzParse checks that Parse never panics and that every file it accepts encodes to a canonical form Parse reads back, which encodes to the same bytes.
func FuzzParse(f *testing.F) {
	if golden, err := os.ReadFile("testdata/game.yaml"); err == nil {
		f.Add(golden)
	}
	f.Add([]byte("version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\n"))
	f.Add([]byte("version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\naudio: {buses: [{name: m, effects: [{delay: {time: 1.5s}}]}]}\n"))
	f.Add([]byte("version: 1\nname: G\nid: a.b\nappVersion: x\nwindow: {width: 1e9, height: 0.5}\ninput: {actions: [{name: a}], axes: [{name: x, positive: a}]}\n"))
	f.Add([]byte("version: 1\nname: G\nid: a.b\nappVersion: x\nwindow: {width: 1, height: 1}\neditor: {plugins: [health, paths]}\n"))
	f.Add([]byte("version: 1\nname: G\nid: a.b\nappVersion: x\nwindow: {width: 1, height: 1}\neditor: {plugins: [paths]}\nautoload: {resources: [{name: t, path: p/t/a.json}]}\npacks: [{name: t, kind: plugin, packVersion: 1.0.0, sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08, dir: p/t, files: [a.json, pack.yaml], resources: [t], plugins: [paths]}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := project.Parse(data)
		if err != nil {
			return
		}
		first, err := project.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed file failed: %v", err)
		}
		again, err := project.Parse(first)
		if err != nil {
			t.Fatalf("Parse of an encoded file failed: %v\n%s", err, first)
		}
		second, err := project.Encode(again)
		if err != nil {
			t.Fatalf("second Encode failed: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable:\n%s\nthen:\n%s", first, second)
		}
		if _, err := doc.SyncNSGo(nil); err != nil {
			t.Fatalf("SyncNSGo of a valid file failed: %v", err)
		}
	})
}
