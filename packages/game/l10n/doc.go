// Package l10n provides localization for NSGo games: string tables with BCP-47
// locale fallback, interpolation, and pluralization; per-language asset
// resolution, fs.FS discovery, runtime locale switching, and missing-variant
// auditing; Unicode script detection and per-script/per-locale font selection;
// and right-to-left text reordering and HUD layout mirroring.
//
// It builds only on NSGo's public core package and the Go standard library.
package l10n
