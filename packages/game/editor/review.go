package editor

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const (
	// maxReviewFileBytes bounds individual before/after snapshots accepted by StartTurnReview (8 MiB).
	maxReviewFileBytes = 8 << 20
	// maxReviewNoteLen bounds the length (in runes) of one change-scoped review note.
	maxReviewNoteLen = 4096
	// maxReviewNotesCount bounds how many unsent or pending notes can exist simultaneously in one review.
	maxReviewNotesCount = 256
	// maxReviewRounds bounds how many redo rounds a single review session can advance through.
	maxReviewRounds = 20
	// maxReviewRedoYAMLLines bounds custom YAML diff lines provided on one review_redo item.
	maxReviewRedoYAMLLines = 2000
	// maxReviewYAMLEditBytes bounds an in-place YAML edit draft in the Review right pane (256 KiB).
	maxReviewYAMLEditBytes = 256 << 10
)

var (
	// ErrNoReview is returned by review actions when no agent turn review is pending.
	ErrNoReview = errors.New("editor: no agent turn review is open")
	// ErrReviewBlocked is returned by KeepTurnReview when an unresolved blocker prevents keeping.
	ErrReviewBlocked = errors.New("editor: review has an unresolved blocker")
	// ErrReviewReadOnlyRound is returned when trying to edit, revert, or add notes while viewing an earlier round.
	ErrReviewReadOnlyRound = errors.New("editor: earlier review rounds are read-only")
	// ErrReviewNoteEmpty is returned when saving or sending an empty note.
	ErrReviewNoteEmpty = errors.New("editor: review note cannot be empty")
	// ErrReviewNoteTooLong is returned when a review note exceeds maxReviewNoteLen runes.
	ErrReviewNoteTooLong = errors.New("editor: review note exceeds maximum length")
	// ErrReviewNotesLimit is returned when a review exceeds maxReviewNotesCount simultaneous notes.
	ErrReviewNotesLimit = errors.New("editor: review has too many active notes")
	// ErrReviewNoNotesToSend is returned by SendReviewNotes when no unsent notes exist.
	ErrReviewNoNotesToSend = errors.New("editor: no unsent review notes to send")
	// ErrReviewChangeNotFound is returned when a change ID does not exist in the active review.
	ErrReviewChangeNotFound = errors.New("editor: review change not found")
	// ErrReviewChangeNotNoted is returned by ApplyReviewRedo when targeting a change that was not noted and sent.
	ErrReviewChangeNotNoted = errors.New("editor: review_redo can modify only changes you left notes on")
	// ErrReviewMaxRounds is returned when a review reaches maxReviewRounds.
	ErrReviewMaxRounds = errors.New("editor: review reached maximum number of rounds")
	// ErrReviewInvalidRound is returned when selecting an out-of-range review round.
	ErrReviewInvalidRound = errors.New("editor: invalid review round")
	// ErrReviewYAMLInvalid is returned by FinishReviewYAMLEdit when the hand-edited YAML does not parse or validate.
	ErrReviewYAMLInvalid = errors.New("editor: review YAML is invalid")
)

// ReviewBlockedError describes why KeepTurnReview refused to land the turn.
type ReviewBlockedError struct {
	Blocker ReviewBlocker
}

func (e *ReviewBlockedError) Error() string {
	return "editor: review has an unresolved blocker: " + e.Blocker.Reason
}

func (e *ReviewBlockedError) Unwrap() error { return ErrReviewBlocked }

// ReviewFileKind classifies the kind of file touched by an agent turn.
type ReviewFileKind string

const (
	ReviewFileScene  ReviewFileKind = "scene"
	ReviewFilePrefab ReviewFileKind = "prefab"
	ReviewFileData   ReviewFileKind = "data"
	ReviewFileGo     ReviewFileKind = "go"
	ReviewFileOther  ReviewFileKind = "other"
)

// ReviewChangeKind classifies an individual change in an agent turn review.
type ReviewChangeKind string

const (
	ReviewChangeAdded   ReviewChangeKind = "added"
	ReviewChangeChanged ReviewChangeKind = "changed"
	ReviewChangeRemoved ReviewChangeKind = "removed"
)

// ReviewFilter selects which changes appear in the left review list.
type ReviewFilter string

const (
	ReviewFilterAll     ReviewFilter = "all"
	ReviewFilterAdded   ReviewFilter = "added"
	ReviewFilterChanged ReviewFilter = "changed"
	ReviewFilterRemoved ReviewFilter = "removed"
	ReviewFilterUnseen  ReviewFilter = "unseen"
	ReviewFilterNotes   ReviewFilter = "notes"
)

// ReviewOutcome reports the lifecycle status of the most recent agent turn review.
type ReviewOutcome string

const (
	ReviewOutcomeNone     ReviewOutcome = ""
	ReviewOutcomePending  ReviewOutcome = "pending"
	ReviewOutcomeKept     ReviewOutcome = "kept"
	ReviewOutcomeReverted ReviewOutcome = "reverted"
)

// ReviewRowKind identifies a visible row in the grouped left change list.
type ReviewRowKind string

const (
	ReviewRowFile       ReviewRowKind = "file"
	ReviewRowKindHeader ReviewRowKind = "kind"
	ReviewRowCard       ReviewRowKind = "card"
)

// ReviewYAMLLine is one line in the right-hand YAML/code diff view.
type ReviewYAMLLine struct {
	Line   int              `json:"line"`
	Sign   string           `json:"sign,omitempty"` // "", "+", "-"
	Text   string           `json:"text"`
	Tag    string           `json:"tag,omitempty"`
	Author ReviewLineAuthor `json:"author,omitempty"`
	Error  bool             `json:"error,omitempty"`
}

// ReviewDataCellDiff describes a changed cell in a data table row.
type ReviewDataCellDiff struct {
	Column   string `json:"column"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	DeltaPct string `json:"deltaPct,omitempty"`
}

// ReviewDataRow is one row in the center small-data-table preview.
type ReviewDataRow struct {
	ID        string                        `json:"id"`
	ChangeID  string                        `json:"changeId,omitempty"`
	Values    map[string]string             `json:"values"`
	Changed   bool                          `json:"changed,omitempty"`
	Added     bool                          `json:"added,omitempty"`
	Removed   bool                          `json:"removed,omitempty"`
	Focused   bool                          `json:"focused,omitempty"`
	CellDiffs map[string]ReviewDataCellDiff `json:"cellDiffs,omitempty"`
}

// ReviewDataTablePreview holds the center-pane table view model for a small data table file.
type ReviewDataTablePreview struct {
	File         string          `json:"file"`
	Columns      []string        `json:"columns"`
	TotalRows    int             `json:"totalRows"`
	ChangedCount int             `json:"changedCount"`
	Rows         []ReviewDataRow `json:"rows"`
}

// ReviewBlocker describes an active blocker preventing Keep until resolved.
type ReviewBlocker struct {
	Kind     string `json:"kind,omitempty"` // "yaml_error", "conflict", or "custom"
	ChangeID string `json:"changeId"`
	ObjectID string `json:"objectId,omitempty"`
	Line     int    `json:"line,omitempty"`
	Reason   string `json:"reason"`
}

// ReviewChangeVersion holds one round's immutable snapshot of a ReviewChange (1-based).
type ReviewChangeVersion struct {
	Round         int                    `json:"round"`
	Detail        string                 `json:"detail"`
	Why           string                 `json:"why"`
	OldValue      string                 `json:"oldValue,omitempty"`
	NewValue      string                 `json:"newValue,omitempty"`
	DeltaPct      string                 `json:"deltaPct,omitempty"`
	Line          int                    `json:"line,omitempty"`
	X             float32                `json:"x,omitempty"`
	Y             float32                `json:"y,omitempty"`
	YAMLLines     []ReviewYAMLLine       `json:"yamlLines,omitempty"`
	afterObj      *sceneformat.ObjectDoc `json:"-"`
	afterLayer    string                 `json:"-"`
	parentID      string                 `json:"-"`
	afterYAMLNode *yaml.Node             `json:"-"`
	cellDiffs     []ReviewDataCellDiff   `json:"-"`
}

// ReviewChange is one reviewable change inside an agent turn.
type ReviewChange struct {
	ID                       string                 `json:"id"`
	File                     string                 `json:"file"`
	FileKind                 ReviewFileKind         `json:"fileKind"`
	Kind                     ReviewChangeKind       `json:"kind"`
	Title                    string                 `json:"title"`
	Detail                   string                 `json:"detail"`
	Why                      string                 `json:"why"`
	ObjectID                 string                 `json:"objectId,omitempty"`
	Layer                    string                 `json:"layer,omitempty"`
	ParentID                 string                 `json:"parentId,omitempty"`
	Field                    string                 `json:"field,omitempty"`
	OldValue                 string                 `json:"oldValue,omitempty"`
	NewValue                 string                 `json:"newValue,omitempty"`
	DeltaPct                 string                 `json:"deltaPct,omitempty"`
	PrefabImpact             string                 `json:"prefabImpact,omitempty"`
	RowID                    string                 `json:"rowId,omitempty"`
	Line                     int                    `json:"line,omitempty"`
	YAMLLines                []ReviewYAMLLine       `json:"yamlLines,omitempty"`
	Seen                     bool                   `json:"seen"`
	Reverted                 bool                   `json:"reverted"`
	Note                     string                 `json:"note,omitempty"`
	SentNote                 string                 `json:"sentNote,omitempty"`
	RedoneInRound            int                    `json:"redoneInRound,omitempty"`
	RedoneNote               string                 `json:"redoneNote,omitempty"`
	Versions                 []ReviewChangeVersion  `json:"versions,omitempty"`
	Blocker                  string                 `json:"blocker,omitempty"`
	GoRequiresConfirm        bool                   `json:"goRequiresConfirm,omitempty"`
	GoConfirmed              bool                   `json:"goConfirmed,omitempty"`
	Author                   ReviewLineAuthor       `json:"author,omitempty"`
	Conflicts                []ReviewConflict       `json:"conflicts,omitempty"`
	ConflictChoice           ReviewConflictChoice   `json:"conflictChoice,omitempty"`
	AutoMergedFields         []string               `json:"autoMergedFields,omitempty"`
	EditingYAML              bool                   `json:"editingYaml,omitempty"`
	HandEdited               bool                   `json:"handEdited,omitempty"`
	HandEditBy               ReviewLineAuthor       `json:"handEditBy,omitempty"`
	HandEditFields           []string               `json:"handEditFields,omitempty"`
	HandEditLines            []ReviewYAMLLine       `json:"handEditLines,omitempty"`
	HandEditYAML             string                 `json:"handEditYaml,omitempty"`
	YAMLError                *ReviewYAMLError       `json:"yamlError,omitempty"`
	beforeObj                *sceneformat.ObjectDoc `json:"-"`
	afterObj                 *sceneformat.ObjectDoc `json:"-"`
	personObj                *sceneformat.ObjectDoc `json:"-"`
	beforeLayer              string                 `json:"-"`
	afterLayer               string                 `json:"-"`
	beforeParentID           string                 `json:"-"`
	beforeYAMLNode           *yaml.Node             `json:"-"`
	afterYAMLNode            *yaml.Node             `json:"-"`
	cellDiffs                []ReviewDataCellDiff   `json:"-"`
	handEditPreYAML          string                 `json:"-"`
	handEditObj              *sceneformat.ObjectDoc `json:"-"`
	handEditBaseObj          *sceneformat.ObjectDoc `json:"-"`
	handEditLineNums         []int                  `json:"-"`
	hadHandEditBeforeSession bool                   `json:"-"`
	preSessionHandObj        *sceneformat.ObjectDoc `json:"-"`
	preSessionHandFields     []string               `json:"-"`
	preSessionHandLineNums   []int                  `json:"-"`
}

// ReviewFileSummary summarizes one file group in the review.
type ReviewFileSummary struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	FileKind    ReviewFileKind `json:"fileKind"`
	Added       int            `json:"added"`
	Changed     int            `json:"changed"`
	Removed     int            `json:"removed"`
	AllReverted bool           `json:"allReverted"`
	Collapsed   bool           `json:"collapsed"`
}

// ReviewVisibleRow represents one rendered row in the left grouped change list.
type ReviewVisibleRow struct {
	Key            string           `json:"key"`
	RowKind        ReviewRowKind    `json:"rowKind"`
	File           string           `json:"file"`
	FileName       string           `json:"fileName,omitempty"`
	FileKind       ReviewFileKind   `json:"fileKind,omitempty"`
	Kind           ReviewChangeKind `json:"kind,omitempty"`
	ChangeID       string           `json:"changeId,omitempty"`
	Count          int              `json:"count,omitempty"`
	Added          int              `json:"added,omitempty"`
	Changed        int              `json:"changed,omitempty"`
	Removed        int              `json:"removed,omitempty"`
	Collapsed      bool             `json:"collapsed,omitempty"`
	AllReverted    bool             `json:"allReverted,omitempty"`
	CanRevertGroup bool             `json:"canRevertGroup,omitempty"`
}

// ReviewSceneOverlay is an inspectable overlay entry drawn in the Review viewport or main editor viewport.
type ReviewSceneOverlay struct {
	ChangeID    string           `json:"changeId"`
	File        string           `json:"file"`
	ObjectID    string           `json:"objectId"`
	Kind        ReviewChangeKind `json:"kind"`
	ColorRole   string           `json:"colorRole"` // "added", "changed", "removed" (never "accent")
	Focused     bool             `json:"focused"`
	StrokeWidth float32          `json:"strokeWidth"` // 2.5 when focused, 1.5 otherwise
	Dashed      bool             `json:"dashed"`
	PillText    string           `json:"pillText"`
	X           float32          `json:"x"`
	Y           float32          `json:"y"`
	W           float32          `json:"w"`
	H           float32          `json:"h"`
}

// ReviewOverlayVisibility controls which overlay kinds are visible in the Review viewport legend.
type ReviewOverlayVisibility struct {
	Added    bool `json:"added"`
	Changed  bool `json:"changed"`
	Removed  bool `json:"removed"`
	Selected bool `json:"selected"`
}

// ReviewFileInput specifies one touched file when opening a turn review.
type ReviewFileInput struct {
	// Path is the slash-separated path relative to the project root (for example "scenes/level_01.scene.yaml").
	Path string
	// FileKind optionally overrides automatic detection from Path.
	FileKind ReviewFileKind
	// BeforeYAML is the exact file content before the agent turn (empty/nil when newly created).
	BeforeYAML []byte
	// AfterYAML is the exact file content after the agent turn (empty/nil when deleted).
	AfterYAML []byte
	// PersonYAML optionally supplies concurrent manual edits made by the person to this file.
	PersonYAML []byte
	// ExistedBefore explicitly records whether the file existed on disk before the turn.
	// When nil, it defaults to len(BeforeYAML) > 0.
	ExistedBefore *bool
	// Reasons maps a change target key (object ID, prefab field path, data row ID, or "*" default) to the agent's Why explanation.
	Reasons map[string]string
	// Details maps a change target key to an optional custom detail line shown on its card.
	Details map[string]string
	// Notes maps a change target key to an optional review note.
	Notes map[string]string
	// Blockers maps a change target key to an optional blocker reason that disables Keep until cleared.
	Blockers map[string]string
}

// ReviewTurnInput configures Editor.StartTurnReview.
type ReviewTurnInput struct {
	// Request is the quoted user request displayed in the Review header and Agent panel.
	Request string
	// Files are the project files touched by this agent turn, in presentation order.
	Files []ReviewFileInput
	// DefaultWhy is used when a change has no per-target or per-file reason.
	DefaultWhy string
	// StartPending leaves the review pending in the Agent panel instead of opening full-window immediately.
	StartPending bool
}

// ReviewSnapshot is the read-only state returned by Editor.ReviewStatus and editormcp's review_status tool.
type ReviewSnapshot struct {
	Active               bool                    `json:"active"`
	Open                 bool                    `json:"open"`
	Outcome              ReviewOutcome           `json:"outcome"`
	Request              string                  `json:"request,omitempty"`
	Round                int                     `json:"round,omitempty"`
	ViewRound            int                     `json:"viewRound,omitempty"`
	ViewingReadOnlyRound bool                    `json:"viewingReadOnlyRound,omitempty"`
	DraftNoteCount       int                     `json:"draftNoteCount,omitempty"`
	ComposingChangeID    string                  `json:"composingChangeId,omitempty"`
	PendingBatch         *ReviewNoteBatch        `json:"pendingBatch,omitempty"`
	RoundBanner          string                  `json:"roundBanner,omitempty"`
	TotalChanges         int                     `json:"totalChanges"`
	SeenCount            int                     `json:"seenCount"`
	KeptCount            int                     `json:"keptCount"`
	RevertedCount        int                     `json:"revertedCount"`
	FileCount            int                     `json:"fileCount"`
	FocusedID            string                  `json:"focusedId,omitempty"`
	FocusedRowKey        string                  `json:"focusedRowKey,omitempty"`
	Filter               ReviewFilter            `json:"filter,omitempty"`
	Search               string                  `json:"search,omitempty"`
	PeekBefore           bool                    `json:"peekBefore"`
	RevertConfirm        bool                    `json:"revertConfirm"`
	Blockers             []ReviewBlocker         `json:"blockers,omitempty"`
	OverlayLegend        ReviewOverlayVisibility `json:"overlayLegend"`
	Files                []ReviewFileSummary     `json:"files,omitempty"`
	Changes              []ReviewChange          `json:"changes,omitempty"`
	LastRounds           int                     `json:"lastRounds,omitempty"`
	LastOutcomeMsg       string                  `json:"lastOutcomeMsg,omitempty"`
}

type reviewFileRound struct {
	afterYAML  []byte
	afterDoc   *sceneformat.SceneDoc
	afterTable *datatable.Document
	built      *sceneformat.BuiltScene
	builtDirty bool
}

type reviewFileState struct {
	path          string
	kind          ReviewFileKind
	existedBefore bool
	beforeYAML    []byte
	afterYAML     []byte
	beforeDoc     *sceneformat.SceneDoc
	afterDoc      *sceneformat.SceneDoc
	personDoc     *sceneformat.SceneDoc
	personEdited  bool
	beforeBuilt   *sceneformat.BuiltScene
	currentBuilt  *sceneformat.BuiltScene
	builtDirty    bool
	beforeTable   *datatable.Document
	afterTable    *datatable.Document
	rounds        []reviewFileRound
	sceneView     View2D
	sceneFramed   bool
	canvasW       float32
	canvasH       float32
}

type turnReviewState struct {
	open               bool
	outcome            ReviewOutcome
	request            string
	defaultWhy         string
	round              int
	viewRound          int
	composingID        string
	noteDraft          string
	pendingBatch       *ReviewNoteBatch
	roundRedone        map[int]int
	files              []*reviewFileState
	filesByPath        map[string]*reviewFileState
	changes            []*ReviewChange
	changesByID        map[string]*ReviewChange
	focusedID          string
	focusedRowKey      string
	filter             ReviewFilter
	search             string
	collapsedFiles     map[string]bool
	collapsedKinds     map[string]bool
	peekBefore         bool
	revertConfirm      bool
	legend             ReviewOverlayVisibility
	activeRegion       string
	lastKept           int
	lastReverted       int
	lastTotal          int
	lastFileCount      int
	lastRounds         int
	lastRequest        string
	lastOutcomeMsg     string
	lastChanges        []ReviewChange
	lastFiles          []ReviewFileSummary
	applyingCmd        bool
	yamlEditSessionRev uint64
	uiRev              uint64
}

func (e *Editor) bumpReviewUIRev() {
	if e == nil {
		return
	}
	e.review.uiRev++
}

func (e *Editor) reviewUIRev() uint64 {
	if e == nil {
		return 0
	}
	return e.review.uiRev
}

// HasAgentPanelCard reports whether the editor has a pending, kept, or reverted agent turn card to show in the right rail.
func (e *Editor) HasAgentPanelCard() bool {
	if e == nil {
		return false
	}
	return e.review.outcome != ReviewOutcomeNone
}

// ReviewOpen reports whether the full-window change-by-change Review screen is currently open.
func (e *Editor) ReviewOpen() bool {
	return e != nil && e.review.outcome == ReviewOutcomePending && e.review.open
}

// HasPendingReview reports whether an agent turn review is pending (either open full-window or paused via Esc).
func (e *Editor) HasPendingReview() bool {
	return e != nil && e.review.outcome == ReviewOutcomePending && len(e.review.changes) > 0
}

// ReviewOutcome returns the lifecycle status of the most recent agent turn review.
func (e *Editor) ReviewOutcome() ReviewOutcome {
	if e == nil {
		return ReviewOutcomeNone
	}
	return e.review.outcome
}

// resetTurnReviewForProject clears pending turn review state when switching projects or opening the Demo Room,
// preserving uiRev monotonicity so subscribed UI memos refresh cleanly.
func (e *Editor) resetTurnReviewForProject() {
	if e == nil {
		return
	}
	nextRev := e.review.uiRev + 1
	e.review = turnReviewState{uiRev: nextRev}
}

// reviewModalBlocking reports whether a modal overlay or shortcut capture is active on top of the Review screen
// and must prevent Review shortcuts/key handlers from firing underneath it.
func (e *Editor) reviewModalBlocking() bool {
	if e == nil {
		return true
	}
	return e.ReviewRevertAllDialogOpen() ||
		e.SettingsOpen() ||
		e.CommandPaletteOpen() ||
		e.ShortcutsSheetOpen() ||
		e.PermissionsDraftOpen() ||
		e.ConnectDialogOpen() ||
		e.AgentReviewOpen() ||
		e.shortcutCaptureActive
}

// reviewKeepShortcutAllowed reports whether ⌘↵ (CmdReviewKeep) may run right now without
// crossing You-only trust boundaries, firing behind modals, or colliding with other screens.
func (e *Editor) reviewKeepShortcutAllowed() bool {
	if e == nil || !e.HasPendingReview() || e.reviewModalBlocking() {
		return false
	}
	if e.ReviewOpen() {
		return true
	}
	return !e.Playing() &&
		!e.AskInPlaceOpen() &&
		!e.StartScreenOpen() &&
		!e.FirstRunFullScreenOpen() &&
		!e.MergeConflictOpen() &&
		!e.ChangesOpen() &&
		!e.BuildReleaseScreenOpen() &&
		!e.UsagesScreenOpen() &&
		!e.PlaytestReportOpen() &&
		!e.AssetGeneratorOpen() &&
		!e.HUDLayoutOpen() &&
		!e.CutsceneOpen() &&
		!e.SketchActive()
}

func (e *Editor) validateReviewRelPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("editor: invalid review file path %q", raw)
	}
	slashed := strings.ReplaceAll(trimmed, `\`, "/")
	if strings.HasPrefix(slashed, "/") || strings.ContainsRune(slashed, ':') {
		return "", fmt.Errorf("editor: invalid review file path %q", raw)
	}
	cleanPath := path.Clean(slashed)
	if cleanPath == "." || cleanPath == ".." || cleanPath == "" || strings.HasPrefix(cleanPath, "../") || path.IsAbs(cleanPath) {
		return "", fmt.Errorf("editor: invalid review file path %q", raw)
	}
	lower := strings.ToLower(cleanPath)
	if lower == ".git" || strings.HasPrefix(lower, ".git/") || strings.Contains(lower, "/.git/") {
		return "", fmt.Errorf("editor: review cannot modify git metadata %q", cleanPath)
	}
	if dom, ok := e.CheckYouOnlyTarget(cleanPath); ok {
		return "", fmt.Errorf("editor: review cannot modify You-only file %q (%s)", cleanPath, dom.DisplayName())
	}
	return cleanPath, nil
}

// StartTurnReview analyzes the agent turn's before/after file snapshots into grouped file/kind changes
// and opens the full-window Review screen (or leaves it pending when input.StartPending is true).
func (e *Editor) StartTurnReview(input ReviewTurnInput) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(input.Files) == 0 {
		return errors.New("editor: review turn has no files")
	}

	st := turnReviewState{
		open:           !input.StartPending,
		outcome:        ReviewOutcomePending,
		request:        strings.TrimSpace(input.Request),
		defaultWhy:     strings.TrimSpace(input.DefaultWhy),
		round:          1,
		viewRound:      1,
		roundRedone:    make(map[int]int),
		filesByPath:    make(map[string]*reviewFileState, len(input.Files)),
		changesByID:    make(map[string]*ReviewChange),
		filter:         ReviewFilterAll,
		collapsedFiles: make(map[string]bool),
		collapsedKinds: make(map[string]bool),
		legend: ReviewOverlayVisibility{
			Added:    true,
			Changed:  true,
			Removed:  true,
			Selected: true,
		},
		uiRev: e.review.uiRev + 1,
	}

	seenPaths := make(map[string]bool, len(input.Files))
	for _, fi := range input.Files {
		cleanPath, err := e.validateReviewRelPath(fi.Path)
		if err != nil {
			return err
		}
		if len(fi.BeforeYAML) > maxReviewFileBytes || len(fi.AfterYAML) > maxReviewFileBytes || len(fi.PersonYAML) > maxReviewFileBytes {
			return fmt.Errorf("editor: review file %q exceeds %d byte limit", cleanPath, maxReviewFileBytes)
		}
		if seenPaths[cleanPath] {
			return fmt.Errorf("editor: duplicate review file path %q", cleanPath)
		}
		seenPaths[cleanPath] = true

		existed := len(fi.BeforeYAML) > 0
		if fi.ExistedBefore != nil {
			existed = *fi.ExistedBefore
		}
		kind := fi.FileKind
		if kind == "" {
			kind = detectReviewFileKind(cleanPath, fi.BeforeYAML, fi.AfterYAML)
		}

		fs := &reviewFileState{
			path:          cleanPath,
			kind:          kind,
			existedBefore: existed,
			beforeYAML:    append([]byte(nil), fi.BeforeYAML...),
			afterYAML:     append([]byte(nil), fi.AfterYAML...),
			builtDirty:    true,
		}

		fileChanges, err := e.diffReviewFile(fs, fi, st.defaultWhy)
		if err != nil {
			return fmt.Errorf("editor: diff review file %s: %w", cleanPath, err)
		}
		if fs.kind == ReviewFileScene {
			if err := e.initReviewScenePersonDoc(fs, fi); err != nil {
				return fmt.Errorf("editor: parse review person scene %s: %w", cleanPath, err)
			}
		}
		if len(fileChanges) == 0 {
			continue
		}
		fs.initRound1()
		st.files = append(st.files, fs)
		st.filesByPath[cleanPath] = fs
		for _, ch := range fileChanges {
			ch.Note = truncateReviewNote(strings.TrimSpace(ch.Note))
			ch.initRound1()
			st.changes = append(st.changes, ch)
			st.changesByID[ch.ID] = ch
		}
	}

	if len(st.changes) == 0 {
		return errors.New("editor: review turn has no changes")
	}

	// Focus the first change and mark it seen.
	first := st.changes[0]
	first.Seen = true
	st.focusedID = first.ID
	st.focusedRowKey = reviewCardRowKey(first.ID)
	st.lastRequest = st.request
	st.lastTotal = len(st.changes)
	st.lastFileCount = len(st.files)

	e.ExitHistoryPreview()
	e.review = st
	e.syncAllReviewConflicts()
	e.cancelAutoSaveTimer()
	if input.StartPending {
		e.showAgentTabForReview()
	}
	e.refreshPrefabImpacts()
	e.frameFocusedReviewChange()
	e.notify()
	return nil
}

// showAgentTabForReview switches the unified right rail to the Agent tab (closing the Tasks panel if open)
// without emitting a premature notification when the caller is about to call notify.
func (e *Editor) showAgentTabForReview() {
	if e == nil || e.agent.mode == AgentModeNone {
		return
	}
	e.tasks.mu.Lock()
	e.tasks.panelOpen = false
	e.tasks.mu.Unlock()
	e.agent.rightTab = RightTabAgent
}

func detectReviewFileKind(cleanPath string, before, after []byte) ReviewFileKind {
	lower := strings.ToLower(cleanPath)
	switch {
	case strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml"):
		return ReviewFileScene
	case strings.HasSuffix(lower, ".prefab.yaml") || strings.HasSuffix(lower, ".prefab.yml") || strings.HasPrefix(lower, "prefabs/"):
		return ReviewFilePrefab
	case strings.HasSuffix(lower, ".go"):
		return ReviewFileGo
	case strings.HasPrefix(lower, "data/") && (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")):
		return ReviewFileData
	}
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		sample := after
		if len(sample) == 0 {
			sample = before
		}
		if len(sample) > 0 {
			if _, err := datatable.ParseDocument(cleanPath, sample); err == nil {
				return ReviewFileData
			}
		}
	}
	return ReviewFileOther
}

func reviewLookupReason(reasons map[string]string, target, fallback string) string {
	if reasons != nil {
		if r := strings.TrimSpace(reasons[target]); r != "" {
			return r
		}
		if r := strings.TrimSpace(reasons["*"]); r != "" {
			return r
		}
	}
	if fallback != "" {
		return fallback
	}
	return "Changed as part of this agent request."
}

func (e *Editor) diffReviewFile(fs *reviewFileState, fi ReviewFileInput, defaultWhy string) ([]*ReviewChange, error) {
	switch fs.kind {
	case ReviewFileScene:
		return e.diffReviewSceneFile(fs, fi, defaultWhy)
	case ReviewFilePrefab:
		return e.diffReviewPrefabFile(fs, fi, defaultWhy)
	case ReviewFileData:
		return e.diffReviewDataFile(fs, fi, defaultWhy)
	default:
		return diffReviewTextFile(fs, fi, defaultWhy), nil
	}
}

type reviewFlatSceneObj struct {
	obj      *sceneformat.ObjectDoc
	layer    string
	parentID string
}

func flattenReviewSceneDocObjects(doc *sceneformat.SceneDoc) ([]reviewFlatSceneObj, map[string]reviewFlatSceneObj) {
	var list []reviewFlatSceneObj
	byID := make(map[string]reviewFlatSceneObj)
	if doc == nil {
		return list, byID
	}
	var walk func(layer, parent string, objs []sceneformat.ObjectDoc)
	walk = func(layer, parent string, objs []sceneformat.ObjectDoc) {
		for i := range objs {
			cp := cloneObjectDoc(objs[i])
			item := reviewFlatSceneObj{obj: &cp, layer: layer, parentID: parent}
			list = append(list, item)
			byID[cp.ID] = item
			if len(objs[i].Children) > 0 {
				walk(layer, cp.ID, objs[i].Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Name, "", doc.Layers[i].Objects)
	}
	return list, byID
}

func (e *Editor) diffReviewSceneFile(fs *reviewFileState, fi ReviewFileInput, defaultWhy string) ([]*ReviewChange, error) {
	opts := sceneformat.ParseOptions{Components: e.components}
	var beforeDoc, afterDoc *sceneformat.SceneDoc
	var err error
	if len(fs.beforeYAML) > 0 {
		beforeDoc, err = sceneformat.ParseWith(fs.beforeYAML, opts)
		if err != nil {
			return nil, fmt.Errorf("parse before scene: %w", err)
		}
	} else {
		beforeDoc = DefaultSceneDoc()
	}
	if len(fs.afterYAML) > 0 {
		afterDoc, err = sceneformat.ParseWith(fs.afterYAML, opts)
		if err != nil {
			return nil, fmt.Errorf("parse after scene: %w", err)
		}
	} else {
		afterDoc = DefaultSceneDoc()
	}
	fs.beforeDoc = beforeDoc
	fs.afterDoc = afterDoc

	beforeList, beforeMap := flattenReviewSceneDocObjects(beforeDoc)
	afterList, afterMap := flattenReviewSceneDocObjects(afterDoc)

	var added, changed, removed []*ReviewChange

	for _, a := range afterList {
		b, existed := beforeMap[a.obj.ID]
		if !existed {
			detail := fi.Details[a.obj.ID]
			if detail == "" {
				detail = formatAddedObjectDetail(a.obj)
			}
			line, ylines := buildAddedObjectYAMLDiff(fs.afterYAML, a.obj)
			ch := &ReviewChange{
				ID:          reviewChangeID(fs.path, ReviewChangeAdded, a.obj.ID),
				File:        fs.path,
				FileKind:    ReviewFileScene,
				Kind:        ReviewChangeAdded,
				Title:       a.obj.ID,
				Detail:      detail,
				Why:         reviewLookupReason(fi.Reasons, a.obj.ID, defaultWhy),
				ObjectID:    a.obj.ID,
				Layer:       a.layer,
				ParentID:    a.parentID,
				Line:        line,
				YAMLLines:   ylines,
				Note:        fi.Notes[a.obj.ID],
				Blocker:     fi.Blockers[a.obj.ID],
				afterObj:    a.obj,
				afterLayer:  a.layer,
				beforeLayer: a.layer,
			}
			added = append(added, ch)
			continue
		}
		if !sceneObjectsEqual(b.obj, a.obj) || b.layer != a.layer || b.parentID != a.parentID {
			detail := fi.Details[a.obj.ID]
			if detail == "" {
				detail = formatChangedObjectDetail(b.obj, a.obj, b.layer, a.layer)
			}
			line, ylines := buildChangedObjectYAMLDiff(fs.beforeYAML, fs.afterYAML, b.obj, a.obj)
			ch := &ReviewChange{
				ID:             reviewChangeID(fs.path, ReviewChangeChanged, a.obj.ID),
				File:           fs.path,
				FileKind:       ReviewFileScene,
				Kind:           ReviewChangeChanged,
				Title:          a.obj.ID,
				Detail:         detail,
				Why:            reviewLookupReason(fi.Reasons, a.obj.ID, defaultWhy),
				ObjectID:       a.obj.ID,
				Layer:          a.layer,
				ParentID:       a.parentID,
				Line:           line,
				YAMLLines:      ylines,
				Note:           fi.Notes[a.obj.ID],
				Blocker:        fi.Blockers[a.obj.ID],
				beforeObj:      b.obj,
				afterObj:       a.obj,
				beforeLayer:    b.layer,
				afterLayer:     a.layer,
				beforeParentID: b.parentID,
			}
			changed = append(changed, ch)
		}
	}

	for _, b := range beforeList {
		if _, exists := afterMap[b.obj.ID]; !exists {
			detail := fi.Details[b.obj.ID]
			if detail == "" {
				detail = fmt.Sprintf("removed from %s (%.0f, %.0f)", b.layer, b.obj.X, b.obj.Y)
			}
			line, ylines := buildRemovedObjectYAMLDiff(fs.beforeYAML, b.obj)
			ch := &ReviewChange{
				ID:          reviewChangeID(fs.path, ReviewChangeRemoved, b.obj.ID),
				File:        fs.path,
				FileKind:    ReviewFileScene,
				Kind:        ReviewChangeRemoved,
				Title:       b.obj.ID,
				Detail:      detail,
				Why:         reviewLookupReason(fi.Reasons, b.obj.ID, defaultWhy),
				ObjectID:    b.obj.ID,
				Layer:       b.layer,
				ParentID:    b.parentID,
				Line:        line,
				YAMLLines:   ylines,
				Note:        fi.Notes[b.obj.ID],
				Blocker:     fi.Blockers[b.obj.ID],
				beforeObj:   b.obj,
				beforeLayer: b.layer,
			}
			removed = append(removed, ch)
		}
	}

	out := make([]*ReviewChange, 0, len(added)+len(changed)+len(removed))
	out = append(out, added...)
	out = append(out, changed...)
	out = append(out, removed...)
	return out, nil
}

func sceneObjectsEqual(a, b *sceneformat.ObjectDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	ca := cloneObjectDoc(*a)
	cb := cloneObjectDoc(*b)
	// Compare object's own fields, ignoring children (which are diffed individually).
	ca.Children = nil
	cb.Children = nil
	return reflect.DeepEqual(ca, cb)
}

func formatAddedObjectDetail(o *sceneformat.ObjectDoc) string {
	var parts []string
	if o.Prefab != "" {
		parts = append(parts, path.Base(o.Prefab))
	} else if o.Scene != "" {
		parts = append(parts, path.Base(o.Scene))
	}
	parts = append(parts, fmt.Sprintf("x %s, y %s", formatCompactFloat(float64(o.X)), formatCompactFloat(float64(o.Y))))
	return strings.Join(parts, " · ")
}

func formatChangedObjectDetail(before, after *sceneformat.ObjectDoc, beforeLayer, afterLayer string) string {
	var parts []string
	if before.X != after.X {
		parts = append(parts, fmt.Sprintf("x %s → %s", formatCompactFloat(float64(before.X)), formatCompactFloat(float64(after.X))))
	}
	if before.Y != after.Y {
		parts = append(parts, fmt.Sprintf("y %s → %s", formatCompactFloat(float64(before.Y)), formatCompactFloat(float64(after.Y))))
	}
	if before.Z != after.Z {
		parts = append(parts, fmt.Sprintf("z %d → %d", before.Z, after.Z))
	}
	if before.Name != after.Name {
		parts = append(parts, fmt.Sprintf("name %q → %q", before.Name, after.Name))
	}
	if before.Rotation != after.Rotation {
		parts = append(parts, fmt.Sprintf("rotation %s → %s", formatCompactFloat(float64(before.Rotation)), formatCompactFloat(float64(after.Rotation))))
	}
	if before.EffectiveScaleX() != after.EffectiveScaleX() || before.EffectiveScaleY() != after.EffectiveScaleY() {
		parts = append(parts, fmt.Sprintf("scale (%s,%s) → (%s,%s)",
			formatCompactFloat(float64(before.EffectiveScaleX())), formatCompactFloat(float64(before.EffectiveScaleY())),
			formatCompactFloat(float64(after.EffectiveScaleX())), formatCompactFloat(float64(after.EffectiveScaleY()))))
	}
	if beforeLayer != afterLayer {
		parts = append(parts, fmt.Sprintf("layer %s → %s", beforeLayer, afterLayer))
	}
	if !reflect.DeepEqual(before.Components, after.Components) {
		compParts := diffFlatMapFields(before.Components, after.Components, "")
		parts = append(parts, compParts...)
	}
	if !reflect.DeepEqual(before.Properties, after.Properties) {
		propParts := diffFlatMapFields(before.Properties, after.Properties, "")
		parts = append(parts, propParts...)
	}
	if len(parts) == 0 {
		return "updated fields"
	}
	if len(parts) > 2 {
		return strings.Join(parts[:2], " · ") + fmt.Sprintf(" · +%d more", len(parts)-2)
	}
	return strings.Join(parts, " · ")
}

func diffFlatMapFields(before, after map[string]any, prefix string) []string {
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]bool)
	for k := range before {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range after {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out []string
	for _, k := range keys {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		bv, bok := before[k]
		av, aok := after[k]
		if !bok {
			out = append(out, fmt.Sprintf("+%s %v", full, av))
			continue
		}
		if !aok {
			out = append(out, fmt.Sprintf("−%s", full))
			continue
		}
		bm, bmok := bv.(map[string]any)
		am, amok := av.(map[string]any)
		if bmok && amok {
			out = append(out, diffFlatMapFields(bm, am, full)...)
			continue
		}
		if !reflect.DeepEqual(bv, av) {
			out = append(out, fmt.Sprintf("%s %v → %v", full, bv, av))
		}
	}
	return out
}

func formatCompactFloat(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-4 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func formatDeltaPercent(oldStr, newStr string) string {
	ov, err1 := strconv.ParseFloat(strings.TrimSpace(oldStr), 64)
	nv, err2 := strconv.ParseFloat(strings.TrimSpace(newStr), 64)
	if err1 != nil || err2 != nil || ov == 0 || math.IsNaN(ov) || math.IsNaN(nv) || math.IsInf(ov, 0) || math.IsInf(nv, 0) {
		return ""
	}
	pct := ((nv - ov) / math.Abs(ov)) * 100
	rounded := math.Round(pct)
	if rounded == 0 {
		return ""
	}
	if rounded < 0 {
		return fmt.Sprintf("−%d%%", int(-rounded))
	}
	return fmt.Sprintf("+%d%%", int(rounded))
}

type yamlLeafEntry struct {
	path  string
	value string
	line  int
	node  *yaml.Node
}

func collectYAMLLeaves(data []byte) ([]yamlLeafEntry, map[string]yamlLeafEntry, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, map[string]yamlLeafEntry{}, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}
	var list []yamlLeafEntry
	byPath := make(map[string]yamlLeafEntry)
	var walk func(n *yaml.Node, prefix string)
	walk = func(n *yaml.Node, prefix string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(c, prefix)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i].Value
				v := n.Content[i+1]
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				if v.Kind == yaml.MappingNode {
					walk(v, p)
				} else {
					val := yamlScalarDisplay(v)
					ent := yamlLeafEntry{path: p, value: val, line: n.Content[i].Line, node: v}
					list = append(list, ent)
					byPath[p] = ent
				}
			}
		case yaml.SequenceNode:
			for idx, c := range n.Content {
				p := fmt.Sprintf("%s[%d]", prefix, idx)
				if c.Kind == yaml.MappingNode {
					walk(c, p)
				} else {
					val := yamlScalarDisplay(c)
					ent := yamlLeafEntry{path: p, value: val, line: c.Line, node: c}
					list = append(list, ent)
					byPath[p] = ent
				}
			}
		}
	}
	walk(&root, "")
	return list, byPath, nil
}

func yamlScalarDisplay(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return n.Value
	}
	return strings.TrimSpace(string(b))
}

func (e *Editor) diffReviewPrefabFile(fs *reviewFileState, fi ReviewFileInput, defaultWhy string) ([]*ReviewChange, error) {
	beforeList, beforeMap, err := collectYAMLLeaves(fs.beforeYAML)
	if err != nil {
		return nil, fmt.Errorf("parse before prefab YAML: %w", err)
	}
	afterList, afterMap, err := collectYAMLLeaves(fs.afterYAML)
	if err != nil {
		return nil, fmt.Errorf("parse after prefab YAML: %w", err)
	}

	var added, changed, removed []*ReviewChange
	for _, a := range afterList {
		displayField := trimComponentsPrefix(a.path)
		b, existed := beforeMap[a.path]
		if !existed {
			detail := fi.Details[a.path]
			if detail == "" {
				detail = fi.Details[displayField]
			}
			if detail == "" {
				detail = a.value
			}
			ch := &ReviewChange{
				ID:            reviewChangeID(fs.path, ReviewChangeAdded, a.path),
				File:          fs.path,
				FileKind:      ReviewFilePrefab,
				Kind:          ReviewChangeAdded,
				Title:         displayField,
				Detail:        detail,
				Why:           reviewLookupReason(fi.Reasons, a.path, reviewLookupReason(fi.Reasons, displayField, defaultWhy)),
				Field:         a.path,
				NewValue:      a.value,
				Line:          a.line,
				YAMLLines:     buildFieldYAMLDiff(fs.beforeYAML, fs.afterYAML, 0, a.line),
				Note:          reviewFirstNonEmpty(fi.Notes[a.path], fi.Notes[displayField]),
				Blocker:       reviewFirstNonEmpty(fi.Blockers[a.path], fi.Blockers[displayField]),
				afterYAMLNode: a.node,
			}
			added = append(added, ch)
			continue
		}
		if b.value != a.value {
			delta := formatDeltaPercent(b.value, a.value)
			detail := fi.Details[a.path]
			if detail == "" {
				detail = fi.Details[displayField]
			}
			if detail == "" {
				detail = fmt.Sprintf("%s → %s", b.value, a.value)
				if delta != "" {
					detail += " · " + delta
				}
			}
			ch := &ReviewChange{
				ID:             reviewChangeID(fs.path, ReviewChangeChanged, a.path),
				File:           fs.path,
				FileKind:       ReviewFilePrefab,
				Kind:           ReviewChangeChanged,
				Title:          displayField,
				Detail:         detail,
				Why:            reviewLookupReason(fi.Reasons, a.path, reviewLookupReason(fi.Reasons, displayField, defaultWhy)),
				Field:          a.path,
				OldValue:       b.value,
				NewValue:       a.value,
				DeltaPct:       delta,
				Line:           a.line,
				YAMLLines:      buildFieldYAMLDiff(fs.beforeYAML, fs.afterYAML, b.line, a.line),
				Note:           reviewFirstNonEmpty(fi.Notes[a.path], fi.Notes[displayField]),
				Blocker:        reviewFirstNonEmpty(fi.Blockers[a.path], fi.Blockers[displayField]),
				beforeYAMLNode: b.node,
				afterYAMLNode:  a.node,
			}
			changed = append(changed, ch)
		}
	}
	for _, b := range beforeList {
		if _, exists := afterMap[b.path]; !exists {
			displayField := trimComponentsPrefix(b.path)
			detail := fi.Details[b.path]
			if detail == "" {
				detail = fi.Details[displayField]
			}
			if detail == "" {
				detail = "removed (" + b.value + ")"
			}
			ch := &ReviewChange{
				ID:             reviewChangeID(fs.path, ReviewChangeRemoved, b.path),
				File:           fs.path,
				FileKind:       ReviewFilePrefab,
				Kind:           ReviewChangeRemoved,
				Title:          displayField,
				Detail:         detail,
				Why:            reviewLookupReason(fi.Reasons, b.path, reviewLookupReason(fi.Reasons, displayField, defaultWhy)),
				Field:          b.path,
				OldValue:       b.value,
				Line:           b.line,
				YAMLLines:      buildFieldYAMLDiff(fs.beforeYAML, fs.afterYAML, b.line, 0),
				Note:           reviewFirstNonEmpty(fi.Notes[b.path], fi.Notes[displayField]),
				Blocker:        reviewFirstNonEmpty(fi.Blockers[b.path], fi.Blockers[displayField]),
				beforeYAMLNode: b.node,
			}
			removed = append(removed, ch)
		}
	}

	out := make([]*ReviewChange, 0, len(added)+len(changed)+len(removed))
	out = append(out, added...)
	out = append(out, changed...)
	out = append(out, removed...)
	return out, nil
}

func trimComponentsPrefix(fieldPath string) string {
	if strings.HasPrefix(fieldPath, "components.") {
		return strings.TrimPrefix(fieldPath, "components.")
	}
	return fieldPath
}

func reviewFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (e *Editor) diffReviewDataFile(fs *reviewFileState, fi ReviewFileInput, defaultWhy string) ([]*ReviewChange, error) {
	var beforeDoc, afterDoc *datatable.Document
	var err error
	if len(fs.beforeYAML) > 0 {
		beforeDoc, err = datatable.ParseDocument(fs.path, fs.beforeYAML)
		if err != nil {
			return nil, fmt.Errorf("parse before data table: %w", err)
		}
	}
	if len(fs.afterYAML) > 0 {
		afterDoc, err = datatable.ParseDocument(fs.path, fs.afterYAML)
		if err != nil {
			return nil, fmt.Errorf("parse after data table: %w", err)
		}
	}
	fs.beforeTable = beforeDoc
	fs.afterTable = afterDoc

	beforeByID := make(map[string]datatable.Row)
	if beforeDoc != nil {
		for _, r := range beforeDoc.Rows() {
			beforeByID[dataRowID(r)] = r
		}
	}
	afterByID := make(map[string]datatable.Row)
	if afterDoc != nil {
		for _, r := range afterDoc.Rows() {
			afterByID[dataRowID(r)] = r
		}
	}

	var added, changed, removed []*ReviewChange
	if afterDoc != nil {
		for _, ar := range afterDoc.Rows() {
			id := dataRowID(ar)
			br, existed := beforeByID[id]
			if !existed {
				detail := fi.Details[id]
				if detail == "" {
					detail = "new record"
				}
				line, ylines := buildDataRowYAMLDiff(fs.beforeYAML, fs.afterYAML, id, ReviewChangeAdded)
				added = append(added, &ReviewChange{
					ID:        reviewChangeID(fs.path, ReviewChangeAdded, id),
					File:      fs.path,
					FileKind:  ReviewFileData,
					Kind:      ReviewChangeAdded,
					Title:     id,
					Detail:    detail,
					Why:       reviewLookupReason(fi.Reasons, id, defaultWhy),
					RowID:     id,
					Line:      line,
					YAMLLines: ylines,
					Note:      fi.Notes[id],
					Blocker:   fi.Blockers[id],
				})
				continue
			}
			diffs := diffDataTableRows(br, ar)
			if len(diffs) > 0 {
				detail := fi.Details[id]
				if detail == "" {
					d0 := diffs[0]
					detail = fmt.Sprintf("%s %s → %s", d0.Column, d0.OldValue, d0.NewValue)
					if d0.DeltaPct != "" {
						detail += " · " + d0.DeltaPct
					}
					if len(diffs) > 1 {
						detail += fmt.Sprintf(" · +%d more", len(diffs)-1)
					}
				}
				line, ylines := buildDataRowYAMLDiff(fs.beforeYAML, fs.afterYAML, id, ReviewChangeChanged)
				changed = append(changed, &ReviewChange{
					ID:        reviewChangeID(fs.path, ReviewChangeChanged, id),
					File:      fs.path,
					FileKind:  ReviewFileData,
					Kind:      ReviewChangeChanged,
					Title:     id,
					Detail:    detail,
					Why:       reviewLookupReason(fi.Reasons, id, defaultWhy),
					RowID:     id,
					Field:     diffs[0].Column,
					OldValue:  diffs[0].OldValue,
					NewValue:  diffs[0].NewValue,
					DeltaPct:  diffs[0].DeltaPct,
					Line:      line,
					YAMLLines: ylines,
					Note:      fi.Notes[id],
					Blocker:   fi.Blockers[id],
					cellDiffs: diffs,
				})
			}
		}
	}
	if beforeDoc != nil {
		for _, br := range beforeDoc.Rows() {
			id := dataRowID(br)
			if _, exists := afterByID[id]; !exists {
				detail := fi.Details[id]
				if detail == "" {
					detail = "removed record"
				}
				line, ylines := buildDataRowYAMLDiff(fs.beforeYAML, fs.afterYAML, id, ReviewChangeRemoved)
				removed = append(removed, &ReviewChange{
					ID:        reviewChangeID(fs.path, ReviewChangeRemoved, id),
					File:      fs.path,
					FileKind:  ReviewFileData,
					Kind:      ReviewChangeRemoved,
					Title:     id,
					Detail:    detail,
					Why:       reviewLookupReason(fi.Reasons, id, defaultWhy),
					RowID:     id,
					Line:      line,
					YAMLLines: ylines,
					Note:      fi.Notes[id],
					Blocker:   fi.Blockers[id],
				})
			}
		}
	}

	out := make([]*ReviewChange, 0, len(added)+len(changed)+len(removed))
	out = append(out, added...)
	out = append(out, changed...)
	out = append(out, removed...)
	return out, nil
}

func dataValueDisplay(v datatable.Value) string {
	if v.Kind == datatable.KindList {
		parts := make([]string, len(v.List))
		for i, item := range v.List {
			parts[i] = dataValueDisplay(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return v.Text
}

func dataRowID(r datatable.Row) string {
	if v, ok := r.Get("id"); ok {
		return dataValueDisplay(v)
	}
	if len(r.Cells) > 0 {
		return dataValueDisplay(r.Cells[0].Value)
	}
	return "row"
}

func diffDataTableRows(before, after datatable.Row) []ReviewDataCellDiff {
	keys := make([]string, 0, len(before.Cells)+len(after.Cells))
	seen := make(map[string]bool)
	for _, k := range before.Keys() {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, k := range after.Keys() {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	var diffs []ReviewDataCellDiff
	for _, k := range keys {
		if k == "id" {
			continue
		}
		bv := ""
		if v, ok := before.Get(k); ok {
			bv = dataValueDisplay(v)
		}
		av := ""
		if v, ok := after.Get(k); ok {
			av = dataValueDisplay(v)
		}
		if bv != av {
			diffs = append(diffs, ReviewDataCellDiff{
				Column:   k,
				OldValue: bv,
				NewValue: av,
				DeltaPct: formatDeltaPercent(bv, av),
			})
		}
	}
	return diffs
}

func diffReviewTextFile(fs *reviewFileState, fi ReviewFileInput, defaultWhy string) []*ReviewChange {
	if bytes.Equal(fs.beforeYAML, fs.afterYAML) {
		return nil
	}
	kind := ReviewChangeChanged
	if !fs.existedBefore && len(fs.afterYAML) > 0 {
		kind = ReviewChangeAdded
	} else if len(fs.afterYAML) == 0 && fs.existedBefore {
		kind = ReviewChangeRemoved
	}
	ylines, addCount, delCount := buildUnifiedTextDiff(fs.beforeYAML, fs.afterYAML)
	name := path.Base(fs.path)
	detail := fi.Details[name]
	if detail == "" {
		detail = fi.Details[fs.path]
	}
	if detail == "" {
		detail = fmt.Sprintf("+%d −%d", addCount, delCount)
	}
	return []*ReviewChange{{
		ID:                reviewChangeID(fs.path, kind, name),
		File:              fs.path,
		FileKind:          fs.kind,
		Kind:              kind,
		Title:             name,
		Detail:            detail,
		Why:               reviewLookupReason(fi.Reasons, name, reviewLookupReason(fi.Reasons, fs.path, defaultWhy)),
		Line:              1,
		YAMLLines:         ylines,
		Note:              reviewFirstNonEmpty(fi.Notes[name], fi.Notes[fs.path]),
		Blocker:           reviewFirstNonEmpty(fi.Blockers[name], fi.Blockers[fs.path]),
		GoRequiresConfirm: fs.kind == ReviewFileGo,
	}}
}

func reviewChangeID(file string, kind ReviewChangeKind, target string) string {
	return file + "::" + string(kind) + "::" + target
}

func reviewFileRowKey(file string) string {
	return "file:" + file
}

func reviewKindRowKey(file string, kind ReviewChangeKind) string {
	return "kind:" + file + ":" + string(kind)
}

func reviewCardRowKey(changeID string) string {
	return "card:" + changeID
}

// --- Line-numbered YAML diff builders ---

func splitLinesNoCR(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func findObjectBlockInYAML(data []byte, objectID string) (startLine int, lines []string) {
	all := splitLinesNoCR(data)
	needle := "- id: " + objectID
	needleQuoted := fmt.Sprintf("- id: %q", objectID)
	needleSingle := fmt.Sprintf("- id: '%s'", objectID)
	idPlain := "id: " + objectID
	idQuoted := fmt.Sprintf("id: %q", objectID)
	idSingle := fmt.Sprintf("id: '%s'", objectID)
	for i, l := range all {
		trimmed := strings.TrimSpace(l)
		if trimmed == needle || trimmed == needleQuoted || trimmed == needleSingle || trimmed == idPlain || trimmed == idQuoted || trimmed == idSingle {
			indent := len(l) - len(strings.TrimLeft(l, " "))
			end := i + 1
			for end < len(all) {
				next := all[end]
				if strings.TrimSpace(next) == "" {
					end++
					continue
				}
				ni := len(next) - len(strings.TrimLeft(next, " "))
				if ni <= indent {
					break
				}
				end++
			}
			return i + 1, all[i:end]
		}
	}
	return 1, nil
}

func buildAddedObjectYAMLDiff(afterYAML []byte, obj *sceneformat.ObjectDoc) (int, []ReviewYAMLLine) {
	start, block := findObjectBlockInYAML(afterYAML, obj.ID)
	all := splitLinesNoCR(afterYAML)
	var out []ReviewYAMLLine
	if start > 1 && start-2 < len(all) {
		ctxStart := max(1, start-2)
		for ln := ctxStart; ln < start; ln++ {
			out = append(out, ReviewYAMLLine{Line: ln, Text: all[ln-1]})
		}
	}
	for i, l := range block {
		out = append(out, ReviewYAMLLine{Line: start + i, Sign: "+", Text: l})
	}
	if len(out) == 0 {
		b, _ := yaml.Marshal(obj)
		for i, l := range splitLinesNoCR(b) {
			out = append(out, ReviewYAMLLine{Line: i + 1, Sign: "+", Text: "  " + l})
		}
	}
	return start, out
}

func buildRemovedObjectYAMLDiff(beforeYAML []byte, obj *sceneformat.ObjectDoc) (int, []ReviewYAMLLine) {
	start, block := findObjectBlockInYAML(beforeYAML, obj.ID)
	var out []ReviewYAMLLine
	for i, l := range block {
		out = append(out, ReviewYAMLLine{Line: start + i, Sign: "-", Text: l})
	}
	if len(out) == 0 {
		b, _ := yaml.Marshal(obj)
		for i, l := range splitLinesNoCR(b) {
			out = append(out, ReviewYAMLLine{Line: i + 1, Sign: "-", Text: "  " + l})
		}
	}
	return start, out
}

func buildChangedObjectYAMLDiff(beforeYAML, afterYAML []byte, beforeObj, afterObj *sceneformat.ObjectDoc) (int, []ReviewYAMLLine) {
	bStart, bBlock := findObjectBlockInYAML(beforeYAML, beforeObj.ID)
	aStart, aBlock := findObjectBlockInYAML(afterYAML, afterObj.ID)
	if len(bBlock) == 0 || len(aBlock) == 0 {
		bRaw, _ := yaml.Marshal(beforeObj)
		aRaw, _ := yaml.Marshal(afterObj)
		lines, _, _ := buildUnifiedTextDiff(bRaw, aRaw)
		return max(aStart, 1), lines
	}
	out := diffLineBlocks(bBlock, aBlock, bStart, aStart)
	return aStart, out
}

func buildFieldYAMLDiff(beforeYAML, afterYAML []byte, beforeLine, afterLine int) []ReviewYAMLLine {
	bLines := splitLinesNoCR(beforeYAML)
	aLines := splitLinesNoCR(afterYAML)
	var out []ReviewYAMLLine
	if beforeLine > 0 && afterLine > 0 && beforeLine <= len(bLines) && afterLine <= len(aLines) {
		ctxStart := max(1, afterLine-2)
		for ln := ctxStart; ln < afterLine; ln++ {
			out = append(out, ReviewYAMLLine{Line: ln, Text: aLines[ln-1]})
		}
		out = append(out, ReviewYAMLLine{Line: beforeLine, Sign: "-", Text: bLines[beforeLine-1]})
		out = append(out, ReviewYAMLLine{Line: afterLine, Sign: "+", Text: aLines[afterLine-1]})
		ctxEnd := min(len(aLines), afterLine+2)
		for ln := afterLine + 1; ln <= ctxEnd; ln++ {
			out = append(out, ReviewYAMLLine{Line: ln, Text: aLines[ln-1]})
		}
		return out
	}
	if afterLine > 0 && afterLine <= len(aLines) {
		ctxStart := max(1, afterLine-2)
		for ln := ctxStart; ln < afterLine; ln++ {
			out = append(out, ReviewYAMLLine{Line: ln, Text: aLines[ln-1]})
		}
		out = append(out, ReviewYAMLLine{Line: afterLine, Sign: "+", Text: aLines[afterLine-1]})
		return out
	}
	if beforeLine > 0 && beforeLine <= len(bLines) {
		out = append(out, ReviewYAMLLine{Line: beforeLine, Sign: "-", Text: bLines[beforeLine-1]})
		return out
	}
	lines, _, _ := buildUnifiedTextDiff(beforeYAML, afterYAML)
	return lines
}

func buildDataRowYAMLDiff(beforeYAML, afterYAML []byte, rowID string, kind ReviewChangeKind) (int, []ReviewYAMLLine) {
	bStart, bBlock := findObjectBlockInYAML(beforeYAML, rowID)
	aStart, aBlock := findObjectBlockInYAML(afterYAML, rowID)
	switch kind {
	case ReviewChangeAdded:
		var out []ReviewYAMLLine
		for i, l := range aBlock {
			out = append(out, ReviewYAMLLine{Line: aStart + i, Sign: "+", Text: l})
		}
		return aStart, out
	case ReviewChangeRemoved:
		var out []ReviewYAMLLine
		for i, l := range bBlock {
			out = append(out, ReviewYAMLLine{Line: bStart + i, Sign: "-", Text: l})
		}
		return bStart, out
	default:
		return aStart, diffLineBlocks(bBlock, aBlock, bStart, aStart)
	}
}

func buildUnifiedTextDiff(before, after []byte) ([]ReviewYAMLLine, int, int) {
	bLines := splitLinesNoCR(before)
	aLines := splitLinesNoCR(after)
	lines := diffLineBlocks(bLines, aLines, 1, 1)
	adds, dels := 0, 0
	for _, l := range lines {
		switch l.Sign {
		case "+":
			adds++
		case "-":
			dels++
		}
	}
	return lines, adds, dels
}

// diffLineBlocks computes a deterministic LCS-based line diff between before and after with 1-based line numbers.
func diffLineBlocks(before, after []string, beforeStart, afterStart int) []ReviewYAMLLine {
	m, n := len(before), len(after)
	// Bound DP table for very large files; fall back to linear prefix/suffix diff.
	if m*n > 200000 {
		var out []ReviewYAMLLine
		for i, l := range before {
			out = append(out, ReviewYAMLLine{Line: beforeStart + i, Sign: "-", Text: l})
		}
		for j, l := range after {
			out = append(out, ReviewYAMLLine{Line: afterStart + j, Sign: "+", Text: l})
		}
		return out
	}
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if before[i] == after[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out []ReviewYAMLLine
	i, j := 0, 0
	for i < m || j < n {
		switch {
		case i < m && j < n && before[i] == after[j]:
			out = append(out, ReviewYAMLLine{Line: afterStart + j, Text: after[j]})
			i++
			j++
		case i < m && (j == n || dp[i+1][j] >= dp[i][j+1]):
			out = append(out, ReviewYAMLLine{Line: beforeStart + i, Sign: "-", Text: before[i]})
			i++
		default:
			out = append(out, ReviewYAMLLine{Line: afterStart + j, Sign: "+", Text: after[j]})
			j++
		}
	}
	return out
}

// --- Prefab reference impact computation ---

func (e *Editor) refreshPrefabImpacts() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	for _, ch := range e.review.changes {
		if ch.FileKind != ReviewFilePrefab {
			continue
		}
		ch.PrefabImpact = e.computePrefabImpact(ch.File)
	}
}

func (e *Editor) computePrefabImpact(prefabPath string) string {
	prefabClean := path.Clean(strings.ReplaceAll(prefabPath, `\`, "/"))
	stem := strings.TrimSuffix(strings.TrimSuffix(path.Base(prefabClean), ".prefab.yaml"), ".yaml")
	if stem == "" {
		stem = "instance"
	}

	type sceneCount struct {
		name  string
		count int
	}

	// 1. Count instances across scenes in this review (using effective current docs).
	reviewScenes := make(map[string]bool)
	var inReview []sceneCount
	totalInReview := 0
	for _, fs := range e.review.files {
		if fs.kind != ReviewFileScene {
			continue
		}
		reviewScenes[fs.path] = true
		doc := e.effectiveReviewSceneDoc(fs)
		n := countScenePrefabInstances(doc, prefabClean)
		if n > 0 {
			name := strings.TrimSuffix(strings.TrimSuffix(path.Base(fs.path), ".scene.yaml"), ".yaml")
			inReview = append(inReview, sceneCount{name: name, count: n})
			totalInReview += n
		}
	}

	// 2. Count instances across existing scenes in the project outside this review using FindUsages.
	var inProject []sceneCount
	totalInProject := 0
	if e.ProjectPath != "" {
		if rep, err := e.FindUsages(prefabClean); err == nil && rep != nil {
			for _, sg := range rep.Scenes {
				cleanScene := path.Clean(strings.ReplaceAll(sg.File, `\`, "/"))
				if reviewScenes[cleanScene] || len(sg.Refs) == 0 {
					continue
				}
				name := strings.TrimSuffix(strings.TrimSuffix(path.Base(cleanScene), ".scene.yaml"), ".yaml")
				inProject = append(inProject, sceneCount{name: name, count: len(sg.Refs)})
				totalInProject += len(sg.Refs)
			}
		}
	}

	noun := func(n int) string {
		if n == 1 {
			return stem
		}
		return stem + "s"
	}

	var clauses []string
	if totalInReview > 0 {
		var breakdown []string
		for _, sc := range inReview {
			breakdown = append(breakdown, fmt.Sprintf("%d in %s", sc.count, sc.name))
		}
		clauses = append(clauses, fmt.Sprintf("all %d %s in this review (%s)", totalInReview, noun(totalInReview), strings.Join(breakdown, ", ")))
	}
	if totalInProject > 0 {
		var breakdown []string
		for _, sc := range inProject {
			breakdown = append(breakdown, fmt.Sprintf("the %d %s already in %s", sc.count, noun(sc.count), sc.name))
		}
		clauses = append(clauses, strings.Join(breakdown, ", "))
	}
	if len(clauses) == 0 {
		return "No scene instances reference this prefab yet."
	}
	return "Applies to " + strings.Join(clauses, " and to ") + "."
}

func countScenePrefabInstances(doc *sceneformat.SceneDoc, prefabClean string) int {
	if doc == nil {
		return 0
	}
	n := 0
	var walk func(objs []sceneformat.ObjectDoc)
	walk = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			p := path.Clean(strings.ReplaceAll(objs[i].Prefab, `\`, "/"))
			s := path.Clean(strings.ReplaceAll(objs[i].Scene, `\`, "/"))
			if (objs[i].Prefab != "" && p == prefabClean) || (objs[i].Scene != "" && s == prefabClean) {
				n++
			}
			if len(objs[i].Children) > 0 {
				walk(objs[i].Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Objects)
	}
	return n
}

// --- Effective file reconstruction (honoring per-change reverts) ---

func (e *Editor) allFileChangesReverted(filePath string) bool {
	found := false
	for _, ch := range e.review.changes {
		if ch.File != filePath {
			continue
		}
		found = true
		if !ch.Reverted {
			return false
		}
	}
	return found
}

func (e *Editor) effectiveReviewSceneDoc(fs *reviewFileState) *sceneformat.SceneDoc {
	return e.effectiveReviewSceneDocForRound(fs, e.reviewActiveViewRound())
}

func (e *Editor) effectiveReviewSceneDocForRound(fs *reviewFileState, round int) *sceneformat.SceneDoc {
	if fs == nil {
		return nil
	}
	latest := e.reviewLatestRound()
	isLatest := latest <= 0 || round >= latest
	rs := fs.roundState(round)
	afterDoc := fs.afterDoc
	if rs != nil && rs.afterDoc != nil {
		afterDoc = rs.afterDoc
	}
	if fs.personEdited && isLatest {
		if merged := e.effectiveReviewSceneDocWithConflicts(fs); merged != nil {
			return merged
		}
	}
	if fs.beforeDoc == nil {
		doc := cloneSceneDoc(afterDoc)
		if isLatest {
			e.applyReviewHandEditsToDoc(fs, doc)
		}
		return doc
	}
	doc := cloneSceneDoc(fs.beforeDoc)

	// Index active (non-reverted) changes for this scene at the requested round.
	activeRemoved := make(map[string]bool)
	activeChanged := make(map[string]struct {
		ch  *ReviewChange
		ver ReviewChangeVersion
	})
	var activeAdded []struct {
		ch  *ReviewChange
		ver ReviewChangeVersion
	}

	for _, ch := range e.review.changes {
		if ch.File != fs.path {
			continue
		}
		ver := ch.VersionFor(round)
		switch ch.Kind {
		case ReviewChangeRemoved:
			if !ch.Reverted {
				// If a later redo turned a removal into a kept/repositioned object (afterObj != nil), treat it as changed.
				if ver.afterObj != nil {
					activeChanged[ch.ObjectID] = struct {
						ch  *ReviewChange
						ver ReviewChangeVersion
					}{ch: ch, ver: ver}
				} else {
					activeRemoved[ch.ObjectID] = true
				}
			}
		case ReviewChangeChanged:
			if !ch.Reverted {
				activeChanged[ch.ObjectID] = struct {
					ch  *ReviewChange
					ver ReviewChangeVersion
				}{ch: ch, ver: ver}
			}
		case ReviewChangeAdded:
			if !ch.Reverted {
				activeAdded = append(activeAdded, struct {
					ch  *ReviewChange
					ver ReviewChangeVersion
				}{ch: ch, ver: ver})
			}
		}
	}

	// 1. Apply active removals & in-place changes on doc, detaching objects whose layer or parent moved.
	type movedObj struct {
		ch  *ReviewChange
		ver ReviewChangeVersion
		obj sceneformat.ObjectDoc
	}
	var moved []movedObj
	var applySlice func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc
	applySlice = func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc {
		out := make([]sceneformat.ObjectDoc, 0, len(objs))
		for i := range objs {
			o := objs[i]
			if activeRemoved[o.ID] {
				continue
			}
			entry, isChanged := activeChanged[o.ID]
			if isChanged && entry.ver.afterObj != nil {
				children := o.Children
				o = cloneObjectDoc(*entry.ver.afterObj)
				o.Children = children
			}
			if len(o.Children) > 0 {
				o.Children = applySlice(o.Children)
			}
			if isChanged && (entry.ch.beforeLayer != entry.ver.afterLayer || entry.ch.beforeParentID != entry.ver.parentID) {
				moved = append(moved, movedObj{ch: entry.ch, ver: entry.ver, obj: o})
				continue
			}
			out = append(out, o)
		}
		return out
	}
	for li := range doc.Layers {
		doc.Layers[li].Objects = applySlice(doc.Layers[li].Objects)
	}

	// 2. Apply active additions to their target layers/parents.
	for _, entry := range activeAdded {
		if entry.ver.afterObj == nil {
			continue
		}
		obj := cloneObjectDoc(*entry.ver.afterObj)
		obj.Children = nil
		if entry.ver.parentID != "" {
			if insertChildByID(doc, entry.ver.parentID, obj) {
				continue
			}
		}
		targetLayer := entry.ver.afterLayer
		if targetLayer == "" && len(doc.Layers) > 0 {
			targetLayer = doc.Layers[0].Name
		}
		li := findOrCreateReviewLayer(doc, targetLayer, afterDoc)
		doc.Layers[li].Objects = append(doc.Layers[li].Objects, obj)
	}

	// 3. Re-insert moved/reparented objects at their target parents/layers (after additions so newly added parents exist).
	for _, m := range moved {
		if m.ver.parentID != "" {
			if insertChildByID(doc, m.ver.parentID, m.obj) {
				continue
			}
		}
		targetLayer := m.ver.afterLayer
		if targetLayer == "" && len(doc.Layers) > 0 {
			targetLayer = doc.Layers[0].Name
		}
		li := findOrCreateReviewLayer(doc, targetLayer, afterDoc)
		doc.Layers[li].Objects = append(doc.Layers[li].Objects, m.obj)
	}

	if isLatest {
		e.applyReviewHandEditsToDoc(fs, doc)
	}
	return doc
}

func insertChildByID(doc *sceneformat.SceneDoc, parentID string, child sceneformat.ObjectDoc) bool {
	var walk func(objs []sceneformat.ObjectDoc) bool
	walk = func(objs []sceneformat.ObjectDoc) bool {
		for i := range objs {
			if objs[i].ID == parentID {
				objs[i].Children = append(objs[i].Children, child)
				return true
			}
			if walk(objs[i].Children) {
				return true
			}
		}
		return false
	}
	for i := range doc.Layers {
		if walk(doc.Layers[i].Objects) {
			return true
		}
	}
	return false
}

func findOrCreateReviewLayer(doc *sceneformat.SceneDoc, name string, afterDoc *sceneformat.SceneDoc) int {
	for i := range doc.Layers {
		if doc.Layers[i].Name == name {
			return i
		}
	}
	newLayer := sceneformat.LayerDoc{Name: name, Space: sceneformat.SpaceWorld}
	if afterDoc != nil {
		for i := range afterDoc.Layers {
			if afterDoc.Layers[i].Name == name {
				newLayer = afterDoc.Layers[i]
				newLayer.Objects = nil
				break
			}
		}
	}
	doc.Layers = append(doc.Layers, newLayer)
	return len(doc.Layers) - 1
}

func (e *Editor) effectiveReviewFileBytes(fs *reviewFileState) ([]byte, bool, error) {
	return e.effectiveReviewFileBytesForRound(fs, e.reviewLatestRound())
}

func (e *Editor) effectiveReviewFileBytesForRound(fs *reviewFileState, round int) ([]byte, bool, error) {
	if fs == nil {
		return nil, false, nil
	}
	if e.allFileChangesReverted(fs.path) && !fs.personEdited {
		return append([]byte(nil), fs.beforeYAML...), fs.existedBefore, nil
	}
	latest := e.reviewLatestRound()
	isLatest := latest <= 0 || round >= latest
	rs := fs.roundState(round)
	afterYAML := fs.afterYAML
	if rs != nil {
		afterYAML = rs.afterYAML
	}
	// If no changes in this file are reverted, hand-edited, or merged with person edits, preserve exact AfterYAML for the requested round!
	anyReverted := false
	anyHandEdited := false
	for _, ch := range e.review.changes {
		if ch.File != fs.path {
			continue
		}
		if ch.Reverted {
			anyReverted = true
		}
		if ch.HandEdited {
			anyHandEdited = true
		}
	}
	if !anyReverted && (!isLatest || (!anyHandEdited && !fs.personEdited)) {
		return append([]byte(nil), afterYAML...), len(afterYAML) > 0, nil
	}

	switch fs.kind {
	case ReviewFileScene:
		doc := e.effectiveReviewSceneDocForRound(fs, round)
		b, err := sceneformat.Encode(doc)
		if err != nil {
			return nil, false, err
		}
		return b, true, nil
	case ReviewFileData:
		doc := e.effectiveReviewDataTableDocForRound(fs, round)
		if doc == nil {
			return append([]byte(nil), fs.beforeYAML...), fs.existedBefore, nil
		}
		b, err := doc.Marshal()
		if err != nil {
			return nil, false, err
		}
		return b, true, nil
	case ReviewFilePrefab:
		b, err := e.effectiveReviewPrefabBytesForRound(fs, round)
		if err != nil {
			return nil, false, err
		}
		return b, len(b) > 0, nil
	default:
		return append([]byte(nil), afterYAML...), len(afterYAML) > 0, nil
	}
}

func (e *Editor) effectiveReviewDataTableDoc(fs *reviewFileState) *datatable.Document {
	return e.effectiveReviewDataTableDocForRound(fs, e.reviewActiveViewRound())
}

func (e *Editor) effectiveReviewDataTableDocForRound(fs *reviewFileState, round int) *datatable.Document {
	if fs == nil {
		return nil
	}
	rs := fs.roundState(round)
	afterTable := fs.afterTable
	if rs != nil && rs.afterTable != nil {
		afterTable = rs.afterTable
	}
	if fs.beforeTable == nil && afterTable == nil {
		return nil
	}
	base := fs.beforeTable.Clone()
	if base == nil {
		base = datatable.NewDocument()
	}
	afterByID := make(map[string]datatable.Row)
	if afterTable != nil {
		for _, r := range afterTable.Rows() {
			afterByID[dataRowID(r)] = r
		}
	}
	activeChanged := make(map[string]bool)
	activeRemoved := make(map[string]bool)
	var activeAdded []string
	for _, ch := range e.review.changes {
		if ch.File != fs.path || ch.Reverted {
			continue
		}
		switch ch.Kind {
		case ReviewChangeAdded:
			activeAdded = append(activeAdded, ch.RowID)
		case ReviewChangeChanged:
			activeChanged[ch.RowID] = true
		case ReviewChangeRemoved:
			activeRemoved[ch.RowID] = true
		}
	}
	// Apply removals (back-to-front to keep indices stable) and cell changes in place.
	for i := base.RowCount() - 1; i >= 0; i-- {
		r := base.Row(i)
		id := dataRowID(r)
		if activeRemoved[id] {
			_ = base.DeleteRow(i)
			continue
		}
		if activeChanged[id] {
			if ar, ok := afterByID[id]; ok {
				for _, c := range ar.Cells {
					_ = base.SetCell(i, c.Key, c.Value.Clone())
				}
				for _, k := range r.Keys() {
					if _, exists := ar.Get(k); !exists {
						_ = base.DeleteCell(i, k)
					}
				}
			}
		}
	}
	for _, id := range activeAdded {
		if ar, ok := afterByID[id]; ok {
			_, _ = base.AppendRow(ar.Clone().Cells...)
		}
	}
	return base
}

func (e *Editor) effectiveReviewPrefabBytes(fs *reviewFileState) ([]byte, error) {
	return e.effectiveReviewPrefabBytesForRound(fs, e.reviewActiveViewRound())
}

func (e *Editor) effectiveReviewPrefabBytesForRound(fs *reviewFileState, round int) ([]byte, error) {
	rs := fs.roundState(round)
	afterYAML := fs.afterYAML
	if rs != nil {
		afterYAML = rs.afterYAML
	}
	if len(bytes.TrimSpace(fs.beforeYAML)) == 0 {
		return append([]byte(nil), afterYAML...), nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(fs.beforeYAML, &root); err != nil {
		return nil, err
	}
	for _, ch := range e.review.changes {
		if ch.File != fs.path || ch.Reverted {
			continue
		}
		ver := ch.VersionFor(round)
		switch ch.Kind {
		case ReviewChangeChanged, ReviewChangeAdded:
			if ver.afterYAMLNode != nil {
				setYAMLNodeAtPath(&root, strings.Split(ch.Field, "."), ver.afterYAMLNode)
			}
		case ReviewChangeRemoved:
			deleteYAMLNodeAtPath(&root, strings.Split(ch.Field, "."))
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func setYAMLNodeAtPath(root *yaml.Node, segs []string, val *yaml.Node) {
	if root == nil || len(segs) == 0 || val == nil {
		return
	}
	cur := root
	if cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	for idx, key := range segs {
		if cur.Kind != yaml.MappingNode {
			return
		}
		found := -1
		for i := 0; i+1 < len(cur.Content); i += 2 {
			if cur.Content[i].Value == key {
				found = i + 1
				break
			}
		}
		if idx == len(segs)-1 {
			cp := *val
			if found >= 0 {
				cur.Content[found] = &cp
			} else {
				cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &cp)
			}
			return
		}
		if found >= 0 {
			cur = cur.Content[found]
		} else {
			child := &yaml.Node{Kind: yaml.MappingNode}
			cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, child)
			cur = child
		}
	}
}

func deleteYAMLNodeAtPath(root *yaml.Node, segs []string) {
	if root == nil || len(segs) == 0 {
		return
	}
	cur := root
	if cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	for idx, key := range segs {
		if cur.Kind != yaml.MappingNode {
			return
		}
		found := -1
		for i := 0; i+1 < len(cur.Content); i += 2 {
			if cur.Content[i].Value == key {
				found = i
				break
			}
		}
		if found < 0 {
			return
		}
		if idx == len(segs)-1 {
			cur.Content = append(cur.Content[:found], cur.Content[found+2:]...)
			return
		}
		cur = cur.Content[found+1]
	}
}

// --- Review scene building, framing, overlays, and data table previews ---

func (e *Editor) reviewSceneBuilt(fs *reviewFileState, before bool) *sceneformat.BuiltScene {
	return e.reviewSceneBuiltForRound(fs, e.reviewActiveViewRound(), before)
}

func (e *Editor) reviewSceneBuiltForRound(fs *reviewFileState, round int, before bool) *sceneformat.BuiltScene {
	if e == nil || fs == nil || fs.kind != ReviewFileScene {
		return nil
	}
	if before {
		if fs.beforeBuilt == nil && fs.beforeDoc != nil {
			if b, err := sceneformat.Build(fs.beforeDoc, e.Resolver); err == nil {
				b.DrawAllTiles = true
				fs.beforeBuilt = b
			}
		}
		return fs.beforeBuilt
	}
	if rs := fs.roundState(round); rs != nil {
		if rs.builtDirty || rs.built == nil {
			doc := e.effectiveReviewSceneDocForRound(fs, round)
			if doc != nil {
				if b, err := sceneformat.Build(doc, e.Resolver); err == nil {
					b.DrawAllTiles = true
					rs.built = b
					rs.builtDirty = false
				}
			}
		}
		return rs.built
	}
	if fs.builtDirty || fs.currentBuilt == nil {
		doc := e.effectiveReviewSceneDocForRound(fs, round)
		if doc != nil {
			if b, err := sceneformat.Build(doc, e.Resolver); err == nil {
				b.DrawAllTiles = true
				fs.currentBuilt = b
				fs.builtDirty = false
			}
		}
	}
	return fs.currentBuilt
}

// ReviewDataTablePreviewFor returns the small-table preview model for a focused data table file.
func (e *Editor) ReviewDataTablePreviewFor(filePath string) *ReviewDataTablePreview {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return nil
	}
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind != ReviewFileData {
		return nil
	}
	round := e.reviewActiveViewRound()
	eff := e.effectiveReviewDataTableDocForRound(fs, round)
	if eff == nil {
		return nil
	}
	afterTable := fs.afterTable
	if rs := fs.roundState(round); rs != nil && rs.afterTable != nil {
		afterTable = rs.afterTable
	}
	colSet := make(map[string]bool)
	var cols []string
	addCols := func(doc *datatable.Document) {
		if doc == nil {
			return
		}
		for _, k := range doc.Columns() {
			if !colSet[k] {
				colSet[k] = true
				cols = append(cols, k)
			}
		}
	}
	addCols(fs.beforeTable)
	addCols(afterTable)
	// Put "id" first.
	if idx := slices.Index(cols, "id"); idx > 0 {
		cols = append([]string{"id"}, append(cols[:idx], cols[idx+1:]...)...)
	}

	changeByRow := make(map[string]*ReviewChange)
	changedActive := 0
	for _, ch := range e.review.changes {
		if ch.File == filePath {
			changeByRow[ch.RowID] = ch
			if !ch.Reverted {
				changedActive++
			}
		}
	}

	effRows := eff.Rows()
	rows := make([]ReviewDataRow, 0, len(effRows))
	for _, r := range effRows {
		id := dataRowID(r)
		vals := make(map[string]string, len(cols))
		for _, c := range cols {
			if v, ok := r.Get(c); ok {
				vals[c] = dataValueDisplay(v)
			}
		}
		row := ReviewDataRow{ID: id, Values: vals}
		if ch, ok := changeByRow[id]; ok {
			ver := ch.VersionFor(round)
			row.ChangeID = ch.ID
			row.Focused = ch.ID == e.review.focusedID
			if !ch.Reverted {
				switch ch.Kind {
				case ReviewChangeAdded:
					row.Added = true
				case ReviewChangeChanged:
					row.Changed = true
					row.CellDiffs = make(map[string]ReviewDataCellDiff, len(ver.cellDiffs))
					for _, d := range ver.cellDiffs {
						row.CellDiffs[d.Column] = d
					}
				}
			}
		}
		rows = append(rows, row)
	}
	// Include active removed rows as ghosted rows so the reviewer can see what was removed.
	if fs.beforeTable != nil {
		for _, r := range fs.beforeTable.Rows() {
			id := dataRowID(r)
			ch, ok := changeByRow[id]
			if !ok || ch.Kind != ReviewChangeRemoved || ch.Reverted {
				continue
			}
			vals := make(map[string]string, len(cols))
			for _, c := range cols {
				if v, ok := r.Get(c); ok {
					vals[c] = dataValueDisplay(v)
				}
			}
			rows = append(rows, ReviewDataRow{
				ID:       id,
				ChangeID: ch.ID,
				Values:   vals,
				Removed:  true,
				Focused:  ch.ID == e.review.focusedID,
			})
		}
	}

	return &ReviewDataTablePreview{
		File:         filePath,
		Columns:      cols,
		TotalRows:    len(rows),
		ChangedCount: changedActive,
		Rows:         rows,
	}
}

func (e *Editor) frameFocusedReviewChange() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	ch := e.review.changesByID[e.review.focusedID]
	if ch == nil || ch.FileKind != ReviewFileScene {
		return
	}
	fs := e.review.filesByPath[ch.File]
	if fs == nil {
		return
	}
	round := e.reviewActiveViewRound()
	ver := ch.VersionFor(round)
	obj := ver.afterObj
	if obj == nil || ch.Reverted || (ch.Kind == ReviewChangeRemoved && ver.afterObj == nil) {
		if ch.beforeObj != nil {
			obj = ch.beforeObj
		}
	}
	if obj == nil {
		return
	}
	built := e.reviewSceneBuiltForRound(fs, round, ch.Kind == ReviewChangeRemoved && ver.afterObj == nil && !ch.Reverted)
	cx, cy := obj.X, obj.Y
	if built != nil {
		if bo := built.Object(ch.ObjectID); bo != nil {
			rx, ry, rw, rh := objectOutline(bo)
			p := bo.World.TransformPoint(math2d.Vec2{X: rx + rw*0.5, Y: ry + rh*0.5})
			cx, cy = p.X, p.Y
		}
	}
	zoom := float32(1.25)
	if fs.sceneView.Zoom > 0 {
		zoom = fs.sceneView.Zoom
	}
	fs.sceneView = View2D{X: cx, Y: cy, Zoom: clampView2DZoom(zoom)}
	fs.sceneFramed = true
}

// ReviewViewportView2D returns the Review center viewport's current 2D camera view for the focused scene.
func (e *Editor) ReviewViewportView2D() View2D {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return View2D{Zoom: 1}
	}
	ch := e.review.changesByID[e.review.focusedID]
	if ch == nil {
		return View2D{Zoom: 1}
	}
	fs := e.review.filesByPath[ch.File]
	if fs == nil || !fs.sceneFramed {
		return View2D{Zoom: 1}
	}
	return fs.sceneView
}

// ReviewOverlays returns the inspectable list of change overlays for the focused review scene
// (or for the editor's open scene when the review is paused/pending in the editor).
func (e *Editor) ReviewOverlays() []ReviewSceneOverlay {
	if e == nil || !e.HasPendingReview() {
		return nil
	}
	if e.review.open && e.review.peekBefore {
		return nil
	}
	var targetFile string
	if e.review.open {
		if ch := e.review.changesByID[e.review.focusedID]; ch != nil && ch.FileKind == ReviewFileScene {
			targetFile = ch.File
		}
	} else {
		targetFile = e.openSceneProjectRelPath()
	}
	if targetFile == "" {
		return nil
	}
	return e.reviewOverlaysForFile(targetFile)
}

func (e *Editor) reviewOverlaysForFile(filePath string) []ReviewSceneOverlay {
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind != ReviewFileScene {
		return nil
	}
	round := e.reviewActiveViewRound()
	effBuilt := e.reviewSceneBuiltForRound(fs, round, false)
	beforeBuilt := e.reviewSceneBuiltForRound(fs, round, true)
	var out []ReviewSceneOverlay
	for _, ch := range e.review.changes {
		if ch.File != filePath || ch.Reverted {
			continue
		}
		if !e.reviewOverlayKindVisible(ch.Kind) {
			continue
		}
		ver := ch.VersionFor(round)
		focused := e.review.open && ch.ID == e.review.focusedID
		var bo *sceneformat.BuiltObject
		if ch.Kind == ReviewChangeRemoved && ver.afterObj == nil {
			if beforeBuilt != nil {
				bo = beforeBuilt.Object(ch.ObjectID)
			}
		} else if effBuilt != nil {
			bo = effBuilt.Object(ch.ObjectID)
		}
		var x, y, w, h float32
		if bo != nil {
			rx, ry, rw, rh := objectOutline(bo)
			p0 := bo.World.TransformPoint(math2d.Vec2{X: rx, Y: ry})
			x, y, w, h = p0.X, p0.Y, rw, rh
		} else {
			var obj *sceneformat.ObjectDoc
			if !e.ReviewViewingReadOnlyRound() && ch.handEditObj != nil {
				obj = ch.handEditObj
			} else {
				obj = ver.afterObj
			}
			if obj == nil {
				obj = ch.beforeObj
			}
			if obj != nil {
				x, y, w, h = obj.X-defaultMarkerHalfSize, obj.Y-defaultMarkerHalfSize, defaultMarkerHalfSize*2, defaultMarkerHalfSize*2
			}
		}
		sw := float32(1.5)
		if focused {
			sw = 2.5
		}
		dashed := ch.Kind != ReviewChangeRemoved
		pill := ch.Title
		switch ch.Kind {
		case ReviewChangeAdded:
			pill = "+ " + ch.Title
		case ReviewChangeChanged:
			if ver.Detail != "" {
				pill = ver.Detail
			}
		case ReviewChangeRemoved:
			pill = ch.Title + " removed"
		}
		out = append(out, ReviewSceneOverlay{
			ChangeID:    ch.ID,
			File:        filePath,
			ObjectID:    ch.ObjectID,
			Kind:        ch.Kind,
			ColorRole:   string(ch.Kind), // Always own kind color ("added"/"changed"/"removed"), NEVER "accent".
			Focused:     focused,
			StrokeWidth: sw,
			Dashed:      dashed,
			PillText:    pill,
			X:           x,
			Y:           y,
			W:           w,
			H:           h,
		})
	}
	return out
}

func (e *Editor) reviewOverlayKindVisible(kind ReviewChangeKind) bool {
	switch kind {
	case ReviewChangeAdded:
		return e.review.legend.Added
	case ReviewChangeChanged:
		return e.review.legend.Changed
	case ReviewChangeRemoved:
		return e.review.legend.Removed
	default:
		return true
	}
}

// ToggleReviewOverlayLegend toggles visibility of one overlay kind ("added", "changed", "removed", "selected") in the Review viewport legend.
func (e *Editor) ToggleReviewOverlayLegend(kind string) {
	if e == nil {
		return
	}
	switch kind {
	case "added":
		e.review.legend.Added = !e.review.legend.Added
	case "changed":
		e.review.legend.Changed = !e.review.legend.Changed
	case "removed":
		e.review.legend.Removed = !e.review.legend.Removed
	case "selected":
		e.review.legend.Selected = !e.review.legend.Selected
	default:
		return
	}
	e.bumpReviewUIRev()
	e.notify()
}

// --- Filtering, Grouping, Focus, Navigation, Revert/Restore ---

// ReviewChipCount returns the count for filter chip f.
func (e *Editor) ReviewChipCount(f ReviewFilter) int {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return 0
	}
	viewingLatest := !e.ReviewViewingReadOnlyRound()
	n := 0
	for _, ch := range e.review.changes {
		if reviewMatchesFilter(ch, f, viewingLatest) {
			n++
		}
	}
	return n
}

func reviewMatchesFilter(ch *ReviewChange, f ReviewFilter, viewingLatest bool) bool {
	if ch == nil {
		return false
	}
	switch f {
	case ReviewFilterAll, "":
		return true
	case ReviewFilterAdded:
		return ch.Kind == ReviewChangeAdded
	case ReviewFilterChanged:
		return ch.Kind == ReviewChangeChanged
	case ReviewFilterRemoved:
		return ch.Kind == ReviewChangeRemoved
	case ReviewFilterUnseen:
		return !ch.Seen
	case ReviewFilterNotes:
		if strings.TrimSpace(ch.Note) != "" || strings.TrimSpace(ch.SentNote) != "" {
			return true
		}
		return viewingLatest && ch.RedoneInRound > 1 && strings.TrimSpace(ch.RedoneNote) != ""
	default:
		return true
	}
}

func reviewMatchesSearch(ch *ReviewChange, q string) bool {
	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" {
		return true
	}
	hay := strings.ToLower(ch.Title + " " + ch.Detail + " " + ch.Field + " " + ch.File + " " + ch.ObjectID + " " + ch.RowID)
	for _, word := range strings.Fields(q) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}

// ReviewVisibleRows returns the flattened visible rows (file headers, kind headers, change cards)
// honoring the active filter chip, search query, and collapsed file/kind groups.
func (e *Editor) ReviewVisibleRows() []ReviewVisibleRow {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return nil
	}
	var rows []ReviewVisibleRow
	for _, fs := range e.review.files {
		var allFile, matched []*ReviewChange
		addCnt, chgCnt, remCnt := 0, 0, 0
		revertableCnt, revertedCnt := 0, 0
		for _, ch := range e.review.changes {
			if ch.File != fs.path {
				continue
			}
			allFile = append(allFile, ch)
			switch ch.Kind {
			case ReviewChangeAdded:
				addCnt++
			case ReviewChangeChanged:
				chgCnt++
			case ReviewChangeRemoved:
				remCnt++
			}
			if !ch.GoRequiresConfirm {
				revertableCnt++
				if ch.Reverted {
					revertedCnt++
				}
			}
			if reviewMatchesFilter(ch, e.review.filter, !e.ReviewViewingReadOnlyRound()) && reviewMatchesSearch(ch, e.review.search) {
				matched = append(matched, ch)
			}
		}
		if len(matched) == 0 {
			continue
		}
		fileCollapsed := e.review.collapsedFiles[fs.path]
		allReverted := revertableCnt > 0 && revertedCnt == revertableCnt
		rows = append(rows, ReviewVisibleRow{
			Key:            reviewFileRowKey(fs.path),
			RowKind:        ReviewRowFile,
			File:           fs.path,
			FileName:       path.Base(fs.path),
			FileKind:       fs.kind,
			Count:          len(allFile),
			Added:          addCnt,
			Changed:        chgCnt,
			Removed:        remCnt,
			Collapsed:      fileCollapsed,
			AllReverted:    allReverted,
			CanRevertGroup: fs.kind != ReviewFileGo && revertableCnt > 0,
		})
		if fileCollapsed {
			continue
		}
		for _, kind := range []ReviewChangeKind{ReviewChangeAdded, ReviewChangeChanged, ReviewChangeRemoved} {
			var ks []*ReviewChange
			kReverted := 0
			for _, ch := range matched {
				if ch.Kind == kind {
					ks = append(ks, ch)
					if ch.Reverted {
						kReverted++
					}
				}
			}
			if len(ks) == 0 {
				continue
			}
			kindKey := fs.path + ":" + string(kind)
			kindCollapsed := e.review.collapsedKinds[kindKey]
			if len(allFile) > 1 {
				rows = append(rows, ReviewVisibleRow{
					Key:            reviewKindRowKey(fs.path, kind),
					RowKind:        ReviewRowKindHeader,
					File:           fs.path,
					FileKind:       fs.kind,
					Kind:           kind,
					Count:          len(ks),
					Collapsed:      kindCollapsed,
					AllReverted:    kReverted == len(ks),
					CanRevertGroup: fs.kind != ReviewFileGo,
				})
				if kindCollapsed {
					continue
				}
			}
			for _, ch := range ks {
				rows = append(rows, ReviewVisibleRow{
					Key:      reviewCardRowKey(ch.ID),
					RowKind:  ReviewRowCard,
					File:     fs.path,
					FileKind: fs.kind,
					Kind:     ch.Kind,
					ChangeID: ch.ID,
				})
			}
		}
	}
	return rows
}

// SetReviewFilter switches the active left-list filter chip (All, Added, Changed, Removed, Unseen, Notes).
func (e *Editor) SetReviewFilter(f ReviewFilter) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	if f == "" {
		f = ReviewFilterAll
	}
	if e.review.filter == f {
		return
	}
	e.review.filter = f
	e.bumpReviewUIRev()
	e.notify()
}

// SetReviewSearch updates the left-list filter query (matching change name, field, detail, or file path).
func (e *Editor) SetReviewSearch(q string) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	if e.review.search == q {
		return
	}
	e.review.search = q
	e.bumpReviewUIRev()
	e.notify()
}

// FocusReviewChange focuses changeID (full change ID or object ID), expands its file/kind groups if collapsed,
// marks it seen, and frames it.
func (e *Editor) FocusReviewChange(changeID string) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return false
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return false
	}
	// Close any valid inline YAML edit on another change when moving away; keep broken edits flagged.
	for _, other := range e.review.changes {
		if other.ID != ch.ID && other.EditingYAML && other.YAMLError == nil {
			other.EditingYAML = false
		}
	}
	e.review.focusedID = ch.ID
	e.review.focusedRowKey = reviewCardRowKey(ch.ID)
	ch.Seen = true
	e.review.collapsedFiles[ch.File] = false
	e.review.collapsedKinds[ch.File+":"+string(ch.Kind)] = false
	e.review.peekBefore = false
	e.review.yamlEditSessionRev++
	e.frameFocusedReviewChange()
	e.bumpReviewUIRev()
	e.notify()
	return true
}

// FocusReviewFileRow focuses a file header row in the left list so pressing X reverts/restores that file group.
func (e *Editor) FocusReviewFileRow(filePath string) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return false
	}
	if _, ok := e.review.filesByPath[filePath]; !ok {
		return false
	}
	e.review.focusedRowKey = reviewFileRowKey(filePath)
	// Also keep focusedID on the first change in this file if current focus is elsewhere.
	if cur := e.review.changesByID[e.review.focusedID]; cur == nil || cur.File != filePath {
		for _, ch := range e.review.changes {
			if ch.File == filePath {
				e.review.focusedID = ch.ID
				ch.Seen = true
				e.frameFocusedReviewChange()
				break
			}
		}
	}
	e.bumpReviewUIRev()
	e.notify()
	return true
}

// FocusReviewKindRow focuses a kind subheader row in the left list so pressing X reverts/restores that kind group.
func (e *Editor) FocusReviewKindRow(filePath string, kind ReviewChangeKind) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return false
	}
	if _, ok := e.review.filesByPath[filePath]; !ok {
		return false
	}
	e.review.focusedRowKey = reviewKindRowKey(filePath, kind)
	e.bumpReviewUIRev()
	e.notify()
	return true
}

// StepReviewChange moves focus to the next (+1, J) or previous (-1, K) visible change card,
// marks it seen, and frames it in the viewport.
// When filtering by Unseen, stepping advances cleanly through the remaining unseen items even after
// the previously focused item becomes seen.
func (e *Editor) StepReviewChange(delta int) string {
	if e == nil || e.review.outcome != ReviewOutcomePending || delta == 0 {
		return ""
	}
	// Build the ordered list of matching change cards (expanding folded files/kinds would show them,
	// and visible rows gives the exact current presentation order).
	rows := e.ReviewVisibleRows()
	var cardIDs []string
	for _, r := range rows {
		if r.RowKind == ReviewRowCard {
			cardIDs = append(cardIDs, r.ChangeID)
		}
	}
	// If all visible files are folded, fall back to matching changes across all files.
	if len(cardIDs) == 0 {
		viewingLatest := !e.ReviewViewingReadOnlyRound()
		for _, ch := range e.review.changes {
			if reviewMatchesFilter(ch, e.review.filter, viewingLatest) && reviewMatchesSearch(ch, e.review.search) {
				cardIDs = append(cardIDs, ch.ID)
			}
		}
	}
	if len(cardIDs) == 0 {
		return ""
	}

	idx := slices.Index(cardIDs, e.review.focusedID)
	var targetID string
	if idx >= 0 {
		next := idx + delta
		if next < 0 {
			next = 0
		}
		if next >= len(cardIDs) {
			next = len(cardIDs) - 1
		}
		targetID = cardIDs[next]
	} else {
		// Focused change is no longer in the filtered list (for example, after it became seen under Unseen filter).
		// Find the nearest matching card after (delta > 0) or before (delta < 0) its position in the full list.
		fullIdx := -1
		for i, ch := range e.review.changes {
			if ch.ID == e.review.focusedID {
				fullIdx = i
				break
			}
		}
		if fullIdx < 0 {
			targetID = cardIDs[0]
		} else if delta > 0 {
			for i := fullIdx + 1; i < len(e.review.changes); i++ {
				if slices.Contains(cardIDs, e.review.changes[i].ID) {
					targetID = e.review.changes[i].ID
					break
				}
			}
			if targetID == "" {
				targetID = cardIDs[0]
			}
		} else {
			for i := fullIdx - 1; i >= 0; i-- {
				if slices.Contains(cardIDs, e.review.changes[i].ID) {
					targetID = e.review.changes[i].ID
					break
				}
			}
			if targetID == "" {
				targetID = cardIDs[len(cardIDs)-1]
			}
		}
	}
	if targetID != "" {
		e.FocusReviewChange(targetID)
	}
	return targetID
}

// StepReviewRow moves keyboard row focus up (-1) or down (+1) across all visible rows (file headers, kind headers, and change cards).
func (e *Editor) StepReviewRow(delta int) string {
	if e == nil || e.review.outcome != ReviewOutcomePending || delta == 0 {
		return ""
	}
	rows := e.ReviewVisibleRows()
	if len(rows) == 0 {
		return ""
	}
	idx := -1
	for i, r := range rows {
		if r.Key == e.review.focusedRowKey {
			idx = i
			break
		}
	}
	var next int
	if idx < 0 {
		if delta > 0 {
			next = 0
		} else {
			next = len(rows) - 1
		}
	} else {
		next = max(0, min(len(rows)-1, idx+delta))
	}
	row := rows[next]
	switch row.RowKind {
	case ReviewRowFile:
		e.FocusReviewFileRow(row.File)
	case ReviewRowKindHeader:
		e.FocusReviewKindRow(row.File, row.Kind)
	case ReviewRowCard:
		e.FocusReviewChange(row.ChangeID)
	}
	return row.Key
}

// ToggleReviewFileCollapsed folds or unfolds filePath in the left list.
func (e *Editor) ToggleReviewFileCollapsed(filePath string) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	if _, ok := e.review.filesByPath[filePath]; !ok {
		return
	}
	e.review.collapsedFiles[filePath] = !e.review.collapsedFiles[filePath]
	e.bumpReviewUIRev()
	e.notify()
}

// ToggleReviewKindCollapsed folds or unfolds kind within filePath in the left list.
func (e *Editor) ToggleReviewKindCollapsed(filePath string, kind ReviewChangeKind) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	k := filePath + ":" + string(kind)
	e.review.collapsedKinds[k] = !e.review.collapsedKinds[k]
	e.bumpReviewUIRev()
	e.notify()
}

// ToggleReviewChangeReverted toggles reverted state on one change card.
func (e *Editor) ToggleReviewChangeReverted(changeID string) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.ReviewViewingReadOnlyRound() {
		return false
	}
	ch := e.findReviewChange(changeID)
	if ch == nil || ch.GoRequiresConfirm {
		return false
	}
	ch.Reverted = !ch.Reverted
	if ch.Reverted {
		ch.EditingYAML = false
		ch.YAMLError = nil
		e.review.yamlEditSessionRev++
	}
	if fs := e.review.filesByPath[ch.File]; fs != nil {
		fs.markAllRoundsDirty()
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
	e.refreshPrefabImpacts()
	e.bumpReviewUIRev()
	e.notify()
	return ch.Reverted
}

// ToggleReviewFileReverted reverts all revertable changes in filePath (or restores them all if already all reverted).
func (e *Editor) ToggleReviewFileReverted(filePath string) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.ReviewViewingReadOnlyRound() {
		return false
	}
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind == ReviewFileGo {
		return false
	}
	allOff := true
	any := false
	for _, ch := range e.review.changes {
		if ch.File == filePath && !ch.GoRequiresConfirm {
			any = true
			if !ch.Reverted {
				allOff = false
			}
		}
	}
	if !any {
		return false
	}
	target := !allOff
	for _, ch := range e.review.changes {
		if ch.File == filePath && !ch.GoRequiresConfirm {
			ch.Reverted = target
			if target {
				ch.EditingYAML = false
				ch.YAMLError = nil
			}
		}
	}
	if target {
		e.review.yamlEditSessionRev++
	}
	fs.markAllRoundsDirty()
	e.refreshPrefabImpacts()
	e.bumpReviewUIRev()
	e.notify()
	return target
}

// ToggleReviewKindReverted reverts (or restores) all changes of kind within filePath.
func (e *Editor) ToggleReviewKindReverted(filePath string, kind ReviewChangeKind) bool {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.ReviewViewingReadOnlyRound() {
		return false
	}
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind == ReviewFileGo {
		return false
	}
	allOff := true
	any := false
	for _, ch := range e.review.changes {
		if ch.File == filePath && ch.Kind == kind && !ch.GoRequiresConfirm {
			any = true
			if !ch.Reverted {
				allOff = false
			}
		}
	}
	if !any {
		return false
	}
	target := !allOff
	for _, ch := range e.review.changes {
		if ch.File == filePath && ch.Kind == kind && !ch.GoRequiresConfirm {
			ch.Reverted = target
			if target {
				ch.EditingYAML = false
				ch.YAMLError = nil
			}
		}
	}
	if target {
		e.review.yamlEditSessionRev++
	}
	fs.markAllRoundsDirty()
	e.refreshPrefabImpacts()
	e.bumpReviewUIRev()
	e.notify()
	return target
}

// ToggleFocusedReviewRevert implements the X shortcut in Review scope:
// reverts or restores the focused file group, kind group, or change card.
func (e *Editor) ToggleFocusedReviewRevert() bool {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.ReviewViewingReadOnlyRound() {
		return false
	}
	key := e.review.focusedRowKey
	switch {
	case strings.HasPrefix(key, "file:"):
		return e.ToggleReviewFileReverted(strings.TrimPrefix(key, "file:"))
	case strings.HasPrefix(key, "kind:"):
		rest := strings.TrimPrefix(key, "kind:")
		if idx := strings.LastIndexByte(rest, ':'); idx > 0 {
			return e.ToggleReviewKindReverted(rest[:idx], ReviewChangeKind(rest[idx+1:]))
		}
	}
	return e.ToggleReviewChangeReverted(e.review.focusedID)
}

// SetReviewPeekBefore switches Before-request peek mode (holding B).
func (e *Editor) SetReviewPeekBefore(peek bool) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	if e.review.peekBefore == peek {
		return
	}
	e.review.peekBefore = peek
	e.bumpReviewUIRev()
	e.notify()
}

// ToggleReviewPeekBefore flips Before-request peek mode.
func (e *Editor) ToggleReviewPeekBefore() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	e.SetReviewPeekBefore(!e.review.peekBefore)
}

// ReviewPeekingBefore reports whether the Review viewport is currently peeking at the pre-request scene.
func (e *Editor) ReviewPeekingBefore() bool {
	return e != nil && e.review.outcome == ReviewOutcomePending && e.review.peekBefore
}

// SetReviewChangeNote sets or clears an unsent annotation note on changeID.
func (e *Editor) SetReviewChangeNote(changeID, note string) {
	if strings.TrimSpace(note) == "" {
		_ = e.RemoveReviewNote(changeID)
		return
	}
	_ = e.SetReviewNote(changeID, note)
}

// SetReviewChangeBlocker sets or clears a blocker reason on changeID (plug-in hook for Conflicts / YAML errors).
func (e *Editor) SetReviewChangeBlocker(changeID, reason string) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	ch := e.review.changesByID[changeID]
	if ch == nil {
		return
	}
	ch.Blocker = strings.TrimSpace(reason)
	e.bumpReviewUIRev()
	e.notify()
}

// ReviewBlockers returns all active blockers on non-reverted changes in priority order:
// active YAML parse errors first ("Fix YAML on line N"), then unresolved merge conflicts
// ("Choose how <title> merges"), then any custom blockers.
func (e *Editor) ReviewBlockers() []ReviewBlocker {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return nil
	}
	var out []ReviewBlocker
	for _, ch := range e.review.changes {
		if ch.Reverted {
			continue
		}
		if ch.YAMLError != nil {
			out = append(out, ReviewBlocker{
				Kind:     "yaml_error",
				ChangeID: ch.ID,
				ObjectID: ch.ObjectID,
				Line:     ch.YAMLError.Line,
				Reason:   fmt.Sprintf("Fix YAML on line %d", ch.YAMLError.Line),
			})
		}
	}
	for _, ch := range e.review.changes {
		if ch.Reverted {
			continue
		}
		if ch.HasUnresolvedConflict() {
			out = append(out, ReviewBlocker{
				Kind:     "conflict",
				ChangeID: ch.ID,
				ObjectID: ch.ObjectID,
				Reason:   fmt.Sprintf("Choose how %s merges", ch.Title),
			})
		}
	}
	for _, ch := range e.review.changes {
		if ch.Reverted {
			continue
		}
		if ch.Blocker != "" {
			out = append(out, ReviewBlocker{
				Kind:     "custom",
				ChangeID: ch.ID,
				ObjectID: ch.ObjectID,
				Reason:   ch.Blocker,
			})
		}
	}
	return out
}

// JumpToReviewBlocker focuses the first active blocker change and returns its ID (or "" if none).
func (e *Editor) JumpToReviewBlocker() string {
	blockers := e.ReviewBlockers()
	if len(blockers) == 0 {
		return ""
	}
	if e.review.filter != ReviewFilterAll {
		e.review.filter = ReviewFilterAll
	}
	e.review.search = ""
	if blockers[0].Kind == "yaml_error" {
		if ch := e.review.changesByID[blockers[0].ChangeID]; ch != nil {
			ch.EditingYAML = true
		}
	}
	e.FocusReviewChange(blockers[0].ChangeID)
	return blockers[0].ChangeID
}

// PauseReview (Esc / Back to editor) leaves the full-window Review screen while keeping the turn pending,
// preserving the exact focused change, seen state, filter, and reverted set.
func (e *Editor) PauseReview() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	e.review.open = false
	e.review.revertConfirm = false
	e.review.peekBefore = false
	e.review.composingID = ""
	e.review.noteDraft = ""
	e.showAgentTabForReview()
	e.bumpReviewUIRev()
	e.notify()
}

// ResumeReview re-opens the full-window Review screen at the exact focused change and seen state left by PauseReview.
func (e *Editor) ResumeReview() error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	e.review.open = true
	e.review.revertConfirm = false
	e.frameFocusedReviewChange()
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// OpenReviewRevertAllDialog opens the Revert All confirmation dialog (⌘⌫).
func (e *Editor) OpenReviewRevertAllDialog() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	e.review.revertConfirm = true
	e.bumpReviewUIRev()
	e.notify()
}

// CancelReviewRevertAllDialog closes the Revert All confirmation dialog without reverting.
func (e *Editor) CancelReviewRevertAllDialog() {
	if e == nil || !e.review.revertConfirm {
		return
	}
	e.review.revertConfirm = false
	e.bumpReviewUIRev()
	e.notify()
}

// ReviewRevertAllDialogOpen reports whether the Revert All confirmation dialog is currently shown.
func (e *Editor) ReviewRevertAllDialogOpen() bool {
	return e != nil && e.review.outcome == ReviewOutcomePending && e.review.revertConfirm
}

// --- Keep & Revert All as one atomic History undo step ---

type reviewFileWrite struct {
	relPath       string
	absPath       string
	existedBefore bool
	beforeBytes   []byte
	existedAfter  bool
	afterBytes    []byte
}

func (e *Editor) reviewAbsPath(relPath string) string {
	if e == nil || relPath == "" {
		return ""
	}
	if e.ProjectPath != "" {
		return filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(relPath))
	}
	if e.FilePath != "" {
		return filepath.Join(filepath.Dir(e.FilePath), filepath.FromSlash(relPath))
	}
	return filepath.FromSlash(relPath)
}

func (e *Editor) openSceneProjectRelPath() string {
	if e == nil {
		return ""
	}
	if e.FilePath != "" {
		if e.ProjectPath != "" {
			root := filepath.Dir(e.ProjectPath)
			if rel, err := filepath.Rel(root, e.FilePath); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(e.FilePath)
	}
	if e.ScenePath != "" {
		return path.Clean(strings.ReplaceAll(e.ScenePath, `\`, "/"))
	}
	return ""
}

func reviewDefaultTargetOS() core.OS {
	switch runtime.GOOS {
	case "darwin":
		return core.OSMacOS
	case "windows":
		return core.OSWindows
	default:
		return core.OSLinux
	}
}

func (e *Editor) reviewShortcutHint(cmdID, fallback string) string {
	sc := e.EffectiveShortcut(cmdID)
	if sc == "" {
		sc = fallback
	}
	targetOS := reviewDefaultTargetOS()
	if e != nil && e.scope != nil {
		targetOS = e.scope.Env().OS()
	}
	return FormatShortcut(sc, targetOS)
}

func (e *Editor) resolveSafeReviewWritePath(relPath string) (string, error) {
	cleanRel, err := e.validateReviewRelPath(relPath)
	if err != nil {
		return "", err
	}
	abs := e.reviewAbsPath(cleanRel)
	if abs == "" {
		return "", fmt.Errorf("editor: cannot resolve review path %q", relPath)
	}
	if e.ProjectPath != "" {
		projRoot := filepath.Dir(e.ProjectPath)
		realRoot, rerr := filepath.EvalSymlinks(projRoot)
		if rerr != nil {
			realRoot, rerr = filepath.Abs(projRoot)
			if rerr != nil {
				return "", fmt.Errorf("editor: resolve project root: %w", rerr)
			}
		}
		realTarget, ok := resolvePathThroughExistingAncestors(abs)
		if !ok {
			return "", fmt.Errorf("editor: cannot resolve review target %q", cleanRel)
		}
		rel, rerr := filepath.Rel(realRoot, realTarget)
		if rerr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", fmt.Errorf("editor: review target %q escapes project root", cleanRel)
		}
		if dom, isYO := e.CheckYouOnlyTarget(realTarget); isYO {
			return "", fmt.Errorf("editor: review cannot modify You-only file %q (%s)", cleanRel, dom.DisplayName())
		}
	}
	if st, lerr := os.Lstat(abs); lerr == nil && st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("editor: review refuses to overwrite or delete symlink %q", cleanRel)
	}
	return abs, nil
}

func writeReviewFileAtomic(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".nsgo-review-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, absPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// KeepTurnReview applies all non-reverted changes across every touched file as ONE History undo step,
// saves them cleanly, shows a Toast with Undo, and updates the Agent panel summary.
// When any blocker is active, KeepTurnReview jumps to the first blocker and returns *ReviewBlockedError
// (which wraps ErrReviewBlocked).
func (e *Editor) KeepTurnReview() error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if blockers := e.ReviewBlockers(); len(blockers) > 0 {
		e.JumpToReviewBlocker()
		return &ReviewBlockedError{Blocker: blockers[0]}
	}

	diskBacked := e.ProjectPath != "" || e.FilePath != ""
	writes := make([]reviewFileWrite, 0, len(e.review.files))
	for _, fs := range e.review.files {
		if fs.kind == ReviewFileGo {
			continue
		}
		var abs string
		if diskBacked {
			var err error
			abs, err = e.resolveSafeReviewWritePath(fs.path)
			if err != nil {
				return err
			}
		} else {
			if _, err := e.validateReviewRelPath(fs.path); err != nil {
				return err
			}
		}
		if fs.kind == ReviewFileScene {
			effDoc := e.effectiveReviewSceneDoc(fs)
			if err := sceneformat.Validate(effDoc); err != nil {
				return fmt.Errorf("editor: validate review scene %s: %w", fs.path, err)
			}
		}
		afterBytes, existsAfter, err := e.effectiveReviewFileBytes(fs)
		if err != nil {
			return fmt.Errorf("editor: encode review file %s: %w", fs.path, err)
		}
		beforeBytes := append([]byte(nil), fs.beforeYAML...)
		if fs.kind == ReviewFileScene && fs.personEdited && fs.personDoc != nil {
			if enc, eerr := sceneformat.Encode(fs.personDoc); eerr == nil {
				beforeBytes = enc
			}
		}
		writes = append(writes, reviewFileWrite{
			relPath:       fs.path,
			absPath:       abs,
			existedBefore: fs.existedBefore,
			beforeBytes:   beforeBytes,
			existedAfter:  existsAfter,
			afterBytes:    afterBytes,
		})
	}

	// Commit any active valid inline YAML edits before capturing the kept snapshot.
	for _, ch := range e.review.changes {
		if ch.EditingYAML && ch.YAMLError == nil {
			ch.EditingYAML = false
		}
	}

	keptSnap := e.ReviewStatus()
	latestRound := e.reviewLatestRound()
	if e.ReviewViewingReadOnlyRound() {
		keptSnap.Changes = make([]ReviewChange, len(e.review.changes))
		for i, ch := range e.review.changes {
			keptSnap.Changes[i] = cloneReviewChangeForSnapshot(ch, latestRound, false)
		}
	}
	e.ExitHistoryPreview()
	kept, reverted := e.reviewKeptAndRevertedCounts()
	savedState := e.review
	label := "Keep review"
	if savedState.request != "" {
		label = "Agent: " + savedState.request
	}

	cmd := &agentTurnKeepCmd{
		ed:          e,
		label:       label,
		writes:      writes,
		kept:        kept,
		reverted:    reverted,
		total:       len(savedState.changes),
		fileCount:   len(savedState.files),
		rounds:      max(savedState.round, 1),
		request:     savedState.request,
		keptChanges: keptSnap.Changes,
		keptFiles:   keptSnap.Files,
		priorState:  savedState,
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}
	return nil
}

// ConfirmReviewRevertAll reverts all changes in the pending review as one reversible step
// so that ⌘Z (or the toast's Undo button) brings the review back.
func (e *Editor) ConfirmReviewRevertAll() error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	savedState := e.review
	savedState.revertConfirm = false
	savedState.peekBefore = false
	savedState.composingID = ""
	savedState.noteDraft = ""

	writes := make([]reviewFileWrite, 0, len(e.review.files))
	for _, fs := range e.review.files {
		abs, err := e.resolveSafeReviewWritePath(fs.path)
		if err != nil {
			return err
		}
		curBytes, err := os.ReadFile(abs)
		existedCur := true
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("editor: read review file %s before revert: %w", fs.path, err)
			}
			existedCur = false
			curBytes = nil
		}
		writes = append(writes, reviewFileWrite{
			relPath:       fs.path,
			absPath:       abs,
			existedBefore: existedCur,
			beforeBytes:   curBytes,
			existedAfter:  fs.existedBefore,
			afterBytes:    append([]byte(nil), fs.beforeYAML...),
		})
	}

	e.ExitHistoryPreview()
	cmd := &agentTurnRevertAllCmd{
		ed:         e,
		label:      fmt.Sprintf("Revert all %d changes", len(savedState.changes)),
		writes:     writes,
		total:      len(savedState.changes),
		fileCount:  len(savedState.files),
		request:    savedState.request,
		priorState: savedState,
	}
	return e.History.execute(cmd)
}

func (e *Editor) applyReviewFileWrites(writes []reviewFileWrite, forward bool) error {
	for i, w := range writes {
		var data []byte
		var wantExist bool
		if forward {
			data = w.afterBytes
			wantExist = w.existedAfter
		} else {
			data = w.beforeBytes
			wantExist = w.existedBefore
		}
		if w.absPath == "" {
			continue
		}
		if _, err := e.resolveSafeReviewWritePath(w.relPath); err != nil {
			e.rollbackReviewFileWrites(writes[:i], !forward)
			return err
		}
		if !wantExist {
			if err := os.Remove(w.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				// Best-effort rollback of earlier writes.
				e.rollbackReviewFileWrites(writes[:i], !forward)
				return err
			}
			continue
		}
		if err := writeReviewFileAtomic(w.absPath, data); err != nil {
			e.rollbackReviewFileWrites(writes[:i], !forward)
			return err
		}
	}
	e.syncOpenSceneAfterReviewWrites(writes, forward)
	return nil
}

func (e *Editor) rollbackReviewFileWrites(writes []reviewFileWrite, toAfter bool) {
	for i := len(writes) - 1; i >= 0; i-- {
		w := writes[i]
		if w.absPath == "" {
			continue
		}
		data, wantExist := w.beforeBytes, w.existedBefore
		if toAfter {
			data, wantExist = w.afterBytes, w.existedAfter
		}
		if !wantExist {
			_ = os.Remove(w.absPath)
		} else {
			_ = writeReviewFileAtomic(w.absPath, data)
		}
	}
}

func (e *Editor) syncOpenSceneAfterReviewWrites(writes []reviewFileWrite, forward bool) {
	openRel := e.openSceneProjectRelPath()
	for _, w := range writes {
		data, wantExist := w.beforeBytes, w.existedBefore
		if forward {
			data, wantExist = w.afterBytes, w.existedAfter
		}
		if !wantExist || len(data) == 0 {
			continue
		}
		if openRel != "" && w.relPath == openRel {
			doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: e.components})
			if err != nil {
				continue
			}
			e.Doc = doc
			e.Rebuild()
			if canon, err := sceneformat.Encode(doc); err == nil {
				e.sceneSaved = canon
			}
			e.syncActiveTab()
			if w.absPath != "" {
				e.recordSceneDiskSnapshot(w.absPath, data)
			}
			e.cancelAutoSaveTimer()
			e.recordSaveSucceededFor(e.activeTabID(), false)
			continue
		}
		// Keep any inactive open scene tabs whose path matches w.relPath in sync so switching tabs
		// never reveals stale pre-review scene docs or triggers false disk conflicts.
		for ti, tab := range e.tabs {
			if tab == nil || ti == e.activeTab {
				continue
			}
			tabRel := path.Clean(strings.ReplaceAll(tab.scenePath, `\`, "/"))
			if tabRel != w.relPath && !sameHostFilePath(tab.filePath, w.absPath) {
				continue
			}
			doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: e.components})
			if err != nil {
				continue
			}
			tab.doc = doc
			if built, berr := sceneformat.Build(doc, e.liveResolver()); berr == nil {
				built.DrawAllTiles = true
				tab.built = built
				tab.buildErr = nil
			}
			if canon, cerr := sceneformat.Encode(doc); cerr == nil {
				tab.sceneSaved = canon
			}
			tab.liveYAMLValid = false
			tab.sceneDisk = makeDiskSnapshot(w.absPath, data)
			e.recordSaveSucceededFor(tab.id, false)
		}
	}
}

type agentTurnKeepCmd struct {
	ed          *Editor
	label       string
	writes      []reviewFileWrite
	kept        int
	reverted    int
	total       int
	fileCount   int
	rounds      int
	request     string
	keptChanges []ReviewChange
	keptFiles   []ReviewFileSummary
	priorState  turnReviewState
}

func (c *agentTurnKeepCmd) Label() string       { return c.label }
func (c *agentTurnKeepCmd) Actor() HistoryActor { return HistoryActorAgent }
func (c *agentTurnKeepCmd) Do()                 { c.ed.report(c.do()) }
func (c *agentTurnKeepCmd) Undo()               { c.ed.report(c.undo()) }

func (c *agentTurnKeepCmd) historyMeta() HistoryMeta {
	files := make([]string, 0, len(c.writes))
	beforeFiles := make(map[string]historyFileSnapshot, len(c.writes))
	afterFiles := make(map[string]historyFileSnapshot, len(c.writes))
	openRel := c.ed.openSceneProjectRelPath()
	for _, w := range c.writes {
		files = append(files, w.relPath)
		if w.relPath != openRel {
			beforeFiles[w.relPath] = historyFileSnapshot{exists: w.existedBefore, data: slices.Clone(w.beforeBytes)}
			afterFiles[w.relPath] = historyFileSnapshot{exists: w.existedAfter, data: slices.Clone(w.afterBytes)}
		}
	}
	summary := strings.TrimSpace(c.request)
	if summary == "" {
		summary = "Kept review"
	}
	return HistoryMeta{
		Author:      HistoryAuthorAgent,
		Summary:     summary,
		Detail:      formatAgentTurnDetail("", c.total, c.kept),
		Files:       files,
		ChangeCount: c.total,
		KeptCount:   c.kept,
		BeforeFiles: beforeFiles,
		AfterFiles:  afterFiles,
	}
}

func (c *agentTurnKeepCmd) do() error {
	c.ed.review.applyingCmd = true
	err := c.ed.applyReviewFileWrites(c.writes, true)
	c.ed.review.applyingCmd = false
	if err != nil {
		return err
	}
	msg := formatKeptSummary(c.kept, c.reverted, c.rounds)
	c.ed.review = turnReviewState{
		open:           false,
		outcome:        ReviewOutcomeKept,
		request:        c.request,
		lastKept:       c.kept,
		lastReverted:   c.reverted,
		lastTotal:      c.total,
		lastFileCount:  c.fileCount,
		lastRounds:     max(c.rounds, 1),
		lastRequest:    c.request,
		lastOutcomeMsg: msg,
		lastChanges:    cloneReviewChangesSlice(c.keptChanges),
		lastFiles:      slices.Clone(c.keptFiles),
		legend:         c.priorState.legend,
		uiRev:          c.ed.review.uiRev + 1,
	}
	c.ed.showAgentTabForReview()
	c.ed.ShowToast(Toast{
		ID:             "review-outcome",
		Kind:           ToastSuccess,
		Message:        formatKeptToast(c.kept, c.reverted),
		ActionLabel:    "Undo",
		ActionShortcut: c.ed.reviewShortcutHint(CmdUndo, "mod+z"),
		OnAction: func() {
			c.ed.DismissToast()
			c.ed.Undo()
		},
	})
	c.ed.Status = msg
	c.ed.notify()
	return nil
}

func (c *agentTurnKeepCmd) undo() error {
	c.ed.review.applyingCmd = true
	err := c.ed.applyReviewFileWrites(c.writes, false)
	c.ed.review.applyingCmd = false
	if err != nil {
		return err
	}
	c.ed.review.outcome = ReviewOutcomeNone
	c.ed.review.open = false
	c.ed.review.lastOutcomeMsg = ""
	c.ed.review.lastChanges = nil
	c.ed.review.lastFiles = nil
	c.ed.bumpReviewUIRev()
	c.ed.Status = "Undid kept review"
	c.ed.notify()
	return nil
}

type agentTurnRevertAllCmd struct {
	ed         *Editor
	label      string
	writes     []reviewFileWrite
	total      int
	fileCount  int
	request    string
	priorState turnReviewState
}

func (c *agentTurnRevertAllCmd) Label() string       { return c.label }
func (c *agentTurnRevertAllCmd) Actor() HistoryActor { return HistoryActorPerson }
func (c *agentTurnRevertAllCmd) Do()                 { c.ed.report(c.do()) }
func (c *agentTurnRevertAllCmd) Undo()               { c.ed.report(c.undo()) }

func (c *agentTurnRevertAllCmd) historyMeta() HistoryMeta {
	files := make([]string, 0, len(c.writes))
	beforeFiles := make(map[string]historyFileSnapshot, len(c.writes))
	afterFiles := make(map[string]historyFileSnapshot, len(c.writes))
	openRel := c.ed.openSceneProjectRelPath()
	for _, w := range c.writes {
		files = append(files, w.relPath)
		if w.relPath != openRel {
			beforeFiles[w.relPath] = historyFileSnapshot{exists: w.existedBefore, data: slices.Clone(w.beforeBytes)}
			afterFiles[w.relPath] = historyFileSnapshot{exists: w.existedAfter, data: slices.Clone(w.afterBytes)}
		}
	}
	return HistoryMeta{
		Author:      HistoryAuthorYou,
		Summary:     c.label,
		Detail:      fmt.Sprintf("%d %s reverted", c.total, pluralWord(c.total, "change", "changes")),
		Files:       files,
		BeforeFiles: beforeFiles,
		AfterFiles:  afterFiles,
	}
}

func (c *agentTurnRevertAllCmd) do() error {
	c.ed.review.applyingCmd = true
	err := c.ed.applyReviewFileWrites(c.writes, true)
	c.ed.review.applyingCmd = false
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Reverted all %d · files are as they were before this request", c.total)
	c.ed.review = turnReviewState{
		open:           false,
		outcome:        ReviewOutcomeReverted,
		request:        c.request,
		lastKept:       0,
		lastReverted:   c.total,
		lastTotal:      c.total,
		lastFileCount:  c.fileCount,
		lastRounds:     max(c.priorState.round, 1),
		lastRequest:    c.request,
		lastOutcomeMsg: msg,
		legend:         c.priorState.legend,
		uiRev:          c.ed.review.uiRev + 1,
	}
	c.ed.showAgentTabForReview()
	c.ed.ShowToast(Toast{
		ID:             "review-outcome",
		Kind:           ToastInfo,
		Message:        fmt.Sprintf("Reverted all %d changes", c.total),
		ActionLabel:    "Undo",
		ActionShortcut: c.ed.reviewShortcutHint(CmdUndo, "mod+z"),
		OnAction: func() {
			c.ed.DismissToast()
			c.ed.Undo()
		},
	})
	c.ed.Status = msg
	c.ed.notify()
	return nil
}

func (c *agentTurnRevertAllCmd) undo() error {
	c.ed.review.applyingCmd = true
	err := c.ed.applyReviewFileWrites(c.writes, false)
	c.ed.review.applyingCmd = false
	if err != nil {
		return err
	}
	restored := c.priorState
	restored.open = true
	restored.outcome = ReviewOutcomePending
	restored.revertConfirm = false
	restored.uiRev = c.ed.review.uiRev + 1
	c.ed.review = restored
	c.ed.Status = "Restored pending review"
	c.ed.notify()
	return nil
}

func formatKeptToast(kept, reverted int) string {
	if reverted > 0 {
		return fmt.Sprintf("Kept %d · reverted %d", kept, reverted)
	}
	return fmt.Sprintf("Kept %d %s", kept, pluralWord(kept, "change", "changes"))
}

func formatKeptSummary(kept, reverted, rounds int) string {
	parts := []string{fmt.Sprintf("Kept %d", kept)}
	if reverted > 0 {
		parts = append(parts, fmt.Sprintf("reverted %d", reverted))
	}
	if rounds > 1 {
		parts = append(parts, fmt.Sprintf("%d rounds", rounds))
	}
	parts = append(parts, "one undo step")
	return strings.Join(parts, " · ")
}

func (e *Editor) reviewKeptAndRevertedCounts() (kept, reverted int) {
	for _, ch := range e.review.changes {
		if ch.GoRequiresConfirm {
			continue
		}
		if ch.Reverted {
			reverted++
		} else {
			kept++
		}
	}
	return kept, reverted
}

// ReviewStatus returns a complete read-only snapshot of the current or most recent turn review.
func (e *Editor) ReviewStatus() ReviewSnapshot {
	if e == nil {
		return ReviewSnapshot{}
	}
	st := &e.review
	snap := ReviewSnapshot{
		Active:         st.outcome == ReviewOutcomePending,
		Open:           st.outcome == ReviewOutcomePending && st.open,
		Outcome:        st.outcome,
		Request:        st.request,
		FocusedID:      st.focusedID,
		FocusedRowKey:  st.focusedRowKey,
		Filter:         st.filter,
		Search:         st.search,
		PeekBefore:     st.peekBefore,
		RevertConfirm:  st.revertConfirm,
		OverlayLegend:  st.legend,
		LastRounds:     st.lastRounds,
		LastOutcomeMsg: st.lastOutcomeMsg,
	}
	if st.outcome != ReviewOutcomePending {
		snap.TotalChanges = st.lastTotal
		snap.KeptCount = st.lastKept
		snap.RevertedCount = st.lastReverted
		snap.FileCount = st.lastFileCount
		if snap.Request == "" {
			snap.Request = st.lastRequest
		}
		if st.outcome == ReviewOutcomeKept && len(st.lastChanges) > 0 {
			snap.Changes = cloneReviewChangesSlice(st.lastChanges)
			snap.Files = slices.Clone(st.lastFiles)
			seen := 0
			for _, ch := range snap.Changes {
				if ch.Seen {
					seen++
				}
			}
			snap.SeenCount = seen
		}
		return snap
	}

	viewRound := e.reviewActiveViewRound()
	snap.Round = e.reviewLatestRound()
	snap.ViewRound = viewRound
	snap.ViewingReadOnlyRound = e.ReviewViewingReadOnlyRound()
	snap.DraftNoteCount = e.ReviewDraftNoteCount()
	snap.ComposingChangeID = st.composingID
	snap.PendingBatch = e.PendingReviewNotes()
	snap.RoundBanner = e.ReviewRoundBannerText()
	snap.TotalChanges = len(st.changes)
	snap.FileCount = len(st.files)
	snap.Blockers = e.ReviewBlockers()
	kept, reverted := e.reviewKeptAndRevertedCounts()
	snap.KeptCount = kept
	snap.RevertedCount = reverted

	seen := 0
	snap.Changes = make([]ReviewChange, len(st.changes))
	for i, ch := range st.changes {
		if ch.Seen {
			seen++
		}
		snap.Changes[i] = cloneReviewChangeForSnapshot(ch, viewRound, snap.ViewingReadOnlyRound)
	}
	snap.SeenCount = seen

	snap.Files = make([]ReviewFileSummary, 0, len(st.files))
	for _, fs := range st.files {
		sum := ReviewFileSummary{
			Path:      fs.path,
			Name:      path.Base(fs.path),
			FileKind:  fs.kind,
			Collapsed: st.collapsedFiles[fs.path],
		}
		revCnt, revOff := 0, 0
		for _, ch := range st.changes {
			if ch.File != fs.path {
				continue
			}
			switch ch.Kind {
			case ReviewChangeAdded:
				sum.Added++
			case ReviewChangeChanged:
				sum.Changed++
			case ReviewChangeRemoved:
				sum.Removed++
			}
			if !ch.GoRequiresConfirm {
				revCnt++
				if ch.Reverted {
					revOff++
				}
			}
		}
		sum.AllReverted = revCnt > 0 && revOff == revCnt
		snap.Files = append(snap.Files, sum)
	}
	return snap
}

// ReviewRevertAllFileList returns a human-readable list of files affected by Revert All for the confirmation dialog.
func (e *Editor) ReviewRevertAllFileList() string {
	if e == nil || len(e.review.files) == 0 {
		return ""
	}
	names := make([]string, 0, len(e.review.files))
	for _, fs := range e.review.files {
		name := path.Base(fs.path)
		name = strings.TrimSuffix(name, ".scene.yaml")
		names = append(names, name)
	}
	return joinEnglishList(names)
}
