package selfhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func loadAll(t *testing.T, f *FileStore) []string {
	t.Helper()
	var got []string
	if err := f.Load(context.Background(), func(r Record) error {
		got = append(got, r.Account)
		return nil
	}); err != nil {
		t.Fatalf("load: %v", err)
	}
	return got
}

func TestFileStoreTakesBackAFailedSave(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)
	rec := func(account string) Record {
		return Record{Board: "high-score", Period: AllTimePeriod, Account: account, Score: 1, Achieved: at}
	}
	for name, fail := range map[string]func(f *FileStore){
		// Disk full: half the line reaches the file before the write fails.
		"short write": func(f *FileStore) {
			f.writeLine = func(file *os.File, line []byte) (int, error) {
				n, _ := file.Write(line[:len(line)/2])
				return n, errors.New("no space left on device")
			}
		},
		// The whole line is written but the flush fails, so the caller is told the score is lost.
		"failed sync": func(f *FileStore) {
			f.sync = true
			f.syncFile = func(*os.File) error { return errors.New("input/output error") }
		},
		// The write fails and the file cannot be cut back either, so the next Save cuts the broken line off before it appends.
		"short write, no truncate": func(f *FileStore) {
			f.writeLine = func(file *os.File, line []byte) (int, error) {
				n, _ := file.Write(line[:len(line)/2])
				_ = file.Close()
				return n, errors.New("no space left on device")
			}
		},
	} {
		dir := t.TempDir()
		f, err := OpenFileStore(dir, FileStoreOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Save(ctx, rec("ada")); err != nil {
			t.Fatal(err)
		}
		fail(f)
		if err := f.Save(ctx, rec("bob")); err == nil {
			t.Fatalf("%s: Save succeeded", name)
		}
		f.writeLine, f.syncFile = (*os.File).Write, (*os.File).Sync
		if err := f.Save(ctx, rec("cyd")); err != nil {
			t.Fatalf("%s: Save after a failure: %v", name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("%s: close: %v", name, err)
		}
		again, err := OpenFileStore(dir, FileStoreOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := loadAll(t, again); !slices.Equal(got, []string{"ada", "cyd"}) {
			t.Fatalf("%s: reopened store = %v, want [ada cyd]", name, got)
		}
		_ = again.Close()
	}
}

func writeCompactablePeriodFile(t *testing.T, dir string) (string, []byte) {
	t.Helper()
	at := time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	for i := int64(1); i <= compactMin+100; i++ {
		buf.Write(encodeRecord(Record{
			Board:    "high-score",
			Period:   AllTimePeriod,
			Account:  "ada",
			Score:    i,
			Achieved: at.Add(time.Duration(i) * time.Millisecond),
		}))
	}
	buf.Write(encodeRecord(Record{
		Board:    "high-score",
		Period:   AllTimePeriod,
		Account:  "bob",
		Score:    5,
		Achieved: at,
	}))
	boardDir := filepath.Join(dir, "high-score")
	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(boardDir, AllTimePeriod+fileExt)
	orig := append([]byte(nil), buf.Bytes()...)
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, orig
}

func TestFileStoreCompactionPreservesFileOnError(t *testing.T) {
	ctx := context.Background()
	errWrite := errors.New("no space left on device")
	errSync := errors.New("input/output error")
	errClose := errors.New("close failed")
	errRename := errors.New("rename failed")

	cases := []struct {
		name    string
		wantErr error
		inject  func(*FileStore)
	}{
		{
			name:    "partial write error",
			wantErr: errWrite,
			inject: func(f *FileStore) {
				f.writeLine = func(file *os.File, data []byte) (int, error) {
					n, _ := file.Write(data[:len(data)/2])
					return n, errWrite
				}
			},
		},
		{
			name:    "short write without error",
			wantErr: io.ErrShortWrite,
			inject: func(f *FileStore) {
				f.writeLine = func(file *os.File, data []byte) (int, error) {
					return file.Write(data[:len(data)/2])
				}
			},
		},
		{
			name:    "sync error",
			wantErr: errSync,
			inject: func(f *FileStore) {
				f.syncFile = func(*os.File) error { return errSync }
			},
		},
		{
			name:    "close error",
			wantErr: errClose,
			inject: func(f *FileStore) {
				f.closeFile = func(file *os.File) error {
					_ = file.Close()
					return errClose
				}
			},
		},
		{
			name:    "rename error",
			wantErr: errRename,
			inject: func(f *FileStore) {
				f.renameFile = func(string, string) error { return errRename }
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path, orig := writeCompactablePeriodFile(t, dir)

			f, err := OpenFileStore(dir, FileStoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			tc.inject(f)

			var seen int
			err = f.Load(ctx, func(Record) error {
				seen++
				return nil
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Load err = %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("Load err = %q, want period path %q", err.Error(), path)
			}
			if wantSeen := compactMin + 101; seen != wantSeen {
				t.Fatalf("Load delivered %d records before compaction error, want %d", seen, wantSeen)
			}

			gotBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotBytes, orig) {
				t.Fatalf("period file changed after failed compaction: got %d bytes, want %d bytes", len(gotBytes), len(orig))
			}

			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != AllTimePeriod+fileExt {
				t.Fatalf("unexpected files left in board dir: %v", entries)
			}

			// Reopening without faults still loads every record and compacts cleanly.
			again, err := OpenFileStore(dir, FileStoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = again.Close() }()
			latest := map[string]int64{}
			if err := again.Load(ctx, func(r Record) error {
				latest[r.Account] = r.Score
				return nil
			}); err != nil {
				t.Fatalf("reopen Load: %v", err)
			}
			if latest["ada"] != compactMin+100 || latest["bob"] != 5 || len(latest) != 2 {
				t.Fatalf("reopen records = %v, want ada=%d bob=5", latest, compactMin+100)
			}
			compacted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if lines := bytes.Count(compacted, []byte{'\n'}); lines != 2 {
				t.Fatalf("after recovery compaction: got %d lines, want 2", lines)
			}
		})
	}
}

func TestFileStoreCompactionReadOnlyDirPreservesFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0555 does not block temp file creation on Windows or as root")
	}
	dir := t.TempDir()
	path, orig := writeCompactablePeriodFile(t, dir)
	boardDir := filepath.Dir(path)
	if err := os.Chmod(boardDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(boardDir, 0o755) })

	f, err := OpenFileStore(dir, FileStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	err = f.Load(context.Background(), func(Record) error { return nil })
	if err == nil {
		t.Fatal("Load succeeded on read-only directory during compaction")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Load err = %q, want period path %q", err.Error(), path)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, orig) {
		t.Fatalf("period file changed after CreateTemp failure: got %d bytes, want %d bytes", len(gotBytes), len(orig))
	}

	if err := os.Chmod(boardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	latest := map[string]int64{}
	if err := f.Load(context.Background(), func(r Record) error {
		latest[r.Account] = r.Score
		return nil
	}); err != nil {
		t.Fatalf("Load after restoring write permission: %v", err)
	}
	if latest["ada"] != compactMin+100 || latest["bob"] != 5 || len(latest) != 2 {
		t.Fatalf("recovered records = %v, want ada=%d bob=5", latest, compactMin+100)
	}
}
