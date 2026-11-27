package liveops

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// PlatformShareHostOptions configures NewPlatformShareHost for mobile, desktop and web targets over NSGo's core APIs.
type PlatformShareHostOptions struct {
	// BaseURL is the invite URL prefix used to build shared invite links and match incoming links
	// (for example "mygame://invite" on mobile/desktop or "https://play.example.com/invite" on the web).
	// On core.OSWeb, when BaseURL is empty, NewPlatformShareHost derives it from cx.AppURL() by stripping its query and fragment.
	BaseURL string
	// Encoding chooses whether the invite payload is placed in the URL query ("?k=v", default) or fragment ("#k=v").
	Encoding InviteEncoding
	// AcceptFragment, when true with InviteEncodeQuery, also extracts payloads from the URL fragment when the query string is empty.
	AcceptFragment bool
	// MaxPayloadBytes overrides DefaultMaxInvitePayloadBytes (1024) when positive.
	MaxPayloadBytes int
	// FallbackToClipboard, when true, copies the formatted share text/link to the clipboard via cx.SetClipboard
	// if cx.Share reports core.CodeUnavailable (for example on desktop browsers without navigator.share).
	FallbackToClipboard bool
	// FormatText builds the share text from the card's title and formatted invite link.
	// When nil, non-empty title formats as "<title>\n<link>" and empty title formats as "<link>".
	FormatText func(title, link string) string
}

// PlatformShareHost implements ShareHost on mobile (iOS, Android), desktop (macOS, Windows, Linux) and the web (core.OSWeb)
// using NSGo's core.Scope.Share, core.Scope.OnOpenLinks, core.Scope.AppURL and core.Scope.OnAppURLChange.
type PlatformShareHost struct {
	cx                  *core.Scope
	baseURL             string
	linkOpts            InviteLinkOptions
	maxPayloadBytes     int
	fallbackToClipboard bool
	formatText          func(title, link string) string

	mu                sync.Mutex
	launchChecked     bool
	suppressLaunchURL string
}

// NewPlatformShareHost creates a ShareHost backed by cx's native/web share sheet and link-opening hooks.
func NewPlatformShareHost(cx *core.Scope, opts PlatformShareHostOptions) (*PlatformShareHost, error) {
	if cx == nil {
		return nil, errors.New("liveops: PlatformShareHost needs a core.Scope")
	}
	if opts.MaxPayloadBytes < 0 {
		return nil, fmt.Errorf("%w: negative MaxPayloadBytes %d", ErrInvalidInvitePayload, opts.MaxPayloadBytes)
	}
	if opts.Encoding != InviteEncodeQuery && opts.Encoding != InviteEncodeFragment {
		return nil, fmt.Errorf("%w: unknown invite encoding %d", ErrInvalidInviteURL, opts.Encoding)
	}

	baseURL := opts.BaseURL
	if baseURL != "" {
		u, err := parseInviteBaseURL(baseURL)
		if err != nil {
			return nil, err
		}
		if opts.Encoding == InviteEncodeQuery && u.RawQuery != "" {
			return nil, fmt.Errorf("%w: base URL %q must not contain a query string", ErrInvalidInviteURL, baseURL)
		}
		if opts.Encoding == InviteEncodeFragment && (u.Fragment != "" || u.RawFragment != "") {
			return nil, fmt.Errorf("%w: base URL %q must not contain a fragment", ErrInvalidInviteURL, baseURL)
		}
	} else if core.Untracked(cx, cx.Env().OS) == core.OSWeb {
		baseURL = deriveWebBaseURL(core.Untracked(cx, cx.AppURL))
	}

	return &PlatformShareHost{
		cx:      cx,
		baseURL: baseURL,
		linkOpts: InviteLinkOptions{
			BaseURL:        baseURL,
			Encoding:       opts.Encoding,
			AcceptFragment: opts.AcceptFragment,
		},
		maxPayloadBytes:     effectivePayloadLimit(opts.MaxPayloadBytes),
		fallbackToClipboard: opts.FallbackToClipboard,
		formatText:          opts.FormatText,
	}, nil
}

// MaxPayloadBytes returns the host's maximum encoded payload size in bytes.
func (h *PlatformShareHost) MaxPayloadBytes() int {
	if h == nil {
		return DefaultMaxInvitePayloadBytes
	}
	return h.maxPayloadBytes
}

// Share builds the invite link for card and presents it through core.Scope.Share (attaching card.Image when it carries image bytes, pixels or a local file path).
func (h *PlatformShareHost) Share(card PreparedCard, done func(shown bool, err error)) {
	finish := func(shown bool, err error) {
		if done != nil {
			done(shown, err)
		}
	}
	if h == nil || h.cx == nil {
		finish(false, ErrShareUnavailable)
		return
	}

	baseURL := h.effectiveBaseURL()
	if baseURL == "" {
		finish(false, fmt.Errorf("%w: PlatformShareHostOptions.BaseURL is required on this platform", ErrInvalidInviteURL))
		return
	}
	link, err := FormatInviteURL(baseURL, card.Query, h.linkOpts.Encoding)
	if err != nil {
		finish(false, err)
		return
	}

	var text string
	if h.formatText != nil {
		text = h.formatText(card.Title, link)
	} else if card.Title != "" {
		text = card.Title + "\n" + link
	} else {
		text = link
	}

	shareOpts := core.ShareOptions{
		Text:   text,
		Anchor: card.Anchor,
	}
	if file, ok, err := sharedFileFromImageSource(card.Image); err != nil {
		finish(false, err)
		return
	} else if ok {
		shareOpts.Files = []core.SharedFile{file}
	}

	h.cx.Share(shareOpts, func(shown bool, err error) {
		if err != nil && core.ErrorCode(err) == core.CodeUnavailable {
			if h.fallbackToClipboard {
				h.cx.SetClipboard(text)
				finish(true, nil)
				return
			}
			finish(false, fmt.Errorf("%w: %v", ErrShareUnavailable, err))
			return
		}
		finish(shown, err)
	})
}

// SetMenuCard is a no-op on native and web platforms, which have no host-managed Forward menu.
func (h *PlatformShareHost) SetMenuCard(func() (PreparedCard, error)) {}

// LaunchInvite inspects cx.AppURL() once for a cold-start invite link matching PlatformShareHostOptions.
func (h *PlatformShareHost) LaunchInvite() (LaunchInvite, bool) {
	if h == nil || h.cx == nil {
		return LaunchInvite{}, false
	}
	h.mu.Lock()
	if h.launchChecked {
		h.mu.Unlock()
		return LaunchInvite{}, false
	}
	h.launchChecked = true
	h.mu.Unlock()

	rawURL := core.Untracked(h.cx, h.cx.AppURL)
	if rawURL == "" {
		return LaunchInvite{}, false
	}
	q, matched, err := ExtractInviteQuery(rawURL, h.currentLinkOpts())
	if err != nil {
		// Surface malformed matching launch URLs to Invites so OnError sees them instead of silently dropping them.
		return LaunchInvite{RawQuery: rawURL, RawURL: rawURL, Source: InviteSourceLaunch}, true
	}
	if !matched {
		return LaunchInvite{}, false
	}

	h.mu.Lock()
	h.suppressLaunchURL = rawURL
	h.mu.Unlock()

	return LaunchInvite{
		RawQuery: q,
		RawURL:   rawURL,
		Source:   InviteSourceLaunch,
	}, true
}

// OnInvite subscribes to incoming deep links (core.Scope.OnOpenLinks) and web address/hash changes (core.Scope.OnAppURLChange).
func (h *PlatformShareHost) OnInvite(fn func(LaunchInvite)) (stop func()) {
	if h == nil || h.cx == nil || fn == nil {
		return func() {}
	}

	var (
		mu     sync.Mutex
		active = true
	)
	deliverURL := func(rawURL string) {
		mu.Lock()
		isActive := active
		mu.Unlock()
		if !isActive || rawURL == "" {
			return
		}

		// Suppress the single queued cold-start link that NSGo's OnOpenLinks flushes on the first Post tick
		// when LaunchInvite already read the same URL synchronously from cx.AppURL().
		h.mu.Lock()
		if h.suppressLaunchURL != "" {
			if rawURL == h.suppressLaunchURL {
				h.suppressLaunchURL = ""
				h.mu.Unlock()
				return
			}
			h.suppressLaunchURL = ""
		}
		h.mu.Unlock()

		q, matched, err := ExtractInviteQuery(rawURL, h.currentLinkOpts())
		if err != nil {
			fn(LaunchInvite{RawQuery: rawURL, RawURL: rawURL, Source: InviteSourceResume})
			return
		}
		if !matched {
			return
		}
		fn(LaunchInvite{
			RawQuery: q,
			RawURL:   rawURL,
			Source:   InviteSourceResume,
		})
	}

	h.cx.OnOpenLinks(func(links []string) {
		for _, link := range links {
			deliverURL(link)
		}
	})
	stopURL := h.cx.OnAppURLChange(func(ev core.AppURLChange) {
		deliverURL(ev.URL)
	})

	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			active = false
			mu.Unlock()
			if stopURL != nil {
				stopURL()
			}
		})
	}
}

func (h *PlatformShareHost) effectiveBaseURL() string {
	if h.baseURL != "" {
		return h.baseURL
	}
	if core.Untracked(h.cx, h.cx.Env().OS) == core.OSWeb {
		return deriveWebBaseURL(core.Untracked(h.cx, h.cx.AppURL))
	}
	return ""
}

func (h *PlatformShareHost) currentLinkOpts() InviteLinkOptions {
	opts := h.linkOpts
	if opts.BaseURL == "" {
		opts.BaseURL = h.effectiveBaseURL()
	}
	return opts
}

func deriveWebBaseURL(pageURL string) string {
	if pageURL == "" {
		return ""
	}
	u, err := url.Parse(pageURL)
	if err != nil || u.Scheme == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func sharedFileFromImageSource(src core.ImageSource) (core.SharedFile, bool, error) {
	if len(src.Data) > 0 {
		mime, ext := detectImageFormat(src.Data)
		return core.SharedFile{
			Data: bytes.Clone(src.Data),
			Name: "invite" + ext,
			MIME: mime,
		}, true, nil
	}
	if src.Pixels != nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, src.Pixels); err != nil {
			return core.SharedFile{}, false, fmt.Errorf("liveops: encode share card image: %w", err)
		}
		return core.SharedFile{
			Data: buf.Bytes(),
			Name: "invite.png",
			MIME: "image/png",
		}, true, nil
	}
	if src.URI != "" {
		if path, ok := localFilePathFromURI(src.URI); ok {
			return core.SharedFile{Path: path}, true, nil
		}
	}
	return core.SharedFile{}, false, nil
}

func localFilePathFromURI(uri string) (string, bool) {
	if strings.HasPrefix(uri, "file://") {
		u, err := url.Parse(uri)
		if err != nil || u.Path == "" {
			return "", false
		}
		return u.Path, true
	}
	// Remote/virtual schemes (http, https, asset, data, etc.) are not disk files for core.Scope.Share.
	if strings.Contains(uri, ":") {
		return "", false
	}
	return uri, true
}

func detectImageFormat(data []byte) (mime, ext string) {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png", ".png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg", ".jpg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp", ".webp"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif", ".gif"
	default:
		return "application/octet-stream", ".bin"
	}
}

// MiniGameShareCard is the share-card payload passed to a MiniGameShareDriver
// (matching WeChat mini game's wx.shareAppMessage and wx.onShareAppMessage fields).
type MiniGameShareCard struct {
	Title      string
	ImageURL   string
	ImageData  []byte
	ImageURLID string
	Query      string
}

// MiniGameLaunchOptions mirrors a mini-game runtime's launch and onShow payloads
// (such as WeChat's wx.getLaunchOptionsSync and wx.onShow).
type MiniGameLaunchOptions struct {
	// Query is the raw "key=value&..." string. When empty and QueryMap is non-empty, MiniGameShareHost encodes QueryMap canonically.
	Query string
	// QueryMap is the decoded key-value object supplied by runtimes like WeChat mini games.
	QueryMap map[string]string
	// Scene is the platform launch/resume scene code.
	Scene int
	// ShareTicket is the opaque group-share ticket delivered when opened from a group chat with WithShareTicket enabled.
	ShareTicket string
}

// MiniGameShareDriver is the platform runtime hook implemented by mini-game hosts
// (and by NSGo once nsgo-dev/nsgo#2672 adds mini-game share cards and launch options).
type MiniGameShareDriver interface {
	// ShareAppMessage opens the mini-game forward dialog with card.
	ShareAppMessage(card MiniGameShareCard, done func(shown bool, err error))
	// ShowShareMenu enables or disables the mini-game menu's Forward item (wx.showShareMenu / wx.hideShareMenu).
	ShowShareMenu(enabled bool, withShareTicket bool)
	// OnShareAppMessage registers the handler invoked when the player taps Forward in the mini-game menu. Passing nil clears it.
	OnShareAppMessage(fn func() MiniGameShareCard)
	// LaunchOptions returns the cold-start launch options (wx.getLaunchOptionsSync).
	LaunchOptions() (MiniGameLaunchOptions, bool)
	// OnShow subscribes fn to resume events (wx.onShow) and returns an unsubscribe function.
	OnShow(fn func(MiniGameLaunchOptions)) (stop func())
}

// MiniGameShareHostOptions configures NewMiniGameShareHost.
type MiniGameShareHostOptions struct {
	// WithShareTicket requests group-chat shareTicket delivery when enabling the share menu.
	WithShareTicket bool
	// MaxPayloadBytes overrides DefaultMaxInvitePayloadBytes (1024) when positive.
	MaxPayloadBytes int
	// FallbackMenuCard is returned to the mini-game menu if the game's SetMenuCard callback returns an invalid or oversized Card.
	FallbackMenuCard MiniGameShareCard
}

// MiniGameShareHost adapts a MiniGameShareDriver to ShareHost.
// Passing a nil driver yields a safe unavailable host (returning ErrShareUnavailable from Share) until NSGo ships mini-game share cards (nsgo-dev/nsgo#2672).
type MiniGameShareHost struct {
	driver          MiniGameShareDriver
	withShareTicket bool
	maxPayloadBytes int
	fallbackMenu    MiniGameShareCard

	mu            sync.Mutex
	launchChecked bool
}

// NewMiniGameShareHost wraps driver as a ShareHost.
// If driver is nil, Share fails with ErrShareUnavailable and LaunchInvite/OnInvite/SetMenuCard are safe no-ops.
func NewMiniGameShareHost(driver MiniGameShareDriver, opts MiniGameShareHostOptions) *MiniGameShareHost {
	return &MiniGameShareHost{
		driver:          driver,
		withShareTicket: opts.WithShareTicket,
		maxPayloadBytes: effectivePayloadLimit(opts.MaxPayloadBytes),
		fallbackMenu:    opts.FallbackMenuCard,
	}
}

// MaxPayloadBytes returns the host's maximum encoded payload size in bytes.
func (h *MiniGameShareHost) MaxPayloadBytes() int {
	if h == nil {
		return DefaultMaxInvitePayloadBytes
	}
	return h.maxPayloadBytes
}

// Share forwards card to MiniGameShareDriver.ShareAppMessage, or answers ErrShareUnavailable when driver is nil.
func (h *MiniGameShareHost) Share(card PreparedCard, done func(shown bool, err error)) {
	finish := func(shown bool, err error) {
		if done != nil {
			done(shown, err)
		}
	}
	if h == nil || h.driver == nil {
		finish(false, ErrShareUnavailable)
		return
	}
	mgCard, err := miniGameCardFromPrepared(card)
	if err != nil {
		finish(false, err)
		return
	}
	h.driver.ShareAppMessage(mgCard, finish)
}

// SetMenuCard enables the mini-game Forward menu (ShowShareMenu + OnShareAppMessage) when fn is non-nil, or disables it when fn is nil.
func (h *MiniGameShareHost) SetMenuCard(fn func() (PreparedCard, error)) {
	if h == nil || h.driver == nil {
		return
	}
	if fn == nil {
		h.driver.OnShareAppMessage(nil)
		h.driver.ShowShareMenu(false, false)
		return
	}
	h.driver.ShowShareMenu(true, h.withShareTicket)
	h.driver.OnShareAppMessage(func() MiniGameShareCard {
		prepared, err := fn()
		if err != nil {
			return h.fallbackMenu
		}
		mgCard, err := miniGameCardFromPrepared(prepared)
		if err != nil {
			return h.fallbackMenu
		}
		return mgCard
	})
}

// LaunchInvite reads the mini game's cold-start LaunchOptions once.
func (h *MiniGameShareHost) LaunchInvite() (LaunchInvite, bool) {
	if h == nil || h.driver == nil {
		return LaunchInvite{}, false
	}
	h.mu.Lock()
	if h.launchChecked {
		h.mu.Unlock()
		return LaunchInvite{}, false
	}
	h.launchChecked = true
	h.mu.Unlock()

	opts, ok := h.driver.LaunchOptions()
	if !ok {
		return LaunchInvite{}, false
	}
	return miniGameLaunchToInvite(opts, InviteSourceLaunch)
}

// OnInvite subscribes to MiniGameShareDriver.OnShow, ignoring ordinary foreground resumes that carry no query payload.
func (h *MiniGameShareHost) OnInvite(fn func(LaunchInvite)) (stop func()) {
	if h == nil || h.driver == nil || fn == nil {
		return func() {}
	}
	return h.driver.OnShow(func(opts MiniGameLaunchOptions) {
		if inv, ok := miniGameLaunchToInvite(opts, InviteSourceResume); ok {
			fn(inv)
		}
	})
}

func miniGameCardFromPrepared(card PreparedCard) (MiniGameShareCard, error) {
	out := MiniGameShareCard{
		Title:      card.Title,
		ImageURL:   card.Image.URI,
		ImageURLID: card.ImageID,
		Query:      card.Query,
	}
	if len(card.Image.Data) > 0 {
		out.ImageData = bytes.Clone(card.Image.Data)
	} else if card.Image.Pixels != nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, card.Image.Pixels); err != nil {
			return MiniGameShareCard{}, fmt.Errorf("liveops: encode mini-game share image: %w", err)
		}
		out.ImageData = buf.Bytes()
	}
	return out, nil
}

func miniGameLaunchToInvite(opts MiniGameLaunchOptions, src InviteSource) (LaunchInvite, bool) {
	rawQuery := opts.Query
	if rawQuery == "" && len(opts.QueryMap) > 0 {
		keys := make([]string, 0, len(opts.QueryMap))
		for k := range opts.QueryMap {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var b strings.Builder
		for i, k := range keys {
			if i > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(opts.QueryMap[k]))
		}
		rawQuery = b.String()
	}
	if rawQuery == "" {
		return LaunchInvite{}, false
	}
	return LaunchInvite{
		RawQuery:    rawQuery,
		Scene:       opts.Scene,
		ShareTicket: opts.ShareTicket,
		Source:      src,
	}, true
}
