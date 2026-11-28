package editor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const publishRecordVersion = 1

// PublishStepKind describes whether a step can be checked by tooling or needs a person.
type PublishStepKind string

const (
	PublishStepChecked   PublishStepKind = "checked"
	PublishStepConfirmed PublishStepKind = "confirmed"
)

// PublishStepScope controls whether a confirmation survives a new upload.
type PublishStepScope string

const (
	PublishStepPerVersion PublishStepScope = "per_version"
	PublishStepPerApp     PublishStepScope = "per_app"
)

// PublishStep is one ordered release or store-console step.
type PublishStep struct {
	ID    string           `yaml:"id"`
	Label string           `yaml:"label"`
	Kind  PublishStepKind  `yaml:"kind"`
	Scope PublishStepScope `yaml:"scope"`
}

// PublishUpload records the artifact most recently uploaded to a destination.
type PublishUpload struct {
	Version      string `yaml:"version"`
	BuildNumber  string `yaml:"buildNumber,omitempty"`
	ArtifactName string `yaml:"artifactName"`
	ArtifactSHA  string `yaml:"artifactSHA256"`
	SizeBytes    int64  `yaml:"sizeBytes"`
	UploadedAt   string `yaml:"uploadedAt"`
	ResultURL    string `yaml:"resultURL,omitempty"`
	Outcome      string `yaml:"outcome"`
}

// PublishConfirmation records a person-confirmed step and its version scope.
type PublishConfirmation struct {
	StepID      string `yaml:"stepID"`
	Version     string `yaml:"version,omitempty"`
	ConfirmedAt string `yaml:"confirmedAt"`
}

// PublishRecord is the local, credential-free state for one project and destination.
type PublishRecord struct {
	Version       int                   `yaml:"version"`
	Destination   PublishDestination    `yaml:"destination"`
	LastUpload    *PublishUpload        `yaml:"lastUpload,omitempty"`
	Confirmations []PublishConfirmation `yaml:"confirmations,omitempty"`
}

// PublishStatusStep is the derived state of one publish step.
type PublishStatusStep struct {
	Step   PublishStep `yaml:"step"`
	State  string      `yaml:"state"`
	Detail string      `yaml:"detail,omitempty"`
}

// PublishStatusReport is the state shown by the editor and read-only agent tools.
type PublishStatusReport struct {
	Destination     PublishDestination  `yaml:"destination"`
	Steps           []PublishStatusStep `yaml:"steps"`
	CurrentStep     string              `yaml:"currentStep,omitempty"`
	ProposedVersion string              `yaml:"proposedVersion,omitempty"`
	ProposedBuild   string              `yaml:"proposedBuild,omitempty"`
}

// DefaultPublishSteps returns the ordered steps shared by the current destinations.
func DefaultPublishSteps(_ PublishDestination) []PublishStep {
	return []PublishStep{
		{ID: "account", Label: "Store account", Kind: PublishStepConfirmed, Scope: PublishStepPerApp},
		{ID: "upload", Label: "Upload artifact", Kind: PublishStepChecked, Scope: PublishStepPerVersion},
		{ID: "testers", Label: "Testers and track", Kind: PublishStepConfirmed, Scope: PublishStepPerVersion},
		{ID: "review", Label: "Store review", Kind: PublishStepConfirmed, Scope: PublishStepPerVersion},
		{ID: "release", Label: "Release to players", Kind: PublishStepConfirmed, Scope: PublishStepPerVersion},
	}
}

// PublishRecordPath returns the local state path for project and destination.
func PublishRecordPath(projectDir string, destination PublishDestination) string {
	return filepath.Join(projectDir, ".game", "publish", strings.ReplaceAll(string(destination), "/", "_")+".yaml")
}

// LoadPublishRecord loads a strict versioned record. Missing state returns a default record.
func LoadPublishRecord(projectDir string, destination PublishDestination) (PublishRecord, error) {
	path := PublishRecordPath(projectDir, destination)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PublishRecord{Version: publishRecordVersion, Destination: destination}, nil
	}
	if err != nil {
		return PublishRecord{}, err
	}
	var rec PublishRecord
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&rec); err != nil {
		return PublishRecord{}, fmt.Errorf("editor: decode publish record: %w", err)
	}
	if rec.Version != publishRecordVersion {
		return PublishRecord{}, fmt.Errorf("editor: unsupported publish record version %d", rec.Version)
	}
	if rec.Destination != destination {
		return PublishRecord{}, fmt.Errorf("editor: publish record destination %q does not match %q", rec.Destination, destination)
	}
	return rec, nil
}

// SavePublishRecord writes a record atomically with deterministic YAML field order.
func SavePublishRecord(projectDir string, rec PublishRecord) error {
	if rec.Version == 0 {
		rec.Version = publishRecordVersion
	}
	if rec.Version != publishRecordVersion || !rec.Destination.Valid() {
		return errors.New("editor: invalid publish record")
	}
	dir := filepath.Dir(PublishRecordPath(projectDir, rec.Destination))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".publish-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, PublishRecordPath(projectDir, rec.Destination))
}

// RecordUpload records a successful artifact upload and clears per-version confirmations.
func (r *PublishRecord) RecordUpload(version, build, artifactName, artifactPath, resultURL string, size int64, at time.Time) error {
	if r == nil {
		return errors.New("editor: nil publish record")
	}
	sum, err := sha256File(artifactPath)
	if err != nil {
		return err
	}
	r.LastUpload = &PublishUpload{Version: version, BuildNumber: build, ArtifactName: artifactName, ArtifactSHA: sum, SizeBytes: size, UploadedAt: at.UTC().Format(time.RFC3339), ResultURL: resultURL, Outcome: "uploaded"}
	steps := DefaultPublishSteps(r.Destination)
	kept := r.Confirmations[:0]
	for _, c := range r.Confirmations {
		for _, step := range steps {
			if step.ID == c.StepID && step.Scope == PublishStepPerApp {
				kept = append(kept, c)
			}
		}
	}
	r.Confirmations = kept
	return nil
}

// ConfirmPublishStep records a person-confirmed step. Per-version steps require a successful upload.
func (r *PublishRecord) ConfirmPublishStep(stepID string, at time.Time) error {
	if r == nil {
		return errors.New("editor: nil publish record")
	}
	var found *PublishStep
	for _, step := range DefaultPublishSteps(r.Destination) {
		if step.ID == stepID {
			found = &step
			break
		}
	}
	if found == nil || found.Kind != PublishStepConfirmed {
		return fmt.Errorf("editor: unknown confirmable publish step %q", stepID)
	}
	if found.Scope == PublishStepPerVersion && r.LastUpload == nil {
		return errors.New("editor: cannot confirm a per-version step before a successful upload")
	}
	version := ""
	if found.Scope == PublishStepPerVersion {
		version = r.LastUpload.Version
	}
	for i := range r.Confirmations {
		if r.Confirmations[i].StepID == stepID && r.Confirmations[i].Version == version {
			r.Confirmations[i].ConfirmedAt = at.UTC().Format(time.RFC3339)
			return nil
		}
	}
	r.Confirmations = append(r.Confirmations, PublishConfirmation{StepID: stepID, Version: version, ConfirmedAt: at.UTC().Format(time.RFC3339)})
	return nil
}

// Status derives the ordered state without guessing unavailable store facts.
func (r PublishRecord) Status() PublishStatusReport {
	report := PublishStatusReport{Destination: r.Destination}
	for _, step := range DefaultPublishSteps(r.Destination) {
		state := "unknown"
		detail := "store state is not available"
		if step.ID == "upload" {
			if r.LastUpload != nil && r.LastUpload.Outcome == "uploaded" {
				state, detail = "done", "uploaded artifact recorded"
			} else {
				state, detail = "todo", "upload an artifact"
			}
		} else {
			version := ""
			if step.Scope == PublishStepPerVersion && r.LastUpload != nil {
				version = r.LastUpload.Version
			}
			for _, c := range r.Confirmations {
				if c.StepID == step.ID && c.Version == version {
					state, detail = "done", "confirmed"
				}
			}
			if state == "unknown" && step.Kind == PublishStepConfirmed {
				state, detail = "todo", "person confirmation required"
			}
		}
		report.Steps = append(report.Steps, PublishStatusStep{Step: step, State: state, Detail: detail})
		if report.CurrentStep == "" && state != "done" {
			report.CurrentStep = step.ID
		}
	}
	// With no recorded upload there is nothing to follow, so the proposal stays empty rather than guessed.
	if r.LastUpload != nil {
		report.ProposedVersion = bumpPatchVersion(r.LastUpload.Version)
		report.ProposedBuild = bumpPatchVersion(r.LastUpload.BuildNumber)
	}
	return report
}

// sha256File streams path through SHA-256 so a multi-gigabyte artifact is never held in memory.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func bumpPatchVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return "1"
	}
	last, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return v
	}
	parts[len(parts)-1] = strconv.Itoa(last + 1)
	return strings.Join(parts, ".")
}
