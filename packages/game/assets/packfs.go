package assets

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
)

// readFileInitialCap is the most mountedPack.ReadFile allocates before an entry's bytes arrive. A larger entry grows its buffer as it decompresses, so a header that claims more than the entry holds costs nothing (#546).
const readFileInitialCap = 1 << 20

// mountedPack is a verified DLC pack mounted from one cache reader. Every read is bounded by the manager's PackLimits as the bytes decompress, not only by the sizes the archive's headers declare. Close releases the reader and the pack's mount count on its DLCManager.
type mountedPack struct {
	zr     *zip.Reader
	limits PackLimits
	ref    *mountRef
}

// mountRef is the part of a mountedPack that its cleanup may reach: it must not point back at the mountedPack, or the cleanup never runs.
type mountRef struct {
	d      *DLCManager
	key    string
	r      io.ReaderAt
	once   sync.Once
	closed atomic.Bool
}

func (m *mountRef) release() {
	m.once.Do(func() {
		m.closed.Store(true)
		closeReader(m.r)
		m.d.unmount(m.key)
	})
}

// newPackFS mounts zr, verified on r, as a pack of key and counts the mount. A mountedPack dropped without Close is released when it is garbage collected.
func (d *DLCManager) newPackFS(key string, r io.ReaderAt, zr *zip.Reader) *mountedPack {
	ref := &mountRef{d: d, key: key, r: r}
	d.mountsMu.Lock()
	m := d.mounts[key]
	if m == nil {
		m = &packMount{}
		d.mounts[key] = m
	}
	m.refs++
	m.removePending = false
	d.mountsMu.Unlock()
	p := &mountedPack{zr: zr, limits: d.limits, ref: ref}
	runtime.AddCleanup(p, func(ref *mountRef) { ref.release() }, ref)
	return p
}

// Close releases the pack: later reads fail with fs.ErrClosed, and a cache entry that DLCManager.Remove was asked to delete while the pack was mounted is deleted once no mount of it is left. Close is idempotent.
func (p *mountedPack) Close() error {
	p.ref.release()
	return nil
}

// Open implements fs.FS.
func (p *mountedPack) Open(name string) (fs.File, error) {
	if p.ref.closed.Load() {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrClosed}
	}
	f, err := p.zr.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		return f, nil
	}
	return &packFile{File: f, pack: p, name: name, limit: p.limits.MaxFileBytes}, nil
}

// ReadDir implements fs.ReadDirFS.
func (p *mountedPack) ReadDir(name string) ([]fs.DirEntry, error) {
	if p.ref.closed.Load() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrClosed}
	}
	return fs.ReadDir(p.zr, name)
}

// ReadFile implements fs.ReadFileFS. Unlike fs.ReadFile, it does not allocate the size an entry's header declares up front: the buffer starts at no more than readFileInitialCap and grows with the bytes that actually decompress, and reading past PackLimits.MaxFileBytes fails with ErrPackTooLarge.
func (p *mountedPack) ReadFile(name string) ([]byte, error) {
	f, err := p.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fmt.Errorf("is a directory")}
	}
	size := info.Size()
	if size < 0 || uint64(size) > p.limits.MaxFileBytes {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fmt.Errorf("%w: entry declares %d bytes, more than %d", ErrPackTooLarge, size, p.limits.MaxFileBytes)}
	}
	data := make([]byte, 0, min(size, readFileInitialCap)+1)
	for {
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
		n, err := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// packFile counts what an entry decompresses to and refuses bytes past the pack's per-file limit, whatever its header says.
type packFile struct {
	fs.File
	pack  *mountedPack // keeps the pack, and so its reader, alive while the file is open
	name  string
	limit uint64
	read  uint64
}

// Read implements fs.File.
func (f *packFile) Read(b []byte) (int, error) {
	if f.pack.ref.closed.Load() {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrClosed}
	}
	n, err := f.File.Read(b)
	f.read += uint64(n)
	if f.read > f.limit {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fmt.Errorf("%w: entry decompresses past %d bytes", ErrPackTooLarge, f.limit)}
	}
	return n, err
}

// packMount counts the live mounts of one cache key.
type packMount struct {
	refs int
	// removePending records a DLCManager.Remove made while the pack was mounted; the entry is deleted when the last mount is released.
	removePending bool
}

// unmount releases one mount of key, deleting the cache entry when it was the last one and a Remove is pending.
func (d *DLCManager) unmount(key string) {
	d.mountsMu.Lock()
	defer d.mountsMu.Unlock()
	m := d.mounts[key]
	if m == nil {
		return
	}
	m.refs--
	if m.refs > 0 {
		return
	}
	delete(d.mounts, key)
	if m.removePending {
		// Nobody is left to report an error to; a failed delete leaves the entry for the next Fetch to verify.
		_ = d.cache.Remove(key)
	}
}

// mounted reports whether a pack of key is mounted.
func (d *DLCManager) mounted(key string) bool {
	d.mountsMu.Lock()
	defer d.mountsMu.Unlock()
	return d.mounts[key] != nil
}

// removeUnlessMounted deletes the cached pack of key unless a mount still reads it. The caller holds key's lock.
func (d *DLCManager) removeUnlessMounted(key string) {
	if !d.mounted(key) {
		_ = d.cache.Remove(key)
	}
}

// releasePack closes a pack FetchGroup mounted on g, if any.
func (g *Group) releasePack() {
	if g.pack != nil {
		_ = g.pack.Close()
		g.pack = nil
	}
}
