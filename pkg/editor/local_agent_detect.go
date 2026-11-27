package editor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type localAgentSearchEnv struct {
	goos          string
	pathEnv       string
	homeDir       string
	appData       string
	localAppData  string
	programFiles  string
	pathExt       string
	loginShellEnv func() string
}

var (
	loginShellPathOnce sync.Once
	loginShellPathVal  string
)

// defaultLocalAgentSearchEnv captures the host process environment for local CLI discovery.
func defaultLocalAgentSearchEnv() localAgentSearchEnv {
	home, _ := os.UserHomeDir()
	return localAgentSearchEnv{
		goos:          runtime.GOOS,
		pathEnv:       os.Getenv("PATH"),
		homeDir:       home,
		appData:       os.Getenv("APPDATA"),
		localAppData:  os.Getenv("LOCALAPPDATA"),
		programFiles:  os.Getenv("ProgramFiles"),
		pathExt:       os.Getenv("PATHEXT"),
		loginShellEnv: cachedLoginShellPATH,
	}
}

// resolveLocalAgentBinary locates a locally installed agent CLI binary (such as "claude" or "codex").
// GUI desktop apps launched from Finder, Spotlight, or Dock inherit only a minimal system PATH
// (e.g. /usr/bin:/bin:/usr/sbin:/sbin on macOS), so after checking PATH this probes standard
// user and package-manager bin directories (~/.local/bin, ~/.npm-global/bin, Homebrew, pnpm,
// bun, volta, fnm, mise, asdf, nvm, ~/.npmrc prefix) and finally falls back to a cached login shell PATH.
func resolveLocalAgentBinary(name string) (string, bool) {
	return resolveLocalAgentBinaryIn(name, defaultLocalAgentSearchEnv())
}

func resolveLocalAgentBinaryIn(name string, env localAgentSearchEnv) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if strings.ContainsAny(name, "/\\") {
		if resolved, err := exec.LookPath(name); err == nil && resolved != "" {
			return resolved, true
		}
		if isExecutableCandidate(name, env.goos) {
			return name, true
		}
		return "", false
	}

	exts := candidateExtensions(name, env.goos, env.pathExt)

	// 1. Current process PATH (fast path for CLI launches and test overrides).
	if p, ok := searchBinaryInDirs(name, filepath.SplitList(env.pathEnv), exts, env.goos); ok {
		return p, true
	}

	// 2. Standard user & package-manager binary directories.
	commonDirs := commonLocalAgentBinDirs(env)
	if p, ok := searchBinaryInDirs(name, commonDirs, exts, env.goos); ok {
		return p, true
	}

	// 3. Cached login-shell PATH fallback on macOS/Linux for custom shell configurations (Nix, MacPorts, etc.).
	if env.goos != "windows" && env.loginShellEnv != nil {
		if shellPath := strings.TrimSpace(env.loginShellEnv()); shellPath != "" {
			if p, ok := searchBinaryInDirs(name, filepath.SplitList(shellPath), exts, env.goos); ok {
				return p, true
			}
		}
	}

	return "", false
}

// enrichedAgentEnv returns an environment slice based on baseEnv whose PATH includes the
// resolved binary's directory and standard user/package-manager tool directories. This ensures
// script-based CLIs such as Codex (#!/usr/bin/env node) and child MCP commands find node/go
// even when the editor was launched as a macOS GUI .app bundle with a minimal launchd PATH.
func enrichedAgentEnv(baseEnv []string, resolvedBin string) []string {
	env := defaultLocalAgentSearchEnv()
	return enrichedAgentEnvWith(baseEnv, resolvedBin, env)
}

func enrichedAgentEnvWith(baseEnv []string, resolvedBin string, env localAgentSearchEnv) []string {
	var existingPath string
	pathIdx := -1
	out := make([]string, 0, len(baseEnv)+1)
	for _, kv := range baseEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && equalEnvKey(k, "PATH", env.goos) {
			existingPath = v
			pathIdx = len(out)
		}
		out = append(out, kv)
	}

	seen := make(map[string]struct{})
	dirs := make([]string, 0, 24)
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		clean := filepath.Clean(d)
		key := clean
		if env.goos == "windows" {
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
	for _, d := range commonLocalAgentBinDirs(env) {
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

func equalEnvKey(a, b, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func candidateExtensions(name, goos, pathExt string) []string {
	if goos != "windows" {
		return []string{""}
	}
	if filepath.Ext(name) != "" {
		return []string{""}
	}
	if strings.TrimSpace(pathExt) == "" {
		pathExt = ".COM;.EXE;.BAT;.CMD"
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
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
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

func commonLocalAgentBinDirs(env localAgentSearchEnv) []string {
	var dirs []string
	add := func(parts ...string) {
		for _, p := range parts {
			if p == "" {
				return
			}
		}
		dirs = append(dirs, filepath.Join(parts...))
	}

	if env.goos == "windows" {
		if env.appData != "" {
			add(env.appData, "npm")
		}
		if env.localAppData != "" {
			add(env.localAppData, "Programs", "claude")
			add(env.localAppData, "pnpm")
		}
		if env.homeDir != "" {
			if prefix := readNpmrcPrefix(env.homeDir); prefix != "" {
				add(prefix)
			}
			add(env.homeDir, ".local", "bin")
			add(env.homeDir, ".claude", "local")
			add(env.homeDir, ".bun", "bin")
			add(env.homeDir, ".volta", "bin")
			add(env.homeDir, ".cargo", "bin")
			add(env.homeDir, "go", "bin")
		}
		if env.programFiles != "" {
			add(env.programFiles, "nodejs")
			add(env.programFiles, "Go", "bin")
		}
		return dirs
	}

	if env.homeDir != "" {
		// Claude Code official native installer & legacy local installer.
		add(env.homeDir, ".local", "bin")
		add(env.homeDir, ".claude", "local")
		// User-configured npm global prefix (~/.npmrc prefix=...).
		if prefix := readNpmrcPrefix(env.homeDir); prefix != "" {
			add(prefix, "bin")
		}
		add(env.homeDir, ".npm-global", "bin")
		add(env.homeDir, ".npm-packages", "bin")
		add(env.homeDir, ".bun", "bin")
		if env.goos == "darwin" {
			add(env.homeDir, "Library", "pnpm")
			add(env.homeDir, "Library", "Application Support", "fnm", "aliases", "default", "bin")
		}
		add(env.homeDir, ".local", "share", "pnpm")
		add(env.homeDir, ".volta", "bin")
		add(env.homeDir, ".local", "share", "mise", "shims")
		add(env.homeDir, ".asdf", "shims")
		add(env.homeDir, ".fnm", "current", "bin")
		dirs = append(dirs, nvmNodeBinDirs(env.homeDir)...)
		add(env.homeDir, ".yarn", "bin")
		add(env.homeDir, "bin")
		add(env.homeDir, "go", "bin")
		add(env.homeDir, ".cargo", "bin")
	}

	// Standard Homebrew / system-local / Go installer directories (not in macOS launchd default PATH).
	if env.goos == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin", "/opt/homebrew/sbin")
	}
	dirs = append(dirs, "/usr/local/go/bin", "/usr/local/bin", "/usr/local/sbin")
	return dirs
}

// readNpmrcPrefix reads `prefix = ...` from ~/.npmrc if present, expanding a leading ~.
func readNpmrcPrefix(homeDir string) string {
	data, err := os.ReadFile(filepath.Join(homeDir, ".npmrc"))
	if err != nil || len(data) == 0 || len(data) > 64*1024 {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != "prefix" {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v == "" {
			continue
		}
		if v == "~" {
			return homeDir
		}
		if strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
			return filepath.Join(homeDir, v[2:])
		}
		return v
	}
	return ""
}

// nvmNodeBinDirs returns ~/.nvm/versions/node/*/bin directories sorted newest semver first.
func nvmNodeBinDirs(homeDir string) []string {
	root := filepath.Join(homeDir, ".nvm", "versions", "node")
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			names = append(names, ent.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return compareSemverDesc(names[i], names[j])
	})
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(root, n, "bin"))
	}
	return out
}

func compareSemverDesc(a, b string) bool {
	pa := parseSemverTuple(a)
	pb := parseSemverTuple(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return a > b
}

func parseSemverTuple(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			out[i] = n
		}
	}
	return out
}

// cachedLoginShellPATH asks the user's login shell for PATH once per process with a short timeout.
// This recovers custom user PATH entries (e.g. Nix, MacPorts, custom aliases) when launched as a macOS .app.
func cachedLoginShellPATH() string {
	loginShellPathOnce.Do(func() {
		if runtime.GOOS == "windows" {
			return
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
			return
		}
		loginShellPathVal = strings.TrimSpace(stdout.String())
	})
	return loginShellPathVal
}
