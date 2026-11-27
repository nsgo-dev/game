package quest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const (
	fixBook     = "write the quest book as version: 1 YAML with valid quests, objectives and rewards"
	fixReadBook = "check that the quest book file exists and is readable"
)

// FormatVersion is the quest book schema version this package reads and writes.
const FormatVersion = 1

// ErrInvalidBook reports that a quest book breaks the schema: a wrong version, a missing or duplicate ID, an unknown objective kind or reference, a negative count, or a prerequisite cycle.
var ErrInvalidBook = errors.New("quest: invalid book")

// Kind is what an objective asks the player to do.
type Kind string

const (
	// KindCollect counts items picked up, matched by item ID (Collected events).
	KindCollect Kind = "collect"
	// KindReach completes when the player arrives at a place, matched by place ID (Reached events).
	KindReach Kind = "reach"
	// KindDefeat counts enemies defeated, matched by enemy ID (Defeated events).
	KindDefeat Kind = "defeat"
	// KindTalk completes when the player talks to a character, matched by character ID (Talked events).
	KindTalk Kind = "talk"
)

// Valid reports whether k is one of KindCollect, KindReach, KindDefeat or KindTalk.
func (k Kind) Valid() bool {
	switch k {
	case KindCollect, KindReach, KindDefeat, KindTalk:
		return true
	}
	return false
}

// Book is the root of a versioned YAML quest file.
type Book struct {
	// Version is the schema version; it must equal FormatVersion.
	Version int `yaml:"version"`
	// Quests lists the book's quests; Log reports them in this order.
	Quests []Quest `yaml:"quests,omitempty"`
}

// Quest is one quest: its objectives, prerequisites and rewards.
type Quest struct {
	// ID uniquely identifies the quest within the book and in saves.
	ID string `yaml:"id"`
	// Title is the quest's display name or a localization key; the package does not interpret it.
	Title string `yaml:"title,omitempty"`
	// Description is the quest's journal text or a localization key; the package does not interpret it.
	Description string `yaml:"description,omitempty"`
	// Requires lists the IDs of quests that must be completed before this one can start.
	Requires []string `yaml:"requires,omitempty,flow"`
	// AutoStart starts the quest as soon as its prerequisites are complete, on Log.StartReady and after every quest completion.
	AutoStart bool `yaml:"autoStart,omitempty"`
	// Repeatable lets Log.Start begin the quest again after it completed.
	Repeatable bool `yaml:"repeatable,omitempty"`
	// Ordered makes objectives advance one at a time in list order: an objective accepts events only once every required objective before it is complete.
	Ordered bool `yaml:"ordered,omitempty"`
	// Objectives are the quest's goals; the quest completes when every non-optional one is complete. At least one is required.
	Objectives []Objective `yaml:"objectives"`
	// Rewards are reported in the QuestCompleted update for the game to grant.
	Rewards []Reward `yaml:"rewards,omitempty"`
}

// Objective is one goal of a quest.
type Objective struct {
	// ID uniquely identifies the objective within its quest and in saves.
	ID string `yaml:"id"`
	// Kind is what the objective asks for.
	Kind Kind `yaml:"kind"`
	// Target is the item, place, enemy or character ID that events must name to advance the objective.
	Target string `yaml:"target"`
	// Count is how many matching events (or how much collected) complete the objective; 0 means 1.
	Count int `yaml:"count,omitempty"`
	// Optional objectives never block the quest's completion or, in an ordered quest, later objectives.
	Optional bool `yaml:"optional,omitempty"`
	// Text is the objective's journal line or a localization key; the package does not interpret it.
	Text string `yaml:"text,omitempty"`
}

// Required returns how many units complete o: Count, or 1 when Count is 0.
func (o Objective) Required() int {
	if o.Count <= 0 {
		return 1
	}
	return o.Count
}

// Reward is something a completed quest grants. The package reports rewards and never grants them, so they can name inventory items, stats, currency or anything else a game defines.
type Reward struct {
	// Kind optionally groups rewards for the game, such as "item", "xp" or "gold"; the package does not interpret it.
	Kind string `yaml:"kind,omitempty"`
	// ID names the rewarded thing.
	ID string `yaml:"id"`
	// Amount is how many are granted; 0 means 1.
	Amount int `yaml:"amount,omitempty"`
}

// Quantity returns how many r grants: Amount, or 1 when Amount is 0.
func (r Reward) Quantity() int {
	if r.Amount <= 0 {
		return 1
	}
	return r.Amount
}

// Quest returns the quest with the given ID, or nil when b has none.
func (b *Book) Quest(id string) *Quest {
	if b == nil {
		return nil
	}
	for i := range b.Quests {
		if b.Quests[i].ID == id {
			return &b.Quests[i]
		}
	}
	return nil
}

// Parse decodes a YAML quest book and validates it. Unknown fields and trailing YAML documents are refused. An empty list, such as `requires: []`, decodes the same as an omitted one (nil), so a parsed book equals Parse(Encode(book)).
//
// Errors are *loaderr.Error values wrapping ErrInvalidBook with line, field and fix; callers that read from a file name it with loaderr.WithFile.
func Parse(data []byte) (*Book, error) {
	b, err := parseBook(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixBook)
	}
	return b, nil
}

func parseBook(data []byte) (*Book, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixBook, "%w: empty document", ErrInvalidBook)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var b Book
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrInvalidBook, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the quest book; remove the trailing --- document", "%w: unexpected trailing YAML document", ErrInvalidBook)
		}
		return nil, fmt.Errorf("%w: decode: %w", ErrInvalidBook, err)
	}
	if err := Validate(&b); err != nil {
		return nil, err
	}
	normalize(&b)
	return &b, nil
}

// normalize turns empty lists into nil, the form Encode omits, so Parse(Encode(b)) is deeply equal to b.
func normalize(b *Book) {
	if len(b.Quests) == 0 {
		b.Quests = nil
	}
	for i := range b.Quests {
		q := &b.Quests[i]
		if len(q.Requires) == 0 {
			q.Requires = nil
		}
		if len(q.Rewards) == 0 {
			q.Rewards = nil
		}
	}
}

// LoadFS reads and parses the quest book at name in fsys.
func LoadFS(fsys fs.FS, name string) (*Book, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("quest: read %s: %w", name, err), name, nil, fixReadBook)
	}
	b, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
	}
	return b, nil
}

// Encode validates b and writes it in canonical form: deterministic YAML with 2-space indentation, fields in declaration order, `requires` in flow style and everything else in block style, empty lists and zero values omitted, and no generated values. Parse(Encode(b)) equals b for a book from Parse, and Encode(Parse(f)) gives back f byte for byte when f is already canonical. A file that is not canonical comes back in canonical form, and YAML comments are not kept.
func Encode(b *Book) ([]byte, error) {
	if err := Validate(b); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(b); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("quest: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("quest: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks b against the schema: the version, unique non-empty IDs, known objective kinds with targets, non-negative counts and amounts, prerequisites that name other quests in the book, and no prerequisite cycles. Errors wrap ErrInvalidBook.
func Validate(b *Book) error {
	if b == nil {
		return loaderr.Errorf("", fixBook, "%w: nil book", ErrInvalidBook)
	}
	if b.Version != FormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", FormatVersion), "%w: unsupported version %d (want %d)", ErrInvalidBook, b.Version, FormatVersion)
	}
	index := make(map[string]int, len(b.Quests))
	for i := range b.Quests {
		q := &b.Quests[i]
		if err := validID(q.ID); err != nil {
			return loaderr.Errorf(loaderr.Path("quests", i, "id"), "give the quest a non-empty id without leading or trailing spaces", "%w: quest %d id: %v", ErrInvalidBook, i, err)
		}
		if _, dup := index[q.ID]; dup {
			return loaderr.Errorf(loaderr.Path("quests", i, "id"), "give each quest a unique id", "%w: duplicate quest id %q", ErrInvalidBook, q.ID)
		}
		index[q.ID] = i
	}
	for i := range b.Quests {
		if err := validateQuest(i, &b.Quests[i], index); err != nil {
			return err
		}
	}
	return checkCycles(b, index)
}

func validateQuest(qi int, q *Quest, index map[string]int) error {
	qPath := loaderr.Path("quests", qi)
	seen := make(map[string]struct{}, len(q.Requires))
	for ri, req := range q.Requires {
		rPath := loaderr.Join(qPath, "requires", ri)
		if req == q.ID {
			return loaderr.Errorf(rPath, "remove the quest's own id from requires", "%w: quest %q requires itself", ErrInvalidBook, q.ID)
		}
		if _, ok := index[req]; !ok {
			return loaderr.Errorf(rPath, "name a quest id declared in this book or remove the prerequisite", "%w: quest %q requires unknown quest %q", ErrInvalidBook, q.ID, req)
		}
		if _, dup := seen[req]; dup {
			return loaderr.Errorf(rPath, "remove the duplicate prerequisite id", "%w: quest %q requires %q twice", ErrInvalidBook, q.ID, req)
		}
		seen[req] = struct{}{}
	}
	if len(q.Objectives) == 0 {
		return loaderr.Errorf(loaderr.Join(qPath, "objectives"), "add at least one required objective to the quest", "%w: quest %q has no objectives", ErrInvalidBook, q.ID)
	}
	required := 0
	objIDs := make(map[string]struct{}, len(q.Objectives))
	for j := range q.Objectives {
		o := &q.Objectives[j]
		oPath := loaderr.Join(qPath, "objectives", j)
		if err := validID(o.ID); err != nil {
			return loaderr.Errorf(loaderr.Join(oPath, "id"), "give the objective a non-empty id without leading or trailing spaces", "%w: quest %q objective %d id: %v", ErrInvalidBook, q.ID, j, err)
		}
		if _, dup := objIDs[o.ID]; dup {
			return loaderr.Errorf(loaderr.Join(oPath, "id"), "give each objective in the quest a unique id", "%w: quest %q has duplicate objective id %q", ErrInvalidBook, q.ID, o.ID)
		}
		objIDs[o.ID] = struct{}{}
		if !o.Kind.Valid() {
			return loaderr.Errorf(loaderr.Join(oPath, "kind"), "set kind to collect, reach, defeat or talk", "%w: quest %q objective %q has unknown kind %q (want collect, reach, defeat or talk)", ErrInvalidBook, q.ID, o.ID, o.Kind)
		}
		if err := validID(o.Target); err != nil {
			return loaderr.Errorf(loaderr.Join(oPath, "target"), "give the objective a non-empty target id without leading or trailing spaces", "%w: quest %q objective %q target: %v", ErrInvalidBook, q.ID, o.ID, err)
		}
		if o.Count < 0 {
			return loaderr.Errorf(loaderr.Join(oPath, "count"), "set count to a non-negative integer, or omit it for 1", "%w: quest %q objective %q has negative count %d", ErrInvalidBook, q.ID, o.ID, o.Count)
		}
		if !o.Optional {
			required++
		}
	}
	if required == 0 {
		return loaderr.Errorf(loaderr.Join(qPath, "objectives"), "make at least one objective non-optional", "%w: quest %q has only optional objectives", ErrInvalidBook, q.ID)
	}
	for k, r := range q.Rewards {
		rwPath := loaderr.Join(qPath, "rewards", k)
		if err := validID(r.ID); err != nil {
			return loaderr.Errorf(loaderr.Join(rwPath, "id"), "give the reward a non-empty id without leading or trailing spaces", "%w: quest %q reward %d id: %v", ErrInvalidBook, q.ID, k, err)
		}
		if r.Amount < 0 {
			return loaderr.Errorf(loaderr.Join(rwPath, "amount"), "set amount to a non-negative integer, or omit it for 1", "%w: quest %q reward %q has negative amount %d", ErrInvalidBook, q.ID, r.ID, r.Amount)
		}
	}
	return nil
}

func validID(id string) error {
	if id == "" {
		return errors.New("empty")
	}
	if strings.TrimSpace(id) != id {
		return fmt.Errorf("%q has leading or trailing space", id)
	}
	return nil
}

// checkCycles refuses prerequisite cycles, which would leave every quest in them unstartable.
func checkCycles(b *Book, index map[string]int) error {
	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]uint8, len(b.Quests))
	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			return loaderr.Errorf(loaderr.Path("quests", i, "requires"), "break the prerequisite cycle by removing one of the requires entries", "%w: prerequisite cycle through quest %q", ErrInvalidBook, b.Quests[i].ID)
		case done:
			return nil
		}
		state[i] = visiting
		for _, req := range b.Quests[i].Requires {
			if err := visit(index[req]); err != nil {
				return err
			}
		}
		state[i] = done
		return nil
	}
	for i := range b.Quests {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}
