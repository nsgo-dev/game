package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/project"
)

const (
	// maxProposedDeletionBytes caps proposed deletion preview/checkpoint snapshots at 1 MiB so huge binary files cannot OOM History.
	maxProposedDeletionBytes = 1 << 20
	// binarySniffLimit is how many leading bytes summarizeFileLines checks for NUL/invalid UTF-8.
	binarySniffLimit = 8192
)

// AgentReviewChangeKind classifies a regular (non-deletion) entry in an agent review.
type AgentReviewChangeKind string

const (
	ReviewChangeModified AgentReviewChangeKind = "modified"
	ReviewChangeAdded    AgentReviewChangeKind = "added"
)

// AgentReviewChange is one non-deletion file change listed in the Review changes panel.
type AgentReviewChange struct {
	ID       string                `json:"id"`
	Path     string                `json:"path"`
	Kind     AgentReviewChangeKind `json:"kind"`
	Title    string                `json:"title"`
	Detail   string                `json:"detail"`
	Reverted bool                  `json:"reverted,omitempty"`
	Kept     bool                  `json:"kept,omitempty"`
	OnKeep   func() error          `json:"-"`
	OnRevert func() error          `json:"-"`
}

// ProposedDeletionStatus tracks the person's explicit decision on a proposed file deletion.
type ProposedDeletionStatus string

const (
	// DeletionPending means the deletion has not been confirmed yet; Keep all NEVER applies it.
	DeletionPending ProposedDeletionStatus = "pending"
	// DeletionConfirmed means the person explicitly clicked "Delete this file" while other review items remain.
	DeletionConfirmed ProposedDeletionStatus = "confirmed"
	// DeletionKept means the person explicitly clicked "Keep the file".
	DeletionKept ProposedDeletionStatus = "kept"
	// DeletionApplied means the file was deleted from disk via a reversible checkpoint command (⌘Z restores it).
	DeletionApplied ProposedDeletionStatus = "applied"
)

// ProposedDeletion describes one file deletion proposed by the agent (e.g. via game_delete_file).
// Keep all never applies a pending deletion; it requires explicit per-file confirmation, and ⌘Z restores the file from its checkpoint.
type ProposedDeletion struct {
	ID             string                 `json:"id"`
	Path           string                 `json:"path"`
	AbsPath        string                 `json:"-"`
	LineCount      int                    `json:"lineCount"`
	SceneUsages    []string               `json:"sceneUsages,omitempty"`
	UsedByAnyScene bool                   `json:"usedByAnyScene"`
	UsageSummary   string                 `json:"usageSummary"`
	Detail         string                 `json:"detail"`
	PreviewLines   []string               `json:"previewLines,omitempty"`
	MoreLines      int                    `json:"moreLines,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Status         ProposedDeletionStatus `json:"status"`
}

// AgentReviewSnapshot is a read-only view of the active Review changes panel.
type AgentReviewSnapshot struct {
	Open      bool                `json:"open"`
	Prompt    string              `json:"prompt,omitempty"`
	Changes   []AgentReviewChange `json:"changes,omitempty"`
	Deletions []ProposedDeletion  `json:"deletions,omitempty"`
}

// KeepAllResult reports what KeepAllReviewChanges did.
type KeepAllResult struct {
	KeptChanges          int      `json:"keptChanges"`
	DeletedFiles         []string `json:"deletedFiles,omitempty"`
	LeftPendingDeletions []string `json:"leftPendingDeletions,omitempty"`
	LeftPermissionsDraft bool     `json:"leftPermissionsDraft,omitempty"`
	ReviewStillOpen      bool     `json:"reviewStillOpen"`
}

type agentReviewState struct {
	open      bool
	prompt    string
	changes   []AgentReviewChange
	deletions []ProposedDeletion
	seq       uint64
	rev       uint64
}

func (e *Editor) resetAgentReviewForProject() {
	if e == nil {
		return
	}
	e.agentReview = agentReviewState{rev: e.agentReview.rev + 1}
}

func (e *Editor) agentReviewRev() uint64 {
	if e == nil {
		return 0
	}
	return e.agentReview.rev
}

// AgentReviewOpen reports whether the Review changes panel is currently open.
func (e *Editor) AgentReviewOpen() bool {
	return e != nil && e.agentReview.open
}

// AgentReview returns a read-only copy of the current Review changes state, or nil when closed.
func (e *Editor) AgentReview() *AgentReviewSnapshot {
	if e == nil || !e.agentReview.open {
		return nil
	}
	changes := make([]AgentReviewChange, len(e.agentReview.changes))
	copy(changes, e.agentReview.changes)
	deletions := make([]ProposedDeletion, len(e.agentReview.deletions))
	for i, d := range e.agentReview.deletions {
		cp := d
		cp.SceneUsages = slices.Clone(d.SceneUsages)
		cp.PreviewLines = slices.Clone(d.PreviewLines)
		deletions[i] = cp
	}
	return &AgentReviewSnapshot{
		Open:      e.agentReview.open,
		Prompt:    e.agentReview.prompt,
		Changes:   changes,
		Deletions: deletions,
	}
}

// OpenAgentReview opens (or updates) the Review changes panel with prompt and non-deletion changes.
func (e *Editor) OpenAgentReview(prompt string, changes []AgentReviewChange) {
	if e == nil {
		return
	}
	e.agentReview.open = true
	if strings.TrimSpace(prompt) != "" {
		e.agentReview.prompt = strings.TrimSpace(prompt)
	}
	e.agentReview.changes = make([]AgentReviewChange, len(changes))
	for i, c := range changes {
		if c.ID == "" {
			e.agentReview.seq++
			c.ID = fmt.Sprintf("chg-%d", e.agentReview.seq)
		}
		if c.Kind == "" {
			c.Kind = ReviewChangeModified
		}
		if c.Title == "" && c.Path != "" {
			c.Title = path.Base(c.Path)
		}
		e.agentReview.changes[i] = c
	}
	e.agentReview.rev++
	e.notify()
}

// AddReviewChange appends a non-deletion change to the active agent review (opening it if closed).
func (e *Editor) AddReviewChange(change AgentReviewChange) {
	if e == nil {
		return
	}
	e.agentReview.open = true
	if change.ID == "" {
		e.agentReview.seq++
		change.ID = fmt.Sprintf("chg-%d", e.agentReview.seq)
	}
	if change.Kind == "" {
		change.Kind = ReviewChangeModified
	}
	if change.Title == "" && change.Path != "" {
		change.Title = path.Base(change.Path)
	}
	e.agentReview.changes = append(e.agentReview.changes, change)
	e.agentReview.rev++
	e.notify()
}

// CloseAgentReview closes the Review changes panel without deleting any unconfirmed files.
func (e *Editor) CloseAgentReview() {
	if e == nil || !e.agentReview.open {
		return
	}
	e.agentReview.open = false
	e.agentReview.rev++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
}

// RevertReviewChange marks one non-deletion review item as reverted and runs its OnRevert hook if present.
func (e *Editor) RevertReviewChange(id string) error {
	if e == nil || !e.agentReview.open {
		return errors.New("editor: no review is open")
	}
	idx := slices.IndexFunc(e.agentReview.changes, func(c AgentReviewChange) bool { return c.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: review change %q not found", id)
	}
	if e.agentReview.changes[idx].Reverted {
		return nil
	}
	if fn := e.agentReview.changes[idx].OnRevert; fn != nil {
		if err := fn(); err != nil {
			return err
		}
	}
	e.agentReview.changes[idx].Reverted = true
	e.RecordAgentReviewOutcome(false, 1)
	e.agentReview.rev++
	e.notify()
	return nil
}

func (e *Editor) isOpenSceneTabPath(absPath string) bool {
	if e == nil || absPath == "" {
		return false
	}
	check := func(p string) bool {
		if p == "" {
			return false
		}
		if !filepath.IsAbs(p) && e.ProjectPath != "" {
			p = filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(p))
		}
		return sameHostFilePath(absPath, p)
	}
	if check(e.FilePath) || check(e.ScenePath) {
		return true
	}
	for _, tab := range e.tabs {
		if check(tab.filePath) || check(tab.scenePath) {
			return true
		}
	}
	return false
}

// validateProposedDeletionTarget resolves relPath inside the open project root, refuses You-only files,
// refuses .git/**, game.yaml, and open scene tabs, rejects symlinks and non-regular files via os.Lstat,
// and enforces the 1 MiB checkpoint cap.
func (e *Editor) validateProposedDeletionTarget(relPath string) (cleanRel, absPath string, st fs.FileInfo, err error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return "", "", nil, ErrNoProject
	}
	if dom, isYouOnly := e.CheckYouOnlyTarget(relPath); isYouOnly {
		return "", "", nil, fmt.Errorf("editor: %s is You only (%s) and cannot be deleted by the agent", relPath, dom.DisplayName())
	}
	root := filepath.Dir(e.ProjectPath)
	cleanRel, absPath, err = resolveProjectFileWithinRoot(root, relPath, false)
	if err != nil {
		return "", "", nil, err
	}
	if dom, isYouOnly := e.CheckYouOnlyTarget(cleanRel); isYouOnly {
		return "", "", nil, fmt.Errorf("editor: %s is You only (%s) and cannot be deleted by the agent", cleanRel, dom.DisplayName())
	}
	lowerClean := strings.ToLower(cleanRel)
	if lowerClean == ".git" || strings.HasPrefix(lowerClean, ".git/") || strings.Contains(lowerClean, "/.git/") {
		return "", "", nil, fmt.Errorf("editor: %s is inside .git and cannot be deleted", cleanRel)
	}
	if lowerClean == strings.ToLower(project.FileName) {
		return "", "", nil, fmt.Errorf("editor: %s is the project manifest and cannot be deleted", cleanRel)
	}
	if e.isOpenSceneTabPath(absPath) {
		return "", "", nil, fmt.Errorf("editor: %s is currently open in a scene tab and cannot be deleted while open", cleanRel)
	}
	// Use os.Lstat (never os.Stat) so symlinks are explicitly rejected and cannot be followed on delete.
	st, err = os.Lstat(absPath)
	if err != nil {
		return "", "", nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, fmt.Errorf("editor: %s is a symbolic link and cannot be deleted via proposed deletion", cleanRel)
	}
	if !st.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("editor: %s is not a regular file", cleanRel)
	}
	if st.Size() > maxProposedDeletionBytes {
		return "", "", nil, fmt.Errorf("editor: %s (%d bytes) exceeds the %d-byte reversible deletion checkpoint limit", cleanRel, st.Size(), maxProposedDeletionBytes)
	}
	return cleanRel, absPath, st, nil
}

func readBoundedDeletionFile(absPath string) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxProposedDeletionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxProposedDeletionBytes {
		return nil, fmt.Errorf("editor: file exceeds the %d-byte reversible deletion checkpoint limit", maxProposedDeletionBytes)
	}
	return raw, nil
}

// ProposeFileDeletion adds a proposed file deletion to the Review changes panel WITHOUT deleting the file on disk.
// It inspects the file's line count, preview lines, and scene usages via FindUsages, and refuses You-only files,
// symlinks, .git/**, game.yaml, open scene tabs, and files larger than 1 MiB.
func (e *Editor) ProposeFileDeletion(relPath, reason string) (*ProposedDeletion, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	if e.playing {
		return nil, ErrPlaying
	}
	if e.StartScreenOpen() {
		return nil, errors.New("editor: cannot propose file deletion while Start Screen is open")
	}
	if e.MergeConflictOpen() {
		return nil, errors.New("editor: cannot propose file deletion while Merge Conflicts screen is open")
	}
	cleanRel, absPath, _, err := e.validateProposedDeletionTarget(relPath)
	if err != nil {
		return nil, err
	}
	raw, err := readBoundedDeletionFile(absPath)
	if err != nil {
		return nil, err
	}

	lineCount, previewLines, moreLines, isBinary := summarizeFileLines(raw, 3)

	var sceneUsages []string
	if rep, uerr := e.FindUsages(cleanRel); uerr == nil && rep != nil {
		for _, sg := range rep.Scenes {
			sceneUsages = append(sceneUsages, sg.File)
		}
		for _, vg := range rep.Variants {
			if strings.HasSuffix(strings.ToLower(vg.File), ".scene.yaml") || strings.HasSuffix(strings.ToLower(vg.File), ".scene.yml") {
				if !slices.Contains(sceneUsages, vg.File) {
					sceneUsages = append(sceneUsages, vg.File)
				}
			}
		}
		slices.Sort(sceneUsages)
	}

	usedByScene := len(sceneUsages) > 0
	usageSummary := "no scene uses it"
	if len(sceneUsages) == 1 {
		usageSummary = fmt.Sprintf("used by 1 scene (%s)", path.Base(sceneUsages[0]))
	} else if len(sceneUsages) > 1 {
		usageSummary = fmt.Sprintf("used by %d scenes", len(sceneUsages))
	}

	var sizeOrLines string
	if isBinary {
		sizeOrLines = fmt.Sprintf("binary · %d bytes", len(raw))
	} else if lineCount == 1 {
		sizeOrLines = "1 line"
	} else {
		sizeOrLines = fmt.Sprintf("%d lines", lineCount)
	}
	detail := fmt.Sprintf("Delete file · %s · %s", sizeOrLines, usageSummary)

	// Update existing entry for the same path if already present, else append.
	for i := range e.agentReview.deletions {
		if e.agentReview.deletions[i].Path == cleanRel {
			e.agentReview.deletions[i].AbsPath = absPath
			e.agentReview.deletions[i].LineCount = lineCount
			e.agentReview.deletions[i].SceneUsages = sceneUsages
			e.agentReview.deletions[i].UsedByAnyScene = usedByScene
			e.agentReview.deletions[i].UsageSummary = usageSummary
			e.agentReview.deletions[i].Detail = detail
			e.agentReview.deletions[i].PreviewLines = previewLines
			e.agentReview.deletions[i].MoreLines = moreLines
			if strings.TrimSpace(reason) != "" {
				e.agentReview.deletions[i].Reason = strings.TrimSpace(reason)
			}
			e.agentReview.deletions[i].Status = DeletionPending
			e.agentReview.open = true
			if e.agentReview.prompt == "" && strings.TrimSpace(reason) != "" {
				e.agentReview.prompt = strings.TrimSpace(reason)
			}
			e.agentReview.rev++
			e.notify()
			cp := e.agentReview.deletions[i]
			cp.SceneUsages = slices.Clone(cp.SceneUsages)
			cp.PreviewLines = slices.Clone(cp.PreviewLines)
			return &cp, nil
		}
	}

	e.agentReview.seq++
	pd := ProposedDeletion{
		ID:             fmt.Sprintf("del-%d", e.agentReview.seq),
		Path:           cleanRel,
		AbsPath:        absPath,
		LineCount:      lineCount,
		SceneUsages:    sceneUsages,
		UsedByAnyScene: usedByScene,
		UsageSummary:   usageSummary,
		Detail:         detail,
		PreviewLines:   previewLines,
		MoreLines:      moreLines,
		Reason:         strings.TrimSpace(reason),
		Status:         DeletionPending,
	}
	e.agentReview.deletions = append(e.agentReview.deletions, pd)
	e.agentReview.open = true
	if e.agentReview.prompt == "" && pd.Reason != "" {
		e.agentReview.prompt = pd.Reason
	}
	e.agentReview.rev++
	e.notify()

	cp := pd
	cp.SceneUsages = slices.Clone(cp.SceneUsages)
	cp.PreviewLines = slices.Clone(cp.PreviewLines)
	return &cp, nil
}

func isBinaryContent(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	sample := raw
	if len(sample) > binarySniffLimit {
		sample = sample[:binarySniffLimit]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	// Walk UTF-8 runes; ignore a trailing partial rune if we truncated at binarySniffLimit.
	for i := 0; i < len(sample); {
		r, sz := utf8.DecodeRune(sample[i:])
		if r == utf8.RuneError && sz == 1 {
			if len(raw) > binarySniffLimit && len(sample)-i < utf8.UTFMax {
				break
			}
			return true
		}
		i += sz
	}
	return false
}

func summarizeFileLines(raw []byte, maxPreview int) (lineCount int, preview []string, more int, binary bool) {
	if len(raw) == 0 {
		return 0, nil, 0, false
	}
	if isBinaryContent(raw) {
		return 0, []string{fmt.Sprintf("[binary file · %d bytes]", len(raw))}, 0, true
	}
	trimmed := strings.TrimSuffix(string(raw), "\n")
	lines := strings.Split(trimmed, "\n")
	lineCount = len(lines)
	n := min(maxPreview, lineCount)
	preview = make([]string, n)
	for i := range n {
		preview[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if lineCount > n {
		more = lineCount - n
	}
	return lineCount, preview, more, false
}

func (e *Editor) unkeptRegularChangeCount() int {
	n := 0
	for _, c := range e.agentReview.changes {
		if !c.Reverted && !c.Kept {
			n++
		}
	}
	return n
}

func (e *Editor) confirmedDeletionCount() int {
	n := 0
	for _, d := range e.agentReview.deletions {
		if d.Status == DeletionConfirmed {
			n++
		}
	}
	return n
}

func (e *Editor) pendingDeletionCount() int {
	n := 0
	for _, d := range e.agentReview.deletions {
		if d.Status == DeletionPending {
			n++
		}
	}
	return n
}

// ConfirmProposedDeletion handles the person clicking "Delete this file" on a proposed deletion.
// If unkept regular changes remain in the review, it marks the deletion as DeletionConfirmed
// ("Deletion confirmed · happens with Keep") so pressing Keep applies both together.
// If no unkept regular changes remain (for example after Keep all already kept the other changes),
// it deletes the file immediately through a reversible checkpoint command so ⌘Z restores it.
func (e *Editor) ConfirmProposedDeletion(relPath string) error {
	if e == nil || !e.agentReview.open {
		return errors.New("editor: no review is open")
	}
	if e.playing {
		return ErrPlaying
	}
	idx := e.findProposedDeletionIndex(relPath)
	if idx < 0 {
		return fmt.Errorf("editor: proposed deletion %q not found", relPath)
	}
	d := &e.agentReview.deletions[idx]
	if d.Status == DeletionApplied {
		return nil
	}
	if e.unkeptRegularChangeCount() > 0 {
		d.Status = DeletionConfirmed
		e.agentReview.rev++
		e.notify()
		return nil
	}
	// No unkept regular changes left: execute the confirmed deletion right now via a reversible checkpoint command!
	if err := e.executeProposedDeletion(idx); err != nil {
		return err
	}
	if e.pendingDeletionCount() == 0 && e.confirmedDeletionCount() == 0 {
		e.agentReview.open = false
		e.scheduleAutoSaveIfNeeded()
	}
	e.agentReview.rev++
	e.notify()
	return nil
}

// KeepProposedFile marks a proposed deletion as DeletionKept ("The file stays · the rest of the review is unchanged").
func (e *Editor) KeepProposedFile(relPath string) error {
	if e == nil || !e.agentReview.open {
		return errors.New("editor: no review is open")
	}
	idx := e.findProposedDeletionIndex(relPath)
	if idx < 0 {
		return fmt.Errorf("editor: proposed deletion %q not found", relPath)
	}
	if e.agentReview.deletions[idx].Status == DeletionApplied {
		return errors.New("editor: file was already deleted; use Undo (⌘Z) to restore it")
	}
	e.agentReview.deletions[idx].Status = DeletionKept
	e.agentReview.rev++
	e.notify()
	return nil
}

// ResetProposedDeletion returns a confirmed or kept proposed deletion back to DeletionPending.
func (e *Editor) ResetProposedDeletion(relPath string) {
	if e == nil || !e.agentReview.open {
		return
	}
	idx := e.findProposedDeletionIndex(relPath)
	if idx < 0 {
		return
	}
	if e.agentReview.deletions[idx].Status == DeletionConfirmed || e.agentReview.deletions[idx].Status == DeletionKept {
		e.agentReview.deletions[idx].Status = DeletionPending
		e.agentReview.rev++
		e.notify()
	}
}

func (e *Editor) findProposedDeletionIndex(relPath string) int {
	norm := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(relPath), `\`, "/"), "./")
	for i, d := range e.agentReview.deletions {
		if d.Path == norm || d.ID == relPath {
			return i
		}
	}
	return -1
}

// KeepAllReviewChanges applies Keep all (and ⌘↵) to the open review.
// Invariants enforced by the trust boundary (#1015):
//   - NEVER applies an open You-only PermissionsDraft (regardless of profile, Autonomous included).
//   - NEVER deletes any DeletionPending file; unconfirmed deletions remain on disk and stay pending for explicit confirmation.
//   - Keeps all non-reverted regular changes (and any open tile review) and applies ONLY deletions the person explicitly confirmed.
func (e *Editor) KeepAllReviewChanges() (KeepAllResult, error) {
	var res KeepAllResult
	if e == nil {
		return res, nil
	}
	if e.playing {
		return res, ErrPlaying
	}
	// Hard invariant: Keep all / ⌘↵ NEVER applies a You-only permissions draft.
	res.LeftPermissionsDraft = e.PermissionsDraftOpen()

	if e.TileReviewOpen() {
		if _, err := e.KeepTileReview(); err != nil {
			return res, err
		}
	}
	if !e.agentReview.open {
		return res, nil
	}

	// 1. Keep all non-reverted regular changes.
	for i := range e.agentReview.changes {
		c := &e.agentReview.changes[i]
		if c.Reverted || c.Kept {
			continue
		}
		if c.OnKeep != nil {
			if err := c.OnKeep(); err != nil {
				return res, err
			}
		}
		c.Kept = true
		res.KeptChanges++
	}

	// 2. Apply ONLY deletions explicitly confirmed by the person; leave DeletionPending untouched!
	var confirmedIdxs []int
	for i, d := range e.agentReview.deletions {
		switch d.Status {
		case DeletionConfirmed:
			confirmedIdxs = append(confirmedIdxs, i)
		case DeletionPending:
			res.LeftPendingDeletions = append(res.LeftPendingDeletions, d.Path)
		}
	}
	if len(confirmedIdxs) == 1 {
		idx := confirmedIdxs[0]
		if err := e.executeProposedDeletion(idx); err != nil {
			return res, err
		}
		res.DeletedFiles = append(res.DeletedFiles, e.agentReview.deletions[idx].Path)
	} else if len(confirmedIdxs) > 1 {
		err := e.History.Group(fmt.Sprintf("Delete %d confirmed files", len(confirmedIdxs)), func() error {
			for _, idx := range confirmedIdxs {
				if err := e.executeProposedDeletion(idx); err != nil {
					return err
				}
				res.DeletedFiles = append(res.DeletedFiles, e.agentReview.deletions[idx].Path)
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}

	if res.KeptChanges > 0 {
		e.RecordAgentReviewOutcome(true, res.KeptChanges)
	}

	// Keep the review open if any proposed deletions still await confirmation; close otherwise.
	if len(res.LeftPendingDeletions) > 0 {
		res.ReviewStillOpen = true
	} else {
		e.agentReview.open = false
		res.ReviewStillOpen = false
		e.scheduleAutoSaveIfNeeded()
	}
	e.agentReview.rev++
	e.notify()
	return res, nil
}

// RevertAllReviewChanges reverts all regular changes in the open review and dismisses all unapplied proposed deletions without touching disk.
func (e *Editor) RevertAllReviewChanges() error {
	if e == nil || !e.agentReview.open {
		return nil
	}
	revertedCount := 0
	for i := range e.agentReview.changes {
		c := &e.agentReview.changes[i]
		if c.Reverted || c.Kept {
			continue
		}
		if c.OnRevert != nil {
			if err := c.OnRevert(); err != nil {
				return err
			}
		}
		c.Reverted = true
		revertedCount++
	}
	if revertedCount > 0 {
		e.RecordAgentReviewOutcome(false, revertedCount)
	}
	e.agentReview.open = false
	e.agentReview.deletions = nil
	e.agentReview.changes = nil
	e.agentReview.rev++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

func (e *Editor) executeProposedDeletion(idx int) error {
	if idx < 0 || idx >= len(e.agentReview.deletions) {
		return errors.New("editor: proposed deletion index out of range")
	}
	d := e.agentReview.deletions[idx]
	// Re-resolve and re-validate fresh on execution (closing TOCTOU symlink/You-only/size/open-tab races).
	cleanRel, abs, st, err := e.validateProposedDeletionTarget(d.Path)
	if err != nil {
		return err
	}
	raw, err := readBoundedDeletionFile(abs)
	if err != nil {
		return err
	}
	cmd := &deleteFileCmd{
		ed:         e,
		relPath:    cleanRel,
		absPath:    abs,
		snapshot:   raw,
		mode:       st.Mode().Perm(),
		deletionID: d.ID,
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}
	e.ShowToast(Toast{
		ID:             "deleted-file-" + cleanRel,
		Kind:           ToastInfo,
		Message:        fmt.Sprintf("Deleted %s · ⌘Z restores it", path.Base(cleanRel)),
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction: func() {
			e.Undo()
		},
	})
	return nil
}

// deleteFileCmd deletes a project file on disk after snapshotting its exact bytes and mode,
// so Undo (⌘Z) restores the file from the checkpoint byte-for-byte.
type deleteFileCmd struct {
	ed         *Editor
	relPath    string
	absPath    string
	snapshot   []byte
	mode       fs.FileMode
	deletionID string
}

func (c *deleteFileCmd) Label() string       { return "Delete " + c.relPath }
func (c *deleteFileCmd) Actor() HistoryActor { return HistoryActorPerson }
func (c *deleteFileCmd) Do()                 { c.ed.report(c.do()) }
func (c *deleteFileCmd) Undo()               { c.ed.report(c.undo()) }

func (c *deleteFileCmd) do() error {
	// Re-validate fresh before os.Remove (protects Redo and concurrent TOCTOU symlink swaps).
	_, abs, _, err := c.ed.validateProposedDeletionTarget(c.relPath)
	if err != nil {
		return err
	}
	c.absPath = abs
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("editor: delete %s: %w", c.relPath, err)
	}
	for i := range c.ed.agentReview.deletions {
		if c.ed.agentReview.deletions[i].ID == c.deletionID || c.ed.agentReview.deletions[i].Path == c.relPath {
			c.ed.agentReview.deletions[i].Status = DeletionApplied
		}
	}
	if c.ed.SelectedFile == c.relPath {
		c.ed.SelectedFile = ""
		c.ed.selectedFileRev++
	}
	c.ed.filesRev++
	c.ed.assetsRev++
	c.ed.invalidateProjectIndex()
	c.ed.snapshotProjectTree()
	c.ed.syncDiskWatchTargets()
	c.ed.agentReview.rev++
	c.ed.Status = "Deleted " + c.relPath
	c.ed.notify()
	return nil
}

func (c *deleteFileCmd) undo() error {
	if c.ed == nil || c.ed.Project == nil || c.ed.ProjectPath == "" {
		return ErrNoProject
	}
	root := filepath.Dir(c.ed.ProjectPath)
	_, abs, err := resolveProjectFileWithinRoot(root, c.relPath, true)
	if err != nil {
		return err
	}
	// Refuse overwriting if the path was recreated externally or replaced by a symlink.
	if st, lerr := os.Lstat(abs); lerr == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("editor: cannot restore %s: path is now a symbolic link", c.relPath)
		}
		return fmt.Errorf("editor: cannot restore %s: file already exists on disk", c.relPath)
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return fmt.Errorf("editor: stat %s before restore: %w", c.relPath, lerr)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("editor: restore directory for %s: %w", c.relPath, err)
	}
	if err := writeFileAtomic(abs, c.snapshot); err != nil {
		return fmt.Errorf("editor: restore %s: %w", c.relPath, err)
	}
	if c.mode != 0 {
		_ = os.Chmod(abs, c.mode)
	}
	c.absPath = abs
	for i := range c.ed.agentReview.deletions {
		if c.ed.agentReview.deletions[i].ID == c.deletionID || c.ed.agentReview.deletions[i].Path == c.relPath {
			c.ed.agentReview.deletions[i].Status = DeletionPending
		}
	}
	c.ed.filesRev++
	c.ed.assetsRev++
	c.ed.invalidateProjectIndex()
	c.ed.snapshotProjectTree()
	c.ed.syncDiskWatchTargets()
	c.ed.agentReview.rev++
	c.ed.Status = "Restored " + c.relPath
	c.ed.notify()
	return nil
}
