package datatable

import (
	"strings"
	"testing"
)

func TestRegistryMatchPanicsOnInvalidEntryPattern(t *testing.T) {
	rt, err := RowTypeFor[struct {
		ID string `table:"id"`
	}]()
	if err != nil {
		t.Fatalf("RowTypeFor: %v", err)
	}
	reg := &Registry{
		entries: []registryEntry{
			{pattern: "data/[", rowType: rt},
		},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Match did not panic on corrupted registered pattern")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "data/[") {
			t.Fatalf("unexpected panic value: %#v", r)
		}
	}()

	_ = reg.Match("data/enemies.yaml")
}
