// Package gltf implements a pure-Go glTF 2.0 (.gltf) and binary GLB (.glb)
// asset loader using only the Go standard library and packages/game/math3d.
//
// Supported features:
//   - Auto-detection of JSON .gltf and binary .glb containers via Load and LoadFS
//   - Embedded base64 data URIs, GLB BIN chunks, and external fs.FS buffers/images
//   - Full typed accessor decoding (SCALAR, VEC2, VEC3, VEC4, MAT4, indices,
//     joints, weights) with all component types, normalized integer unpacking,
//     interleaved byteStride buffers, zero-initialized accessors, and sparse accessors
//   - Scene graph and node hierarchy with matrix/TRS transforms, cycle detection,
//     and world matrix computation
//   - PBR metallic-roughness materials, textures, samplers, PNG/JPEG image decoding,
//     KHR_materials_unlit, KHR_texture_transform, and KHR_materials_variants
//   - Mesh primitives with bounds, morph targets with CPU position/normal blending,
//     skins with inverse bind matrices, and STEP/LINEAR/CUBICSPLINE keyframe animations
package gltf
