package repocheck

import (
	"bufio"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type PrivacyExemption struct {
	Path    string `yaml:"path"`
	Pattern string `yaml:"pattern"`
	Reason  string `yaml:"reason"`
}
type PrivacyExemptions struct {
	Version int                `yaml:"version"`
	Entries []PrivacyExemption `yaml:"entries"`
}
type PrivacyFinding struct {
	Path   string
	Reason string
}

var privatePath = regexp.MustCompile(`(?i)(/(?:Users|home)/)([^/\\\s]+)|[A-Za-z]:[\\/]Users[\\/]([^\\/\s]+)`)
var privateNames = regexp.MustCompile(`(?i)(^|/)(?:\.env(?:\..*)?|\.npmrc|\.pypirc|\.netrc|id_rsa[^/]*|id_ed25519[^/]*|[^/]+\.(?:pem|key|p12|pfx|jks|keystore|mobileprovision)|\.DS_Store)$`)
var secretText = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|(?:api[_-]?key|secret|token)\s*[:=]\s*["']?[A-Za-z0-9_\-]{20,})`)

func ParsePrivacyExemptions(data []byte) (PrivacyExemptions, error) {
	var x PrivacyExemptions
	d := yaml.NewDecoder(strings.NewReader(string(data)))
	d.KnownFields(true)
	if err := d.Decode(&x); err != nil {
		return x, err
	}
	if x.Version != 1 {
		return x, fmt.Errorf("privacy exemptions: version must be 1")
	}
	for i, e := range x.Entries {
		if strings.TrimSpace(e.Path) == "" || strings.TrimSpace(e.Reason) == "" || (e.Pattern == "" && e.Path == "") {
			return x, fmt.Errorf("privacy exemptions: entry %d needs path or pattern and reason", i)
		}
	}
	return x, nil
}
func ScanPrivacy(root fs.FS, exemptions PrivacyExemptions) ([]PrivacyFinding, error) {
	var out []PrivacyFinding
	err := fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." || d.IsDir() {
			return nil
		}
		rel := path.Clean(name)
		if exempt(rel, "", exemptions) {
			return nil
		}
		if privateNames.MatchString(rel) {
			out = append(out, PrivacyFinding{rel, "private filename"})
			return nil
		}
		f, e := root.Open(rel)
		if e != nil {
			return e
		}
		defer f.Close()
		r := bufio.NewReader(f)
		b, _ := r.Peek(1 << 16)
		if m := privatePath.FindStringSubmatch(string(b)); m != nil && !exempt(rel, m[0], exemptions) {
			user := m[2]
			if user == "you" || user == "user" || user == "example" {
				return nil
			}
			out = append(out, PrivacyFinding{rel, "personal path"})
		} else if secretText.MatchString(string(b)) {
			out = append(out, PrivacyFinding{rel, "secret-like text"})
		}
		return nil
	})
	return out, err
}
func exempt(p, value string, x PrivacyExemptions) bool {
	for _, e := range x.Entries {
		if e.Path == p || e.Pattern != "" && strings.Contains(value, e.Pattern) {
			return true
		}
	}
	return false
}
