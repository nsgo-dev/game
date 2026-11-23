package selfhost_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/leaderboard/selfhost"
)

// serverKeyID and serverKey are the public half of seedKey(9).
var serverKeyYAML = func() string {
	pk := mustSigner(9).PublicKey()
	return "serverKeys:\n  - id: " + pk.ID + "\n    key: " + pk.Key + "\n"
}()

var canonicalConfig = `version: 1
proofTTL: 10m
boards:
  - name: high-score
    order: descending
    reset: allTime
  - name: weekly-sprint
    order: ascending
    reset: weekly
    weekStart: sunday
    keep: 4
    validation: signed
    min: 1
    max: 3600000
  - name: daily
    order: descending
    reset: daily
` + serverKeyYAML

func TestConfigRoundTripsByteForByte(t *testing.T) {
	cfg, err := selfhost.ParseConfig([]byte(canonicalConfig))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != canonicalConfig {
		t.Fatalf("Marshal =\n%s\nwant\n%s", out, canonicalConfig)
	}
	b, ok := cfg.Board("weekly-sprint")
	if !ok || b.Order != selfhost.Ascending || b.Reset != selfhost.Weekly || b.Keep != 4 || *b.Min != 1 || *b.Max != 3600000 {
		t.Fatalf("weekly-sprint = %+v", b)
	}
	if _, ok := cfg.Board("missing"); ok {
		t.Fatal("found a board that is not declared")
	}
}

func TestConfigRefusesInvalidFiles(t *testing.T) {
	base := "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n"
	cases := map[string]string{
		"empty":            "",
		"version":          "version: 2\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n",
		"no boards":        "version: 1\nboards: []\n",
		"unknown field":    base + "colour: red\n",
		"unknown in board": "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    shiny: true\n",
		"duplicate key":    "version: 1\nversion: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n",
		"trailing doc":     base + "---\nversion: 1\n",
		"bad name":         "version: 1\nboards:\n  - name: High Score\n    order: descending\n    reset: allTime\n",
		"dot name":         "version: 1\nboards:\n  - name: .hidden\n    order: descending\n    reset: allTime\n",
		"twice":            base + "  - name: b\n    order: ascending\n    reset: daily\n",
		"order":            "version: 1\nboards:\n  - name: b\n    order: up\n    reset: allTime\n",
		"reset":            "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: monthly\n",
		"keep on allTime":  "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    keep: 2\n",
		"keep too big":     "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: daily\n    keep: 1001\n",
		"weekStart daily":  "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: daily\n    weekStart: monday\n",
		"weekStart caps":   "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: weekly\n    weekStart: Monday\n",
		"validation":       "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    validation: none\n",
		"signed no keys":   "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    validation: signed\n",
		"min above max":    "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    min: 5\n    max: 4\n",
		"unsafe max":       "version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n    max: 9007199254740992\n",
		"proofTTL":         "version: 1\nproofTTL: 500ms\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n",
		"proofTTL number":  "version: 1\nproofTTL: 10\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n",
		"key id":           base + "serverKeys:\n  - id: 0000000000000000\n    key: " + mustSigner(9).PublicKey().Key + "\n",
		"key":              base + "serverKeys:\n  - id: x\n    key: notbase64\n",
		"key twice":        base + strings.TrimSuffix(serverKeyYAML, "\n") + "\n" + strings.SplitAfterN(serverKeyYAML, "\n", 2)[1],
	}
	for name, src := range cases {
		if _, err := selfhost.ParseConfig([]byte(src)); err == nil {
			t.Errorf("%s: ParseConfig accepted\n%s", name, src)
		}
	}
	if _, err := selfhost.ParseConfig([]byte(base)); err != nil {
		t.Fatalf("base: %v", err)
	}
}

func FuzzParseConfig(f *testing.F) {
	f.Add([]byte(canonicalConfig))
	f.Add([]byte("version: 1\nboards:\n  - name: b\n    order: descending\n    reset: allTime\n"))
	f.Add([]byte("version: 1\nproofTTL: 1h30m\nboards:\n  - name: w\n    order: ascending\n    reset: weekly\n    keep: 3\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := selfhost.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := cfg.Marshal()
		if err != nil {
			t.Fatalf("a parsed config does not marshal: %v", err)
		}
		again, err := selfhost.ParseConfig(out)
		if err != nil {
			t.Fatalf("a marshalled config does not parse: %v\n%s", err, out)
		}
		out2, err := again.Marshal()
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("marshal is not stable:\n%s\n%s", out, out2)
		}
	})
}
