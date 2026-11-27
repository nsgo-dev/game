package datatable_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/tooling/datatable"
)

func diffLines(before, after string) (changed int, diffs []string) {
	a := strings.Split(before, "\n")
	b := strings.Split(after, "\n")
	n := max(len(a), len(b))
	for i := range n {
		var la, lb string
		if i < len(a) {
			la = a[i]
		}
		if i < len(b) {
			lb = b[i]
		}
		if la != lb {
			changed++
			diffs = append(diffs, la+" -> "+lb)
		}
	}
	return changed, diffs
}

func TestDocumentRoundTripByteForByteIdentical(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "canonical enemies",
			yaml: enemiesYAML,
		},
		{
			name: "comments blank lines and custom indentation",
			yaml: "# Enemy balance table\nversion: 1\n\nrows:\n    # First enemy\n    - enemy_id: goblin\n      display_name: \"Cave Goblin\" # inline note\n      hp: 45\n      move_speed: 3.50\n      is_boss: false\n      tags: [melee, scout]\n      cooldown: 500ms\n\n    - enemy_id: dragon\n      hp: 1200\n      is_boss: true\n",
		},
		{
			name: "empty rows list",
			yaml: "version: 1\nrows: []\n",
		},
		{
			name: "missing rows key",
			yaml: "version: 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := datatable.ParseDocument("test.yaml", []byte(tc.yaml))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			got, err := doc.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !bytes.Equal(got, []byte(tc.yaml)) {
				t.Fatalf("round-trip changed bytes:\ngot:\n%q\nwant:\n%q", got, tc.yaml)
			}
		})
	}
}

func TestDocumentEditSingleCellChangesOnlyOneLine(t *testing.T) {
	src := `# Enemy table
version: 1

rows:
  # Weak starter foe
  - enemy_id: goblin
    display_name: Cave Goblin
    hp: 45 # starter health
    Armor: 2
    move_speed: 3.5
    is_boss: false
    tags: [melee, scout]
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
	t.Run("edit number cell with inline comment", func(t *testing.T) {
		doc, err := datatable.ParseDocument("enemies.yaml", []byte(src))
		if err != nil {
			t.Fatalf("ParseDocument: %v", err)
		}
		if err := doc.SetCell(0, "hp", datatable.IntValue(60)); err != nil {
			t.Fatalf("SetCell: %v", err)
		}
		got, err := doc.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		changed, diffs := diffLines(src, string(got))
		if changed != 1 {
			t.Fatalf("changed %d lines, want 1:\n%v\nfull:\n%s", changed, diffs, got)
		}
		if !strings.Contains(string(got), "    hp: 60 # starter health\n") {
			t.Fatalf("inline comment or value not preserved:\n%s", got)
		}
		// Must still load cleanly with LoadYAML.
		enemies, err := datatable.LoadYAML[Enemy]("enemies.yaml", got)
		if err != nil {
			t.Fatalf("LoadYAML after edit: %v", err)
		}
		if enemies[0].HP != 60 || enemies[1].HP != 1200 {
			t.Fatalf("enemies HP = %d, %d", enemies[0].HP, enemies[1].HP)
		}
	})

	t.Run("edit list cell", func(t *testing.T) {
		doc, err := datatable.ParseDocument("enemies.yaml", []byte(src))
		if err != nil {
			t.Fatalf("ParseDocument: %v", err)
		}
		if err := doc.SetCell(0, "tags", datatable.ListValue(
			datatable.StringValue("melee"),
			datatable.StringValue("scout"),
			datatable.StringValue("elite"),
		)); err != nil {
			t.Fatalf("SetCell: %v", err)
		}
		got, err := doc.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		changed, diffs := diffLines(src, string(got))
		if changed != 1 {
			t.Fatalf("changed %d lines, want 1:\n%v\nfull:\n%s", changed, diffs, got)
		}
		enemies, err := datatable.LoadYAML[Enemy]("enemies.yaml", got)
		if err != nil {
			t.Fatalf("LoadYAML: %v", err)
		}
		if !reflect.DeepEqual(enemies[0].Tags, []string{"melee", "scout", "elite"}) {
			t.Fatalf("tags = %v", enemies[0].Tags)
		}
	})

	t.Run("edit duration bool and ambiguous string", func(t *testing.T) {
		doc, err := datatable.ParseDocument("enemies.yaml", []byte(src))
		if err != nil {
			t.Fatalf("ParseDocument: %v", err)
		}
		if err := doc.SetCell(0, "cooldown", datatable.DurationValue(1500*time.Millisecond)); err != nil {
			t.Fatalf("SetCell cooldown: %v", err)
		}
		got1, err := doc.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if changed, diffs := diffLines(src, string(got1)); changed != 1 {
			t.Fatalf("cooldown edit changed %d lines: %v", changed, diffs)
		}

		// Edit same cell a second time: still only 1 line changed vs original.
		if err := doc.SetCell(0, "cooldown", datatable.DurationValue(250*time.Millisecond)); err != nil {
			t.Fatalf("SetCell cooldown again: %v", err)
		}
		got2, err := doc.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if changed, diffs := diffLines(src, string(got2)); changed != 1 {
			t.Fatalf("second cooldown edit changed %d lines: %v", changed, diffs)
		}

		// Ambiguous string "true" must be quoted so LoadYAML reads string.
		doc2, _ := datatable.ParseDocument("enemies.yaml", []byte(src))
		if err := doc2.SetCell(0, "display_name", datatable.StringValue("true")); err != nil {
			t.Fatalf("SetCell display_name: %v", err)
		}
		got3, err := doc2.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if changed, diffs := diffLines(src, string(got3)); changed != 1 {
			t.Fatalf("display_name edit changed %d lines: %v", changed, diffs)
		}
		enemies, err := datatable.LoadYAML[Enemy]("enemies.yaml", got3)
		if err != nil || enemies[0].Name != "true" {
			t.Fatalf("LoadYAML display_name = %q, err = %v", enemies[0].Name, err)
		}
	})

	t.Run("no-op edit preserves exact bytes", func(t *testing.T) {
		doc, err := datatable.ParseDocument("enemies.yaml", []byte(src))
		if err != nil {
			t.Fatalf("ParseDocument: %v", err)
		}
		if err := doc.SetCell(0, "hp", datatable.IntValue(45)); err != nil {
			t.Fatalf("SetCell: %v", err)
		}
		got, err := doc.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !bytes.Equal(got, []byte(src)) {
			t.Fatalf("no-op SetCell modified bytes")
		}
	})
}

func TestDocumentPreservesPerRowKeyOrderAndUnionColumns(t *testing.T) {
	src := `version: 1
rows:
  - b: 1
    a: hello
  - c: true
    a: world
    d: 500ms
    tags: [x, y]
`
	doc, err := datatable.ParseDocument("ordered.yaml", []byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	wantCols := []string{"b", "a", "c", "d", "tags"}
	if got := doc.Columns(); !reflect.DeepEqual(got, wantCols) {
		t.Fatalf("Columns = %v, want %v", got, wantCols)
	}
	if got := doc.Row(0).Keys(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("row 0 keys = %v", got)
	}
	if got := doc.Row(1).Keys(); !reflect.DeepEqual(got, []string{"c", "a", "d", "tags"}) {
		t.Fatalf("row 1 keys = %v", got)
	}

	// Verify inferred kinds for all five kinds.
	wantKinds := map[string]datatable.ValueKind{
		"b":    datatable.KindNumber,
		"a":    datatable.KindString,
		"c":    datatable.KindBool,
		"d":    datatable.KindDuration,
		"tags": datatable.KindList,
	}
	for col, wantKind := range wantKinds {
		if got := doc.ColumnKind(col); got != wantKind {
			t.Fatalf("ColumnKind(%q) = %v, want %v", col, got, wantKind)
		}
	}

	// Moving row 1 above row 0 keeps each row's key order intact.
	if err := doc.MoveRow(1, 0); err != nil {
		t.Fatalf("MoveRow: %v", err)
	}
	saved, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	reloaded, err := datatable.ParseDocument("ordered.yaml", saved)
	if err != nil {
		t.Fatalf("ParseDocument reloaded: %v", err)
	}
	if got := reloaded.Row(0).Keys(); !reflect.DeepEqual(got, []string{"c", "a", "d", "tags"}) {
		t.Fatalf("reloaded row 0 keys = %v", got)
	}
	if got := reloaded.Row(1).Keys(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("reloaded row 1 keys = %v", got)
	}
}

func TestDocumentAddDeleteMoveRowAndSave(t *testing.T) {
	doc := datatable.NewDocument()
	gotEmpty, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal empty: %v", err)
	}
	if string(gotEmpty) != "version: 1\nrows: []\n" {
		t.Fatalf("empty document = %q", gotEmpty)
	}

	idx, err := doc.AppendRow(
		datatable.Cell{Key: "enemy_id", Value: datatable.StringValue("slime")},
		datatable.Cell{Key: "hp", Value: datatable.IntValue(15)},
		datatable.Cell{Key: "is_boss", Value: datatable.BoolValue(false)},
		datatable.Cell{Key: "tags", Value: datatable.ListValue(datatable.StringValue("goo"))},
		datatable.Cell{Key: "cooldown", Value: datatable.DurationValue(750 * time.Millisecond)},
	)
	if err != nil || idx != 0 {
		t.Fatalf("AppendRow = %d, %v", idx, err)
	}
	if err := doc.AddRow(0,
		datatable.Cell{Key: "enemy_id", Value: datatable.StringValue("bat")},
		datatable.Cell{Key: "hp", Value: datatable.IntValue(10)},
	); err != nil {
		t.Fatalf("AddRow: %v", err)
	}
	if doc.RowCount() != 2 {
		t.Fatalf("RowCount = %d", doc.RowCount())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "enemies.yaml")
	if err := doc.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := datatable.LoadDocument(path)
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	if loaded.RowCount() != 2 {
		t.Fatalf("loaded RowCount = %d", loaded.RowCount())
	}
	if err := loaded.DeleteRow(0); err != nil {
		t.Fatalf("DeleteRow: %v", err)
	}
	if loaded.RowCount() != 1 {
		t.Fatalf("after delete RowCount = %d", loaded.RowCount())
	}
	v, ok := loaded.Cell(0, "enemy_id")
	if !ok || v.Text != "slime" {
		t.Fatalf("remaining row enemy_id = %+v, %v", v, ok)
	}
	// Saving after loading produces deterministic YAML.
	if err := loaded.Save(path); err != nil {
		t.Fatalf("Save after delete: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	enemies, err := datatable.LoadYAML[Enemy](path, raw)
	if err != nil {
		t.Fatalf("LoadYAML after Save: %v", err)
	}
	if len(enemies) != 1 || enemies[0].ID != "slime" || enemies[0].HP != 15 || enemies[0].Cooldown != 750*time.Millisecond {
		t.Fatalf("enemies = %+v", enemies)
	}
}

func TestDocumentRejectsInvalidEnvelopes(t *testing.T) {
	bad := []string{
		"",
		"null\n",
		"- id: a\n",
		"version: 2\nrows: []\n",
		"version: 1\nextra: 1\nrows: []\n",
		"version: 1\nrows:\n  - hp: 1\n    hp: 2\n",
		"version: 1\nrows:\n  - nested: {a: 1}\n",
		"version: 1\nrows: []\n---\nversion: 1\n",
	}
	for _, src := range bad {
		if _, err := datatable.ParseDocument("bad.yaml", []byte(src)); err == nil {
			t.Errorf("ParseDocument accepted invalid input %q", src)
		}
	}
}

func TestValueInferenceAndParsing(t *testing.T) {
	if v := datatable.InferValue("true"); v.Kind != datatable.KindBool {
		t.Fatalf("InferValue(true) = %+v", v)
	}
	if v := datatable.InferValue("42"); v.Kind != datatable.KindNumber || v.Text != "42" {
		t.Fatalf("InferValue(42) = %+v", v)
	}
	if v := datatable.InferValue("-3.5"); v.Kind != datatable.KindNumber {
		t.Fatalf("InferValue(-3.5) = %+v", v)
	}
	if v := datatable.InferValue("500ms"); v.Kind != datatable.KindDuration {
		t.Fatalf("InferValue(500ms) = %+v", v)
	}
	if v := datatable.InferValue("[1, 2]"); v.Kind != datatable.KindList || len(v.List) != 2 {
		t.Fatalf("InferValue([1, 2]) = %+v", v)
	}
	if v := datatable.InferValue("goblin"); v.Kind != datatable.KindString {
		t.Fatalf("InferValue(goblin) = %+v", v)
	}
	// Bare "0" must be KindNumber, not KindDuration.
	if v := datatable.InferValue("0"); v.Kind != datatable.KindNumber {
		t.Fatalf("InferValue(0) = %+v, want KindNumber", v)
	}

	// ParseValue checks.
	if _, err := datatable.ParseValue(datatable.KindNumber, "abc"); err == nil {
		t.Fatal("ParseValue(KindNumber, abc) succeeded")
	}
	if _, err := datatable.ParseValue(datatable.KindBool, "maybe"); err == nil {
		t.Fatal("ParseValue(KindBool, maybe) succeeded")
	}
	if _, err := datatable.ParseValue(datatable.KindDuration, "forever"); err == nil {
		t.Fatal("ParseValue(KindDuration, forever) succeeded")
	}
	lv, err := datatable.ParseValue(datatable.KindList, "a, b, c")
	if err != nil || lv.Kind != datatable.KindList || len(lv.List) != 3 {
		t.Fatalf("ParseValue(KindList, a, b, c) = %+v, %v", lv, err)
	}
}

func TestRegistryAndRowTypeValidationMatchesLoadYAML(t *testing.T) {
	reg := datatable.NewRegistry()
	if err := datatable.RegisterRowType[Enemy](reg, "data/enemies/*.yaml"); err != nil {
		t.Fatalf("RegisterRowType: %v", err)
	}
	if err := datatable.RegisterRowType[Item](reg, "items.yaml"); err != nil {
		t.Fatalf("RegisterRowType items: %v", err)
	}
	if err := datatable.RegisterRowType[Enemy](reg, "tables/**/bosses.yaml"); err != nil {
		t.Fatalf("RegisterRowType globstar: %v", err)
	}

	if got := reg.Match("data/enemies/forest.yaml"); got == nil || got.Name() != "Enemy" {
		t.Fatalf("Match(data/enemies/forest.yaml) = %v", got)
	}
	if got := reg.Match("nested/dir/items.yaml"); got == nil || got.Name() != "Item" {
		t.Fatalf("Match(nested/dir/items.yaml) = %v", got)
	}
	if got := reg.Match("tables/act1/chapter2/bosses.yaml"); got == nil || got.Name() != "Enemy" {
		t.Fatalf("Match(globstar) = %v", got)
	}
	if got := reg.Match("data/other.yaml"); got != nil {
		t.Fatalf("Match(data/other.yaml) = %v, want nil", got)
	}

	rt := reg.Match("data/enemies/forest.yaml")
	doc, err := datatable.ParseDocument("data/enemies/forest.yaml", []byte(enemiesYAML))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := rt.ValidateDocument("data/enemies/forest.yaml", doc); err != nil {
		t.Fatalf("ValidateDocument valid table: %v", err)
	}

	// Refuse unknown field.
	err = rt.ValidateCell("enemies.yaml", 0, "hit_points", datatable.IntValue(50))
	var ce *datatable.CellError
	if !errors.As(err, &ce) || !errors.Is(err, datatable.ErrUnknownField) || ce.Row != 1 || ce.Field != "hit_points" {
		t.Fatalf("unknown field error = %v", err)
	}

	// Refuse invalid int (float and non-number).
	for _, bad := range []string{"abc", "3.5"} {
		if _, err := rt.ParseCellText("enemies.yaml", 0, "hp", bad); !errors.As(err, &ce) || ce.Field != "hp" {
			t.Fatalf("ParseCellText(hp, %q) = %v, want CellError", bad, err)
		}
	}
	// Refuse invalid bool, duration, int slice element.
	if _, err := rt.ParseCellText("enemies.yaml", 0, "is_boss", "maybe"); !errors.As(err, &ce) || ce.Field != "is_boss" {
		t.Fatalf("ParseCellText(is_boss, maybe) = %v", err)
	}
	if _, err := rt.ParseCellText("enemies.yaml", 0, "cooldown", "soon"); !errors.As(err, &ce) || ce.Field != "cooldown" {
		t.Fatalf("ParseCellText(cooldown, soon) = %v", err)
	}
	if _, err := rt.ParseCellText("enemies.yaml", 0, "drop_ids", "101, nope"); !errors.As(err, &ce) || ce.Field != "drop_ids" {
		t.Fatalf("ParseCellText(drop_ids, '101, nope') = %v", err)
	}
	// Refuse int8 overflow on Item.Delta.
	itemRT := reg.Match("items.yaml")
	if _, err := itemRT.ParseCellText("items.yaml", 0, "delta", "300"); !errors.As(err, &ce) || ce.Field != "delta" {
		t.Fatalf("ParseCellText(delta, 300) = %v", err)
	}
	// Accept valid nullable pointer clear and valid values.
	if v, err := itemRT.ParseCellText("items.yaml", 0, "stack", ""); err != nil || v.Kind != datatable.KindNull {
		t.Fatalf("ParseCellText(stack, '') = %+v, %v", v, err)
	}
	if v, err := rt.ParseCellText("enemies.yaml", 0, "hp", "99"); err != nil || v.Text != "99" {
		t.Fatalf("ParseCellText(hp, 99) = %+v, %v", v, err)
	}
}

func TestDocumentRowLineAndCellLine(t *testing.T) {
	const src = `# shop items
version: 1
rows:
  - id: rusty_sword
    price: 40
    power: 2
  - id: plate_armor
    price: 600
    power: 8
`
	doc, err := datatable.ParseDocument("items.yaml", []byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if got := doc.RowLine(0); got != 4 {
		t.Fatalf("RowLine(0) = %d, want 4", got)
	}
	if got := doc.RowLine(1); got != 7 {
		t.Fatalf("RowLine(1) = %d, want 7", got)
	}
	if got := doc.CellLine(0, "price"); got != 5 {
		t.Fatalf("CellLine(0, price) = %d, want 5", got)
	}
	if got := doc.CellLine(1, "power"); got != 9 {
		t.Fatalf("CellLine(1, power) = %d, want 9", got)
	}
	if got := doc.CellLine(1, "missing"); got != 0 {
		t.Fatalf("CellLine(1, missing) = %d, want 0", got)
	}
	if got := doc.RowLine(99); got != 0 {
		t.Fatalf("RowLine(99) = %d, want 0", got)
	}
}
