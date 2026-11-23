package statedump_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/statedump"
)

// FuzzParse checks that Parse never panics and that whatever it accepts encodes to a file that parses back and encodes to the same bytes.
func FuzzParse(f *testing.F) {
	for _, name := range []string{"level.yaml", "roundtrip.yaml"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: {f: 1., h: 0x10, b: True, d: 2001-12-14, n: ~}\nentities: []\n"))
	f.Add([]byte("version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: &x [1]\n  b: *x\nentities: []\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := statedump.Parse(data)
		if err != nil {
			return
		}
		out, err := statedump.Encode(d)
		if err != nil {
			t.Fatalf("Encode of a parsed dump failed: %v", err)
		}
		again, err := statedump.Parse(out)
		if err != nil {
			t.Fatalf("Parse of an encoded dump failed: %v\n%s", err, out)
		}
		out2, err := statedump.Encode(again)
		if err != nil {
			t.Fatal(err)
		}
		if diff := statedump.Diff(out, out2); diff != "" {
			t.Fatalf("round trip changed the file:\n%s", diff)
		}
	})
}

// FuzzDiff checks that Diff never panics and returns "" exactly when its inputs are equal.
func FuzzDiff(f *testing.F) {
	f.Add([]byte("a\nb\nc\n"), []byte("a\nx\nc\n"))
	f.Add([]byte(""), []byte("\n"))
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if got := statedump.Diff(a, b); (got == "") != (string(a) == string(b)) {
			t.Fatalf("Diff(%q, %q) = %q", a, b, got)
		}
	})
}

func benchDump(n int) *statedump.Dump {
	d := &statedump.Dump{Version: statedump.FormatVersion, Seed: 1, Rate: 60, Steps: 600}
	for i := range n {
		d.Entities = append(d.Entities, entities.EntitySnapshot{
			Index:      uint32(i),
			Generation: 1,
			Components: []entities.ComponentSnapshot{
				{Type: "health", Data: json.RawMessage(fmt.Sprintf(`{"hp":%d,"tags":["enemy"]}`, i%50))},
				{Type: "pos", Data: json.RawMessage(fmt.Sprintf(`{"x":%d.25,"y":%d.5}`, i, i*2))},
			},
		})
	}
	return d
}

func BenchmarkEncode1000Entities(b *testing.B) {
	d := benchDump(1000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := statedump.Encode(d); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse1000Entities(b *testing.B) {
	data, err := statedump.Encode(benchDump(1000))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := statedump.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiffOneChange1000Entities(b *testing.B) {
	d := benchDump(1000)
	a, err := statedump.Encode(d)
	if err != nil {
		b.Fatal(err)
	}
	d.Entities[500].Components[1].Data = json.RawMessage(`{"x":1,"y":2}`)
	c, err := statedump.Encode(d)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if statedump.Diff(a, c) == "" {
			b.Fatal("no diff")
		}
	}
}
