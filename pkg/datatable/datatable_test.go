package datatable_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type Enemy struct {
	ID       string        `table:"enemy_id"`
	Name     string        `json:"display_name"`
	HP       int           `table:"hp"`
	Armor    uint16        // matched case-insensitively by field name
	Speed    float64       `table:"move_speed"`
	Boss     bool          `table:"is_boss"`
	Tags     []string      `table:"tags"`
	Drops    []int         `table:"drop_ids"`
	Cooldown time.Duration `table:"cooldown"`
	Ignored  string        `table:"-"`
}

func wantEnemies() []Enemy {
	return []Enemy{
		{
			ID:       "goblin",
			Name:     "Cave Goblin",
			HP:       45,
			Armor:    2,
			Speed:    3.5,
			Boss:     false,
			Tags:     []string{"melee", "scout"},
			Drops:    []int{101, 102},
			Cooldown: 500 * time.Millisecond,
		},
		{
			ID:       "dragon",
			Name:     "Ancient Wyrm",
			HP:       1200,
			Armor:    40,
			Speed:    6.25,
			Boss:     true,
			Tags:     []string{"flying", "fire", "boss"},
			Drops:    []int{900},
			Cooldown: 2 * time.Second,
		},
	}
}

func TestLoadCSVJSONYAMLAndLoadFile(t *testing.T) {
	csvData := `enemy_id,display_name,hp,armor,move_speed,is_boss,tags,drop_ids,cooldown,ignored
goblin,Cave Goblin,45,2,3.5,false,melee;scout,101;102,500ms,should_skip
dragon,Ancient Wyrm,1200,40,6.25,true,flying|fire|boss,900,2s,should_skip
`
	jsonData := `[
		{
			"enemy_id": "goblin",
			"display_name": "Cave Goblin",
			"hp": 45,
			"ARMOR": 2,
			"move_speed": 3.5,
			"is_boss": false,
			"tags": ["melee", "scout"],
			"drop_ids": [101, 102],
			"cooldown": "500ms",
			"ignored": "skip"
		},
		{
			"enemy_id": "dragon",
			"display_name": "Ancient Wyrm",
			"hp": 1200,
			"armor": 40,
			"move_speed": 6.25,
			"is_boss": true,
			"tags": ["flying", "fire", "boss"],
			"drop_ids": [900],
			"cooldown": "2s"
		}
	]`
	yamlData := `
version: 1
rows:
  - enemy_id: goblin
    display_name: Cave Goblin
    hp: 45
    armor: 2
    move_speed: 3.5
    is_boss: false
    tags:
      - melee
      - scout
    drop_ids: [101, 102]
    cooldown: 500ms
  - enemy_id: dragon
    display_name: Ancient Wyrm
    hp: 1200
    Armor: 40
    move_speed: 6.25
    is_boss: true
    tags: [flying, fire, boss]
    drop_ids: [900]
    cooldown: 2s
`

	want := wantEnemies()

	t.Run("CSV", func(t *testing.T) {
		got, err := datatable.LoadCSV[Enemy]("enemies.csv", []byte(csvData))
		if err != nil {
			t.Fatalf("LoadCSV failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("LoadCSV =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("JSON", func(t *testing.T) {
		got, err := datatable.LoadJSON[Enemy]("enemies.json", []byte(jsonData))
		if err != nil {
			t.Fatalf("LoadJSON failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("LoadJSON =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("YAML", func(t *testing.T) {
		got, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(yamlData))
		if err != nil {
			t.Fatalf("LoadYAML failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("LoadYAML =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("LoadFile auto-detection", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string]string{
			"enemies.csv":  csvData,
			"enemies.json": jsonData,
			"enemies.yml":  yamlData,
		}
		for name, body := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := datatable.LoadFile[Enemy](p)
			if err != nil {
				t.Fatalf("LoadFile(%s) failed: %v", name, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("LoadFile(%s) mismatch:\ngot  %+v\nwant %+v", name, got, want)
			}
		}
	})
}

func TestBadCellsReportFileRowAndField(t *testing.T) {
	t.Run("CSV bad int cell", func(t *testing.T) {
		csv := "enemy_id,hp,move_speed\ngoblin,50,2.5\norc,not_a_number,1.5\n"
		_, err := datatable.LoadCSV[Enemy]("data/enemies.csv", []byte(csv))
		var ce *datatable.CellError
		if !errors.As(err, &ce) {
			t.Fatalf("expected *CellError, got %T: %v", err, err)
		}
		if ce.File != "data/enemies.csv" || ce.Row != 2 || ce.Field != "hp" || ce.Value != "not_a_number" {
			t.Fatalf("unexpected CellError: %+v", ce)
		}
	})

	t.Run("JSON bad uint overflow and wrong type", func(t *testing.T) {
		js := `[
			{"enemy_id":"a","hp":10,"armor":5},
			{"enemy_id":"b","hp":10,"armor":70000}
		]`
		_, err := datatable.LoadJSON[Enemy]("enemies.json", []byte(js))
		var ce *datatable.CellError
		if !errors.As(err, &ce) {
			t.Fatalf("expected *CellError, got %T: %v", err, err)
		}
		if ce.File != "enemies.json" || ce.Row != 2 || ce.Field != "armor" {
			t.Fatalf("unexpected CellError: %+v", ce)
		}

		js2 := `[{"enemy_id":"a","hp":"fifty"}]`
		_, err = datatable.LoadJSON[Enemy]("enemies.json", []byte(js2))
		if !errors.As(err, &ce) || ce.Row != 1 || ce.Field != "hp" {
			t.Fatalf("expected CellError at row 1 field hp, got %v", err)
		}
	})

	t.Run("YAML bad float cell", func(t *testing.T) {
		yml := `
version: 1
rows:
  - enemy_id: slime
    hp: 10
    move_speed: 1.25
  - enemy_id: ghost
    hp: 30
    move_speed: fast
`
		_, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(yml))
		var ce *datatable.CellError
		if !errors.As(err, &ce) {
			t.Fatalf("expected *CellError, got %T: %v", err, err)
		}
		if ce.File != "enemies.yaml" || ce.Row != 2 || ce.Field != "move_speed" || ce.Value != "fast" {
			t.Fatalf("unexpected CellError: %+v", ce)
		}
	})
}

func TestTableHotReloadAndErrorRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enemies.csv")
	v1 := "enemy_id,display_name,hp,move_speed\ngoblin,Goblin,40,3.0\n"
	v2 := "enemy_id,display_name,hp,move_speed\ngoblin,Goblin,65,4.2\norc,Orc,110,2.5\n"
	bad := "enemy_id,display_name,hp,move_speed\ngoblin,Goblin,WHOOPS,3.0\n"

	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := datatable.NewTable[Enemy](path)
	if err != nil {
		t.Fatalf("NewTable failed: %v", err)
	}
	if tbl.Version() != 1 || len(tbl.Rows()) != 1 || tbl.Rows()[0].HP != 40 {
		t.Fatalf("initial table state unexpected: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}

	// Mutating the slice returned by Rows must not affect internal storage.
	rowsCopy := tbl.Rows()
	rowsCopy[0].HP = 9999
	if tbl.Rows()[0].HP != 40 {
		t.Fatalf("Rows() returned shared slice backing array")
	}

	var (
		reloaded atomic.Int32
		lastErr  atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		tbl.WatchWithError(w, func(rows []Enemy, err error) {
			if err != nil {
				lastErr.Store(true)
				return
			}
			if len(rows) == 2 && rows[0].HP == 65 {
				reloaded.Add(1)
			}
		})
		return core.View{}
	})

	// Write an invalid file first: table should keep v1 intact and record LastError.
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, lastErr.Load)
	if tbl.Version() != 1 || tbl.Rows()[0].HP != 40 {
		t.Fatalf("invalid reload corrupted table: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}
	var ce *datatable.CellError
	if !errors.As(tbl.LastError(), &ce) || ce.Row != 1 || ce.Field != "hp" {
		t.Fatalf("LastError = %v, want CellError at row 1 field hp", tbl.LastError())
	}

	// Now fix the file: table should update live to v2 and bump Version to 2.
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return reloaded.Load() >= 1 })

	if tbl.Version() != 2 {
		t.Fatalf("Version = %d, want 2", tbl.Version())
	}
	if tbl.LastError() != nil {
		t.Fatalf("LastError after valid reload = %v, want nil", tbl.LastError())
	}
	got := tbl.Rows()
	if len(got) != 2 || got[0].HP != 65 || got[1].ID != "orc" {
		t.Fatalf("updated rows = %+v, want 2 rows with goblin HP=65 and orc", got)
	}
}

func TestRegistryLookup(t *testing.T) {
	var nilReg *datatable.Registry
	if _, ok := nilReg.Lookup("enemies.yaml"); ok {
		t.Fatal("nil Registry.Lookup should return false")
	}
	reg := datatable.NewRegistry()
	if _, ok := reg.Lookup(""); ok {
		t.Fatal("empty pattern Lookup should return false")
	}
	rt, err := datatable.RowTypeFor[Enemy]()
	if err != nil {
		t.Fatalf("RowTypeFor: %v", err)
	}
	if err := reg.Register("data/enemies.yaml", rt); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, ok := reg.Lookup("./data/enemies.yaml"); !ok || got != rt {
		t.Fatalf("Lookup normalized = %v, %v", got, ok)
	}
	if _, ok := reg.Lookup("data/other.yaml"); ok {
		t.Fatal("Lookup of unregistered pattern returned true")
	}
}
