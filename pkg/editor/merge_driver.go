package editor

import (
	"errors"

	"github.com/nsgo-dev/game/internal/scenemerge"
)

type mergeDriverState struct {
	needed     bool
	dismissed  bool
	repoRoot   string
	projectDir string
	attrPath   string
	errMsg     string
	rev        uint64
}

func (e *Editor) mergeDriverRev() uint64 {
	if e == nil {
		return 0
	}
	return e.mergeDriver.rev
}

func (e *Editor) resetMergeDriverForProject() {
	if e == nil {
		return
	}
	e.mergeDriver = mergeDriverState{rev: e.mergeDriver.rev + 1}
}

// SceneMergeDriverSetupNeeded reports whether the open project's Git repository routes
// scene files to merge=nsgo-scene in .gitattributes without merge.nsgo-scene.driver configured.
func (e *Editor) SceneMergeDriverSetupNeeded() bool {
	return e != nil && e.mergeDriver.needed
}

// SceneMergeDriverBannerVisible reports whether the one-click scene merge driver setup banner should be displayed.
func (e *Editor) SceneMergeDriverBannerVisible() bool {
	return e != nil && e.mergeDriver.needed && !e.mergeDriver.dismissed
}

// SceneMergeDriverError returns the error from the last failed SetupSceneMergeDriver attempt, if any.
func (e *Editor) SceneMergeDriverError() string {
	if e == nil {
		return ""
	}
	return e.mergeDriver.errMsg
}

// RefreshSceneMergeDriverStatus checks whether the open project's Git repository
// has .gitattributes naming nsgo-scene without a configured merge.nsgo-scene.driver.
func (e *Editor) RefreshSceneMergeDriverStatus() {
	if e == nil {
		return
	}
	dir := e.gitProjectDir()
	if dir == "" {
		if e.mergeDriver.needed || e.mergeDriver.repoRoot != "" || e.mergeDriver.projectDir != "" || e.mergeDriver.errMsg != "" {
			e.mergeDriver.needed = false
			e.mergeDriver.dismissed = false
			e.mergeDriver.repoRoot = ""
			e.mergeDriver.projectDir = ""
			e.mergeDriver.attrPath = ""
			e.mergeDriver.errMsg = ""
			e.mergeDriver.rev++
			e.notify()
		}
		return
	}
	status, err := scenemerge.Check(dir, e.runGit)
	if err != nil {
		return
	}
	needed := status.NeedsSetup()
	changedProject := e.mergeDriver.projectDir != dir
	if changedProject {
		e.mergeDriver.dismissed = false
		e.mergeDriver.errMsg = ""
	}
	changed := changedProject ||
		e.mergeDriver.needed != needed ||
		e.mergeDriver.repoRoot != status.RepoRoot ||
		e.mergeDriver.attrPath != status.AttributesPath
	e.mergeDriver.needed = needed
	e.mergeDriver.repoRoot = status.RepoRoot
	e.mergeDriver.projectDir = dir
	e.mergeDriver.attrPath = status.AttributesPath
	if !needed {
		e.mergeDriver.errMsg = ""
	}
	if changed {
		e.mergeDriver.rev++
		e.notify()
	}
}

// SetupSceneMergeDriver configures merge.nsgo-scene.name and merge.nsgo-scene.driver
// in the open project's local Git config and clears the setup banner.
func (e *Editor) SetupSceneMergeDriver() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	dir := e.mergeDriver.projectDir
	if dir == "" {
		dir = e.gitProjectDir()
	}
	if dir == "" {
		return errors.New("editor: no project inside a Git repository")
	}
	if err := scenemerge.ConfigureDriver(dir, e.runGit); err != nil {
		e.mergeDriver.errMsg = err.Error()
		e.mergeDriver.rev++
		e.notify()
		return err
	}
	e.mergeDriver.needed = false
	e.mergeDriver.dismissed = false
	e.mergeDriver.errMsg = ""
	e.Status = e.T("mergeDriver.configuredStatus")
	e.mergeDriver.rev++
	e.notify()
	return nil
}

// DismissSceneMergeDriverBanner hides the merge driver setup banner for the current project session.
func (e *Editor) DismissSceneMergeDriverBanner() {
	if e == nil || !e.mergeDriver.needed || e.mergeDriver.dismissed {
		return
	}
	e.mergeDriver.dismissed = true
	e.mergeDriver.rev++
	e.notify()
}
