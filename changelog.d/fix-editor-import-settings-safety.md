### Fixed

- Memoize the asset import inspector and stop static tooltips from subscribing `InspectorView` to `rev`, preserving unsaved credit/font drafts and focus across unrelated updates; guard atlas `meta.image` and target output paths against path traversal during `ReloadAssets`; report the resolved multi-page atlas JSON path (`name-0.json`) in `AssetReloader` callbacks; and clamp non-finite numeric input conversions (#915).
