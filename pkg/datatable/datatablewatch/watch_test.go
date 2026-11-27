package datatablewatch_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/datatable/datatablewatch"
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
		datatablewatch.WatchWithError(tbl, w, func(rows []Enemy, err error) {
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

func TestYAMLTableHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enemies.yaml")
	v1 := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 40\n    move_speed: 3\n"
	typo := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 40\n    move_sped: 3\n"
	v2 := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 65\n    move_speed: 4.2\n  - enemy_id: orc\n    display_name: Orc\n    hp: 110\n    move_speed: 2.5\n"

	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := datatable.NewTable[Enemy](path)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	if tbl.Version() != 1 || len(tbl.Rows()) != 1 || tbl.Rows()[0].HP != 40 || tbl.Rows()[0].Speed != 3 {
		t.Fatalf("initial table: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}

	var (
		reloads atomic.Int32
		failed  atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		datatablewatch.WatchWithError(tbl, w, func(rows []Enemy, err error) {
			if err != nil {
				failed.Store(true)
				return
			}
			if len(rows) == 2 && rows[0].HP == 65 {
				reloads.Add(1)
			}
		})
		return core.View{}
	})

	// A misspelt field is refused: the table keeps v1 and reports the row and key.
	if err := os.WriteFile(path, []byte(typo), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, failed.Load)
	if tbl.Version() != 1 || tbl.Rows()[0].HP != 40 {
		t.Fatalf("refused reload changed the table: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}
	var ce *datatable.CellError
	if !errors.As(tbl.LastError(), &ce) || ce.Row != 1 || ce.Field != "move_sped" || !errors.Is(ce, datatable.ErrUnknownField) {
		t.Fatalf("LastError = %v, want unknown field move_sped at row 1", tbl.LastError())
	}

	// The fixed file replaces the rows and bumps Version.
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return reloads.Load() >= 1 })
	if tbl.Version() != 2 || tbl.LastError() != nil {
		t.Fatalf("after valid reload: ver=%d err=%v", tbl.Version(), tbl.LastError())
	}
	got := tbl.Rows()
	if len(got) != 2 || got[0].HP != 65 || got[0].Speed != 4.2 || got[1].ID != "orc" || got[1].HP != 110 {
		t.Fatalf("reloaded rows = %+v", got)
	}
}
