package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestHearthDataIsCanonical checks that hearth.yaml is valid and in the form encodeData writes, so a load and save changes nothing.
func TestHearthDataIsCanonical(t *testing.T) {
	d, err := parseData(hearthYAML)
	if err != nil {
		t.Fatal(err)
	}
	out, err := encodeData(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, hearthYAML) {
		t.Fatalf("hearth.yaml is not canonical; encodeData writes:\n%s", out)
	}
}

func TestParseDataRefusesBadFiles(t *testing.T) {
	for _, tc := range []struct{ name, from, to, want string }{
		{"a newer version", "version: 1", "version: 2", "version 2"},
		{"an unknown field", "  burn: 1\n", "  burn: 1\n  smoke: 2\n", "smoke"},
		{"a second document", "  stepSeconds: 1\n", "  stepSeconds: 1\n---\nversion: 1\n", "more than one document"},
		{"a number that is not one", `cost: "10"`, `cost: ten`, "invalid number"},
		{"a duplicate generator", "id: kindling", "id: spark", "duplicate id"},
		{"a growth of 1", "growth: 1.15", "growth: 1", "growth"},
		{"an unknown effect", "effect: stoke", "effect: smoke", "effect"},
		{"a target that is not a generator", "target: spark", "target: bellows", "not a generator"},
		{"a multiplier of 1", "value: 4", "value: 1", "out of range"},
		{"no fuel", "capacity: 100", "capacity: 0", "fuel"},
		{"an infinite capacity", "capacity: 100", "capacity: .inf", "fuel"},
		{"an infinite burn", "burn: 1\n", "burn: .inf\n", "fuel"},
		{"an efficiency above 1", "efficiency: 0.5", "efficiency: 2", "offline"},
		{"a goal for nothing", `cost: "1000000000000"`, `cost: "0"`, "goal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(string(hearthYAML), tc.from, tc.to, 1)
			if src == string(hearthYAML) {
				t.Fatalf("hearth.yaml has no %q to replace", tc.from)
			}
			if _, err := parseData([]byte(src)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseData = %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// FuzzParseData checks that parseData never panics and that whatever it accepts encodes to a canonical file that parses back to the same bytes.
func FuzzParseData(f *testing.F) {
	f.Add(hearthYAML)
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, src []byte) {
		d, err := parseData(src)
		if err != nil {
			return
		}
		out, err := encodeData(d)
		if err != nil {
			t.Fatalf("encodeData of an accepted file: %v", err)
		}
		d2, err := parseData(out)
		if err != nil {
			t.Fatalf("parseData of encodeData's output: %v\n%s", err, out)
		}
		if out2, err := encodeData(d2); err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encodeData is not stable (%v):\n%s\n---\n%s", err, out, out2)
		}
	})
}
