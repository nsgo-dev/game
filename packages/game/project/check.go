package project

import (
	"errors"
	"fmt"
	"io/fs"
)

// Check reads the project at the root of fsys and reports every problem a person should fix before running or building it, joined with errors.Join: a game.yaml that fails Parse, an nsgo.yaml beside it that disagrees with it (CheckNSGo), and an autoloaded scene or resource that is missing or, for a scene, invalid (LoadAutoloads). It returns the parsed file whenever game.yaml parses, so a caller can go on with it after reporting the rest. A project without an nsgo.yaml has nothing to disagree with.
func Check(fsys fs.FS) (*File, error) {
	f, err := ReadFS(fsys)
	if err != nil {
		return nil, err
	}
	var errs []error
	if nsgo, err := fs.ReadFile(fsys, NSGoFileName); err == nil {
		if err := f.CheckNSGo(nsgo); err != nil {
			errs = append(errs, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("project: %w", err))
	}
	if _, err := f.LoadAutoloads(fsys); err != nil {
		errs = append(errs, err)
	}
	return f, errors.Join(errs...)
}
