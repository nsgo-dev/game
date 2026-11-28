package assetbuild

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/project"
)

type remoteSourceFile struct {
	rel  string // slash-separated path relative to source root (used for URI and hashed path)
	data []byte
}

func (b *Builder) remote(a *project.Assets, builtOutputs []string) (prepared, error) {
	r := a.Remote
	prefix := r.Prefix
	if prefix == "" {
		prefix = "asset:"
	}
	srcDir := r.In
	if srcDir == "" && a.Manifest != nil {
		srcDir = a.Manifest.In
	}
	settings := struct {
		Out     string
		Base    string
		Prefix  string
		Include []string
	}{
		Out:     r.Out,
		Base:    r.Base,
		Prefix:  prefix,
		Include: r.Include,
	}
	k, err := newKeyer(b, KindRemote, settings)
	if err != nil {
		return prepared{}, err
	}

	files, err := b.collectRemoteFiles(srcDir, builtOutputs, r.Include)
	if err != nil {
		return prepared{}, err
	}
	for _, f := range files {
		k.input(f.rel, f.data)
	}

	return prepared{key: k.key(), build: func(root string) ([]string, []string, error) {
		entries := make([]assets.RemoteEntry, 0, len(files))
		outs := make([]string, 0, len(files)+1)
		seenHashed := make(map[string]string, len(files))

		for _, f := range files {
			digest := sum(f.data)
			hashed := assets.HashedPath(f.rel, digest)
			if prev, dup := seenHashed[hashed]; dup {
				return nil, nil, fmt.Errorf("files %q and %q map to the same hashed path %q", prev, f.rel, hashed)
			}
			seenHashed[hashed] = f.rel

			dest := under(root, path.Join(r.Out, hashed))
			if err := writeFile(dest, f.data); err != nil {
				return nil, nil, err
			}
			outs = append(outs, dest)
			entries = append(entries, assets.RemoteEntry{
				URI:    prefix + f.rel,
				Path:   hashed,
				Size:   int64(len(f.data)),
				SHA256: digest,
			})
		}

		manifestData, err := assets.EncodeRemoteManifest(&assets.RemoteManifest{
			Version: assets.RemoteManifestVersion,
			Base:    r.Base,
			Files:   entries,
		})
		if err != nil {
			return nil, nil, err
		}
		manifestDest := under(root, path.Join(r.Out, assets.RemoteManifestFile))
		if err := writeFile(manifestDest, manifestData); err != nil {
			return nil, nil, err
		}
		outs = append(outs, manifestDest)
		return outs, nil, nil
	}}, nil
}

func (b *Builder) collectRemoteFiles(srcDir string, builtOutputs, include []string) ([]remoteSourceFile, error) {
	type candidate struct {
		rel string
		abs string
	}
	var cands []candidate
	if srcDir != "" {
		root := b.abs(srcDir)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(name, ".go") || importsettings.IsSidecar(name) {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if len(include) > 0 && !matchesRemoteInclude(rel, include) {
				return nil
			}
			cands = append(cands, candidate{rel: rel, abs: p})
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		for _, out := range builtOutputs {
			name := path.Base(out)
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".go") || importsettings.IsSidecar(name) {
				continue
			}
			if len(include) > 0 && !matchesRemoteInclude(out, include) {
				continue
			}
			cands = append(cands, candidate{rel: out, abs: b.abs(out)})
		}
	}

	slices.SortFunc(cands, func(a, b candidate) int {
		return strings.Compare(a.rel, b.rel)
	})

	files := make([]remoteSourceFile, 0, len(cands))
	for _, c := range cands {
		data, err := os.ReadFile(c.abs)
		if err != nil {
			return nil, err
		}
		files = append(files, remoteSourceFile{rel: c.rel, data: data})
	}
	return files, nil
}

func matchesRemoteInclude(rel string, patterns []string) bool {
	for _, pat := range patterns {
		if strings.HasSuffix(pat, "/") {
			if strings.HasPrefix(rel, pat) {
				return true
			}
			continue
		}
		if strings.ContainsAny(pat, "*?[") {
			if ok, err := path.Match(pat, rel); err == nil && ok {
				return true
			}
			if !strings.Contains(pat, "/") {
				if ok, err := path.Match(pat, path.Base(rel)); err == nil && ok {
					return true
				}
			}
			continue
		}
		if rel == pat || strings.HasPrefix(rel, pat+"/") {
			return true
		}
	}
	return false
}

func (b *Builder) runRemoteStep(a *project.Assets, builtOutputs []string, prev, next *cache, written map[string]string, anyStale bool) (Target, error) {
	id := "remote " + a.Remote.Out
	old := prev.lookup(id)
	t := Target{ID: id, Kind: KindRemote}

	p, err := b.remote(a, builtOutputs)
	if err != nil {
		if b.Check && anyStale && errors.Is(err, fs.ErrNotExist) {
			t.Status = Stale
			if old != nil {
				t.Outputs = old.paths()
			}
			return t, nil
		}
		return t, fmt.Errorf("assetbuild: %s: %w", id, err)
	}

	if !b.Force && old != nil && old.Key == p.key && b.outputsIntact(old) {
		t.Status = UpToDate
		t.Outputs = old.paths()
		next.Targets = append(next.Targets, *old)
	} else if b.Check {
		current, outs, _, err := b.matchesDisk(p.build)
		if err != nil {
			return t, fmt.Errorf("assetbuild: %s: %w", id, err)
		}
		if current {
			t.Status = UpToDate
			t.Outputs = outs
		} else {
			t.Status = Stale
			if old != nil {
				t.Outputs = old.paths()
			}
		}
	} else {
		outs, _, err := p.build(b.Root)
		if err != nil {
			return t, fmt.Errorf("assetbuild: %s: %w", id, err)
		}
		entry, err := record(b.Root, id, p.key, outs)
		if err != nil {
			return t, fmt.Errorf("assetbuild: %s: %w", id, err)
		}
		if old != nil {
			if err := b.removeStale(old, entry, written); err != nil {
				return t, fmt.Errorf("assetbuild: %s: %w", id, err)
			}
		}
		t.Status = Built
		t.Outputs = entry.paths()
		next.Targets = append(next.Targets, *entry)
	}

	for _, o := range t.Outputs {
		if other, dup := written[o]; dup {
			return t, fmt.Errorf("assetbuild: %s and %s both write %s", other, id, o)
		}
		written[o] = id
	}
	return t, nil
}
