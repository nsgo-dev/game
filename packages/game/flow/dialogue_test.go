package flow_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
)

const merchantQuestYarn = `
// Multi-node merchant & dungeon quest script.
title: Merchant
tags: shop hub
---
<<declare $gold = 20>>
<<declare $has_key = false>>
<<declare $discount = false>>

<<if $has_key>>
    Merchant: Good luck inside the dungeon, {hero}! #line:merchant_good_luck #mood:encouraging
    <<jump Farewell>>
<<elseif visited_count("Merchant") > 1 && visited("Farewell")>>
    Merchant: Back again, {$hero}? Still {$gold} gold in your pouch. #line:merchant_return
<<else>>
    Merchant: Welcome, {$hero}! You carry {$gold} gold. #line:merchant_welcome #mood:happy
<<endif>>

-> Buy the dungeon key (10 gold) <<if $gold >= 10 && !$has_key>> #line:opt_buy_key #action:trade
    <<set $gold -= 10>>
    <<set $has_key = true>>
    <<give_item key 1>>
    <<play_sfx chime>>
    Merchant: Pleasure doing business! {$gold} gold remains. #line:merchant_sold
    <<jump QuestHook>>
-> Ask about the dungeon #line:opt_ask_dungeon
    Merchant: The north gate is sealed tightly. #line:merchant_rumor
    -> Any advice for getting through? #line:opt_advice
        <<set $discount = true>>
        Merchant: Bring a lantern and item \#1 from my crate. #line:merchant_advice
    -> Never mind. #line:opt_nevermind
        Merchant: Suit yourself. #line:merchant_suit
    <<jump Farewell>>
-> Leave #line:opt_leave
    <<jump Farewell>>
===

title: QuestHook
---
A faint chime echoes from the north gate. #line:narrator_chime
<<grant_reward "Iron Key" 1 {$gold}>>
<<if $gold >= 15 and $has_key>>
    Merchant: You even have coin left for potions! #line:merchant_rich
<<elseif $gold > 0 or $discount>>
    Merchant: Spend your last {$gold} gold wisely. #line:merchant_wise
<<else>>
    Merchant: Broke, eh? Make that key count! #line:merchant_broke
<<endif>>
<<stop>>
Merchant: This line after stop is never reached.
===

title: Farewell
---
Merchant: Safe travels! #line:merchant_farewell
===
`

func TestDialogueMerchantQuestBranchesAndBusEvents(t *testing.T) {
	script, err := flow.ParseYarn(merchantQuestYarn)
	if err != nil {
		t.Fatalf("ParseYarn failed: %v", err)
	}

	wantNodes := []string{"Merchant", "QuestHook", "Farewell"}
	if got := script.Nodes(); !slices.Equal(got, wantNodes) {
		t.Fatalf("script.Nodes() = %v, want %v", got, wantNodes)
	}
	if !script.HasNode("Merchant") || script.HasNode("Missing") {
		t.Fatalf("HasNode mismatch: Merchant=%v Missing=%v", script.HasNode("Merchant"), script.HasNode("Missing"))
	}
	if hdr := script.NodeHeaders("Merchant"); hdr["tags"] != "shop hub" {
		t.Fatalf("Merchant headers = %v, want tags='shop hub'", hdr)
	}

	// Branch A: Rich hero buys the key, fires Bus events & OnCommand, jumps to QuestHook, hits <<stop>>.
	var bus flow.Bus
	var busEvents []flow.DialogueEvent
	var cbEvents []flow.DialogueEvent
	flow.Subscribe(&bus, func(ev flow.DialogueEvent) {
		busEvents = append(busEvents, ev)
	})

	runner := flow.NewDialogueRunner(script, flow.DialogueOptions{
		Bus: &bus,
		OnCommand: func(ev flow.DialogueEvent) {
			cbEvents = append(cbEvents, ev)
		},
	})
	runner.SetVar("hero", "Aria")
	runner.SetVar("$gold", 25) // leading '$' and Go int normalized cleanly

	// Drive steps inside a game.Loop under nsgotest to verify seamless loop + bus integration.
	var steps []flow.DialogueStep
	var loopErr error
	tick := 0
	app := startLoop(t, 60, func(dt time.Duration) {
		tick++
		if loopErr != nil {
			return
		}
		var st flow.DialogueStep
		switch tick {
		case 1:
			st, loopErr = runner.Start("Merchant")
		case 2:
			st, loopErr = runner.Continue()
		case 3:
			st, loopErr = runner.Choose(0) // Buy key
		case 4:
			st, loopErr = runner.Continue() // Jump -> QuestHook narration
		case 5:
			st, loopErr = runner.Continue() // QuestHook <<if>> line
		case 6:
			st, loopErr = runner.Continue() // <<stop>> -> StepEnded
		default:
			return
		}
		if loopErr == nil {
			steps = append(steps, st)
		}
	})

	app.Advance(120 * time.Millisecond)
	if loopErr != nil {
		t.Fatalf("dialogue loop step failed: %v", loopErr)
	}
	if len(steps) != 6 {
		t.Fatalf("got %d steps, want 6", len(steps))
	}

	// Step 0: Merchant greeting line
	if steps[0].Kind != flow.StepLine {
		t.Fatalf("step 0 kind = %v, want line", steps[0].Kind)
	}
	if steps[0].Line.Speaker != "Merchant" || steps[0].Line.Text != "Welcome, Aria! You carry 25 gold." {
		t.Fatalf("step 0 line = %+v", steps[0].Line)
	}
	if steps[0].Line.LineID != "merchant_welcome" || !slices.Equal(steps[0].Line.Tags, []string{"mood:happy"}) {
		t.Fatalf("step 0 metadata: id=%q tags=%v", steps[0].Line.LineID, steps[0].Line.Tags)
	}

	// Step 1: Choices (all 3 available since gold=25 >= 10)
	if steps[1].Kind != flow.StepChoices || len(steps[1].Choices) != 3 {
		t.Fatalf("step 1 = %+v, want 3 choices", steps[1])
	}
	for i, c := range steps[1].Choices {
		if c.Index != i || !c.Available {
			t.Fatalf("choice %d = %+v, want index=%d available=true", i, c, i)
		}
	}
	if steps[1].Choices[0].LineID != "opt_buy_key" || !slices.Equal(steps[1].Choices[0].Tags, []string{"action:trade"}) {
		t.Fatalf("choice 0 metadata = %+v", steps[1].Choices[0])
	}

	// Step 2: Sold line after <<set>>, <<give_item>>, <<play_sfx>>
	if steps[2].Kind != flow.StepLine || steps[2].Line.Text != "Pleasure doing business! 15 gold remains." {
		t.Fatalf("step 2 = %+v", steps[2])
	}

	// Step 3: Narration line in QuestHook (no speaker)
	if steps[3].Kind != flow.StepLine || steps[3].Line.Speaker != "" || steps[3].Line.Text != "A faint chime echoes from the north gate." {
		t.Fatalf("step 3 = %+v", steps[3])
	}

	// Step 4: <<if $gold >= 15 and $has_key>> branch
	if steps[4].Kind != flow.StepLine || steps[4].Line.Text != "You even have coin left for potions!" {
		t.Fatalf("step 4 = %+v", steps[4])
	}

	// Step 5: <<stop>> ends dialogue before unreachable line
	if steps[5].Kind != flow.StepEnded {
		t.Fatalf("step 5 kind = %v, want ended", steps[5].Kind)
	}

	// Verify Bus and OnCommand received identical typed DialogueEvents.
	wantEvents := []flow.DialogueEvent{
		{Command: "give_item", Args: []string{"key", "1"}, Node: "Merchant"},
		{Command: "play_sfx", Args: []string{"chime"}, Node: "Merchant"},
		{Command: "grant_reward", Args: []string{"Iron Key", "1", "15"}, Node: "QuestHook"},
	}
	if len(busEvents) != len(wantEvents) || len(cbEvents) != len(wantEvents) {
		t.Fatalf("events len: bus=%d cb=%d want=%d\nbus=%+v", len(busEvents), len(cbEvents), len(wantEvents), busEvents)
	}
	for i, want := range wantEvents {
		if busEvents[i].Command != want.Command || busEvents[i].Node != want.Node || !slices.Equal(busEvents[i].Args, want.Args) {
			t.Fatalf("busEvents[%d] = %+v, want %+v", i, busEvents[i], want)
		}
		if cbEvents[i].Command != want.Command || cbEvents[i].Node != want.Node || !slices.Equal(cbEvents[i].Args, want.Args) {
			t.Fatalf("cbEvents[%d] = %+v, want %+v", i, cbEvents[i], want)
		}
	}

	// Verify variable and visit queries.
	gold, ok := runner.Var("gold")
	if !ok || gold != float64(15) {
		t.Fatalf("Var(gold) = %v, %v; want 15, true", gold, ok)
	}
	hasKey, ok := runner.Var("$has_key")
	if !ok || hasKey != true {
		t.Fatalf("Var($has_key) = %v, %v; want true, true", hasKey, ok)
	}
	if runner.Visited("Merchant") != 1 || runner.Visited("QuestHook") != 1 || runner.Visited("Farewell") != 0 {
		t.Fatalf("Visited counts: Merchant=%d QuestHook=%d Farewell=%d",
			runner.Visited("Merchant"), runner.Visited("QuestHook"), runner.Visited("Farewell"))
	}

	// Re-entering Merchant now takes the <<if $has_key>> branch and jumps to Farewell.
	st, err := runner.Start("Merchant")
	if err != nil {
		t.Fatalf("second Start(Merchant) failed: %v", err)
	}
	if st.Kind != flow.StepLine || st.Line.Text != "Good luck inside the dungeon, Aria!" {
		t.Fatalf("revisit step 1 = %+v", st)
	}
	st, err = runner.Continue()
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Safe travels!" {
		t.Fatalf("revisit step 2 = %+v, err=%v", st, err)
	}
	st, err = runner.Continue()
	if err != nil || st.Kind != flow.StepEnded {
		t.Fatalf("revisit step 3 = %+v, err=%v", st, err)
	}
	if runner.Visited("Merchant") != 2 || runner.Visited("Farewell") != 1 {
		t.Fatalf("Visited after revisit: Merchant=%d Farewell=%d", runner.Visited("Merchant"), runner.Visited("Farewell"))
	}

	// Branch B: Poor hero (5 gold) cannot pick buy option; explores nested choice & escaped \#.
	poor := flow.NewDialogueRunner(script, flow.DialogueOptions{})
	poor.SetVar("hero", "Bram")
	poor.SetVar("gold", 5)

	st, err = poor.Start("Merchant")
	if err != nil || st.Kind != flow.StepLine {
		t.Fatalf("poor Start = %+v, err=%v", st, err)
	}
	st, err = poor.Continue()
	if err != nil || st.Kind != flow.StepChoices {
		t.Fatalf("poor choices = %+v, err=%v", st, err)
	}
	if st.Choices[0].Available {
		t.Fatalf("buy option should be unavailable with 5 gold: %+v", st.Choices[0])
	}
	if !st.Choices[1].Available || !st.Choices[2].Available {
		t.Fatalf("ask/leave options should be available: %+v", st.Choices)
	}
	if _, err := poor.Choose(0); !errors.Is(err, flow.ErrChoiceUnavailable) {
		t.Fatalf("Choose(unavailable) err = %v, want ErrChoiceUnavailable", err)
	}

	// Pick "Ask about the dungeon" -> nested choice menu -> pick advice -> jump Farewell.
	st, err = poor.Choose(1)
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "The north gate is sealed tightly." {
		t.Fatalf("ask branch line = %+v, err=%v", st, err)
	}
	st, err = poor.Continue()
	if err != nil || st.Kind != flow.StepChoices || len(st.Choices) != 2 {
		t.Fatalf("nested choices = %+v, err=%v", st, err)
	}
	st, err = poor.Choose(0)
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Bring a lantern and item #1 from my crate." {
		t.Fatalf("nested advice line = %+v, err=%v", st, err)
	}
	st, err = poor.Continue()
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Safe travels!" {
		t.Fatalf("farewell after advice = %+v, err=%v", st, err)
	}
	st, err = poor.Continue()
	if err != nil || st.Kind != flow.StepEnded {
		t.Fatalf("end after farewell = %+v, err=%v", st, err)
	}

	// Revisit as poor hero hits <<elseif visited("Merchant") > 1>>.
	st, err = poor.Start("Merchant")
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Back again, Bram? Still 5 gold in your pouch." {
		t.Fatalf("poor revisit line = %+v, err=%v", st, err)
	}
}

func TestDialogueLocalizationStringTableAndFallback(t *testing.T) {
	script, err := flow.ParseYarn(merchantQuestYarn)
	if err != nil {
		t.Fatalf("ParseYarn failed: %v", err)
	}

	catalogs := map[string]map[string]string{
		"en": {
			"merchant_welcome": "Greetings, {$hero}! You have {$gold} gold coins.",
			"opt_buy_key":      "Purchase dungeon key (10g)",
			// opt_ask_dungeon intentionally omitted to test fallback to script source text.
			"opt_leave":         "Walk away",
			"merchant_farewell": "May the roads favor you!",
		},
		"ja": {
			"merchant_welcome": "ようこそ、{hero}！所持金は{$gold}ゴールドですね。",
			"opt_buy_key":      "ダンジョンの鍵を買う（10ゴールド）",
			// opt_ask_dungeon intentionally omitted to test fallback.
			"opt_leave":         "立ち去る",
			"merchant_farewell": "良い旅を！",
		},
	}

	locale := "en"
	resolver := func(lineID, fallback string, vars map[string]any) string {
		if table, ok := catalogs[locale]; ok {
			if localized, ok := table[lineID]; ok {
				return localized
			}
		}
		return fallback
	}

	runner := flow.NewDialogueRunner(script, flow.DialogueOptions{
		StringResolver: resolver,
	})
	runner.SetVar("hero", "Sora")
	runner.SetVar("gold", 30)

	// EN locale
	st, err := runner.Start("Merchant")
	if err != nil || st.Line.Text != "Greetings, Sora! You have 30 gold coins." {
		t.Fatalf("EN welcome = %+v, err=%v", st, err)
	}
	st, err = runner.Continue()
	if err != nil || st.Kind != flow.StepChoices {
		t.Fatalf("EN choices = %+v, err=%v", st, err)
	}
	if st.Choices[0].Text != "Purchase dungeon key (10g)" {
		t.Fatalf("EN choice 0 = %q", st.Choices[0].Text)
	}
	if st.Choices[1].Text != "Ask about the dungeon" {
		t.Fatalf("EN fallback choice 1 = %q, want source fallback", st.Choices[1].Text)
	}
	if st.Choices[2].Text != "Walk away" {
		t.Fatalf("EN choice 2 = %q", st.Choices[2].Text)
	}

	// Switch to JA locale mid-conversation before picking Leave -> Farewell.
	locale = "ja"
	st, err = runner.Choose(2)
	if err != nil || st.Line.Text != "良い旅を！" {
		t.Fatalf("JA farewell = %+v, err=%v", st, err)
	}

	// Fresh runner in JA locale verifies localized interpolation with both {hero} and {$gold} + fallback.
	jaRunner := flow.NewDialogueRunner(script, flow.DialogueOptions{
		StringResolver: resolver,
	})
	jaRunner.SetVar("hero", "ソラ")
	jaRunner.SetVar("gold", 12)
	st, err = jaRunner.Start("Merchant")
	if err != nil || st.Line.Text != "ようこそ、ソラ！所持金は12ゴールドですね。" {
		t.Fatalf("JA welcome = %+v, err=%v", st, err)
	}
	st, err = jaRunner.Continue()
	if err != nil || st.Kind != flow.StepChoices {
		t.Fatalf("JA choices = %+v, err=%v", st, err)
	}
	if st.Choices[0].Text != "ダンジョンの鍵を買う（10ゴールド）" || st.Choices[1].Text != "Ask about the dungeon" {
		t.Fatalf("JA choices = %+v", st.Choices)
	}
}

func TestDialogueStatePersistenceAndBusMutation(t *testing.T) {
	const src = `
title: Gate
---
<<check_pass>>
<<if $pass_granted>>
    Guard: Your pass is valid, {$name}.
<<else>>
    Guard: Access denied.
<<endif>>
===
`
	script, err := flow.ParseYarn(src)
	if err != nil {
		t.Fatalf("ParseYarn failed: %v", err)
	}

	var bus flow.Bus
	var r *flow.DialogueRunner
	flow.Subscribe(&bus, func(ev flow.DialogueEvent) {
		if ev.Command == "check_pass" {
			// Synchronous Bus handler mutates dialogue variables seen immediately by next <<if>>.
			r.SetVar("pass_granted", true)
		}
	})

	r = flow.NewDialogueRunner(script, flow.DialogueOptions{Bus: &bus})
	r.SetVar("name", "Kael")
	r.SetVar("score", 42)

	st, err := r.Start("Gate")
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Your pass is valid, Kael." {
		t.Fatalf("step after synchronous bus mutation = %+v, err=%v", st, err)
	}

	// Vars() returns a defensive copy.
	varsCopy := r.Vars()
	varsCopy["name"] = "Corrupted"
	if got, _ := r.Var("name"); got != "Kael" {
		t.Fatalf("mutating Vars() copy affected runner: got %v", got)
	}

	// JSON round-trip of State() -> LoadState().
	blob, err := json.Marshal(r.State())
	if err != nil {
		t.Fatalf("json.Marshal(State) failed: %v", err)
	}
	var restored flow.DialogueState
	if err := json.Unmarshal(blob, &restored); err != nil {
		t.Fatalf("json.Unmarshal(State) failed: %v", err)
	}

	r2 := flow.NewDialogueRunner(script, flow.DialogueOptions{})
	r2.LoadState(restored)
	if r2.Visited("Gate") != 1 {
		t.Fatalf("restored Visited(Gate) = %d, want 1", r2.Visited("Gate"))
	}
	if v, ok := r2.Var("score"); !ok || v != float64(42) {
		t.Fatalf("restored score = %v, %v; want 42, true", v, ok)
	}
	if v, ok := r2.Var("pass_granted"); !ok || v != true {
		t.Fatalf("restored pass_granted = %v, %v; want true, true", v, ok)
	}
}

func TestDialogueExpressionsAndSkipUnavailableChoiceGroup(t *testing.T) {
	const src = `
title: Math
---
<<set $a = 2 + 3 * 4>>
<<set $b = (2 + 3) * 4>>
<<set $c = -$a + $b / 2>>
<<set $label = "Result=" + $c>>
<<set $ok = ($a == 14) && ($b is 20) && ($c == -4) && !(false || !true)>>
<<if $ok>>
    System: {$label} and count={visited_count("Math")}
<<endif>>
-> Locked option <<if false>>
    System: unreachable
System: Continued past all-unavailable menu.
===
`
	script, err := flow.ParseYarn(src)
	if err != nil {
		t.Fatalf("ParseYarn failed: %v", err)
	}
	r := flow.NewDialogueRunner(script, flow.DialogueOptions{})
	st, err := r.Start("Math")
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Result=-4 and count=1" {
		t.Fatalf("math line 1 = %+v, err=%v", st, err)
	}
	st, err = r.Continue()
	if err != nil || st.Kind != flow.StepLine || st.Line.Text != "Continued past all-unavailable menu." {
		t.Fatalf("line after skipped choice group = %+v, err=%v", st, err)
	}
	st, err = r.Continue()
	if err != nil || st.Kind != flow.StepEnded {
		t.Fatalf("end = %+v, err=%v", st, err)
	}
	// Continuing after ended is an idempotent StepEnded.
	st, err = r.Continue()
	if err != nil || st.Kind != flow.StepEnded {
		t.Fatalf("continue after end = %+v, err=%v", st, err)
	}
}

func TestDialogueErrorsAndGuards(t *testing.T) {
	t.Run("ParseSyntaxErrors", func(t *testing.T) {
		cases := []struct {
			name string
			src  string
			want string
			line int
		}{
			{"EmptyScript", "   \n// only comment\n", "no nodes", 1},
			{"MissingTitle", "tags: a\n---\nHello\n===\n", "missing a 'title'", 2},
			{"DuplicateTitle", "title: A\n---\nHi\n===\ntitle: A\n---\nBye\n===\n", "duplicate node title", 6},
			{"MissingBodySeparator", "title: A\n===\n", "before '---'", 2},
			{"UnterminatedNode", "title: A\n---\nHello\n", "missing '==='", 1},
			{"UnclosedIf", "title: A\n---\n<<if true>>\nHi\n===\n", "unclosed <<if>>", 3},
			{"StrayEndif", "title: A\n---\n<<endif>>\n===\n", "unexpected <<endif>>", 3},
			{"ElseifAfterElse", "title: A\n---\n<<if true>>\nA\n<<else>>\nB\n<<elseif false>>\nC\n<<endif>>\n===\n", "after <<else>>", 7},
			{"UnclosedCommand", "title: A\n---\n<<set $x = 1\n===\n", "unclosed command", 3},
			{"InvalidExpr", "title: A\n---\n<<set $x = 1 + >>\n===\n", "invalid expression", 3},
			{"UnclosedInterpolation", "title: A\n---\nHello {$name\n===\n", "unclosed '{'", 3},
			{"EmptyChoice", "title: A\n---\n-> <<if true>>\n    Hi\n===\n", "empty choice text", 3},
			{"UnterminatedCmdString", "title: A\n---\n<<say \"unterminated>>\n===\n", "unterminated quoted", 3},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := flow.ParseYarn(tc.src)
				if err == nil {
					t.Fatalf("ParseYarn succeeded, want error containing %q", tc.want)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("ParseYarn error = %q, want substring %q", err.Error(), tc.want)
				}
				loaderrtest.Expect(t, loaderr.WithFile(err, "intro.yarn"), "intro.yarn", tc.line)
			})
		}
	})

	t.Run("RuntimeErrors", func(t *testing.T) {
		// Nil script
		nilRunner := flow.NewDialogueRunner(nil, flow.DialogueOptions{})
		if _, err := nilRunner.Start("Start"); !errors.Is(err, flow.ErrNilScript) {
			t.Fatalf("nil script Start err = %v, want ErrNilScript", err)
		}

		const src = `
title: Start
---
-> Go to missing
    <<jump DoesNotExist>>
-> Stay
    Narrator: Done.
===
`
		script, err := flow.ParseYarn(src)
		if err != nil {
			t.Fatalf("ParseYarn failed: %v", err)
		}
		r := flow.NewDialogueRunner(script, flow.DialogueOptions{})

		if _, err := r.Start("UnknownNode"); !errors.Is(err, flow.ErrUnknownNode) {
			t.Fatalf("Start(UnknownNode) err = %v, want ErrUnknownNode", err)
		}
		if _, err := r.Choose(0); !errors.Is(err, flow.ErrNoPendingChoice) {
			t.Fatalf("Choose before Start err = %v, want ErrNoPendingChoice", err)
		}

		st, err := r.Start("Start")
		if err != nil || st.Kind != flow.StepChoices {
			t.Fatalf("Start = %+v, err=%v", st, err)
		}
		if _, err := r.Continue(); !errors.Is(err, flow.ErrChoiceRequired) {
			t.Fatalf("Continue while choices pending err = %v, want ErrChoiceRequired", err)
		}
		if _, err := r.Choose(-1); !errors.Is(err, flow.ErrInvalidChoice) {
			t.Fatalf("Choose(-1) err = %v, want ErrInvalidChoice", err)
		}
		if _, err := r.Choose(2); !errors.Is(err, flow.ErrInvalidChoice) {
			t.Fatalf("Choose(2) err = %v, want ErrInvalidChoice", err)
		}
		if _, err := r.Choose(0); !errors.Is(err, flow.ErrUnknownNode) {
			t.Fatalf("jump to missing node err = %v, want ErrUnknownNode", err)
		}
	})

	t.Run("InfiniteJumpGuard", func(t *testing.T) {
		const loopSrc = `
title: Ping
---
<<jump Pong>>
===

title: Pong
---
<<jump Ping>>
===
`
		script, err := flow.ParseYarn(loopSrc)
		if err != nil {
			t.Fatalf("ParseYarn failed: %v", err)
		}
		r := flow.NewDialogueRunner(script, flow.DialogueOptions{
			MaxStepsPerContinue: 32,
		})
		if _, err := r.Start("Ping"); !errors.Is(err, flow.ErrStepLimitExceeded) {
			t.Fatalf("infinite jump err = %v, want ErrStepLimitExceeded", err)
		}
		if r.CurrentNode() != "" {
			t.Fatalf("CurrentNode after step limit = %q, want empty", r.CurrentNode())
		}
	})
}
