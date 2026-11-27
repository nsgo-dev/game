// Command cldrplurals generates integer-only Unicode CLDR cardinal plural rules
// and CLDR sample-integer tests for github.com/nsgo-dev/game/pkg/l10n from a
// pinned CLDR common/supplemental/plurals.xml file.
//
// Run from tools/:
//
//	go run ./cldrplurals
//	go run ./cldrplurals -in /path/to/plurals.xml
//	go run ./cldrplurals -check
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	cldrVersion = "48.2"
	cldrURL     = "https://raw.githubusercontent.com/unicode-org/cldr/release-48-2/common/supplemental/plurals.xml"
	cldrSHA256  = "d701d8b461afd2ba5eb21f9c1d645c8a661de225596afcf0f492ee36c69d727d"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cldrplurals: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("cldrplurals", flag.ContinueOnError)
	inPath := fs.String("in", "", "path to a local CLDR common/supplemental/plurals.xml (default: fetch pinned CLDR "+cldrVersion+")")
	outDir := fs.String("out", "", "output directory for generated files (default: <repo>/pkg/l10n)")
	check := fs.Bool("check", false, "verify committed generated files match without rewriting")
	verifyHash := fs.Bool("verify-hash", true, "verify SHA-256 of plurals.xml against pinned CLDR "+cldrVersion)
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := *outDir
	if dir == "" {
		repoRoot, err := findRepoRoot()
		if err != nil {
			return err
		}
		dir = filepath.Join(repoRoot, "pkg", "l10n")
	}

	rawXML, err := loadPluralsXML(*inPath, *verifyHash)
	if err != nil {
		return err
	}

	groups, err := parsePluralsXML(rawXML)
	if err != nil {
		return err
	}

	genCode, genTest, err := generate(groups, cldrVersion)
	if err != nil {
		return err
	}

	targets := []struct {
		name string
		data []byte
	}{
		{"plurals_cldr_gen.go", genCode},
		{"plurals_cldr_gen_test.go", genTest},
	}

	if *check {
		for _, t := range targets {
			p := filepath.Join(dir, t.name)
			existing, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read %s: %w", p, err)
			}
			if !bytes.Equal(existing, t.data) {
				return fmt.Errorf("%s is out of date; run (cd tools && go run ./cldrplurals)", p)
			}
		}
		fmt.Fprintf(stdout, "cldrplurals: generated files in %s match CLDR %s\n", dir, cldrVersion)
		return nil
	}

	for _, t := range targets {
		p := filepath.Join(dir, t.name)
		if err := os.WriteFile(p, t.data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	fmt.Fprintf(stdout, "cldrplurals: wrote generated files to %s for CLDR %s\n", dir, cldrVersion)
	return nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "pkg", "l10n", "catalog.go")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not locate repository root containing pkg/l10n/catalog.go")
		}
	}
}

func loadPluralsXML(inPath string, verifyHash bool) ([]byte, error) {
	var data []byte
	var err error
	if inPath != "" {
		data, err = os.ReadFile(inPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", inPath, err)
		}
	} else {
		resp, err := http.Get(cldrURL) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", cldrURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: HTTP %s", cldrURL, resp.Status)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response %s: %w", cldrURL, err)
		}
	}
	if verifyHash {
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if got != cldrSHA256 {
			return nil, fmt.Errorf("SHA-256 mismatch: got %s, want %s (pass -verify-hash=false when updating CLDR version)", got, cldrSHA256)
		}
	}
	return data, nil
}

type xmlSupplementalData struct {
	Plurals []xmlPlurals `xml:"plurals"`
}

type xmlPlurals struct {
	Type        string           `xml:"type,attr"`
	PluralRules []xmlPluralRules `xml:"pluralRules"`
}

type xmlPluralRules struct {
	Locales string          `xml:"locales,attr"`
	Rules   []xmlPluralRule `xml:"pluralRule"`
}

type xmlPluralRule struct {
	Count string `xml:"count,attr"`
	Body  string `xml:",chardata"`
}

// pluralCategoryOrder is the canonical evaluation order in CLDR.
var pluralCategoryOrder = []string{"zero", "one", "two", "few", "many", "other"}

var categoryConstName = map[string]string{
	"zero":  "PluralZero",
	"one":   "PluralOne",
	"two":   "PluralTwo",
	"few":   "PluralFew",
	"many":  "PluralMany",
	"other": "PluralOther",
}

type parsedRule struct {
	Category   string
	CondExpr   string // Go boolean expression on n uint64; "" means statically false; "true" means always true
	HasIntSamp bool
	Samples    []int
}

type parsedGroup struct {
	Locales []string // normalized BCP-47 locale tags
	Rules   []parsedRule
}

func parsePluralsXML(data []byte) ([]parsedGroup, error) {
	var doc xmlSupplementalData
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode XML: %w", err)
	}
	var cardinal *xmlPlurals
	for i := range doc.Plurals {
		if doc.Plurals[i].Type == "cardinal" {
			cardinal = &doc.Plurals[i]
			break
		}
	}
	if cardinal == nil {
		return nil, errors.New("no <plurals type=\"cardinal\"> element found")
	}

	var groups []parsedGroup
	seenLocales := make(map[string]bool)
	for _, pr := range cardinal.PluralRules {
		rawLocales := strings.Fields(pr.Locales)
		if len(rawLocales) == 0 {
			return nil, errors.New("pluralRules element with empty locales attribute")
		}
		locales := make([]string, 0, len(rawLocales))
		for _, loc := range rawLocales {
			norm := normalizeLocale(loc)
			if seenLocales[norm] {
				return nil, fmt.Errorf("duplicate locale %q in plurals.xml", norm)
			}
			seenLocales[norm] = true
			locales = append(locales, norm)
		}
		slices.Sort(locales)

		rulesByCat := make(map[string]parsedRule, len(pr.Rules))
		for _, r := range pr.Rules {
			if _, ok := categoryConstName[r.Count]; !ok {
				return nil, fmt.Errorf("unknown plural category %q for locales %v", r.Count, locales)
			}
			condText, intSamples, err := splitRuleAndIntegerSamples(r.Body)
			if err != nil {
				return nil, fmt.Errorf("locales %v count=%q: %w", locales, r.Count, err)
			}
			var condExpr string
			if r.Count == "other" {
				condExpr = "true"
			} else {
				condExpr, err = compileIntegerCondition(condText)
				if err != nil {
					return nil, fmt.Errorf("locales %v count=%q condition %q: %w", locales, r.Count, condText, err)
				}
			}
			// Sanity check: integer samples and simplified condition must agree on reachability.
			if r.Count != "other" {
				if len(intSamples) > 0 && condExpr == "" {
					return nil, fmt.Errorf("locales %v count=%q has @integer samples %v but condition simplified to false", locales, r.Count, intSamples)
				}
				if len(intSamples) == 0 && condExpr != "" {
					return nil, fmt.Errorf("locales %v count=%q has no @integer samples but condition %q did not simplify to false", locales, r.Count, condExpr)
				}
			}
			rulesByCat[r.Count] = parsedRule{
				Category:   r.Count,
				CondExpr:   condExpr,
				HasIntSamp: len(intSamples) > 0,
				Samples:    intSamples,
			}
		}

		ordered := make([]parsedRule, 0, len(rulesByCat))
		for _, cat := range pluralCategoryOrder {
			if r, ok := rulesByCat[cat]; ok {
				ordered = append(ordered, r)
			}
		}
		groups = append(groups, parsedGroup{
			Locales: locales,
			Rules:   ordered,
		})
	}
	return groups, nil
}

// splitRuleAndIntegerSamples splits a CLDR pluralRule body into its condition
// expression (everything before '@') and its expanded @integer sample list.
func splitRuleAndIntegerSamples(body string) (string, []int, error) {
	condPart := body
	rest := ""
	if idx := strings.IndexByte(body, '@'); idx >= 0 {
		condPart = strings.TrimSpace(body[:idx])
		rest = body[idx:]
	} else {
		condPart = strings.TrimSpace(condPart)
	}

	var samples []int
	if idx := strings.Index(rest, "@integer"); idx >= 0 {
		intPart := rest[idx+len("@integer"):]
		if decIdx := strings.IndexByte(intPart, '@'); decIdx >= 0 {
			intPart = intPart[:decIdx]
		}
		var err error
		samples, err = parseIntegerSamples(intPart)
		if err != nil {
			return "", nil, err
		}
	}
	return condPart, samples, nil
}

func parseIntegerSamples(s string) ([]int, error) {
	var out []int
	seen := make(map[int]bool)
	add := func(v int) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" || tok == "…" || tok == "..." {
			continue
		}
		if loStr, hiStr, ok := strings.Cut(tok, "~"); ok {
			lo, err := parseSampleNumber(strings.TrimSpace(loStr))
			if err != nil {
				return nil, fmt.Errorf("sample range %q: %w", tok, err)
			}
			hi, err := parseSampleNumber(strings.TrimSpace(hiStr))
			if err != nil {
				return nil, fmt.Errorf("sample range %q: %w", tok, err)
			}
			if hi < lo || hi-lo > 1000 {
				return nil, fmt.Errorf("unexpected sample range %q", tok)
			}
			for v := lo; v <= hi; v++ {
				add(v)
			}
			continue
		}
		v, err := parseSampleNumber(tok)
		if err != nil {
			return nil, fmt.Errorf("sample %q: %w", tok, err)
		}
		add(v)
	}
	return out, nil
}

// parseSampleNumber parses a CLDR integer sample token such as "100" or compact
// exponent notation "1c6" (= 1_000_000) / "2e6".
func parseSampleNumber(tok string) (int, error) {
	if idx := strings.IndexAny(tok, "ce"); idx >= 0 {
		base, err := strconv.Atoi(tok[:idx])
		if err != nil {
			return 0, err
		}
		exp, err := strconv.Atoi(tok[idx+1:])
		if err != nil {
			return 0, err
		}
		val := int64(base)
		for range exp {
			val *= 10
		}
		if val > math.MaxInt32 {
			return 0, fmt.Errorf("sample %q overflows 32-bit int", tok)
		}
		return int(val), nil
	}
	v, err := strconv.Atoi(tok)
	if err != nil {
		return 0, err
	}
	return v, nil
}

type intRange struct {
	lo, hi uint64
}

func (r intRange) contains(v uint64) bool {
	return v >= r.lo && v <= r.hi
}

// compileIntegerCondition parses a CLDR plural rule condition and specializes it
// for non-negative integers (n = i, and decimal/compact operands v = w = f = t = c = e = 0).
// It returns "" if the condition is statically false for all integers, "true" if
// statically true, or a parenthesized Go boolean expression over n uint64.
func compileIntegerCondition(cond string) (string, error) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return "true", nil
	}
	orParts := splitKeyword(cond, "or")
	var disjuncts []string
	for _, orPart := range orParts {
		andParts := splitKeyword(orPart, "and")
		var conjuncts []string
		clauseFalse := false
		for _, relText := range andParts {
			expr, staticVal, isStatic, err := compileIntegerRelation(relText)
			if err != nil {
				return "", err
			}
			if isStatic {
				if !staticVal {
					clauseFalse = true
					break
				}
				continue
			}
			conjuncts = append(conjuncts, expr)
		}
		if clauseFalse {
			continue
		}
		if len(conjuncts) == 0 {
			return "true", nil
		}
		joined := strings.Join(conjuncts, " && ")
		if len(conjuncts) > 1 && len(orParts) > 1 {
			joined = "(" + joined + ")"
		}
		disjuncts = append(disjuncts, joined)
	}
	if len(disjuncts) == 0 {
		return "", nil
	}
	return strings.Join(disjuncts, " || "), nil
}

func splitKeyword(s, kw string) []string {
	tokens := strings.Fields(s)
	var parts []string
	var cur []string
	for _, tok := range tokens {
		if tok == kw {
			parts = append(parts, strings.Join(cur, " "))
			cur = cur[:0]
			continue
		}
		cur = append(cur, tok)
	}
	if len(cur) > 0 {
		parts = append(parts, strings.Join(cur, " "))
	}
	return parts
}

func compileIntegerRelation(rel string) (expr string, staticVal bool, isStatic bool, err error) {
	var op string
	var lhsStr, rhsStr string
	if l, r, ok := strings.Cut(rel, "!="); ok {
		op, lhsStr, rhsStr = "!=", strings.TrimSpace(l), strings.TrimSpace(r)
	} else if l, r, ok := strings.Cut(rel, "="); ok {
		op, lhsStr, rhsStr = "=", strings.TrimSpace(l), strings.TrimSpace(r)
	} else {
		return "", false, false, fmt.Errorf("relation %q missing = or !=", rel)
	}

	lhsTokens := strings.Fields(lhsStr)
	if len(lhsTokens) == 0 {
		return "", false, false, fmt.Errorf("empty LHS in relation %q", rel)
	}
	operand := lhsTokens[0]
	var mod uint64
	switch len(lhsTokens) {
	case 1:
	case 3:
		if lhsTokens[1] != "%" && lhsTokens[1] != "mod" {
			return "", false, false, fmt.Errorf("unexpected operator %q in %q", lhsTokens[1], rel)
		}
		mod, err = strconv.ParseUint(lhsTokens[2], 10, 64)
		if err != nil || mod == 0 {
			return "", false, false, fmt.Errorf("invalid modulus %q in %q", lhsTokens[2], rel)
		}
	default:
		return "", false, false, fmt.Errorf("unexpected LHS %q in %q", lhsStr, rel)
	}

	ranges, err := parseRangeList(rhsStr)
	if err != nil {
		return "", false, false, fmt.Errorf("relation %q: %w", rel, err)
	}

	switch operand {
	case "v", "w", "f", "t", "c", "e":
		// For integers, all fraction and compact exponent operands are 0 (and 0 % mod == 0).
		in := false
		for _, rg := range ranges {
			if rg.contains(0) {
				in = true
				break
			}
		}
		if op == "!=" {
			in = !in
		}
		return "", in, true, nil
	case "n", "i":
		lhs := "n"
		if mod != 0 {
			lhs = fmt.Sprintf("n%%%d", mod)
		}
		return formatRangeCondition(lhs, op == "=", ranges), false, false, nil
	default:
		return "", false, false, fmt.Errorf("unknown CLDR operand %q in %q", operand, rel)
	}
}

func parseRangeList(s string) ([]intRange, error) {
	var out []intRange
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if loStr, hiStr, ok := strings.Cut(item, ".."); ok {
			lo, err := strconv.ParseUint(strings.TrimSpace(loStr), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("range %q: %w", item, err)
			}
			hi, err := strconv.ParseUint(strings.TrimSpace(hiStr), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("range %q: %w", item, err)
			}
			out = append(out, intRange{lo: lo, hi: hi})
			continue
		}
		v, err := strconv.ParseUint(item, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value %q: %w", item, err)
		}
		out = append(out, intRange{lo: v, hi: v})
	}
	if len(out) == 0 {
		return nil, errors.New("empty range list")
	}
	return out, nil
}

func formatRangeCondition(lhs string, equal bool, ranges []intRange) string {
	// Collapse adjacent singletons / overlapping ranges to keep generated code clean
	// and canonical (e.g. "i = 0,1" and "n = 0..1" produce identical Go code).
	merged := mergeRanges(ranges)
	var parts []string
	for _, rg := range merged {
		if equal {
			if rg.lo == rg.hi {
				parts = append(parts, fmt.Sprintf("%s == %d", lhs, rg.lo))
			} else if rg.lo == 0 {
				parts = append(parts, fmt.Sprintf("%s <= %d", lhs, rg.hi))
			} else {
				parts = append(parts, fmt.Sprintf("(%s >= %d && %s <= %d)", lhs, rg.lo, lhs, rg.hi))
			}
		} else {
			if rg.lo == rg.hi {
				parts = append(parts, fmt.Sprintf("%s != %d", lhs, rg.lo))
			} else if rg.lo == 0 {
				parts = append(parts, fmt.Sprintf("%s > %d", lhs, rg.hi))
			} else {
				parts = append(parts, fmt.Sprintf("(%s < %d || %s > %d)", lhs, rg.lo, lhs, rg.hi))
			}
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	sep := " || "
	if !equal {
		sep = " && "
	}
	return "(" + strings.Join(parts, sep) + ")"
}

func mergeRanges(in []intRange) []intRange {
	cp := slices.Clone(in)
	slices.SortFunc(cp, func(a, b intRange) int {
		if a.lo != b.lo {
			if a.lo < b.lo {
				return -1
			}
			return 1
		}
		if a.hi < b.hi {
			return -1
		}
		if a.hi > b.hi {
			return 1
		}
		return 0
	})
	out := cp[:0]
	for _, rg := range cp {
		if len(out) == 0 || rg.lo > out[len(out)-1].hi+1 {
			out = append(out, rg)
		} else if rg.hi > out[len(out)-1].hi {
			out[len(out)-1].hi = rg.hi
		}
	}
	return out
}

type compiledRuleSet struct {
	ID         int
	Key        string
	Clauses    []ruleClause
	CatMask    uint8 // bitmask of 1<<PluralCategory reachable by integers
	Locales    []string
	SampleSets []localeSamples
}

type ruleClause struct {
	Category string
	CondExpr string
}

type localeSamples struct {
	Locale  string
	Samples []categorySamples
}

type categorySamples struct {
	Category string
	Values   []int
}

func categoryBit(cat string) uint8 {
	switch cat {
	case "other":
		return 1 << 0
	case "zero":
		return 1 << 1
	case "one":
		return 1 << 2
	case "two":
		return 1 << 3
	case "few":
		return 1 << 4
	case "many":
		return 1 << 5
	default:
		panic("unknown category: " + cat)
	}
}

func generate(groups []parsedGroup, version string) (codeBytes, testBytes []byte, err error) {
	byKey := make(map[string]*compiledRuleSet)
	var ruleSets []*compiledRuleSet

	for _, g := range groups {
		var clauses []ruleClause
		var catMask uint8
		var keyParts []string
		for _, r := range g.Rules {
			if r.Category == "other" {
				if r.HasIntSamp {
					catMask |= categoryBit("other")
				}
				continue
			}
			if r.CondExpr == "" {
				continue
			}
			clauses = append(clauses, ruleClause{Category: r.Category, CondExpr: r.CondExpr})
			catMask |= categoryBit(r.Category)
			keyParts = append(keyParts, r.Category+":"+r.CondExpr)
		}
		key := fmt.Sprintf("mask=%d|%s", catMask, strings.Join(keyParts, ";"))
		rs, ok := byKey[key]
		if !ok {
			rs = &compiledRuleSet{
				Key:     key,
				Clauses: clauses,
				CatMask: catMask,
			}
			byKey[key] = rs
			ruleSets = append(ruleSets, rs)
		}
		rs.Locales = append(rs.Locales, g.Locales...)

		var catSamps []categorySamples
		for _, r := range g.Rules {
			if len(r.Samples) == 0 {
				continue
			}
			catSamps = append(catSamps, categorySamples{Category: r.Category, Values: r.Samples})
		}
		for _, loc := range g.Locales {
			rs.SampleSets = append(rs.SampleSets, localeSamples{Locale: loc, Samples: catSamps})
		}
	}

	// Ensure rule 0 is the "other"-only rule (CLDR root fallback).
	slices.SortFunc(ruleSets, func(a, b *compiledRuleSet) int {
		aRoot := slices.Contains(a.Locales, "root")
		bRoot := slices.Contains(b.Locales, "root")
		if aRoot != bRoot {
			if aRoot {
				return -1
			}
			return 1
		}
		slices.Sort(a.Locales)
		slices.Sort(b.Locales)
		return strings.Compare(a.Locales[0], b.Locales[0])
	})
	for i, rs := range ruleSets {
		rs.ID = i
		slices.Sort(rs.Locales)
	}

	type localeEntry struct {
		locale string
		ruleID int
	}
	var allLocales []localeEntry
	var allSampleSets []localeSamples
	for _, rs := range ruleSets {
		for _, loc := range rs.Locales {
			allLocales = append(allLocales, localeEntry{locale: loc, ruleID: rs.ID})
		}
		allSampleSets = append(allSampleSets, rs.SampleSets...)
	}
	slices.SortFunc(allLocales, func(a, b localeEntry) int {
		return strings.Compare(a.locale, b.locale)
	})
	slices.SortFunc(allSampleSets, func(a, b localeSamples) int {
		return strings.Compare(a.Locale, b.Locale)
	})

	// Emit plurals_cldr_gen.go.
	var cb bytes.Buffer
	fmt.Fprintf(&cb, "// Code generated by tools/cldrplurals from Unicode CLDR %s (common/supplemental/plurals.xml); DO NOT EDIT.\n\n", version)
	cb.WriteString("package l10n\n\n")
	fmt.Fprintf(&cb, "// CLDRVersion is the Unicode CLDR release used to generate the cardinal integer plural rules.\nconst CLDRVersion = %q\n\n", version)

	cb.WriteString("// cldrPluralRuleIndex maps normalized CLDR locale tags to their integer cardinal plural rule ID.\n")
	cb.WriteString("var cldrPluralRuleIndex = map[string]uint8{\n")
	for _, e := range allLocales {
		fmt.Fprintf(&cb, "\t%q: %d,\n", e.locale, e.ruleID)
	}
	cb.WriteString("}\n\n")

	cb.WriteString("// cldrRuleCategoryMasks holds a bitmask (1 << PluralCategory) of CLDR cardinal\n")
	cb.WriteString("// categories reachable by non-negative integer counts for each rule ID.\n")
	fmt.Fprintf(&cb, "var cldrRuleCategoryMasks = [%d]uint8{\n", len(ruleSets))
	for _, rs := range ruleSets {
		fmt.Fprintf(&cb, "\t%d: %#02x, // %s\n", rs.ID, rs.CatMask, strings.Join(rs.Locales, " "))
	}
	cb.WriteString("}\n\n")

	cb.WriteString("// evalCLDRPluralRule evaluates the CLDR integer cardinal plural rule for rule id and n = |count|.\n")
	cb.WriteString("func evalCLDRPluralRule(id uint8, n uint64) PluralCategory {\n")
	cb.WriteString("\tswitch id {\n")
	for _, rs := range ruleSets {
		if len(rs.Clauses) == 0 {
			continue
		}
		fmt.Fprintf(&cb, "\tcase %d: // %s\n", rs.ID, strings.Join(rs.Locales, " "))
		cb.WriteString("\t\tswitch {\n")
		for _, cl := range rs.Clauses {
			fmt.Fprintf(&cb, "\t\tcase %s:\n\t\t\treturn %s\n", cl.CondExpr, categoryConstName[cl.Category])
		}
		cb.WriteString("\t\t}\n")
	}
	cb.WriteString("\t}\n")
	cb.WriteString("\treturn PluralOther\n")
	cb.WriteString("}\n")

	codeBytes, err = format.Source(cb.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("gofmt plurals_cldr_gen.go: %w\n%s", err, cb.String())
	}

	// Emit plurals_cldr_gen_test.go.
	var tb bytes.Buffer
	fmt.Fprintf(&tb, "// Code generated by tools/cldrplurals from Unicode CLDR %s (common/supplemental/plurals.xml); DO NOT EDIT.\n\n", version)
	tb.WriteString("package l10n_test\n\n")
	tb.WriteString("import (\n\t\"testing\"\n\n\t\"github.com/nsgo-dev/game/pkg/l10n\"\n)\n\n")
	tb.WriteString("type cldrCategorySample struct {\n\tcategory l10n.PluralCategory\n\tvalues   []int\n}\n\n")
	tb.WriteString("type cldrLocaleSample struct {\n\tlocale  string\n\tsamples []cldrCategorySample\n}\n\n")
	tb.WriteString("var cldrSampleCases = []cldrLocaleSample{\n")
	for _, ls := range allSampleSets {
		fmt.Fprintf(&tb, "\t{locale: %q, samples: []cldrCategorySample{\n", ls.Locale)
		for _, cs := range ls.Samples {
			fmt.Fprintf(&tb, "\t\t{category: l10n.%s, values: []int{", categoryConstName[cs.Category])
			for i, v := range cs.Values {
				if i > 0 {
					tb.WriteString(", ")
				}
				tb.WriteString(strconv.Itoa(v))
			}
			tb.WriteString("}},\n")
		}
		tb.WriteString("\t}},\n")
	}
	tb.WriteString("}\n\n")
	tb.WriteString(`func TestCLDRSampleIntegers(t *testing.T) {
	allForms := l10n.PluralForms{
		Zero:  "zero",
		One:   "one",
		Two:   "two",
		Few:   "few",
		Many:  "many",
		Other: "other",
	}
	for _, lc := range cldrSampleCases {
		cat := l10n.NewCatalog("en")
		cat.SetPlural(lc.locale, "k", allForms)
		for _, cs := range lc.samples {
			for _, n := range cs.values {
				if got := l10n.PluralCategoryFor(lc.locale, n); got != cs.category {
					t.Errorf("PluralCategoryFor(%q, %d) = %s, want %s", lc.locale, n, got, cs.category)
				}
				// Catalog.Plural honors an explicit zero form when n == 0 for every locale;
				// for non-zero integers it must select the exact CLDR category.
				wantForm := cs.category.String()
				if n == 0 {
					wantForm = "zero"
				}
				if got := cat.Plural(lc.locale, "k", n); got != wantForm {
					t.Errorf("Catalog.Plural(%q, k, %d) = %q, want %q", lc.locale, n, got, wantForm)
				}
			}
		}
	}
}
`)

	testBytes, err = format.Source(tb.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("gofmt plurals_cldr_gen_test.go: %w\n%s", err, tb.String())
	}
	return codeBytes, testBytes, nil
}

// normalizeLocale matches pkg/l10n.NormalizeLocale so generated table keys align with runtime lookup.
func normalizeLocale(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	tag = strings.ReplaceAll(tag, "_", "-")
	rawParts := strings.Split(tag, "-")
	parts := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		if p != "" {
			parts = append(parts, p)
		}
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

func isASCIIAlpha(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

func isASCIIDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
