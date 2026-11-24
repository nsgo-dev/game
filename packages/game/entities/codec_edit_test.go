package entities_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
)

func TestWorldDecodePatchRemoveComponent(t *testing.T) {
	type stats struct {
		HP   int            `json:"hp"`
		Max  int            `json:"max"`
		Tags map[string]int `json:"tags,omitempty"`
	}

	w := entities.NewWorld()
	if err := entities.RegisterCodec[stats](w, "stats"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[entities.Transform2D](w, "xform"); err != nil {
		t.Fatal(err)
	}
	e := w.Spawn()
	entities.Set(w, e, stats{HP: 10, Max: 20, Tags: map[string]int{"fire": 1}})

	// Patch updates only supplied fields and preserves omitted fields.
	if err := w.PatchComponent(e, "stats", json.RawMessage(`{"hp": 7}`)); err != nil {
		t.Fatalf("PatchComponent: %v", err)
	}
	got, ok := entities.Get[stats](w, e)
	if !ok || got.HP != 7 || got.Max != 20 || got.Tags["fire"] != 1 {
		t.Fatalf("after patch = %+v, want hp=7 max=20 tags={fire:1}", got)
	}

	// Failed patch with unknown field must NOT mutate existing map/struct fields.
	if err := w.PatchComponent(e, "stats", json.RawMessage(`{"tags": {"ice": 2}, "bogus": true}`)); err == nil {
		t.Fatal("expected error for unknown field in PatchComponent")
	}
	got, _ = entities.Get[stats](w, e)
	if _, leaked := got.Tags["ice"]; leaked || got.Tags["fire"] != 1 {
		t.Fatalf("failed patch mutated existing map: %+v", got.Tags)
	}

	// DecodeComponent replaces from zero value.
	if err := w.DecodeComponent(e, "stats", json.RawMessage(`{"hp": 5}`)); err != nil {
		t.Fatalf("DecodeComponent: %v", err)
	}
	got, _ = entities.Get[stats](w, e)
	if got.HP != 5 || got.Max != 0 || len(got.Tags) != 0 {
		t.Fatalf("after DecodeComponent = %+v, want hp=5 max=0 empty tags", got)
	}

	// Reject null for struct, trailing data, unknown codec, dead entity, disposed/nil world.
	if err := w.DecodeComponent(e, "stats", json.RawMessage(`null`)); err == nil {
		t.Fatal("expected error decoding null into struct")
	}
	if err := w.DecodeComponent(e, "stats", json.RawMessage(`{"hp":1} {"hp":2}`)); err == nil {
		t.Fatal("expected error for trailing JSON data")
	}
	if err := w.DecodeComponent(e, "missing", json.RawMessage(`{}`)); !errors.Is(err, entities.ErrUnknownCodec) {
		t.Fatalf("unknown codec = %v, want ErrUnknownCodec", err)
	}

	// Decoding Transform2D codec marks Hierarchy2D dirty automatically.
	h := w.Hierarchy2D()
	h.SetLocal(e, entities.NewTransform2D(1, 2))
	h.Sync()
	if h.IsDirty(e) {
		t.Fatal("expected clean transform after Sync")
	}
	if err := w.PatchComponent(e, "xform", json.RawMessage(`{"x": 42}`)); err != nil {
		t.Fatalf("patch xform: %v", err)
	}
	if !h.IsDirty(e) {
		t.Fatal("expected Transform2D codec edit to mark Hierarchy2D dirty")
	}
	h.Sync()
	wt, ok := h.WorldTransform(e)
	if !ok || wt.X != 42 || wt.Y != 2 || wt.ScaleX != 1 || wt.ScaleY != 1 {
		t.Fatalf("world transform = %+v, want x=42 y=2 scale=(1,1)", wt)
	}

	// RemoveComponent removes present component and returns false when absent.
	removed, err := w.RemoveComponent(e, "stats")
	if err != nil || !removed || entities.Has[stats](w, e) {
		t.Fatalf("first RemoveComponent = (%v, %v), has=%v", removed, err, entities.Has[stats](w, e))
	}
	removed, err = w.RemoveComponent(e, "stats")
	if err != nil || removed {
		t.Fatalf("second RemoveComponent = (%v, %v), want (false, nil)", removed, err)
	}
	if _, err := w.RemoveComponent(e, "missing"); !errors.Is(err, entities.ErrUnknownCodec) {
		t.Fatalf("remove unknown codec = %v, want ErrUnknownCodec", err)
	}

	// Dead entity & disposed/nil world errors.
	w.Destroy(e)
	if err := w.DecodeComponent(e, "stats", json.RawMessage(`{"hp":1}`)); !errors.Is(err, entities.ErrDeadEntity) {
		t.Fatalf("dead entity decode = %v, want ErrDeadEntity", err)
	}
	if _, err := w.RemoveComponent(e, "stats"); !errors.Is(err, entities.ErrDeadEntity) {
		t.Fatalf("dead entity remove = %v, want ErrDeadEntity", err)
	}

	var nilW *entities.World
	if err := nilW.DecodeComponent(e, "stats", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected nil world decode error")
	}
	if _, err := nilW.RemoveComponent(e, "stats"); err == nil {
		t.Fatal("expected nil world remove error")
	}

	w.Dispose()
	if err := w.DecodeComponent(e, "stats", json.RawMessage(`{}`)); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("disposed world decode = %v, want ErrWorldDisposed", err)
	}
	if _, err := w.RemoveComponent(e, "stats"); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("disposed world remove = %v, want ErrWorldDisposed", err)
	}
}
