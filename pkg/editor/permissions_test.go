package editor_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestPermissionsYAMLStrictAndRoundTrip(t *testing.T) {
	for _, profile := range []editor.PermissionProfile{
		editor.PermissionProfileCautious,
		editor.PermissionProfileBalanced,
		editor.PermissionProfileAutonomous,
	} {
		preset := editor.PermissionPreset(profile)
		first, err := editor.EncodePermissions(&preset)
		if err != nil {
			t.Fatalf("EncodePermissions(%s): %v", profile, err)
		}
		parsed, err := editor.ParsePermissions(first)
		if err != nil {
			t.Fatalf("ParsePermissions(%s): %v", profile, err)
		}
		second, err := editor.EncodePermissions(parsed)
		if err != nil {
			t.Fatalf("re-encode(%s): %v", profile, err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("%s round-trip changed bytes:\n--- first ---\n%s\n--- second ---\n%s", profile, first, second)
		}
		if !strings.HasPrefix(string(first), editor.PermissionsSchemaComment+"\n") {
			t.Fatalf("%s missing schema comment header:\n%s", profile, first)
		}
	}

	// Unknown field must be refused with line number.
	unknownYAML := []byte("version: 1\nprofile: balanced\nextraFlag: true\nrules:\n  - action: edit\n    patterns:\n      - scenes/**\n    decision: allow\n")
	_, err := editor.ParsePermissions(unknownYAML)
	if err == nil {
		t.Fatal("ParsePermissions accepted unknown field")
	}
	var le *loaderr.Error
	if !errors.As(err, &le) {
		t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
	}
	if le.Line != 3 {
		t.Fatalf("unknown field line = %d, want 3 (err: %v)", le.Line, err)
	}
	if !strings.Contains(err.Error(), "extraFlag") {
		t.Fatalf("unknown field error %q does not name extraFlag", err.Error())
	}

	// Refusal cases: missing version, zero version, future version, invalid profile/action/decision, empty patterns, anchor/alias, duplicate keys, trailing doc.
	badCases := map[string]string{
		"empty":          "",
		"missingVersion": "profile: balanced\nrules: []\n",
		"zeroVersion":    "version: 0\nprofile: balanced\nrules: []\n",
		"futureVersion":  "version: 99\nprofile: balanced\nrules: []\n",
		"badProfile":     "version: 1\nprofile: reckless\nrules: []\n",
		"missingRules":   "version: 1\nprofile: balanced\n",
		"badAction":      "version: 1\nprofile: balanced\nrules:\n  - action: explode\n    patterns: ['**']\n    decision: deny\n",
		"badDecision":    "version: 1\nprofile: balanced\nrules:\n  - action: edit\n    patterns: ['**']\n    decision: maybe\n",
		"emptyPatterns":  "version: 1\nprofile: balanced\nrules:\n  - action: edit\n    patterns: []\n    decision: ask\n",
		"escapePattern":  "version: 1\nprofile: balanced\nrules:\n  - action: edit\n    patterns: ['../secret.yaml']\n    decision: allow\n",
		"duplicateKey":   "version: 1\nprofile: balanced\nprofile: cautious\nrules: []\n",
		"yamlAnchor":     "version: 1\nprofile: &p balanced\nrules: []\n",
		"trailingDoc":    "version: 1\nprofile: balanced\nrules: []\n---\nversion: 1\n",
	}
	for name, raw := range badCases {
		if _, err := editor.ParsePermissions([]byte(raw)); err == nil {
			t.Errorf("ParsePermissions(%s) succeeded, want error", name)
		}
	}
}

func TestPermissionsRuleOrderingAndGlobs(t *testing.T) {
	// Rule ordering: Allow on scenes/** before Ask on ** lets scene edits through, while other edits ask.
	custom := editor.PermissionsFile{
		Version: editor.PermissionsVersion,
		Profile: editor.PermissionProfileBalanced,
		Rules: []editor.PermissionRule{
			{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**"}, Decision: editor.PermissionAllow},
			{Action: editor.PermissionActionEdit, Patterns: []string{"**"}, Decision: editor.PermissionAsk},
		},
	}

	evScene := custom.Evaluate(editor.PermissionRequest{
		Tool:   "scene_edit",
		Action: editor.PermissionActionEdit,
		Target: "scenes/floor1/level_01.scene.yaml",
	})
	if evScene.Decision != editor.PermissionAllow || evScene.RuleIndex != 0 || evScene.RuleNumber != 1 {
		t.Fatalf("scene edit eval = %+v, want allow by rule 1", evScene)
	}

	evData := custom.Evaluate(editor.PermissionRequest{
		Tool:   "data_edit",
		Action: editor.PermissionActionEdit,
		Target: "data/enemies.yaml",
	})
	if evData.Decision != editor.PermissionAsk || evData.RuleIndex != 1 || evData.RuleNumber != 2 {
		t.Fatalf("data edit eval = %+v, want ask by rule 2", evData)
	}

	// Reversing order makes Ask on ** win first.
	reversed := editor.PermissionsFile{
		Version: editor.PermissionsVersion,
		Profile: editor.PermissionProfileBalanced,
		Rules: []editor.PermissionRule{
			{Action: editor.PermissionActionEdit, Patterns: []string{"**"}, Decision: editor.PermissionAsk},
			{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**"}, Decision: editor.PermissionAllow},
		},
	}
	evSceneRev := reversed.Evaluate(editor.PermissionRequest{
		Tool:   "scene_edit",
		Action: editor.PermissionActionEdit,
		Target: "scenes/level_01.scene.yaml",
	})
	if evSceneRev.Decision != editor.PermissionAsk || evSceneRev.RuleIndex != 0 {
		t.Fatalf("reversed scene edit eval = %+v, want ask by rule 1", evSceneRev)
	}

	// **/*.go matches both root main.go and nested scripts/slime.go.
	balanced := editor.PermissionPreset(editor.PermissionProfileBalanced)
	for _, goPath := range []string{"main.go", "scripts/slime.go", "pkg/a/b/c.go"} {
		ev := balanced.Evaluate(editor.PermissionRequest{Action: editor.PermissionActionCode, Target: goPath})
		if ev.Decision != editor.PermissionAsk || ev.RuleNumber != 4 {
			t.Fatalf("code edit %s = %+v, want ask by rule 4", goPath, ev)
		}
	}

	// Balanced denies shell commands by rule 7 of 7 and links to "Settings › Permissions · rule 7 of 7".
	evShell := balanced.Evaluate(editor.PermissionRequest{
		Tool:   "shell_run",
		Action: editor.PermissionActionRun,
		Target: "shell: go generate ./assets",
	})
	if evShell.Decision != editor.PermissionDeny || evShell.RuleIndex != 6 || evShell.RuleNumber != 7 || evShell.RuleTotal != 7 {
		t.Fatalf("shell eval = %+v, want deny by rule 7 of 7", evShell)
	}
	if evShell.Link != "Settings › Permissions · rule 7 of 7" {
		t.Fatalf("shell eval link = %q", evShell.Link)
	}
}

func TestPermissionsSettingsUIProfilesArrowsBlockedLinkAndAlwaysToast(t *testing.T) {
	projDir := t.TempDir()
	gameYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(projDir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(projDir, "game.yaml")); err != nil {
		t.Fatal(err)
	}

	var scope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	postUI := func(fn func()) {
		scope.Post(fn)
		app.Settle()
	}

	// 1. Blocked step link in workspace opens Settings › Permissions with rule 7 of 7 focused.
	postUI(func() {
		shellEval := ed.EvaluatePermission(editor.PermissionRequest{
			Tool:   "shell_run",
			Action: editor.PermissionActionRun,
			Target: "shell: go generate ./assets",
		})
		ed.RecordBlockedPermission(shellEval, "run go generate ./assets")
	})

	app.Press("id:workspace-blocked-step-link")
	if !ed.PermissionsSettingsOpen() {
		t.Fatal("clicking blocked step link did not open Settings › Permissions")
	}
	if got := ed.PermissionsFocusedRule(); got != 6 {
		t.Fatalf("focused rule after blocked step link = %d, want 6 (rule 7 of 7)", got)
	}

	// 2. Selecting Cautious profile fills table and persists permissions.yaml beside game.yaml.
	app.Press("id:perm-profile-cautious")
	perms := ed.Permissions()
	if perms.Profile != editor.PermissionProfileCautious {
		t.Fatalf("profile = %q, want cautious", perms.Profile)
	}
	if perms.Rules[0].Decision != editor.PermissionAsk || perms.Rules[1].Decision != editor.PermissionAsk {
		t.Fatalf("cautious rules 1-2 = %+v, want ask", perms.Rules[:2])
	}
	if perms.IsModified() {
		t.Fatal("freshly selected Cautious profile should not be marked modified")
	}
	if _, err := os.Stat(filepath.Join(projDir, editor.PermissionsFileName)); err != nil {
		t.Fatalf("permissions.yaml not written beside game.yaml: %v", err)
	}

	// 3. Editing rule 1 via radio group arrow keys keeps Cautious profile name with Changed mark.
	// Rule 0 is currently Ask; pressing Right arrow moves to Deny, Left arrow moves back to Ask, Left again moves to Allow!
	app.Focus("id:perm-rule-0-ask")
	app.Key("right")
	if got := ed.Permissions().Rules[0].Decision; got != editor.PermissionDeny {
		t.Fatalf("after right arrow on rule 0 Ask: decision = %q, want deny", got)
	}
	if ed.Permissions().Profile != editor.PermissionProfileCautious || !ed.Permissions().IsModified() {
		t.Fatalf("expected Cautious profile with modified flag after edit: %+v", ed.Permissions())
	}
	app.Focus("id:perm-rule-0-deny")
	app.Key("left")
	app.Focus("id:perm-rule-0-ask")
	app.Key("left")
	if got := ed.Permissions().Rules[0].Decision; got != editor.PermissionAllow {
		t.Fatalf("after two left arrows on rule 0: decision = %q, want allow", got)
	}

	// 4. Reorder (move down/up), add, and remove rules.
	beforeCount := len(ed.Permissions().Rules)
	app.Press("id:perm-add-rule")
	if got := len(ed.Permissions().Rules); got != beforeCount+1 {
		t.Fatalf("after add rule: count = %d, want %d", got, beforeCount+1)
	}
	lastIdx := len(ed.Permissions().Rules) - 1
	app.Press("id:perm-rule-7-up")
	if ed.PermissionsFocusedRule() != lastIdx-1 {
		t.Fatalf("after move up: focused = %d, want %d", ed.PermissionsFocusedRule(), lastIdx-1)
	}
	app.Press("id:perm-rule-6-remove")
	if got := len(ed.Permissions().Rules); got != beforeCount {
		t.Fatalf("after remove rule: count = %d, want %d", got, beforeCount)
	}

	// 5. "Always" approval on a pending Ask card saves rule, shows toast with Show and Undo, and Undo restores exact YAML.
	app.Press("id:perm-profile-cautious")
	beforeYAML, err := os.ReadFile(filepath.Join(projDir, editor.PermissionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	postUI(func() {
		playEval := ed.EvaluatePermission(editor.PermissionRequest{
			Tool:   "game_playtest",
			Action: editor.PermissionActionRun,
			Target: editor.PermissionRunPlaytests,
		})
		ed.EnqueuePermissionRequest(playEval, "run playtests", nil)
	})

	app.Press("id:perm-rail-ask-always")
	toast, ok := ed.PermissionToast()
	if !ok {
		t.Fatal("Always approval did not show toast")
	}
	if toast.Message != "Always permission rule saved" {
		t.Fatalf("toast message = %q, want %q", toast.Message, "Always permission rule saved")
	}
	// Now playtests must evaluate to Allow (first-match-wins ahead of rule 2's Ask).
	afterEval := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "game_playtest",
		Action: editor.PermissionActionRun,
		Target: editor.PermissionRunPlaytests,
	})
	if afterEval.Decision != editor.PermissionAllow {
		t.Fatalf("playtest eval after Always = %+v, want allow", afterEval)
	}

	// Pressing Show opens Settings › Permissions focused on the newly inserted rule.
	postUI(func() {
		ed.ClosePermissionsSettings()
	})
	app.Press("id:perm-toast-show")
	if !ed.PermissionsSettingsOpen() || ed.PermissionsFocusedRule() != toast.RuleIndex {
		t.Fatalf("Show toast: open=%v focused=%d want true, %d", ed.PermissionsSettingsOpen(), ed.PermissionsFocusedRule(), toast.RuleIndex)
	}

	// Pressing Undo removes the saved rule and restores permissions.yaml byte-for-byte.
	app.Press("id:perm-toast-undo")
	if _, ok := ed.PermissionToast(); ok {
		t.Fatal("toast still visible after Undo")
	}
	restoredYAML, err := os.ReadFile(filepath.Join(projDir, editor.PermissionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeYAML, restoredYAML) {
		t.Fatalf("Undo did not restore permissions.yaml byte-for-byte:\n--- before ---\n%s\n--- restored ---\n%s", beforeYAML, restoredYAML)
	}
}

func TestPermissionsSecurityHardeningAndLifecycle(t *testing.T) {
	// 1. Unrecognized action fails closed (PermissionDeny) even under Autonomous.
	auto := editor.PermissionPreset(editor.PermissionProfileAutonomous)
	if ev := auto.Evaluate(editor.PermissionRequest{Action: "unknown", Target: "scenes/a.scene.yaml"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("unknown action decision = %q, want deny", ev.Decision)
	}

	// 2. Escaping, absolute, drive-prefixed, and control-char paths NEVER match "**" or "*" and fail closed.
	for _, badTarget := range []string{"../secret.yaml", "scenes/../../etc/passwd", "/game.yaml", "C:/game.yaml", "scenes/a\x00.yaml"} {
		if editor.MatchPermissionPattern(editor.PermissionActionEdit, "**", badTarget) {
			t.Fatalf("MatchPermissionPattern(**) matched escaping target %q", badTarget)
		}
		if editor.MatchPermissionPattern(editor.PermissionActionEdit, "*", badTarget) {
			t.Fatalf("MatchPermissionPattern(*) matched escaping target %q", badTarget)
		}
		if ev := auto.Evaluate(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: badTarget}); ev.Decision != editor.PermissionDeny {
			t.Fatalf("Evaluate(%q) = %q, want deny", badTarget, ev.Decision)
		}
	}
	if editor.MatchPermissionPattern(editor.PermissionActionEdit, "**", "") {
		t.Fatal("MatchPermissionPattern(**) matched empty file target")
	}

	// 3. Symlink resolution maps internal symlinks to their real target and rejects outside escapes.
	projDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "game.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.yaml")
	if err := os.WriteFile(outsideFile, []byte("secret: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "game.yaml"), filepath.Join(projDir, "scenes", "link_game.scene.yaml")); err == nil {
		got, err := editor.ResolvePermissionFileTarget(projDir, "scenes/link_game.scene.yaml")
		if err != nil || got != "game.yaml" {
			t.Fatalf("ResolvePermissionFileTarget internal symlink = (%q, %v), want (\"game.yaml\", nil)", got, err)
		}
	}
	if err := os.Symlink(outsideFile, filepath.Join(projDir, "scenes", "escape.scene.yaml")); err == nil {
		if got, err := editor.ResolvePermissionFileTarget(projDir, "scenes/escape.scene.yaml"); err == nil {
			t.Fatalf("ResolvePermissionFileTarget outside symlink succeeded with %q, want error", got)
		}
	}

	// 4. Case-insensitive matching on macOS/Windows prevents Game.yaml from bypassing game.yaml rules.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		bal := editor.PermissionPreset(editor.PermissionProfileBalanced)
		if ev := bal.Evaluate(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "Game.yaml"}); ev.Decision != editor.PermissionAsk || ev.RuleNumber != 3 {
			t.Fatalf("case-folded Game.yaml eval = %+v, want ask by rule 3", ev)
		}
	}

	// 5. Adversarial multi-** glob must complete in polynomial time (DP O(|p|*|t|)), not exponential backtracking.
	start := time.Now()
	advPat := "a/**/b/**/c/**/d/**/e/**/f/**/g/**/h/**/i/**/j"
	advTarget := strings.Repeat("a/", 30) + "z"
	if editor.MatchPermissionPattern(editor.PermissionActionEdit, advPat, advTarget) {
		t.Fatal("adversarial glob unexpectedly matched")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("adversarial glob took %v, expected fast DP execution", elapsed)
	}

	// 6. Corrupt permissions.yaml fails closed (PermissionsErr != nil, profile Cautious, EvaluatePermission == Deny).
	corruptProj := t.TempDir()
	if err := os.WriteFile(filepath.Join(corruptProj, "game.yaml"), []byte("version: 1\nname: corrupt\nid: dev.test.corrupt\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 240\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptProj, editor.PermissionsFileName), []byte("version: 1\nprofile: cautious\nunknownField: 123\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(corruptProj, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if ed.PermissionsErr() == nil {
		t.Fatal("expected PermissionsErr for corrupt permissions.yaml")
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "scene_edit",
		Action: editor.PermissionActionEdit,
		Target: "scenes/level_01.scene.yaml",
	}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("corrupt permissions.yaml must fail closed with deny, got %+v", ev)
	}
	if _, err := ed.ApproveAlwaysPermission(editor.PermissionEvaluation{
		Action: editor.PermissionActionEdit,
		Target: "scenes/level_01.scene.yaml",
	}); err == nil {
		t.Fatal("expected ApproveAlwaysPermission to fail while permissions.yaml is invalid")
	}

	// 7. ApproveAlwaysPermission refuses empty target, escapes glob metacharacters, and refuses to bypass Deny with stale RuleIndex.
	cleanEd, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanEd.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanEd.ApproveAlwaysPermission(editor.PermissionEvaluation{
		Action:    editor.PermissionActionEdit,
		Target:    "",
		RuleIndex: 0,
	}); err == nil {
		t.Fatal("expected ApproveAlwaysPermission with empty target to fail instead of inserting Allow **")
	}
	// Escaping glob metacharacters in literal filename: scenes/level*.scene.yaml must NOT allow scenes/level_other.scene.yaml!
	idx, err := cleanEd.ApproveAlwaysPermission(editor.PermissionEvaluation{
		Action:    editor.PermissionActionEdit,
		Target:    "scenes/level*.scene.yaml",
		RuleIndex: 0,
	})
	if err != nil || idx < 0 {
		t.Fatalf("ApproveAlwaysPermission literal glob char: idx=%d err=%v", idx, err)
	}
	if ev := cleanEd.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "scenes/level*.scene.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("escaped literal target should allow itself, got %+v", ev)
	}
	if ev := cleanEd.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "scenes/level_other.scene.yaml"}); ev.Decision != editor.PermissionAsk {
		t.Fatalf("escaped literal pattern leaked wildcard to scenes/level_other.scene.yaml: %+v", ev)
	}
	// Stale RuleIndex: user changed rule to Deny after Ask was issued -> ApproveAlwaysPermission must refuse!
	askEval := cleanEd.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"})
	if askEval.Decision != editor.PermissionAsk {
		t.Fatalf("expected Ask for game.yaml, got %+v", askEval)
	}
	if err := cleanEd.SetPermissionRuleDecision(askEval.RuleIndex, editor.PermissionDeny); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanEd.ApproveAlwaysPermission(askEval); err == nil {
		t.Fatal("expected ApproveAlwaysPermission to refuse bypassing a rule that is now Deny")
	}

	// 8. OpenDemo() resets permissions path/state so Demo edits never overwrite the previous project's permissions.yaml.
	projA := t.TempDir()
	if err := os.WriteFile(filepath.Join(projA, "game.yaml"), []byte("version: 1\nname: projA\nid: dev.test.proja\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 240\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(projA, "game.yaml")); err != nil {
		t.Fatalf("OpenProject projA: %v", err)
	}
	if err := ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	projABytesBefore, err := os.ReadFile(filepath.Join(projA, editor.PermissionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.PermissionsPath() != "" {
		t.Fatalf("after OpenDemo, PermissionsPath = %q, want empty", ed.PermissionsPath())
	}
	if err := ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	projABytesAfter, err := os.ReadFile(filepath.Join(projA, editor.PermissionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(projABytesBefore, projABytesAfter) {
		t.Fatal("editing permissions in Demo overwrote previous project's permissions.yaml")
	}

	// 9. Atomic SetPermissions: disk write failure must NOT mutate in-memory rules.
	unwritableDir := filepath.Join(t.TempDir(), editor.PermissionsFileName)
	if err := os.MkdirAll(unwritableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	atomicEd, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = atomicEd.LoadProjectPermissions(unwritableDir) // fails to read directory as file -> loadErr set, Cautious loaded
	beforeProfile := atomicEd.Permissions().Profile
	if err := atomicEd.SelectPermissionProfile(editor.PermissionProfileAutonomous); err == nil {
		t.Fatal("expected SetPermissions to fail when path is a directory")
	}
	if got := atomicEd.Permissions().Profile; got != beforeProfile {
		t.Fatalf("failed SetPermissions mutated in-memory profile from %q to %q", beforeProfile, got)
	}

	// 10. Pending queue is bounded and ParsePermissions enforces size/rule caps.
	for i := range 100 {
		cleanEd.EnqueuePermissionRequest(askEval, fmt.Sprintf("ask %d", i), nil)
	}
	if got := len(cleanEd.PendingPermissionRequests()); got > 64 {
		t.Fatalf("pending permission requests grew to %d, want <= 64", got)
	}
	if _, err := editor.ParsePermissions(bytes.Repeat([]byte(" "), (1<<20)+16)); err == nil {
		t.Fatal("expected ParsePermissions to reject >1MiB document")
	}

	// 11. Unified Settings integration + Spec Light theme renders cleanly.
	cleanEd.OpenPermissionsSettings(0)
	if !cleanEd.SettingsOpen() || cleanEd.SettingsPage() != editor.SettingsPagePermissions || !cleanEd.PermissionsSettingsOpen() {
		t.Fatalf("expected unified Settings on SettingsPagePermissions, open=%v page=%q", cleanEd.SettingsOpen(), cleanEd.SettingsPage())
	}
	if err := cleanEd.SetAppearance(editor.AppearanceLight); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, cleanEd)
	}, nsgotest.Size(1280, 800))
	app.Settle()
}
