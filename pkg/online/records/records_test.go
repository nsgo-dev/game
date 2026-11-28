package records

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestApplyCASAndPermissions(t *testing.T) {
	s := New(nil)
	r := Record{Owner: "a", Collection: "inv", Key: "gold", Value: json.RawMessage(`1`), Read: OwnerRead}
	if e := s.Apply(context.Background(), Tx{Puts: []Put{{Record: r}}}); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(context.Background(), "a", "inv", "gold", "a")
	if e != nil || got.Version != 1 {
		t.Fatalf("get=%+v %v", got, e)
	}
	if _, e = s.Get(context.Background(), "a", "inv", "gold", "b"); !errors.Is(e, ErrPermission) {
		t.Fatal(e)
	}
	if e = s.Apply(context.Background(), Tx{Puts: []Put{{Record: r, Expected: 1}}}); e != nil {
		t.Fatal(e)
	}
	if e = s.Apply(context.Background(), Tx{Puts: []Put{{Record: r, Expected: 1}}}); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
}
