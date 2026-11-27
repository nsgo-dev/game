# Agent Note: Localize AccessibleMenu and CaptionsView via MenuText, CaptionStyle, and embedded l10n catalogs

Status: implemented

## Problem

`pkg/a11y`'s `AccessibleMenu` and `CaptionsView` built screen-reader announcements, live-region labels, and visible toggle values from hard-coded English literals (`"Menu"`, `"%s opened, %d items. %s"`, `"checked"`, `"unchecked"`, `"%s, %d of %d"`, `"%s selected"`, `"%s closed"`, `"On"`, `"Off"`, `"Captions"`, `"! "`, `"♪ "`). Non-English games produced mixed-language screen-reader output with fixed English word order and pluralization (for example `"辅助选项 opened, 3 items. 减弱动态效果, unchecked"`), and focusing a `RoleMenuItemCheckbox` repeated `"checked"`/`"unchecked"` even though NSGo's accessibility backends already report `Checked` natively.

## Decision

`pkg/a11y` exposes customizable text structs and embedded `pkg/l10n` string tables while keeping zero-value defaults backward-compatible in English:

- **`MenuText` and `MenuProps.Text`** (`pkg/a11y/l10n.go`, `pkg/a11y/menu.go`): holds every visible and spoken string (`DefaultLabel`, `Opened`, `OpenedWithItem`, `Items` CLDR `l10n.PluralForms` / `ItemsFunc` / `Locale`, `Checked`, `Unchecked`, `Position`, `Selected`, `Closed`, `On`, `Off`, `Separator`). Unset fields inherit from `DefaultMenuText()`, matching `gameui.SettingsText`. Template fields use single-pass named `{placeholder}` interpolation (`{label}`, `{items}`, `{count}`, `{item}`, `{index}`) so word order adapts to CJK and RTL languages without double-substitution.
- **`CaptionStyle` text fields** (`pkg/a11y/captions.go`): `RegionLabel` (default `"Captions"`), `ImportantPrefix` (default `"! "`), and `MusicPrefix` (default `"♪ "`), formatted by `CaptionStyle.FormatCaption(c)` while `Caption.Format()` preserves existing defaults.
- **Embedded `l10n` YAML catalogs** (`pkg/a11y/locales/en.yaml`, `pkg/a11y/locales/zh.yaml`): canonical `l10n.EncodeYAML` string tables embedded via `//go:embed`, exposed through `DefaultCatalog()` and `LoadDefaultCatalog(*l10n.Catalog)`, and read by `MenuTextFromCatalog`, `MenuTextFromLocalizer`, `CaptionStyleFromCatalog`, and `CaptionStyleFromLocalizer` under documented `a11y.menu.*` and `a11y.captions.*` keys (`Key*` constants). Missing keys fall back to English defaults rather than leaking raw key names.
- **Avoiding duplicate checked announcements on focus**: when `AccessibleMenu` moves focus to an item whose resolved role natively reports `Checked` in NSGo (`RoleMenuItemCheckbox`, `RoleMenuItemRadio`, `RoleCheckbox`, `RoleSwitch`, `RoleRadio`), the focus announcement omits `Checked`/`Unchecked` so VoiceOver, TalkBack, UIA, AT-SPI, and ARIA do not speak the state twice. Mount announcements (before focus moves) and toggle activations (where focus stays on the item) still announce the localized checked state, as do focus moves on items whose explicit `Role` is non-checkable.

## Alternatives considered

- **Make `AccessibleMenu` depend on a mandatory `*l10n.Localizer`**: rejected because callers without an `l10n` setup or with hand-built tests would face a breaking signature change; optional `MenuProps.Text` plus `MenuTextFromCatalog`/`MenuTextFromLocalizer` matches `gameui.SettingsText` and keeps zero-config English calls unchanged.
- **Keep `fmt.Sprintf` positional format strings (`%s`, `%d`) in `MenuText`**: rejected because positional verbs cannot reorder arguments cleanly across languages (especially RTL and CJK) and do not match `pkg/l10n`'s named `{placeholder}` syntax.
- **Omit `Checked`/`Unchecked` from toggle activation announcements as well**: rejected because activation keeps focus on the same element and existing callers and tests expect an explicit live announcement of the toggled state.

## Consequences

- **Benefits**: Games localized into Chinese, RTL languages, or any CLDR plural locale get fully localized `AccessibleMenu` and `CaptionsView` output with zero leaked English strings; English singular `"1 item"` is also fixed while multi-item defaults remain identical; screen readers no longer double-speak checked state on focus.
- **Costs and known limits**: `pkg/a11y` now imports `pkg/l10n` (which depends only on `pkg/loaderr` and `nsgo/core`, introducing no cycles). Callers that switch locales while an `AccessibleMenu` is already mounted must pass updated `MenuProps.Text` on remount or re-render, matching `MenuProps.Label` and `MenuProps.Items`.
