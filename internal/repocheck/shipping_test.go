package repocheck_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
)

// shippingProjectBlock returns the YAML fence immediately after marker.
func shippingProjectBlock(page, marker string) ([]byte, error) {
	lines := strings.Split(page, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != "```yaml" {
			return nil, fmt.Errorf("%s is not followed by a YAML fence", marker)
		}
		for end := i + 2; end < len(lines); end++ {
			if strings.TrimSpace(lines[end]) == "```" {
				return []byte(strings.Join(lines[i+2:end], "\n") + "\n"), nil
			}
		}
		return nil, fmt.Errorf("%s has an unterminated YAML fence", marker)
	}
	return nil, fmt.Errorf("missing %s", marker)
}

// TestShippingProjectExamples checks that following the shipping guide cannot make nsgo.yaml disagree with game.yaml.
func TestShippingProjectExamples(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		t.Run(lang, func(t *testing.T) {
			name := "docs/" + lang + "/shipping.md"
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			page := string(data)
			gameYAML, err := shippingProjectBlock(page, "<!-- shipping-project: game.yaml -->")
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			nsgoYAML, err := shippingProjectBlock(page, "<!-- shipping-project: nsgo.yaml -->")
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			f, err := project.Parse(gameYAML)
			if err != nil {
				t.Fatalf("%s: game.yaml: %v", name, err)
			}
			if err := f.CheckNSGo(nsgoYAML); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}
