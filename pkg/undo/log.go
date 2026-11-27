package undo

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// LogVersion is the schema version of the Log YAML format this package reads and writes.
const LogVersion = 1

// Log is the saved form of a History: its moves in order and how many of them are applied. It does not hold the starting state, which is the state given to New or, after a Clear, the state at that Clear; a game saves that state with the log and passes it to Restore. It is written as YAML with its fields in the order below, all three required:
//
//	version: 1
//	applied: 2
//	moves:
//	  - dir: left
//	  - dir: up
//	  - dir: up
//
// M must be a concrete type the YAML package encodes and decodes, keeping what a move records during Apply in unexported fields or fields tagged `yaml:"-"`, so a Log holds only what a player chose.
type Log[M any] struct {
	Version int `yaml:"version"` // LogVersion
	Applied int `yaml:"applied"` // moves applied, in [0, len(Moves)]
	Moves   []M `yaml:"moves"`   // every recorded move, applied first
}

// Log returns the history's moves as a Log of the current version. The slice is a copy, but moves that are pointers are shared with the history.
func (h *History[S, M]) Log() Log[M] {
	return Log[M]{Version: LogVersion, Applied: h.applied, Moves: h.Moves()}
}

// Validate reports whether l has the current version and an applied count within its moves.
func (l Log[M]) Validate() error {
	if l.Version != LogVersion {
		return fmt.Errorf("undo: unsupported log version %d (want %d)", l.Version, LogVersion)
	}
	if l.Applied < 0 || l.Applied > len(l.Moves) {
		return fmt.Errorf("undo: log applies %d of %d moves", l.Applied, len(l.Moves))
	}
	for i, m := range l.Moves {
		if isNil(m) {
			return fmt.Errorf("undo: log move %d is nil", i)
		}
	}
	return nil
}

// Restore returns a History over state, the starting state the log was recorded from (the state given to New, or the state at the history's last Clear), with the log's moves: it applies the first log.Applied moves to state in order and keeps the rest for Redo. OnChange is not called.
func Restore[S any, M Move[S]](state S, log Log[M]) (*History[S, M], error) {
	if err := log.Validate(); err != nil {
		return nil, err
	}
	h := &History[S, M]{state: state, moves: append([]M(nil), log.Moves...)}
	for h.applied < log.Applied {
		h.moves[h.applied].Apply(state)
		h.applied++
	}
	return h, nil
}

// EncodeLog validates log and writes it as deterministic 2-space YAML.
func EncodeLog[M any](log Log[M]) ([]byte, error) {
	if err := log.Validate(); err != nil {
		return nil, err
	}
	if log.Moves == nil {
		log.Moves = []M{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(log); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("undo: encode log: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("undo: encode log: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeLog reads a YAML Log and validates it. Missing version, applied or moves keys, a null moves list, unknown fields, in the log or in its moves, and trailing YAML documents are refused.
func DecodeLog[M any](data []byte) (Log[M], error) {
	var log Log[M]
	if len(bytes.TrimSpace(data)) == 0 {
		return log, errors.New("undo: empty log")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&log); err != nil {
		return Log[M]{}, fmt.Errorf("undo: decode log: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Log[M]{}, errors.New("undo: unexpected trailing YAML document")
		}
		return Log[M]{}, fmt.Errorf("undo: decode log: %w", err)
	}
	if err := requireLogKeys(data); err != nil {
		return Log[M]{}, err
	}
	if log.Moves == nil {
		log.Moves = []M{}
	}
	if err := log.Validate(); err != nil {
		return Log[M]{}, err
	}
	return log, nil
}

// requireLogKeys reports an error unless the first YAML document of data, already decoded as a Log, is a mapping with version, applied and a non-null moves, so a truncated or hand-edited save is not read as zero applied moves.
func requireLogKeys(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("undo: decode log: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("undo: log is not a YAML mapping")
	}
	root := doc.Content[0]
	values := map[string]*yaml.Node{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		values[root.Content[i].Value] = root.Content[i+1]
	}
	for _, key := range []string{"version", "applied", "moves"} {
		if values[key] == nil {
			return fmt.Errorf("undo: log has no %s", key)
		}
	}
	if values["moves"].Kind != yaml.SequenceNode {
		return errors.New("undo: log moves is not a list")
	}
	return nil
}
