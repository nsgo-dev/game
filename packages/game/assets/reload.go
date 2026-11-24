package assets

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/internal/devimage"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/audio"
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
					if g.rawBytes == nil {
						g.rawBytes = map[string]int64{}
					}
					g.rawBytes[spec.name] = int64(len(data))
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
				if g.rawBytes == nil {
					g.rawBytes = map[string]int64{}
				}
				g.rawBytes[spec.name] = int64(len(data))
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

// ReloadedAsset describes one loaded asset touched by ReloadAsset or ReloadGroup.
type ReloadedAsset struct {
	Group  string
	Name   string
	Kind   string
	URI    string
	Bytes  int64
	Frames int
	Err    error
}

// ReloadAsset reloads loaded assets matching key by asset name, URI, or relative path,
// reading fresh bytes through each group's configured reader (group FS, Manager.ReadAsset,
// Manager FS, or local assets/). When groupName is non-empty, only that group is searched.
//
// Matching atlases are updated in place (*cur = *next), preserving existing *sprite.Atlas
// pointers held by the running game. On a read or parse error, the previous working asset
// stays intact and the failure is reported both in the returned slice entry's Err and in
// the joined error. ReloadAsset returns an empty slice and nil error when no loaded asset
// matches key (or a group-not-found error when groupName is unknown).
//
// Call ReloadAsset on the engine goroutine.
func (m *Manager) ReloadAsset(ctx context.Context, groupName, key string) ([]ReloadedAsset, error) {
	if m == nil || m.isDisposed() {
		return nil, errors.New("assets: manager is disposed")
	}
	if ctx == nil {
		ctx = m.ctx
	}
	var groups []*Group
	if groupName != "" {
		g, ok := m.groups[groupName]
		if !ok {
			return nil, fmt.Errorf("assets: group %q not found", groupName)
		}
		groups = []*Group{g}
	} else {
		groups = make([]*Group, 0, len(m.groupOrder))
		for _, name := range m.groupOrder {
			if g, ok := m.groups[name]; ok {
				groups = append(groups, g)
			}
		}
	}

	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	var (
		results  []ReloadedAsset
		errs     []error
		replaced bool
	)
	for _, g := range groups {
		for _, spec := range g.specs {
			matchMode, matchedURI := matchSpec(spec, key, ratio)
			if matchMode == specMatchNone {
				continue
			}
			if !g.isSpecLoaded(spec) {
				continue
			}
			res := m.reloadSpec(ctx, g, spec, matchMode, matchedURI, ratio)
			results = append(results, res)
			if res.Err != nil {
				errs = append(errs, res.Err)
			} else {
				replaced = true
			}
		}
	}
	if replaced {
		m.revision.Set(m.revision.GetUntracked() + 1)
	}
	return results, errors.Join(errs...)
}

// ReloadGroup reloads every loaded asset in the named group in place by re-reading its
// sources through the group's configured asset reader. Individual asset read/parse errors
// keep the previous working asset intact and are returned both per-item and as a joined error.
//
// Call ReloadGroup on the engine goroutine.
func (m *Manager) ReloadGroup(ctx context.Context, groupName string) ([]ReloadedAsset, error) {
	if m == nil || m.isDisposed() {
		return nil, errors.New("assets: manager is disposed")
	}
	g, ok := m.groups[groupName]
	if !ok {
		return nil, fmt.Errorf("assets: group %q not found", groupName)
	}
	if g.state == stateLoading {
		return nil, fmt.Errorf("assets: group %q is currently loading", groupName)
	}
	if g.state != stateLoaded {
		return nil, fmt.Errorf("assets: group %q is not loaded (state: %s)", groupName, groupStateName(g.state))
	}
	if ctx == nil {
		ctx = m.ctx
	}

	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	results := make([]ReloadedAsset, 0, len(g.specs))
	var (
		errs     []error
		replaced bool
	)
	for _, spec := range g.specs {
		if !g.isSpecLoaded(spec) {
			continue
		}
		res := m.reloadSpec(ctx, g, spec, specMatchName, "", ratio)
		results = append(results, res)
		if res.Err != nil {
			errs = append(errs, res.Err)
		} else {
			replaced = true
		}
	}
	if replaced {
		m.revision.Set(m.revision.GetUntracked() + 1)
	}
	return results, errors.Join(errs...)
}

type specMatchKind uint8

const (
	specMatchNone specMatchKind = iota
	specMatchName
	specMatchURI
	specMatchImageURI
)

func matchSpec(spec assetSpec, key string, ratio float32) (specMatchKind, string) {
	if key == "" {
		return specMatchNone, ""
	}
	if spec.name == key {
		return specMatchName, ""
	}
	if spec.uri != "" && matchURI(spec.uri, key) {
		return specMatchURI, spec.uri
	}
	if spec.img.URI != "" {
		shown := devimage.Shown(spec.img, ratio)
		if matchURI(shown, key) {
			return specMatchImageURI, shown
		}
		if shown != spec.img.URI && matchURI(spec.img.URI, key) {
			return specMatchImageURI, spec.img.URI
		}
	}
	if spec.sound.URI != "" && matchURI(spec.sound.URI, key) {
		return specMatchURI, spec.sound.URI
	}
	return specMatchNone, ""
}

func matchURI(specURI, key string) bool {
	normSpec := normalizeURI(specURI)
	if normalizeURI(key) == normSpec {
		return true
	}
	if !strings.HasPrefix(key, "asset:") && !strings.HasPrefix(key, "file://") {
		if normalizeURI("asset:"+strings.TrimPrefix(key, "/")) == normSpec {
			return true
		}
	}
	return false
}

func (g *Group) isSpecLoaded(spec assetSpec) bool {
	switch spec.kind {
	case kindAtlas:
		a, ok := g.atlases[spec.name]
		return ok && a != nil
	case kindSound:
		s, ok := g.sounds[spec.name]
		return ok && s != nil
	case kindImage:
		_, ok := g.images[spec.name]
		return ok
	case kindBytes:
		_, ok := g.bytes[spec.name]
		return ok
	case kindCustom:
		_, ok := g.custom[spec.name]
		return ok
	default:
		return false
	}
}

func (m *Manager) reloadSpec(ctx context.Context, g *Group, spec assetSpec, mode specMatchKind, matchedURI string, ratio float32) ReloadedAsset {
	res := ReloadedAsset{
		Group: g.name,
		Name:  spec.name,
		Kind:  assetKindName(spec.kind),
		URI:   specURI(spec),
	}
	readFn := g.readAssetFunc()
	same := func(a, b string) bool { return normalizeURI(a) == normalizeURI(b) }

	switch spec.kind {
	case kindAtlas:
		cur := g.atlases[spec.name]
		if mode == specMatchImageURI {
			targetURI := matchedURI
			if targetURI == "" {
				targetURI = devimage.Shown(spec.img, ratio)
			}
			res.URI = targetURI
			data, err := readFn(ctx, targetURI)
			if err != nil {
				res.Bytes = estimateAtlasBytes(cur, g.rawBytes[spec.name])
				res.Frames = len(cur.Frames)
				res.Err = fmt.Errorf("assets: atlas %q: read %q: %w", spec.name, targetURI, err)
				return res
			}
			if img, ok := devimage.Reloaded(spec.img, targetURI, data, ratio, same); ok {
				cur.Image = img
			} else {
				cur.Image = devimage.WithData(cur.Image, data)
			}
			res.Bytes = estimateAtlasBytes(cur, g.rawBytes[spec.name])
			res.Frames = len(cur.Frames)
			return res
		}

		data, err := readFn(ctx, spec.uri)
		if err != nil {
			res.Bytes = estimateAtlasBytes(cur, g.rawBytes[spec.name])
			res.Frames = len(cur.Frames)
			res.Err = fmt.Errorf("assets: atlas %q: read %q: %w", spec.name, spec.uri, err)
			return res
		}
		img := cur.Image
		if mode == specMatchName && spec.img.URI != "" {
			shown := devimage.Shown(spec.img, ratio)
			if imgData, imgErr := readFn(ctx, shown); imgErr == nil && len(imgData) > 0 {
				if nextImg, ok := devimage.Reloaded(spec.img, shown, imgData, ratio, same); ok {
					img = nextImg
				}
			}
		}
		next, err := sprite.LoadAtlas(img, data)
		if err != nil {
			res.Bytes = estimateAtlasBytes(cur, g.rawBytes[spec.name])
			res.Frames = len(cur.Frames)
			res.Err = fmt.Errorf("assets: atlas %q: %w", spec.name, loaderr.WithFile(err, spec.uri))
			return res
		}
		*cur = *next
		if g.rawBytes == nil {
			g.rawBytes = map[string]int64{}
		}
		g.rawBytes[spec.name] = int64(len(data))
		res.Bytes = estimateAtlasBytes(cur, g.rawBytes[spec.name])
		res.Frames = len(cur.Frames)
		return res

	case kindImage:
		targetURI := matchedURI
		if targetURI == "" {
			targetURI = devimage.Shown(spec.img, ratio)
		}
		if targetURI == "" {
			res.Bytes = estimateImageBytes(g.images[spec.name])
			res.Err = fmt.Errorf("assets: image %q has no reloadable URI", spec.name)
			return res
		}
		res.URI = targetURI
		data, err := readFn(ctx, targetURI)
		if err != nil {
			res.Bytes = estimateImageBytes(g.images[spec.name])
			res.Err = fmt.Errorf("assets: image %q: read %q: %w", spec.name, targetURI, err)
			return res
		}
		if img, ok := devimage.Reloaded(spec.img, targetURI, data, ratio, same); ok {
			g.images[spec.name] = img
		} else {
			g.images[spec.name] = devimage.WithData(g.images[spec.name], data)
		}
		if g.rawBytes == nil {
			g.rawBytes = map[string]int64{}
		}
		g.rawBytes[spec.name] = int64(len(data))
		res.Bytes = estimateImageBytes(g.images[spec.name])
		return res

	case kindBytes:
		data, err := readFn(ctx, spec.uri)
		if err != nil {
			res.Bytes = int64(len(g.bytes[spec.name]))
			res.Err = fmt.Errorf("assets: bytes %q: read %q: %w", spec.name, spec.uri, err)
			return res
		}
		g.bytes[spec.name] = data
		res.Bytes = int64(len(data))
		return res

	case kindCustom:
		old := g.custom[spec.name]
		data, err := readFn(ctx, spec.uri)
		if err != nil {
			res.Bytes = estimateCustomBytes(old.value, g.rawBytes[spec.name])
			res.Err = fmt.Errorf("assets: custom %q: read %q: %w", spec.name, spec.uri, err)
			return res
		}
		var val any = data
		if spec.loader != nil {
			val, err = spec.loader(ctx, data)
			if err != nil {
				res.Bytes = estimateCustomBytes(old.value, g.rawBytes[spec.name])
				res.Err = fmt.Errorf("assets: custom %q: %w", spec.name, err)
				return res
			}
		}
		if old.release != nil && old.value != nil {
			old.release(old.value)
		}
		g.custom[spec.name] = customEntry{value: val, release: spec.release}
		if g.rawBytes == nil {
			g.rawBytes = map[string]int64{}
		}
		g.rawBytes[spec.name] = int64(len(data))
		res.Bytes = estimateCustomBytes(val, g.rawBytes[spec.name])
		return res

	case kindSound:
		src := spec.sound
		if src.URI != "" {
			if data, err := readFn(ctx, src.URI); err == nil && len(data) > 0 {
				src = audio.Source{URI: src.URI, Data: data}
			}
		}
		gen := g.gen
		name := spec.name
		m.cx.Run(func() {
			audio.LoadSound(m.cx, src, func(s *audio.Sound, err error) {
				if m.isDisposed() || g.gen != gen || err != nil || s == nil {
					if s != nil {
						s.Close()
					}
					return
				}
				if old := g.sounds[name]; old != nil && old != s {
					old.Close()
				}
				g.sounds[name] = s
				m.revision.Set(m.revision.GetUntracked() + 1)
			})
		})
		res.Bytes = g.assetStats(spec).Bytes
		return res
	}
	return res
}
