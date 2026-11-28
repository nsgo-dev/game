package tags_test

import (
	"bytes"
	"github.com/nsgo-dev/game/pkg/tags"
	"reflect"
	"strings"
	"testing"
)

func sample() []byte {
	return []byte("version: 1\ntags:\n  - name: status\n    description: broad state\n  - name: status.debuff.stun\n  - name: weapon.fire\n")
}

func TestParseEncodeRoundTrip(t *testing.T) {
	set, err := tags.Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	out, err := tags.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, sample()) {
		t.Fatalf("output changed:\n%s", out)
	}
	again, err := tags.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set, again) {
		t.Fatal("round trip changed set")
	}
	r, err := tags.New(set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Resolve("status.debuff"); !ok {
		t.Fatal("implied parent did not resolve")
	}
}

func TestContainerParentAndReferenceCount(t *testing.T) {
	r, err := tags.NewRegistry(tags.Def{Name: "status"}, tags.Def{Name: "status.debuff.stun"})
	if err != nil {
		t.Fatal(err)
	}
	parent := r.MustResolve("status")
	child := r.MustResolve("status.debuff.stun")
	c := tags.NewContainer(r)
	c.Add(child)
	c.Add(child)
	if !c.Has(parent) || !c.HasExact(child) || c.Count(child) != 2 {
		t.Fatal("grant missing")
	}
	c.Remove(child)
	if !c.Has(child) {
		t.Fatal("reference count removed too early")
	}
	c.Remove(child)
	if c.Has(child) {
		t.Fatal("grant remained")
	}
}

func TestQueries(t *testing.T) {
	r, err := tags.NewRegistry(tags.Def{Name: "status"}, tags.Def{Name: "status.buff"}, tags.Def{Name: "weapon.fire"})
	if err != nil {
		t.Fatal(err)
	}
	c := tags.NewContainer(r)
	c.Add(r.MustResolve("status.buff"))
	q, err := r.QueryNames(tags.QueryNames{All: []string{"status"}, None: []string{"weapon.fire"}})
	if err != nil || !q.Match(c) {
		t.Fatalf("query = %#v, err %v", q, err)
	}
	any, _ := r.QueryNames(tags.QueryNames{Any: []string{"weapon.fire", "status"}})
	if !any.Match(c) {
		t.Fatal("any query did not match")
	}
	_, err = r.QueryNames(tags.QueryNames{All: []string{"missing"}})
	if err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatal("unknown query accepted")
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, tc := range []struct{ doc, want string }{{"version: 2\ntags: []\n", "unsupported version"}, {"version: 1\ntags:\n- name: bad tag\n", "dotted tag"}, {"version: 1\ntags:\n- name: x\n- name: x\n", "duplicate"}, {"version: 1\ntags:\n- name: x\n  unknown: true\n", "unknown field"}} {
		_, err := tags.Parse([]byte(tc.doc))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}
