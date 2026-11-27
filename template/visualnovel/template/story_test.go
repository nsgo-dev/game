package main

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/vn"
)

// TestStoryFilesAreCanonical checks that cast.yaml and the string tables are in the form their encoders write, so that a tool that loads and saves them changes nothing.
func TestStoryFilesAreCanonical(t *testing.T) {
	cast, err := vn.ParseCast(castYAML)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := vn.EncodeCast(cast); err != nil || !bytes.Equal(out, castYAML) {
		t.Errorf("story/cast.yaml is not canonical (%v):\n%s", err, out)
	}
	for _, table := range stringTables {
		st, err := l10n.ParseYAML(table.data)
		if err != nil {
			t.Fatalf("%s: %v", table.name, err)
		}
		if out, err := l10n.EncodeYAML(st); err != nil || !bytes.Equal(out, table.data) {
			t.Errorf("%s is not canonical (%v):\n%s", table.name, err, out)
		}
		if want := "strings/" + st.Locale + ".yaml"; table.name != want || !slices.Contains(languages, st.Locale) {
			t.Errorf("%s holds locale %s: name it %s and list it in languages", table.name, st.Locale, want)
		}
	}
	if len(stringTables) != len(languages) {
		t.Errorf("%d string tables for %d languages", len(stringTables), len(languages))
	}
}

// TestEveryLineIsTranslated checks that every line and option has a unique #line: id, that every other language translates each of them and nothing else, and that every language has the interface strings the base one has.
func TestEveryLineIsTranslated(t *testing.T) {
	ids := lineIDs(storyYarn)
	if len(ids) == 0 {
		t.Fatal("the script has no #line: ids")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("#line:%s is used twice", id)
		}
		seen[id] = true
	}
	// Every line and option of the script carries an id, which translates it and lets a save slot point at it. A node's headers, such as title:, chapter:, tags: and position:, come before its --- and are no lines.
	inHeader := true
	for i, line := range strings.Split(storyYarn, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "---":
			inHeader = false
			continue
		case trimmed == "===":
			inHeader = true
			continue
		case trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "<<"):
			continue
		case inHeader:
			if !headerPattern.MatchString(trimmed) {
				t.Errorf("story/story.yarn:%d is not a node header, and no node's --- comes before it: %s", i+1, trimmed)
			}
			continue
		}
		if !lineIDPattern.MatchString(trimmed) {
			t.Errorf("story/story.yarn:%d has no #line: id: %s", i+1, trimmed)
		}
	}

	tables := map[string]*l10n.StringTable{}
	for _, table := range stringTables {
		st, err := l10n.ParseYAML(table.data)
		if err != nil {
			t.Fatal(err)
		}
		tables[st.Locale] = st
	}
	base := tables[baseLocale]
	for _, locale := range languages[1:] {
		st := tables[locale]
		for _, id := range ids {
			if _, ok := st.Strings[lineKey(id)]; !ok {
				t.Errorf("%s does not translate #line:%s", locale, id)
			}
		}
		for key := range st.Strings {
			if id, ok := strings.CutPrefix(key, "line."); ok && !seen[id] {
				t.Errorf("%s translates %s, which no line of the script has", locale, key)
			} else if !ok && !hasString(base, key) {
				t.Errorf("%s has %s, which %s lacks", locale, key, baseLocale)
			}
		}
		for key := range base.Strings {
			if !hasString(st, key) {
				t.Errorf("%s lacks %s", locale, key)
			}
		}
	}
	for key := range base.Strings {
		if strings.HasPrefix(key, "line.") {
			t.Errorf("%s has %s: the script itself holds the base language's lines", baseLocale, key)
		}
	}
}

// headerPattern matches a node header of a Yarn script, such as "title: Start" or "position: 120,40".
var headerPattern = regexp.MustCompile(`^\w+:`)

// commandPattern finds the script's <<bg>> and <<ending>> commands and what they name.
var commandPattern = regexp.MustCompile(`(?m)^[ \t]*<<(bg|ending)\b[ \t]*([^>]*)>>`)

func hasString(st *l10n.StringTable, key string) bool {
	_, ok := st.Strings[key]
	return ok
}

// TestStoryNamesWhatTheGameDraws checks that every background and ending the script names exists, that every ending and chapter has its strings in every language, and that every cast member has a look.
func TestStoryNamesWhatTheGameDraws(t *testing.T) {
	s, err := loadStory()
	if err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, m := range commandPattern.FindAllStringSubmatch(storyYarn, -1) {
		args := strings.Fields(m[2])
		switch {
		case len(args) != 1:
			t.Errorf("<<%s %s>> takes one name", m[1], m[2])
		case m[1] == commandBackground && backgrounds[args[0]] == nil:
			t.Errorf("<<bg %s>> names no background of stage.go", args[0])
		case m[1] == commandEnding:
			named = append(named, args[0])
		}
	}
	slices.Sort(named)
	if want := slices.Sorted(slices.Values(endings)); !slices.Equal(slices.Compact(named), want) {
		t.Errorf("the script names the endings %v, want %v", named, want)
	}
	for _, locale := range languages {
		for _, e := range endings {
			for _, key := range []string{"ending." + e, "ending." + e + ".text"} {
				if !s.catalog.Has(locale, key) {
					t.Errorf("%s lacks %s", locale, key)
				}
			}
		}
		for _, node := range s.script.Nodes() {
			chapter := s.chapterOf(node)
			if chapter == "" || !s.catalog.Has(locale, "chapter."+chapter) {
				t.Errorf("node %s: give it a chapter header with a chapter.<name> string in %s", node, locale)
			}
		}
	}
	for _, ch := range s.cast.Characters {
		if _, ok := looks[ch.ID]; !ok {
			t.Errorf("cast member %s has no look in stage.go", ch.ID)
		}
		for _, locale := range languages {
			if !s.catalog.Has(locale, "name."+ch.ID) {
				t.Errorf("%s lacks name.%s", locale, ch.ID)
			}
		}
	}
	if err := s.cast.CheckFrames(portraitAtlas(s.cast)); err != nil {
		t.Error(err)
	}
}

// TestEveryPathEndsInAnEnding plays every path through the story, taking each option at each choice, and checks that each one ends in an ending, that every ending is reached, that every speaker is in the cast, and that every command run names a background or an ending the game knows.
func TestEveryPathEndsInAnEnding(t *testing.T) {
	s, err := loadStory()
	if err != nil {
		t.Fatal(err)
	}
	reached := map[string]int{}
	var walk func(path []int)
	walk = func(path []int) {
		ending := ""
		p := vn.New(s.script, s.cast, vn.Options{Dialogue: dialogue.Options{OnCommand: func(ev dialogue.Event) {
			switch {
			case ev.Command == commandEnding && len(ev.Args) == 1 && slices.Contains(endings, ev.Args[0]):
				ending = ev.Args[0]
			case ev.Command == commandBackground && len(ev.Args) == 1 && backgrounds[ev.Args[0]] != nil:
			default:
				t.Errorf("path %v: %s: <<%s %v>> is no command the game knows", path, ev.Node, ev.Command, ev.Args)
			}
		}}, CharsPerSecond: -1})
		if err := p.Start(startNode); err != nil {
			t.Fatal(err)
		}
		k := 0
		for range maxReplaySteps {
			if line, ok := p.Line(); ok && line.Name != "" && line.Character == "" {
				t.Errorf("path %v: speaker %q is not in the cast", path, line.Speaker)
			}
			if p.Ended() {
				if ending == "" {
					t.Errorf("path %v ends without an <<ending>>", path)
				}
				reached[ending]++
				return
			}
			if choices := p.Choices(); len(choices) > 0 {
				if k == len(path) {
					for _, c := range choices {
						if c.Available {
							walk(append(slices.Clone(path), c.Index))
						}
					}
					return
				}
				if err := p.Choose(path[k]); err != nil {
					t.Fatal(err)
				}
				k++
				continue
			}
			if err := p.Advance(); err != nil {
				t.Fatal(err)
			}
		}
		t.Errorf("path %v does not end", path)
	}
	walk(nil)
	for _, e := range endings {
		if reached[e] == 0 {
			t.Errorf("no path reaches the %s ending", e)
		}
	}
	// Two options at each of three choices: the dawn ending takes trusting Tobin twice and the marsh.
	if reached["dawn"] != 1 || reached["lost"] != 7 {
		t.Errorf("endings reached = %v, want dawn on 1 path and lost on 7", reached)
	}
}

func TestPickLanguageFollowsTheDevice(t *testing.T) {
	for tag, want := range map[string]string{"": "en", "en-US": "en", "fr": "en", "zh": "zh-Hans", "zh-CN": "zh-Hans", "zh_Hans_CN": "zh-Hans"} {
		if got := pickLanguage(tag); got != want {
			t.Errorf("pickLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
	if got := nextLanguage("zh-Hans"); got != "en" {
		t.Errorf("nextLanguage(zh-Hans) = %q, want en", got)
	}
}
