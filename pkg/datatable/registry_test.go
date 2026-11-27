package datatable_test

import (
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/datatable"
)

func TestRegistryRegisterPatternValidation(t *testing.T) {
	rt, err := datatable.RowTypeFor[Enemy]()
	if err != nil {
		t.Fatalf("RowTypeFor[Enemy]: %v", err)
	}

	t.Run("rejects malformed patterns", func(t *testing.T) {
		cases := []struct {
			name    string
			pattern string
			wantSeg string
		}{
			{name: "bare open bracket", pattern: "[", wantSeg: "["},
			{name: "single segment unclosed class", pattern: "[x", wantSeg: "[x"},
			{name: "single segment empty class", pattern: "[]", wantSeg: "[]"},
			{name: "single segment open range", pattern: "[a-", wantSeg: "[a-"},
			{name: "two segment bad tail", pattern: "data/[", wantSeg: "["},
			{name: "globstar then bad tail", pattern: "data/**/[x", wantSeg: "[x"},
			{name: "three segment bad tail", pattern: "a/b/[", wantSeg: "["},
			{name: "middle segment empty class", pattern: "data/[]/enemies.yaml", wantSeg: "[]"},
			{name: "middle segment open range", pattern: "data/[a-/enemies.yaml", wantSeg: "[a-"},
			{name: "leading globstar bad negated empty class", pattern: "**/[^]/*.yaml", wantSeg: "[^]"},
			{name: "cleaned relative path bad tail", pattern: "data/a/../[", wantSeg: "["},
			{name: "wildcard before bad class", pattern: "data/enemies/*[a-", wantSeg: "*[a-"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reg := datatable.NewRegistry()
				err := reg.Register(tc.pattern, rt)
				if err == nil {
					t.Fatalf("Register(%q) = nil, want error", tc.pattern)
				}
				if !errors.Is(err, path.ErrBadPattern) {
					t.Errorf("Register(%q) error = %v, want errors.Is(_, path.ErrBadPattern)", tc.pattern, err)
				}
				wantSeg := `segment "` + tc.wantSeg + `"`
				if !strings.Contains(err.Error(), wantSeg) {
					t.Errorf("Register(%q) error = %q, want it to name %s", tc.pattern, err.Error(), wantSeg)
				}

				if err := datatable.RegisterRowType[Enemy](reg, tc.pattern); !errors.Is(err, path.ErrBadPattern) {
					t.Errorf("RegisterRowType(%q) error = %v, want path.ErrBadPattern", tc.pattern, err)
				}

				for _, sample := range []string{"test.yaml", "other/dir/file.yaml", "data/a/b/c.yaml"} {
					matched, merr := datatable.MatchPattern(tc.pattern, sample)
					if matched || !errors.Is(merr, path.ErrBadPattern) {
						t.Errorf("MatchPattern(%q, %q) = (%v, %v), want (false, path.ErrBadPattern)", tc.pattern, sample, matched, merr)
					}
				}
			})
		}
	})

	t.Run("rejects empty patterns", func(t *testing.T) {
		for _, pattern := range []string{"", "   ", ".", "./", "./.", "./   /", "data/.."} {
			reg := datatable.NewRegistry()
			if err := reg.Register(pattern, rt); err == nil {
				t.Errorf("Register(%q) = nil, want empty-pattern error", pattern)
			}
			if matched, err := datatable.MatchPattern(pattern, "test.yaml"); matched || err == nil {
				t.Errorf("MatchPattern(%q, test.yaml) = (%v, %v), want (false, error)", pattern, matched, err)
			}
		}
	})

	t.Run("rejects nil receiver or nil row type", func(t *testing.T) {
		var nilReg *datatable.Registry
		if err := nilReg.Register("items.yaml", rt); err == nil {
			t.Error("(*Registry)(nil).Register = nil, want error")
		}
		if err := datatable.RegisterRowType[Enemy](nil, "items.yaml"); err == nil {
			t.Error("RegisterRowType(nil, ...) = nil, want error")
		}
		if err := datatable.NewRegistry().Register("items.yaml", nil); err == nil {
			t.Error("Register(..., nil) = nil, want error")
		}
	})

	t.Run("accepts valid patterns and matches", func(t *testing.T) {
		cases := []struct {
			name        string
			pattern     string
			matchPaths  []string
			rejectPaths []string
		}{
			{
				name:        "basename exact",
				pattern:     "items.yaml",
				matchPaths:  []string{"items.yaml", "nested/dir/items.yaml"},
				rejectPaths: []string{"enemies.yaml", "data/my_items.yaml"},
			},
			{
				name:        "basename glob",
				pattern:     "*.yaml",
				matchPaths:  []string{"a.yaml", "data/b.yaml"},
				rejectPaths: []string{"data/b.json"},
			},
			{
				name:        "multi-segment glob",
				pattern:     "data/enemies/*.yaml",
				matchPaths:  []string{"data/enemies/forest.yaml", "./data/enemies/cave.yaml"},
				rejectPaths: []string{"data/other.yaml", "data/enemies/sub/forest.yaml"},
			},
			{
				name:        "middle globstar",
				pattern:     "tables/**/bosses.yaml",
				matchPaths:  []string{"tables/bosses.yaml", "tables/act1/bosses.yaml", "tables/act1/ch2/bosses.yaml"},
				rejectPaths: []string{"tables/act1/minions.yaml", "other/bosses.yaml"},
			},
			{
				name:        "trailing globstar",
				pattern:     "data/**",
				matchPaths:  []string{"data/a.yaml", "data/sub/b.yaml"},
				rejectPaths: []string{"other/a.yaml"},
			},
			{
				name:        "leading globstar with character class",
				pattern:     "**/enemies/[a-z]*.yaml",
				matchPaths:  []string{"enemies/goblin.yaml", "a/b/enemies/orc.yaml"},
				rejectPaths: []string{"enemies/1.yaml", "a/b/items/goblin.yaml"},
			},
			{
				name:        "leading dot slash with question mark",
				pattern:     "./data/items/item_?.yaml",
				matchPaths:  []string{"data/items/item_1.yaml", "./data/items/item_a.yaml"},
				rejectPaths: []string{"data/items/item_10.yaml"},
			},
			{
				name:        "bare globstar",
				pattern:     "**",
				matchPaths:  []string{"anything.yaml", "a/b/c.yaml"},
				rejectPaths: nil,
			},
			{
				name:        "multiple globstars",
				pattern:     "data/a/**/b/**/c.yaml",
				matchPaths:  []string{"data/a/b/c.yaml", "data/a/x/b/y/z/c.yaml"},
				rejectPaths: []string{"data/a/x/c.yaml"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reg := datatable.NewRegistry()
				if err := reg.Register(tc.pattern, rt); err != nil {
					t.Fatalf("Register(%q) unexpected error: %v", tc.pattern, err)
				}
				for _, p := range tc.matchPaths {
					if got := reg.Match(p); got != rt {
						t.Errorf("Match(%q) for pattern %q = %v, want Enemy", p, tc.pattern, got)
					}
				}
				for _, p := range tc.rejectPaths {
					if got := reg.Match(p); got != nil {
						t.Errorf("Match(%q) for pattern %q = %v, want nil", p, tc.pattern, got)
					}
				}
			})
		}
	})

	t.Run("replacing normalized pattern updates binding", func(t *testing.T) {
		itemRT, err := datatable.RowTypeFor[Item]()
		if err != nil {
			t.Fatalf("RowTypeFor[Item]: %v", err)
		}
		reg := datatable.NewRegistry()
		if err := reg.Register("data/tables/*.yaml", rt); err != nil {
			t.Fatalf("Register first: %v", err)
		}
		if err := reg.Register("./data/tables/*.yaml", itemRT); err != nil {
			t.Fatalf("Register replacement: %v", err)
		}
		if got := reg.Match("data/tables/loot.yaml"); got != itemRT {
			t.Fatalf("Match after replacement = %v, want Item", got)
		}
	})
}
