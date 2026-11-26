package project_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/project"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*project.File]{
		Decode: project.Parse,
		Encode: project.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "project",
	})
}

func TestBriefFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*project.Brief]{
		Decode: project.ParseBrief,
		Encode: project.EncodeBrief,
		Files:  []string{"testdata/brief/*.yaml"},
		Doc:    "project#brief",
	})
}
