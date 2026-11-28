package particles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

type vec3Doc struct {
	X float32 `json:"x,omitempty" yaml:"x,omitempty"`
	Y float32 `json:"y,omitempty" yaml:"y,omitempty"`
	Z float32 `json:"z,omitempty" yaml:"z,omitempty"`
}

func vec3ToDoc(v math3d.Vec3) *vec3Doc {
	if v == (math3d.Vec3{}) {
		return nil
	}
	return &vec3Doc{X: v.X, Y: v.Y, Z: v.Z}
}

func vec3FromDoc(d *vec3Doc) math3d.Vec3 {
	if d == nil {
		return math3d.Vec3{}
	}
	return math3d.Vec3{X: d.X, Y: d.Y, Z: d.Z}
}

type quatDoc struct {
	X float32 `json:"x,omitempty" yaml:"x,omitempty"`
	Y float32 `json:"y,omitempty" yaml:"y,omitempty"`
	Z float32 `json:"z,omitempty" yaml:"z,omitempty"`
	W float32 `json:"w,omitempty" yaml:"w,omitempty"`
}

func quatToDoc(q math3d.Quat) *quatDoc {
	if q == (math3d.Quat{}) || q == math3d.QuatIdentity() {
		return nil
	}
	return &quatDoc{X: q.X, Y: q.Y, Z: q.Z, W: q.W}
}

func quatFromDoc(d *quatDoc) math3d.Quat {
	if d == nil {
		return math3d.Quat{}
	}
	return math3d.Quat{X: d.X, Y: d.Y, Z: d.Z, W: d.W}
}

type shape3DDoc struct {
	Kind   string  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Radius float32 `json:"radius,omitempty" yaml:"radius,omitempty"`
	Angle  float32 `json:"angle,omitempty" yaml:"angle,omitempty"`
	Width  float32 `json:"width,omitempty" yaml:"width,omitempty"`
	Height float32 `json:"height,omitempty" yaml:"height,omitempty"`
	Depth  float32 `json:"depth,omitempty" yaml:"depth,omitempty"`
}

type config3DDoc struct {
	Simulation         string         `json:"simulation,omitempty" yaml:"simulation,omitempty"`
	Version            docInt         `json:"version,omitempty" yaml:"version,omitempty"`
	Position           *vec3Doc       `json:"position,omitempty" yaml:"position,omitempty"`
	Direction          *vec3Doc       `json:"direction,omitempty" yaml:"direction,omitempty"`
	Up                 *vec3Doc       `json:"up,omitempty" yaml:"up,omitempty"`
	Rotation           *quatDoc       `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	Space              string         `json:"space,omitempty" yaml:"space,omitempty"`
	Shape              *shape3DDoc    `json:"shape,omitempty" yaml:"shape,omitempty"`
	Billboard          string         `json:"billboard,omitempty" yaml:"billboard,omitempty"`
	Blend              string         `json:"blend,omitempty" yaml:"blend,omitempty"`
	StretchFactor      float32        `json:"stretchFactor,omitempty" yaml:"stretchFactor,omitempty"`
	Axis               *vec3Doc       `json:"axis,omitempty" yaml:"axis,omitempty"`
	Rate               float32        `json:"rate,omitempty" yaml:"rate,omitempty"`
	MaxParticles       docInt         `json:"maxParticles,omitempty" yaml:"maxParticles,omitempty"`
	LifetimeMin        docDuration    `json:"lifetimeMin,omitempty" yaml:"lifetimeMin,omitempty"`
	LifetimeMax        docDuration    `json:"lifetimeMax,omitempty" yaml:"lifetimeMax,omitempty"`
	SpeedMin           float32        `json:"speedMin,omitempty" yaml:"speedMin,omitempty"`
	SpeedMax           float32        `json:"speedMax,omitempty" yaml:"speedMax,omitempty"`
	Spread             float32        `json:"spread,omitempty" yaml:"spread,omitempty"`
	Drag               float32        `json:"drag,omitempty" yaml:"drag,omitempty"`
	Gravity            *vec3Doc       `json:"gravity,omitempty" yaml:"gravity,omitempty"`
	SizeMin            float32        `json:"sizeMin,omitempty" yaml:"sizeMin,omitempty"`
	SizeMax            float32        `json:"sizeMax,omitempty" yaml:"sizeMax,omitempty"`
	InitialRotationMin float32        `json:"initialRotationMin,omitempty" yaml:"initialRotationMin,omitempty"`
	InitialRotationMax float32        `json:"initialRotationMax,omitempty" yaml:"initialRotationMax,omitempty"`
	RotationSpeedMin   float32        `json:"rotationSpeedMin,omitempty" yaml:"rotationSpeedMin,omitempty"`
	RotationSpeedMax   float32        `json:"rotationSpeedMax,omitempty" yaml:"rotationSpeedMax,omitempty"`
	SizeCurve          []floatStopDoc `json:"sizeCurve,omitempty" yaml:"sizeCurve,omitempty"`
	AlphaCurve         []floatStopDoc `json:"alphaCurve,omitempty" yaml:"alphaCurve,omitempty"`
	RotationCurve      []floatStopDoc `json:"rotationCurve,omitempty" yaml:"rotationCurve,omitempty"`
	ColorCurve         []colorStopDoc `json:"colorCurve,omitempty" yaml:"colorCurve,omitempty"`
	Color              *docColor      `json:"color,omitempty" yaml:"color,omitempty"`
	Seed               docUint64      `json:"seed,omitempty" yaml:"seed,omitempty"`
}

// MarshalConfig3DYAML encodes cfg as a versioned YAML 3D emitter definition (version: 1) with fixed key order and 2-space indentation, so saving the same Emitter3DConfig always yields the same bytes.
func MarshalConfig3DYAML(cfg Emitter3DConfig) ([]byte, error) {
	doc, err := config3DToDoc(cfg)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := root.Encode(yamlzero.Clean(doc)); err != nil {
		return nil, fmt.Errorf("particles: encode 3D config YAML: %w", err)
	}
	plainYAMLKeys(&root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("particles: encode 3D config YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("particles: encode 3D config YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalConfig3DYAML decodes a versioned YAML 3D emitter definition into an [Emitter3DConfig].
// It refuses unknown fields, keys with no value, trailing YAML documents, unsupported versions, fractions in integer fields, shape fields that do not apply to the shape kind, and non-finite numbers.
func UnmarshalConfig3DYAML(data []byte) (Emitter3DConfig, error) {
	cfg, err := unmarshalConfig3DYAML(data)
	if err != nil {
		return Emitter3DConfig{}, loaderr.Locate(err, "", data, fixConfig)
	}
	yamlzero.Normalize(&cfg)
	return cfg, nil
}

func unmarshalConfig3DYAML(data []byte) (Emitter3DConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Emitter3DConfig{}, errors.New("particles: empty 3D config YAML")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return Emitter3DConfig{}, errors.New("particles: empty 3D config YAML")
		}
		return Emitter3DConfig{}, fmt.Errorf("particles: decode 3D config YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Emitter3DConfig{}, errors.New("particles: unexpected trailing YAML document in 3D config")
		}
		return Emitter3DConfig{}, fmt.Errorf("particles: decode 3D config YAML: %w", err)
	}
	if err := refuseYAMLNulls(&root); err != nil {
		return Emitter3DConfig{}, err
	}

	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var doc config3DDoc
	if err := strict.Decode(&doc); err != nil {
		return Emitter3DConfig{}, fmt.Errorf("particles: decode 3D config YAML: %w", err)
	}
	if doc.Version != FormatVersion {
		return Emitter3DConfig{}, versionError(int(doc.Version))
	}
	if doc.Simulation != "" && doc.Simulation != "cpu" && doc.Simulation != "gpu" {
		return Emitter3DConfig{}, fmt.Errorf("particles: simulation must be cpu or gpu")
	}
	return config3DFromDoc(&doc)
}

// MarshalConfig3DJSON encodes cfg into the versioned JSON form of the 3D emitter definition (version: 1).
func MarshalConfig3DJSON(cfg Emitter3DConfig) ([]byte, error) {
	doc, err := config3DToDoc(cfg)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// UnmarshalConfig3DJSON decodes the versioned JSON form of a 3D emitter definition into an [Emitter3DConfig].
func UnmarshalConfig3DJSON(data []byte) (Emitter3DConfig, error) {
	cfg, err := unmarshalConfig3DJSON(data)
	if err != nil {
		return Emitter3DConfig{}, loaderr.Locate(err, "", data, fixConfig)
	}
	return cfg, nil
}

func unmarshalConfig3DJSON(data []byte) (Emitter3DConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Emitter3DConfig{}, errors.New("particles: empty 3D config JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc config3DDoc
	if err := dec.Decode(&doc); err != nil {
		return Emitter3DConfig{}, fmt.Errorf("particles: decode 3D config JSON: %w", err)
	}
	if doc.Version != FormatVersion {
		return Emitter3DConfig{}, versionError(int(doc.Version))
	}
	if doc.Simulation != "" && doc.Simulation != "cpu" && doc.Simulation != "gpu" {
		return Emitter3DConfig{}, fmt.Errorf("particles: simulation must be cpu or gpu")
	}
	return config3DFromDoc(&doc)
}

// LoadConfig3DFile reads and decodes a versioned YAML 3D emitter definition file from disk.
func LoadConfig3DFile(path string) (Emitter3DConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Emitter3DConfig{}, loaderr.Locate(err, path, nil, fixReadConfig)
	}
	cfg, err := UnmarshalConfig3DYAML(data)
	if err != nil {
		return Emitter3DConfig{}, loaderr.WithFile(err, path)
	}
	return cfg, nil
}

// LoadConfig3DFS reads and decodes a versioned YAML 3D emitter definition file from fsys.
func LoadConfig3DFS(fsys fs.FS, name string) (Emitter3DConfig, error) {
	if fsys == nil {
		return Emitter3DConfig{}, loaderr.WithFile(loaderr.Errorf("", "pass a non-nil fs.FS", "particles: nil fs.FS"), name)
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Emitter3DConfig{}, loaderr.Locate(err, name, nil, fixReadConfig)
	}
	cfg, err := UnmarshalConfig3DYAML(data)
	if err != nil {
		return Emitter3DConfig{}, loaderr.WithFile(err, name)
	}
	return cfg, nil
}

// SaveConfig3DFile writes cfg as a versioned YAML 3D emitter definition file to path.
func SaveConfig3DFile(path string, cfg Emitter3DConfig) error {
	data, err := MarshalConfig3DYAML(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func config3DToDoc(cfg Emitter3DConfig) (*config3DDoc, error) {
	if err := validateConfig3DFloats(cfg); err != nil {
		return nil, err
	}
	doc := &config3DDoc{
		Version:            FormatVersion,
		Simulation:         cfg.Simulation,
		Position:           vec3ToDoc(cfg.Position),
		Direction:          vec3ToDoc(cfg.Direction),
		Up:                 vec3ToDoc(cfg.Up),
		Rotation:           quatToDoc(cfg.Rotation),
		Space:              cfg.Space.String(),
		Shape:              shape3DToDoc(cfg.Shape),
		Billboard:          cfg.Billboard.String(),
		Blend:              cfg.Blend.String(),
		StretchFactor:      cfg.StretchFactor,
		Axis:               vec3ToDoc(cfg.Axis),
		Rate:               cfg.Rate,
		MaxParticles:       docInt(cfg.MaxParticles),
		LifetimeMin:        docDuration(cfg.LifetimeMin),
		LifetimeMax:        docDuration(cfg.LifetimeMax),
		SpeedMin:           cfg.SpeedMin,
		SpeedMax:           cfg.SpeedMax,
		Spread:             cfg.Spread,
		Drag:               cfg.Drag,
		Gravity:            vec3ToDoc(cfg.Gravity),
		SizeMin:            cfg.SizeMin,
		SizeMax:            cfg.SizeMax,
		InitialRotationMin: cfg.InitialRotationMin,
		InitialRotationMax: cfg.InitialRotationMax,
		RotationSpeedMin:   cfg.RotationSpeedMin,
		RotationSpeedMax:   cfg.RotationSpeedMax,
		SizeCurve:          floatCurveToDoc(cfg.SizeCurve),
		AlphaCurve:         floatCurveToDoc(cfg.AlphaCurve),
		RotationCurve:      floatCurveToDoc(cfg.RotationCurve),
		ColorCurve:         colorCurveToDoc(cfg.ColorCurve),
		Seed:               docUint64(cfg.Seed),
	}
	if cfg.Color != 0 {
		c := docColor(cfg.Color)
		doc.Color = &c
	}
	return doc, nil
}

func config3DFromDoc(doc *config3DDoc) (Emitter3DConfig, error) {
	space, err := parseSimulationSpace(doc.Space)
	if err != nil {
		return Emitter3DConfig{}, &loaderr.Error{Field: "space", Problem: err.Error(), Expected: `"world" or "local"`, Fix: "set space to world or local, or remove it for world", Err: err}
	}
	shape, err := shape3DFromDoc(doc.Shape)
	if err != nil {
		return Emitter3DConfig{}, &loaderr.Error{Field: "shape", Problem: err.Error(), Fix: "use one of point, sphere, hemisphere, box, cone or ring with only the parameters that kind accepts", Err: err}
	}
	billboard, err := parseBillboardMode3D(doc.Billboard)
	if err != nil {
		return Emitter3DConfig{}, &loaderr.Error{Field: "billboard", Problem: err.Error(), Expected: `"spherical", "cylindrical" or "stretched"`, Fix: "set billboard to spherical, cylindrical or stretched, or remove it for spherical", Err: err}
	}
	blend, err := parseParticleBlendMode(doc.Blend)
	if err != nil {
		return Emitter3DConfig{}, &loaderr.Error{Field: "blend", Problem: err.Error(), Expected: `"alpha", "additive" or "premultiplied"`, Fix: "set blend to alpha, additive or premultiplied, or remove it for alpha", Err: err}
	}
	if doc.MaxParticles < 0 {
		return Emitter3DConfig{}, loaderr.Errorf("maxParticles", "set maxParticles to a non-negative integer", "particles: maxParticles must not be negative")
	}
	if doc.Drag < 0 {
		return Emitter3DConfig{}, loaderr.Errorf("drag", "set drag to 0 or greater", "particles: drag must not be negative")
	}
	if doc.Spread < 0 {
		return Emitter3DConfig{}, loaderr.Errorf("spread", "set spread to 0 or greater (in radians)", "particles: spread must not be negative")
	}

	cfg := Emitter3DConfig{
		Simulation:         doc.Simulation,
		Position:           vec3FromDoc(doc.Position),
		Direction:          vec3FromDoc(doc.Direction),
		Up:                 vec3FromDoc(doc.Up),
		Rotation:           quatFromDoc(doc.Rotation),
		Space:              space,
		Shape:              shape,
		Billboard:          billboard,
		Blend:              blend,
		StretchFactor:      doc.StretchFactor,
		Axis:               vec3FromDoc(doc.Axis),
		Rate:               doc.Rate,
		MaxParticles:       int(doc.MaxParticles),
		LifetimeMin:        time.Duration(doc.LifetimeMin),
		LifetimeMax:        time.Duration(doc.LifetimeMax),
		SpeedMin:           doc.SpeedMin,
		SpeedMax:           doc.SpeedMax,
		Spread:             doc.Spread,
		Drag:               doc.Drag,
		Gravity:            vec3FromDoc(doc.Gravity),
		SizeMin:            doc.SizeMin,
		SizeMax:            doc.SizeMax,
		InitialRotationMin: doc.InitialRotationMin,
		InitialRotationMax: doc.InitialRotationMax,
		RotationSpeedMin:   doc.RotationSpeedMin,
		RotationSpeedMax:   doc.RotationSpeedMax,
		SizeCurve:          floatCurveFromDoc(doc.SizeCurve),
		AlphaCurve:         floatCurveFromDoc(doc.AlphaCurve),
		RotationCurve:      floatCurveFromDoc(doc.RotationCurve),
		ColorCurve:         colorCurveFromDoc(doc.ColorCurve),
		Seed:               uint64(doc.Seed),
	}
	if doc.Color != nil {
		cfg.Color = core.Color(*doc.Color)
	}
	if err := validateConfig3DFloats(cfg); err != nil {
		return Emitter3DConfig{}, loaderr.Errorf("", "use finite numbers throughout the 3D emitter definition", "%w", err)
	}
	return cfg, nil
}

func validateConfig3DFloats(cfg Emitter3DConfig) error {
	floats := []float32{
		cfg.Position.X, cfg.Position.Y, cfg.Position.Z,
		cfg.Direction.X, cfg.Direction.Y, cfg.Direction.Z,
		cfg.Up.X, cfg.Up.Y, cfg.Up.Z,
		cfg.Rotation.X, cfg.Rotation.Y, cfg.Rotation.Z, cfg.Rotation.W,
		cfg.StretchFactor,
		cfg.Axis.X, cfg.Axis.Y, cfg.Axis.Z,
		cfg.Rate, cfg.SpeedMin, cfg.SpeedMax, cfg.Spread, cfg.Drag,
		cfg.Gravity.X, cfg.Gravity.Y, cfg.Gravity.Z,
		cfg.SizeMin, cfg.SizeMax,
		cfg.InitialRotationMin, cfg.InitialRotationMax,
		cfg.RotationSpeedMin, cfg.RotationSpeedMax,
		cfg.Shape.radius, cfg.Shape.angle, cfg.Shape.width, cfg.Shape.height, cfg.Shape.depth,
	}
	for _, v := range floats {
		if isBadFloat(v) {
			return errors.New("particles: 3D config contains NaN or Inf float value")
		}
	}
	for _, curve := range []FloatCurve{cfg.SizeCurve, cfg.AlphaCurve, cfg.RotationCurve} {
		for _, s := range curve {
			if isBadFloat(s.T) || isBadFloat(s.Value) {
				return errors.New("particles: float curve contains NaN or Inf")
			}
		}
	}
	for _, s := range cfg.ColorCurve {
		if isBadFloat(s.T) {
			return errors.New("particles: color curve contains NaN or Inf")
		}
	}
	return nil
}

func shape3DToDoc(s Shape3D) *shape3DDoc {
	switch s.kind {
	case shape3DSphere:
		return &shape3DDoc{Kind: "sphere", Radius: s.radius}
	case shape3DHemisphere:
		return &shape3DDoc{Kind: "hemisphere", Radius: s.radius}
	case shape3DBox:
		return &shape3DDoc{Kind: "box", Width: s.width, Height: s.height, Depth: s.depth}
	case shape3DCone:
		return &shape3DDoc{Kind: "cone", Radius: s.radius, Angle: s.angle}
	case shape3DRing:
		return &shape3DDoc{Kind: "ring", Radius: s.radius}
	default:
		return &shape3DDoc{Kind: "point"}
	}
}

func shape3DFromDoc(d *shape3DDoc) (Shape3D, error) {
	if d == nil {
		return Point3D(), nil
	}
	switch d.Kind {
	case "", "point":
		if d.Radius != 0 || d.Angle != 0 || d.Width != 0 || d.Height != 0 || d.Depth != 0 {
			return Shape3D{}, errors.New("particles: a point shape takes no radius, angle, width, height or depth")
		}
		return Point3D(), nil
	case "sphere":
		if d.Angle != 0 || d.Width != 0 || d.Height != 0 || d.Depth != 0 {
			return Shape3D{}, errors.New("particles: a sphere shape takes only radius")
		}
		if d.Radius < 0 {
			return Shape3D{}, errors.New("particles: sphere radius must not be negative")
		}
		return Sphere(d.Radius), nil
	case "hemisphere":
		if d.Angle != 0 || d.Width != 0 || d.Height != 0 || d.Depth != 0 {
			return Shape3D{}, errors.New("particles: a hemisphere shape takes only radius")
		}
		if d.Radius < 0 {
			return Shape3D{}, errors.New("particles: hemisphere radius must not be negative")
		}
		return Hemisphere(d.Radius), nil
	case "ring":
		if d.Angle != 0 || d.Width != 0 || d.Height != 0 || d.Depth != 0 {
			return Shape3D{}, errors.New("particles: a ring shape takes only radius")
		}
		if d.Radius < 0 {
			return Shape3D{}, errors.New("particles: ring radius must not be negative")
		}
		return Ring3D(d.Radius), nil
	case "box":
		if d.Radius != 0 || d.Angle != 0 {
			return Shape3D{}, errors.New("particles: a box shape takes width, height and depth, not radius or angle")
		}
		if d.Width < 0 || d.Height < 0 || d.Depth < 0 {
			return Shape3D{}, errors.New("particles: box dimensions must not be negative")
		}
		return Box(d.Width, d.Height, d.Depth), nil
	case "cone":
		if d.Width != 0 || d.Height != 0 || d.Depth != 0 {
			return Shape3D{}, errors.New("particles: a cone shape takes radius and angle, not width, height or depth")
		}
		if d.Radius < 0 || d.Angle < 0 {
			return Shape3D{}, errors.New("particles: cone radius and angle must not be negative")
		}
		return Cone(d.Radius, d.Angle), nil
	default:
		return Shape3D{}, fmt.Errorf("particles: unknown 3D shape kind %q", d.Kind)
	}
}

func parseBillboardMode3D(s string) (BillboardMode3D, error) {
	switch s {
	case "", "spherical":
		return BillboardSpherical, nil
	case "cylindrical":
		return BillboardCylindrical, nil
	case "stretched":
		return BillboardStretched, nil
	default:
		return BillboardSpherical, fmt.Errorf("particles: unknown billboard mode %q", s)
	}
}

func parseParticleBlendMode(s string) (ParticleBlendMode, error) {
	switch s {
	case "", "alpha":
		return BlendAlpha, nil
	case "additive":
		return BlendAdditive, nil
	case "premultiplied":
		return BlendPremultiplied, nil
	default:
		return BlendAlpha, fmt.Errorf("particles: unknown blend mode %q", s)
	}
}
