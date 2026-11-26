package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoDataTableReview is returned by data table review actions when no data table review is active.
var ErrNoDataTableReview = errors.New("editor: no data table review is open")

// DataTableReviewFilter selects which rows are visible in the Data Table review table.
type DataTableReviewFilter string

const (
	DataTableReviewFilterAll       DataTableReviewFilter = "all"
	DataTableReviewFilterChanged   DataTableReviewFilter = "changed"
	DataTableReviewFilterUnchanged DataTableReviewFilter = "unchanged"
	DataTableReviewFilterReverted  DataTableReviewFilter = "reverted"
	DataTableReviewFilterYours     DataTableReviewFilter = "yours"
)

// DataTableReviewCheck is one named check figure attached by the agent to a data table review turn
// (for example "Over 12 min, floors 1–3": "none", before "9", basis "Coin rate from 20 playtests of floor 3: 30 coins a minute.").
type DataTableReviewCheck struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Before string `json:"before,omitempty"`
	Basis  string `json:"basis,omitempty"`
}

// DataTableReviewFileSpec describes one data table file included in an agent review turn.
type DataTableReviewFileSpec struct {
	// Path is the absolute or disk file path written on Keep (empty for in-memory-only tables).
	Path string
	// RelPath is the display / project-relative slash-separated path (for example "data/items.yaml").
	RelPath string
	// Base is the original parsed datatable.Document before the agent's request (nil for a new file).
	Base *datatable.Document
	// Proposed is the agent's proposed datatable.Document (must be non-nil; may have 0 rows).
	Proposed *datatable.Document
	// IsNew marks a file that did not exist before the agent's request.
	IsNew bool
}

// DataTableReviewRequest opens a full-window row-by-row Data Table review for one agent turn.
type DataTableReviewRequest struct {
	Prompt string
	Files  []DataTableReviewFileSpec
	Checks []DataTableReviewCheck
}

// DataTableReviewColumn describes one visible column in the Data Table review table.
type DataTableReviewColumn struct {
	Name      string
	Label     string
	Kind      datatable.ValueKind
	HasDelta  bool
	IsPrimary bool
}

// DataTableReviewCellDiff describes the original, proposed, and active value of one cell in a reviewed record.
type DataTableReviewCellDiff struct {
	Column      string
	Kind        datatable.ValueKind
	Base        datatable.Value
	HasBase     bool
	Proposed    datatable.Value
	HasProposed bool
	Current     datatable.Value
	HasCurrent  bool
	Changed     bool
	DeltaPct    int
	HasDeltaPct bool
	UserEdited  bool
}

// DataTableReviewRecord is a read-only view of one row in the active Data Table review file.
type DataTableReviewRecord struct {
	Key           string
	BaseIndex     int
	ProposedIndex int
	FileOrder     int
	DisplayName   string
	KindTag       string
	Changed       bool
	Added         bool
	Removed       bool
	Reverted      bool
	UserEdited    bool
	UserReordered bool
	Cells         map[string]DataTableReviewCellDiff
}

// DataTableReviewYAMLLine is one line in the focused record's right-hand YAML diff pane.
type DataTableReviewYAMLLine struct {
	Line int
	Sign string // "", "+", or "-"
	Text string
	Tag  string // "" or "you"
}

// DataTableReviewFileSummary summarizes one file in the active Data Table review turn.
type DataTableReviewFileSummary struct {
	RelPath           string `json:"relPath"`
	Name              string `json:"name"`
	Path              string `json:"path,omitempty"`
	IsNew             bool   `json:"isNew,omitempty"`
	TotalRecords      int    `json:"totalRecords"`
	ChangedRecords    int    `json:"changedRecords"`
	KeptRecords       int    `json:"keptRecords"`
	RevertedRecords   int    `json:"revertedRecords"`
	UserEditedRecords int    `json:"userEditedRecords"`
	Meta              string `json:"meta"`
}

// DataTableReviewSnapshot is a read-only summary of the active Data Table review for UI and MCP.
type DataTableReviewSnapshot struct {
	Open            bool                         `json:"open"`
	Paused          bool                         `json:"paused,omitempty"`
	Prompt          string                       `json:"prompt,omitempty"`
	ActiveFile      string                       `json:"activeFile,omitempty"`
	ActiveFileIndex int                          `json:"activeFileIndex"`
	FocusKey        string                       `json:"focusKey,omitempty"`
	Filter          DataTableReviewFilter        `json:"filter"`
	SearchQuery     string                       `json:"searchQuery,omitempty"`
	SortColumn      string                       `json:"sortColumn,omitempty"`
	SortOrder       string                       `json:"sortOrder,omitempty"`
	AriaSort        string                       `json:"ariaSort,omitempty"`
	KeptCount       int                          `json:"keptCount"`
	TotalChanged    int                          `json:"totalChanged"`
	UserEditedCount int                          `json:"userEditedCount"`
	Files           []DataTableReviewFileSummary `json:"files,omitempty"`
	Checks          []DataTableReviewCheck       `json:"checks,omitempty"`
	ChecksOutOfDate bool                         `json:"checksOutOfDate,omitempty"`
}

type dtReviewRecord struct {
	key           string
	baseIndex     int // -1 if added
	proposedIndex int // -1 if removed
	baseRow       datatable.Row
	hasBase       bool
	proposedRow   datatable.Row
	hasProposed   bool
	agentChanged  bool
	added         bool
	removed       bool
	reverted      bool
	userEdits     map[string]datatable.Value
	userReordered bool
	baseLine      int
}

func (r *dtReviewRecord) clone() *dtReviewRecord {
	if r == nil {
		return nil
	}
	cp := *r
	cp.baseRow = r.baseRow.Clone()
	cp.proposedRow = r.proposedRow.Clone()
	if len(r.userEdits) > 0 {
		cp.userEdits = make(map[string]datatable.Value, len(r.userEdits))
		for k, v := range r.userEdits {
			cp.userEdits[k] = v.Clone()
		}
	}
	return &cp
}

type dtReviewFile struct {
	path        string
	relPath     string
	name        string
	isNew       bool
	base        *datatable.Document
	proposed    *datatable.Document
	rowType     *datatable.RowType
	idCol       string
	nameCol     string
	kindCol     string
	allColumns  []string
	colKinds    map[string]datatable.ValueKind
	records     []*dtReviewRecord // in current file order
	recordByKey map[string]*dtReviewRecord
	initialKeys []string // baseline/proposed key order for reorder detection
	focusKey    string
	filter      DataTableReviewFilter
	query       string
	sortCol     string
	sortOrder   SortOrder
	editKey     string
	editCol     string
	editDraft   string
	lastErr     error
}

func (f *dtReviewFile) clone() *dtReviewFile {
	if f == nil {
		return nil
	}
	cp := *f
	cp.base = f.base.Clone()
	cp.proposed = f.proposed.Clone()
	cp.allColumns = slices.Clone(f.allColumns)
	cp.initialKeys = slices.Clone(f.initialKeys)
	if len(f.colKinds) > 0 {
		cp.colKinds = make(map[string]datatable.ValueKind, len(f.colKinds))
		for k, v := range f.colKinds {
			cp.colKinds[k] = v
		}
	}
	cp.records = make([]*dtReviewRecord, len(f.records))
	cp.recordByKey = make(map[string]*dtReviewRecord, len(f.records))
	for i, rec := range f.records {
		rc := rec.clone()
		cp.records[i] = rc
		cp.recordByKey[rc.key] = rc
	}
	return &cp
}

type dataTableReviewState struct {
	prompt          string
	files           []*dtReviewFile
	activeFile      int
	checks          []DataTableReviewCheck
	checksOutOfDate bool
	paused          bool
	rev             uint64
}

func (s *dataTableReviewState) clone() *dataTableReviewState {
	if s == nil {
		return nil
	}
	cp := *s
	cp.checks = slices.Clone(s.checks)
	cp.files = make([]*dtReviewFile, len(s.files))
	for i, f := range s.files {
		cp.files[i] = f.clone()
	}
	return &cp
}

// OpenDataTableReview opens a full-window row-by-row review for an agent's data table changes
// without modifying files on disk until KeepDataTableReview is called.
func (e *Editor) OpenDataTableReview(req DataTableReviewRequest) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if len(req.Files) == 0 {
		return errors.New("editor: data table review requires at least one file")
	}

	builtFiles := make([]*dtReviewFile, 0, len(req.Files))
	for i, spec := range req.Files {
		if spec.Proposed == nil {
			return fmt.Errorf("editor: data table review file %d has nil proposed document", i)
		}
		if spec.Path != "" {
			if err := e.checkDataTableReviewPath(spec.Path); err != nil {
				return err
			}
		}
		rel := strings.TrimSpace(spec.RelPath)
		if rel == "" && spec.Path != "" {
			rel = e.dataTableMatchPath(spec.Path)
		}
		if rel == "" {
			rel = fmt.Sprintf("table_%d.yaml", i+1)
		}
		rel = filepath.ToSlash(rel)
		if err := e.checkDataTableReviewRelPath(rel); err != nil {
			return err
		}
		rt := e.DataTableRowTypeFor(spec.Path)
		if rt == nil {
			rt = e.DataTableRowTypeFor(rel)
		}
		if rt != nil {
			if err := rt.ValidateDocument(rel, spec.Proposed); err != nil {
				return err
			}
		}
		f := buildDataTableReviewFile(spec, rel, rt)
		builtFiles = append(builtFiles, f)
	}

	checks := make([]DataTableReviewCheck, 0, len(req.Checks))
	for _, ch := range req.Checks {
		if strings.TrimSpace(ch.Label) == "" && strings.TrimSpace(ch.Value) == "" {
			continue
		}
		checks = append(checks, DataTableReviewCheck{
			Label:  strings.TrimSpace(ch.Label),
			Value:  strings.TrimSpace(ch.Value),
			Before: strings.TrimSpace(ch.Before),
			Basis:  strings.TrimSpace(ch.Basis),
		})
	}

	e.tableReview = &dataTableReviewState{
		prompt:          strings.TrimSpace(req.Prompt),
		files:           builtFiles,
		activeFile:      0,
		checks:          checks,
		checksOutOfDate: false,
		paused:          false,
		rev:             1,
	}
	e.cancelAutoSaveTimer()
	kept, total := e.DataTableReviewKeptCount()
	e.Status = fmt.Sprintf("Reviewing %d of %d data table changes", kept, total)
	e.notify()
	return nil
}

func (e *Editor) checkDataTableReviewRelPath(rel string) error {
	clean := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(rel)), "./")
	if clean == "" {
		return nil
	}
	if e != nil {
		if dom, ok := e.CheckYouOnlyTarget(clean); ok {
			return fmt.Errorf("editor: data table review refuses You-only file %q (%s)", clean, dom.DisplayName())
		}
	}
	lower := strings.ToLower(clean)
	if lower == ".git" || strings.HasPrefix(lower, ".git/") {
		return fmt.Errorf("editor: data table review refuses path inside .git (%q)", clean)
	}
	if lower == strings.ToLower(project.FileName) {
		return fmt.Errorf("editor: data table review refuses project manifest %q", clean)
	}
	return nil
}

func (e *Editor) checkDataTableReviewPath(filePath string) error {
	if e == nil || filePath == "" {
		return nil
	}
	if dom, ok := e.CheckYouOnlyTarget(filePath); ok {
		return fmt.Errorf("editor: data table review refuses You-only file %q (%s)", filePath, dom.DisplayName())
	}
	if e.ProjectPath == "" {
		return nil
	}
	root := filepath.Dir(e.ProjectPath)
	if absRoot, err := filepath.Abs(root); err == nil {
		root = absRoot
	}
	if realRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = realRoot
	}
	_, rel, err := resolvePathWithinRoot(root, filePath)
	if err != nil {
		return err
	}
	return e.checkDataTableReviewRelPath(rel)
}

func buildDataTableReviewFile(spec DataTableReviewFileSpec, rel string, rt *datatable.RowType) *dtReviewFile {
	var base *datatable.Document
	if spec.Base != nil {
		base = spec.Base.Clone()
	}
	proposed := spec.Proposed.Clone()
	isNew := spec.IsNew || base == nil

	// Collect union of columns in deterministic first-appearance order.
	var allCols []string
	seenCol := make(map[string]struct{})
	addCols := func(doc *datatable.Document) {
		if doc == nil {
			return
		}
		for _, c := range doc.Columns() {
			if _, ok := seenCol[c]; !ok {
				seenCol[c] = struct{}{}
				allCols = append(allCols, c)
			}
		}
	}
	addCols(base)
	addCols(proposed)
	if rt != nil {
		for _, c := range rt.Columns() {
			if _, ok := seenCol[c.Name]; !ok {
				seenCol[c.Name] = struct{}{}
				allCols = append(allCols, c.Name)
			}
		}
	}

	colKinds := make(map[string]datatable.ValueKind, len(allCols))
	for _, c := range allCols {
		kind := datatable.KindNull
		if rt != nil {
			if cs, ok := rt.Column(c); ok {
				kind = cs.Kind
			}
		}
		if kind == datatable.KindNull && base != nil {
			kind = base.ColumnKind(c)
		}
		if kind == datatable.KindNull && proposed != nil {
			kind = proposed.ColumnKind(c)
		}
		colKinds[c] = kind
	}

	idCol := detectReviewIDColumn(base, proposed)
	nameCol := ""
	if _, ok := seenCol["name"]; ok {
		nameCol = "name"
	} else if idCol != "" {
		nameCol = idCol
	} else if len(allCols) > 0 {
		nameCol = allCols[0]
	}
	kindCol := ""
	if _, ok := seenCol["kind"]; ok {
		kindCol = "kind"
	}

	// Build key maps for matching rows between base and proposed.
	type indexedRow struct {
		idx  int
		row  datatable.Row
		line int
	}
	baseByKey := make(map[string]indexedRow)
	baseKeys := make([]string, 0)
	if base != nil {
		for i := 0; i < base.RowCount(); i++ {
			r := base.Row(i)
			k := reviewRowKey(r, idCol, i)
			baseByKey[k] = indexedRow{idx: i, row: r, line: base.RowLine(i)}
			baseKeys = append(baseKeys, k)
		}
	}
	propByKey := make(map[string]indexedRow)
	propKeys := make([]string, 0)
	if proposed != nil {
		for i := 0; i < proposed.RowCount(); i++ {
			r := proposed.Row(i)
			k := reviewRowKey(r, idCol, i)
			propByKey[k] = indexedRow{idx: i, row: r, line: proposed.RowLine(i)}
			propKeys = append(propKeys, k)
		}
	}

	// Order: proposed rows in proposed order, followed by any removed rows from base.
	records := make([]*dtReviewRecord, 0, max(len(propKeys), len(baseKeys)))
	recordByKey := make(map[string]*dtReviewRecord, len(propKeys)+len(baseKeys))
	initialKeys := make([]string, 0, len(propKeys)+len(baseKeys))

	for _, k := range propKeys {
		pr := propByKey[k]
		br, hasBase := baseByKey[k]
		rec := &dtReviewRecord{
			key:           k,
			baseIndex:     -1,
			proposedIndex: pr.idx,
			proposedRow:   pr.row,
			hasProposed:   true,
			baseLine:      pr.line,
		}
		if hasBase {
			rec.baseIndex = br.idx
			rec.baseRow = br.row
			rec.hasBase = true
			if br.line > 0 {
				rec.baseLine = br.line
			}
			rec.agentChanged = !rowsEqual(br.row, pr.row)
		} else {
			rec.added = true
			rec.agentChanged = true
		}
		records = append(records, rec)
		recordByKey[k] = rec
		initialKeys = append(initialKeys, k)
	}
	for _, k := range baseKeys {
		if _, exists := recordByKey[k]; exists {
			continue
		}
		br := baseByKey[k]
		rec := &dtReviewRecord{
			key:           k,
			baseIndex:     br.idx,
			proposedIndex: -1,
			baseRow:       br.row,
			hasBase:       true,
			removed:       true,
			agentChanged:  true,
			baseLine:      br.line,
		}
		records = append(records, rec)
		recordByKey[k] = rec
		initialKeys = append(initialKeys, k)
	}

	// Default focus: first changed record, else first record.
	focusKey := ""
	for _, rec := range records {
		if rec.agentChanged {
			focusKey = rec.key
			break
		}
	}
	if focusKey == "" && len(records) > 0 {
		focusKey = records[0].key
	}

	return &dtReviewFile{
		path:        spec.Path,
		relPath:     rel,
		name:        filepath.Base(rel),
		isNew:       isNew,
		base:        base,
		proposed:    proposed,
		rowType:     rt,
		idCol:       idCol,
		nameCol:     nameCol,
		kindCol:     kindCol,
		allColumns:  allCols,
		colKinds:    colKinds,
		records:     records,
		recordByKey: recordByKey,
		initialKeys: initialKeys,
		focusKey:    focusKey,
		filter:      DataTableReviewFilterAll,
		sortOrder:   SortNone,
	}
}

func detectReviewIDColumn(base, proposed *datatable.Document) string {
	candidates := []string{"id", "key", "slug", "name"}
	checkUnique := func(doc *datatable.Document, col string) bool {
		if doc == nil || doc.RowCount() == 0 {
			return true
		}
		seen := make(map[string]struct{}, doc.RowCount())
		for i := 0; i < doc.RowCount(); i++ {
			v, ok := doc.Cell(i, col)
			if !ok || v.Kind == datatable.KindNull {
				return false
			}
			s := strings.TrimSpace(v.Display())
			if s == "" {
				return false
			}
			if _, dup := seen[s]; dup {
				return false
			}
			seen[s] = struct{}{}
		}
		return true
	}
	hasAny := func(col string) bool {
		if base != nil && base.RowCount() > 0 {
			if _, ok := base.Cell(0, col); ok {
				return true
			}
		}
		if proposed != nil && proposed.RowCount() > 0 {
			if _, ok := proposed.Cell(0, col); ok {
				return true
			}
		}
		return false
	}
	for _, col := range candidates {
		if hasAny(col) && checkUnique(base, col) && checkUnique(proposed, col) {
			return col
		}
	}
	return ""
}

func reviewRowKey(r datatable.Row, idCol string, index int) string {
	if idCol != "" {
		if v, ok := r.Get(idCol); ok {
			if s := strings.TrimSpace(v.Display()); s != "" {
				return s
			}
		}
	}
	return fmt.Sprintf("row-%d", index+1)
}

func rowsEqual(a, b datatable.Row) bool {
	if len(a.Cells) != len(b.Cells) {
		return false
	}
	for _, ca := range a.Cells {
		vb, ok := b.Get(ca.Key)
		if !ok || !ca.Value.Equal(vb) {
			return false
		}
	}
	return true
}

// DataTableReviewOpen reports whether the full-window Data Table review screen is currently visible.
func (e *Editor) DataTableReviewOpen() bool {
	return e != nil && e.tableReview != nil && !e.tableReview.paused
}

// DataTableReviewPending reports whether a Data Table review is active (either open or paused with Esc).
func (e *Editor) DataTableReviewPending() bool {
	return e != nil && e.tableReview != nil
}

// DataTableReviewPaused reports whether a Data Table review is currently paused in the background.
func (e *Editor) DataTableReviewPaused() bool {
	return e != nil && e.tableReview != nil && e.tableReview.paused
}

func (e *Editor) dataTableReviewRev() uint64 {
	if e == nil || e.tableReview == nil {
		return 0
	}
	return e.tableReview.rev
}

func (e *Editor) activeDataTableReviewFile() *dtReviewFile {
	if e == nil || e.tableReview == nil || len(e.tableReview.files) == 0 {
		return nil
	}
	idx := e.tableReview.activeFile
	if idx < 0 || idx >= len(e.tableReview.files) {
		idx = 0
		e.tableReview.activeFile = 0
	}
	return e.tableReview.files[idx]
}

// DataTableReviewPrompt returns the quoted request prompt of the active review, or "".
func (e *Editor) DataTableReviewPrompt() string {
	if e == nil || e.tableReview == nil {
		return ""
	}
	return e.tableReview.prompt
}

// PauseDataTableReview exits the full-window review screen while keeping the review pending
// and shows the "Review paused" toast with a Resume action.
func (e *Editor) PauseDataTableReview() {
	if e == nil || e.tableReview == nil || e.tableReview.paused {
		return
	}
	if f := e.activeDataTableReviewFile(); f != nil {
		f.editKey = ""
		f.editCol = ""
		f.editDraft = ""
	}
	e.tableReview.paused = true
	e.tableReview.rev++
	e.ShowToast(Toast{
		ID:          "datatable-review-paused",
		Kind:        ToastInfo,
		Message:     e.T("dtReview.toastPaused"),
		ActionLabel: e.T("dtReview.resume"),
		OnAction: func() {
			e.ResumeDataTableReview()
		},
	})
	e.Status = e.T("dtReview.toastPaused")
	e.notify()
}

// pauseDataTableReviewSilent pauses an open Data Table review without showing a toast
// (used when entering Play mode so Play is never hidden behind the review screen).
func (e *Editor) pauseDataTableReviewSilent() {
	if e == nil || e.tableReview == nil || e.tableReview.paused {
		return
	}
	if f := e.activeDataTableReviewFile(); f != nil {
		f.editKey = ""
		f.editCol = ""
		f.editDraft = ""
	}
	e.tableReview.paused = true
	e.tableReview.rev++
}

// ResumeDataTableReview reopens a paused Data Table review at the exact file, focus, filter, and sort state.
func (e *Editor) ResumeDataTableReview() {
	if e == nil || e.tableReview == nil || e.playing || e.HistoryPreviewActive() {
		return
	}
	if !e.tableReview.paused {
		return
	}
	e.tableReview.paused = false
	e.tableReview.rev++
	if t, ok := e.ActiveToast(); ok && t.ID == "datatable-review-paused" {
		e.DismissToast()
	}
	e.notify()
}

// DataTableReviewFiles returns summaries of all data table files in the active review turn.
func (e *Editor) DataTableReviewFiles() []DataTableReviewFileSummary {
	if e == nil || e.tableReview == nil {
		return nil
	}
	out := make([]DataTableReviewFileSummary, len(e.tableReview.files))
	for i, f := range e.tableReview.files {
		out[i] = f.summary(e)
	}
	return out
}

func (f *dtReviewFile) summary(e *Editor) DataTableReviewFileSummary {
	total := len(f.records)
	changed, kept, reverted, yours := f.counts()
	var meta string
	if f.isNew {
		meta = e.T("dtReview.fileMetaNew", l10n.Param("count", total))
	} else {
		meta = e.T("dtReview.fileMetaChanged", l10n.Param("changed", changed), l10n.Param("total", total))
	}
	return DataTableReviewFileSummary{
		RelPath:           f.relPath,
		Name:              f.name,
		Path:              f.path,
		IsNew:             f.isNew,
		TotalRecords:      total,
		ChangedRecords:    changed,
		KeptRecords:       kept,
		RevertedRecords:   reverted,
		UserEditedRecords: yours,
		Meta:              meta,
	}
}

func (f *dtReviewFile) counts() (changed, kept, reverted, yours int) {
	for _, rec := range f.records {
		if rec.agentChanged {
			changed++
			if rec.reverted {
				reverted++
			} else {
				kept++
			}
		}
		if len(rec.userEdits) > 0 || rec.userReordered {
			yours++
		}
	}
	return changed, kept, reverted, yours
}

// DataTableReviewActiveFileIndex returns the 0-based index of the currently selected file in the review.
func (e *Editor) DataTableReviewActiveFileIndex() int {
	if e == nil || e.tableReview == nil {
		return -1
	}
	return e.tableReview.activeFile
}

// SelectDataTableReviewFile switches the active file in the Data Table review by 0-based index.
func (e *Editor) SelectDataTableReviewFile(index int) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if index < 0 || index >= len(e.tableReview.files) {
		return fmt.Errorf("editor: data table review file %d out of range", index)
	}
	if e.tableReview.activeFile == index {
		return nil
	}
	if cur := e.activeDataTableReviewFile(); cur != nil {
		cur.editKey = ""
		cur.editCol = ""
		cur.editDraft = ""
	}
	e.tableReview.activeFile = index
	e.tableReview.rev++
	e.notify()
	return nil
}

// SelectDataTableReviewFileByPath switches the active file in the review by project-relative or base path.
func (e *Editor) SelectDataTableReviewFileByPath(relOrName string) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	target := filepath.ToSlash(strings.TrimSpace(relOrName))
	for i, f := range e.tableReview.files {
		if f.relPath == target || f.name == target || f.path == relOrName {
			return e.SelectDataTableReviewFile(i)
		}
	}
	return fmt.Errorf("editor: data table review file %q not found", relOrName)
}

// DataTableReviewKeptCount returns the total kept changed records and total changed records across all files in the review.
func (e *Editor) DataTableReviewKeptCount() (kept, total int) {
	if e == nil || e.tableReview == nil {
		return 0, 0
	}
	for _, f := range e.tableReview.files {
		c, k, _, _ := f.counts()
		kept += k
		total += c
	}
	return kept, total
}

// DataTableReviewUserEditCount returns the total number of records edited or reordered by the person across all files.
func (e *Editor) DataTableReviewUserEditCount() int {
	if e == nil || e.tableReview == nil {
		return 0
	}
	yours := 0
	for _, f := range e.tableReview.files {
		_, _, _, y := f.counts()
		yours += y
	}
	return yours
}

// DataTableReviewChipCounts returns counts for All, Changed, Unchanged, Reverted, and Yours in the active file.
func (e *Editor) DataTableReviewChipCounts() (all, changed, unchanged, reverted, yours int) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return 0, 0, 0, 0, 0
	}
	all = len(f.records)
	changed, _, reverted, yours = f.counts()
	unchanged = all - changed
	return all, changed, unchanged, reverted, yours
}

// DataTableReviewFilter returns the active filter chip for the selected file.
func (e *Editor) DataTableReviewFilter() DataTableReviewFilter {
	f := e.activeDataTableReviewFile()
	if f == nil || f.filter == "" {
		return DataTableReviewFilterAll
	}
	return f.filter
}

// SetDataTableReviewFilter switches the active row filter chip (All, Changed, Unchanged, Reverted, Yours).
func (e *Editor) SetDataTableReviewFilter(filter DataTableReviewFilter) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return
	}
	switch filter {
	case DataTableReviewFilterAll, DataTableReviewFilterChanged, DataTableReviewFilterUnchanged, DataTableReviewFilterReverted, DataTableReviewFilterYours:
	default:
		filter = DataTableReviewFilterAll
	}
	if f.filter == filter {
		return
	}
	f.filter = filter
	f.editKey = ""
	f.editCol = ""
	f.editDraft = ""
	e.tableReview.rev++
	e.notify()
}

// DataTableReviewSearch returns the text filter query for the active file.
func (e *Editor) DataTableReviewSearch() string {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ""
	}
	return f.query
}

// SetDataTableReviewSearch updates the case-insensitive text filter query for the active file.
func (e *Editor) SetDataTableReviewSearch(query string) {
	f := e.activeDataTableReviewFile()
	if f == nil || f.query == query {
		return
	}
	f.query = query
	e.tableReview.rev++
	e.notify()
}

// DataTableReviewSort returns the active file's sort column and direction.
func (e *Editor) DataTableReviewSort() (string, SortOrder) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return "", SortNone
	}
	return f.sortCol, f.sortOrder
}

// SetDataTableReviewSort sets the active file's sort column and direction without mutating file row order.
func (e *Editor) SetDataTableReviewSort(col string, order SortOrder) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return
	}
	if order == SortNone || col == "" {
		col = ""
		order = SortNone
	}
	if f.sortCol == col && f.sortOrder == order {
		return
	}
	f.sortCol = col
	f.sortOrder = order
	e.tableReview.rev++
	e.notify()
}

// CycleDataTableReviewSort cycles col through ascending -> descending -> file order (none).
func (e *Editor) CycleDataTableReviewSort(col string) SortOrder {
	f := e.activeDataTableReviewFile()
	if f == nil || col == "" {
		return SortNone
	}
	if f.sortCol != col {
		f.sortCol = col
		f.sortOrder = SortAsc
	} else {
		switch f.sortOrder {
		case SortAsc:
			f.sortOrder = SortDesc
		case SortDesc:
			f.sortCol = ""
			f.sortOrder = SortNone
		default:
			f.sortOrder = SortAsc
		}
	}
	e.tableReview.rev++
	e.notify()
	return f.sortOrder
}

// DataTableReviewAriaSort returns the WAI-ARIA aria-sort value ("ascending", "descending", or "none") for col.
func (e *Editor) DataTableReviewAriaSort(col string) string {
	sortCol, order := e.DataTableReviewSort()
	if sortCol != col {
		return "none"
	}
	switch order {
	case SortAsc:
		return "ascending"
	case SortDesc:
		return "descending"
	default:
		return "none"
	}
}

// DataTableReviewColumns returns the display columns for the active review file.
// When both `id` and `name` exist and `id` has no changes, `name` serves as the primary display column
// (and `kind`, when unchanged, is folded alongside `name` as in ReviewData.dc.html); any changed column
// is always kept visible.
func (e *Editor) DataTableReviewColumns() []DataTableReviewColumn {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return nil
	}
	return f.displayColumns()
}

func (f *dtReviewFile) columnHasChanges(col string) bool {
	for _, rec := range f.records {
		d := f.cellDiff(rec, col)
		if d.Changed || d.UserEdited {
			return true
		}
	}
	return false
}

func (f *dtReviewFile) displayColumns() []DataTableReviewColumn {
	hideID := f.idCol != "" && f.nameCol != "" && f.idCol != f.nameCol && !f.columnHasChanges(f.idCol)
	foldKind := f.kindCol != "" && f.nameCol != "" && f.kindCol != f.nameCol && !f.columnHasChanges(f.kindCol)

	cols := make([]DataTableReviewColumn, 0, len(f.allColumns))
	for _, name := range f.allColumns {
		if hideID && name == f.idCol {
			continue
		}
		if foldKind && name == f.kindCol {
			continue
		}
		kind := f.colKinds[name]
		hasDelta := kind == datatable.KindNumber && f.columnHasChanges(name)
		label := strings.ToUpper(strings.ReplaceAll(name, "_", " "))
		if name == f.nameCol && f.idCol != "" && f.nameCol == "name" {
			label = "ITEM"
		}
		if hasDelta {
			label += " Δ"
		}
		cols = append(cols, DataTableReviewColumn{
			Name:      name,
			Label:     label,
			Kind:      kind,
			HasDelta:  hasDelta,
			IsPrimary: name == f.nameCol,
		})
	}
	return cols
}

func (f *dtReviewFile) cellDiff(rec *dtReviewRecord, col string) DataTableReviewCellDiff {
	baseVal, hasBase := datatable.Value{}, false
	if rec.hasBase {
		baseVal, hasBase = rec.baseRow.Get(col)
	}
	propVal, hasProp := datatable.Value{}, false
	if rec.hasProposed {
		propVal, hasProp = rec.proposedRow.Get(col)
	}
	userVal, userEdited := rec.userEdits[col]

	var curVal datatable.Value
	var hasCur bool
	if rec.reverted {
		curVal, hasCur = baseVal, hasBase
		userEdited = false
	} else if rec.removed {
		curVal, hasCur = datatable.Value{}, false
		userEdited = false
	} else if userEdited {
		curVal, hasCur = userVal, true
	} else if hasProp {
		curVal, hasCur = propVal, true
	} else if hasBase && !rec.added {
		curVal, hasCur = baseVal, true
	}

	changed := false
	if !rec.reverted {
		if hasBase != hasCur {
			changed = true
		} else if hasBase && !baseVal.Equal(curVal) {
			changed = true
		}
	}

	kind := f.colKinds[col]
	if kind == datatable.KindNull && hasCur {
		kind = curVal.Kind
	}
	if kind == datatable.KindNull && hasBase {
		kind = baseVal.Kind
	}

	deltaPct := 0
	hasDelta := false
	if changed && hasBase && hasCur && baseVal.Kind == datatable.KindNumber && curVal.Kind == datatable.KindNumber {
		if bf, okB := baseVal.Float64(); okB && bf != 0 && !math.IsNaN(bf) && !math.IsInf(bf, 0) {
			if cf, okC := curVal.Float64(); okC && !math.IsNaN(cf) && !math.IsInf(cf, 0) {
				rawPct := (cf - bf) / math.Abs(bf) * 100
				if !math.IsNaN(rawPct) && !math.IsInf(rawPct, 0) && math.Abs(rawPct) <= 1e15 {
					deltaPct = int(math.Round(rawPct))
					hasDelta = true
				}
			}
		}
	}

	return DataTableReviewCellDiff{
		Column:      col,
		Kind:        kind,
		Base:        baseVal.Clone(),
		HasBase:     hasBase,
		Proposed:    propVal.Clone(),
		HasProposed: hasProp,
		Current:     curVal.Clone(),
		HasCurrent:  hasCur,
		Changed:     changed,
		DeltaPct:    deltaPct,
		HasDeltaPct: hasDelta,
		UserEdited:  userEdited,
	}
}

func (f *dtReviewFile) buildRecordView(rec *dtReviewRecord, fileOrder int) DataTableReviewRecord {
	cells := make(map[string]DataTableReviewCellDiff, len(f.allColumns))
	for _, col := range f.allColumns {
		cells[col] = f.cellDiff(rec, col)
	}
	displayName := rec.key
	if f.nameCol != "" {
		if d, ok := cells[f.nameCol]; ok {
			if d.HasCurrent {
				if s := d.Current.Display(); s != "" {
					displayName = s
				}
			} else if d.HasBase {
				if s := d.Base.Display(); s != "" {
					displayName = s
				}
			}
		}
	}
	kindTag := ""
	if f.kindCol != "" {
		if d, ok := cells[f.kindCol]; ok {
			if d.HasCurrent {
				kindTag = d.Current.Display()
			} else if d.HasBase {
				kindTag = d.Base.Display()
			}
		}
	}
	return DataTableReviewRecord{
		Key:           rec.key,
		BaseIndex:     rec.baseIndex,
		ProposedIndex: rec.proposedIndex,
		FileOrder:     fileOrder,
		DisplayName:   displayName,
		KindTag:       kindTag,
		Changed:       rec.agentChanged,
		Added:         rec.added,
		Removed:       rec.removed,
		Reverted:      rec.reverted,
		UserEdited:    len(rec.userEdits) > 0,
		UserReordered: rec.userReordered,
		Cells:         cells,
	}
}

// DataTableReviewAllRecords returns all records in the active review file in current file order,
// ignoring the UI filter chip, search query, and sort order.
func (e *Editor) DataTableReviewAllRecords() []DataTableReviewRecord {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return nil
	}
	out := make([]DataTableReviewRecord, len(f.records))
	for i, rec := range f.records {
		out[i] = f.buildRecordView(rec, i)
	}
	return out
}

// DataTableReviewVisibleRecords returns the records visible in the active file after applying
// the active chip filter, text search query, and sort column/direction.
func (e *Editor) DataTableReviewVisibleRecords() []DataTableReviewRecord {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(f.query))
	out := make([]DataTableReviewRecord, 0, len(f.records))
	for i, rec := range f.records {
		switch f.filter {
		case DataTableReviewFilterChanged:
			if !rec.agentChanged {
				continue
			}
		case DataTableReviewFilterUnchanged:
			if rec.agentChanged {
				continue
			}
		case DataTableReviewFilterReverted:
			if !rec.reverted {
				continue
			}
		case DataTableReviewFilterYours:
			if len(rec.userEdits) == 0 && !rec.userReordered {
				continue
			}
		}
		view := f.buildRecordView(rec, i)
		if q != "" && !reviewRecordMatchesQuery(view, q) {
			continue
		}
		out = append(out, view)
	}

	if f.sortOrder != SortNone && f.sortCol != "" {
		col := f.sortCol
		desc := f.sortOrder == SortDesc
		useDelta := f.colKinds[col] == datatable.KindNumber && f.columnHasChanges(col)
		slices.SortStableFunc(out, func(a, b DataTableReviewRecord) int {
			var c int
			if useDelta {
				da := deltaSortKey(a.Cells[col])
				db := deltaSortKey(b.Cells[col])
				c = cmp.Compare(da, db)
			} else {
				ca := a.Cells[col]
				cb := b.Cells[col]
				nullA := !ca.HasCurrent || ca.Current.Kind == datatable.KindNull
				nullB := !cb.HasCurrent || cb.Current.Kind == datatable.KindNull
				switch {
				case nullA && nullB:
					c = 0
				case nullA:
					return 1
				case nullB:
					return -1
				default:
					c = compareCellValues(ca.Current, cb.Current)
				}
			}
			if c == 0 {
				c = cmp.Compare(a.FileOrder, b.FileOrder)
			}
			if desc {
				return -c
			}
			return c
		})
	}
	return out
}

func deltaSortKey(d DataTableReviewCellDiff) float64 {
	if !d.HasBase || !d.HasCurrent {
		return 0
	}
	bf, okB := d.Base.Float64()
	cf, okC := d.Current.Float64()
	if !okB || !okC || bf == 0 || math.IsNaN(bf) || math.IsInf(bf, 0) || math.IsNaN(cf) || math.IsInf(cf, 0) {
		return 0
	}
	rawPct := (cf - bf) / math.Abs(bf) * 100
	if math.IsNaN(rawPct) || math.IsInf(rawPct, 0) {
		return 0
	}
	return math.Round(rawPct)
}

func reviewRecordMatchesQuery(r DataTableReviewRecord, lowerQ string) bool {
	if strings.Contains(strings.ToLower(r.Key), lowerQ) ||
		strings.Contains(strings.ToLower(r.DisplayName), lowerQ) ||
		strings.Contains(strings.ToLower(r.KindTag), lowerQ) {
		return true
	}
	for _, c := range r.Cells {
		if c.HasCurrent && strings.Contains(strings.ToLower(c.Current.Display()), lowerQ) {
			return true
		}
		if c.HasBase && strings.Contains(strings.ToLower(c.Base.Display()), lowerQ) {
			return true
		}
	}
	return false
}

// FormatReviewCellValue formats a cell value for display in the Data Table review grid:
// numeric zero displays as "—" (em dash), matching ReviewData.dc.html.
func FormatReviewCellValue(v datatable.Value) string {
	if v.Kind == datatable.KindNull {
		return "—"
	}
	if v.Kind == datatable.KindNumber {
		if f, ok := v.Float64(); ok && f == 0 {
			return "—"
		}
	}
	return v.Display()
}

// FormatReviewDeltaPct formats a percentage delta (e.g. "+33%", "−42%", or "" when 0).
func FormatReviewDeltaPct(pct int) string {
	if pct > 0 {
		return fmt.Sprintf("+%d%%", pct)
	}
	if pct < 0 {
		// Convert via uint64 to avoid signed overflow if pct == math.MinInt.
		return fmt.Sprintf("−%d%%", uint64(-int64(pct+1))+1)
	}
	return ""
}

// DataTableReviewFocusKey returns the focused record key in the active file, or "".
func (e *Editor) DataTableReviewFocusKey() string {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ""
	}
	return f.focusKey
}

// FocusDataTableReviewRecord focuses record key in the active file so the right-hand YAML pane shows its diff.
func (e *Editor) FocusDataTableReviewRecord(key string) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return
	}
	if _, ok := f.recordByKey[key]; !ok {
		return
	}
	if f.focusKey == key {
		return
	}
	f.focusKey = key
	e.tableReview.rev++
	e.notify()
}

// StepDataTableReviewFocus moves focus to the next (+1) or previous (-1) visible record (J / K keys).
func (e *Editor) StepDataTableReviewFocus(delta int) {
	f := e.activeDataTableReviewFile()
	if f == nil || delta == 0 {
		return
	}
	visible := e.DataTableReviewVisibleRecords()
	if len(visible) == 0 {
		return
	}
	idx := -1
	for i, r := range visible {
		if r.Key == f.focusKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		if delta > 0 {
			idx = 0
		} else {
			idx = len(visible) - 1
		}
	} else {
		idx += delta
		if idx < 0 {
			idx = 0
		} else if idx >= len(visible) {
			idx = len(visible) - 1
		}
	}
	e.FocusDataTableReviewRecord(visible[idx].Key)
}

// ToggleDataTableReviewRecord toggles record key between reverted and kept, marking the agent's checks out of date.
func (e *Editor) ToggleDataTableReviewRecord(key string) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	rec, ok := f.recordByKey[key]
	if !ok {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if !rec.agentChanged {
		return nil
	}
	rec.reverted = !rec.reverted
	if f.editKey == key {
		f.editKey = ""
		f.editCol = ""
		f.editDraft = ""
	}
	f.lastErr = nil
	e.tableReview.checksOutOfDate = true
	e.tableReview.rev++
	e.notify()
	return nil
}

// RevertDataTableReviewRecord marks record key as reverted and marks the agent's checks out of date.
func (e *Editor) RevertDataTableReviewRecord(key string) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	rec, ok := f.recordByKey[key]
	if !ok {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if rec.reverted {
		return nil
	}
	return e.ToggleDataTableReviewRecord(key)
}

// RestoreDataTableReviewRecord restores a reverted record key and marks the agent's checks out of date.
func (e *Editor) RestoreDataTableReviewRecord(key string) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	rec, ok := f.recordByKey[key]
	if !ok {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if !rec.reverted {
		return nil
	}
	return e.ToggleDataTableReviewRecord(key)
}

// StartDataTableReviewCellEdit opens the inline value editor for (key, col) in the active file.
func (e *Editor) StartDataTableReviewCellEdit(key, col string) {
	if e == nil || e.playing || e.HistoryPreviewActive() {
		return
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return
	}
	rec, ok := f.recordByKey[key]
	if !ok || rec.reverted || rec.removed {
		return
	}
	f.focusKey = key
	f.editKey = key
	f.editCol = col
	diff := f.cellDiff(rec, col)
	if diff.HasCurrent {
		f.editDraft = diff.Current.Display()
	} else {
		f.editDraft = ""
	}
	f.lastErr = nil
	e.tableReview.rev++
	e.notify()
}

// FinishDataTableReviewCellEdit closes the active inline cell editor.
func (e *Editor) FinishDataTableReviewCellEdit() {
	f := e.activeDataTableReviewFile()
	if f == nil || (f.editKey == "" && f.editCol == "") {
		return
	}
	f.editKey = ""
	f.editCol = ""
	f.editDraft = ""
	e.tableReview.rev++
	e.notify()
}

// EditingDataTableReviewCell returns the record key and column currently being edited inline, if any.
func (e *Editor) EditingDataTableReviewCell() (key, col string, ok bool) {
	f := e.activeDataTableReviewFile()
	if f == nil || f.editKey == "" || f.editCol == "" {
		return "", "", false
	}
	return f.editKey, f.editCol, true
}

// DataTableReviewEditDraft returns the in-progress text in the active inline cell editor.
func (e *Editor) DataTableReviewEditDraft() string {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ""
	}
	return f.editDraft
}

// SetDataTableReviewEditDraft updates the in-progress text in the active inline cell editor without triggering a table remount.
func (e *Editor) SetDataTableReviewEditDraft(text string) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return
	}
	f.editDraft = text
}

// DataTableReviewError returns the most recent inline validation error in the active review file, if any.
func (e *Editor) DataTableReviewError() error {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return nil
	}
	return f.lastErr
}

// SetDataTableReviewCellText edits cell (key, col) in place as the person's edit.
// It joins the active review (filtered by Yours and tagged `you` in the YAML) and marks
// the agent's checks out of date.
func (e *Editor) SetDataTableReviewCellText(key, col, text string) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	rec, ok := f.recordByKey[key]
	if !ok {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if f.editKey == key && f.editCol == col {
		f.editDraft = text
	}
	if rec.reverted {
		err := fmt.Errorf("editor: restore %q before editing its values", key)
		f.lastErr = err
		e.notify()
		return err
	}
	if rec.removed {
		err := fmt.Errorf("editor: cannot edit removed record %q", key)
		f.lastErr = err
		e.notify()
		return err
	}
	rowIdx := rec.proposedIndex
	if rowIdx < 0 {
		rowIdx = rec.baseIndex
	}
	if rowIdx < 0 {
		rowIdx = 0
	}

	var (
		val datatable.Value
		err error
	)
	if f.rowType != nil {
		val, err = f.rowType.ParseCellText(f.relPath, rowIdx, col, text)
		if err != nil {
			f.lastErr = err
			e.tableReview.rev++
			e.notify()
			return err
		}
	} else {
		kind := f.colKinds[col]
		if kind == datatable.KindNull {
			val = datatable.InferValue(text)
		} else {
			val, err = datatable.ParseValue(kind, text)
			if err != nil {
				ce := &datatable.CellError{
					File:  f.relPath,
					Row:   rowIdx + 1,
					Field: col,
					Value: text,
					Err:   err,
				}
				f.lastErr = ce
				e.tableReview.rev++
				e.notify()
				return ce
			}
		}
	}

	return e.applyDataTableReviewCellValue(f, rec, col, val)
}

// StepDataTableReviewCellNumber increments or decrements a numeric cell by delta in place.
func (e *Editor) StepDataTableReviewCellNumber(key, col string, delta float64) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return errors.New("editor: numeric step delta must be finite")
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	rec, ok := f.recordByKey[key]
	if !ok {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if rec.reverted {
		err := fmt.Errorf("editor: restore %q before editing its values", key)
		f.lastErr = err
		e.notify()
		return err
	}
	if rec.removed {
		err := fmt.Errorf("editor: cannot edit removed record %q", key)
		f.lastErr = err
		e.notify()
		return err
	}
	diff := f.cellDiff(rec, col)
	if !diff.HasCurrent || diff.Current.Kind != datatable.KindNumber {
		return fmt.Errorf("editor: cell %s.%s is not numeric", key, col)
	}
	var nextVal datatable.Value
	if iv, okI := diff.Current.Int64(); okI && math.Trunc(delta) == delta && math.Abs(delta) <= float64(math.MaxInt64/2) {
		dInt := int64(delta)
		if (dInt > 0 && iv > math.MaxInt64-dInt) || (dInt < 0 && iv < math.MinInt64-dInt) {
			return fmt.Errorf("editor: cell %s.%s value overflows int64", key, col)
		}
		next := iv + dInt
		if iv >= 0 && next < 0 {
			next = 0
		}
		nextVal = datatable.IntValue(next)
	} else {
		fv, okF := diff.Current.Float64()
		if !okF || math.IsNaN(fv) || math.IsInf(fv, 0) {
			return fmt.Errorf("editor: cell %s.%s is not a valid number", key, col)
		}
		next := math.Round((fv+delta)*1000) / 1000
		if math.IsNaN(next) || math.IsInf(next, 0) {
			return fmt.Errorf("editor: cell %s.%s value is out of range", key, col)
		}
		if fv >= 0 && next < 0 {
			next = 0
		}
		nextVal = datatable.FloatValue(next)
	}
	if err := e.applyDataTableReviewCellValue(f, rec, col, nextVal); err != nil {
		return err
	}
	if f.editKey == key && f.editCol == col {
		f.editDraft = nextVal.Display()
	}
	return nil
}

func (e *Editor) applyDataTableReviewCellValue(f *dtReviewFile, rec *dtReviewRecord, col string, val datatable.Value) error {
	// Validate row against registered Go row type if present.
	if f.rowType != nil {
		candidate := rec.effectiveRow()
		replaced := false
		for i := range candidate.Cells {
			if candidate.Cells[i].Key == col {
				candidate.Cells[i].Value = val.Clone()
				replaced = true
				break
			}
		}
		if !replaced {
			candidate.Cells = append(candidate.Cells, datatable.Cell{Key: col, Value: val.Clone()})
		}
		rowIdx := max(0, rec.proposedIndex)
		if err := f.rowType.ValidateRow(f.relPath, rowIdx, candidate); err != nil {
			f.lastErr = err
			e.tableReview.rev++
			e.notify()
			return err
		}
	}

	// Compare against the agent's proposed value (or base value for unchanged rows).
	refVal, hasRef := datatable.Value{}, false
	if rec.hasProposed {
		refVal, hasRef = rec.proposedRow.Get(col)
	} else if rec.hasBase {
		refVal, hasRef = rec.baseRow.Get(col)
	}

	prevDiff := f.cellDiff(rec, col)
	if hasRef && refVal.Equal(val) {
		delete(rec.userEdits, col)
	} else {
		if rec.userEdits == nil {
			rec.userEdits = make(map[string]datatable.Value)
		}
		rec.userEdits[col] = val.Clone()
	}

	f.focusKey = rec.key
	f.lastErr = nil
	if !prevDiff.HasCurrent || !prevDiff.Current.Equal(val) {
		e.tableReview.checksOutOfDate = true
	}
	e.tableReview.rev++
	e.notify()
	return nil
}

func (r *dtReviewRecord) effectiveRow() datatable.Row {
	var row datatable.Row
	if r.reverted && r.hasBase {
		row = r.baseRow.Clone()
	} else if r.hasProposed {
		row = r.proposedRow.Clone()
	} else if r.hasBase {
		row = r.baseRow.Clone()
	}
	if !r.reverted && len(r.userEdits) > 0 {
		editCols := make([]string, 0, len(r.userEdits))
		for col := range r.userEdits {
			editCols = append(editCols, col)
		}
		slices.Sort(editCols)
		for _, col := range editCols {
			val := r.userEdits[col]
			if idx := row.Index(col); idx >= 0 {
				row.Cells[idx].Value = val.Clone()
			} else {
				row.Cells = append(row.Cells, datatable.Cell{Key: col, Value: val.Clone()})
			}
		}
	}
	return row
}

// ReorderDataTableReviewRecord moves record key to 0-based position toIndex in the active file's row order.
// The reorder joins the review as the person's edit (filtered by Yours and tagged `you` in the YAML)
// and marks the agent's checks out of date.
func (e *Editor) ReorderDataTableReviewRecord(key string, toIndex int) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	f := e.activeDataTableReviewFile()
	if f == nil {
		return ErrNoDataTableReview
	}
	fromIdx := -1
	for i, rec := range f.records {
		if rec.key == key {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		return fmt.Errorf("editor: data table review record %q not found", key)
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(f.records) {
		toIndex = len(f.records) - 1
	}
	if fromIdx == toIndex {
		return nil
	}
	rec := f.records[fromIdx]
	f.records = slices.Delete(f.records, fromIdx, fromIdx+1)
	f.records = slices.Insert(f.records, toIndex, rec)
	// Update userReordered flags: if the entire file is back to its initial order, clear all flags;
	// otherwise mark the moved record as reordered by the person.
	allRestored := len(f.records) == len(f.initialKeys)
	if allRestored {
		for i, r := range f.records {
			if r.key != f.initialKeys[i] {
				allRestored = false
				break
			}
		}
	}
	if allRestored {
		for _, r := range f.records {
			r.userReordered = false
		}
	} else {
		rec.userReordered = true
	}
	f.focusKey = rec.key
	e.tableReview.checksOutOfDate = true
	e.tableReview.rev++
	e.notify()
	return nil
}

// SetDataTableReviewChecks attaches or refreshes the agent's named check figures on the active review,
// clearing the out-of-date flag.
func (e *Editor) SetDataTableReviewChecks(checks []DataTableReviewCheck) error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	clean := make([]DataTableReviewCheck, 0, len(checks))
	for _, ch := range checks {
		if strings.TrimSpace(ch.Label) == "" && strings.TrimSpace(ch.Value) == "" {
			continue
		}
		clean = append(clean, DataTableReviewCheck{
			Label:  strings.TrimSpace(ch.Label),
			Value:  strings.TrimSpace(ch.Value),
			Before: strings.TrimSpace(ch.Before),
			Basis:  strings.TrimSpace(ch.Basis),
		})
	}
	e.tableReview.checks = clean
	e.tableReview.checksOutOfDate = false
	e.tableReview.rev++
	e.notify()
	return nil
}

// DataTableReviewChecks returns a copy of the agent's named check figures on the active review.
func (e *Editor) DataTableReviewChecks() []DataTableReviewCheck {
	if e == nil || e.tableReview == nil {
		return nil
	}
	return slices.Clone(e.tableReview.checks)
}

// DataTableReviewChecksOutOfDate reports whether the person has reverted, edited, or reordered records
// since the agent's check figures were last attached or refreshed.
func (e *Editor) DataTableReviewChecksOutOfDate() bool {
	return e != nil && e.tableReview != nil && e.tableReview.checksOutOfDate
}

// DataTableReviewFocusedYAML returns the header title and +/- YAML diff lines for the focused record
// (or the empty file's YAML lines when the active file has 0 records).
func (e *Editor) DataTableReviewFocusedYAML() (string, []DataTableReviewYAMLLine) {
	f := e.activeDataTableReviewFile()
	if f == nil {
		return "", nil
	}
	if len(f.records) == 0 {
		sign := ""
		if f.isNew {
			sign = "+"
		}
		return f.name, []DataTableReviewYAMLLine{
			{Line: 1, Sign: sign, Text: "version: 1"},
			{Line: 2, Sign: sign, Text: "rows: []"},
		}
	}
	rec := f.recordByKey[f.focusKey]
	if rec == nil {
		rec = f.records[0]
	}

	// Preserve row key order: base row keys first, then any newly added keys from proposed or user edits.
	keys := make([]string, 0, len(f.allColumns))
	seen := make(map[string]struct{})
	addRowKeys := func(row datatable.Row) {
		for _, c := range row.Cells {
			if _, ok := seen[c.Key]; !ok {
				seen[c.Key] = struct{}{}
				keys = append(keys, c.Key)
			}
		}
	}
	if rec.hasBase {
		addRowKeys(rec.baseRow)
	}
	if rec.hasProposed {
		addRowKeys(rec.proposedRow)
	}
	if len(rec.userEdits) > 0 {
		extraKeys := make([]string, 0, len(rec.userEdits))
		for k := range rec.userEdits {
			extraKeys = append(extraKeys, k)
		}
		slices.Sort(extraKeys)
		for _, k := range extraKeys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				keys = append(keys, k)
			}
		}
	}

	startLine := rec.baseLine
	if startLine <= 0 {
		filePos := 0
		for i, r := range f.records {
			if r.key == rec.key {
				filePos = i
				break
			}
		}
		startLine = 3 + filePos*max(1, len(keys))
	}

	lines := make([]DataTableReviewYAMLLine, 0, len(keys)+2)
	lineCursor := startLine
	for i, col := range keys {
		prefix := "    "
		if i == 0 {
			prefix = "  - "
		}
		lineNum := lineCursor
		if rec.hasBase && f.base != nil && rec.baseIndex >= 0 {
			if cl := f.base.CellLine(rec.baseIndex, col); cl > 0 {
				lineNum = cl
			}
		} else if rec.hasProposed && f.proposed != nil && rec.proposedIndex >= 0 {
			if cl := f.proposed.CellLine(rec.proposedIndex, col); cl > 0 {
				lineNum = cl
			}
		}
		diff := f.cellDiff(rec, col)
		rowTag := ""
		if i == 0 && rec.userReordered {
			rowTag = "you"
		}

		switch {
		case rec.added && !rec.reverted:
			if diff.HasCurrent {
				tag := rowTag
				if diff.UserEdited {
					tag = "you"
				}
				lines = append(lines, DataTableReviewYAMLLine{
					Line: lineNum,
					Sign: "+",
					Text: prefix + col + ": " + formatYAMLReviewScalar(diff.Current),
					Tag:  tag,
				})
				lineCursor = lineNum + 1
			}
		case rec.removed && !rec.reverted:
			if diff.HasBase {
				lines = append(lines, DataTableReviewYAMLLine{
					Line: lineNum,
					Sign: "-",
					Text: prefix + col + ": " + formatYAMLReviewScalar(diff.Base),
					Tag:  rowTag,
				})
				lineCursor = lineNum + 1
			}
		case diff.Changed && !rec.reverted:
			if diff.HasBase {
				lines = append(lines, DataTableReviewYAMLLine{
					Line: lineNum,
					Sign: "-",
					Text: prefix + col + ": " + formatYAMLReviewScalar(diff.Base),
				})
			}
			if diff.HasCurrent {
				tag := rowTag
				if diff.UserEdited {
					tag = "you"
				}
				lines = append(lines, DataTableReviewYAMLLine{
					Line: lineNum,
					Sign: "+",
					Text: prefix + col + ": " + formatYAMLReviewScalar(diff.Current),
					Tag:  tag,
				})
			}
			lineCursor = lineNum + 1
		default:
			if diff.HasCurrent {
				lines = append(lines, DataTableReviewYAMLLine{
					Line: lineNum,
					Sign: "",
					Text: prefix + col + ": " + formatYAMLReviewScalar(diff.Current),
					Tag:  rowTag,
				})
				lineCursor = lineNum + 1
			}
		}
	}
	return rec.key, lines
}

func formatYAMLReviewScalar(v datatable.Value) string {
	if v.Kind == datatable.KindNull {
		return "null"
	}
	if v.Kind == datatable.KindList {
		if len(v.List) == 0 {
			return "[]"
		}
		parts := make([]string, len(v.List))
		for i, elem := range v.List {
			parts[i] = formatYAMLReviewScalar(elem)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return v.Display()
}

// DataTableReviewSnapshot returns a full read-only snapshot of the active Data Table review.
func (e *Editor) DataTableReviewSnapshot() DataTableReviewSnapshot {
	if e == nil || e.tableReview == nil {
		return DataTableReviewSnapshot{}
	}
	kept, total := e.DataTableReviewKeptCount()
	f := e.activeDataTableReviewFile()
	snap := DataTableReviewSnapshot{
		Open:            !e.tableReview.paused,
		Paused:          e.tableReview.paused,
		Prompt:          e.tableReview.prompt,
		ActiveFileIndex: e.tableReview.activeFile,
		KeptCount:       kept,
		TotalChanged:    total,
		UserEditedCount: e.DataTableReviewUserEditCount(),
		Files:           e.DataTableReviewFiles(),
		Checks:          e.DataTableReviewChecks(),
		ChecksOutOfDate: e.tableReview.checksOutOfDate,
	}
	if f != nil {
		snap.ActiveFile = f.relPath
		snap.FocusKey = f.focusKey
		snap.Filter = f.filter
		snap.SearchQuery = f.query
		snap.SortColumn = f.sortCol
		snap.SortOrder = f.sortOrder.String()
		snap.AriaSort = e.DataTableReviewAriaSort(f.sortCol)
	}
	return snap
}

func composeDataTableReviewFile(f *dtReviewFile) (*datatable.Document, []byte, error) {
	if f.isNew || f.base == nil {
		// Compose new file from proposed, honoring per-record reverts, user edits, and row reorders.
		doc := datatable.NewDocument()
		for _, rec := range f.records {
			if rec.reverted {
				continue
			}
			row := rec.effectiveRow()
			if _, err := doc.AppendRow(row.Cells...); err != nil {
				return nil, nil, err
			}
		}
		if f.rowType != nil {
			if err := f.rowType.ValidateDocument(f.relPath, doc); err != nil {
				return nil, nil, err
			}
		}
		if len(f.records) == 0 && f.proposed != nil {
			data, err := f.proposed.Marshal()
			if err != nil {
				return nil, nil, err
			}
			return f.proposed.Clone(), data, nil
		}
		data, err := doc.Marshal()
		if err != nil {
			return nil, nil, err
		}
		return doc, data, nil
	}

	// Existing file: determine whether the kept result has any structural changes
	// (row order changed from base, or kept added/removed rows).
	structural := false
	var activeKeys []string
	for _, rec := range f.records {
		if rec.added && !rec.reverted {
			structural = true
			activeKeys = append(activeKeys, rec.key)
			continue
		}
		if rec.removed && !rec.reverted {
			structural = true
			continue
		}
		if rec.hasBase {
			activeKeys = append(activeKeys, rec.key)
		}
	}
	if !structural {
		if len(activeKeys) != f.base.RowCount() {
			structural = true
		} else {
			expectedBaseIdx := 0
			for _, rec := range f.records {
				if rec.added && rec.reverted {
					continue
				}
				if rec.baseIndex != expectedBaseIdx {
					structural = true
					break
				}
				expectedBaseIdx++
			}
		}
	}

	// Fast, lossless path: start from base.Clone() so preserved raw bytes stay intact,
	// and apply SetCell ONLY for kept/edited cells that differ from base.
	// Reverted records and untouched cells are never touched, so only kept & edited lines change!
	if !structural {
		doc := f.base.Clone()
		for _, rec := range f.records {
			if rec.reverted || !rec.hasBase || rec.baseIndex < 0 {
				continue
			}
			// Check for deleted columns on this row.
			eff := rec.effectiveRow()
			for _, bc := range rec.baseRow.Cells {
				if _, ok := eff.Get(bc.Key); !ok {
					if err := doc.DeleteCell(rec.baseIndex, bc.Key); err != nil {
						return nil, nil, err
					}
				}
			}
			for _, ec := range eff.Cells {
				bv, hadBase := rec.baseRow.Get(ec.Key)
				if hadBase && bv.Equal(ec.Value) {
					continue
				}
				if err := doc.SetCell(rec.baseIndex, ec.Key, ec.Value); err != nil {
					return nil, nil, err
				}
			}
		}
		if f.rowType != nil {
			if err := f.rowType.ValidateDocument(f.relPath, doc); err != nil {
				return nil, nil, err
			}
		}
		data, err := doc.Marshal()
		if err != nil {
			return nil, nil, err
		}
		return doc, data, nil
	}

	// Structural path (rows added, removed, or reordered): start from base.Clone() so untouched rows
	// keep their YAML AST nodes, comments, and key order.
	doc := f.base.Clone()
	// Map each base index to its current index in doc as we apply updates/deletes/inserts/moves.
	curKeys := make([]string, doc.RowCount())
	for i := 0; i < doc.RowCount(); i++ {
		curKeys[i] = reviewRowKey(doc.Row(i), f.idCol, i)
	}
	indexOfKey := func(k string) int {
		for i, ck := range curKeys {
			if ck == k {
				return i
			}
		}
		return -1
	}

	// 1. Delete kept removals.
	for _, rec := range f.records {
		if rec.removed && !rec.reverted {
			if idx := indexOfKey(rec.key); idx >= 0 {
				if err := doc.DeleteRow(idx); err != nil {
					return nil, nil, err
				}
				curKeys = slices.Delete(curKeys, idx, idx+1)
			}
		}
	}

	// 2. Append kept additions and apply cell edits to kept existing rows.
	desiredOrder := make([]string, 0, len(f.records))
	for _, rec := range f.records {
		if rec.removed && !rec.reverted {
			continue
		}
		if rec.added && rec.reverted {
			continue
		}
		desiredOrder = append(desiredOrder, rec.key)
		if rec.added {
			eff := rec.effectiveRow()
			if _, err := doc.AppendRow(eff.Cells...); err != nil {
				return nil, nil, err
			}
			curKeys = append(curKeys, rec.key)
			continue
		}
		if rec.reverted {
			continue
		}
		idx := indexOfKey(rec.key)
		if idx < 0 {
			continue
		}
		eff := rec.effectiveRow()
		for _, bc := range rec.baseRow.Cells {
			if _, ok := eff.Get(bc.Key); !ok {
				if err := doc.DeleteCell(idx, bc.Key); err != nil {
					return nil, nil, err
				}
			}
		}
		for _, ec := range eff.Cells {
			bv, hadBase := rec.baseRow.Get(ec.Key)
			if hadBase && bv.Equal(ec.Value) {
				continue
			}
			if err := doc.SetCell(idx, ec.Key, ec.Value); err != nil {
				return nil, nil, err
			}
		}
	}

	// 3. Move rows into desiredOrder.
	for targetIdx, key := range desiredOrder {
		curIdx := indexOfKey(key)
		if curIdx < 0 || curIdx == targetIdx {
			continue
		}
		if err := doc.MoveRow(curIdx, targetIdx); err != nil {
			return nil, nil, err
		}
		moved := curKeys[curIdx]
		curKeys = slices.Delete(curKeys, curIdx, curIdx+1)
		curKeys = slices.Insert(curKeys, targetIdx, moved)
	}

	if f.rowType != nil {
		if err := f.rowType.ValidateDocument(f.relPath, doc); err != nil {
			return nil, nil, err
		}
	}
	data, err := doc.Marshal()
	if err != nil {
		return nil, nil, err
	}
	return doc, data, nil
}

type dtReviewFileChange struct {
	path          string
	relPath       string
	existedBefore bool
	beforeBytes   []byte
	afterBytes    []byte
	afterDoc      *datatable.Document
}

// KeepDataTableReview commits all kept record changes and user edits across every file in the
// active Data Table review as ONE atomic undo step, preserving untouched lines byte for byte.
func (e *Editor) KeepDataTableReview() error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}

	review := e.tableReview
	keptCount, totalChanged := e.DataTableReviewKeptCount()
	revertedCount := totalChanged - keptCount
	yoursCount := e.DataTableReviewUserEditCount()

	changes := make([]dtReviewFileChange, 0, len(review.files))
	for _, f := range review.files {
		doc, afterBytes, err := composeDataTableReviewFile(f)
		if err != nil {
			f.lastErr = err
			review.rev++
			e.notify()
			return err
		}
		var (
			beforeBytes   []byte
			existedBefore bool
		)
		if f.path != "" {
			if raw, rerr := os.ReadFile(f.path); rerr == nil {
				beforeBytes = raw
				existedBefore = true
			} else if !errors.Is(rerr, os.ErrNotExist) {
				return rerr
			}
		} else if f.base != nil {
			if raw, merr := f.base.Marshal(); merr == nil {
				beforeBytes = raw
				existedBefore = !f.isNew
			}
		}
		if existedBefore && bytes.Equal(beforeBytes, afterBytes) {
			continue
		}
		changes = append(changes, dtReviewFileChange{
			path:          f.path,
			relPath:       f.relPath,
			existedBefore: existedBefore,
			beforeBytes:   beforeBytes,
			afterBytes:    afterBytes,
			afterDoc:      doc,
		})
	}

	// Pre-write check: validate paths stay inside project root and obey You-only boundaries.
	for _, ch := range changes {
		if ch.path != "" {
			if err := e.checkDataTableReviewPath(ch.path); err != nil {
				return err
			}
		}
		if err := e.checkDataTableReviewRelPath(ch.relPath); err != nil {
			return err
		}
	}

	cmd := &dataTableReviewKeepCmd{
		ed:          e,
		label:       fmt.Sprintf("Keep %d of %d data table changes", keptCount, totalChanged),
		changes:     changes,
		beforeTable: e.table.clone(),
		beforeSaved: bytes.Clone(e.tableSaved),
	}
	if err := cmd.apply(true); err != nil {
		return err
	}
	cmd.appliedOnce = true
	e.tableReview = nil

	// Record rich metadata for the unified History timeline (agent turn, touched files, before/after snapshots).
	touchedFiles := make([]string, 0, len(changes))
	var beforeFiles, afterFiles map[string]historyFileSnapshot
	if len(changes) > 0 {
		beforeFiles = make(map[string]historyFileSnapshot, len(changes))
		afterFiles = make(map[string]historyFileSnapshot, len(changes))
		for _, ch := range changes {
			rel := filepath.ToSlash(strings.TrimSpace(ch.relPath))
			if rel == "" && ch.path != "" {
				rel = e.displayHistoryPath(ch.path)
			}
			if rel != "" {
				touchedFiles = append(touchedFiles, rel)
				beforeFiles[rel] = historyFileSnapshot{
					exists: ch.existedBefore,
					data:   bytes.Clone(ch.beforeBytes),
				}
				afterFiles[rel] = historyFileSnapshot{
					exists: true,
					data:   bytes.Clone(ch.afterBytes),
				}
			}
		}
	}
	summary := strings.TrimSpace(review.prompt)
	if summary == "" {
		summary = cmd.label
	}
	prevMeta := e.historyPanel.pendingMeta
	e.historyPanel.pendingMeta = &HistoryMeta{
		Author:      HistoryAuthorAgent,
		Summary:     summary,
		Detail:      formatAgentTurnDetail("", totalChanged, keptCount),
		Files:       touchedFiles,
		ChangeCount: totalChanged,
		KeptCount:   keptCount,
		BeforeFiles: beforeFiles,
		AfterFiles:  afterFiles,
	}
	e.History.Execute(cmd)
	if e.historyPanel.pendingMeta != nil {
		e.historyPanel.pendingMeta = prevMeta
	}
	e.scheduleAutoSaveIfNeeded()

	msg := e.T("dtReview.toastKept",
		l10n.Param("kept", keptCount),
		l10n.Param("reverted", revertedCount),
	)
	if yoursCount > 0 {
		msg += " · " + e.T("dtReview.toastYours", l10n.Param("count", yoursCount))
	}
	e.Status = msg
	shortcut := FormatShortcut(e.EffectiveShortcut(CmdUndo), e.currentOS())
	e.ShowToast(Toast{
		ID:             "datatable-review-result",
		Kind:           ToastSuccess,
		Message:        msg,
		ActionLabel:    e.T("toolbar.undo"),
		ActionShortcut: shortcut,
		OnAction: func() {
			e.Undo()
			e.DismissToast()
		},
	})
	e.notify()
	return nil
}

// RevertAllDataTableReview discards all pending changes in the active Data Table review without
// modifying files on disk, closing the review with a one-step Undo toast that restores the review.
func (e *Editor) RevertAllDataTableReview() error {
	if e == nil || e.tableReview == nil {
		return ErrNoDataTableReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	_, totalChanged := e.DataTableReviewKeptCount()
	snap := e.tableReview.clone()
	snap.paused = false

	e.History.Execute(&dataTableReviewRevertAllCmd{
		ed:     e,
		label:  fmt.Sprintf("Revert all %d data table changes", totalChanged),
		before: snap,
	})

	msg := e.T("dtReview.toastReverted", l10n.Param("count", totalChanged))
	e.Status = msg
	shortcut := FormatShortcut(e.EffectiveShortcut(CmdUndo), e.currentOS())
	e.ShowToast(Toast{
		ID:             "datatable-review-result",
		Kind:           ToastInfo,
		Message:        msg,
		ActionLabel:    e.T("toolbar.undo"),
		ActionShortcut: shortcut,
		OnAction: func() {
			e.Undo()
			e.DismissToast()
		},
	})
	e.notify()
	return nil
}

type dataTableReviewKeepCmd struct {
	ed          *Editor
	label       string
	changes     []dtReviewFileChange
	beforeTable *dataTableState
	beforeSaved []byte
	appliedOnce bool
}

func (c *dataTableReviewKeepCmd) Label() string { return c.label }

func (c *dataTableReviewKeepCmd) Do() {
	if c.appliedOnce {
		c.appliedOnce = false
		return
	}
	_ = c.apply(true)
}

func (c *dataTableReviewKeepCmd) Undo() {
	_ = c.apply(false)
}

func (c *dataTableReviewKeepCmd) apply(forward bool) error {
	e := c.ed
	if forward {
		written := 0
		rollback := func() {
			for i := written - 1; i >= 0; i-- {
				ch := c.changes[i]
				if ch.path == "" {
					continue
				}
				if ch.existedBefore {
					_ = writeFileAtomic(ch.path, ch.beforeBytes)
				} else {
					_ = os.Remove(ch.path)
				}
			}
			e.table = c.beforeTable.clone()
			e.tableSaved = bytes.Clone(c.beforeSaved)
		}
		for i, ch := range c.changes {
			if ch.path != "" {
				if err := os.MkdirAll(filepath.Dir(ch.path), 0o755); err != nil {
					rollback()
					return err
				}
				if err := writeFileAtomic(ch.path, ch.afterBytes); err != nil {
					rollback()
					return err
				}
			}
			written = i + 1
			if e.table != nil && pathsMatchReview(e, e.table.path, ch.path, ch.relPath) {
				e.table.doc = ch.afterDoc.Clone()
				e.table.lastErr = nil
				e.tableSaved = bytes.Clone(ch.afterBytes)
			}
		}
	} else {
		for i := len(c.changes) - 1; i >= 0; i-- {
			ch := c.changes[i]
			if ch.path != "" {
				if ch.existedBefore {
					if err := writeFileAtomic(ch.path, ch.beforeBytes); err != nil {
						return err
					}
				} else {
					_ = os.Remove(ch.path)
				}
			}
		}
		e.table = c.beforeTable.clone()
		e.tableSaved = bytes.Clone(c.beforeSaved)
	}
	e.filesRev++
	e.invalidateProjectIndex()
	e.snapshotProjectTree()
	return nil
}

func pathsMatchReview(e *Editor, openPath, absPath, relPath string) bool {
	if openPath == "" {
		return false
	}
	if absPath != "" && filepath.Clean(openPath) == filepath.Clean(absPath) {
		return true
	}
	return relPath != "" && e.dataTableMatchPath(openPath) == filepath.ToSlash(relPath)
}

type dataTableReviewRevertAllCmd struct {
	ed     *Editor
	label  string
	before *dataTableReviewState
}

func (c *dataTableReviewRevertAllCmd) Label() string { return c.label }

func (c *dataTableReviewRevertAllCmd) Do() {
	c.ed.tableReview = nil
	c.ed.scheduleAutoSaveIfNeeded()
}

func (c *dataTableReviewRevertAllCmd) Undo() {
	if c.before != nil {
		cp := c.before.clone()
		cp.rev++
		c.ed.tableReview = cp
		c.ed.cancelAutoSaveTimer()
	}
}

func (e *Editor) currentOS() core.OS {
	if e != nil && e.scope != nil && !e.scope.Disposed() {
		return e.scope.Env().OS()
	}
	return core.OSMacOS
}
