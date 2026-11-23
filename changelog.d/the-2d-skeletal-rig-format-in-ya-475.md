### Changed

- `game/skeletal`: rigs are YAML (`.skel.yaml`, same version-1 schema, JSON that is also valid YAML still loads); `Load` refuses unknown fields, duplicate keys, trailing documents, non-finite numbers, times beyond `MaxSeconds` and Bezier points on other curves, and the new `Encode` writes a deterministic canonical form (names in byte order, numbers without exponents) that round-trips losslessly (#475).
