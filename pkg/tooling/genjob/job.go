// Package genjob defines the durable, versioned record for an asset generation request.
package genjob

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const Version = 1

// State is the lifecycle state of a generation job.
type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed"
)

// Candidate records one validated output stored beside job.yaml.
type Candidate struct {
	FileName  string `yaml:"file"`
	MediaType string `yaml:"mediaType"`
	Bytes     int64  `yaml:"bytes"`
	SHA256    string `yaml:"sha256"`
}

// Job is the canonical record stored at .game/generated/<request-id>/job.yaml.
type Job struct {
	Version     int               `yaml:"version"`
	RequestID   string            `yaml:"requestId"`
	Generator   string            `yaml:"generator"`
	Tool        string            `yaml:"tool"`
	Kind        string            `yaml:"kind"`
	Target      string            `yaml:"target"`
	Parameters  map[string]string `yaml:"parameters,omitempty"`
	Fingerprint string            `yaml:"fingerprint"`
	State       State             `yaml:"state"`
	Phase       string            `yaml:"phase"`
	RemoteTask  string            `yaml:"remoteTask,omitempty"`
	Error       string            `yaml:"error,omitempty"`
	Candidates  []Candidate       `yaml:"candidates,omitempty"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Parse decodes and validates one strict generation job YAML document.
func Parse(data []byte) (*Job, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("genjob: empty document")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("genjob: parse YAML: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("genjob: document must be a YAML mapping")
	}
	if err := duplicateKeys(root.Content[0]); err != nil {
		return nil, err
	}
	var job Job
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&job); err != nil {
		return nil, fmt.Errorf("genjob: decode: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("genjob: trailing YAML document")
		}
		return nil, fmt.Errorf("genjob: trailing YAML: %w", err)
	}
	if err := Validate(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

// Encode validates and writes deterministic two-space YAML.
func Encode(job *Job) ([]byte, error) {
	if err := Validate(job); err != nil {
		return nil, err
	}
	cp := *job
	if job.Parameters != nil {
		cp.Parameters = make(map[string]string, len(job.Parameters))
		keys := make([]string, 0, len(job.Parameters))
		for key := range job.Parameters {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			cp.Parameters[key] = job.Parameters[key]
		}
	}
	cp.Candidates = append([]Candidate(nil), job.Candidates...)
	sort.Slice(cp.Candidates, func(i, j int) bool { return cp.Candidates[i].FileName < cp.Candidates[j].FileName })
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&cp); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("genjob: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("genjob: encode: %w", err)
	}
	return out.Bytes(), nil
}

// Validate checks the version, lifecycle fields, identifiers and candidate metadata.
func Validate(job *Job) error {
	if job == nil {
		return errors.New("genjob: nil job")
	}
	if job.Version != Version {
		return fmt.Errorf("genjob: unsupported version %d (want %d)", job.Version, Version)
	}
	if !idPattern.MatchString(job.RequestID) {
		return fmt.Errorf("genjob: requestId %q must be 1..128 ASCII letters, digits, '-' or '_'", job.RequestID)
	}
	for name, value := range map[string]string{"generator": job.Generator, "tool": job.Tool, "kind": job.Kind, "target": job.Target, "fingerprint": job.Fingerprint, "phase": job.Phase} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("genjob: %s is required", name)
		}
	}
	switch job.State {
	case StateQueued, StateRunning, StateCompleted, StateCancelled, StateFailed:
	default:
		return fmt.Errorf("genjob: invalid state %q", job.State)
	}
	for key := range job.Parameters {
		if strings.TrimSpace(key) == "" {
			return errors.New("genjob: parameter keys must not be empty")
		}
	}
	for i, c := range job.Candidates {
		if strings.TrimSpace(c.FileName) == "" || strings.Contains(c.FileName, "/") || strings.Contains(c.FileName, "\\") || c.FileName == "." || c.FileName == ".." {
			return fmt.Errorf("genjob: candidates[%d].file must be a file name", i)
		}
		if strings.TrimSpace(c.MediaType) == "" {
			return fmt.Errorf("genjob: candidates[%d].mediaType is required", i)
		}
		if c.Bytes < 0 {
			return fmt.Errorf("genjob: candidates[%d].bytes must be non-negative", i)
		}
		if !isSHA256(c.SHA256) {
			return fmt.Errorf("genjob: candidates[%d].sha256 must be 64 lowercase hexadecimal characters", i)
		}
	}
	return nil
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func duplicateKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if seen[key] {
				return fmt.Errorf("genjob: duplicate field %q", key)
			}
			seen[key] = true
			if err := duplicateKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
	} else {
		for _, child := range node.Content {
			if err := duplicateKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}
