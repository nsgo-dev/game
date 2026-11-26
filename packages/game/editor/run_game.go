package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrGameRunning is returned by RunGame and RunGameWith when a game process is already running.
var ErrGameRunning = errors.New("editor: game is already running")

// ErrRunGameUnsupported is returned by RunGame and RunGameWith on non-desktop targets when RunGameCommand is nil.
var ErrRunGameUnsupported = errors.New("editor: running the game is only supported on desktop")

// RunGameOptions configures launching the open project with `go tool game dev`.
type RunGameOptions struct {
	// StartAtOpenScene passes the currently open scene (when it belongs to the open project) to `game dev -scene <path>` and sets hotreload.SceneEnv in the child environment.
	StartAtOpenScene bool
	// Automation passes `-automation` to `game dev` and connects the editor's remote scene tree and inspector to the running game over MCP.
	Automation bool
	// ExtraArgs are optional extra arguments appended after `go tool game dev` (and `-scene <path>` when set).
	ExtraArgs []string
}

// RunGameSpec describes the child process started by RunGame / RunGameWith.
type RunGameSpec struct {
	// Dir is the open project's root directory where the command executes.
	Dir string
	// Name is the executable to run ("go").
	Name string
	// Args are the command arguments (for example ["tool", "game", "dev"]).
	Args []string
	// Env is the full process environment passed to the child process.
	Env []string
	// Scene is the slash-separated project-relative path of the open scene when StartAtOpenScene is active, or empty.
	Scene string
}

// GameProcess represents a running child game process owned by the editor.
type GameProcess interface {
	// Wait blocks until the process exits and returns its exit error, if any.
	Wait() error
	// Stop terminates the process and every child process it started.
	Stop() error
}

type gameRun struct {
	proc     GameProcess
	spec     RunGameSpec
	done     chan struct{}
	stopping atomic.Bool
	waitErr  error
}

type gameRunState struct {
	mu               sync.Mutex
	active           *gameRun
	lastErr          error
	startAtOpenScene bool
	automation       bool
	scope            *core.Scope
}

func (g *gameRunState) setScope(cx *core.Scope) {
	g.mu.Lock()
	g.scope = cx
	g.mu.Unlock()
}

// GameRunning reports whether a game child process started by RunGame or RunGameWith is currently running. It is safe to call from any goroutine.
func (e *Editor) GameRunning() bool {
	if e == nil {
		return false
	}
	e.gameRun.mu.Lock()
	defer e.gameRun.mu.Unlock()
	return e.gameRun.active != nil
}

// RunStartAtOpenScene reports whether RunGame (and the toolbar Run button / F5 shortcut) starts the game at the currently open scene when one is open inside the project.
func (e *Editor) RunStartAtOpenScene() bool {
	if e == nil {
		return false
	}
	e.gameRun.mu.Lock()
	defer e.gameRun.mu.Unlock()
	return e.gameRun.startAtOpenScene
}

// SetRunStartAtOpenScene toggles whether RunGame starts the game at the currently open scene.
func (e *Editor) SetRunStartAtOpenScene(startAtOpen bool) {
	if e == nil {
		return
	}
	e.gameRun.mu.Lock()
	changed := e.gameRun.startAtOpenScene != startAtOpen
	e.gameRun.startAtOpenScene = startAtOpen
	e.gameRun.mu.Unlock()
	if changed {
		e.notify()
	}
}

// RunWithAutomation reports whether RunGame passes `-automation` and connects the editor's Remote tab and inspector to the running game over MCP.
func (e *Editor) RunWithAutomation() bool {
	if e == nil {
		return false
	}
	e.gameRun.mu.Lock()
	defer e.gameRun.mu.Unlock()
	return e.gameRun.automation
}

// SetRunWithAutomation toggles whether RunGame launches the game with `-automation` and connects the editor's Remote inspector.
func (e *Editor) SetRunWithAutomation(on bool) {
	if e == nil {
		return
	}
	e.gameRun.mu.Lock()
	changed := e.gameRun.automation != on
	e.gameRun.automation = on
	e.gameRun.mu.Unlock()
	if changed {
		e.notify()
	}
}

// RunGame launches `go tool game dev` in the open project's directory using the editor's RunStartAtOpenScene and RunWithAutomation settings.
func (e *Editor) RunGame() error {
	return e.RunGameWith(RunGameOptions{
		StartAtOpenScene: e.RunStartAtOpenScene(),
		Automation:       e.RunWithAutomation(),
	})
}

// RunGameWith launches `go tool game dev` in the open project's directory as a child process on desktop (or through Editor.RunGameCommand when set), streaming its standard output and error into the Output panel off the UI goroutine. Call StopGame or Close to terminate it and every process it started.
func (e *Editor) RunGameWith(opts RunGameOptions) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}

	e.gameRun.mu.Lock()
	if e.gameRun.active != nil {
		e.gameRun.mu.Unlock()
		return ErrGameRunning
	}
	e.gameRun.mu.Unlock()

	runner := e.RunGameCommand
	if runner == nil {
		if !canRunGameDesktop {
			return ErrRunGameUnsupported
		}
		runner = startGameProcess
	}

	projectDir := filepath.Dir(e.ProjectPath)
	args := []string{"tool", "game", "dev"}
	env := slices.Clone(os.Environ())
	var scenePath string
	if opts.StartAtOpenScene {
		scenePath = e.openRunSceneRelPath()
		if scenePath != "" {
			args = append(args, "-scene", scenePath)
			env = setEnvVar(env, hotreload.SceneEnv, scenePath)
		}
	}
	hasAutomationArg := slices.Contains(opts.ExtraArgs, "-automation") || slices.Contains(opts.ExtraArgs, "--automation")
	if opts.Automation && !hasAutomationArg {
		args = append(args, "-automation")
	}
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	connectRemote := opts.Automation || hasAutomationArg

	spec := RunGameSpec{
		Dir:   projectDir,
		Name:  "go",
		Args:  args,
		Env:   env,
		Scene: scenePath,
	}

	stdoutWriter := newOutputStreamWriter(func(line string) {
		e.appendGameOutput(OutputStdout, OutputInfo, line, projectDir)
	})
	stderrWriter := newOutputStreamWriter(func(line string) {
		e.appendGameOutput(OutputStderr, OutputError, line, projectDir)
	})

	proc, err := runner(spec, stdoutWriter, stderrWriter)
	if err != nil {
		e.reportStatusError("Run error: " + err.Error())
		return err
	}

	run := &gameRun{
		proc: proc,
		spec: spec,
		done: make(chan struct{}),
	}
	e.gameRun.mu.Lock()
	e.gameRun.active = run
	e.gameRun.lastErr = nil
	e.gameRun.mu.Unlock()

	e.output.mu.Lock()
	e.output.open = true
	e.output.runCount++
	e.output.runLines = nil
	e.output.mu.Unlock()

	startMsg := fmt.Sprintf("Running game: %s %s", spec.Name, strings.Join(spec.Args, " "))
	e.appendOutputEntry(OutputEditor, OutputInfo, startMsg, projectDir)
	e.setStatusWithoutLogging("Running game")
	e.notify()

	if connectRemote {
		e.startRemoteAutoConnect(spec, run.done)
	}

	go func() {
		waitErr := proc.Wait()
		stdoutWriter.Flush()
		stderrWriter.Flush()
		if connectRemote {
			_ = e.disconnectRemote(true)
		}

		stoppedByUser := run.stopping.Load()
		var exitStatus string
		var exitLevel OutputLevel
		switch {
		case stoppedByUser:
			exitStatus = "Game stopped"
			exitLevel = OutputInfo
		case waitErr != nil:
			exitStatus = "Game exited with error: " + waitErr.Error()
			exitLevel = OutputError
		default:
			exitStatus = "Game exited"
			exitLevel = OutputInfo
		}

		e.appendOutputEntry(OutputEditor, exitLevel, exitStatus, projectDir)

		e.gameRun.mu.Lock()
		run.waitErr = waitErr
		if e.gameRun.active == run {
			e.gameRun.active = nil
		}
		if !stoppedByUser {
			e.gameRun.lastErr = waitErr
		} else {
			e.gameRun.lastErr = nil
		}
		scope := e.gameRun.scope
		e.gameRun.mu.Unlock()

		close(run.done)

		if scope != nil {
			scope.Post(func() {
				e.setStatusWithoutLogging(exitStatus)
				e.notify()
			})
		}
	}()

	return nil
}

// StopGame stops the running game child process and every process it started, waiting until the process exits and all streamed output has been flushed. It is safe to call when no game is running.
func (e *Editor) StopGame() error {
	if e == nil {
		return nil
	}
	e.gameRun.mu.Lock()
	run := e.gameRun.active
	e.gameRun.mu.Unlock()
	if run == nil {
		return nil
	}
	run.stopping.Store(true)
	_ = e.DisconnectRemote()
	stopErr := run.proc.Stop()
	<-run.done
	e.setStatusWithoutLogging("Game stopped")
	e.notify()
	return stopErr
}

// WaitGame blocks until the currently running game process exits and returns its exit error (nil when stopped via StopGame or when idle).
func (e *Editor) WaitGame() error {
	if e == nil {
		return nil
	}
	e.gameRun.mu.Lock()
	run := e.gameRun.active
	e.gameRun.mu.Unlock()
	if run != nil {
		<-run.done
	}
	e.gameRun.mu.Lock()
	err := e.gameRun.lastErr
	e.gameRun.mu.Unlock()
	return err
}

// Close releases background resources owned by the editor, stopping any running game process, profiler client, export build, clip playback, audio audition, or play-in-editor loop.
func (e *Editor) Close() error {
	if e == nil {
		return nil
	}
	e.CancelInspectorScrub()
	e.stopClipLoop()
	e.stopStatesLoop()
	e.stopCutsceneLoop()
	e.closeModelPreview()
	e.stopAudioAudition()
	e.stopChangedAudioPlayback()
	e.cancelChangesOps()
	e.stopExportBuild()
	e.stopReleaseBuild()
	e.CancelReleaseUpload()
	e.StopAndroidDeviceRun()
	webSrvErr := e.stopWebBuildServerInternal(false)
	e.stopAssetsBuild()
	e.stopProblemCheckTimer()
	e.stopDiskWatcher()
	e.stopAgentTimers()
	e.stopAutoSave()
	e.stopAllTaskRunners()
	e.cancelCreateProject()
	e.cancelFirstRunOps()
	var playErr error
	if e.Playing() {
		playErr = e.Stop()
	}
	e.stopAutoSave()
	profErr := e.DisconnectProfiler()
	remoteErr := e.DisconnectRemote()
	stopErr := e.StopGame()
	return errors.Join(stopErr, remoteErr, playErr, profErr, webSrvErr)
}

func (e *Editor) openRunSceneRelPath() string {
	if e == nil {
		return ""
	}
	if e.ScenePath != "" {
		return filepath.ToSlash(e.ScenePath)
	}
	if e.FilePath == "" || e.ProjectPath == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Dir(e.ProjectPath), e.FilePath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return ""
	}
	return rel
}

func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func (e *Editor) appendGameOutput(source OutputSource, level OutputLevel, line, projectDir string) {
	e.appendOutputEntry(source, level, line, projectDir)
	e.postOutputUI()
}

func (e *Editor) postOutputUI() {
	e.gameRun.mu.Lock()
	scope := e.gameRun.scope
	e.gameRun.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
	}
}
