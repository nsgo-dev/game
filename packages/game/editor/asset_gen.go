package editor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	maxGeneratedImageBytes        = 16 << 20 // 16 MiB
	maxGeneratedAudioBytes        = 32 << 20 // 32 MiB
	maxGeneratedImageDim          = 4096
	maxGeneratedCandidates        = 16
	maxGeneratedTotalDecodedBytes = 64 << 20 // 64 MiB total decoded pixels/audio per request
	maxGeneratedBriefBytes        = 1 << 20  // 1 MiB
	maxGeneratedSourceBytes       = 1 << 20  // 1 MiB
	defaultAssetGenConnectTimeout = 30 * time.Second
	defaultAssetGenCallTimeout    = 60 * time.Second
	maxGeneratedElapsed           = 24 * time.Hour
)

var errAssetGenMergeConflict = errors.New("editor: resolve merge conflicts before using generated assets")

// AssetGeneratorKind classifies the media type an MCP generator server produces.
type AssetGeneratorKind string

const (
	// AssetGenImages produces image candidates (PNG/JPEG/GIF decoded strictly to pixels in memory).
	AssetGenImages AssetGeneratorKind = "images"
	// AssetGenSound produces audio candidates (WAV/OGG decoded strictly in memory).
	AssetGenSound AssetGeneratorKind = "sound"
)

// Valid reports whether k is a supported asset generator kind.
func (k AssetGeneratorKind) Valid() bool {
	return k == AssetGenImages || k == AssetGenSound
}

// GeneratorServerConfig describes one MCP generator server configured under Settings > Tools.
type GeneratorServerConfig struct {
	// Name uniquely identifies the generator server (for example "pixel-gen" or "sfx-gen").
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Kind declares what kind of assets the generator produces ("images" or "sound").
	Kind AssetGeneratorKind `yaml:"kind" schema:"required,enum=images|sound"`
	// Command is the executable launched over stdio MCP transport when connecting from Settings > Tools.
	Command string `yaml:"command,omitempty"`
	// Args are command-line arguments passed to Command.
	Args []string `yaml:"args,omitempty"`
	// Tool optionally names the specific MCP tool to invoke; empty selects the first matching generate tool advertised by the server.
	Tool string `yaml:"tool,omitempty"`
}

// AssetGeneratorServer describes the live status of a configured or connected MCP generator server.
type AssetGeneratorServer struct {
	Config    GeneratorServerConfig
	Connected bool
	ToolName  string
	Error     string
}

// AssetPreviewBackground defines one selectable preview backdrop for image candidates (such as a level floor or checkerboard).
type AssetPreviewBackground struct {
	ID      string
	Label   string
	Color   core.Color
	Checker bool
}

// AssetStyleBrief holds the art and audio rules read from brief.yaml (or defaults) used to check generated candidates.
type AssetStyleBrief struct {
	// Summary is the short style description shown in the inspector's STYLE AGAINST THE BRIEF section.
	Summary string
	// PixelGrid is the base pixel art grid size in pixels (for example 16); 0 disables grid checks.
	PixelGrid int
	// MaxColors is the maximum shading color count allowed before outline colors (for example 4); 0 disables palette checks.
	MaxColors int
	// OutlineColors is the number of extra outline colors allowed alongside MaxColors (default 1).
	OutlineColors int
	// MinContrast is the minimum WCAG luminance contrast ratio against the preview background (default 3.0).
	MinContrast float64
	// TargetLoudDB is the target RMS loudness in dBFS for generated sound effects (default -14.0).
	TargetLoudDB float64
	// MaxPeakDB is the maximum allowed peak amplitude in dBFS for generated sounds (default -0.5).
	MaxPeakDB float64
	// LoudnessTolDB is the allowed loudness deviation from TargetLoudDB or reference sound in dB (default 3.0).
	LoudnessTolDB float64
	// Backgrounds are the selectable preview backdrops shown in PREVIEW ON.
	Backgrounds []AssetPreviewBackground
}

// DefaultAssetStyleBrief returns default art and audio brief checks matching a 16 px pixel-art game.
func DefaultAssetStyleBrief() AssetStyleBrief {
	return AssetStyleBrief{
		Summary:       "16 px pixel art, 4-colour shading, cold stone with warm torch light. 1 px dark outline on characters.",
		PixelGrid:     16,
		MaxColors:     4,
		OutlineColors: 1,
		MinContrast:   3.0,
		TargetLoudDB:  -14.0,
		MaxPeakDB:     -0.5,
		LoudnessTolDB: 3.0,
		Backgrounds: []AssetPreviewBackground{
			{ID: "floor3", Label: "Floor 3", Color: core.RGB(0x1A2130)},
			{ID: "floor1", Label: "Floor 1", Color: core.RGB(0x231D1C)},
			{ID: "checker", Label: "Checker", Color: core.RGB(0x171A21), Checker: true},
		},
	}
}

// AssetStyleCheck is one pass/fail style verification result shown on a candidate card and inspector.
type AssetStyleCheck struct {
	Pass    bool
	Title   string
	Summary string
	Detail  string
}

// AssetGenerationStatus classifies the lifecycle state of a generated asset entry in the GENERATED rail.
type AssetGenerationStatus string

const (
	AssetGenPending        AssetGenerationStatus = "pending"
	AssetGenPicked         AssetGenerationStatus = "picked"
	AssetGenInUse          AssetGenerationStatus = "in_use"
	AssetGenLicenseUnknown AssetGenerationStatus = "license_unknown"
)

// GeneratedCandidate holds one untrusted candidate decoded strictly in memory (never written to disk before Pick).
type GeneratedCandidate struct {
	ID                    string
	Seed                  *int64
	MimeType              string
	CleanBytes            []byte
	Width                 int
	Height                int
	Pixels                *image.RGBA
	Image                 core.ImageSource
	ColorCount            int
	HasAntiAliasing       bool
	ContrastRatio         float64
	Duration              time.Duration
	SampleRate            int
	Channels              int
	LoudnessDBFS          float64
	PeakDBFS              float64
	AudioLoudnessMeasured bool
	Waveform              []float32
	AudioCheckPass        bool
	AudioCheckText        string
	Checks                []AssetStyleCheck
	License               string
	CommercialUse         bool
}

// AssetGenerationRequest holds one image or sound generation request and its in-memory candidates.
type AssetGenerationRequest struct {
	ID                  string
	TargetPath          string
	Kind                AssetGeneratorKind
	Generator           string
	Model               string
	Prompt              string
	UserPrompt          string
	References          []string
	ReferenceAsset      string
	RefWidth            int
	RefHeight           int
	RefPixels           *image.RGBA
	RefImage            core.ImageSource
	RefLoudDB           *float64
	RefSoundName        string
	Count               int
	Elapsed             time.Duration
	License             string
	CommercialUse       bool
	Status              AssetGenerationStatus
	UsageCount          int
	PickedCandidateID   string
	SelectedCandidateID string
	Candidates          []GeneratedCandidate
	LastNote            string
}

// GenerateAssetParams configures an MCP asset generation call.
type GenerateAssetParams struct {
	// TargetPath is the project-relative slash path where the asset will be written once picked (for example "art/characters/slime_frost.png").
	TargetPath string
	// Generator names the connected MCP generator server to invoke; empty picks the first connected server matching Kind.
	Generator string
	// Kind selects images or sound; empty is inferred from TargetPath's extension or Generator's kind.
	Kind AssetGeneratorKind
	// Model optionally requests a specific generator model.
	Model string
	// Prompt is the generation prompt sent to the MCP server.
	Prompt string
	// UserPrompt is an optional human-facing summary quote shown in the request header (defaults to Prompt).
	UserPrompt string
	// References lists project-relative asset or brief references sent to the generator (for example "art/characters/slime.png", "brief.yaml#art").
	References []string
	// Count is the number of candidates to request (defaults to 4 for images and 3 for sounds).
	Count int
	// BaseSeed optionally suggests a starting seed.
	BaseSeed *int64
	// VaryFromSeed optionally requests variations anchored on an earlier candidate's seed.
	VaryFromSeed *int64
	// Refine optionally adds refinement words while keeping the same references.
	Refine string
}

type assetGenServerConn struct {
	config  GeneratorServerConfig
	session *sdkmcp.ClientSession
	tool    string
	errMsg  string
}

type assetGenState struct {
	mu               sync.Mutex
	open             bool
	rev              uint64
	epoch            uint64
	inFlightCancel   context.CancelFunc
	inFlightSeq      uint64
	busy             bool
	busyLabel        string
	servers          map[string]*assetGenServerConn
	serverOrder      []string
	configured       []GeneratorServerConfig
	requests         []*AssetGenerationRequest
	lastPickByReq    map[string]*pickGeneratedAssetCmd
	activeRequestID  string
	activeSoundReqID string
	previewBGID      string
	brief            AssetStyleBrief
	briefLoaded      bool
	licenses         []credits.GeneratedAssetLicense
	licenseScanErr   string
	approvalAuthor   string
	playingReqID     string
	playingCandID    string
	refineDraft      string
	licenseDraft     string
	licenseEditPath  string
	licenseEditComm  bool
	nextSeq          int
}

func (e *Editor) ensureAssetGenLocked() {
	if e.assetGen.servers == nil {
		e.assetGen.servers = make(map[string]*assetGenServerConn)
	}
	if e.assetGen.lastPickByReq == nil {
		e.assetGen.lastPickByReq = make(map[string]*pickGeneratedAssetCmd)
	}
	if !e.assetGen.briefLoaded {
		e.assetGen.brief = DefaultAssetStyleBrief()
	}
	if e.assetGen.previewBGID == "" && len(e.assetGen.brief.Backgrounds) > 0 {
		e.assetGen.previewBGID = e.assetGen.brief.Backgrounds[0].ID
	}
}

func (e *Editor) cancelAssetGenOps() {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	cancel := e.assetGen.inFlightCancel
	e.assetGen.inFlightCancel = nil
	e.assetGen.inFlightSeq++
	wasBusy := e.assetGen.busy
	e.assetGen.busy = false
	e.assetGen.busyLabel = ""
	if wasBusy {
		e.assetGen.rev++
	}
	e.assetGen.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// resetAssetGenForProject cancels in-flight generation, disconnects project-scoped generator sessions,
// stops audio audition, and clears all generated-asset requests, brief rules, license scans, and drafts
// so switching projects or demos never leaks stale state or writes across projects.
func (e *Editor) resetAssetGenForProject() {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	cancel := e.assetGen.inFlightCancel
	e.assetGen.inFlightCancel = nil
	e.assetGen.inFlightSeq++
	e.assetGen.epoch++
	e.assetGen.busy = false
	e.assetGen.busyLabel = ""
	e.assetGen.open = false
	e.assetGen.requests = nil
	e.assetGen.lastPickByReq = nil
	e.assetGen.activeRequestID = ""
	e.assetGen.activeSoundReqID = ""
	e.assetGen.previewBGID = ""
	e.assetGen.brief = DefaultAssetStyleBrief()
	e.assetGen.briefLoaded = false
	e.assetGen.licenses = nil
	e.assetGen.licenseScanErr = ""
	e.assetGen.playingReqID = ""
	e.assetGen.playingCandID = ""
	e.assetGen.refineDraft = ""
	e.assetGen.licenseDraft = ""
	e.assetGen.licenseEditPath = ""
	e.assetGen.licenseEditComm = false
	var sessions []*sdkmcp.ClientSession
	for _, conn := range e.assetGen.servers {
		if conn != nil && conn.session != nil {
			sessions = append(sessions, conn.session)
			conn.session = nil
		}
	}
	e.assetGen.servers = nil
	e.assetGen.serverOrder = nil
	e.assetGen.rev++
	e.assetGen.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, s := range sessions {
		_ = s.Close()
	}
	e.stopAudioAudition()
}

func (e *Editor) assetGenRev() uint64 {
	if e == nil {
		return 0
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.rev
}

// AssetGeneratorOpen reports whether the full-window Generated Assets screen is open.
func (e *Editor) AssetGeneratorOpen() bool {
	if e == nil {
		return false
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.open
}

// SetAssetGeneratorOpen opens or closes the full-window Generated Assets screen.
func (e *Editor) SetAssetGeneratorOpen(open bool) {
	if e == nil {
		return
	}
	if open && (e.playing || e.MergeConflictOpen()) {
		return
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	changed := e.assetGen.open != open
	e.assetGen.open = open
	if changed {
		e.assetGen.rev++
	}
	e.assetGen.mu.Unlock()
	if open {
		e.CloseAskInPlace()
		e.LoadAssetStyleBrief()
		e.RefreshGeneratedAssetLicenses()
	} else {
		e.cancelAssetGenOps()
		e.StopGeneratedSoundCandidate()
	}
	if changed {
		e.notify()
	}
}

// CloseAssetGenerator closes the Generated Assets screen and returns to the scene editor.
func (e *Editor) CloseAssetGenerator() {
	e.SetAssetGeneratorOpen(false)
}

// AssetApprovalAuthor returns the author name recorded in approvedBy when picking a generated candidate.
func (e *Editor) AssetApprovalAuthor() string {
	if e == nil {
		return "editor"
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	if strings.TrimSpace(e.assetGen.approvalAuthor) != "" {
		return strings.TrimSpace(e.assetGen.approvalAuthor)
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user
	}
	return "editor"
}

// SetAssetApprovalAuthor sets the author name written to approvedBy in <file>.source.yaml when picking a candidate.
func (e *Editor) SetAssetApprovalAuthor(author string) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	e.assetGen.approvalAuthor = strings.TrimSpace(author)
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	e.notify()
}

// SetAssetStyleBrief overrides the active style brief used to check generated candidates and re-evaluates all in-memory candidates.
func (e *Editor) SetAssetStyleBrief(brief AssetStyleBrief) {
	if e == nil {
		return
	}
	if len(brief.Backgrounds) == 0 {
		brief.Backgrounds = DefaultAssetStyleBrief().Backgrounds
	}
	if brief.MinContrast <= 0 {
		brief.MinContrast = 3.0
	}
	if brief.TargetLoudDB == 0 {
		brief.TargetLoudDB = -14.0
	}
	if brief.MaxPeakDB == 0 {
		brief.MaxPeakDB = -0.5
	}
	if brief.LoudnessTolDB <= 0 {
		brief.LoudnessTolDB = 3.0
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	e.assetGen.brief = brief
	e.assetGen.briefLoaded = true
	if !hasPreviewBG(brief.Backgrounds, e.assetGen.previewBGID) {
		e.assetGen.previewBGID = brief.Backgrounds[0].ID
	}
	bg := currentPreviewBGLocked(&e.assetGen)
	for _, req := range e.assetGen.requests {
		recheckRequestLocked(req, e.assetGen.brief, bg)
	}
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	e.notify()
}

// AssetStyleBrief returns the currently active style brief.
func (e *Editor) AssetStyleBrief() AssetStyleBrief {
	if e == nil {
		return DefaultAssetStyleBrief()
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	b := e.assetGen.brief
	b.Backgrounds = slices.Clone(b.Backgrounds)
	e.assetGen.mu.Unlock()
	return b
}

// LoadAssetStyleBrief reads brief.yaml from the open project root when present, falling back to DefaultAssetStyleBrief for omitted fields.
func (e *Editor) LoadAssetStyleBrief() AssetStyleBrief {
	brief := DefaultAssetStyleBrief()
	if e == nil || e.ProjectPath == "" {
		return e.AssetStyleBrief()
	}
	root := filepath.Dir(e.ProjectPath)
	briefPath := filepath.Join(root, "brief.yaml")
	var data []byte
	var err error
	if err = verifyAssetGenReadPath(root, briefPath, "brief.yaml"); err == nil {
		data, err = readBoundedAssetFile(briefPath, maxGeneratedBriefBytes)
	}
	if err != nil {
		e.assetGen.mu.Lock()
		e.ensureAssetGenLocked()
		if !e.assetGen.briefLoaded {
			e.assetGen.brief = brief
			e.assetGen.briefLoaded = true
		}
		out := e.assetGen.brief
		out.Backgrounds = slices.Clone(out.Backgrounds)
		e.assetGen.mu.Unlock()
		return out
	}
	parsed := parseBriefStyleYAML(data, brief)
	e.SetAssetStyleBrief(parsed)
	return parsed
}

func parseBriefStyleYAML(data []byte, base AssetStyleBrief) AssetStyleBrief {
	var doc struct {
		Summary      string `yaml:"summary"`
		Pitch        string `yaml:"pitch"`
		ArtDirection string `yaml:"artDirection"`
		Art          struct {
			Summary       string  `yaml:"summary"`
			Style         string  `yaml:"style"`
			PixelGrid     int     `yaml:"pixelGrid"`
			PixelGridAlt  int     `yaml:"pixel_grid"`
			MaxColors     int     `yaml:"maxColors"`
			MaxColorsAlt  int     `yaml:"max_colors"`
			OutlineColors *int    `yaml:"outlineColors"`
			MinContrast   float64 `yaml:"minContrast"`
			MinContrastA  float64 `yaml:"min_contrast"`
			Backgrounds   []struct {
				ID      string `yaml:"id"`
				Label   string `yaml:"label"`
				Color   string `yaml:"color"`
				Checker bool   `yaml:"checker"`
			} `yaml:"backgrounds"`
		} `yaml:"art"`
		Audio struct {
			TargetLoudDB float64 `yaml:"targetLoudnessDB"`
			TargetLUFS   float64 `yaml:"targetLUFS"`
			MaxPeakDB    float64 `yaml:"maxPeakDB"`
			ToleranceDB  float64 `yaml:"toleranceDB"`
		} `yaml:"audio"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return base
	}
	if s := strings.TrimSpace(doc.Art.Summary); s != "" {
		base.Summary = s
	} else if s := strings.TrimSpace(doc.Art.Style); s != "" {
		base.Summary = s
	} else if s := strings.TrimSpace(doc.ArtDirection); s != "" {
		base.Summary = s
	} else if s := strings.TrimSpace(doc.Summary); s != "" {
		base.Summary = s
	} else if s := strings.TrimSpace(doc.Pitch); s != "" {
		base.Summary = s
	}
	if doc.Art.PixelGrid > 0 {
		base.PixelGrid = doc.Art.PixelGrid
	} else if doc.Art.PixelGridAlt > 0 {
		base.PixelGrid = doc.Art.PixelGridAlt
	}
	if doc.Art.MaxColors > 0 {
		base.MaxColors = doc.Art.MaxColors
	} else if doc.Art.MaxColorsAlt > 0 {
		base.MaxColors = doc.Art.MaxColorsAlt
	}
	if doc.Art.OutlineColors != nil && *doc.Art.OutlineColors >= 0 {
		base.OutlineColors = *doc.Art.OutlineColors
	}
	if doc.Art.MinContrast > 0 {
		base.MinContrast = doc.Art.MinContrast
	} else if doc.Art.MinContrastA > 0 {
		base.MinContrast = doc.Art.MinContrastA
	}
	if len(doc.Art.Backgrounds) > 0 {
		var bgs []AssetPreviewBackground
		for _, bg := range doc.Art.Backgrounds {
			if strings.TrimSpace(bg.ID) == "" {
				continue
			}
			c := core.RGB(0x1A2130)
			if parsedCol, err := core.ParseColor(strings.TrimSpace(bg.Color)); err == nil {
				c = parsedCol
			}
			label := strings.TrimSpace(bg.Label)
			if label == "" {
				label = bg.ID
			}
			bgs = append(bgs, AssetPreviewBackground{
				ID:      strings.TrimSpace(bg.ID),
				Label:   label,
				Color:   c,
				Checker: bg.Checker,
			})
		}
		if len(bgs) > 0 {
			base.Backgrounds = bgs
		}
	}
	if doc.Audio.TargetLoudDB < 0 {
		base.TargetLoudDB = doc.Audio.TargetLoudDB
	} else if doc.Audio.TargetLUFS < 0 {
		base.TargetLoudDB = doc.Audio.TargetLUFS
	}
	if doc.Audio.MaxPeakDB < 0 {
		base.MaxPeakDB = doc.Audio.MaxPeakDB
	}
	if doc.Audio.ToleranceDB > 0 {
		base.LoudnessTolDB = doc.Audio.ToleranceDB
	}
	return base
}

func hasPreviewBG(bgs []AssetPreviewBackground, id string) bool {
	for _, b := range bgs {
		if b.ID == id {
			return true
		}
	}
	return false
}

func currentPreviewBGLocked(st *assetGenState) AssetPreviewBackground {
	for _, b := range st.brief.Backgrounds {
		if b.ID == st.previewBGID {
			return b
		}
	}
	if len(st.brief.Backgrounds) > 0 {
		return st.brief.Backgrounds[0]
	}
	return DefaultAssetStyleBrief().Backgrounds[0]
}

// AssetGenPreviewBackground returns the currently selected preview backdrop for generated image candidates.
func (e *Editor) AssetGenPreviewBackground() AssetPreviewBackground {
	if e == nil {
		return DefaultAssetStyleBrief().Backgrounds[0]
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	bg := currentPreviewBGLocked(&e.assetGen)
	e.assetGen.mu.Unlock()
	return bg
}

// SetAssetGenPreviewBackground switches the active preview backdrop (for example "floor3", "floor1", "checker") and updates contrast checks.
func (e *Editor) SetAssetGenPreviewBackground(bgID string) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	if !hasPreviewBG(e.assetGen.brief.Backgrounds, bgID) || e.assetGen.previewBGID == bgID {
		e.assetGen.mu.Unlock()
		return
	}
	e.assetGen.previewBGID = bgID
	bg := currentPreviewBGLocked(&e.assetGen)
	for _, req := range e.assetGen.requests {
		recheckRequestLocked(req, e.assetGen.brief, bg)
	}
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	e.notify()
}

// ConnectAssetGenerator connects an MCP asset generator server over transport (for example an in-memory transport in tests or stdio CommandTransport on desktop) and discovers its generation tool.
func (e *Editor) ConnectAssetGenerator(ctx context.Context, cfg GeneratorServerConfig, transport sdkmcp.Transport) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return errors.New("editor: generator server name is required")
	}
	if !cfg.Kind.Valid() {
		return fmt.Errorf("editor: invalid generator kind %q (want images or sound)", cfg.Kind)
	}
	if transport == nil {
		return errors.New("editor: nil MCP generator transport")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultAssetGenConnectTimeout)
		defer cancel()
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "nsgo-game-editor",
		Version: "1.0.0",
	}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		e.assetGen.mu.Lock()
		e.ensureAssetGenLocked()
		e.upsertServerLocked(&assetGenServerConn{config: cfg, errMsg: err.Error()})
		e.assetGen.rev++
		e.assetGen.mu.Unlock()
		e.notify()
		return fmt.Errorf("editor: connect MCP generator %q: %w", cfg.Name, err)
	}

	toolName := strings.TrimSpace(cfg.Tool)
	if toolName == "" {
		res, listErr := cs.ListTools(ctx, nil)
		if listErr != nil {
			_ = cs.Close()
			return fmt.Errorf("editor: list tools on MCP generator %q: %w", cfg.Name, listErr)
		}
		toolName = pickGeneratorTool(res.Tools, cfg.Kind)
		if toolName == "" {
			_ = cs.Close()
			return fmt.Errorf("editor: MCP generator %q advertises no tools", cfg.Name)
		}
	}

	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	var prev *sdkmcp.ClientSession
	if old, ok := e.assetGen.servers[cfg.Name]; ok && old != nil {
		prev = old.session
	}
	e.upsertServerLocked(&assetGenServerConn{
		config:  cfg,
		session: cs,
		tool:    toolName,
	})
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
	e.Status = fmt.Sprintf("Connected MCP generator %s (%s)", cfg.Name, cfg.Kind)
	e.notify()
	return nil
}

// ConnectAssetGeneratorCommand connects a configured stdio MCP generator server process on desktop platforms.
func (e *Editor) ConnectAssetGeneratorCommand(ctx context.Context, cfg GeneratorServerConfig) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	transport, err := newAssetGeneratorCommandTransport(dir, cfg.Command, cfg.Args)
	if err != nil {
		return err
	}
	return e.ConnectAssetGenerator(ctx, cfg, transport)
}

func (e *Editor) upsertServerLocked(conn *assetGenServerConn) {
	name := conn.config.Name
	if _, exists := e.assetGen.servers[name]; !exists {
		e.assetGen.serverOrder = append(e.assetGen.serverOrder, name)
	}
	e.assetGen.servers[name] = conn
}

func pickGeneratorTool(tools []*sdkmcp.Tool, kind AssetGeneratorKind) string {
	preferred := []string{"generate_image", "generate_sprite", "generate_asset", "generate"}
	if kind == AssetGenSound {
		preferred = []string{"generate_sound", "generate_audio", "generate_sfx", "generate_asset", "generate"}
	}
	for _, want := range preferred {
		for _, t := range tools {
			if t != nil && strings.EqualFold(t.Name, want) {
				return t.Name
			}
		}
	}
	for _, t := range tools {
		if t != nil && strings.Contains(strings.ToLower(t.Name), "generate") {
			return t.Name
		}
	}
	for _, t := range tools {
		if t != nil && t.Name != "" {
			return t.Name
		}
	}
	return ""
}

// DisconnectAssetGenerator closes and disconnects the named MCP generator server.
func (e *Editor) DisconnectAssetGenerator(name string) error {
	if e == nil {
		return nil
	}
	e.assetGen.mu.Lock()
	conn := e.assetGen.servers[name]
	var sess *sdkmcp.ClientSession
	if conn != nil {
		sess = conn.session
		conn.session = nil
		e.assetGen.rev++
	}
	e.assetGen.mu.Unlock()
	var err error
	if sess != nil {
		err = sess.Close()
	}
	e.notify()
	return err
}

// DisconnectAllAssetGenerators closes every connected MCP generator server session.
func (e *Editor) DisconnectAllAssetGenerators() {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	var sessions []*sdkmcp.ClientSession
	for _, conn := range e.assetGen.servers {
		if conn != nil && conn.session != nil {
			sessions = append(sessions, conn.session)
			conn.session = nil
		}
	}
	e.assetGen.mu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
}

// AssetGenerators returns all connected and configured MCP generator servers in deterministic order.
func (e *Editor) AssetGenerators() []AssetGeneratorServer {
	if e == nil {
		return nil
	}
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	cfgList := cloneGeneratorConfigs(e.assetGen.configured)
	seen := make(map[string]bool, len(e.assetGen.serverOrder)+len(cfgList))
	var out []AssetGeneratorServer
	for _, name := range e.assetGen.serverOrder {
		conn := e.assetGen.servers[name]
		if conn == nil {
			continue
		}
		seen[name] = true
		out = append(out, AssetGeneratorServer{
			Config:    conn.config,
			Connected: conn.session != nil,
			ToolName:  conn.tool,
			Error:     conn.errMsg,
		})
	}
	e.assetGen.mu.Unlock()
	for _, cfg := range cfgList {
		if seen[cfg.Name] {
			continue
		}
		out = append(out, AssetGeneratorServer{
			Config:    cfg,
			Connected: false,
		})
	}
	return out
}

// SetGeneratorServerConfigs updates the configured MCP generator servers shown under Settings > Tools and GENERATORS · MCP.
func (e *Editor) SetGeneratorServerConfigs(servers []GeneratorServerConfig) error {
	if e == nil {
		return nil
	}
	seen := make(map[string]bool, len(servers))
	for i, g := range servers {
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("editor: generator server %d is missing name", i)
		}
		if seen[g.Name] {
			return fmt.Errorf("editor: duplicate generator server name %q", g.Name)
		}
		seen[g.Name] = true
		if !g.Kind.Valid() {
			return fmt.Errorf("editor: generator server %q has invalid kind %q (want images or sound)", g.Name, g.Kind)
		}
	}
	e.assetGen.mu.Lock()
	e.assetGen.configured = cloneGeneratorConfigs(servers)
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	e.notify()
	return nil
}

func cloneGeneratorConfigs(in []GeneratorServerConfig) []GeneratorServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]GeneratorServerConfig, len(in))
	for i, c := range in {
		c.Args = slices.Clone(c.Args)
		out[i] = c
	}
	return out
}

func inferAssetGenKind(targetPath string, fallback AssetGeneratorKind) AssetGeneratorKind {
	if fallback.Valid() {
		return fallback
	}
	switch strings.ToLower(path.Ext(targetPath)) {
	case ".wav", ".ogg", ".mp3", ".flac", ".m4a", ".aac", ".opus":
		return AssetGenSound
	default:
		return AssetGenImages
	}
}

func cleanAssetTargetPath(rel string) (string, error) {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return "", errors.New("editor: target asset path is required")
	}
	clean := path.Clean(rel)
	if !fs.ValidPath(clean) || clean == "." || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("editor: invalid target asset path %q", rel)
	}
	if importsettings.IsSidecar(clean) || importsettings.IsAssetSource(clean) {
		return "", fmt.Errorf("editor: target asset path %q must name the media file, not a sidecar", rel)
	}
	return clean, nil
}

// verifyAssetGenWritePath verifies that absPath (and every existing ancestor directory down to rootDir)
// contains no symlink segments and resolves strictly inside rootDir.
func verifyAssetGenWritePath(rootDir, absPath, relLabel string) error {
	if strings.TrimSpace(rootDir) == "" {
		return ErrNoProject
	}
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	targetAbs, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	relClean, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || relClean == ".." || strings.HasPrefix(relClean, ".."+string(filepath.Separator)) || filepath.IsAbs(relClean) {
		return fmt.Errorf("editor: asset path %q resolves outside the project root", relLabel)
	}

	// Walk every existing path component from rootAbs down to targetAbs and refuse any symlink.
	cur := rootAbs
	for _, seg := range strings.Split(filepath.ToSlash(relClean), "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, lerr := os.Lstat(cur)
		if lerr != nil {
			if errors.Is(lerr, fs.ErrNotExist) {
				break
			}
			return lerr
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("editor: asset path %q traverses symlink outside containment", relLabel)
		}
	}

	// Resolve nearest existing ancestor (or target itself if present) via EvalSymlinks.
	probe := targetAbs
	for {
		realPath, rerr := filepath.EvalSymlinks(probe)
		if rerr == nil {
			relReal, err := filepath.Rel(rootReal, realPath)
			if err != nil || relReal == ".." || strings.HasPrefix(relReal, ".."+string(filepath.Separator)) || filepath.IsAbs(relReal) {
				return fmt.Errorf("editor: asset path %q resolves outside the project root", relLabel)
			}
			return nil
		}
		if !errors.Is(rerr, fs.ErrNotExist) {
			return rerr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return fmt.Errorf("editor: asset path %q resolves outside the project root", relLabel)
		}
		probe = parent
	}
}

// verifyAssetGenReadPath verifies that an existing file at absPath is not a symlink escaping rootDir.
func verifyAssetGenReadPath(rootDir, absPath, relLabel string) error {
	if fi, err := os.Lstat(absPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("editor: asset path %q refuses symlink file", relLabel)
	}
	return verifyFileWithinRoot(rootDir, absPath, relLabel)
}

func readBoundedAssetFile(absPath string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds maximum size of %d bytes", maxBytes)
	}
	return data, nil
}

// sanitizeSingleLineMeta cleans untrusted server-reported single-line metadata (model, license)
// so control characters or leading/trailing whitespace cannot break .source.yaml encoding later.
func sanitizeSingleLineMeta(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

type preparedAssetGenCall struct {
	params     GenerateAssetParams
	target     string
	prompt     string
	kind       AssetGeneratorKind
	session    *sdkmcp.ClientSession
	toolName   string
	genName    string
	brief      AssetStyleBrief
	bg         AssetPreviewBackground
	projectDir string
	epoch      uint64
	args       map[string]any
}

func (e *Editor) prepareAssetGenCall(params GenerateAssetParams) (*preparedAssetGenCall, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	if e.Playing() {
		return nil, ErrPlaying
	}
	if e.MergeConflictOpen() {
		return nil, errAssetGenMergeConflict
	}
	target, err := cleanAssetTargetPath(params.TargetPath)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return nil, errors.New("editor: generation prompt is required")
	}
	if strings.IndexFunc(prompt, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0 {
		return nil, errors.New("editor: generation prompt contains invalid control characters")
	}

	kind := inferAssetGenKind(target, params.Kind)
	projectDir := ""
	if e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}

	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	var conn *assetGenServerConn
	if name := strings.TrimSpace(params.Generator); name != "" {
		conn = e.assetGen.servers[name]
	} else {
		for _, name := range e.assetGen.serverOrder {
			if c := e.assetGen.servers[name]; c != nil && c.session != nil && c.config.Kind == kind {
				conn = c
				break
			}
		}
	}
	if conn == nil || conn.session == nil {
		e.assetGen.mu.Unlock()
		return nil, fmt.Errorf("editor: MCP generator %q is not connected", params.Generator)
	}
	session := conn.session
	toolName := conn.tool
	genName := conn.config.Name
	if !params.Kind.Valid() {
		kind = conn.config.Kind
	}
	brief := e.assetGen.brief
	bg := currentPreviewBGLocked(&e.assetGen)
	epoch := e.assetGen.epoch
	e.assetGen.mu.Unlock()

	count := params.Count
	if count <= 0 {
		if kind == AssetGenSound {
			count = 3
		} else {
			count = 4
		}
	}
	if count > maxGeneratedCandidates {
		count = maxGeneratedCandidates
	}

	args := map[string]any{
		"target": target,
		"prompt": prompt,
		"count":  count,
		"kind":   string(kind),
	}
	if m := sanitizeSingleLineMeta(params.Model); m != "" {
		args["model"] = m
	}
	if len(params.References) > 0 {
		args["references"] = slices.Clone(params.References)
	}
	if params.BaseSeed != nil {
		args["seed"] = *params.BaseSeed
	}
	if params.VaryFromSeed != nil {
		args["vary_seed"] = *params.VaryFromSeed
	}
	if r := strings.TrimSpace(params.Refine); r != "" {
		args["refine"] = r
	}

	return &preparedAssetGenCall{
		params:     params,
		target:     target,
		prompt:     prompt,
		kind:       kind,
		session:    session,
		toolName:   toolName,
		genName:    genName,
		brief:      brief,
		bg:         bg,
		projectDir: projectDir,
		epoch:      epoch,
		args:       args,
	}, nil
}

// executeAssetGenCall performs the MCP RPC and untrusted in-memory decoding without accessing any non-thread-safe Editor fields.
func executeAssetGenCall(ctx context.Context, prep *preparedAssetGenCall) (*AssetGenerationRequest, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultAssetGenCallTimeout)
		defer cancel()
	}

	start := time.Now()
	res, err := prep.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      prep.toolName,
		Arguments: prep.args,
	})
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("editor: MCP generator %q tool %q: %w", prep.genName, prep.toolName, err)
	}
	if res == nil {
		return nil, fmt.Errorf("editor: MCP generator %q returned nil result", prep.genName)
	}
	if res.IsError {
		return nil, fmt.Errorf("editor: MCP generator %q error: %s", prep.genName, extractAssetGenMCPText(res.Content))
	}

	payload, err := parseUntrustedMCPGeneratorResult(res, prep.kind)
	if err != nil {
		return nil, fmt.Errorf("editor: untrusted output from %q rejected: %w", prep.genName, err)
	}
	if payload.Elapsed > 0 {
		elapsed = payload.Elapsed
	}

	// Keep the user's requested prompt authoritative so an untrusted server cannot rewrite .source.yaml prompts.
	req := &AssetGenerationRequest{
		TargetPath:    prep.target,
		Kind:          prep.kind,
		Generator:     prep.genName,
		Model:         firstNonEmpty(sanitizeSingleLineMeta(payload.Model), sanitizeSingleLineMeta(prep.params.Model)),
		Prompt:        prep.prompt,
		UserPrompt:    firstNonEmpty(strings.TrimSpace(prep.params.UserPrompt), prep.prompt),
		References:    slices.Clone(prep.params.References),
		Count:         len(payload.Candidates),
		Elapsed:       elapsed,
		License:       sanitizeSingleLineMeta(payload.License),
		CommercialUse: payload.CommercialUse,
		Status:        AssetGenPending,
	}
	loadRequestReferenceFromDir(prep.projectDir, req)

	req.Candidates = make([]GeneratedCandidate, 0, len(payload.Candidates))
	var totalDecodedBytes int64
	for i, rawCand := range payload.Candidates {
		cid := candidateLetter(i)
		lic := firstNonEmpty(sanitizeSingleLineMeta(rawCand.License), req.License)
		comm := req.CommercialUse
		if rawCand.CommercialUse != nil {
			comm = *rawCand.CommercialUse
		}
		cand := GeneratedCandidate{
			ID:            cid,
			Seed:          cloneInt64Ptr(rawCand.Seed),
			MimeType:      rawCand.MimeType,
			License:       lic,
			CommercialUse: comm,
		}
		switch prep.kind {
		case AssetGenImages:
			imgRes, decErr := decodeUntrustedImage(rawCand.Data)
			if decErr != nil {
				return nil, fmt.Errorf("editor: candidate %s rejected (untrusted non-image payload): %w", cid, decErr)
			}
			candBytes := int64(imgRes.width)*int64(imgRes.height)*4 + int64(len(imgRes.cleanPNG))
			totalDecodedBytes += candBytes
			if totalDecodedBytes > maxGeneratedTotalDecodedBytes {
				return nil, fmt.Errorf("editor: untrusted output from %q rejected: total decoded image memory exceeds %d bytes", prep.genName, maxGeneratedTotalDecodedBytes)
			}
			cand.MimeType = "image/png"
			cand.CleanBytes = imgRes.cleanPNG
			cand.Width = imgRes.width
			cand.Height = imgRes.height
			cand.Pixels = imgRes.rgba
			cand.Image = core.ImageSource{Pixels: imgRes.rgba}
			cand.ColorCount = imgRes.colorCount
			cand.HasAntiAliasing = imgRes.hasAntiAliasing
		case AssetGenSound:
			audRes, decErr := decodeUntrustedAudio(rawCand.Data)
			if decErr != nil {
				return nil, fmt.Errorf("editor: candidate %s rejected (untrusted non-audio payload): %w", cid, decErr)
			}
			totalDecodedBytes += int64(len(audRes.cleanBytes))
			if totalDecodedBytes > maxGeneratedTotalDecodedBytes {
				return nil, fmt.Errorf("editor: untrusted output from %q rejected: total decoded audio memory exceeds %d bytes", prep.genName, maxGeneratedTotalDecodedBytes)
			}
			cand.MimeType = audRes.mimeType
			cand.CleanBytes = audRes.cleanBytes
			cand.Duration = audRes.duration
			cand.SampleRate = audRes.sampleRate
			cand.Channels = audRes.channels
			cand.LoudnessDBFS = audRes.rmsDBFS
			cand.PeakDBFS = audRes.peakDBFS
			cand.AudioLoudnessMeasured = audRes.loudnessMeasured
			cand.Waveform = audRes.waveform
		}
		req.Candidates = append(req.Candidates, cand)
	}
	if len(req.Candidates) > 0 {
		req.SelectedCandidateID = req.Candidates[0].ID
	}
	recheckRequestLocked(req, prep.brief, prep.bg)
	return req, nil
}

func (e *Editor) commitAssetGenResult(prep *preparedAssetGenCall, req *AssetGenerationRequest, note string, updateUI bool) (*AssetGenerationRequest, error) {
	e.assetGen.mu.Lock()
	if e.assetGen.epoch != prep.epoch {
		e.assetGen.mu.Unlock()
		return nil, context.Canceled
	}
	e.ensureAssetGenLocked()
	var existing *AssetGenerationRequest
	for _, r := range e.assetGen.requests {
		if r.TargetPath == prep.target {
			existing = r
			break
		}
	}
	if existing != nil {
		req.ID = existing.ID
		*existing = *req
		req = existing
	} else {
		e.assetGen.nextSeq++
		req.ID = fmt.Sprintf("gen-%d", e.assetGen.nextSeq)
		e.assetGen.requests = append(e.assetGen.requests, req)
	}
	if note != "" {
		req.LastNote = note
	}
	if prep.kind == AssetGenImages {
		e.assetGen.activeRequestID = req.ID
	} else {
		e.assetGen.activeSoundReqID = req.ID
		if e.assetGen.activeRequestID == "" {
			e.assetGen.activeRequestID = req.ID
		}
	}
	e.assetGen.rev++
	out := cloneAssetGenRequest(req)
	e.assetGen.mu.Unlock()

	if updateUI {
		e.Status = fmt.Sprintf("%s returned %d candidates for %s · waiting for your pick", prep.genName, len(out.Candidates), path.Base(prep.target))
		e.notify()
	}
	return &out, nil
}

// GenerateAsset invokes a connected MCP generator server, strictly decodes untrusted candidate payloads as pixels or audio in memory ONLY (writing NOTHING to disk), runs style and audio checks against brief.yaml and references, and adds or updates the request in the Generated Assets view.
func (e *Editor) GenerateAsset(ctx context.Context, params GenerateAssetParams) (*AssetGenerationRequest, error) {
	prep, err := e.prepareAssetGenCall(params)
	if err != nil {
		return nil, err
	}
	req, err := executeAssetGenCall(ctx, prep)
	if err != nil {
		return nil, err
	}
	return e.commitAssetGenResult(prep, req, "", true)
}

// startAsyncAssetGen dispatches an MCP generation call on a background goroutine with a bounded timeout,
// cancelling any previous in-flight UI request and marshaling completion back onto the UI thread cleanly.
func (e *Editor) startAsyncAssetGen(label string, params GenerateAssetParams, noteFn func(*AssetGenerationRequest) string) error {
	prep, err := e.prepareAssetGenCall(params)
	if err != nil {
		return err
	}
	scope := e.scope
	ctx, cancel := context.WithTimeout(context.Background(), defaultAssetGenCallTimeout)

	e.assetGen.mu.Lock()
	prevCancel := e.assetGen.inFlightCancel
	e.assetGen.inFlightCancel = cancel
	e.assetGen.inFlightSeq++
	seq := e.assetGen.inFlightSeq
	e.assetGen.busy = true
	e.assetGen.busyLabel = label
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}
	e.notify()

	finish := func(req *AssetGenerationRequest, callErr error, onUIThread bool) {
		cancel()
		e.assetGen.mu.Lock()
		stale := e.assetGen.inFlightSeq != seq || e.assetGen.epoch != prep.epoch
		if !stale {
			e.assetGen.inFlightCancel = nil
			e.assetGen.busy = false
			e.assetGen.busyLabel = ""
			e.assetGen.rev++
		}
		e.assetGen.mu.Unlock()
		if stale {
			return
		}
		if callErr != nil {
			if onUIThread {
				if !errors.Is(callErr, context.Canceled) {
					e.report(callErr)
				}
				e.notify()
			}
			return
		}
		note := ""
		if noteFn != nil {
			note = noteFn(req)
		}
		if _, err := e.commitAssetGenResult(prep, req, note, onUIThread); err != nil && onUIThread && !errors.Is(err, context.Canceled) {
			e.report(err)
		}
	}

	if scope == nil {
		go func() {
			req, callErr := executeAssetGenCall(ctx, prep)
			finish(req, callErr, false)
		}()
		return nil
	}

	go func() {
		req, callErr := executeAssetGenCall(ctx, prep)
		scope.Post(func() {
			finish(req, callErr, true)
		})
	}()
	return nil
}

// AssetGenBusy reports whether an asynchronous UI generation request is currently in flight and its status label.
func (e *Editor) AssetGenBusy() (bool, string) {
	if e == nil {
		return false, ""
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.busy, e.assetGen.busyLabel
}

func (e *Editor) startVaryGeneratedCandidateAsync(requestID, candidateID string) error {
	req, cand, err := e.lookupRequestCandidate(requestID, candidateID)
	if err != nil {
		return err
	}
	seedNote := ""
	if cand.Seed != nil {
		seedNote = fmt.Sprintf(" · seed %d kept, references unchanged", *cand.Seed)
	}
	params := GenerateAssetParams{
		TargetPath:   req.TargetPath,
		Generator:    req.Generator,
		Kind:         req.Kind,
		Model:        req.Model,
		Prompt:       req.Prompt,
		UserPrompt:   req.UserPrompt,
		References:   req.References,
		Count:        max(req.Count, len(req.Candidates)),
		VaryFromSeed: cloneInt64Ptr(cand.Seed),
	}
	return e.startAsyncAssetGen("Varying "+candidateID+"…", params, func(updated *AssetGenerationRequest) string {
		return fmt.Sprintf("Asked %s for %d variations of %s%s", req.Generator, len(updated.Candidates), candidateID, seedNote)
	})
}

func (e *Editor) startRefineGeneratedAssetAsync(requestID, words string) error {
	words = strings.TrimSpace(words)
	if words == "" {
		return errors.New("editor: refinement instructions are required")
	}
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return err
	}
	params := GenerateAssetParams{
		TargetPath: req.TargetPath,
		Generator:  req.Generator,
		Kind:       req.Kind,
		Model:      req.Model,
		Prompt:     req.Prompt + ", " + words,
		UserPrompt: req.UserPrompt,
		References: req.References,
		Count:      max(req.Count, len(req.Candidates)),
		Refine:     words,
	}
	return e.startAsyncAssetGen("Refining "+path.Base(req.TargetPath)+"…", params, func(*AssetGenerationRequest) string {
		return fmt.Sprintf("Refined %s with %q · references unchanged", path.Base(req.TargetPath), words)
	})
}

func (e *Editor) startRegenerateAssetAsync(requestID string) error {
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return err
	}
	var nextSeed *int64
	if len(req.Candidates) > 0 && req.Candidates[len(req.Candidates)-1].Seed != nil {
		v := *req.Candidates[len(req.Candidates)-1].Seed + 1
		nextSeed = &v
	}
	params := GenerateAssetParams{
		TargetPath: req.TargetPath,
		Generator:  req.Generator,
		Kind:       req.Kind,
		Model:      req.Model,
		Prompt:     req.Prompt,
		UserPrompt: req.UserPrompt,
		References: req.References,
		Count:      max(req.Count, len(req.Candidates)),
		BaseSeed:   nextSeed,
	}
	return e.startAsyncAssetGen("Regenerating "+path.Base(req.TargetPath)+"…", params, func(updated *AssetGenerationRequest) string {
		return fmt.Sprintf("Asked %s for %d new candidates · same prompt and references", req.Generator, len(updated.Candidates))
	})
}

// VaryGeneratedCandidate asks the generator for new variations of candidateID while preserving the request's references and seed context. Nothing is written to disk.
func (e *Editor) VaryGeneratedCandidate(ctx context.Context, requestID, candidateID string) (*AssetGenerationRequest, error) {
	req, cand, err := e.lookupRequestCandidate(requestID, candidateID)
	if err != nil {
		return nil, err
	}
	updated, err := e.GenerateAsset(ctx, GenerateAssetParams{
		TargetPath:   req.TargetPath,
		Generator:    req.Generator,
		Kind:         req.Kind,
		Model:        req.Model,
		Prompt:       req.Prompt,
		UserPrompt:   req.UserPrompt,
		References:   req.References,
		Count:        max(req.Count, len(req.Candidates)),
		VaryFromSeed: cloneInt64Ptr(cand.Seed),
	})
	if err != nil {
		return nil, err
	}
	seedNote := ""
	if cand.Seed != nil {
		seedNote = fmt.Sprintf(" · seed %d kept, references unchanged", *cand.Seed)
	}
	note := fmt.Sprintf("Asked %s for %d variations of %s%s", req.Generator, len(updated.Candidates), candidateID, seedNote)
	e.setRequestNote(updated.ID, note)
	updated.LastNote = note
	return updated, nil
}

// RefineGeneratedAsset re-invokes the generator with additional refinement words while keeping the exact same references. Nothing is written to disk.
func (e *Editor) RefineGeneratedAsset(ctx context.Context, requestID, words string) (*AssetGenerationRequest, error) {
	words = strings.TrimSpace(words)
	if words == "" {
		return nil, errors.New("editor: refinement instructions are required")
	}
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return nil, err
	}
	refinedPrompt := req.Prompt + ", " + words
	updated, err := e.GenerateAsset(ctx, GenerateAssetParams{
		TargetPath: req.TargetPath,
		Generator:  req.Generator,
		Kind:       req.Kind,
		Model:      req.Model,
		Prompt:     refinedPrompt,
		UserPrompt: req.UserPrompt,
		References: req.References,
		Count:      max(req.Count, len(req.Candidates)),
		Refine:     words,
	})
	if err != nil {
		return nil, err
	}
	note := fmt.Sprintf("Refined %s with %q · references unchanged", path.Base(req.TargetPath), words)
	e.setRequestNote(updated.ID, note)
	updated.LastNote = note
	return updated, nil
}

// RegenerateAsset re-invokes the generator for N fresh candidates with the same prompt and references. Nothing is written to disk.
func (e *Editor) RegenerateAsset(ctx context.Context, requestID string) (*AssetGenerationRequest, error) {
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return nil, err
	}
	var nextSeed *int64
	if len(req.Candidates) > 0 && req.Candidates[len(req.Candidates)-1].Seed != nil {
		v := *req.Candidates[len(req.Candidates)-1].Seed + 1
		nextSeed = &v
	}
	updated, err := e.GenerateAsset(ctx, GenerateAssetParams{
		TargetPath: req.TargetPath,
		Generator:  req.Generator,
		Kind:       req.Kind,
		Model:      req.Model,
		Prompt:     req.Prompt,
		UserPrompt: req.UserPrompt,
		References: req.References,
		Count:      max(req.Count, len(req.Candidates)),
		BaseSeed:   nextSeed,
	})
	if err != nil {
		return nil, err
	}
	note := fmt.Sprintf("Asked %s for %d new candidates · same prompt and references", req.Generator, len(updated.Candidates))
	e.setRequestNote(updated.ID, note)
	updated.LastNote = note
	return updated, nil
}

func (e *Editor) setRequestNote(requestID, note string) {
	e.assetGen.mu.Lock()
	for _, r := range e.assetGen.requests {
		if r.ID == requestID {
			r.LastNote = note
			e.assetGen.rev++
			break
		}
	}
	e.assetGen.mu.Unlock()
	e.notify()
}

// PickGeneratedCandidate writes EXACTLY the picked media file and its <file>.source.yaml sidecar to the open project atomically as ONE undoable History step.
// Calling Editor.Undo() once removes both files (or restores prior bytes if overwriting).
func (e *Editor) PickGeneratedCandidate(requestID, candidateID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return errAssetGenMergeConflict
	}
	if e.ProjectPath == "" {
		return ErrNoProject
	}
	req, cand, err := e.lookupRequestCandidate(requestID, candidateID)
	if err != nil {
		return err
	}
	if len(cand.CleanBytes) == 0 {
		return fmt.Errorf("editor: candidate %s has no decoded media bytes", candidateID)
	}

	assetRel, err := cleanAssetTargetPath(req.TargetPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(e.ProjectPath)
	sourceRel := importsettings.AssetSourcePath(assetRel)
	assetAbs := filepath.Join(root, filepath.FromSlash(assetRel))
	sourceAbs := filepath.Join(root, filepath.FromSlash(sourceRel))
	if err := verifyAssetGenWritePath(root, assetAbs, assetRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(root, sourceAbs, sourceRel); err != nil {
		return err
	}

	srcDoc := &importsettings.AssetSource{
		Version:       importsettings.AssetSourceVersion,
		Generator:     req.Generator,
		Model:         req.Model,
		Prompt:        req.Prompt,
		Seed:          cloneInt64Ptr(cand.Seed),
		References:    slices.Clone(req.References),
		License:       firstNonEmpty(cand.License, req.License),
		CommercialUse: cand.CommercialUse,
		ApprovedBy:    e.AssetApprovalAuthor(),
	}
	sourceBytes, err := importsettings.EncodeAssetSource(srcDoc)
	if err != nil {
		return err
	}

	beforeAssetExists, beforeAssetBytes, err := readOptionalBoundedFile(assetAbs, maxGeneratedAudioBytes)
	if err != nil {
		return err
	}
	beforeSourceExists, beforeSourceBytes, err := readOptionalBoundedFile(sourceAbs, maxGeneratedSourceBytes)
	if err != nil {
		return err
	}

	nextStatus := AssetGenPicked
	if importsettings.IsUnknownLicense(srcDoc.License) {
		nextStatus = AssetGenLicenseUnknown
	}

	cmd := &pickGeneratedAssetCmd{
		ed:                 e,
		label:              fmt.Sprintf("Pick %s for %s", candidateID, path.Base(assetRel)),
		rootDir:            root,
		requestID:          req.ID,
		candidateID:        candidateID,
		assetRel:           assetRel,
		sourceRel:          sourceRel,
		assetAbs:           assetAbs,
		sourceAbs:          sourceAbs,
		beforeAssetExists:  beforeAssetExists,
		beforeAssetBytes:   beforeAssetBytes,
		beforeSourceExists: beforeSourceExists,
		beforeSourceBytes:  beforeSourceBytes,
		afterAssetBytes:    slices.Clone(cand.CleanBytes),
		afterSourceBytes:   sourceBytes,
		beforePickedID:     req.PickedCandidateID,
		beforeStatus:       req.Status,
		afterStatus:        nextStatus,
	}
	return e.History.execute(cmd)
}

// UndoPickedCandidate reverts a previously picked generated asset cleanly.
// When the pick is still the most recent command in History, it invokes Editor.Undo() directly;
// otherwise it executes a targeted undoable revert of ONLY that picked asset and its .source.yaml
// without touching unrelated edits on History.
func (e *Editor) UndoPickedCandidate(requestID, candidateID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return errAssetGenMergeConflict
	}
	if e.ProjectPath == "" {
		return ErrNoProject
	}
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return err
	}
	if candidateID == "" {
		candidateID = req.PickedCandidateID
	}
	if req.PickedCandidateID == "" || req.PickedCandidateID != candidateID {
		return fmt.Errorf("editor: candidate %q is not currently picked for %s", candidateID, req.TargetPath)
	}
	if top, ok := e.History.topUndoCommand().(*pickGeneratedAssetCmd); ok && top.requestID == req.ID && top.candidateID == candidateID {
		e.Undo()
		return nil
	}

	assetRel, err := cleanAssetTargetPath(req.TargetPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(e.ProjectPath)
	sourceRel := importsettings.AssetSourcePath(assetRel)
	assetAbs := filepath.Join(root, filepath.FromSlash(assetRel))
	sourceAbs := filepath.Join(root, filepath.FromSlash(sourceRel))
	if err := verifyAssetGenWritePath(root, assetAbs, assetRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(root, sourceAbs, sourceRel); err != nil {
		return err
	}

	curAssetExists, curAssetBytes, err := readOptionalBoundedFile(assetAbs, maxGeneratedAudioBytes)
	if err != nil {
		return err
	}
	curSourceExists, curSourceBytes, err := readOptionalBoundedFile(sourceAbs, maxGeneratedSourceBytes)
	if err != nil {
		return err
	}

	var (
		restoreAssetExists  bool
		restoreAssetBytes   []byte
		restoreSourceExists bool
		restoreSourceBytes  []byte
		restorePickedID     string
		restoreStatus       = AssetGenPending
	)
	e.assetGen.mu.Lock()
	if prev := e.assetGen.lastPickByReq[req.ID]; prev != nil && prev.candidateID == candidateID {
		restoreAssetExists = prev.beforeAssetExists
		restoreAssetBytes = slices.Clone(prev.beforeAssetBytes)
		restoreSourceExists = prev.beforeSourceExists
		restoreSourceBytes = slices.Clone(prev.beforeSourceBytes)
		restorePickedID = prev.beforePickedID
		restoreStatus = prev.beforeStatus
	}
	e.assetGen.mu.Unlock()

	cmd := &unpickGeneratedAssetCmd{
		ed:                  e,
		label:               fmt.Sprintf("Undo pick %s for %s", candidateID, path.Base(assetRel)),
		rootDir:             root,
		requestID:           req.ID,
		candidateID:         candidateID,
		assetRel:            assetRel,
		sourceRel:           sourceRel,
		assetAbs:            assetAbs,
		sourceAbs:           sourceAbs,
		curAssetExists:      curAssetExists,
		curAssetBytes:       curAssetBytes,
		curSourceExists:     curSourceExists,
		curSourceBytes:      curSourceBytes,
		restoreAssetExists:  restoreAssetExists,
		restoreAssetBytes:   restoreAssetBytes,
		restoreSourceExists: restoreSourceExists,
		restoreSourceBytes:  restoreSourceBytes,
		curPickedID:         req.PickedCandidateID,
		curStatus:           req.Status,
		restorePickedID:     restorePickedID,
		restoreStatus:       restoreStatus,
	}
	return e.History.execute(cmd)
}

func readOptionalBoundedFile(absPath string, limit int64) (bool, []byte, error) {
	data, err := readBoundedAssetFile(absPath, limit)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil, nil
		}
		return false, nil, err
	}
	return true, data, nil
}

type pickGeneratedAssetCmd struct {
	ed                 *Editor
	label              string
	rootDir            string
	requestID          string
	candidateID        string
	assetRel           string
	sourceRel          string
	assetAbs           string
	sourceAbs          string
	beforeAssetExists  bool
	beforeAssetBytes   []byte
	beforeSourceExists bool
	beforeSourceBytes  []byte
	afterAssetBytes    []byte
	afterSourceBytes   []byte
	beforePickedID     string
	beforeStatus       AssetGenerationStatus
	afterStatus        AssetGenerationStatus
}

func (c *pickGeneratedAssetCmd) Label() string { return c.label }
func (c *pickGeneratedAssetCmd) Do()           { c.ed.report(c.do()) }
func (c *pickGeneratedAssetCmd) Undo()         { c.ed.report(c.undo()) }

func (c *pickGeneratedAssetCmd) do() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.assetAbs, c.assetRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.assetAbs), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(c.assetAbs, c.afterAssetBytes); err != nil {
		return err
	}
	if err := writeFileAtomic(c.sourceAbs, c.afterSourceBytes); err != nil {
		// Roll back the asset file if writing the source sidecar failed.
		_ = restoreOptionalFile(c.assetAbs, c.beforeAssetExists, c.beforeAssetBytes)
		return err
	}
	c.setReqState(c.candidateID, c.afterStatus, true)
	c.ed.Status = fmt.Sprintf("Picked %s · %s and %s written", c.candidateID, path.Base(c.assetRel), path.Base(c.sourceRel))
	return nil
}

func (c *pickGeneratedAssetCmd) undo() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(c.rootDir, c.assetAbs, c.assetRel); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.sourceAbs, c.beforeSourceExists, c.beforeSourceBytes); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.assetAbs, c.beforeAssetExists, c.beforeAssetBytes); err != nil {
		return err
	}
	c.setReqState(c.beforePickedID, c.beforeStatus, false)
	c.ed.Status = fmt.Sprintf("Undid pick for %s", path.Base(c.assetRel))
	return nil
}

func (c *pickGeneratedAssetCmd) setReqState(pickedID string, status AssetGenerationStatus, recordPick bool) {
	c.ed.assetGen.mu.Lock()
	c.ed.ensureAssetGenLocked()
	for _, r := range c.ed.assetGen.requests {
		if r.ID == c.requestID {
			r.PickedCandidateID = pickedID
			r.Status = status
			if pickedID != "" {
				r.SelectedCandidateID = pickedID
			}
			break
		}
	}
	if recordPick {
		c.ed.assetGen.lastPickByReq[c.requestID] = c
	} else if c.ed.assetGen.lastPickByReq[c.requestID] == c {
		delete(c.ed.assetGen.lastPickByReq, c.requestID)
	}
	c.ed.assetGen.rev++
	c.ed.assetGen.mu.Unlock()
	c.ed.filesRev++
	c.ed.invalidateProjectIndex()
	c.ed.RefreshGeneratedAssetLicenses()
	if c.ed.ExportPanelOpen() {
		c.ed.CheckExportCredits()
	}
	c.ed.notify()
}

type unpickGeneratedAssetCmd struct {
	ed                  *Editor
	label               string
	rootDir             string
	requestID           string
	candidateID         string
	assetRel            string
	sourceRel           string
	assetAbs            string
	sourceAbs           string
	curAssetExists      bool
	curAssetBytes       []byte
	curSourceExists     bool
	curSourceBytes      []byte
	restoreAssetExists  bool
	restoreAssetBytes   []byte
	restoreSourceExists bool
	restoreSourceBytes  []byte
	curPickedID         string
	curStatus           AssetGenerationStatus
	restorePickedID     string
	restoreStatus       AssetGenerationStatus
}

func (c *unpickGeneratedAssetCmd) Label() string { return c.label }
func (c *unpickGeneratedAssetCmd) Do()           { c.ed.report(c.do()) }
func (c *unpickGeneratedAssetCmd) Undo()         { c.ed.report(c.undo()) }

func (c *unpickGeneratedAssetCmd) do() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(c.rootDir, c.assetAbs, c.assetRel); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.sourceAbs, c.restoreSourceExists, c.restoreSourceBytes); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.assetAbs, c.restoreAssetExists, c.restoreAssetBytes); err != nil {
		return err
	}
	c.setReqState(c.restorePickedID, c.restoreStatus)
	c.ed.Status = fmt.Sprintf("Undid pick for %s", path.Base(c.assetRel))
	return nil
}

func (c *unpickGeneratedAssetCmd) undo() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.assetAbs, c.assetRel); err != nil {
		return err
	}
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.assetAbs, c.curAssetExists, c.curAssetBytes); err != nil {
		return err
	}
	if err := restoreOptionalFile(c.sourceAbs, c.curSourceExists, c.curSourceBytes); err != nil {
		return err
	}
	c.setReqState(c.curPickedID, c.curStatus)
	c.ed.Status = fmt.Sprintf("Restored pick %s for %s", c.candidateID, path.Base(c.assetRel))
	return nil
}

func (c *unpickGeneratedAssetCmd) setReqState(pickedID string, status AssetGenerationStatus) {
	c.ed.assetGen.mu.Lock()
	for _, r := range c.ed.assetGen.requests {
		if r.ID == c.requestID {
			r.PickedCandidateID = pickedID
			r.Status = status
			if pickedID != "" {
				r.SelectedCandidateID = pickedID
			}
			break
		}
	}
	c.ed.assetGen.rev++
	c.ed.assetGen.mu.Unlock()
	c.ed.filesRev++
	c.ed.invalidateProjectIndex()
	c.ed.RefreshGeneratedAssetLicenses()
	if c.ed.ExportPanelOpen() {
		c.ed.CheckExportCredits()
	}
	c.ed.notify()
}

func restoreOptionalFile(absPath string, existed bool, data []byte) error {
	if existed {
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		return writeFileAtomic(absPath, data)
	}
	if err := os.Remove(absPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SetGeneratedAssetLicense updates license and commercialUse in <relPath>.source.yaml as one undoable History step.
func (e *Editor) SetGeneratedAssetLicense(relPath, license string, commercialUse bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return errAssetGenMergeConflict
	}
	if e.ProjectPath == "" {
		return ErrNoProject
	}
	clean, err := cleanAssetTargetPath(relPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(e.ProjectPath)
	sourceRel := importsettings.AssetSourcePath(clean)
	sourceAbs := filepath.Join(root, filepath.FromSlash(sourceRel))
	if err := verifyAssetGenWritePath(root, sourceAbs, sourceRel); err != nil {
		return err
	}
	beforeBytes, err := readBoundedAssetFile(sourceAbs, maxGeneratedSourceBytes)
	if err != nil {
		return fmt.Errorf("editor: read %s: %w", sourceRel, err)
	}
	src, err := importsettings.ParseAssetSource(beforeBytes)
	if err != nil {
		return err
	}
	src.License = sanitizeSingleLineMeta(license)
	src.CommercialUse = commercialUse
	afterBytes, err := importsettings.EncodeAssetSource(src)
	if err != nil {
		return err
	}
	cmd := &setSourceLicenseCmd{
		ed:          e,
		label:       fmt.Sprintf("Set license for %s", path.Base(clean)),
		rootDir:     root,
		assetRel:    clean,
		sourceRel:   sourceRel,
		sourceAbs:   sourceAbs,
		beforeBytes: beforeBytes,
		afterBytes:  afterBytes,
		afterLic:    src.License,
		afterComm:   src.CommercialUse,
	}
	return e.History.execute(cmd)
}

// BeginEditGeneratedLicense opens the inline license editor for assetPath without fabricating a license value.
func (e *Editor) BeginEditGeneratedLicense(assetPath string) {
	if e == nil {
		return
	}
	clean, err := cleanAssetTargetPath(assetPath)
	if err != nil {
		e.report(err)
		return
	}
	draft := ""
	comm := false
	e.assetGen.mu.Lock()
	for _, entry := range e.assetGen.licenses {
		if entry.Asset == clean {
			if !importsettings.IsUnknownLicense(entry.License) {
				draft = entry.License
			}
			comm = entry.CommercialUse
			break
		}
	}
	e.assetGen.licenseEditPath = clean
	e.assetGen.licenseDraft = draft
	e.assetGen.licenseEditComm = comm
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	e.notify()
}

// CancelEditGeneratedLicense closes the inline license editor without writing anything.
func (e *Editor) CancelEditGeneratedLicense() {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	changed := e.assetGen.licenseEditPath != "" || e.assetGen.licenseDraft != ""
	e.assetGen.licenseEditPath = ""
	e.assetGen.licenseDraft = ""
	e.assetGen.licenseEditComm = false
	if changed {
		e.assetGen.rev++
	}
	e.assetGen.mu.Unlock()
	if changed {
		e.notify()
	}
}

// SetGeneratedLicenseDraft updates the in-memory license text being typed in the inline editor
// without bumping assetGen.rev so the active text input never remounts or loses focus.
func (e *Editor) SetGeneratedLicenseDraft(val string) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	e.assetGen.licenseDraft = val
	e.assetGen.mu.Unlock()
}

// SetGeneratedLicenseCommercial toggles the commercialUse flag in the inline license editor.
func (e *Editor) SetGeneratedLicenseCommercial(on bool) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	e.assetGen.licenseEditComm = on
	e.assetGen.mu.Unlock()
}

// SaveEditedGeneratedLicense commits the user-entered license string and commercialUse flag to <asset>.source.yaml.
func (e *Editor) SaveEditedGeneratedLicense() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.assetGen.mu.Lock()
	editPath := e.assetGen.licenseEditPath
	draft := strings.TrimSpace(e.assetGen.licenseDraft)
	comm := e.assetGen.licenseEditComm
	e.assetGen.mu.Unlock()
	if editPath == "" {
		return errors.New("editor: no generated asset selected for license edit")
	}
	if draft == "" {
		return errors.New("editor: enter a license identifier (for example CC0-1.0, CC-BY-4.0, or Commercial)")
	}
	return e.SetGeneratedAssetLicense(editPath, draft, comm)
}

func (e *Editor) assetGenLicenseEditState() (editPath, draft string, comm bool) {
	if e == nil {
		return "", "", false
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.licenseEditPath, e.assetGen.licenseDraft, e.assetGen.licenseEditComm
}

type setSourceLicenseCmd struct {
	ed          *Editor
	label       string
	rootDir     string
	assetRel    string
	sourceRel   string
	sourceAbs   string
	beforeBytes []byte
	afterBytes  []byte
	afterLic    string
	afterComm   bool
}

func (c *setSourceLicenseCmd) Label() string { return c.label }
func (c *setSourceLicenseCmd) Do()           { c.ed.report(c.do()) }
func (c *setSourceLicenseCmd) Undo()         { c.ed.report(c.undo()) }

func (c *setSourceLicenseCmd) do() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := writeFileAtomic(c.sourceAbs, c.afterBytes); err != nil {
		return err
	}
	c.sync(c.afterLic, c.afterComm)
	return nil
}

func (c *setSourceLicenseCmd) undo() error {
	if err := verifyAssetGenWritePath(c.rootDir, c.sourceAbs, c.sourceRel); err != nil {
		return err
	}
	if err := writeFileAtomic(c.sourceAbs, c.beforeBytes); err != nil {
		return err
	}
	src, _ := importsettings.ParseAssetSource(c.beforeBytes)
	lic, comm := "", false
	if src != nil {
		lic, comm = src.License, src.CommercialUse
	}
	c.sync(lic, comm)
	return nil
}

func (c *setSourceLicenseCmd) sync(lic string, comm bool) {
	c.ed.assetGen.mu.Lock()
	for _, r := range c.ed.assetGen.requests {
		if r.TargetPath == c.assetRel {
			r.License = lic
			r.CommercialUse = comm
			if importsettings.IsUnknownLicense(lic) {
				r.Status = AssetGenLicenseUnknown
			} else if r.PickedCandidateID != "" {
				r.Status = AssetGenPicked
			}
		}
	}
	c.ed.assetGen.licenseEditPath = ""
	c.ed.assetGen.licenseDraft = ""
	c.ed.assetGen.licenseEditComm = false
	c.ed.assetGen.rev++
	c.ed.assetGen.mu.Unlock()
	c.ed.RefreshGeneratedAssetLicenses()
	if c.ed.ExportPanelOpen() {
		c.ed.CheckExportCredits()
	}
	c.ed.notify()
}

// RefreshGeneratedAssetLicenses scans every *.source.yaml in the open project and updates the LICENSES · CHECKED AT EXPORT list.
func (e *Editor) RefreshGeneratedAssetLicenses() []credits.GeneratedAssetLicense {
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	items, err := credits.ScanGeneratedLicenses(filepath.Dir(e.ProjectPath))
	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	if err != nil {
		e.assetGen.licenseScanErr = err.Error()
		e.assetGen.licenses = nil
		e.assetGen.rev++
		e.assetGen.mu.Unlock()
		return nil
	}
	e.assetGen.licenseScanErr = ""
	e.assetGen.licenses = items
	e.assetGen.rev++
	e.assetGen.mu.Unlock()
	return slices.Clone(items)
}

// GeneratedAssetLicenseError returns the last scan/parse error from RefreshGeneratedAssetLicenses, if any.
func (e *Editor) GeneratedAssetLicenseError() string {
	if e == nil {
		return ""
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.licenseScanErr
}

// GeneratedAssetLicenses returns the last scanned *.source.yaml license entries for the open project.
func (e *Editor) GeneratedAssetLicenses() []credits.GeneratedAssetLicense {
	if e == nil {
		return nil
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return slices.Clone(e.assetGen.licenses)
}

// GeneratedAssetRequests returns a snapshot of all active generation requests in order.
func (e *Editor) GeneratedAssetRequests() []AssetGenerationRequest {
	if e == nil {
		return nil
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	out := make([]AssetGenerationRequest, len(e.assetGen.requests))
	for i, r := range e.assetGen.requests {
		out[i] = cloneAssetGenRequest(r)
	}
	return out
}

// SelectGeneratedRequest activates requestID in the Generated Assets screen.
func (e *Editor) SelectGeneratedRequest(requestID string) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	for _, r := range e.assetGen.requests {
		if r.ID == requestID {
			if r.Kind == AssetGenSound {
				e.assetGen.activeSoundReqID = r.ID
			} else {
				e.assetGen.activeRequestID = r.ID
			}
			e.assetGen.rev++
			break
		}
	}
	e.assetGen.mu.Unlock()
	e.notify()
}

// SelectGeneratedCandidate selects candidateID inside requestID for detailed style and source inspection.
func (e *Editor) SelectGeneratedCandidate(requestID, candidateID string) {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	for _, r := range e.assetGen.requests {
		if r.ID == requestID {
			for _, c := range r.Candidates {
				if c.ID == candidateID {
					r.SelectedCandidateID = candidateID
					e.assetGen.rev++
					break
				}
			}
			break
		}
	}
	e.assetGen.mu.Unlock()
	e.notify()
}

// PlayGeneratedSoundCandidate auditions an in-memory generated sound candidate without writing anything to disk.
func (e *Editor) PlayGeneratedSoundCandidate(requestID, candidateID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return errAssetGenMergeConflict
	}
	req, cand, err := e.lookupRequestCandidate(requestID, candidateID)
	if err != nil {
		return err
	}
	if req.Kind != AssetGenSound || len(cand.CleanBytes) == 0 {
		return fmt.Errorf("editor: candidate %s is not a playable sound", candidateID)
	}
	e.assetGen.mu.Lock()
	same := e.assetGen.playingReqID == req.ID && e.assetGen.playingCandID == cand.ID
	if same {
		e.assetGen.playingReqID = ""
		e.assetGen.playingCandID = ""
		e.assetGen.rev++
		e.assetGen.mu.Unlock()
		e.stopAudioAudition()
		e.notify()
		return nil
	}
	e.assetGen.playingReqID = req.ID
	e.assetGen.playingCandID = cand.ID
	e.assetGen.rev++
	e.assetGen.mu.Unlock()

	// When a UI scope is attached, play directly from in-memory bytes via the editor's audio player.
	if e.scope != nil {
		if mixer := e.AuditionMixer(); mixer != nil {
			src := audio.Source{Data: cand.CleanBytes}
			if e.audioMixer.music != nil {
				e.audioMixer.music.Play(src, 0)
			}
		}
	}
	e.Status = fmt.Sprintf("Playing candidate %s of %s", cand.ID, path.Base(req.TargetPath))
	e.notify()
	return nil
}

// StopGeneratedSoundCandidate stops any currently auditioning generated sound candidate.
func (e *Editor) StopGeneratedSoundCandidate() {
	if e == nil {
		return
	}
	e.assetGen.mu.Lock()
	had := e.assetGen.playingReqID != "" || e.assetGen.playingCandID != ""
	e.assetGen.playingReqID = ""
	e.assetGen.playingCandID = ""
	if had {
		e.assetGen.rev++
	}
	e.assetGen.mu.Unlock()
	if had {
		e.stopAudioAudition()
		e.notify()
	}
}

// PlayingGeneratedSound returns the currently auditioning (requestID, candidateID), or ("", "") when idle.
func (e *Editor) PlayingGeneratedSound() (string, string) {
	if e == nil {
		return "", ""
	}
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.playingReqID, e.assetGen.playingCandID
}

func (e *Editor) lookupRequest(requestID string) (AssetGenerationRequest, error) {
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	for _, r := range e.assetGen.requests {
		if r.ID == requestID || r.TargetPath == requestID {
			return cloneAssetGenRequest(r), nil
		}
	}
	return AssetGenerationRequest{}, fmt.Errorf("editor: generated asset request %q not found", requestID)
}

func (e *Editor) lookupRequestCandidate(requestID, candidateID string) (AssetGenerationRequest, GeneratedCandidate, error) {
	req, err := e.lookupRequest(requestID)
	if err != nil {
		return AssetGenerationRequest{}, GeneratedCandidate{}, err
	}
	for _, c := range req.Candidates {
		if c.ID == candidateID {
			return req, c, nil
		}
	}
	return AssetGenerationRequest{}, GeneratedCandidate{}, fmt.Errorf("editor: candidate %q not found in request %q", candidateID, requestID)
}

func cloneAssetGenRequest(r *AssetGenerationRequest) AssetGenerationRequest {
	cp := *r
	cp.References = slices.Clone(r.References)
	if r.RefLoudDB != nil {
		v := *r.RefLoudDB
		cp.RefLoudDB = &v
	}
	cp.Candidates = make([]GeneratedCandidate, len(r.Candidates))
	for i, c := range r.Candidates {
		c.Seed = cloneInt64Ptr(c.Seed)
		c.CleanBytes = slices.Clone(c.CleanBytes)
		c.Waveform = slices.Clone(c.Waveform)
		c.Checks = slices.Clone(c.Checks)
		cp.Candidates[i] = c
	}
	return cp
}

func cloneInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func candidateLetter(idx int) string {
	if idx >= 0 && idx < 26 {
		return string(rune('A' + idx))
	}
	return strconv.Itoa(idx + 1)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- Untrusted MCP payload decoding (strictly in memory, never touches disk) ---

type mcpGeneratorWireCandidate struct {
	Seed          *int64
	MimeType      string
	Data          []byte
	License       string
	CommercialUse *bool
}

type mcpGeneratorWireResponse struct {
	Model         string
	Prompt        string
	License       string
	CommercialUse bool
	Elapsed       time.Duration
	Candidates    []mcpGeneratorWireCandidate
}

func extractAssetGenMCPText(content []sdkmcp.Content) string {
	var parts []string
	for _, c := range content {
		if tc, ok := c.(*sdkmcp.TextContent); ok && strings.TrimSpace(tc.Text) != "" {
			parts = append(parts, strings.TrimSpace(tc.Text))
		}
	}
	return strings.Join(parts, "; ")
}

func parseUntrustedMCPGeneratorResult(res *sdkmcp.CallToolResult, kind AssetGeneratorKind) (*mcpGeneratorWireResponse, error) {
	type jsonCand struct {
		Seed             *int64 `json:"seed,omitempty"`
		MimeType         string `json:"mimeType,omitempty"`
		MimeTypeAlt      string `json:"mime_type,omitempty"`
		Data             string `json:"data,omitempty"`
		Base64           string `json:"base64,omitempty"`
		License          string `json:"license,omitempty"`
		CommercialUse    *bool  `json:"commercialUse,omitempty"`
		CommercialUseAlt *bool  `json:"commercial_use,omitempty"`
	}
	type jsonPayload struct {
		Model            string     `json:"model,omitempty"`
		Prompt           string     `json:"prompt,omitempty"`
		License          string     `json:"license,omitempty"`
		CommercialUse    *bool      `json:"commercialUse,omitempty"`
		CommercialUseAlt *bool      `json:"commercial_use,omitempty"`
		ElapsedMs        int64      `json:"elapsedMs,omitempty"`
		ElapsedSeconds   float64    `json:"elapsedSeconds,omitempty"`
		Candidates       []jsonCand `json:"candidates,omitempty"`
		Seeds            []int64    `json:"seeds,omitempty"`
	}

	var meta jsonPayload
	haveMeta := false
	if res.StructuredContent != nil {
		if raw, err := json.Marshal(res.StructuredContent); err == nil {
			if err := json.Unmarshal(raw, &meta); err == nil {
				haveMeta = true
			}
		}
	}
	if !haveMeta {
		for _, c := range res.Content {
			tc, ok := c.(*sdkmcp.TextContent)
			if !ok {
				continue
			}
			txt := strings.TrimSpace(tc.Text)
			if strings.HasPrefix(txt, "{") {
				if err := json.Unmarshal([]byte(txt), &meta); err == nil {
					haveMeta = true
					break
				}
			}
		}
	}

	out := &mcpGeneratorWireResponse{
		Model:   sanitizeSingleLineMeta(meta.Model),
		Prompt:  meta.Prompt,
		License: sanitizeSingleLineMeta(meta.License),
	}
	if meta.CommercialUse != nil {
		out.CommercialUse = *meta.CommercialUse
	} else if meta.CommercialUseAlt != nil {
		out.CommercialUse = *meta.CommercialUseAlt
	}
	if meta.ElapsedMs > 0 {
		ms := meta.ElapsedMs
		if maxMs := maxGeneratedElapsed.Milliseconds(); ms > maxMs {
			ms = maxMs
		}
		out.Elapsed = time.Duration(ms) * time.Millisecond
	} else if meta.ElapsedSeconds > 0 && !math.IsNaN(meta.ElapsedSeconds) && !math.IsInf(meta.ElapsedSeconds, 0) {
		sec := meta.ElapsedSeconds
		if maxSec := maxGeneratedElapsed.Seconds(); sec > maxSec {
			sec = maxSec
		}
		out.Elapsed = time.Duration(sec * float64(time.Second))
	}

	maxRaw := maxGeneratedImageBytes
	if kind == AssetGenSound {
		maxRaw = maxGeneratedAudioBytes
	}
	maxB64Len := base64.StdEncoding.EncodedLen(maxRaw)

	// 1. Decode inline JSON candidates when present.
	if len(meta.Candidates) > maxGeneratedCandidates {
		return nil, fmt.Errorf("generator returned %d candidates, exceeding limit %d", len(meta.Candidates), maxGeneratedCandidates)
	}
	var totalRaw int64
	for i, jc := range meta.Candidates {
		b64 := strings.TrimSpace(firstNonEmpty(jc.Data, jc.Base64))
		if b64 == "" {
			continue
		}
		if len(b64) > maxB64Len {
			return nil, fmt.Errorf("candidate %d base64 payload (%d chars) exceeds limit for %d raw bytes", i+1, len(b64), maxRaw)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("candidate %d invalid base64: %w", i+1, err)
		}
		if len(raw) > maxRaw {
			return nil, fmt.Errorf("candidate %d payload (%d bytes) exceeds limit %d", i+1, len(raw), maxRaw)
		}
		totalRaw += int64(len(raw))
		if totalRaw > maxGeneratedTotalDecodedBytes {
			return nil, fmt.Errorf("generator total payload (%d bytes) exceeds limit %d", totalRaw, maxGeneratedTotalDecodedBytes)
		}
		comm := jc.CommercialUse
		if comm == nil {
			comm = jc.CommercialUseAlt
		}
		seed := cloneInt64Ptr(jc.Seed)
		if seed == nil && i < len(meta.Seeds) {
			s := meta.Seeds[i]
			seed = &s
		}
		out.Candidates = append(out.Candidates, mcpGeneratorWireCandidate{
			Seed:          seed,
			MimeType:      firstNonEmpty(jc.MimeType, jc.MimeTypeAlt),
			Data:          raw,
			License:       sanitizeSingleLineMeta(jc.License),
			CommercialUse: comm,
		})
	}

	// 2. Also collect standard MCP ImageContent / AudioContent blocks.
	if len(out.Candidates) == 0 {
		mediaIdx := 0
		for _, c := range res.Content {
			switch block := c.(type) {
			case *sdkmcp.ImageContent:
				if kind != AssetGenImages {
					continue
				}
				if len(out.Candidates) >= maxGeneratedCandidates {
					return nil, fmt.Errorf("generator returned more than %d image candidates", maxGeneratedCandidates)
				}
				if len(block.Data) > maxRaw {
					return nil, fmt.Errorf("candidate %d image payload (%d bytes) exceeds limit %d", mediaIdx+1, len(block.Data), maxRaw)
				}
				totalRaw += int64(len(block.Data))
				if totalRaw > maxGeneratedTotalDecodedBytes {
					return nil, fmt.Errorf("generator total payload (%d bytes) exceeds limit %d", totalRaw, maxGeneratedTotalDecodedBytes)
				}
				var seed *int64
				if mediaIdx < len(meta.Seeds) {
					s := meta.Seeds[mediaIdx]
					seed = &s
				}
				out.Candidates = append(out.Candidates, mcpGeneratorWireCandidate{
					Seed:     seed,
					MimeType: block.MIMEType,
					Data:     slices.Clone(block.Data),
				})
				mediaIdx++
			case *sdkmcp.AudioContent:
				if kind != AssetGenSound {
					continue
				}
				if len(out.Candidates) >= maxGeneratedCandidates {
					return nil, fmt.Errorf("generator returned more than %d sound candidates", maxGeneratedCandidates)
				}
				if len(block.Data) > maxRaw {
					return nil, fmt.Errorf("candidate %d audio payload (%d bytes) exceeds limit %d", mediaIdx+1, len(block.Data), maxRaw)
				}
				totalRaw += int64(len(block.Data))
				if totalRaw > maxGeneratedTotalDecodedBytes {
					return nil, fmt.Errorf("generator total payload (%d bytes) exceeds limit %d", totalRaw, maxGeneratedTotalDecodedBytes)
				}
				var seed *int64
				if mediaIdx < len(meta.Seeds) {
					s := meta.Seeds[mediaIdx]
					seed = &s
				}
				out.Candidates = append(out.Candidates, mcpGeneratorWireCandidate{
					Seed:     seed,
					MimeType: block.MIMEType,
					Data:     slices.Clone(block.Data),
				})
				mediaIdx++
			}
		}
	}

	if len(out.Candidates) == 0 {
		return nil, errors.New("generator returned no decodable candidates")
	}
	return out, nil
}

type decodedImageInfo struct {
	cleanPNG        []byte
	rgba            *image.RGBA
	width           int
	height          int
	colorCount      int
	hasAntiAliasing bool
}

// decodeUntrustedImage strictly decodes raw bytes as raster pixels in memory only and re-encodes a clean PNG.
// Non-image content (HTML, SVG, scripts, executables) or oversized images are refused immediately.
func decodeUntrustedImage(raw []byte) (*decodedImageInfo, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty image payload")
	}
	if len(raw) > maxGeneratedImageBytes {
		return nil, fmt.Errorf("image payload %d bytes exceeds limit %d", len(raw), maxGeneratedImageBytes)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("not a valid raster image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxGeneratedImageDim || cfg.Height > maxGeneratedImageDim {
		return nil, fmt.Errorf("image dimensions %dx%d out of safe bounds", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode raster image: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)

	palette := make(map[color.RGBA]struct{})
	hasAA := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := rgba.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			if c.A < 255 {
				hasAA = true
			}
			palette[c] = struct{}{}
		}
	}

	var cleanBuf bytes.Buffer
	if err := png.Encode(&cleanBuf, rgba); err != nil {
		return nil, fmt.Errorf("re-encode clean PNG: %w", err)
	}
	return &decodedImageInfo{
		cleanPNG:        cleanBuf.Bytes(),
		rgba:            rgba,
		width:           w,
		height:          h,
		colorCount:      len(palette),
		hasAntiAliasing: hasAA,
	}, nil
}

type decodedAudioInfo struct {
	mimeType         string
	cleanBytes       []byte
	duration         time.Duration
	sampleRate       int
	channels         int
	rmsDBFS          float64
	peakDBFS         float64
	loudnessMeasured bool
	waveform         []float32
}

// decodeUntrustedAudio strictly validates WAV or OGG audio bytes in memory only.
func decodeUntrustedAudio(raw []byte) (*decodedAudioInfo, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty audio payload")
	}
	if len(raw) > maxGeneratedAudioBytes {
		return nil, fmt.Errorf("audio payload %d bytes exceeds limit %d", len(raw), maxGeneratedAudioBytes)
	}
	if len(raw) >= 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WAVE" {
		buf, err := audioconv.DecodeWAV(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("decode WAV: %w", err)
		}
		if err := buf.Validate(); err != nil {
			return nil, fmt.Errorf("invalid WAV buffer: %w", err)
		}
		var clean bytes.Buffer
		if err := audioconv.EncodeWAV(&clean, buf); err != nil {
			return nil, fmt.Errorf("re-encode clean WAV: %w", err)
		}
		return &decodedAudioInfo{
			mimeType:         "audio/wav",
			cleanBytes:       clean.Bytes(),
			duration:         buf.Duration(),
			sampleRate:       buf.SampleRate,
			channels:         buf.Channels,
			rmsDBFS:          buf.RMSDBFS(),
			peakDBFS:         buf.PeakDBFS(),
			loudnessMeasured: true,
			waveform:         computeAssetGenWaveformBars(buf, 64),
		}, nil
	}
	if len(raw) >= 28 && string(raw[0:4]) == "OggS" {
		dur, sr, ch, err := validateUntrustedOGG(raw)
		if err != nil {
			return nil, err
		}
		return &decodedAudioInfo{
			mimeType:         "audio/ogg",
			cleanBytes:       slices.Clone(raw),
			duration:         dur,
			sampleRate:       sr,
			channels:         ch,
			rmsDBFS:          math.NaN(),
			peakDBFS:         math.NaN(),
			loudnessMeasured: false,
			waveform:         nil,
		}, nil
	}
	return nil, errors.New("unsupported or non-audio payload (want WAV or OGG)")
}

func validateUntrustedOGG(raw []byte) (time.Duration, int, int, error) {
	if len(raw) < 28 || string(raw[0:4]) != "OggS" || raw[4] != 0 {
		return 0, 0, 0, errors.New("invalid OGG container header")
	}
	segCount := int(raw[26])
	if len(raw) < 27+segCount {
		return 0, 0, 0, errors.New("truncated OGG page segment table")
	}
	bodyOffset := 27 + segCount
	bodyLen := 0
	for i := 0; i < segCount; i++ {
		bodyLen += int(raw[27+i])
	}
	if len(raw) < bodyOffset+bodyLen {
		return 0, 0, 0, errors.New("truncated OGG identification page")
	}
	pkt := raw[bodyOffset : bodyOffset+bodyLen]
	sampleRate := 48000
	channels := 1
	switch {
	case len(pkt) >= 16 && pkt[0] == 1 && string(pkt[1:7]) == "vorbis":
		channels = int(pkt[11])
		sampleRate = int(binary.LittleEndian.Uint32(pkt[12:16]))
	case len(pkt) >= 16 && string(pkt[0:8]) == "OpusHead":
		channels = int(pkt[9])
		sampleRate = 48000
	default:
		return 0, 0, 0, errors.New("OGG stream is neither Vorbis nor Opus")
	}
	if sampleRate <= 0 || channels <= 0 || channels > 2 {
		return 0, 0, 0, errors.New("invalid OGG audio stream parameters")
	}
	// Find last OggS page granule position to estimate duration.
	var lastGranule uint64
	for pos := 0; pos+27 <= len(raw); {
		if string(raw[pos:pos+4]) != "OggS" {
			break
		}
		g := binary.LittleEndian.Uint64(raw[pos+6 : pos+14])
		if g != ^uint64(0) && g > lastGranule {
			lastGranule = g
		}
		sc := int(raw[pos+26])
		if pos+27+sc > len(raw) {
			break
		}
		bl := 0
		for i := 0; i < sc; i++ {
			bl += int(raw[pos+27+i])
		}
		pos += 27 + sc + bl
	}
	var dur time.Duration
	if lastGranule > 0 {
		dur = time.Duration(float64(lastGranule) / float64(sampleRate) * float64(time.Second))
	}
	return dur, sampleRate, channels, nil
}

func computeAssetGenWaveformBars(buf *audioconv.Buffer, bars int) []float32 {
	if buf == nil || buf.Frames() == 0 || bars <= 0 {
		return nil
	}
	frames := buf.Frames()
	ch := buf.Channels
	out := make([]float32, bars)
	for i := 0; i < bars; i++ {
		start := i * frames / bars
		end := (i + 1) * frames / bars
		if end <= start {
			end = min(frames, start+1)
		}
		var peak float64
		for f := start; f < end; f++ {
			for c := 0; c < ch; c++ {
				if a := math.Abs(buf.Samples[f*ch+c]); a > peak {
					peak = a
				}
			}
		}
		out[i] = float32(min(1.0, max(0.05, peak)))
	}
	return out
}

// --- Reference loading & style/audio checking ---

func loadRequestReferenceFromDir(root string, req *AssetGenerationRequest) {
	if root == "" || req == nil {
		return
	}
	for _, ref := range req.References {
		refPath, _, _ := strings.Cut(ref, "#")
		refPath = strings.TrimSpace(filepath.ToSlash(refPath))
		if refPath == "" || strings.HasSuffix(refPath, ".yaml") {
			continue
		}
		cleanRef, err := cleanAssetTargetPath(refPath)
		if err != nil {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(cleanRef))
		if err := verifyAssetGenReadPath(root, abs, cleanRef); err != nil {
			continue
		}
		limit := int64(maxGeneratedImageBytes)
		if req.Kind == AssetGenSound {
			limit = int64(maxGeneratedAudioBytes)
		}
		data, err := readBoundedAssetFile(abs, limit)
		if err != nil {
			continue
		}
		switch req.Kind {
		case AssetGenImages:
			if info, decErr := decodeUntrustedImage(data); decErr == nil {
				req.ReferenceAsset = cleanRef
				req.RefWidth = info.width
				req.RefHeight = info.height
				req.RefPixels = info.rgba
				req.RefImage = core.ImageSource{Pixels: info.rgba}
				return
			}
		case AssetGenSound:
			if info, decErr := decodeUntrustedAudio(data); decErr == nil {
				req.ReferenceAsset = cleanRef
				req.RefSoundName = path.Base(cleanRef)
				if info.loudnessMeasured && !math.IsNaN(info.rmsDBFS) && !math.IsInf(info.rmsDBFS, 0) {
					v := info.rmsDBFS
					req.RefLoudDB = &v
				}
				return
			}
		}
	}
}

func recheckRequestLocked(req *AssetGenerationRequest, brief AssetStyleBrief, bg AssetPreviewBackground) {
	for i := range req.Candidates {
		switch req.Kind {
		case AssetGenImages:
			evaluateImageCandidate(&req.Candidates[i], req, brief, bg)
		case AssetGenSound:
			evaluateSoundCandidate(&req.Candidates[i], req, brief)
		}
	}
}

func evaluateImageCandidate(cand *GeneratedCandidate, req *AssetGenerationRequest, brief AssetStyleBrief, bg AssetPreviewBackground) {
	var checks []AssetStyleCheck

	// 1. Palette size check
	if brief.MaxColors > 0 {
		allowed := brief.MaxColors + brief.OutlineColors
		pass := cand.ColorCount <= allowed
		shadingCount := cand.ColorCount
		if brief.OutlineColors > 0 && shadingCount > brief.OutlineColors {
			shadingCount -= brief.OutlineColors
		}
		summary := fmt.Sprintf("%d colours + outline", shadingCount)
		detail := fmt.Sprintf("%d colours + %d px outline", shadingCount, max(1, brief.OutlineColors))
		if !pass {
			summary = fmt.Sprintf("%d colours, brief allows %d", shadingCount, brief.MaxColors)
			detail = fmt.Sprintf("%d colours; the brief allows %d-colour shading", shadingCount, brief.MaxColors)
		}
		checks = append(checks, AssetStyleCheck{
			Pass:    pass,
			Title:   "Palette",
			Summary: summary,
			Detail:  detail,
		})
	}

	// 2. Pixel grid alignment check
	if brief.PixelGrid > 0 {
		gridPass := !cand.HasAntiAliasing
		scaleNote := ""
		if req.RefWidth > 0 && req.RefHeight > 0 && (cand.Width != req.RefWidth || cand.Height != req.RefHeight) {
			rx := float64(cand.Width) / float64(req.RefWidth)
			ry := float64(cand.Height) / float64(req.RefHeight)
			if math.Abs(rx-math.Round(rx)) > 0.01 || math.Abs(ry-math.Round(ry)) > 0.01 {
				gridPass = false
				scaleNote = fmt.Sprintf("%.1f×", rx)
			}
		}
		summary := fmt.Sprintf("On the %d px grid", brief.PixelGrid)
		detail := "every pixel on the grid, no anti-aliasing"
		if !gridPass {
			if scaleNote != "" {
				summary = fmt.Sprintf("Drawn at %s, off the grid", scaleNote)
				detail = fmt.Sprintf("scaled %s; pixels straddle the %d px grid", scaleNote, brief.PixelGrid)
			} else {
				summary = "Anti-aliased edges off grid"
				detail = fmt.Sprintf("semi-transparent fringe pixels straddle the %d px grid", brief.PixelGrid)
			}
		}
		checks = append(checks, AssetStyleCheck{
			Pass:    gridPass,
			Title:   "Pixel grid",
			Summary: summary,
			Detail:  detail,
		})
	}

	// 3. Size against reference asset check
	{
		sizePass := true
		refLabel := path.Base(req.ReferenceAsset)
		if req.RefWidth > 0 && req.RefHeight > 0 {
			sizePass = cand.Width == req.RefWidth && cand.Height == req.RefHeight
			summary := fmt.Sprintf("%d × %d, like %s", cand.Width, cand.Height, refLabel)
			detail := fmt.Sprintf("%d × %d, same as %s", cand.Width, cand.Height, refLabel)
			if !sizePass {
				summary = fmt.Sprintf("%d × %d, others are %d × %d", cand.Width, cand.Height, req.RefWidth, req.RefHeight)
				detail = fmt.Sprintf("%d × %d; reference %s is %d × %d", cand.Width, cand.Height, refLabel, req.RefWidth, req.RefHeight)
			}
			checks = append(checks, AssetStyleCheck{
				Pass:    sizePass,
				Title:   "Size",
				Summary: summary,
				Detail:  detail,
			})
		} else {
			checks = append(checks, AssetStyleCheck{
				Pass:    cand.Width > 0 && cand.Height > 0,
				Title:   "Size",
				Summary: fmt.Sprintf("%d × %d px", cand.Width, cand.Height),
				Detail:  fmt.Sprintf("%d × %d px (no reference image)", cand.Width, cand.Height),
			})
		}
	}

	// 4. Contrast against level background check
	ratio := computeImageContrast(cand.Pixels, bg.Color)
	cand.ContrastRatio = ratio
	minC := brief.MinContrast
	if minC <= 0 {
		minC = 3.0
	}
	contrastPass := ratio >= minC
	detail := fmt.Sprintf("%.1f : 1 against the wall", ratio)
	if !contrastPass {
		detail = fmt.Sprintf("%.1f : 1 against the wall (brief requires %.1f : 1)", ratio, minC)
	}
	checks = append(checks, AssetStyleCheck{
		Pass:    contrastPass,
		Title:   "Contrast on " + strings.ToLower(bg.Label),
		Summary: fmt.Sprintf("%.1f : 1 contrast", ratio),
		Detail:  detail,
	})

	cand.Checks = checks
}

func evaluateSoundCandidate(cand *GeneratedCandidate, req *AssetGenerationRequest, brief AssetStyleBrief) {
	if !cand.AudioLoudnessMeasured || math.IsNaN(cand.LoudnessDBFS) || math.IsNaN(cand.PeakDBFS) {
		cand.AudioCheckPass = true
		cand.AudioCheckText = "OGG loudness unmeasured"
		cand.Checks = []AssetStyleCheck{
			{
				Pass:    true,
				Title:   "Loudness",
				Summary: cand.AudioCheckText,
				Detail:  "OGG header verified · loudness check requires WAV",
			},
		}
		return
	}
	targetDB := brief.TargetLoudDB
	refLabel := "target"
	if req.RefLoudDB != nil && !math.IsInf(*req.RefLoudDB, 0) && !math.IsNaN(*req.RefLoudDB) {
		targetDB = *req.RefLoudDB
		refLabel = firstNonEmpty(req.RefSoundName, path.Base(req.ReferenceAsset), "reference")
	}
	tol := brief.LoudnessTolDB
	if tol <= 0 {
		tol = 3.0
	}
	maxPeak := brief.MaxPeakDB
	if maxPeak == 0 {
		maxPeak = -0.5
	}

	pass := true
	text := fmt.Sprintf("Peak %.1f dB", cand.PeakDBFS)
	if math.IsInf(cand.LoudnessDBFS, -1) {
		pass = false
		text = "Silent audio"
	} else if cand.PeakDBFS > maxPeak {
		pass = false
		text = fmt.Sprintf("Peak %.1f dB clips above %.1f dB", cand.PeakDBFS, maxPeak)
	} else if diff := cand.LoudnessDBFS - targetDB; diff > tol {
		pass = false
		text = fmt.Sprintf("%.0f dB louder than %s", math.Round(diff), refLabel)
	} else if diff := targetDB - cand.LoudnessDBFS; diff > tol {
		pass = false
		text = fmt.Sprintf("%.0f dB quieter than %s", math.Round(diff), refLabel)
	}

	cand.AudioCheckPass = pass
	cand.AudioCheckText = text
	cand.Checks = []AssetStyleCheck{
		{
			Pass:    pass,
			Title:   "Loudness",
			Summary: text,
			Detail:  fmt.Sprintf("%.1f dBFS RMS · %s", cand.LoudnessDBFS, text),
		},
	}
}

func computeImageContrast(rgba *image.RGBA, bg core.Color) float64 {
	if rgba == nil {
		return 1.0
	}
	b := rgba.Bounds()
	var sumR, sumG, sumB float64
	count := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := rgba.RGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			// Exclude very dark 1px outline pixels from body luminance average so outline doesn't skew foreground readability.
			if c.R < 36 && c.G < 36 && c.B < 44 {
				continue
			}
			sumR += float64(c.R) / 255.0
			sumG += float64(c.G) / 255.0
			sumB += float64(c.B) / 255.0
			count++
		}
	}
	if count == 0 {
		// Fall back to all opaque pixels if the sprite is entirely dark.
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := rgba.RGBAAt(x, y)
				if c.A < 128 {
					continue
				}
				sumR += float64(c.R) / 255.0
				sumG += float64(c.G) / 255.0
				sumB += float64(c.B) / 255.0
				count++
			}
		}
	}
	if count == 0 {
		return 1.0
	}
	fgLum := relativeLuminance(sumR/float64(count), sumG/float64(count), sumB/float64(count))
	bgLum := relativeLuminance(
		float64((bg>>24)&0xFF)/255.0,
		float64((bg>>16)&0xFF)/255.0,
		float64((bg>>8)&0xFF)/255.0,
	)
	hi := max(fgLum, bgLum)
	lo := min(fgLum, bgLum)
	return (hi + 0.05) / (lo + 0.05)
}

func relativeLuminance(r, g, b float64) float64 {
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}
