package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const (
	defaultGitTimeout        = 30 * time.Second
	defaultGitNetworkTimeout = 90 * time.Second
)

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	rem := b.limit - int64(b.buf.Len())
	if rem <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > rem {
		_, _ = b.buf.Write(p[:rem])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func gitBaseEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+2+len(extra))
	env = append(env, os.Environ()...)
	env = append(env, "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	env = append(env, extra...)
	return env
}

func (e *Editor) runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
	defer cancel()
	return e.runGitEnvCtx(ctx, dir, nil, nil, args...)
}

func (e *Editor) runGitCtx(ctx context.Context, dir string, args ...string) (string, error) {
	return e.runGitEnvCtx(ctx, dir, nil, nil, args...)
}

func (e *Editor) runGitEnvCtx(ctx context.Context, dir string, extraEnv []string, stdin []byte, args ...string) (string, error) {
	if e != nil && e.GitRunner != nil {
		return e.GitRunner(dir, args...)
	}
	return runGitExecCtx(ctx, dir, extraEnv, stdin, args...)
}

func runGitExecCtx(ctx context.Context, dir string, extraEnv []string, stdin []byte, args ...string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultGitTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitBaseEnv(extraEnv)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	} else {
		cmd.Stdin = nil
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (e *Editor) runGitRaw(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
	defer cancel()
	data, _, err := e.runGitRawBoundedCtx(ctx, dir, 0, args...)
	return data, err
}

func (e *Editor) runGitRawBoundedCtx(ctx context.Context, dir string, maxBytes int64, args ...string) ([]byte, bool, error) {
	if e != nil && e.GitRunner != nil {
		out, err := e.GitRunner(dir, args...)
		raw := []byte(out)
		if maxBytes > 0 && int64(len(raw)) > maxBytes {
			return raw[:maxBytes], true, err
		}
		return raw, false, err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultGitTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitBaseEnv(nil)
	cmd.Stdin = nil
	stdout := &limitedBuffer{limit: maxBytes}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.buf.String())
		if msg == "" {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: %s", err, msg)
	}
	return stdout.buf.Bytes(), stdout.truncated, nil
}

// readFileBounded reads up to maxBytes from absPath, returning the bytes read, whether the file
// exceeded maxBytes, and the total file size in bytes.
func readFileBounded(absPath string, maxBytes int64) (data []byte, truncated bool, size int64, err error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, false, 0, err
	}
	defer f.Close()
	if st, serr := f.Stat(); serr == nil {
		size = st.Size()
	}
	if maxBytes <= 0 {
		raw, rerr := io.ReadAll(f)
		return raw, false, int64(len(raw)), rerr
	}
	raw, rerr := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if rerr != nil {
		return nil, false, size, rerr
	}
	if int64(len(raw)) > maxBytes {
		return raw[:maxBytes], true, max(size, int64(len(raw))), nil
	}
	if size == 0 {
		size = int64(len(raw))
	}
	return raw, false, size, nil
}

// DetectSceneMergeConflicts checks the open project's Git repository (or dir when given)
// for an in-progress merge with unmerged scene YAML files, returning their slash-separated paths
// relative to the Git repository root.
func (e *Editor) DetectSceneMergeConflicts() ([]string, error) {
	dir := e.gitProjectDir()
	if dir == "" {
		return nil, nil
	}
	return e.detectSceneMergeConflictsIn(dir)
}

func (e *Editor) gitProjectDir() string {
	if e == nil {
		return ""
	}
	if e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	if e.FilePath != "" {
		return filepath.Dir(e.FilePath)
	}
	return ""
}

func (e *Editor) refreshDetectedMergeConflicts() {
	if e == nil {
		return
	}
	scenes, err := e.DetectSceneMergeConflicts()
	if err != nil {
		e.merge.detectedScenes = nil
		return
	}
	e.merge.detectedScenes = scenes
}

func (e *Editor) detectSceneMergeConflictsIn(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	repoRoot, err := e.runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		return nil, nil
	}
	if _, err := e.runGit(repoRoot, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return nil, nil
	}
	out, err := e.runGit(repoRoot, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	projectPrefix := ""
	if e.ProjectPath != "" {
		if rel, rerr := filepath.Rel(repoRoot, filepath.Dir(e.ProjectPath)); rerr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			projectPrefix = filepath.ToSlash(rel) + "/"
		}
	}
	var scenes []string
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		p = filepath.ToSlash(p)
		projRel := p
		if projectPrefix != "" && strings.HasPrefix(p, projectPrefix) {
			projRel = strings.TrimPrefix(p, projectPrefix)
		}
		if isProjectScene(e.Project, p) || (projRel != p && isProjectScene(e.Project, projRel)) {
			scenes = append(scenes, p)
		}
	}
	slices.Sort(scenes)
	return scenes, nil
}

func isWithinCleanRoot(realRoot, candidate string) (string, bool) {
	rel, err := filepath.Rel(realRoot, candidate)
	if err != nil || rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// resolvePathWithinRoot verifies that absCandidate (after resolving symlinks on the target or its
// nearest existing ancestor) stays inside realRoot, returning the clean relative path and absolute path.
func resolvePathWithinRoot(realRoot, absCandidate string) (string, string, error) {
	absCandidate = filepath.Clean(absCandidate)
	if realTarget, err := filepath.EvalSymlinks(absCandidate); err == nil {
		rel, ok := isWithinCleanRoot(realRoot, realTarget)
		if !ok {
			return "", "", fmt.Errorf("editor: %s resolves outside %s", absCandidate, realRoot)
		}
		return filepath.ToSlash(rel), realTarget, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}

	// Walk up to nearest existing parent directory and ensure no symlink escapes realRoot.
	cur := absCandidate
	var tail []string
	for {
		parent := filepath.Dir(cur)
		tail = append(tail, filepath.Base(cur))
		if parent == cur {
			return "", "", fmt.Errorf("editor: %s is outside %s", absCandidate, realRoot)
		}
		if realParent, err := filepath.EvalSymlinks(parent); err == nil {
			slices.Reverse(tail)
			joined := filepath.Join(append([]string{realParent}, tail...)...)
			rel, ok := isWithinCleanRoot(realRoot, joined)
			if !ok {
				return "", "", fmt.Errorf("editor: %s resolves outside %s", absCandidate, realRoot)
			}
			return filepath.ToSlash(rel), joined, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		cur = parent
	}
}

// resolveRepoScenePath validates scenePath against repoRoot (and optional projectDir), refusing
// traversal ("..") or symlinks that escape repoRoot.
func resolveRepoScenePath(repoRoot, projectDir, scenePath string) (string, string, error) {
	if repoRoot == "" {
		return "", "", errors.New("editor: empty repository directory")
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		realRoot = absRoot
	}
	if strings.TrimSpace(scenePath) == "" {
		return "", "", errors.New("editor: empty scene path")
	}
	if filepath.IsAbs(scenePath) {
		rel, abs, err := resolvePathWithinRoot(realRoot, scenePath)
		if err != nil {
			return "", "", fmt.Errorf("editor: %s is outside Git repository %s", scenePath, repoRoot)
		}
		return rel, abs, nil
	}
	if filepath.VolumeName(scenePath) != "" || strings.Contains(scenePath, `\`) {
		return "", "", fmt.Errorf("editor: invalid scene path %q", scenePath)
	}
	cleaned := path.Clean(scenePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", "", fmt.Errorf("editor: %s is outside Git repository %s", scenePath, repoRoot)
	}

	// Try project-relative path first when inside a nested project directory.
	if projectDir != "" {
		projCandidate := filepath.Join(projectDir, filepath.FromSlash(cleaned))
		if rel, abs, perr := resolvePathWithinRoot(realRoot, projCandidate); perr == nil {
			if _, serr := os.Lstat(projCandidate); serr == nil {
				return rel, abs, nil
			}
			// If repo-root path does not exist either, still prefer the project-relative resolution.
			repoCandidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
			if _, rserr := os.Lstat(repoCandidate); errors.Is(rserr, fs.ErrNotExist) {
				return rel, abs, nil
			}
		}
	}

	repoCandidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
	rel, abs, err := resolvePathWithinRoot(realRoot, repoCandidate)
	if err != nil {
		return "", "", fmt.Errorf("editor: %s is outside Git repository %s", scenePath, repoRoot)
	}
	return rel, abs, nil
}

// OpenMergeConflict opens the object-by-object Scene Merge Conflict screen for an unmerged
// scene file in a Git repository. scenePath may be empty to open the first conflicted scene
// detected in the open project, a slash-separated path relative to the repository/project root,
// or an absolute OS path inside the repository.
func (e *Editor) OpenMergeConflict(scenePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.Playing() {
		return ErrPlaying
	}
	searchDir := e.gitProjectDir()
	if scenePath != "" && filepath.IsAbs(scenePath) {
		searchDir = filepath.Dir(scenePath)
	}
	if searchDir == "" {
		return errors.New("editor: open a project or pass an absolute scene path to inspect Git merge conflicts")
	}
	repoRoot, err := e.runGit(searchDir, "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		return fmt.Errorf("editor: %s is not inside a Git repository", searchDir)
	}
	if _, err := e.runGit(repoRoot, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return errors.New("editor: no Git merge is currently in progress")
	}

	projectDir := ""
	if e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}
	relPath := ""
	if scenePath == "" {
		conflicts, err := e.detectSceneMergeConflictsIn(repoRoot)
		if err != nil {
			return err
		}
		if len(conflicts) == 0 {
			return errors.New("editor: no conflicted scene files found in the active Git merge")
		}
		relPath, _, err = resolveRepoScenePath(repoRoot, "", conflicts[0])
		if err != nil {
			return err
		}
	} else {
		relPath, _, err = resolveRepoScenePath(repoRoot, projectDir, scenePath)
		if err != nil {
			return err
		}
	}

	// Read Git index stages :1: (base), :2: (ours/mine), :3: (theirs).
	var baseDoc *sceneformat.SceneDoc
	if baseBytes, berr := e.runGitRaw(repoRoot, "show", ":1:"+relPath); berr == nil && len(bytes.TrimSpace(baseBytes)) > 0 {
		baseDoc, err = sceneformat.ParseWith(baseBytes, sceneformat.ParseOptions{File: relPath + " (base)", Components: e.components})
		if err != nil {
			return fmt.Errorf("editor: parse merge base %s: %w", relPath, err)
		}
	}
	mineBytes, err := e.runGitRaw(repoRoot, "show", ":2:"+relPath)
	if err != nil {
		return fmt.Errorf("editor: read ours stage for %s: %w", relPath, err)
	}
	mineDoc, err := sceneformat.ParseWith(mineBytes, sceneformat.ParseOptions{File: relPath + " (ours)", Components: e.components})
	if err != nil {
		return fmt.Errorf("editor: parse ours %s: %w", relPath, err)
	}
	theirsBytes, err := e.runGitRaw(repoRoot, "show", ":3:"+relPath)
	if err != nil {
		return fmt.Errorf("editor: read theirs stage for %s: %w", relPath, err)
	}
	theirsDoc, err := sceneformat.ParseWith(theirsBytes, sceneformat.ParseOptions{File: relPath + " (theirs)", Components: e.components})
	if err != nil {
		return fmt.Errorf("editor: parse theirs %s: %w", relPath, err)
	}

	mineMeta := e.readGitSideMeta(repoRoot, "HEAD", "")
	theirsBranch := e.detectMergeTheirsBranch(repoRoot)
	theirsMeta := e.readGitSideMeta(repoRoot, "MERGE_HEAD", theirsBranch)

	e.refreshDetectedMergeConflicts()

	return e.OpenMergeConflictSession(MergeConflictSessionConfig{
		RepoDir:   repoRoot,
		ScenePath: relPath,
		Mine:      mineMeta,
		Theirs:    theirsMeta,
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	})
}

func (e *Editor) readGitSideMeta(repoRoot, ref, branchOverride string) MergeSideMeta {
	meta := MergeSideMeta{Branch: branchOverride}
	if meta.Branch == "" {
		if ref == "HEAD" {
			if b, err := e.runGit(repoRoot, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "" {
				meta.Branch = b
			}
		} else {
			if b, err := e.runGit(repoRoot, "name-rev", "--name-only", "--no-undefined", ref); err == nil && b != "" {
				meta.Branch = strings.TrimPrefix(b, "remotes/")
			}
		}
	}
	if meta.Branch == "" {
		meta.Branch = ref
	}
	if info, err := e.runGit(repoRoot, "log", "-1", "--format=%an%n%h%n%s", ref); err == nil && info != "" {
		parts := strings.SplitN(info, "\n", 3)
		if len(parts) >= 1 {
			meta.Author = strings.TrimSpace(parts[0])
		}
		if len(parts) >= 2 {
			meta.Commit = strings.TrimSpace(parts[1])
		}
		if len(parts) >= 3 {
			meta.Message = strings.TrimSpace(parts[2])
		}
	}
	return meta
}

func (e *Editor) detectMergeTheirsBranch(repoRoot string) string {
	gitDir, err := e.runGit(repoRoot, "rev-parse", "--git-dir")
	if err == nil && gitDir != "" {
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(repoRoot, gitDir)
		}
		if msg, rerr := os.ReadFile(filepath.Join(gitDir, "MERGE_MSG")); rerr == nil {
			if branch := parseMergeBranchFromMsg(string(msg)); branch != "" {
				return branch
			}
		}
	}
	if b, err := e.runGit(repoRoot, "name-rev", "--name-only", "--no-undefined", "MERGE_HEAD"); err == nil && b != "" {
		return strings.TrimPrefix(b, "remotes/")
	}
	return "MERGE_HEAD"
}

func parseMergeBranchFromMsg(msg string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	// Common Git merge subjects:
	//   Merge branch 'main' into feat/slime-patrol
	//   Merge remote-tracking branch 'origin/main' into feat/slime-patrol
	//   Merge branch 'main'
	start := strings.IndexByte(firstLine, '\'')
	if start >= 0 {
		if end := strings.IndexByte(firstLine[start+1:], '\''); end > 0 {
			return firstLine[start+1 : start+1+end]
		}
	}
	return ""
}

func (e *Editor) stageAndCommitMerge(repoRoot, relPath string) (sha string, staged bool, err error) {
	if out, err := e.runGit(repoRoot, "add", "--", relPath); err != nil {
		return "", false, fmt.Errorf("git add %s: %w (%s)", relPath, err, out)
	}
	// Check if any other unmerged files remain before creating the merge commit.
	remOut, err := e.runGit(repoRoot, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return "", true, fmt.Errorf("git diff unmerged: %w", err)
	}
	if strings.TrimSpace(remOut) != "" {
		return "", true, nil
	}
	if _, err := e.runGit(repoRoot, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		// Not in a merge state anymore; return current HEAD short SHA.
		sha, _ := e.runGit(repoRoot, "rev-parse", "--short", "HEAD")
		return sha, true, nil
	}
	if out, err := e.runGit(repoRoot, "commit", "--no-edit"); err != nil {
		return "", true, fmt.Errorf("git commit --no-edit: %w (%s)", err, out)
	}
	sha, err = e.runGit(repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", true, fmt.Errorf("git rev-parse --short HEAD: %w", err)
	}
	return sha, true, nil
}

func (e *Editor) abortGitMerge(repoRoot string) error {
	if _, err := e.runGit(repoRoot, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return nil
	}
	if out, err := e.runGit(repoRoot, "merge", "--abort"); err != nil {
		return fmt.Errorf("git merge --abort: %w (%s)", err, out)
	}
	return nil
}

// isSceneConflictFile reports whether data decodes as a valid sceneformat.ConflictDoc.
func isSceneConflictFile(data []byte) bool {
	_, err := sceneformat.ParseConflict(data)
	return err == nil
}
