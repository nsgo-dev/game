package editor

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/qrcode"
	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
)

var (
	// ErrNonLANAddress is returned when the web build server is asked to bind to or advertise a non-local/non-LAN IP.
	ErrNonLANAddress = errors.New("editor: web build server refuses non-LAN address")
	// ErrNoWebBuildDir is returned when no built web directory or archive can be located to serve.
	ErrNoWebBuildDir = errors.New("editor: no web build directory found to serve")
	// ErrWebBridgeSessionNotFound is returned when calling a tool on an unknown or disconnected phone session.
	ErrWebBridgeSessionNotFound = errors.New("editor: web MCP bridge session not found")
	// ErrNoAndroidDevice is returned when running an Android build on USB without a connected Android device.
	ErrNoAndroidDevice = errors.New("editor: no connected Android USB device")
	// ErrAndroidBuildNotReady is returned when attempting to run on an Android USB device before the Android build succeeds.
	ErrAndroidBuildNotReady = errors.New("editor: Android build has not finished yet")
)

// BuildDeviceState reports the live readiness or execution state of a RUN ON DEVICE target.
type BuildDeviceState string

const (
	BuildDeviceReady        BuildDeviceState = "ready"
	BuildDeviceServing      BuildDeviceState = "serving"
	BuildDeviceConnected    BuildDeviceState = "connected"
	BuildDeviceStopped      BuildDeviceState = "stopped"
	BuildDeviceInstalling   BuildDeviceState = "installing"
	BuildDeviceRunning      BuildDeviceState = "running"
	BuildDeviceDisconnected BuildDeviceState = "disconnected"
	BuildDeviceUnavailable  BuildDeviceState = "unavailable"
	BuildDeviceFailed       BuildDeviceState = "failed"
)

// ConnectedDevice describes one physical or emulated USB device reported by `adb` / DeviceDiscoverer.
type ConnectedDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	Transport string `json:"transport"`
	Online    bool   `json:"online"`
}

// DeviceCommandRequest describes an install-and-run command dispatched through `go tool nsgo`.
type DeviceCommandRequest struct {
	Dir          string          `json:"dir"`
	Platform     string          `json:"platform"`
	Device       ConnectedDevice `json:"device"`
	ArtifactPath string          `json:"artifactPath,omitempty"`
	Args         []string        `json:"args"`
}

// WebBridgeSession records one phone or browser Play-mode client connected back to the editor's MCP bridge.
type WebBridgeSession struct {
	SessionID    string    `json:"sessionId"`
	ClientID     string    `json:"clientId,omitempty"`
	Device       string    `json:"device"`
	RemoteIP     string    `json:"remoteIp"`
	PlayMode     bool      `json:"playMode"`
	Capabilities []string  `json:"capabilities,omitempty"`
	ConnectedAt  time.Time `json:"connectedAt"`
}

// WebBridgeInfo is returned by GET /__nsgo_mcp/info so a served web build in Play mode can connect back to the editor's MCP bridge.
type WebBridgeInfo struct {
	Protocol          string `json:"protocol"`
	Project           string `json:"project"`
	PlayMode          bool   `json:"playMode"`
	SessionEndpoint   string `json:"sessionEndpoint"`
	RPCEndpoint       string `json:"rpcEndpoint"`
	PollEndpoint      string `json:"pollEndpoint"`
	ReplyEndpoint     string `json:"replyEndpoint"`
	BridgeScript      string `json:"bridgeScript"`
	Token             string `json:"token"`
	ConnectedClients  int    `json:"connectedClients"`
	EditorBridgeReady bool   `json:"editorBridgeReady"`
}

// WebBuildServerSnapshot is a thread-safe value copy of the LAN web build server state.
type WebBuildServerSnapshot struct {
	Serving          bool               `json:"serving"`
	StoppedByUser    bool               `json:"stoppedByUser"`
	URL              string             `json:"url,omitempty"`
	ListenAddr       string             `json:"listenAddr,omitempty"`
	Host             string             `json:"host,omitempty"`
	Port             int                `json:"port,omitempty"`
	ServeDir         string             `json:"serveDir,omitempty"`
	BridgeInfoURL    string             `json:"bridgeInfoUrl,omitempty"`
	BridgeToken      string             `json:"bridgeToken,omitempty"`
	ConnectedClients []WebBridgeSession `json:"connectedClients,omitempty"`
	LastErr          string             `json:"lastErr,omitempty"`
}

type webBridgePendingCall struct {
	requestID string
	tool      string
	args      json.RawMessage
	replyCh   chan webBridgeCallResult
}

type webBridgeCallResult struct {
	result json.RawMessage
	err    string
}

type webBridgeClientState struct {
	info    WebBridgeSession
	queue   chan webBridgePendingCall
	pending map[string]chan webBridgeCallResult
}

type activeWebServer struct {
	listener  net.Listener
	server    *http.Server
	zipReader *zip.ReadCloser
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	url       string
	host      string
	port      int
	serveDir  string
	token     string

	mu      sync.Mutex
	clients map[string]*webBridgeClientState
	nextReq uint64
}

func (s *activeWebServer) markClosed() {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
}

func (s *activeWebServer) snapshotClients() []WebBridgeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.clients) == 0 {
		return nil
	}
	out := make([]WebBridgeSession, 0, len(s.clients))
	for _, cl := range s.clients {
		cp := cl.info
		cp.Capabilities = slices.Clone(cl.info.Capabilities)
		out = append(out, cp)
	}
	slices.SortFunc(out, func(a, b WebBridgeSession) int {
		if a.ConnectedAt.Before(b.ConnectedAt) {
			return -1
		}
		if a.ConnectedAt.After(b.ConnectedAt) {
			return 1
		}
		return strings.Compare(a.SessionID, b.SessionID)
	})
	return out
}

type androidDeviceRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

func (r *androidDeviceRun) finish() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

// EncodeBuildQR encodes rawURL as a deterministic ISO/IEC 18004 QR Code (EC level M)
// suitable for scanning on a phone camera on the local Wi-Fi.
func EncodeBuildQR(rawURL string) (*qrcode.Code, error) {
	return qrcode.Encode(strings.TrimSpace(rawURL), qrcode.Medium)
}

// isLocalOrLANIP reports whether ip is strictly a loopback, RFC 1918 / RFC 4193 private LAN,
// or link-local address. Publicly routable addresses always return false.
func isLocalOrLANIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// isPrivateLANIPv4 reports whether ip is an RFC 1918 IPv4 address (10/8, 172.16/12, 192.168/16).
func isPrivateLANIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(v4)
	if !ok {
		return false
	}
	return addr.IsPrivate()
}

// defaultResolveLANIP picks the host's best private IPv4 LAN address for local Wi-Fi phone access,
// falling back to 127.0.0.1 when no private LAN interface is active. It never returns a public IP.
func defaultResolveLANIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1", nil
	}
	var fallbackLinkLocal string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			v4 := ip.To4()
			if v4 == nil || v4.IsLoopback() {
				continue
			}
			if isPrivateLANIPv4(v4) {
				return v4.String(), nil
			}
			if fallbackLinkLocal == "" && v4.IsLinkLocalUnicast() {
				fallbackLinkLocal = v4.String()
			}
		}
	}
	if fallbackLinkLocal != "" {
		return fallbackLinkLocal, nil
	}
	return "127.0.0.1", nil
}

func randomBridgeToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("nsgo-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// WebBuildServerState returns a thread-safe snapshot of the LAN web build server and connected phone MCP sessions.
func (e *Editor) WebBuildServerState() WebBuildServerSnapshot {
	if e == nil {
		return WebBuildServerSnapshot{}
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.webServerSnapshotLocked()
}

func (e *Editor) webServerSnapshotLocked() WebBuildServerSnapshot {
	srv := e.buildRelease.webServer
	if srv == nil {
		return WebBuildServerSnapshot{
			Serving:       false,
			StoppedByUser: e.buildRelease.webServerStoppedByUser,
			ServeDir:      e.buildRelease.lastWebServeDir,
			LastErr:       e.buildRelease.webServerLastErr,
		}
	}
	return WebBuildServerSnapshot{
		Serving:          true,
		StoppedByUser:    false,
		URL:              srv.url,
		ListenAddr:       srv.listener.Addr().String(),
		Host:             srv.host,
		Port:             srv.port,
		ServeDir:         srv.serveDir,
		BridgeInfoURL:    srv.url + "__nsgo_mcp/info",
		BridgeToken:      srv.token,
		ConnectedClients: srv.snapshotClients(),
	}
}

// StartWebBuildServer starts serving servePath (a built web directory or .zip archive containing index.html)
// strictly on the local network (LAN IP or loopback, never public), exposing the LAN URL, QR code payload,
// and /__nsgo_mcp/* Play-mode debug bridge endpoints. If servePath is empty, the editor locates the latest
// web build output directory automatically.
func (e *Editor) StartWebBuildServer(servePath string) (WebBuildServerSnapshot, error) {
	return e.startWebBuildServerInternal(servePath, e.LANAddressResolver, e.WebBuildServePort, true)
}

func (e *Editor) startWebBuildServerInternal(
	servePath string,
	lanResolver func() (string, error),
	servePort int,
	notifyDirect bool,
) (WebBuildServerSnapshot, error) {
	if e == nil {
		return WebBuildServerSnapshot{}, ErrNoProject
	}
	resolvedPath, err := e.resolveWebServeTarget(servePath)
	if err != nil {
		e.buildRelease.mu.Lock()
		e.buildRelease.webServerLastErr = err.Error()
		e.buildRelease.rev++
		e.buildRelease.mu.Unlock()
		if notifyDirect {
			e.notify()
		}
		return WebBuildServerSnapshot{}, err
	}

	// Stop any previous server first so the old port is freed cleanly.
	_ = e.stopWebBuildServerInternal(false)

	// Resolve LAN bind IP and strictly enforce local/LAN-only binding.
	host := ""
	if lanResolver != nil {
		host, err = lanResolver()
		if err != nil {
			return WebBuildServerSnapshot{}, err
		}
	} else {
		host, err = defaultResolveLANIP()
		if err != nil {
			return WebBuildServerSnapshot{}, err
		}
	}
	host = strings.TrimSpace(host)
	parsedIP := net.ParseIP(host)
	if parsedIP == nil || !isLocalOrLANIP(parsedIP) {
		return WebBuildServerSnapshot{}, fmt.Errorf("%w: %q", ErrNonLANAddress, host)
	}

	var fileFS fs.FS
	var zipCloser *zip.ReadCloser
	info, statErr := os.Stat(resolvedPath)
	if statErr != nil {
		return WebBuildServerSnapshot{}, statErr
	}
	if info.IsDir() {
		fileFS = safeDirFS{root: resolvedPath}
	} else if strings.EqualFold(filepath.Ext(resolvedPath), ".zip") {
		zr, zerr := zip.OpenReader(resolvedPath)
		if zerr != nil {
			return WebBuildServerSnapshot{}, fmt.Errorf("editor: open web build archive %s: %w", resolvedPath, zerr)
		}
		zipCloser = zr
		fileFS = &zr.Reader
	} else {
		return WebBuildServerSnapshot{}, fmt.Errorf("editor: %q is not a web build directory or zip archive", resolvedPath)
	}

	port := servePort
	if port < 0 || port > 65535 {
		if zipCloser != nil {
			_ = zipCloser.Close()
		}
		return WebBuildServerSnapshot{}, fmt.Errorf("editor: invalid web server port %d", port)
	}
	bindAddr := net.JoinHostPort(parsedIP.String(), strconv.Itoa(port))
	ln, err := net.Listen("tcp4", bindAddr)
	if err != nil {
		if zipCloser != nil {
			_ = zipCloser.Close()
		}
		return WebBuildServerSnapshot{}, fmt.Errorf("editor: listen on LAN %s: %w", bindAddr, err)
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port
	serveURL := fmt.Sprintf("http://%s/", net.JoinHostPort(parsedIP.String(), strconv.Itoa(actualPort)))
	token := randomBridgeToken()

	projectName := ""
	if e.Project != nil {
		projectName = e.Project.Name
	}

	active := &activeWebServer{
		listener:  ln,
		zipReader: zipCloser,
		done:      make(chan struct{}),
		closed:    make(chan struct{}),
		url:       serveURL,
		host:      parsedIP.String(),
		port:      actualPort,
		serveDir:  resolvedPath,
		token:     token,
		clients:   make(map[string]*webBridgeClientState),
	}

	handler := e.newWebBuildHTTPHandler(active, fileFS, projectName)
	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	active.server = httpSrv

	e.buildRelease.mu.Lock()
	e.buildRelease.webServer = active
	e.buildRelease.webServerStoppedByUser = false
	e.buildRelease.webServerLastErr = ""
	e.buildRelease.lastWebServeDir = resolvedPath
	e.buildRelease.browserOpened = false
	if idx := e.findBuildPresetLocked("web"); idx >= 0 && e.buildRelease.presets[idx].Result != nil {
		e.buildRelease.presets[idx].Result.TestURL = serveURL
		e.buildRelease.presets[idx].Result.WebBuildDir = resolvedPath
	}
	e.buildRelease.rev++
	snap := e.webServerSnapshotLocked()
	e.buildRelease.mu.Unlock()

	go func() {
		defer close(active.done)
		defer active.markClosed()
		_ = httpSrv.Serve(ln)
	}()

	if notifyDirect {
		e.notify()
	}
	return snap, nil
}

// StopWebBuildServer closes the LAN web build HTTP listener and all active connections,
// immediately freeing the TCP port before returning.
func (e *Editor) StopWebBuildServer() error {
	return e.stopWebBuildServerInternal(true)
}

func (e *Editor) stopWebBuildServerInternal(notifyDirect bool) error {
	if e == nil {
		return nil
	}
	e.buildRelease.mu.Lock()
	srv := e.buildRelease.webServer
	e.buildRelease.webServer = nil
	if srv != nil {
		e.buildRelease.webServerStoppedByUser = true
		e.buildRelease.browserOpened = false
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()

	if srv == nil {
		return nil
	}
	srv.markClosed()
	srv.mu.Lock()
	for _, cl := range srv.clients {
		for id, ch := range cl.pending {
			select {
			case ch <- webBridgeCallResult{err: "server stopped"}:
			default:
			}
			delete(cl.pending, id)
		}
	}
	srv.mu.Unlock()

	errClose := srv.server.Close()
	_ = srv.listener.Close()
	<-srv.done
	if srv.zipReader != nil {
		_ = srv.zipReader.Close()
	}

	if notifyDirect {
		e.notify()
	}
	return errClose
}

func (e *Editor) resolveWebServeTarget(explicit string) (string, error) {
	if s := strings.TrimSpace(explicit); s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", err
		}
		return abs, nil
	}

	e.buildRelease.mu.Lock()
	lastDir := e.buildRelease.lastWebServeDir
	var webResult *BuildArtifactResult
	if idx := e.findBuildPresetLocked("web"); idx >= 0 && e.buildRelease.presets[idx].Result != nil {
		cp := *e.buildRelease.presets[idx].Result
		webResult = &cp
	}
	e.buildRelease.mu.Unlock()

	var candidates []string
	if webResult != nil {
		if webResult.WebBuildDir != "" {
			candidates = append(candidates, webResult.WebBuildDir)
		}
		if webResult.ArtifactPath != "" {
			candidates = append(candidates, webResult.ArtifactPath)
			candidates = append(candidates, filepath.Join(filepath.Dir(webResult.ArtifactPath), "web"))
		}
	}
	if lastDir != "" {
		candidates = append(candidates, lastDir)
	}
	if e.ProjectPath != "" {
		projDir := filepath.Dir(e.ProjectPath)
		if e.Project != nil && e.Project.Export != nil && e.Project.Export.Output != "" {
			outDir := filepath.Join(projDir, filepath.FromSlash(e.Project.Export.Output))
			candidates = append(candidates, filepath.Join(outDir, "web"), outDir)
		}
		candidates = append(candidates,
			filepath.Join(projDir, "dist", "web"),
			filepath.Join(projDir, "build", "web"),
		)
	}

	for _, c := range candidates {
		if c == "" {
			continue
		}
		info, err := os.Stat(c)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if _, idxErr := os.Stat(filepath.Join(c, "index.html")); idxErr == nil {
				return c, nil
			}
		} else if strings.EqualFold(filepath.Ext(c), ".zip") {
			return c, nil
		}
	}
	return "", ErrNoWebBuildDir
}

const (
	maxWebAssetBytes         = 256 << 20 // 256 MiB per streamed zip asset
	maxWebBridgeSessions     = 16
	maxWebBridgeFieldLen     = 128
	maxWebBridgeCapabilities = 32
	maxWebBridgeCapLen       = 64
	maxAndroidRunLogBytes    = 64 * 1024
	androidDeviceRunTimeout  = 10 * time.Minute
)

// lockedCappedBuffer is a concurrency-safe byte buffer with a hard byte cap,
// used to capture combined stdout+stderr from device subprocesses without data races or OOM.
type lockedCappedBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (b *lockedCappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rem := b.limit - len(b.buf); rem > 0 {
		if len(p) > rem {
			b.buf = append(b.buf, p[:rem]...)
		} else {
			b.buf = append(b.buf, p...)
		}
	}
	return len(p), nil
}

// isBlockedWebAssetPath reports whether a clean slash-separated relative asset path
// attempts to read dot-prefixed entries (.git, .env, .game, .DS_Store) or sensitive
// project manifest files.
func isBlockedWebAssetPath(rel string) bool {
	if rel == "" {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ".") {
			return true
		}
		switch strings.ToLower(seg) {
		case "game.yaml", "game.yml", "nsgo.yaml", "nsgo.yml", "go.mod", "go.sum", "go.work", "go.work.sum":
			return true
		}
	}
	return false
}

// safeDirFS serves files strictly beneath root, refusing symlink traversal outside root
// as well as dotfiles and sensitive project manifest files.
type safeDirFS struct {
	root string
}

func (s safeDirFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if isBlockedWebAssetPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	f, err := os.OpenInRoot(s.root, filepath.FromSlash(name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return nil, err
		}
		if strings.Contains(err.Error(), "escapes from parent") {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		return nil, err
	}
	return f, nil
}

func (e *Editor) newWebBuildHTTPHandler(srv *activeWebServer, files fs.FS, projectName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Enforce local network only on every incoming connection.
		if !isRequestFromLAN(r.RemoteAddr) {
			http.Error(w, "forbidden: local network only", http.StatusForbidden)
			return
		}
		// Reject mismatched Host headers to defeat DNS rebinding attacks.
		if !isAllowedLANHostHeader(r.Host, srv) {
			http.Error(w, "forbidden: invalid host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		if strings.HasPrefix(r.URL.Path, "/__nsgo_mcp/") {
			if (r.Method == http.MethodPost || r.Method == http.MethodDelete) && !isAllowedBridgeOrigin(r.Header.Get("Origin"), srv) {
				http.Error(w, "forbidden: cross-origin bridge request", http.StatusForbidden)
				return
			}
		}

		switch {
		case r.URL.Path == "/__nsgo_mcp/info":
			e.handleWebBridgeInfo(srv, projectName, w, r)
			return
		case r.URL.Path == "/__nsgo_mcp/bridge.js":
			handleWebBridgeScript(w, r)
			return
		case r.URL.Path == "/__nsgo_mcp/session":
			e.handleWebBridgeSession(srv, w, r)
			return
		case r.URL.Path == "/__nsgo_mcp/poll":
			handleWebBridgePoll(srv, w, r)
			return
		case r.URL.Path == "/__nsgo_mcp/reply":
			handleWebBridgeReply(srv, w, r)
			return
		case r.URL.Path == "/__nsgo_mcp/rpc":
			e.handleWebBridgeRPC(srv, projectName, w, r)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		serveWebBuildAsset(files, w, r)
	})
}

func isAllowedLANHost(host string, srv *activeWebServer) bool {
	if srv == nil {
		return false
	}
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if strings.EqualFold(h, srv.host) {
		return true
	}
	boundIP := net.ParseIP(srv.host)
	if boundIP != nil && boundIP.IsLoopback() {
		if strings.EqualFold(h, "localhost") || h == "127.0.0.1" || h == "::1" {
			return true
		}
	}
	return false
}

func isAllowedLANHostHeader(hostHeader string, srv *activeWebServer) bool {
	h := strings.TrimSpace(hostHeader)
	if h == "" {
		return true
	}
	if host, portStr, err := net.SplitHostPort(h); err == nil {
		if srv != nil && srv.port > 0 && portStr != strconv.Itoa(srv.port) {
			return false
		}
		return isAllowedLANHost(host, srv)
	}
	h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	return isAllowedLANHost(h, srv)
}

func isAllowedBridgeOrigin(originHeader string, srv *activeWebServer) bool {
	origin := strings.TrimSpace(originHeader)
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return isAllowedLANHostHeader(u.Host, srv)
}

func isRequestFromLAN(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	// Strip IPv6 zone if present.
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	return isLocalOrLANIP(ip)
}

func serveWebBuildAsset(files fs.FS, w http.ResponseWriter, r *http.Request) {
	cleaned := path.Clean("/" + r.URL.Path)
	rel := strings.TrimPrefix(cleaned, "/")
	if rel == "" || strings.HasSuffix(r.URL.Path, "/") {
		if rel == "" {
			rel = "index.html"
		} else {
			rel = path.Join(rel, "index.html")
		}
	}
	if isBlockedWebAssetPath(rel) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := files.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, fs.ErrPermission) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to open asset", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "failed to stat asset", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		subRel := path.Join(rel, "index.html")
		if isBlockedWebAssetPath(subRel) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		sub, serr := files.Open(subRel)
		if serr != nil {
			http.NotFound(w, r)
			return
		}
		defer sub.Close()
		subInfo, serr := sub.Stat()
		if serr != nil || subInfo.IsDir() {
			http.NotFound(w, r)
			return
		}
		f = sub
		info = subInfo
		rel = subRel
	}

	if ct := webAssetContentType(rel); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if rel == "index.html" || strings.HasSuffix(rel, "/index.html") {
		w.Header().Set("X-NSGo-MCP-Bridge", "/__nsgo_mcp/info")
		w.Header().Set("X-NSGo-Play-Mode", "debug")
	}

	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(rel), info.ModTime(), rs)
		return
	}
	if size := info.Size(); size > maxWebAssetBytes {
		http.Error(w, "asset too large", http.StatusRequestEntityTooLarge)
		return
	} else if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(f, maxWebAssetBytes))
	}
}

func webAssetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".yaml", ".yml":
		return "application/yaml; charset=utf-8"
	default:
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			return ct
		}
		return "application/octet-stream"
	}
}

func validBridgeToken(expected, got string) bool {
	if expected == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

func extractRequestToken(r *http.Request, bodyToken string) string {
	if bodyToken != "" {
		return bodyToken
	}
	if h := strings.TrimSpace(r.Header.Get("X-NSGo-MCP-Token")); h != "" {
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (e *Editor) handleWebBridgeInfo(srv *activeWebServer, projectName string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clients := srv.snapshotClients()
	info := WebBridgeInfo{
		Protocol:          "nsgo-web-mcp-v1",
		Project:           projectName,
		PlayMode:          true,
		SessionEndpoint:   "/__nsgo_mcp/session",
		RPCEndpoint:       "/__nsgo_mcp/rpc",
		PollEndpoint:      "/__nsgo_mcp/poll",
		ReplyEndpoint:     "/__nsgo_mcp/reply",
		BridgeScript:      "/__nsgo_mcp/bridge.js",
		Token:             srv.token,
		ConnectedClients:  len(clients),
		EditorBridgeReady: true,
	}
	writeJSONResponse(w, http.StatusOK, info)
}

func handleWebBridgeScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, webBridgeBootstrapJS)
}

func (e *Editor) handleWebBridgeSession(srv *activeWebServer, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Token        string   `json:"token"`
			ClientID     string   `json:"clientId"`
			Device       string   `json:"device"`
			PlayMode     bool     `json:"playMode"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		if !validBridgeToken(srv.token, extractRequestToken(r, req.Token)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		device := strings.TrimSpace(req.Device)
		if len(device) > maxWebBridgeFieldLen {
			device = device[:maxWebBridgeFieldLen]
		}
		if device == "" {
			device = "Phone browser"
		}
		remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			remoteHost = r.RemoteAddr
		}
		sessionID := strings.TrimSpace(req.ClientID)
		if len(sessionID) > maxWebBridgeFieldLen {
			sessionID = sessionID[:maxWebBridgeFieldLen]
		}
		if sessionID == "" {
			sessionID = "sess-" + randomBridgeToken()[:12]
		}
		var caps []string
		if len(req.Capabilities) > 0 {
			n := min(len(req.Capabilities), maxWebBridgeCapabilities)
			caps = make([]string, 0, n)
			for _, c := range req.Capabilities[:n] {
				c = strings.TrimSpace(c)
				if len(c) > maxWebBridgeCapLen {
					c = c[:maxWebBridgeCapLen]
				}
				if c != "" {
					caps = append(caps, c)
				}
			}
		}
		now := time.Now().UTC()
		sess := WebBridgeSession{
			SessionID:    sessionID,
			ClientID:     sessionID,
			Device:       device,
			RemoteIP:     remoteHost,
			PlayMode:     req.PlayMode,
			Capabilities: caps,
			ConnectedAt:  now,
		}
		srv.mu.Lock()
		if existing, ok := srv.clients[sessionID]; ok {
			existing.info = sess
		} else if len(srv.clients) >= maxWebBridgeSessions {
			srv.mu.Unlock()
			http.Error(w, "too many active sessions", http.StatusTooManyRequests)
			return
		} else {
			srv.clients[sessionID] = &webBridgeClientState{
				info:    sess,
				queue:   make(chan webBridgePendingCall, 16),
				pending: make(map[string]chan webBridgeCallResult),
			}
		}
		srv.mu.Unlock()

		e.bumpBuildReleaseRevAndNotifyUI()
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"ok":            true,
			"sessionId":     sessionID,
			"bridge":        "nsgo-game-editor",
			"playMode":      sess.PlayMode,
			"pollEndpoint":  "/__nsgo_mcp/poll",
			"replyEndpoint": "/__nsgo_mcp/reply",
			"rpcEndpoint":   "/__nsgo_mcp/rpc",
		})

	case http.MethodDelete:
		if !validBridgeToken(srv.token, extractRequestToken(r, "")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sessionID := strings.TrimSpace(r.URL.Query().Get("id"))
		srv.mu.Lock()
		if cl, ok := srv.clients[sessionID]; ok {
			for id, ch := range cl.pending {
				select {
				case ch <- webBridgeCallResult{err: "client disconnected"}:
				default:
				}
				delete(cl.pending, id)
			}
			delete(srv.clients, sessionID)
		}
		srv.mu.Unlock()

		e.bumpBuildReleaseRevAndNotifyUI()
		writeJSONResponse(w, http.StatusOK, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleWebBridgePoll(srv *activeWebServer, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validBridgeToken(srv.token, extractRequestToken(r, "")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("id"))
	srv.mu.Lock()
	cl, ok := srv.clients[sessionID]
	srv.mu.Unlock()
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()

	select {
	case call := <-cl.queue:
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"requestId": call.requestID,
			"tool":      call.tool,
			"arguments": call.args,
		})
	case <-r.Context().Done():
		return
	case <-srv.closed:
		http.Error(w, "server closed", http.StatusServiceUnavailable)
	case <-timer.C:
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleWebBridgeReply(srv *activeWebServer, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token     string          `json:"token"`
		SessionID string          `json:"sessionId"`
		RequestID string          `json:"requestId"`
		Result    json.RawMessage `json:"result"`
		Error     string          `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	if !validBridgeToken(srv.token, extractRequestToken(r, req.Token)) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	srv.mu.Lock()
	cl, ok := srv.clients[req.SessionID]
	var ch chan webBridgeCallResult
	if ok {
		ch = cl.pending[req.RequestID]
		delete(cl.pending, req.RequestID)
	}
	srv.mu.Unlock()
	if !ok || ch == nil {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}
	ch <- webBridgeCallResult{result: req.Result, err: req.Error}
	writeJSONResponse(w, http.StatusOK, map[string]any{"ok": true})
}

func (e *Editor) handleWebBridgeRPC(srv *activeWebServer, projectName string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Token   string          `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON-RPC payload", http.StatusBadRequest)
		return
	}
	if !validBridgeToken(srv.token, extractRequestToken(r, req.Token)) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	replyErr := func(code int, msg string) {
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]any{"code": code, "message": msg},
		})
	}
	replyOK := func(result any) {
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}

	if e.WebBridgeHandler != nil {
		val, err := e.WebBridgeHandler(r.Context(), req.Method, req.Params)
		if err != nil {
			replyErr(-32000, err.Error())
			return
		}
		replyOK(val)
		return
	}

	switch req.Method {
	case "ping":
		replyOK(map[string]any{"pong": true, "bridge": "nsgo-game-editor", "project": projectName})
	case "editor/status":
		replyOK(map[string]any{
			"project":          projectName,
			"serving":          true,
			"connectedClients": len(srv.snapshotClients()),
			"playModeBridge":   true,
		})
	case "tools/list":
		replyOK(map[string]any{
			"tools": []map[string]string{
				{"name": "game_scenes_list", "description": "List scenes running in the phone web build"},
				{"name": "game_entities_list", "description": "List live entities in the phone web build"},
				{"name": "game_entity_inspect", "description": "Inspect one live entity in the phone web build"},
				{"name": "game_console_exec", "description": "Execute a debug console command in the phone web build"},
				{"name": "game_screenshot", "description": "Capture a screenshot from the phone web build"},
			},
		})
	default:
		replyErr(-32601, fmt.Sprintf("method %q not found", req.Method))
	}
}

// CallWebBridgeTool dispatches an MCP inspection/debug tool call to a connected web Play-mode client
// (such as a phone that scanned the QR code) and waits for its JSON reply.
func (e *Editor) CallWebBridgeTool(ctx context.Context, sessionID, toolName string, args any) (json.RawMessage, error) {
	if e == nil {
		return nil, ErrNoProject
	}
	e.buildRelease.mu.Lock()
	srv := e.buildRelease.webServer
	e.buildRelease.mu.Unlock()
	if srv == nil {
		return nil, ErrWebBridgeSessionNotFound
	}
	rawArgs := json.RawMessage(`{}`)
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		rawArgs = b
	}

	srv.mu.Lock()
	cl, ok := srv.clients[sessionID]
	if !ok {
		srv.mu.Unlock()
		return nil, fmt.Errorf("%w: %q", ErrWebBridgeSessionNotFound, sessionID)
	}
	srv.nextReq++
	reqID := fmt.Sprintf("req-%d", srv.nextReq)
	replyCh := make(chan webBridgeCallResult, 1)
	cl.pending[reqID] = replyCh
	srv.mu.Unlock()

	call := webBridgePendingCall{
		requestID: reqID,
		tool:      toolName,
		args:      rawArgs,
		replyCh:   replyCh,
	}

	select {
	case cl.queue <- call:
	case <-ctx.Done():
		srv.mu.Lock()
		delete(cl.pending, reqID)
		srv.mu.Unlock()
		return nil, ctx.Err()
	case <-srv.closed:
		return nil, errors.New("editor: web build server stopped")
	}

	select {
	case res := <-replyCh:
		if res.err != "" {
			return nil, errors.New(res.err)
		}
		return res.result, nil
	case <-ctx.Done():
		srv.mu.Lock()
		delete(cl.pending, reqID)
		srv.mu.Unlock()
		return nil, ctx.Err()
	case <-srv.closed:
		return nil, errors.New("editor: web build server stopped")
	}
}

// WebBridgeSessions returns all phone/browser Play-mode sessions currently connected to the LAN MCP bridge.
func (e *Editor) WebBridgeSessions() []WebBridgeSession {
	return e.WebBuildServerState().ConnectedClients
}

func (e *Editor) bumpBuildReleaseRevAndNotifyUI() {
	e.buildRelease.mu.Lock()
	e.buildRelease.rev++
	scope := e.buildRelease.scope
	e.buildRelease.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
	}
}

func writeJSONResponse(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "json encode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// CopyWebBuildURL copies the active LAN web build URL (or web preset TestURL) to the clipboard.
func (e *Editor) CopyWebBuildURL() (string, error) {
	if e == nil {
		return "", ErrNoProject
	}
	urlStr := e.activeWebTestURL()
	if urlStr == "" {
		return "", ErrNoWebBuildDir
	}
	switch {
	case e.WriteClipboard != nil:
		e.WriteClipboard(urlStr)
	case e.scope != nil:
		e.scope.SetClipboard(urlStr)
	}
	e.Status = "Copied " + urlStr
	e.notify()
	return urlStr, nil
}

func (e *Editor) activeWebTestURL() string {
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	if e.buildRelease.webServer != nil {
		return e.buildRelease.webServer.url
	}
	for _, p := range e.buildRelease.presets {
		if p.Platform == "web" && p.Result != nil && p.Result.TestURL != "" {
			return p.Result.TestURL
		}
	}
	return ""
}

// RefreshConnectedDevices queries connected USB devices via Editor.DeviceDiscoverer (or `adb devices -l` on desktop)
// and updates the RUN ON DEVICE Android USB state.
func (e *Editor) RefreshConnectedDevices() ([]ConnectedDevice, error) {
	if e == nil {
		return nil, ErrNoProject
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	discover := e.DeviceDiscoverer
	if discover == nil {
		discover = defaultDiscoverConnectedDevices
	}
	devs, err := discover(context.Background(), dir)
	if err != nil {
		return nil, err
	}

	e.buildRelease.mu.Lock()
	e.buildRelease.connectedDevices = slices.Clone(devs)
	hasOnlineAndroid := false
	for _, d := range devs {
		if d.Platform == "android" && d.Online {
			hasOnlineAndroid = true
			break
		}
	}
	if !hasOnlineAndroid {
		if e.buildRelease.androidRun == nil {
			e.buildRelease.androidDeviceState = BuildDeviceDisconnected
			e.buildRelease.androidDeviceErr = ""
		}
	} else if e.buildRelease.androidDeviceState == BuildDeviceDisconnected {
		e.buildRelease.androidDeviceState = BuildDeviceReady
		e.buildRelease.androidDeviceErr = ""
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return slices.Clone(devs), nil
}

// LaunchBuildOnDevice runs the selected RUN ON DEVICE target:
//   - "browser": opens the served web build URL in this Mac/computer's browser via OpenBrowserURL.
//   - "phone-wifi": selects the phone QR target and starts serving the web build if stopped.
//   - "android-usb": installs and runs the finished Android build on the connected USB Android device via `go tool nsgo`.
func (e *Editor) LaunchBuildOnDevice(deviceID string) error {
	if e == nil {
		return ErrNoProject
	}
	switch deviceID {
	case "browser":
		urlStr := e.activeWebTestURL()
		if urlStr == "" {
			if snap, err := e.StartWebBuildServer(""); err == nil {
				urlStr = snap.URL
			} else {
				return err
			}
		}
		opener := e.OpenBrowserURL
		if opener == nil {
			opener = defaultOpenBrowserURL
		}
		if err := opener(urlStr); err != nil {
			return err
		}
		e.buildRelease.mu.Lock()
		e.buildRelease.selectedDeviceID = "browser"
		e.buildRelease.browserOpened = true
		e.buildRelease.rev++
		e.buildRelease.mu.Unlock()
		e.Status = "Opened " + urlStr + " in browser"
		e.notify()
		return nil

	case "phone-wifi":
		e.SelectBuildDevice("phone-wifi")
		if !e.WebBuildServerState().Serving {
			if _, err := e.StartWebBuildServer(""); err != nil && !errors.Is(err, ErrNoWebBuildDir) {
				return err
			}
		}
		return nil

	case "android-usb":
		e.SelectBuildDevice("android-usb")
		return e.RunBuildOnAndroidDevice()

	default:
		return fmt.Errorf("editor: unknown device target %q", deviceID)
	}
}

// RunBuildOnAndroidDevice installs and launches the finished Android build onto the connected USB Android device
// using `go -C <dir> tool nsgo build -target <android-target> -run -device <serial>` (or Editor.DeviceCommandRunner).
func (e *Editor) RunBuildOnAndroidDevice() error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	dir := filepath.Dir(e.ProjectPath)

	e.buildRelease.mu.Lock()
	if e.buildRelease.androidRun != nil {
		e.buildRelease.mu.Unlock()
		return nil
	}
	var androidPreset *BuildPresetEntry
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == "android" {
			cp := cloneBuildPresetEntry(e.buildRelease.presets[i])
			androidPreset = &cp
			break
		}
	}
	devs := slices.Clone(e.buildRelease.connectedDevices)
	e.buildRelease.mu.Unlock()

	if androidPreset == nil || androidPreset.Status != BuildPresetDone {
		return ErrAndroidBuildNotReady
	}
	var dev *ConnectedDevice
	for i := range devs {
		if devs[i].Platform == "android" && devs[i].Online {
			d := devs[i]
			dev = &d
			break
		}
	}
	if dev == nil {
		refreshed, err := e.RefreshConnectedDevices()
		if err != nil {
			return err
		}
		for i := range refreshed {
			if refreshed[i].Platform == "android" && refreshed[i].Online {
				d := refreshed[i]
				dev = &d
				break
			}
		}
	}
	if dev == nil {
		e.buildRelease.mu.Lock()
		e.buildRelease.androidDeviceState = BuildDeviceDisconnected
		e.buildRelease.rev++
		e.buildRelease.mu.Unlock()
		e.notify()
		return ErrNoAndroidDevice
	}

	target := "android_arm64"
	if len(androidPreset.Targets) > 0 {
		target = androidPreset.Targets[0]
	}
	if !validBuildTargetName(target) {
		return fmt.Errorf("editor: invalid Android build target %q", target)
	}
	if dev.ID != "" && !validDeviceSerial(dev.ID) {
		return fmt.Errorf("editor: invalid Android device serial %q", dev.ID)
	}
	args := []string{"-C", dir, "tool", "nsgo", "build", "-target", target, "-run"}
	if dev.ID != "" {
		args = append(args, "-device", dev.ID)
	}
	if e.Project.Export != nil && e.Project.Export.Output != "" {
		args = append(args, "-output", filepath.FromSlash(e.Project.Export.Output))
	}
	artifactPath := ""
	if androidPreset.Result != nil {
		artifactPath = androidPreset.Result.ArtifactPath
	}
	req := DeviceCommandRequest{
		Dir:          dir,
		Platform:     "android",
		Device:       *dev,
		ArtifactPath: artifactPath,
		Args:         args,
	}

	ctx, cancel := context.WithTimeout(context.Background(), androidDeviceRunTimeout)
	run := &androidDeviceRun{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	e.buildRelease.mu.Lock()
	e.buildRelease.androidRun = run
	e.buildRelease.androidDeviceState = BuildDeviceInstalling
	e.buildRelease.androidDeviceErr = ""
	e.buildRelease.selectedDeviceID = "android-usb"
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.Status = fmt.Sprintf("Installing on %s…", dev.Name)
	e.notify()

	runner := e.DeviceCommandRunner
	if runner == nil {
		runner = func(ctx context.Context, req DeviceCommandRequest, stdout, stderr io.Writer) error {
			return defaultDeviceCommandRunner(ctx, req, stdout, stderr)
		}
	}

	go func() {
		defer run.finish()
		defer cancel()

		logBuf := &lockedCappedBuffer{limit: maxAndroidRunLogBytes}
		err := runner(ctx, req, logBuf, logBuf)

		e.buildRelease.mu.Lock()
		active := e.buildRelease.androidRun == run
		scope := e.buildRelease.scope
		if active {
			e.buildRelease.androidRun = nil
			switch {
			case ctx.Err() != nil:
				e.buildRelease.androidDeviceState = BuildDeviceStopped
				e.buildRelease.androidDeviceErr = ""
			case err != nil:
				e.buildRelease.androidDeviceState = BuildDeviceFailed
				e.buildRelease.androidDeviceErr = err.Error()
				e.buildRelease.pendingStatus = fmt.Sprintf("Android USB run failed: %v", err)
				e.buildRelease.hasPendingStatus = true
			default:
				e.buildRelease.androidDeviceState = BuildDeviceRunning
				e.buildRelease.androidDeviceErr = ""
				e.buildRelease.pendingStatus = fmt.Sprintf("Running on %s", dev.Name)
				e.buildRelease.hasPendingStatus = true
			}
			e.buildRelease.rev++
		}
		e.buildRelease.mu.Unlock()

		if active && scope != nil {
			scope.Post(func() {
				e.flushBuildReleaseStatus()
				e.notify()
			})
		}
	}()

	return nil
}

// WaitAndroidDeviceRun blocks until any in-flight Android USB install/run command finishes and returns its error.
func (e *Editor) WaitAndroidDeviceRun() error {
	if e == nil {
		return nil
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.androidRun
	e.buildRelease.mu.Unlock()
	if run != nil {
		<-run.done
	}
	e.flushBuildReleaseStatus()
	e.buildRelease.mu.Lock()
	errText := e.buildRelease.androidDeviceErr
	e.buildRelease.mu.Unlock()
	if errText != "" {
		return errors.New(errText)
	}
	return nil
}

// StopAndroidDeviceRun cancels any in-flight Android USB install/run command.
func (e *Editor) StopAndroidDeviceRun() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.androidRun
	e.buildRelease.androidRun = nil
	if run != nil {
		e.buildRelease.androidDeviceState = BuildDeviceStopped
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// autoServeWebBuildIfReady starts the LAN web build server automatically after a non-dry-run web build finishes
// whenever a servable web build directory or archive exists. It runs on the build worker goroutine and does not
// call e.notify() directly; the caller posts UI notification through Scope.Post.
func (e *Editor) autoServeWebBuildIfReady(
	plan *releaseplan.Plan,
	measured *BuildArtifactResult,
	lanResolver func() (string, error),
	servePort int,
) {
	if e == nil || measured == nil {
		return
	}
	candidates := []string{}
	if measured.WebBuildDir != "" {
		candidates = append(candidates, measured.WebBuildDir)
	}
	if plan != nil && plan.OutputDir != "" {
		candidates = append(candidates, filepath.Join(plan.OutputDir, "web"))
	}
	if measured.ArtifactPath != "" {
		candidates = append(candidates, measured.ArtifactPath)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		info, err := os.Stat(c)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if _, idxErr := os.Stat(filepath.Join(c, "index.html")); idxErr != nil {
				continue
			}
		} else if !strings.EqualFold(filepath.Ext(c), ".zip") {
			continue
		}
		if snap, err := e.startWebBuildServerInternal(c, lanResolver, servePort, false); err == nil {
			measured.TestURL = snap.URL
			measured.WebBuildDir = snap.ServeDir
			return
		}
	}
}

func localBrowserDeviceName() string {
	if runtime.GOOS == "darwin" {
		return "This Mac's browser"
	}
	return "This computer's browser"
}

func validDeviceSerial(s string) bool {
	if s == "" || len(s) > maxWebBridgeFieldLen || s[0] == '-' {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == ':' || c == '@' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func validBuildTargetName(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '-' {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func parseADBDevicesOutput(out string) []ConnectedDevice {
	var devices []ConnectedDevice
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		serial := fields[0]
		if !validDeviceSerial(serial) {
			continue
		}
		state := fields[1]
		name := serial
		for _, f := range fields[2:] {
			if after, ok := strings.CutPrefix(f, "model:"); ok && after != "" {
				name = strings.ReplaceAll(after, "_", " ")
				break
			}
		}
		devices = append(devices, ConnectedDevice{
			ID:        serial,
			Name:      name,
			Platform:  "android",
			Transport: "usb",
			Online:    state == "device",
		})
	}
	return devices
}

const webBridgeBootstrapJS = `// NSGo Game Editor LAN MCP Bridge bootstrap for served web builds in Play mode.
(function () {
  async function connect() {
    const infoRes = await fetch('/__nsgo_mcp/info', { cache: 'no-store' });
    if (!infoRes.ok) return;
    const info = await infoRes.json();
    const device = navigator.userAgentData && navigator.userAgentData.platform
      ? navigator.userAgentData.platform
      : (navigator.platform || 'Phone browser');
    const sessRes = await fetch(info.sessionEndpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-NSGo-MCP-Token': info.token },
      body: JSON.stringify({
        token: info.token,
        device: device,
        playMode: true,
        capabilities: ['inspect', 'input', 'screenshot', 'console']
      })
    });
    if (!sessRes.ok) return;
    const sess = await sessRes.json();
    window.__nsgoMCPBridge = { info: info, session: sess };
    while (true) {
      try {
        const pollRes = await fetch(
          info.pollEndpoint + '?id=' + encodeURIComponent(sess.sessionId) + '&token=' + encodeURIComponent(info.token),
          { cache: 'no-store' }
        );
        if (pollRes.status === 204) continue;
        if (!pollRes.ok) break;
        const call = await pollRes.json();
        let result = null;
        let errText = '';
        try {
          if (window.__nsgoMCP && typeof window.__nsgoMCP.callTool === 'function') {
            result = await window.__nsgoMCP.callTool(call.tool, call.arguments || {});
          } else {
            errText = 'window.__nsgoMCP.callTool is not registered';
          }
        } catch (err) {
          errText = String(err);
        }
        await fetch(info.replyEndpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-NSGo-MCP-Token': info.token },
          body: JSON.stringify({
            token: info.token,
            sessionId: sess.sessionId,
            requestId: call.requestId,
            result: result,
            error: errText
          })
        });
      } catch (_) {
        break;
      }
    }
  }
  connect();
})();
`
