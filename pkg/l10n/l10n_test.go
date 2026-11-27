package l10n_test

import (
	"fmt"
	"testing"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type customStringer string

func (c customStringer) String() string { return "<" + string(c) + ">" }

func TestCatalogLoadJSONFlatNestedAndPlurals(t *testing.T) {
	cat := l10n.NewCatalog("en")

	enBase := []byte(`{
		"title": "Dungeon Quest",
		"hud.score": "Score: {score}",
		"menu": {
			"play": "Play",
			"settings": {
				"audio": "Audio",
				"language": "Language"
			}
		},
		"coins": {
			"zero": "No coins",
			"one": "{count} coin",
			"other": "{count} coins"
		},
		"inventory": {
			"items": {
				"one": "{count} item in {bag}",
				"other": "{count} items in {bag}"
			}
		}
	}`)
	if err := cat.LoadJSON("en", enBase); err != nil {
		t.Fatalf("LoadJSON en: %v", err)
	}

	// Merging a second JSON file into the same locale preserves old keys and updates colliding ones.
	enExtra := []byte(`{
		"menu": {
			"quit": "Quit"
		},
		"title": "Dungeon Quest DX"
	}`)
	if err := cat.LoadJSON("en", enExtra); err != nil {
		t.Fatalf("LoadJSON en extra: %v", err)
	}

	checks := []struct {
		got, want string
	}{
		{cat.T("en", "title"), "Dungeon Quest DX"},
		{cat.T("en", "hud.score", l10n.Param("score", 42)), "Score: 42"},
		{cat.T("en", "menu.play"), "Play"},
		{cat.T("en", "menu.quit"), "Quit"},
		{cat.T("en", "menu.settings.audio"), "Audio"},
		{cat.T("en", "menu.settings.language"), "Language"},
		{cat.Plural("en", "coins", 0), "No coins"},
		{cat.Plural("en", "coins", 1), "1 coin"},
		{cat.Plural("en", "coins", 7), "7 coins"},
		{cat.Plural("en", "inventory.items", 1, l10n.Param("bag", "satchel")), "1 item in satchel"},
		{cat.Plural("en", "inventory.items", 4, l10n.Param("bag", "satchel")), "4 items in satchel"},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}

	// Error cases.
	if err := cat.LoadJSON("", []byte(`{"a":"b"}`)); err == nil {
		t.Error("expected error for empty locale")
	}
	if err := cat.LoadJSON("en", []byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if err := cat.LoadJSON("en", []byte(`{"bad": 123}`)); err == nil {
		t.Error("expected error for non-string leaf value")
	}
	if err := cat.LoadJSON("en", []byte(`null`)); err == nil {
		t.Error("expected error for null JSON root")
	}
}

func TestCatalogFallbackChain(t *testing.T) {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "greeting", "Hello")
	cat.Set("en", "only_en", "English Only")
	cat.Set("zh", "greeting", "你好 (zh)")
	cat.Set("zh", "only_zh", "中文")
	cat.Set("zh-Hans", "greeting", "你好 (zh-Hans)")
	cat.Set("zh-Hans", "only_hans", "简体")
	cat.Set("zh-Hans-CN", "greeting", "你好 (zh-Hans-CN)")

	tests := []struct {
		locale, key, want string
	}{
		{"zh-Hans-CN", "greeting", "你好 (zh-Hans-CN)"},
		{"zh_hans_cn", "greeting", "你好 (zh-Hans-CN)"},
		{"zh-Hans-CN", "only_hans", "简体"},
		{"zh-Hans-CN", "only_zh", "中文"},
		{"zh-Hans-CN", "only_en", "English Only"},
		{"zh-Hans-SG", "greeting", "你好 (zh-Hans)"},
		{"zh-Hant-TW", "greeting", "你好 (zh)"},
		{"fr-FR", "greeting", "Hello"},
		{"", "greeting", "Hello"},
		{"zh-Hans-CN", "missing.key", "missing.key"},
	}
	for _, tc := range tests {
		if got := cat.T(tc.locale, tc.key); got != tc.want {
			t.Errorf("T(%q, %q) = %q, want %q", tc.locale, tc.key, got, tc.want)
		}
	}

	if !cat.Has("zh-Hans-CN", "only_en") {
		t.Error("Has(zh-Hans-CN, only_en) = false, want true via fallback")
	}
	if cat.Has("zh-Hans-CN", "missing.key") {
		t.Error("Has(zh-Hans-CN, missing.key) = true, want false")
	}

	locales := cat.Locales()
	wantLocales := []string{"en", "zh", "zh-Hans", "zh-Hans-CN"}
	if fmt.Sprint(locales) != fmt.Sprint(wantLocales) {
		t.Errorf("Locales() = %v, want %v", locales, wantLocales)
	}
}

func TestCatalogInterpolation(t *testing.T) {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "hud", "Score: {score}, {player}! {{literal}} {missing}")
	cat.Set("en", "types", "{str} {i} {b} {fmt}")

	got := cat.T("en", "hud", l10n.Param("score", 42), l10n.Param("player", "Ada"))
	want := "Score: 42, Ada! {literal} {missing}"
	if got != want {
		t.Errorf("T(hud) = %q, want %q", got, want)
	}

	got = cat.T("en", "types",
		l10n.Param("str", "ok"),
		l10n.Param("i", int64(99)),
		l10n.Param("b", true),
		l10n.Param("fmt", customStringer("hero")),
	)
	want = "ok 99 true <hero>"
	if got != want {
		t.Errorf("T(types) = %q, want %q", got, want)
	}

	// Later param overrides earlier param of the same name.
	got = cat.T("en", "hud",
		l10n.Param("score", 1),
		l10n.Param("score", 999),
		l10n.Param("player", "Lin"),
	)
	want = "Score: 999, Lin! {literal} {missing}"
	if got != want {
		t.Errorf("T(hud override) = %q, want %q", got, want)
	}
}

func TestCatalogPlurals(t *testing.T) {
	cat := l10n.NewCatalog("en")
	cat.SetPlural("en", "lives", l10n.PluralForms{
		Zero:  "No lives left",
		One:   "{count} life",
		Other: "{count} lives",
	})
	cat.SetPlural("ar", "gems", l10n.PluralForms{
		Zero:  "لا جواهر",
		One:   "جوهرة واحدة",
		Two:   "جوهرتان",
		Few:   "{count} جواهر",
		Many:  "{count} جوهرة",
		Other: "{count} جوهرة (other)",
	})
	cat.SetPlural("ru", "keys", l10n.PluralForms{
		One:   "{count} ключ",
		Few:   "{count} ключа",
		Many:  "{count} ключей",
		Other: "{count} ключей",
	})
	cat.SetPlural("ja", "coins", l10n.PluralForms{
		Other: "コイン {count}枚",
	})
	cat.SetPlural("fr", "items", l10n.PluralForms{
		One:   "{count} objet",
		Other: "{count} objets",
	})
	cat.SetPlural("he", "stars", l10n.PluralForms{
		One:   "כוכב אחד",
		Two:   "שני כוכבים",
		Other: "{count} כוכבים",
	})

	tests := []struct {
		locale, key string
		count       int
		want        string
	}{
		{"en", "lives", 0, "No lives left"},
		{"en", "lives", 1, "1 life"},
		{"en", "lives", 3, "3 lives"},
		{"en", "lives", -1, "-1 life"},
		{"ar", "gems", 0, "لا جواهر"},
		{"ar", "gems", 1, "جوهرة واحدة"},
		{"ar", "gems", 2, "جوهرتان"},
		{"ar", "gems", 5, "5 جواهر"},
		{"ar", "gems", 25, "25 جوهرة"},
		{"ar", "gems", 100, "100 جوهرة (other)"},
		{"ru", "keys", 1, "1 ключ"},
		{"ru", "keys", 2, "2 ключа"},
		{"ru", "keys", 5, "5 ключей"},
		{"ru", "keys", 11, "11 ключей"},
		{"ru", "keys", 21, "21 ключ"},
		{"ru", "keys", 24, "24 ключа"},
		{"ja", "coins", 1, "コイン 1枚"},
		{"ja", "coins", 5, "コイン 5枚"},
		{"fr", "items", 0, "0 objet"},
		{"fr", "items", 1, "1 objet"},
		{"fr", "items", 2, "2 objets"},
		{"he", "stars", 1, "כוכב אחד"},
		{"he", "stars", 2, "שני כוכבים"},
		{"he", "stars", 5, "5 כוכבים"},
		{"en", "missing", 3, "missing"},
	}
	for _, tc := range tests {
		if got := cat.Plural(tc.locale, tc.key, tc.count); got != tc.want {
			t.Errorf("Plural(%q, %q, %d) = %q, want %q", tc.locale, tc.key, tc.count, got, tc.want)
		}
	}
}

func TestLocalizerReactiveEnvironmentAndOverride(t *testing.T) {
	cat := l10n.NewCatalog("en")
	if err := cat.LoadJSON("en", []byte(`{
		"play": "Play",
		"score": "Score: {n}",
		"coins": {"one": "{count} coin", "other": "{count} coins"}
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := cat.LoadJSON("zh-Hans", []byte(`{
		"play": "开始游戏",
		"score": "得分：{n}",
		"coins": {"other": "{count} 枚金币"}
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := cat.LoadJSON("ar", []byte(`{
		"play": "العب",
		"score": "النتيجة: {n}"
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := cat.LoadJSON("ja", []byte(`{
		"play": "プレイ"
	}`)); err != nil {
		t.Fatal(err)
	}

	var (
		loc       *l10n.Localizer
		seenPlay  []string
		seenScore string
		seenCoins string
		seenDir   l10n.Direction
		seenEnvD  l10n.Direction
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loc = l10n.NewLocalizer(cx, cat)
		cx.Effect(func() {
			seenPlay = append(seenPlay, loc.T("play"))
			seenScore = loc.T("score", l10n.Param("n", 100))
			seenCoins = loc.Plural("coins", 3)
			seenDir = loc.Direction()
			seenEnvD = l10n.EnvDirection(cx)
		})
		return core.View{}
	})

	// Initial state uses fallback locale ("en") since test env starts empty.
	if got := seenPlay[len(seenPlay)-1]; got != "Play" {
		t.Fatalf("initial play = %q, want Play", got)
	}
	if seenDir != l10n.DirLTR || seenEnvD != l10n.DirLTR {
		t.Fatalf("initial dir = (%v, %v), want LTR", seenDir, seenEnvD)
	}

	// Environment switches to Simplified Chinese (China) -> falls back to zh-Hans.
	app.SetEnvironment(core.Environment{Locale: "zh-Hans-CN", RTL: false})
	if got := seenPlay[len(seenPlay)-1]; got != "开始游戏" {
		t.Fatalf("zh-Hans-CN play = %q, want 开始游戏", got)
	}
	if seenScore != "得分：100" {
		t.Fatalf("zh-Hans-CN score = %q, want 得分：100", seenScore)
	}
	if seenCoins != "3 枚金币" {
		t.Fatalf("zh-Hans-CN coins = %q, want 3 枚金币", seenCoins)
	}
	if seenDir != l10n.DirLTR {
		t.Fatalf("zh-Hans-CN dir = %v, want LTR", seenDir)
	}

	// Environment switches to Arabic with RTL: true.
	app.SetEnvironment(core.Environment{Locale: "ar-SA", RTL: true})
	if got := seenPlay[len(seenPlay)-1]; got != "العب" {
		t.Fatalf("ar-SA play = %q, want العب", got)
	}
	if seenDir != l10n.DirRTL || seenEnvD != l10n.DirRTL {
		t.Fatalf("ar-SA dir = (%v, %v), want RTL", seenDir, seenEnvD)
	}
	// Missing plural in ar falls back to en.
	if seenCoins != "3 coins" {
		t.Fatalf("ar-SA coins fallback = %q, want 3 coins", seenCoins)
	}

	// Manual override switches to Japanese (LTR) despite system env being Arabic RTL.
	app.Post(func() { loc.SetLocale("ja") })
	app.Settle()
	if got := seenPlay[len(seenPlay)-1]; got != "プレイ" {
		t.Fatalf("override ja play = %q, want プレイ", got)
	}
	if seenDir != l10n.DirLTR {
		t.Fatalf("override ja dir = %v, want LTR", seenDir)
	}

	// Clearing manual override resumes following the environment (ar-SA / RTL).
	app.Post(func() { loc.SetLocale("") })
	app.Settle()
	if got := seenPlay[len(seenPlay)-1]; got != "العب" {
		t.Fatalf("cleared override play = %q, want العب", got)
	}
	if seenDir != l10n.DirRTL {
		t.Fatalf("cleared override dir = %v, want RTL", seenDir)
	}

	// Switching environment back to English LTR updates reactively.
	app.SetEnvironment(core.Environment{Locale: "en-US", RTL: false})
	if got := seenPlay[len(seenPlay)-1]; got != "Play" {
		t.Fatalf("en-US play = %q, want Play", got)
	}
	if seenDir != l10n.DirLTR || seenEnvD != l10n.DirLTR {
		t.Fatalf("en-US dir = (%v, %v), want LTR", seenDir, seenEnvD)
	}
}

func TestDetectScript(t *testing.T) {
	tests := []struct {
		text string
		want l10n.Script
	}{
		{"Hello World!", l10n.ScriptLatin},
		{"Score: 100", l10n.ScriptLatin},
		{"得分 100", l10n.ScriptCJK},
		{"スコア 100", l10n.ScriptJapanese},
		{"日本語テスト", l10n.ScriptJapanese},
		{"안녕하세요", l10n.ScriptKorean},
		{"مرحبا بالعالم", l10n.ScriptArabic},
		{"שלום עולם", l10n.ScriptHebrew},
		{"Привет мир", l10n.ScriptCyrillic},
		{"Γειά σου", l10n.ScriptGreek},
		{"สวัสดี", l10n.ScriptThai},
		{"नमस्ते", l10n.ScriptDevanagari},
		{"12345 !@#", l10n.ScriptCommon},
		{"", l10n.ScriptCommon},
	}
	for _, tc := range tests {
		if got := l10n.DetectScript(tc.text); got != tc.want {
			t.Errorf("DetectScript(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestSegmentByScript(t *testing.T) {
	if runs := l10n.SegmentByScript(""); runs != nil {
		t.Fatalf("SegmentByScript(\"\") = %v, want nil", runs)
	}

	pureCommon := l10n.SegmentByScript("12345")
	if len(pureCommon) != 1 || pureCommon[0].Script != l10n.ScriptCommon || pureCommon[0].Text != "12345" {
		t.Fatalf("SegmentByScript(\"12345\") = %+v, want single Common run", pureCommon)
	}

	// Mixed Latin + CJK.
	s := "Score: 100 得分"
	runs := l10n.SegmentByScript(s)
	if len(runs) != 2 {
		t.Fatalf("SegmentByScript(%q) returned %d runs: %+v, want 2", s, len(runs), runs)
	}
	if runs[0].Text != "Score: 100 " || runs[0].Script != l10n.ScriptLatin {
		t.Errorf("run[0] = %+v, want Latin \"Score: 100 \"", runs[0])
	}
	if runs[1].Text != "得分" || runs[1].Script != l10n.ScriptCJK {
		t.Errorf("run[1] = %+v, want CJK \"得分\"", runs[1])
	}
	for _, r := range runs {
		if s[r.Start:r.End] != r.Text {
			t.Errorf("slice s[%d:%d] = %q, want %q", r.Start, r.End, s[r.Start:r.End], r.Text)
		}
	}

	// Leading numbers attach to the first strong script.
	lead := l10n.SegmentByScript("100 得分")
	if len(lead) != 1 || lead[0].Script != l10n.ScriptCJK || lead[0].Text != "100 得分" {
		t.Fatalf("SegmentByScript(\"100 得分\") = %+v, want single CJK run", lead)
	}

	// Multi-script: Latin, Arabic, Hebrew, Cyrillic.
	multi := l10n.SegmentByScript("HP: مرحبا שלום Привет")
	if len(multi) != 4 {
		t.Fatalf("multi runs = %+v, want 4", multi)
	}
	wantScripts := []l10n.Script{l10n.ScriptLatin, l10n.ScriptArabic, l10n.ScriptHebrew, l10n.ScriptCyrillic}
	for i, w := range wantScripts {
		if multi[i].Script != w {
			t.Errorf("multi[%d].Script = %v, want %v (text %q)", i, multi[i].Script, w, multi[i].Text)
		}
	}
}

func TestFontSetCoreFontAndBitmapFont(t *testing.T) {
	// 1. Platform core.Font (value struct).
	latinFont := core.Font{Family: "Inter", Size: 16}
	cjkFont := core.Font{Family: "Noto Sans CJK", Size: 16}
	scFont := core.Font{Family: "Noto Sans SC", Language: "zh-Hans", Size: 16}
	tcFont := core.Font{Family: "Noto Sans TC", Language: "zh-Hant", Size: 16}
	jpFont := core.Font{Family: "Noto Sans JP", Language: "ja", Size: 16}
	arFont := core.Font{Family: "Noto Sans Arabic", Size: 16}
	heFont := core.Font{Family: "Noto Sans Hebrew", Size: 16}
	cyFont := core.Font{Family: "Noto Sans Cyrillic", Size: 16}

	fs := l10n.NewFontSet(latinFont)
	fs.SetScript(l10n.ScriptCJK, cjkFont)
	fs.SetScript(l10n.ScriptArabic, arFont)
	fs.SetScript(l10n.ScriptHebrew, heFont)
	fs.SetScript(l10n.ScriptCyrillic, cyFont)
	fs.SetLocale("zh-Hans", scFont)
	fs.SetLocale("zh-Hant", tcFont)
	fs.SetLocale("ja", jpFont)

	if got := fs.ForLocale("en-US"); got != latinFont {
		t.Errorf("ForLocale(en-US) = %+v, want %+v", got, latinFont)
	}
	if got := fs.ForLocale("zh-Hans-CN"); got != scFont {
		t.Errorf("ForLocale(zh-Hans-CN) = %+v, want %+v", got, scFont)
	}
	if got := fs.ForLocale("zh-Hant-TW"); got != tcFont {
		t.Errorf("ForLocale(zh-Hant-TW) = %+v, want %+v", got, tcFont)
	}
	if got := fs.ForLocale("ja-JP"); got != jpFont {
		t.Errorf("ForLocale(ja-JP) = %+v, want %+v", got, jpFont)
	}
	if got := fs.ForLocale("ar-SA"); got != arFont {
		t.Errorf("ForLocale(ar-SA) = %+v, want %+v", got, arFont)
	}
	if got := fs.ForLocale("he-IL"); got != heFont {
		t.Errorf("ForLocale(he-IL) = %+v, want %+v", got, heFont)
	}
	if got := fs.ForLocale("ru-RU"); got != cyFont {
		t.Errorf("ForLocale(ru-RU) = %+v, want %+v", got, cyFont)
	}

	// ForText respects locale-specific Han overrides vs generic CJK fallback.
	if got := fs.ForText("zh-Hans", "你好"); got != scFont {
		t.Errorf("ForText(zh-Hans, 你好) = %+v, want %+v", got, scFont)
	}
	if got := fs.ForText("zh-Hant", "你好"); got != tcFont {
		t.Errorf("ForText(zh-Hant, 你好) = %+v, want %+v", got, tcFont)
	}
	if got := fs.ForText("ja", "日本語"); got != jpFont {
		t.Errorf("ForText(ja, 日本語) = %+v, want %+v", got, jpFont)
	}
	if got := fs.ForText("en", "得分"); got != cjkFont {
		t.Errorf("ForText(en, 得分) = %+v, want %+v", got, cjkFont)
	}
	if got := fs.ForText("en", "مرحبا"); got != arFont {
		t.Errorf("ForText(en, مرحبا) = %+v, want %+v", got, arFont)
	}
	if got := fs.ForText("en", "שלום"); got != heFont {
		t.Errorf("ForText(en, שלום) = %+v, want %+v", got, heFont)
	}
	if got := fs.ForText("en", "Привет"); got != cyFont {
		t.Errorf("ForText(en, Привет) = %+v, want %+v", got, cyFont)
	}
	// Neutral digits inherit the locale's font.
	if got := fs.ForText("ar-SA", "100"); got != arFont {
		t.Errorf("ForText(ar-SA, 100) = %+v, want %+v", got, arFont)
	}

	// RunsForText on mixed-script text.
	runs := fs.RunsForText("zh-Hans", "Score: 100 得分")
	if len(runs) != 2 {
		t.Fatalf("RunsForText len = %d, want 2", len(runs))
	}
	if runs[0].Font != latinFont || runs[0].Text != "Score: 100 " {
		t.Errorf("runs[0] = %+v, want latinFont", runs[0])
	}
	if runs[1].Font != scFont || runs[1].Text != "得分" {
		t.Errorf("runs[1] = %+v, want scFont", runs[1])
	}

	// 2. Bitmap / pointer font type (*bitmapFont).
	type bitmapFont struct{ name string }
	bmpDefault := &bitmapFont{name: "pixel-latin"}
	bmpCJK := &bitmapFont{name: "pixel-cjk"}
	bmpArabic := &bitmapFont{name: "pixel-ar"}

	bfs := l10n.NewFontSet(bmpDefault)
	bfs.SetScript(l10n.ScriptCJK, bmpCJK)
	bfs.SetScript(l10n.ScriptArabic, bmpArabic)

	bmpRuns := bfs.RunsForText("en", "HP 50 生命 مرحبا")
	if len(bmpRuns) != 3 {
		t.Fatalf("bmpRuns len = %d, want 3", len(bmpRuns))
	}
	if bmpRuns[0].Font != bmpDefault || bmpRuns[1].Font != bmpCJK || bmpRuns[2].Font != bmpArabic {
		t.Errorf("bmpRuns fonts = [%s, %s, %s], want [pixel-latin, pixel-cjk, pixel-ar]",
			bmpRuns[0].Font.name, bmpRuns[1].Font.name, bmpRuns[2].Font.name)
	}
}

func TestLocaleDirectionAndTextDirection(t *testing.T) {
	rtlLocales := []string{"ar", "ar-SA", "he", "he-IL", "iw", "fa", "fa-IR", "ur", "ur-PK", "yi", "ps", "sd", "ug", "dv", "syr", "ckb", "az-Arab"}
	for _, loc := range rtlLocales {
		if got := l10n.LocaleDirection(loc); got != l10n.DirRTL {
			t.Errorf("LocaleDirection(%q) = %v, want DirRTL", loc, got)
		}
	}

	ltrLocales := []string{"en", "en-US", "zh-Hans", "ja", "ko", "ru", "el", "th", "hi", "ar-Latn", "az-Latn", ""}
	for _, loc := range ltrLocales {
		if got := l10n.LocaleDirection(loc); got != l10n.DirLTR {
			t.Errorf("LocaleDirection(%q) = %v, want DirLTR", loc, got)
		}
	}

	textTests := []struct {
		text string
		want l10n.Direction
	}{
		{"Hello world", l10n.DirLTR},
		{"123 Score", l10n.DirLTR},
		{"שלום עולם", l10n.DirRTL},
		{"100 مرحبا", l10n.DirRTL},
		{"(النتيجة)", l10n.DirRTL},
		{"Score: שלום", l10n.DirLTR},
		{"12345", l10n.DirLTR},
		{"", l10n.DirLTR},
	}
	for _, tc := range textTests {
		if got := l10n.TextDirection(tc.text); got != tc.want {
			t.Errorf("TextDirection(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestVisualOrder(t *testing.T) {
	tests := []struct {
		name string
		in   string
		dir  l10n.Direction
		want string
	}{
		{
			name: "pure LTR unchanged",
			in:   "Score: 100 (High)",
			dir:  l10n.DirLTR,
			want: "Score: 100 (High)",
		},
		{
			name: "pure Hebrew reversed",
			in:   "שלום עולם",
			dir:  l10n.DirRTL,
			want: "םלוע םולש",
		},
		{
			name: "pure Arabic reversed",
			in:   "مرحبا",
			dir:  l10n.DirRTL,
			want: "ابحرم",
		},
		{
			name: "Arabic with embedded number stays LTR",
			in:   "النتيجة: 100",
			dir:  l10n.DirRTL,
			want: "100 :ةجيتنلا",
		},
		{
			name: "Hebrew with embedded number",
			in:   "שלום 123 עולם",
			dir:  l10n.DirRTL,
			want: "םלוע 123 םולש",
		},
		{
			name: "Hebrew with embedded multi-word English",
			in:   "שלום Hello World עולם",
			dir:  l10n.DirRTL,
			want: "םלוע Hello World םולש",
		},
		{
			name: "mirrored parentheses around RTL",
			in:   "שלום (עולם)",
			dir:  l10n.DirRTL,
			want: "(םלוע) םולש",
		},
		{
			name: "mirrored parentheses around number in RTL",
			in:   "שלום (100)",
			dir:  l10n.DirRTL,
			want: "(100) םולש",
		},
		{
			name: "mirrored bracket pairs [], {}, <> in RTL",
			in:   "שלום [100] {עולם} <HP>",
			dir:  l10n.DirRTL,
			want: "<HP> {םלוע} [100] םולש",
		},
		{
			name: "embedded English with its own parentheses stays LTR",
			in:   "שלום Hello (World) עולם",
			dir:  l10n.DirRTL,
			want: "םלוע Hello (World) םולש",
		},
		{
			name: "LTR paragraph with embedded RTL",
			in:   "Name: שלום!",
			dir:  l10n.DirLTR,
			want: "Name: םולש!",
		},
		{
			name: "trailing punctuation in RTL",
			in:   "שלום!",
			dir:  l10n.DirRTL,
			want: "!םולש",
		},
		{
			name: "multi-line reorders each line independently",
			in:   "שלום\nעולם 42",
			dir:  l10n.DirRTL,
			want: "םולש\n42 םלוע",
		},
		{
			name: "combining mark stays attached to base rune in RTL",
			in:   "ש\u05B8לום", // shin + qamats + lamed + vav + final mem
			dir:  l10n.DirRTL,
			want: "םולש\u05B8",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := l10n.VisualOrder(tc.in, tc.dir); got != tc.want {
				t.Errorf("VisualOrder(%q, %v) = %q (%U), want %q (%U)",
					tc.in, tc.dir, got, []rune(got), tc.want, []rune(tc.want))
			}
		})
	}
}

func TestLayoutAlignmentAndMirrorX(t *testing.T) {
	alignTests := []struct {
		align string
		dir   l10n.Direction
		want  string
	}{
		{"left", l10n.DirLTR, "left"},
		{"left", l10n.DirRTL, "right"},
		{"right", l10n.DirLTR, "right"},
		{"right", l10n.DirRTL, "left"},
		{"start", l10n.DirLTR, "left"},
		{"start", l10n.DirRTL, "right"},
		{"end", l10n.DirLTR, "right"},
		{"end", l10n.DirRTL, "left"},
		{"center", l10n.DirLTR, "center"},
		{"center", l10n.DirRTL, "center"},
		{"justify", l10n.DirRTL, "justify"},
	}
	for _, tc := range alignTests {
		if got := l10n.LayoutAlignment(tc.align, tc.dir); got != tc.want {
			t.Errorf("LayoutAlignment(%q, %v) = %q, want %q", tc.align, tc.dir, got, tc.want)
		}
	}

	// HUD bar: x=20, width=200 inside 800px screen.
	if got := l10n.MirrorX(20, 200, 800, l10n.DirLTR); got != 20 {
		t.Errorf("MirrorX LTR = %v, want 20", got)
	}
	mirrored := l10n.MirrorX(20, 200, 800, l10n.DirRTL)
	if mirrored != 580 {
		t.Errorf("MirrorX RTL = %v, want 580", mirrored)
	}
	// Involution: mirroring again restores x.
	if back := l10n.MirrorX(mirrored, 200, 800, l10n.DirRTL); back != 20 {
		t.Errorf("MirrorX RTL twice = %v, want 20", back)
	}
}
