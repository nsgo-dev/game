// Package gotoolchain locates the Go toolchain executable and enriches child process
// environments when the editor or CLI is launched from a GUI desktop launcher (Finder,
// Dock, Spotlight, Windows shortcut, or Linux desktop launcher) that inherits only a
// minimal system PATH.
package gotoolchain

import (
	"bytes"
	"context"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultMinVersion is the minimum Go version required by the project templates.
const DefaultMinVersion = "1.27.3"

// DownloadURL is the official Go download page linked when Go is missing or too old.
const DownloadURL = "https://go.dev/dl/"

// Status describes the result of probing for a usable Go toolchain.
type Status struct {
	// Found reports whether a `go` executable was located.
	Found bool
	// Binary is the resolved path to the `go` executable, or empty when Found is false.
	Binary string
	// Version is the Go version reported by `go version` without the leading "go" prefix (for example "1.27.4").
	Version string
	// MinVersion is the minimum Go version checked against (for example "1.27.3").
	MinVersion string
	// TooOld reports whether Found is true and Version is older than MinVersion.
	TooOld bool
	// PATH is the resolved PATH string containing Binary's directory and standard tool directories.
	PATH string
	// Err holds any error encountered while running `go version`.
	Err string
}

// OK reports whether a Go binary was found, responded to `go version`, and satisfies MinVersion.
func (s Status) OK() bool {
	return s.Found && !s.TooOld && s.Err == ""
}

// SearchEnv captures host environment parameters used to discover the Go toolchain.
// Tests populate it directly (including RootDir and LoginShellPATH) without mutating process state.
type SearchEnv struct {
	GOOS           string
	PathEnv        string
	HomeDir        string
	GoRoot         string
	ProgramFiles   string
	LocalAppData   string
	PathExt        string
	RootDir        string
	LoginShellPATH func() string
	VersionOf      func(ctx context.Context, binary string, env []string) (string, error)
}

var (
	cacheMu               sync.Mutex
	cachedProbeValid      bool
	cachedProbe           Status
	cachedShellValid      bool
	cachedShellPATH       string
	testSearchEnvOverride *SearchEnv
)

// DefaultSearchEnv returns the host process environment for Go toolchain discovery.
func DefaultSearchEnv() SearchEnv {
	home, _ := os.UserHomeDir()
	programFiles := os.Getenv("ProgramFiles")
	if runtime.GOOS == "windows" && strings.TrimSpace(programFiles) == "" {
		programFiles = `C:\Program Files`
	}
	return SearchEnv{
		GOOS:           runtime.GOOS,
		PathEnv:        os.Getenv("PATH"),
		HomeDir:        home,
		GoRoot:         os.Getenv("GOROOT"),
		ProgramFiles:   programFiles,
		LocalAppData:   os.Getenv("LOCALAPPDATA"),
		PathExt:        os.Getenv("PATHEXT"),
		LoginShellPATH: cachedLoginShellPATH,
		VersionOf:      runGoVersion,
	}
}

func activeSearchEnv() SearchEnv {
	cacheMu.Lock()
	override := testSearchEnvOverride
	cacheMu.Unlock()
	if override != nil {
		cp := *override
		if cp.GOOS == "" {
			cp.GOOS = runtime.GOOS
		}
		if cp.VersionOf == nil {
			cp.VersionOf = runGoVersion
		}
		return cp
	}
	return DefaultSearchEnv()
}

// SetSearchEnvForTest replaces the process-wide SearchEnv used by Resolve, Refresh, Command, and Env,
// clears the cached probe result, and returns a function that restores the previous state.
func SetSearchEnvForTest(env SearchEnv) func() {
	cacheMu.Lock()
	prevOverride := testSearchEnvOverride
	prevValid := cachedProbeValid
	prevProbe := cachedProbe
	prevShellValid := cachedShellValid
	prevShellPATH := cachedShellPATH
	cp := env
	testSearchEnvOverride = &cp
	cachedProbeValid = false
	cachedProbe = Status{}
	cachedShellValid = false
	cachedShellPATH = ""
	cacheMu.Unlock()

	return func() {
		cacheMu.Lock()
		testSearchEnvOverride = prevOverride
		cachedProbeValid = prevValid
		cachedProbe = prevProbe
		cachedShellValid = prevShellValid
		cachedShellPATH = prevShellPATH
		cacheMu.Unlock()
	}
}

// ResetCache clears the cached Go toolchain and login-shell PATH discovery results.
func ResetCache() {
	cacheMu.Lock()
	cachedProbeValid = false
	cachedProbe = Status{}
	cachedShellValid = false
	cachedShellPATH = ""
	cacheMu.Unlock()
}

// Resolve locates Go once per process and evaluates it against DefaultMinVersion.
func Resolve() Status {
	return ResolveMin(DefaultMinVersion)
}

// ResolveMin returns the cached Go toolchain probe evaluated against minVersion (DefaultMinVersion when empty).
func ResolveMin(minVersion string) Status {
	minVersion = normalizeMinVersion(minVersion)
	base := cachedBaseProbe(false)
	return evaluateMinVersion(base, minVersion)
}

// Refresh clears the cached Go toolchain and login-shell PATH results, re-probes, and evaluates against DefaultMinVersion.
func Refresh() Status {
	return RefreshMin(DefaultMinVersion)
}

// RefreshMin clears the cached Go toolchain and login-shell PATH results, re-probes, and evaluates against minVersion.
func RefreshMin(minVersion string) Status {
	minVersion = normalizeMinVersion(minVersion)
	base := cachedBaseProbe(true)
	return evaluateMinVersion(base, minVersion)
}

func cachedBaseProbe(force bool) Status {
	cacheMu.Lock()
	if !force && cachedProbeValid {
		st := cachedProbe
		cacheMu.Unlock()
		return st
	}
	if force {
		cachedShellValid = false
		cachedShellPATH = ""
	}
	cacheMu.Unlock()

	env := activeSearchEnv()
	st := ProbeIn(env, DefaultMinVersion)

	cacheMu.Lock()
	cachedProbe = st
	cachedProbeValid = true
	cacheMu.Unlock()
	return st
}

func normalizeMinVersion(minVersion string) string {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(minVersion), "go"))
	if v == "" {
		return DefaultMinVersion
	}
	return v
}

func evaluateMinVersion(st Status, minVersion string) Status {
	st.MinVersion = minVersion
	st.TooOld = false
	if !st.Found || st.Err != "" || minVersion == "" {
		return st
	}
	if st.Version == "" {
		return st
	}
	if CompareVersion(st.Version, minVersion) < 0 {
		st.TooOld = true
	}
	return st
}

// Binary returns the cached resolved path to `go`, or "go" when no binary was found.
func Binary() string {
	st := cachedBaseProbe(false)
	if st.Found && st.Binary != "" {
		return st.Binary
	}
	return "go"
}

// BinaryForEnv resolves "go" using PATH from env first, falling back to the cached toolchain Binary.
// Non-"go" executable names are returned unchanged.
func BinaryForEnv(name string, env []string) string {
	if name != "go" {
		return name
	}
	senv := activeSearchEnv()
	if pathVal, ok := lookupEnvValue(env, "PATH", senv.GOOS); ok {
		exts := candidateExtensions("go", senv.GOOS, senv.PathExt)
		if p, found := searchBinaryInDirs("go", filepath.SplitList(pathVal), exts, senv.GOOS); found {
			return p
		}
	}
	return Binary()
}

// Env returns a copy of baseEnv (or os.Environ() when baseEnv is nil) whose PATH includes
// the resolved Go binary's directory and standard Go install directories.
func Env(baseEnv []string) []string {
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	senv := activeSearchEnv()
	st := cachedBaseProbe(false)
	return EnrichedEnvWith(baseEnv, st.Binary, senv)
}

// EnvWith returns Env(baseEnv) with extra KEY=VALUE entries applied (replacing matching keys).
func EnvWith(baseEnv []string, extra ...string) []string {
	out := Env(baseEnv)
	goos := activeSearchEnv().GOOS
	for _, kv := range extra {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			out = append(out, kv)
			continue
		}
		replaced := false
		for i, existing := range out {
			if ek, _, eok := strings.Cut(existing, "="); eok && equalEnvKey(ek, k, goos) {
				out[i] = kv
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, kv)
		}
	}
	return out
}

// Command returns an *exec.Cmd configured to run the resolved Go executable with an enriched PATH.
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command(Binary(), args...)
	cmd.Env = Env(os.Environ())
	return cmd
}

// CommandContext returns an *exec.Cmd configured to run the resolved Go executable with ctx and an enriched PATH.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, Binary(), args...)
	cmd.Env = Env(os.Environ())
	return cmd
}

// ProbeIn discovers `go` using env, runs `go version`, and checks it against minVersion.
func ProbeIn(env SearchEnv, minVersion string) Status {
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
	minVersion = normalizeMinVersion(minVersion)

	bin, shellPATH, ok := resolveBinaryDetailed(env)
	enriched := enrichedEnvInternal(baseEnvForSearch(env), bin, shellPATH, env)
	resolvedPATH, _ := lookupEnvValue(enriched, "PATH", env.GOOS)
	if !ok {
		return Status{
			Found:      false,
			MinVersion: minVersion,
			PATH:       resolvedPATH,
		}
	}

	versionFn := env.VersionOf
	if versionFn == nil {
		versionFn = runGoVersion
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := versionFn(ctx, bin, enriched)
	if err != nil {
		return Status{
			Found:      true,
			Binary:     bin,
			MinVersion: minVersion,
			PATH:       resolvedPATH,
			Err:        strings.TrimSpace(err.Error()),
		}
	}
	ver, parsed := ParseGoVersion(out)
	if !parsed {
		return Status{
			Found:      true,
			Binary:     bin,
			MinVersion: minVersion,
			PATH:       resolvedPATH,
			Err:        fmt.Sprintf("could not parse go version output %q", strings.TrimSpace(out)),
		}
	}
	st := Status{
		Found:      true,
		Binary:     bin,
		Version:    ver,
		MinVersion: minVersion,
		PATH:       resolvedPATH,
	}
	return evaluateMinVersion(st, minVersion)
}

func baseEnvForSearch(env SearchEnv) []string {
	base := slices.Clone(os.Environ())
	if env.PathEnv != "" {
		return setEnvKey(base, "PATH", env.PathEnv, env.GOOS)
	}
	return base
}

// ResolveBinaryIn locates a `go` executable in env by checking:
//  1. env.PathEnv
//  2. Standard Go install locations, Homebrew, and version-manager shims
//  3. Login shell PATH on Unix hosts
func ResolveBinaryIn(env SearchEnv) (string, bool) {
	bin, _, ok := resolveBinaryDetailed(env)
	return bin, ok
}

func resolveBinaryDetailed(env SearchEnv) (bin string, shellPATH string, ok bool) {
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
	exts := candidateExtensions("go", env.GOOS, env.PathExt)

	// 1. Current process PATH (fast path for terminal launches and test overrides).
	if p, found := searchBinaryInDirs("go", filepath.SplitList(env.PathEnv), exts, env.GOOS); found {
		return p, "", true
	}

	// 2. Standard Go installers, Homebrew, and version-manager shim directories.
	if p, found := searchBinaryInDirs("go", StandardGoBinDirs(env), exts, env.GOOS); found {
		return p, "", true
	}

	// 3. Login-shell PATH fallback on macOS/Linux/BSD (Nix, MacPorts, custom shell profiles).
	if supportsLoginShell(env.GOOS) && env.LoginShellPATH != nil {
		shellPATH = strings.TrimSpace(env.LoginShellPATH())
		if shellPATH != "" {
			if p, found := searchBinaryInDirs("go", filepath.SplitList(shellPATH), exts, env.GOOS); found {
				return p, shellPATH, true
			}
		}
	}

	return "", shellPATH, false
}

// StandardGoBinDirs returns the ordered standard Go install, package-manager, and version-manager
// directories probed when `go` is absent from a GUI launcher's minimal PATH.
func StandardGoBinDirs(env SearchEnv) []string {
	var dirs []string
	add := func(parts ...string) {
		for _, p := range parts {
			if strings.TrimSpace(p) == "" {
				return
			}
		}
		dirs = append(dirs, filepath.Join(parts...))
	}
	addSystem := func(absUnixPath string) {
		if env.RootDir != "" {
			rel := strings.TrimPrefix(absUnixPath, "/")
			dirs = append(dirs, filepath.Join(env.RootDir, filepath.FromSlash(rel)))
			return
		}
		dirs = append(dirs, absUnixPath)
	}

	if env.GoRoot != "" {
		add(env.GoRoot, "bin")
	}

	if env.GOOS == "windows" {
		if env.ProgramFiles != "" {
			add(env.ProgramFiles, "Go", "bin")
		}
		if env.LocalAppData != "" {
			add(env.LocalAppData, "Programs", "Go", "bin")
		}
		if env.HomeDir != "" {
			add(env.HomeDir, "scoop", "shims")
			add(env.HomeDir, "scoop", "apps", "go", "current", "bin")
			add(env.HomeDir, ".local", "share", "mise", "shims")
			add(env.HomeDir, "go", "bin")
		}
		if env.RootDir != "" {
			add(env.RootDir, "Go", "bin")
		} else if env.ProgramFiles == "" {
			dirs = append(dirs, `C:\Program Files\Go\bin`, `C:\Go\bin`)
		} else {
			dirs = append(dirs, `C:\Go\bin`)
		}
		return dirs
	}

	// Official go.dev installer (/usr/local/go/bin) — added via /etc/paths.d on macOS,
	// which launchd GUI sessions do not read.
	addSystem("/usr/local/go/bin")
	if env.GOOS == "darwin" {
		addSystem("/opt/homebrew/bin")
		addSystem("/opt/homebrew/sbin")
	}
	addSystem("/usr/local/bin")
	addSystem("/usr/local/sbin")

	if env.HomeDir != "" {
		add(env.HomeDir, ".local", "share", "mise", "shims")
		add(env.HomeDir, ".asdf", "shims")
		add(env.HomeDir, ".gvm", "bin")
		add(env.HomeDir, "go", "bin")
		add(env.HomeDir, ".local", "bin")
		add(env.HomeDir, "bin")
	}

	if env.GOOS == "linux" {
		addSystem("/snap/bin")
		addSystem("/usr/lib/go/bin")
	}
	return dirs
}

// EnrichedEnvWith returns a copy of baseEnv whose PATH includes resolvedBin's directory,
// existing PATH entries, and any existing standard Go/tool directories.
func EnrichedEnvWith(baseEnv []string, resolvedBin string, env SearchEnv) []string {
	var shellPATH string
	cacheMu.Lock()
	if cachedShellValid {
		shellPATH = cachedShellPATH
	}
	cacheMu.Unlock()
	return enrichedEnvInternal(baseEnv, resolvedBin, shellPATH, env)
}

func enrichedEnvInternal(baseEnv []string, resolvedBin, shellPATH string, env SearchEnv) []string {
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
	var existingPath string
	pathIdx := -1
	out := make([]string, 0, len(baseEnv)+1)
	for _, kv := range baseEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && equalEnvKey(k, "PATH", env.GOOS) {
			existingPath = v
			pathIdx = len(out)
		}
		out = append(out, kv)
	}

	seen := make(map[string]struct{})
	dirs := make([]string, 0, 16)
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		clean := filepath.Clean(d)
		key := clean
		if env.GOOS == "windows" {
			key = strings.ToLower(clean)
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		dirs = append(dirs, clean)
	}

	if resolvedBin != "" {
		if dir := filepath.Dir(resolvedBin); dir != "" && dir != "." {
			addDir(dir)
		}
	}
	for _, d := range filepath.SplitList(existingPath) {
		addDir(d)
	}
	for _, d := range StandardGoBinDirs(env) {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			addDir(d)
		}
	}
	for _, d := range filepath.SplitList(shellPATH) {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			addDir(d)
		}
	}

	joined := strings.Join(dirs, string(os.PathListSeparator))
	if pathIdx >= 0 {
		out[pathIdx] = "PATH=" + joined
	} else {
		out = append(out, "PATH="+joined)
	}
	return out
}

// ParseGoVersion extracts a Go version string (without the leading "go" prefix, e.g. "1.27.4")
// from `go version` output such as "go version go1.27.4 darwin/arm64" or
// "go version devel go1.28-abcdef0123 Thu Nov 27 ...".
func ParseGoVersion(output string) (string, bool) {
	fields := strings.Fields(output)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "devel" && i+1 < len(fields) {
			cand := fields[i+1]
			if idx := strings.IndexAny(cand, "-+"); idx > 0 {
				cand = cand[:idx]
			}
			if version.IsValid(cand) {
				return strings.TrimPrefix(cand, "go"), true
			}
		}
		if strings.HasPrefix(f, "go") && version.IsValid(f) {
			return strings.TrimPrefix(f, "go"), true
		}
	}
	return "", false
}

// CompareVersion compares two Go versions (with or without a leading "go" prefix)
// using Go's canonical version ordering (go/version.Compare).
// Returns -1 if a < b, 0 if a == b, and +1 if a > b.
func CompareVersion(a, b string) int {
	return version.Compare(toGoVersion(a), toGoVersion(b))
}

func toGoVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "go") {
		return "go" + v
	}
	return v
}

// ParseGoModMinVersion extracts the `go X.Y[.Z]` directive version from a go.mod or go.mod.tmpl file.
func ParseGoModMinVersion(goMod []byte) (string, bool) {
	for _, rawLine := range strings.Split(string(goMod), "\n") {
		line := strings.TrimSpace(rawLine)
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			if version.IsValid(toGoVersion(fields[1])) {
				return strings.TrimPrefix(fields[1], "go"), true
			}
		}
	}
	return "", false
}

// MaxGoVersion returns the highest valid Go version among versions, or empty if none are valid.
func MaxGoVersion(versions ...string) string {
	best := ""
	for _, v := range versions {
		norm := strings.TrimPrefix(strings.TrimSpace(v), "go")
		if norm == "" || !version.IsValid(toGoVersion(norm)) {
			continue
		}
		if best == "" || CompareVersion(norm, best) > 0 {
			best = norm
		}
	}
	return best
}

func runGoVersion(ctx context.Context, binary string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.Env = env
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func cachedLoginShellPATH() string {
	cacheMu.Lock()
	if cachedShellValid {
		v := cachedShellPATH
		cacheMu.Unlock()
		return v
	}
	cacheMu.Unlock()

	v := queryLoginShellPATH()

	cacheMu.Lock()
	cachedShellPATH = v
	cachedShellValid = true
	cacheMu.Unlock()
	return v
}

func queryLoginShellPATH() string {
	if !supportsLoginShell(runtime.GOOS) {
		return ""
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		} else {
			shell = "/bin/sh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lc", `printf '%s' "$PATH"`)
	cmd.Stdin = nil
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func supportsLoginShell(goos string) bool {
	switch goos {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd":
		return true
	default:
		return false
	}
}

func candidateExtensions(name, goos, pathExt string) []string {
	if goos != "windows" {
		return []string{""}
	}
	if filepath.Ext(name) != "" {
		return []string{""}
	}
	if strings.TrimSpace(pathExt) == "" {
		pathExt = ".EXE;.CMD;.BAT;.COM"
	}
	raw := strings.Split(pathExt, ";")
	out := make([]string, 0, len(raw)+1)
	for _, ext := range raw {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out = append(out, strings.ToLower(ext))
	}
	if len(out) == 0 {
		out = []string{".exe", ".cmd", ".bat"}
	}
	return out
}

func searchBinaryInDirs(name string, dirs []string, exts []string, goos string) (string, bool) {
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		key := clean
		if goos == "windows" {
			key = strings.ToLower(clean)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		for _, ext := range exts {
			cand := filepath.Join(clean, name+ext)
			if isExecutableCandidate(cand, goos) {
				return cand, true
			}
		}
	}
	return "", false
}

func isExecutableCandidate(path, goos string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if goos == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func equalEnvKey(a, b, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func lookupEnvValue(env []string, key, goos string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && equalEnvKey(k, key, goos) {
			return v, true
		}
	}
	return "", false
}

func setEnvKey(env []string, key, val, goos string) []string {
	for i, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && equalEnvKey(k, key, goos) {
			env[i] = key + "=" + val
			return env
		}
	}
	return append(env, key+"="+val)
}
