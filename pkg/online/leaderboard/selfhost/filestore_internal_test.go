package selfhost

import (
	"context"
	"errors"
	"os"
	"slices"
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
