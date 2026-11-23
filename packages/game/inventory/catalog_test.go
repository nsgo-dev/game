package inventory_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/inventory"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func readTestdata(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}

func testCatalog(t testing.TB) *inventory.Catalog {
	t.Helper()
	cat, err := inventory.Parse(readTestdata(t, "items.yaml"))
	if err != nil {
		t.Fatalf("Parse items.yaml: %v", err)
	}
	return cat
}

func TestParseReadsDefinitions(t *testing.T) {
	cat := testCatalog(t)
	if cat.Len() != 6 {
		t.Fatalf("Len = %d, want 6", cat.Len())
	}
	potion, ok := cat.Item("potion")
	if !ok {
		t.Fatal("potion missing")
	}
	if potion.EffectiveMaxStack() != 20 || potion.Icon != "asset:icons/potion.png" || !potion.HasTag("healing") || potion.HasTag("quest") {
		t.Fatalf("potion = %+v", potion)
	}
	if potion.Stats["heal"] != 25 || potion.Stats["price"] != 12.5 {
		t.Fatalf("potion stats = %v", potion.Stats)
	}
	helm, _ := cat.Item("iron-helm")
	if helm.EffectiveMaxStack() != 1 || helm.Equip != "head" {
		t.Fatalf("iron-helm = %+v", helm)
	}
	if cat.Has("sword") {
		t.Fatal("Has(sword) = true")
	}
	ids := []string{}
	for _, d := range cat.Items() {
		ids = append(ids, d.ID)
	}
	if got := strings.Join(ids, ","); got != "potion,arrow,iron-helm,gold-ring,silver-ring,old-key" {
		t.Fatalf("Items order = %s", got)
	}
}

func TestEncodeRoundTripsCanonicalYAML(t *testing.T) {
	raw := readTestdata(t, "items.yaml")
	cat := testCatalog(t)
	out, err := cat.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Fatalf("Encode changed the canonical file:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
	}
	again, err := inventory.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v", err)
	}
	out2, err := again.Encode()
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatal("Encode is not stable across a round trip")
	}
}

func TestEncodeIsDeterministicForBuiltCatalog(t *testing.T) {
	cat, err := inventory.NewCatalog(inventory.ItemDef{ID: "gem", Stats: map[string]float64{"z": 1, "a": 2, "m": 3}})
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nitems:\n  - id: gem\n    stats:\n      a: 2\n      m: 3\n      z: 1\n"
	for range 5 {
		out, err := cat.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != want {
			t.Fatalf("Encode =\n%s\nwant\n%s", out, want)
		}
	}
	empty, err := inventory.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	out, err := empty.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "version: 1\nitems: []\n" {
		t.Fatalf("empty Encode = %q", out)
	}
	if _, err := inventory.Parse(out); err != nil {
		t.Fatalf("Parse empty catalog: %v", err)
	}
}

func TestParseRejectsInvalidCatalogs(t *testing.T) {
	cases := map[string]string{
		"empty":           "  \n",
		"no version":      "items: []\n",
		"future version":  "version: 2\nitems: []\n",
		"unknown field":   "version: 1\nitems:\n  - id: a\n    weight: 3\n",
		"unknown top":     "version: 1\nitems: []\nextra: true\n",
		"empty id":        "version: 1\nitems:\n  - name: x\n",
		"space in id":     "version: 1\nitems:\n  - id: \"a b\"\n",
		"duplicate id":    "version: 1\nitems:\n  - id: a\n  - id: a\n",
		"negative stack":  "version: 1\nitems:\n  - id: a\n    maxStack: -1\n",
		"huge stack":      "version: 1\nitems:\n  - id: a\n    maxStack: 2000000000\n",
		"bad equip":       "version: 1\nitems:\n  - id: a\n    equip: \"left hand\"\n",
		"empty tag":       "version: 1\nitems:\n  - id: a\n    tags: [\"\"]\n",
		"duplicate tag":   "version: 1\nitems:\n  - id: a\n    tags: [x, x]\n",
		"nan stat":        "version: 1\nitems:\n  - id: a\n    stats:\n      dmg: .nan\n",
		"inf stat":        "version: 1\nitems:\n  - id: a\n    stats:\n      dmg: .inf\n",
		"empty stat name": "version: 1\nitems:\n  - id: a\n    stats:\n      \"\": 1\n",
		"trailing doc":    "version: 1\nitems: []\n---\nversion: 1\n",
		"not a mapping":   "- 1\n- 2\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := inventory.Parse([]byte(src)); err == nil {
				t.Fatalf("Parse accepted %q", src)
			}
		})
	}
}

func TestCatalogCopiesDefinitions(t *testing.T) {
	def := inventory.ItemDef{ID: "gem", Tags: []string{"shiny"}, Stats: map[string]float64{"price": 10}}
	cat, err := inventory.NewCatalog(def)
	if err != nil {
		t.Fatal(err)
	}
	def.Tags[0] = "dull"
	def.Stats["price"] = 0
	got, _ := cat.Item("gem")
	if got.Tags[0] != "shiny" || got.Stats["price"] != 10 {
		t.Fatalf("catalog shares caller's slices or maps: %+v", got)
	}
	got.Tags[0] = "dull"
	got.Stats["price"] = 0
	again, _ := cat.Item("gem")
	if again.Tags[0] != "shiny" || again.Stats["price"] != 10 {
		t.Fatalf("Item returned shared slices or maps: %+v", again)
	}
	if _, err := inventory.NewCatalog(inventory.ItemDef{ID: "a"}, inventory.ItemDef{ID: "a"}); err == nil {
		t.Fatal("NewCatalog accepted duplicate ids")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(readTestdata(f, "items.yaml"))
	f.Add([]byte("version: 1\nitems: []\n"))
	f.Add([]byte("version: 1\nitems:\n  - id: a\n    maxStack: 5\n    stats: {x: 1e300}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cat, err := inventory.Parse(data)
		if err != nil {
			return
		}
		out, err := cat.Encode()
		if err != nil {
			t.Fatalf("Encode of a parsed catalog failed: %v", err)
		}
		again, err := inventory.Parse(out)
		if err != nil {
			t.Fatalf("Parse(Encode) failed: %v\n%s", err, out)
		}
		out2, err := again.Encode()
		if err != nil {
			t.Fatalf("second Encode failed: %v", err)
		}
		if !bytes.Equal(out, out2) {
			t.Fatalf("round trip not stable:\n%s\n---\n%s", out, out2)
		}
		if again.Len() != cat.Len() {
			t.Fatalf("round trip changed item count %d -> %d", cat.Len(), again.Len())
		}
	})
}

func TestErrorsWrapSentinels(t *testing.T) {
	cat := testCatalog(t)
	bag, err := inventory.NewContainer(cat, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bag.Add("sword", 1); !errors.Is(err, inventory.ErrUnknownItem) {
		t.Fatalf("Add unknown = %v", err)
	}
	if _, err := bag.Add("potion", 0); !errors.Is(err, inventory.ErrInvalidCount) {
		t.Fatalf("Add 0 = %v", err)
	}
	if err := bag.Move(0, 5); !errors.Is(err, inventory.ErrSlotOutOfRange) {
		t.Fatalf("Move out of range = %v", err)
	}
	if _, err := inventory.NewContainer(cat, 0); err == nil {
		t.Fatal("NewContainer(0) accepted")
	}
	if _, err := inventory.NewContainer(nil, 1); err == nil {
		t.Fatal("NewContainer(nil) accepted")
	}
}
