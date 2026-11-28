package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

const secretMarker = "MARKER-SECRET"

// secretFixtureFiles are project files that commonly hold credentials or the editor's private state, each holding secretMarker, keyed by their path from the project root.
var secretFixtureFiles = []string{
	".env",
	".env.production",
	".envrc",
	"config/.env.local",
	"signing/upload.key.pem",
	"signing/server.key",
	"signing/AuthKey_ABC123.p8",
	"signing/dist.p12",
	"signing/dist.pfx",
	"android/release.jks",
	"android/release.keystore",
	"android/keystore.properties",
	".netrc",
	".npmrc",
	"keys/id_ed25519",
	"keys/id_rsa.pub",
	".git/config",
	"vendor/lib/.git/config",
	".nsgo/threads/t1.yaml",
	// Case-changed names are refused too, as they open the same file on a case-insensitive file system.
	".ENV.Staging",
	"Signing/Release.PEM",
	".GIT/HEAD",
}

func writeProjectFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSecretRefused(t *testing.T, what, got string, isError bool) {
	t.Helper()
	if !isError || strings.Contains(got, secretMarker) || !strings.Contains(got, "never sends it to an agent") {
		t.Fatalf("%s: isError=%v text=%q, want a credential refusal without the file's content", what, isError, got)
	}
}

func TestSecretFilesNeverLeaveUnderDefaultPrivacy(t *testing.T) {
	f := openFixture(t)
	for i, rel := range secretFixtureFiles {
		writeProjectFile(t, f.dir, rel, "TOKEN="+secretMarker+"-"+string(rune('A'+i))+"\n")
	}
	// In-project symlinks with innocent names pointing at a credential and into .git.
	if err := os.Symlink(filepath.Join("..", ".env"), filepath.Join(f.dir, "scenes", "sneaky_env.scene.yaml")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", ".git", "config"), filepath.Join(f.dir, "scenes", "sneaky_git.data.yaml")); err != nil {
		t.Fatal(err)
	}
	// Ordinary project text, including names close to the secret patterns, still reads.
	ordinary := map[string]string{
		"AGENTS.md":                 "# Rules\nKeep it small.\n",
		"data/id_table.data.yaml":   "version: 1\nrows: []\n",
		"data/environment.yaml":     "weather: rain\n",
		"docs/keys.md":              "Press space to jump.\n",
		"data/keyboard.data.yaml":   "version: 1\nrows: []\n",
		"config/gitignore-notes.md": "notes\n",
	}
	for rel, content := range ordinary {
		writeProjectFile(t, f.dir, rel, content)
	}

	// Autonomous permissions let edits and renames through the permission rules, so the privacy refusal is what stops them.
	if err := f.ed.SetAgentPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	paths := append(append([]string{}, secretFixtureFiles...), "scenes/sneaky_env.scene.yaml", "scenes/sneaky_git.data.yaml", "./.env", "scenes/../.env", `signing\upload.key.pem`)
	for _, rel := range paths {
		res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": rel})
		assertSecretRefused(t, "scene_read "+rel, text(res), res.IsError)
		res = call(t, cs, editormcp.ToolRefsFind, map[string]any{"file": rel})
		assertSecretRefused(t, "refs_find "+rel, text(res), res.IsError)
		res = call(t, cs, editormcp.ToolTilesetRead, map[string]any{"file": rel})
		assertSecretRefused(t, "tileset_read "+rel, text(res), res.IsError)
		res = call(t, cs, editormcp.ToolStatesTest, map[string]any{"file": rel})
		assertSecretRefused(t, "states_test "+rel, text(res), res.IsError)
		res = call(t, cs, editormcp.ToolFileEdit, map[string]any{"file": rel, "yaml": "a: 1\n"})
		assertSecretRefused(t, "file_edit "+rel, text(res), res.IsError)
	}
	res := call(t, cs, editormcp.ToolRefactorRename, map[string]any{"from": "scenes/room.scene.yaml", "to": ".git/hooks/room.scene.yaml"})
	if !res.IsError || !strings.Contains(text(res), "never sends it to an agent") {
		t.Fatalf("refactor_rename into .git: isError=%v text=%q", res.IsError, text(res))
	}

	// Turning every switch on still never sends a credential.
	for _, kind := range []editor.PrivacyContentKind{editor.PrivacySceneAndData, editor.PrivacyGoSource, editor.PrivacyArtAndAudio, editor.PrivacyViewportImages} {
		if err := f.ed.SetAgentPrivacy(kind, true); err != nil {
			t.Fatal(err)
		}
	}
	res = call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": ".env"})
	assertSecretRefused(t, "scene_read .env with every switch on", text(res), res.IsError)

	for rel := range ordinary {
		res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": rel})
		if res.IsError {
			t.Fatalf("scene_read %s: %q, want the file", rel, text(res))
		}
	}
	if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/room.scene.yaml"}); res.IsError {
		t.Fatalf("scene_read scenes/room.scene.yaml: %q", text(res))
	}

	// The @-mention picker never offers a credential as a chip.
	f.ed.OpenMentionPicker("")
	for _, it := range f.ed.MentionPickerItems() {
		for _, s := range []string{it.Target, it.Name, it.SceneFile} {
			if s != "" && editor.ClassifyProjectFilePrivacy(s) == editor.PrivacySecrets {
				t.Fatalf("mention picker offers %+v", it)
			}
		}
	}
	f.ed.CloseMentionPicker()
}

func TestClassifyProjectFilePrivacySecrets(t *testing.T) {
	for _, rel := range append(append([]string{}, secretFixtureFiles...), ".env::$DATA", ".git./config", `.NSGO\editor.yaml`, "/.env") {
		if got := editor.ClassifyProjectFilePrivacy(rel); got != editor.PrivacySecrets {
			t.Errorf("ClassifyProjectFilePrivacy(%q) = %q, want %q", rel, got, editor.PrivacySecrets)
		}
	}
	for rel, want := range map[string]editor.PrivacyContentKind{
		"AGENTS.md":                editor.PrivacySceneAndData,
		"scenes/room.scene.yaml":   editor.PrivacySceneAndData,
		"data/id_table.data.yaml":  editor.PrivacySceneAndData,
		".gitignore":               editor.PrivacySceneAndData,
		".github/workflows/ci.yml": editor.PrivacySceneAndData,
		"main.go":                  editor.PrivacyGoSource,
		"art/hero.png":             editor.PrivacyArtAndAudio,
	} {
		if got := editor.ClassifyProjectFilePrivacy(rel); got != want {
			t.Errorf("ClassifyProjectFilePrivacy(%q) = %q, want %q", rel, got, want)
		}
	}
	if editor.DefaultAgentPrivacyPolicy().Allows(editor.PrivacySecrets) || (editor.AgentPrivacyPolicy{SceneAndData: true, ViewportImages: true, GoSource: true, ArtAndAudio: true}).Allows(editor.PrivacySecrets) {
		t.Fatal("a privacy policy allows secrets")
	}
}
