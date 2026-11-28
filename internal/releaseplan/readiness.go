package releaseplan

import (
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

// ReadinessStatus indicates whether a platform readiness requirement passes, warns, or blocks building.
type ReadinessStatus string

const (
	ReadinessOK   ReadinessStatus = "ok"
	ReadinessWarn ReadinessStatus = "warn"
	ReadinessFail ReadinessStatus = "fail"
)

// ReadinessItem describes one platform readiness check shared by the scene editor and CLI doctor.
type ReadinessItem struct {
	ID             string          `json:"id"`
	Label          string          `json:"label"`
	Detail         string          `json:"detail"`
	Status         ReadinessStatus `json:"status"`
	ActionLabel    string          `json:"actionLabel,omitempty"`
	AgentCanHandle bool            `json:"agentCanHandle,omitempty"`
	RequiresPerson bool            `json:"requiresPerson,omitempty"`
}

// DefaultTouchLayoutRelPath is the project-relative touch layout file written when generating touch controls from game.yaml actions.
const DefaultTouchLayoutRelPath = "controls.touch.yaml"

// EvaluateReadiness computes the release readiness checklist for platform against the project at dir and f.
// Currently evaluates iOS checks against nsgo.yaml, resources/PrivacyInfo.xcprivacy, *.touch.yaml layouts, and game.yaml.
func EvaluateReadiness(platform, dir string, f *project.File) []ReadinessItem {
	var items []ReadinessItem
	if platform == "ios" {
		items = evaluateIOSReadiness(dir, f)
	}
	for _, item := range CheckResources(dir, f) {
		if item.Status != ReadinessOK {
			items = append(items, item)
		}
	}
	return items
}

func readOptionalNSGoSettings(dir string) (cfg NSGoSettings, present bool, err error) {
	if dir == "" {
		return NSGoSettings{}, false, nil
	}
	path := filepath.Join(dir, project.NSGoFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NSGoSettings{}, false, nil
	}
	if err != nil {
		return NSGoSettings{}, true, err
	}
	cfg, err = parseNSGoSettings(path, data)
	return cfg, true, err
}

func evaluateIOSReadiness(dir string, f *project.File) []ReadinessItem {
	nsgo, nsgoPresent, nsgoErr := readOptionalNSGoSettings(dir)

	var items []ReadinessItem

	// 1. Bundle id (nsgo.yaml id, synced from game.yaml id).
	bundleItem, bundleID := inspectIOSBundleID(dir, f, nsgo, nsgoPresent, nsgoErr)
	items = append(items, bundleItem)

	// 2. App icon (nsgo.yaml icon.apple / icon.png, or defaults assets/AppIcon.icon / assets/Icon.png).
	items = append(items, inspectIOSAppIcon(dir, nsgo, nsgoErr))

	// 3. Provisioning profile (nsgo.yaml sign.profile). Evaluate before distribution cert so cert can reflect profile auto-selection.
	profileItem, profileOK := inspectIOSProvisioningProfile(dir, bundleID, nsgo, nsgoErr)

	// 4. Apple Distribution certificate (nsgo.yaml sign.identity, or auto-selected from keychain for sign.profile).
	certItem := inspectIOSDistributionCert(nsgo, nsgoErr, profileOK)

	// Preserve UI item ordering: bundle_id, app_icon, distribution_cert, provisioning_profile, privacy_manifest, touch_controls.
	items = append(items, certItem, profileItem)

	// 5. Privacy manifest (NSGo writes PrivacyInfo.xcprivacy on every Apple build, merging apple.privacy and resources/PrivacyInfo.xcprivacy).
	items = append(items, inspectIOSPrivacyManifest(dir, nsgo, nsgoErr))

	// 6. Touch controls (*.touch.yaml layouts and game.yaml touchRegion bindings).
	items = append(items, inspectTouchControls(dir, f))

	return items
}

func inspectIOSBundleID(dir string, f *project.File, nsgo NSGoSettings, nsgoPresent bool, nsgoErr error) (ReadinessItem, string) {
	gameID := ""
	if f != nil {
		gameID = strings.TrimSpace(f.ID)
	}
	if nsgoErr != nil {
		return ReadinessItem{
			ID:     "bundle_id",
			Label:  "Bundle id",
			Detail: fmt.Sprintf("could not read nsgo.yaml: %v", nsgoErr),
			Status: ReadinessFail,
		}, gameID
	}
	nsgoID := strings.TrimSpace(nsgo.ID)

	switch {
	case nsgoID != "" && gameID != "" && nsgoID != gameID:
		return ReadinessItem{
			ID:             "bundle_id",
			Label:          "Bundle id",
			Detail:         fmt.Sprintf("nsgo.yaml id (%s) disagrees with game.yaml id (%s); sync nsgo.yaml", nsgoID, gameID),
			Status:         ReadinessFail,
			ActionLabel:    "Sync nsgo.yaml",
			AgentCanHandle: true,
		}, nsgoID
	case nsgoID != "" && gameID != "":
		return ReadinessItem{
			ID:     "bundle_id",
			Label:  "Bundle id " + nsgoID,
			Detail: "from nsgo.yaml (id) and game.yaml",
			Status: ReadinessOK,
		}, nsgoID
	case nsgoID != "":
		return ReadinessItem{
			ID:     "bundle_id",
			Label:  "Bundle id " + nsgoID,
			Detail: "from nsgo.yaml (id)",
			Status: ReadinessOK,
		}, nsgoID
	case gameID != "" && dir != "" && nsgoPresent:
		return ReadinessItem{
			ID:             "bundle_id",
			Label:          "Bundle id",
			Detail:         fmt.Sprintf("id is empty in nsgo.yaml (game.yaml has %s); sync nsgo.yaml", gameID),
			Status:         ReadinessFail,
			ActionLabel:    "Sync nsgo.yaml",
			AgentCanHandle: true,
		}, gameID
	case gameID != "" && dir != "" && !nsgoPresent:
		return ReadinessItem{
			ID:             "bundle_id",
			Label:          "Bundle id",
			Detail:         fmt.Sprintf("nsgo.yaml not found (set id: %s in nsgo.yaml)", gameID),
			Status:         ReadinessFail,
			ActionLabel:    "Sync nsgo.yaml",
			AgentCanHandle: true,
		}, gameID
	case gameID != "":
		return ReadinessItem{
			ID:     "bundle_id",
			Label:  "Bundle id " + gameID,
			Detail: "from game.yaml",
			Status: ReadinessOK,
		}, gameID
	default:
		return ReadinessItem{
			ID:             "bundle_id",
			Label:          "Bundle id",
			Detail:         "set id in nsgo.yaml and game.yaml",
			Status:         ReadinessFail,
			ActionLabel:    "Set bundle id",
			RequiresPerson: true,
		}, ""
	}
}

func inspectIOSAppIcon(dir string, nsgo NSGoSettings, nsgoErr error) ReadinessItem {
	if nsgoErr != nil {
		return ReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         "fix nsgo.yaml to inspect icon.apple and icon.png",
			Status:         ReadinessWarn,
			ActionLabel:    "Check icon…",
			RequiresPerson: true,
		}
	}
	if dir == "" {
		return ReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         "set icon.apple or icon.png in nsgo.yaml, or add assets/AppIcon.icon or assets/Icon.png",
			Status:         ReadinessWarn,
			ActionLabel:    "Check icon…",
			RequiresPerson: true,
		}
	}

	// 1. Explicit icon.apple in nsgo.yaml.
	appleRaw := strings.TrimSpace(nsgo.Icon.Apple)
	icnsOnlyNote := ""
	if appleRaw != "" {
		abs := resolveNSGoPath(dir, appleRaw)
		if _, err := os.Stat(abs); err != nil {
			return ReadinessItem{
				ID:             "app_icon",
				Label:          "App icon",
				Detail:         fmt.Sprintf("icon.apple (%s) in nsgo.yaml not found", appleRaw),
				Status:         ReadinessFail,
				ActionLabel:    "Check icon…",
				RequiresPerson: true,
			}
		}
		switch strings.ToLower(filepath.Ext(appleRaw)) {
		case ".icon":
			return ReadinessItem{
				ID:     "app_icon",
				Label:  "App icon",
				Detail: fmt.Sprintf("%s (from nsgo.yaml icon.apple)", appleRaw),
				Status: ReadinessOK,
			}
		case ".icns":
			// .icns is macOS-only; iOS falls back to icon.png / assets/Icon.png.
			icnsOnlyNote = fmt.Sprintf("icon.apple (%s) is macOS-only (.icns); ", appleRaw)
		default:
			return ReadinessItem{
				ID:             "app_icon",
				Label:          "App icon",
				Detail:         fmt.Sprintf("icon.apple (%s) in nsgo.yaml must be .icon or .icns", appleRaw),
				Status:         ReadinessFail,
				ActionLabel:    "Check icon…",
				RequiresPerson: true,
			}
		}
	} else if _, ok := exactRelativePath(dir, "assets/AppIcon.icon"); ok {
		return ReadinessItem{
			ID:     "app_icon",
			Label:  "App icon",
			Detail: "assets/AppIcon.icon (default icon.apple)",
			Status: ReadinessOK,
		}
	}

	// 2. Explicit icon.png in nsgo.yaml.
	if pngRaw := strings.TrimSpace(nsgo.Icon.PNG); pngRaw != "" {
		abs := resolveNSGoPath(dir, pngRaw)
		return inspectSquarePNGIcon(abs, pngRaw, "from nsgo.yaml icon.png", true)
	}

	// 3. Default assets/Icon.png (case-sensitive check so Linux and macOS agree).
	if abs, ok := exactRelativePath(dir, "assets/Icon.png"); ok {
		return inspectSquarePNGIcon(abs, "assets/Icon.png", "", false)
	}

	return ReadinessItem{
		ID:             "app_icon",
		Label:          "App icon",
		Detail:         icnsOnlyNote + "set icon.apple or icon.png in nsgo.yaml, or add assets/AppIcon.icon or assets/Icon.png (1024 × 1024 square PNG)",
		Status:         ReadinessWarn,
		ActionLabel:    "Check icon…",
		RequiresPerson: true,
	}
}

func inspectSquarePNGIcon(absPath, displayPath, source string, explicit bool) ReadinessItem {
	f, err := os.Open(absPath)
	if err != nil {
		detail := fmt.Sprintf("%s not found", displayPath)
		if explicit {
			detail = fmt.Sprintf("icon.png (%s) in nsgo.yaml not found", displayPath)
		}
		return ReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         detail,
			Status:         ReadinessFail,
			ActionLabel:    "Check icon…",
			RequiresPerson: true,
		}
	}
	cfg, err := png.DecodeConfig(f)
	_ = f.Close()
	if err != nil {
		detail := fmt.Sprintf("%s is not a valid PNG", displayPath)
		if explicit {
			detail = fmt.Sprintf("icon.png (%s) in nsgo.yaml is not a valid PNG", displayPath)
		}
		return ReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         detail,
			Status:         ReadinessFail,
			ActionLabel:    "Check icon…",
			RequiresPerson: true,
		}
	}
	if cfg.Width != cfg.Height || cfg.Width <= 0 {
		detail := fmt.Sprintf("%s is %d × %d; iOS icons must be square PNGs", displayPath, cfg.Width, cfg.Height)
		if explicit {
			detail = fmt.Sprintf("icon.png (%s) in nsgo.yaml is %d × %d; must be square", displayPath, cfg.Width, cfg.Height)
		}
		return ReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         detail,
			Status:         ReadinessFail,
			ActionLabel:    "Check icon…",
			RequiresPerson: true,
		}
	}
	detail := fmt.Sprintf("%s, %d × %d", displayPath, cfg.Width, cfg.Height)
	if source != "" {
		detail += " (" + source + ")"
	}
	return ReadinessItem{
		ID:     "app_icon",
		Label:  "App icon",
		Detail: detail,
		Status: ReadinessOK,
	}
}

func inspectIOSDistributionCert(nsgo NSGoSettings, nsgoErr error, profileOK bool) ReadinessItem {
	if nsgoErr != nil {
		return ReadinessItem{
			ID:             "distribution_cert",
			Label:          "Apple Distribution certificate",
			Detail:         "fix nsgo.yaml to inspect sign.identity",
			Status:         ReadinessWarn,
			RequiresPerson: true,
		}
	}
	if id := strings.TrimSpace(nsgo.Sign.Identity); id != "" {
		return ReadinessItem{
			ID:     "distribution_cert",
			Label:  "Apple Distribution certificate",
			Detail: fmt.Sprintf("%s (from nsgo.yaml sign.identity)", id),
			Status: ReadinessOK,
		}
	}
	if profileOK {
		return ReadinessItem{
			ID:     "distribution_cert",
			Label:  "Apple Distribution certificate",
			Detail: "selected from login keychain for sign.profile, or set sign.identity in nsgo.yaml",
			Status: ReadinessOK,
		}
	}
	return ReadinessItem{
		ID:             "distribution_cert",
		Label:          "Apple Distribution certificate",
		Detail:         "set sign.identity in nsgo.yaml, or configure sign.profile to select the matching keychain identity",
		Status:         ReadinessWarn,
		RequiresPerson: true,
	}
}

func inspectIOSProvisioningProfile(dir, bundleID string, nsgo NSGoSettings, nsgoErr error) (ReadinessItem, bool) {
	if nsgoErr != nil {
		return ReadinessItem{
			ID:             "provisioning_profile",
			Label:          "Provisioning profile",
			Detail:         "fix nsgo.yaml to inspect sign.profile",
			Status:         ReadinessFail,
			ActionLabel:    "Sign in to Apple…",
			RequiresPerson: true,
		}, false
	}
	raw := strings.TrimSpace(nsgo.Sign.Profile)
	if raw == "" {
		detail := "set sign.profile in nsgo.yaml (or pass -profile)"
		if bundleID != "" {
			detail = fmt.Sprintf("set sign.profile in nsgo.yaml for %s (or pass -profile)", bundleID)
		}
		return ReadinessItem{
			ID:             "provisioning_profile",
			Label:          "Provisioning profile",
			Detail:         detail,
			Status:         ReadinessFail,
			ActionLabel:    "Sign in to Apple…",
			RequiresPerson: true,
		}, false
	}
	if dir != "" {
		resolved := resolveNSGoPath(dir, raw)
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return ReadinessItem{
				ID:             "provisioning_profile",
				Label:          "Provisioning profile",
				Detail:         fmt.Sprintf("sign.profile (%s) in nsgo.yaml not found", raw),
				Status:         ReadinessFail,
				ActionLabel:    "Sign in to Apple…",
				RequiresPerson: true,
			}, false
		}
	}
	return ReadinessItem{
		ID:     "provisioning_profile",
		Label:  "Provisioning profile",
		Detail: fmt.Sprintf("%s (from nsgo.yaml sign.profile)", raw),
		Status: ReadinessOK,
	}, true
}

func inspectIOSPrivacyManifest(dir string, nsgo NSGoSettings, nsgoErr error) ReadinessItem {
	if nsgoErr != nil {
		return ReadinessItem{
			ID:     "privacy_manifest",
			Label:  "Privacy manifest",
			Detail: "fix nsgo.yaml to inspect apple.privacy",
			Status: ReadinessWarn,
		}
	}

	var resourceBytes []byte
	resourcePresent := false
	if dir != "" {
		if abs, ok := exactRelativePath(dir, PrivacyManifestRelPath); ok {
			data, err := os.ReadFile(abs)
			if err != nil {
				return ReadinessItem{
					ID:             "privacy_manifest",
					Label:          "Privacy manifest",
					Detail:         fmt.Sprintf("could not read %s: %v", PrivacyManifestRelPath, err),
					Status:         ReadinessFail,
					ActionLabel:    "Write " + PrivacyManifestRelPath,
					AgentCanHandle: true,
				}
			}
			resourceBytes = data
			resourcePresent = true
		}
	}

	hasNSGo := HasNSGoPrivacy(nsgo.Apple.Privacy)

	// Validate resource manifest, apple.privacy, and their merged store manifest.
	if resourcePresent || hasNSGo {
		if _, err := MergePrivacyManifest(resourceBytes, nsgo.Apple.Privacy); err != nil {
			item := ReadinessItem{
				ID:     "privacy_manifest",
				Label:  "Privacy manifest",
				Detail: err.Error(),
				Status: ReadinessFail,
			}
			if resourcePresent {
				item.Detail = fmt.Sprintf("%s: %v", PrivacyManifestRelPath, err)
				item.ActionLabel = "Rewrite " + PrivacyManifestRelPath
				item.AgentCanHandle = true
			} else {
				item.Detail = fmt.Sprintf("nsgo.yaml %v", err)
			}
			return item
		}
	}

	if resourcePresent {
		detail := PrivacyManifestRelPath + " merged with NSGo's defaults"
		if hasNSGo {
			detail = PrivacyManifestRelPath + " and apple.privacy in nsgo.yaml merged with NSGo's defaults"
		}
		return ReadinessItem{
			ID:     "privacy_manifest",
			Label:  "Privacy manifest",
			Detail: detail,
			Status: ReadinessOK,
		}
	}

	// Flag stray legacy manifests at paths NSGo does not package.
	if dir != "" {
		for _, stray := range []string{PrivacyManifestName, "ios/" + PrivacyManifestName, "export/" + PrivacyManifestName} {
			if _, ok := exactRelativePath(dir, stray); ok {
				return ReadinessItem{
					ID:             "privacy_manifest",
					Label:          "Privacy manifest",
					Detail:         fmt.Sprintf("%s is not packaged by nsgo build; move it to %s or configure apple.privacy in nsgo.yaml", stray, PrivacyManifestRelPath),
					Status:         ReadinessWarn,
					ActionLabel:    "Write " + PrivacyManifestRelPath,
					AgentCanHandle: true,
				}
			}
		}
	}

	if hasNSGo {
		return ReadinessItem{
			ID:             "privacy_manifest",
			Label:          "Privacy manifest",
			Detail:         "apple.privacy in nsgo.yaml merged with NSGo's defaults",
			Status:         ReadinessOK,
			ActionLabel:    "Add app manifest",
			AgentCanHandle: true,
		}
	}

	return ReadinessItem{
		ID:             "privacy_manifest",
		Label:          "Privacy manifest",
		Detail:         "NSGo writes PrivacyInfo.xcprivacy on build; configure apple.privacy in nsgo.yaml or " + PrivacyManifestRelPath + " for app-specific declarations",
		Status:         ReadinessOK,
		ActionLabel:    "Add app manifest",
		AgentCanHandle: true,
	}
}

func inspectTouchControls(dir string, f *project.File) ReadinessItem {
	// 1. Look for *.touch.yaml layouts used by input.TouchControls.
	if dir != "" {
		layouts := findTouchLayoutFiles(dir)
		var validPaths []string
		totalControls := 0
		firstBadRel := ""
		firstBadErr := ""
		for _, rel := range layouts {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
			if err != nil {
				if firstBadRel == "" {
					firstBadRel = rel
					firstBadErr = err.Error()
				}
				continue
			}
			l, err := input.ParseTouchLayout(data)
			if err != nil {
				if firstBadRel == "" {
					firstBadRel = rel
					firstBadErr = err.Error()
				}
				continue
			}
			if len(l.Controls) == 0 {
				if firstBadRel == "" {
					firstBadRel = rel
					firstBadErr = "declares no controls"
				}
				continue
			}
			validPaths = append(validPaths, rel)
			totalControls += len(l.Controls)
		}
		if len(validPaths) > 0 {
			detail := fmt.Sprintf("%s (%d %s)", validPaths[0], totalControls, pluralize("control", totalControls))
			if len(validPaths) > 1 {
				detail = fmt.Sprintf("%s and %d more (%d %s)", validPaths[0], len(validPaths)-1, totalControls, pluralize("control", totalControls))
			}
			return ReadinessItem{
				ID:     "touch_controls",
				Label:  "Touch controls",
				Detail: detail,
				Status: ReadinessOK,
			}
		}
		if firstBadRel != "" {
			return ReadinessItem{
				ID:     "touch_controls",
				Label:  "Touch controls",
				Detail: fmt.Sprintf("%s: %s", firstBadRel, firstBadErr),
				Status: ReadinessWarn,
			}
		}
	}

	// 2. Check touchRegion bindings in game.yaml.
	if f != nil && f.Input != nil {
		var names []string
		for _, a := range f.Input.Actions {
			for _, b := range a.Bindings {
				if b.Kind == project.KindTouchRegion {
					names = append(names, a.Name)
					break
				}
			}
		}
		if len(names) > 0 {
			return ReadinessItem{
				ID:     "touch_controls",
				Label:  "Touch controls",
				Detail: fmt.Sprintf("game.yaml touchRegion covers %s", strings.Join(names, ", ")),
				Status: ReadinessOK,
			}
		}
	}

	// 3. Advisory when missing; offer layout generation only when game.yaml declares real actions/axes.
	if _, ok := GenerateTouchLayout(f); ok {
		return ReadinessItem{
			ID:             "touch_controls",
			Label:          "Touch controls",
			Detail:         "no *.touch.yaml layout or touchRegion bindings; generate " + DefaultTouchLayoutRelPath + " for game.yaml actions",
			Status:         ReadinessWarn,
			ActionLabel:    "Generate " + DefaultTouchLayoutRelPath,
			AgentCanHandle: true,
		}
	}

	return ReadinessItem{
		ID:     "touch_controls",
		Label:  "Touch controls",
		Detail: "no *.touch.yaml layout or touchRegion bindings; add a " + DefaultTouchLayoutRelPath + " layout or declare input.actions in game.yaml",
		Status: ReadinessWarn,
	}
}

// GenerateTouchLayout builds a canonical input.TouchLayout from the actions and axes already declared
// in game.yaml (f.Input). It returns false when f declares no actions or axes to bind, so callers
// never invent placeholder actions that game code does not read.
func GenerateTouchLayout(f *project.File) (input.TouchLayout, bool) {
	if f == nil || f.Input == nil {
		return input.TouchLayout{}, false
	}

	var leftAct, rightAct, upAct, downAct string
	var stickX, stickY string
	usedActions := map[string]bool{}

	// Prefer declared directional axes (Negative/Positive action pairs).
	for _, ax := range f.Input.Axes {
		lower := strings.ToLower(ax.Name)
		negLower := strings.ToLower(ax.Negative)
		posLower := strings.ToLower(ax.Positive)
		switch {
		case isHorizontalName(lower) || isLeftName(negLower) || isRightName(posLower):
			if leftAct == "" && ax.Negative != "" {
				leftAct = ax.Negative
			}
			if rightAct == "" && ax.Positive != "" {
				rightAct = ax.Positive
			}
		case isVerticalName(lower) || isDownName(negLower) || isUpName(posLower) || isUpName(negLower) || isDownName(posLower):
			if isUpName(negLower) || isDownName(posLower) {
				if upAct == "" && ax.Negative != "" {
					upAct = ax.Negative
				}
				if downAct == "" && ax.Positive != "" {
					downAct = ax.Positive
				}
			} else {
				if downAct == "" && ax.Negative != "" {
					downAct = ax.Negative
				}
				if upAct == "" && ax.Positive != "" {
					upAct = ax.Positive
				}
			}
		}
	}

	// Fill any missing directional slots from matching action names.
	for _, a := range f.Input.Actions {
		lower := strings.ToLower(a.Name)
		switch {
		case leftAct == "" && isLeftName(lower):
			leftAct = a.Name
		case rightAct == "" && isRightName(lower):
			rightAct = a.Name
		case upAct == "" && isUpName(lower):
			upAct = a.Name
		case downAct == "" && isDownName(lower):
			downAct = a.Name
		}
	}

	// If no directional actions exist, check GamepadAxes for a movement stick.
	if leftAct == "" && rightAct == "" && upAct == "" && downAct == "" {
		for _, ga := range f.Input.GamepadAxes {
			switch ga.Axis {
			case "leftX":
				if stickX == "" {
					stickX = ga.Name
				}
			case "leftY":
				if stickY == "" {
					stickY = ga.Name
				}
			}
		}
	}

	var controls []input.TouchControlSpec
	usedIDs := map[string]bool{}
	claimID := func(base string) string {
		id := sanitizeControlID(base)
		if !usedIDs[id] {
			usedIDs[id] = true
			return id
		}
		for n := 2; ; n++ {
			cand := fmt.Sprintf("%s_%d", id, n)
			if !usedIDs[cand] {
				usedIDs[cand] = true
				return cand
			}
		}
	}

	hasHoriz := leftAct != "" || rightAct != ""
	hasVert := upAct != "" || downAct != ""
	if hasHoriz || hasVert {
		ways := 4
		if hasHoriz && hasVert {
			ways = 8
		}
		controls = append(controls, input.TouchControlSpec{
			ID:     claimID("move"),
			Kind:   input.TouchKindDPad,
			Anchor: input.AnchorBottomLeft,
			Offset: input.TouchOffset{X: 16, Y: 16},
			Size:   input.TouchSize{Width: 112, Height: 112},
			Label:  "Move",
			Ways:   ways,
			Up:     upAct,
			Down:   downAct,
			Left:   leftAct,
			Right:  rightAct,
		})
		for _, name := range []string{leftAct, rightAct, upAct, downAct} {
			if name != "" {
				usedActions[name] = true
			}
		}
	} else if stickX != "" || stickY != "" {
		controls = append(controls, input.TouchControlSpec{
			ID:       claimID("move"),
			Kind:     input.TouchKindStick,
			Anchor:   input.AnchorBottomLeft,
			Offset:   input.TouchOffset{X: 16, Y: 16},
			Size:     input.TouchSize{Width: 140, Height: 140},
			Label:    "Move",
			AxisX:    stickX,
			AxisY:    stickY,
			Floating: true,
		})
	}

	buttonSlots := []struct {
		offset input.TouchOffset
		size   input.TouchSize
	}{
		{offset: input.TouchOffset{X: 20, Y: 16}, size: input.TouchSize{Width: 84, Height: 84}},
		{offset: input.TouchOffset{X: 116, Y: 24}, size: input.TouchSize{Width: 72, Height: 72}},
		{offset: input.TouchOffset{X: 20, Y: 112}, size: input.TouchSize{Width: 72, Height: 72}},
		{offset: input.TouchOffset{X: 116, Y: 108}, size: input.TouchSize{Width: 72, Height: 72}},
	}
	btnIdx := 0
	for _, a := range f.Input.Actions {
		if a.Name == "" || usedActions[a.Name] || btnIdx >= len(buttonSlots) {
			continue
		}
		slot := buttonSlots[btnIdx]
		btnIdx++
		label := humanizeActionName(a.Name)
		controls = append(controls, input.TouchControlSpec{
			ID:     claimID(a.Name),
			Kind:   input.TouchKindButton,
			Anchor: input.AnchorBottomRight,
			Offset: slot.offset,
			Size:   slot.size,
			Label:  label,
			Action: a.Name,
			Text:   strings.ToUpper(label),
		})
	}

	if len(controls) == 0 {
		return input.TouchLayout{}, false
	}

	layout := input.TouchLayout{
		Version:   input.TouchLayoutVersion,
		Reference: input.TouchSize{Width: 844, Height: 390},
		MaxScale:  1.5,
		IdleAfter: 4 * time.Second,
		IdleAlpha: 0.45,
		Controls:  controls,
	}
	if err := input.ValidateTouchLayout(layout); err != nil {
		return input.TouchLayout{}, false
	}
	return layout, true
}

func findTouchLayoutFiles(dir string) []string {
	var found []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && slices.Contains([]string{".git", ".nsgo", "dist", "build", "node_modules"}, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".touch.yaml") {
			if rel, rerr := filepath.Rel(dir, p); rerr == nil {
				found = append(found, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(found)
	return found
}

// resolveNSGoPath matches NSGo's project.resolve: absolute paths stay absolute; relative paths join dir.
func resolveNSGoPath(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, filepath.FromSlash(p))
}

// exactRelativePath checks that slash-separated rel exists inside dir with exact filename casing
// on every segment, so case-insensitive filesystems (macOS) match Linux's case sensitivity.
func exactRelativePath(dir, rel string) (string, bool) {
	if dir == "" || rel == "" {
		return "", false
	}
	cur := dir
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return "", false
		}
		matched := false
		for _, e := range entries {
			if e.Name() == seg {
				matched = true
				break
			}
		}
		if !matched {
			return "", false
		}
		cur = filepath.Join(cur, seg)
	}
	return cur, true
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func isHorizontalName(s string) bool {
	return s == "move_x" || s == "movex" || s == "horizontal" || s == "move" || s == "steer" || s == "strafe"
}

func isVerticalName(s string) bool {
	return s == "move_y" || s == "movey" || s == "vertical"
}

func isLeftName(s string) bool {
	return s == "left" || s == "move_left" || s == "moveleft" || s == "steer_left" || s == "strafe_left"
}

func isRightName(s string) bool {
	return s == "right" || s == "move_right" || s == "moveright" || s == "steer_right" || s == "strafe_right"
}

func isUpName(s string) bool {
	return s == "up" || s == "move_up" || s == "moveup"
}

func isDownName(s string) bool {
	return s == "down" || s == "move_down" || s == "movedown"
}

func sanitizeControlID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "control"
	}
	return b.String()
}

func humanizeActionName(name string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(name), func(r rune) bool {
		return r == '_' || r == '-' || unicode.IsSpace(r)
	})
	if len(parts) == 0 {
		return "Action"
	}
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
