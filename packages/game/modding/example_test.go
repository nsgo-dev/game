package modding_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/modding"
)

// ParseManifest reads and validates a mod's manifest, including the capabilities the mod asks for.
func ExampleParseManifest() {
	m, err := modding.ParseManifest([]byte(`{
		"id": "more-slimes",
		"name": "More Slimes",
		"version": "1.2.0",
		"entry": "mod.ts",
		"capabilities": ["spawnEntity", "readState"],
		"gameVersion": "^1.0.0"
	}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.ID, m.Version, m.Capabilities.Names())
	fmt.Println(m.Capabilities.Has(modding.CapSpawnEntity), m.Capabilities.Has(modding.CapStorage))
	// Output:
	// more-slimes 1.2.0 [readState spawnEntity]
	// true false
}

// ResolveOrder sorts mods so each loads after the mods it depends on, and refuses a dependency that is missing or of the wrong version.
func ExampleResolveOrder() {
	mods := []modding.ModManifest{
		{ID: "slime-hats", Name: "Slime Hats", Version: "1.0.0", Entry: "mod.ts", Dependencies: map[string]string{"more-slimes": ">=1.1.0"}},
		{ID: "more-slimes", Name: "More Slimes", Version: "1.2.0", Entry: "mod.ts"},
	}
	ordered, err := modding.ResolveOrder(mods)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, m := range ordered {
		fmt.Println(m.ID)
	}
	// Output:
	// more-slimes
	// slime-hats
}

// CheckConstraint tests a version against a constraint such as a manifest's gameVersion.
func ExampleCheckConstraint() {
	for _, c := range []string{"^1.2.0", ">=2.0.0", "~1.4.0"} {
		ok, err := modding.CheckConstraint("1.4.3", c)
		fmt.Println(c, ok, err)
	}
	v, _ := modding.ParseSemVer("2.0.0-beta.1")
	stable, _ := modding.ParseSemVer("2.0.0")
	fmt.Println(v, v.Compare(stable))
	// Output:
	// ^1.2.0 true <nil>
	// >=2.0.0 false <nil>
	// ~1.4.0 true <nil>
	// 2.0.0-beta.1 -1
}
