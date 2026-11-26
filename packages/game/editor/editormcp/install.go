package editormcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ClaudeConfigFile is the project-relative MCP configuration file Claude Code reads.
	ClaudeConfigFile = ".mcp.json"
	// CodexConfigFile is the project-relative MCP configuration file Codex reads.
	CodexConfigFile = ".codex/config.toml"
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
	claudeData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ClaudeConfigFile)))
	if err != nil || !bytes.Contains(claudeData, []byte(`"`+ServerName+`"`)) {
		return false
	}
	codexData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(CodexConfigFile)))
	if err != nil || !bytes.Contains(codexData, []byte(ServerName)) {
		return false
	}
	return true
}

func installClaudeConfig(root string) (InstallFile, error) {
	target := filepath.Join(root, filepath.FromSlash(ClaudeConfigFile))
	var existing []byte
	doc := map[string]any{}
	if data, err := os.ReadFile(target); err == nil {
		existing = data
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return InstallFile{}, fmt.Errorf("editormcp: parse existing %s: %w", ClaudeConfigFile, err)
			}
			if doc == nil {
				doc = map[string]any{}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallFile{}, fmt.Errorf("editormcp: read %s: %w", ClaudeConfigFile, err)
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

	changed := !bytes.Equal(existing, encoded)
	if changed {
		if err := writeInstallFileAtomic(target, encoded); err != nil {
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
	target := filepath.Join(root, filepath.FromSlash(CodexConfigFile))
	var existing []byte
	if data, err := os.ReadFile(target); err == nil {
		existing = data
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallFile{}, fmt.Errorf("editormcp: read %s: %w", CodexConfigFile, err)
	}

	encoded := upsertCodexTOML(string(existing))
	changed := !bytes.Equal(existing, encoded)
	if changed {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return InstallFile{}, fmt.Errorf("editormcp: create %s directory: %w", filepath.Dir(CodexConfigFile), err)
		}
		if err := writeInstallFileAtomic(target, encoded); err != nil {
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

func isCodexTargetHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if idx := strings.IndexByte(trimmed, '#'); idx >= 0 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	return trimmed == fmt.Sprintf("[mcp_servers.%s]", ServerName) ||
		trimmed == fmt.Sprintf(`[mcp_servers.%q]`, ServerName)
}

func isTOMLTableHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]")
}

// upsertCodexTOML inserts or replaces the [mcp_servers.nsgo-game-editor] section deterministically while preserving all other sections and comments.
func upsertCodexTOML(src string) []byte {
	section := codexSectionLines()
	normalized := strings.ReplaceAll(src, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []byte(strings.Join(section, "\n") + "\n")
	}

	lines := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	var out []string
	replaced := false
	for i := 0; i < len(lines); {
		if isCodexTargetHeader(lines[i]) {
			if !replaced {
				out = append(out, section...)
				replaced = true
			}
			i++
			for i < len(lines) && !isTOMLTableHeader(lines[i]) {
				i++
			}
			// Preserve a blank separator line before the following section if one exists.
			if i < len(lines) && (len(out) == 0 || strings.TrimSpace(out[len(out)-1]) != "") {
				out = append(out, "")
			}
			continue
		}
		out = append(out, lines[i])
		i++
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
	return []byte(strings.Join(out, "\n") + "\n")
}

func writeInstallFileAtomic(path string, data []byte) error {
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
	if err := tmp.Chmod(0o644); err != nil {
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
