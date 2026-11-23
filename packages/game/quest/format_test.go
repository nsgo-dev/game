package quest_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/quest"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func loadVillage(t testing.TB) *quest.Book {
	t.Helper()
	b, err := quest.LoadFS(os.DirFS("testdata"), "village.yaml")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return b
}

func TestParseVillage(t *testing.T) {
	b := loadVillage(t)
	if len(b.Quests) != 3 {
		t.Fatalf("quests = %d, want 3", len(b.Quests))
	}
	rats := b.Quest("rat-problem")
	if rats == nil || !rats.Ordered || len(rats.Objectives) != 5 || rats.Requires[0] != "welcome" {
		t.Fatalf("rat-problem = %+v", rats)
	}
	if got := rats.Objectives[2]; got.Kind != quest.KindDefeat || got.Target != "rat" || got.Required() != 5 {
		t.Fatalf("kill-rats = %+v", got)
	}
	if got := rats.Rewards[1]; got.ID != "cellar-key" || got.Quantity() != 1 {
		t.Fatalf("reward = %+v", got)
	}
	if b.Quest("missing") != nil {
		t.Fatal("Quest(missing) != nil")
	}
}

func TestEncodeRoundTripIsLossless(t *testing.T) {
	src, err := os.ReadFile("testdata/village.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := quest.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := quest.Encode(b)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Fatalf("Encode(Parse(file)) differs from file:\n%s", out)
	}
	again, err := quest.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v", err)
	}
	if !reflect.DeepEqual(again, b) {
		t.Fatalf("round trip changed the book:\n%+v\n%+v", again, b)
	}
	out2, err := quest.Encode(again)
	if err != nil || !bytes.Equal(out, out2) {
		t.Fatalf("second Encode differs (err %v)", err)
	}
}

func TestEncodeWritesCanonicalForm(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"empty quests": {"version: 1\nquests: []\n", "version: 1\n"},
		"empty lists": {
			"version: 1\nquests:\n  - id: q\n    requires: []\n    objectives:\n      - {id: a, kind: talk, target: npc}\n    rewards: []\n",
			"version: 1\nquests:\n  - id: q\n    objectives:\n      - id: a\n        kind: talk\n        target: npc\n",
		},
		"comments": {
			"# designer note\nversion: 1\nquests:\n  - id: q # main\n    objectives:\n      - id: a\n        kind: talk\n        target: npc\n",
			"version: 1\nquests:\n  - id: q\n    objectives:\n      - id: a\n        kind: talk\n        target: npc\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := quest.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := quest.Encode(b)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(out) != c.want {
				t.Fatalf("Encode =\n%s\nwant\n%s", out, c.want)
			}
			again, err := quest.Parse(out)
			if err != nil {
				t.Fatalf("Parse(Encode): %v", err)
			}
			if !reflect.DeepEqual(again, b) {
				t.Fatalf("Parse(Encode(b)) = %#v, want %#v", again, b)
			}
		})
	}
}

func TestParseRefusesInvalidBooks(t *testing.T) {
	const ok = "    objectives:\n      - {id: a, kind: talk, target: npc}\n"
	cases := map[string]string{
		"empty":             "",
		"version":           "version: 2\nquests: []\n",
		"no version":        "quests: []\n",
		"unknown field":     "version: 1\nquests:\n  - id: q\n    colour: red\n" + ok,
		"unknown obj key":   "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: talk, target: npc, where: x}\n",
		"trailing doc":      "version: 1\n---\nversion: 1\n",
		"empty id":          "version: 1\nquests:\n  - id: ''\n" + ok,
		"spaced id":         "version: 1\nquests:\n  - id: ' q'\n" + ok,
		"duplicate quest":   "version: 1\nquests:\n  - id: q\n" + ok + "  - id: q\n" + ok,
		"no objectives":     "version: 1\nquests:\n  - id: q\n    objectives: []\n",
		"only optional":     "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: talk, target: npc, optional: true}\n",
		"bad kind":          "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: dance, target: npc}\n",
		"no target":         "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: talk}\n",
		"negative count":    "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: defeat, target: rat, count: -1}\n",
		"duplicate obj":     "version: 1\nquests:\n  - id: q\n    objectives:\n      - {id: a, kind: talk, target: npc}\n      - {id: a, kind: talk, target: npc}\n",
		"unknown require":   "version: 1\nquests:\n  - id: q\n    requires: [nope]\n" + ok,
		"self require":      "version: 1\nquests:\n  - id: q\n    requires: [q]\n" + ok,
		"duplicate require": "version: 1\nquests:\n  - id: p\n" + ok + "  - id: q\n    requires: [p, p]\n" + ok,
		"cycle":             "version: 1\nquests:\n  - id: a\n    requires: [b]\n" + ok + "  - id: b\n    requires: [a]\n" + ok,
		"reward id":         "version: 1\nquests:\n  - id: q\n" + ok + "    rewards:\n      - {kind: gold}\n",
		"negative reward":   "version: 1\nquests:\n  - id: q\n" + ok + "    rewards:\n      - {id: coin, amount: -3}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := quest.Parse([]byte(src))
			if !errors.Is(err, quest.ErrInvalidBook) {
				t.Fatalf("Parse error = %v, want ErrInvalidBook", err)
			}
		})
	}
}

func TestEncodeRefusesInvalidBook(t *testing.T) {
	if _, err := quest.Encode(&quest.Book{Version: 1, Quests: []quest.Quest{{ID: "q"}}}); !errors.Is(err, quest.ErrInvalidBook) {
		t.Fatalf("Encode error = %v, want ErrInvalidBook", err)
	}
	if _, err := quest.Encode(nil); !errors.Is(err, quest.ErrInvalidBook) {
		t.Fatalf("Encode(nil) error = %v, want ErrInvalidBook", err)
	}
}

func TestLoadFSReportsName(t *testing.T) {
	_, err := quest.LoadFS(os.DirFS("testdata"), "missing.yaml")
	if err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("LoadFS(missing) error = %v", err)
	}
}

func FuzzParse(f *testing.F) {
	src, err := os.ReadFile("testdata/village.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(src)
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nquests:\n  - id: a\n    requires: [a]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := quest.Parse(data)
		if err != nil {
			return
		}
		out, err := quest.Encode(b)
		if err != nil {
			t.Fatalf("Encode of a parsed book: %v", err)
		}
		again, err := quest.Parse(out)
		if err != nil {
			t.Fatalf("Parse(Encode(b)): %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, b) {
			t.Fatalf("Parse(Encode(b)) differs from b:\n%#v\n%#v", again, b)
		}
		out2, err := quest.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable (err %v):\n%s\n%s", err, out, out2)
		}
		if _, err := quest.NewLog(b); err != nil {
			t.Fatalf("NewLog of a parsed book: %v", err)
		}
	})
}
