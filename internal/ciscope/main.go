package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct{ Full, Mobile, Docs bool }

type DocDelta struct{ Before, After string }

func classify(paths []string, docs map[string]DocDelta) Result {
	if len(paths) == 0 {
		return Result{Full: true}
	}
	r := Result{}
	for _, raw := range paths {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "docs/") || strings.HasPrefix(p, "design/") || strings.HasPrefix(p, "changelog.d/") || strings.HasPrefix(p, ".agents/notes/") || strings.HasSuffix(strings.ToLower(p), ".md") {
			r.Docs = true
			if d, ok := docs[p]; ok && documentationContractChanged(d.Before, d.After) {
				r.Full = true
			}
			continue
		}
		r.Full = true
		if strings.Contains(p, "/android/") || strings.Contains(p, "/ios/") || strings.HasSuffix(p, ".ipa") || strings.HasSuffix(p, ".aab") {
			r.Mobile = true
		}
	}
	return r
}

func documentationContractChanged(before, after string) bool {
	before = strings.ReplaceAll(before, "\r\n", "\n")
	after = strings.ReplaceAll(after, "\r\n", "\n")
	if before == after {
		return false
	}
	return contractLines(before) != contractLines(after)
}

func contractLines(s string) string {
	var out []string
	fenced := false
	lang := ""
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			if !fenced {
				lang = strings.TrimSpace(strings.TrimPrefix(trim, "```"))
			} else {
				lang = ""
			}
			fenced = !fenced
			continue
		}
		if fenced && (lang == "sh" || lang == "bash" || lang == "console" || lang == "shell") {
			out = append(out, line)
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "go tool game") || strings.Contains(lower, "go tool nsgo") || strings.Contains(lower, "game new") || strings.Contains(lower, "go run") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func changedFiles() ([]string, error) {
	base := strings.TrimSpace(os.Getenv("CI_BASE_SHA"))
	if base == "" {
		base = "HEAD~1"
	}
	args := []string{"diff", "--name-only", base + "...HEAD"}
	if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
		args = []string{"diff", "--name-only", "HEAD~1", "HEAD"}
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func main() {
	paths, err := changedFiles()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r := classify(paths, nil)
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("full=%t\nmobile=%t\ndocs=%t\n", r.Full, r.Mobile, r.Docs))
	if out := os.Getenv("GITHUB_OUTPUT"); out != "" {
		f, e := os.OpenFile(out, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if e != nil {
			panic(e)
		}
		defer f.Close()
		_, _ = f.WriteString(b.String())
	}
	w := bufio.NewWriter(os.Stdout)
	_, _ = w.WriteString(b.String())
	_ = w.Flush()
}
