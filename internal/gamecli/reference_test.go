package gamecli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the command reference pages under docs/<lang>/cli-reference.md")

// referencePath returns where the reference page in t's language is committed, from this package's directory.
func referencePath(t referenceText) string {
	return filepath.Join("..", "..", "docs", t.lang, "cli-reference.md")
}

// TestReference keeps the committed reference pages equal to what the commands' definitions generate; run it with -update to rewrite them after changing a command.
func TestReference(t *testing.T) {
	specs := commandSpecs()
	for _, text := range referenceTexts {
		var b strings.Builder
		writeReference(&b, text, specs)
		want := b.String()
		path := referencePath(text)
		if *update {
			if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update to generate it)", err)
		}
		if string(got) != want {
			t.Errorf("%s is out of date with the commands' definitions; run go tool nsgo test ./internal/gamecli -run TestReference -update", path)
		}
	}
}

func TestReferenceCoversEveryCommandAndFlag(t *testing.T) {
	specs := commandSpecs()
	for _, text := range referenceTexts {
		var b strings.Builder
		writeReference(&b, text, specs)
		page := b.String()
		if !strings.HasPrefix(page, "# "+text.title+"\n\n"+referenceHeader+"\n") {
			t.Errorf("%s page does not start with the title and header:\n%s", text.lang, page)
		}
		for _, s := range specs {
			start := strings.Index(page, "\n## game "+s.name+"\n")
			if start < 0 {
				t.Fatalf("%s page has no section for %s", text.lang, s.name)
			}
			section := page[start+1:]
			if next := strings.Index(section, "\n## "); next >= 0 {
				section = section[:next]
			}
			if !strings.Contains(section, "```sh\ngo tool "+s.usage+"\n```") {
				t.Errorf("%s %s section lacks its usage line:\n%s", text.lang, s.name, section)
			}
			for _, f := range s.flags {
				if !strings.Contains(section, "| `-"+f.name) {
					t.Errorf("%s %s section lacks -%s:\n%s", text.lang, s.name, f.name, section)
				}
			}
			if len(s.flags) == 0 && !strings.Contains(section, text.noFlags) {
				t.Errorf("%s %s section neither lists flags nor says it has none:\n%s", text.lang, s.name, section)
			}
		}
	}
}

func TestCodeSpan(t *testing.T) {
	cases := map[string]string{
		"-in string": "`-in string`",
		"a`b":        "``a`b``",
		"`x":         "`` `x ``",
		"a|b":        "`a\\|b`",
		" ":          "`   `",
	}
	for in, want := range cases {
		if got := codeSpan(in); got != want {
			t.Errorf("codeSpan(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMDEscape(t *testing.T) {
	if got, want := mdEscape(`a|b *c* _d_ <e> [f] \g`+"`h`"), `a\|b \*c\* \_d\_ \<e> \[f\] \\g`+"\\`h\\`"; got != want {
		t.Errorf("mdEscape = %q, want %q", got, want)
	}
}
