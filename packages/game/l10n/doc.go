// Package l10n provides localization for NSGo games: versioned YAML and JSON string tables with BCP-47 locale fallback, interpolation, and CLDR pluralization; per-language asset resolution, fs.FS discovery, versioned YAML asset manifests, runtime locale switching, and missing-variant auditing; Unicode script detection and per-script/per-locale font selection; and right-to-left text reordering and HUD layout mirroring.
//
// It builds on NSGo's public core package, the Go standard library, and go.yaml.in/yaml/v3 for YAML string tables and asset manifests.
package l10n
