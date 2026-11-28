package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublishRecordRoundTripStrictAndAtomic(t *testing.T) {
	dir := t.TempDir()
	rec := PublishRecord{Version: 1, Destination: PublishDestItchIO, Confirmations: []PublishConfirmation{{StepID: "account", ConfirmedAt: "2026-11-28T00:00:00Z"}}}
	if err := SavePublishRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(PublishRecordPath(dir, PublishDestItchIO))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "version: 1\ndestination: itch.io\n") {
		t.Fatalf("unexpected key order:\n%s", b)
	}
	got, err := LoadPublishRecord(dir, PublishDestItchIO)
	if err != nil || len(got.Confirmations) != 1 {
		t.Fatalf("load = %+v, err=%v", got, err)
	}
	if err := os.WriteFile(PublishRecordPath(dir, PublishDestItchIO), []byte("version: 1\ndestination: itch.io\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPublishRecord(dir, PublishDestItchIO); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".game", "publish")); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRecordUploadClearsVersionConfirmationsAndProposesNext(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "game.zip")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := PublishRecord{Version: 1, Destination: PublishDestGooglePlay, Confirmations: []PublishConfirmation{{StepID: "account", ConfirmedAt: "x"}, {StepID: "review", Version: "1.2.3", ConfirmedAt: "x"}}}
	if err := rec.RecordUpload("1.2.3", "7", "game.aab", artifact, "https://store.example/upload", 8, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if rec.LastUpload.ArtifactSHA != "c7c5c1d70c5dec4416ab6158afd0b223ef40c29b1dc1f97ed9428b94d4cadb1c" {
		t.Fatalf("sha = %q", rec.LastUpload.ArtifactSHA)
	}
	if len(rec.Confirmations) != 1 || rec.Confirmations[0].StepID != "account" {
		t.Fatalf("confirmations = %+v", rec.Confirmations)
	}
	status := rec.Status()
	if status.ProposedVersion != "1.2.4" || status.ProposedBuild != "8" {
		t.Fatalf("proposed = %+v", status)
	}
	if err := rec.ConfirmPublishStep("review", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRecordRequiresUploadForVersionConfirmation(t *testing.T) {
	rec := PublishRecord{Version: 1, Destination: PublishDestTestFlight}
	if err := rec.ConfirmPublishStep("review", time.Now()); err == nil {
		t.Fatal("confirmed per-version step without upload")
	}
}

func TestPublishRecordSaveLoadSaveIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	rec := PublishRecord{
		Version:       1,
		Destination:   PublishDestGooglePlay,
		LastUpload:    &PublishUpload{Version: "1.2.3", BuildNumber: "7", ArtifactName: "game.aab", ArtifactSHA: "abc", SizeBytes: 8, UploadedAt: "2026-11-28T00:00:00Z", ResultURL: "https://store.example/upload", Outcome: "uploaded"},
		Confirmations: []PublishConfirmation{{StepID: "account", ConfirmedAt: "2026-11-28T00:00:00Z"}, {StepID: "review", Version: "1.2.3", ConfirmedAt: "2026-11-28T01:00:00Z"}},
	}
	if err := SavePublishRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	path := PublishRecordPath(dir, PublishDestGooglePlay)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = `version: 1
destination: google_play_internal
lastUpload:
    version: 1.2.3
    buildNumber: "7"
    artifactName: game.aab
    artifactSHA256: abc
    sizeBytes: 8
    uploadedAt: "2026-11-28T00:00:00Z"
    resultURL: https://store.example/upload
    outcome: uploaded
confirmations:
    - stepID: account
      confirmedAt: "2026-11-28T00:00:00Z"
    - stepID: review
      version: 1.2.3
      confirmedAt: "2026-11-28T01:00:00Z"
`
	if string(first) != want {
		t.Fatalf("saved record:\n%s\nwant:\n%s", first, want)
	}
	loaded, err := LoadPublishRecord(dir, PublishDestGooglePlay)
	if err != nil {
		t.Fatal(err)
	}
	if err := SavePublishRecord(dir, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("round trip changed the file:\n%s\nwas:\n%s", second, first)
	}
}

func TestPublishRecordRefusesMissingOrUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := PublishRecordPath(dir, PublishDestItchIO)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"destination: itch.io\n", "version: 2\ndestination: itch.io\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPublishRecord(dir, PublishDestItchIO); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestPublishRecordLeftoverTempFileKeepsPreviousRecord(t *testing.T) {
	dir := t.TempDir()
	rec := PublishRecord{Version: 1, Destination: PublishDestItchIO, Confirmations: []PublishConfirmation{{StepID: "account", ConfirmedAt: "2026-11-28T00:00:00Z"}}}
	if err := SavePublishRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	// A write interrupted before the rename leaves only a partial temp file beside the record.
	partial := filepath.Join(filepath.Dir(PublishRecordPath(dir, PublishDestItchIO)), ".publish-interrupted.yaml")
	if err := os.WriteFile(partial, []byte("version: 1\ndestination: itch"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPublishRecord(dir, PublishDestItchIO)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Confirmations) != 1 || got.Confirmations[0].StepID != "account" {
		t.Fatalf("previous record = %+v", got)
	}
}

func TestPublishStatusProposesNothingBeforeFirstUpload(t *testing.T) {
	status := PublishRecord{Version: 1, Destination: PublishDestTestFlight}.Status()
	if status.ProposedVersion != "" || status.ProposedBuild != "" {
		t.Fatalf("proposed without an upload: %+v", status)
	}
	if status.CurrentStep != "account" {
		t.Fatalf("current step = %q", status.CurrentStep)
	}
}
