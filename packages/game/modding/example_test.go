package modding_test

import (
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/modding"
	"github.com/nsgo-dev/nsgo/core"
)

// ParseManifest reads and validates a mod's manifest, including the capabilities the mod asks for.
func ExampleParseManifest() {
	m, err := modding.ParseManifest([]byte(`manifestVersion: 1
id: more-slimes
name: More Slimes
version: 1.2.0
entry: mod.ts
capabilities: [spawnEntity, readState]
gameVersion: ^1.0.0
`))
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

// A Manager discovers mods in a directory, one per subdirectory with a mod.yaml, checks their manifests, entries and dependencies, and keeps them in load order; Disable keeps a mod from loading and, with a StorageKey, remembers that in the app's storage. Load then runs an enabled mod's entry in its TSX sandbox.
func ExampleManager() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	mods := fstest.MapFS{
		"hats/mod.yaml":   {Data: []byte("manifestVersion: 1\nid: slime-hats\nname: Slime Hats\nversion: 1.0.0\nentry: mod.ts\ndependencies:\n  more-slimes: '>=1.1.0'\n")},
		"hats/mod.ts":     {Data: []byte("export function init() {}\n")},
		"slimes/mod.yaml": {Data: []byte("manifestVersion: 1\nid: more-slimes\nname: More Slimes\nversion: 1.2.0\nentry: mod.ts\ngameVersion: ^1.0.0\n")},
		"slimes/mod.ts":   {Data: []byte("export function init() {}\n")},
	}
	opts := modding.ManagerOptions{StorageKey: "mods", GameVersion: "1.4.0"}

	m := modding.NewManager(root, opts)
	if _, err := m.DiscoverFS(mods); err != nil {
		fmt.Println(err)
		return
	}
	_ = m.Disable("slime-hats")
	m.Close()

	// A later session finds the preference in storage.
	again := modding.NewManager(root, opts)
	defer again.Close()
	if _, err := again.DiscoverFS(mods); err != nil {
		fmt.Println(err)
		return
	}
	for _, info := range again.List() {
		fmt.Println(info.Order, info.Manifest.ID, info.State, info.Enabled)
	}
	// Output:
	// 0 more-slimes discovered true
	// 1 slime-hats disabled false
}
