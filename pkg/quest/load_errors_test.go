package quest_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/quest"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nquests:\n  - id: q1\n    extra: true\n    objectives:\n      - id: o1\n        kind: kill\n        target: slime\n")
		_, err := quest.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
		if !errors.Is(err, quest.ErrInvalidBook) {
			t.Errorf("errors.Is(err, ErrInvalidBook) = false (err: %v)", err)
		}
	})

	t.Run("duplicate quest id", func(t *testing.T) {
		data := []byte("version: 1\nquests:\n  - id: q1\n    objectives:\n      - id: o1\n        kind: kill\n        target: slime\n  - id: q1\n    objectives:\n      - id: o2\n        kind: kill\n        target: bat\n")
		_, err := quest.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "quests[1].id")
		if !errors.Is(err, quest.ErrInvalidBook) {
			t.Errorf("errors.Is(err, ErrInvalidBook) = false (err: %v)", err)
		}
	})

	t.Run("unknown prerequisite", func(t *testing.T) {
		data := []byte("version: 1\nquests:\n  - id: q1\n    requires:\n      - missing\n    objectives:\n      - id: o1\n        kind: kill\n        target: slime\n")
		_, err := quest.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "quests[0].requires[0]")
		if !errors.Is(err, quest.ErrInvalidBook) {
			t.Errorf("errors.Is(err, ErrInvalidBook) = false (err: %v)", err)
		}
	})

	t.Run("LoadFS names file on parse error", func(t *testing.T) {
		data := []byte("version: 1\nquests:\n  - id: q1\n    objectives: []\n")
		fsys := fstest.MapFS{"quests/main.yaml": &fstest.MapFile{Data: data}}
		_, err := quest.LoadFS(fsys, "quests/main.yaml")
		loaderrtest.ExpectField(t, err, "quests/main.yaml", data, "quests[0].objectives")
	})

	t.Run("LoadFS names file on missing file", func(t *testing.T) {
		_, err := quest.LoadFS(fstest.MapFS{}, "quests/missing.yaml")
		loaderrtest.Expect(t, err, "quests/missing.yaml", 0)
	})
}
