package a11y_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func extractPlaceholders(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if s[i] == '{' {
			if i+1 < len(s) && s[i+1] == '{' {
				i += 2
				continue
			}
			end := strings.IndexByte(s[i+1:], '}')
			if end < 0 {
				break
			}
			name := s[i+1 : i+1+end]
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
			i += end + 2
			continue
		}
		i++
	}
	slices.Sort(out)
	return out
}

func containsASCIIWord(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func TestLocaleCatalogsComplete(t *testing.T) {
	files := []struct {
		path   string
		locale string
	}{
		{filepath.Join("locales", "en.yaml"), "en"},
		{filepath.Join("locales", "zh.yaml"), "zh"},
	}

	tables := make(map[string]*l10n.StringTable, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("read %s: %v", f.path, err)
		}
		st, err := l10n.ParseYAML(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", f.path, err)
		}
		if st.Locale != f.locale {
			t.Fatalf("%s locale = %q, want %q", f.path, st.Locale, f.locale)
		}
		canon, err := l10n.EncodeYAML(st)
		if err != nil {
			t.Fatalf("encode %s: %v", f.path, err)
		}
		if !bytes.Equal(canon, raw) {
			t.Fatalf("%s is not in canonical l10n.EncodeYAML form:\n--- got ---\n%s\n--- want ---\n%s", f.path, string(raw), string(canon))
		}
		tables[f.locale] = st
	}

	en := tables["en"]
	zh := tables["zh"]

	expectedStringKeys := []string{
		a11y.KeyMenuDefaultLabel,
		a11y.KeyMenuOpened,
		a11y.KeyMenuOpenedWithItem,
		a11y.KeyMenuPosition,
		a11y.KeyMenuSelected,
		a11y.KeyMenuClosed,
		a11y.KeyMenuChecked,
		a11y.KeyMenuUnchecked,
		a11y.KeyMenuOn,
		a11y.KeyMenuOff,
		a11y.KeyMenuSeparator,
		a11y.KeyCaptionsRegionLabel,
		a11y.KeyCaptionsImportantPrefix,
		a11y.KeyCaptionsMusicPrefix,
		a11y.KeyCaptionsBgBlack,
		a11y.KeyCaptionsBgGray,
		a11y.KeyCaptionsBgBlue,
		a11y.KeyCaptionsBgWhite,
		a11y.KeyCaptionsTextWhite,
		a11y.KeyCaptionsTextYellow,
		a11y.KeyCaptionsTextCyan,
		a11y.KeyCaptionsTextGreen,
		a11y.KeyCaptionsTextBlack,
		a11y.KeyCaptionsSizeSmall,
		a11y.KeyCaptionsSizeMedium,
		a11y.KeyCaptionsSizeLarge,
		a11y.KeyCaptionsSizeXLarge,
		a11y.KeyCaptionsPreviewSpeaker,
		a11y.KeyCaptionsPreviewQualifier,
		a11y.KeyCaptionsPreviewDialogue,
		a11y.KeyCaptionsPreviewChatterSpeaker,
		a11y.KeyCaptionsPreviewChatter,
		a11y.KeyCaptionsPreviewSFX,
	}

	for _, key := range expectedStringKeys {
		enVal, ok := en.Strings[key]
		if !ok {
			t.Errorf("en.yaml missing string key %q", key)
		}
		zhVal, ok := zh.Strings[key]
		if !ok {
			t.Errorf("zh.yaml missing string key %q", key)
		}
		if !slices.Equal(extractPlaceholders(enVal), extractPlaceholders(zhVal)) {
			t.Errorf("placeholder mismatch for %q: en=%v zh=%v", key, extractPlaceholders(enVal), extractPlaceholders(zhVal))
		}
	}

	if len(en.Strings) != len(zh.Strings) {
		t.Fatalf("string key count mismatch: en=%d zh=%d", len(en.Strings), len(zh.Strings))
	}
	if _, ok := en.Plurals[a11y.KeyMenuItems]; !ok {
		t.Fatalf("en.yaml missing plural key %q", a11y.KeyMenuItems)
	}
	if _, ok := zh.Plurals[a11y.KeyMenuItems]; !ok {
		t.Fatalf("zh.yaml missing plural key %q", a11y.KeyMenuItems)
	}

	// DefaultCatalog("en") must match DefaultMenuText and default CaptionStyle.
	cat := a11y.DefaultCatalog()
	enMenu := a11y.MenuTextFromCatalog(cat, "en")
	defMenu := a11y.DefaultMenuText()
	if enMenu.DefaultLabel != defMenu.DefaultLabel ||
		enMenu.Opened != defMenu.Opened ||
		enMenu.OpenedWithItem != defMenu.OpenedWithItem ||
		enMenu.Checked != defMenu.Checked ||
		enMenu.Unchecked != defMenu.Unchecked ||
		enMenu.Position != defMenu.Position ||
		enMenu.Selected != defMenu.Selected ||
		enMenu.Closed != defMenu.Closed ||
		enMenu.On != defMenu.On ||
		enMenu.Off != defMenu.Off ||
		enMenu.Separator != defMenu.Separator {
		t.Fatalf("DefaultCatalog(en) MenuText = %+v, want %+v", enMenu, defMenu)
	}

	enCap := a11y.CaptionStyleFromCatalog(cat, "en")
	if enCap.RegionLabel != "Captions" || enCap.ImportantPrefix != "! " || enCap.MusicPrefix != "♪ " {
		t.Fatalf("DefaultCatalog(en) CaptionStyle = %+v", enCap)
	}
}

func TestAccessibleMenuDefaultEnglishUnchangedAndPlurals(t *testing.T) {
	// 1-item menu uses English singular "1 item" and default "Menu" label when Label is empty.
	app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			TestID: "single-menu",
			Items: []a11y.MenuItem{
				{ID: "play", Label: "Play"},
			},
		})
	}, nsgotest.Environment(desktopEnv))

	if got := app1.Find("id:single-menu").Props.Label; got != "Menu" {
		t.Fatalf("default menu label = %q, want Menu", got)
	}
	if len(app1.Announced) != 1 || app1.Announced[0] != "Menu opened, 1 item. Play" {
		t.Fatalf("single-item open announcement = %v, want [Menu opened, 1 item. Play]", app1.Announced)
	}

	// 0-item menu uses plural "0 items" and Opened template (no trailing item).
	app0 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			Label: "Empty Menu",
		})
	}, nsgotest.Environment(desktopEnv))
	if len(app0.Announced) != 1 || app0.Announced[0] != "Empty Menu opened, 0 items" {
		t.Fatalf("empty open announcement = %v, want [Empty Menu opened, 0 items]", app0.Announced)
	}

	// Multi-item menu: default English, Checked focus omits duplicate "checked"/"unchecked"
	// when RoleMenuItemCheckbox reports Checked natively, while explicit RoleMenuItem still speaks it.
	var checkedA, checkedB *core.Signal[bool]
	var closed int
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		checkedA = core.NewSignal(cx, false)
		checkedB = core.NewSignal(cx, true)
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			TestID:  "eng-menu",
			Label:   "Pause Menu",
			OnClose: func() { closed++ },
			Items: []a11y.MenuItem{
				{ID: "resume", Label: "Resume"},
				{
					ID:       "toggle-native",
					Label:    "Reduce Motion",
					Checked:  checkedA.Get,
					OnSelect: func() { checkedA.Set(!checkedA.Get()) },
				},
				{
					ID:       "toggle-custom-role",
					Label:    "Screen Shake",
					Role:     core.RoleMenuItem,
					Checked:  checkedB.Get,
					OnSelect: func() { checkedB.Set(!checkedB.Get()) },
				},
			},
		})
	}, nsgotest.Environment(desktopEnv))

	if got := app.Announced[0]; got != "Pause Menu opened, 3 items. Resume" {
		t.Fatalf("mount announcement = %q, want %q", got, "Pause Menu opened, 3 items. Resume")
	}
	// Visible toggle values default to "On" / "Off".
	app.Find("text:Off")
	app.Find("text:On")

	app.Focus("id:eng-menu")
	app.Key("return")
	if got := app.Announced[len(app.Announced)-1]; got != "Resume selected" {
		t.Fatalf("action announcement = %q, want %q", got, "Resume selected")
	}

	// Focusing RoleMenuItemCheckbox omits redundant "unchecked" because NSGo reports Checked natively.
	app.Key("down")
	if got := app.Announced[len(app.Announced)-1]; got != "Reduce Motion, 2 of 3" {
		t.Fatalf("focus RoleMenuItemCheckbox announcement = %q, want %q", got, "Reduce Motion, 2 of 3")
	}

	// Toggling via Space speaks the new state since focus does not move.
	app.Key("space")
	if got := app.Announced[len(app.Announced)-1]; got != "Reduce Motion, checked" {
		t.Fatalf("toggle announcement = %q, want %q", got, "Reduce Motion, checked")
	}

	// Focusing an item whose Role does NOT natively report Checked keeps the spoken checked state.
	app.Key("down")
	if got := app.Announced[len(app.Announced)-1]; got != "Screen Shake, checked, 3 of 3" {
		t.Fatalf("focus non-checkable Role announcement = %q, want %q", got, "Screen Shake, checked, 3 of 3")
	}

	app.Key("escape")
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := app.Announced[len(app.Announced)-1]; got != "Pause Menu closed" {
		t.Fatalf("close announcement = %q, want %q", got, "Pause Menu closed")
	}
}

func TestAccessibleMenuChineseLocalization(t *testing.T) {
	cat := a11y.DefaultCatalog()
	var reduceMotion *core.Signal[bool]
	var speedText *core.Signal[string]
	var closed int

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loc := l10n.NewLocalizer(cx, cat)
		loc.SetLocale("zh-Hans")
		reduceMotion = core.NewSignal(cx, false)
		speedText = core.NewSignal(cx, "100%")
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			TestID:  "zh-menu",
			Label:   "辅助选项",
			Text:    a11y.MenuTextFromLocalizer(loc),
			OnClose: func() { closed++ },
			Items: []a11y.MenuItem{
				{
					ID:       "reduce-motion",
					Label:    "减弱动态效果",
					Checked:  reduceMotion.Get,
					OnSelect: func() { reduceMotion.Set(!reduceMotion.Get()) },
				},
				{
					ID:    "game-speed",
					Label: "游戏速度",
					Value: speedText.Get,
					OnSelect: func() {
						if speedText.Get() == "100%" {
							speedText.Set("75%")
						} else {
							speedText.Set("100%")
						}
					},
				},
				{
					ID:    "resume",
					Label: "继续",
				},
			},
		})
	}, nsgotest.Environment(desktopEnv))

	// Mount announcement is fully Chinese with Chinese item count and unchecked state.
	wantMount := "辅助选项已打开，共 3 项。减弱动态效果，未勾选"
	if len(app.Announced) == 0 || app.Announced[0] != wantMount {
		t.Fatalf("zh mount announcement = %v, want [%q]", app.Announced, wantMount)
	}
	// Visible toggle value is Chinese "关" (not "Off").
	app.Find("text:关")
	if app.Exists("text:Off") || app.Exists("text:On") {
		t.Fatal("Chinese menu rendered English On/Off toggle text")
	}

	// Toggle first item: switches visible badge to "开" and speaks Chinese checked state.
	app.Focus("id:zh-menu")
	app.Key("space")
	app.Find("text:开")
	if got := app.Announced[len(app.Announced)-1]; got != "减弱动态效果，已勾选" {
		t.Fatalf("zh toggle announcement = %q, want %q", got, "减弱动态效果，已勾选")
	}

	// Move down to value item and activate it.
	app.Key("down")
	if got := app.Announced[len(app.Announced)-1]; got != "游戏速度，100%，第 2 项，共 3 项" {
		t.Fatalf("zh focus value announcement = %q, want %q", got, "游戏速度，100%，第 2 项，共 3 项")
	}
	app.Key("enter")
	if got := app.Announced[len(app.Announced)-1]; got != "游戏速度，75%" {
		t.Fatalf("zh value activation announcement = %q, want %q", got, "游戏速度，75%")
	}

	// Move down to action item and activate it.
	app.Key("down")
	if got := app.Announced[len(app.Announced)-1]; got != "继续，第 3 项，共 3 项" {
		t.Fatalf("zh focus action announcement = %q, want %q", got, "继续，第 3 项，共 3 项")
	}
	app.Key("return")
	if got := app.Announced[len(app.Announced)-1]; got != "已选择继续" {
		t.Fatalf("zh selected announcement = %q, want %q", got, "已选择继续")
	}

	// Move back up to toggle item: focus relies on platform Checked (no duplicate 已勾选).
	app.Key("home")
	if got := app.Announced[len(app.Announced)-1]; got != "减弱动态效果，第 1 项，共 3 项" {
		t.Fatalf("zh home focus toggle announcement = %q, want %q", got, "减弱动态效果，第 1 项，共 3 项")
	}

	// Close menu.
	app.Key("escape")
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := app.Announced[len(app.Announced)-1]; got != "辅助选项已关闭" {
		t.Fatalf("zh close announcement = %q, want %q", got, "辅助选项已关闭")
	}

	// Every announcement in the entire Chinese session must contain zero ASCII letters.
	for i, ann := range app.Announced {
		if containsASCIIWord(ann) {
			t.Errorf("announcement[%d] = %q contains leaked English ASCII letters", i, ann)
		}
	}

	// Empty label falls back to Chinese default label "菜单"; 0-item menu uses Opened.
	appDefault := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			TestID: "zh-default",
			Text:   a11y.MenuTextFromCatalog(cat, "zh-CN"),
		})
	}, nsgotest.Environment(desktopEnv))
	if got := appDefault.Find("id:zh-default").Props.Label; got != "菜单" {
		t.Fatalf("zh default menu label = %q, want 菜单", got)
	}
	if got := appDefault.Announced[0]; got != "菜单已打开，共 0 项" {
		t.Fatalf("zh empty mount announcement = %q, want %q", got, "菜单已打开，共 0 项")
	}
}

func TestAccessibleMenuCustomCatalogPluralsAndPartialFallback(t *testing.T) {
	// Arabic catalog tests RTL template ordering and CLDR multi-category plurals (0/1/2/3).
	arCat := l10n.NewCatalog("en")
	arCat.Set("ar", a11y.KeyMenuDefaultLabel, "القائمة")
	arCat.Set("ar", a11y.KeyMenuOpened, "{label} مفتوحة، {items}")
	arCat.Set("ar", a11y.KeyMenuOpenedWithItem, "{label} مفتوحة، {items}. {item}")
	arCat.SetPlural("ar", a11y.KeyMenuItems, l10n.PluralForms{
		Zero:  "لا توجد عناصر",
		One:   "عنصر واحد",
		Two:   "عنصران",
		Few:   "{count} عناصر",
		Many:  "{count} عنصرًا",
		Other: "{count} عنصر",
	})
	arCat.Set("ar", a11y.KeyMenuPosition, "{item}، {index} من {count}")
	arCat.Set("ar", a11y.KeyMenuSelected, "تم تحديد {label}")
	arCat.Set("ar", a11y.KeyMenuClosed, "تم إغلاق {label}")

	cases := []struct {
		count int
		want  string
	}{
		{0, "القائمة مفتوحة، لا توجد عناصر"},
		{1, "القائمة مفتوحة، عنصر واحد. خيار"},
		{2, "القائمة مفتوحة، عنصران. خيار"},
		{3, "القائمة مفتوحة، 3 عناصر. خيار"},
	}
	for _, tc := range cases {
		items := make([]a11y.MenuItem, tc.count)
		for i := range items {
			items[i] = a11y.MenuItem{Label: "خيار"}
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return a11y.AccessibleMenu(cx, a11y.MenuProps{
				Text:  a11y.MenuTextFromCatalog(arCat, "ar"),
				Items: items,
			})
		}, nsgotest.Environment(desktopEnv))
		if got := app.Announced[0]; got != tc.want {
			t.Errorf("Arabic count=%d announcement = %q, want %q", tc.count, got, tc.want)
		}
	}

	// Partial hand-written MenuText keeps custom fields and falls back to English for unset fields.
	partial := a11y.MenuText{
		Locale: "ru",
		Items: l10n.PluralForms{
			One:  "{count} пункт",
			Few:  "{count} пункта",
			Many: "{count} пунктов",
		},
		On:  "Вкл",
		Off: "Выкл",
	}
	appPartial := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			Label: "Меню",
			Text:  partial,
			Items: []a11y.MenuItem{
				{Label: "Звук", Checked: func() bool { return true }},
				{Label: "Видео"},
			},
		})
	}, nsgotest.Environment(desktopEnv))

	// Russian plural for 2 uses Few ("2 пункта"); unset OpenedWithItem/Checked fall back to English.
	if got := appPartial.Announced[0]; got != "Меню opened, 2 пункта. Звук, checked" {
		t.Fatalf("partial Russian announcement = %q", got)
	}
	appPartial.Find("text:Вкл")

	// Missing keys in a sparse catalog must fall back to English defaults, never raw keys.
	sparse := l10n.NewCatalog("en")
	sparse.Set("fr", a11y.KeyMenuClosed, "{label} fermé")
	frText := a11y.MenuTextFromCatalog(sparse, "fr")
	if frText.Closed != "{label} fermé" || frText.Opened != a11y.DefaultMenuText().Opened {
		t.Fatalf("sparse MenuTextFromCatalog = %+v", frText)
	}
	for _, s := range []string{frText.DefaultLabel, frText.Opened, frText.OpenedWithItem, frText.Position, frText.Selected, frText.On, frText.Off} {
		if strings.HasPrefix(s, "a11y.") {
			t.Fatalf("sparse catalog leaked raw key %q", s)
		}
	}
}

func TestCaptionsViewLocalizationAndPrefixes(t *testing.T) {
	// Default CaptionStyle keeps "Captions", "! ", "♪ ".
	var trackDef *a11y.CaptionTrack
	appDef := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		trackDef = a11y.NewCaptionTrack(cx)
		return a11y.CaptionsView(cx, trackDef, a11y.CaptionStyle{})
	})
	if got := appDef.Find("id:a11y-captions").Props.Label; got != "Captions" {
		t.Fatalf("default captions region label = %q, want Captions", got)
	}
	appDef.Post(func() {
		trackDef.ShowImportant("Alarm", 2*time.Second)
		trackDef.Show(a11y.Caption{Text: "Main theme", Kind: a11y.CaptionMusic, Duration: 2 * time.Second})
	})
	appDef.Settle()
	appDef.Find("text:! Alarm")
	appDef.Find("text:♪ Main theme")

	// Chinese CaptionStyle via DefaultCatalog + Localizer.
	var trackZH *a11y.CaptionTrack
	cat := a11y.DefaultCatalog()
	appZH := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loc := l10n.NewLocalizer(cx, cat)
		loc.SetLocale("zh-Hans")
		trackZH = a11y.NewCaptionTrack(cx)
		style := a11y.CaptionStyleFromLocalizer(loc)
		style.TestID = "zh-captions"
		return a11y.CaptionsView(cx, trackZH, style)
	})
	if got := appZH.Find("id:zh-captions").Props.Label; got != "字幕" {
		t.Fatalf("zh captions region label = %q, want 字幕", got)
	}
	appZH.Post(func() {
		trackZH.ShowImportant("首领来袭", 2*time.Second)
		trackZH.Show(a11y.Caption{Text: "战斗配乐", Kind: a11y.CaptionMusic, Duration: 2 * time.Second})
	})
	appZH.Settle()
	appZH.Find("text:！首领来袭")
	appZH.Find("text:♪ 战斗配乐")

	// Custom prefixes and FormatCaption directly.
	custom := a11y.CaptionStyle{
		RegionLabel:     "Untertitel",
		ImportantPrefix: "[Wichtig] ",
		MusicPrefix:     "[Musik] ",
	}
	if got := custom.FormatCaption(a11y.Caption{Text: "Alarm", Kind: a11y.CaptionImportant}); got != "[Wichtig] Alarm" {
		t.Fatalf("FormatCaption(important) = %q, want [Wichtig] Alarm", got)
	}
	if got := custom.FormatCaption(a11y.Caption{Text: "Walzer", Kind: a11y.CaptionMusic, Direction: 0.8}); got != "[Musik] Walzer [>]" {
		t.Fatalf("FormatCaption(music) = %q, want [Musik] Walzer [>]", got)
	}

	// Sparse catalog falls back cleanly to English defaults.
	sparse := l10n.NewCatalog("en")
	sparse.Set("de", a11y.KeyCaptionsRegionLabel, "Untertitel")
	deStyle := a11y.CaptionStyleFromCatalog(sparse, "de")
	if deStyle.RegionLabel != "Untertitel" || deStyle.ImportantPrefix != "! " || deStyle.MusicPrefix != "♪ " {
		t.Fatalf("sparse CaptionStyleFromCatalog = %+v", deStyle)
	}
}

func TestLoadDefaultCatalogAndOverrides(t *testing.T) {
	if err := a11y.LoadDefaultCatalog(nil); err == nil {
		t.Fatal("LoadDefaultCatalog(nil) succeeded, want error")
	}

	cat := l10n.NewCatalog("en")
	if err := a11y.LoadDefaultCatalog(cat); err != nil {
		t.Fatalf("LoadDefaultCatalog: %v", err)
	}
	// Game overrides one key in zh while keeping all other defaults.
	cat.Set("zh", a11y.KeyMenuOn, "开启")
	zhText := a11y.MenuTextFromCatalog(cat, "zh-Hans")
	if zhText.On != "开启" || zhText.Off != "关" || zhText.DefaultLabel != "菜单" {
		t.Fatalf("overridden zh MenuText = %+v", zhText)
	}

	// Make sure MenuTextFromLocalizer(nil) and CaptionStyleFromLocalizer(nil) return defaults cleanly.
	if def := a11y.MenuTextFromLocalizer(nil); def.DefaultLabel != "Menu" || def.On != "On" {
		t.Fatalf("MenuTextFromLocalizer(nil) = %+v", def)
	}
	if defCap := a11y.CaptionStyleFromLocalizer(nil); defCap.RegionLabel != "Captions" {
		t.Fatalf("CaptionStyleFromLocalizer(nil) = %+v", defCap)
	}
	// Guard unused unicode import if needed: verify Chinese strings actually contain Han runes.
	hasHan := false
	for _, r := range zhText.DefaultLabel {
		if unicode.Is(unicode.Han, r) {
			hasHan = true
		}
	}
	if !hasHan {
		t.Fatalf("expected Han runes in %q", zhText.DefaultLabel)
	}
}
