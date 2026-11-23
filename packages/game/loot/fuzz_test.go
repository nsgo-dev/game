package loot_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/random"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{canonicalDoc, weightedDoc, nestedDoc, hardPityDoc, "version: 1\ntables: []\n", "version: 1\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := loot.Parse(data)
		if err != nil {
			return
		}
		out, err := loot.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed document failed: %v", err)
		}
		doc2, err := loot.Parse(out)
		if err != nil {
			t.Fatalf("Parse of encoded text failed: %v\n%s", err, out)
		}
		out2, err := loot.Encode(doc2)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", out, out2)
		}
		doc3, err := loot.Parse(out2)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(doc2, doc3) {
			t.Fatal("canonical text did not round-trip to an equal document")
		}

		lib, err := loot.New(doc)
		if err != nil {
			t.Fatalf("New rejected a parsed document: %v", err)
		}
		r := loot.NewRoller(lib, random.New(1))
		for _, name := range lib.Tables() {
			for range 3 {
				if _, err := r.Roll(name); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}
