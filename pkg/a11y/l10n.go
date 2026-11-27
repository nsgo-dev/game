package a11y

import (
	"embed"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
)

//go:embed locales/en.yaml locales/zh.yaml
var embeddedLocalesFS embed.FS

// Documented localization keys read by MenuTextFromCatalog,
// MenuTextFromLocalizer, CaptionStyleFromCatalog, and CaptionStyleFromLocalizer.
const (
	// KeyMenuDefaultLabel is the fallback accessible label when MenuProps.Label is empty.
	KeyMenuDefaultLabel = "a11y.menu.defaultLabel"
	// KeyMenuItems is a plural key formatted with {count} for the menu's item count.
	KeyMenuItems = "a11y.menu.items"
	// KeyMenuOpened formats the mount announcement when no item is enabled ({label}, {items}, {count}).
	KeyMenuOpened = "a11y.menu.opened"
	// KeyMenuOpenedWithItem formats the mount announcement with an active item ({label}, {items}, {count}, {item}).
	KeyMenuOpenedWithItem = "a11y.menu.openedWithItem"
	// KeyMenuPosition formats focus-change announcements ({item}, {index}, {count}).
	KeyMenuPosition = "a11y.menu.position"
	// KeyMenuSelected formats activation announcements for non-toggle items ({label}).
	KeyMenuSelected = "a11y.menu.selected"
	// KeyMenuClosed formats the Escape/close announcement ({label}).
	KeyMenuClosed = "a11y.menu.closed"
	// KeyMenuChecked is the spoken checked state word.
	KeyMenuChecked = "a11y.menu.checked"
	// KeyMenuUnchecked is the spoken unchecked state word.
	KeyMenuUnchecked = "a11y.menu.unchecked"
	// KeyMenuOn is the visible value shown for checked toggle items.
	KeyMenuOn = "a11y.menu.on"
	// KeyMenuOff is the visible value shown for unchecked toggle items.
	KeyMenuOff = "a11y.menu.off"
	// KeyMenuSeparator joins item status parts (label, checked state, value) in announcements.
	KeyMenuSeparator = "a11y.menu.separator"

	// KeyCaptionsRegionLabel is the accessible label of CaptionsView's live region.
	KeyCaptionsRegionLabel = "a11y.captions.regionLabel"
	// KeyCaptionsImportantPrefix is prepended to speakerless CaptionImportant cues.
	KeyCaptionsImportantPrefix = "a11y.captions.importantPrefix"
	// KeyCaptionsMusicPrefix is prepended to speakerless CaptionMusic cues.
	KeyCaptionsMusicPrefix = "a11y.captions.musicPrefix"
)

const (
	defaultCaptionRegionLabel     = "Captions"
	defaultCaptionImportantPrefix = "! "
	defaultCaptionMusicPrefix     = "♪ "
)

// DefaultCatalog returns a new localization catalog preloaded with pkg/a11y's
// built-in English ("en") and Simplified Chinese ("zh") string tables.
func DefaultCatalog() *l10n.Catalog {
	cat := l10n.NewCatalog("en")
	if err := LoadDefaultCatalog(cat); err != nil {
		panic("a11y: load embedded locales: " + err.Error())
	}
	return cat
}

// LoadDefaultCatalog merges pkg/a11y's built-in English and Simplified Chinese
// string tables into cat. Call it before loading game-specific overrides so
// custom entries take precedence.
func LoadDefaultCatalog(cat *l10n.Catalog) error {
	if cat == nil {
		return fmt.Errorf("a11y: nil catalog")
	}
	for _, name := range []string{"locales/en.yaml", "locales/zh.yaml"} {
		data, err := embeddedLocalesFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("a11y: read %s: %w", name, err)
		}
		if err := cat.LoadYAML(data); err != nil {
			return fmt.Errorf("a11y: parse %s: %w", name, err)
		}
	}
	return nil
}

// MenuText holds every user-visible and screen-reader string produced by
// AccessibleMenu so games can localize menus into any language.
//
// Template fields use named {placeholder} interpolation:
//   - Opened: {label}, {items}, {count}
//   - OpenedWithItem: {label}, {items}, {count}, {item}
//   - Position: {item}, {index}, {count}
//   - Selected: {label}
//   - Closed: {label}
//   - Items: CLDR plural templates with {count}
//
// Partial MenuText values passed via MenuProps.Text inherit any unset fields
// from DefaultMenuText.
type MenuText struct {
	// DefaultLabel is used when MenuProps.Label is empty (default "Menu").
	DefaultLabel string
	// Opened formats the mount announcement when no item is enabled.
	Opened string
	// OpenedWithItem formats the mount announcement when an initial item is active.
	OpenedWithItem string
	// Items holds CLDR plural forms for the item count ({count}), selected using
	// Locale (default "en"). Ignored when ItemsFunc is set.
	Items l10n.PluralForms
	// ItemsFunc, when non-nil, formats the item count directly (set automatically
	// by MenuTextFromCatalog and MenuTextFromLocalizer).
	ItemsFunc func(count int) string
	// Locale is the BCP-47 tag used to select plural rules for Items (default "en").
	Locale string
	// Checked is spoken for checked toggle items when the platform does not
	// already report the checked state (default "checked").
	Checked string
	// Unchecked is spoken for unchecked toggle items when the platform does not
	// already report the checked state (default "unchecked").
	Unchecked string
	// Position formats focus-change announcements ({item}, {index}, {count}).
	Position string
	// Selected formats activation announcements for action items ({label}).
	Selected string
	// Closed formats the Escape/close announcement ({label}).
	Closed string
	// On is the visible toggle text when Checked returns true (default "On").
	On string
	// Off is the visible toggle text when Checked returns false (default "Off").
	Off string
	// Separator joins item status parts (label, checked state, value) in
	// spoken announcements (default ", ").
	Separator string
}

// DefaultMenuText returns the English strings used by AccessibleMenu when
// MenuProps.Text fields are left empty.
func DefaultMenuText() MenuText {
	return MenuText{
		DefaultLabel:   "Menu",
		Opened:         "{label} opened, {items}",
		OpenedWithItem: "{label} opened, {items}. {item}",
		Items: l10n.PluralForms{
			One:   "{count} item",
			Other: "{count} items",
		},
		Locale:    "en",
		Checked:   "checked",
		Unchecked: "unchecked",
		Position:  "{item}, {index} of {count}",
		Selected:  "{label} selected",
		Closed:    "{label} closed",
		On:        "On",
		Off:       "Off",
		Separator: ", ",
	}
}

// MenuTextFromCatalog fills a MenuText from cat for locale using the documented
// a11y.menu.* keys. Any key missing from cat (along locale's fallback chain)
// falls back to DefaultMenuText. The item count uses cat.Plural for
// KeyMenuItems.
func MenuTextFromCatalog(cat *l10n.Catalog, locale string) MenuText {
	out := DefaultMenuText()
	if cat == nil {
		return out
	}
	loc := l10n.NormalizeLocale(locale)
	if loc == "" {
		loc = cat.FallbackLocale()
	}
	if loc != "" {
		out.Locale = loc
	}
	lookup := func(key string, dst *string) {
		if cat.Has(loc, key) {
			*dst = cat.T(loc, key)
		}
	}
	lookup(KeyMenuDefaultLabel, &out.DefaultLabel)
	lookup(KeyMenuOpened, &out.Opened)
	lookup(KeyMenuOpenedWithItem, &out.OpenedWithItem)
	lookup(KeyMenuChecked, &out.Checked)
	lookup(KeyMenuUnchecked, &out.Unchecked)
	lookup(KeyMenuPosition, &out.Position)
	lookup(KeyMenuSelected, &out.Selected)
	lookup(KeyMenuClosed, &out.Closed)
	lookup(KeyMenuOn, &out.On)
	lookup(KeyMenuOff, &out.Off)
	lookup(KeyMenuSeparator, &out.Separator)
	if cat.Has(loc, KeyMenuItems) {
		out.ItemsFunc = func(count int) string {
			return cat.Plural(loc, KeyMenuItems, count)
		}
	}
	return out
}

// MenuTextFromLocalizer fills a MenuText from loc's active locale and catalog.
// If loc is nil, DefaultMenuText is returned.
func MenuTextFromLocalizer(loc *l10n.Localizer) MenuText {
	if loc == nil {
		return DefaultMenuText()
	}
	return MenuTextFromCatalog(loc.Catalog(), loc.Locale())
}

// CaptionStyleFromCatalog returns a CaptionStyle whose RegionLabel,
// ImportantPrefix, and MusicPrefix are filled from cat for locale using the
// documented a11y.captions.* keys. Missing keys fall back to the English
// defaults ("Captions", "! ", "♪ ").
func CaptionStyleFromCatalog(cat *l10n.Catalog, locale string) CaptionStyle {
	out := CaptionStyle{
		RegionLabel:     defaultCaptionRegionLabel,
		ImportantPrefix: defaultCaptionImportantPrefix,
		MusicPrefix:     defaultCaptionMusicPrefix,
	}
	if cat == nil {
		return out
	}
	loc := l10n.NormalizeLocale(locale)
	if loc == "" {
		loc = cat.FallbackLocale()
	}
	if cat.Has(loc, KeyCaptionsRegionLabel) {
		out.RegionLabel = cat.T(loc, KeyCaptionsRegionLabel)
	}
	if cat.Has(loc, KeyCaptionsImportantPrefix) {
		out.ImportantPrefix = cat.T(loc, KeyCaptionsImportantPrefix)
	}
	if cat.Has(loc, KeyCaptionsMusicPrefix) {
		out.MusicPrefix = cat.T(loc, KeyCaptionsMusicPrefix)
	}
	return out
}

// CaptionStyleFromLocalizer returns a CaptionStyle populated from loc's active
// locale and catalog. If loc is nil, default English caption labels are used.
func CaptionStyleFromLocalizer(loc *l10n.Localizer) CaptionStyle {
	if loc == nil {
		return CaptionStyleFromCatalog(nil, "")
	}
	return CaptionStyleFromCatalog(loc.Catalog(), loc.Locale())
}

type resolvedMenuText struct {
	MenuText
	formatItems func(count int) string
}

func resolveMenuText(in MenuText) resolvedMenuText {
	def := DefaultMenuText()
	fill := func(dst *string, fallback string) {
		if *dst == "" {
			*dst = fallback
		}
	}
	fill(&in.DefaultLabel, def.DefaultLabel)
	fill(&in.Opened, def.Opened)
	fill(&in.OpenedWithItem, def.OpenedWithItem)
	fill(&in.Locale, def.Locale)
	fill(&in.Checked, def.Checked)
	fill(&in.Unchecked, def.Unchecked)
	fill(&in.Position, def.Position)
	fill(&in.Selected, def.Selected)
	fill(&in.Closed, def.Closed)
	fill(&in.On, def.On)
	fill(&in.Off, def.Off)
	fill(&in.Separator, def.Separator)

	var formatItems func(int) string
	if in.ItemsFunc != nil {
		formatItems = in.ItemsFunc
	} else {
		forms := in.Items
		if forms == (l10n.PluralForms{}) {
			forms = def.Items
		}
		loc := l10n.NormalizeLocale(in.Locale)
		if loc == "" {
			loc = "en"
		}
		cat := l10n.NewCatalog(loc)
		cat.SetPlural(loc, KeyMenuItems, forms)
		formatItems = func(count int) string {
			return cat.Plural(loc, KeyMenuItems, count)
		}
	}
	return resolvedMenuText{
		MenuText:    in,
		formatItems: formatItems,
	}
}

// interpolateTemplate replaces named {placeholder} tokens in tmpl using
// key/value pairs in a single pass, matching pkg/l10n's {{ and }} escaping.
func interpolateTemplate(tmpl string, pairs ...string) string {
	if !strings.ContainsAny(tmpl, "{}") {
		return tmpl
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	for i := 0; i < len(tmpl); {
		switch tmpl[i] {
		case '{':
			if i+1 < len(tmpl) && tmpl[i+1] == '{' {
				b.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(tmpl[i+1:], '}')
			if end < 0 {
				b.WriteString(tmpl[i:])
				return b.String()
			}
			name := tmpl[i+1 : i+1+end]
			found := false
			for j := len(pairs) - 2; j >= 0; j -= 2 {
				if pairs[j] == name {
					b.WriteString(pairs[j+1])
					found = true
					break
				}
			}
			if !found {
				b.WriteString(tmpl[i : i+2+end])
			}
			i += end + 2
		case '}':
			if i+1 < len(tmpl) && tmpl[i+1] == '}' {
				b.WriteByte('}')
				i += 2
				continue
			}
			b.WriteByte('}')
			i++
		default:
			b.WriteByte(tmpl[i])
			i++
		}
	}
	return b.String()
}
