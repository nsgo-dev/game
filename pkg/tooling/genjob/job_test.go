package genjob

import (
	"bytes"
	"strings"
	"testing"
)

func validJob() *Job {
	return &Job{
		Version: Version, RequestID: "req_123", Generator: "pixel-gen", Tool: "generate", Kind: "images", Target: "art/slime.png",
		Parameters: map[string]string{"prompt": "a slime", "count": "2"}, Fingerprint: strings.Repeat("a", 64), State: StateCompleted, Phase: "downloaded",
		Candidates: []Candidate{{FileName: "candidate-02.png", MediaType: "image/png", Bytes: 12, SHA256: strings.Repeat("b", 64)}, {FileName: "candidate-01.png", MediaType: "image/png", Bytes: 10, SHA256: strings.Repeat("c", 64)}},
	}
}

func TestRoundTripCanonicalAndSorted(t *testing.T) {
	got, err := Encode(validJob())
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nrequestId: req_123\ngenerator: pixel-gen\ntool: generate\nkind: images\ntarget: art/slime.png\nparameters:\n  count: \"2\"\n  prompt: a slime\nfingerprint: " + strings.Repeat("a", 64) + "\nstate: completed\nphase: downloaded\ncandidates:\n  - file: candidate-01.png\n    mediaType: image/png\n    bytes: 10\n    sha256: " + strings.Repeat("c", 64) + "\n  - file: candidate-02.png\n    mediaType: image/png\n    bytes: 12\n    sha256: " + strings.Repeat("b", 64) + "\n"
	if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("Encode =\n%s\nwant\n%s", got, want)
	}
	parsed, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(parsed)
	if err != nil || !bytes.Equal(again, got) {
		t.Fatalf("round trip changed bytes: err=%v\n%s", err, again)
	}
}

func TestParseRejectsUnknownDuplicateTrailingAndBadVersion(t *testing.T) {
	base, err := Encode(validJob())
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"unknown":   append(base, []byte("unknown: true\n")...),
		"duplicate": append(base, []byte("version: 1\n")...),
		"trailing":  append(base, []byte("---\nversion: 1\n")...),
		"version":   []byte(strings.Replace(string(base), "version: 1", "version: 2", 1)),
	} {
		if _, err := Parse(data); err == nil {
			t.Errorf("Parse(%s) succeeded", name)
		}
	}
}

func TestValidateCandidateHashAndPath(t *testing.T) {
	job := validJob()
	job.Candidates[0].SHA256 = strings.Repeat("A", 64)
	if err := Validate(job); err == nil {
		t.Fatal("uppercase hash accepted")
	}
	job = validJob()
	job.Candidates[0].FileName = "../candidate.png"
	if err := Validate(job); err == nil {
		t.Fatal("path traversal candidate accepted")
	}
}
