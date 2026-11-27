package refs

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/resource"
	"go.yaml.in/yaml/v3"
)

// Plan is a set of changes to a project's files that keeps its references working: files to move, then files to write. A Plan only describes them; Apply makes them.
type Plan struct {
	// Moves are the files and directories to move, in order.
	Moves []Move
	// Writes are the files to write after the moves, at their paths after the moves, in path order.
	Writes []Write
	// Updated are the references the writes change, as they were before the plan.
	Updated []Ref
	// UpdatedTextKeys are the localization text key references and definitions the writes change, as they were before the plan.
	UpdatedTextKeys []TextKeyRef
	// Assigned are the scene, screen and resource files the writes give a new uid, at their paths after the moves.
	Assigned []string
}

// Move moves the file or directory From to To, both slash-separated paths in the project.
type Move struct {
	From, To string
}

// Write replaces the contents of the file at Path, or creates it, with Data.
type Write struct {
	Path string
	Data []byte
}

// Empty reports whether the plan changes nothing.
func (pl *Plan) Empty() bool { return len(pl.Moves) == 0 && len(pl.Writes) == 0 }

// Apply makes the plan's changes in dir, the directory the plan's paths are relative to, which must be the one the planning function scanned: each move, creating the directories it moves into, and then each write, through a temporary file renamed over the old one so a failed write never leaves a file half written. It works inside an os.Root, so no path leads outside dir. A move comes first so that when Apply stops part way, the project index still lists the moved files where they were, and game index finds them and finishes the rewrite.
func (pl *Plan) Apply(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("refs: %w", err)
	}
	defer root.Close()
	for _, m := range pl.Moves {
		if d := path.Dir(m.To); d != "." {
			if err := root.MkdirAll(d, 0o755); err != nil {
				return fmt.Errorf("refs: %w", err)
			}
		}
		if toInfo, err := root.Lstat(m.To); err == nil {
			fromInfo, ferr := root.Lstat(m.From)
			if ferr != nil || !strings.EqualFold(m.From, m.To) || !os.SameFile(fromInfo, toInfo) {
				return fmt.Errorf("refs: move %s to %s: %s already exists", m.From, m.To, m.To)
			}
			// A rename that changes only the case of the name, on a file system that ignores case, where To already names From: go through a temporary name, so that the new case is kept.
			tmp, err := tempName(root, m.From)
			if err != nil {
				return fmt.Errorf("refs: %w", err)
			}
			if err := root.Rename(m.From, tmp); err != nil {
				return fmt.Errorf("refs: %w", err)
			}
			// Where To is a second link to the file rather than the same name, it is still there, and a rename onto a link of the same file does nothing; drop that link first.
			if toInfo, err := root.Lstat(m.To); err == nil {
				if tmpInfo, err := root.Lstat(tmp); err == nil && os.SameFile(toInfo, tmpInfo) {
					if err := root.Remove(m.To); err != nil {
						return fmt.Errorf("refs: %w", err)
					}
				}
			}
			if err := root.Rename(tmp, m.To); err != nil {
				return fmt.Errorf("refs: %w", err)
			}
			continue
		}
		if err := root.Rename(m.From, m.To); err != nil {
			return fmt.Errorf("refs: %w", err)
		}
	}
	for _, w := range pl.Writes {
		if err := writeAtomic(root, w.Path, w.Data); err != nil {
			return fmt.Errorf("refs: write %s: %w", w.Path, err)
		}
	}
	return nil
}

// tempName returns an unused name beside name in root to move name through.
func tempName(root *os.Root, name string) (string, error) {
	for i := range 100 {
		tmp := path.Join(path.Dir(name), fmt.Sprintf(".%s.%d.move", path.Base(name), i))
		if _, err := root.Lstat(tmp); errors.Is(err, fs.ErrNotExist) {
			return tmp, nil
		}
	}
	return "", fmt.Errorf("no unused name to move %s through", name)
}

// writeAtomic writes data to a temporary file beside name in root and renames it over name, keeping name's permissions when it exists.
func writeAtomic(root *os.Root, name string, data []byte) error {
	perm := fs.FileMode(0o644)
	if info, err := root.Stat(name); err == nil {
		perm = info.Mode().Perm()
	}
	var tmp string
	var f *os.File
	for i := 0; ; i++ {
		tmp = path.Join(path.Dir(name), fmt.Sprintf(".%s.%d.tmp", path.Base(name), i))
		var err error
		f, err = root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i >= 100 {
			return err
		}
	}
	_, err := f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}

// planner gathers the edits of one plan.
type planner struct {
	p *Project
	// mapPath maps a path before the plan's moves to its path after them.
	mapPath func(string) string
	edits   map[string][]edit
	plan    Plan
	// keep are index entries of files still gone after the plan, by root, which the new index keeps so Moved can still find them.
	keep map[string][]IndexEntry
	// fixed marks the references the plan rewrites, by file and offset.
	fixed map[refKey]bool
	// retarget is the file each reference game index points at a moved file now names, by file and offset.
	retarget map[refKey]string
}

// refKey identifies a reference by its file and where its value starts.
type refKey struct {
	file  string
	start int
}

func newPlanner(p *Project, mapPath func(string) string) *planner {
	return &planner{p: p, mapPath: mapPath, edits: make(map[string][]edit), keep: make(map[string][]IndexEntry), fixed: make(map[refKey]bool), retarget: make(map[refKey]string)}
}

// rewrite plans setting the reference r, in the file now at file, to name target.
func (pl *planner) rewrite(r Ref, file, target string) error {
	v := pl.p.valueFor(r, file, target)
	if v == r.Value {
		return nil
	}
	if !r.FromFile && pl.p.Root(target) != pl.p.Root(r.File) {
		return fmt.Errorf("%s:%d: %s %q would name %s, outside the project root %s", r.File, r.Line, r.Field, r.Value, target, pl.p.Root(r.File))
	}
	if r.span.start < 0 {
		return fmt.Errorf("%s:%d: %s %q is not written as a plain or quoted value on one line, so it cannot be rewritten in place; write it on one line and try again", r.File, r.Line, r.Field, r.Value)
	}
	if pl.fixed[refKey{r.File, r.span.start}] {
		return nil
	}
	pl.fixed[refKey{r.File, r.span.start}] = true
	pl.edits[r.File] = append(pl.edits[r.File], edit{r.span.start, r.span.end, formatValue(r.span.syntax, v)})
	pl.plan.Updated = append(pl.plan.Updated, r)
	return nil
}

// assignUID plans giving the scene or resource file f the uid id: replacing the one it has, filling in an empty one, or adding a line after its version.
func (pl *planner) assignUID(f *File, id string) error {
	if f.uid != nil {
		s, err := yamlSpan(f.data, f.starts, f.uid)
		switch {
		case err == nil:
			pl.edits[f.Path] = append(pl.edits[f.Path], edit{s.start, s.end, formatValue(s.syntax, id)})
		case f.uid.Value == "" && f.uid.Style == 0:
			// "uid:" with nothing after it: write the value after the colon.
			at, aerr := afterKey(f, f.uidKey)
			if aerr != nil {
				return fmt.Errorf("%s:%d: uid: %w", f.Path, f.uidKey.Line, aerr)
			}
			pl.edits[f.Path] = append(pl.edits[f.Path], edit{at, at, " " + formatValue(syntaxPlain, id)})
		default:
			return fmt.Errorf("%s:%d: uid: %w", f.Path, f.uid.Line, err)
		}
	} else {
		at, text, err := uidLine(f, id)
		if err != nil {
			return err
		}
		pl.edits[f.Path] = append(pl.edits[f.Path], edit{at, at, text})
	}
	f.UID = id
	pl.plan.Assigned = append(pl.plan.Assigned, pl.mapPath(f.Path))
	return nil
}

// afterKey returns the offset just after the colon that ends the plain key k of f, written on one line.
func afterKey(f *File, k *yaml.Node) (int, error) {
	if k == nil || k.Style != 0 {
		return 0, errors.New("the key is not written plain")
	}
	o := offset(f.data, f.starts, k.Line, k.Column)
	if o < 0 || !bytes.HasPrefix(f.data[o:], []byte(k.Value)) {
		return 0, errors.New("the key is not where the parser put it")
	}
	o += len(k.Value)
	for o < len(f.data) && f.data[o] == ' ' {
		o++
	}
	if o >= len(f.data) || f.data[o] != ':' {
		return 0, errors.New("no colon after the key")
	}
	return o + 1, nil
}

// uidLine returns where to add the line giving f the uid id, the line after its version key, and the line itself, indented as that key is and ended as that line is.
func uidLine(f *File, id string) (int, string, error) {
	doc := f.doc
	if doc.Style&yaml.FlowStyle != 0 {
		return 0, "", fmt.Errorf("%s: the document is a flow mapping; write it in block style to give it a uid", f.Path)
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		k, v := doc.Content[i], doc.Content[i+1]
		if k.Value != "version" {
			continue
		}
		if v.Kind != yaml.ScalarNode || v.Line != k.Line {
			break
		}
		line := k.Line
		if line < len(f.starts) {
			at := f.starts[line]
			br := "\n"
			if at >= 2 && f.data[at-2] == '\r' && f.data[at-1] == '\n' {
				br = "\r\n"
			} else if f.data[at-1] == '\r' {
				br = "\r"
			}
			return at, strings.Repeat(" ", k.Column-1) + "uid: " + formatValue(syntaxPlain, id) + br, nil
		}
		return len(f.data), "\n" + strings.Repeat(" ", k.Column-1) + "uid: " + formatValue(syntaxPlain, id) + "\n", nil
	}
	return 0, "", fmt.Errorf("%s: no version on a line of its own to add the uid after", f.Path)
}

// finish turns the planned edits into writes, adds each project root's new index, and returns the plan.
func (pl *planner) finish() (*Plan, error) {
	edited := make(map[string][]byte, len(pl.edits))
	for _, name := range sortedKeys(pl.edits) {
		f := pl.p.files[name]
		data := applyEdits(f.data, pl.edits[name])
		edited[name] = data
		pl.plan.Writes = append(pl.plan.Writes, Write{Path: pl.mapPath(name), Data: data})
	}
	writes, err := pl.indexes(edited)
	if err != nil {
		return nil, err
	}
	pl.plan.Writes = append(pl.plan.Writes, writes...)
	slices.SortFunc(pl.plan.Writes, func(a, b Write) int { return cmp.Compare(a.Path, b.Path) })
	slices.Sort(pl.plan.Assigned)
	return &pl.plan, nil
}

// indexes returns the writes that bring each project root's index up to date after the plan, whose edits leave the files in edited with new contents: one for every root that has an index or has a file to list, when it changes.
func (pl *planner) indexes(edited map[string][]byte) ([]Write, error) {
	p := pl.p
	entries := make(map[string]map[string]IndexEntry)
	addEntry := func(name string, e IndexEntry) {
		root := p.Root(name)
		if entries[root] == nil {
			entries[root] = make(map[string]IndexEntry)
		}
		e.Path = strings.TrimPrefix(name, rootPrefix(root))
		entries[root][e.Path] = e
	}
	uidFiles := make(map[string]bool)
	for _, f := range p.Files {
		if f.UID != "" && p.exists[f.Path] {
			addEntry(pl.mapPath(f.Path), IndexEntry{UID: f.UID})
			uidFiles[f.Path] = true
		}
	}
	for _, r := range p.Refs {
		if t, ok := pl.retarget[refKey{r.File, r.span.start}]; ok {
			r.Target = t
		}
		// A folder has no contents to hash, and what a tool writes changes with every build, so neither is listed.
		if r.Target == "" || r.Dir || r.Output {
			continue
		}
		if !p.exists[r.Target] {
			if pl.fixed[refKey{r.File, r.span.start}] {
				continue
			}
			if e, ok := p.indexes[p.Root(r.Target)].Lookup(strings.TrimPrefix(r.Target, rootPrefix(p.Root(r.Target)))); ok {
				pl.keep[p.Root(r.Target)] = append(pl.keep[p.Root(r.Target)], e)
			}
			continue
		}
		if uidFiles[r.Target] {
			continue
		}
		if id := p.pendingUID(r.Target); id != "" {
			addEntry(pl.mapPath(r.Target), IndexEntry{UID: id})
			continue
		}
		var h string
		if data, ok := edited[r.Target]; ok {
			h = hashContent(r.Target, data)
		} else {
			var err error
			if h, err = p.sha(r.Target); err != nil {
				return nil, fmt.Errorf("refs: %w", err)
			}
		}
		addEntry(pl.mapPath(r.Target), IndexEntry{SHA256: h})
	}
	roots := make(map[string]bool)
	for root := range entries {
		roots[root] = true
	}
	for root := range p.indexes {
		roots[root] = true
	}
	for root, kept := range pl.keep {
		for _, e := range kept {
			if _, taken := entries[root][e.Path]; !taken {
				if entries[root] == nil {
					entries[root] = make(map[string]IndexEntry)
				}
				if e.UID != "" && slices.ContainsFunc(sortedValues(entries[root]), func(o IndexEntry) bool { return o.UID == e.UID }) {
					continue
				}
				entries[root][e.Path] = e
			}
		}
	}
	var writes []Write
	for _, root := range sortedKeys(roots) {
		ix := &Index{Version: IndexVersion, Files: sortedValues(entries[root])}
		data, err := EncodeIndex(ix)
		if err != nil {
			return nil, err
		}
		name := path.Join(root, IndexFileName)
		if old, err := fs.ReadFile(p.fsys, name); err == nil && bytes.Equal(old, data) {
			continue
		}
		writes = append(writes, Write{Path: name, Data: data})
	}
	return writes, nil
}

func compareRefs(a, b Ref) int {
	return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.span.start, b.span.start), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func sortedValues(m map[string]IndexEntry) []IndexEntry {
	out := make([]IndexEntry, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, m[k])
	}
	return out
}

// PlanMove plans moving the file or directory from to to, both slash-separated paths in fsys such as os.DirFS of a game's directory, and rewriting every reference Scan finds to what it moves: references from other files, installed pack folders in game.yaml, and the references a moved Tiled or LDtk file makes relative to itself. It also brings the project index at the root up to date. It refuses a move that would leave a reference it cannot rewrite: one out of the project root, one written in a way it cannot rewrite in place, one in a file that should name from but does not parse, or moving a tracked file or subfolder inside an installed pack without moving the pack's folder. It refuses to move game.yaml, an index, or a directory holding a project root of its own.
func PlanMove(fsys fs.FS, from, to string) (*Plan, error) {
	for _, s := range []string{from, to} {
		if !validPath(s) {
			return nil, fmt.Errorf("refs: %q must be a slash-separated path inside the project", s)
		}
	}
	if from == to {
		return nil, fmt.Errorf("refs: %s is already at %s", from, to)
	}
	if strings.HasPrefix(to, from+"/") {
		return nil, fmt.Errorf("refs: cannot move %s into itself", from)
	}
	info, err := fs.Stat(fsys, from)
	if err != nil {
		return nil, fmt.Errorf("refs: %w", err)
	}
	if toInfo, err := fs.Stat(fsys, to); err == nil {
		// On a file system that ignores case, to names from itself when only the case differs; that rename is allowed.
		if !strings.EqualFold(from, to) || !os.SameFile(info, toInfo) {
			return nil, fmt.Errorf("refs: %s already exists", to)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("refs: %w", err)
	}
	p, err := Scan(fsys)
	if err != nil {
		return nil, err
	}
	if len(p.Problems) > 0 {
		return nil, fmt.Errorf("refs: fix these first:\n%s", problemList(p.Problems))
	}
	if base := path.Base(from); !info.IsDir() && (base == project.FileName || base == IndexFileName) || path.Base(to) == IndexFileName || path.Base(to) == project.FileName {
		return nil, fmt.Errorf("refs: cannot move %s to %s: game.yaml and %s stay at the project root", from, to, IndexFileName)
	}
	if p.Root(from) != p.Root(to) {
		return nil, fmt.Errorf("refs: cannot move %s out of its project root %s", from, p.Root(from))
	}
	if info.IsDir() {
		for _, name := range p.paths {
			if strings.HasPrefix(name, from+"/") {
				switch path.Base(name) {
				case project.FileName, project.NSGoFileName, "go.mod", IndexFileName:
					return nil, fmt.Errorf("refs: cannot move %s: it holds %s, a project of its own", from, name)
				}
			}
		}
	}
	for _, pk := range p.packs {
		if !strings.HasPrefix(from, pk.dir+"/") {
			continue
		}
		for _, tf := range pk.files {
			if tf == from {
				return nil, fmt.Errorf("refs: cannot move %s: it belongs to installed pack %q in %s; move the pack folder %s instead", from, pk.name, pk.file, pk.dir)
			}
			if strings.HasPrefix(tf, from+"/") {
				return nil, fmt.Errorf("refs: cannot move %s: it holds %s, which belongs to installed pack %q in %s; move the pack folder %s instead", from, tf, pk.name, pk.file, pk.dir)
			}
		}
	}
	mapPath := func(s string) string {
		if s == from {
			return to
		}
		if rest, ok := strings.CutPrefix(s, from+"/"); ok {
			return to + "/" + rest
		}
		return s
	}
	for _, name := range p.unread {
		data, err := fs.ReadFile(fsys, name)
		if err == nil && bytes.Contains(data, []byte(strings.TrimPrefix(from, rootPrefix(p.Root(from))))) {
			return nil, fmt.Errorf("refs: %s does not parse, so its references to %s cannot be rewritten; fix it first", name, from)
		}
	}
	pl := newPlanner(p, mapPath)
	pl.plan.Moves = []Move{{From: from, To: to}}
	for _, r := range p.Refs {
		if r.Target == "" {
			continue
		}
		file, target := mapPath(r.File), mapPath(r.Target)
		// A root-relative path changes only when its target moves; a path relative to its file also changes when the file moves.
		if target == r.Target && (!r.FromFile || file == r.File) {
			continue
		}
		if err := pl.rewrite(r, file, target); err != nil {
			return nil, fmt.Errorf("refs: %w", err)
		}
	}
	slices.SortFunc(pl.plan.Updated, compareRefs)
	plan, err := pl.finish()
	if err != nil {
		return nil, err
	}
	for _, w := range plan.Writes {
		if path.Base(w.Path) == project.FileName {
			if _, err := project.Parse(w.Data); err != nil {
				return nil, fmt.Errorf("refs: moving %s to %s would make %s invalid: %w", from, to, w.Path, err)
			}
		}
	}
	return plan, nil
}

// IndexOptions configures PlanIndex.
type IndexOptions struct {
	// Rand is where new uids come from (resource.NewUID); nil is crypto/rand.
	Rand io.Reader
}

// PlanIndex plans what game index does to the project in fsys: rewrite every reference to a file that is gone and that Project.Moved finds, give each scene and resource file without a uid a new one, give a new uid to each file sharing its uid with the file the index lists for it (or, when the index lists neither, with the first in path order), and bring the index at every project root up to date. A file that holds a uid that is not valid is refused, since its uid cannot be kept.
func PlanIndex(fsys fs.FS, opts IndexOptions) (*Plan, error) {
	p, err := Scan(fsys)
	if err != nil {
		return nil, err
	}
	bad := slices.Clone(p.indexProblems)
	for _, f := range p.Files {
		if n := field(f.doc, "uid"); (f.Kind == KindScene || f.Kind == KindPrefab || f.Kind == KindResource || f.Kind == KindScreen || f.Kind == KindStates) && n != nil && f.uid == nil {
			bad = append(bad, Problem{File: f.Path, Line: n.Line, Message: fmt.Sprintf("uid %q is not a uid; delete the line and run game index again to get a new one", n.Value)})
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("refs: fix these first:\n%s", problemList(bad))
	}
	pl := newPlanner(p, func(s string) string { return s })
	for _, r := range p.Refs {
		if r.Target == "" || r.Dir || r.Output || p.exists[r.Target] {
			continue
		}
		if to, ok := p.Moved(r.Target); ok {
			if err := pl.rewrite(r, r.File, to); err != nil {
				return nil, fmt.Errorf("refs: %w", err)
			}
			pl.retarget[refKey{r.File, r.span.start}] = to
		}
	}
	slices.SortFunc(pl.plan.Updated, compareRefs)
	used := make(map[string]bool, len(p.uids))
	for id := range p.uids {
		used[id] = true
	}
	newUID := func() (string, error) {
		for range 100 {
			id, err := resource.NewUID(opts.Rand)
			if err != nil {
				return "", err
			}
			if !used[id] {
				used[id] = true
				return id, nil
			}
		}
		return "", errors.New("refs: no unused uid after 100 tries")
	}
	for _, id := range sortedKeys(p.uids) {
		// Only files of one project share a uid by mistake; each project's index is its own.
		var roots []string
		for _, h := range p.uids[id] {
			if !slices.Contains(roots, p.files[h].Root) {
				roots = append(roots, p.files[h].Root)
			}
		}
		for _, root := range roots {
			holders := p.holders(id, root)
			if len(holders) < 2 {
				continue
			}
			keep := holders[0]
			for _, h := range holders {
				if e, ok := p.indexes[root].Lookup(strings.TrimPrefix(h, rootPrefix(root))); ok && e.UID == id {
					keep = h
					break
				}
			}
			for _, h := range holders {
				if h == keep {
					continue
				}
				nid, err := newUID()
				if err != nil {
					return nil, err
				}
				if err := pl.assignUID(p.files[h], nid); err != nil {
					return nil, fmt.Errorf("refs: %w", err)
				}
			}
		}
	}
	for _, f := range p.Files {
		if (f.Kind != KindScene && f.Kind != KindPrefab && f.Kind != KindResource && f.Kind != KindScreen && f.Kind != KindStates) || f.UID != "" {
			continue
		}
		id, err := newUID()
		if err != nil {
			return nil, err
		}
		if err := pl.assignUID(f, id); err != nil {
			return nil, fmt.Errorf("refs: %w", err)
		}
	}
	return pl.finish()
}

// PlanRenameTextKey plans renaming the localization text key from to to across every *.screen.yaml file (textKey fields) and YAML localization string table (strings and plurals keys) in fsys.
// It rewrites each occurrence in place, preserving comments and formatting, and refuses empty keys, no-op renames, unknown keys, or a target key that already exists in a string table holding from.
func PlanRenameTextKey(fsys fs.FS, from, to string) (*Plan, error) {
	if from == "" || strings.TrimSpace(from) != from {
		return nil, fmt.Errorf("refs: text key %q must be non-empty without surrounding spaces", from)
	}
	if to == "" || strings.TrimSpace(to) != to {
		return nil, fmt.Errorf("refs: text key %q must be non-empty without surrounding spaces", to)
	}
	if from == to {
		return nil, fmt.Errorf("refs: text key is already %q", to)
	}
	p, err := Scan(fsys)
	if err != nil {
		return nil, err
	}
	if len(p.Problems) > 0 {
		return nil, fmt.Errorf("refs: fix these first:\n%s", problemList(p.Problems))
	}
	usages := p.TextKeyUsages(from)
	if len(usages) == 0 {
		return nil, fmt.Errorf("refs: no text key %q found in screens or localization tables", from)
	}
	// Refuse colliding with an existing key in any string table where from is defined.
	tablesWithFrom := make(map[string]bool)
	for _, u := range usages {
		if u.Field == FieldStringKey || u.Field == FieldPluralKey {
			tablesWithFrom[u.File] = true
		}
	}
	for _, existing := range p.TextKeyUsages(to) {
		if (existing.Field == FieldStringKey || existing.Field == FieldPluralKey) && tablesWithFrom[existing.File] {
			return nil, fmt.Errorf("refs: %s:%d: text key %q already exists", existing.File, existing.Line, to)
		}
	}

	edits := make(map[string][]edit)
	plan := &Plan{}
	seen := make(map[refKey]bool)
	for _, u := range usages {
		if u.span.start < 0 {
			return nil, fmt.Errorf("refs: %s:%d: %s %q is not written as a plain or quoted scalar on one line, so it cannot be rewritten in place", u.File, u.Line, u.Field, u.Key)
		}
		rk := refKey{u.File, u.span.start}
		if seen[rk] {
			continue
		}
		seen[rk] = true
		edits[u.File] = append(edits[u.File], edit{u.span.start, u.span.end, formatValue(u.span.syntax, to)})
		plan.UpdatedTextKeys = append(plan.UpdatedTextKeys, u)
	}
	for _, name := range sortedKeys(edits) {
		f := p.files[name]
		data := applyEdits(f.data, edits[name])
		plan.Writes = append(plan.Writes, Write{Path: name, Data: data})
	}
	slices.SortFunc(plan.Writes, func(a, b Write) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortFunc(plan.UpdatedTextKeys, func(a, b TextKeyRef) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})
	return plan, nil
}

func problemList(probs []Problem) string {
	lines := make([]string, len(probs))
	for i, prob := range probs {
		lines[i] = prob.String()
	}
	return strings.Join(lines, "\n")
}
