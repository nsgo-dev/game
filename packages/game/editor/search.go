package editor

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"go.yaml.in/yaml/v3"
)

// SearchKind classifies what kind of project entity a SearchResult matched.
type SearchKind string

const (
	// SearchFile matched a project file's name, path, or scene name.
	SearchFile SearchKind = "file"
	// SearchUID matched a file's stable UID header or project index UID.
	SearchUID SearchKind = "uid"
	// SearchObject matched a scene object's ID or display name.
	SearchObject SearchKind = "object"
	// SearchComponent matched a component type on a scene object or override.
	SearchComponent SearchKind = "component"
	// SearchPrefab matched a prefab name referenced by a scene object or override.
	SearchPrefab SearchKind = "prefab"
	// SearchSceneRef matched an instanced scene (object.scene) or inherited scene (inherits) path.
	SearchSceneRef SearchKind = "scene"
)

// SearchTab selects which view the Project Search and References panel displays.
type SearchTab int

const (
	// SearchTabSearch displays the project-wide search field and results.
	SearchTabSearch SearchTab = iota
	// SearchTabReferences displays incoming references to a selected project file.
	SearchTabReferences
)

// SearchResult is one hit returned by Editor.Search.
type SearchResult struct {
	// Kind says which kind of entity or field matched.
	Kind SearchKind
	// File is the slash-separated path of the file from the open project's root.
	File string
	// UID is the file's stable UID when present.
	UID string
	// Layer is the scene layer name for object-level hits.
	Layer string
	// ObjectID is the matching object's ID inside a scene file, or empty for file-level hits.
	ObjectID string
	// TopObjectID is the top-level ancestor object's ID selectable in the editor (equal to ObjectID for top-level objects).
	TopObjectID string
	// ObjectName is the matching object's display name when set.
	ObjectName string
	// Match is the specific matched token or value (such as a UID, component type, prefab, or scene path).
	Match string
	// Detail is a human-readable summary of why the result matched.
	Detail string
}

// projectSearchState holds the project search index, active search query/results,
// and Find References state for an Editor.
type projectSearchState struct {
	open        bool
	tab         SearchTab
	query       string
	results     []SearchResult
	refsTarget  string
	refsResults []refs.Ref
	loading     bool
	lastErr     error
	index       *projectSearchIndex
}

// projectSearchIndex caches the scanned project files, refs.Scan output, and
// parsed scene objects for the open project directory.
type projectSearchIndex struct {
	projectPath string
	rootDir     string
	fingerprint string
	dirty       bool
	scan        *refs.Project
	files       []indexedFile
	fileByPath  map[string]int
	sceneFiles  map[string]bool
	scenes      map[string]indexedScene
	dirs        map[string]bool
}

type indexedFile struct {
	path      string
	uid       string
	sceneName string
	inherits  string
}

type indexedScene struct {
	uid       string
	name      string
	inherits  string
	objects   []indexedObject
	overrides []indexedObject
}

type indexedObject struct {
	layer      string
	id         string
	topID      string
	name       string
	prefab     string
	scene      string
	components []string
	isOverride bool
}

// Search searches the open project for query across file names and paths, file UIDs,
// and inside scene files: scene names, inherits paths, object IDs and names,
// component types, and prefab or scene references. Matching is case-insensitive;
// space-separated terms must all match the same file or object. It returns
// ErrNoProject when no project is open.
func (e *Editor) Search(query string) ([]SearchResult, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, nil
	}
	ix, err := e.ensureProjectIndex(false)
	if err != nil {
		return nil, err
	}
	return e.queryProjectIndex(ix, trimmed), nil
}

// FindReferences returns every reference to target across the open project as
// refs.Scan reports them, preserving file, 1-based line, column, field and value.
// target is a slash-separated path from the project root (or a directory path,
// matching references to that directory or files inside it). It returns
// ErrNoProject when no project is open.
func (e *Editor) FindReferences(target string) ([]refs.Ref, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	clean := e.normalizeProjectPath(target)
	if clean == "" {
		return nil, nil
	}
	ix, err := e.ensureProjectIndex(false)
	if err != nil {
		return nil, err
	}
	return filterReferences(ix, clean), nil
}

// RefreshSearch rescans the open project from disk, refreshes the file browser
// listing and updates any active search or Find References results. It returns
// ErrNoProject when no project is open.
func (e *Editor) RefreshSearch() error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	e.filesRev++
	e.assetsRev++
	ix, err := e.ensureProjectIndex(true)
	e.search.loading = false
	e.search.lastErr = err
	if err != nil {
		e.notify()
		return err
	}
	e.recomputeActiveSearch(ix)
	e.notify()
	return nil
}

// SearchOpen reports whether the Project Search and References panel is open.
func (e *Editor) SearchOpen() bool {
	return e.search.open
}

// SetSearchOpen shows or hides the Project Search and References panel.
func (e *Editor) SetSearchOpen(open bool) {
	if open && (e.Project == nil || e.ProjectPath == "") {
		return
	}
	if e.search.open == open {
		return
	}
	e.search.open = open
	if open {
		if ix, err := e.ensureProjectIndex(false); err == nil {
			e.search.lastErr = nil
			e.recomputeActiveSearch(ix)
		} else {
			e.search.lastErr = err
		}
	}
	e.notify()
}

// SearchTab returns the active tab of the Project Search and References panel.
func (e *Editor) SearchTab() SearchTab {
	return e.search.tab
}

// SetSearchTab switches the Project Search and References panel between Search and References.
func (e *Editor) SetSearchTab(tab SearchTab) {
	if e.search.tab == tab {
		return
	}
	e.search.tab = tab
	e.notify()
}

// SearchQuery returns the current query string in the project search field.
func (e *Editor) SearchQuery() string {
	return e.search.query
}

// SetSearchQuery updates the project search field, opens the search panel on
// the Search tab when query is non-empty, runs Search, and notifies listeners.
func (e *Editor) SetSearchQuery(query string) error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	e.search.query = query
	e.search.tab = SearchTabSearch
	if strings.TrimSpace(query) != "" {
		e.search.open = true
	}
	results, err := e.Search(query)
	e.search.loading = false
	e.search.results = results
	e.search.lastErr = err
	e.notify()
	return err
}

// SearchResults returns a copy of the current search panel results.
func (e *Editor) SearchResults() []SearchResult {
	if len(e.search.results) == 0 {
		return nil
	}
	out := make([]SearchResult, len(e.search.results))
	copy(out, e.search.results)
	return out
}

// SearchErr returns the last error encountered while indexing or searching the project.
func (e *Editor) SearchErr() error {
	return e.search.lastErr
}

// SearchLoading reports whether the Search & References panel is currently scanning or searching.
func (e *Editor) SearchLoading() bool {
	return e != nil && e.search.loading
}

// SetSearchLoading updates the Search & References panel loading state and notifies listeners.
func (e *Editor) SetSearchLoading(loading bool) {
	if e == nil || e.search.loading == loading {
		return
	}
	e.search.loading = loading
	e.notify()
}

// ShowReferences runs FindReferences for target, opens the Search & References
// panel on the References tab with the results, and returns them.
func (e *Editor) ShowReferences(target string) ([]refs.Ref, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	clean := e.normalizeProjectPath(target)
	if clean == "" {
		e.search.loading = false
		e.search.refsTarget = ""
		e.search.refsResults = nil
		e.notify()
		return nil, nil
	}
	found, err := e.FindReferences(clean)
	e.search.loading = false
	e.search.lastErr = err
	if err != nil {
		e.notify()
		return nil, err
	}
	e.search.refsTarget = clean
	e.search.refsResults = found
	e.search.tab = SearchTabReferences
	e.search.open = true
	if len(found) == 0 {
		e.Status = "No references to " + clean
	} else {
		e.Status = fmt.Sprintf("Found %d reference(s) to %s", len(found), clean)
	}
	e.notify()
	return slices.Clone(found), nil
}

// ShowSelectedFileReferences runs ShowReferences for Editor.SelectedFile.
func (e *Editor) ShowSelectedFileReferences() ([]refs.Ref, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	return e.ShowReferences(e.SelectedFile)
}

// ReferencesTarget returns the slash-separated project path currently inspected
// in the Find References view, or empty when none is active.
func (e *Editor) ReferencesTarget() string {
	return e.search.refsTarget
}

// References returns a copy of the Find References results for ReferencesTarget.
func (e *Editor) References() []refs.Ref {
	if len(e.search.refsResults) == 0 {
		return nil
	}
	out := make([]refs.Ref, len(e.search.refsResults))
	copy(out, e.search.refsResults)
	return out
}

// ClearReferences clears the active Find References target and results.
func (e *Editor) ClearReferences() {
	if e.search.refsTarget == "" && len(e.search.refsResults) == 0 {
		return
	}
	e.search.refsTarget = ""
	e.search.refsResults = nil
	if e.search.tab == SearchTabReferences {
		e.search.tab = SearchTabSearch
	}
	e.notify()
}

// OpenSearchResult activates a search result: if it refers to a scene file (or an object inside one), it opens that scene when it is not already open (preserving unsaved edits when it is already open), updates ScenePath and SelectedFile, and selects the matching object, nested children included, or its top-level ancestor when the child cannot be selected. For any non-scene file, it selects the file in the file browser. It returns ErrNoProject when no project is open. Opening another scene goes through OpenScene, which clears OnSave and loses the open scene's unsaved edits; SearchPanelView asks first (HasUnsavedChanges).
func (e *Editor) OpenSearchResult(r SearchResult) error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if r.File == "" {
		return nil
	}
	if r.ObjectID != "" || e.isKnownSceneFile(r.File) {
		if !e.isOpenScene(r.File) {
			// OpenScene clears OnSave, which persisted the scene open before.
			if err := e.OpenScene(r.File); err != nil {
				e.selectFile(r.File)
				e.notify()
				return err
			}
		} else if e.ScenePath == "" {
			e.ScenePath = r.File
		}
		e.selectFile(r.File)
		if r.ObjectID != "" {
			selected := e.SelectObject(r.ObjectID)
			if !selected && r.TopObjectID != "" && r.TopObjectID != r.ObjectID {
				selected = e.SelectObject(r.TopObjectID)
			}
			if selected {
				e.Status = fmt.Sprintf("Opened %s (%s)", r.File, e.SelectedID)
				e.notify()
				return nil
			}
		}
		e.Status = "Opened " + r.File
		e.notify()
		return nil
	}
	e.selectFile(r.File)
	e.Status = "Selected " + r.File
	e.notify()
	return nil
}

// OpenReference navigates to a Find References result r: if r.File is a scene
// file, it opens that scene (unless already open), selects the top-level object
// at r.Line when one owns that line, and selects r.File in the file browser;
// otherwise it selects r.File in the file browser. It returns ErrNoProject when
// no project is open. Opening another scene goes through OpenScene, as
// OpenSearchResult does, and SearchPanelView asks first while edits are unsaved.
func (e *Editor) OpenReference(r refs.Ref) error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if r.File == "" {
		return nil
	}
	if e.isKnownSceneFile(r.File) {
		abs := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(r.File))
		if !e.isOpenScene(r.File) {
			// OpenScene clears OnSave, which persisted the scene open before.
			if err := e.OpenScene(r.File); err != nil {
				e.selectFile(r.File)
				e.notify()
				return err
			}
		} else if e.ScenePath == "" {
			e.ScenePath = r.File
		}
		e.selectFile(r.File)
		if r.Line > 0 {
			if data, err := os.ReadFile(abs); err == nil {
				if objID := sceneObjectAtLine(data, r.Line); objID != "" {
					e.SelectObject(objID)
				}
			}
			e.Status = fmt.Sprintf("Opened %s:%d", r.File, r.Line)
		} else {
			e.Status = "Opened " + r.File
		}
		e.notify()
		return nil
	}
	e.selectFile(r.File)
	if r.Line > 0 {
		e.Status = fmt.Sprintf("Selected %s:%d", r.File, r.Line)
	} else {
		e.Status = "Selected " + r.File
	}
	e.notify()
	return nil
}

// --- Index lifecycle & invalidation hooks ---

func (e *Editor) resetSearchForProject() {
	e.search = projectSearchState{}
}

func (e *Editor) invalidateProjectIndex() {
	if e.search.index != nil {
		e.search.index.dirty = true
	}
	if e.Project == nil || e.ProjectPath == "" {
		return
	}
	if e.search.query == "" && e.search.refsTarget == "" {
		return
	}
	ix, err := e.ensureProjectIndex(true)
	e.search.lastErr = err
	if err == nil {
		e.recomputeActiveSearch(ix)
	}
}

func (e *Editor) followSearchMove(forth func(string) string) {
	if e.search.refsTarget != "" {
		e.search.refsTarget = forth(e.search.refsTarget)
	}
	e.invalidateProjectIndex()
}

// refreshOpenSceneSearch updates active search results in memory after a scene
// edit (PlaceObject, DeleteObject, SetObjectName, SetComponentField, Undo/Redo)
// without re-reading disk.
func (e *Editor) refreshOpenSceneSearch() {
	if e.Project == nil || e.ProjectPath == "" || strings.TrimSpace(e.search.query) == "" {
		return
	}
	if e.search.index == nil || e.search.index.projectPath != e.ProjectPath || e.search.index.dirty {
		if ix, err := e.ensureProjectIndex(false); err == nil {
			e.search.lastErr = nil
			e.search.results = e.queryProjectIndex(ix, strings.TrimSpace(e.search.query))
		}
		return
	}
	e.search.results = e.queryProjectIndex(e.search.index, strings.TrimSpace(e.search.query))
}

func (e *Editor) recomputeActiveSearch(ix *projectSearchIndex) {
	if trimmed := strings.TrimSpace(e.search.query); trimmed != "" {
		e.search.results = e.queryProjectIndex(ix, trimmed)
	} else {
		e.search.results = nil
	}
	if e.search.refsTarget != "" {
		e.search.refsResults = filterReferences(ix, e.search.refsTarget)
	} else {
		e.search.refsResults = nil
	}
}

func (e *Editor) ensureProjectIndex(force bool) (*projectSearchIndex, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	rootDir := filepath.Dir(e.ProjectPath)
	fp, err := projectFingerprint(rootDir)
	if err != nil {
		return nil, err
	}
	ix := e.search.index
	if !force && ix != nil && !ix.dirty && ix.projectPath == e.ProjectPath && ix.fingerprint == fp {
		return ix, nil
	}
	rebuilt, err := buildProjectSearchIndex(e.ProjectPath, rootDir, e.Project, fp)
	if err != nil {
		return nil, err
	}
	e.search.index = rebuilt
	return rebuilt, nil
}

func (e *Editor) openSceneRelPath() string {
	if e.Project == nil || e.ProjectPath == "" || e.Doc == nil {
		return ""
	}
	if e.ScenePath != "" {
		return e.normalizeProjectPath(e.ScenePath)
	}
	if e.FilePath == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Dir(e.ProjectPath), e.FilePath)
	if err != nil {
		return ""
	}
	slash := filepath.ToSlash(rel)
	if slash == "." || slash == ".." || strings.HasPrefix(slash, "../") {
		return ""
	}
	return slash
}

func (e *Editor) isOpenScene(relPath string) bool {
	open := e.openSceneRelPath()
	return open != "" && open == relPath
}

func (e *Editor) isKnownSceneFile(relPath string) bool {
	if isProjectScene(e.Project, relPath) {
		return true
	}
	if e.search.index != nil && e.search.index.sceneFiles[relPath] {
		return true
	}
	if ix, err := e.ensureProjectIndex(false); err == nil {
		return ix.sceneFiles[relPath]
	}
	return false
}

func (e *Editor) normalizeProjectPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) && e.ProjectPath != "" {
		if rel, err := filepath.Rel(filepath.Dir(e.ProjectPath), p); err == nil {
			p = rel
		}
	}
	p = filepath.ToSlash(p)
	p = path.Clean(p)
	if p == "." || p == "/" || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return strings.TrimPrefix(p, "/")
}

// --- Indexing implementation ---

// projectFingerprint walks rootDir (stat only, no file reads) skipping the same
// directories as refs.Scan and ProjectFiles, returning a deterministic summary
// of every file's path, size and modification time.
func projectFingerprint(rootDir string) (string, error) {
	fsys := os.DirFS(rootDir)
	var b strings.Builder
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." {
			return nil
		}
		if name := d.Name(); d.IsDir() && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
			return fs.SkipDir
		}
		if d.IsDir() {
			b.WriteString("D:")
			b.WriteString(p)
			b.WriteByte('\n')
			return nil
		}
		info, infoErr := d.Info()
		b.WriteString("F:")
		b.WriteString(p)
		if infoErr == nil {
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(info.Size(), 10))
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

func buildProjectSearchIndex(projectPath, rootDir string, proj *project.File, fp string) (*projectSearchIndex, error) {
	fsys := os.DirFS(rootDir)
	scan, err := refs.Scan(fsys)
	if err != nil {
		return nil, err
	}
	ix := &projectSearchIndex{
		projectPath: projectPath,
		rootDir:     rootDir,
		fingerprint: fp,
		scan:        scan,
		fileByPath:  make(map[string]int),
		sceneFiles:  make(map[string]bool),
		scenes:      make(map[string]indexedScene),
		dirs:        make(map[string]bool),
	}

	// Map UIDs and scene kinds discovered by refs.Scan.
	uids := make(map[string]string, len(scan.Files))
	for _, f := range scan.Files {
		if f.UID != "" {
			uids[f.Path] = f.UID
		}
		if f.Kind == refs.KindScene {
			ix.sceneFiles[f.Path] = true
		}
	}
	if rootIdx := scan.Index("."); rootIdx != nil {
		for _, entry := range rootIdx.Files {
			if entry.UID != "" && uids[entry.Path] == "" {
				uids[entry.Path] = entry.UID
			}
		}
	}

	// Collect all project files and directories matching ProjectFiles / refs.Scan walk rules.
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." {
			return nil
		}
		if name := d.Name(); d.IsDir() && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
			return fs.SkipDir
		}
		if d.IsDir() {
			ix.dirs[p] = true
			return nil
		}
		if isProjectScene(proj, p) {
			ix.sceneFiles[p] = true
		}
		ix.fileByPath[p] = len(ix.files)
		ix.files = append(ix.files, indexedFile{
			path: p,
			uid:  uids[p],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Parse scene files to index scene metadata, objects, components, prefabs and scene references.
	for i := range ix.files {
		f := &ix.files[i]
		if !ix.sceneFiles[f.path] {
			continue
		}
		data, err := fs.ReadFile(fsys, f.path)
		if err != nil {
			continue
		}
		sc := indexSceneBytes(data)
		if sc.uid != "" && f.uid == "" {
			f.uid = sc.uid
		}
		f.sceneName = sc.name
		f.inherits = sc.inherits
		ix.scenes[f.path] = sc
	}

	return ix, nil
}

func indexSceneBytes(data []byte) indexedScene {
	if doc, err := sceneformat.Parse(data); err == nil {
		return indexSceneDoc(doc)
	}
	// Fallback for valid YAML scenes that currently fail strict sceneformat validation.
	return indexSceneYAML(data)
}

func indexSceneDoc(doc *sceneformat.SceneDoc) indexedScene {
	if doc == nil {
		return indexedScene{}
	}
	sc := indexedScene{
		uid:      doc.UID,
		name:     doc.Name,
		inherits: doc.Inherits,
	}
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		for oi := range layer.Objects {
			top := &layer.Objects[oi]
			topID := top.ID
			sceneformat.WalkObjects(layer.Objects[oi:oi+1], func(o, _ *sceneformat.ObjectDoc) bool {
				comps := collectObjectComponents(o)
				sc.objects = append(sc.objects, indexedObject{
					layer:      layer.Name,
					id:         o.ID,
					topID:      topID,
					name:       o.Name,
					prefab:     o.Prefab,
					scene:      o.Scene,
					components: comps,
				})
				return true
			})
		}
	}
	if len(doc.Overrides) > 0 {
		ids := make([]string, 0, len(doc.Overrides))
		for id := range doc.Overrides {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			ov := doc.Overrides[id]
			var name, prefab string
			if ov.Name != nil {
				name = *ov.Name
			}
			if ov.Prefab != nil {
				prefab = *ov.Prefab
			}
			comps := sortedMapKeys(ov.Components)
			if name == "" && prefab == "" && len(comps) == 0 {
				continue
			}
			sc.overrides = append(sc.overrides, indexedObject{
				id:         id,
				name:       name,
				prefab:     prefab,
				components: comps,
				isOverride: true,
			})
		}
	}
	return sc
}

func collectObjectComponents(o *sceneformat.ObjectDoc) []string {
	if o == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(o.Components))
	for k := range o.Components {
		seen[k] = struct{}{}
	}
	for _, ov := range o.Overrides {
		for k := range ov.Components {
			seen[k] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func indexSceneYAML(data []byte) indexedScene {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return indexedScene{}
	}
	doc := yamlDeref(root.Content[0])
	if doc == nil || doc.Kind != yaml.MappingNode {
		return indexedScene{}
	}
	sc := indexedScene{
		uid:      yamlFieldString(doc, "uid"),
		name:     yamlFieldString(doc, "name"),
		inherits: yamlFieldString(doc, "inherits"),
	}
	layers := yamlField(doc, "layers")
	if layers != nil && layers.Kind == yaml.SequenceNode {
		for _, ln := range layers.Content {
			ln = yamlDeref(ln)
			if ln == nil || ln.Kind != yaml.MappingNode {
				continue
			}
			layerName := yamlFieldString(ln, "name")
			objs := yamlField(ln, "objects")
			if objs == nil || objs.Kind != yaml.SequenceNode {
				continue
			}
			for _, on := range objs.Content {
				on = yamlDeref(on)
				if on == nil || on.Kind != yaml.MappingNode {
					continue
				}
				topID := yamlFieldString(on, "id")
				collectYAMLObjects(on, layerName, topID, 0, &sc.objects)
			}
		}
	}
	return sc
}

func collectYAMLObjects(n *yaml.Node, layer, topID string, depth int, out *[]indexedObject) {
	if n == nil || n.Kind != yaml.MappingNode || depth > 128 {
		return
	}
	id := yamlFieldString(n, "id")
	var comps []string
	if cm := yamlField(n, "components"); cm != nil && cm.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(cm.Content); i += 2 {
			comps = append(comps, cm.Content[i].Value)
		}
		slices.Sort(comps)
	}
	if id != "" || len(comps) > 0 {
		*out = append(*out, indexedObject{
			layer:      layer,
			id:         id,
			topID:      topID,
			name:       yamlFieldString(n, "name"),
			prefab:     yamlFieldString(n, "prefab"),
			scene:      yamlFieldString(n, "scene"),
			components: comps,
		})
	}
	if children := yamlField(n, "children"); children != nil && children.Kind == yaml.SequenceNode {
		for _, ch := range children.Content {
			collectYAMLObjects(yamlDeref(ch), layer, topID, depth+1, out)
		}
	}
}

// --- Query matching & ranking ---

type rankedResult struct {
	rank int
	res  SearchResult
}

func (e *Editor) queryProjectIndex(ix *projectSearchIndex, query string) []SearchResult {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}

	e.syncActiveTab()
	openScenes := make(map[string]indexedScene, len(e.tabs))
	var openOrder []string
	for i, t := range e.tabs {
		if t.doc == nil {
			continue
		}
		var rel string
		if i == e.activeTab {
			rel = e.openSceneRelPath()
		} else if t.scenePath != "" {
			rel = e.normalizeProjectPath(t.scenePath)
		} else if t.filePath != "" {
			rel = e.normalizeProjectPath(t.filePath)
		}
		if rel == "" {
			continue
		}
		if _, dup := openScenes[rel]; !dup {
			openOrder = append(openOrder, rel)
		}
		openScenes[rel] = indexSceneDoc(t.doc)
	}

	var hits []rankedResult

	// Evaluate every indexed project file (and overlay live Doc metadata for every open tab).
	seenOpen := make(map[string]bool, len(openScenes))
	for _, f := range ix.files {
		cur := f
		openScene, isOpen := openScenes[cur.path]
		if isOpen {
			seenOpen[cur.path] = true
			if openScene.uid != "" {
				cur.uid = openScene.uid
			}
			cur.sceneName = openScene.name
			cur.inherits = openScene.inherits
		}
		if rr, ok := matchFile(cur, terms); ok {
			hits = append(hits, rr)
		}
		if ix.sceneFiles[cur.path] || isOpen {
			sc := ix.scenes[cur.path]
			if isOpen {
				sc = openScene
			}
			for _, obj := range sc.objects {
				if rr, ok := matchObject(cur.path, cur.uid, obj, terms); ok {
					hits = append(hits, rr)
				}
			}
			for _, ov := range sc.overrides {
				if rr, ok := matchObject(cur.path, cur.uid, ov, terms); ok {
					hits = append(hits, rr)
				}
			}
		}
	}

	// Include any open tab with a project-relative path that has not been saved to disk yet.
	for _, openRel := range openOrder {
		if seenOpen[openRel] {
			continue
		}
		openScene := openScenes[openRel]
		cur := indexedFile{
			path:      openRel,
			uid:       openScene.uid,
			sceneName: openScene.name,
			inherits:  openScene.inherits,
		}
		if rr, ok := matchFile(cur, terms); ok {
			hits = append(hits, rr)
		}
		for _, obj := range openScene.objects {
			if rr, ok := matchObject(openRel, openScene.uid, obj, terms); ok {
				hits = append(hits, rr)
			}
		}
		for _, ov := range openScene.overrides {
			if rr, ok := matchObject(openRel, openScene.uid, ov, terms); ok {
				hits = append(hits, rr)
			}
		}
	}

	slices.SortFunc(hits, func(a, b rankedResult) int {
		return cmp.Or(
			cmp.Compare(a.rank, b.rank),
			cmp.Compare(a.res.File, b.res.File),
			cmp.Compare(a.res.ObjectID, b.res.ObjectID),
			cmp.Compare(a.res.Kind, b.res.Kind),
			cmp.Compare(a.res.Detail, b.res.Detail),
		)
	})

	out := make([]SearchResult, len(hits))
	for i := range hits {
		out[i] = hits[i].res
	}
	return out
}

func matchFile(f indexedFile, terms []string) (rankedResult, bool) {
	pathLow := strings.ToLower(f.path)
	baseLow := strings.ToLower(path.Base(f.path))
	uidLow := strings.ToLower(f.uid)
	nameLow := strings.ToLower(f.sceneName)
	inhLow := strings.ToLower(f.inherits)

	// Every query term must appear in at least one searchable field of this file.
	for _, t := range terms {
		if !strings.Contains(pathLow, t) &&
			(uidLow == "" || !strings.Contains(uidLow, t)) &&
			(nameLow == "" || !strings.Contains(nameLow, t)) &&
			(inhLow == "" || !strings.Contains(inhLow, t)) {
			return rankedResult{}, false
		}
	}

	first := terms[0]
	var kind SearchKind
	var match, detail string
	rank := 2

	switch {
	case uidLow != "" && strings.Contains(uidLow, first) && !strings.Contains(pathLow, first):
		kind = SearchUID
		match = f.uid
		detail = "uid: " + f.uid
		rank = matchRank(uidLow, first)
	case strings.Contains(pathLow, first):
		kind = SearchFile
		match = f.path
		if f.uid != "" {
			detail = "uid: " + f.uid
		} else {
			detail = f.path
		}
		rank = min(matchRank(baseLow, first), matchRank(pathLow, first))
	case uidLow != "" && strings.Contains(uidLow, first):
		kind = SearchUID
		match = f.uid
		detail = "uid: " + f.uid
		rank = matchRank(uidLow, first)
	case inhLow != "" && strings.Contains(inhLow, first):
		kind = SearchSceneRef
		match = f.inherits
		detail = "inherits: " + f.inherits
		rank = matchRank(inhLow, first)
	default:
		kind = SearchFile
		match = f.sceneName
		detail = "scene: " + f.sceneName
		rank = matchRank(nameLow, first)
	}

	return rankedResult{
		rank: rank,
		res: SearchResult{
			Kind:   kind,
			File:   f.path,
			UID:    f.uid,
			Match:  match,
			Detail: detail,
		},
	}, true
}

func matchObject(file, fileUID string, o indexedObject, terms []string) (rankedResult, bool) {
	fileLow := strings.ToLower(file)
	idLow := strings.ToLower(o.id)
	nameLow := strings.ToLower(o.name)
	prefabLow := strings.ToLower(o.prefab)
	sceneLow := strings.ToLower(o.scene)
	layerLow := strings.ToLower(o.layer)

	compsLow := make([]string, len(o.components))
	for i, c := range o.components {
		compsLow[i] = strings.ToLower(c)
	}

	matchedObjField := false
	for _, t := range terms {
		inID := idLow != "" && strings.Contains(idLow, t)
		inName := nameLow != "" && strings.Contains(nameLow, t)
		inPrefab := prefabLow != "" && strings.Contains(prefabLow, t)
		inScene := sceneLow != "" && strings.Contains(sceneLow, t)
		inComp := slices.ContainsFunc(compsLow, func(c string) bool { return strings.Contains(c, t) })
		if inID || inName || inPrefab || inScene || inComp {
			matchedObjField = true
			continue
		}
		// Allow secondary terms to narrow by file path or layer, as long as at least one term matched an object field.
		if !strings.Contains(fileLow, t) && (layerLow == "" || !strings.Contains(layerLow, t)) {
			return rankedResult{}, false
		}
	}
	if !matchedObjField {
		return rankedResult{}, false
	}

	// Pick the highest-priority matched object field across the query terms.
	var kind SearchKind
	var match, detail string
	rank := 3

	update := func(r int, k SearchKind, m, d string) {
		if r < rank || (r == rank && kind == "") {
			rank, kind, match, detail = r, k, m, d
		}
	}

	for _, t := range terms {
		if idLow != "" && strings.Contains(idLow, t) {
			d := "id: " + o.id
			if o.name != "" {
				d = fmt.Sprintf("id: %s (%s)", o.id, o.name)
			}
			update(matchRank(idLow, t), SearchObject, o.id, d)
		}
		if nameLow != "" && strings.Contains(nameLow, t) {
			update(matchRank(nameLow, t), SearchObject, o.name, "name: "+o.name)
		}
		for i, cl := range compsLow {
			if strings.Contains(cl, t) {
				c := o.components[i]
				update(matchRank(cl, t), SearchComponent, c, "component: "+c)
			}
		}
		if prefabLow != "" && strings.Contains(prefabLow, t) {
			update(matchRank(prefabLow, t), SearchPrefab, o.prefab, "prefab: "+o.prefab)
		}
		if sceneLow != "" && strings.Contains(sceneLow, t) {
			update(matchRank(sceneLow, t), SearchSceneRef, o.scene, "scene: "+o.scene)
		}
	}

	return rankedResult{
		rank: rank,
		res: SearchResult{
			Kind:        kind,
			File:        file,
			UID:         fileUID,
			Layer:       o.layer,
			ObjectID:    o.id,
			TopObjectID: o.topID,
			ObjectName:  o.name,
			Match:       match,
			Detail:      detail,
		},
	}, true
}

func matchRank(haystack, needle string) int {
	if haystack == needle {
		return 0
	}
	if strings.HasPrefix(haystack, needle) {
		return 1
	}
	return 2
}

func filterReferences(ix *projectSearchIndex, target string) []refs.Ref {
	if ix == nil || ix.scan == nil || target == "" {
		return nil
	}
	isDir := ix.dirs[target]
	prefix := target + "/"
	var out []refs.Ref
	for _, r := range ix.scan.Refs {
		if r.Target == target || (isDir && strings.HasPrefix(r.Target, prefix)) {
			out = append(out, r)
		}
	}
	return out
}

// --- YAML line helpers ---

// sceneObjectAtLine returns the ID of the top-level object in the YAML scene
// document data whose span contains the 1-based line number, or "" when line
// does not lie inside any top-level object.
func sceneObjectAtLine(data []byte, line int) string {
	if line <= 0 {
		return ""
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return ""
	}
	doc := yamlDeref(root.Content[0])
	if doc == nil || doc.Kind != yaml.MappingNode {
		return ""
	}
	layers := yamlField(doc, "layers")
	if layers == nil || layers.Kind != yaml.SequenceNode {
		return ""
	}
	for _, ln := range layers.Content {
		ln = yamlDeref(ln)
		if ln == nil || ln.Kind != yaml.MappingNode {
			continue
		}
		objs := yamlField(ln, "objects")
		if objs == nil || objs.Kind != yaml.SequenceNode {
			continue
		}
		for _, on := range objs.Content {
			on = yamlDeref(on)
			if on == nil || on.Kind != yaml.MappingNode {
				continue
			}
			minL, maxL := yamlLineBounds(on)
			if minL <= line && line <= maxL {
				return yamlFieldString(on, "id")
			}
		}
	}
	return ""
}

func yamlLineBounds(n *yaml.Node) (minL, maxL int) {
	if n == nil {
		return 0, 0
	}
	minL, maxL = n.Line, n.Line
	var walk func(cur *yaml.Node, depth int)
	walk = func(cur *yaml.Node, depth int) {
		if cur == nil || depth > 128 {
			return
		}
		if cur.Line > 0 {
			if minL == 0 || cur.Line < minL {
				minL = cur.Line
			}
			if cur.Line > maxL {
				maxL = cur.Line
			}
		}
		for _, c := range cur.Content {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return minL, maxL
}

func yamlDeref(n *yaml.Node) *yaml.Node {
	for range 128 {
		if n == nil || n.Kind != yaml.AliasNode {
			return n
		}
		n = n.Alias
	}
	return nil
}

func yamlField(n *yaml.Node, key string) *yaml.Node {
	n = yamlDeref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return yamlDeref(n.Content[i+1])
		}
	}
	return nil
}

func yamlFieldString(n *yaml.Node, key string) string {
	v := yamlField(n, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}
