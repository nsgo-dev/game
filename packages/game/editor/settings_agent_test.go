package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestEditorSettingsAgentYAMLRoundTripAndValidation(t *testing.T) {
	on := true
	off := false
	limit := 250
	in := &editor.EditorSettings{
		Version:    editor.SettingsVersion,
		Language:   editor.LanguageEnglish,
		Appearance: editor.AppearanceDark,
		AutoSave:   &on,
		Agent: &editor.AgentSettings{
			BuiltInProvider: editor.DefaultBuiltInProviderName,
			Routes: map[string]string{
				"ask_in_place":      "Anthropic API · fast",
				"playtest_analysis": "Anthropic API · fast",
				"scene_edits":       "Claude Code · default",
			},
			Tools: map[string]bool{
				"game_delete_file": false,
				"viewport_image":   true,
			},
			MCPServers: []editor.AgentMCPServer{
				{
					Name:        "sprite-gen",
					Title:       "Sprite generator",
					Description: "generates 16 px pixel sprites",
					Command:     "npx",
					Args:        []string{"-y", "@example/sprite-mcp"},
					Enabled:     &on,
				},
				{
					Name:        editor.DefaultRunningGameMCPServerName,
					Title:       "Running game · nsgo dev",
					Description: "inspect and drive the live game",
					Command:     "go",
					Args:        []string{"tool", "nsgo", "mcp"},
					Enabled:     &on,
				},
			},
			WeeklyRequestLimit: &limit,
			Privacy: &editor.AgentPrivacySettings{
				SceneAndData:   &on,
				ViewportImages: &on,
				GoSource:       &off,
				ArtAndAudio:    &off,
			},
			Usage: &editor.AgentUsageStats{
				WeekStart:       "2026-11-23",
				WeeklyRequests:  142,
				ReviewedChanges: 50,
				KeptChanges:     43,
				PlaytestRuns:    380,
			},
		},
	}

	encoded1, err := editor.EncodeEditorSettings(in)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	parsed, err := editor.ParseEditorSettings(encoded1)
	if err != nil {
		t.Fatalf("ParseEditorSettings: %v\n%s", err, encoded1)
	}
	encoded2, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("second EncodeEditorSettings: %v", err)
	}
	if !bytes.Equal(encoded1, encoded2) {
		t.Fatalf("round trip changed bytes:\n--- 1 ---\n%s\n--- 2 ---\n%s", encoded1, encoded2)
	}
	// Servers must be canonically sorted by Name.
	if len(parsed.Agent.MCPServers) != 2 || parsed.Agent.MCPServers[0].Name != editor.DefaultRunningGameMCPServerName || parsed.Agent.MCPServers[1].Name != "sprite-gen" {
		t.Fatalf("MCPServers order = %+v", parsed.Agent.MCPServers)
	}

	// Unknown/secret fields and invalid values must be refused.
	badDocs := []struct {
		name string
		yaml string
	}{
		{"secretField", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  apiKey: sk-nope\n"},
		{"negativeLimit", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  weeklyRequestLimit: -5\n"},
		{"reservedServer", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  mcpServers:\n    - name: nsgo-game-editor\n      command: go\n"},
		{"duplicateServer", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  mcpServers:\n    - name: dup\n      command: go\n    - name: dup\n      command: go\n"},
		{"badToolName", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  tools:\n    \"bad tool name\": false\n"},
		{"keptExceedsReviewed", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  usage:\n    reviewedChanges: 3\n    keptChanges: 5\n"},
		{"badWeekStart", "version: 1\nlanguage: en\nappearance: dark\nagent:\n  usage:\n    weekStart: not-a-date\n"},
	}
	for _, tc := range badDocs {
		if _, err := editor.ParseEditorSettings([]byte(tc.yaml)); err == nil {
			t.Errorf("%s: expected ParseEditorSettings error, got nil", tc.name)
		}
	}
}

func TestAgentAPIKeyStoredOnlyInKeychainNeverInFiles(t *testing.T) {
	projDir := writeProjectSettingsFixture(t)
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "editor.yaml")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	kc := editor.NewMemoryKeychain()
	ed.Keychain = kc
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatal(err)
	}

	const secret1 = "sk-ant-super-secret-key-99999"
	const secret2 = "sk-ant-connect-dialog-secret-88888"

	// 1. Store via StoreAgentAPIKey.
	if err := ed.StoreAgentAPIKey(editor.DefaultAgentAPIKeyAccount, secret1); err != nil {
		t.Fatalf("StoreAgentAPIKey: %v", err)
	}
	if !ed.HasAgentAPIKey(editor.DefaultAgentAPIKeyAccount) {
		t.Fatal("HasAgentAPIKey = false after StoreAgentAPIKey")
	}
	if got, err := ed.AgentAPIKey(editor.DefaultAgentAPIKeyAccount); err != nil || got != secret1 {
		t.Fatalf("AgentAPIKey = (%q, %v), want %q", got, err, secret1)
	}

	// 2. Store via Connect dialog flow.
	ed.OpenConnectDialog()
	ed.SetConnectChoice(editor.AgentModeAPIKey)
	ed.SetConnectAPIKey(secret2)
	ed.CompleteConnectDialog()
	if got := ed.ConnectDialogAPIKey(); got != "" {
		t.Fatalf("ConnectDialogAPIKey not cleared after CompleteConnectDialog: %q", got)
	}
	if got, err := kc.Get(editor.AgentKeychainServiceName, editor.DefaultAgentAPIKeyAccount); err != nil || got != secret2 {
		t.Fatalf("keychain Get = (%q, %v), want %q", got, err, secret2)
	}

	// Save both settings and project files.
	if err := ed.SaveSettings(); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	// Walk every file written under settingsDir and projDir; NO API key may appear anywhere.
	for _, root := range []string{settingsDir, projDir} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			for _, secret := range []string{secret1, secret2} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Errorf("API key %q leaked into file %s", secret, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Delete key from keychain cleanly.
	if err := ed.DeleteAgentAPIKey(editor.DefaultAgentAPIKeyAccount); err != nil {
		t.Fatalf("DeleteAgentAPIKey: %v", err)
	}
	if ed.HasAgentAPIKey(editor.DefaultAgentAPIKeyAccount) {
		t.Fatal("HasAgentAPIKey = true after DeleteAgentAPIKey")
	}
}

func TestAgentSettingsYouOnlyRefusesAgentActor(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.Keychain = editor.NewMemoryKeychain()

	err = ed.WithAgentActor(func() error {
		checks := []struct {
			name string
			err  error
		}{
			{"SetMCPToolEnabled", ed.SetMCPToolEnabled(editor.ToolNameGameDeleteFile, true)},
			{"AddAgentMCPServer", ed.AddAgentMCPServer(editor.AgentMCPServer{Name: "evil", Command: "sh"})},
			{"SetAgentMCPServerEnabled", ed.SetAgentMCPServerEnabled(editor.DefaultRunningGameMCPServerName, false)},
			{"RemoveAgentMCPServer", ed.RemoveAgentMCPServer("evil")},
			{"SetWeeklyRequestLimit", ed.SetWeeklyRequestLimit(99999)},
			{"SetAgentPrivacy", ed.SetAgentPrivacy(editor.PrivacyGoSource, true)},
			{"SetAgentTaskRoute", ed.SetAgentTaskRoute("scene_edits", "Evil")},
			{"StoreAgentAPIKey", ed.StoreAgentAPIKey(editor.DefaultAgentAPIKeyAccount, "sk-evil")},
			{"DeleteAgentAPIKey", ed.DeleteAgentAPIKey(editor.DefaultAgentAPIKeyAccount)},
		}
		if _, gerr := ed.AgentAPIKey(editor.DefaultAgentAPIKeyAccount); !errors.Is(gerr, editor.ErrAgentSettingsYouOnly) {
			t.Errorf("AgentAPIKey under agent actor = %v, want ErrAgentSettingsYouOnly", gerr)
		}
		for _, c := range checks {
			if !errors.Is(c.err, editor.ErrAgentSettingsYouOnly) {
				t.Errorf("%s under agent actor = %v, want ErrAgentSettingsYouOnly", c.name, c.err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ed.IsMCPToolEnabled(editor.ToolNameGameDeleteFile) {
		t.Fatal("agent enabled game_delete_file")
	}
	if ed.AgentPrivacy().GoSource {
		t.Fatal("agent enabled GoSource privacy")
	}
}

func TestSettingsAgentPagesUIAndTabReachability(t *testing.T) {
	projDir := writeProjectSettingsFixture(t)
	settingsPath := filepath.Join(t.TempDir(), "editor.yaml")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.Keychain = editor.NewMemoryKeychain()
	ed.Now = func() time.Time { return time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC) }
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:          "c1",
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Transport:   "stdio",
	})
	ed.RecordAgentRequest(142)
	ed.RecordAgentReviewOutcome(true, 43)
	ed.RecordAgentReviewOutcome(false, 7)
	ed.RecordPlaytestRun(380)

	var syncedServers []editor.AgentMCPServer
	ed.SyncMCPConfig = func(_ string, servers []editor.AgentMCPServer, _ []string) error {
		syncedServers = servers
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// 1. Models and connections page
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageModels) })
	app.Settle()
	if !app.Exists("id:settings-nav-group-agent") || !app.Exists("id:settings-models-page") {
		t.Fatal("expected AGENT nav group and Models page")
	}
	if got := app.Find("id:settings-page-title").Text; got != "Models and connections" {
		t.Fatalf("Models page title = %q", got)
	}
	if got := allText(app.Find("id:settings-page-scope")); !strings.Contains(got, "This computer") {
		t.Fatalf("Models page scope = %q, want This computer", got)
	}
	if got := app.Find("id:settings-nav-models-meta").Text; got != "1 on" {
		t.Fatalf("models meta before API key = %q, want '1 on'", got)
	}

	// Set built-in API key via UI form -> stored in keychain, meta updates to "2 on", NEVER written to editor.yaml.
	app.Press("id:settings-models-apikey-edit")
	app.Settle()
	const uiSecret = "sk-ant-ui-keychain-only-777"
	app.Type("id:settings-models-apikey-input", uiSecret)
	app.Press("id:settings-models-apikey-save")
	app.Settle()
	if !ed.HasAgentAPIKey(editor.DefaultAgentAPIKeyAccount) {
		t.Fatal("API key was not saved to keychain from Models page UI")
	}
	if got := app.Find("id:settings-nav-models-meta").Text; got != "2 on" {
		t.Fatalf("models meta after API key = %q, want '2 on'", got)
	}
	yamlBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read editor.yaml: %v", err)
	}
	if bytes.Contains(yamlBytes, []byte(uiSecret)) {
		t.Fatalf("API key leaked into editor.yaml:\n%s", yamlBytes)
	}

	// "+ Connect another agent or model" opens Connect dialog.
	app.Press("id:settings-models-connect-btn")
	app.Settle()
	if !ed.ConnectDialogOpen() {
		t.Fatal("pressing + Connect another agent or model did not open Connect dialog")
	}
	app.Post(func() { ed.CloseConnectDialog() })
	app.Settle()

	// 2. Tools page
	app.Press("id:settings-nav-tools")
	app.Settle()
	if !app.Exists("id:settings-tools-page") {
		t.Fatal("Tools page did not open")
	}
	enBefore, total := ed.EnabledMCPToolCount()
	if got := app.Find("id:settings-nav-tools-meta").Text; got != "6 / 7" && enBefore != total-1 {
		t.Fatalf("tools meta = %q (en=%d total=%d), want game_delete_file off by default", got, enBefore, total)
	}
	// game_delete_file switch is role=switch and off by default; pressing it turns it on and updates meta.
	delSwitch := app.Find("id:settings-tool-switch-" + editor.ToolNameGameDeleteFile)
	if delSwitch == nil || delSwitch.Props.Role != core.RoleSwitch {
		t.Fatalf("game_delete_file switch missing or wrong role: %+v", delSwitch)
	}
	app.Press("id:settings-tool-switch-" + editor.ToolNameGameDeleteFile)
	app.Settle()
	if !ed.IsMCPToolEnabled(editor.ToolNameGameDeleteFile) {
		t.Fatal("pressing game_delete_file switch did not enable it")
	}
	// Turn it back off.
	app.Press("id:settings-tool-switch-" + editor.ToolNameGameDeleteFile)
	app.Settle()
	if ed.IsMCPToolEnabled(editor.ToolNameGameDeleteFile) {
		t.Fatal("second press did not disable game_delete_file")
	}

	// Default running-game server is listed; "+ Add a server" adds a custom server and syncs MCP config.
	if !app.Exists("id:settings-mcp-server-" + editor.DefaultRunningGameMCPServerName) {
		t.Fatal("expected Running game · nsgo dev server row")
	}
	app.Press("id:settings-mcp-server-add-toggle")
	app.Settle()
	app.Type("id:settings-mcp-server-name", "sprite-gen")
	app.Type("id:settings-mcp-server-command", "npx")
	app.Type("id:settings-mcp-server-args", "-y sprite-mcp")
	app.Type("id:settings-mcp-server-desc", "pixel sprite generator")
	app.Press("id:settings-mcp-server-submit")
	app.Settle()
	if !app.Exists("id:settings-mcp-server-sprite-gen") {
		t.Fatal("added MCP server sprite-gen not rendered")
	}
	if len(syncedServers) < 2 {
		t.Fatalf("SyncMCPConfig not called with added server: %+v", syncedServers)
	}

	// 3. Usage and privacy page
	app.Press("id:settings-nav-usage")
	app.Settle()
	if !app.Exists("id:settings-usage-page") {
		t.Fatal("Usage and privacy page did not open")
	}
	if got := app.Find("id:settings-usage-week-value").Text; !strings.Contains(got, "142") {
		t.Fatalf("this week value = %q, want 142 requests", got)
	}
	if got := app.Find("id:settings-usage-kept-value").Text; got != "86%" {
		t.Fatalf("kept value = %q, want 86%%", got)
	}
	if got := app.Find("id:settings-usage-playtests-value").Text; !strings.Contains(got, "380") {
		t.Fatalf("playtests value = %q, want 380 runs", got)
	}

	// Update weekly limit.
	app.Clear("id:settings-usage-limit-input")
	app.Type("id:settings-usage-limit-input", "200")
	app.Press("id:settings-usage-limit-apply")
	app.Settle()
	if ed.WeeklyRequestLimit() != 200 {
		t.Fatalf("WeeklyRequestLimit = %d, want 200", ed.WeeklyRequestLimit())
	}

	// Toggle Go source privacy switch (role=switch).
	goSwitch := app.Find("id:settings-privacy-go-switch")
	if goSwitch == nil || goSwitch.Props.Role != core.RoleSwitch {
		t.Fatalf("Go source switch missing or wrong role: %+v", goSwitch)
	}
	if ed.AgentPrivacy().GoSource {
		t.Fatal("GoSource should be off by default")
	}
	app.Press("id:settings-privacy-go-switch")
	app.Settle()
	if !ed.AgentPrivacy().GoSource {
		t.Fatal("pressing Go source switch did not turn it on")
	}

	// Tab reachability across controls on Usage and privacy.
	app.Focus("id:settings-nav-models")
	wantControls := []string{
		"settings-nav-models",
		"settings-nav-tools",
		"settings-nav-usage",
		"settings-usage-limit-input",
		"settings-usage-limit-apply",
		"settings-privacy-scene-switch",
		"settings-privacy-viewport-switch",
		"settings-privacy-go-switch",
		"settings-privacy-art-switch",
	}
	seen := map[string]bool{}
	for step := 0; step < 80; step++ {
		for _, id := range wantControls {
			if viewOrDescendantFocused(app.Find("id:" + id)) {
				seen[id] = true
			}
		}
		app.Key("tab")
	}
	for _, id := range wantControls {
		if !seen[id] {
			t.Errorf("Usage and privacy: control %q was NOT reached by Tab", id)
		}
	}

	// Spec Light live repaint and live Chinese switch.
	app.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false})
	app.Settle()
	if got := app.Find("id:settings-view").Style.Background; got != editor.LightSpecPalette().Bg {
		t.Fatalf("light settings-view bg = %#x, want %#x", uint32(got), uint32(editor.LightSpecPalette().Bg))
	}
	app.Post(func() { _ = ed.SetLanguage(editor.LanguageChinese) })
	app.Settle()
	if got := app.Find("id:settings-page-title").Text; got != "用量与隐私" {
		t.Fatalf("zh Usage title = %q, want 用量与隐私", got)
	}
}

func TestAgentUsageWeeklyRolloverAndLimitDraftPreservedAcrossNotifications(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC) // Thursday
	ed.Now = func() time.Time { return now }
	ed.RecordAgentRequest(12)
	if got := ed.AgentUsage().WeeklyRequests; got != 12 {
		t.Fatalf("WeeklyRequests = %d, want 12", got)
	}
	// Advance to next Monday -> WeeklyRequests must roll over to 0 on read.
	now = time.Date(2026, 11, 30, 0, 1, 0, 0, time.UTC)
	if got := ed.AgentUsage().WeeklyRequests; got != 0 {
		t.Fatalf("WeeklyRequests after Monday rollover = %d, want 0", got)
	}

	// Verify unrelated editor notifications (rev bumps) do NOT clobber settings-usage-limit-input while typing.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageUsage) })
	app.Settle()
	app.Clear("id:settings-usage-limit-input")
	app.Type("id:settings-usage-limit-input", "450")
	// Trigger an unrelated notification that bumps rev without changing WeeklyRequestLimit.
	app.Post(func() { ed.RecordPlaytestRun(1) })
	app.Settle()
	app.Press("id:settings-usage-limit-apply")
	app.Settle()
	if got := ed.WeeklyRequestLimit(); got != 450 {
		t.Fatalf("WeeklyRequestLimit after unrelated rev bump = %d, want 450 (draft was clobbered)", got)
	}
}
