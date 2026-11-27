// Package docpkg is a fixture for PackageDocs.
package docpkg

// Point is a place.
//
// It has two paragraphs.
type Point struct {
	// X and Y are the coordinates,
	// written on two lines.
	X float32
	Y float32
	// Label names it.
	Label string
	Other string
}

type (
	// Grouped is declared in a group.
	Grouped struct{}
)
