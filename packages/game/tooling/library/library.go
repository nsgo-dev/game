package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

// Library is an index together with where it was read from, so that its entries' relative sources resolve.
type Library struct {
	// Index is the library's parsed index.
	Index *Index
	// Location is where the index was read from: a URL, a file path, or the name of the index in an fs.FS.
	Location string
	// Client fetches http(s) indexes and packs; nil uses http.DefaultClient.
	Client *http.Client

	// fsys and dir are a local library's files and the directory of its index in them; fsys is nil for a library read over http(s).
	fsys fs.FS
	dir  string
	// base is the index's URL for a library read over http(s).
	base *url.URL
}

// Open reads the index at location: an http(s) URL of an index file, a local index file, or a local directory holding index.yaml. Plain http is accepted only for a loopback host, such as a test server's.
func Open(ctx context.Context, location string) (*Library, error) {
	if isURL(location) {
		u, err := parseFetchURL(location)
		if err != nil {
			return nil, err
		}
		l := &Library{Location: location, base: u}
		data, err := l.get(ctx, u)
		if err != nil {
			return nil, err
		}
		if l.Index, err = ParseIndex(data); err != nil {
			return nil, fmt.Errorf("%w (%s)", err, location)
		}
		return l, nil
	}
	file := location
	if info, err := os.Stat(location); err == nil && info.IsDir() {
		file = filepath.Join(location, IndexFileName)
	}
	l, err := OpenFS(os.DirFS(filepath.Dir(file)), filepath.Base(file))
	if err != nil {
		return nil, err
	}
	l.Location = file
	return l, nil
}

// OpenFS reads the index called name in fsys, whose relative sources resolve from name's directory in fsys.
func OpenFS(fsys fs.FS, name string) (*Library, error) {
	data, err := readLimited(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	idx, err := ParseIndex(data)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", err, name)
	}
	return &Library{Index: idx, Location: name, fsys: fsys, dir: path.Dir(name)}, nil
}

// Remote reports whether fetching from l, or from any of its packs, may reach the network, so that a caller with a user interface fetches off its event loop.
func (l *Library) Remote() bool {
	if l.fsys == nil {
		return true
	}
	for _, e := range l.Index.Packs {
		if isURL(e.Source) {
			return true
		}
	}
	return false
}

// Fetch fetches the pack called name from its source and checks it against its index entry: its digest, name, kind and version must all be the entry's.
func (l *Library) Fetch(ctx context.Context, name string) (*Pack, error) {
	e, ok := l.Index.Find(name)
	if !ok {
		return nil, fmt.Errorf("library: no pack %q in %s", name, l.Location)
	}
	var p *Pack
	var err error
	switch {
	case isURL(e.Source) || l.fsys == nil:
		var base *url.URL
		if isURL(e.Source) {
			base, err = parseFetchURL(e.Source)
		} else {
			base, err = l.base.Parse(e.Source)
		}
		if err != nil {
			return nil, err
		}
		p, err = readPack(func(file string) ([]byte, error) {
			return l.get(ctx, base.JoinPath(file))
		})
	default:
		p, err = ReadPack(l.fsys, path.Join(l.dir, e.Source))
	}
	if err != nil {
		return nil, err
	}
	m := p.Manifest
	if m.Name != e.Name || m.Kind != e.Kind || m.PackVersion != e.PackVersion {
		return nil, fmt.Errorf("library: pack %q at %s: its manifest names %s pack %q version %s, but the index lists %s pack %q version %s", name, e.Source, m.Kind, m.Name, m.PackVersion, e.Kind, e.Name, e.PackVersion)
	}
	if got := p.Digest(); got != e.SHA256 {
		return nil, fmt.Errorf("library: pack %q at %s has digest %s, but the index lists %s", name, e.Source, got, e.SHA256)
	}
	return p, nil
}

// parseFetchURL parses an http(s) URL Open or Fetch may read: https, or http to a loopback host.
func parseFetchURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return nil, fmt.Errorf("library: %s must be an https URL, or http to a loopback host", raw)
	}
	return u, nil
}

// isLoopback reports whether host names this machine.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// get fetches u, refusing a status other than 200 OK and a body larger than MaxFileSize, and a redirect to a URL parseFetchURL refuses.
func (l *Library) get(ctx context.Context, u *url.URL) ([]byte, error) {
	if _, err := parseFetchURL(u.String()); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	// A redirect must stay on a URL Open would read, so an https library cannot be sent to plain http.
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := parseFetchURL(req.URL.String()); err != nil {
			return err
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("library: GET %s: %s", u, resp.Status)
	}
	data, err := readAllLimited(resp.Body, u.String())
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return data, nil
}
