package l10n_test

import (
	"math"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAssetCatalogResolveFallbackChain(t *testing.T) {
	cat := l10n.NewAssetCatalog("en")
	if got := cat.BaseLocale(); got != "en" {
		t.Fatalf("BaseLocale() = %q, want %q", got, "en")
	}

	cat.RegisterMap("voice/intro.wav", map[string]string{
		"en":         "locales/en/voice/intro.wav",
		"zh":         "locales/zh/voice/intro.wav",
		"zh-Hans":    "locales/zh-Hans/voice/intro.wav",
		"zh-Hans-CN": "locales/zh-Hans-CN/voice/intro.wav",
	})
	cat.Register("voice/briefing.wav", "en", "locales/en/voice/briefing.wav")
	cat.Register("voice/briefing.wav", "zh", "locales/zh/voice/briefing.wav")
	cat.Register("voice/briefing.wav", "zh-Hans", "locales/zh-Hans/voice/briefing.wav")

	cat.Register("voice/credits.wav", "en", "locales/en/voice/credits.wav")
	cat.Register("voice/credits.wav", "zh", "locales/zh/voice/credits.wav")

	cat.Register("ui/title_banner.png", "en", "ui/title_banner.png")

	tests := []struct {
		name           string
		asset          string
		locale         string
		wantOK         bool
		wantReqLocale  string
		wantResLocale  string
		wantPath       string
		wantIsFallback bool
	}{
		{
			name:           "exact zh-Hans-CN match",
			asset:          "voice/intro.wav",
			locale:         "zh-Hans-CN",
			wantOK:         true,
			wantReqLocale:  "zh-Hans-CN",
			wantResLocale:  "zh-Hans-CN",
			wantPath:       "locales/zh-Hans-CN/voice/intro.wav",
			wantIsFallback: false,
		},
		{
			name:           "normalized underscore tag exact match",
			asset:          "voice/intro.wav",
			locale:         "zh_hans_cn",
			wantOK:         true,
			wantReqLocale:  "zh-Hans-CN",
			wantResLocale:  "zh-Hans-CN",
			wantPath:       "locales/zh-Hans-CN/voice/intro.wav",
			wantIsFallback: false,
		},
		{
			name:           "zh-Hans-CN falls back to zh-Hans",
			asset:          "voice/briefing.wav",
			locale:         "zh-Hans-CN",
			wantOK:         true,
			wantReqLocale:  "zh-Hans-CN",
			wantResLocale:  "zh-Hans",
			wantPath:       "locales/zh-Hans/voice/briefing.wav",
			wantIsFallback: true,
		},
		{
			name:           "zh-Hans-CN falls back to zh",
			asset:          "voice/credits.wav",
			locale:         "zh-Hans-CN",
			wantOK:         true,
			wantReqLocale:  "zh-Hans-CN",
			wantResLocale:  "zh",
			wantPath:       "locales/zh/voice/credits.wav",
			wantIsFallback: true,
		},
		{
			name:           "zh-Hans-CN falls back to BaseLocale en",
			asset:          "ui/title_banner.png",
			locale:         "zh-Hans-CN",
			wantOK:         true,
			wantReqLocale:  "zh-Hans-CN",
			wantResLocale:  "en",
			wantPath:       "ui/title_banner.png",
			wantIsFallback: true,
		},
		{
			name:           "zh-Hant-TW skips zh-Hans and falls back to zh",
			asset:          "voice/briefing.wav",
			locale:         "zh-Hant-TW",
			wantOK:         true,
			wantReqLocale:  "zh-Hant-TW",
			wantResLocale:  "zh",
			wantPath:       "locales/zh/voice/briefing.wav",
			wantIsFallback: true,
		},
		{
			name:           "untranslated locale falls back to BaseLocale",
			asset:          "voice/intro.wav",
			locale:         "fr-FR",
			wantOK:         true,
			wantReqLocale:  "fr-FR",
			wantResLocale:  "en",
			wantPath:       "locales/en/voice/intro.wav",
			wantIsFallback: true,
		},
		{
			name:           "empty locale falls back to BaseLocale",
			asset:          "ui/title_banner.png",
			locale:         "",
			wantOK:         true,
			wantReqLocale:  "",
			wantResLocale:  "en",
			wantPath:       "ui/title_banner.png",
			wantIsFallback: true,
		},
		{
			name:   "missing asset returns false",
			asset:  "voice/nonexistent.wav",
			locale: "en",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cat.Resolve(tc.asset, tc.locale)
			if ok != tc.wantOK {
				t.Fatalf("Resolve(%q, %q) ok = %v, want %v (res=%+v)", tc.asset, tc.locale, ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got.LogicalName != tc.asset ||
				got.RequestedLocale != tc.wantReqLocale ||
				got.ResolvedLocale != tc.wantResLocale ||
				got.Path != tc.wantPath ||
				got.IsFallback != tc.wantIsFallback {
				t.Errorf("Resolve(%q, %q) = %+v, want {LogicalName:%q RequestedLocale:%q ResolvedLocale:%q Path:%q IsFallback:%v}",
					tc.asset, tc.locale, got, tc.asset, tc.wantReqLocale, tc.wantResLocale, tc.wantPath, tc.wantIsFallback)
			}
		})
	}

	wantLocales := []string{"en", "zh", "zh-Hans", "zh-Hans-CN"}
	if got := cat.Locales(); !slices.Equal(got, wantLocales) {
		t.Errorf("Locales() = %v, want %v", got, wantLocales)
	}
	wantNames := []string{"ui/title_banner.png", "voice/briefing.wav", "voice/credits.wav", "voice/intro.wav"}
	if got := cat.Names(); !slices.Equal(got, wantNames) {
		t.Errorf("Names() = %v, want %v", got, wantNames)
	}
}

func TestAssetCatalogDiscoverFS(t *testing.T) {
	if _, err := l10n.DiscoverFS(nil, "en"); err == nil {
		t.Fatal("DiscoverFS(nil, \"en\") = nil error, want error")
	}

	fsys := fstest.MapFS{
		// Directory convention: locales/<locale>/<name>
		"locales/en/voice/intro.wav":      {Data: []byte("en-intro")},
		"locales/ja/voice/intro.wav":      {Data: []byte("ja-intro")},
		"locales/zh-Hans/voice/intro.wav": {Data: []byte("zh-hans-intro")},
		"locales/zh_hans_cn/voice/ad.wav": {Data: []byte("zh-cn-ad")},

		// Dot suffix convention: <name>.<locale>.<ext>
		"ui/title_banner.png":         {Data: []byte("en-banner")},
		"ui/title_banner.ja.png":      {Data: []byte("ja-banner")},
		"ui/title_banner.zh-Hans.png": {Data: []byte("zh-banner")},

		// Underscore suffix convention: <name>_<locale>.<ext>
		"ui/hud_badge.png":            {Data: []byte("en-badge")},
		"ui/hud_badge_ja.png":         {Data: []byte("ja-badge")},
		"ui/hud_badge_zh_Hans.png":    {Data: []byte("zh-hans-badge")},
		"ui/hud_badge_zh_Hans_CN.png": {Data: []byte("zh-cn-badge")},

		// Explicit baseLocale variant takes precedence over unlocalized default.
		"ui/logo.png":            {Data: []byte("default-logo")},
		"locales/en/ui/logo.png": {Data: []byte("explicit-en-logo")},

		// Unlocalized assets with non-locale suffixes must NOT be misparsed.
		"ui/btn_ok.png":      {Data: []byte("ok")},
		"ui/bg_hd.png":       {Data: []byte("hd")},
		"sfx/hit_01.wav":     {Data: []byte("hit")},
		"sfx/click.wav":      {Data: []byte("click")},
		"ui/level_bg_en.png": {Data: []byte("en-level-bg")},
		"ui/level_bg_ja.png": {Data: []byte("ja-level-bg")},
		".DS_Store":          {Data: []byte("ignored")},
		"ui/.gitkeep":        {Data: []byte("ignored")},
	}

	cat, err := l10n.DiscoverFS(fsys, "en")
	if err != nil {
		t.Fatalf("DiscoverFS: %v", err)
	}

	checks := []struct {
		asset          string
		locale         string
		wantPath       string
		wantResolved   string
		wantIsFallback bool
	}{
		{"voice/intro.wav", "en", "locales/en/voice/intro.wav", "en", false},
		{"voice/intro.wav", "ja", "locales/ja/voice/intro.wav", "ja", false},
		{"voice/intro.wav", "zh-Hans-CN", "locales/zh-Hans/voice/intro.wav", "zh-Hans", true},
		{"voice/ad.wav", "zh-Hans-CN", "locales/zh_hans_cn/voice/ad.wav", "zh-Hans-CN", false},

		{"ui/title_banner.png", "en", "ui/title_banner.png", "en", false},
		{"ui/title_banner.png", "ja", "ui/title_banner.ja.png", "ja", false},
		{"ui/title_banner.png", "zh-Hans", "ui/title_banner.zh-Hans.png", "zh-Hans", false},
		{"ui/title_banner.png", "fr", "ui/title_banner.png", "en", true},

		{"ui/hud_badge.png", "en", "ui/hud_badge.png", "en", false},
		{"ui/hud_badge.png", "ja", "ui/hud_badge_ja.png", "ja", false},
		{"ui/hud_badge.png", "zh-Hans", "ui/hud_badge_zh_Hans.png", "zh-Hans", false},
		{"ui/hud_badge.png", "zh-Hans-CN", "ui/hud_badge_zh_Hans_CN.png", "zh-Hans-CN", false},

		{"ui/logo.png", "en", "locales/en/ui/logo.png", "en", false},

		{"ui/btn_ok.png", "en", "ui/btn_ok.png", "en", false},
		{"ui/bg_hd.png", "ja", "ui/bg_hd.png", "en", true},
		{"sfx/hit_01.wav", "ja", "sfx/hit_01.wav", "en", true},
		{"sfx/click.wav", "zh-Hans", "sfx/click.wav", "en", true},

		// level_bg_en.png must parse as logical "ui/level_bg.png" locale "en" (not bg-EN).
		{"ui/level_bg.png", "en", "ui/level_bg_en.png", "en", false},
		{"ui/level_bg.png", "ja", "ui/level_bg_ja.png", "ja", false},
	}

	for _, tc := range checks {
		got, ok := cat.Resolve(tc.asset, tc.locale)
		if !ok {
			t.Errorf("Resolve(%q, %q) = not found, want %q", tc.asset, tc.locale, tc.wantPath)
			continue
		}
		if got.Path != tc.wantPath || got.ResolvedLocale != tc.wantResolved || got.IsFallback != tc.wantIsFallback {
			t.Errorf("Resolve(%q, %q) = %+v, want path=%q resolved=%q fallback=%v",
				tc.asset, tc.locale, got, tc.wantPath, tc.wantResolved, tc.wantIsFallback)
		}
	}

	if _, ok := cat.Resolve(".DS_Store", "en"); ok {
		t.Error("expected .DS_Store to be ignored by DiscoverFS")
	}
	if _, ok := cat.Resolve("ui/.gitkeep", "en"); ok {
		t.Error("expected ui/.gitkeep to be ignored by DiscoverFS")
	}
}

func TestAssetCatalogAffectedGroupsAndSetLocale(t *testing.T) {
	cat := l10n.NewAssetCatalog("en")
	cat.RegisterMap("voice/intro.wav", map[string]string{
		"en":      "locales/en/voice/intro.wav",
		"ja":      "locales/ja/voice/intro.wav",
		"zh-Hans": "locales/zh-Hans/voice/intro.wav",
	})
	cat.RegisterMap("ui/title_banner.png", map[string]string{
		"en": "ui/title_banner.png",
		"ja": "ui/title_banner.ja.png",
	})
	cat.Register("sfx/click.wav", "en", "sfx/click.wav")

	cat.RegisterGroup("voice", "voice/intro.wav")
	cat.RegisterGroup("ui", "ui/title_banner.png")
	cat.RegisterGroup("shared", "sfx/click.wav")

	if got := cat.Groups(); !slices.Equal(got, []string{"shared", "ui", "voice"}) {
		t.Fatalf("Groups() = %v, want [shared ui voice]", got)
	}
	if got := cat.GroupAssets("voice"); !slices.Equal(got, []string{"voice/intro.wav"}) {
		t.Fatalf("GroupAssets(voice) = %v, want [voice/intro.wav]", got)
	}

	// en -> ja affects ui and voice, not shared.
	if got := cat.AffectedGroups("en", "ja"); !slices.Equal(got, []string{"ui", "voice"}) {
		t.Errorf("AffectedGroups(en, ja) = %v, want [ui voice]", got)
	}
	// en -> zh-Hans affects voice only (ui falls back to en in both!).
	if got := cat.AffectedGroups("en", "zh-Hans"); !slices.Equal(got, []string{"voice"}) {
		t.Errorf("AffectedGroups(en, zh-Hans) = %v, want [voice]", got)
	}
	// zh-Hans -> zh-Hans-CN affects nothing (all assets resolve identically).
	if got := cat.AffectedGroups("zh-Hans", "zh-Hans-CN"); len(got) != 0 {
		t.Errorf("AffectedGroups(zh-Hans, zh-Hans-CN) = %v, want empty", got)
	}
	// en -> fr affects nothing (fr falls back to en for everything).
	if got := cat.AffectedGroups("en", "fr"); len(got) != 0 {
		t.Errorf("AffectedGroups(en, fr) = %v, want empty", got)
	}
	// ja -> fr affects ui and voice (switching back to en fallback).
	if got := cat.AffectedGroups("ja", "fr"); !slices.Equal(got, []string{"ui", "voice"}) {
		t.Errorf("AffectedGroups(ja, fr) = %v, want [ui voice]", got)
	}

	type reloadEvent struct {
		groups []string
		locale string
	}
	var events []reloadEvent
	stop := cat.OnReload(func(groups []string, locale string) {
		events = append(events, reloadEvent{groups: groups, locale: locale})
	})

	if got := cat.SetLocale("ja"); !slices.Equal(got, []string{"ui", "voice"}) {
		t.Fatalf("SetLocale(ja) = %v, want [ui voice]", got)
	}
	if got := cat.SetLocale("zh-Hans"); !slices.Equal(got, []string{"ui", "voice"}) {
		t.Fatalf("SetLocale(zh-Hans) = %v, want [ui voice]", got)
	}
	// zh-Hans -> zh-Hans-CN should trigger no reload listener.
	if got := cat.SetLocale("zh-Hans-CN"); len(got) != 0 {
		t.Fatalf("SetLocale(zh-Hans-CN) = %v, want empty", got)
	}
	if len(events) != 2 {
		t.Fatalf("OnReload event count = %d, want 2: %+v", len(events), events)
	}
	if cat.Locale() != "zh-Hans-CN" {
		t.Errorf("Locale() = %q, want zh-Hans-CN", cat.Locale())
	}

	stop()
	cat.SetLocale("en")
	if len(events) != 2 {
		t.Errorf("OnReload fired after stop(): events = %+v", events)
	}
}

func TestAssetCatalogWatchLocaleLiveSwitch(t *testing.T) {
	cat := l10n.NewAssetCatalog("en")
	cat.RegisterMap("voice/intro.wav", map[string]string{
		"en":      "locales/en/voice/intro.wav",
		"ja":      "locales/ja/voice/intro.wav",
		"zh-Hans": "locales/zh-Hans/voice/intro.wav",
	})
	cat.RegisterMap("voice/outro.wav", map[string]string{
		"en":      "locales/en/voice/outro.wav",
		"ja":      "locales/ja/voice/outro.wav",
		"zh-Hans": "locales/zh-Hans/voice/outro.wav",
	})
	cat.Register("sfx/click.wav", "en", "sfx/click.wav")
	cat.Register("sfx/jump.wav", "en", "sfx/jump.wav")

	cat.RegisterGroup("voice", "voice/intro.wav", "voice/outro.wav")
	cat.RegisterGroup("shared", "sfx/click.wav", "sfx/jump.wav")

	type reloadCall struct {
		groups []string
		locale string
	}

	var (
		startCount  int
		localeSig   *core.Signal[string]
		stopWatch   func()
		reloads     []reloadCall
		groupLoads  = make(map[string]int)
		loadedPaths = make(map[string]string)
	)

	// Simulate initial load at startup for "en".
	for _, g := range cat.Groups() {
		for _, a := range cat.ResolveGroup(g, "en") {
			loadedPaths[a.LogicalName] = a.Path
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		startCount++
		localeSig = core.NewSignal(cx, "en")
		stopWatch = cat.WatchLocale(cx, localeSig, func(groups []string, locale string) {
			reloads = append(reloads, reloadCall{
				groups: slices.Clone(groups),
				locale: locale,
			})
			for _, g := range groups {
				groupLoads[g]++
				for _, a := range cat.ResolveGroup(g, locale) {
					loadedPaths[a.LogicalName] = a.Path
				}
			}
		})
		return core.View{}
	})

	if startCount != 1 {
		t.Fatalf("startCount = %d, want 1", startCount)
	}
	if len(reloads) != 0 {
		t.Fatalf("expected no reload on initial mount, got %+v", reloads)
	}

	// Switch "en" -> "ja": reloads ONLY "voice", skips "shared", without restarting.
	app.Post(func() { localeSig.Set("ja") })
	app.Settle()

	if startCount != 1 {
		t.Fatalf("app restarted on en->ja switch: startCount = %d", startCount)
	}
	if len(reloads) != 1 || !slices.Equal(reloads[0].groups, []string{"voice"}) || reloads[0].locale != "ja" {
		t.Fatalf("reloads after en->ja = %+v, want [{[voice] ja}]", reloads)
	}
	if groupLoads["voice"] != 1 || groupLoads["shared"] != 0 {
		t.Errorf("groupLoads after en->ja = %v, want voice=1 shared=0", groupLoads)
	}
	if loadedPaths["voice/intro.wav"] != "locales/ja/voice/intro.wav" ||
		loadedPaths["voice/outro.wav"] != "locales/ja/voice/outro.wav" ||
		loadedPaths["sfx/click.wav"] != "sfx/click.wav" {
		t.Errorf("loadedPaths after en->ja = %v", loadedPaths)
	}

	// Switch "ja" -> "zh-Hans": reloads ONLY "voice", skips "shared", without restarting.
	app.Post(func() { localeSig.Set("zh-Hans") })
	app.Settle()

	if startCount != 1 {
		t.Fatalf("app restarted on ja->zh-Hans switch: startCount = %d", startCount)
	}
	if len(reloads) != 2 || !slices.Equal(reloads[1].groups, []string{"voice"}) || reloads[1].locale != "zh-Hans" {
		t.Fatalf("reloads after ja->zh-Hans = %+v, want second call {[voice] zh-Hans}", reloads)
	}
	if groupLoads["voice"] != 2 || groupLoads["shared"] != 0 {
		t.Errorf("groupLoads after ja->zh-Hans = %v, want voice=2 shared=0", groupLoads)
	}
	if loadedPaths["voice/intro.wav"] != "locales/zh-Hans/voice/intro.wav" ||
		loadedPaths["voice/outro.wav"] != "locales/zh-Hans/voice/outro.wav" ||
		loadedPaths["sfx/jump.wav"] != "sfx/jump.wav" {
		t.Errorf("loadedPaths after ja->zh-Hans = %v", loadedPaths)
	}

	// Switch "zh-Hans" -> "zh-Hans-CN": resolves to identical assets -> NO reload!
	app.Post(func() { localeSig.Set("zh-Hans-CN") })
	app.Settle()

	if len(reloads) != 2 {
		t.Fatalf("unexpected reload on zh-Hans->zh-Hans-CN: %+v", reloads)
	}

	// Stopping the watcher prevents further reloads.
	app.Post(func() { stopWatch() })
	app.Post(func() { localeSig.Set("en") })
	app.Settle()

	if len(reloads) != 2 {
		t.Fatalf("watcher fired after stop: %+v", reloads)
	}
}

func TestAssetCatalogWatchLocaleEnvFallback(t *testing.T) {
	cat := l10n.NewAssetCatalog("en")
	cat.RegisterMap("voice/intro.wav", map[string]string{
		"en": "locales/en/voice/intro.wav",
		"ja": "locales/ja/voice/intro.wav",
	})
	cat.Register("sfx/click.wav", "en", "sfx/click.wav")
	cat.RegisterGroup("voice", "voice/intro.wav")
	cat.RegisterGroup("shared", "sfx/click.wav")

	var seenGroups [][]string
	var seenLocales []string

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cat.WatchLocale(cx, nil, func(groups []string, locale string) {
			seenGroups = append(seenGroups, slices.Clone(groups))
			seenLocales = append(seenLocales, locale)
		})
		return core.View{}
	})

	// Initial empty env defaults to BaseLocale ("en") without reloading.
	if len(seenGroups) != 0 {
		t.Fatalf("unexpected initial reload: %v", seenGroups)
	}

	app.SetEnvironment(core.Environment{Locale: "ja-JP"})
	if len(seenGroups) != 1 || !slices.Equal(seenGroups[0], []string{"voice"}) || seenLocales[0] != "ja-JP" {
		t.Fatalf("env ja-JP reload = groups:%v locales:%v, want [[voice]] [ja-JP]", seenGroups, seenLocales)
	}
}

func TestAssetCatalogCheckMissing(t *testing.T) {
	// Empty catalog has 100% coverage and no missing assets.
	empty := l10n.NewAssetCatalog("en")
	emptyReports := empty.CheckMissing("en")
	if len(emptyReports) != 1 || emptyReports[0].TotalAssets != 0 || emptyReports[0].Coverage != 1.0 || !emptyReports[0].Complete() {
		t.Fatalf("empty CheckMissing(en) = %+v", emptyReports)
	}

	cat := l10n.NewAssetCatalog("en")
	cat.RegisterMap("voice/intro.wav", map[string]string{
		"en":      "locales/en/voice/intro.wav",
		"ja":      "locales/ja/voice/intro.wav",
		"zh-Hans": "locales/zh-Hans/voice/intro.wav",
	})
	cat.RegisterMap("voice/outro.wav", map[string]string{
		"en": "locales/en/voice/outro.wav",
		"ja": "locales/ja/voice/outro.wav",
	})
	cat.Register("ui/title_banner.png", "en", "ui/title_banner.png")
	cat.RegisterGroup("voice", "voice/intro.wav", "voice/outro.wav")
	cat.RegisterGroup("ui", "ui/title_banner.png")

	// CheckMissing() with no arguments audits all registered locales in sorted order.
	all := cat.CheckMissing()
	if len(all) != 3 {
		t.Fatalf("CheckMissing() returned %d reports, want 3: %+v", len(all), all)
	}
	wantOrder := []string{"en", "ja", "zh-Hans"}
	for i, wantLoc := range wantOrder {
		if all[i].Locale != wantLoc {
			t.Errorf("all[%d].Locale = %q, want %q", i, all[i].Locale, wantLoc)
		}
	}

	// Explicit locales with unsorted / duplicate / unnormalized input.
	reports := cat.CheckMissing("fr", "zh_hans", "JA", "en", "ja")
	if len(reports) != 4 {
		t.Fatalf("CheckMissing explicit returned %d reports, want 4: %+v", len(reports), reports)
	}

	expect := []struct {
		locale   string
		total    int
		present  int
		missing  []string
		coverage float64
		complete bool
	}{
		{"en", 3, 3, nil, 1.0, true},
		{"fr", 3, 0, []string{"ui/title_banner.png", "voice/intro.wav", "voice/outro.wav"}, 0.0, false},
		{"ja", 3, 2, []string{"ui/title_banner.png"}, 2.0 / 3.0, false},
		{"zh-Hans", 3, 1, []string{"ui/title_banner.png", "voice/outro.wav"}, 1.0 / 3.0, false},
	}

	for i, tc := range expect {
		got := reports[i]
		if got.Locale != tc.locale {
			t.Errorf("reports[%d].Locale = %q, want %q", i, got.Locale, tc.locale)
		}
		if got.TotalAssets != tc.total || got.Present != tc.present {
			t.Errorf("reports[%d] (%s) counts = %d/%d, want %d/%d", i, tc.locale, got.Present, got.TotalAssets, tc.present, tc.total)
		}
		if !slices.Equal(got.Missing, tc.missing) {
			t.Errorf("reports[%d] (%s) Missing = %v, want %v", i, tc.locale, got.Missing, tc.missing)
		}
		if math.Abs(got.Coverage-tc.coverage) > 1e-9 {
			t.Errorf("reports[%d] (%s) Coverage = %v, want %v", i, tc.locale, got.Coverage, tc.coverage)
		}
		if got.Complete() != tc.complete {
			t.Errorf("reports[%d] (%s) Complete() = %v, want %v", i, tc.locale, got.Complete(), tc.complete)
		}
	}

	// Group-scoped audit: "ja" has 100% coverage for "voice" group!
	voiceReports := cat.CheckMissingGroup("voice", "ja", "zh-Hans")
	if len(voiceReports) != 2 {
		t.Fatalf("CheckMissingGroup(voice) len = %d, want 2", len(voiceReports))
	}
	if voiceReports[0].Locale != "ja" || !voiceReports[0].Complete() || voiceReports[0].Coverage != 1.0 {
		t.Errorf("voiceReports[0] = %+v, want ja 100%% complete", voiceReports[0])
	}
	if voiceReports[1].Locale != "zh-Hans" || !slices.Equal(voiceReports[1].Missing, []string{"voice/outro.wav"}) || voiceReports[1].Coverage != 0.5 {
		t.Errorf("voiceReports[1] = %+v, want zh-Hans missing [voice/outro.wav] 50%%", voiceReports[1])
	}
}
