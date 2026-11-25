package sceneformat

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/skeletal"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// SkeletalComponentVersion is the schema version of the built-in "skeletal" component.
const SkeletalComponentVersion = 1

// SkeletalComponentName is the built-in 2D skeletal rig component name in scene YAML files.
const SkeletalComponentName = "skeletal"

// SkeletonResolver may optionally be implemented by an AssetResolver to load .skel.yaml rigs
// referenced by a "skeletal" component. Without it, Build reads the file through FileResolver
// and parses it with skeletal.Load.
type SkeletonResolver interface {
	Skeleton(path string) (*skeletal.SkeletonData, error)
}

// IsSkeletalComponent reports whether name is the built-in "skeletal" component type.
func IsSkeletalComponent(name string) bool {
	return name == SkeletalComponentName
}

// SkeletalComponent returns the ComponentType schema for the built-in "skeletal" component,
// which places a 2D skeletal rig (.skel.yaml) on a scene object with an atlas, skin, and animation.
func SkeletalComponent() ComponentType {
	return ComponentType{
		Name: SkeletalComponentName,
		Doc:  "2D skeletal rig (.skel.yaml) drawn with a sprite atlas, optional skin, and initial animation.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: SkeletalComponentVersion, Min: Float64(SkeletalComponentVersion), Max: Float64(SkeletalComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "rig", Kind: FieldString, Default: "", Doc: "Project-relative path to a .skel.yaml skeletal rig file."},
			{Name: "atlas", Kind: FieldString, Default: "", Doc: "Name of the sprite.Atlas supplying the rig's attachment frames."},
			{Name: "skin", Kind: FieldString, Default: "", Doc: `Active skin name (empty activates "default" when defined).`},
			{Name: "animation", Kind: FieldString, Default: "", Doc: "Initial animation name to play (empty stays in bind pose)."},
			{Name: "loop", Kind: FieldBool, Default: false, Doc: "Override whether the initial animation loops (when omitted, uses the animation's own loop flag)."},
		},
	}
}

// RegisterSkeletalComponent registers the built-in "skeletal" component type on reg.
func RegisterSkeletalComponent(reg *ComponentRegistry) error {
	return reg.Register(SkeletalComponent())
}

// SkeletalComponentRegistry returns a new ComponentRegistry pre-populated with the built-in "skeletal" component.
func SkeletalComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterSkeletalComponent(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinSkeletalReg = SkeletalComponentRegistry()

type skeletalSpec struct {
	rig       string
	atlas     string
	skin      string
	animation string
	loop      bool
	hasLoop   bool
}

func resolveSkeletalComponent(objID string, raw any) (skeletalSpec, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return skeletalSpec{}, &ComponentError{
			Object:    objID,
			Component: SkeletalComponentName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinSkeletalReg.Resolve(SkeletalComponentName, m)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.Object = objID
		}
		return skeletalSpec{}, err
	}
	_, hasLoop := m["loop"]
	return skeletalSpec{
		rig:       resolved["rig"].(string),
		atlas:     resolved["atlas"].(string),
		skin:      resolved["skin"].(string),
		animation: resolved["animation"].(string),
		loop:      resolved["loop"].(bool),
		hasLoop:   hasLoop,
	}, nil
}

func buildObjectSkeleton(
	bl *BuiltLayer,
	bo *BuiltObject,
	resolver AssetResolver,
	getAtlas func(string) (*sprite.Atlas, error),
	getSkeleton func(string) (*skeletal.SkeletonData, error),
) error {
	raw, has := bo.Doc.Components[SkeletalComponentName]
	if !has {
		return nil
	}
	spec, err := resolveSkeletalComponent(bo.ID, raw)
	if err != nil {
		return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, err)
	}
	if spec.rig == "" {
		return nil
	}
	if !validFilePath(spec.rig) {
		return fmt.Errorf("sceneformat: layer %q object %q: skeletal rig %q must be a slash-separated path relative to the project root", bl.Name, bo.ID, spec.rig)
	}
	data, err := getSkeleton(spec.rig)
	if err != nil {
		return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, err)
	}
	var atlas *sprite.Atlas
	if spec.atlas != "" {
		atlas, err = getAtlas(spec.atlas)
		if err != nil {
			return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, err)
		}
	}
	skel := skeletal.NewSkeleton(data, atlas)
	if spec.skin != "" {
		if err := skel.SetSkin(spec.skin); err != nil {
			return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, err)
		}
	}
	if spec.animation != "" {
		loop := spec.loop
		if !spec.hasLoop {
			if a := data.Animations[spec.animation]; a != nil {
				loop = a.Loop
			}
		}
		if err := skel.Play(spec.animation, loop); err != nil {
			return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, err)
		}
	}
	bo.Skeleton = skel
	if bo.Atlas == nil && atlas != nil {
		bo.Atlas = atlas
	}
	if !bo.syncSkeleton() {
		return fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, bo.ID, errShearedSprite)
	}
	return nil
}

func makeSkeletonLoader(resolver AssetResolver) func(string) (*skeletal.SkeletonData, error) {
	cache := make(map[string]*skeletal.SkeletonData)
	return func(p string) (*skeletal.SkeletonData, error) {
		if sd, ok := cache[p]; ok {
			return sd, nil
		}
		if sr, ok := resolver.(SkeletonResolver); ok {
			sd, err := sr.Skeleton(p)
			if err != nil {
				return nil, fmt.Errorf("resolve skeleton %q: %w", p, err)
			}
			if sd == nil {
				return nil, fmt.Errorf("resolver returned nil skeleton for %q", p)
			}
			cache[p] = sd
			return sd, nil
		}
		fr, ok := resolver.(FileResolver)
		if !ok {
			return nil, fmt.Errorf("skeletal rig %q requires a SkeletonResolver or FileResolver (such as FSAssets)", p)
		}
		raw, err := fr.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read skeletal rig %q: %w", p, err)
		}
		sd, err := skeletal.Load(raw)
		if err != nil {
			return nil, loaderr.WithFile(err, p)
		}
		cache[p] = sd
		return sd, nil
	}
}
