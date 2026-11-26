package editor

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// ErrNoLocalization is returned by localization edits when the Localization panel has no tables open.
var ErrNoLocalization = errors.New("editor: no localization tables open")

// MissingTranslation identifies a (locale, key) cell that lacks a translation in the Localization panel.
type MissingTranslation struct {
	Locale string
	Key    string
	Plural bool
}

// pluralCategories lists the six CLDR plural categories in canonical order.
var pluralCategories = []string{"zero", "one", "two", "few", "many", "other"}

// localizationState holds the open per-locale string tables edited by the Localization panel.
type localizationState struct {
	dir     string
	locales []string
	// keys maps every message key row to true for CLDR plural keys and false for simple strings.
	keys    map[string]bool
	strings map[string]map[string]string
	plurals map[string]map[string]l10n.PluralForms
	// saved records the last opened or saved canonical YAML bytes per locale, used for dirty checks and removing deleted locale files on save.
	saved   map[string][]byte
	loading bool
	lastErr error
}

func newLocalizationState(dir string) *localizationState {
	return &localizationState{
		dir:     dir,
		keys:    make(map[string]bool),
		strings: make(map[string]map[string]string),
		plurals: make(map[string]map[string]l10n.PluralForms),
		saved:   make(map[string][]byte),
	}
}

func (s *localizationState) clone() *localizationState {
	if s == nil {
		return nil
	}
	c := &localizationState{
		dir:     s.dir,
		locales: slices.Clone(s.locales),
		keys:    maps.Clone(s.keys),
		strings: make(map[string]map[string]string, len(s.strings)),
		plurals: make(map[string]map[string]l10n.PluralForms, len(s.plurals)),
		saved:   make(map[string][]byte, len(s.saved)),
	}
	for loc, m := range s.strings {
		c.strings[loc] = maps.Clone(m)
	}
	for loc, m := range s.plurals {
		c.plurals[loc] = maps.Clone(m)
	}
	for loc, raw := range s.saved {
		c.saved[loc] = bytes.Clone(raw)
	}
	return c
}

func (s *localizationState) hasLocale(loc string) bool {
	return slices.Contains(s.locales, loc)
}

func (s *localizationState) tableFor(loc string) *l10n.StringTable {
	t := &l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  loc,
	}
	if sm := s.strings[loc]; len(sm) > 0 {
		t.Strings = make(map[string]string, len(sm))
		for k, v := range sm {
			if isPlural, ok := s.keys[k]; ok && !isPlural && v != "" {
				t.Strings[k] = v
			}
		}
		if len(t.Strings) == 0 {
			t.Strings = nil
		}
	}
	if pm := s.plurals[loc]; len(pm) > 0 {
		t.Plurals = make(map[string]l10n.PluralForms, len(pm))
		for k, forms := range pm {
			if isPlural, ok := s.keys[k]; ok && isPlural && forms != (l10n.PluralForms{}) {
				t.Plurals[k] = forms
			}
		}
		if len(t.Plurals) == 0 {
			t.Plurals = nil
		}
	}
	return t
}

func (s *localizationState) isMissing(loc, key string) bool {
	isPlural, ok := s.keys[key]
	if !ok || !s.hasLocale(loc) {
		return false
	}
	if isPlural {
		forms := s.plurals[loc][key]
		return forms.Other == ""
	}
	return s.strings[loc][key] == ""
}

// l10nCmd applies a reversible edit to Editor.l10n.
type l10nCmd struct {
	ed            *Editor
	label         string
	before, after *localizationState
}

func (c *l10nCmd) Label() string { return c.label }
func (c *l10nCmd) Do()           { c.ed.l10n = c.after.clone() }
func (c *l10nCmd) Undo()         { c.ed.l10n = c.before.clone() }

func (e *Editor) swapLocalization(label string, next *localizationState) {
	if next != nil {
		next.lastErr = nil
	}
	e.History.Execute(&l10nCmd{
		ed:     e,
		label:  label,
		before: e.l10n.clone(),
		after:  next.clone(),
	})
}

func (e *Editor) openDefaultLocalizations() error {
	if e.ProjectPath != "" {
		dir := filepath.Join(filepath.Dir(e.ProjectPath), "locales")
		if err := e.OpenLocalizations(dir); err != nil {
			return err
		}
		if len(e.LocalizationLocales()) == 0 {
			return e.AddLocalizationLocale("en")
		}
		return nil
	}
	return e.NewLocalizations("", "en")
}

// LocalizationOpen reports whether the Localization panel currently has a workspace open.
func (e *Editor) LocalizationOpen() bool {
	return e != nil && e.l10n != nil
}

// LocalizationDir returns the directory where the open localization YAML files live, or "".
func (e *Editor) LocalizationDir() string {
	if e == nil || e.l10n == nil {
		return ""
	}
	return e.l10n.dir
}

// LocalizationError returns the most recent error encountered in the Localization panel, or nil.
func (e *Editor) LocalizationError() error {
	if e == nil || e.l10n == nil {
		return nil
	}
	return e.l10n.lastErr
}

// ClearLocalizationError clears any error banner currently displayed in the Localization panel.
func (e *Editor) ClearLocalizationError() {
	if e == nil || e.l10n == nil || e.l10n.lastErr == nil {
		return
	}
	e.l10n.lastErr = nil
	e.notify()
}

// LocalizationLoading reports whether the Localization panel is currently loading tables.
func (e *Editor) LocalizationLoading() bool {
	return e != nil && e.l10n != nil && e.l10n.loading
}

// SetLocalizationLoading updates the Localization panel's loading state and notifies listeners.
func (e *Editor) SetLocalizationLoading(loading bool) {
	if e == nil || e.l10n == nil || e.l10n.loading == loading {
		return
	}
	e.l10n.loading = loading
	e.notify()
}

func (e *Editor) reportLocalizationError(err error) {
	if err == nil {
		return
	}
	if e.l10n != nil {
		e.l10n.lastErr = err
	}
	e.report(err)
}

// NewLocalizations opens a fresh localization workspace targeting dir with the given BCP-47 locales (sorted and deduplicated) as an undoable command.
func (e *Editor) NewLocalizations(dir string, locales ...string) error {
	if e.playing {
		return ErrPlaying
	}
	st := newLocalizationState(dir)
	for _, raw := range locales {
		loc, err := normalizeEditorLocale(raw)
		if err != nil {
			return err
		}
		if st.hasLocale(loc) {
			return fmt.Errorf("editor: duplicate locale %q", loc)
		}
		st.locales = append(st.locales, loc)
		st.strings[loc] = make(map[string]string)
		st.plurals[loc] = make(map[string]l10n.PluralForms)
	}
	slices.Sort(st.locales)
	e.swapLocalization("New localizations", st)
	e.Status = "Opened localization table"
	e.notify()
	return nil
}

// OpenLocalizations reads all *.yaml string table files in dir with l10n.ParseYAML and opens them in the Localization panel as an undoable command. A missing directory opens an empty workspace targeting dir so saving creates it.
func (e *Editor) OpenLocalizations(dir string) error {
	if e.playing {
		return ErrPlaying
	}
	st := newLocalizationState(dir)
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else {
			for _, ent := range entries {
				if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") {
					continue
				}
				full := filepath.Join(dir, ent.Name())
				data, err := os.ReadFile(full)
				if err != nil {
					return err
				}
				tbl, err := l10n.ParseYAML(data)
				if err != nil {
					return loaderr.WithFile(err, full)
				}
				if st.hasLocale(tbl.Locale) {
					return fmt.Errorf("editor: duplicate locale %q in %s", tbl.Locale, full)
				}
				canonical, err := l10n.EncodeYAML(tbl)
				if err != nil {
					return err
				}
				st.locales = append(st.locales, tbl.Locale)
				st.strings[tbl.Locale] = make(map[string]string, len(tbl.Strings))
				st.plurals[tbl.Locale] = make(map[string]l10n.PluralForms, len(tbl.Plurals))
				st.saved[tbl.Locale] = canonical

				for k, v := range tbl.Strings {
					if isPlural, exists := st.keys[k]; exists && isPlural {
						return fmt.Errorf("editor: %s defines key %q as a string, but another locale defines it as plural", full, k)
					}
					st.keys[k] = false
					st.strings[tbl.Locale][k] = v
				}
				for k, forms := range tbl.Plurals {
					if isPlural, exists := st.keys[k]; exists && !isPlural {
						return fmt.Errorf("editor: %s defines key %q as plural, but another locale defines it as a string", full, k)
					}
					st.keys[k] = true
					st.plurals[tbl.Locale][k] = forms
				}
			}
		}
	}
	slices.Sort(st.locales)
	e.swapLocalization("Open localizations", st)
	e.Status = "Opened localizations"
	if dir != "" {
		e.Status += " " + dir
	}
	e.notify()
	return nil
}

// CloseLocalizations closes the Localization panel as an undoable command.
func (e *Editor) CloseLocalizations() error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return nil
	}
	e.swapLocalization("Close localizations", nil)
	e.Status = "Closed localization table"
	e.notify()
	return nil
}

// LocalizationLocales returns the sorted BCP-47 locale columns of the open localization workspace.
func (e *Editor) LocalizationLocales() []string {
	if e == nil || e.l10n == nil {
		return nil
	}
	return slices.Clone(e.l10n.locales)
}

// LocalizationKeys returns the sorted message key rows of the open localization workspace.
func (e *Editor) LocalizationKeys() []string {
	if e == nil || e.l10n == nil {
		return nil
	}
	out := make([]string, 0, len(e.l10n.keys))
	for k := range e.l10n.keys {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// LocalizationKeyPlural reports whether key is edited as CLDR plural forms.
func (e *Editor) LocalizationKeyPlural(key string) bool {
	if e == nil || e.l10n == nil {
		return false
	}
	return e.l10n.keys[key]
}

// LocalizationValue returns the plain string translation for (locale, key) and whether it is present.
func (e *Editor) LocalizationValue(locale, key string) (string, bool) {
	if e == nil || e.l10n == nil {
		return "", false
	}
	loc := l10n.NormalizeLocale(locale)
	isPlural, ok := e.l10n.keys[key]
	if !ok || isPlural {
		return "", false
	}
	v := e.l10n.strings[loc][key]
	return v, v != ""
}

// LocalizationPlural returns the CLDR plural forms for (locale, key) and whether any form is set.
func (e *Editor) LocalizationPlural(locale, key string) (l10n.PluralForms, bool) {
	if e == nil || e.l10n == nil {
		return l10n.PluralForms{}, false
	}
	loc := l10n.NormalizeLocale(locale)
	isPlural, ok := e.l10n.keys[key]
	if !ok || !isPlural {
		return l10n.PluralForms{}, false
	}
	forms := e.l10n.plurals[loc][key]
	return forms, forms != (l10n.PluralForms{})
}

// LocalizationIsMissing reports whether (locale, key) has a missing translation (an empty string for a simple key, or a missing "other" form for a plural key).
func (e *Editor) LocalizationIsMissing(locale, key string) bool {
	if e == nil || e.l10n == nil {
		return false
	}
	return e.l10n.isMissing(l10n.NormalizeLocale(locale), key)
}

// LocalizationMissing returns every missing (locale, key) translation in deterministic key-then-locale order.
func (e *Editor) LocalizationMissing() []MissingTranslation {
	if e == nil || e.l10n == nil {
		return nil
	}
	keys := e.LocalizationKeys()
	var missing []MissingTranslation
	for _, k := range keys {
		isPlural := e.l10n.keys[k]
		for _, loc := range e.l10n.locales {
			if e.l10n.isMissing(loc, k) {
				missing = append(missing, MissingTranslation{
					Locale: loc,
					Key:    k,
					Plural: isPlural,
				})
			}
		}
	}
	return missing
}

// LocalizationMissingCount returns the number of missing translations across all keys and locales.
func (e *Editor) LocalizationMissingCount() int {
	return len(e.LocalizationMissing())
}

// AddLocalizationLocale adds a normalized BCP-47 locale column to the Localization panel as an undoable command.
func (e *Editor) AddLocalizationLocale(locale string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	loc, err := normalizeEditorLocale(locale)
	if err != nil {
		return err
	}
	if e.l10n.hasLocale(loc) {
		return fmt.Errorf("editor: locale %q already exists", loc)
	}
	next := e.l10n.clone()
	next.locales = append(next.locales, loc)
	slices.Sort(next.locales)
	next.strings[loc] = make(map[string]string)
	next.plurals[loc] = make(map[string]l10n.PluralForms)
	e.swapLocalization("Add locale "+loc, next)
	return nil
}

// RemoveLocalizationLocale removes locale and all its translations from the Localization panel as an undoable command.
func (e *Editor) RemoveLocalizationLocale(locale string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	loc := l10n.NormalizeLocale(locale)
	idx := slices.Index(e.l10n.locales, loc)
	if idx < 0 {
		return fmt.Errorf("editor: locale %q not found", locale)
	}
	next := e.l10n.clone()
	next.locales = slices.Delete(next.locales, idx, idx+1)
	delete(next.strings, loc)
	delete(next.plurals, loc)
	e.swapLocalization("Remove locale "+loc, next)
	return nil
}

// AddLocalizationKey adds a message key row to the Localization panel (plain string when plural is false, CLDR plural forms when true) as an undoable command.
func (e *Editor) AddLocalizationKey(key string, plural bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	if err := validateLocalizationKey(key); err != nil {
		return err
	}
	if _, exists := e.l10n.keys[key]; exists {
		return fmt.Errorf("editor: localization key %q already exists", key)
	}
	next := e.l10n.clone()
	next.keys[key] = plural
	e.swapLocalization("Add key "+key, next)
	return nil
}

// RemoveLocalizationKey removes key from every locale in the Localization panel as an undoable command.
func (e *Editor) RemoveLocalizationKey(key string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	if _, exists := e.l10n.keys[key]; !exists {
		return fmt.Errorf("editor: localization key %q not found", key)
	}
	next := e.l10n.clone()
	delete(next.keys, key)
	for _, m := range next.strings {
		delete(m, key)
	}
	for _, m := range next.plurals {
		delete(m, key)
	}
	e.swapLocalization("Remove key "+key, next)
	return nil
}

// SetLocalizationKeyPlural switches key between plain string mode and CLDR plural mode as an undoable command, preserving text in the "other" plural form (and vice versa).
func (e *Editor) SetLocalizationKeyPlural(key string, plural bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	cur, exists := e.l10n.keys[key]
	if !exists {
		return fmt.Errorf("editor: localization key %q not found", key)
	}
	if cur == plural {
		return nil
	}
	next := e.l10n.clone()
	next.keys[key] = plural
	for _, loc := range next.locales {
		if plural {
			if txt := next.strings[loc][key]; txt != "" {
				if next.plurals[loc] == nil {
					next.plurals[loc] = make(map[string]l10n.PluralForms)
				}
				next.plurals[loc][key] = l10n.PluralForms{Other: txt}
			}
			delete(next.strings[loc], key)
		} else {
			forms := next.plurals[loc][key]
			txt := forms.Other
			if txt == "" {
				txt = forms.One
			}
			if txt != "" {
				if next.strings[loc] == nil {
					next.strings[loc] = make(map[string]string)
				}
				next.strings[loc][key] = txt
			}
			delete(next.plurals[loc], key)
		}
	}
	e.swapLocalization("Toggle plural "+key, next)
	return nil
}

// SetLocalizationString sets the localized string for (locale, key) as an undoable command. Setting an empty string clears the translation so it is flagged as missing and omitted on save (allowing runtime fallback).
func (e *Editor) SetLocalizationString(locale, key, value string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	loc := l10n.NormalizeLocale(locale)
	if !e.l10n.hasLocale(loc) {
		return fmt.Errorf("editor: locale %q not found", locale)
	}
	isPlural, exists := e.l10n.keys[key]
	if !exists {
		return fmt.Errorf("editor: localization key %q not found", key)
	}
	if isPlural {
		return fmt.Errorf("editor: localization key %q is plural; use SetLocalizationPlural", key)
	}
	if e.l10n.strings[loc][key] == value {
		return nil
	}
	next := e.l10n.clone()
	if next.strings[loc] == nil {
		next.strings[loc] = make(map[string]string)
	}
	if value == "" {
		delete(next.strings[loc], key)
	} else {
		next.strings[loc][key] = value
	}
	e.swapLocalization("Edit "+key+" ("+loc+")", next)
	return nil
}

// SetLocalizationPlural sets the CLDR plural forms for (locale, key) as an undoable command. Setting an empty PluralForms clears the translation so it is flagged as missing and omitted on save.
func (e *Editor) SetLocalizationPlural(locale, key string, forms l10n.PluralForms) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	loc := l10n.NormalizeLocale(locale)
	if !e.l10n.hasLocale(loc) {
		return fmt.Errorf("editor: locale %q not found", locale)
	}
	isPlural, exists := e.l10n.keys[key]
	if !exists {
		return fmt.Errorf("editor: localization key %q not found", key)
	}
	if !isPlural {
		return fmt.Errorf("editor: localization key %q is not plural; use SetLocalizationString", key)
	}
	if e.l10n.plurals[loc][key] == forms {
		return nil
	}
	next := e.l10n.clone()
	if next.plurals[loc] == nil {
		next.plurals[loc] = make(map[string]l10n.PluralForms)
	}
	if forms == (l10n.PluralForms{}) {
		delete(next.plurals[loc], key)
	} else {
		next.plurals[loc][key] = forms
	}
	e.swapLocalization("Edit plural "+key+" ("+loc+")", next)
	return nil
}

// SetLocalizationPluralForm sets one CLDR plural category ("zero", "one", "two", "few", "many", or "other") of (locale, key) as an undoable command.
func (e *Editor) SetLocalizationPluralForm(locale, key, category, value string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.l10n == nil {
		return ErrNoLocalization
	}
	loc := l10n.NormalizeLocale(locale)
	if !e.l10n.hasLocale(loc) {
		return fmt.Errorf("editor: locale %q not found", locale)
	}
	isPlural, exists := e.l10n.keys[key]
	if !exists {
		return fmt.Errorf("editor: localization key %q not found", key)
	}
	if !isPlural {
		return fmt.Errorf("editor: localization key %q is not plural", key)
	}
	forms := e.l10n.plurals[loc][key]
	switch category {
	case "zero":
		forms.Zero = value
	case "one":
		forms.One = value
	case "two":
		forms.Two = value
	case "few":
		forms.Few = value
	case "many":
		forms.Many = value
	case "other":
		forms.Other = value
	default:
		return fmt.Errorf("editor: unknown CLDR plural category %q", category)
	}
	return e.SetLocalizationPlural(loc, key, forms)
}

// LocalizationTable returns the canonical l10n.StringTable snapshot for locale.
func (e *Editor) LocalizationTable(locale string) (*l10n.StringTable, error) {
	if e == nil || e.l10n == nil {
		return nil, ErrNoLocalization
	}
	loc := l10n.NormalizeLocale(locale)
	if !e.l10n.hasLocale(loc) {
		return nil, fmt.Errorf("editor: locale %q not found", locale)
	}
	return e.l10n.tableFor(loc), nil
}

// SaveLocalizationYAML encodes locale's string table as deterministic canonical YAML.
func (e *Editor) SaveLocalizationYAML(locale string) ([]byte, error) {
	t, err := e.LocalizationTable(locale)
	if err != nil {
		return nil, err
	}
	return l10n.EncodeYAML(t)
}

// LocalizationDirty reports whether any locale table in the open workspace differs from what was opened or last saved.
func (e *Editor) LocalizationDirty() bool {
	if e == nil || e.l10n == nil {
		return false
	}
	if len(e.l10n.locales) != len(e.l10n.saved) {
		return true
	}
	for _, loc := range e.l10n.locales {
		prev, ok := e.l10n.saved[loc]
		if !ok {
			return true
		}
		cur, err := l10n.EncodeYAML(e.l10n.tableFor(loc))
		if err != nil || !bytes.Equal(cur, prev) {
			return true
		}
	}
	return false
}

// SaveLocalizations writes each locale's canonical YAML file to <LocalizationDir>/<locale>.yaml and removes files for locales that were loaded from disk and subsequently removed.
func (e *Editor) SaveLocalizations() error {
	if e == nil || e.l10n == nil {
		return ErrNoLocalization
	}
	return e.SaveLocalizationsTo(e.l10n.dir)
}

// SaveLocalizationsTo writes each locale's canonical YAML file to <dir>/<locale>.yaml and sets dir as the workspace's localization directory.
func (e *Editor) SaveLocalizationsTo(dir string) error {
	if e == nil || e.l10n == nil {
		return ErrNoLocalization
	}
	if dir == "" {
		return errors.New("editor: no localization directory set")
	}
	encoded := make(map[string][]byte, len(e.l10n.locales))
	for _, loc := range e.l10n.locales {
		data, err := l10n.EncodeYAML(e.l10n.tableFor(loc))
		if err != nil {
			return fmt.Errorf("editor: encode locale %q: %w", loc, err)
		}
		encoded[loc] = data
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, loc := range e.l10n.locales {
		target := filepath.Join(dir, loc+".yaml")
		if err := writeFileAtomic(target, encoded[loc]); err != nil {
			return err
		}
	}
	// Remove files for locales that previously existed in dir and were removed.
	if dir == e.l10n.dir {
		for oldLoc := range e.l10n.saved {
			if !e.l10n.hasLocale(oldLoc) {
				if err := os.Remove(filepath.Join(dir, oldLoc+".yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	e.l10n.dir = dir
	e.l10n.saved = encoded
	e.l10n.lastErr = nil
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = fmt.Sprintf("Saved %d locale file(s) to %s", len(e.l10n.locales), dir)
	e.notify()
	return nil
}

func normalizeEditorLocale(raw string) (string, error) {
	loc := l10n.NormalizeLocale(raw)
	if loc == "" {
		return "", fmt.Errorf("editor: empty locale %q", raw)
	}
	for sub := range strings.SplitSeq(loc, "-") {
		if len(sub) == 0 || len(sub) > 8 {
			return "", fmt.Errorf("editor: locale %q is not a valid BCP-47 tag", raw)
		}
		for i := range len(sub) {
			c := sub[i]
			if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
				return "", fmt.Errorf("editor: locale %q is not a valid BCP-47 tag", raw)
			}
		}
	}
	return loc, nil
}

func validateLocalizationKey(key string) error {
	if key == "" {
		return errors.New("editor: localization key must not be empty")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("editor: localization key %q must not have surrounding spaces", key)
	}
	if strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return fmt.Errorf("editor: localization key %q must not contain control characters", key)
	}
	return nil
}

// LocalizationPanelView renders the Localization table editor below the viewport while a localization workspace is open: keys as rows, project locales as columns, missing translation highlights, CLDR plural form editors, adding/removing keys and locales, and saving each locale's YAML file.
func LocalizationPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.LocalizationOpen()
		},
		Then: func(cx *core.Scope) core.Node { return localizationPanel(cx, ed, rev) },
	}
}

func localizationPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	newKey := core.NewSignal(cx, "")
	newKeyPlural := core.NewSignal(cx, false)
	newLocale := core.NewSignal(cx, "")
	saveShortcut := FormatShortcut(ed.EffectiveShortcut(CmdSave), cx.Env().OS())

	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	// structure changes when locales, keys, loading state, theme appearance, or key plural modes change, so the table remounts then and not on every keystroke inside a translation input.
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.LocalizationOpen() {
			return ""
		}
		var b strings.Builder
		if cx.Dark() {
			b.WriteString("D|")
		} else {
			b.WriteString("L|")
		}
		if ed.LocalizationLoading() {
			b.WriteString("loading|")
		}
		b.WriteString("L:")
		for _, loc := range ed.LocalizationLocales() {
			b.WriteString(loc)
			b.WriteByte(',')
		}
		b.WriteString("|K:")
		for _, k := range ed.LocalizationKeys() {
			b.WriteString(k)
			if ed.LocalizationKeyPlural(k) {
				b.WriteString(":p")
			} else {
				b.WriteString(":s")
			}
			b.WriteByte(',')
		}
		return b.String()
	})

	// Memoize missing-presence so unrelated rev ticks do not remount the status pill icon.
	hasMissing := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.LocalizationMissingCount() > 0
	})

	missingPill := core.View{
		StyleFunc: func() core.Style {
			bg := p.Added.Fill
			border := p.Added.Edge
			if hasMissing.Get() {
				bg = p.Warning.Fill
				border = p.Warning.Edge
			}
			return core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          specSpace4,
				Height:       core.Pt(24),
				Padding:      core.Symmetric(0, specSpace8),
				CornerRadius: specRadiusControl,
				Background:   bg,
				BorderWidth:  1,
				BorderColor:  border,
				FlexShrink:   0,
			}
		},
		Children: []core.Node{
			specDynamicIcon(
				func() *core.SVG {
					if hasMissing.Get() {
						return lucide.TriangleAlert()
					}
					return lucide.CircleCheck()
				},
				13,
				func() core.Color {
					if hasMissing.Get() {
						return p.Warning.Fg
					}
					return p.Added.Fg
				},
			),
			core.Text{
				TestID: "l10n-missing-count",
				ContentFunc: func() string {
					_ = rev.Get()
					n := ed.LocalizationMissingCount()
					if n == 1 {
						return "1 missing"
					}
					return fmt.Sprintf("%d missing", n)
				},
				StyleFunc: func() core.Style {
					col := p.Added.Fg
					if hasMissing.Get() {
						col = p.Warning.Fg
					}
					return core.Style{FontSize: 11.5, FontWeight: core.FontWeightSemibold, Color: col}
				},
			},
		},
	}

	header := specPanelHeader(p,
		specPanelTitle(p, "", lucide.Languages, "Localization", nil),
		missingPill,
		core.View{
			Style: core.Style{Width: core.Pt(144)},
			Children: []core.Node{ui.Input{
				TestID:      "l10n-add-key-input",
				Label:       "New Key",
				Placeholder: "menu.play",
				Style:       specInputStyle(p),
				Disabled:    disabled,
				Value:       newKey.Get,
				OnChange:    newKey.Set,
			}},
		},
		specPanelToggle(p, specPanelToggleSpec{
			TestID:   "l10n-add-key-plural",
			Text:     "Plural",
			Label:    "CLDR plural key",
			Tooltip:  "Create a CLDR plural key (zero, one, two, few, many, other)",
			Disabled: disabled,
			On:       newKeyPlural.Get,
			OnChange: newKeyPlural.Set,
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:  "l10n-add-key",
			Text:    "Add Key",
			Icon:    lucide.Plus,
			Label:   "Add Key",
			Tooltip: "Add translation key row",
			Variant: specBtnSecondary,
			Compact: true,
			Disabled: func() bool {
				return disabled() || strings.TrimSpace(newKey.Get()) == ""
			},
			OnPress: func() {
				if err := ed.AddLocalizationKey(newKey.Get(), newKeyPlural.Get()); err != nil {
					ed.reportLocalizationError(err)
					return
				}
				newKey.Set("")
				newKeyPlural.Set(false)
			},
		}),
		core.View{
			Style: core.Style{Width: core.Pt(88)},
			Children: []core.Node{ui.Input{
				TestID:      "l10n-add-locale-input",
				Label:       "New Locale",
				Placeholder: "ja",
				Style:       specInputStyle(p),
				Disabled:    disabled,
				Value:       newLocale.Get,
				OnChange:    newLocale.Set,
			}},
		},
		specPanelButton(p, specPanelButtonSpec{
			TestID:  "l10n-add-locale",
			Text:    "Add Locale",
			Icon:    lucide.Globe,
			Label:   "Add Locale",
			Tooltip: "Add BCP-47 locale column",
			Variant: specBtnSecondary,
			Compact: true,
			Disabled: func() bool {
				return disabled() || strings.TrimSpace(newLocale.Get()) == ""
			},
			OnPress: func() {
				if err := ed.AddLocalizationLocale(newLocale.Get()); err != nil {
					ed.reportLocalizationError(err)
					return
				}
				newLocale.Set("")
			},
		}),
		core.View{Style: core.Style{FlexGrow: 1}},
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "l10n-save",
			Text:     "Save Strings",
			Icon:     lucide.Save,
			Label:    "Save Strings",
			Tooltip:  specShortcutTip("Save Strings", saveShortcut),
			Variant:  specBtnQuiet,
			Compact:  true,
			Disabled: disabled,
			OnPress:  func() { ed.reportLocalizationError(ed.SaveLocalizations()) },
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "l10n-close",
			Icon:     lucide.X,
			Label:    "Close Localization",
			Tooltip:  specShortcutTip("Close Localization", "Esc"),
			Variant:  specBtnGhost,
			Compact:  true,
			Disabled: disabled,
			OnPress:  func() { ed.reportLocalizationError(ed.CloseLocalizations()) },
		}),
	)

	errorBanner := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.LocalizationError() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			dismissBtn := specPanelButton(p, specPanelButtonSpec{
				TestID:  "l10n-error-dismiss",
				Icon:    lucide.X,
				Label:   "Dismiss localization error",
				Tooltip: "Dismiss error",
				Variant: specBtnGhost,
				Compact: true,
				OnPress: ed.ClearLocalizationError,
			})
			return core.View{
				Style: core.Style{Padding: core.Edges{Top: specSpace8, Left: specSpace16, Right: specSpace16}, FlexShrink: 0},
				Children: []core.Node{
					specStatusBanner(p, "l10n-error", false, "", func() string {
						_ = rev.Get()
						if err := ed.LocalizationError(); err != nil {
							return err.Error()
						}
						return ""
					}, dismissBtn),
				},
			}
		},
	}

	table := ui.ScrollView{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
		ContentStyle: core.Style{
			Padding: core.Symmetric(specSpace8, specSpace16),
			Gap:     specSpace8,
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if structure.Get() == "" {
				return nil
			}
			p := ed.SpecPalette(cx)
			if ed.LocalizationLoading() {
				return specLoadingState(p, "l10n-loading", "Loading localization tables…")
			}
			locales := ed.LocalizationLocales()
			keys := ed.LocalizationKeys()
			if len(locales) == 0 && len(keys) == 0 {
				return specEmptyState(p, "l10n-empty", lucide.Languages, p.TextFaint, "No locales or keys yet", "Add a locale and key above to begin editing translations.", nil)
			}

			// Column headers: Key + one column per locale.
			colHeaders := make([]core.Node, 0, len(locales)+1)
			colHeaders = append(colHeaders, core.View{
				Style: core.Style{Width: core.Pt(188), Padding: core.Symmetric(0, specSpace4)},
				Children: []core.Node{
					specSectionLabel(p, "KEY"),
				},
			})
			for _, locale := range locales {
				loc := locale
				colHeaders = append(colHeaders, core.View{
					TestID: "l10n-col-" + loc,
					Style: core.Style{
						FlexGrow:       1,
						FlexBasis:      core.Pt(188),
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Padding:        core.Symmetric(0, specSpace8),
						Gap:            specSpace8,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace4},
							Children: []core.Node{
								specIcon(lucide.Globe, 13, p.TextFaint),
								core.Text{
									Content: loc,
									Style: core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   11.5,
										FontWeight: core.FontWeightSemibold,
										Color:      p.TextPrimary,
									},
								},
							},
						},
						specPanelButton(p, specPanelButtonSpec{
							TestID:     "l10n-remove-locale-" + loc,
							Icon:       lucide.X,
							Label:      "Remove locale " + loc,
							Tooltip:    "Remove locale " + loc,
							Variant:    specBtnGhost,
							Compact:    true,
							InsetFocus: true,
							Disabled:   disabled,
							OnPress:    func() { ed.reportLocalizationError(ed.RemoveLocalizationLocale(loc)) },
						}),
					},
				})
			}

			rows := make([]core.Node, 0, len(keys)+2)
			rows = append(rows, core.View{
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         specSpace8,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Line,
					Padding:     core.Edges{Bottom: specSpace4},
				},
				Children: colHeaders,
			})

			if len(keys) == 0 {
				rows = append(rows, specEmptyState(p, "l10n-empty", lucide.Languages, p.TextFaint, "No translation keys yet", "Enter a key above and press Add Key to start translating.", nil))
				return core.View{Style: core.Style{Gap: specSpace8}, Children: rows}
			}

			for _, keyName := range keys {
				k := keyName
				isPlural := ed.LocalizationKeyPlural(k)
				cells := make([]core.Node, 0, len(locales)+1)

				keyLabelNodes := []core.Node{
					core.Text{
						Content: k,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							FontWeight: core.FontWeightSemibold,
							Color:      p.TextPrimary,
						},
					},
				}
				if isPlural {
					keyLabelNodes = append(keyLabelNodes, core.View{
						Style: core.Style{
							Height:         core.Pt(20),
							Padding:        core.Symmetric(0, specSpace8),
							CornerRadius:   specRadiusControl,
							Background:     p.Raised,
							BorderWidth:    1,
							BorderColor:    p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								Content: "plural",
								Style: core.Style{
									FontSize:   11,
									FontWeight: core.FontWeightSemibold,
									Color:      p.TextFaint,
								},
							},
						},
					})
				}
				cells = append(cells, core.View{
					Style: core.Style{
						Width:      core.Pt(188),
						Padding:    core.Symmetric(specSpace4, specSpace4),
						Gap:        specSpace8,
						AlignItems: core.AlignStart,
					},
					Children: []core.Node{
						core.View{
							Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8, Wrap: core.Wrap},
							Children: keyLabelNodes,
						},
						specPanelButton(p, specPanelButtonSpec{
							TestID:     "l10n-remove-key-" + k,
							Text:       "Remove",
							Icon:       lucide.Trash,
							Label:      "Remove key " + k,
							Tooltip:    "Remove key " + k,
							Variant:    specBtnGhost,
							Compact:    true,
							InsetFocus: true,
							Disabled:   disabled,
							OnPress:    func() { ed.reportLocalizationError(ed.RemoveLocalizationKey(k)) },
						}),
					},
				})

				for _, locale := range locales {
					loc := locale
					cells = append(cells, localizationCell(cx, p, ed, rev, loc, k, isPlural, disabled))
				}

				rows = append(rows, core.View{
					TestID: "l10n-row-" + k,
					Style: core.Style{
						Direction:   core.Row,
						AlignItems:  core.AlignStart,
						Gap:         specSpace8,
						Padding:     core.Symmetric(specSpace8, 0),
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Line,
					},
					Children: cells,
				})
			}

			return core.View{Style: core.Style{Gap: specSpace4}, Children: rows}
		})},
	}

	return specPanelContainer(p, "l10n-panel", "Localization", 272, header, errorBanner, table)
}

func localizationCell(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], loc, key string, isPlural bool, disabled func() bool) core.Node {
	isMissing := func() bool {
		_ = rev.Get()
		return ed.LocalizationIsMissing(loc, key)
	}

	// Missing translation badge placed at the top of the cell with warning token + alert icon (never color alone).
	editors := []core.Node{
		core.Show{
			When: isMissing,
			Then: func(cx *core.Scope) core.Node {
				p := ed.SpecPalette(cx)
				return core.View{
					TestID: "l10n-missing-" + loc + "-" + key,
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        specSpace4,
					},
					Children: []core.Node{
						specIcon(lucide.TriangleAlert, 13, p.Warning.Fg),
						core.Text{
							Content: "Missing translation",
							Style: core.Style{
								FontSize:   11,
								FontWeight: core.FontWeightSemibold,
								Color:      p.Warning.Fg,
							},
						},
					},
				}
			},
		},
	}

	if !isPlural {
		editors = append(editors, ui.Input{
			TestID:      "l10n-input-" + loc + "-" + key,
			Label:       fmt.Sprintf("%s (%s)", key, loc),
			Placeholder: "Missing translation",
			Style:       specInputStyle(p),
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				v, _ := ed.LocalizationValue(loc, key)
				return v
			},
			OnChange: func(text string) {
				ed.reportLocalizationError(ed.SetLocalizationString(loc, key, text))
			},
		})
	} else {
		for _, category := range pluralCategories {
			cat := category
			editors = append(editors, core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
				},
				Children: []core.Node{
					core.Text{
						Content: cat,
						Style: core.Style{
							Width:      core.Pt(40),
							FontFamily: core.FontMonospace,
							FontSize:   11,
							FontWeight: core.FontWeightSemibold,
							Color:      p.TextFaint,
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{ui.Input{
							TestID:      fmt.Sprintf("l10n-plural-%s-%s-%s", loc, key, cat),
							Label:       fmt.Sprintf("%s %s (%s)", key, cat, loc),
							Placeholder: cat,
							Style:       specInputStyle(p),
							Disabled:    disabled,
							Value: func() string {
								_ = rev.Get()
								forms, _ := ed.LocalizationPlural(loc, key)
								return pluralFormField(forms, cat)
							},
							OnChange: func(text string) {
								ed.reportLocalizationError(ed.SetLocalizationPluralForm(loc, key, cat, text))
							},
						}},
					},
				},
			})
		}
	}

	return core.View{
		TestID: "l10n-cell-" + loc + "-" + key,
		StyleFunc: func() core.Style {
			bg := p.Raised
			border := p.LineRaised
			if isMissing() {
				bg = p.Warning.Fill
				border = p.Warning.Edge
			}
			return core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(188),
				Padding:      core.All(specSpace8),
				Gap:          specSpace4,
				CornerRadius: specRadiusButton,
				BorderWidth:  1,
				BorderColor:  border,
				Background:   bg,
			}
		},
		Children: editors,
	}
}

func pluralFormField(forms l10n.PluralForms, category string) string {
	switch category {
	case "zero":
		return forms.Zero
	case "one":
		return forms.One
	case "two":
		return forms.Two
	case "few":
		return forms.Few
	case "many":
		return forms.Many
	case "other":
		return forms.Other
	default:
		return ""
	}
}
