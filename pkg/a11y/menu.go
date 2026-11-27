package a11y

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// MenuItem describes a single interactive entry inside an AccessibleMenu.
type MenuItem struct {
	// ID is an optional stable suffix used to derive a deterministic TestID.
	ID string
	// Label is the item's visible and accessible name.
	Label string
	// LabelFunc overrides Label with a reactive label.
	LabelFunc func() string
	// Hint describes what activating the item does (read after Label and Description).
	Hint string
	// Description provides additional accessible context (AccessibilityDescription).
	Description string
	// DescriptionFunc overrides Description with a reactive description.
	DescriptionFunc func() string
	// Value is an optional reactive textual value (e.g. "75%", "Deuteranopia"),
	// exposed both visually and via AccessibilityValue.
	Value func() string
	// AccessibilityValue, when non-nil, overrides Value for assistive technology.
	AccessibilityValue func() core.AccessibilityValue
	// Role overrides the default RoleMenuItem (or RoleMenuItemCheckbox when Checked != nil).
	Role core.Role
	// Checked, when non-nil, marks the item as a checkable toggle (RoleMenuItemCheckbox).
	Checked func() bool
	// Disabled, when returning true, prevents activation and skips the item during
	// keyboard navigation.
	Disabled func() bool
	// OnSelect runs when the user activates the item via keyboard, press, or
	// screen-reader activate action.
	OnSelect func()
	// TestID sets an explicit test identifier for this menu item.
	TestID string
}

// MenuProps configures an AccessibleMenu.
type MenuProps struct {
	// Label is the accessible name of the menu (e.g. "Pause Menu").
	Label string
	// Hint is an optional accessibility hint for the menu container.
	Hint string
	// Description is an optional AccessibilityDescription for the menu container.
	Description string
	// Items are the menu entries in vertical order.
	Items []MenuItem
	// Palette sets the menu's colors; when zero, Assist.Palette() (if Assist != nil)
	// or PaletteFor(VisionNormal) is used.
	Palette Palette
	// Assist optionally scales text and supplies a live reactive Palette.
	Assist *Assist
	// Text customizes screen-reader announcements and visible toggle labels.
	// Unset fields fall back to DefaultMenuText (English).
	Text MenuText
	// InitialIndex selects the starting focused item (clamped to the first enabled item).
	InitialIndex int
	// SuppressOpenAnnouncement disables the automatic cx.Announce call on menu mount.
	SuppressOpenAnnouncement bool
	// OnClose runs when Escape or screen-reader ActionEscape is triggered.
	OnClose func()
	// TestID overrides the menu container's TestID (default "a11y-menu").
	TestID string
	// Style merges additional styling onto the menu container.
	Style core.Style
}

// roleReportsChecked reports whether NSGo's native accessibility backends
// expose Checked directly on views with role, so a focus announcement need not
// repeat the checked/unchecked word.
func roleReportsChecked(role core.Role) bool {
	switch role {
	case core.RoleCheckbox, core.RoleSwitch, core.RoleRadio, core.RoleMenuItemCheckbox, core.RoleMenuItemRadio:
		return true
	default:
		return false
	}
}

// AccessibleMenu renders a screen-reader- and keyboard-accessible native NSGo
// menu with RoleMenu / RoleMenuItem (or RoleMenuItemCheckbox), full ARIA/a11y
// metadata, keyboard navigation (Up/Down/Home/End/Return/Space/Escape via
// OnKeyDown), and live screen-reader announcements via cx.Announce on open,
// focus change, toggle, and close.
func AccessibleMenu(cx *core.Scope, props MenuProps) core.Node {
	text := resolveMenuText(props.Text)
	testID := props.TestID
	if testID == "" {
		testID = "a11y-menu"
	}
	menuLabel := props.Label
	if menuLabel == "" {
		menuLabel = text.DefaultLabel
	}

	items := props.Items
	itemRefs := make([]*core.Ref, len(items))
	for i := range items {
		itemRefs[i] = &core.Ref{}
	}
	menuRef := &core.Ref{}

	isDisabled := func(i int) bool {
		if i < 0 || i >= len(items) {
			return true
		}
		return items[i].Disabled != nil && items[i].Disabled()
	}

	itemRole := func(i int) core.Role {
		if i < 0 || i >= len(items) {
			return core.RoleNone
		}
		role := items[i].Role
		if role == core.RoleNone {
			if items[i].Checked != nil {
				return core.RoleMenuItemCheckbox
			}
			return core.RoleMenuItem
		}
		return role
	}

	itemLabel := func(i int) string {
		if i < 0 || i >= len(items) {
			return ""
		}
		if items[i].LabelFunc != nil {
			return items[i].LabelFunc()
		}
		return items[i].Label
	}

	itemStatusText := func(i int, includeChecked bool) string {
		if i < 0 || i >= len(items) {
			return ""
		}
		it := items[i]
		var parts []string
		if lbl := itemLabel(i); lbl != "" {
			parts = append(parts, lbl)
		}
		if includeChecked && it.Checked != nil {
			if it.Checked() {
				parts = append(parts, text.Checked)
			} else {
				parts = append(parts, text.Unchecked)
			}
		}
		if it.AccessibilityValue != nil {
			av := it.AccessibilityValue()
			if av.Text != "" {
				parts = append(parts, av.Text)
			} else if av.IsRange() && !av.NoNow {
				parts = append(parts, fmt.Sprintf("%g", av.Now))
			}
		} else if it.Value != nil {
			if v := it.Value(); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, text.Separator)
	}

	findEnabled := func(start, dir int) int {
		n := len(items)
		if n == 0 {
			return -1
		}
		idx := start
		for range n {
			idx = ((idx % n) + n) % n
			if !isDisabled(idx) {
				return idx
			}
			idx += dir
		}
		return -1
	}

	initial := findEnabled(props.InitialIndex, 1)
	activeIdx := core.NewSignal(cx, initial)

	if !props.SuppressOpenAnnouncement {
		itemsText := text.formatItems(len(items))
		countStr := strconv.Itoa(len(items))
		if initial >= 0 {
			cx.Announce(interpolateTemplate(text.OpenedWithItem,
				"label", menuLabel,
				"items", itemsText,
				"count", countStr,
				"item", itemStatusText(initial, true),
			))
		} else {
			cx.Announce(interpolateTemplate(text.Opened,
				"label", menuLabel,
				"items", itemsText,
				"count", countStr,
			))
		}
	}

	focusItem := func(idx int, announce bool) {
		if idx < 0 || idx >= len(items) || isDisabled(idx) {
			return
		}
		activeIdx.Set(idx)
		itemRefs[idx].Focus()
		itemRefs[idx].SetAccessibilityFocus()
		if announce {
			includeChecked := !roleReportsChecked(itemRole(idx))
			cx.Announce(interpolateTemplate(text.Position,
				"item", itemStatusText(idx, includeChecked),
				"index", strconv.Itoa(idx+1),
				"count", strconv.Itoa(len(items)),
			))
		}
	}

	activateItem := func(idx int) {
		if idx < 0 || idx >= len(items) || isDisabled(idx) {
			return
		}
		activeIdx.Set(idx)
		it := items[idx]
		if it.OnSelect != nil {
			it.OnSelect()
		}
		switch {
		case it.Checked != nil:
			cx.Announce(itemStatusText(idx, true))
		case it.Value != nil || it.AccessibilityValue != nil:
			cx.Announce(itemStatusText(idx, true))
		default:
			cx.Announce(interpolateTemplate(text.Selected, "label", itemLabel(idx)))
		}
	}

	closeMenu := func() bool {
		if props.OnClose == nil {
			return false
		}
		props.OnClose()
		cx.Announce(interpolateTemplate(text.Closed, "label", menuLabel))
		return true
	}

	handleKey := func(ev core.KeyEvent) bool {
		switch ev.Key {
		case "down":
			cur := activeIdx.Get()
			next := findEnabled(cur+1, 1)
			if next >= 0 {
				focusItem(next, true)
			}
			return true
		case "up":
			cur := activeIdx.Get()
			if cur < 0 {
				cur = len(items)
			}
			prev := findEnabled(cur-1, -1)
			if prev >= 0 {
				focusItem(prev, true)
			}
			return true
		case "home":
			first := findEnabled(0, 1)
			if first >= 0 {
				focusItem(first, true)
			}
			return true
		case "end":
			last := findEnabled(len(items)-1, -1)
			if last >= 0 {
				focusItem(last, true)
			}
			return true
		case "enter", "return", "space":
			cur := activeIdx.Get()
			if cur >= 0 {
				activateItem(cur)
				return true
			}
			return false
		case "escape":
			return closeMenu()
		default:
			return false
		}
	}

	resolvePalette := func() Palette {
		if props.Palette != (Palette{}) {
			return props.Palette
		}
		if props.Assist != nil {
			return props.Assist.Palette()
		}
		return PaletteFor(VisionNormal)
	}

	resolveScale := func() core.Points {
		if props.Assist != nil {
			return core.Points(props.Assist.TextScale())
		}
		return 1
	}

	children := make([]core.Node, 0, len(items))
	for i := range items {
		idx := i
		it := items[i]

		role := itemRole(idx)

		itemTestID := it.TestID
		if itemTestID == "" {
			if it.ID != "" {
				itemTestID = testID + "-" + it.ID
			} else {
				itemTestID = fmt.Sprintf("%s-item-%d", testID, idx)
			}
		}

		var a11yVal func() core.AccessibilityValue
		if it.AccessibilityValue != nil {
			a11yVal = it.AccessibilityValue
		} else if it.Value != nil {
			a11yVal = func() core.AccessibilityValue {
				return core.AccessibilityValue{Text: it.Value()}
			}
		}

		rowNodes := []core.Node{
			core.Text{
				ContentFunc: func() string { return itemLabel(idx) },
				StyleFunc: func() core.Style {
					p := resolvePalette()
					fg := EnsureContrast(p.Foreground, p.Background, 4.5)
					if isDisabled(idx) {
						fg = p.Neutral
					}
					return core.Style{
						Color:      fg,
						FontSize:   16 * resolveScale(),
						FontWeight: core.FontWeightMedium,
						FlexGrow:   1,
					}
				},
			},
		}

		if it.Checked != nil || it.Value != nil {
			rowNodes = append(rowNodes, core.Text{
				ContentFunc: func() string {
					if it.Checked != nil {
						if it.Checked() {
							return text.On
						}
						return text.Off
					}
					if it.Value != nil {
						return it.Value()
					}
					return ""
				},
				StyleFunc: func() core.Style {
					p := resolvePalette()
					c := p.Accent
					if it.Checked != nil && it.Checked() {
						c = p.Highlight
					}
					c = EnsureContrast(c, p.Background, 4.5)
					return core.Style{
						Color:      c,
						FontSize:   14 * resolveScale(),
						FontWeight: core.FontWeightBold,
					}
				},
			})
		}

		itemView := core.View{
			Ref:                          itemRefs[idx],
			TestID:                       itemTestID,
			Role:                         role,
			Label:                        it.Label,
			LabelFunc:                    it.LabelFunc,
			Hint:                         it.Hint,
			AccessibilityDescription:     it.Description,
			AccessibilityDescriptionFunc: it.DescriptionFunc,
			AccessibilityValue:           a11yVal,
			Checked:                      it.Checked,
			Selected:                     func() bool { return activeIdx.Get() == idx },
			Disabled:                     it.Disabled,
			Accessible:                   true,
			Focusable:                    func() bool { return !isDisabled(idx) },
			OnFocus: func() {
				if activeIdx.Get() != idx {
					activeIdx.Set(idx)
				}
			},
			OnKeyDown: handleKey,
			OnPress: func(core.PressEvent) {
				activateItem(idx)
			},
			AccessibilityActions: []core.AccessibilityAction{
				{Name: core.ActionActivate},
				{Name: core.ActionEscape},
			},
			OnAccessibilityAction: func(name string) {
				switch name {
				case core.ActionActivate:
					activateItem(idx)
				case core.ActionEscape:
					closeMenu()
				}
			},
			StyleFunc: func() core.Style {
				p := resolvePalette()
				selected := activeIdx.Get() == idx
				borderColor := core.Transparent
				borderW := core.Points(2)
				bg := p.Background
				if selected {
					borderColor = EnsureContrast(p.Highlight, p.Background, 4.5)
				}
				return core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            12,
					Padding:        core.Edges{Top: 10, Right: 14, Bottom: 10, Left: 14},
					CornerRadius:   6,
					Background:     bg,
					BorderWidth:    borderW,
					BorderColor:    borderColor,
				}
			},
			Children: rowNodes,
		}
		children = append(children, itemView)
	}

	return core.View{
		Ref:                      menuRef,
		TestID:                   testID,
		Role:                     core.RoleMenu,
		Label:                    menuLabel,
		Hint:                     props.Hint,
		AccessibilityDescription: props.Description,
		LiveRegion:               core.LiveRegionPolite,
		Focusable:                func() bool { return true },
		OnKeyDown:                handleKey,
		AccessibilityActions: []core.AccessibilityAction{
			{Name: core.ActionEscape},
		},
		OnAccessibilityAction: func(name string) {
			if name == core.ActionEscape {
				closeMenu()
			}
		},
		StyleFunc: func() core.Style {
			p := resolvePalette()
			base := core.Style{
				Direction:    core.Column,
				Gap:          6,
				Padding:      core.Edges{Top: 10, Right: 10, Bottom: 10, Left: 10},
				Background:   p.Background,
				BorderWidth:  2,
				BorderColor:  EnsureContrast(p.Neutral, p.Background, 3.0),
				CornerRadius: 10,
			}
			return base.Merge(props.Style)
		},
		Children: children,
	}
}
