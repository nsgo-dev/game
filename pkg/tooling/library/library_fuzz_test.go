package library_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/library"
)

// FuzzParseIndex checks that ParseIndex never panics and that every index it accepts encodes to a canonical form it reads back, which encodes to the same bytes.
func FuzzParseIndex(f *testing.F) {
	if data, err := os.ReadFile(filepath.Join(repoLibrary, library.IndexFileName)); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\npacks: [{name: a, kind: plugin, packVersion: '1', description: A., tags: [x], license: MIT OR Apache-2.0, source: 'https://example.com/a', sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		idx, err := library.ParseIndex(data)
		if err != nil {
			return
		}
		first, err := library.EncodeIndex(idx)
		if err != nil {
			t.Fatalf("EncodeIndex of a parsed index failed: %v", err)
		}
		again, err := library.ParseIndex(first)
		if err != nil {
			t.Fatalf("ParseIndex of an encoded index failed: %v\n%s", err, first)
		}
		second, err := library.EncodeIndex(again)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable (%v):\n%s\nthen:\n%s", err, first, second)
		}
	})
}

// FuzzParseManifest checks the same of ParseManifest and EncodeManifest.
func FuzzParseManifest(f *testing.F) {
	if data, err := os.ReadFile(filepath.Join(repoLibrary, "packs", "dungeon-tiles", library.ManifestFileName)); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\nname: p\nkind: plugin\npackVersion: '1'\ndescription: P.\nlicense: MIT\ninto: tools/p\nfiles: [a.yaml, b/c.yaml]\nautoload: {scenes: [{name: s, path: a.yaml}], resources: [{name: r, path: b/c.yaml}]}\nplugins: [p]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := library.ParseManifest(data)
		if err != nil {
			return
		}
		first, err := library.EncodeManifest(m)
		if err != nil {
			t.Fatalf("EncodeManifest of a parsed manifest failed: %v", err)
		}
		again, err := library.ParseManifest(first)
		if err != nil {
			t.Fatalf("ParseManifest of an encoded manifest failed: %v\n%s", err, first)
		}
		second, err := library.EncodeManifest(again)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable (%v):\n%s\nthen:\n%s", err, first, second)
		}
	})
}
