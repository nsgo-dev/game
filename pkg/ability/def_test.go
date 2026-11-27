package ability_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ability"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func readSample(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/hero.abilities.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseSampleAndRoundTripUnchanged(t *testing.T) {
	data := readSample(t)
	set, err := ability.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Abilities) != 4 {
		t.Fatalf("got %d abilities, want 4", len(set.Abilities))
	}
	fb := set.Abilities[0]
	if fb.ID != "fireball" || fb.Action != "cast" || fb.Cooldown != 2*time.Second || fb.CastTime != 500*time.Millisecond {
		t.Fatalf("fireball = %+v", fb)
	}
	if !reflect.DeepEqual(fb.Conditions, []string{"grounded", "!silenced"}) {
		t.Fatalf("fireball conditions = %q", fb.Conditions)
	}
	drain := set.Abilities[2]
	if drain.EffectiveTrigger() != ability.TriggerHold || drain.Costs[1] != (ability.Cost{Resource: "stamina", Amount: 2.5}) {
		t.Fatalf("drain = %+v", drain)
	}
	if set.Abilities[3].MaxChargesOrOne() != 1 || set.Abilities[3].EffectiveTrigger() != ability.TriggerPress {
		t.Fatalf("potion defaults = %+v", set.Abilities[3])
	}

	out, err := ability.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("Encode(Parse(sample)) changed the file:\n--- got\n%s--- want\n%s", out, data)
	}
	again, err := ability.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, set) {
		t.Fatalf("Parse(Encode(set)) = %+v, want %+v", again, set)
	}
}

func TestEncodeIsCanonicalForOtherSpellings(t *testing.T) {
	set, err := ability.Parse([]byte("version: 1\nabilities:\n  - {id: a, cooldown: 1500ms, castTime: 0.25s}\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ability.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nabilities:\n  - id: a\n    cooldown: 1.5s\n    castTime: 250ms\n"
	if string(out) != want {
		t.Fatalf("Encode = %q, want %q", out, want)
	}
	if out2, _ := ability.Encode(set); !bytes.Equal(out, out2) {
		t.Fatal("Encode is not deterministic")
	}
}

func TestParseRefusesInvalidSets(t *testing.T) {
	cases := []struct {
		name, doc, want string
	}{
		{"empty", "  \n", "empty"},
		{"version", "version: 2\nabilities: []\n", "unsupported version 2"},
		{"unknown field", "version: 1\nabilities:\n  - id: a\n    colldown: 1s\n", `unknown field "colldown"`},
		{"unknown top field", "version: 1\nabilites: []\n", `unknown field "abilites"`},
		{"duplicate key", "version: 1\nabilities:\n  - id: a\n    id: b\n", "already defined"},
		{"trailing document", "version: 1\nabilities: []\n---\nversion: 1\n", "trailing"},
		{"duplicate id", "version: 1\nabilities:\n  - id: a\n  - id: a\n", `duplicate ability id "a"`},
		{"empty id", "version: 1\nabilities:\n  - name: x\n", "id: is empty"},
		{"bad id", "version: 1\nabilities:\n  - id: 'a b'\n", "use ASCII letters"},
		{"long id", "version: 1\nabilities:\n  - id: " + strings.Repeat("a", 65) + "\n", "more than 64"},
		{"bad action", "version: 1\nabilities:\n  - id: a\n    action: 'x y'\n", "action"},
		{"bad trigger", "version: 1\nabilities:\n  - id: a\n    action: x\n    trigger: tap\n", `trigger "tap"`},
		{"hold without action", "version: 1\nabilities:\n  - id: a\n    trigger: hold\n", "needs an action"},
		{"negative duration", "version: 1\nabilities:\n  - id: a\n    cooldown: -1s\n", "cooldown -1s"},
		{"huge duration", "version: 1\nabilities:\n  - id: a\n    castTime: 25h\n", "castTime 25h0m0s"},
		{"integer duration", "version: 1\nabilities:\n  - id: a\n    cooldown: 2\n", "cannot use !!int"},
		{"charges without recharge", "version: 1\nabilities:\n  - id: a\n    charges: 2\n", "need a recharge"},
		{"negative charges", "version: 1\nabilities:\n  - id: a\n    charges: -1\n", "charges -1"},
		{"too many charges", "version: 1\nabilities:\n  - id: a\n    charges: 100\n    recharge: 1s\n", "charges 100"},
		{"tick without channel", "version: 1\nabilities:\n  - id: a\n    tickInterval: 1s\n", "needs a channelTime"},
		{"tick longer than channel", "version: 1\nabilities:\n  - id: a\n    channelTime: 1s\n    tickInterval: 2s\n", "longer than channelTime"},
		{"zero cost", "version: 1\nabilities:\n  - id: a\n    costs: [{resource: mana, amount: 0}]\n", "finite positive"},
		{"nan cost", "version: 1\nabilities:\n  - id: a\n    costs: [{resource: mana, amount: .nan}]\n", "finite positive"},
		{"duplicate cost", "version: 1\nabilities:\n  - id: a\n    costs: [{resource: mana, amount: 1}, {resource: mana, amount: 2}]\n", "costed twice"},
		{"empty resource", "version: 1\nabilities:\n  - id: a\n    costs: [{amount: 1}]\n", "resource: is empty"},
		{"bad condition", "version: 1\nabilities:\n  - id: a\n    conditions: ['!']\n", `condition "!"`},
		{"duplicate condition", "version: 1\nabilities:\n  - id: a\n    conditions: [x, '!x']\n", "appears twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ability.Parse([]byte(tc.doc))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateLimits(t *testing.T) {
	if err := ability.Validate(nil); err == nil {
		t.Fatal("Validate(nil) succeeded")
	}
	set := &ability.Set{Version: 1, Abilities: make([]ability.Def, ability.MaxAbilities+1)}
	if err := ability.Validate(set); err == nil || !strings.Contains(err.Error(), "more than 1024") {
		t.Fatalf("Validate(too many) = %v", err)
	}
	if _, err := ability.Encode(&ability.Set{Version: 0}); err == nil {
		t.Fatal("Encode accepted an invalid set")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(readSample(f))
	f.Add([]byte("version: 1\nabilities: []\n"))
	f.Add([]byte("version: 1\nabilities:\n  - {id: a, charges: 99, recharge: 24h, channelTime: 1ns, tickInterval: 1ns}\n"))
	f.Add([]byte("version: 1\nabilities:\n  - id: a\n    costs: [{resource: r, amount: 1e308}]\n"))
	f.Add([]byte("a: &a [*a]"))
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, data []byte) {
		set, err := ability.Parse(data)
		if err != nil {
			return
		}
		out, err := ability.Encode(set)
		if err != nil {
			t.Fatalf("Encode of a parsed set failed: %v", err)
		}
		again, err := ability.Parse(out)
		if err != nil {
			t.Fatalf("Parse of encoded set failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, set) {
			t.Fatalf("round trip changed the set:\n%+v\n%+v", set, again)
		}
		out2, err := ability.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("second encode differs (%v):\n%s\n%s", err, out, out2)
		}
		if _, err := ability.NewCaster(set, 0); err != nil {
			t.Fatalf("NewCaster refused a valid set: %v", err)
		}
	})
}
