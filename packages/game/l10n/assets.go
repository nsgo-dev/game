package l10n

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// ResolvedAsset describes the concrete localized asset path selected for a
// logical asset name and requested BCP-47 locale.
type ResolvedAsset struct {
	LogicalName     string
	RequestedLocale string
	ResolvedLocale  string
	Path            string
	IsFallback      bool
}

// MissingReport summarizes direct-variant localization coverage for a single
// locale across a set of logical assets.
type MissingReport struct {
	Locale      string
	TotalAssets int
	Present     int
	Missing     []string
	Coverage    float64
}

// Complete reports whether every audited asset has a direct variant for Locale.
func (r MissingReport) Complete() bool {
	return len(r.Missing) == 0
}

type reloadListener struct {
	id int
	fn func(groups []string, locale string)
}

// AssetCatalog maps logical asset names (such as "voice/intro.wav" or
// "ui/title_banner.png") to per-locale asset paths, resolves lookups along the
// BCP-47 fallback chain, tracks named asset groups for selective runtime
// reloads on language switch, and audits missing locale variants.
type AssetCatalog struct {
	mu            sync.RWMutex
	baseLocale    string
	currentLocale string
	assets        map[string]map[string]string   // logical name -> normalized locale -> path
	groups        map[string]map[string]struct{} // group name -> set of logical names
	nextListener  int
	listeners     []reloadListener
}

// NewAssetCatalog creates a new AssetCatalog whose fallback locale is
// baseLocale (for example "en" or "en-US").
func NewAssetCatalog(baseLocale string) *AssetCatalog {
	base := NormalizeLocale(baseLocale)
	return &AssetCatalog{
		baseLocale:    base,
		currentLocale: base,
		assets:        make(map[string]map[string]string),
		groups:        make(map[string]map[string]struct{}),
	}
}

// DiscoverFS walks fsys and builds an AssetCatalog using baseLocale as the
// fallback locale for unlocalized files and BCP-47 fallback chains.
//
// Recognized layouts:
//   - Directory layout: "locales/<locale>/<name>" (or "<prefix>/locales/<locale>/<name>")
//   - Dot suffix layout: "<name>.<locale>.<ext>" (for example "ui/title_banner.ja.png")
//   - Underscore suffix layout: "<name>_<locale>.<ext>" (for example "voice/intro_zh-Hans.wav" or "voice/intro_zh_Hans.wav")
//   - Unlocalized files (such as "sfx/click.wav"): registered under baseLocale when
//     baseLocale is non-empty and no explicit baseLocale variant exists for that name.
func DiscoverFS(fsys fs.FS, baseLocale string) (*AssetCatalog, error) {
	cat := NewAssetCatalog(baseLocale)
	if err := cat.Discover(fsys); err != nil {
		return nil, err
	}
	return cat, nil
}

// BaseLocale returns the normalized base/fallback locale configured on c.
func (c *AssetCatalog) BaseLocale() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseLocale
}

// Locale returns the currently active locale tracked by c, falling back to
// BaseLocale if no active locale has been set.
func (c *AssetCatalog) Locale() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.currentLocale != "" {
		return c.currentLocale
	}
	return c.baseLocale
}

// Locales returns a sorted list of all locales that have at least one asset
// variant registered in c.
func (c *AssetCatalog) Locales() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.localesLocked()
}

func (c *AssetCatalog) localesLocked() []string {
	seen := make(map[string]struct{})
	for _, variants := range c.assets {
		for loc := range variants {
			seen[loc] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for loc := range seen {
		out = append(out, loc)
	}
	slices.Sort(out)
	return out
}

// Names returns a sorted list of all logical asset names registered in c.
func (c *AssetCatalog) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.namesLocked()
}

func (c *AssetCatalog) namesLocked() []string {
	out := make([]string, 0, len(c.assets))
	for name := range c.assets {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Groups returns a sorted list of all asset group names registered in c.
func (c *AssetCatalog) Groups() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.groups))
	for g := range c.groups {
		out = append(out, g)
	}
	slices.Sort(out)
	return out
}

// GroupAssets returns a sorted list of logical asset names registered in group.
func (c *AssetCatalog) GroupAssets(group string) []string {
	g := strings.TrimSpace(group)
	if g == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	members, ok := c.groups[g]
	if !ok || len(members) == 0 {
		return nil
	}
	out := make([]string, 0, len(members))
	for name := range members {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Register registers a localized variant path for logical asset name and locale.
func (c *AssetCatalog) Register(name, locale, assetPath string) {
	cleanName := cleanAssetName(name)
	loc := NormalizeLocale(locale)
	p := strings.TrimSpace(assetPath)
	if cleanName == "" || loc == "" || p == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerLocked(cleanName, loc, p)
}

func (c *AssetCatalog) registerLocked(name, loc, assetPath string) {
	variants, ok := c.assets[name]
	if !ok {
		variants = make(map[string]string)
		c.assets[name] = variants
	}
	variants[loc] = assetPath
}

// RegisterMap registers multiple locale variants (locale -> path) for logical
// asset name.
func (c *AssetCatalog) RegisterMap(name string, variants map[string]string) {
	cleanName := cleanAssetName(name)
	if cleanName == "" || len(variants) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for loc, assetPath := range variants {
		normLoc := NormalizeLocale(loc)
		p := strings.TrimSpace(assetPath)
		if normLoc == "" || p == "" {
			continue
		}
		c.registerLocked(cleanName, normLoc, p)
	}
}

// RegisterGroup associates logical asset names with group so AffectedGroups,
// SetLocale, and WatchLocale can determine which asset groups require reloading
// on a locale switch. Calling RegisterGroup multiple times for the same group
// merges names.
func (c *AssetCatalog) RegisterGroup(group string, names ...string) {
	g := strings.TrimSpace(group)
	if g == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerGroupLocked(g, names...)
}

func (c *AssetCatalog) registerGroupLocked(group string, names ...string) {
	members, ok := c.groups[group]
	if !ok {
		members = make(map[string]struct{})
		c.groups[group] = members
	}
	for _, name := range names {
		if clean := cleanAssetName(name); clean != "" {
			members[clean] = struct{}{}
		}
	}
}

// Discover walks fsys and registers all discovered asset variants into c.
// Explicit locale matches ("locales/<locale>/<name>", "<name>.<locale>.<ext>",
// "<name>_<locale>.<ext>") take precedence over unlocalized default files.
func (c *AssetCatalog) Discover(fsys fs.FS) error {
	if fsys == nil {
		return fmt.Errorf("l10n: nil fs.FS")
	}

	type assetEntry struct {
		name   string
		locale string
		path   string
	}
	var (
		explicit []assetEntry
		defaults []assetEntry
	)

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := path.Base(p)
		if strings.HasPrefix(base, ".") {
			return nil
		}
		if name, loc, ok := parseDiscoveredPath(p); ok {
			explicit = append(explicit, assetEntry{name: name, locale: loc, path: p})
		} else if clean := cleanAssetName(p); clean != "" {
			defaults = append(defaults, assetEntry{name: clean, path: p})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("l10n: discover assets: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range explicit {
		c.registerLocked(e.name, e.locale, e.path)
	}
	if c.baseLocale != "" {
		for _, e := range defaults {
			variants := c.assets[e.name]
			if variants != nil {
				if _, exists := variants[c.baseLocale]; exists {
					continue
				}
			}
			c.registerLocked(e.name, c.baseLocale, e.path)
		}
	}
	return nil
}

// Resolve looks up logical asset name for locale along the BCP-47 fallback
// chain (for example "zh-Hans-CN" -> "zh-Hans" -> "zh" -> BaseLocale).
//
// IsFallback is true whenever the resolved locale differs from the normalized
// requested locale. Returns (ResolvedAsset{}, false) if no variant exists in
// the fallback chain.
func (c *AssetCatalog) Resolve(name, locale string) (ResolvedAsset, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resolveLocked(name, locale)
}

func (c *AssetCatalog) resolveLocked(name, locale string) (ResolvedAsset, bool) {
	cleanName := cleanAssetName(name)
	if cleanName == "" {
		return ResolvedAsset{}, false
	}
	variants, ok := c.assets[cleanName]
	if !ok {
		return ResolvedAsset{}, false
	}
	req := NormalizeLocale(locale)
	for _, cand := range localeFallbackChain(req, c.baseLocale) {
		if p, exists := variants[cand]; exists {
			return ResolvedAsset{
				LogicalName:     cleanName,
				RequestedLocale: req,
				ResolvedLocale:  cand,
				Path:            p,
				IsFallback:      cand != req,
			}, true
		}
	}
	return ResolvedAsset{}, false
}

// ResolveGroup resolves every asset registered in group for locale, returning
// the resolved assets in deterministic sorted order by LogicalName.
func (c *AssetCatalog) ResolveGroup(group, locale string) []ResolvedAsset {
	g := strings.TrimSpace(group)
	if g == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	members, ok := c.groups[g]
	if !ok || len(members) == 0 {
		return nil
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]ResolvedAsset, 0, len(names))
	for _, name := range names {
		if res, ok := c.resolveLocked(name, locale); ok {
			out = append(out, res)
		}
	}
	return out
}

// AffectedAssets returns a sorted list of logical asset names whose resolved
// path or resolved locale changes when switching from oldLocale to newLocale.
func (c *AssetCatalog) AffectedAssets(oldLocale, newLocale string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string
	for name := range c.assets {
		if c.assetChangedLocked(name, oldLocale, newLocale) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// AffectedGroups returns a sorted list of registered group names that contain
// at least one asset whose resolved variant changes when switching from
// oldLocale to newLocale.
func (c *AssetCatalog) AffectedGroups(oldLocale, newLocale string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.affectedGroupsLocked(oldLocale, newLocale)
}

func (c *AssetCatalog) affectedGroupsLocked(oldLocale, newLocale string) []string {
	var out []string
	for group, members := range c.groups {
		for name := range members {
			if c.assetChangedLocked(name, oldLocale, newLocale) {
				out = append(out, group)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

func (c *AssetCatalog) assetChangedLocked(name, oldLocale, newLocale string) bool {
	oldRes, oldOK := c.resolveLocked(name, oldLocale)
	newRes, newOK := c.resolveLocked(name, newLocale)
	if oldOK != newOK {
		return true
	}
	if !oldOK {
		return false
	}
	return oldRes.Path != newRes.Path || oldRes.ResolvedLocale != newRes.ResolvedLocale
}

// OnReload registers a callback invoked by SetLocale whenever one or more
// asset groups are affected by a locale switch. The returned function removes
// the callback.
func (c *AssetCatalog) OnReload(fn func(groups []string, locale string)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	c.mu.Lock()
	id := c.nextListener
	c.nextListener++
	c.listeners = append(c.listeners, reloadListener{id: id, fn: fn})
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, l := range c.listeners {
			if l.id == id {
				c.listeners = append(c.listeners[:i], c.listeners[i+1:]...)
				return
			}
		}
	}
}

// SetLocale switches the catalog's active locale at runtime, computes the
// affected asset groups between the previous locale and locale, notifies any
// OnReload listeners if at least one group is affected, and returns the sorted
// list of affected groups.
func (c *AssetCatalog) SetLocale(locale string) []string {
	c.mu.Lock()
	prev := c.currentLocale
	if prev == "" {
		prev = c.baseLocale
	}
	next := NormalizeLocale(locale)
	if next == "" {
		next = c.baseLocale
	}
	affected := c.affectedGroupsLocked(prev, next)
	c.currentLocale = next
	var listeners []reloadListener
	if len(affected) > 0 && len(c.listeners) > 0 {
		listeners = make([]reloadListener, len(c.listeners))
		copy(listeners, c.listeners)
	}
	c.mu.Unlock()

	for _, l := range listeners {
		l.fn(slices.Clone(affected), next)
	}
	return affected
}

// WatchLocale subscribes to locale changes on localeSignal inside cx and
// invokes reloadFn with ONLY the affected asset groups whenever the active
// locale changes.
//
// If localeSignal is nil, WatchLocale follows cx.Env().Locale(). The initial
// locale is recorded without triggering a reload; reloadFn is called only when
// a subsequent locale change alters at least one asset in a registered group.
// Calling the returned stop function cancels the subscription.
func (c *AssetCatalog) WatchLocale(
	cx *core.Scope,
	localeSignal *core.Signal[string],
	reloadFn func(groups []string, locale string),
) (stop func()) {
	if cx == nil {
		return func() {}
	}

	initialized := false
	prev := ""

	return cx.Effect(func() {
		var raw string
		if localeSignal != nil {
			raw = localeSignal.Get()
		} else {
			raw = cx.Env().Locale()
		}

		next := NormalizeLocale(raw)
		if next == "" {
			next = c.BaseLocale()
		}

		if !initialized {
			initialized = true
			prev = next
			c.mu.Lock()
			c.currentLocale = next
			c.mu.Unlock()
			return
		}

		if next == prev {
			return
		}

		oldLoc := prev
		prev = next

		c.mu.Lock()
		affected := c.affectedGroupsLocked(oldLoc, next)
		c.currentLocale = next
		c.mu.Unlock()

		if len(affected) > 0 && reloadFn != nil {
			cx.Untrack(func() {
				reloadFn(affected, next)
			})
		}
	})
}

// CheckMissing audits direct-variant asset coverage for locales in
// deterministic sorted order.
//
// If no locales are provided, all locales currently registered in c are
// audited. Each MissingReport lists the sorted logical asset names that lack a
// direct variant for that locale (and would therefore require fallback or fail
// to resolve).
func (c *AssetCatalog) CheckMissing(locales ...string) []MissingReport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkMissingNamesLocked(c.namesLocked(), locales)
}

// CheckMissingGroup audits direct-variant asset coverage for the logical assets
// registered in group across locales in deterministic sorted order.
func (c *AssetCatalog) CheckMissingGroup(group string, locales ...string) []MissingReport {
	g := strings.TrimSpace(group)
	c.mu.RLock()
	defer c.mu.RUnlock()
	members := c.groups[g]
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	slices.Sort(names)
	return c.checkMissingNamesLocked(names, locales)
}

func (c *AssetCatalog) checkMissingNamesLocked(names []string, locales []string) []MissingReport {
	var targets []string
	if len(locales) == 0 {
		targets = c.localesLocked()
	} else {
		seen := make(map[string]struct{}, len(locales))
		for _, loc := range locales {
			norm := NormalizeLocale(loc)
			if norm == "" {
				continue
			}
			if _, dup := seen[norm]; !dup {
				seen[norm] = struct{}{}
				targets = append(targets, norm)
			}
		}
		slices.Sort(targets)
	}

	reports := make([]MissingReport, 0, len(targets))
	total := len(names)
	for _, loc := range targets {
		var missing []string
		present := 0
		for _, name := range names {
			if variants := c.assets[name]; variants != nil {
				if _, ok := variants[loc]; ok {
					present++
					continue
				}
			}
			missing = append(missing, name)
		}
		coverage := 1.0
		if total > 0 {
			coverage = float64(present) / float64(total)
		}
		reports = append(reports, MissingReport{
			Locale:      loc,
			TotalAssets: total,
			Present:     present,
			Missing:     missing,
			Coverage:    coverage,
		})
	}
	return reports
}

// cleanAssetName canonicalizes a logical asset path to slash-separated form
// without leading slashes or "./".
func cleanAssetName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Clean(s)
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimPrefix(s, "./")
	if s == "" || s == "." || s == ".." || strings.HasPrefix(s, "../") {
		return ""
	}
	return s
}

// parseDiscoveredPath inspects an fs.FS relative file path for localized
// directory or filename patterns:
//   - locales/<locale>/<name> (or <prefix>/locales/<locale>/<name>)
//   - <name>.<locale>.<ext>
//   - <name>_<locale>.<ext>
func parseDiscoveredPath(p string) (logicalName, locale string, ok bool) {
	clean := cleanAssetName(p)
	if clean == "" {
		return "", "", false
	}

	// 1. Directory pattern: locales/<locale>/<name> (or <prefix>/locales/<locale>/<name>).
	parts := strings.Split(clean, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "locales" && isLocaleDirTag(parts[i+1]) {
			loc := NormalizeLocale(parts[i+1])
			var nameParts []string
			if i > 0 {
				nameParts = append(nameParts, parts[:i]...)
			}
			nameParts = append(nameParts, parts[i+2:]...)
			name := cleanAssetName(strings.Join(nameParts, "/"))
			if name != "" && loc != "" {
				return name, loc, true
			}
		}
	}

	// 2. Filename suffix patterns: <name>.<locale>.<ext> and <name>_<locale>.<ext>.
	dir := path.Dir(clean)
	base := path.Base(clean)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		return "", "", false
	}

	buildLogical := func(newStem string) string {
		fname := newStem + ext
		if dir == "." {
			return fname
		}
		return dir + "/" + fname
	}

	// 2a. Dot suffix: <stem>.<locale><ext>
	if dot := strings.LastIndexByte(stem, '.'); dot > 0 {
		cand := stem[dot+1:]
		if isFilenameLocaleTag(cand) {
			return buildLogical(stem[:dot]), NormalizeLocale(cand), true
		}
	}

	// 2b. Underscore suffix: <stem>_<locale><ext>, allowing up to 3 underscore
	// subtags (e.g. _zh_Hans_CN, _zh_Hans, _en_US, _zh-Hans, _ja).
	segs := strings.Split(stem, "_")
	if len(segs) >= 2 {
		maxTake := min(3, len(segs)-1)
		for take := maxTake; take >= 1; take-- {
			cand := strings.Join(segs[len(segs)-take:], "_")
			if isFilenameLocaleTag(cand) {
				logicalStem := strings.Join(segs[:len(segs)-take], "_")
				if logicalStem != "" {
					return buildLogical(logicalStem), NormalizeLocale(cand), true
				}
			}
		}
	}

	return "", "", false
}

// isLocaleDirTag checks whether seg is a structurally valid BCP-47 locale tag
// inside a "locales/<locale>/..." directory.
func isLocaleDirTag(seg string) bool {
	norm := NormalizeLocale(seg)
	if norm == "" {
		return false
	}
	parts := strings.Split(norm, "-")
	return validateBCP47Parts(parts, false)
}

// isFilenameLocaleTag checks whether cand is a recognized BCP-47 locale tag
// suitable for filename suffix matching without colliding with ordinary words
// such as "_banner", "_ok", "_hd", or "_01".
func isFilenameLocaleTag(cand string) bool {
	norm := NormalizeLocale(cand)
	if norm == "" {
		return false
	}
	parts := strings.Split(norm, "-")
	return validateBCP47Parts(parts, true)
}

func validateBCP47Parts(parts []string, strict bool) bool {
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	lang := parts[0]
	if !(len(lang) == 2 || len(lang) == 3) || !isASCIIAlpha(lang) {
		return false
	}
	if strict && !isKnownLanguageSubtag(lang) {
		return false
	}
	if len(parts) == 1 {
		return true
	}

	idx := 1
	// Optional 4-letter ISO 15924 script subtag (Title-cased by NormalizeLocale).
	if len(parts[idx]) == 4 && isASCIIAlpha(parts[idx]) {
		if strict && !isKnownScriptSubtag(parts[idx]) {
			return false
		}
		idx++
		if idx == len(parts) {
			return true
		}
	}

	// Optional 2-letter ISO 3166-1 or 3-digit UN M.49 region subtag.
	if idx == len(parts)-1 {
		reg := parts[idx]
		if len(reg) == 3 && isASCIIDigits(reg) {
			return true
		}
		if len(reg) == 2 && isASCIIAlpha(reg) {
			if strict && !isKnownRegionSubtag(reg) {
				return false
			}
			return true
		}
	}

	return false
}

// isKnownLanguageSubtag reports whether lang (lowercased) is a standard ISO
// 639-1 two-letter language code or common ISO 639-2/3 code used in BCP-47.
func isKnownLanguageSubtag(lang string) bool {
	switch lang {
	case "aa", "ab", "ae", "af", "ak", "am", "an", "ar", "as", "av", "ay", "az",
		"ba", "be", "bg", "bh", "bi", "bm", "bn", "bo", "br", "bs",
		"ca", "ce", "ch", "co", "cr", "cs", "cu", "cv", "cy",
		"da", "de", "dv", "dz",
		"ee", "el", "en", "eo", "es", "et", "eu",
		"fa", "ff", "fi", "fj", "fo", "fr", "fy",
		"ga", "gd", "gl", "gn", "gu", "gv",
		"ha", "he", "hi", "ho", "hr", "ht", "hu", "hy", "hz",
		"ia", "id", "ie", "ig", "ii", "ik", "in", "io", "is", "it", "iu", "iw",
		"ja", "ji", "jv", "jw",
		"ka", "kg", "ki", "kj", "kk", "kl", "km", "kn", "ko", "kr", "ks", "ku", "kv", "kw", "ky",
		"la", "lb", "lg", "li", "ln", "lo", "lt", "lu", "lv",
		"mg", "mh", "mi", "mk", "ml", "mn", "mo", "mr", "ms", "mt", "my",
		"na", "nb", "nd", "ne", "ng", "nl", "nn", "no", "nr", "nv", "ny",
		"oc", "oj", "om", "or", "os",
		"pa", "pi", "pl", "ps", "pt",
		"qu",
		"rm", "rn", "ro", "ru", "rw",
		"sa", "sc", "sd", "se", "sg", "sh", "si", "sk", "sl", "sm", "sn", "so", "sq", "sr", "ss", "st", "su", "sv", "sw",
		"ta", "te", "tg", "th", "ti", "tk", "tl", "tn", "to", "tr", "ts", "tt", "tw", "ty",
		"ug", "uk", "ur", "uz",
		"ve", "vi", "vo",
		"wa", "wo",
		"xh",
		"yi", "yo",
		"za", "zh", "zu",
		// Common 3-letter BCP-47 language subtags.
		"arc", "ast", "bal", "bgn", "ceb", "ckb", "cmn", "dsb", "fil", "glk",
		"gsw", "haw", "hsb", "kab", "khw", "kok", "lrc", "mai", "mzn", "nan",
		"nds", "nqo", "pap", "pnb", "prs", "scn", "syr", "tzm", "yue", "zgh":
		return true
	default:
		return false
	}
}

// isKnownScriptSubtag reports whether script (Title-cased) is a common ISO
// 15924 script subtag.
func isKnownScriptSubtag(script string) bool {
	switch script {
	case "Adlm", "Arab", "Armn", "Beng", "Bopo", "Cans", "Cher", "Chrs", "Cyrl",
		"Deva", "Dsrt", "Ethi", "Geor", "Grek", "Gujr", "Guru", "Hang", "Hani",
		"Hans", "Hant", "Hebr", "Hira", "Jpan", "Kana", "Khmr", "Knda", "Kore",
		"Laoo", "Latn", "Mand", "Mend", "Mlym", "Mong", "Mymr", "Nkoo", "Orya",
		"Osge", "Rohg", "Samr", "Sinh", "Syrc", "Taml", "Telu", "Tfng", "Thaa",
		"Thai", "Tibt", "Vaii", "Yezi":
		return true
	default:
		return false
	}
}

// isKnownRegionSubtag reports whether region (UPPERCASED) is a standard ISO
// 3166-1 alpha-2 country/territory code.
func isKnownRegionSubtag(region string) bool {
	switch region {
	case "AD", "AE", "AF", "AG", "AI", "AL", "AM", "AO", "AQ", "AR", "AS", "AT", "AU", "AW", "AX", "AZ",
		"BA", "BB", "BD", "BE", "BF", "BG", "BH", "BI", "BJ", "BL", "BM", "BN", "BO", "BQ", "BR", "BS", "BT", "BV", "BW", "BY", "BZ",
		"CA", "CC", "CD", "CF", "CG", "CH", "CI", "CK", "CL", "CM", "CN", "CO", "CR", "CU", "CV", "CW", "CX", "CY", "CZ",
		"DE", "DJ", "DK", "DM", "DO", "DZ",
		"EC", "EE", "EG", "EH", "ER", "ES", "ET",
		"FI", "FJ", "FK", "FM", "FO", "FR",
		"GA", "GB", "GD", "GE", "GF", "GG", "GH", "GI", "GL", "GM", "GN", "GP", "GQ", "GR", "GS", "GT", "GU", "GW", "GY",
		"HK", "HM", "HN", "HR", "HT", "HU",
		"ID", "IE", "IL", "IM", "IN", "IO", "IQ", "IR", "IS", "IT",
		"JE", "JM", "JO", "JP",
		"KE", "KG", "KH", "KI", "KM", "KN", "KP", "KR", "KW", "KY", "KZ",
		"LA", "LB", "LC", "LI", "LK", "LR", "LS", "LT", "LU", "LV", "LY",
		"MA", "MC", "MD", "ME", "MF", "MG", "MH", "MK", "ML", "MM", "MN", "MO", "MP", "MQ", "MR", "MS", "MT", "MU", "MV", "MW", "MX", "MY", "MZ",
		"NA", "NC", "NE", "NF", "NG", "NI", "NL", "NO", "NP", "NR", "NU", "NZ",
		"OM",
		"PA", "PE", "PF", "PG", "PH", "PK", "PL", "PM", "PN", "PR", "PS", "PT", "PW", "PY",
		"QA",
		"RE", "RO", "RS", "RU", "RW",
		"SA", "SB", "SC", "SD", "SE", "SG", "SH", "SI", "SJ", "SK", "SL", "SM", "SN", "SO", "SR", "SS", "ST", "SV", "SX", "SY", "SZ",
		"TC", "TD", "TF", "TG", "TH", "TJ", "TK", "TL", "TM", "TN", "TO", "TR", "TT", "TV", "TW", "TZ",
		"UA", "UG", "UM", "US", "UY", "UZ",
		"VA", "VC", "VE", "VG", "VI", "VN", "VU",
		"WF", "WS",
		"XK",
		"YE", "YT",
		"ZA", "ZM", "ZW":
		return true
	default:
		return false
	}
}
