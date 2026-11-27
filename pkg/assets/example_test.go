package assets_test

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/core"
)

// A Group loads a scene's assets together off the engine goroutine and reports progress as they arrive. Here a channel stands in for the host's Post, so the example runs each posted callback itself.
func ExampleGroup() {
	posted := make(chan func(), 16)
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { posted <- fn }})
	defer root.Dispose()

	files := fstest.MapFS{
		"levels/1.txt": {Data: []byte("wall wall door")},
		"credits.txt":  {Data: []byte("Made with NSGo")},
	}
	m := assets.NewManager(root, assets.WithFS(files))
	g := m.Group("level-1").
		AddBytes("credits", "asset:credits.txt").
		AddCustom("layout", "asset:levels/1.txt", func(b []byte) (any, error) { return strings.Fields(string(b)), nil }, nil)

	done := false
	g.Load(func(err error) {
		done = true
		fmt.Println("loaded:", err)
	})
	for !done {
		(<-posted)()
	}
	credits, _ := g.Bytes("credits")
	layout, _ := g.Get("layout")
	fmt.Printf("%d/%d %.0f%%\n", g.Loaded(), g.Total(), g.Progress()*100)
	fmt.Println(string(credits), layout)

	m.ReleaseGroup("level-1")
	_, ok := g.Get("layout")
	fmt.Println("after release:", ok)
	// Output:
	// loaded: <nil>
	// 2/2 100%
	// Made with NSGo [wall wall door]
	// after release: false
}

// ParsePackManifest reads the list of downloadable packs, and Compatible checks each against the running build.
func ExampleParsePackManifest() {
	packs, err := assets.ParsePackManifest([]byte(`{"packs": [
		{"id": "chapter-2", "url": "https://example.com/c2.zip", "version": "1.0.0", "min_build": 10, "sha256": "00"},
		{"id": "chapter-3", "url": "https://example.com/c3.zip", "version": "1.0.0", "min_build": 20, "sha256": "00"}
	]}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, p := range packs {
		fmt.Println(p.ID, p.Compatible(15))
	}
	// Output:
	// chapter-2 true
	// chapter-3 false
}

// SignPack produces the digest and signature a PackSpec carries, so a DLCManager given the public key refuses a pack that was altered.
func ExampleSignPack() {
	pub, priv, _ := ed25519.GenerateKey(strings.NewReader(strings.Repeat("seed", 16)))
	data := []byte("pack contents")
	digest, sig := assets.SignPack(priv, data)
	fmt.Println(digest)
	again, err := assets.SignPackDigest(priv, digest)
	fmt.Println(again == sig, err, len(pub))
	// Output:
	// 61eb17abac363f8e0bc21b11c814d31e7fcda74f2f505770e5ce2de2b69f8ffc
	// true <nil> 32
}

// A MemoryPackCache keeps downloaded packs in memory: a download goes to a part entry first and becomes the pack once committed.
func ExampleMemoryPackCache() {
	c := assets.NewMemoryPackCache()
	w, _ := c.OpenPartWriter("chapter-2@1.0.0", false)
	fmt.Fprint(w, "zip bytes")
	w.Close()
	fmt.Println(c.HasPart("chapter-2@1.0.0"), c.Has("chapter-2@1.0.0"))
	if err := c.CommitPart("chapter-2@1.0.0"); err != nil {
		fmt.Println(err)
		return
	}
	_, size, _ := c.Open("chapter-2@1.0.0")
	fmt.Println(c.HasPart("chapter-2@1.0.0"), c.Has("chapter-2@1.0.0"), size)
	// Output:
	// true false
	// false true 9
}
