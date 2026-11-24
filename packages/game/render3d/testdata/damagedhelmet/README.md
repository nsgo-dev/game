# Damaged Helmet test fixture

The Khronos [Damaged Helmet](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/DamagedHelmet) sample (theblueturtle_, 2016; ctxwing, 2018), used by `render3d`'s glTF material tests. Its model files are licensed CC-BY-NC-4.0 together with CC-BY-4.0 (see `LICENSE.md`), so they are not covered by this repository's Apache-2.0 license and may not be used commercially.

`glTF/DamagedHelmet.gltf` and `glTF/DamagedHelmet.bin` are unmodified. The five JPEG maps are downscaled to 256×256 to keep the repository small and the headless tests fast. `reference.png` and `reference_ibl.png` are the tests' renders of the model, so they are derivatives of the model under the same licenses.

The `go.mod` in this directory makes it a separate module, which keeps every file here out of the `github.com/nsgo-dev/game/packages/game` module that the Go proxy distributes. The tests that need these files skip when the directory is absent, as it is in a downloaded copy of the module.
