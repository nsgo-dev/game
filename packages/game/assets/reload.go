package assets

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/internal/devimage"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Reload replaces the loaded assets that were read from uri with data, the file's new contents, and returns the names of the assets it replaced, in the order of their groups and of the assets in each; hot reload (tooling/hotreload's Watcher.WatchAssets) calls it when a file changes on disk. It runs on the engine goroutine, as the Manager's other methods do.
//
// An atlas whose JSON is uri is parsed again with sprite.LoadAtlas, and one whose image is uri draws data from then on, its frames unchanged; either way the *sprite.Atlas the group holds is updated in place, so code that kept the pointer sees the edit. An image whose source is uri shows data, and raw bytes read from uri become data. For an image, an atlas's too, that ships in several densities (core.ImageSource.Scales), uri is the file the screen's pixel ratio picks, such as "asset:hero@2x.png" on a 2x screen, and the image keeps that file's name and so its density; saving a density the screen does not show changes nothing. Sounds and custom assets are not replaced. Assets a group has not loaded are skipped, as are an atlas's JSON and raw bytes that a group reads from the file system SetFS mounted on it, such as a DLC pack overriding the base file at uri, and an atlas whose new JSON does not parse keeps its previous frames and adds its error to the one returned.
//
// When it replaces anything, Reload increments Revision, so a core.Canvas or core.Effect that reads Revision draws or runs again.
func (m *Manager) Reload(uri string, data []byte) ([]string, error) {
	if m.isDisposed() {
		return nil, nil
	}
	want := normalizeURI(uri)
	same := func(a, b string) bool { return normalizeURI(a) == normalizeURI(b) }
	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	var (
		names []string
		errs  []error
	)
	for _, groupName := range m.groupOrder {
		g, ok := m.groups[groupName]
		if !ok {
			continue
		}
		for _, spec := range g.specs {
			switch spec.kind {
			case kindAtlas:
				cur, ok := g.atlases[spec.name]
				if !ok || cur == nil {
					continue
				}
				if normalizeURI(spec.uri) == want {
					if g.readsFromPack(spec.uri) {
						continue
					}
					next, err := sprite.LoadAtlas(cur.Image, data)
					if err != nil {
						errs = append(errs, fmt.Errorf("assets: atlas %q: %w", spec.name, err))
						continue
					}
					*cur = *next
				} else if img, ok := devimage.Reloaded(spec.img, uri, data, ratio, same); ok {
					cur.Image = img
				} else {
					continue
				}
			case kindImage:
				if _, ok := g.images[spec.name]; !ok {
					continue
				}
				img, ok := devimage.Reloaded(spec.img, uri, data, ratio, same)
				if !ok {
					continue
				}
				g.images[spec.name] = img
			case kindBytes:
				if _, ok := g.bytes[spec.name]; !ok || normalizeURI(spec.uri) != want || g.readsFromPack(spec.uri) {
					continue
				}
				g.bytes[spec.name] = data
			default:
				continue
			}
			names = append(names, spec.name)
		}
	}
	if len(names) > 0 {
		m.revision.Set(m.revision.GetUntracked() + 1)
	}
	return names, errors.Join(errs...)
}

// readsFromPack reports whether g reads uri from the file system SetFS mounted on it, such as a DLC pack's, rather than from the Manager's reader: the pack has the file, so the group shows the pack's copy and a change to the base file is not one it sees.
func (g *Group) readsFromPack(uri string) bool {
	if g.fs == nil {
		return false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(uri, "asset:"), "/")
	if !fs.ValidPath(rel) {
		return false
	}
	_, err := fs.Stat(g.fs, rel)
	return err == nil
}

// Revision returns how many Reload calls have replaced an asset. Reading it inside a core.Canvas Draw callback or a core.Effect subscribes to later reloads.
func (m *Manager) Revision() int {
	return m.revision.Get()
}

// normalizeURI returns uri with an "asset:" path cleaned and without a leading slash, as the default reader resolves it, so "asset:/ui/../hero.json" and "asset:hero.json" name the same file.
func normalizeURI(uri string) string {
	rel, ok := strings.CutPrefix(uri, "asset:")
	if !ok {
		return uri
	}
	return "asset:" + strings.TrimPrefix(path.Clean("/"+rel), "/")
}
