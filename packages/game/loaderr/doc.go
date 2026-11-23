// Package loaderr is the error that every loader and validator in this module returns for a file it cannot use: which file, which line and column, which field, what was wrong, what was expected and how to fix it, so a person or an agent reading the message can act on it without opening the loader's source.
//
// A validator that walks a decoded document does not know lines, so it names the field instead, with Errorf and a path built by Path ("layers[2].objects[0].id"). The loader's entry point then passes every error through Locate with the file name and the bytes it decoded: Locate finds the field's line and column in the YAML or JSON text, turns encoding/json and YAML decode errors (syntax, unknown fields, wrong types) into an Error with their line and a fix, and fills in the file name. A loader that is handed bytes rather than a path does not know the file's name; the caller that read the file names it with WithFile.
//
// See docs/en/errors.md for the guideline loaders follow.
package loaderr
