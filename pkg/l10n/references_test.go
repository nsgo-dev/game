package l10n_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/l10n"
)

func TestCatalogReferences(t *testing.T) {
	cat := l10n.NewCatalog("en")
	if err := cat.LoadYAML([]byte(`version: 1
locale: en
strings:
  requires: "Requires {@sword.name} for {player}. {{@sword.name}}"
  sword.name: "{@quality} Sword"
  quality: "Iron +{rank}"
plurals:
  equipment:
    other: "{count} pieces of {@sword.name}"
`)); err != nil {
		t.Fatal(err)
	}
	cat.Set("fr", "sword.name", "Épée")
	got := cat.T("fr", "requires", l10n.Param("rank", 2), l10n.Param("player", "{@quality}"))
	if want := "Requires Iron +2 Sword for {@quality}. {@sword.name}"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	if got := cat.Plural("en", "equipment", 5, l10n.Param("rank", 2)); got != "5 pieces of Iron +2 Sword" {
		t.Errorf("Plural = %q", got)
	}
}

func TestCatalogReferenceCyclesAndExpansionLimit(t *testing.T) {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "a", "A {@b} {@missing}")
	cat.Set("en", "b", "B {@a}")
	if got := cat.T("en", "a"); got != "A B {@a} {@missing}" {
		t.Errorf("cycle or missing reference = %q", got)
	}
	cat.Set("en", "target", "x")
	cat.Set("en", "many", strings.Repeat("{@target}", 129))
	if got := cat.T("en", "many"); got != strings.Repeat("x", 128)+"{@target}" {
		t.Errorf("expansion limit = %q", got)
	}
}
