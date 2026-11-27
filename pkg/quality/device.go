package quality

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Device describes the hardware a game runs on, as far as Classify needs it.
type Device struct {
	// OS is the operating system, such as core.OSAndroid; empty when unknown.
	OS core.OS
	// Idiom is the kind of device, such as core.IdiomPhone.
	Idiom core.Idiom
	// Model is the hardware model as core.Env.DeviceModel reports it, such as "iPhone15,2" or "Google Pixel 8"; empty when unknown.
	Model string
	// OSVersion is the operating system's version; on Android its Major is the API level.
	OSVersion core.OSVersion
	// CPUs is how many logical CPUs the process can use; 0 when unknown.
	CPUs int
}

// DeviceFromEnv returns the Device env describes, with CPUs from runtime.NumCPU.
func DeviceFromEnv(env *core.Env) Device {
	return Device{
		OS:        env.OS(),
		Idiom:     env.Idiom(),
		Model:     env.DeviceModel(),
		OSVersion: env.OSVersion(),
		CPUs:      runtime.NumCPU(),
	}
}

// Classify returns the level a device starts at when no device rule places it.
//
// Android starts Low, since NSGo reports neither its memory nor its GPU and core counts do not tell a budget phone from a flagship; measured frame times step it up. iPhones start High from the iPhone 13 (model iPhone14,x) on, Medium from the iPhone 11 (iPhone12,x) and Low before; iPads High from model iPad13,x on, Medium from iPad11,x and Low before. Desktops start High with 8 or more CPUs, Medium with 4 to 7 and Low with fewer. TVs and watches start Low, and the web and anything unknown start Medium.
func Classify(d Device) Level {
	switch d.Idiom {
	case core.IdiomTV, core.IdiomWatch:
		return Low
	}
	switch d.OS {
	case core.OSAndroid:
		return Low
	case core.OSIOS:
		if n, ok := appleGeneration(d.Model, "iPhone"); ok {
			return byGeneration(n, 14, 12)
		}
		if n, ok := appleGeneration(d.Model, "iPad"); ok {
			return byGeneration(n, 13, 11)
		}
		return Medium
	case core.OSMacOS, core.OSWindows, core.OSLinux:
		switch {
		case d.CPUs >= 8:
			return High
		case d.CPUs >= 4:
			return Medium
		case d.CPUs > 0:
			return Low
		}
		return Medium
	default:
		return Medium
	}
}

// appleGeneration returns the major number of an Apple model identifier that starts with family, such as 15 for "iPhone15,2".
func appleGeneration(model, family string) (int, bool) {
	rest, ok := strings.CutPrefix(model, family)
	if !ok {
		return 0, false
	}
	major, _, ok := strings.Cut(rest, ",")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(major)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func byGeneration(n, high, medium int) Level {
	switch {
	case n >= high:
		return High
	case n >= medium:
		return Medium
	default:
		return Low
	}
}
