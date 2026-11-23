package selfhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// fileExt is the extension of a period's file.
const fileExt = ".jsonl"

// compactMin is the fewest lines a period file has before Load considers rewriting it.
const compactMin = 1024

// FileStore is a Store in a directory of text files: one JSON Lines file per board and period, at <dir>/<board>/<period>.jsonl, each line an account's new best score:
//
//	{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
//
// Save appends a line and Drop deletes a period's file. Load replays the files; it cuts off a last line a crash left unfinished, and rewrites a file whose lines are mostly superseded scores with one line per account. It is safe for concurrent use.
type FileStore struct {
	dir  string
	sync bool

	mu    sync.Mutex
	files map[string]*os.File
	// torn holds the paths whose last Save failed and could not be taken back, so the file may end in part of a line; the next Save cuts it off before appending.
	torn map[string]bool

	// writeLine and syncFile write and flush a period file; tests replace them to make a Save fail part way.
	writeLine func(*os.File, []byte) (int, error)
	syncFile  func(*os.File) error
}

// FileStoreOptions are a FileStore's settings.
type FileStoreOptions struct {
	// Sync flushes every Save to the disk before it returns. Without it a Save reaches the operating system, which survives the process crashing but not the machine losing power.
	Sync bool
}

// OpenFileStore returns a FileStore in dir, creating the directory when it does not exist.
func OpenFileStore(dir string, opts FileStoreOptions) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("selfhost: %w", err)
	}
	return &FileStore{dir: dir, sync: opts.Sync, files: map[string]*os.File{}, torn: map[string]bool{}, writeLine: (*os.File).Write, syncFile: (*os.File).Sync}, nil
}

// fileRecord is one line of a period file.
type fileRecord struct {
	Account  string    `json:"account"`
	Score    int64     `json:"score"`
	Achieved time.Time `json:"achieved"`
}

func (f *FileStore) path(board, period string) string {
	return filepath.Join(f.dir, board, period+fileExt)
}

// Load replays every period file, boards and periods in name order.
func (f *FileStore) Load(ctx context.Context, fn func(Record) error) error {
	boards, err := os.ReadDir(f.dir)
	if err != nil {
		return fmt.Errorf("selfhost: %w", err)
	}
	for _, b := range boards {
		if !b.IsDir() || !validName(b.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(f.dir, b.Name()))
		if err != nil {
			return fmt.Errorf("selfhost: %w", err)
		}
		for _, file := range files {
			period, ok := strings.CutSuffix(file.Name(), fileExt)
			if !ok || file.IsDir() || !validPeriodID(period) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := f.loadFile(b.Name(), period, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *FileStore) loadFile(board, period string, fn func(Record) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.path(board, period)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("selfhost: %w", err)
	}
	if data, err = cutPartialLine(path, data); err != nil {
		return err
	}
	delete(f.torn, path)
	latest := map[string]int{}
	var records []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for line := 1; sc.Scan(); line++ {
		var fr fileRecord
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		var extra any
		if err := dec.Decode(&fr); err != nil || dec.Decode(&extra) != io.EOF || !validAccount(fr.Account) {
			return fmt.Errorf("selfhost: %s line %d: malformed record", path, line)
		}
		rec := Record{Board: board, Period: period, Account: fr.Account, Score: fr.Score, Achieved: fr.Achieved}
		if i, ok := latest[fr.Account]; ok {
			records[i] = rec
		} else {
			latest[fr.Account] = len(records)
			records = append(records, rec)
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("selfhost: %s: %w", path, err)
	}
	lines := bytes.Count(data, []byte{'\n'})
	if lines >= compactMin && lines > 2*len(records) {
		return f.rewrite(path, records)
	}
	return nil
}

// rewrite replaces a period file with one line per record, through a temporary file and a rename.
func (f *FileStore) rewrite(path string, records []Record) error {
	if old := f.files[path]; old != nil {
		_ = old.Close()
		delete(f.files, path)
	}
	var buf bytes.Buffer
	for _, r := range records {
		buf.Write(encodeRecord(r))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".compact-*")
	if err != nil {
		return fmt.Errorf("selfhost: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("selfhost: %w", err)
	}
	return nil
}

// cutPartialLine truncates the file at path, whose contents are data, after its last whole line, and returns the whole lines. A partial line is a score that was never acknowledged: the process stopped, or a Save failed, in the middle of writing it.
func cutPartialLine(path string, data []byte) ([]byte, error) {
	end := bytes.LastIndexByte(data, '\n') + 1
	if end == len(data) {
		return data, nil
	}
	if err := os.Truncate(path, int64(end)); err != nil {
		return nil, fmt.Errorf("selfhost: %w", err)
	}
	return data[:end], nil
}

func encodeRecord(r Record) []byte {
	line, _ := json.Marshal(fileRecord{Account: r.Account, Score: r.Score, Achieved: r.Achieved.UTC()})
	return append(line, '\n')
}

// Save appends rec to its period's file.
func (f *FileStore) Save(ctx context.Context, rec Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validName(rec.Board) || !validPeriodID(rec.Period) || !validAccount(rec.Account) {
		return errors.New("selfhost: invalid record")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.path(rec.Board, rec.Period)
	file := f.files[path]
	if file == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("selfhost: %w", err)
		}
		if f.torn[path] {
			data, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("selfhost: %w", err)
			}
			if err == nil {
				if _, err := cutPartialLine(path, data); err != nil {
					return err
				}
			}
			delete(f.torn, path)
		}
		var err error
		if file, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644); err != nil {
			return fmt.Errorf("selfhost: %w", err)
		}
		f.files[path] = file
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("selfhost: %w", err)
	}
	_, err = f.writeLine(file, encodeRecord(rec))
	if err == nil && f.sync {
		err = f.syncFile(file)
	}
	if err != nil {
		// Take back whatever of the line reached the file, so a failed Save neither leaves a score the caller was told is lost nor a broken line the next Save would append after.
		if terr := file.Truncate(info.Size()); terr != nil {
			_ = file.Close()
			delete(f.files, path)
			f.torn[path] = true
			return fmt.Errorf("selfhost: %w", errors.Join(err, terr))
		}
		return fmt.Errorf("selfhost: %w", err)
	}
	return nil
}

// Drop deletes a period's file.
func (f *FileStore) Drop(ctx context.Context, board, period string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validName(board) || !validPeriodID(period) {
		return errors.New("selfhost: invalid period")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.path(board, period)
	if file := f.files[path]; file != nil {
		_ = file.Close()
		delete(f.files, path)
	}
	delete(f.torn, path)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("selfhost: %w", err)
	}
	return nil
}

// Close closes the files the store has open for appending.
func (f *FileStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(f.files))
	for p := range f.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var errs []error
	for _, p := range paths {
		if err := f.files[p].Close(); err != nil && !errors.Is(err, fs.ErrClosed) {
			errs = append(errs, err)
		}
		delete(f.files, p)
	}
	return errors.Join(errs...)
}

// validPeriodID reports whether id is AllTimePeriod or a date in periodLayout.
func validPeriodID(id string) bool {
	if id == AllTimePeriod {
		return true
	}
	t, err := time.Parse(periodLayout, id)
	return err == nil && t.Format(periodLayout) == id
}
