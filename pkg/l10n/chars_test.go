package l10n_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/l10n"
)

func TestStripPlaceholders(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Play", "Play"},
		{"Score: {score}", "Score: "},
		{"{count} 枚金币", " 枚金币"},
		{"{player} dealt {dmg} to {target}!", " dealt  to !"},
		{"{{literal}} and {arg} and }}", "{literal} and  and }"},
		{"unclosed {name", "unclosed {name"},
		{"empty {} braces", "empty {} braces"},
		{"line1 {a\nb} line2", "line1 {a\nb} line2"},
	}
	for _, tc := range cases {
		if got := l10n.StripPlaceholders(tc.in); got != tc.want {
			t.Errorf("StripPlaceholders(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringTableCollectChars(t *testing.T) {
	if got := (*l10n.StringTable)(nil).CollectChars(); got != "" {
		t.Fatalf("nil CollectChars = %q, want empty", got)
	}
	tbl := &l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  "zh-Hans",
		Strings: map[string]string{
			"hud.score": "得分：{score}\n继续",
			"menu.esc":  "{{括号}}",
		},
		Plurals: map[string]l10n.PluralForms{
			"coins": {
				Zero:  "无金币",
				One:   "{count} 枚",
				Two:   "两枚",
				Few:   "少许",
				Many:  "大量",
				Other: "{count} 枚金币\t!",
			},
		},
	}
	got := tbl.CollectChars()
	// Placeholders {score} and {count} must not contribute ASCII letters; \n and \t must be stripped; space, !, {, }, and all CJK chars must be sorted and unique.
	for _, forbidden := range []rune{'s', 'c', 'o', 'r', 'e', 'u', 'n', 't', '\n', '\t'} {
		for _, r := range got {
			if r == forbidden {
				t.Errorf("CollectChars(%q) unexpectedly contains %q", got, forbidden)
			}
		}
	}
	for _, required := range []rune{' ', '!', '{', '}', '得', '分', '：', '继', '续', '括', '号', '无', '金', '币', '枚', '两', '少', '许', '大', '量'} {
		found := false
		for _, r := range got {
			if r == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CollectChars(%q) missing expected rune %q", got, required)
		}
	}
	// Verify strictly sorted and deduplicated.
	var prev rune
	for i, r := range got {
		if i > 0 && r <= prev {
			t.Fatalf("CollectChars not strictly sorted at %d: %q <= %q (%q)", i, r, prev, got)
		}
		prev = r
	}
}

func TestValidateLocale(t *testing.T) {
	for _, valid := range []string{"en", "zh-Hans", "zh-Hans-CN", "pt-BR", "ar-001"} {
		if err := l10n.ValidateLocale(valid); err != nil {
			t.Errorf("ValidateLocale(%q) unexpected error: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "zh_hans", "EN", "en-us", "toolongsubtag", "en-!"} {
		if err := l10n.ValidateLocale(invalid); err == nil {
			t.Errorf("ValidateLocale(%q) = nil, want error", invalid)
		}
	}
}
