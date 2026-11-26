package editormcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// ClaudeConfigFile is the project-relative MCP configuration file Claude Code reads.
	ClaudeConfigFile = ".mcp.json"
	// CodexConfigFile is the project-relative MCP configuration file Codex reads.
	CodexConfigFile = ".codex/config.toml"

	maxInstallConfigBytes = 2 << 20 // 2 MiB cap for reading existing MCP config files.
	defaultInstallPerm    = 0o600
	defaultInstallDirPerm = 0o700
)

// InstallFile describes one project-level MCP configuration file written or verified by Install.
type InstallFile struct {
	// Client names the external agent that reads this file ("Claude Code" or "Codex").
	Client string
	// Path is the absolute path on disk.
	Path string
	// RelPath is the slash-separated path relative to the project root.
	RelPath string
	// Changed reports whether Install wrote new bytes (false when the file was already identical).
	Changed bool
}

// InstallResult summarizes the files created or updated by Install.
type InstallResult struct {
	Root  string
	Files []InstallFile
}

// InstallCommand returns the command and arguments an external MCP client runs in the project root to connect to the open editor.
func InstallCommand() (string, []string) {
	return "go", []string{"tool", "game", "mcp"}
}

// Install writes the project-level MCP configuration files that Claude Code (.mcp.json) and Codex (.codex/config.toml) read so they find the editor's MCP server automatically.
// Existing unrelated MCP servers or settings in either file are preserved, and running Install twice produces byte-for-byte identical files.
func Install(path string) (*InstallResult, error) {
	root, err := ProjectRoot(path)
	if err != nil {
		return nil, err
	}
	claudeFile, err := installClaudeConfig(root)
	if err != nil {
		return nil, err
	}
	codexFile, err := installCodexConfig(root)
	if err != nil {
		return nil, err
	}
	return &InstallResult{
		Root:  root,
		Files: []InstallFile{claudeFile, codexFile},
	}, nil
}

// Installed reports whether both Claude Code and Codex project MCP configurations exist in root and reference ServerName.
func Installed(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	claudePath, err := verifyInstallPathWithinRoot(root, ClaudeConfigFile)
	if err != nil {
		return false
	}
	claudeData, _, exists, err := readCappedInstallFile(claudePath, maxInstallConfigBytes)
	if err != nil || !exists || !bytes.Contains(claudeData, []byte(`"`+ServerName+`"`)) {
		return false
	}
	codexPath, err := verifyInstallPathWithinRoot(root, CodexConfigFile)
	if err != nil {
		return false
	}
	codexData, _, exists, err := readCappedInstallFile(codexPath, maxInstallConfigBytes)
	if err != nil || !exists || !bytes.Contains(codexData, []byte(ServerName)) {
		return false
	}
	return true
}

func isWithinCleanRoot(realRoot, target string) bool {
	rel, err := filepath.Rel(realRoot, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// verifyInstallPathWithinRoot ensures relPath under root (including any existing symlink components
// or parent directories) resolves strictly within root.
func verifyInstallPathWithinRoot(root, relPath string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = filepath.Clean(root)
	}
	absCandidate := filepath.Join(realRoot, filepath.FromSlash(relPath))
	if realTarget, err := filepath.EvalSymlinks(absCandidate); err == nil {
		if !isWithinCleanRoot(realRoot, realTarget) {
			return "", fmt.Errorf("editormcp: %s resolves outside project root %s", relPath, realRoot)
		}
		return realTarget, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	// Walk up to nearest existing parent directory and confirm no symlink escapes realRoot.
	cur := absCandidate
	var tail []string
	for {
		parent := filepath.Dir(cur)
		tail = append(tail, filepath.Base(cur))
		if parent == cur {
			return "", fmt.Errorf("editormcp: %s is outside project root %s", relPath, realRoot)
		}
		if realParent, err := filepath.EvalSymlinks(parent); err == nil {
			slices.Reverse(tail)
			joined := filepath.Join(append([]string{realParent}, tail...)...)
			if !isWithinCleanRoot(realRoot, joined) {
				return "", fmt.Errorf("editormcp: %s resolves outside project root %s", relPath, realRoot)
			}
			return joined, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		cur = parent
	}
}

func readCappedInstallFile(path string, maxBytes int64) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, defaultInstallPerm, false, nil
		}
		return nil, 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return nil, 0, false, err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return nil, 0, false, fmt.Errorf("%s exceeds %d byte config limit", path, maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, 0, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, 0, false, fmt.Errorf("%s exceeds %d byte config limit", path, maxBytes)
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		perm = defaultInstallPerm
	}
	return data, perm, true, nil
}

func installClaudeConfig(root string) (InstallFile, error) {
	target, err := verifyInstallPathWithinRoot(root, ClaudeConfigFile)
	if err != nil {
		return InstallFile{}, err
	}
	existing, perm, exists, err := readCappedInstallFile(target, maxInstallConfigBytes)
	if err != nil {
		return InstallFile{}, fmt.Errorf("editormcp: read %s: %w", ClaudeConfigFile, err)
	}
	doc := map[string]any{}
	if exists && len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return InstallFile{}, fmt.Errorf("editormcp: parse existing %s: %w", ClaudeConfigFile, err)
		}
		if doc == nil {
			doc = map[string]any{}
		}
	}

	var servers map[string]any
	if raw, ok := doc["mcpServers"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return InstallFile{}, fmt.Errorf("editormcp: %s: mcpServers must be a JSON object", ClaudeConfigFile)
		}
		servers = m
	} else {
		servers = map[string]any{}
	}

	cmd, args := InstallCommand()
	servers[ServerName] = map[string]any{
		"command": cmd,
		"args":    args,
	}
	doc["mcpServers"] = servers

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return InstallFile{}, fmt.Errorf("editormcp: encode %s: %w", ClaudeConfigFile, err)
	}
	encoded = append(encoded, '\n')

	changed := !exists || !bytes.Equal(existing, encoded)
	if changed {
		if err := writeInstallFileAtomic(target, encoded, perm); err != nil {
			return InstallFile{}, fmt.Errorf("editormcp: write %s: %w", ClaudeConfigFile, err)
		}
	}
	return InstallFile{
		Client:  "Claude Code",
		Path:    target,
		RelPath: ClaudeConfigFile,
		Changed: changed,
	}, nil
}

func installCodexConfig(root string) (InstallFile, error) {
	target, err := verifyInstallPathWithinRoot(root, CodexConfigFile)
	if err != nil {
		return InstallFile{}, err
	}
	existing, perm, exists, err := readCappedInstallFile(target, maxInstallConfigBytes)
	if err != nil {
		return InstallFile{}, fmt.Errorf("editormcp: read %s: %w", CodexConfigFile, err)
	}

	encoded, err := upsertCodexTOML(string(existing))
	if err != nil {
		return InstallFile{}, fmt.Errorf("editormcp: parse existing %s: %w", CodexConfigFile, err)
	}
	changed := !exists || !bytes.Equal(existing, encoded)
	if changed {
		if err := os.MkdirAll(filepath.Dir(target), defaultInstallDirPerm); err != nil {
			return InstallFile{}, fmt.Errorf("editormcp: create %s directory: %w", filepath.Dir(CodexConfigFile), err)
		}
		if err := writeInstallFileAtomic(target, encoded, perm); err != nil {
			return InstallFile{}, fmt.Errorf("editormcp: write %s: %w", CodexConfigFile, err)
		}
	}
	return InstallFile{
		Client:  "Codex",
		Path:    target,
		RelPath: CodexConfigFile,
		Changed: changed,
	}, nil
}

func codexSectionLines() []string {
	cmd, args := InstallCommand()
	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = fmt.Sprintf("%q", a)
	}
	return []string{
		fmt.Sprintf("[mcp_servers.%s]", ServerName),
		fmt.Sprintf("command = %q", cmd),
		fmt.Sprintf("args = [%s]", strings.Join(quotedArgs, ", ")),
	}
}

// stripTOMLComment removes a trailing # comment that occurs outside single/double quotes.
func stripTOMLComment(line string) string {
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble {
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		switch c {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case '#':
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

func isCodexTargetHeader(line string) bool {
	trimmed := stripTOMLComment(line)
	return trimmed == fmt.Sprintf("[mcp_servers.%s]", ServerName) ||
		trimmed == fmt.Sprintf(`[mcp_servers.%q]`, ServerName)
}

// isTOMLTableHeader validates that line (outside any multi-line array) is a well-formed TOML
// table header ([a.b] or [[a.b]]), refusing array literals like ["a", "b"] and reporting malformed
// headers (such as unclosed brackets).
func isTOMLTableHeader(line string) (isHeader bool, err error) {
	trimmed := stripTOMLComment(line)
	if !strings.HasPrefix(trimmed, "[") {
		return false, nil
	}
	inner := ""
	if strings.HasPrefix(trimmed, "[[") {
		if !strings.HasSuffix(trimmed, "]]") || len(trimmed) < 5 {
			return false, fmt.Errorf("malformed TOML table header %q", trimmed)
		}
		inner = strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	} else {
		if !strings.HasSuffix(trimmed, "]") || len(trimmed) < 3 {
			return false, fmt.Errorf("malformed TOML table header %q", trimmed)
		}
		inner = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	if inner == "" {
		return false, fmt.Errorf("empty TOML table header %q", trimmed)
	}
	// Reject array literals like ["foo", "bar"] that contain top-level commas or '='.
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble {
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		switch c {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case ',', '=', '[', ']':
			return false, nil
		}
	}
	if inDouble || inSingle {
		return false, fmt.Errorf("unterminated quote in TOML table header %q", trimmed)
	}
	return true, nil
}

// updateBracketDepth tracks net '[' - ']' outside quotes so multi-line arrays inside a section are
// not mistaken for table headers.
func updateBracketDepth(depth int, line string) int {
	trimmed := stripTOMLComment(line)
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble {
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		switch c {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// upsertCodexTOML inserts or replaces the [mcp_servers.nsgo-game-editor] section deterministically while preserving all other sections and comments.
func upsertCodexTOML(src string) ([]byte, error) {
	section := codexSectionLines()
	normalized := strings.ReplaceAll(src, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []byte(strings.Join(section, "\n") + "\n"), nil
	}

	lines := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	var out []string
	replaced := false
	bracketDepth := 0
	for i := 0; i < len(lines); {
		if bracketDepth == 0 {
			isHdr, err := isTOMLTableHeader(lines[i])
			if err != nil {
				return nil, err
			}
			if isHdr && isCodexTargetHeader(lines[i]) {
				if !replaced {
					out = append(out, section...)
					replaced = true
				}
				i++
				secDepth := 0
				for i < len(lines) {
					if secDepth == 0 {
						nextHdr, err := isTOMLTableHeader(lines[i])
						if err != nil {
							return nil, err
						}
						if nextHdr {
							break
						}
					}
					secDepth = updateBracketDepth(secDepth, lines[i])
					i++
				}
				if secDepth != 0 {
					return nil, errors.New("unclosed '[' array inside [mcp_servers.nsgo-game-editor]")
				}
				// Preserve a blank separator line before the following section if one exists.
				if i < len(lines) && (len(out) == 0 || strings.TrimSpace(out[len(out)-1]) != "") {
					out = append(out, "")
				}
				continue
			}
			if !isHdr {
				bracketDepth = updateBracketDepth(bracketDepth, lines[i])
			}
		} else {
			bracketDepth = updateBracketDepth(bracketDepth, lines[i])
		}
		out = append(out, lines[i])
		i++
	}
	if bracketDepth != 0 {
		return nil, errors.New("unclosed '[' array in config.toml")
	}
	if !replaced {
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, section...)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

func writeInstallFileAtomic(path string, data []byte, perm os.FileMode) error {
	if perm == 0 {
		perm = defaultInstallPerm
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mcp-install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
