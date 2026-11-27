package resource

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/nsgo/core"
)

// Theme is a UI theme for gameui: colors, spacing and font. Every field is optional; Apply overrides only the fields a file sets, so a theme file can restyle a few colors of gameui.DefaultTheme.
type Theme struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Colors holds the theme's CSS colors, each parsed with core.ParseColor.
	Colors ThemeColors `yaml:"colors,omitempty"`
	// Padding is the default inner padding in points.
	Padding *float32 `yaml:"padding,omitempty"`
	// CornerRadius is the default corner radius in points.
	CornerRadius *float32 `yaml:"cornerRadius,omitempty"`
	// BorderWidth is the widget border stroke width in points.
	BorderWidth *float32 `yaml:"borderWidth,omitempty"`
	// FocusWidth is the focus outline stroke width in points.
	FocusWidth *float32 `yaml:"focusWidth,omitempty"`
	// Font is the default font of text-bearing widgets.
	Font *ThemeFont `yaml:"font,omitempty"`
}

// ThemeColors holds a Theme's colors, one per gameui.Theme color field; an empty one keeps the base theme's.
type ThemeColors struct {
	PanelBackground string `yaml:"panelBackground,omitempty"`
	PanelBorder     string `yaml:"panelBorder,omitempty"`
	Background      string `yaml:"background,omitempty"`
	Hover           string `yaml:"hover,omitempty"`
	Active          string `yaml:"active,omitempty"`
	FocusedBorder   string `yaml:"focusedBorder,omitempty"`
	Disabled        string `yaml:"disabled,omitempty"`
	Border          string `yaml:"border,omitempty"`
	Text            string `yaml:"text,omitempty"`
	DisabledText    string `yaml:"disabledText,omitempty"`
	MutedText       string `yaml:"mutedText,omitempty"`
	Accent          string `yaml:"accent,omitempty"`
	AccentHover     string `yaml:"accentHover,omitempty"`
	AccentActive    string `yaml:"accentActive,omitempty"`
	Track           string `yaml:"track,omitempty"`
}

// ThemeFont is a Theme's default font; an empty or zero field keeps the base theme's.
type ThemeFont struct {
	// Family is a font family name.
	Family string `yaml:"family,omitempty"`
	// Size is the font size in points; 0 keeps the base theme's.
	Size float32 `yaml:"size,omitempty"`
	// Weight is "light", "normal", "medium", "semibold" or "bold".
	Weight string `yaml:"weight,omitempty"`
	// Italic sets the italic style when true; nil keeps the base theme's.
	Italic *bool `yaml:"italic,omitempty"`
}

var fontWeights = map[string]core.FontWeight{
	"light":    core.FontWeightLight,
	"normal":   core.FontWeightNormal,
	"medium":   core.FontWeightMedium,
	"semibold": core.FontWeightSemibold,
	"bold":     core.FontWeightBold,
}

// fields lists the color fields with their YAML names and gameui.Theme destinations, in declaration order.
func (c *ThemeColors) fields(t *gameui.Theme) []struct {
	name string
	src  string
	dst  *core.Color
} {
	type f = struct {
		name string
		src  string
		dst  *core.Color
	}
	if t == nil {
		t = &gameui.Theme{}
	}
	return []f{
		{"panelBackground", c.PanelBackground, &t.PanelBackground},
		{"panelBorder", c.PanelBorder, &t.PanelBorder},
		{"background", c.Background, &t.Background},
		{"hover", c.Hover, &t.Hover},
		{"active", c.Active, &t.Active},
		{"focusedBorder", c.FocusedBorder, &t.FocusedBorder},
		{"disabled", c.Disabled, &t.Disabled},
		{"border", c.Border, &t.Border},
		{"text", c.Text, &t.Text},
		{"disabledText", c.DisabledText, &t.DisabledText},
		{"mutedText", c.MutedText, &t.MutedText},
		{"accent", c.Accent, &t.Accent},
		{"accentHover", c.AccentHover, &t.AccentHover},
		{"accentActive", c.AccentActive, &t.AccentActive},
		{"track", c.Track, &t.Track},
	}
}

// Kind returns KindTheme.
func (t *Theme) Kind() Kind { return KindTheme }

// Validate checks the header, every color, the sizes and the font.
func (t *Theme) Validate() error {
	if t == nil {
		return fmt.Errorf("resource: nil theme")
	}
	if err := t.Header.validate(KindTheme); err != nil {
		return err
	}
	for _, f := range t.Colors.fields(nil) {
		if f.src == "" {
			continue
		}
		if _, err := core.ParseColor(f.src); err != nil {
			return fmt.Errorf("resource: theme color %s %q: %w", f.name, f.src, err)
		}
	}
	for _, s := range []struct {
		name string
		v    *float32
	}{{"padding", t.Padding}, {"cornerRadius", t.CornerRadius}, {"borderWidth", t.BorderWidth}, {"focusWidth", t.FocusWidth}} {
		if s.v != nil {
			if err := checkNonNegative("theme "+s.name, *s.v); err != nil {
				return err
			}
		}
	}
	if f := t.Font; f != nil {
		if err := checkNonNegative("theme font size", f.Size); err != nil {
			return err
		}
		if f.Weight != "" {
			if _, ok := fontWeights[f.Weight]; !ok {
				return fmt.Errorf("resource: theme font weight %q must be light, normal, medium, semibold or bold", f.Weight)
			}
		}
	}
	return nil
}

// Apply returns base with every field the theme sets replaced. Fields the file leaves out keep base's values, as do gameui.Theme fields a file cannot hold, such as frame drawers.
func (t *Theme) Apply(base gameui.Theme) gameui.Theme {
	if t == nil {
		return base
	}
	out := base
	for _, f := range t.Colors.fields(&out) {
		if f.src == "" {
			continue
		}
		if c, err := core.ParseColor(f.src); err == nil {
			*f.dst = c
		}
	}
	if t.Padding != nil {
		out.Padding = *t.Padding
	}
	if t.CornerRadius != nil {
		out.CornerRadius = *t.CornerRadius
	}
	if t.BorderWidth != nil {
		out.BorderWidth = *t.BorderWidth
	}
	if t.FocusWidth != nil {
		out.FocusWidth = *t.FocusWidth
	}
	if f := t.Font; f != nil {
		if f.Family != "" {
			out.Font.Family = f.Family
		}
		if f.Size > 0 {
			out.Font.Size = core.Points(f.Size)
		}
		if w, ok := fontWeights[f.Weight]; ok {
			out.Font.Weight = w
		}
		if f.Italic != nil {
			out.Font.Italic = *f.Italic
		}
	}
	return out
}
