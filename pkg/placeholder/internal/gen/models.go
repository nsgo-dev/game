package gen

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/placeholder/internal/samplegltf"
)

// addModels writes the three glTF models package samplegltf builds: a crate, a skinned and animated robot arm, and a gem.
func addModels(files map[string][]byte) error {
	for _, m := range []struct {
		path string
		make func() ([]byte, error)
	}{
		{"models/crate.glb", samplegltf.GenerateCrateGLB},
		{"models/robot_arm.gltf", samplegltf.GenerateRobotArmGLTF},
		{"models/gem.glb", samplegltf.GenerateGemGLB},
	} {
		data, err := m.make()
		if err != nil {
			return fmt.Errorf("%s: %w", m.path, err)
		}
		files[m.path] = data
	}
	return nil
}
