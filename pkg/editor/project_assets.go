package editor

import "github.com/nsgo-dev/game/pkg/project"

// syncProjectAssets keeps the project.Resolver that ProjectAssets set up in step with the editor: it finds atlases nearest the open scene (ScenePath), and reads the project's atlases and materials again (project.Resolver.Refresh) once the editor has made a change that may add, remove or rewrite them, one that bumps assetsRev, such as installing a pack, moving files, ReloadAssets or RefreshSearch. Saving a scene, which writes none of them, keeps what the resolver has loaded. It reports whether the resolver now finds atlases from another scene, which the built scene must be rebuilt for; it does nothing without ProjectAssets or when Resolver is not a project.Resolver.
func (e *Editor) syncProjectAssets() bool {
	r, ok := e.Resolver.(*project.Resolver)
	if !e.ProjectAssets || !ok {
		return false
	}
	if e.assetsResolverRev != e.assetsRev {
		r.Refresh()
		e.assetsResolverRev = e.assetsRev
	}
	if e.assetsScene == e.ScenePath {
		return false
	}
	e.Resolver = r.ForScene(e.ScenePath)
	e.assetsScene = e.ScenePath
	return true
}
