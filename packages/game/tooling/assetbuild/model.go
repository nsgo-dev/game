package assetbuild

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// modelSettings is what a model target's key covers: the settings it packs with, after its model's import settings are applied.
type modelSettings struct {
	Scale      float64
	Animations bool
}

func (b *Builder) model(t project.ModelTarget) (prepared, error) {
	settings, sc, err := b.sidecar(t.In)
	if err != nil {
		return prepared{}, err
	}
	if err := onlySection(settings, "model", t.In, "a model target packs it as a model"); err != nil {
		return prepared{}, err
	}
	if settings != nil && settings.Model != nil {
		if settings.Model.Scale != 0 {
			t.Scale = settings.Model.Scale
		}
		if settings.Model.Animations != nil {
			t.Animations = settings.Model.Animations
		}
	}
	ms := modelSettings{Scale: t.Scale, Animations: true}
	if ms.Scale == 0 {
		ms.Scale = 1
	}
	if t.Animations != nil {
		ms.Animations = *t.Animations
	}
	k, err := newKeyer(b, KindModel, ms)
	if err != nil {
		return prepared{}, err
	}
	data, err := os.ReadFile(b.abs(t.In))
	if err != nil {
		return prepared{}, err
	}
	k.input(t.In, data)
	// The buffers and images the model names by relative path are inputs too, so editing a texture beside a .gltf rebuilds its GLB.
	doc, _, err := decodeModel(data)
	if err != nil {
		return prepared{}, fmt.Errorf("%s: %w", t.In, err)
	}
	files := map[string][]byte{}
	for _, uri := range externalURIs(doc) {
		rel, err := resolveModelURI(t.In, uri)
		if err != nil {
			return prepared{}, fmt.Errorf("%s: %w", t.In, err)
		}
		if _, done := files[uri]; done {
			continue
		}
		fd, err := os.ReadFile(b.abs(rel))
		if err != nil {
			return prepared{}, fmt.Errorf("%s: %w", t.In, err)
		}
		files[uri] = fd
		k.input(rel, fd)
	}
	if sc != nil {
		k.input(importsettings.Path(t.In), sc)
	}
	return prepared{key: k.key(), build: func(root string) ([]string, error) {
		out, err := packGLB(data, files, ms)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.In, err)
		}
		dest := under(root, t.Out)
		if err := writeFile(dest, out); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}}, nil
}

const (
	glbMagic     = 0x46546C67 // "glTF"
	glbChunkJSON = 0x4E4F534A // "JSON"
	glbChunkBIN  = 0x004E4942 // "BIN\0"
)

// decodeModel decodes a .gltf file's JSON, or a .glb file's JSON chunk and binary chunk, keeping every number as written.
func decodeModel(data []byte) (doc map[string]any, bin []byte, err error) {
	jsonData := data
	if len(data) >= 4 && binary.LittleEndian.Uint32(data) == glbMagic {
		jsonData, bin, err = splitGLB(data)
		if err != nil {
			return nil, nil, err
		}
	}
	jsonData = bytes.TrimPrefix(jsonData, []byte{0xEF, 0xBB, 0xBF})
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("decode glTF JSON: %w", err)
	}
	if doc == nil {
		return nil, nil, errors.New("glTF JSON is not an object")
	}
	return doc, bin, nil
}

// splitGLB returns a GLB file's JSON chunk and its binary chunk, nil when it has none.
func splitGLB(data []byte) (jsonChunk, bin []byte, err error) {
	if len(data) < 12 || binary.LittleEndian.Uint32(data[4:]) != 2 {
		return nil, nil, errors.New("not a version 2 GLB file")
	}
	total := int(binary.LittleEndian.Uint32(data[8:]))
	if total > len(data) || total < 12 {
		return nil, nil, fmt.Errorf("GLB length %d does not match the file's %d bytes", total, len(data))
	}
	off := 12
	for i := 0; off+8 <= total; i++ {
		n := int(binary.LittleEndian.Uint32(data[off:]))
		typ := binary.LittleEndian.Uint32(data[off+4:])
		off += 8
		if n < 0 || n > total-off {
			return nil, nil, fmt.Errorf("GLB chunk %d overruns the file", i)
		}
		chunk := data[off : off+n]
		off += n
		switch {
		case i == 0 && typ == glbChunkJSON:
			jsonChunk = chunk
		case i == 0:
			return nil, nil, errors.New("the first GLB chunk is not JSON")
		case i == 1 && typ == glbChunkBIN:
			bin = chunk
		}
	}
	if jsonChunk == nil {
		return nil, nil, errors.New("GLB file has no JSON chunk")
	}
	return jsonChunk, bin, nil
}

// ModelFiles returns the slash-separated paths, relative to the project root, of the files the glTF or GLB model at model (slash-separated, relative to the project root) names by relative path in its buffers and images, given the model file's bytes, in the order the model lists them and without repeats: the files a model target embeds and counts as inputs. Data URIs name no file. A URI with a scheme, an absolute path, or one that leaves the project is an error, as is a model whose JSON does not decode.
func ModelFiles(model string, data []byte) ([]string, error) {
	doc, _, err := decodeModel(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", model, err)
	}
	var out []string
	for _, uri := range externalURIs(doc) {
		rel, err := resolveModelURI(model, uri)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", model, err)
		}
		if !slices.Contains(out, rel) {
			out = append(out, rel)
		}
	}
	return out, nil
}

// externalURIs returns the URIs of the model's buffers and images that name files rather than hold data, in the order the model lists them.
func externalURIs(doc map[string]any) []string {
	var uris []string
	for _, key := range []string{"buffers", "images"} {
		for _, item := range objects(doc[key]) {
			if uri, ok := item["uri"].(string); ok && !strings.HasPrefix(uri, "data:") {
				uris = append(uris, uri)
			}
		}
	}
	return uris
}

// resolveModelURI returns the slash-separated path, relative to the project root, of the file uri names relative to the model at model. A URI with a scheme, an absolute path, or one that leaves the project is refused.
func resolveModelURI(model, uri string) (string, error) {
	p, err := url.PathUnescape(uri)
	if err != nil {
		return "", fmt.Errorf("URI %q: %w", uri, err)
	}
	if strings.Contains(p, ":") || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return "", fmt.Errorf("URI %q must be a relative path to a file in the project", uri)
	}
	rel := path.Join(path.Dir(model), p)
	if !fs.ValidPath(rel) || rel == "." {
		return "", fmt.Errorf("URI %q leaves the project", uri)
	}
	return rel, nil
}

// objects returns v's elements that are JSON objects, v being a JSON array; anything else has none.
func objects(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// intField returns the integer m holds under key, def when it has none; a value that is not a non-negative integer is an error.
func intField(m map[string]any, key string, def int) (int, error) {
	v, ok := m[key]
	if !ok {
		return def, nil
	}
	n, isNum := v.(json.Number)
	if !isNum {
		return 0, fmt.Errorf("%s %v is not a number", key, v)
	}
	i, err := n.Int64()
	if err != nil || i < 0 || i > 1<<40 {
		return 0, fmt.Errorf("%s %s is not an integer of 0 or more", key, n)
	}
	return int(i), nil
}

// dataURI decodes a base64 data URI, returning its bytes and media type.
func dataURI(uri string) ([]byte, string, error) {
	meta, payload, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	mediaType, isBase64 := strings.CutSuffix(meta, ";base64")
	if !ok || !isBase64 {
		return nil, "", errors.New("data URI is not base64")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", fmt.Errorf("data URI: %w", err)
	}
	return data, mediaType, nil
}

// imageMIME returns the glTF mimeType of an image file named uri, from its extension.
func imageMIME(uri string) (string, error) {
	switch strings.ToLower(path.Ext(strings.SplitN(uri, "?", 2)[0])) {
	case ".png":
		return "image/png", nil
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".webp":
		return "image/webp", nil
	case ".ktx2":
		return "image/ktx2", nil
	}
	return "", fmt.Errorf("image %q is not a PNG, JPEG, WebP or KTX2 file, so its mimeType is unknown; set mimeType in the model", uri)
}

// align pads buf with zero bytes to a multiple of n.
func align(buf []byte, n int) []byte {
	for len(buf)%n != 0 {
		buf = append(buf, 0)
	}
	return buf
}

// packGLB packs the model in data, a .gltf or .glb file, into one GLB file: the buffers it holds or names are merged into the GLB's binary chunk and the images it names are embedded in it, files holding what data names by relative URI. With s.Scale other than 1 each root node of a scene is put under a new node scaled by it, one per root however many scenes list it, and with s.Animations false the animations are left out. The JSON keeps every other property, extensions and extras included, in sorted key order.
func packGLB(data []byte, files map[string][]byte, s modelSettings) ([]byte, error) {
	resolve := func(uri string) ([]byte, error) {
		if strings.HasPrefix(uri, "data:") {
			d, _, err := dataURI(uri)
			return d, err
		}
		if d, ok := files[uri]; ok {
			return d, nil
		}
		return nil, fmt.Errorf("URI %q was not read", uri)
	}
	// Loading the model first reports what is wrong with it in the terms of a glTF loader, with the field at fault.
	if _, err := gltf.Load(data, resolve); err != nil {
		return nil, err
	}
	doc, glbBin, err := decodeModel(data)
	if err != nil {
		return nil, err
	}

	buffers := objects(doc["buffers"])
	var bin []byte
	// offset maps each merged buffer to where its bytes start in bin; a buffer without data, such as an EXT_meshopt_compression fallback, keeps its own entry.
	offset := make([]int, len(buffers))
	merged := make([]bool, len(buffers))
	for i, buf := range buffers {
		n, err := intField(buf, "byteLength", -1)
		if err != nil {
			return nil, fmt.Errorf("buffers[%d]: %w", i, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("buffers[%d]: byteLength is required", i)
		}
		var bd []byte
		switch uri, hasURI := buf["uri"].(string); {
		case hasURI:
			if bd, err = resolve(uri); err != nil {
				return nil, fmt.Errorf("buffers[%d]: %w", i, err)
			}
		case i == 0 && glbBin != nil:
			bd = glbBin
		default:
			continue
		}
		if len(bd) < n {
			return nil, fmt.Errorf("buffers[%d] holds %d bytes, fewer than its byteLength %d", i, len(bd), n)
		}
		// 16 bytes suit every accessor's component type and a matrix's columns.
		bin = align(bin, 16)
		offset[i], merged[i] = len(bin), true
		bin = append(bin, bd[:n]...)
	}

	views := objects(doc["bufferViews"])
	for _, img := range objects(doc["images"]) {
		uri, ok := img["uri"].(string)
		if !ok {
			continue
		}
		var id []byte
		mime, _ := img["mimeType"].(string)
		if strings.HasPrefix(uri, "data:") {
			var mt string
			if id, mt, err = dataURI(uri); err != nil {
				return nil, err
			}
			if mime == "" {
				mime = mt
			}
		} else {
			if id, err = resolve(uri); err != nil {
				return nil, err
			}
			if mime == "" {
				if mime, err = imageMIME(uri); err != nil {
					return nil, err
				}
			}
		}
		if mime == "" {
			return nil, fmt.Errorf("image %q has no media type; set mimeType in the model", uri)
		}
		bin = align(bin, 4)
		views = append(views, map[string]any{"buffer": json.Number("-1"), "byteOffset": json.Number(fmt.Sprint(len(bin))), "byteLength": json.Number(fmt.Sprint(len(id)))})
		bin = append(bin, id...)
		img["bufferView"] = json.Number(fmt.Sprint(len(views) - 1))
		img["mimeType"] = mime
		delete(img, "uri")
	}

	// index maps each old buffer to its new one: the binary chunk's buffer 0 for a merged buffer, its own entry after it for one without data.
	index := make([]int, len(buffers))
	var out []any
	if len(bin) > 0 {
		out = append(out, map[string]any{"byteLength": json.Number(fmt.Sprint(len(bin)))})
	}
	for i, buf := range buffers {
		if merged[i] {
			index[i] = 0
			continue
		}
		index[i] = len(out)
		out = append(out, buf)
	}
	remap := func(m map[string]any, where string) error {
		if _, ok := m["buffer"]; !ok {
			return nil
		}
		if m["buffer"] == json.Number("-1") {
			// An image's view, added above, already points into the binary chunk.
			m["buffer"] = json.Number("0")
			return nil
		}
		old, err := intField(m, "buffer", 0)
		if err != nil || old >= len(buffers) {
			return fmt.Errorf("%s: buffer %v is not a buffer of the model", where, m["buffer"])
		}
		m["buffer"] = json.Number(fmt.Sprint(index[old]))
		if merged[old] {
			o, err := intField(m, "byteOffset", 0)
			if err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			m["byteOffset"] = json.Number(fmt.Sprint(offset[old] + o))
		}
		return nil
	}
	for i, v := range views {
		where := fmt.Sprintf("bufferViews[%d]", i)
		if err := remap(v, where); err != nil {
			return nil, err
		}
		// A meshopt-compressed view reads its compressed bytes from a buffer of its own.
		if ext, ok := v["extensions"].(map[string]any); ok {
			for _, name := range []string{"EXT_meshopt_compression", "KHR_meshopt_compression"} {
				if m, ok := ext[name].(map[string]any); ok {
					if err := remap(m, where+".extensions."+name); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	if len(views) > 0 {
		doc["bufferViews"] = toAny(views)
	}
	if len(out) > 0 {
		doc["buffers"] = out
	} else {
		delete(doc, "buffers")
	}

	if s.Scale != 1 {
		nodes, _ := doc["nodes"].([]any)
		sc := json.Number(fmt.Sprint(s.Scale))
		// wrappers maps each root node, by its index as written, to the scaled node put above it; scenes that share a root share its wrapper, since glTF allows a node only one parent.
		wrappers := map[string]json.Number{}
		for _, scene := range objects(doc["scenes"]) {
			roots, ok := scene["nodes"].([]any)
			if !ok || len(roots) == 0 {
				continue
			}
			wrapped := make([]any, len(roots))
			for i, r := range roots {
				key := fmt.Sprint(r)
				w, ok := wrappers[key]
				if !ok {
					nodes = append(nodes, map[string]any{"scale": []any{sc, sc, sc}, "children": []any{r}})
					w = json.Number(fmt.Sprint(len(nodes) - 1))
					wrappers[key] = w
				}
				wrapped[i] = w
			}
			scene["nodes"] = wrapped
		}
		if nodes != nil {
			doc["nodes"] = nodes
		}
	}
	if !s.Animations {
		delete(doc, "animations")
	}

	var js bytes.Buffer
	enc := json.NewEncoder(&js)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	jsonChunk := bytes.TrimRight(js.Bytes(), "\n")
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	bin = align(bin, 4)
	size := 12 + 8 + len(jsonChunk)
	if len(bin) > 0 {
		size += 8 + len(bin)
	}
	glb := make([]byte, 0, size)
	glb = binary.LittleEndian.AppendUint32(glb, glbMagic)
	glb = binary.LittleEndian.AppendUint32(glb, 2)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(size))
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(jsonChunk)))
	glb = binary.LittleEndian.AppendUint32(glb, glbChunkJSON)
	glb = append(glb, jsonChunk...)
	if len(bin) > 0 {
		glb = binary.LittleEndian.AppendUint32(glb, uint32(len(bin)))
		glb = binary.LittleEndian.AppendUint32(glb, glbChunkBIN)
		glb = append(glb, bin...)
	}
	// What was packed must load on its own, with nothing outside the file to resolve.
	if _, err := gltf.Load(glb, nil); err != nil {
		return nil, fmt.Errorf("packed GLB does not load: %w", err)
	}
	return glb, nil
}

// toAny returns objs as a JSON array.
func toAny(objs []map[string]any) []any {
	out := make([]any, len(objs))
	for i, o := range objs {
		out[i] = o
	}
	return out
}
