// Package scene3d provides a 3D scene graph, named socket attachments, spatial
// queries, perspective and orthographic cameras, camera controllers (orbit,
// first-person, third-person follow), and screen-space picking built on math3d.
//
// # Conventions
//
// All transforms, projections, frustums, and rays follow package math3d's
// conventions:
//
//   - Right-handed, Y-up coordinate system (+X right, +Y up, -Z forward).
//   - Column-major matrices with TRS composition (T * R * S) and clip-space
//     transform P * V * M.
//   - Zero-to-one [0, 1] NDC depth (WebGPU / Metal / Vulkan).
//   - Screen coordinates place (0, 0) at the top-left of the viewport and
//     (viewW, viewH) at the bottom-right.
//
// Construct nodes with [NewNode] and scenes with [NewScene] so default
// transforms, visibility, layers, and empty bounding volumes are initialized
// cleanly.
package scene3d
