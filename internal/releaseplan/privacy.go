package releaseplan

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// PrivacyManifestName is the Apple privacy manifest file name at a bundle's resource root.
const PrivacyManifestName = "PrivacyInfo.xcprivacy"

// PrivacyManifestRelPath is the project-relative path NSGo packages into every Apple bundle and merges with NSGo's defaults and nsgo.yaml's apple.privacy.
const PrivacyManifestRelPath = "resources/" + PrivacyManifestName

// NSGoPrivacyReasons mirrors NSGo v0.3.4's required-reason API declarations (cmd/nsgo/privacy.go)
// written into every Apple bundle's PrivacyInfo.xcprivacy.
var NSGoPrivacyReasons = map[string][]string{
	"NSPrivacyAccessedAPICategoryUserDefaults":   {"CA92.1"},
	"NSPrivacyAccessedAPICategoryFileTimestamp":  {"C617.1"},
	"NSPrivacyAccessedAPICategorySystemBootTime": {"35F9.1"},
}

// DefaultPrivacyManifestXML returns a deterministic, store-valid PrivacyInfo.xcprivacy XML plist
// containing NSGo's required-reason API declarations, suitable for resources/PrivacyInfo.xcprivacy
// so authors can add app-specific entries and NSGo merges it cleanly without duplicates.
func DefaultPrivacyManifestXML() []byte {
	data, err := MergePrivacyManifest(nil, NSGoPrivacyConfig{})
	if err != nil {
		panic("releaseplan: default privacy manifest invalid: " + err.Error())
	}
	return data
}

// HasNSGoPrivacy reports whether cfg declares any app-specific privacy fields in nsgo.yaml's apple.privacy.
func HasNSGoPrivacy(cfg NSGoPrivacyConfig) bool {
	return len(cfg.AccessedAPIs) > 0 || len(cfg.CollectedData) > 0 || cfg.Tracking != nil || len(cfg.TrackingDomains) > 0
}

// ValidateNSGoPrivacy checks nsgo.yaml's apple.privacy declarations against NSGo's store-privacy rules.
func ValidateNSGoPrivacy(cfg NSGoPrivacyConfig) error {
	const apiPrefix = "NSPrivacyAccessedAPICategory"
	names := make([]string, 0, len(cfg.AccessedAPIs))
	for name := range cfg.AccessedAPIs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		reasons := cfg.AccessedAPIs[name]
		if !strings.HasPrefix(name, apiPrefix) || name == apiPrefix {
			return fmt.Errorf("apple.privacy.accessed_apis: %q is not an NSPrivacyAccessedAPICategory", name)
		}
		if len(reasons) == 0 {
			return fmt.Errorf("apple.privacy.accessed_apis: %s needs its reasons, such as E174.1", name)
		}
		for _, r := range reasons {
			if strings.TrimSpace(r) == "" {
				return fmt.Errorf("apple.privacy.accessed_apis: %s needs its reasons, such as E174.1", name)
			}
		}
	}
	for i, item := range cfg.CollectedData {
		if err := validateCollectedPrivacyData(plistValueMap(item)); err != nil {
			return fmt.Errorf("apple.privacy.collected_data[%d]: %w", i, err)
		}
	}
	for i, domain := range cfg.TrackingDomains {
		if !validTrackingDomain(domain) {
			return fmt.Errorf("apple.privacy.tracking_domains[%d] must be a domain name without a scheme, port, path or query", i)
		}
	}
	return nil
}

// ValidateResourcePrivacy checks a decoded resources/PrivacyInfo.xcprivacy plist dictionary against NSGo's store-privacy rules.
func ValidateResourcePrivacy(manifest map[string]any) error {
	if tracking, exists := manifest["NSPrivacyTracking"]; exists {
		if _, ok := tracking.(bool); !ok {
			return errors.New("NSPrivacyTracking must be a boolean")
		}
	}
	if domains, exists := manifest["NSPrivacyTrackingDomains"]; exists {
		if _, ok := domains.([]any); !ok {
			return errors.New("NSPrivacyTrackingDomains must be an array of domain names")
		}
	}
	if err := validatePrivacyEntries(manifest, "NSPrivacyAccessedAPITypes", validatePrivacyAPI); err != nil {
		return err
	}
	return validatePrivacyEntries(manifest, "NSPrivacyCollectedDataTypes", validateCollectedPrivacyData)
}

// MergePrivacyManifest merges existingXML (a resources/PrivacyInfo.xcprivacy plist, or nil) with NSGo's
// required-reason APIs and nsgo.yaml's apple.privacy, validating both inputs and the merged result
// with NSGo's store-privacy rules and returning deterministic Apple XML plist bytes.
func MergePrivacyManifest(existingXML []byte, cfg NSGoPrivacyConfig) ([]byte, error) {
	var existing map[string]any
	if len(bytes.TrimSpace(existingXML)) > 0 {
		v, err := DecodePlist(existingXML)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", PrivacyManifestName, err)
		}
		var ok bool
		if existing, ok = v.(map[string]any); !ok {
			return nil, fmt.Errorf("%s: not a dictionary", PrivacyManifestName)
		}
		if err := ValidateResourcePrivacy(existing); err != nil {
			return nil, fmt.Errorf("%s: %w", PrivacyManifestName, err)
		}
	}
	if err := ValidateNSGoPrivacy(cfg); err != nil {
		return nil, err
	}

	out := map[string]any{}
	for k, v := range existing {
		out[k] = v
	}

	reasons := map[string][]string{}
	add := func(name string, rs ...string) {
		for _, r := range rs {
			if !slices.Contains(reasons[name], r) {
				reasons[name] = append(reasons[name], r)
			}
		}
	}

	others := []any{}
	for _, e := range plistArray(existing["NSPrivacyAccessedAPITypes"]) {
		d, _ := e.(map[string]any)
		name, _ := d["NSPrivacyAccessedAPIType"].(string)
		if name == "" {
			others = append(others, e)
			continue
		}
		for _, r := range plistArray(d["NSPrivacyAccessedAPITypeReasons"]) {
			if s, ok := r.(string); ok {
				add(name, s)
			}
		}
	}
	for name, rs := range NSGoPrivacyReasons {
		add(name, rs...)
	}
	for name, rs := range cfg.AccessedAPIs {
		add(name, rs...)
	}
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	accessed := others
	for _, name := range names {
		rs := reasons[name]
		sort.Strings(rs)
		values := make([]any, len(rs))
		for i, r := range rs {
			values[i] = r
		}
		accessed = append(accessed, map[string]any{
			"NSPrivacyAccessedAPIType":        name,
			"NSPrivacyAccessedAPITypeReasons": values,
		})
	}
	out["NSPrivacyAccessedAPITypes"] = accessed

	tracking, _ := existing["NSPrivacyTracking"].(bool)
	if cfg.Tracking != nil && *cfg.Tracking {
		tracking = true
	}
	out["NSPrivacyTracking"] = tracking

	domains := plistArray(existing["NSPrivacyTrackingDomains"])
	for _, d := range cfg.TrackingDomains {
		if !slices.Contains(domains, any(d)) {
			domains = append(domains, d)
		}
	}
	out["NSPrivacyTrackingDomains"] = domains

	data := plistArray(existing["NSPrivacyCollectedDataTypes"])
	for _, item := range cfg.CollectedData {
		v := plistValue(item)
		if !slices.ContainsFunc(data, func(e any) bool { return reflect.DeepEqual(e, v) }) {
			data = append(data, v)
		}
	}
	out["NSPrivacyCollectedDataTypes"] = data

	if err := validatePrivacyTracking(out); err != nil {
		return nil, err
	}
	if out["NSPrivacyTracking"] == false {
		delete(out, "NSPrivacyTrackingDomains")
	}
	return EncodePlist(out), nil
}

func validatePrivacyEntries(manifest map[string]any, key string, validate func(map[string]any) error) error {
	value, exists := manifest[key]
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array of dictionaries", key)
	}
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d] must be a dictionary", key, i)
		}
		if err := validate(entry); err != nil {
			return fmt.Errorf("%s[%d]: %w", key, i, err)
		}
	}
	return nil
}

func validatePrivacyAPI(entry map[string]any) error {
	const prefix = "NSPrivacyAccessedAPICategory"
	if category, _ := entry["NSPrivacyAccessedAPIType"].(string); !strings.HasPrefix(category, prefix) || category == prefix {
		return errors.New("NSPrivacyAccessedAPIType must name a required-reason API category")
	}
	return validatePrivacyStrings(entry, "NSPrivacyAccessedAPITypeReasons")
}

func validateCollectedPrivacyData(entry map[string]any) error {
	const prefix = "NSPrivacyCollectedDataType"
	if category, _ := entry[prefix].(string); !strings.HasPrefix(category, prefix) || category == prefix {
		return errors.New("NSPrivacyCollectedDataType must name a collected data type")
	}
	for _, key := range []string{"NSPrivacyCollectedDataTypeLinked", "NSPrivacyCollectedDataTypeTracking"} {
		if _, ok := entry[key].(bool); !ok {
			return fmt.Errorf("%s must be a boolean", key)
		}
	}
	return validatePrivacyStrings(entry, "NSPrivacyCollectedDataTypePurposes")
}

func validatePrivacyStrings(entry map[string]any, key string) error {
	values, _ := entry[key].([]any)
	if len(values) == 0 {
		return fmt.Errorf("%s must be a nonempty array of strings", key)
	}
	for i, v := range values {
		if value, ok := v.(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] must be a nonempty string", key, i)
		}
	}
	return nil
}

func validatePrivacyTracking(manifest map[string]any) error {
	tracking, _ := manifest["NSPrivacyTracking"].(bool)
	domains := plistArray(manifest["NSPrivacyTrackingDomains"])
	if tracking && len(domains) == 0 {
		return errors.New("NSPrivacyTracking is true; list the tracking domains in NSPrivacyTrackingDomains or apple.privacy.tracking_domains")
	}
	if !tracking && len(domains) > 0 {
		return errors.New("tracking domains require NSPrivacyTracking or apple.privacy.tracking to be true")
	}
	for i, value := range domains {
		domain, ok := value.(string)
		if !ok || !validTrackingDomain(domain) {
			return fmt.Errorf("NSPrivacyTrackingDomains[%d] must be a domain name without a scheme, port, path or query", i)
		}
	}
	return nil
}

func validTrackingDomain(domain string) bool {
	return strings.Contains(domain, ".") &&
		!strings.ContainsAny(domain, ":/?#") &&
		!strings.ContainsFunc(domain, unicode.IsSpace)
}

func plistArray(v any) []any {
	a, _ := v.([]any)
	return append([]any{}, a...)
}

func plistValueMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := plistValue(m).(map[string]any)
	return out
}

func plistValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, e := range v {
			m[k] = plistValue(e)
		}
		return m
	case []any:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = plistValue(e)
		}
		return a
	case []string:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = e
		}
		return a
	case int:
		return int64(v)
	}
	return v
}

// DecodePlist parses an Apple XML property list into Go maps, slices, booleans, numbers, bytes, and strings,
// matching NSGo's cmd/nsgo/sign.go decoder.
func DecodePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var value func(start xml.StartElement) (any, error)
	text := func() (string, error) {
		var b strings.Builder
		for {
			t, err := d.Token()
			if err != nil {
				return "", err
			}
			switch t := t.(type) {
			case xml.CharData:
				b.Write(t)
			case xml.EndElement:
				return b.String(), nil
			}
		}
	}
	next := func() (xml.StartElement, bool, error) {
		for {
			t, err := d.Token()
			if err != nil {
				return xml.StartElement{}, false, err
			}
			switch t := t.(type) {
			case xml.StartElement:
				return t, true, nil
			case xml.EndElement:
				return xml.StartElement{}, false, nil
			}
		}
	}
	value = func(start xml.StartElement) (any, error) {
		switch start.Name.Local {
		case "dict":
			m := map[string]any{}
			for {
				el, ok, err := next()
				if err != nil || !ok {
					return m, err
				}
				if el.Name.Local != "key" {
					return nil, fmt.Errorf("unexpected <%s> inside <dict>, want <key>", el.Name.Local)
				}
				key, err := text()
				if err != nil {
					return nil, err
				}
				v, ok, err := next()
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("missing value for key %q", key)
				}
				if m[key], err = value(v); err != nil {
					return nil, err
				}
			}
		case "array":
			a := []any{}
			for {
				e, ok, err := next()
				if err != nil || !ok {
					return a, err
				}
				v, err := value(e)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
		case "true", "false":
			_, err := text()
			return start.Name.Local == "true", err
		case "integer":
			s, err := text()
			if err != nil {
				return nil, err
			}
			return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		case "real":
			s, err := text()
			if err != nil {
				return nil, err
			}
			return strconv.ParseFloat(strings.TrimSpace(s), 64)
		case "data":
			s, err := text()
			if err != nil {
				return nil, err
			}
			return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		case "string", "date":
			return text()
		default:
			return nil, fmt.Errorf("unsupported plist element <%s>", start.Name.Local)
		}
	}
	for {
		start, ok, err := next()
		if err != nil {
			if err == io.EOF {
				return nil, errors.New("an empty property list")
			}
			return nil, err
		}
		if ok && start.Name.Local != "plist" {
			return value(start)
		}
	}
}

// EncodePlist writes v as a deterministic Apple XML property list with sorted dictionary keys,
// matching NSGo's cmd/nsgo/sign.go encoder.
func EncodePlist(v any) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header + `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n<plist version=\"1.0\">\n")
	esc := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return e.String()
	}
	var write func(v any, indent string)
	write = func(v any, indent string) {
		switch v := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString(indent + "<dict>\n")
			for _, k := range keys {
				b.WriteString(indent + "\t<key>" + esc(k) + "</key>\n")
				write(v[k], indent+"\t")
			}
			b.WriteString(indent + "</dict>\n")
		case []any:
			b.WriteString(indent + "<array>\n")
			for _, e := range v {
				write(e, indent+"\t")
			}
			b.WriteString(indent + "</array>\n")
		case bool:
			if v {
				b.WriteString(indent + "<true/>\n")
			} else {
				b.WriteString(indent + "<false/>\n")
			}
		case int64:
			fmt.Fprintf(&b, "%s<integer>%d</integer>\n", indent, v)
		case float64:
			fmt.Fprintf(&b, "%s<real>%v</real>\n", indent, v)
		case []byte:
			fmt.Fprintf(&b, "%s<data>%s</data>\n", indent, base64.StdEncoding.EncodeToString(v))
		case string:
			b.WriteString(indent + "<string>" + esc(v) + "</string>\n")
		}
	}
	write(v, "")
	b.WriteString("</plist>\n")
	return b.Bytes()
}
