// Package library finds shared asset packs, editor plugin packs and templates in a library and installs them into a game project.
//
// # The Index
//
// A library is a text index anyone can add a pack to with a pull request: a versioned YAML file (index.yaml, IndexFileName) listing each pack's name, kind, version, description, tags, license, source and digest. ParseIndex refuses unknown fields, other versions, duplicate names and sources that leave the library, and EncodeIndex writes the deterministic form ParseIndex reads back unchanged. Index.Search finds packs by kind and by words in their names, descriptions and tags.
//
// # Packs
//
// A pack is a directory holding a manifest, pack.yaml (ManifestFileName), and the files it lists. The manifest names the pack, gives its kind (asset, plugin or template, as project.PackKinds names them), its version and license, the project directory its files go to (into, by default packs/<name>), and the references installing it adds to game.yaml: autoload scenes and resources, and, for a plugin pack, the editor plugins it turns on. A pack's digest (Digest) is the SHA-256 of a sha256sum-style listing of its files, pack.yaml included, so the index pins the exact bytes of every file whatever form they were fetched in.
//
// # Fetching
//
// Open reads an index from a directory, a file or an http(s) URL, and Library.Fetch fetches a pack from its source, relative to the index or an absolute URL, file by file as its manifest lists them, and refuses one whose digest, name, kind or version differs from its index entry. Plain http is accepted only for a loopback host; anything else must use https, and every file is limited to MaxFileSize bytes.
//
// # Installing
//
// PlanInstall works out what installing a fetched pack does to a project, without writing anything: the files to write and remove, and the project.File afterwards, which records the pack under packs with the files it wrote and the references it added. Installing a pack again updates it, replacing its files and references with the new version's; a file that is already in the project and is not the pack's is refused rather than overwritten. PlanRemove undoes an install. Change.Apply then writes the files and game.yaml, restoring what it changed when a write fails. Install and Remove do both for a project directory, and the game library command and the scene editor's library panel run them.
package library
