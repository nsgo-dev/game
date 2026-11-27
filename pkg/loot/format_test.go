package loot_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/loot"
)

const canonicalDoc = `version: 1
tables:
  - name: chest
    rolls:
      min: 1
      max: 3
    guaranteed:
      - item: gold
        count:
          min: 5
          max: 25
    entries:
      - item: sword
        weight: 2.5
      - table: gems
        weight: 1
        count: 2
      - nothing: true
        weight: 6.5
    pity:
      - name: gems
        table: gems
        guaranteeAt: 20
        softFrom: 12
        softStep: 0.75
  - name: gems
    entries:
      - item: ruby
        weight: 1
`

func TestEncodeIsCanonicalAndRoundTrips(t *testing.T) {
	doc, err := loot.Parse([]byte(canonicalDoc))
	if err != nil {
		t.Fatal(err)
	}
	want := &loot.Document{
		Version: 1,
		Tables: []loot.Table{
			{
				Name:       "chest",
				Rolls:      loot.Between(1, 3),
				Guaranteed: []loot.Entry{{Item: "gold", Count: loot.Between(5, 25)}},
				Entries: []loot.Entry{
					{Item: "sword", Weight: 2.5},
					{Table: "gems", Weight: 1, Count: loot.Exactly(2)},
					{Nothing: true, Weight: 6.5},
				},
				Pity: []loot.Pity{{Name: "gems", Table: "gems", GuaranteeAt: 20, SoftFrom: 12, SoftStep: 0.75}},
			},
			{Name: "gems", Entries: []loot.Entry{{Item: "ruby", Weight: 1}}},
		},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("Parse = %#v\nwant %#v", doc, want)
	}
	out, err := loot.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != canonicalDoc {
		t.Fatalf("Encode changed the canonical text:\n%s", out)
	}
	again, err := loot.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, doc) {
		t.Fatal("the round trip changed the document")
	}
}

func TestRangeForms(t *testing.T) {
	doc, err := loot.Parse([]byte(`
version: 1
tables:
  - name: t
    rolls: {min: 2, max: 2}
    entries:
      - item: a
        weight: 1
        count: 3
`))
	if err != nil {
		t.Fatal(err)
	}
	if *doc.Tables[0].Rolls != (loot.Range{Min: 2, Max: 2}) || *doc.Tables[0].Entries[0].Count != (loot.Range{Min: 3, Max: 3}) {
		t.Fatalf("ranges = %+v %+v", doc.Tables[0].Rolls, doc.Tables[0].Entries[0].Count)
	}
	out, err := loot.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "rolls: 2\n") {
		t.Fatalf("an even range should encode as an integer:\n%s", out)
	}
}

// wideTable returns a document with one table that makes 1024 picks among n item entries and has the given number of hard pity rules.
func wideTable(n, pity int) string {
	var b strings.Builder
	b.WriteString("version: 1\ntables:\n  - name: t\n    rolls: 1024\n    entries:\n")
	for i := range n {
		fmt.Fprintf(&b, "      - item: i%d\n        weight: 1\n", i)
	}
	if pity > 0 {
		b.WriteString("    pity:\n")
		for i := range pity {
			fmt.Fprintf(&b, "      - name: p%d\n        item: i%d\n        guaranteeAt: 10\n", i, i)
		}
	}
	return b.String()
}

func TestParseRejects(t *testing.T) {
	const head = "version: 1\ntables:\n"
	cases := []struct {
		name, src, want string
	}{
		{"empty", "  \n", "empty document"},
		{"version", "version: 2\ntables:\n  - name: t\n    entries:\n      - item: a\n        weight: 1\n", "unsupported version 2"},
		{"no tables", "version: 1\ntables: []\n", "at least one table"},
		{"unknown field", head + "  - name: t\n    color: red\n", "field color not found"},
		{"unknown entry field", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n        rarity: 3\n", "field rarity not found"},
		{"unknown range field", head + "  - name: t\n    rolls: {min: 1, most: 2}\n    entries:\n      - item: a\n        weight: 1\n", "field most not found"},
		{"half range", head + "  - name: t\n    rolls: {min: 1}\n    entries:\n      - item: a\n        weight: 1\n", "both min and max"},
		{"range list", head + "  - name: t\n    rolls: [1, 2]\n    entries:\n      - item: a\n        weight: 1\n", "integer or a min/max mapping"},
		{"trailing document", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n---\nversion: 1\n", "trailing YAML document"},
		{"duplicate key", head + "  - name: t\n    name: u\n    entries:\n      - item: a\n        weight: 1\n", "already defined"},
		{"blank name", head + "  - name: ' t'\n    entries:\n      - item: a\n        weight: 1\n", "surrounding spaces"},
		{"duplicate table", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n  - name: t\n    entries:\n      - item: a\n        weight: 1\n", "duplicate table name"},
		{"empty table", head + "  - name: t\n", "needs entries"},
		{"rolls without entries", head + "  - name: t\n    rolls: 2\n    guaranteed:\n      - item: a\n", "rolls needs entries"},
		{"two kinds", head + "  - name: t\n    entries:\n      - item: a\n        table: t\n        weight: 1\n", "exactly one of item, table and nothing"},
		{"no kind", head + "  - name: t\n    entries:\n      - weight: 1\n", "exactly one of item, table and nothing"},
		{"zero weight", head + "  - name: t\n    entries:\n      - item: a\n", "weight 0"},
		{"negative weight", head + "  - name: t\n    entries:\n      - item: a\n        weight: -1\n", "weight -1"},
		{"nan weight", head + "  - name: t\n    entries:\n      - item: a\n        weight: .nan\n", "weight NaN"},
		{"infinite weight", head + "  - name: t\n    entries:\n      - item: a\n        weight: .inf\n", "weight +Inf"},
		{"guaranteed weight", head + "  - name: t\n    guaranteed:\n      - item: a\n        weight: 1\n", "has no weight"},
		{"guaranteed nothing", head + "  - name: t\n    guaranteed:\n      - nothing: true\n", "cannot be nothing"},
		{"nothing count", head + "  - name: t\n    entries:\n      - nothing: true\n        weight: 1\n        count: 2\n", "no count"},
		{"inverted count", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n        count: {min: 3, max: 2}\n", "range [3, 2]"},
		{"negative count", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n        count: -1\n", "range [-1, -1]"},
		{"too many rolls", head + "  - name: t\n    rolls: 1025\n    entries:\n      - item: a\n        weight: 1\n", "range [1025, 1025]"},
		{"duplicate item", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n      - item: a\n        weight: 2\n", "already an entry"},
		{"unknown reference", head + "  - name: t\n    entries:\n      - table: u\n        weight: 1\n", "unknown table \"u\""},
		{"self cycle", head + "  - name: t\n    entries:\n      - table: t\n        weight: 1\n", "cycle"},
		{"cycle", head + "  - name: t\n    guaranteed:\n      - table: u\n  - name: u\n    entries:\n      - table: t\n        weight: 1\n", "cycle"},
		{"runaway", head + "  - name: t\n    rolls: 1000\n    entries:\n      - table: u\n        weight: 1\n        count: 100\n  - name: u\n    entries:\n      - item: a\n        weight: 1\n", "more than 65536 steps"},
		{"pity without rule", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n", "set guaranteeAt, softFrom or both"},
		{"pity no target", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        guaranteeAt: 3\n", "exactly one of item and table"},
		{"pity missing target", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: b\n        guaranteeAt: 3\n", "not one of the table's entries"},
		{"pity guaranteed target", head + "  - name: t\n    guaranteed:\n      - item: b\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: b\n        guaranteeAt: 3\n", "not one of the table's entries"},
		{"pity duplicate", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n        guaranteeAt: 3\n      - name: p\n        item: a\n        guaranteeAt: 4\n", "duplicate pity name"},
		{"pity unnamed", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - item: a\n        guaranteeAt: 3\n", "must be non-empty"},
		{"pity soft without step", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n        softFrom: 3\n", "softStep 0"},
		{"pity step without soft", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n        guaranteeAt: 3\n        softStep: 1\n", "softStep needs softFrom"},
		{"pity soft after hard", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n        guaranteeAt: 3\n        softFrom: 3\n        softStep: 1\n", "below guaranteeAt"},
		{"wide table", wideTable(64, 0), "more than 65536 steps"},
		{"wide table with pity", wideTable(62, 2), "more than 65536 steps"},
		{"pity negative", head + "  - name: t\n    entries:\n      - item: a\n        weight: 1\n    pity:\n      - name: p\n        item: a\n        guaranteeAt: -3\n", "guaranteeAt -3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loot.Parse([]byte(tc.src))
			if err == nil {
				t.Fatalf("Parse accepted:\n%s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestStepsCountEveryEntryOfAPick checks the bound just below the wide tables TestParseRejects refuses: 1024 picks over 63 entries and pity rules plus the picked item is 65536 steps.
func TestStepsCountEveryEntryOfAPick(t *testing.T) {
	for _, src := range []string{wideTable(63, 0), wideTable(61, 2)} {
		if _, err := loot.Parse([]byte(src)); err != nil {
			t.Fatalf("Parse refused a table at the bound: %v", err)
		}
	}
}

func TestEmptyListsRoundTrip(t *testing.T) {
	const src = "version: 1\ntables:\n  - name: t\n    guaranteed: []\n    entries:\n      - item: a\n        weight: 1\n    pity: []\n  - name: g\n    guaranteed:\n      - item: b\n    entries: []\n"
	doc, err := loot.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := loot.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	again, err := loot.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, again) {
		t.Fatalf("Parse(Encode(doc)) = %#v\nwant %#v", again, doc)
	}
}

func TestEncodeValidates(t *testing.T) {
	if _, err := loot.Encode(nil); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
	if _, err := loot.Encode(&loot.Document{Version: 1, Tables: []loot.Table{{Name: "t", Entries: []loot.Entry{{Item: "a"}}}}}); err == nil {
		t.Fatal("Encode accepted a zero weight")
	}
	if _, err := loot.New(&loot.Document{Version: 0}); err == nil {
		t.Fatal("New accepted version 0")
	}
}

func TestLibraryIgnoresLaterDocumentEdits(t *testing.T) {
	doc, err := loot.Parse([]byte(weightedDoc))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := loot.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Tables[0].Entries[0].Weight = 1e9
	doc.Tables[0].Entries[0].Item = "changed"
	if got := lib.Chances("chest")[0]; got != 0.5 {
		t.Fatalf("Chances changed with the document: %v", got)
	}
}

// TestGuideExamplesParse keeps the YAML in docs/en/loot.md and docs/zh/loot.md valid.
func TestGuideExamplesParse(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", lang, "loot.md"))
		if err != nil {
			t.Fatal(err)
		}
		blocks := strings.Split(string(data), "```yaml\n")[1:]
		if len(blocks) != 2 {
			t.Fatalf("%s: found %d YAML blocks, want 2", lang, len(blocks))
		}
		boss := strings.SplitN(blocks[0], "```", 2)[0]
		banner := strings.SplitN(blocks[1], "```", 2)[0]
		full := boss + banner + "  - name: five-star\n    entries:\n      - item: hero\n        weight: 1\n  - name: four-star\n    entries:\n      - item: sidekick\n        weight: 1\n"
		doc, err := loot.Parse([]byte(full))
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if len(doc.Tables) != 6 {
			t.Fatalf("%s: %d tables", lang, len(doc.Tables))
		}
	}
}
