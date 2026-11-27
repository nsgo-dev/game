package gltf_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/gltf"
)

func TestParseLODNodeName(t *testing.T) {
	cases := []struct {
		name     string
		wantBase string
		wantLv   int
		wantOK   bool
	}{
		{"Tree_LOD0", "Tree", 0, true},
		{"Tree_LOD1", "Tree", 1, true},
		{"Tree_LOD2", "Tree", 2, true},
		{"Tree_lod1", "Tree", 1, true},
		{"Oak_Branch_LOD3", "Oak_Branch", 3, true},
		{"Rock_LOD1.001", "Rock", 1, true},
		{"  House_LOD2  ", "House", 2, true},
		{"Tree", "", 0, false},
		{"_LOD1", "", 0, false},
		{"Tree_LOD", "", 0, false},
		{"Tree_LODx", "", 0, false},
		{"Tree_LOD-1", "", 0, false},
		{"Tree_LOD1_extra", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range cases {
		base, lv, ok := gltf.ParseLODNodeName(tc.name)
		if base != tc.wantBase || lv != tc.wantLv || ok != tc.wantOK {
			t.Errorf("ParseLODNodeName(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.name, base, lv, ok, tc.wantBase, tc.wantLv, tc.wantOK)
		}
	}
}
