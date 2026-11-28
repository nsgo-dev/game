package render3d

import "sync"

type CaptureDraw struct {
	Name                 string `json:"name,omitempty"`
	Mesh                 string `json:"mesh,omitempty"`
	Material             string `json:"material,omitempty"`
	Pipeline             string `json:"pipeline,omitempty"`
	Instances, Triangles int
}
type CapturePass struct {
	Name, Target string
	Draws        []CaptureDraw
}
type FrameCapture struct {
	mu     sync.Mutex
	armed  bool
	passes []CapturePass
}

func (c *FrameCapture) Arm()        { c.mu.Lock(); c.armed = true; c.passes = nil; c.mu.Unlock() }
func (c *FrameCapture) Armed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.armed }
func (c *FrameCapture) BeginPass(name, target string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.armed {
		return false
	}
	c.passes = append(c.passes, CapturePass{Name: name, Target: target})
	return true
}
func (c *FrameCapture) Draw(d CaptureDraw) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.armed || len(c.passes) == 0 {
		return
	}
	c.passes[len(c.passes)-1].Draws = append(c.passes[len(c.passes)-1].Draws, d)
}
func (c *FrameCapture) Finish() []CapturePass {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.armed {
		return nil
	}
	out := append([]CapturePass(nil), c.passes...)
	c.armed = false
	return out
}
