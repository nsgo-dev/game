package templatemeta_test

import (
	"github.com/nsgo-dev/game/pkg/tooling/templatemeta"
	"strings"
	"testing"
)

func TestManifestRoundTripAndValidation(t *testing.T) {
	m := templatemeta.Manifest{Version: 1, ID: "puzzle2d", Title: map[string]string{"en": "Puzzle"}, Summary: map[string]string{"en": "A puzzle"}, Genre: "puzzle", Dimension: "2d", Orientation: "portrait", Players: templatemeta.Players{Min: 1, Max: 1}, Inputs: []string{"touch"}}
	raw, err := templatemeta.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := templatemeta.Parse(raw)
	if err != nil || got.TitleFor("en") != "Puzzle" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := templatemeta.Parse([]byte("version: 1\nid: x\ntitle: {en: x}\nsummary: {en: x}\ngenre: unknown\ndimension: 2d\norientation: any\nplayers: {min: 1, max: 1}\n")); err == nil || !strings.Contains(err.Error(), "genre") {
		t.Fatalf("err=%v", err)
	}
}
