package editor

import "github.com/nsgo-dev/game/packages/game/project"

// syncProjectAssets keeps the project.Resolver that ProjectAssets set up in step with the editor: it finds atlases nearest the open scene (ScenePath), and lists the project's atlases again once the editor has changed the project's files, such as by installing a pack of them. It reports whether the resolver now finds atlases from another scene, which the built scene must be rebuilt for; it does nothing without ProjectAssets or when Resolver is not a project.Resolver.
func (e *Editor) syncProjectAssets() bool {
	r, ok := e.Resolver.(*project.Resolver)
	if !e.ProjectAssets || !ok {
		return false
	}
	if e.assetsFilesRev != e.filesRev {
		r.Refresh()
		e.assetsFilesRev = e.filesRev
	}
	if e.assetsScene == e.ScenePath {
		return false
	}
	e.Resolver = r.ForScene(e.ScenePath)
	e.assetsScene = e.ScenePath
	return true
}
