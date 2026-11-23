package quest_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/quest"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type gameSave struct {
	Gold   int            `json:"gold"`
	Quests quest.Snapshot `json:"quests"`
}

func startStore(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *save.Store) {
	t.Helper()
	var store *save.Store
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store = save.NewStorageStore(cx.Storage(), "save")
		return core.View{}
	}, opts...)
	return app, store
}

func TestQuestSurvivesSaveAndRestart(t *testing.T) {
	app, store := startStore(t)

	l, _ := newVillageLog(t)
	var bus flow.Bus
	l.Attach(&bus)
	l.StartReady()
	flow.Publish(&bus, quest.Talked{Character: "elder"})
	if err := l.Start("rat-problem"); err != nil {
		t.Fatal(err)
	}
	if err := l.Start("herbs"); err != nil {
		t.Fatal(err)
	}
	flow.Publish(&bus, quest.Talked{Character: "innkeeper"})
	flow.Publish(&bus, quest.Reached{Place: "cellar"})
	flow.Publish(&bus, quest.Defeated{Enemy: "rat", Amount: 3})
	flow.Publish(&bus, quest.Collected{Item: "moonleaf", Amount: 2})
	if err := l.Fail("herbs"); err != nil {
		t.Fatal(err)
	}

	snap := l.Snapshot()
	if err := store.Save("slot1", gameSave{Gold: 7, Quests: snap}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Copy storage into a fresh app to simulate quitting and relaunching the game.
	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	_, again := startStore(t, nsgotest.Storage(kept))
	var loaded gameSave
	if err := again.Load("slot1", &loaded); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Quests, snap) {
		t.Fatalf("loaded snapshot %+v, want %+v", loaded.Quests, snap)
	}

	restored, updates := newVillageLog(t)
	if err := restored.Restore(loaded.Quests); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(*updates) != 0 {
		t.Fatalf("Restore delivered updates: %v", kinds(*updates))
	}
	for _, id := range []string{"welcome", "rat-problem", "herbs"} {
		if restored.State(id) != l.State(id) {
			t.Fatalf("%s = %s, want %s", id, restored.State(id), l.State(id))
		}
		if !reflect.DeepEqual(restored.Objectives(id), l.Objectives(id)) {
			t.Fatalf("%s objectives = %+v, want %+v", id, restored.Objectives(id), l.Objectives(id))
		}
	}

	// The restored quest keeps advancing from where the save left it.
	var bus2 flow.Bus
	restored.Attach(&bus2)
	flow.Publish(&bus2, quest.Defeated{Enemy: "rat", Amount: 2})
	flow.Publish(&bus2, quest.Talked{Character: "innkeeper"})
	if restored.State("rat-problem") != quest.Completed {
		t.Fatalf("rat-problem after restore = %s, want completed", restored.State("rat-problem"))
	}
	if got := kinds(*updates); got[len(got)-1] != "questCompleted:rat-problem" {
		t.Fatalf("updates = %v", got)
	}
}

func TestSnapshotJSONIsStable(t *testing.T) {
	l, _ := newVillageLog(t)
	l.StartReady()
	l.Handle(quest.Talked{Character: "elder"}.Event())
	if err := l.Start("herbs"); err != nil {
		t.Fatal(err)
	}
	l.Handle(quest.Collected{Item: "sunroot"}.Event())
	l.Handle(quest.Collected{Item: "moonleaf", Amount: 3}.Event())
	got, err := json.Marshal(l.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"quests":[{"id":"welcome","state":"completed","progress":{"meet-elder":1}},{"id":"herbs","state":"active","progress":{"moonleaf":3,"sunroot":1}}]}`
	if string(got) != want {
		t.Fatalf("snapshot JSON\n%s\nwant\n%s", got, want)
	}
	var back quest.Snapshot
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, l.Snapshot()) {
		t.Fatalf("JSON round trip = %+v", back)
	}
}

func TestRestoreRefusesInvalidSnapshots(t *testing.T) {
	welcome := quest.QuestRecord{ID: "welcome", State: quest.Completed, Progress: map[string]int{"meet-elder": 1}}
	with := func(recs ...quest.QuestRecord) quest.Snapshot {
		return quest.Snapshot{Version: 1, Quests: append([]quest.QuestRecord{welcome}, recs...)}
	}
	cases := map[string]quest.Snapshot{
		"version":                 {Version: 2},
		"version 0 quests":        {Quests: []quest.QuestRecord{welcome}},
		"unknown quest":           with(quest.QuestRecord{ID: "nope", State: quest.Active}),
		"duplicate":               {Version: 1, Quests: []quest.QuestRecord{{ID: "welcome", State: quest.Active}, {ID: "welcome", State: quest.Active}}},
		"inactive":                {Version: 1, Quests: []quest.QuestRecord{{ID: "welcome", State: quest.Inactive}}},
		"unknown obj":             with(quest.QuestRecord{ID: "herbs", State: quest.Active, Progress: map[string]int{"nope": 1}}),
		"over count":              with(quest.QuestRecord{ID: "herbs", State: quest.Active, Progress: map[string]int{"moonleaf": 5}}),
		"negative":                with(quest.QuestRecord{ID: "herbs", State: quest.Active, Progress: map[string]int{"moonleaf": -1}}),
		"active but done":         with(quest.QuestRecord{ID: "herbs", State: quest.Active, Progress: map[string]int{"moonleaf": 4, "sunroot": 2}}),
		"abandoned with progress": with(quest.QuestRecord{ID: "herbs", State: quest.Abandoned, Progress: map[string]int{"moonleaf": 1}}),
		"missing prerequisite":    {Version: 1, Quests: []quest.QuestRecord{{ID: "herbs", State: quest.Active}}},
		"prerequisite not done":   {Version: 1, Quests: []quest.QuestRecord{{ID: "welcome", State: quest.Active}, {ID: "rat-problem", State: quest.Completed}}},
		"ordered out of turn":     with(quest.QuestRecord{ID: "rat-problem", State: quest.Active, Progress: map[string]int{"kill-rats": 2}}),
		"ordered skips required":  with(quest.QuestRecord{ID: "rat-problem", State: quest.Failed, Progress: map[string]int{"talk-innkeeper": 1, "report": 1}}),
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			l, _ := newVillageLog(t)
			l.StartReady()
			before := l.Snapshot()
			if err := l.Restore(snap); !errors.Is(err, quest.ErrInvalidSnapshot) {
				t.Fatalf("Restore error = %v, want ErrInvalidSnapshot", err)
			}
			if !reflect.DeepEqual(l.Snapshot(), before) {
				t.Fatal("a refused Restore changed the log")
			}
		})
	}
	var s quest.Snapshot
	if err := json.Unmarshal([]byte(`{"version":1,"quests":[{"id":"herbs","state":"sleeping"}]}`), &s); err == nil {
		t.Fatal("unknown state name decoded")
	}
}

func TestRestoreAcceptsReachableSnapshots(t *testing.T) {
	welcome := quest.QuestRecord{ID: "welcome", State: quest.Completed, Progress: map[string]int{"meet-elder": 1}}
	cases := map[string]quest.Snapshot{
		// An optional objective left incomplete never gates the required ones after it.
		"ordered with optional": {Version: 1, Quests: []quest.QuestRecord{welcome,
			{ID: "rat-problem", State: quest.Completed, Progress: map[string]int{"talk-innkeeper": 1, "find-cellar": 1, "kill-rats": 5, "report": 1}},
		}},
		"ordered in progress": {Version: 1, Quests: []quest.QuestRecord{welcome,
			{ID: "rat-problem", State: quest.Active, Progress: map[string]int{"talk-innkeeper": 1, "find-cellar": 1, "kill-rats": 2}},
		}},
		"abandoned": {Version: 1, Quests: []quest.QuestRecord{welcome, {ID: "herbs", State: quest.Abandoned}}},
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			l, _ := newVillageLog(t)
			if err := l.Restore(snap); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if !reflect.DeepEqual(l.Snapshot(), snap) {
				t.Fatalf("Snapshot after Restore = %+v, want %+v", l.Snapshot(), snap)
			}
		})
	}

	// A repeatable prerequisite may run again after the quest that needs it began.
	b := &quest.Book{Version: 1, Quests: []quest.Quest{
		{ID: "daily", Repeatable: true, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindDefeat, Target: "slime"}}},
		{ID: "after", Requires: []string{"daily"}, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindReach, Target: "gate"}}},
	}}
	l, err := quest.NewLog(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return l.Start("daily") },
		func() error { l.Handle(quest.Defeated{Enemy: "slime"}.Event()); return nil },
		func() error { return l.Start("after") },
		func() error { return l.Start("daily") },
		func() error { return l.Abandon("daily") },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	again, err := quest.NewLog(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Restore(l.Snapshot()); err != nil {
		t.Fatalf("Restore with a restarted repeatable prerequisite: %v", err)
	}
}

func TestRestoreZeroSnapshotMeansNoProgress(t *testing.T) {
	// A slot saved before the game tracked quests decodes its missing quests field to the zero Snapshot.
	var old gameSave
	if err := json.Unmarshal([]byte(`{"gold":3}`), &old); err != nil {
		t.Fatal(err)
	}
	l, _ := newVillageLog(t)
	l.StartReady()
	l.Handle(quest.Talked{Character: "elder"}.Event())
	if err := l.Restore(old.Quests); err != nil {
		t.Fatalf("Restore(zero) = %v", err)
	}
	if got := l.Snapshot(); len(got.Quests) != 0 {
		t.Fatalf("Restore(zero) kept progress: %+v", got)
	}
	if n := l.StartReady(); n != 1 || l.State("welcome") != quest.Active {
		t.Fatalf("StartReady after Restore(zero) = %d, welcome %s", n, l.State("welcome"))
	}
}
