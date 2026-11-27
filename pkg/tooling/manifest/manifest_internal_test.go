package manifest

import (
	"path"
	"strconv"
	"testing"
)

func TestFormatEmbedPattern(t *testing.T) {
	cases := []struct {
		name     string
		embedDir string
		rel      string
		want     string
		decoys   []string
	}{
		{
			name:   "plain path",
			rel:    "hero.png",
			want:   "hero.png",
			decoys: []string{"hero1.png", "xhero.png"},
		},
		{
			name:     "plain nested path with embedDir",
			embedDir: "assets",
			rel:      "sprites/hero.png",
			want:     "assets/sprites/hero.png",
			decoys:   []string{"assets/sprites/hero1.png"},
		},
		{
			name:   "numeric bracket",
			rel:    "hero[1].txt",
			want:   "hero[[]1].txt",
			decoys: []string{"hero1.txt", "hero2.txt"},
		},
		{
			name:   "character range bracket",
			rel:    "hero[a-z].txt",
			want:   "hero[[]a-z].txt",
			decoys: []string{"heroa.txt", "heroz.txt"},
		},
		{
			name:   "negated bracket",
			rel:    "hero[^1].txt",
			want:   "hero[[]^1].txt",
			decoys: []string{"hero2.txt", "heroa.txt"},
		},
		{
			name:   "unclosed opening bracket",
			rel:    "open[.txt",
			want:   "open[[].txt",
			decoys: []string{"open.txt"},
		},
		{
			name:   "bare closing bracket",
			rel:    "close].txt",
			want:   "close].txt",
			decoys: []string{"close.txt"},
		},
		{
			name:   "empty bracket pair",
			rel:    "empty[].txt",
			want:   "empty[[]].txt",
			decoys: []string{"empty.txt"},
		},
		{
			name:   "nested brackets",
			rel:    "a[[1]].txt",
			want:   "a[[][[]1]].txt",
			decoys: []string{"a[1].txt", "a1.txt"},
		},
		{
			name:   "star wildcard",
			rel:    "c*d.txt",
			want:   "c[*]d.txt",
			decoys: []string{"cd.txt", "cxd.txt", "c123d.txt"},
		},
		{
			name:   "question wildcard",
			rel:    "a?b.txt",
			want:   "a[?]b.txt",
			decoys: []string{"axb.txt", "a1b.txt"},
		},
		{
			name:   "combined glob characters",
			rel:    "a*[?][1].txt",
			want:   "a[*][[][?]][[]1].txt",
			decoys: []string{"a11.txt", "ax?1.txt", "a*?1.txt"},
		},
		{
			name:   "space with brackets is quoted",
			rel:    "space [1].txt",
			want:   `"space [[]1].txt"`,
			decoys: []string{"space 1.txt"},
		},
		{
			name:     "embedDir and rel both with brackets",
			embedDir: "assets[0]",
			rel:      "dir[1]/tile[2].png",
			want:     "assets[[]0]/dir[[]1]/tile[[]2].png",
			decoys:   []string{"assets0/dir1/tile2.png", "assets[0]/dir1/tile2.png"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatEmbedPattern(tc.embedDir, tc.rel)
			if got != tc.want {
				t.Fatalf("formatEmbedPattern(%q, %q) = %q, want %q", tc.embedDir, tc.rel, got, tc.want)
			}

			pattern := got
			if len(pattern) > 0 && pattern[0] == '"' {
				var err error
				pattern, err = strconv.Unquote(pattern)
				if err != nil {
					t.Fatalf("strconv.Unquote(%q): %v", got, err)
				}
			}

			target := tc.rel
			if tc.embedDir != "" {
				target = path.Join(tc.embedDir, tc.rel)
			}
			matched, err := path.Match(pattern, target)
			if err != nil {
				t.Fatalf("path.Match(%q, %q) returned error: %v", pattern, target, err)
			}
			if !matched {
				t.Errorf("path.Match(%q, %q) = false, want true", pattern, target)
			}
			for _, decoy := range tc.decoys {
				matched, err := path.Match(pattern, decoy)
				if err != nil {
					t.Fatalf("path.Match(%q, %q) returned error: %v", pattern, decoy, err)
				}
				if matched {
					t.Errorf("path.Match(%q, %q) = true, want false (decoy matched)", pattern, decoy)
				}
			}
		})
	}
}

func TestValidateEmbedPath(t *testing.T) {
	valid := []string{
		"hero.png",
		"hero[1].txt",
		"hero[a-z].txt",
		"open[.txt",
		"close].txt",
		"empty[].txt",
		"neg[^1].txt",
		"space [1].txt",
		"dir[0]/sub_1/file-2.png",
		"_private/tile.png",
		"héllo.png",
		"图标.png",
		"!#$%&()+,-.=@[]^_{}~ .txt",
	}
	for _, p := range valid {
		if err := validateEmbedPath(p); err != nil {
			t.Errorf("validateEmbedPath(%q) = %v, want nil", p, err)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"...",
		"/abs.txt",
		"../escape.txt",
		"a//b.txt",
		"a/",
		"a*b.txt",
		"a?b.txt",
		"a:b.txt",
		`a"b.txt`,
		"a'b.txt",
		"a`b.txt",
		"a|b.txt",
		"a<b.txt",
		"a>b.txt",
		"a;b.txt",
		`a\b.txt`,
		"a\tb.txt",
		"a\nb.txt",
		"trailing.",
		"dir./file.txt",
		"CON.txt",
		"nul",
		"com1.png",
		"lpt9/a.txt",
		".git/config",
		"sub/.svn/entries",
		"icon🎮.png",
		string([]byte{0xff, 'a', '.', 't', 'x', 't'}),
	}
	for _, p := range invalid {
		if err := validateEmbedPath(p); err == nil {
			t.Errorf("validateEmbedPath(%q) = nil, want error", p)
		}
	}
}
