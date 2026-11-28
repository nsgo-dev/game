package scene3d

import (
	"sort"
	"time"
)

// VirtualCamera is a named camera pose selected by a Director's priority.
type VirtualCamera struct {
	Name     string
	Priority int
	Pose     Camera
}

// Director arbitrates virtual camera priorities and blends transitions.
type Director struct {
	cameras      map[string]VirtualCamera
	active       string
	from         Camera
	blendTime    time.Duration
	blendLeft    time.Duration
	current      Camera
	solo         string
	impulse      float32
	impulseDecay time.Duration
}

// NewDirector creates an empty director with an optional default blend duration.
func NewDirector(blend time.Duration) *Director {
	return &Director{cameras: map[string]VirtualCamera{}, blendTime: max(0, blend)}
}

// Add registers or replaces a virtual camera and returns the director for chaining.
func (d *Director) Add(camera VirtualCamera, priorities ...int) *Director {
	if d == nil || camera.Name == "" {
		return d
	}
	if d.cameras == nil {
		d.cameras = map[string]VirtualCamera{}
	}
	if len(priorities) > 0 {
		camera.Priority = priorities[0]
	}
	d.cameras[camera.Name] = camera
	return d
}

// Solo forces one named virtual camera to be active until cleared with Solo("").
func (d *Director) Solo(name string) bool {
	if d == nil {
		return false
	}
	if name != "" {
		if _, ok := d.cameras[name]; !ok {
			return false
		}
	}
	d.solo = name
	return true
}

// Remove unregisters a virtual camera.
func (d *Director) Remove(name string) bool {
	if d == nil {
		return false
	}
	if _, ok := d.cameras[name]; !ok {
		return false
	}
	delete(d.cameras, name)
	if d.active == name {
		d.active = ""
	}
	if d.solo == name {
		d.solo = ""
	}
	return true
}

// Impulse adds a deterministic decaying camera impulse. It is applied along
// the camera's local X axis and is suitable for accessibility-controlled shake.
func (d *Director) Impulse(strength float32, duration time.Duration) {
	if d == nil || duration <= 0 || strength == 0 {
		return
	}
	d.impulse += strength
	if duration > d.impulseDecay {
		d.impulseDecay = duration
	}
}

// SetPriority changes a camera's priority. Ties resolve by name for deterministic selection.
func (d *Director) SetPriority(name string, priority int) bool {
	c, ok := d.cameras[name]
	if !ok {
		return false
	}
	c.Priority = priority
	d.cameras[name] = c
	return true
}

// Active returns the currently selected virtual camera name.
func (d *Director) Active() string {
	if d == nil {
		return ""
	}
	return d.active
}

// Step selects the highest-priority camera and returns its blended pose.
func (d *Director) Step(dt time.Duration) (Camera, bool) {
	if d == nil || len(d.cameras) == 0 {
		return Camera{}, false
	}
	best := d.active
	if d.solo != "" {
		best = d.solo
	}
	list := make([]VirtualCamera, 0, len(d.cameras))
	for _, c := range d.cameras {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Priority != list[j].Priority {
			return list[i].Priority > list[j].Priority
		}
		return list[i].Name < list[j].Name
	})
	if d.solo == "" {
		best = list[0].Name
	}
	if d.active == "" {
		d.active = best
		d.current = list[0].Pose
		return list[0].Pose, true
	}
	if best != d.active {
		d.from = d.current
		d.active, d.blendLeft = best, d.blendTime
	}
	target := d.cameras[d.active].Pose
	if d.blendLeft <= 0 || d.blendTime <= 0 {
		d.current = target
		d.applyImpulse(dt)
		return d.current, true
	}
	if dt > 0 {
		d.blendLeft -= dt
	}
	t := float32(1 - float64(max(0, d.blendLeft))/float64(d.blendTime))
	if t >= 1 {
		d.blendLeft = 0
		d.current = target
		d.applyImpulse(dt)
		return d.current, true
	}
	d.current = d.from.Lerp(&target, t)
	d.applyImpulse(dt)
	return d.current, true
}

func (d *Director) applyImpulse(dt time.Duration) {
	if d.impulse == 0 {
		return
	}
	d.current.Pos = d.current.Pos.Add(d.current.Right().Scale(d.impulse))
	if dt > 0 {
		d.impulseDecay -= dt
		if d.impulseDecay <= 0 {
			d.impulse, d.impulseDecay = 0, 0
		} else {
			d.impulse *= float32(1 - float64(dt)/float64(d.impulseDecay+dt))
		}
	}
}
