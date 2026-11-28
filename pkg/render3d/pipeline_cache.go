package render3d

import (
	"path/filepath"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// AttachPipelineCache opens the app's persistent pipeline cache and attaches it
// to dev. Web targets have no CacheDir and use an in-memory cache. The cache is
// stored below <CacheDir>/pipelines and is safe to share across renderer users.
func AttachPipelineCache(cx *core.Scope, dev *gpu.Device) (*gpu.PipelineCache, error) {
	if dev == nil {
		return nil, gpu.ErrDeviceLost
	}
	dir := ""
	if cx != nil && cx.CacheDir() != "" {
		dir = filepath.Join(cx.CacheDir(), "pipelines")
	}
	cache, err := gpu.NewPipelineCache(dir)
	if err != nil {
		return nil, err
	}
	dev.SetPipelineCache(cache)
	return cache, nil
}
