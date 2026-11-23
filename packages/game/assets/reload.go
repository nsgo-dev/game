package assets

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/internal/devimage"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// Reload replaces the loaded assets that were read from uri with data, the file's new contents, and returns the names of the assets it replaced, in the order of their groups and of the assets in each; hot reload (tooling/hotreload's Watcher.WatchAssets) calls it when a file changes on disk. It runs on the engine goroutine, as the Manager's other methods do.
//
// An atlas whose JSON is uri is parsed again with sprite.LoadAtlas, and one whose image is uri draws data from then on, its frames unchanged; either way the *sprite.Atlas the group holds is updated in place, so code that kept the pointer sees the edit. An image whose source is uri shows data, and raw bytes read from uri become data. Sounds and custom assets are not replaced. Assets a group has not loaded are skipped, and an atlas whose new JSON does not parse keeps its previous frames and adds its error to the one returned.
//
// When it replaces anything, Reload increments Revision, so a core.Canvas or core.Effect that reads Revision draws or runs again.
func (m *Manager) Reload(uri string, data []byte) ([]string, error) {
	if m.isDisposed() {
		return nil, nil
	}
	want := normalizeURI(uri)
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
				switch {
				case normalizeURI(spec.uri) == want:
					next, err := sprite.LoadAtlas(cur.Image, data)
					if err != nil {
						errs = append(errs, fmt.Errorf("assets: atlas %q: %w", spec.name, err))
						continue
					}
					*cur = *next
				case spec.img.URI != "" && normalizeURI(spec.img.URI) == want:
					cur.Image = devimage.WithData(spec.img, data)
				default:
					continue
				}
			case kindImage:
				if _, ok := g.images[spec.name]; !ok || spec.img.URI == "" || normalizeURI(spec.img.URI) != want {
					continue
				}
				g.images[spec.name] = devimage.WithData(spec.img, data)
			case kindBytes:
				if _, ok := g.bytes[spec.name]; !ok || normalizeURI(spec.uri) != want {
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
