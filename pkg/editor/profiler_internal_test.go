package editor

import "testing"

func TestProfilerSeqUnchangedByUnrelatedEditorMutations(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seq0 := ed.profiler.Seq()
	rev0 := ed.Revision()

	ed.SetTool(ToolMove)
	ed.SetTool(ToolSelect)
	if ed.Revision() <= rev0 {
		t.Fatal("expected Revision to increase on tool changes")
	}
	if ed.profiler.Seq() != seq0 {
		t.Fatalf("profiler Seq changed from %d to %d on unrelated tool mutations", seq0, ed.profiler.Seq())
	}

	ed.SetProfilerOpen(true)
	if ed.profiler.Seq() <= seq0 {
		t.Fatal("expected profiler Seq to increase on SetProfilerOpen")
	}
}
