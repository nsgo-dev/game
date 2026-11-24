package liveops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/nsgo-dev/game/packages/game/save"
)

// DirCloud implements ConditionalCloudSync over a directory on disk, storing each slot as "<slot>.json" with atomic temp-file writes and directory sync.
//
// It is designed for desktop storefronts such as Steam Auto-Cloud, which synchronize a configured directory (such as a subdirectory of cx.DataDir()) across machines without a native cloud SDK. Pairing DirCloud with CloudSyncer gives Steam builds the same CloudSync and conflict-resolution pipeline as iCloud and Google Play Games builds; games that do not need CloudSyncer's conflict envelopes can also point Steam Auto-Cloud directly at save.FileBackend(cx.DataDir()).
//
// DirCloud is safe for concurrent use within a process.
type DirCloud struct {
	dir       string
	mu        sync.Mutex
	sweepOnce sync.Once
}

var _ ConditionalCloudSync = (*DirCloud)(nil)

// NewDirCloud returns a DirCloud storing slot files inside dir, or an error when dir is empty (such as cx.DataDir() on the web). The directory is created on the first write.
func NewDirCloud(dir string) (*DirCloud, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("liveops: dir cloud needs a non-empty directory: %w", ErrCloudUnavailable)
	}
	return &DirCloud{dir: filepath.Clean(dir)}, nil
}

// Dir returns the cleaned directory path where DirCloud keeps slot files.
func (c *DirCloud) Dir() string { return c.dir }

func (c *DirCloud) slotPath(slot string) string { return filepath.Join(c.dir, slot+".json") }

// Load implements CloudSync.
func (c *DirCloud) Load(ctx context.Context, slot string) ([]byte, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if err := save.ValidateSlot(slot); err != nil {
		return nil, fmt.Errorf("liveops: load %q: %w", slot, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.slotPath(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("liveops: load %q: %w", slot, ErrSlotNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("liveops: load %q: %w", slot, err)
	}
	return data, nil
}

// Save implements CloudSync.
func (c *DirCloud) Save(ctx context.Context, slot string, data []byte) error {
	return c.save(ctx, slot, data, false, "")
}

// SaveIf implements ConditionalCloudSync.
func (c *DirCloud) SaveIf(ctx context.Context, slot string, data []byte, match string) error {
	return c.save(ctx, slot, data, true, match)
}

func (c *DirCloud) save(ctx context.Context, slot string, data []byte, conditional bool, match string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := save.ValidateSlot(slot); err != nil {
		return fmt.Errorf("liveops: save %q: %w", slot, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if conditional {
		if err := c.checkMatchLocked("save", slot, match); err != nil {
			return err
		}
	}
	if existing, collides := c.caseCollisionLocked(slot); collides {
		return fmt.Errorf("liveops: save %q: %w: collides with existing slot %q on a case-insensitive file system", slot, save.ErrInvalidSlot, existing)
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("liveops: create cloud directory %q: %w", c.dir, err)
	}
	c.sweepOnce.Do(func() { sweepStaleCloudTemps(c.dir) })
	if err := writeAtomicCloudFile(c.dir, c.slotPath(slot), data); err != nil {
		return fmt.Errorf("liveops: save %q: %w", slot, err)
	}
	return nil
}

// Delete implements CloudSync.
func (c *DirCloud) Delete(ctx context.Context, slot string) error {
	return c.del(ctx, slot, false, "")
}

// DeleteIf implements ConditionalCloudSync.
func (c *DirCloud) DeleteIf(ctx context.Context, slot, match string) error {
	return c.del(ctx, slot, true, match)
}

func (c *DirCloud) del(ctx context.Context, slot string, conditional bool, match string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := save.ValidateSlot(slot); err != nil {
		return fmt.Errorf("liveops: delete %q: %w", slot, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if conditional {
		if err := c.checkMatchLocked("delete", slot, match); err != nil {
			return err
		}
		if match == "" {
			return nil
		}
	}
	err := os.Remove(c.slotPath(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("liveops: delete %q: %w", slot, err)
	}
	if err := syncCloudDir(c.dir); err != nil {
		return fmt.Errorf("liveops: delete %q: %w", slot, err)
	}
	return nil
}

// List implements CloudSync. It returns the sorted names of all valid save slots in the directory, or nil if the directory does not exist yet.
func (c *DirCloud) List(ctx context.Context) ([]string, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listLocked()
}

func (c *DirCloud) listLocked() ([]string, error) {
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("liveops: list cloud directory: %w", err)
	}
	var slots []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		slot, ok := strings.CutSuffix(name, ".json")
		if !ok {
			continue
		}
		if save.ValidateSlot(slot) == nil {
			slots = append(slots, slot)
		}
	}
	slices.Sort(slots)
	return slots, nil
}

func (c *DirCloud) checkMatchLocked(op, slot, match string) error {
	cur, err := os.ReadFile(c.slotPath(slot))
	if errors.Is(err, os.ErrNotExist) {
		if match == "" {
			return nil
		}
		return fmt.Errorf("liveops: %s %q: %w", op, slot, ErrCloudChanged)
	}
	if err != nil {
		return fmt.Errorf("liveops: %s %q: %w", op, slot, err)
	}
	if match == "" || PayloadHash(cur) != match {
		return fmt.Errorf("liveops: %s %q: %w", op, slot, ErrCloudChanged)
	}
	return nil
}

func (c *DirCloud) caseCollisionLocked(slot string) (string, bool) {
	slots, err := c.listLocked()
	if err != nil {
		return "", false
	}
	for _, existing := range slots {
		if existing != slot && strings.EqualFold(norm.NFC.String(existing), norm.NFC.String(slot)) {
			return existing, true
		}
	}
	return "", false
}

const staleCloudTempAge = time.Hour

func sweepStaleCloudTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleCloudTempAge)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, ".cloud-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func writeAtomicCloudFile(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".cloud-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	ok = true
	return syncCloudDir(dir)
}

func syncCloudDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil && !cloudDirSyncUnsupported(err) {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func cloudDirSyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, errors.ErrUnsupported)
}
