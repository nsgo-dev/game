package procgen_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/procgen"
	"github.com/nsgo-dev/game/pkg/random"
)

func TestRulesRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/ruins.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := procgen.ParseRules(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tiles) != 3 || len(r.Neighbors) != 12 || r.Index("water") != 2 || r.Index("lava") != -1 {
		t.Fatalf("parsed %+v", r)
	}
	if !r.Walkable(0) || r.Walkable(1) || r.Walkable(99) {
		t.Fatal("Walkable")
	}
	out, err := procgen.EncodeRules(r)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := procgen.ParseRules(out)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := procgen.EncodeRules(r2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("encode is not stable:\n%s\n---\n%s", out, out2)
	}
	want := `version: 1
tiles:
  - name: floor
    weight: 6
    walkable: true
  - name: pillar
    weight: 1
`
	if !strings.HasPrefix(string(out), want) {
		t.Fatalf("encoded rules:\n%s\nwant prefix:\n%s", out, want)
	}
	if !strings.Contains(string(out), "  - left: floor\n    right: pillar\n") || !strings.Contains(string(out), "  - up: water\n    down: water\n") {
		t.Fatalf("encoded neighbors:\n%s", out)
	}
	// The re-encoded file drives WFC to the same level as the original.
	cfg := procgen.WFCConfig{Width: 16, Height: 12, Rules: r}
	a, err := procgen.WFC(random.New(4), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Rules = r2
	b, err := procgen.WFC(random.New(4), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equal(b) {
		t.Fatal("round-tripped rules give a different level")
	}
}

func TestParseRulesRejects(t *testing.T) {
	const tiles = "version: 1\ntiles:\n  - {name: a, weight: 1}\n  - {name: b, weight: 2}\n"
	for name, doc := range map[string]string{
		"empty":          "  \n",
		"version":        "version: 2\ntiles:\n  - {name: a, weight: 1}\n",
		"no version":     "tiles:\n  - {name: a, weight: 1}\n",
		"unknown field":  tiles + "seed: 4\n",
		"unknown tile":   "version: 1\ntiles:\n  - {name: a, weight: 1, color: red}\n",
		"no tiles":       "version: 1\n",
		"unnamed":        "version: 1\ntiles:\n  - {weight: 1}\n",
		"duplicate":      "version: 1\ntiles:\n  - {name: a, weight: 1}\n  - {name: a, weight: 1}\n",
		"zero weight":    "version: 1\ntiles:\n  - {name: a, weight: 0}\n",
		"no weight":      "version: 1\ntiles:\n  - {name: a}\n",
		"inf weight":     "version: 1\ntiles:\n  - {name: a, weight: .inf}\n",
		"nan weight":     "version: 1\ntiles:\n  - {name: a, weight: .nan}\n",
		"weight sum":     "version: 1\ntiles:\n  - {name: a, weight: 1e308}\n  - {name: b, weight: 1e308}\n",
		"mixed pair":     tiles + "neighbors:\n  - {left: a, down: b}\n",
		"half pair":      tiles + "neighbors:\n  - {left: a}\n",
		"empty pair":     tiles + "neighbors:\n  - {}\n",
		"unknown name":   tiles + "neighbors:\n  - {up: a, down: c}\n",
		"repeated pair":  tiles + "neighbors:\n  - {left: a, right: b}\n  - {left: a, right: b}\n",
		"trailing doc":   tiles + "---\nversion: 1\n",
		"duplicate key":  "version: 1\nversion: 1\ntiles:\n  - {name: a, weight: 1}\n",
		"wrong type":     "version: one\n",
		"tiles not list": "version: 1\ntiles: a\n",
	} {
		if _, err := procgen.ParseRules([]byte(doc)); err == nil {
			t.Errorf("%s: ParseRules accepted:\n%s", name, doc)
		}
	}
}

func TestEncodeRulesValidates(t *testing.T) {
	if _, err := procgen.EncodeRules(&procgen.Rules{Version: 1}); err == nil {
		t.Fatal("EncodeRules accepted rules with no tiles")
	}
}
