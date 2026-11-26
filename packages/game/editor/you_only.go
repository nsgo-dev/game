package editor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// YouOnlyDomain names one of the four You-only settings domains that the agent may never write directly.
type YouOnlyDomain string

const (
	// YouOnlyPermissions protects permissions.yaml and .nsgo/permissions.yaml.
	YouOnlyPermissions YouOnlyDomain = "permissions"
	// YouOnlyTools protects the MCP tool list and external server list (tools.yaml, mcp.yaml).
	YouOnlyTools YouOnlyDomain = "tools"
	// YouOnlyModels protects model connections and routing settings (models.yaml).
	YouOnlyModels YouOnlyDomain = "models"
	// YouOnlyUsage protects usage limits and privacy toggles (usage.yaml).
	YouOnlyUsage YouOnlyDomain = "usage"
)

// DisplayName returns the human-readable Settings page name for d.
func (d YouOnlyDomain) DisplayName() string {
	switch d {
	case YouOnlyPermissions:
		return "Permissions"
	case YouOnlyTools:
		return "Tools"
	case YouOnlyModels:
		return "Models and connections"
	case YouOnlyUsage:
		return "Usage and privacy"
	default:
		return string(d)
	}
}

// MatchYouOnlyPath reports whether filePath targets a You-only configuration file (permissions, tools, models, or usage)
// that the MCP server refuses every agent write to, regardless of profile or rules.
func MatchYouOnlyPath(filePath string) (YouOnlyDomain, bool) {
	raw := strings.TrimSpace(filePath)
	if raw == "" {
		return "", false
	}
	norm := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(raw, `\`, "/"), "./"))
	cleaned := path.Clean(norm)
	base := path.Base(cleaned)
	inNSGoDir := strings.HasPrefix(cleaned, ".nsgo/") || strings.Contains(cleaned, "/.nsgo/") ||
		strings.HasPrefix(cleaned, ".game/") || strings.Contains(cleaned, "/.game/")

	switch base {
	case "permissions.yaml", "permissions.yml":
		return YouOnlyPermissions, true
	case "tools.yaml", "tools.yml", "mcp.yaml", "mcp.yml", "mcp-servers.yaml", "mcp-servers.yml":
		if cleaned == base || inNSGoDir {
			return YouOnlyTools, true
		}
	case "models.yaml", "models.yml":
		if cleaned == base || inNSGoDir {
			return YouOnlyModels, true
		}
	case "usage.yaml", "usage.yml", "privacy.yaml", "privacy.yml":
		if cleaned == base || inNSGoDir {
			return YouOnlyUsage, true
		}
	case "editor.yaml", "editor.yml":
		if inNSGoDir || strings.Contains(cleaned, "/nsgo-game/") {
			return YouOnlyPermissions, true
		}
	}
	return "", false
}

// resolvePathThroughExistingAncestors resolves symlinks along the deepest existing ancestor of p
// and rejoins any non-existent trailing path segments. This ensures non-existent targets placed
// under a symlinked directory (and macOS /var -> /private/var aliases) resolve to their real path.
func resolvePathThroughExistingAncestors(p string) (string, bool) {
	if strings.TrimSpace(p) == "" {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	cur := abs
	var tail []string
	var resolvedBase string
	for {
		real, rerr := filepath.EvalSymlinks(cur)
		if rerr == nil {
			resolvedBase = real
			break
		}
		if !errors.Is(rerr, fs.ErrNotExist) {
			return "", false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
	slices.Reverse(tail)
	if len(tail) > 0 {
		return filepath.Join(append([]string{resolvedBase}, tail...)...), true
	}
	return resolvedBase, true
}

func sameHostFilePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca := canonicalTabFilePath(a)
	cb := canonicalTabFilePath(b)
	if ra, ok := resolvePathThroughExistingAncestors(a); ok {
		ca = filepath.Clean(ra)
	}
	if rb, ok := resolvePathThroughExistingAncestors(b); ok {
		cb = filepath.Clean(rb)
	}
	if permissionFileCaseInsensitive() {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// CheckYouOnlyTarget reports whether rawPath (relative to the project root or absolute) resolves to a You-only file,
// including Editor.SettingsPath, case-insensitive host aliases, macOS /var -> /private/var, and symlinked ancestor directories.
func (e *Editor) CheckYouOnlyTarget(rawPath string) (YouOnlyDomain, bool) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", false
	}
	if dom, ok := MatchYouOnlyPath(trimmed); ok {
		return dom, true
	}
	if e == nil {
		return "", false
	}

	var candidate string
	var realRoot string
	if e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		if rr, err := filepath.EvalSymlinks(root); err == nil {
			realRoot = rr
		} else if absRoot, aerr := filepath.Abs(root); aerr == nil {
			realRoot = absRoot
		} else {
			realRoot = root
		}
		if filepath.IsAbs(trimmed) {
			candidate = trimmed
		} else {
			candidate = filepath.Join(realRoot, filepath.FromSlash(strings.ReplaceAll(trimmed, `\`, "/")))
		}
	} else {
		candidate = trimmed
	}

	if e.SettingsPath != "" {
		if sameHostFilePath(trimmed, e.SettingsPath) || sameHostFilePath(candidate, e.SettingsPath) {
			return YouOnlyPermissions, true
		}
	}

	if resolved, ok := resolvePathThroughExistingAncestors(candidate); ok {
		if dom, matched := MatchYouOnlyPath(resolved); matched {
			return dom, true
		}
		if e.SettingsPath != "" && sameHostFilePath(resolved, e.SettingsPath) {
			return YouOnlyPermissions, true
		}
		if realRoot != "" {
			if rel, rerr := filepath.Rel(realRoot, resolved); rerr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if dom, matched := MatchYouOnlyPath(filepath.ToSlash(rel)); matched {
					return dom, true
				}
			}
		}
	}
	return "", false
}

// --- Permissions Draft (You-only card that agent submits and only the person can tick and apply) ---

// ErrNoPermissionsDraft is returned when applying a draft while none is open.
var ErrNoPermissionsDraft = errors.New("editor: no permissions draft is open")

// ErrNoDraftRulesTicked is returned when ApplyPermissionsDraft is called with zero rules ticked.
var ErrNoDraftRulesTicked = errors.New("editor: tick at least one rule before applying the permissions draft")

// PermissionsDraftRuleSpec describes one rule change an agent proposes in a permissions draft.
type PermissionsDraftRuleSpec struct {
	Action   PermissionAction   `json:"action"`
	Patterns []string           `json:"patterns"`
	Decision PermissionDecision `json:"decision"`
}

// PermissionsDraftRequest is submitted by the agent (e.g. via permissions_draft MCP tool) to open a You-only draft card.
type PermissionsDraftRequest struct {
	Request string                     `json:"request,omitempty"`
	Rules   []PermissionsDraftRuleSpec `json:"rules"`
}

// PermissionsDraftRule is one row in the open Permissions Draft card.
// Ticked is always false when a draft opens; only the person may tick a rule.
type PermissionsDraftRule struct {
	ID              string             `json:"id"`
	RuleIndex       int                `json:"ruleIndex"` // >= 0 if updating an existing rule; -1 if adding a new rule
	Action          PermissionAction   `json:"action"`
	Patterns        []string           `json:"patterns"`
	CurrentDecision PermissionDecision `json:"currentDecision"`
	DraftDecision   PermissionDecision `json:"draftDecision"`
	Ticked          bool               `json:"ticked"`
}

// PermissionsDraft holds an agent-submitted draft awaiting explicit per-rule ticks and Apply from the person.
type PermissionsDraft struct {
	ID      string                 `json:"id"`
	Request string                 `json:"request,omitempty"`
	Domain  YouOnlyDomain          `json:"domain"`
	Rules   []PermissionsDraftRule `json:"rules"`
}

// Clone returns a deep copy of d.
func (d *PermissionsDraft) Clone() *PermissionsDraft {
	if d == nil {
		return nil
	}
	cp := &PermissionsDraft{
		ID:      d.ID,
		Request: d.Request,
		Domain:  d.Domain,
		Rules:   make([]PermissionsDraftRule, len(d.Rules)),
	}
	for i, r := range d.Rules {
		cp.Rules[i] = PermissionsDraftRule{
			ID:              r.ID,
			RuleIndex:       r.RuleIndex,
			Action:          r.Action,
			Patterns:        slices.Clone(r.Patterns),
			CurrentDecision: r.CurrentDecision,
			DraftDecision:   r.DraftDecision,
			Ticked:          r.Ticked,
		}
	}
	return cp
}

// PermissionsDraftYAMLLine is one line in the "permissions.yaml · if applied" diff preview of the draft card.
type PermissionsDraftYAMLLine struct {
	Kind string // "", "-", "+"
	Text string
}

// --- Held External Permission Loosenings (clone/pull/external tool) ---

// HeldPermissionLooseningKind classifies how an external permissions.yaml change loosened effective permissions.
type HeldPermissionLooseningKind string

const (
	// HeldLooseningRuleRelaxed marks a rule whose decision became looser (e.g. Deny -> Allow or Ask -> Allow).
	HeldLooseningRuleRelaxed HeldPermissionLooseningKind = "rule_relaxed"
	// HeldLooseningRuleRemoved marks a stricter rule that was removed or narrowed externally, loosening effective evaluation.
	HeldLooseningRuleRemoved HeldPermissionLooseningKind = "rule_removed"
	// HeldLooseningProfileRelaxed marks a base profile change to a looser preset (e.g. Balanced -> Autonomous).
	HeldLooseningProfileRelaxed HeldPermissionLooseningKind = "profile_relaxed"
)

// HeldPermissionLoosening represents one loosened rule or profile from an external permissions.yaml change
// that is held (not used) until the person explicitly confirms it in Settings › Permissions.
type HeldPermissionLoosening struct {
	ID               string                      `json:"id"`
	Kind             HeldPermissionLooseningKind `json:"kind"`
	RuleIndex        int                         `json:"ruleIndex"`
	Action           PermissionAction            `json:"action,omitempty"`
	Patterns         []string                    `json:"patterns,omitempty"`
	CurrentProfile   PermissionProfile           `json:"currentProfile,omitempty"`
	IncomingProfile  PermissionProfile           `json:"incomingProfile,omitempty"`
	CurrentDecision  PermissionDecision          `json:"currentDecision"`
	IncomingDecision PermissionDecision          `json:"incomingDecision"`
	Source           string                      `json:"source,omitempty"`
	Summary          string                      `json:"summary"`
}

func defaultToolEnabledMap() map[string]bool {
	return map[string]bool{
		"game_delete_file": false, // Off by default per Settings.dc.html and #1015: can only propose deletions.
		"code_edit":        false,
	}
}

// ToolEnabled reports whether the named MCP tool is enabled in Settings › Tools (You-only).
// Tools not explicitly disabled default to true; game_delete_file defaults to false.
func (e *Editor) ToolEnabled(toolName string) bool {
	if e == nil {
		return false
	}
	return e.IsMCPToolEnabled(toolName)
}

// SetToolEnabled toggles an MCP tool in Settings › Tools (person-only).
func (e *Editor) SetToolEnabled(toolName string, enabled bool) {
	if e == nil || strings.TrimSpace(toolName) == "" {
		return
	}
	if err := e.SetMCPToolEnabled(toolName, enabled); err != nil {
		return
	}
	e.ensurePermissionsInit()
	e.perms.toolEnabled[toolName] = enabled
	e.bumpPermissionsRev()
}

func profileStrictness(p PermissionProfile) int {
	switch p {
	case PermissionProfileCautious:
		return 2
	case PermissionProfileBalanced:
		return 1
	case PermissionProfileAutonomous:
		return 0
	default:
		return 2 // fail-closed
	}
}

func heldSourceLabel(src string) string {
	if strings.TrimSpace(src) == "" {
		return "external change"
	}
	return strings.TrimSpace(src)
}

func (e *Editor) annotateHeldEvaluation(eval *PermissionEvaluation) {
	if e == nil || eval == nil || len(e.perms.held) == 0 {
		return
	}
	for _, h := range e.perms.held {
		matched := false
		switch h.Kind {
		case HeldLooseningProfileRelaxed:
			if eval.RuleIndex < 0 {
				matched = true
			}
		case HeldLooseningRuleRelaxed:
			if h.RuleIndex >= 0 && h.RuleIndex == eval.RuleIndex {
				matched = true
			}
		case HeldLooseningRuleRemoved:
			if actionMatchesRule(eval.Action, h.Action, eval.Target) {
				for _, pat := range h.Patterns {
					if MatchPermissionPattern(h.Action, pat, eval.Target) {
						matched = true
						break
					}
				}
			}
		}
		if matched {
			eval.HeldByDisk = true
			if !strings.Contains(eval.Reason, "held from") {
				eval.Reason = fmt.Sprintf("%s (held from %s until confirmed in Settings › Permissions)", eval.Reason, heldSourceLabel(h.Source))
			}
			return
		}
	}
}

// HeldPermissionLoosenings returns a copy of externally loosened permission rules/profile changes
// currently held until confirmed by the person in Settings › Permissions.
func (e *Editor) HeldPermissionLoosenings() []HeldPermissionLoosening {
	if e == nil || len(e.perms.held) == 0 {
		return nil
	}
	out := make([]HeldPermissionLoosening, len(e.perms.held))
	for i, h := range e.perms.held {
		cp := h
		cp.Patterns = slices.Clone(h.Patterns)
		out[i] = cp
	}
	return out
}

// ReconcileExternalPermissions reconciles an externally loaded PermissionsFile (from git pull,
// cloning a repo, or an external editor) against prev:
//   - Tightened profiles and rules apply at once (counted in tightened).
//   - Loosened profiles (e.g. Balanced -> Autonomous), loosened rules (Deny -> Ask/Allow, Ask -> Allow),
//     and removed/narrowed stricter rules are HELD in held while effective continues enforcing the stricter
//     decision until the person explicitly confirms each entry in Settings › Permissions.
func ReconcileExternalPermissions(prev, incoming PermissionsFile, source string) (effective PermissionsFile, held []HeldPermissionLoosening, tightened int) {
	eff := incoming.Clone()
	if eff.Version == 0 {
		eff.Version = PermissionsVersion
	}
	if !eff.Profile.Valid() {
		eff.Profile = PermissionProfileBalanced
	}
	prevProfile := prev.Profile
	if !prevProfile.Valid() {
		prevProfile = PermissionProfileBalanced
	}

	nextID := 0
	addHeldRule := func(kind HeldPermissionLooseningKind, ruleIdx int, action PermissionAction, pats []string, curDec, incDec PermissionDecision) {
		nextID++
		summary := fmt.Sprintf("%s · %s: %s → %s (%s)", action.DisplayName(), FormatPermissionPatterns(action, pats), curDec.DisplayName(), incDec.DisplayName(), heldSourceLabel(source))
		held = append(held, HeldPermissionLoosening{
			ID:               fmt.Sprintf("held-%d", nextID),
			Kind:             kind,
			RuleIndex:        ruleIdx,
			Action:           action,
			Patterns:         slices.Clone(pats),
			CurrentDecision:  curDec,
			IncomingDecision: incDec,
			Source:           strings.TrimSpace(source),
			Summary:          summary,
		})
	}

	// 0. Check profile-level loosening/tightening (e.g. Balanced -> Autonomous loosens default fallback!).
	prevProfStrict := profileStrictness(prevProfile)
	incProfStrict := profileStrictness(eff.Profile)
	if incProfStrict < prevProfStrict {
		incProf := eff.Profile
		eff.Profile = prevProfile
		nextID++
		held = append(held, HeldPermissionLoosening{
			ID:               fmt.Sprintf("held-%d", nextID),
			Kind:             HeldLooseningProfileRelaxed,
			RuleIndex:        -1,
			CurrentProfile:   prevProfile,
			IncomingProfile:  incProf,
			CurrentDecision:  PermissionAsk,
			IncomingDecision: PermissionAllow,
			Source:           strings.TrimSpace(source),
			Summary:          fmt.Sprintf("Profile · %s → %s (%s)", prevProfile.DisplayName(), incProf.DisplayName(), heldSourceLabel(source)),
		})
	} else if incProfStrict > prevProfStrict {
		tightened++
	}

	// 1. Check each incoming rule against prev's effective decision for its patterns.
	for i := range eff.Rules {
		r := eff.Rules[i]
		prevStrictest := PermissionAllow
		hasPrev := false
		for _, pat := range r.Patterns {
			ev := prev.Evaluate(PermissionRequest{Action: r.Action, Target: representativePatternTarget(r.Action, pat)})
			if !hasPrev || ev.Decision.Strictness() > prevStrictest.Strictness() {
				prevStrictest = ev.Decision
				hasPrev = true
			}
		}
		if !hasPrev {
			prevStrictest = prev.Evaluate(PermissionRequest{Action: r.Action}).Decision
		}
		if r.Decision.Strictness() < prevStrictest.Strictness() {
			eff.Rules[i].Decision = prevStrictest
			addHeldRule(HeldLooseningRuleRelaxed, i, r.Action, r.Patterns, prevStrictest, r.Decision)
		} else if r.Decision.Strictness() > prevStrictest.Strictness() {
			tightened++
		}
	}

	// 2. Check whether any stricter rule from prev was removed or narrowed in incoming.
	var guardRules []PermissionRule
	for _, pr := range prev.Rules {
		if pr.Decision == PermissionAllow {
			continue
		}
		var loosenedPats []string
		loosestIncoming := pr.Decision
		for _, pat := range pr.Patterns {
			target := representativePatternTarget(pr.Action, pat)
			ev := eff.Evaluate(PermissionRequest{Action: pr.Action, Target: target})
			if ev.Decision.Strictness() < pr.Decision.Strictness() {
				loosenedPats = append(loosenedPats, pat)
				if ev.Decision.Strictness() < loosestIncoming.Strictness() {
					loosestIncoming = ev.Decision
				}
			}
		}
		if len(loosenedPats) > 0 {
			guardRules = append(guardRules, PermissionRule{
				Action:   pr.Action,
				Patterns: loosenedPats,
				Decision: pr.Decision,
			})
			addHeldRule(HeldLooseningRuleRemoved, -1, pr.Action, loosenedPats, pr.Decision, loosestIncoming)
		}
	}
	if len(guardRules) > 0 {
		// Prepend guard rules so first-match-wins enforces the stricter previous rules until confirmed.
		eff.Rules = append(guardRules, eff.Rules...)
		for i := range held {
			if held[i].Kind == HeldLooseningRuleRelaxed && held[i].RuleIndex >= 0 {
				held[i].RuleIndex += len(guardRules)
			}
		}
	}

	return eff, held, tightened
}

func representativePatternTarget(action PermissionAction, pat string) string {
	p := strings.TrimSpace(pat)
	if action == PermissionActionRun {
		return normalizeRunToken(p)
	}
	p = strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), "./")
	if p == "**" || p == "*" {
		return "file.yaml"
	}
	segs := strings.Split(p, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		switch {
		case s == "**":
			out = append(out, "sub")
		case strings.Contains(s, "*"):
			out = append(out, strings.ReplaceAll(s, "*", "sample"))
		case strings.Contains(s, "?"):
			out = append(out, strings.ReplaceAll(s, "?", "a"))
		default:
			out = append(out, s)
		}
	}
	return strings.Join(out, "/")
}

func readBoundedPermissionsFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPermissionsFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPermissionsFileBytes {
		return nil, fmt.Errorf("editor: %s exceeds maximum size of %d bytes", PermissionsFileName, maxPermissionsFileBytes)
	}
	return data, nil
}

// loadProjectPermissionsOnOpen loads permissions.yaml from projectDir when opening a project.
// Any rule stricter than Balanced applies at once; any rule or profile looser than Balanced
// (for example from a cloned repo) is held until confirmed by the person in Settings › Permissions.
// Invalid permissions.yaml fails closed (PermissionsErr != nil, Cautious in memory, EvaluatePermission == Deny).
func (e *Editor) loadProjectPermissionsOnOpen(projectDir string) {
	if e == nil {
		return
	}
	e.ensurePermissionsInit()
	p := PermissionsFilePath(projectDir)
	e.perms.path = p
	e.perms.held = nil
	e.perms.loadErr = nil
	if p == "" {
		e.perms.file = DefaultPermissions()
		e.perms.hasFileOnDisk = false
		e.perms.diskSnap = diskSnapshot{}
		e.perms.diskFile = nil
		e.syncBuildReleasePermissionProfile()
		e.perms.rulesRev++
		e.perms.rev++
		return
	}
	data, err := readBoundedPermissionsFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.perms.file = DefaultPermissions()
			e.perms.hasFileOnDisk = false
			e.perms.diskSnap = diskSnapshot{path: filepath.Clean(p), exists: false}
			e.perms.diskFile = nil
			e.syncBuildReleasePermissionProfile()
			e.perms.rulesRev++
			e.perms.rev++
			return
		}
		e.perms.loadErr = err
		e.perms.file = PermissionPreset(PermissionProfileCautious)
		e.syncBuildReleasePermissionProfile()
		e.perms.rulesRev++
		e.perms.rev++
		return
	}
	e.perms.hasFileOnDisk = true
	e.perms.diskSnap = makeDiskSnapshot(p, data)
	parsed, perr := ParsePermissions(data)
	if perr != nil {
		e.perms.loadErr = loaderr.WithFile(perr, p)
		e.perms.file = PermissionPreset(PermissionProfileCautious)
		e.syncBuildReleasePermissionProfile()
		e.perms.rulesRev++
		e.perms.rev++
		return
	}
	cp := parsed.Clone()
	e.perms.diskFile = &cp
	eff, held, _ := ReconcileExternalPermissions(DefaultPermissions(), *parsed, "cloned project")
	e.perms.file = eff
	e.perms.held = held
	e.syncBuildReleasePermissionProfile()
	e.perms.rulesRev++
	e.perms.rev++
}

// checkPermissionsDiskChanges inspects permissions.yaml on disk for external changes (git pull, external edit, new file).
// Tightened rules apply at once; loosened rules/profiles are held until confirmed in Settings › Permissions.
// Returns true if permissions state or held loosenings changed.
func (e *Editor) checkPermissionsDiskChanges(source string) bool {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return false
	}
	e.ensurePermissionsInit()
	p := e.PermissionsPath()
	if p == "" {
		return false
	}
	data, err := readBoundedPermissionsFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if !e.perms.hasFileOnDisk {
				return false
			}
			// permissions.yaml was deleted externally: reconcile against DefaultPermissions (never loosen silently).
			e.perms.hasFileOnDisk = false
			e.perms.diskSnap = diskSnapshot{path: filepath.Clean(p), exists: false}
			e.perms.diskFile = nil
			src := source
			if src == "" {
				src = "Changed outside the editor"
			}
			eff, held, _ := ReconcileExternalPermissions(e.perms.file, DefaultPermissions(), src)
			e.perms.file = eff
			e.perms.held = held
			e.perms.loadErr = nil
			e.bumpPermissionsRulesRev()
			return true
		}
		// Unreadable or oversized permissions.yaml: fail closed.
		e.perms.loadErr = err
		e.perms.file = PermissionPreset(PermissionProfileCautious)
		e.bumpPermissionsRulesRev()
		return true
	}
	sum := sha256.Sum256(data)
	if e.perms.diskSnap.exists && e.perms.diskSnap.hash == sum {
		return false
	}
	e.perms.hasFileOnDisk = true
	e.perms.diskSnap = makeDiskSnapshot(p, data)

	parsed, perr := ParsePermissions(data)
	if perr != nil {
		e.perms.loadErr = loaderr.WithFile(perr, p)
		e.perms.file = PermissionPreset(PermissionProfileCautious)
		e.bumpPermissionsRulesRev()
		return true
	}
	e.perms.loadErr = nil
	cp := parsed.Clone()
	e.perms.diskFile = &cp

	src := source
	if src == "" {
		src = "Changed outside the editor"
	}
	eff, held, tightened := ReconcileExternalPermissions(e.perms.file, *parsed, src)
	e.perms.file = eff
	e.perms.held = held
	e.bumpPermissionsRulesRev()

	if len(held) > 0 {
		msg := fmt.Sprintf("Held %d loosened permission rule from %s · confirm in Settings › Permissions", len(held), src)
		if len(held) > 1 {
			msg = fmt.Sprintf("Held %d loosened permission rules from %s · confirm in Settings › Permissions", len(held), src)
		}
		e.ShowToast(Toast{
			ID:          "permissions-held-loosening",
			Kind:        ToastWarning,
			Message:     msg,
			ActionLabel: "Review",
			OnAction: func() {
				e.OpenSettingsPage(SettingsPagePermissions)
			},
		})
	} else if tightened > 0 {
		e.ShowToast(Toast{
			ID:      "permissions-tightened",
			Kind:    ToastInfo,
			Message: fmt.Sprintf("Applied %d tightened permission rule(s) from %s", tightened, src),
		})
	}
	return true
}

// ConfirmHeldPermissionLoosening confirms one externally loosened rule or profile change by ID in Settings › Permissions (person-only).
// Only that entry is loosened in effective permissions, recorded in History as the person's.
func (e *Editor) ConfirmHeldPermissionLoosening(id string) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	idx := slices.IndexFunc(e.perms.held, func(h HeldPermissionLoosening) bool { return h.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: held permission loosening %q not found", id)
	}
	h := e.perms.held[idx]
	beforeEff := e.perms.file.Clone()
	beforeHeld := slices.Clone(e.perms.held)

	afterEff := beforeEff.Clone()
	removedIdx := -1
	switch h.Kind {
	case HeldLooseningProfileRelaxed:
		if h.IncomingProfile.Valid() {
			afterEff.Profile = h.IncomingProfile
		}
	case HeldLooseningRuleRelaxed:
		if h.RuleIndex >= 0 && h.RuleIndex < len(afterEff.Rules) {
			afterEff.Rules[h.RuleIndex].Decision = h.IncomingDecision
		}
	case HeldLooseningRuleRemoved:
		// Remove the guard rule that matched (h.Action, h.Patterns, h.CurrentDecision).
		for i, r := range afterEff.Rules {
			if r.Action == h.Action && r.Decision == h.CurrentDecision && slices.Equal(r.Patterns, h.Patterns) {
				afterEff.Rules = slices.Delete(afterEff.Rules, i, i+1)
				removedIdx = i
				break
			}
		}
	}
	afterHeld := slices.Delete(slices.Clone(beforeHeld), idx, idx+1)
	if removedIdx >= 0 {
		// Only decrement RuleIndex for relaxed entries located strictly AFTER the removed guard rule.
		for i := range afterHeld {
			if afterHeld[i].Kind == HeldLooseningRuleRelaxed && afterHeld[i].RuleIndex > removedIdx {
				afterHeld[i].RuleIndex--
			}
		}
	}

	cmd := &permissionsCmd{
		ed:         e,
		label:      "Confirm permission rule: " + h.Summary,
		actor:      HistoryActorPerson,
		beforeFile: beforeEff,
		afterFile:  afterEff,
		beforeHeld: beforeHeld,
		afterHeld:  afterHeld,
		persist:    false, // Disk already holds the incoming file.
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}
	e.ShowToast(Toast{
		ID:          "permissions-confirmed",
		Kind:        ToastSuccess,
		Message:     fmt.Sprintf("Confirmed by you · %s", h.Summary),
		ActionLabel: "Undo",
		OnAction: func() {
			e.Undo()
		},
	})
	return nil
}

// RejectHeldPermissionLoosening rejects an externally loosened rule or profile change, keeping the stricter rule active
// and writing the stricter effective permissions back to permissions.yaml so disk matches.
func (e *Editor) RejectHeldPermissionLoosening(id string) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	idx := slices.IndexFunc(e.perms.held, func(h HeldPermissionLoosening) bool { return h.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: held permission loosening %q not found", id)
	}
	h := e.perms.held[idx]
	beforeEff := e.perms.file.Clone()
	beforeHeld := slices.Clone(e.perms.held)
	afterHeld := slices.Delete(slices.Clone(beforeHeld), idx, idx+1)

	cmd := &permissionsCmd{
		ed:         e,
		label:      "Keep stricter permission rule: " + h.Summary,
		actor:      HistoryActorPerson,
		beforeFile: beforeEff,
		afterFile:  beforeEff.Clone(),
		beforeHeld: beforeHeld,
		afterHeld:  afterHeld,
		persist:    e.PermissionsPath() != "",
	}
	return e.History.execute(cmd)
}

// SubmitPermissionsDraft opens the You-only Permissions Draft card with every rule unticked by default.
// Submitting a draft never changes effective rules or permissions.yaml on disk.
func (e *Editor) SubmitPermissionsDraft(req PermissionsDraftRequest) (*PermissionsDraft, error) {
	if e == nil {
		return nil, ErrNoProject
	}
	e.ensurePermissionsInit()
	if len(req.Rules) == 0 {
		return nil, loaderr.Errorf("rules", "include at least one permission rule in the draft", "editor: permissions draft requires at least one rule")
	}
	if len(req.Rules) > 32 {
		return nil, loaderr.Errorf("rules", "propose at most 32 rules per draft", "editor: permissions draft exceeds maximum of 32 rules")
	}

	cur := e.Permissions()
	rules := make([]PermissionsDraftRule, 0, len(req.Rules))
	for i, spec := range req.Rules {
		if !spec.Action.Valid() {
			return nil, loaderr.Errorf(fmt.Sprintf("rules[%d].action", i), fixPermissionsAction, "editor: invalid rule action %q", spec.Action)
		}
		if !spec.Decision.Valid() {
			return nil, loaderr.Errorf(fmt.Sprintf("rules[%d].decision", i), fixPermissionsDecision, "editor: invalid rule decision %q", spec.Decision)
		}
		if len(spec.Patterns) == 0 {
			return nil, loaderr.Errorf(fmt.Sprintf("rules[%d].patterns", i), fixPermissionsPattern, "editor: rule %d must have at least one pattern", i+1)
		}
		if len(spec.Patterns) > maxPermissionsPatternsPerRow {
			return nil, loaderr.Errorf(fmt.Sprintf("rules[%d].patterns", i), fixPermissionsPattern, "editor: rule %d exceeds maximum of %d patterns", i+1, maxPermissionsPatternsPerRow)
		}
		cleanPats := make([]string, len(spec.Patterns))
		for j, pat := range spec.Patterns {
			if err := validatePermissionPattern(spec.Action, pat); err != nil {
				return nil, loaderr.Errorf(fmt.Sprintf("rules[%d].patterns[%d]", i, j), fixPermissionsPattern, "%v", err)
			}
			cleanPats[j] = strings.TrimSpace(pat)
		}

		matchedIdx := findMatchingRuleIndex(cur, spec.Action, cleanPats)
		curDec := PermissionAsk
		if matchedIdx >= 0 {
			curDec = cur.Rules[matchedIdx].Decision
		} else {
			curDec = cur.Evaluate(PermissionRequest{
				Action: spec.Action,
				Target: representativePatternTarget(spec.Action, cleanPats[0]),
			}).Decision
		}

		rules = append(rules, PermissionsDraftRule{
			ID:              fmt.Sprintf("draft-rule-%d", i+1),
			RuleIndex:       matchedIdx,
			Action:          spec.Action,
			Patterns:        cleanPats,
			CurrentDecision: curDec,
			DraftDecision:   spec.Decision,
			Ticked:          false, // Always unticked by default!
		})
	}

	e.perms.draftSeq++
	draft := &PermissionsDraft{
		ID:      fmt.Sprintf("perm-draft-%d", e.perms.draftSeq),
		Request: strings.TrimSpace(req.Request),
		Domain:  YouOnlyPermissions,
		Rules:   rules,
	}
	e.perms.draft = draft
	e.bumpPermissionsRev()
	return draft.Clone(), nil
}

func findMatchingRuleIndex(f PermissionsFile, action PermissionAction, patterns []string) int {
	for i, r := range f.Rules {
		if r.Action == action && slices.Equal(r.Patterns, patterns) {
			return i
		}
	}
	return -1
}

// PermissionsDraftOpen reports whether an agent-submitted You-only permissions draft card is currently open.
func (e *Editor) PermissionsDraftOpen() bool {
	return e != nil && e.perms.draft != nil
}

// PermissionsDraft returns a copy of the currently open permissions draft, or nil.
func (e *Editor) PermissionsDraft() *PermissionsDraft {
	if e == nil || e.perms.draft == nil {
		return nil
	}
	return e.perms.draft.Clone()
}

// PermissionsDraftTickedCount returns how many rules are currently ticked in the open permissions draft.
func (e *Editor) PermissionsDraftTickedCount() int {
	if e == nil || e.perms.draft == nil {
		return 0
	}
	n := 0
	for _, r := range e.perms.draft.Rules {
		if r.Ticked {
			n++
		}
	}
	return n
}

// SetPermissionsDraftRuleTicked sets the checkbox state of rule index in the open permissions draft (person-only).
func (e *Editor) SetPermissionsDraftRuleTicked(index int, ticked bool) {
	if e == nil || e.perms.draft == nil || index < 0 || index >= len(e.perms.draft.Rules) {
		return
	}
	if e.perms.draft.Rules[index].Ticked == ticked {
		return
	}
	e.perms.draft.Rules[index].Ticked = ticked
	e.bumpPermissionsRev()
}

// TogglePermissionsDraftRule toggles the checkbox state of rule index in the open permissions draft (person-only).
func (e *Editor) TogglePermissionsDraftRule(index int) {
	if e == nil || e.perms.draft == nil || index < 0 || index >= len(e.perms.draft.Rules) {
		return
	}
	e.SetPermissionsDraftRuleTicked(index, !e.perms.draft.Rules[index].Ticked)
}

// PermissionsDraftPreviewYAML returns the diff lines showing what permissions.yaml will change for the currently ticked rules.
func (e *Editor) PermissionsDraftPreviewYAML() []PermissionsDraftYAMLLine {
	if e == nil || e.perms.draft == nil {
		return nil
	}
	var lines []PermissionsDraftYAMLLine
	for _, r := range e.perms.draft.Rules {
		if !r.Ticked {
			continue
		}
		header := fmt.Sprintf("  - %s: [%s]", r.Action, strings.Join(r.Patterns, ", "))
		lines = append(lines, PermissionsDraftYAMLLine{Kind: "", Text: header})
		if r.RuleIndex >= 0 {
			lines = append(lines, PermissionsDraftYAMLLine{Kind: "-", Text: "-     decision: " + string(r.CurrentDecision)})
		}
		lines = append(lines, PermissionsDraftYAMLLine{Kind: "+", Text: "+     decision: " + string(r.DraftDecision)})
	}
	return lines
}

// ApplyPermissionsDraft applies ONLY the rules explicitly ticked by the person in the open draft card.
// It refuses when zero rules are ticked, records the change in History as the person's (HistoryActorPerson),
// persists permissions.yaml when a project is open, and never runs from ⌘↵, Keep all, or automations.
func (e *Editor) ApplyPermissionsDraft() (int, error) {
	if e == nil || e.perms.draft == nil {
		return 0, ErrNoPermissionsDraft
	}
	e.ensurePermissionsInit()
	draft := e.perms.draft
	before := e.perms.file.Clone()
	after := before.Clone()

	applied := 0
	var insertRules []PermissionRule
	for _, r := range draft.Rules {
		if !r.Ticked {
			continue
		}
		applied++
		if idx := findMatchingRuleIndex(after, r.Action, r.Patterns); idx >= 0 {
			after.Rules[idx].Decision = r.DraftDecision
		} else {
			insertRules = append(insertRules, PermissionRule{
				Action:   r.Action,
				Patterns: slices.Clone(r.Patterns),
				Decision: r.DraftDecision,
			})
		}
	}
	if applied == 0 {
		return 0, ErrNoDraftRulesTicked
	}
	if len(insertRules) > 0 {
		// Prepend new specific rules so first-match-wins honors them before broader catch-alls.
		after.Rules = append(insertRules, after.Rules...)
	}
	if err := ValidatePermissions(&after); err != nil {
		return 0, err
	}

	label := fmt.Sprintf("Applied %d permission rule (by you)", applied)
	toastMsg := fmt.Sprintf("Applied %d rule by you · Settings › Permissions", applied)
	if applied > 1 {
		label = fmt.Sprintf("Applied %d permission rules (by you)", applied)
		toastMsg = fmt.Sprintf("Applied %d rules by you · Settings › Permissions", applied)
	}

	cmd := &permissionsCmd{
		ed:         e,
		label:      label,
		actor:      HistoryActorPerson,
		beforeFile: before,
		afterFile:  after,
		beforeHeld: slices.Clone(e.perms.held),
		afterHeld:  slices.Clone(e.perms.held),
		persist:    e.PermissionsPath() != "",
	}
	if err := e.History.execute(cmd); err != nil {
		return 0, err
	}
	e.perms.draft = nil
	e.bumpPermissionsRev()
	e.scheduleAutoSaveIfNeeded()
	e.ShowToast(Toast{
		ID:          "permissions-draft-applied",
		Kind:        ToastSuccess,
		Message:     toastMsg,
		ActionLabel: "Undo",
		OnAction: func() {
			e.Undo()
		},
	})
	return applied, nil
}

// DiscardPermissionsDraft closes the open permissions draft card without modifying any rule or file on disk.
func (e *Editor) DiscardPermissionsDraft() {
	if e == nil || e.perms.draft == nil {
		return
	}
	e.perms.draft = nil
	e.bumpPermissionsRev()
	e.scheduleAutoSaveIfNeeded()
	e.ShowToast(Toast{
		ID:      "permissions-draft-discarded",
		Kind:    ToastInfo,
		Message: "Draft discarded · your rules are unchanged",
	})
}

// permissionsCmd is a reversible, fallible History command for person-approved permission changes.
type permissionsCmd struct {
	ed            *Editor
	label         string
	actor         HistoryActor
	beforeFile    PermissionsFile
	afterFile     PermissionsFile
	beforeHeld    []HeldPermissionLoosening
	afterHeld     []HeldPermissionLoosening
	persist       bool
	hadFileBefore bool
	rawBefore     []byte
}

func (c *permissionsCmd) Label() string       { return c.label }
func (c *permissionsCmd) Actor() HistoryActor { return c.actor }
func (c *permissionsCmd) Do()                 { c.ed.report(c.do()) }
func (c *permissionsCmd) Undo()               { c.ed.report(c.undo()) }

func (c *permissionsCmd) do() error {
	if c.persist {
		p := c.ed.PermissionsPath()
		if p != "" {
			if raw, err := readBoundedPermissionsFile(p); err == nil {
				c.hadFileBefore = true
				c.rawBefore = raw
			} else {
				c.hadFileBefore = false
				c.rawBefore = nil
			}
			encoded, err := EncodePermissions(&c.afterFile)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return fmt.Errorf("editor: create permissions directory: %w", err)
			}
			if err := writeFileAtomic(p, encoded); err != nil {
				return err
			}
			c.ed.perms.hasFileOnDisk = true
			c.ed.perms.diskSnap = makeDiskSnapshot(p, encoded)
			cp := c.afterFile.Clone()
			c.ed.perms.diskFile = &cp
		}
	}
	c.ed.perms.file = c.afterFile.Clone()
	c.ed.perms.held = slices.Clone(c.afterHeld)
	c.ed.perms.loadErr = nil
	c.ed.syncDiskWatchTargets()
	c.ed.bumpPermissionsRulesRev()
	return nil
}

func (c *permissionsCmd) undo() error {
	if c.persist {
		p := c.ed.PermissionsPath()
		if p != "" {
			if c.hadFileBefore {
				if err := writeFileAtomic(p, c.rawBefore); err != nil {
					return err
				}
				c.ed.perms.hasFileOnDisk = true
				c.ed.perms.diskSnap = makeDiskSnapshot(p, c.rawBefore)
				if parsed, err := ParsePermissions(c.rawBefore); err == nil {
					cp := parsed.Clone()
					c.ed.perms.diskFile = &cp
				}
			} else {
				if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				c.ed.perms.hasFileOnDisk = false
				c.ed.perms.diskSnap = diskSnapshot{path: filepath.Clean(p), exists: false}
				c.ed.perms.diskFile = nil
			}
		}
	}
	c.ed.perms.file = c.beforeFile.Clone()
	c.ed.perms.held = slices.Clone(c.beforeHeld)
	c.ed.perms.loadErr = nil
	c.ed.syncDiskWatchTargets()
	c.ed.bumpPermissionsRulesRev()
	return nil
}
