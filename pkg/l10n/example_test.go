package l10n_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/l10n"
)

// A Catalog looks up a message for a locale, falling back along the locale's parents to the fallback locale, and fills in named parameters.
func ExampleCatalog() {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "hud.score", "Score: {score}")
	cat.Set("en", "menu.start", "Start")
	cat.Set("fr", "hud.score", "Score : {score}")
	cat.SetPlural("en", "hud.lives", l10n.PluralForms{One: "{count} life left", Other: "{count} lives left"})

	fmt.Println(cat.T("fr-CA", "hud.score", l10n.Param("score", 42)))
	fmt.Println(cat.T("fr-CA", "menu.start"))
	fmt.Println(cat.Plural("en", "hud.lives", 1))
	fmt.Println(cat.Plural("en", "hud.lives", 3))
	// Output:
	// Score : 42
	// Start
	// 1 life left
	// 3 lives left
}

// LoadJSON reads a locale's strings from a JSON table.
func ExampleCatalog_LoadJSON() {
	cat := l10n.NewCatalog("en")
	err := cat.LoadJSON("ja", []byte(`{"menu.start": "スタート", "menu.quit": "終了"}`))
	fmt.Println(err, cat.T("ja", "menu.quit"), cat.Has("ja", "menu.options"))
	// Output:
	// <nil> 終了 false
}

// LocaleDirection and DetectScript tell a layout which way text runs and which font it needs.
func ExampleLocaleDirection() {
	for _, tag := range []string{"en_US", "ar", "he-IL", "zh-hant-tw"} {
		fmt.Println(l10n.NormalizeLocale(tag), l10n.LocaleDirection(tag), l10n.LocaleScript(tag))
	}
	fmt.Println(l10n.DetectScript("こんにちは"), l10n.DetectScript("Привет"))
	// Output:
	// en-US ltr Latin
	// ar rtl Arabic
	// he-IL rtl Hebrew
	// zh-Hant-TW ltr CJK
	// Japanese Cyrillic
}

// A FontSet chooses a font by script or locale, and splits mixed text into runs that each use one font.
func ExampleFontSet() {
	fonts := l10n.NewFontSet("Latin Sans")
	fonts.SetScript(l10n.ScriptCJK, "CJK Sans")
	fonts.SetLocale("ja", "Japanese Sans")
	fmt.Println(fonts.ForLocale("ja-JP"))
	for _, run := range fonts.RunsForText("en", "Level 3 第三关") {
		fmt.Printf("%q %s\n", run.Text, run.Font)
	}
	// Output:
	// Japanese Sans
	// "Level 3 " Latin Sans
	// "第三关" CJK Sans
}

// An AssetCatalog maps a logical asset name to its file for the current language, falling back to the base locale's file.
func ExampleAssetCatalog() {
	assets := l10n.NewAssetCatalog("en")
	assets.RegisterMap("voice/intro.wav", map[string]string{
		"en": "voice/en/intro.wav",
		"ja": "voice/ja/intro.wav",
	})
	assets.Register("ui/banner.png", "en", "ui/en/banner.png")
	assets.RegisterGroup("title", "voice/intro.wav", "ui/banner.png")

	for _, r := range assets.ResolveGroup("title", "ja") {
		fmt.Println(r.LogicalName, r.Path, r.IsFallback)
	}
	for _, m := range assets.CheckMissing("ja") {
		fmt.Println("missing:", m.Locale, m.Missing)
	}
	// Output:
	// ui/banner.png ui/en/banner.png true
	// voice/intro.wav voice/ja/intro.wav false
	// missing: ja [ui/banner.png]
}
