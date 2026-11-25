// Package scenemodel gives the objects of a scene file their 3D look: the built-in primitive an object names in mesh, or the glTF 2.0 model it names in model, optionally narrowed to one node (modelNode) or one mesh (modelMesh), with the material file it names in material replacing the model's materials (sceneformat.ObjectDoc). So a 3D level can be written as a scene file, as a 2D one is, instead of being assembled in Go.
//
// Build builds a scene into a scene3d node tree, as sceneformat.BuildNodes3D does, loads each model through a ModelResolver, as sceneformat.Build loads materials through a MaterialResolver, and attaches each object's geometry and materials to its node as a Drawable, with the node's LocalBounds set from the geometry. Assets is a ModelResolver, MaterialResolver and ImageResolver over a project's files. Built.AppendDrawItems lists the parts as render3d.DrawItem values for render3d.Queue, and Renderer uploads them to a gpu.Device and draws them lit by a render3d.LightRig and textured with their base color.
//
//	assets := scenemodel.NewAssets(os.DirFS(projectRoot))
//	built, err := scenemodel.Build(doc, scene.Root(), assets)
//	...
//	r, err := scenemodel.NewRenderer(cx, dev, built, scenemodel.RendererOptions{})
//	...
//	// Each frame:
//	r.Prepare(cam, lights)
//	pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
//	r.Draw(pass)
//	pass.End()
//
// game check reports a model file that is missing or does not load, and a modelNode or modelMesh the model does not have, with the scene file and line; game mv, game index and the editor's MoveFile rewrite the scenes that name a model when it moves (package refs).
package scenemodel
