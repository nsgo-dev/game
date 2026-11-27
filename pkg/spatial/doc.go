// Package spatial finds the 2D objects near an area or inside a view without testing every one, built on NSGo's public core types and the game sprite package:
//
//   - Broad-phase indices ([Hash] and [Quadtree]) for O(k) area queries, point queries and collision pair generation, allocation-free once warm.
//   - Viewport culling helpers ([Intersects], [SpriteBounds], [CullSprites], [CullBatch], [BatchCuller] and generic [Cull]) to skip off-screen work.
package spatial
