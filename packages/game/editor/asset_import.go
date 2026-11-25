package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// AssetImportKind identifies which importsettings section applies to an asset file.
type AssetImportKind string

const (
	// AssetImportNone indicates that a file is not an importable asset (for example a scene, YAML file, directory, or sidecar).
	AssetImportNone AssetImportKind = ""
	// AssetImportImage identifies an image asset (.png, .jpg, .jpeg, .webp).
	AssetImportImage AssetImportKind = "image"
	// AssetImportAudio identifies a sound asset (.wav, .ogg, .mp3, .flac).
	AssetImportAudio AssetImportKind = "audio"
	// AssetImportFont identifies a font asset (.ttf, .otf).
	AssetImportFont AssetImportKind = "font"
	// AssetImportModel identifies a 3D model asset (.gltf, .glb).
	AssetImportModel AssetImportKind = "model"
)

// AssetImportKindFor classifies a project-relative file path by its extension, returning AssetImportNone for sidecars and non-asset files.
func AssetImportKindFor(relPath string) AssetImportKind {
	if relPath == "" || importsettings.IsSidecar(relPath) {
		return AssetImportNone
	}
	switch strings.ToLower(path.Ext(relPath)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return AssetImportImage
	case ".wav", ".ogg", ".mp3", ".flac":
		return AssetImportAudio
	case ".ttf", ".otf":
		return AssetImportFont
	case ".gltf", ".glb":
		return AssetImportModel
	default:
		return AssetImportNone
	}
}

// AssetImportKind returns the import asset kind of relPath in the open project, or AssetImportNone when no project is open, relPath is a directory, missing, a sidecar, a generated target output, or not an importable asset.
func (e *Editor) AssetImportKind(relPath string) AssetImportKind {
	if e == nil || e.Project == nil || e.ProjectPath == "" || relPath == "" {
		return AssetImportNone
	}
	clean := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	if !fs.ValidPath(clean) || clean == "." {
		return AssetImportNone
	}
	kind := AssetImportKindFor(clean)
	if kind == AssetImportNone {
		return AssetImportNone
	}
	if e.Project.Assets != nil && e.Project.Assets.IsOutput(clean) {
		return AssetImportNone
	}
	abs := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(clean))
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return AssetImportNone
	}
	return kind
}

// inspectedImportAsset returns the project-relative path and kind of the asset currently targeted by InspectorView,
// or ("", AssetImportNone) when InspectorView should show the selected scene object (or empty state) instead.
func (e *Editor) inspectedImportAsset() (string, AssetImportKind) {
	if e == nil || e.SelectedFile == "" {
		return "", AssetImportNone
	}
	if !e.inspectAssetFile && e.SelectedID != "" {
		return "", AssetImportNone
	}
	kind := e.AssetImportKind(e.SelectedFile)
	if kind == AssetImportNone {
		return "", AssetImportNone
	}
	clean, err := normalizeProjectAssetPath(e.SelectedFile)
	if err != nil {
		return "", AssetImportNone
	}
	return clean, kind
}

// AssetImportInfo describes the current import settings, sidecar state, and inherited game.yaml target defaults for one asset in the open project.
type AssetImportInfo struct {
	// Path is the clean slash-separated asset path relative to the project root.
	Path string
	// SidecarPath is the slash-separated path of the asset's *.import.yaml sidecar.
	SidecarPath string
	// Kind is the asset's import kind (image, audio, font, or model).
	Kind AssetImportKind
	// SidecarExists reports whether SidecarPath currently exists on disk.
	SidecarExists bool
	// Sidecar holds the parsed *.import.yaml sidecar overrides, or nil when absent or unparseable.
	Sidecar *importsettings.File
	// SidecarErr holds any read or parse error from an existing sidecar on disk.
	SidecarErr error

	// Matched targets from game.yaml's assets section (nil when no target reads this asset).
	AtlasTarget   *project.AtlasTarget
	TextureTarget *project.TextureTarget
	AudioTarget   *project.AudioTarget
	FontTarget    *project.FontTarget
	ModelTarget   *project.ModelTarget

	// Inherited defaults resolved from the matched target in game.yaml (or the tool's defaults).
	DefaultImageFilter  string
	DefaultImageFormat  string
	DefaultImageMipmaps bool
	DefaultImageSRGB    bool
	// ImageWidth and ImageHeight are the decoded source pixel dimensions when Kind is AssetImportImage (0 if unreadable).
	ImageWidth  int
	ImageHeight int

	DefaultAudioFormat   string
	DefaultAudioRate     int
	DefaultAudioChannels int
	DefaultAudioBitrate  int
	DefaultAudioPeakDB   *float64
	DefaultAudioRMSDB    *float64

	DefaultFontSize    int
	DefaultFontChars   string
	DefaultFontPadding int

	DefaultModelScale      float64
	DefaultModelAnimations bool
}

// TargetSummary returns a short human-readable summary of the game.yaml target(s) reading this asset.
func (info AssetImportInfo) TargetSummary() string {
	var parts []string
	if info.AtlasTarget != nil {
		parts = append(parts, "Atlas: "+info.AtlasTarget.Name)
	}
	if info.TextureTarget != nil {
		parts = append(parts, "Texture: "+info.TextureTarget.Out)
	}
	if info.AudioTarget != nil {
		parts = append(parts, "Audio: "+info.AudioTarget.Out)
	}
	if info.FontTarget != nil {
		parts = append(parts, "Font: "+info.FontTarget.Out)
	}
	if info.ModelTarget != nil {
		parts = append(parts, "Model: "+info.ModelTarget.Out)
	}
	if len(parts) == 0 {
		return "No target in game.yaml"
	}
	return strings.Join(parts, " · ")
}

// EffectiveImageFilter returns the effective sampling filter and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveImageFilter() (string, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Filter != "" {
		return info.Sidecar.Image.Filter, true
	}
	return cmp.Or(info.DefaultImageFilter, "linear"), false
}

// EffectiveImageFormat returns the effective KTX2 texture format and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveImageFormat() (string, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Format != "" {
		return info.Sidecar.Image.Format, true
	}
	return cmp.Or(info.DefaultImageFormat, "rgba8"), false
}

// EffectiveImageMipmaps returns whether mipmaps are enabled and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveImageMipmaps() (bool, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Mipmaps != nil {
		return *info.Sidecar.Image.Mipmaps, true
	}
	return info.DefaultImageMipmaps, false
}

// EffectiveImageSRGB returns whether sRGB is enabled and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveImageSRGB() (bool, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.SRGB != nil {
		return *info.Sidecar.Image.SRGB, true
	}
	return info.DefaultImageSRGB, false
}

// EffectiveImageSlice returns a copy of the sidecar's sprite-sheet slice grid (nil when unsliced) and whether it is set.
func (info AssetImportInfo) EffectiveImageSlice() (*importsettings.Slice, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Slice != nil {
		s := *info.Sidecar.Image.Slice
		return &s, true
	}
	return nil, false
}

// EffectiveImagePivot returns a deep copy of the sidecar's pivot settings (nil when unset) and whether it is set.
func (info AssetImportInfo) EffectiveImagePivot() (*importsettings.Pivot, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Pivot != nil {
		p := cloneImportPivot(info.Sidecar.Image.Pivot)
		return p, true
	}
	return nil, false
}

// EffectiveDefaultImagePivot returns the effective default normalized pivot ([0, 0] when unset) and whether the sidecar explicitly sets it.
func (info AssetImportInfo) EffectiveDefaultImagePivot() (importsettings.PivotPoint, bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Pivot != nil && info.Sidecar.Image.Pivot.Default != nil {
		return *info.Sidecar.Image.Pivot.Default, true
	}
	return importsettings.PivotPoint{0, 0}, false
}

// EffectiveCellImagePivot returns the effective normalized pivot for 0-based cell, whether the cell has its own per-cell override in pivot.cells, and whether it inherits pivot.default.
func (info AssetImportInfo) EffectiveCellImagePivot(cell int) (pt importsettings.PivotPoint, overridden bool, inherited bool) {
	if info.Sidecar != nil && info.Sidecar.Image != nil && info.Sidecar.Image.Pivot != nil {
		p := info.Sidecar.Image.Pivot
		for _, c := range p.Cells {
			if c.Cell == cell {
				return c.Pivot, true, false
			}
		}
		if p.Default != nil {
			return *p.Default, false, true
		}
	}
	return importsettings.PivotPoint{0, 0}, false, false
}

// EffectiveAudioFormat returns the effective audio format and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveAudioFormat() (string, bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil && info.Sidecar.Audio.Format != "" {
		return info.Sidecar.Audio.Format, true
	}
	return cmp.Or(info.DefaultAudioFormat, "wav"), false
}

// EffectiveAudioRate returns the effective sample rate in Hz (0 keeps source) and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveAudioRate() (int, bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil && info.Sidecar.Audio.Rate != 0 {
		return info.Sidecar.Audio.Rate, true
	}
	return info.DefaultAudioRate, false
}

// EffectiveAudioChannels returns the effective channel count (0 keeps source, 1 mono, 2 stereo) and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveAudioChannels() (int, bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil && info.Sidecar.Audio.Channels != 0 {
		return info.Sidecar.Audio.Channels, true
	}
	return info.DefaultAudioChannels, false
}

// EffectiveAudioBitrate returns the effective bitrate in kbps and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveAudioBitrate() (int, bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil && info.Sidecar.Audio.Bitrate != 0 {
		return info.Sidecar.Audio.Bitrate, true
	}
	return cmp.Or(info.DefaultAudioBitrate, 128), false
}

// EffectiveAudioNormalization returns the active normalization mode ("none", "peakDB", or "rmsDB"), its dBFS level, and whether the sidecar overrides normalization.
func (info AssetImportInfo) EffectiveAudioNormalization() (mode string, db float64, overridden bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil {
		if info.Sidecar.Audio.PeakDB != nil {
			return "peakDB", *info.Sidecar.Audio.PeakDB, true
		}
		if info.Sidecar.Audio.RMSDB != nil {
			return "rmsDB", *info.Sidecar.Audio.RMSDB, true
		}
	}
	if info.DefaultAudioPeakDB != nil {
		return "peakDB", *info.DefaultAudioPeakDB, false
	}
	if info.DefaultAudioRMSDB != nil {
		return "rmsDB", *info.DefaultAudioRMSDB, false
	}
	return "none", 0, false
}

// EffectiveAudioLoop returns a copy of the sidecar's loop region (nil when unset) and whether it is set.
func (info AssetImportInfo) EffectiveAudioLoop() (*importsettings.Loop, bool) {
	if info.Sidecar != nil && info.Sidecar.Audio != nil && info.Sidecar.Audio.Loop != nil {
		l := *info.Sidecar.Audio.Loop
		return &l, true
	}
	return nil, false
}

// EffectiveFontSize returns the effective font pixel size and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveFontSize() (int, bool) {
	if info.Sidecar != nil && info.Sidecar.Font != nil && info.Sidecar.Font.Size != 0 {
		return info.Sidecar.Font.Size, true
	}
	return cmp.Or(info.DefaultFontSize, project.DefaultFontSize), false
}

// EffectiveFontChars returns the effective character set and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveFontChars() (string, bool) {
	if info.Sidecar != nil && info.Sidecar.Font != nil && info.Sidecar.Font.Chars != "" {
		return info.Sidecar.Font.Chars, true
	}
	return cmp.Or(info.DefaultFontChars, project.DefaultFontChars), false
}

// EffectiveFontPadding returns the effective glyph padding in pixels and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveFontPadding() (int, bool) {
	if info.Sidecar != nil && info.Sidecar.Font != nil && info.Sidecar.Font.Padding != nil {
		return *info.Sidecar.Font.Padding, true
	}
	return info.DefaultFontPadding, false
}

// EffectiveModelScale returns the effective model scale multiplier and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveModelScale() (float64, bool) {
	if info.Sidecar != nil && info.Sidecar.Model != nil && info.Sidecar.Model.Scale != 0 {
		return info.Sidecar.Model.Scale, true
	}
	if info.DefaultModelScale > 0 {
		return info.DefaultModelScale, false
	}
	return 1, false
}

// EffectiveModelAnimations returns whether animations are kept and whether the sidecar overrides it.
func (info AssetImportInfo) EffectiveModelAnimations() (bool, bool) {
	if info.Sidecar != nil && info.Sidecar.Model != nil && info.Sidecar.Model.Animations != nil {
		return *info.Sidecar.Model.Animations, true
	}
	return info.DefaultModelAnimations, false
}

// EffectiveModelLODs returns a copy of the sidecar's declared model LOD levels and whether any are set.
func (info AssetImportInfo) EffectiveModelLODs() ([]importsettings.ModelLOD, bool) {
	if info.Sidecar != nil && info.Sidecar.Model != nil && len(info.Sidecar.Model.LODs) > 0 {
		return slices.Clone(info.Sidecar.Model.LODs), true
	}
	return nil, false
}

// EffectiveModelLODHysteresis returns the model LOD hysteresis deadband [0, 0.5] and whether the sidecar sets it.
func (info AssetImportInfo) EffectiveModelLODHysteresis() (float32, bool) {
	if info.Sidecar != nil && info.Sidecar.Model != nil && info.Sidecar.Model.LODHysteresis > 0 {
		return info.Sidecar.Model.LODHysteresis, true
	}
	return 0, false
}

// EffectiveCredit returns a copy of the sidecar's credit section, or nil when unset.
func (info AssetImportInfo) EffectiveCredit() *importsettings.Credit {
	if info.Sidecar != nil && info.Sidecar.Credit != nil {
		c := *info.Sidecar.Credit
		return &c
	}
	return nil
}

// AssetImportSettings reads and parses the *.import.yaml sidecar beside relPath in the open project.
// It returns (nil, false, nil) when no sidecar file exists on disk.
func (e *Editor) AssetImportSettings(relPath string) (*importsettings.File, bool, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil, false, ErrNoProject
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return nil, false, err
	}
	absSidecar := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(importsettings.Path(clean)))
	data, err := os.ReadFile(absSidecar)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	f, err := importsettings.Parse(data)
	if err != nil {
		return nil, true, err
	}
	return f, true, nil
}

// InspectAssetImport returns the resolved import settings and game.yaml target defaults for relPath in the open project.
func (e *Editor) InspectAssetImport(relPath string) (AssetImportInfo, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return AssetImportInfo{}, ErrNoProject
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return AssetImportInfo{}, err
	}
	kind := e.AssetImportKind(clean)
	if kind == AssetImportNone {
		return AssetImportInfo{}, fmt.Errorf("editor: %q is not an importable asset", relPath)
	}

	info := AssetImportInfo{
		Path:                   clean,
		SidecarPath:            importsettings.Path(clean),
		Kind:                   kind,
		DefaultImageFilter:     "linear",
		DefaultImageFormat:     "rgba8",
		DefaultAudioFormat:     "wav",
		DefaultAudioBitrate:    128,
		DefaultFontSize:        project.DefaultFontSize,
		DefaultFontChars:       project.DefaultFontChars,
		DefaultFontPadding:     1,
		DefaultModelScale:      1,
		DefaultModelAnimations: true,
	}

	if a := e.Project.Assets; a != nil {
		switch kind {
		case AssetImportImage:
			for i := range a.Atlases {
				at := a.Atlases[i]
				if strings.EqualFold(path.Ext(clean), ".png") && assetScannedUnder(clean, path.Clean(at.In)) {
					cp := at
					info.AtlasTarget = &cp
					break
				}
			}
			for i := range a.Textures {
				t := a.Textures[i]
				if path.Clean(t.In) == clean {
					cp := t
					info.TextureTarget = &cp
					if t.Format != "" {
						info.DefaultImageFormat = t.Format
					}
					info.DefaultImageMipmaps = t.Mipmaps
					info.DefaultImageSRGB = t.SRGB
					break
				}
			}
		case AssetImportAudio:
			for i := range a.Audio {
				au := a.Audio[i]
				if path.Clean(au.In) == clean {
					cp := au
					info.AudioTarget = &cp
					if au.Format != "" {
						info.DefaultAudioFormat = au.Format
					}
					info.DefaultAudioRate = au.Rate
					info.DefaultAudioChannels = au.Channels
					if au.Bitrate > 0 {
						info.DefaultAudioBitrate = au.Bitrate
					}
					if au.PeakDB != nil {
						v := *au.PeakDB
						info.DefaultAudioPeakDB = &v
					}
					if au.RMSDB != nil {
						v := *au.RMSDB
						info.DefaultAudioRMSDB = &v
					}
					break
				}
			}
		case AssetImportFont:
			for i := range a.Fonts {
				ft := a.Fonts[i]
				if path.Clean(ft.In) == clean {
					cp := ft
					info.FontTarget = &cp
					if ft.Size > 0 {
						info.DefaultFontSize = ft.Size
					}
					if ft.Chars != "" {
						info.DefaultFontChars = ft.Chars
					}
					if ft.Padding != nil {
						info.DefaultFontPadding = *ft.Padding
					}
					break
				}
			}
		case AssetImportModel:
			for i := range a.Models {
				m := a.Models[i]
				if path.Clean(m.In) == clean {
					cp := m
					info.ModelTarget = &cp
					if m.Scale > 0 {
						info.DefaultModelScale = m.Scale
					}
					if m.Animations != nil {
						info.DefaultModelAnimations = *m.Animations
					}
					break
				}
			}
		}
	}

	sidecar, exists, sidecarErr := e.AssetImportSettings(clean)
	info.SidecarExists = exists
	info.Sidecar = sidecar
	info.SidecarErr = sidecarErr
	if kind == AssetImportImage {
		if _, w, h, err := e.loadImportPreviewImage(clean); err == nil {
			info.ImageWidth = w
			info.ImageHeight = h
		}
	}
	return info, nil
}

// EditAssetImportSettings reads the sidecar for relPath (creating an empty version 1 file when absent), applies edit, prunes empty sections, validates and encodes the result through importsettings.Encode (or removes the sidecar when every section is cleared), and writes it on disk as one undoable History step.
// Undo restores the exact previous sidecar bytes, or removes a sidecar that did not exist before.
func (e *Editor) EditAssetImportSettings(relPath, label string, edit func(f *importsettings.File)) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if edit == nil {
		return nil
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return err
	}
	if e.AssetImportKind(clean) == AssetImportNone {
		return fmt.Errorf("editor: %q is not an importable asset", relPath)
	}

	sidecarRel := importsettings.Path(clean)
	absPath := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(sidecarRel))

	var beforeBytes []byte
	var beforeExists bool
	var cur *importsettings.File
	raw, err := os.ReadFile(absPath)
	switch {
	case err == nil:
		beforeExists = true
		beforeBytes = raw
		cur, err = importsettings.Parse(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", sidecarRel, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		cur = &importsettings.File{Version: importsettings.CurrentVersion}
	default:
		return err
	}

	next := cloneImportFile(cur)
	next.Version = importsettings.CurrentVersion
	edit(next)
	pruneEmptyImportSections(next)

	var afterExists bool
	var afterBytes []byte
	if len(next.Sections()) > 0 || next.Credit != nil {
		encoded, err := importsettings.Encode(next)
		if err != nil {
			return err
		}
		afterExists = true
		afterBytes = encoded
	}

	if beforeExists == afterExists && bytes.Equal(beforeBytes, afterBytes) {
		return nil
	}

	if label == "" {
		label = "Edit import settings for " + clean
	}
	return e.History.execute(&importSettingsCmd{
		ed:           e,
		label:        label,
		relPath:      clean,
		sidecarRel:   sidecarRel,
		absPath:      absPath,
		beforeExists: beforeExists,
		beforeBytes:  bytes.Clone(beforeBytes),
		afterExists:  afterExists,
		afterBytes:   bytes.Clone(afterBytes),
	})
}

// SetImageImportFilter sets or clears (when empty) the image sampling filter override ("linear" or "nearest") in relPath's sidecar.
func (e *Editor) SetImageImportFilter(relPath, filter string) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image filter for "+relPath, func(f *importsettings.File) {
		if filter == "" {
			if f.Image != nil {
				f.Image.Filter = ""
			}
			return
		}
		ensureImportImage(f).Filter = filter
	})
}

// SetImageImportFormat sets or clears (when empty) the image KTX2 format override ("rgba8", "bc1", "bc3", or "etc2") in relPath's sidecar.
func (e *Editor) SetImageImportFormat(relPath, format string) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image format for "+relPath, func(f *importsettings.File) {
		if format == "" {
			if f.Image != nil {
				f.Image.Format = ""
			}
			return
		}
		ensureImportImage(f).Format = format
	})
}

// SetImageImportMipmaps sets or clears (when nil) the image mipmaps override in relPath's sidecar.
func (e *Editor) SetImageImportMipmaps(relPath string, mipmaps *bool) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image mipmaps for "+relPath, func(f *importsettings.File) {
		if mipmaps == nil {
			if f.Image != nil {
				f.Image.Mipmaps = nil
			}
			return
		}
		v := *mipmaps
		ensureImportImage(f).Mipmaps = &v
	})
}

// SetImageImportSRGB sets or clears (when nil) the image sRGB override in relPath's sidecar.
func (e *Editor) SetImageImportSRGB(relPath string, srgb *bool) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image sRGB for "+relPath, func(f *importsettings.File) {
		if srgb == nil {
			if f.Image != nil {
				f.Image.SRGB = nil
			}
			return
		}
		v := *srgb
		ensureImportImage(f).SRGB = &v
	})
}

// SetImageImportSlice sets or clears (when nil) the image sprite-sheet slice grid in relPath's sidecar.
// Clearing the slice also removes per-cell pivot overrides (which require image.slice), and setting a positive Count prunes per-cell pivot overrides at or above Count.
func (e *Editor) SetImageImportSlice(relPath string, slice *importsettings.Slice) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image slice for "+relPath, func(f *importsettings.File) {
		if slice == nil {
			if f.Image != nil {
				f.Image.Slice = nil
				if f.Image.Pivot != nil {
					f.Image.Pivot.Cells = nil
				}
			}
			return
		}
		s := *slice
		im := ensureImportImage(f)
		im.Slice = &s
		if im.Pivot != nil && s.Count > 0 && len(im.Pivot.Cells) > 0 {
			kept := im.Pivot.Cells[:0]
			for _, c := range im.Pivot.Cells {
				if c.Cell < s.Count {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				im.Pivot.Cells = nil
			} else {
				im.Pivot.Cells = kept
			}
		}
	})
}

// SetImageImportPivot sets or clears (when nil) the image pivot block in relPath's sidecar, sorting per-cell overrides by ascending cell index.
func (e *Editor) SetImageImportPivot(relPath string, pivot *importsettings.Pivot) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set image pivot for "+relPath, func(f *importsettings.File) {
		if pivot == nil {
			if f.Image != nil {
				f.Image.Pivot = nil
			}
			return
		}
		p := cloneImportPivot(pivot)
		slices.SortFunc(p.Cells, func(a, b importsettings.CellPivot) int {
			return cmp.Compare(a.Cell, b.Cell)
		})
		ensureImportImage(f).Pivot = p
	})
}

// SetImageImportDefaultPivot sets or clears (when nil) the default normalized pivot in relPath's sidecar without disturbing per-cell overrides.
func (e *Editor) SetImageImportDefaultPivot(relPath string, pt *importsettings.PivotPoint) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set default pivot for "+relPath, func(f *importsettings.File) {
		if pt == nil {
			if f.Image != nil && f.Image.Pivot != nil {
				f.Image.Pivot.Default = nil
			}
			return
		}
		v := *pt
		im := ensureImportImage(f)
		if im.Pivot == nil {
			im.Pivot = &importsettings.Pivot{}
		}
		im.Pivot.Default = &v
	})
}

// SetImageImportCellPivot sets or clears (when nil) the normalized pivot override for 0-based cell in relPath's sidecar, keeping cells in ascending order.
func (e *Editor) SetImageImportCellPivot(relPath string, cell int, pt *importsettings.PivotPoint) error {
	if err := e.expectAssetKind(relPath, AssetImportImage); err != nil {
		return err
	}
	if cell < 0 {
		return fmt.Errorf("editor: cell %d must not be negative", cell)
	}
	label := fmt.Sprintf("Set cell %d pivot for %s", cell, relPath)
	if pt == nil {
		label = fmt.Sprintf("Reset cell %d pivot for %s", cell, relPath)
	}
	return e.EditAssetImportSettings(relPath, label, func(f *importsettings.File) {
		if pt == nil {
			if f.Image == nil || f.Image.Pivot == nil || len(f.Image.Pivot.Cells) == 0 {
				return
			}
			kept := f.Image.Pivot.Cells[:0]
			for _, c := range f.Image.Pivot.Cells {
				if c.Cell != cell {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				f.Image.Pivot.Cells = nil
			} else {
				f.Image.Pivot.Cells = kept
			}
			return
		}
		im := ensureImportImage(f)
		if im.Pivot == nil {
			im.Pivot = &importsettings.Pivot{}
		}
		for i := range im.Pivot.Cells {
			if im.Pivot.Cells[i].Cell == cell {
				im.Pivot.Cells[i].Pivot = *pt
				return
			}
		}
		im.Pivot.Cells = append(im.Pivot.Cells, importsettings.CellPivot{Cell: cell, Pivot: *pt})
		slices.SortFunc(im.Pivot.Cells, func(a, b importsettings.CellPivot) int {
			return cmp.Compare(a.Cell, b.Cell)
		})
	})
}

// SetAudioImportFormat sets or clears (when empty) the audio output format override ("wav", "aac", or "mp3") in relPath's sidecar.
func (e *Editor) SetAudioImportFormat(relPath, format string) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio format for "+relPath, func(f *importsettings.File) {
		if format == "" {
			if f.Audio != nil {
				f.Audio.Format = ""
			}
			return
		}
		ensureImportAudio(f).Format = format
	})
}

// SetAudioImportRate sets or clears (when 0) the audio sample rate override in Hz in relPath's sidecar.
func (e *Editor) SetAudioImportRate(relPath string, rate int) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio rate for "+relPath, func(f *importsettings.File) {
		if rate == 0 {
			if f.Audio != nil {
				f.Audio.Rate = 0
			}
			return
		}
		ensureImportAudio(f).Rate = rate
	})
}

// SetAudioImportChannels sets or clears (when 0) the audio channel count override (1 mono, 2 stereo) in relPath's sidecar.
func (e *Editor) SetAudioImportChannels(relPath string, channels int) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio channels for "+relPath, func(f *importsettings.File) {
		if channels == 0 {
			if f.Audio != nil {
				f.Audio.Channels = 0
			}
			return
		}
		ensureImportAudio(f).Channels = channels
	})
}

// SetAudioImportBitrate sets or clears (when 0) the audio bitrate override in kbps in relPath's sidecar.
func (e *Editor) SetAudioImportBitrate(relPath string, bitrate int) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio bitrate for "+relPath, func(f *importsettings.File) {
		if bitrate == 0 {
			if f.Audio != nil {
				f.Audio.Bitrate = 0
			}
			return
		}
		ensureImportAudio(f).Bitrate = bitrate
	})
}

// SetAudioImportPeakDB sets or clears (when nil) the peak normalization override in dBFS, clearing rmsDB when non-nil.
func (e *Editor) SetAudioImportPeakDB(relPath string, peakDB *float64) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio peakDB for "+relPath, func(f *importsettings.File) {
		if peakDB == nil {
			if f.Audio != nil {
				f.Audio.PeakDB = nil
			}
			return
		}
		v := *peakDB
		au := ensureImportAudio(f)
		au.PeakDB = &v
		au.RMSDB = nil
	})
}

// SetAudioImportRMSDB sets or clears (when nil) the RMS normalization override in dBFS, clearing peakDB when non-nil.
func (e *Editor) SetAudioImportRMSDB(relPath string, rmsDB *float64) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio rmsDB for "+relPath, func(f *importsettings.File) {
		if rmsDB == nil {
			if f.Audio != nil {
				f.Audio.RMSDB = nil
			}
			return
		}
		v := *rmsDB
		au := ensureImportAudio(f)
		au.RMSDB = &v
		au.PeakDB = nil
	})
}

// SetAudioImportLoop sets or clears (when nil) the audio loop region in relPath's sidecar.
func (e *Editor) SetAudioImportLoop(relPath string, loop *importsettings.Loop) error {
	if err := e.expectAssetKind(relPath, AssetImportAudio); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set audio loop for "+relPath, func(f *importsettings.File) {
		if loop == nil {
			if f.Audio != nil {
				f.Audio.Loop = nil
			}
			return
		}
		l := *loop
		ensureImportAudio(f).Loop = &l
	})
}

// SetFontImportSize sets or clears (when 0) the baked font pixel size override in relPath's sidecar.
func (e *Editor) SetFontImportSize(relPath string, size int) error {
	if err := e.expectAssetKind(relPath, AssetImportFont); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set font size for "+relPath, func(f *importsettings.File) {
		if size == 0 {
			if f.Font != nil {
				f.Font.Size = 0
			}
			return
		}
		ensureImportFont(f).Size = size
	})
}

// SetFontImportChars sets or clears (when empty) the baked font character set override in relPath's sidecar.
func (e *Editor) SetFontImportChars(relPath, chars string) error {
	if err := e.expectAssetKind(relPath, AssetImportFont); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set font chars for "+relPath, func(f *importsettings.File) {
		if chars == "" {
			if f.Font != nil {
				f.Font.Chars = ""
			}
			return
		}
		ensureImportFont(f).Chars = chars
	})
}

// SetFontImportPadding sets or clears (when nil) the baked font glyph padding override in pixels in relPath's sidecar.
func (e *Editor) SetFontImportPadding(relPath string, padding *int) error {
	if err := e.expectAssetKind(relPath, AssetImportFont); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set font padding for "+relPath, func(f *importsettings.File) {
		if padding == nil {
			if f.Font != nil {
				f.Font.Padding = nil
			}
			return
		}
		v := *padding
		ensureImportFont(f).Padding = &v
	})
}

// SetModelImportScale sets or clears (when 0) the model scale multiplier override in relPath's sidecar.
func (e *Editor) SetModelImportScale(relPath string, scale float64) error {
	if err := e.expectAssetKind(relPath, AssetImportModel); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set model scale for "+relPath, func(f *importsettings.File) {
		if scale == 0 {
			if f.Model != nil {
				f.Model.Scale = 0
			}
			return
		}
		ensureImportModel(f).Scale = scale
	})
}

// SetModelImportAnimations sets or clears (when nil) the model animations override in relPath's sidecar.
func (e *Editor) SetModelImportAnimations(relPath string, animations *bool) error {
	if err := e.expectAssetKind(relPath, AssetImportModel); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set model animations for "+relPath, func(f *importsettings.File) {
		if animations == nil {
			if f.Model != nil {
				f.Model.Animations = nil
			}
			return
		}
		v := *animations
		ensureImportModel(f).Animations = &v
	})
}

// SetModelImportLODs sets or clears (when empty) the model's declared LOD levels in relPath's sidecar.
func (e *Editor) SetModelImportLODs(relPath string, lods []importsettings.ModelLOD) error {
	if err := e.expectAssetKind(relPath, AssetImportModel); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set model LODs for "+relPath, func(f *importsettings.File) {
		if len(lods) == 0 {
			if f.Model != nil {
				f.Model.LODs = nil
			}
			return
		}
		ensureImportModel(f).LODs = slices.Clone(lods)
	})
}

// SetModelImportLODHysteresis sets or clears (when 0) the model's LOD hysteresis deadband in relPath's sidecar.
func (e *Editor) SetModelImportLODHysteresis(relPath string, hysteresis float32) error {
	if err := e.expectAssetKind(relPath, AssetImportModel); err != nil {
		return err
	}
	return e.EditAssetImportSettings(relPath, "Set model LOD hysteresis for "+relPath, func(f *importsettings.File) {
		if hysteresis == 0 {
			if f.Model != nil {
				f.Model.LODHysteresis = 0
			}
			return
		}
		ensureImportModel(f).LODHysteresis = hysteresis
	})
}

// SetAssetImportCredit sets or clears (when nil) the credit section in relPath's sidecar.
func (e *Editor) SetAssetImportCredit(relPath string, credit *importsettings.Credit) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	return e.EditAssetImportSettings(relPath, "Set credit for "+relPath, func(f *importsettings.File) {
		if credit == nil {
			f.Credit = nil
			return
		}
		c := *credit
		f.Credit = &c
	})
}

// ResetAssetImportField resets one overridden field of relPath's sidecar back to its inherited game.yaml default, removing the sidecar file when no overrides or credit remain.
func (e *Editor) ResetAssetImportField(relPath, field string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	kind := e.AssetImportKind(relPath)
	if kind == AssetImportNone {
		return fmt.Errorf("editor: %q is not an importable asset", relPath)
	}
	if field == "credit" {
		return e.SetAssetImportCredit(relPath, nil)
	}
	switch kind {
	case AssetImportImage:
		switch field {
		case "filter":
			return e.SetImageImportFilter(relPath, "")
		case "format":
			return e.SetImageImportFormat(relPath, "")
		case "mipmaps":
			return e.SetImageImportMipmaps(relPath, nil)
		case "srgb":
			return e.SetImageImportSRGB(relPath, nil)
		case "slice":
			return e.SetImageImportSlice(relPath, nil)
		case "pivot":
			return e.SetImageImportPivot(relPath, nil)
		case "pivot.default":
			return e.SetImageImportDefaultPivot(relPath, nil)
		case "pivot.cells":
			return e.EditAssetImportSettings(relPath, "Reset per-cell pivots for "+relPath, func(f *importsettings.File) {
				if f.Image != nil && f.Image.Pivot != nil {
					f.Image.Pivot.Cells = nil
				}
			})
		}
	case AssetImportAudio:
		switch field {
		case "format":
			return e.SetAudioImportFormat(relPath, "")
		case "rate":
			return e.SetAudioImportRate(relPath, 0)
		case "channels":
			return e.SetAudioImportChannels(relPath, 0)
		case "bitrate":
			return e.SetAudioImportBitrate(relPath, 0)
		case "peakDB", "rmsDB", "normalization":
			return e.EditAssetImportSettings(relPath, "Reset audio normalization for "+relPath, func(f *importsettings.File) {
				if f.Audio != nil {
					f.Audio.PeakDB = nil
					f.Audio.RMSDB = nil
				}
			})
		case "loop":
			return e.SetAudioImportLoop(relPath, nil)
		}
	case AssetImportFont:
		switch field {
		case "size":
			return e.SetFontImportSize(relPath, 0)
		case "chars":
			return e.SetFontImportChars(relPath, "")
		case "padding":
			return e.SetFontImportPadding(relPath, nil)
		}
	case AssetImportModel:
		switch field {
		case "scale":
			return e.SetModelImportScale(relPath, 0)
		case "animations":
			return e.SetModelImportAnimations(relPath, nil)
		case "lods":
			return e.SetModelImportLODs(relPath, nil)
		case "lodHysteresis":
			return e.SetModelImportLODHysteresis(relPath, 0)
		}
	}
	return fmt.Errorf("editor: unknown %s import field %q", kind, field)
}

func (e *Editor) expectAssetKind(relPath string, want AssetImportKind) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	got := e.AssetImportKind(relPath)
	if got != want {
		return fmt.Errorf("editor: %q is %q, not %q", relPath, got, want)
	}
	return nil
}

func normalizeProjectAssetPath(relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("editor: asset path is required")
	}
	clean := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	if !fs.ValidPath(clean) || clean == "." {
		return "", fmt.Errorf("editor: asset path %q must be a clean relative path inside the project", relPath)
	}
	if importsettings.IsSidecar(clean) {
		return "", fmt.Errorf("editor: %q is already a sidecar file", relPath)
	}
	return clean, nil
}

// assetScannedUnder mirrors project.scannedUnder: reports whether p is inside dir with no dot-prefixed segment below dir.
func assetScannedUnder(p, dir string) bool {
	var rest string
	switch {
	case p == dir:
		return true
	case dir == ".":
		rest = p
	case strings.HasPrefix(p, dir+"/"):
		rest = p[len(dir)+1:]
	default:
		return false
	}
	for _, seg := range strings.Split(rest, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

func ensureImportImage(f *importsettings.File) *importsettings.Image {
	if f.Image == nil {
		f.Image = &importsettings.Image{}
	}
	return f.Image
}

func ensureImportAudio(f *importsettings.File) *importsettings.Audio {
	if f.Audio == nil {
		f.Audio = &importsettings.Audio{}
	}
	return f.Audio
}

func ensureImportFont(f *importsettings.File) *importsettings.Font {
	if f.Font == nil {
		f.Font = &importsettings.Font{}
	}
	return f.Font
}

func ensureImportModel(f *importsettings.File) *importsettings.Model {
	if f.Model == nil {
		f.Model = &importsettings.Model{}
	}
	return f.Model
}

func cloneImportFile(src *importsettings.File) *importsettings.File {
	if src == nil {
		return &importsettings.File{Version: importsettings.CurrentVersion}
	}
	out := &importsettings.File{Version: src.Version}
	if src.Image != nil {
		im := *src.Image
		if src.Image.Mipmaps != nil {
			v := *src.Image.Mipmaps
			im.Mipmaps = &v
		}
		if src.Image.SRGB != nil {
			v := *src.Image.SRGB
			im.SRGB = &v
		}
		if src.Image.Slice != nil {
			s := *src.Image.Slice
			im.Slice = &s
		}
		if src.Image.Pivot != nil {
			im.Pivot = cloneImportPivot(src.Image.Pivot)
		}
		out.Image = &im
	}
	if src.Audio != nil {
		au := *src.Audio
		if src.Audio.PeakDB != nil {
			v := *src.Audio.PeakDB
			au.PeakDB = &v
		}
		if src.Audio.RMSDB != nil {
			v := *src.Audio.RMSDB
			au.RMSDB = &v
		}
		if src.Audio.Loop != nil {
			l := *src.Audio.Loop
			au.Loop = &l
		}
		out.Audio = &au
	}
	if src.Font != nil {
		fo := *src.Font
		if src.Font.Padding != nil {
			v := *src.Font.Padding
			fo.Padding = &v
		}
		out.Font = &fo
	}
	if src.Model != nil {
		m := *src.Model
		if src.Model.Animations != nil {
			v := *src.Model.Animations
			m.Animations = &v
		}
		m.LODs = slices.Clone(src.Model.LODs)
		out.Model = &m
	}
	if src.Credit != nil {
		c := *src.Credit
		out.Credit = &c
	}
	return out
}

func cloneImportPivot(src *importsettings.Pivot) *importsettings.Pivot {
	if src == nil {
		return nil
	}
	out := &importsettings.Pivot{}
	if src.Default != nil {
		d := *src.Default
		out.Default = &d
	}
	if len(src.Cells) > 0 {
		out.Cells = slices.Clone(src.Cells)
	}
	return out
}

func pruneEmptyImportSections(f *importsettings.File) {
	if f.Image != nil && f.Image.Pivot != nil &&
		f.Image.Pivot.Default == nil &&
		len(f.Image.Pivot.Cells) == 0 {
		f.Image.Pivot = nil
	}
	if f.Image != nil &&
		f.Image.Filter == "" &&
		f.Image.Format == "" &&
		f.Image.Mipmaps == nil &&
		f.Image.SRGB == nil &&
		f.Image.Slice == nil &&
		f.Image.Pivot == nil {
		f.Image = nil
	}
	if f.Audio != nil &&
		f.Audio.Format == "" &&
		f.Audio.Rate == 0 &&
		f.Audio.Channels == 0 &&
		f.Audio.PeakDB == nil &&
		f.Audio.RMSDB == nil &&
		f.Audio.Bitrate == 0 &&
		f.Audio.Loop == nil {
		f.Audio = nil
	}
	if f.Font != nil &&
		f.Font.Size == 0 &&
		f.Font.Chars == "" &&
		f.Font.Padding == nil {
		f.Font = nil
	}
	if f.Model != nil &&
		f.Model.Scale == 0 &&
		f.Model.Animations == nil &&
		len(f.Model.LODs) == 0 &&
		f.Model.LODHysteresis == 0 {
		f.Model = nil
	}
	if f.Credit != nil &&
		f.Credit.Author == "" &&
		f.Credit.License == "" &&
		f.Credit.Source == "" &&
		f.Credit.Notice == "" {
		f.Credit = nil
	}
}

// importSettingsCmd writes or deletes an asset's *.import.yaml sidecar on disk as one undoable fallibleCommand.
type importSettingsCmd struct {
	ed           *Editor
	label        string
	relPath      string
	sidecarRel   string
	absPath      string
	beforeExists bool
	beforeBytes  []byte
	afterExists  bool
	afterBytes   []byte
}

func (c *importSettingsCmd) Label() string { return c.label }
func (c *importSettingsCmd) Do()           { c.ed.report(c.do()) }
func (c *importSettingsCmd) Undo()         { c.ed.report(c.undo()) }

func (c *importSettingsCmd) do() error {
	if err := c.apply(c.afterExists, c.afterBytes); err != nil {
		return err
	}
	c.ed.Status = c.label
	return nil
}

func (c *importSettingsCmd) undo() error {
	return c.apply(c.beforeExists, c.beforeBytes)
}

func (c *importSettingsCmd) apply(exists bool, data []byte) error {
	if exists {
		if err := os.MkdirAll(filepath.Dir(c.absPath), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(c.absPath, data); err != nil {
			return err
		}
	} else {
		if err := os.Remove(c.absPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	c.ed.importRev++
	c.ed.filesRev++
	c.ed.invalidate3DBuild()
	c.ed.invalidateProjectIndex()
	if c.ed.ExportPanelOpen() {
		c.ed.CheckExportCredits()
	}
	c.ed.CheckProblems()
	return nil
}
