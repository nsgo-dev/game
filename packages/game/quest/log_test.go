package quest_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/quest"
)

func newVillageLog(t testing.TB) (*quest.Log, *[]quest.Update) {
	t.Helper()
	l, err := quest.NewLog(loadVillage(t))
	if err != nil {
		t.Fatalf("NewLog: %v", err)
	}
	var updates []quest.Update
	l.OnUpdate = func(u quest.Update) { updates = append(updates, u) }
	return l, &updates
}

func kinds(us []quest.Update) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Kind.String() + ":" + u.Quest
		if u.Objective != "" {
			out[i] += "/" + u.Objective + fmt.Sprintf("=%d", u.Progress)
		}
	}
	return out
}

func TestQuestAdvancesOnBusEvents(t *testing.T) {
	l, updates := newVillageLog(t)
	var bus flow.Bus
	var published []quest.Update
	flow.Subscribe(&bus, func(u quest.Update) { published = append(published, u) })
	detach := l.Attach(&bus)

	if n := l.StartReady(); n != 1 || l.State("welcome") != quest.Active {
		t.Fatalf("StartReady = %d, welcome %s", n, l.State("welcome"))
	}
	if l.CanStart("rat-problem") {
		t.Fatal("rat-problem startable before welcome completes")
	}
	flow.Publish(&bus, quest.Talked{Character: "elder"})
	if l.State("welcome") != quest.Completed {
		t.Fatalf("welcome = %s, want completed", l.State("welcome"))
	}
	if err := l.Start("rat-problem"); err != nil {
		t.Fatalf("Start rat-problem: %v", err)
	}

	// Ordered: the cellar does not count before the innkeeper is met.
	flow.Publish(&bus, quest.Reached{Place: "cellar"})
	if st, _ := l.Objective("rat-problem", "find-cellar"); st.State != quest.ObjectiveLocked || st.Progress != 0 {
		t.Fatalf("find-cellar before talk = %+v", st)
	}
	// One innkeeper talk completes talk-innkeeper but does not also complete the final report.
	flow.Publish(&bus, quest.Talked{Character: "innkeeper"})
	if st, _ := l.Objective("rat-problem", "report"); st.Progress != 0 {
		t.Fatalf("report advanced by the same talk: %+v", st)
	}
	flow.Publish(&bus, quest.Reached{Place: "cellar"})
	flow.Publish(&bus, quest.Defeated{Enemy: "rat", Amount: 2})
	flow.Enqueue(&bus, quest.Defeated{Enemy: "rat"})
	flow.Enqueue(&bus, quest.Event{Kind: quest.KindDefeat, Target: "rat", Amount: 9})
	bus.Flush()
	if st, _ := l.Objective("rat-problem", "kill-rats"); st.Progress != 5 || st.State != quest.ObjectiveDone {
		t.Fatalf("kill-rats = %+v, want 5/5 completed", st)
	}
	// The optional objective does not block the report.
	if st, _ := l.Objective("rat-problem", "report"); st.State != quest.ObjectiveActive {
		t.Fatalf("report = %+v, want active", st)
	}
	flow.Publish(&bus, quest.Collected{Item: "rat-tail"})
	flow.Publish(&bus, quest.Talked{Character: "innkeeper"})
	if l.State("rat-problem") != quest.Completed {
		t.Fatalf("rat-problem = %s, want completed", l.State("rat-problem"))
	}

	want := []string{
		"questStarted:welcome",
		"objectiveProgressed:welcome/meet-elder=1",
		"objectiveCompleted:welcome/meet-elder=1",
		"questCompleted:welcome",
		"questStarted:rat-problem",
		"objectiveProgressed:rat-problem/talk-innkeeper=1",
		"objectiveCompleted:rat-problem/talk-innkeeper=1",
		"objectiveProgressed:rat-problem/find-cellar=1",
		"objectiveCompleted:rat-problem/find-cellar=1",
		"objectiveProgressed:rat-problem/kill-rats=2",
		"objectiveProgressed:rat-problem/kill-rats=3",
		"objectiveProgressed:rat-problem/kill-rats=5",
		"objectiveCompleted:rat-problem/kill-rats=5",
		"objectiveProgressed:rat-problem/tails=1",
		"objectiveProgressed:rat-problem/report=1",
		"objectiveCompleted:rat-problem/report=1",
		"questCompleted:rat-problem",
	}
	if got := kinds(*updates); !slices.Equal(got, want) {
		t.Fatalf("updates:\n%v\nwant:\n%v", got, want)
	}
	if !reflect.DeepEqual(published, *updates) {
		t.Fatal("bus updates differ from OnUpdate updates")
	}
	last := (*updates)[len(*updates)-1]
	wantRewards := []quest.Reward{{Kind: "gold", ID: "coin", Amount: 50}, {Kind: "item", ID: "cellar-key"}}
	if !reflect.DeepEqual(last.Rewards, wantRewards) {
		t.Fatalf("rewards = %+v, want %+v", last.Rewards, wantRewards)
	}

	detach()
	detach()
	before := len(*updates)
	if err := l.Start("herbs"); err != nil {
		t.Fatal(err)
	}
	flow.Publish(&bus, quest.Collected{Item: "moonleaf"})
	if st, _ := l.Objective("herbs", "moonleaf"); st.Progress != 0 {
		t.Fatalf("detached log still handles bus events: %+v", st)
	}
	if len(published) != before {
		t.Fatal("detached log still publishes updates")
	}
}

func TestStartRules(t *testing.T) {
	l, _ := newVillageLog(t)
	if err := l.Start("nope"); !errors.Is(err, quest.ErrUnknownQuest) {
		t.Fatalf("Start unknown = %v", err)
	}
	if err := l.Start("herbs"); !errors.Is(err, quest.ErrNotStartable) {
		t.Fatalf("Start before prerequisite = %v", err)
	}
	if err := l.Start("welcome"); err != nil {
		t.Fatal(err)
	}
	if err := l.Start("welcome"); !errors.Is(err, quest.ErrNotStartable) {
		t.Fatalf("Start active = %v", err)
	}
	l.Handle(quest.Talked{Character: "elder"}.Event())
	if err := l.Start("welcome"); !errors.Is(err, quest.ErrNotStartable) {
		t.Fatalf("Start completed non-repeatable = %v", err)
	}
	if l.StartReady() != 0 {
		t.Fatal("StartReady restarted a completed quest")
	}

	// Repeatable quests start again from zero once completed.
	if err := l.Start("herbs"); err != nil {
		t.Fatal(err)
	}
	if err := l.SetProgress("herbs", "moonleaf", 99); err != nil {
		t.Fatal(err)
	}
	if err := l.SetProgress("herbs", "moonleaf", 1); !errors.Is(err, quest.ErrNotActive) {
		t.Fatalf("SetProgress on completed objective = %v", err)
	}
	if err := l.SetProgress("herbs", "nope", 1); !errors.Is(err, quest.ErrUnknownObjective) {
		t.Fatalf("SetProgress unknown objective = %v", err)
	}
	l.Handle(quest.Event{Kind: quest.KindCollect, Target: "sunroot", Amount: 1})
	// A huge amount clamps to the count instead of overflowing.
	l.Handle(quest.Event{Kind: quest.KindCollect, Target: "sunroot", Amount: math.MaxInt})
	if l.State("herbs") != quest.Completed {
		t.Fatalf("herbs = %s", l.State("herbs"))
	}
	if err := l.Start("herbs"); err != nil {
		t.Fatalf("restart repeatable: %v", err)
	}
	if st, _ := l.Objective("herbs", "moonleaf"); st.Progress != 0 || st.State != quest.ObjectiveActive {
		t.Fatalf("restarted moonleaf = %+v", st)
	}
	if got := l.Quests(quest.Active); !slices.Equal(got, []string{"herbs"}) {
		t.Fatalf("Active = %v", got)
	}
	if got := l.Quests(quest.Completed); !slices.Equal(got, []string{"welcome"}) {
		t.Fatalf("Completed = %v", got)
	}
}

func TestFailAndAbandon(t *testing.T) {
	l, updates := newVillageLog(t)
	l.StartReady()
	l.Handle(quest.Event{Kind: quest.KindTalk, Target: "elder"})
	if err := l.Start("herbs"); err != nil {
		t.Fatal(err)
	}
	l.Handle(quest.Collected{Item: "moonleaf", Amount: 3}.Event())
	if err := l.Fail("herbs"); err != nil {
		t.Fatal(err)
	}
	if l.State("herbs") != quest.Failed {
		t.Fatalf("herbs = %s", l.State("herbs"))
	}
	if l.Handle(quest.Collected{Item: "moonleaf"}.Event()) {
		t.Fatal("failed quest handled an event")
	}
	if st, _ := l.Objective("herbs", "moonleaf"); st.Progress != 3 || st.State != quest.ObjectiveLocked {
		t.Fatalf("failed moonleaf = %+v", st)
	}
	if err := l.Fail("herbs"); !errors.Is(err, quest.ErrNotActive) {
		t.Fatalf("Fail twice = %v", err)
	}
	if err := l.Start("herbs"); err != nil {
		t.Fatalf("retry failed quest: %v", err)
	}
	l.Handle(quest.Collected{Item: "moonleaf"}.Event())
	if err := l.Abandon("herbs"); err != nil {
		t.Fatal(err)
	}
	if l.State("herbs") != quest.Abandoned {
		t.Fatalf("abandoned herbs = %s", l.State("herbs"))
	}
	if st, _ := l.Objective("herbs", "moonleaf"); st.Progress != 0 {
		t.Fatalf("abandoned progress kept: %+v", st)
	}
	if err := l.Abandon("herbs"); !errors.Is(err, quest.ErrNotActive) {
		t.Fatalf("Abandon inactive = %v", err)
	}
	got := kinds((*updates)[len(*updates)-4:])
	want := []string{"questFailed:herbs", "questStarted:herbs", "objectiveProgressed:herbs/moonleaf=1", "questAbandoned:herbs"}
	if !slices.Equal(got, want) {
		t.Fatalf("updates = %v, want %v", got, want)
	}
}

func TestAbandonedAutoStartQuestStaysAbandoned(t *testing.T) {
	b := &quest.Book{Version: 1, Quests: []quest.Quest{
		{ID: "intro", AutoStart: true, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindTalk, Target: "guide"}}},
		{ID: "side", Objectives: []quest.Objective{{ID: "o", Kind: quest.KindReach, Target: "well"}}},
	}}
	l, err := quest.NewLog(b)
	if err != nil {
		t.Fatal(err)
	}
	l.StartReady()
	if err := l.Abandon("intro"); err != nil {
		t.Fatal(err)
	}
	if err := l.Start("side"); err != nil {
		t.Fatal(err)
	}
	// Completing another quest runs auto-start, which must skip the abandoned quest.
	l.Handle(quest.Reached{Place: "well"}.Event())
	if l.State("side") != quest.Completed {
		t.Fatalf("side = %s", l.State("side"))
	}
	if n := l.StartReady(); n != 0 || l.State("intro") != quest.Abandoned {
		t.Fatalf("StartReady = %d, intro %s; want 0 and abandoned", n, l.State("intro"))
	}

	// The abandoned state survives a save, so reloading does not restart the quest either.
	reloaded, err := quest.NewLog(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Restore(l.Snapshot()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if n := reloaded.StartReady(); n != 0 || reloaded.State("intro") != quest.Abandoned {
		t.Fatalf("after reload StartReady = %d, intro %s; want 0 and abandoned", n, reloaded.State("intro"))
	}
	if !reloaded.CanStart("intro") {
		t.Fatal("an abandoned quest cannot be started again")
	}
	if err := reloaded.Start("intro"); err != nil || reloaded.State("intro") != quest.Active {
		t.Fatalf("Start abandoned intro = %v, state %s", err, reloaded.State("intro"))
	}
}

func TestAutoStartChainAndNestedCalls(t *testing.T) {
	b := &quest.Book{Version: 1, Quests: []quest.Quest{
		{ID: "a", AutoStart: true, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindReach, Target: "gate"}}},
		{ID: "b", AutoStart: true, Requires: []string{"a"}, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindReach, Target: "gate"}}},
		{ID: "c", Requires: []string{"b"}, Objectives: []quest.Objective{{ID: "o", Kind: quest.KindDefeat, Target: "boss"}}},
	}}
	l, err := quest.NewLog(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Quests[0].Objectives[0].Target = "changed"
	var got []string
	l.OnUpdate = func(u quest.Update) {
		got = append(got, u.Kind.String()+":"+u.Quest)
		// A handler that starts the follow-up quest itself: its updates arrive after the current ones.
		if u.Kind == quest.QuestCompleted && u.Quest == "b" {
			if err := l.Start("c"); err != nil {
				t.Errorf("nested Start: %v", err)
			}
		}
	}
	l.StartReady()
	// Reaching the gate completes a; b auto-starts but the same event does not count for it.
	l.Handle(quest.Reached{Place: "gate"}.Event())
	if l.State("b") != quest.Active {
		t.Fatalf("b = %s, want active", l.State("b"))
	}
	if st, _ := l.Objective("b", "o"); st.Progress != 0 {
		t.Fatalf("b advanced by the event that started it: %+v", st)
	}
	l.Handle(quest.Reached{Place: "gate"}.Event())
	want := []string{
		"questStarted:a",
		"objectiveProgressed:a", "objectiveCompleted:a", "questCompleted:a", "questStarted:b",
		"objectiveProgressed:b", "objectiveCompleted:b", "questCompleted:b", "questStarted:c",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("updates = %v\nwant %v", got, want)
	}
	if l.Book().Quests[0].Objectives[0].Target != "gate" {
		t.Fatal("Log shares the caller's book")
	}
}

func TestStringers(t *testing.T) {
	if quest.Failed.String() != "failed" || quest.State(9).String() != "State(9)" {
		t.Fatal("State.String")
	}
	if quest.ObjectiveCompleted.String() != "objectiveCompleted" || quest.ObjectiveDone.String() != "done" || quest.ObjectiveLocked.String() != "locked" {
		t.Fatal("String names")
	}
	var s quest.State
	if err := s.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("UnmarshalText accepted bogus")
	}
	if _, err := quest.State(9).MarshalText(); err == nil {
		t.Fatal("MarshalText accepted an invalid state")
	}
}

func BenchmarkHandle(b *testing.B) {
	book := &quest.Book{Version: 1}
	for i := range 200 {
		book.Quests = append(book.Quests, quest.Quest{
			ID: fmt.Sprintf("q%d", i),
			Objectives: []quest.Objective{
				{ID: "kill", Kind: quest.KindDefeat, Target: fmt.Sprintf("enemy%d", i%20), Count: 1 << 30},
				{ID: "loot", Kind: quest.KindCollect, Target: "gem", Count: 1 << 30},
			},
		})
	}
	l, err := quest.NewLog(book)
	if err != nil {
		b.Fatal(err)
	}
	for _, q := range book.Quests {
		if err := l.Start(q.ID); err != nil {
			b.Fatal(err)
		}
	}
	miss := quest.Event{Kind: quest.KindTalk, Target: "nobody"}
	hit := quest.Event{Kind: quest.KindDefeat, Target: "enemy3"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if i%2 == 0 {
			l.Handle(miss)
		} else {
			l.Handle(hit)
		}
	}
}
