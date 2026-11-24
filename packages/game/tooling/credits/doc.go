// Package credits lists who made each of a game's assets and under what license, from the credit section of the import settings sidecar beside each one (package importsettings), and writes the list as the credits file a release ships with.
//
// Assets returns the assets game.yaml's assets section (package project) reaches: the images each atlas packs, the image of each texture target, the sound of each audio target, and every file in the manifest's folder that the manifest lists, except the files the targets write, which are made from those inputs. Collect reads each one's sidecar and returns its credit, with a *MissingError naming every asset that has no sidecar or a sidecar without a credit. Markdown writes the entries as FileName, CREDITS.md: a table of each asset's author, license and source, a count of the assets under each license, and each notice a license requires to be shipped, word for word.
//
// game release writes the file beside the packages it builds, and refuses to build when an asset has no credit; game credits writes or prints it on its own.
package credits
