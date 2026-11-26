package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func findReviewChangeByObject(t *testing.T, snap editor.ReviewSnapshot, objectOrTitle string) editor.ReviewChange {
	t.Helper()
	for _, ch := range snap.Changes {
		if ch.ObjectID == objectOrTitle || ch.Title == objectOrTitle {
			return ch
		}
	}
	t.Fatalf("review change %q not found in %+v", objectOrTitle, snap.Changes)
	return editor.ReviewChange{}
}

func TestReviewNotesMultiRoundKeepAndSingleUndo(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	snap1 := f.ed.ReviewStatus()
	slime1 := findReviewChangeByObject(t, snap1, "slime_01")
	slime3 := findReviewChangeByObject(t, snap1, "slime_03")
	prefabSpeed := findReviewChangeByObject(t, snap1, "patrol.speed")

	// Mark slime_03 seen first so we can verify Round 2 marks it unseen again.
	f.ed.FocusReviewChange(slime3.ID)

	note1 := "This one sits where you land coming off the upper ledge. Move it about 40 px left."
	note2 := "Try speed 26 instead of 24."
	if err := f.ed.SetReviewNote(slime3.ID, note1); err != nil {
		t.Fatalf("SetReviewNote(slime3): %v", err)
	}
	if err := f.ed.SetReviewNote(prefabSpeed.ID, note2); err != nil {
		t.Fatalf("SetReviewNote(prefabSpeed): %v", err)
	}

	if got := f.ed.ReviewDraftNoteCount(); got != 2 {
		t.Fatalf("ReviewDraftNoteCount = %d, want 2", got)
	}
	if got := f.ed.ReviewChipCount(editor.ReviewFilterNotes); got != 2 {
		t.Fatalf("Notes chip count = %d, want 2", got)
	}
	// Viewport pin appears beside noted scene object slime_03.
	pins := f.ed.ReviewViewportPins()
	if len(pins) != 1 || pins[0].ChangeID != slime3.ID || pins[0].Note != note1 {
		t.Fatalf("ReviewViewportPins = %+v, want 1 pin for %s", pins, slime3.ID)
	}
	// Notes are hidden when peeking before request.
	f.ed.SetReviewPeekBefore(true)
	if len(f.ed.ReviewViewportPins()) != 0 {
		t.Fatal("viewport pins must hide while peeking before request")
	}
	f.ed.SetReviewPeekBefore(false)

	// Send both notes.
	batch, err := f.ed.SendReviewNotes()
	if err != nil {
		t.Fatalf("SendReviewNotes: %v", err)
	}
	if len(batch.Notes) != 2 {
		t.Fatalf("sent batch len = %d, want 2", len(batch.Notes))
	}
	if batch.Notes[0].ChangeID != slime3.ID || batch.Notes[0].File != "scenes/level_01.scene.yaml" || batch.Notes[0].Object != "slime_03" || batch.Notes[0].Field == "" {
		t.Fatalf("batch.Notes[0] = %+v", batch.Notes[0])
	}
	if batch.Notes[1].ChangeID != prefabSpeed.ID || batch.Notes[1].File != "prefabs/slime.prefab.yaml" || batch.Notes[1].Field != "components.patrol.speed" {
		t.Fatalf("batch.Notes[1] = %+v", batch.Notes[1])
	}
	if f.ed.ReviewDraftNoteCount() != 0 {
		t.Fatalf("draft count after send = %d, want 0", f.ed.ReviewDraftNoteCount())
	}

	// Refusing to redo an un-noted change (slime_01) must fail atomically without advancing rounds.
	xBad := float32(120)
	if err := f.ed.ApplyReviewRedo([]editor.ReviewRedoChange{{ChangeID: slime1.ID, X: &xBad}}); !errors.Is(err, editor.ErrReviewChangeNotNoted) {
		t.Fatalf("ApplyReviewRedo(un-noted) = %v, want ErrReviewChangeNotNoted", err)
	}
	if f.ed.ReviewRound() != 1 {
		t.Fatalf("round changed after refused redo: got %d, want 1", f.ed.ReviewRound())
	}

	// Redo ONLY slime_03 (move 40 px left: 452 -> 412).
	x2 := float32(412)
	if err := f.ed.ApplyReviewRedo([]editor.ReviewRedoChange{
		{
			ChangeID: slime3.ID,
			Detail:   "patrols the plank x 400–450",
			X:        &x2,
		},
	}); err != nil {
		t.Fatalf("ApplyReviewRedo(slime3): %v", err)
	}

	snap2 := f.ed.ReviewStatus()
	if snap2.Round != 2 || snap2.ViewRound != 2 || snap2.ViewingReadOnlyRound {
		t.Fatalf("round state after redo = %d/%d (ro=%v)", snap2.Round, snap2.ViewRound, snap2.ViewingReadOnlyRound)
	}
	if !strings.Contains(snap2.RoundBanner, "Round 2") || !strings.Contains(snap2.RoundBanner, "1 change you left notes on") {
		t.Fatalf("Round 2 banner = %q", snap2.RoundBanner)
	}

	s3After := findReviewChangeByObject(t, snap2, "slime_03")
	if s3After.Seen {
		t.Fatal("redone slime_03 should be marked unseen again in Round 2")
	}
	if s3After.RedoneInRound != 2 || s3After.Why != "Redone from your note: "+note1 {
		t.Fatalf("redone slime_03 = %+v", s3After)
	}
	if s3After.VersionFor(1).X != 452 || s3After.VersionFor(2).X != 412 {
		t.Fatalf("slime_03 versions: r1=%v r2=%v", s3After.VersionFor(1).X, s3After.VersionFor(2).X)
	}

	s1After := findReviewChangeByObject(t, snap2, "slime_01")
	if s1After.RedoneInRound != 0 || s1After.VersionFor(1).X != s1After.VersionFor(2).X {
		t.Fatalf("un-noted slime_01 changed unexpectedly: %+v", s1After)
	}

	// Switch to Round 1: read-only, shows first version, refuses revert & note composer.
	if err := f.ed.SetReviewViewRound(1); err != nil {
		t.Fatalf("SetReviewViewRound(1): %v", err)
	}
	if !f.ed.ReviewViewingReadOnlyRound() {
		t.Fatal("expected ReviewViewingReadOnlyRound() == true on Round 1")
	}
	if b1 := f.ed.ReviewRoundBannerText(); !strings.Contains(b1, "Round 1, as the agent first did it") {
		t.Fatalf("Round 1 banner = %q", b1)
	}
	snapR1 := f.ed.ReviewStatus()
	s3R1 := findReviewChangeByObject(t, snapR1, "slime_03")
	if strings.Contains(s3R1.Why, "Redone from your note") {
		t.Fatalf("Round 1 Why leaked Round 2 note: %q", s3R1.Why)
	}
	if f.ed.ToggleReviewChangeReverted(slime3.ID) {
		t.Fatal("ToggleReviewChangeReverted must refuse on read-only Round 1")
	}
	if err := f.ed.OpenReviewNoteComposer(slime3.ID); !errors.Is(err, editor.ErrReviewReadOnlyRound) {
		t.Fatalf("OpenReviewNoteComposer on Round 1 = %v, want ErrReviewReadOnlyRound", err)
	}
	if err := f.ed.SetReviewNote(slime3.ID, "nope"); !errors.Is(err, editor.ErrReviewReadOnlyRound) {
		t.Fatalf("SetReviewNote on Round 1 = %v, want ErrReviewReadOnlyRound", err)
	}

	// Keep while viewing Round 1 MUST apply Round 2 (latest round) as ONE undo step!
	if err := f.ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview: %v", err)
	}
	keptStatus := f.ed.ReviewStatus()
	if keptStatus.Outcome != editor.ReviewOutcomeKept || keptStatus.LastRounds != 2 || !strings.Contains(keptStatus.LastOutcomeMsg, "2 rounds") {
		t.Fatalf("keptStatus = %+v", keptStatus)
	}

	gotScene, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	gotPrefab, err := os.ReadFile(filepath.Join(f.dir, "prefabs", "slime.prefab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotScene), "x: 412") {
		t.Fatalf("kept scene missing Round 2 x: 412:\n%s", gotScene)
	}
	if strings.Contains(string(gotScene), note1) || strings.Contains(string(gotScene), note2) ||
		strings.Contains(string(gotPrefab), note1) || strings.Contains(string(gotPrefab), note2) {
		t.Fatal("review note leaked into saved project YAML")
	}

	// ONE Undo restores EVERY file and open Editor.Doc to exact pre-request bytes.
	if !f.ed.Undo() {
		t.Fatal("expected single Undo to succeed")
	}
	afterUndoScene, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	afterUndoPrefab, err := os.ReadFile(filepath.Join(f.dir, "prefabs", "slime.prefab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndoScene, f.level1Before) {
		t.Fatalf("level_01 after 1 Undo differs:\ngot:\n%s\nwant:\n%s", afterUndoScene, f.level1Before)
	}
	if !bytes.Equal(afterUndoPrefab, f.prefabBefore) {
		t.Fatalf("slime.prefab.yaml after 1 Undo differs:\ngot:\n%s\nwant:\n%s", afterUndoPrefab, f.prefabBefore)
	}
	if obj, _, _ := f.ed.FindObject("slime_03"); obj != nil {
		t.Fatal("slime_03 still present in Editor.Doc after 1 Undo")
	}
}

func TestReviewNoteAddedAndRemovedBeforeSendingNeverReachesAgent(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	snap := f.ed.ReviewStatus()
	slime1 := findReviewChangeByObject(t, snap, "slime_01")
	slime3 := findReviewChangeByObject(t, snap, "slime_03")

	if err := f.ed.OpenReviewNoteComposer(slime1.ID); err != nil {
		t.Fatal(err)
	}
	f.ed.SetReviewNoteDraft("Temporary note that will be removed")
	if err := f.ed.CommitReviewNote(); err != nil {
		t.Fatal(err)
	}
	if f.ed.ReviewDraftNoteCount() != 1 {
		t.Fatalf("draft count = %d, want 1", f.ed.ReviewDraftNoteCount())
	}
	if err := f.ed.RemoveReviewNote(slime1.ID); err != nil {
		t.Fatal(err)
	}
	if f.ed.ReviewDraftNoteCount() != 0 {
		t.Fatalf("draft count after remove = %d, want 0", f.ed.ReviewDraftNoteCount())
	}
	if pending := f.ed.PendingReviewNotes(); pending != nil {
		t.Fatalf("PendingReviewNotes = %+v, want nil", pending)
	}
	if _, err := f.ed.SendReviewNotes(); !errors.Is(err, editor.ErrReviewNoNotesToSend) {
		t.Fatalf("SendReviewNotes after remove = %v, want ErrReviewNoNotesToSend", err)
	}

	// Add and remove another note alongside a real note; only the real note is sent.
	if err := f.ed.SetReviewNote(slime1.ID, "Never send this"); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.RemoveReviewNote(slime1.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetReviewNote(slime3.ID, "Move left 40 px"); err != nil {
		t.Fatal(err)
	}
	batch, err := f.ed.SendReviewNotes()
	if err != nil {
		t.Fatalf("SendReviewNotes: %v", err)
	}
	if len(batch.Notes) != 1 || batch.Notes[0].ChangeID != slime3.ID {
		t.Fatalf("batch = %+v, want only %s", batch.Notes, slime3.ID)
	}
}

func TestReviewRevertAndRedoWithThisNote(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	snap := f.ed.ReviewStatus()
	slime3 := findReviewChangeByObject(t, snap, "slime_03")

	batch, err := f.ed.RevertAndRedoReviewChange(slime3.ID, "Place it on the floor instead.")
	if err != nil {
		t.Fatalf("RevertAndRedoReviewChange: %v", err)
	}
	if len(batch.Notes) != 1 || batch.Notes[0].ChangeID != slime3.ID || !batch.Notes[0].RevertFirst {
		t.Fatalf("batch = %+v, want RevertFirst=true", batch.Notes)
	}
	s3 := findReviewChangeByObject(t, f.ed.ReviewStatus(), "slime_03")
	if !s3.Reverted {
		t.Fatal("expected slime_03 to be marked reverted until redone")
	}

	yFloor := float32(278)
	if err := f.ed.ApplyReviewRedo([]editor.ReviewRedoChange{{ChangeID: slime3.ID, Y: &yFloor}}); err != nil {
		t.Fatalf("ApplyReviewRedo: %v", err)
	}
	s3After := findReviewChangeByObject(t, f.ed.ReviewStatus(), "slime_03")
	if s3After.Reverted || s3After.Seen || s3After.VersionFor(2).Y != 278 {
		t.Fatalf("slime_03 after redo = %+v", s3After)
	}
}

func TestReviewNoteBoundsAndUnnotedFileIsolation(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	snap := f.ed.ReviewStatus()
	slime3 := findReviewChangeByObject(t, snap, "slime_03")

	// Empty and oversized notes are refused.
	if err := f.ed.SetReviewNote(slime3.ID, "   \t\n "); !errors.Is(err, editor.ErrReviewNoteEmpty) {
		t.Fatalf("empty note err = %v, want ErrReviewNoteEmpty", err)
	}
	tooLong := strings.Repeat("a", 5000)
	if err := f.ed.SetReviewNote(slime3.ID, tooLong); !errors.Is(err, editor.ErrReviewNoteTooLong) {
		t.Fatalf("oversized note err = %v, want ErrReviewNoteTooLong", err)
	}

	if err := f.ed.SetReviewNote(slime3.ID, "Shift left 40 px"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ed.SendReviewNotes(); err != nil {
		t.Fatal(err)
	}

	// Non-finite X must be refused.
	nanX := float32(math.NaN())
	if err := f.ed.ApplyReviewRedo([]editor.ReviewRedoChange{{ChangeID: slime3.ID, X: &nanX}}); err == nil {
		t.Fatal("expected NaN coordinate to be refused")
	}

	// Sneaking an edit to un-noted slime_01 inside slime_03's FileContent must NOT alter slime_01!
	sneakyDoc, err := sceneformat.Parse(f.level1After)
	if err != nil {
		t.Fatal(err)
	}
	for li := range sneakyDoc.Layers {
		for oi := range sneakyDoc.Layers[li].Objects {
			switch sneakyDoc.Layers[li].Objects[oi].ID {
			case "slime_03":
				sneakyDoc.Layers[li].Objects[oi].X = 412
			case "slime_01":
				sneakyDoc.Layers[li].Objects[oi].X = 999 // un-noted tamper attempt!
			}
		}
	}
	sneakyBytes, err := sceneformat.Encode(sneakyDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ed.ApplyReviewRedo([]editor.ReviewRedoChange{{ChangeID: slime3.ID, FileContent: sneakyBytes}}); err != nil {
		t.Fatalf("ApplyReviewRedo: %v", err)
	}
	if err := f.ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview: %v", err)
	}
	keptBytes, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	keptDoc, err := sceneformat.Parse(keptBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range keptDoc.Layers {
		for _, o := range l.Objects {
			if o.ID == "slime_03" && o.X != 412 {
				t.Fatalf("slime_03.X = %v, want 412", o.X)
			}
			if o.ID == "slime_01" && o.X == 999 {
				t.Fatal("un-noted slime_01 was silently modified via slime_03's FileContent!")
			}
		}
	}
}

func TestReviewNoteComposerFocusStabilityAndShortcuts(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	// Verify CmdReviewNote is registered in ScopeReview with 'c'.
	foundNoteCmd := false
	for _, cmd := range f.ed.Commands() {
		if cmd.ID == editor.CmdReviewNote && cmd.Scope == editor.ScopeReview && cmd.Shortcut == "c" {
			foundNoteCmd = true
		}
	}
	if !foundNoteCmd {
		t.Fatal("CmdReviewNote ('c') not registered in ScopeReview")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// Pressing 'c' opens the inline note composer on the focused change.
	app.Key("c")
	app.Settle()
	if f.ed.ReviewNoteComposerChangeID() == "" {
		t.Fatal("pressing 'c' did not open inline note composer")
	}
	if !app.Exists("id:review-note-input") {
		t.Fatal("review-note-input not mounted when composer open")
	}

	// Typing spaces and Review shortcut letters ('x', 'j', 'k', 'b', 'c') inside the composer
	// must NEVER fire shortcuts or trim trailing spaces!
	app.Type("id:review-note-input", "Move box j k x b c ")
	app.Settle()
	if got := f.ed.ReviewNoteDraft(); got != "Move box j k x b c " {
		t.Fatalf("draft while typing = %q, want trailing space preserved", got)
	}
	// Make sure typing 'x' did not revert the focused change and 'j' did not move focus.
	snap := f.ed.ReviewStatus()
	if snap.RevertedCount != 0 {
		t.Fatalf("typing 'x' in note composer reverted a change! reverted=%d", snap.RevertedCount)
	}

	// Pressing Esc cancels the composer without pausing the full-window Review screen.
	app.Key("escape")
	app.Settle()
	if f.ed.ReviewNoteComposerChangeID() != "" {
		t.Fatal("Esc did not cancel inline note composer")
	}
	if !f.ed.ReviewOpen() {
		t.Fatal("Esc while composer was open should cancel composer first, not pause review")
	}
}
