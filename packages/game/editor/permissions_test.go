package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/loaderr"
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
