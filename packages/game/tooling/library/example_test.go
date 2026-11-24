package library_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
)

// A library is an index of packs, each a directory with a pack.yaml; PlanInstall shows what installing one would write and how game.yaml would record it.
func ExamplePlanInstall() {
	manifest := []byte(`version: 1
name: coins
kind: asset
packVersion: 1.0.0
description: A spinning coin.
license: CC0-1.0
files: [coin.png]
autoload:
  resources:
    - name: coin
      path: coin.png
`)
	libFiles := fstest.MapFS{
		"packs/coins/pack.yaml": {Data: manifest},
		"packs/coins/coin.png":  {Data: []byte("PNG")},
	}
	pack, err := library.ReadPack(libFiles, "packs/coins")
	if err != nil {
		fmt.Println(err)
		return
	}
	index := fmt.Sprintf(`version: 1
packs:
  - name: coins
    kind: asset
    packVersion: 1.0.0
    description: A spinning coin.
    license: CC0-1.0
    source: packs/coins
    sha256: %s
`, pack.Digest())
	libFiles["index.yaml"] = &fstest.MapFile{Data: []byte(index)}

	lib, err := library.OpenFS(libFiles, "index.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, e := range lib.Index.Search("asset", "coin") {
		fmt.Println("found", e.Name, e.PackVersion)
	}
	pack, err = lib.Fetch(context.Background(), "coins")
	if err != nil {
		fmt.Println(err)
		return
	}

	game := project.MustParse([]byte("version: 1\nname: Coin Run\nid: dev.example.coinrun\nappVersion: 0.1.0\nwindow: {width: 320, height: 180}\n"))
	change, err := library.PlanInstall(fstest.MapFS{}, game, pack, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, name := range []string{"packs/coins/coin.png", "packs/coins/pack.yaml"} {
		fmt.Println("write", name, len(change.Write[name]), "bytes")
	}
	fmt.Println("autoload", change.Project.Autoload.Resources[0].Name, change.Project.Autoload.Resources[0].Path)
	rec := change.Project.FindPack("coins")
	fmt.Println("record", rec.Name, rec.Kind, rec.PackVersion, rec.Dir, rec.Files)
	// Output:
	// found coins 1.0.0
	// write packs/coins/coin.png 3 bytes
	// write packs/coins/pack.yaml 180 bytes
	// autoload coin packs/coins/coin.png
	// record coins asset 1.0.0 packs/coins [coin.png pack.yaml]
}
