package jsonedit

import (
	"strings"
	"testing"
	"time"
)

type inner struct {
	A int `json:"a"`
	B int `json:"b"`
}

type cache struct {
	hits int
}

type component struct {
	State   string         `json:"state"`
	Speed   float64        `json:"speed"`
	Mods    map[string]int `json:"mods,omitempty"`
	List    []int          `json:"list,omitempty"`
	Target  *inner         `json:"target,omitempty"`
	Handle  *cache         `json:"-"`
	Runtime map[string]int `json:"-"`
	OnHit   func()         `json:"-"`
	Hook    func()
	At      time.Time `json:"at"`
	Nested  inner     `json:"nested"`
	private *cache
	note    string
}

func newComponent() *component {
	return &component{
		State:   "idle",
		Speed:   1,
		Mods:    map[string]int{"acid": 1},
		List:    []int{1, 2, 3},
		Target:  &inner{A: 1, B: 2},
		Handle:  &cache{hits: 3},
		Runtime: map[string]int{"frames": 9},
		OnHit:   func() {},
		Hook:    func() {},
		At:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Nested:  inner{A: 5, B: 6},
		private: &cache{hits: 7},
		note:    "kept",
	}
}

func TestPatchKeepsUnchangedFieldsAndIdentity(t *testing.T) {
	cur := newComponent()
	got, err := Patch(cur, []byte(`{"state":"chase","nested":{"a":50}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "chase" || got.Speed != 1 || got.Nested != (inner{A: 50, B: 6}) {
		t.Fatalf("patched exported fields = %+v", got)
	}
	if got.private != cur.private || got.note != "kept" {
		t.Fatal("unexported fields were not preserved")
	}
	if got.Handle != cur.Handle || got.Runtime == nil || got.Runtime["frames"] != 9 || got.OnHit == nil || got.Hook == nil {
		t.Fatal(`json:"-" fields were not preserved`)
	}
	if got.Target != cur.Target {
		t.Fatal("unchanged pointer field lost its identity")
	}
	got.Mods["new"] = 1
	if _, shared := cur.Mods["new"]; !shared {
		t.Fatal("unchanged map field lost its identity")
	}
	if !got.At.Equal(cur.At) {
		t.Fatalf("time field = %v, want %v", got.At, cur.At)
	}
	if cur.State != "idle" || cur.Nested.A != 5 {
		t.Fatalf("Patch mutated *cur: %+v", cur)
	}
}

func TestPatchChangedContainersAreFresh(t *testing.T) {
	cur := newComponent()
	got, err := Patch(cur, []byte(`{"mods":{"fire":2},"list":[9],"target":{"a":10},"at":"2027-01-01T00:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Mods["acid"] != 1 || got.Mods["fire"] != 2 {
		t.Fatalf("patched map = %v, want acid and fire", got.Mods)
	}
	if len(cur.Mods) != 1 || cur.Mods["fire"] != 0 {
		t.Fatalf("Patch mutated the live map: %v", cur.Mods)
	}
	if len(got.List) != 1 || got.List[0] != 9 || cur.List[0] != 1 {
		t.Fatalf("list = %v, live list = %v", got.List, cur.List)
	}
	if got.Target == cur.Target || *got.Target != (inner{A: 10, B: 2}) || *cur.Target != (inner{A: 1, B: 2}) {
		t.Fatalf("target = %+v, live target = %+v", got.Target, cur.Target)
	}
	if got.At.Year() != 2027 || cur.At.Year() != 2026 {
		t.Fatalf("time = %v, live time = %v", got.At, cur.At)
	}
}

func TestPatchFailureLeavesLiveValueUntouched(t *testing.T) {
	cur := newComponent()
	for _, patch := range []string{
		`{"mods":{"poison":9},"bogus":1}`,
		`{"list":[7,7,7],"target":{"a":99},"bogus":1}`,
		`{"target":{"a":99}} {"state":"x"}`,
		`{"speed":"fast"}`,
	} {
		if _, err := Patch(cur, []byte(patch)); err == nil {
			t.Fatalf("Patch(%s) succeeded, want error", patch)
		}
	}
	if len(cur.Mods) != 1 || cur.Mods["acid"] != 1 {
		t.Fatalf("failed patches mutated the live map: %v", cur.Mods)
	}
	if cur.List[0] != 1 || cur.List[1] != 2 || cur.List[2] != 3 {
		t.Fatalf("failed patches mutated the live slice: %v", cur.List)
	}
	if *cur.Target != (inner{A: 1, B: 2}) {
		t.Fatalf("failed patches mutated the live pointee: %+v", *cur.Target)
	}
}

func TestPatchNonStructAndCycles(t *testing.T) {
	m := map[string]int{"a": 1}
	got, err := Patch(&m, []byte(`{"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 || got["b"] != 2 || len(m) != 1 {
		t.Fatalf("map patch = %v, live = %v", got, m)
	}

	type node struct {
		Name string `json:"name"`
		Next *node  `json:"-"`
	}
	n := &node{Name: "loop"}
	n.Next = n
	patched, err := Patch(n, []byte(`{"name":"renamed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Name != "renamed" || patched.Next != n || n.Name != "loop" {
		t.Fatalf("cyclic patch = %+v", patched)
	}
}

func TestDecodeStrict(t *testing.T) {
	var v inner
	if err := DecodeStrict([]byte(`{"a":1}`), &v); err != nil || v.A != 1 {
		t.Fatalf("DecodeStrict = %v, %+v", err, v)
	}
	if err := DecodeStrict([]byte(`{"c":1}`), &v); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field = %v", err)
	}
	if err := DecodeStrict([]byte(`{"a":1} 2`), &v); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing data = %v", err)
	}
}
