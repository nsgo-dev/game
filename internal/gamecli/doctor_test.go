package gamecli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorValidProject(t *testing.T) {
	dir := filepath.Join("testdata", "doctor", "valid")
	got := doctor(dir)
	if len(got) != 0 {
		t.Fatalf("findings=%+v", got)
	}
}
func TestDoctorReportsModuleAndDefaultID(t *testing.T) {
	got := doctor(filepath.Join("testdata", "doctor", "unresolvable"))
	text := ""
	for _, f := range got {
		text += f.Message
	}
	for _, want := range []string{"game tool is not declared", "default dev.nsgo ID"} {
		if !strings.Contains(text, want) {
			t.Errorf("findings=%+v missing %q", got, want)
		}
	}
}
