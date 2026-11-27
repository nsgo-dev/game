package importsettings

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CollisionModes are the collision generation modes an image's sidecar may name:
// "alpha" traces the frame's alpha channel into convex polygons, "box" fits a tight axis-aligned box,
// "ellipse" fits an inscribed convex polygon, "polygon" uses explicit vertex coordinates, and "none" disables collision.
var CollisionModes = []string{"alpha", "box", "ellipse", "polygon", "none"}

// DefaultCollisionMaxVertices is the default maximum vertex count per baked convex collision polygon, matching physics.MaxPolygonVertices.
const DefaultCollisionMaxVertices = 8

// MinCollisionVertices is the minimum vertex count of a valid convex polygon.
const MinCollisionVertices = 3

// MaxCollisionVertices is the maximum vertex count of a baked convex collision polygon, matching physics.MaxPolygonVertices.
const MaxCollisionVertices = 8

// Point2D is an [x, y] coordinate in untrimmed frame pixels from the top-left corner, used for collision polygon vertices and named image points.
type Point2D [2]float64

// UnmarshalYAML decodes a [x, y] sequence of two finite numbers into p.
func (p *Point2D) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode || len(value.Content) != 2 {
		return fmt.Errorf("point must be a [x, y] sequence of 2 numbers")
	}
	for i, item := range value.Content {
		if item.Kind != yaml.ScalarNode || (item.ShortTag() != "!!int" && item.ShortTag() != "!!float") {
			return fmt.Errorf("point[%d] must be a number", i)
		}
		var v float64
		if err := item.Decode(&v); err != nil {
			return fmt.Errorf("point[%d]: %w", i, err)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("point[%d] must be a finite number", i)
		}
		if v == 0 {
			v = 0
		}
		p[i] = v
	}
	return nil
}

// MarshalYAML encodes p as a deterministic YAML flow sequence [x, y].
func (p Point2D) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for i, v := range p {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("point[%d] must be a finite number", i)
		}
		if v == 0 {
			v = 0
		}
		var elem yaml.Node
		if err := elem.Encode(v); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &elem)
	}
	return node, nil
}

// CollisionSpec is the resolved collision configuration for a single image or slice cell.
type CollisionSpec struct {
	// Mode is "alpha", "box", "ellipse", "polygon", or "none".
	Mode string
	// Tolerance is the alpha cutoff in [0, 255]; pixels with alpha > Tolerance are treated as solid.
	Tolerance int
	// MaxVertices limits how many vertices each baked convex polygon may have (3 to 8; 0 defaults to 8).
	MaxVertices int
	// Points holds explicit polygon vertices in untrimmed frame pixels when Mode is "polygon".
	Points []Point2D
}

// EffectiveMaxVertices returns MaxVertices when set, or DefaultCollisionMaxVertices (8) when 0.
func (s CollisionSpec) EffectiveMaxVertices() int {
	if s.MaxVertices == 0 {
		return DefaultCollisionMaxVertices
	}
	return s.MaxVertices
}

// Collision configures 2D collision shapes baked into packed atlas metadata when an atlas packs the image,
// as a default for every frame and optional per-cell overrides for a sliced sheet.
type Collision struct {
	// Mode selects how collision shapes are generated: "alpha" (traced from pixel alpha), "box", "ellipse", "polygon" (using Points), or "none".
	Mode string `yaml:"mode,omitempty" schema:"enum=alpha|box|ellipse|polygon|none"`
	// Tolerance is the alpha cutoff in [0, 255] for "alpha", "box", and "ellipse" modes: pixels with alpha > Tolerance are treated as solid (0 treats any non-transparent pixel as solid).
	Tolerance int `yaml:"tolerance,omitempty" schema:"minimum=0,maximum=255"`
	// MaxVertices limits how many vertices each baked convex polygon may have (3 to 8; 0 uses 8).
	MaxVertices int `yaml:"maxVertices,omitempty" schema:"minimum=0,maximum=8"`
	// Points lists explicit polygon vertices in untrimmed frame pixels when Mode is "polygon" (at least 3 points; concave outlines are split into convex parts).
	Points []Point2D `yaml:"points,omitempty"`
	// Cells overrides collision settings for individual 0-based cells of a sliced sprite sheet, in ascending cell index order.
	Cells []CellCollision `yaml:"cells,omitempty"`
}

// CellCollision overrides collision settings for one 0-based cell of a sliced sprite sheet.
type CellCollision struct {
	// Cell is the 0-based cell index (row by row from the top left), matching the frame suffix _<cell>.
	Cell int `yaml:"cell" schema:"required,minimum=0"`
	// Mode selects how collision shapes are generated for this cell: "alpha", "box", "ellipse", "polygon", or "none".
	Mode string `yaml:"mode" schema:"required,enum=alpha|box|ellipse|polygon|none"`
	// Tolerance is the alpha cutoff in [0, 255] for "alpha", "box", and "ellipse" modes.
	Tolerance int `yaml:"tolerance,omitempty" schema:"minimum=0,maximum=255"`
	// MaxVertices limits how many vertices each baked convex polygon may have (3 to 8; 0 uses 8).
	MaxVertices int `yaml:"maxVertices,omitempty" schema:"minimum=0,maximum=8"`
	// Points lists explicit polygon vertices in untrimmed frame pixels when Mode is "polygon".
	Points []Point2D `yaml:"points,omitempty"`
}

// UnmarshalYAML decodes a CellCollision mapping, requiring both cell and mode keys and refusing unknown or repeated keys.
func (c *CellCollision) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return errors.New("cell collision must be a mapping with cell and mode")
	}
	var hasCell, hasMode, hasTolerance, hasMaxVertices, hasPoints bool
	for i := 0; i+1 < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		switch k.Value {
		case "cell":
			if hasCell {
				return errors.New(`duplicate field "cell" in cell collision`)
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" {
				return errors.New("cell must be an integer")
			}
			if err := v.Decode(&c.Cell); err != nil {
				return fmt.Errorf("cell: %w", err)
			}
			hasCell = true
		case "mode":
			if hasMode {
				return errors.New(`duplicate field "mode" in cell collision`)
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
				return errors.New("mode must be a string")
			}
			if err := v.Decode(&c.Mode); err != nil {
				return fmt.Errorf("mode: %w", err)
			}
			hasMode = true
		case "tolerance":
			if hasTolerance {
				return errors.New(`duplicate field "tolerance" in cell collision`)
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" {
				return errors.New("tolerance must be an integer")
			}
			if err := v.Decode(&c.Tolerance); err != nil {
				return fmt.Errorf("tolerance: %w", err)
			}
			hasTolerance = true
		case "maxVertices":
			if hasMaxVertices {
				return errors.New(`duplicate field "maxVertices" in cell collision`)
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" {
				return errors.New("maxVertices must be an integer")
			}
			if err := v.Decode(&c.MaxVertices); err != nil {
				return fmt.Errorf("maxVertices: %w", err)
			}
			hasMaxVertices = true
		case "points":
			if hasPoints {
				return errors.New(`duplicate field "points" in cell collision`)
			}
			if err := v.Decode(&c.Points); err != nil {
				return fmt.Errorf("points: %w", err)
			}
			hasPoints = true
		default:
			return fmt.Errorf("field %s not found in type importsettings.CellCollision", k.Value)
		}
	}
	if !hasCell {
		return errors.New("cell is required in cell collision")
	}
	if !hasMode {
		return errors.New("mode is required in cell collision")
	}
	return nil
}

// ForImage returns the collision specification for an unsliced image, or false when no default mode is set (or mode is "none").
func (c *Collision) ForImage() (CollisionSpec, bool) {
	if c == nil || c.Mode == "" || c.Mode == "none" {
		return CollisionSpec{}, false
	}
	return CollisionSpec{
		Mode:        c.Mode,
		Tolerance:   c.Tolerance,
		MaxVertices: c.MaxVertices,
		Points:      slices.Clone(c.Points),
	}, true
}

// ForCell returns the effective collision specification for the 0-based sliced cell, preferring an explicit override in Cells and falling back to the default, or false when neither sets an active mode (or the effective mode is "none").
func (c *Collision) ForCell(cell int) (CollisionSpec, bool) {
	if c == nil {
		return CollisionSpec{}, false
	}
	for _, cc := range c.Cells {
		if cc.Cell == cell {
			if cc.Mode == "" || cc.Mode == "none" {
				return CollisionSpec{}, false
			}
			return CollisionSpec{
				Mode:        cc.Mode,
				Tolerance:   cc.Tolerance,
				MaxVertices: cc.MaxVertices,
				Points:      slices.Clone(cc.Points),
			}, true
		}
	}
	return c.ForImage()
}

// CellPoints overrides or adds named image points for one 0-based cell of a sliced sprite sheet.
type CellPoints struct {
	// Cell is the 0-based cell index (row by row from the top left), matching the frame suffix _<cell>.
	Cell int `yaml:"cell" schema:"required,minimum=0"`
	// Points maps point names (such as "muzzle" or "feet") to [x, y] pixel coordinates in the untrimmed cell, overriding any default point of the same name.
	Points map[string]Point2D `yaml:"points" schema:"required"`
}

// UnmarshalYAML decodes a CellPoints mapping, requiring both cell and points keys and refusing unknown or repeated keys.
func (c *CellPoints) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return errors.New("cell points must be a mapping with cell and points")
	}
	var hasCell, hasPoints bool
	for i := 0; i+1 < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		switch k.Value {
		case "cell":
			if hasCell {
				return errors.New(`duplicate field "cell" in cell points`)
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" {
				return errors.New("cell must be an integer")
			}
			if err := v.Decode(&c.Cell); err != nil {
				return fmt.Errorf("cell: %w", err)
			}
			hasCell = true
		case "points":
			if hasPoints {
				return errors.New(`duplicate field "points" in cell points`)
			}
			if v.Kind != yaml.MappingNode {
				return errors.New("points must be a mapping of name to [x, y]")
			}
			if err := v.Decode(&c.Points); err != nil {
				return fmt.Errorf("points: %w", err)
			}
			hasPoints = true
		default:
			return fmt.Errorf("field %s not found in type importsettings.CellPoints", k.Value)
		}
	}
	if !hasCell {
		return errors.New("cell is required in cell points")
	}
	if !hasPoints {
		return errors.New("points is required in cell points")
	}
	return nil
}

// PointsForImage returns a copy of im's default named points, or nil when none are set.
func (im *Image) PointsForImage() map[string]Point2D {
	if im == nil || len(im.Points) == 0 {
		return nil
	}
	return maps.Clone(im.Points)
}

// PointsForCell returns the effective named points for 0-based sliced cell, merging per-cell overrides on top of im.Points, or nil when none apply.
func (im *Image) PointsForCell(cell int) map[string]Point2D {
	if im == nil {
		return nil
	}
	var cellPts map[string]Point2D
	for _, cp := range im.CellPoints {
		if cp.Cell == cell {
			cellPts = cp.Points
			break
		}
	}
	if len(im.Points) == 0 && len(cellPts) == 0 {
		return nil
	}
	out := make(map[string]Point2D, len(im.Points)+len(cellPts))
	maps.Copy(out, im.Points)
	maps.Copy(out, cellPts)
	return out
}

func validateCollisionSpec(label string, mode string, tolerance, maxVertices int, points []Point2D) error {
	if !slices.Contains(CollisionModes, mode) {
		return fmt.Errorf("collision: %s mode %q must be %s", label, mode, strings.Join(CollisionModes, ", "))
	}
	if tolerance < 0 || tolerance > 255 {
		return fmt.Errorf("collision: %s tolerance %d must be between 0 and 255", label, tolerance)
	}
	if maxVertices != 0 && (maxVertices < MinCollisionVertices || maxVertices > MaxCollisionVertices) {
		return fmt.Errorf("collision: %s maxVertices %d must be 0 or between %d and %d", label, maxVertices, MinCollisionVertices, MaxCollisionVertices)
	}
	if mode == "polygon" {
		if len(points) < MinCollisionVertices {
			return fmt.Errorf("collision: %s polygon mode requires at least %d points", label, MinCollisionVertices)
		}
		if tolerance != 0 {
			return fmt.Errorf("collision: %s tolerance applies only to alpha, box and ellipse modes", label)
		}
		for i, pt := range points {
			if err := validatePoint2D(fmt.Sprintf("collision: %s points[%d]", label, i), pt); err != nil {
				return err
			}
		}
		return nil
	}
	if len(points) > 0 {
		return fmt.Errorf("collision: %s points apply only to polygon mode", label)
	}
	if mode == "none" && (tolerance != 0 || maxVertices != 0) {
		return fmt.Errorf("collision: %s none mode does not take tolerance or maxVertices", label)
	}
	return nil
}

func validatePoint2D(label string, pt Point2D) error {
	for i, v := range pt {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s[%d] must be a finite number", label, i)
		}
	}
	return nil
}

func validatePointMap(label string, pts map[string]Point2D) error {
	for name, pt := range pts {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s: point name must not be empty", label)
		}
		if err := validatePoint2D(fmt.Sprintf("%s.%s", label, name), pt); err != nil {
			return err
		}
	}
	return nil
}

func normalizePoint2D(pt *Point2D) {
	if pt[0] == 0 {
		pt[0] = 0
	}
	if pt[1] == 0 {
		pt[1] = 0
	}
}

func cloneCollision(src *Collision) *Collision {
	if src == nil {
		return nil
	}
	out := *src
	if len(src.Points) > 0 {
		out.Points = slices.Clone(src.Points)
	}
	if len(src.Cells) > 0 {
		out.Cells = make([]CellCollision, len(src.Cells))
		for i, cc := range src.Cells {
			out.Cells[i] = cc
			if len(cc.Points) > 0 {
				out.Cells[i].Points = slices.Clone(cc.Points)
			}
		}
	}
	return &out
}

func cloneCellPoints(src []CellPoints) []CellPoints {
	if len(src) == 0 {
		return nil
	}
	out := make([]CellPoints, len(src))
	for i, cp := range src {
		out[i] = CellPoints{
			Cell:   cp.Cell,
			Points: maps.Clone(cp.Points),
		}
	}
	return out
}
