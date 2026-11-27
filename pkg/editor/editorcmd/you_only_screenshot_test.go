package editorcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestYouOnlyAndProposedDeletionsMacOSScreenshots captures real native macOS screenshots
// of the You-only Permissions Draft card, Proposed Deletion review panel, and Settings › Permissions
// with held external loosenings beside docs/design/editor/Permissions.dc.html and Settings.dc.html.
//
// Run: env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestYouOnlyAndProposedDeletionsMacOSScreenshots ./editor/editorcmd
func TestYouOnlyAndProposedDeletionsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), []byte("version: 1\nname: Level 01\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var oldPrefab strings.Builder
	oldPrefab.WriteString("id: slime_old\nsprite: enemies/slime_green_0\ncomponents:\n")
	for i := 4; i <= 41; i++ {
		oldPrefab.WriteString("  # slime_old property\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "prefabs", "slime_old.prefab.yaml"), []byte(oldPrefab.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Dungeon Run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.4.2",
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			Scenes:    []project.Entry{{Name: "level_01", Path: "scenes/level_01.scene.yaml"}},
			MainScene: "level_01",
		},
	}
	data, err := project.Encode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 1. Settings › Permissions with a held pulled loosening (Deny -> Allow held until confirmed).
	pulled := editor.DefaultPermissions()
	pulled.Rules[5].Decision = editor.PermissionAllow // Delete ** : Deny -> Allow (held!)
	pulled.Rules[0].Decision = editor.PermissionAsk   // Edit scenes/** : Allow -> Ask (tightened, applied!)
	if err := editor.SavePermissions(editor.PermissionsFilePath(dir), &pulled); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		ed.PollDiskChanges()
		ed.DismissToast()
		ed.OpenSettingsPage(editor.SettingsPagePermissions)
	})
	app.Settle()
	app.Screenshot("you-only-settings-held-loosenings")

	// 2. Permissions Draft card over Settings › Permissions (Dark, 1 rule ticked by person to show YAML diff).
	app.Post(func() {
		_, _ = ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
			Request: "stop asking me before every scene edit",
			Rules: []editor.PermissionsDraftRuleSpec{
				{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**", "prefabs/**", "data/**"}, Decision: editor.PermissionAllow},
				{Action: editor.PermissionActionRun, Patterns: []string{"game check"}, Decision: editor.PermissionAllow},
				{Action: editor.PermissionActionDelete, Patterns: []string{"prefabs/**"}, Decision: editor.PermissionAsk},
			},
		})
		ed.SetPermissionsDraftRuleTicked(0, true)
	})
	app.Settle()
	app.Screenshot("you-only-permissions-draft")

	// 3. Permissions Draft card in Light theme (SpecPalette Light check).
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("you-only-permissions-draft-light")

	// 4. Review changes panel with Proposed Deletion (Dark, matching Permissions.dc.html Proposed deletion).
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceDark)
		ed.DiscardPermissionsDraft()
		ed.DismissToast()
		ed.CloseSettings()
		ed.OpenAgentReview("clean up the old slime", []editor.AgentReviewChange{
			{ID: "c1", Path: "scenes/level_01.scene.yaml", Title: "level_01.scene.yaml", Detail: "slime_02 uses prefabs/slime.prefab.yaml"},
			{ID: "c2", Path: "scenes/level_02.scene.yaml", Title: "level_02.scene.yaml", Detail: "slime_05, slime_06 use the new prefab"},
			{ID: "c3", Path: "prefabs/slime.prefab.yaml", Title: "slime.prefab.yaml", Detail: "patrol speed 24, taken from slime_old"},
		})
		_, _ = ed.ProposeFileDeletion("prefabs/slime_old.prefab.yaml", "clean up the old slime")
	})
	app.Settle()
	app.Screenshot("you-only-proposed-deletion")
}
