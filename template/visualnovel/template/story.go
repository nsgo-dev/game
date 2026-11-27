package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/vn"
)

// The story's files: the Yarn script, the cast and a string table per language. Edit them to write another story; story_test.go checks that they agree with each other.
var (
	//go:embed story/story.yarn
	storyYarn string
	//go:embed story/cast.yaml
	castYAML []byte
	//go:embed strings/en.yaml
	enYAML []byte
	//go:embed strings/zh-Hans.yaml
	zhYAML []byte
)

const (
	// startNode is the node a new game begins at.
	startNode = "Start"
	// chapterHeader is the node header that names the chapter a save slot shows, the key chapter.<value> in the string tables.
	chapterHeader = "chapter"
	// baseLocale is the language the script is written in; its string table holds the interface's strings, and the script's own text is its lines.
	baseLocale = "en"
)

// Commands the script runs that the game handles, beside vn's <<show>>, <<hide>> and <<expr>>.
const (
	// commandBackground changes the background: <<bg river>>. stage.go draws each name in backgrounds.
	commandBackground = "bg"
	// commandEnding names the ending the story reached, just before it ends: <<ending dawn>>. Each ending has the strings ending.<name> and ending.<name>.text.
	commandEnding = "ending"
)

// endings are the story's endings, in the order the title screen counts them.
var endings = []string{"dawn", "lost"}

// stringTables are the embedded string tables, one per language, with the files they were read from.
var stringTables = []struct {
	name string
	data []byte
}{
	{"strings/en.yaml", enYAML},
	{"strings/zh-Hans.yaml", zhYAML},
}

// languages are the locales of the string tables, the languages the title screen cycles through, the base locale first. Adding one adds its string table here and to the embeds above.
var languages = []string{baseLocale, "zh-Hans"}

// lineIDPattern finds the #line: ids of a Yarn script.
var lineIDPattern = regexp.MustCompile(`#line:([A-Za-z0-9_]+)`)

// story is the parsed script, cast and strings every playthrough shares.
type story struct {
	script  *flow.DialogueScript
	cast    *vn.Cast
	catalog *l10n.Catalog
}

// loadStory parses the embedded story files.
func loadStory() (*story, error) {
	script, err := flow.ParseYarn(storyYarn)
	if err != nil {
		return nil, loaderr.WithFile(err, "story/story.yarn")
	}
	if !script.HasNode(startNode) {
		return nil, fmt.Errorf("story/story.yarn: no %s node to begin at", startNode)
	}
	cast, err := vn.ParseCast(castYAML)
	if err != nil {
		return nil, loaderr.WithFile(err, "story/cast.yaml")
	}
	catalog := l10n.NewCatalog(baseLocale)
	for _, table := range stringTables {
		if err := catalog.LoadYAML(table.data); err != nil {
			return nil, loaderr.WithFile(err, table.name)
		}
	}
	return &story{script: script, cast: cast, catalog: catalog}, nil
}

// lineIDs returns the #line: ids of the script's lines and options, in the order they appear.
func lineIDs(src string) []string {
	var ids []string
	for _, m := range lineIDPattern.FindAllStringSubmatch(src, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// lineKey is the string table key that translates the line or option with id.
func lineKey(id string) string { return "line." + id }

// chapterOf returns the chapter name of node, from its chapter header.
func (s *story) chapterOf(node string) string {
	return s.script.NodeHeaders(node)[chapterHeader]
}

// resolver returns the flow.StringResolver that shows the script's lines in the language locale returns: the string table's line.<id> where it has one, and the script's own text otherwise.
func (s *story) resolver(locale func() string) flow.StringResolver {
	return func(lineID, fallback string, _ map[string]any) string {
		if lineID == "" {
			return fallback
		}
		loc := locale()
		if loc == baseLocale || !s.catalog.Has(loc, lineKey(lineID)) {
			return fallback
		}
		return s.catalog.T(loc, lineKey(lineID))
	}
}

// localizeNames sets each cast member's name plate to its name.<id> string in locale. vn reads a character's Name as its plate, so the game rewrites the cast it owns when the language changes; lines name speakers by id, which stays the same.
func (s *story) localizeNames(locale string) {
	for i := range s.cast.Characters {
		ch := &s.cast.Characters[i]
		if key := "name." + ch.ID; s.catalog.Has(locale, key) {
			ch.Name = s.catalog.T(locale, key)
		}
	}
}

// nextLanguage returns the language after locale in languages, wrapping around.
func nextLanguage(locale string) string {
	i := slices.Index(languages, locale)
	return languages[(i+1)%len(languages)]
}

// pickLanguage returns the language of languages that the device's locale tag asks for: the one with the same primary language subtag, such as zh-Hans for zh-CN, or else the base locale.
func pickLanguage(tag string) string {
	want, _, _ := strings.Cut(l10n.NormalizeLocale(tag), "-")
	for _, l := range languages {
		if primary, _, _ := strings.Cut(l, "-"); primary == want {
			return l
		}
	}
	return baseLocale
}
