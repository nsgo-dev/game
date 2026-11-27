package undo

import (
	"bytes"
	"fmt"
	"io"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// LogVersion is the schema version of the Log YAML format this package reads and writes. Version 2 adds the forgotten key; version 1 logs, which predate History.Limit and so forgot nothing, are still read.
const LogVersion = 2

// Log is the saved form of a History: its moves in order, how many of them are applied and how many a Limit forgot before them. It does not hold the starting state, which is the state given to New or, after a Clear, the state at that Clear; a game saves that state with the log and passes it to Restore. It is written as YAML with its fields in the order below, the first three and moves required:
//
//	version: 2
//	applied: 2
//	forgotten: 0
//	moves:
//	  - dir: left
//	  - dir: up
//	  - dir: up
//
// M must be a concrete type the YAML package encodes and decodes, keeping what a move records during Apply in unexported fields or fields tagged `yaml:"-"`, so a Log holds only what a player chose.
type Log[M any] struct {
	Version   int `yaml:"version"`   // LogVersion; version 1 is read as 2 with no forgotten moves
	Applied   int `yaml:"applied"`   // moves applied, in [0, len(Moves)]
	Forgotten int `yaml:"forgotten"` // moves a History.Limit forgot before Moves; written since version 2
	Moves     []M `yaml:"moves"`     // every recorded move, applied first
}

// Log returns the history's moves as a Log of the current version. The slice is a copy, but moves that are pointers are shared with the history.
func (h *History[S, M]) Log() Log[M] {
	return Log[M]{Version: LogVersion, Applied: h.applied, Forgotten: h.forgotten, Moves: h.Moves()}
}

// Validate reports whether l has a supported version, an applied count within its moves and no forgotten moves a version 1 log cannot hold.
func (l Log[M]) Validate() error {
	if l.Version < 1 || l.Version > LogVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to 1 or %d at the top of the undo log", LogVersion), "undo: unsupported log version %d (want %d)", l.Version, LogVersion)
	}
	if l.Forgotten < 0 {
		return loaderr.Errorf("forgotten", "set forgotten to a non-negative integer", "undo: log forgets %d moves", l.Forgotten)
	}
	if l.Version == 1 && l.Forgotten > 0 {
		return loaderr.Errorf("forgotten", fmt.Sprintf("set version: %d or set forgotten: 0 for a version 1 log", LogVersion), "undo: log version 1 records %d forgotten moves, which version 2 introduced", l.Forgotten)
	}
	if l.Applied < 0 || l.Applied > len(l.Moves) {
		return loaderr.Errorf("applied", fmt.Sprintf("set applied to an integer in [0, %d]", len(l.Moves)), "undo: log applies %d of %d moves", l.Applied, len(l.Moves))
	}
	for i, m := range l.Moves {
		if isNil(m) {
			return loaderr.Errorf(fmt.Sprintf("moves[%d]", i), "replace the nil move entry with a concrete move value", "undo: log move %d is nil", i)
		}
	}
	return nil
}

// Restore returns a History over state, the starting state the log was recorded from (the state given to New, or the state at the history's last Clear), with the log's moves: it applies the first log.Applied moves to state in order and keeps the rest for Redo. OnChange is not called.
//
// A log whose Limit forgot moves cannot be restored: the forgotten moves' effects are part of the history's state but not of the starting state the log was recorded from, and the log no longer holds the moves that would replay them. Clear the history and save the state at the Clear with the log, as after any Clear, so the next log forgets nothing.
func Restore[S any, M Move[S]](state S, log Log[M]) (*History[S, M], error) {
	if err := log.Validate(); err != nil {
		return nil, err
	}
	if log.Forgotten > 0 {
		return nil, fmt.Errorf("undo: log forgot %d moves the starting state lacks; Clear the history and save its state with the log", log.Forgotten)
	}
	h := &History[S, M]{state: state, moves: append([]M(nil), log.Moves...), noMerge: true}
	for h.applied < log.Applied {
		h.moves[h.applied].Apply(state)
		h.applied++
	}
	return h, nil
}

// EncodeLog validates log and writes it as deterministic 2-space YAML of the current version: a log read from an older version is saved in the current form.
func EncodeLog[M any](log Log[M]) ([]byte, error) {
	if err := log.Validate(); err != nil {
		return nil, err
	}
	log.Version = LogVersion
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
		return log, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add an undo log starting with version: %d, applied: 0, and moves: []", LogVersion), "undo: empty log"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&log); err != nil {
		return Log[M]{}, loaderr.Locate(fmt.Errorf("undo: decode log: %w", err), "", data, "check the YAML syntax and field names in the undo log")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Log[M]{}, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the undo log and remove any --- separator", "undo: unexpected trailing YAML document"), "", data, "")
		}
		return Log[M]{}, loaderr.Locate(fmt.Errorf("undo: decode log: %w", err), "", data, "remove trailing YAML content after the undo log")
	}
	if err := requireLogKeys(data); err != nil {
		return Log[M]{}, loaderr.Locate(err, "", data, "")
	}
	if log.Moves == nil {
		log.Moves = []M{}
	}
	if err := log.Validate(); err != nil {
		return Log[M]{}, loaderr.Locate(err, "", data, "")
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
		return loaderr.Errorf("", "write the undo log as a YAML mapping with version, applied and moves keys", "undo: log is not a YAML mapping")
	}
	root := doc.Content[0]
	values := map[string]*yaml.Node{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		values[root.Content[i].Value] = root.Content[i+1]
	}
	for _, key := range []string{"version", "applied", "moves"} {
		if values[key] == nil {
			return loaderr.Errorf(key, fmt.Sprintf("add the required %s field to the undo log", key), "undo: log has no %s", key)
		}
	}
	if values["moves"].Kind != yaml.SequenceNode {
		return &loaderr.Error{
			Line:    values["moves"].Line,
			Column:  values["moves"].Column,
			Field:   "moves",
			Problem: "undo: log moves is not a list",
			Fix:     "set moves to a YAML list (use [] for an empty list)",
		}
	}
	return nil
}
