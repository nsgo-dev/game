package l10n

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// Arg is a named placeholder argument passed to Catalog.T, Catalog.Plural,
// Localizer.T, or Localizer.Plural.
type Arg struct {
	Name  string
	Value any
}

// Param creates a named placeholder argument for string interpolation
// (e.g. Param("score", 42) substitutes "{score}").
func Param(name string, value any) Arg {
	return Arg{Name: name, Value: value}
}

// PluralForms holds localized message templates keyed by CLDR plural category.
// Any unset category falls back to Other (or One if Other is empty).
type PluralForms struct {
	// Zero is the template used when count is 0 (if provided) or matches the CLDR zero category.
	Zero string `yaml:"zero,omitempty"`
	// One is the template used for the CLDR one plural category.
	One string `yaml:"one,omitempty"`
	// Two is the template used for the CLDR two plural category.
	Two string `yaml:"two,omitempty"`
	// Few is the template used for the CLDR few plural category.
	Few string `yaml:"few,omitempty"`
	// Many is the template used for the CLDR many plural category.
	Many string `yaml:"many,omitempty"`
	// Other is the default fallback template for pluralization.
	Other string `yaml:"other,omitempty"`
}

// entry stores either a plain string or plural forms for a catalog key.
type entry struct {
	isPlural bool
	text     string
	plural   PluralForms
}

// Catalog stores localized string tables keyed by BCP-47 locale tag and message
// key, resolving lookups along the BCP-47 fallback chain to a fallback locale.
type Catalog struct {
	mu       sync.RWMutex
	fallback string
	tables   map[string]map[string]entry
}

// NewCatalog creates a new localization catalog whose fallback locale is
// fallbackLocale (for example "en" or "en-US").
func NewCatalog(fallbackLocale string) *Catalog {
	return &Catalog{
		fallback: NormalizeLocale(fallbackLocale),
		tables:   make(map[string]map[string]entry),
	}
}

// FallbackLocale returns the normalized fallback locale configured on c.
func (c *Catalog) FallbackLocale() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fallback
}

// Locales returns a sorted list of all locales that have entries in c.
func (c *Catalog) Locales() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.tables))
	for loc := range c.tables {
		out = append(out, loc)
	}
	slices.Sort(out)
	return out
}

// Set stores a localized string for locale and key.
func (c *Catalog) Set(locale, key, value string) {
	loc := NormalizeLocale(locale)
	if loc == "" || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tbl := c.tableLocked(loc)
	tbl[key] = entry{text: value}
}

// SetPlural stores plural form templates for locale and key.
func (c *Catalog) SetPlural(locale, key string, forms PluralForms) {
	loc := NormalizeLocale(locale)
	if loc == "" || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tbl := c.tableLocked(loc)
	tbl[key] = entry{isPlural: true, plural: forms}
}

// Has reports whether key resolves in locale or anywhere along its fallback chain.
func (c *Catalog) Has(locale, key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, _, ok := c.lookupLocked(locale, key)
	return ok
}

// LoadJSON parses a JSON string table and merges its entries into locale.
//
// Supported JSON shapes:
//   - Flat keys: {"title": "Quest", "hud.score": "Score: {score}"}
//   - Nested objects: {"menu": {"play": "Play"}} -> key "menu.play"
//   - Plural objects: {"coins": {"zero": "No coins", "one": "{count} coin", "other": "{count} coins"}}
//
// Nested objects may contain plural objects at any depth. Calling LoadJSON
// multiple times for the same locale merges new keys into the existing table.
//
// Errors about data are *loaderr.Error values that give the line and key of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func (c *Catalog) LoadJSON(locale string, data []byte) error {
	loc := NormalizeLocale(locale)
	if loc == "" {
		return fmt.Errorf("l10n: empty locale")
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return loaderr.Locate(err, "", data, fixStringTable)
	}
	if raw == nil {
		return loaderr.Locate(loaderr.Errorf("", fixStringTable, "JSON root for %q must be an object", loc), "", data, "")
	}

	parsed := make(map[string]entry)
	if err := flattenJSON(nil, raw, parsed); err != nil {
		return loaderr.Locate(err, "", data, fixStringTable)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	tbl := c.tableLocked(loc)
	for k, v := range parsed {
		tbl[k] = v
	}
	return nil
}

// T resolves key for locale along the BCP-47 fallback chain and interpolates
// any named placeholders ({name}) using args.
//
// If key is not found in any locale in the chain, key itself is returned.
func (c *Catalog) T(locale, key string, args ...Arg) string {
	c.mu.RLock()
	e, _, ok := c.lookupLocked(locale, key)
	c.mu.RUnlock()
	if !ok {
		return key
	}
	tmpl := e.text
	if e.isPlural {
		tmpl = e.plural.Other
		if tmpl == "" {
			tmpl = e.plural.One
		}
	}
	return interpolate(tmpl, args)
}

// Plural resolves key for locale, selects the plural form matching count for
// the matched locale's plural rules, and interpolates named placeholders.
//
// The "{count}" placeholder is automatically populated with count unless args
// explicitly provides a Param("count", ...). If key is not found, key itself is
// returned.
func (c *Catalog) Plural(locale, key string, count int, args ...Arg) string {
	c.mu.RLock()
	e, matchedLoc, ok := c.lookupLocked(locale, key)
	c.mu.RUnlock()
	if !ok {
		return key
	}

	var tmpl string
	if e.isPlural {
		tmpl = selectPluralForm(matchedLoc, count, e.plural)
	} else {
		tmpl = e.text
	}

	hasCount := false
	for _, a := range args {
		if a.Name == "count" {
			hasCount = true
			break
		}
	}
	if !hasCount {
		withCount := make([]Arg, 0, len(args)+1)
		withCount = append(withCount, Param("count", count))
		withCount = append(withCount, args...)
		args = withCount
	}
	return interpolate(tmpl, args)
}

func (c *Catalog) tableLocked(loc string) map[string]entry {
	tbl, ok := c.tables[loc]
	if !ok {
		tbl = make(map[string]entry)
		c.tables[loc] = tbl
	}
	return tbl
}

func (c *Catalog) lookupLocked(locale, key string) (entry, string, bool) {
	for _, candidate := range localeFallbackChain(locale, c.fallback) {
		if tbl, ok := c.tables[candidate]; ok {
			if e, exists := tbl[key]; exists {
				return e, candidate, true
			}
		}
	}
	return entry{}, "", false
}

// Localizer is a reactive localization view bound to a core.Scope.
//
// By default it follows cx.Env().Locale() (and cx.Env().RTL()), so UI and HUD
// bindings that call Locale, Direction, T, or Plural re-evaluate automatically
// whenever the environment locale changes. Calling SetLocale sets an explicit
// override signal; calling SetLocale("") returns to following the environment.
type Localizer struct {
	cx       *core.Scope
	cat      *Catalog
	override *core.Signal[string]
}

// NewLocalizer creates a reactive Localizer owned by cx backed by cat.
func NewLocalizer(cx *core.Scope, cat *Catalog) *Localizer {
	return &Localizer{
		cx:       cx,
		cat:      cat,
		override: core.NewSignal(cx, ""),
	}
}

// Catalog returns the underlying Catalog.
func (l *Localizer) Catalog() *Catalog { return l.cat }

// SetLocale overrides the active locale with a BCP-47 tag (such as "ja" or
// "zh-Hans"). Pass an empty string to clear the override and follow
// cx.Env().Locale() again.
func (l *Localizer) SetLocale(locale string) {
	l.override.Set(NormalizeLocale(locale))
}

// Locale returns the active BCP-47 locale tag. When read inside a reactive
// computation, it subscribes to both manual SetLocale overrides and
// cx.Env().Locale() changes.
func (l *Localizer) Locale() string {
	if ov := l.override.Get(); ov != "" {
		return ov
	}
	if envLoc := NormalizeLocale(l.cx.Env().Locale()); envLoc != "" {
		return envLoc
	}
	if l.cat != nil {
		return l.cat.FallbackLocale()
	}
	return ""
}

// Direction returns the active layout direction (DirLTR or DirRTL).
//
// When a manual locale override is active, it returns LocaleDirection of that
// locale. Otherwise it honors cx.Env().RTL() and falls back to
// LocaleDirection(l.Locale()). Reading Direction inside a reactive computation
// subscribes to locale and RTL environment changes.
func (l *Localizer) Direction() Direction {
	if ov := l.override.Get(); ov != "" {
		return LocaleDirection(ov)
	}
	if l.cx.Env().RTL() {
		return DirRTL
	}
	return LocaleDirection(l.Locale())
}

// RTL reports whether the active layout direction is right-to-left.
func (l *Localizer) RTL() bool {
	return l.Direction() == DirRTL
}

// T resolves key for the active locale and interpolates args. Reading T inside
// a reactive computation subscribes it to locale changes.
func (l *Localizer) T(key string, args ...Arg) string {
	loc := l.Locale()
	if l.cat == nil {
		return key
	}
	return l.cat.T(loc, key, args...)
}

// Plural resolves a pluralized key for the active locale and count. Reading
// Plural inside a reactive computation subscribes it to locale changes.
func (l *Localizer) Plural(key string, count int, args ...Arg) string {
	loc := l.Locale()
	if l.cat == nil {
		return key
	}
	return l.cat.Plural(loc, key, count, args...)
}

// Visual reorders s into visual left-to-right drawing order for the active
// Direction.
func (l *Localizer) Visual(s string) string {
	return VisualOrder(s, l.Direction())
}

// NormalizeLocale canonicalizes a BCP-47 locale tag: underscores become
// hyphens, the primary language subtag is lowercased, 4-letter script subtags
// are Title-cased (e.g. "Hans"), and 2-letter region subtags are UPPERCASED
// (e.g. "US").
func NormalizeLocale(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	tag = strings.ReplaceAll(tag, "_", "-")
	rawParts := strings.Split(tag, "-")
	parts := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		switch {
		case len(p) == 4 && isASCIIAlpha(p):
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		case len(p) == 2 && isASCIIAlpha(p):
			parts[i] = strings.ToUpper(p)
		case len(p) == 3 && isASCIIDigits(p):
			parts[i] = p
		default:
			parts[i] = strings.ToLower(p)
		}
	}
	return strings.Join(parts, "-")
}

// localeFallbackChain builds the ordered, deduplicated BCP-47 fallback chain
// for locale followed by fallback (for example:
// "zh-Hans-CN" -> ["zh-Hans-CN", "zh-Hans", "zh", "en"]).
func localeFallbackChain(locale, fallback string) []string {
	var chain []string
	addHierarchy := func(tag string) {
		norm := NormalizeLocale(tag)
		if norm == "" {
			return
		}
		parts := strings.Split(norm, "-")
		for n := len(parts); n >= 1; n-- {
			cand := strings.Join(parts[:n], "-")
			if !slices.Contains(chain, cand) {
				chain = append(chain, cand)
			}
		}
	}
	addHierarchy(locale)
	addHierarchy(fallback)
	return chain
}

func primaryLanguage(locale string) string {
	norm := NormalizeLocale(locale)
	if norm == "" {
		return ""
	}
	if idx := strings.IndexByte(norm, '-'); idx >= 0 {
		return norm[:idx]
	}
	return norm
}

func scriptSubtag(locale string) string {
	norm := NormalizeLocale(locale)
	if norm == "" {
		return ""
	}
	parts := strings.Split(norm, "-")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) == 4 && isASCIIAlpha(parts[i]) {
			return parts[i]
		}
	}
	return ""
}

func isASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// fixStringTable is the fix for a string table error that has no more specific one.
const fixStringTable = `write the table as a JSON object of strings, nested objects and plural objects, such as {"menu": {"play": "Play"}}`

// flattenJSON adds the entries of obj, the object at the JSON keys path, to out under dotted keys.
func flattenJSON(path []string, obj map[string]any, out map[string]entry) error {
	// Sorted, so the same key is reported every time when several are wrong.
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		v := obj[k]
		at := append(slices.Clip(path), k)
		if k == "" {
			return loaderr.Errorf(jsonPath(at), "give the entry a non-empty key", "empty key under %q", strings.Join(path, "."))
		}
		fullKey := strings.Join(at, ".")
		switch val := v.(type) {
		case string:
			out[fullKey] = entry{text: val}
		case map[string]any:
			if forms, ok := asPluralObject(val); ok {
				out[fullKey] = entry{isPlural: true, plural: forms}
			} else {
				if err := flattenJSON(at, val, out); err != nil {
					return err
				}
			}
		default:
			return loaderr.Errorf(jsonPath(at), "make the value a string, or an object of nested keys or plural forms", "key %q has unsupported JSON value type %T", fullKey, v)
		}
	}
	return nil
}

// jsonPath writes JSON object keys as a field path.
func jsonPath(keys []string) string {
	parts := make([]any, len(keys))
	for i, k := range keys {
		parts[i] = k
	}
	return loaderr.Path(parts...)
}

func asPluralObject(m map[string]any) (PluralForms, bool) {
	if len(m) == 0 {
		return PluralForms{}, false
	}
	var forms PluralForms
	hasOther := false
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return PluralForms{}, false
		}
		switch k {
		case "zero":
			forms.Zero = s
		case "one":
			forms.One = s
		case "two":
			forms.Two = s
		case "few":
			forms.Few = s
		case "many":
			forms.Many = s
		case "other":
			forms.Other = s
			hasOther = true
		default:
			return PluralForms{}, false
		}
	}
	if !hasOther && len(m) < 2 {
		return PluralForms{}, false
	}
	return forms, true
}

func selectPluralForm(locale string, count int, forms PluralForms) string {
	n := count
	if n < 0 {
		n = -n
	}
	// Honor an explicit zero form when provided.
	if n == 0 && forms.Zero != "" {
		return forms.Zero
	}

	pick := func(primary string) string {
		if primary != "" {
			return primary
		}
		if forms.Other != "" {
			return forms.Other
		}
		return forms.One
	}

	switch primaryLanguage(locale) {
	case "ja", "zh", "ko", "th", "vi", "id", "ms":
		return pick(forms.Other)
	case "fr", "pt", "hi":
		if n == 0 || n == 1 {
			return pick(forms.One)
		}
		return pick(forms.Other)
	case "ar":
		switch {
		case n == 0:
			return pick(forms.Zero)
		case n == 1:
			return pick(forms.One)
		case n == 2:
			return pick(forms.Two)
		case n%100 >= 3 && n%100 <= 10:
			return pick(forms.Few)
		case n%100 >= 11 && n%100 <= 99:
			return pick(forms.Many)
		default:
			return pick(forms.Other)
		}
	case "ru", "uk", "sr", "hr", "be":
		mod10, mod100 := n%10, n%100
		switch {
		case mod10 == 1 && mod100 != 11:
			return pick(forms.One)
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return pick(forms.Few)
		default:
			return pick(forms.Many)
		}
	case "pl":
		mod10, mod100 := n%10, n%100
		switch {
		case n == 1:
			return pick(forms.One)
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return pick(forms.Few)
		default:
			return pick(forms.Many)
		}
	case "he", "iw":
		switch n {
		case 1:
			return pick(forms.One)
		case 2:
			return pick(forms.Two)
		default:
			return pick(forms.Other)
		}
	default:
		if n == 1 {
			return pick(forms.One)
		}
		return pick(forms.Other)
	}
}

func interpolate(tmpl string, args []Arg) string {
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
			if val, ok := findArg(args, name); ok {
				b.WriteString(formatArgValue(val))
			} else {
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

func findArg(args []Arg, name string) (any, bool) {
	// Search from the end so later arguments override earlier ones.
	for i := len(args) - 1; i >= 0; i-- {
		if args[i].Name == name {
			return args[i].Value, true
		}
	}
	return nil, false
}

func formatArgValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(v)
	}
}
