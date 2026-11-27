package datatable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/tooling/datatable"
)

func TestLoadErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		file, data string
		line       int
	}{
		{"enemies.csv", "enemy_id,hp\ngoblin,50\norc,lots\n", 3},
		{"enemies.csv", "enemy_id,hp\ngoblin,50\norc\n", 3},
		{"enemies.csv", "enemy_id,hp\ngoblin,\"50\n", 2},
		{"enemies.json", "[\n  {\"enemy_id\": \"goblin\", \"hp\": 50},\n  {\"enemy_id\": \"orc\",\n   \"hp\": \"lots\"}\n]", 4},
		{"enemies.json", "[\n  {\"enemy_id\": \"goblin\"},\n  7\n]", 3},
		{"enemies.json", "[\n  {\"enemy_id\": \"goblin\"}\n]\n{}", 4},
		{"enemies.json", "[\n  {\"enemy_id\": \"goblin\"},\n]", 3},
		{"enemies.yaml", "version: 1\nrows:\n  - enemy_id: goblin\n    hp: 50\n  - enemy_id: orc\n    hp: lots\n", 6},
		{"enemies.yaml", "version: 1\nrows:\n  - enemy_id: goblin\n  - just a string\n", 4},
		{"enemies.yaml", "version: 1\nrows:\n  - enemy_id: goblin\n    sped: 3\n", 4},
		{"enemies.yaml", "enemy_id: goblin\n", 1},
		{"enemies.yaml", "- enemy_id: goblin\n", 1},
		{"enemies.yaml", "version: 2\nrows: []\n", 1},
		{"enemies.yaml", "version: 1\nrows:\n  - enemy_id: goblin\n    hp: [1\n", 4},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.file+" "+tc.data, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := datatable.LoadFile[Enemy](path)
			loaderrtest.Expect(t, err, path, tc.line)
		})
	}
	t.Run("unknown extension", func(t *testing.T) {
		path := filepath.Join(dir, "enemies.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := datatable.LoadFile[Enemy](path)
		loaderrtest.Expect(t, err, path, 0)
	})
}
