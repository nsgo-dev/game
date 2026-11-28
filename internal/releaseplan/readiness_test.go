package releaseplan

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 40, G: 110, B: 220, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func itemByID(t *testing.T, items []ReadinessItem, id string) ReadinessItem {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("readiness item %q not found in %+v", id, items)
	return ReadinessItem{}
}

func TestCheckResourcesRefusesCredentialsDotfilesPEMAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "resources", "keys"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".env":             "TOKEN=secret",
		"keys/release.p12": "bytes",
		"keys/private.txt": "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----",
		"keys/deploy.txt":  "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----",
		"keys/cert.txt":    "-----BEGIN CERTIFICATE-----\npublic\n-----END CERTIFICATE-----",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, "resources", name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "resources", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "resources", "linked-dir")); err != nil {
		t.Fatal(err)
	}
	items := CheckResources(dir, &project.File{})
	if len(items) != 6 {
		t.Fatalf("items = %+v", items)
	}
	for _, item := range items {
		if item.Status != ReadinessFail {
			t.Errorf("%s status = %s, want fail", item.ID, item.Status)
		}
	}
}

func TestCheckResourcesPassesCleanResources(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "resources", "sprites"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "sprites", "hero.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := CheckResources(dir, &project.File{})
	if len(items) != 1 || items[0].Status != ReadinessOK {
		t.Fatalf("items = %+v, want one OK item", items)
	}
}

func TestEvaluateReadinessSkipsResourcesForUnsavedProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", ".env"), []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if items := EvaluateReadiness("web", "", &project.File{}); len(items) != 0 {
		t.Fatalf("items = %+v, want none for a project without a directory", items)
	}
	if items := EvaluateReadiness("web", dir, &project.File{}); len(items) != 1 || items[0].Status != ReadinessFail {
		t.Fatalf("items = %+v, want one failure for resources/.env", items)
	}
}

func TestCheckResourcesAllowsDotfileAsWarning(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "resources", ".well-known"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", ".well-known", "asset"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	proj := &project.File{Export: &project.Export{AllowResources: []string{"resources/.well-known", "resources/.well-known/asset"}}}
	items := CheckResources(dir, proj)
	if len(items) != 1 || items[0].Status != ReadinessWarn {
		t.Fatalf("items = %+v, want one warning for the allowed directory", items)
	}
}

func TestEvaluateReadinessIOSConfiguredThroughNSGoYAMLPassesEveryItem(t *testing.T) {
	dir := t.TempDir()

	writeTestPNG(t, filepath.Join(dir, "art", "MyIcon.png"), 1024, 1024)
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "certs", "AppStore.mobileprovision"), []byte("profile-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	nsgoYAML := `name: Cave Run
id: com.example.caverun
version: 1.0.0
icon:
  png: art/MyIcon.png
sign:
  profile: certs/AppStore.mobileprovision
  identity: "Apple Distribution: Example Studio (TEAMID)"
apple:
  privacy:
    accessed_apis:
      NSPrivacyAccessedAPICategoryDiskSpace:
        - E174.1
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	touchYAML := `version: 1
reference: {width: 844, height: 390}
controls:
  - id: move
    kind: dpad
    anchor: bottomLeft
    size: {width: 104, height: 104}
    left: left
    right: right
  - id: jump
    kind: button
    anchor: bottomRight
    size: {width: 96, height: 96}
    action: jump
    text: JUMP
`
	if err := os.WriteFile(filepath.Join(dir, "controls.touch.yaml"), []byte(touchYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	proj := &project.File{
		Version: 1,
		Name:    "Cave Run",
		ID:      "com.example.caverun",
	}

	items := EvaluateReadiness("ios", dir, proj)
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Status != ReadinessOK {
			t.Errorf("item %s status = %q (%s), want ok", it.ID, it.Status, it.Detail)
		}
	}

	if got := itemByID(t, items, "provisioning_profile").Detail; !strings.Contains(got, "certs/AppStore.mobileprovision") {
		t.Errorf("provisioning_profile detail = %q, want certs/AppStore.mobileprovision", got)
	}
	if got := itemByID(t, items, "distribution_cert").Detail; !strings.Contains(got, "sign.identity") {
		t.Errorf("distribution_cert detail = %q, want sign.identity", got)
	}
	if got := itemByID(t, items, "privacy_manifest").Detail; !strings.Contains(got, "apple.privacy") {
		t.Errorf("privacy_manifest detail = %q, want apple.privacy", got)
	}
	if got := itemByID(t, items, "touch_controls").Detail; !strings.Contains(got, "controls.touch.yaml") {
		t.Errorf("touch_controls detail = %q, want controls.touch.yaml", got)
	}

	// Without explicit sign.identity, sign.profile still passes distribution_cert via keychain auto-selection.
	nsgoNoIdentity := strings.ReplaceAll(nsgoYAML, "  identity: \"Apple Distribution: Example Studio (TEAMID)\"\n", "")
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoNoIdentity), 0o644); err != nil {
		t.Fatal(err)
	}
	itemsNoID := EvaluateReadiness("ios", dir, proj)
	if cert := itemByID(t, itemsNoID, "distribution_cert"); cert.Status != ReadinessOK {
		t.Fatalf("distribution_cert without sign.identity = %+v, want ok when sign.profile is configured", cert)
	}
}

func TestEvaluateReadinessIOSEmptyProjectNamesNSGoYAMLKeys(t *testing.T) {
	dir := t.TempDir()
	items := EvaluateReadiness("ios", dir, &project.File{})
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}

	bundle := itemByID(t, items, "bundle_id")
	if bundle.Status != ReadinessFail || !strings.Contains(bundle.Detail, "nsgo.yaml") || !strings.Contains(bundle.Detail, "id") {
		t.Errorf("bundle_id = %+v, want fail naming nsgo.yaml id", bundle)
	}

	icon := itemByID(t, items, "app_icon")
	if icon.Status != ReadinessWarn || !strings.Contains(icon.Detail, "icon.apple") || !strings.Contains(icon.Detail, "icon.png") || !strings.Contains(icon.Detail, "assets/Icon.png") {
		t.Errorf("app_icon = %+v, want warn naming icon.apple, icon.png, and assets/Icon.png", icon)
	}

	cert := itemByID(t, items, "distribution_cert")
	if cert.Status != ReadinessWarn || !strings.Contains(cert.Detail, "sign.identity") || !strings.Contains(cert.Detail, "nsgo.yaml") {
		t.Errorf("distribution_cert = %+v, want warn naming sign.identity in nsgo.yaml", cert)
	}

	profile := itemByID(t, items, "provisioning_profile")
	if profile.Status != ReadinessFail || !strings.Contains(profile.Detail, "sign.profile") || !strings.Contains(profile.Detail, "nsgo.yaml") {
		t.Errorf("provisioning_profile = %+v, want fail naming sign.profile in nsgo.yaml", profile)
	}

	privacy := itemByID(t, items, "privacy_manifest")
	if privacy.Status != ReadinessOK || !strings.Contains(privacy.Detail, "apple.privacy") || !strings.Contains(privacy.Detail, PrivacyManifestRelPath) {
		t.Errorf("privacy_manifest = %+v, want ok naming apple.privacy and %s", privacy, PrivacyManifestRelPath)
	}

	touch := itemByID(t, items, "touch_controls")
	if touch.Status != ReadinessWarn || touch.AgentCanHandle || touch.ActionLabel != "" {
		t.Errorf("touch_controls without actions = %+v, want advisory warn with no fake agent action", touch)
	}
}

func TestEvaluateReadinessIOSIconsCaseSensitivityAndAppleIcon(t *testing.T) {
	// Default assets/Icon.png (exact case) passes; lowercase assets/icon.png does not.
	dirLower := t.TempDir()
	writeTestPNG(t, filepath.Join(dirLower, "assets", "icon.png"), 512, 512)
	if it := itemByID(t, EvaluateReadiness("ios", dirLower, &project.File{ID: "com.example.a"}), "app_icon"); it.Status == ReadinessOK {
		t.Fatalf("lowercase assets/icon.png should not pass as default assets/Icon.png: %+v", it)
	}

	dirDefault := t.TempDir()
	writeTestPNG(t, filepath.Join(dirDefault, "assets", "Icon.png"), 1024, 1024)
	if it := itemByID(t, EvaluateReadiness("ios", dirDefault, &project.File{ID: "com.example.a"}), "app_icon"); it.Status != ReadinessOK || !strings.Contains(it.Detail, "assets/Icon.png") {
		t.Fatalf("default assets/Icon.png = %+v, want ok", it)
	}

	// Non-square PNG fails (NSGo readSquareIcon refuses non-square icons).
	dirRect := t.TempDir()
	writeTestPNG(t, filepath.Join(dirRect, "assets", "Icon.png"), 512, 256)
	if it := itemByID(t, EvaluateReadiness("ios", dirRect, &project.File{ID: "com.example.a"}), "app_icon"); it.Status != ReadinessFail || !strings.Contains(it.Detail, "square") {
		t.Fatalf("non-square assets/Icon.png = %+v, want fail mentioning square", it)
	}

	// Default assets/AppIcon.icon passes.
	dirAppleDefault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirAppleDefault, "assets", "AppIcon.icon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if it := itemByID(t, EvaluateReadiness("ios", dirAppleDefault, &project.File{ID: "com.example.a"}), "app_icon"); it.Status != ReadinessOK || !strings.Contains(it.Detail, "assets/AppIcon.icon") {
		t.Fatalf("default assets/AppIcon.icon = %+v, want ok", it)
	}

	// Explicit icon.apple pointing to .icns (macOS-only) falls back to icon.png / warns if no PNG.
	dirICNS := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirICNS, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirICNS, "assets", "AppIcon.icns"), []byte("icns"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirICNS, project.NSGoFileName), []byte("name: A\nid: com.example.a\nicon:\n  apple: assets/AppIcon.icns\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if it := itemByID(t, EvaluateReadiness("ios", dirICNS, &project.File{ID: "com.example.a"}), "app_icon"); it.Status != ReadinessWarn || !strings.Contains(it.Detail, "macOS-only") {
		t.Fatalf(".icns without PNG = %+v, want warn mentioning macOS-only", it)
	}
}

func TestPrivacyManifestDefaultAndMergeMatchesNSGo(t *testing.T) {
	def := DefaultPrivacyManifestXML()
	decoded, err := DecodePlist(def)
	if err != nil {
		t.Fatalf("DecodePlist(DefaultPrivacyManifestXML): %v", err)
	}
	m, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded type = %T, want map[string]any", decoded)
	}
	if err := ValidateResourcePrivacy(m); err != nil {
		t.Fatalf("ValidateResourcePrivacy(default): %v", err)
	}
	if _, hasDomains := m["NSPrivacyTrackingDomains"]; hasDomains {
		t.Fatalf("default manifest must omit unused NSPrivacyTrackingDomains when tracking is false:\n%s", def)
	}
	if m["NSPrivacyTracking"] != false {
		t.Fatalf("NSPrivacyTracking = %v, want false", m["NSPrivacyTracking"])
	}
	apis, _ := m["NSPrivacyAccessedAPITypes"].([]any)
	if len(apis) != len(NSGoPrivacyReasons) {
		t.Fatalf("default NSPrivacyAccessedAPITypes count = %d, want %d:\n%s", len(apis), len(NSGoPrivacyReasons), def)
	}

	// Merging default resources/PrivacyInfo.xcprivacy with nsgo.yaml's apple.privacy deduplicates NSGo reasons and adds app entries.
	tracking := true
	merged, err := MergePrivacyManifest(def, NSGoPrivacyConfig{
		AccessedAPIs: map[string][]string{
			"NSPrivacyAccessedAPICategoryDiskSpace":    {"E174.1"},
			"NSPrivacyAccessedAPICategoryUserDefaults": {"CA92.1", "1C8F.1"},
		},
		Tracking:        &tracking,
		TrackingDomains: []string{"telemetry.example.com"},
		CollectedData: []map[string]any{{
			"NSPrivacyCollectedDataType":         "NSPrivacyCollectedDataTypeEmailAddress",
			"NSPrivacyCollectedDataTypeLinked":   true,
			"NSPrivacyCollectedDataTypeTracking": false,
			"NSPrivacyCollectedDataTypePurposes": []any{"NSPrivacyCollectedDataTypePurposeAppFunctionality"},
		}},
	})
	if err != nil {
		t.Fatalf("MergePrivacyManifest: %v", err)
	}
	// Idempotent re-merge produces byte-identical plist.
	mergedAgain, err := MergePrivacyManifest(merged, NSGoPrivacyConfig{})
	if err != nil {
		t.Fatalf("second MergePrivacyManifest: %v", err)
	}
	if !bytes.Equal(merged, mergedAgain) {
		t.Fatalf("MergePrivacyManifest not idempotent:\n%s\n---\n%s", merged, mergedAgain)
	}

	gotVal, err := DecodePlist(merged)
	if err != nil {
		t.Fatalf("DecodePlist(merged): %v", err)
	}
	got := gotVal.(map[string]any)
	wantAPIs := []any{
		map[string]any{"NSPrivacyAccessedAPIType": "NSPrivacyAccessedAPICategoryDiskSpace", "NSPrivacyAccessedAPITypeReasons": []any{"E174.1"}},
		map[string]any{"NSPrivacyAccessedAPIType": "NSPrivacyAccessedAPICategoryFileTimestamp", "NSPrivacyAccessedAPITypeReasons": []any{"C617.1"}},
		map[string]any{"NSPrivacyAccessedAPIType": "NSPrivacyAccessedAPICategorySystemBootTime", "NSPrivacyAccessedAPITypeReasons": []any{"35F9.1"}},
		map[string]any{"NSPrivacyAccessedAPIType": "NSPrivacyAccessedAPICategoryUserDefaults", "NSPrivacyAccessedAPITypeReasons": []any{"1C8F.1", "CA92.1"}},
	}
	if !reflect.DeepEqual(got["NSPrivacyAccessedAPITypes"], wantAPIs) {
		t.Fatalf("merged accessed APIs:\n%#v\nwant:\n%#v", got["NSPrivacyAccessedAPITypes"], wantAPIs)
	}
	if got["NSPrivacyTracking"] != true || !reflect.DeepEqual(got["NSPrivacyTrackingDomains"], []any{"telemetry.example.com"}) {
		t.Fatalf("merged tracking = (%v, %v)", got["NSPrivacyTracking"], got["NSPrivacyTrackingDomains"])
	}
}

func TestEvaluateReadinessIOSPrivacyManifestValidationAndStrayRoot(t *testing.T) {
	// Stray root PrivacyInfo.xcprivacy warns that nsgo build ignores it.
	dirStray := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirStray, PrivacyManifestName), DefaultPrivacyManifestXML(), 0o644); err != nil {
		t.Fatal(err)
	}
	it := itemByID(t, EvaluateReadiness("ios", dirStray, &project.File{ID: "com.example.a"}), "privacy_manifest")
	if it.Status != ReadinessWarn || !strings.Contains(it.Detail, PrivacyManifestRelPath) || !it.AgentCanHandle {
		t.Fatalf("stray root PrivacyInfo.xcprivacy = %+v, want warn with agent fix", it)
	}

	// Malformed resources/PrivacyInfo.xcprivacy fails.
	dirBad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirBad, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirBad, "resources", PrivacyManifestName), []byte("<plist><dict><key>NSPrivacyTracking</key><string>nope</string></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	it = itemByID(t, EvaluateReadiness("ios", dirBad, &project.File{ID: "com.example.a"}), "privacy_manifest")
	if it.Status != ReadinessFail || !strings.Contains(it.Detail, "NSPrivacyTracking") {
		t.Fatalf("malformed resources/PrivacyInfo.xcprivacy = %+v, want fail", it)
	}

	// Invalid apple.privacy in nsgo.yaml fails.
	dirBadNSGo := t.TempDir()
	nsgoBad := "name: A\nid: com.example.a\napple:\n  privacy:\n    accessed_apis:\n      DiskSpace: [E174.1]\n"
	if err := os.WriteFile(filepath.Join(dirBadNSGo, project.NSGoFileName), []byte(nsgoBad), 0o644); err != nil {
		t.Fatal(err)
	}
	it = itemByID(t, EvaluateReadiness("ios", dirBadNSGo, &project.File{ID: "com.example.a"}), "privacy_manifest")
	if it.Status != ReadinessFail || !strings.Contains(it.Detail, "apple.privacy") {
		t.Fatalf("invalid apple.privacy = %+v, want fail naming apple.privacy", it)
	}
}

func TestGenerateTouchLayoutUsesExistingActionsOnly(t *testing.T) {
	if _, ok := GenerateTouchLayout(&project.File{}); ok {
		t.Fatal("GenerateTouchLayout returned true for project with no input actions")
	}

	proj := &project.File{
		Input: &project.Input{
			Actions: []project.Action{
				{Name: "move_left"},
				{Name: "move_right"},
				{Name: "jump"},
				{Name: "dash"},
			},
			Axes: []project.Axis{
				{Name: "move_x", Negative: "move_left", Positive: "move_right"},
			},
		},
	}
	layout, ok := GenerateTouchLayout(proj)
	if !ok {
		t.Fatal("GenerateTouchLayout returned false for project with actions")
	}
	raw, err := input.EncodeTouchLayout(layout)
	if err != nil {
		t.Fatalf("EncodeTouchLayout: %v", err)
	}
	parsed, err := input.ParseTouchLayout(raw)
	if err != nil {
		t.Fatalf("ParseTouchLayout: %v", err)
	}
	if len(parsed.Controls) != 3 {
		t.Fatalf("controls = %d, want 3 (dpad + jump + dash):\n%s", len(parsed.Controls), raw)
	}
	if parsed.Controls[0].Kind != input.TouchKindDPad || parsed.Controls[0].Left != "move_left" || parsed.Controls[0].Right != "move_right" {
		t.Fatalf("controls[0] = %+v, want dpad binding move_left/move_right", parsed.Controls[0])
	}
	if parsed.Controls[1].Kind != input.TouchKindButton || parsed.Controls[1].Action != "jump" {
		t.Fatalf("controls[1] = %+v, want jump button", parsed.Controls[1])
	}
	if parsed.Controls[2].Kind != input.TouchKindButton || parsed.Controls[2].Action != "dash" {
		t.Fatalf("controls[2] = %+v, want dash button", parsed.Controls[2])
	}
	if strings.Contains(string(raw), "touch_action") {
		t.Fatalf("generated layout must never invent touch_action:\n%s", raw)
	}
}
