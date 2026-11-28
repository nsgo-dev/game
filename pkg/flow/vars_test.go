package flow_test

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/flow"
)

const sampleVarsYAML = `version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: True once the east bridge is fixed.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
    doc: Player gold coins.
  - name: affinity
    kind: float
    default: 0.5
    min: 0
    max: 1
    doc: Relationship score with Mira.
  - name: hero_name
    kind: string
    default: Aria
    doc: Protagonist name.
`

func TestParseVarsLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		field string
		want  error
	}{
		{
			name:  "empty document",
			yaml:  "   \n",
			field: "",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "wrong version",
			yaml:  "version: 2\nvars: []\n",
			field: "version",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "unknown top-level field",
			yaml:  "version: 1\nbogus: true\n",
			field: "bogus",
		},
		{
			name:  "unknown var field",
			yaml:  "version: 1\nvars:\n  - name: ok\n    kind: bool\n    extra: 1\n",
			field: "vars[0].extra",
		},
		{
			name:  "leading dollar in name",
			yaml:  "version: 1\nvars:\n  - name: $gold\n    kind: int\n",
			field: "vars[0].name",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "duplicate name",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: int\n  - name: gold\n    kind: int\n",
			field: "vars[1].name",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "unknown kind",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: list\n",
			field: "vars[0].kind",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "bool default mismatch",
			yaml:  "version: 1\nvars:\n  - name: flag\n    kind: bool\n    default: 1\n",
			field: "vars[0].default",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "int fractional default",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: int\n    default: 1.5\n",
			field: "vars[0].default",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "string default mismatch",
			yaml:  "version: 1\nvars:\n  - name: name\n    kind: string\n    default: 42\n",
			field: "vars[0].default",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "min on bool",
			yaml:  "version: 1\nvars:\n  - name: flag\n    kind: bool\n    min: 0\n",
			field: "vars[0].min",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "max on string",
			yaml:  "version: 1\nvars:\n  - name: name\n    kind: string\n    max: 10\n",
			field: "vars[0].max",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "fractional int min",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: int\n    min: 0.5\n",
			field: "vars[0].min",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "min greater than max",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: int\n    min: 10\n    max: 5\n",
			field: "vars[0].max",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "default below min",
			yaml:  "version: 1\nvars:\n  - name: gold\n    kind: int\n    default: 5\n    min: 10\n",
			field: "vars[0].default",
			want:  flow.ErrInvalidVars,
		},
		{
			name:  "omitted default outside min",
			yaml:  "version: 1\nvars:\n  - name: level\n    kind: int\n    min: 1\n",
			field: "vars[0]",
			want:  flow.ErrInvalidVars,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{"vars.yaml": {Data: []byte(tc.yaml)}}
			_, err := flow.LoadVarsFS(fsys, "vars.yaml")
			if tc.field != "" {
				loaderrtest.ExpectField(t, err, "vars.yaml", []byte(tc.yaml), tc.field)
			} else {
				loaderrtest.Expect(t, err, "vars.yaml", 1)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}

	t.Run("trailing document refused", func(t *testing.T) {
		raw := "version: 1\nvars: []\n---\nversion: 1\n"
		_, err := flow.ParseVars([]byte(raw))
		if !errors.Is(err, flow.ErrInvalidVars) {
			t.Fatalf("ParseVars trailing doc err = %v, want ErrInvalidVars", err)
		}
	})
}

func TestVarsTypedAccessAndBounds(t *testing.T) {
	doc, err := flow.ParseVars([]byte(sampleVarsYAML))
	if err != nil {
		t.Fatalf("ParseVars: %v", err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatalf("NewVars: %v", err)
	}

	if got := vars.Names(); !slices.Equal(got, []string{"bridge_repaired", "gold", "affinity", "hero_name"}) {
		t.Fatalf("Names() = %v", got)
	}
	if !vars.Has("gold") || !vars.Has("$gold") || vars.Has("missing") {
		t.Fatalf("Has mismatch")
	}

	// Defaults.
	if b, err := vars.Bool("bridge_repaired"); err != nil || b {
		t.Fatalf("Bool(bridge_repaired) = %v, %v", b, err)
	}
	if g, err := vars.Int("$gold"); err != nil || g != 50 {
		t.Fatalf("Int($gold) = %v, %v", g, err)
	}
	if a, err := vars.Float("affinity"); err != nil || a != 0.5 {
		t.Fatalf("Float(affinity) = %v, %v", a, err)
	}
	if n, err := vars.String("hero_name"); err != nil || n != "Aria" {
		t.Fatalf("String(hero_name) = %v, %v", n, err)
	}
	if val, ok := vars.Lookup("$gold"); !ok || val != 50 {
		t.Fatalf("Lookup($gold) = %v, %v", val, ok)
	}

	// Declared kinds enforced on getters and setters.
	if _, err := vars.Int("bridge_repaired"); !errors.Is(err, flow.ErrVarTypeMismatch) {
		t.Fatalf("Int on bool err = %v, want ErrVarTypeMismatch", err)
	}
	if _, err := vars.Bool("unknown"); !errors.Is(err, flow.ErrUnknownVar) {
		t.Fatalf("Bool on unknown err = %v, want ErrUnknownVar", err)
	}
	if err := vars.Set("gold", "50"); !errors.Is(err, flow.ErrVarTypeMismatch) {
		t.Fatalf("Set string to int err = %v, want ErrVarTypeMismatch", err)
	}
	if err := vars.Set("gold", 12.5); !errors.Is(err, flow.ErrVarTypeMismatch) {
		t.Fatalf("Set float to int err = %v, want ErrVarTypeMismatch", err)
	}
	if err := vars.Set("bridge_repaired", 1); !errors.Is(err, flow.ErrVarTypeMismatch) {
		t.Fatalf("Set int to bool err = %v, want ErrVarTypeMismatch", err)
	}
	if err := vars.Set("affinity", math.NaN()); !errors.Is(err, flow.ErrVarOutOfRange) {
		t.Fatalf("Set NaN err = %v, want ErrVarOutOfRange", err)
	}

	// Bounds enforced on setters.
	if err := vars.SetInt("gold", -1); !errors.Is(err, flow.ErrVarOutOfRange) {
		t.Fatalf("SetInt(-1) err = %v, want ErrVarOutOfRange", err)
	}
	if err := vars.SetInt("gold", 10000); !errors.Is(err, flow.ErrVarOutOfRange) {
		t.Fatalf("SetInt(10000) err = %v, want ErrVarOutOfRange", err)
	}
	if err := vars.SetFloat("affinity", 1.5); !errors.Is(err, flow.ErrVarOutOfRange) {
		t.Fatalf("SetFloat(1.5) err = %v, want ErrVarOutOfRange", err)
	}
	// Value must remain unchanged after failed sets.
	if g, _ := vars.Int("gold"); g != 50 {
		t.Fatalf("gold mutated on error: got %d, want 50", g)
	}

	// SetClamped clamps out-of-range values.
	oldV, newV, clamped, err := vars.SetClamped("gold", 12000)
	if err != nil || !clamped || oldV != 50 || newV != 9999 {
		t.Fatalf("SetClamped = (%v, %v, %v, %v), want (50, 9999, true, nil)", oldV, newV, clamped, err)
	}
}

func TestVarsOnChangeOrderAndUnsubscribe(t *testing.T) {
	doc, err := flow.ParseVars([]byte(sampleVarsYAML))
	if err != nil {
		t.Fatal(err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}

	var log []string
	var unsubSecond func()

	unsubFirst := vars.OnChange("gold", func(ch flow.VarChange) {
		log = append(log, "first")
		// Unsubscribing another listener during delivery must not skip it in this notification.
		unsubSecond()
	})
	unsubAny := vars.OnChange("", func(ch flow.VarChange) {
		log = append(log, "any:"+ch.Name)
	})
	unsubSecond = vars.OnChange("$gold", func(ch flow.VarChange) {
		log = append(log, "second")
	})

	// Setting same value must not fire listeners.
	if err := vars.SetInt("gold", 50); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Fatalf("no-op set fired listeners: %v", log)
	}

	// First change fires all three in exact registration order.
	if err := vars.SetInt("gold", 75); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "any:gold", "second"}
	if !slices.Equal(log, want) {
		t.Fatalf("first change log = %v, want %v", log, want)
	}

	// Second change skips the unsubscribed second listener.
	log = nil
	if err := vars.SetInt("gold", 100); err != nil {
		t.Fatal(err)
	}
	want = []string{"first", "any:gold"}
	if !slices.Equal(log, want) {
		t.Fatalf("second change log = %v, want %v", log, want)
	}

	unsubFirst()
	unsubFirst() // idempotent
	unsubAny()
	log = nil
	if err := vars.SetInt("gold", 120); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Fatalf("after unsub all, log = %v", log)
	}
}

func TestVarsSelfAndSnapshotRoundTrip(t *testing.T) {
	doc, err := flow.ParseVars([]byte(sampleVarsYAML))
	if err != nil {
		t.Fatal(err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}

	if err := vars.SetBool("bridge_repaired", true); err != nil {
		t.Fatal(err)
	}
	if err := vars.SetInt("gold", 420); err != nil {
		t.Fatal(err)
	}
	if err := vars.SetFloat("affinity", 0.75); err != nil {
		t.Fatal(err)
	}
	if err := vars.SetString("hero_name", "Mira"); err != nil {
		t.Fatal(err)
	}

	chest := vars.Self("chest_01")
	if chest.Bool("opened") {
		t.Fatal("unset self switch should be false")
	}
	chest.SetBool("opened", true)
	chest.SetInt("uses", 2)
	vars.Self("door_east").SetString("state", "unlocked")

	if got := vars.SelfObjects(); !slices.Equal(got, []string{"chest_01", "door_east"}) {
		t.Fatalf("SelfObjects = %v", got)
	}

	snap := vars.Snapshot()

	// JSON round trip (simulating save.Store).
	rawJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decodedJSON flow.VarsSnapshot
	if err := json.Unmarshal(rawJSON, &decodedJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// YAML round trip.
	rawYAML, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var decodedYAML flow.VarsSnapshot
	if err := yaml.Unmarshal(rawYAML, &decodedYAML); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	for _, tc := range []struct {
		name string
		snap flow.VarsSnapshot
	}{
		{"direct", snap},
		{"json", decodedJSON},
		{"yaml", decodedYAML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restored, err := flow.NewVars(doc)
			if err != nil {
				t.Fatal(err)
			}
			fired := 0
			restored.OnChange("", func(flow.VarChange) { fired++ })
			if err := restored.Restore(tc.snap); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if fired != 0 {
				t.Fatalf("Restore fired %d OnChange notifications, want 0", fired)
			}
			if b, _ := restored.Bool("bridge_repaired"); !b {
				t.Errorf("bridge_repaired = false, want true")
			}
			if g, _ := restored.Int("gold"); g != 420 {
				t.Errorf("gold = %d, want 420", g)
			}
			if a, _ := restored.Float("affinity"); a != 0.75 {
				t.Errorf("affinity = %g, want 0.75", a)
			}
			if n, _ := restored.String("hero_name"); n != "Mira" {
				t.Errorf("hero_name = %q, want Mira", n)
			}
			if !restored.Self("chest_01").Bool("opened") || restored.Self("chest_01").Int("uses") != 2 {
				t.Errorf("chest_01 self vars = %+v", restored.Self("chest_01").Map())
			}
			if got := restored.Self("door_east").String("state"); got != "unlocked" {
				t.Errorf("door_east.state = %q, want unlocked", got)
			}
		})
	}

	// Invalid snapshots are refused without mutating state.
	badSnaps := []flow.VarsSnapshot{
		{Version: 99},
		{Version: 1, Vars: map[string]any{"no_such_var": true}},
		{Version: 1, Vars: map[string]any{"gold": -5}},
		{Version: 1, Vars: map[string]any{"gold": 12.5}},
		{Version: 1, Vars: map[string]any{"bridge_repaired": "yes"}},
	}
	for i, bad := range badSnaps {
		if err := vars.Restore(bad); !errors.Is(err, flow.ErrInvalidSnapshot) {
			t.Errorf("bad[%d]: err = %v, want ErrInvalidSnapshot", i, err)
		}
	}
	if g, _ := vars.Int("gold"); g != 420 {
		t.Fatalf("gold mutated after invalid restores: got %d, want 420", g)
	}

	// Zero snapshot resets to defaults and clears self vars.
	if err := vars.Restore(flow.VarsSnapshot{}); err != nil {
		t.Fatalf("zero Restore: %v", err)
	}
	if g, _ := vars.Int("gold"); g != 50 {
		t.Fatalf("after zero Restore gold = %d, want 50", g)
	}
	if len(vars.SelfObjects()) != 0 {
		t.Fatalf("after zero Restore SelfObjects = %v, want empty", vars.SelfObjects())
	}
}
