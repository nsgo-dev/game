package editor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
	"golang.org/x/image/webp"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// AgentBriefPreviewID is the preview target ID used when hovering or focusing the brief.yaml Always chip.
	AgentBriefPreviewID = "brief"

	maxAgentRefImageBytes  = 16 << 20 // 16 MiB per attached or pinned reference image
	maxAgentRefImageDim    = 4096     // max width or height in pixels
	maxAgentRefImagePixels = 16 << 20 // 16M pixels cap
	maxAgentAttachedImages = 8        // max one-shot reference images attached to a single request
	maxAgentBriefRefImages = 16       // max pinned reference images loaded from brief.yaml
	maxAgentBriefYAMLBytes = 1 << 20  // 1 MiB cap for brief.yaml
	agentRefFolderRel      = "art/reference"
)

// AgentImageFormat identifies one of the three supported reference image formats for agent requests.
type AgentImageFormat string

const (
	AgentImagePNG  AgentImageFormat = "png"
	AgentImageJPEG AgentImageFormat = "jpeg"
	AgentImageWebP AgentImageFormat = "webp"
)

// MIMEType returns the standard MIME type for f.
func (f AgentImageFormat) MIMEType() string {
	switch f {
	case AgentImagePNG:
		return "image/png"
	case AgentImageJPEG:
		return "image/jpeg"
	case AgentImageWebP:
		return "image/webp"
	default:
		return ""
	}
}

// Extension returns the canonical lowercase file extension (including the leading dot) for f.
func (f AgentImageFormat) Extension() string {
	switch f {
	case AgentImagePNG:
		return ".png"
	case AgentImageJPEG:
		return ".jpg"
	case AgentImageWebP:
		return ".webp"
	default:
		return ""
	}
}

// Label returns the human-readable format name shown in previews and errors.
func (f AgentImageFormat) Label() string {
	switch f {
	case AgentImagePNG:
		return "PNG"
	case AgentImageJPEG:
		return "JPEG"
	case AgentImageWebP:
		return "WebP"
	default:
		return strings.ToUpper(string(f))
	}
}

// AgentReferenceImage is a validated PNG, JPEG, or WebP reference image attached to a single composer
// request or pinned to brief.yaml for every request in the project.
type AgentReferenceImage struct {
	// ID uniquely identifies this image within the current composer draft (or "brief:<path>" for pinned brief references).
	ID string
	// Name is the display file name (for example "spikes_ref.png").
	Name string
	// Path is the project-relative slash path when pinned or read from brief.yaml (for example "art/reference/spikes_ref.png"),
	// or empty for a one-shot ephemeral composer attachment.
	Path string
	// Format is AgentImagePNG, AgentImageJPEG, or AgentImageWebP.
	Format AgentImageFormat
	// MIMEType is "image/png", "image/jpeg", or "image/webp".
	MIMEType string
	// Data holds the raw encoded image bytes sent to the agent.
	Data []byte
	// Width and Height are the decoded pixel dimensions.
	Width  int
	Height int
	// Pinned reports whether this image has been copied to art/reference/ and added to brief.yaml.
	Pinned bool
	// FromBrief reports whether this image joined the request through brief.yaml (the Always chip).
	FromBrief bool
	// Note is an optional short description recorded alongside path in brief.yaml.
	Note string
	// Pixels holds the decoded RGBA image for in-memory thumbnail and hover preview rendering.
	Pixels *image.RGBA
	// ImageVersion uniquely keys core.ImageSource pixel uploads.
	ImageVersion uint64
}

func (img AgentReferenceImage) clone() AgentReferenceImage {
	cp := img
	cp.Data = slices.Clone(img.Data)
	return cp
}

// BriefReference is one entry listed under art.references in brief.yaml.
type BriefReference struct {
	// Path is the project-relative slash path to the reference image (for example "art/reference/spikes_ref.png").
	Path string
	// Note is an optional short description for the agent.
	Note string
}

// BriefPinDiffLine is one formatted line in the Pin to brief confirmation card's YAML diff.
type BriefPinDiffLine struct {
	Sign    string
	Text    string
	Added   bool
	Removed bool
}

// BriefPinNotice describes the most recent Pin to brief action shown in the composer confirmation card with Undo.
type BriefPinNotice struct {
	ImageID string
	Name    string
	RelPath string
	Note    string
	Diff    []BriefPinDiffLine
}

type cachedBriefRefImage struct {
	modTime time.Time
	size    int64
	img     AgentReferenceImage
}

type cachedBriefMeta struct {
	valid   bool
	path    string
	exists  bool
	modTime time.Time
	size    int64
	refs    []BriefReference
}

func (e *Editor) invalidateBriefReferenceCaches() {
	if e == nil {
		return
	}
	e.agent.briefMeta = cachedBriefMeta{}
	clear(e.agent.briefRefCache)
}

// DecodeAgentReferenceImage validates that data is a well-formed PNG, JPEG, or WebP image within safe bounds
// and decodes its pixels into an RGBA image for thumbnails and hover previews.
// Unsupported file types (such as GIF, SVG, BMP, text, or YAML) are refused with a clear reason.
func DecodeAgentReferenceImage(name string, data []byte) (AgentReferenceImage, error) {
	displayName := strings.TrimSpace(filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")))
	if displayName == "." || displayName == "/" {
		displayName = ""
	}
	if len(data) == 0 {
		return AgentReferenceImage{}, formatRefAttachError(displayName, "empty file", "file is empty")
	}
	if len(data) > maxAgentRefImageBytes {
		return AgentReferenceImage{}, fmt.Errorf("cannot attach %s: file is larger than %d MiB", quoteDisplayName(displayName), maxAgentRefImageBytes>>20)
	}

	format, detectedOther := sniffAgentImageFormat(displayName, data)
	if format == "" {
		reason := detectedOther
		if reason == "" {
			if ext := strings.ToLower(path.Ext(displayName)); ext != "" {
				reason = ext
			} else {
				reason = "unsupported format"
			}
		}
		return AgentReferenceImage{}, formatRefAttachError(displayName, reason, "only PNG, JPEG, or WebP images are supported")
	}

	var (
		cfg image.Config
		img image.Image
		err error
	)
	switch format {
	case AgentImagePNG:
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return AgentReferenceImage{}, fmt.Errorf("cannot attach %s: invalid PNG image (%v)", quoteDisplayName(displayName), err)
		}
		if err := validateRefImageDimensions(displayName, cfg.Width, cfg.Height); err != nil {
			return AgentReferenceImage{}, err
		}
		img, err = png.Decode(bytes.NewReader(data))
	case AgentImageJPEG:
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return AgentReferenceImage{}, fmt.Errorf("cannot attach %s: invalid JPEG image (%v)", quoteDisplayName(displayName), err)
		}
		if err := validateRefImageDimensions(displayName, cfg.Width, cfg.Height); err != nil {
			return AgentReferenceImage{}, err
		}
		img, err = jpeg.Decode(bytes.NewReader(data))
	case AgentImageWebP:
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return AgentReferenceImage{}, fmt.Errorf("cannot attach %s: invalid WebP image (%v)", quoteDisplayName(displayName), err)
		}
		if err := validateRefImageDimensions(displayName, cfg.Width, cfg.Height); err != nil {
			return AgentReferenceImage{}, err
		}
		img, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return AgentReferenceImage{}, fmt.Errorf("cannot attach %s: invalid %s image (%v)", quoteDisplayName(displayName), format.Label(), err)
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if err := validateRefImageDimensions(displayName, w, h); err != nil {
		return AgentReferenceImage{}, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)

	cleanName := sanitizeReferenceFileName(displayName, format)
	return AgentReferenceImage{
		Name:     cleanName,
		Format:   format,
		MIMEType: format.MIMEType(),
		Data:     slices.Clone(data),
		Width:    w,
		Height:   h,
		Pixels:   rgba,
	}, nil
}

func validateRefImageDimensions(displayName string, w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("cannot attach %s: image dimensions are invalid (%dx%d)", quoteDisplayName(displayName), w, h)
	}
	if w > maxAgentRefImageDim || h > maxAgentRefImageDim {
		return fmt.Errorf("cannot attach %s: image is too large (%dx%d, max %dx%d)", quoteDisplayName(displayName), w, h, maxAgentRefImageDim, maxAgentRefImageDim)
	}
	if int64(w)*int64(h) > maxAgentRefImagePixels {
		return fmt.Errorf("cannot attach %s: image has too many pixels (%dx%d)", quoteDisplayName(displayName), w, h)
	}
	return nil
}

func sniffAgentImageFormat(name string, data []byte) (AgentImageFormat, string) {
	// PNG magic: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return AgentImagePNG, ""
	}
	// JPEG magic: FF D8 FF
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return AgentImageJPEG, ""
	}
	// WebP magic: RIFF <4 bytes> WEBP
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return AgentImageWebP, ""
	}
	// Identify common unsupported types so the refusal explains what was dropped/pasted.
	if len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))) {
		return "", "GIF"
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return "", "BMP"
	}
	if len(data) >= 4 && bytes.HasPrefix(data, []byte("%PDF")) {
		return "", "PDF"
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<svg")) || (bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(trimmed[:min(len(trimmed), 256)], []byte("<svg"))) {
		return "", "SVG"
	}
	if ext := strings.ToLower(path.Ext(name)); ext != "" {
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp":
			return "", "not a valid " + strings.ToUpper(strings.TrimPrefix(ext, ".")) + " file"
		default:
			return "", ext
		}
	}
	return "", "unsupported format"
}

func quoteDisplayName(name string) string {
	if name == "" {
		return "pasted image"
	}
	return fmt.Sprintf("%q", name)
}

func formatRefAttachError(displayName, detected, detail string) error {
	if detected != "" {
		return fmt.Errorf("cannot attach %s (%s): %s", quoteDisplayName(displayName), detected, detail)
	}
	return fmt.Errorf("cannot attach %s: %s", quoteDisplayName(displayName), detail)
}

func sanitizeReferenceFileName(raw string, format AgentImageFormat) string {
	base := strings.TrimSpace(filepath.Base(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")))
	if base == "" || base == "." || base == ".." || base == "/" {
		return "reference" + format.Extension()
	}
	ext := strings.ToLower(path.Ext(base))
	stem := strings.TrimSuffix(base, path.Ext(base))
	var b strings.Builder
	for _, r := range stem {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		}
	}
	cleanStem := strings.Trim(b.String(), "._-")
	if cleanStem == "" {
		cleanStem = "reference"
	}
	validExt := format.Extension()
	if format == AgentImageJPEG && (ext == ".jpeg" || ext == ".jpg") {
		validExt = ext
	}
	return cleanStem + validExt
}

// AgentAttachedImages returns a copy of the one-shot reference images currently attached to the composer draft.
func (e *Editor) AgentAttachedImages() []AgentReferenceImage {
	if e == nil || len(e.agent.attachedImages) == 0 {
		return nil
	}
	out := make([]AgentReferenceImage, len(e.agent.attachedImages))
	for i, img := range e.agent.attachedImages {
		out[i] = img.clone()
	}
	return out
}

// AgentComposerDragOver reports whether files are currently dragged over the Agent composer drop target.
func (e *Editor) AgentComposerDragOver() bool {
	return e != nil && e.agent.composerDragOver
}

// SetAgentComposerDragOver sets whether the Agent composer shows its "Drop to attach" drop overlay.
func (e *Editor) SetAgentComposerDragOver(active bool) {
	if e == nil || e.agent.composerDragOver == active {
		return
	}
	e.agent.composerDragOver = active
	e.notify()
}

// AgentImagePreviewID returns the ID of the attached image chip currently hovered/previewed
// (or AgentBriefPreviewID when previewing brief.yaml's pinned references), or "".
func (e *Editor) AgentImagePreviewID() string {
	if e == nil {
		return ""
	}
	return e.agent.previewImageID
}

// SetAgentImagePreviewID sets which image chip (or AgentBriefPreviewID) is showing its hover preview above the composer.
func (e *Editor) SetAgentImagePreviewID(id string) {
	if e == nil || e.agent.previewImageID == id {
		return
	}
	e.agent.previewImageID = id
	e.notify()
}

// AgentAttachError returns the latest refusal reason shown in the composer when an unsupported file is dropped or pasted.
func (e *Editor) AgentAttachError() string {
	if e == nil {
		return ""
	}
	return e.agent.attachErr
}

// DismissAgentAttachError clears the composer's attachment refusal banner.
func (e *Editor) DismissAgentAttachError() {
	if e == nil || e.agent.attachErr == "" {
		return
	}
	e.agent.attachErr = ""
	e.notify()
}

// AgentLastPinNotice returns the confirmation card state for the most recent Pin to brief action, or nil.
func (e *Editor) AgentLastPinNotice() *BriefPinNotice {
	if e == nil || e.agent.lastPinNotice == nil {
		return nil
	}
	cp := *e.agent.lastPinNotice
	cp.Diff = slices.Clone(e.agent.lastPinNotice.Diff)
	return &cp
}

// DismissAgentPinNotice hides the Pin to brief confirmation card without undoing the pin.
func (e *Editor) DismissAgentPinNotice() {
	if e == nil || e.agent.lastPinNotice == nil {
		return
	}
	e.agent.lastPinNotice = nil
	e.notify()
}

// AttachAgentReferenceBytes validates raw PNG, JPEG, or WebP bytes and attaches the image to the next composer request only.
func (e *Editor) AttachAgentReferenceBytes(name string, data []byte) (AgentReferenceImage, error) {
	if e == nil {
		return AgentReferenceImage{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return AgentReferenceImage{}, ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return AgentReferenceImage{}, errors.New("editor: agent is disabled")
	}
	if len(e.agent.attachedImages) >= maxAgentAttachedImages {
		err := fmt.Errorf("cannot attach more than %d reference images to one request", maxAgentAttachedImages)
		e.agent.attachErr = err.Error()
		e.Status = err.Error()
		e.notify()
		return AgentReferenceImage{}, err
	}
	img, err := DecodeAgentReferenceImage(name, data)
	if err != nil {
		e.agent.attachErr = err.Error()
		e.Status = err.Error()
		e.notify()
		return AgentReferenceImage{}, err
	}
	e.agent.attachSeq++
	e.agent.attachImgVer++
	img.ID = fmt.Sprintf("img-%d", e.agent.attachSeq)
	img.ImageVersion = e.agent.attachImgVer
	// If no name was provided (for example pasted from clipboard), give each paste a distinct filename.
	if strings.TrimSpace(name) == "" && e.agent.attachSeq > 1 {
		img.Name = fmt.Sprintf("pasted_%d%s", e.agent.attachSeq, img.Format.Extension())
	}
	e.agent.attachedImages = append(e.agent.attachedImages, img)
	e.agent.attachErr = ""
	e.Status = fmt.Sprintf("Attached %s for this request", img.Name)
	e.notify()
	return img.clone(), nil
}

// AttachAgentReferenceFile reads a local file path or file:// URL, validates it as PNG, JPEG, or WebP,
// and attaches it to the current composer request only.
func (e *Editor) AttachAgentReferenceFile(pathOrURI string) (AgentReferenceImage, error) {
	if e == nil {
		return AgentReferenceImage{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return AgentReferenceImage{}, ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return AgentReferenceImage{}, errors.New("editor: agent is disabled")
	}
	raw := strings.TrimSpace(pathOrURI)
	if raw == "" {
		err := errors.New("cannot attach empty file path: only PNG, JPEG, or WebP images are supported")
		e.agent.attachErr = err.Error()
		e.notify()
		return AgentReferenceImage{}, err
	}
	localPath, err := resolveReferenceFileInput(raw)
	if err != nil {
		e.agent.attachErr = err.Error()
		e.Status = err.Error()
		e.notify()
		return AgentReferenceImage{}, err
	}
	base := filepath.Base(localPath)
	// Fast-fail clearly on obviously unsupported extensions before reading huge files, while still validating magic bytes for image extensions.
	if ext := strings.ToLower(path.Ext(base)); ext != "" && ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		err := formatRefAttachError(base, ext, "only PNG, JPEG, or WebP images are supported")
		e.agent.attachErr = err.Error()
		e.Status = err.Error()
		e.notify()
		return AgentReferenceImage{}, err
	}
	// Refuse directories and special files (FIFOs, sockets, device nodes) before opening so a pipe cannot block the UI thread.
	info, statErr := os.Stat(localPath)
	if statErr != nil {
		refErr := fmt.Errorf("cannot attach %q: %w", base, statErr)
		e.agent.attachErr = refErr.Error()
		e.Status = refErr.Error()
		e.notify()
		return AgentReferenceImage{}, refErr
	}
	if !info.Mode().IsRegular() {
		refErr := formatRefAttachError(base, "not a regular file", "only PNG, JPEG, or WebP images are supported")
		e.agent.attachErr = refErr.Error()
		e.Status = refErr.Error()
		e.notify()
		return AgentReferenceImage{}, refErr
	}
	data, err := readFileLimited(localPath, maxAgentRefImageBytes)
	if err != nil {
		refErr := fmt.Errorf("cannot attach %q: %w", base, err)
		e.agent.attachErr = refErr.Error()
		e.Status = refErr.Error()
		e.notify()
		return AgentReferenceImage{}, refErr
	}
	return e.AttachAgentReferenceBytes(base, data)
}

func resolveReferenceFileInput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if idx := strings.Index(trimmed, "://"); idx > 0 {
		scheme := strings.ToLower(trimmed[:idx])
		if scheme != "file" {
			return "", fmt.Errorf("cannot attach %q: unsupported URI scheme %q (only local PNG, JPEG, or WebP files are supported)", trimmed, scheme)
		}
		// Always validate host (reject non-localhost remote hosts) and normalize Windows drive paths first.
		p, err := parseLocalFileInput(trimmed)
		if err != nil {
			return "", fmt.Errorf("cannot attach %q: %w", trimmed, err)
		}
		if f, ferr := core.FileFromURL(trimmed); ferr == nil && f.Path != "" {
			return f.Path, nil
		}
		return p, nil
	}
	return parseLocalFileInput(trimmed)
}

// DropAgentReferenceFiles attaches one or more dropped file paths or file:// URLs onto the Agent composer.
// Valid PNG/JPEG/WebP files are attached as chips; unsupported files are refused with a clear reason.
func (e *Editor) DropAgentReferenceFiles(uris []string) ([]AgentReferenceImage, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	e.agent.composerDragOver = false
	if e.playing {
		e.notify()
		return nil, ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		e.notify()
		return nil, errors.New("editor: agent is disabled")
	}
	var attached []AgentReferenceImage
	var errs []string
	for _, raw := range uris {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		img, err := e.AttachAgentReferenceFile(raw)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		attached = append(attached, img)
	}
	if len(errs) > 0 {
		msg := strings.Join(errs, "; ")
		e.agent.attachErr = msg
		e.Status = msg
		e.notify()
		return attached, errors.New(msg)
	}
	e.notify()
	return attached, nil
}

// HandleAgentComposerPaste processes a core.PasteEvent from the Agent composer input.
// Returns (attached, handled, err). Plain text pastes return (nil, false, nil) so the input inserts text normally.
func (e *Editor) HandleAgentComposerPaste(ev core.PasteEvent) ([]AgentReferenceImage, bool, error) {
	if e == nil {
		return nil, false, errors.New("editor: nil editor")
	}
	if len(ev.Files) == 0 && len(ev.URIs) == 0 {
		return nil, false, nil
	}
	if e.playing {
		return nil, true, ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return nil, false, errors.New("editor: agent is disabled")
	}
	var attached []AgentReferenceImage
	var errs []string
	for _, f := range ev.Files {
		name := strings.TrimSpace(f.Name)
		if name == "" && f.MIME != "" {
			switch strings.ToLower(strings.TrimSpace(f.MIME)) {
			case "image/png":
				name = "pasted.png"
			case "image/jpeg", "image/jpg":
				name = "pasted.jpg"
			case "image/webp":
				name = "pasted.webp"
			}
		}
		img, err := e.AttachAgentReferenceBytes(name, f.Data)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		attached = append(attached, img)
	}
	for _, uri := range ev.URIs {
		if strings.TrimSpace(uri) == "" {
			continue
		}
		img, err := e.AttachAgentReferenceFile(uri)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		attached = append(attached, img)
	}
	if len(errs) > 0 {
		msg := strings.Join(errs, "; ")
		e.agent.attachErr = msg
		e.Status = msg
		e.notify()
		return attached, true, errors.New(msg)
	}
	return attached, true, nil
}

// PasteAgentReferenceImage reads an image or copied file(s) from the clipboard (via Editor.ReadClipboardImage /
// Editor.ReadClipboardFiles when set, or the attached UI scope) and attaches them to the composer.
func (e *Editor) PasteAgentReferenceImage() ([]AgentReferenceImage, bool, error) {
	if e == nil {
		return nil, false, errors.New("editor: nil editor")
	}
	if e.playing {
		return nil, false, ErrPlaying
	}
	if e.ReadClipboardImage != nil {
		var (
			gotImg core.ClipboardImage
			gotOK  bool
			gotErr error
			called bool
		)
		e.ReadClipboardImage(func(img core.ClipboardImage, ok bool, err error) {
			gotImg, gotOK, gotErr, called = img, ok, err, true
		})
		if called {
			if gotErr != nil {
				e.agent.attachErr = gotErr.Error()
				e.notify()
				return nil, true, gotErr
			}
			if gotOK && len(gotImg.PNG) > 0 {
				img, err := e.AttachAgentReferenceBytes("pasted.png", gotImg.PNG)
				if err != nil {
					return nil, true, err
				}
				return []AgentReferenceImage{img}, true, nil
			}
		}
	}
	if e.ReadClipboardFiles != nil {
		var uris []string
		e.ReadClipboardFiles(func(u []string) { uris = u })
		if len(uris) > 0 {
			attached, err := e.DropAgentReferenceFiles(uris)
			return attached, true, err
		}
	}
	if cx := e.headless.getScope(); cx != nil && !cx.Disposed() {
		startRoot := e.agentProjectRoot()
		cx.ReadClipboardImage(func(img core.ClipboardImage, ok bool, err error) {
			if cx.Disposed() || e.playing || e.agent.mode == AgentModeNone || e.agentProjectRoot() != startRoot {
				return
			}
			if err != nil {
				e.agent.attachErr = err.Error()
				e.Status = err.Error()
				e.notify()
				return
			}
			if ok && len(img.PNG) > 0 {
				_, _ = e.AttachAgentReferenceBytes("pasted.png", img.PNG)
				return
			}
			cx.ReadClipboardFiles(func(uris []string) {
				if cx.Disposed() || e.playing || e.agent.mode == AgentModeNone || e.agentProjectRoot() != startRoot {
					return
				}
				if len(uris) > 0 {
					_, _ = e.DropAgentReferenceFiles(uris)
				}
			})
		})
		return nil, true, nil
	}
	return nil, false, nil
}

// PickAndAttachAgentReferenceImages opens the native file picker (or Editor.PickAgentReferenceFiles in tests)
// for PNG, JPEG, and WebP images and attaches any chosen files to the next request.
func (e *Editor) PickAndAttachAgentReferenceImages() {
	if e == nil || e.playing || e.agent.mode == AgentModeNone {
		return
	}
	if e.PickAgentReferenceFiles != nil {
		e.PickAgentReferenceFiles(func(uris []string) {
			if len(uris) > 0 {
				_, _ = e.DropAgentReferenceFiles(uris)
			}
		})
		return
	}
	cx := e.headless.getScope()
	if cx == nil || cx.Disposed() {
		return
	}
	startDir := e.agentProjectRoot()
	cx.PickFiles(core.PickFilesOptions{
		Multiple:  true,
		Types:     []string{"image/png", "image/jpeg", "image/webp", ".png", ".jpg", ".jpeg", ".webp"},
		TypesName: "Images (PNG, JPEG, WebP)",
		Title:     "Attach reference image",
		Button:    "Attach",
		Start:     startDir,
	}, func(files []core.File, err error) {
		if err != nil || len(files) == 0 || cx.Disposed() || e.playing || e.agent.mode == AgentModeNone || e.agentProjectRoot() != startDir {
			return
		}
		uris := make([]string, 0, len(files))
		for _, f := range files {
			if f.Path != "" {
				uris = append(uris, f.Path)
			}
		}
		if len(uris) > 0 {
			_, _ = e.DropAgentReferenceFiles(uris)
		}
	})
}

// RemoveAgentReferenceImage removes an attached one-shot image chip from the current composer draft.
func (e *Editor) RemoveAgentReferenceImage(id string) bool {
	if e == nil || id == "" {
		return false
	}
	idx := -1
	for i := range e.agent.attachedImages {
		if e.agent.attachedImages[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	e.agent.attachedImages = append(e.agent.attachedImages[:idx], e.agent.attachedImages[idx+1:]...)
	if e.agent.previewImageID == id {
		e.agent.previewImageID = ""
	}
	e.notify()
	return true
}

// ClearAgentAttachedImages removes all one-shot reference image attachments from the current composer draft.
func (e *Editor) ClearAgentAttachedImages() {
	if e == nil || len(e.agent.attachedImages) == 0 {
		return
	}
	e.agent.attachedImages = nil
	if e.agent.previewImageID != "" && e.agent.previewImageID != AgentBriefPreviewID {
		e.agent.previewImageID = ""
	}
	e.notify()
}

// verifyBriefReadPath ensures absPath is lexically inside rootDir, traverses no symlinks
// anywhere below rootDir (file or intermediate directories), and resolves within rootDir.
func verifyBriefReadPath(rootDir, absPath, relLabel string) error {
	if err := verifyAssetGenWritePath(rootDir, absPath, relLabel); err != nil {
		return err
	}
	return verifyFileWithinRoot(rootDir, absPath, relLabel)
}

func (e *Editor) readBriefMeta() (exists bool, refs []BriefReference) {
	root := e.agentProjectRoot()
	if root == "" {
		return false, nil
	}
	briefAbs := filepath.Join(root, project.BriefFileName)
	info, err := os.Lstat(briefAbs)
	if err != nil {
		e.agent.briefMeta = cachedBriefMeta{valid: true, path: briefAbs, exists: false}
		return false, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		e.agent.briefMeta = cachedBriefMeta{valid: true, path: briefAbs, exists: false}
		return false, nil
	}
	if e.agent.briefMeta.valid &&
		e.agent.briefMeta.path == briefAbs &&
		e.agent.briefMeta.exists &&
		e.agent.briefMeta.size == info.Size() &&
		e.agent.briefMeta.modTime.Equal(info.ModTime()) {
		return true, slices.Clone(e.agent.briefMeta.refs)
	}
	if err := verifyBriefReadPath(root, briefAbs, project.BriefFileName); err != nil {
		e.agent.briefMeta = cachedBriefMeta{valid: true, path: briefAbs, exists: false}
		return false, nil
	}
	data, err := readFileLimited(briefAbs, maxAgentBriefYAMLBytes)
	if err != nil {
		e.agent.briefMeta = cachedBriefMeta{valid: true, path: briefAbs, exists: true, modTime: info.ModTime(), size: info.Size()}
		return true, nil
	}
	parsed := parseBriefReferencesYAML(data)
	e.agent.briefMeta = cachedBriefMeta{
		valid:   true,
		path:    briefAbs,
		exists:  true,
		modTime: info.ModTime(),
		size:    info.Size(),
		refs:    slices.Clone(parsed),
	}
	return true, parsed
}

// BriefFileExists reports whether brief.yaml exists as a regular confined file at the open project root.
func (e *Editor) BriefFileExists() bool {
	exists, _ := e.readBriefMeta()
	return exists
}

// BriefReferences reads the list of pinned reference images under art.references in <projectRoot>/brief.yaml.
func (e *Editor) BriefReferences() []BriefReference {
	_, refs := e.readBriefMeta()
	return refs
}

func parseBriefReferencesYAML(data []byte) []BriefReference {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	artNode := yamlMapValue(doc, "art")
	if artNode == nil || artNode.Kind != yaml.MappingNode {
		return nil
	}
	refsNode := yamlMapValue(artNode, "references")
	if refsNode == nil || refsNode.Kind != yaml.SequenceNode {
		return nil
	}
	var out []BriefReference
	for _, item := range refsNode.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			p := cleanBriefRefPath(item.Value)
			if p != "" {
				out = append(out, BriefReference{Path: p})
			}
		case yaml.MappingNode:
			pNode := yamlMapValue(item, "path")
			if pNode == nil || pNode.Kind != yaml.ScalarNode {
				continue
			}
			p := cleanBriefRefPath(pNode.Value)
			if p == "" {
				continue
			}
			var note string
			if nNode := yamlMapValue(item, "note"); nNode != nil && nNode.Kind == yaml.ScalarNode {
				note = strings.TrimSpace(nNode.Value)
			}
			out = append(out, BriefReference{Path: p, Note: note})
		}
	}
	return out
}

func cleanBriefRefPath(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if s == "" {
		return ""
	}
	s = path.Clean(s)
	s = strings.TrimPrefix(s, "./")
	if s == "." || s == ".." || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/") {
		return ""
	}
	return s
}

func yamlMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// BriefReferenceImages loads and validates all pinned PNG/JPEG/WebP reference images listed in brief.yaml.
// Valid images are cached by file modtime and size so repeated UI renders do not re-decode from disk.
func (e *Editor) BriefReferenceImages() []AgentReferenceImage {
	root := e.agentProjectRoot()
	if root == "" {
		return nil
	}
	refs := e.BriefReferences()
	if len(refs) == 0 {
		return nil
	}
	if e.agent.briefRefCache == nil {
		e.agent.briefRefCache = make(map[string]cachedBriefRefImage)
	}
	var out []AgentReferenceImage
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if len(out) >= maxAgentBriefRefImages {
			break
		}
		if seen[ref.Path] {
			continue
		}
		seen[ref.Path] = true
		abs := filepath.Join(root, filepath.FromSlash(ref.Path))
		if err := verifyBriefReadPath(root, abs, ref.Path); err != nil {
			continue
		}
		info, err := os.Lstat(abs)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAgentRefImageBytes {
			continue
		}
		if cached, ok := e.agent.briefRefCache[ref.Path]; ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
			img := cached.img.clone()
			img.Note = ref.Note
			out = append(out, img)
			continue
		}
		data, err := readFileLimited(abs, maxAgentRefImageBytes)
		if err != nil {
			continue
		}
		decoded, err := DecodeAgentReferenceImage(path.Base(ref.Path), data)
		if err != nil {
			continue
		}
		e.agent.attachImgVer++
		decoded.ID = "brief:" + ref.Path
		decoded.Name = path.Base(ref.Path)
		decoded.Path = ref.Path
		decoded.Pinned = true
		decoded.FromBrief = true
		decoded.Note = ref.Note
		decoded.ImageVersion = e.agent.attachImgVer
		e.agent.briefRefCache[ref.Path] = cachedBriefRefImage{
			modTime: info.ModTime(),
			size:    info.Size(),
			img:     decoded.clone(),
		}
		out = append(out, decoded)
	}
	for k := range e.agent.briefRefCache {
		if !seen[k] {
			delete(e.agent.briefRefCache, k)
		}
	}
	return out
}

// PinAgentReferenceImage copies the attached image with the given ID to art/reference/<name>
// and adds it to art.references in brief.yaml as one atomic, undoable Editor.History step.
// Undo removes both the copied file and the YAML entry cleanly.
func (e *Editor) PinAgentReferenceImage(id, note string) (BriefPinNotice, error) {
	if e == nil {
		return BriefPinNotice{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return BriefPinNotice{}, ErrPlaying
	}
	if e.MergeConflictOpen() {
		return BriefPinNotice{}, ErrMergeConflictInProgress
	}
	if e.HistoryPreviewActive() {
		return BriefPinNotice{}, ErrHistoryPreview
	}
	root := e.agentProjectRoot()
	if root == "" {
		return BriefPinNotice{}, ErrNoProject
	}
	idx := -1
	for i := range e.agent.attachedImages {
		if e.agent.attachedImages[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return BriefPinNotice{}, fmt.Errorf("editor: attached image %q not found", id)
	}
	img := e.agent.attachedImages[idx]
	if img.Pinned && img.Path != "" {
		if e.agent.lastPinNotice != nil && e.agent.lastPinNotice.ImageID == id {
			return *e.AgentLastPinNotice(), nil
		}
		return BriefPinNotice{
			ImageID: id,
			Name:    path.Base(img.Path),
			RelPath: img.Path,
			Note:    strings.TrimSpace(note),
		}, nil
	}

	// Resolve unique target file path under art/reference/<name>.
	relPath, absImagePath, fileExisted, err := resolvePinImageTarget(root, img.Name, img.Data)
	if err != nil {
		return BriefPinNotice{}, err
	}
	artDirAbs := filepath.Join(root, "art")
	refDirAbs := filepath.Join(root, "art", "reference")
	_, errArt := os.Stat(artDirAbs)
	artDirExisted := errArt == nil
	_, errRef := os.Stat(refDirAbs)
	refDirExisted := errRef == nil

	briefAbs := filepath.Join(root, project.BriefFileName)
	if err := verifyAssetGenWritePath(root, briefAbs, project.BriefFileName); err != nil {
		return BriefPinNotice{}, err
	}
	var beforeBrief []byte
	briefExisted := false
	if fi, statErr := os.Lstat(briefAbs); statErr == nil {
		if !fi.Mode().IsRegular() {
			return BriefPinNotice{}, fmt.Errorf("editor: %s is not a regular file", project.BriefFileName)
		}
		if err := verifyBriefReadPath(root, briefAbs, project.BriefFileName); err != nil {
			return BriefPinNotice{}, err
		}
		beforeBrief, err = readFileLimited(briefAbs, maxAgentBriefYAMLBytes)
		if err != nil {
			return BriefPinNotice{}, fmt.Errorf("editor: read brief.yaml: %w", err)
		}
		briefExisted = true
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return BriefPinNotice{}, statErr
	}

	trimmedNote := sanitizeSingleLineMeta(note)
	afterBrief, err := appendBriefReferenceYAML(beforeBrief, relPath, trimmedNote)
	if err != nil {
		return BriefPinNotice{}, err
	}
	diffLines := buildBriefPinDiff(beforeBrief, afterBrief)
	notice := BriefPinNotice{
		ImageID: id,
		Name:    path.Base(relPath),
		RelPath: relPath,
		Note:    trimmedNote,
		Diff:    diffLines,
	}

	cmd := &pinReferenceImageCmd{
		ed:            e,
		rootDir:       root,
		label:         fmt.Sprintf("Pin %s to brief", notice.Name),
		imageID:       id,
		relPath:       relPath,
		absImagePath:  absImagePath,
		imageBytes:    slices.Clone(img.Data),
		fileExisted:   fileExisted,
		artDirAbs:     artDirAbs,
		artDirExisted: artDirExisted,
		refDirAbs:     refDirAbs,
		refDirExisted: refDirExisted,
		briefAbs:      briefAbs,
		briefExisted:  briefExisted,
		beforeBrief:   slices.Clone(beforeBrief),
		afterBrief:    slices.Clone(afterBrief),
		notice:        notice,
	}
	if err := e.History.execute(cmd); err != nil {
		return BriefPinNotice{}, err
	}
	return *e.AgentLastPinNotice(), nil
}

// CanUndoLastBriefPin reports whether the most recent Pin to brief command is currently at the top
// of Editor.History and can be undone safely without reverting unrelated later edits.
func (e *Editor) CanUndoLastBriefPin() bool {
	if e == nil || e.playing || e.MergeConflictOpen() || e.HistoryPreviewActive() {
		return false
	}
	if e.agent.lastPinCmd == nil || e.History == nil {
		return false
	}
	return e.History.topUndoCmd() == e.agent.lastPinCmd
}

// UndoLastBriefPin undoes the Pin to brief step via Editor.Undo() only when it is still the latest
// command in Editor.History, never silently undoing unrelated later edits.
func (e *Editor) UndoLastBriefPin() bool {
	if !e.CanUndoLastBriefPin() {
		return false
	}
	return e.Undo()
}

func resolvePinImageTarget(root, name string, data []byte) (relPath, absPath string, existedSame bool, err error) {
	base := filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if base == "" || base == "." || base == ".." {
		base = "reference.png"
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for attempt := 1; attempt <= 999; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s_%d%s", stem, attempt, ext)
		}
		rel := path.Join(agentRefFolderRel, candidate)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := verifyAssetGenWritePath(root, abs, rel); err != nil {
			return "", "", false, err
		}
		fi, lerr := os.Lstat(abs)
		if errors.Is(lerr, fs.ErrNotExist) {
			return rel, abs, false, nil
		}
		if lerr != nil {
			return "", "", false, lerr
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", "", false, fmt.Errorf("editor: asset path %q refuses symlink file", rel)
		}
		if !fi.Mode().IsRegular() || fi.Size() != int64(len(data)) {
			// Different size or directory: allocate a distinct suffixed filename without reading it.
			continue
		}
		existing, rerr := readFileLimited(abs, maxAgentRefImageBytes)
		if rerr != nil {
			return "", "", false, rerr
		}
		if bytes.Equal(existing, data) {
			return rel, abs, true, nil
		}
	}
	return "", "", false, fmt.Errorf("editor: could not allocate unique filename under %s for %q", agentRefFolderRel, base)
}

// appendBriefReferenceYAML parses existing brief.yaml (if non-empty) and appends an entry under art.references,
// preserving all existing keys, ordering, and comments. When creating a new brief.yaml, it emits a canonical
// version 1 header with BriefSchemaComment so project.ParseBrief accepts it cleanly.
func appendBriefReferenceYAML(existing []byte, relPath, note string) ([]byte, error) {
	var doc yaml.Node
	fresh := len(bytes.TrimSpace(existing)) == 0
	if fresh {
		doc = yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{
				{
					Kind: yaml.MappingNode,
					Tag:  "!!map",
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"},
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", project.BriefVersion)},
					},
				},
			},
		}
	} else {
		if err := yaml.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("editor: parse brief.yaml: %w", err)
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			fresh = true
			doc = yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode,
						Tag:  "!!map",
						Content: []*yaml.Node{
							{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"},
							{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", project.BriefVersion)},
						},
					},
				},
			}
		}
		if doc.Content[0].Kind != yaml.MappingNode {
			return nil, errors.New("editor: brief.yaml root must be a YAML mapping")
		}
	}

	rootMap := doc.Content[0]
	artNode := yamlMapValue(rootMap, "art")
	if artNode == nil {
		artNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		rootMap.Content = append(rootMap.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "art"},
			artNode,
		)
	} else if artNode.Kind != yaml.MappingNode {
		return nil, errors.New("editor: brief.yaml art field must be a YAML mapping")
	}

	refsNode := yamlMapValue(artNode, "references")
	if refsNode == nil {
		refsNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		artNode.Content = append(artNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "references"},
			refsNode,
		)
	} else if refsNode.Kind != yaml.SequenceNode {
		return nil, errors.New("editor: brief.yaml art.references field must be a YAML sequence")
	}

	// Check if relPath is already present; if so, update note if requested and avoid duplicating the entry.
	for _, item := range refsNode.Content {
		if item.Kind == yaml.ScalarNode && cleanBriefRefPath(item.Value) == relPath {
			return encodeBriefYAMLNode(&doc, fresh)
		}
		if item.Kind == yaml.MappingNode {
			if pNode := yamlMapValue(item, "path"); pNode != nil && cleanBriefRefPath(pNode.Value) == relPath {
				if note != "" {
					if nNode := yamlMapValue(item, "note"); nNode != nil {
						nNode.Value = note
					} else {
						item.Content = append(item.Content,
							&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "note"},
							&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: note},
						)
					}
				}
				return encodeBriefYAMLNode(&doc, fresh)
			}
		}
	}

	entry := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: relPath},
		},
	}
	if note != "" {
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "note"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: note},
		)
	}
	refsNode.Content = append(refsNode.Content, entry)
	return encodeBriefYAMLNode(&doc, fresh)
}

func encodeBriefYAMLNode(doc *yaml.Node, withSchemaComment bool) ([]byte, error) {
	var buf bytes.Buffer
	if withSchemaComment {
		buf.WriteString(project.BriefSchemaComment)
		buf.WriteByte('\n')
	}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: encode brief.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: encode brief.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// buildBriefPinDiff produces a concise diff showing the surrounding brief.yaml lines and the newly added lines.
func buildBriefPinDiff(before, after []byte) []BriefPinDiffLine {
	oldLines := splitBriefDiffLines(before)
	newLines := splitBriefDiffLines(after)
	oldSet := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		oldSet[l]++
	}
	var all []BriefPinDiffLine
	firstAdded := -1
	for _, l := range newLines {
		if oldSet[l] > 0 {
			oldSet[l]--
			all = append(all, BriefPinDiffLine{Sign: "", Text: l})
		} else {
			if firstAdded < 0 {
				firstAdded = len(all)
			}
			all = append(all, BriefPinDiffLine{Sign: "+", Text: l, Added: true})
		}
	}
	if firstAdded < 0 || len(all) <= 8 {
		return all
	}
	start := max(0, firstAdded-4)
	end := min(len(all), firstAdded+4)
	return all[start:end]
}

func splitBriefDiffLines(data []byte) []string {
	s := strings.TrimRight(string(data), "\r\n")
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	for i := range raw {
		raw[i] = strings.TrimRight(raw[i], "\r")
	}
	return raw
}

// pinReferenceImageCmd copies an attached reference image to art/reference/<name> and adds it to brief.yaml
// in one atomic, undoable Editor.History step.
type pinReferenceImageCmd struct {
	ed            *Editor
	rootDir       string
	label         string
	imageID       string
	relPath       string
	absImagePath  string
	imageBytes    []byte
	fileExisted   bool
	artDirAbs     string
	artDirExisted bool
	refDirAbs     string
	refDirExisted bool
	briefAbs      string
	briefExisted  bool
	beforeBrief   []byte
	afterBrief    []byte
	notice        BriefPinNotice
}

func (c *pinReferenceImageCmd) Label() string { return c.label }
func (c *pinReferenceImageCmd) Do()           { c.ed.report(c.do()) }
func (c *pinReferenceImageCmd) Undo()         { c.ed.report(c.undo()) }

func (c *pinReferenceImageCmd) do() error {
	// Re-verify symlink containment on every Do/Redo to guard against TOCTOU symlink swaps.
	if err := verifyAssetGenWritePath(c.rootDir, c.absImagePath, c.relPath); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(c.rootDir, c.briefAbs, project.BriefFileName); err != nil {
		return err
	}
	if !c.fileExisted {
		if err := os.MkdirAll(filepath.Dir(c.absImagePath), 0o755); err != nil {
			return fmt.Errorf("editor: create %s: %w", agentRefFolderRel, err)
		}
		if err := verifyAssetGenWritePath(c.rootDir, c.absImagePath, c.relPath); err != nil {
			return err
		}
		if err := writeFileAtomic(c.absImagePath, c.imageBytes); err != nil {
			return fmt.Errorf("editor: write %s: %w", c.relPath, err)
		}
	}
	if err := writeFileAtomic(c.briefAbs, c.afterBrief); err != nil {
		// Roll back the image file we just created if brief.yaml write fails.
		if !c.fileExisted {
			_ = os.Remove(c.absImagePath)
			c.cleanupEmptyRefDirs()
		}
		return fmt.Errorf("editor: write brief.yaml: %w", err)
	}
	c.setAttachmentPinned(true, c.relPath)
	noticeCopy := c.notice
	noticeCopy.Diff = slices.Clone(c.notice.Diff)
	c.ed.agent.lastPinNotice = &noticeCopy
	c.ed.agent.lastPinCmd = c
	c.ed.Status = fmt.Sprintf("Pinned %s to brief.yaml", c.notice.Name)
	c.refreshProjectAfterPin()
	return nil
}

func (c *pinReferenceImageCmd) undo() error {
	// Re-verify symlink containment before restoring or removing files on Undo.
	if err := verifyAssetGenWritePath(c.rootDir, c.briefAbs, project.BriefFileName); err != nil {
		return err
	}
	if !c.fileExisted {
		if err := verifyAssetGenWritePath(c.rootDir, c.absImagePath, c.relPath); err != nil {
			return err
		}
	}
	if c.briefExisted {
		if err := writeFileAtomic(c.briefAbs, c.beforeBrief); err != nil {
			return fmt.Errorf("editor: restore brief.yaml: %w", err)
		}
	} else {
		if err := os.Remove(c.briefAbs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("editor: remove brief.yaml: %w", err)
		}
	}
	if !c.fileExisted {
		if err := os.Remove(c.absImagePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("editor: remove %s: %w", c.relPath, err)
		}
		c.cleanupEmptyRefDirs()
	}
	c.setAttachmentPinned(false, "")
	if c.ed.agent.lastPinCmd == c {
		c.ed.agent.lastPinNotice = nil
		c.ed.agent.lastPinCmd = nil
	}
	c.ed.Status = fmt.Sprintf("Undid pin of %s", c.notice.Name)
	c.refreshProjectAfterPin()
	return nil
}

func (c *pinReferenceImageCmd) setAttachmentPinned(pinned bool, relPath string) {
	for i := range c.ed.agent.attachedImages {
		if c.ed.agent.attachedImages[i].ID == c.imageID {
			c.ed.agent.attachedImages[i].Pinned = pinned
			c.ed.agent.attachedImages[i].Path = relPath
			break
		}
	}
}

func (c *pinReferenceImageCmd) cleanupEmptyRefDirs() {
	removeIfSafeEmptyDir := func(absDir, rel string) {
		if err := verifyAssetGenWritePath(c.rootDir, absDir, rel); err != nil {
			return
		}
		if fi, err := os.Lstat(absDir); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return
		}
		_ = os.Remove(absDir) // succeeds only if empty
	}
	if !c.refDirExisted {
		removeIfSafeEmptyDir(c.refDirAbs, agentRefFolderRel)
	}
	if !c.artDirExisted {
		removeIfSafeEmptyDir(c.artDirAbs, "art")
	}
}

func (c *pinReferenceImageCmd) refreshProjectAfterPin() {
	c.ed.invalidateBriefReferenceCaches()
	c.ed.reloadBriefFromDisk()
	c.ed.filesRev++
	c.ed.assetsRev++
	c.ed.invalidateProjectIndex()
	if c.ed.ChangesOpen() {
		_ = c.ed.RefreshChanges()
	} else {
		c.ed.RefreshChangesCount()
	}
	c.ed.CheckProblems()
	c.ed.notify()
}

// collectRequestReferenceImages snapshots the one-shot attached images and the pinned brief.yaml images
// for an outgoing AgentRequest, deduplicating by project-relative path so an image pinned during the
// current composer draft is never sent twice.
func (e *Editor) collectRequestReferenceImages() (attached, brief, combined []AgentReferenceImage) {
	brief = e.BriefReferenceImages()
	briefByPath := make(map[string]bool, len(brief))
	for _, img := range brief {
		if img.Path != "" {
			briefByPath[img.Path] = true
		}
	}
	for _, img := range e.agent.attachedImages {
		cp := img.clone()
		// If the user pinned this chip before clicking Send, it already joins through brief.yaml.
		if cp.Pinned && cp.Path != "" && briefByPath[cp.Path] {
			continue
		}
		attached = append(attached, cp)
	}
	combined = make([]AgentReferenceImage, 0, len(attached)+len(brief))
	combined = append(combined, attached...)
	combined = append(combined, brief...)
	return attached, brief, combined
}
