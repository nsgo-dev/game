// Package perf provides allocation-free game-loop performance primitives built
// on NSGo's public core types and the game sprite package:
//
//   - Deterministic object pools ([Pool], [Arena], [SlicePool]) that reuse
//     memory across frames without dropping entries during garbage collection.
//   - 2D broad-phase spatial indices ([SpatialHash] and [Quadtree]) for O(k)
//     area queries, point queries, and collision pair generation.
//   - Viewport culling helpers ([Intersects], [SpriteBounds], [CullSprites],
//     [CullBatch], [BatchCuller], and generic [Cull]) to skip off-screen work.
package perf
