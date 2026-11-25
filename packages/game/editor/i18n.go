package editor

import (
	"embed"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
)

//go:embed locales/en.yaml locales/zh.yaml
var embeddedLocalesFS embed.FS

var defaultEditorCatalog = mustLoadDefaultCatalog()

func mustLoadDefaultCatalog() *l10n.Catalog {
	cat, err := loadEmbeddedCatalog()
	if err != nil {
		panic("editor: load embedded locales: " + err.Error())
	}
	return cat
}

func loadEmbeddedCatalog() (*l10n.Catalog, error) {
	cat := l10n.NewCatalog("en")
	for _, name := range []string{"locales/en.yaml", "locales/zh.yaml"} {
		data, err := embeddedLocalesFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if err := cat.LoadYAML(data); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
	}
	return cat, nil
}

// DefaultCatalog returns a copy of the editor's built-in localization catalog (English and Simplified Chinese).
func DefaultCatalog() *l10n.Catalog {
	cat, err := loadEmbeddedCatalog()
	if err != nil {
		return l10n.NewCatalog("en")
	}
	return cat
}

// Catalog returns the editor's active localization catalog.
func (e *Editor) Catalog() *l10n.Catalog {
	if e == nil || e.catalog == nil {
		return defaultEditorCatalog
	}
	return e.catalog
}

// ResolvedLanguage returns the active BCP-47 locale ("en" or "zh") used to render the editor UI and plugins.
// When Language() is LanguageSystem, it follows the attached scope's environment locale, resolving Simplified Chinese locales ("zh", "zh-CN", "zh-Hans", etc.) to "zh" and all others to "en".
func (e *Editor) ResolvedLanguage() string {
	if e == nil {
		return "en"
	}
	switch e.Language() {
	case LanguageEnglish:
		return "en"
	case LanguageChinese:
		return "zh"
	default:
		if e.scope != nil {
			env := e.scope.Env()
			if loc := resolveSystemLocale(env.Locale()); loc != "" {
				return loc
			}
			if loc := resolveSystemLocale(env.Language()); loc != "" {
				return loc
			}
		}
		return "en"
	}
}

func resolveSystemLocale(tag string) string {
	norm := l10n.NormalizeLocale(tag)
	if norm == "" {
		return ""
	}
	primary := norm
	if idx := strings.IndexByte(norm, '-'); idx >= 0 {
		primary = norm[:idx]
	}
	if primary == "zh" {
		return "zh"
	}
	return "en"
}

// T translates key in the editor's active language using the editor's localization catalog.
func (e *Editor) T(key string, args ...l10n.Arg) string {
	return e.Catalog().T(e.ResolvedLanguage(), key, args...)
}

// Plural translates a pluralized key in the editor's active language using the editor's localization catalog.
func (e *Editor) Plural(key string, count int, args ...l10n.Arg) string {
	return e.Catalog().Plural(e.ResolvedLanguage(), key, count, args...)
}

// PluginT translates key from the named registered plugin's localization catalog in the editor's active language.
// If the plugin is not registered or has no catalog, key is returned.
func (e *Editor) PluginT(pluginName, key string, args ...l10n.Arg) string {
	if e == nil {
		return key
	}
	p := e.plugin(pluginName)
	if p == nil || p.catalog == nil {
		return key
	}
	return p.catalog.T(e.ResolvedLanguage(), key, args...)
}

// PluginPlural translates a pluralized key from the named registered plugin's localization catalog in the editor's active language.
// If the plugin is not registered or has no catalog, key is returned.
func (e *Editor) PluginPlural(pluginName, key string, count int, args ...l10n.Arg) string {
	if e == nil {
		return key
	}
	p := e.plugin(pluginName)
	if p == nil || p.catalog == nil {
		return key
	}
	return p.catalog.Plural(e.ResolvedLanguage(), key, count, args...)
}

// translatePluginText resolves a plugin's display string in the editor's active language:
// if key is set and present in p's catalog it translates key; otherwise if fallback itself is a key in p's catalog it translates fallback; otherwise it returns fallback unchanged.
func (e *Editor) translatePluginText(p *Plugin, key, fallback string) string {
	if p != nil && p.catalog != nil {
		loc := e.ResolvedLanguage()
		if key != "" && p.catalog.Has(loc, key) {
			return p.catalog.T(loc, key)
		}
		if fallback != "" && p.catalog.Has(loc, fallback) {
			return p.catalog.T(loc, fallback)
		}
	}
	if fallback != "" {
		return fallback
	}
	return key
}

// localizedToolName returns the display name of t in the editor's active language.
func (e *Editor) localizedToolName(t Tool) string {
	switch t {
	case ToolSelect:
		return e.T("tool.select")
	case ToolMove:
		return e.T("tool.move")
	case ToolPlace:
		return e.T("tool.place")
	case ToolPaintTile:
		return e.T("tool.paintTile")
	case ToolEraseTile:
		return e.T("tool.eraseTile")
	case ToolFillTile:
		return e.T("tool.fillTile")
	case ToolPlugin:
		return e.T("tool.plugin")
	case ToolPickTile:
		return e.T("tool.pickTile")
	case ToolNavTestPath:
		return e.T("tool.navTestPath")
	default:
		return t.String()
	}
}

// localizedGizmoName returns the display name of m in the editor's active language.
func (e *Editor) localizedGizmoName(m GizmoMode3D) string {
	switch m {
	case Gizmo3DTranslate:
		return e.T("gizmo.translate")
	case Gizmo3DRotate:
		return e.T("gizmo.rotate")
	case Gizmo3DScale:
		return e.T("gizmo.scale")
	default:
		return m.String()
	}
}
