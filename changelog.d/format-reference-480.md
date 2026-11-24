### Added

- Docs: a File Formats page (`docs/en/formats.md`, `docs/zh/formats.md`) listing every format a game reads or writes, with its files, version, the Go functions that read and write it, the guide to its fields and an example; each YAML format's conformance check now names its entry in `formattest.Format.Doc` and loads and saves the entry's examples, and `TestFormatsPage` fails when a format or a package that decodes JSON is missing from the page or the two languages differ (#480).
