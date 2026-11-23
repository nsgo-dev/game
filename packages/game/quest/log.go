package quest

import (
	"errors"
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/flow"
)

var (
	// ErrUnknownQuest reports a quest ID the Log's book does not define.
	ErrUnknownQuest = errors.New("quest: unknown quest")
	// ErrUnknownObjective reports an objective ID the quest does not define.
	ErrUnknownObjective = errors.New("quest: unknown objective")
	// ErrNotStartable reports that a quest cannot start: its prerequisites are incomplete, it is already active, or it completed and is not repeatable.
	ErrNotStartable = errors.New("quest: quest cannot start")
	// ErrNotActive reports an operation that needs an active quest (or an active objective) on one that is not.
	ErrNotActive = errors.New("quest: quest is not active")
)

// State is where a quest stands in a Log.
type State uint8

const (
	// Inactive quests have not started, or were abandoned.
	Inactive State = iota
	// Active quests accept events for their objectives.
	Active
	// Completed quests finished every required objective.
	Completed
	// Failed quests were failed by the game with Log.Fail; Start can retry them.
	Failed
)

var stateNames = [...]string{Inactive: "inactive", Active: "active", Completed: "completed", Failed: "failed"}

// String returns the state's lowercase name, such as "active".
func (s State) String() string {
	if int(s) < len(stateNames) {
		return stateNames[s]
	}
	return fmt.Sprintf("State(%d)", uint8(s))
}

// MarshalText encodes s as its name, so saves read "active" rather than a number.
func (s State) MarshalText() ([]byte, error) {
	if int(s) >= len(stateNames) {
		return nil, fmt.Errorf("quest: invalid state %d", uint8(s))
	}
	return []byte(stateNames[s]), nil
}

// UnmarshalText decodes a state name written by MarshalText.
func (s *State) UnmarshalText(text []byte) error {
	for i, name := range stateNames {
		if string(text) == name {
			*s = State(i)
			return nil
		}
	}
	return fmt.Errorf("quest: unknown state %q", text)
}

// ObjectiveState is where an objective stands within its quest.
type ObjectiveState uint8

const (
	// ObjectiveLocked objectives ignore events: their quest is not active, or in an ordered quest an earlier required objective is incomplete.
	ObjectiveLocked ObjectiveState = iota
	// ObjectiveActive objectives accept matching events.
	ObjectiveActive
	// ObjectiveDone objectives reached their required count.
	ObjectiveDone
)

var objectiveStateNames = [...]string{ObjectiveLocked: "locked", ObjectiveActive: "active", ObjectiveDone: "done"}

// String returns the objective state's lowercase name, such as "locked".
func (s ObjectiveState) String() string {
	if int(s) < len(objectiveStateNames) {
		return objectiveStateNames[s]
	}
	return fmt.Sprintf("ObjectiveState(%d)", uint8(s))
}

// ObjectiveStatus is a read-only view of one objective's progress.
type ObjectiveStatus struct {
	// ID, Kind, Target, Optional and Text repeat the objective's definition.
	ID       string
	Kind     Kind
	Target   string
	Optional bool
	Text     string
	// Progress is how many units have been counted, from 0 to Required.
	Progress int
	// Required is how many units complete the objective.
	Required int
	// State is whether the objective is locked, active or done.
	State ObjectiveState
}

// UpdateKind says what changed in an Update.
type UpdateKind uint8

const (
	// QuestStarted reports that a quest became active, from Start or an auto-start.
	QuestStarted UpdateKind = iota + 1
	// ObjectiveProgressed reports that an objective's progress changed.
	ObjectiveProgressed
	// ObjectiveCompleted reports that an objective reached its required count.
	ObjectiveCompleted
	// QuestCompleted reports that a quest completed; Update.Rewards holds what the game should grant.
	QuestCompleted
	// QuestFailed reports that a quest failed through Log.Fail.
	QuestFailed
	// QuestAbandoned reports that an active quest returned to Inactive through Log.Abandon.
	QuestAbandoned
)

var updateKindNames = [...]string{QuestStarted: "questStarted", ObjectiveProgressed: "objectiveProgressed", ObjectiveCompleted: "objectiveCompleted", QuestCompleted: "questCompleted", QuestFailed: "questFailed", QuestAbandoned: "questAbandoned"}

// String returns the update kind's name, such as "questCompleted".
func (k UpdateKind) String() string {
	if k > 0 && int(k) < len(updateKindNames) {
		return updateKindNames[k]
	}
	return fmt.Sprintf("UpdateKind(%d)", uint8(k))
}

// Update reports one change in a Log. A Log delivers updates to OnUpdate and, while attached, publishes them on its flow.Bus.
type Update struct {
	// Kind says what changed.
	Kind UpdateKind
	// Quest is the quest's ID.
	Quest string
	// Objective is the objective's ID for ObjectiveProgressed and ObjectiveCompleted, and empty otherwise.
	Objective string
	// Progress and Required are the objective's progress after the change, for ObjectiveProgressed and ObjectiveCompleted.
	Progress int
	Required int
	// Rewards is a copy of the quest's rewards for QuestCompleted, and nil otherwise.
	Rewards []Reward
}

type eventKey struct {
	kind   Kind
	target string
}

type objRef struct {
	quest, objective int
}

type progress struct {
	state  State
	counts []int
}

// Log tracks one player's progress through a Book. Create it with NewLog.
//
// A Log is not safe for concurrent use; drive it from the game loop. Its methods may be called from OnUpdate and bus handlers: updates raised by such nested calls are delivered after the current ones, in order.
type Log struct {
	// OnUpdate, when set, receives every Update in the order the changes happened.
	OnUpdate func(Update)

	book     Book
	index    map[string]int
	byTarget map[eventKey][]objRef
	quests   []progress

	bus         *flow.Bus
	detach      func()
	pending     []Update
	dispatching bool
	eligible    []objRef
}

// NewLog validates book and returns a Log with every quest Inactive. The Log keeps its own copy of book, so later changes to book do not affect it.
func NewLog(book *Book) (*Log, error) {
	if err := Validate(book); err != nil {
		return nil, err
	}
	l := &Log{
		book:     cloneBook(book),
		index:    make(map[string]int, len(book.Quests)),
		byTarget: make(map[eventKey][]objRef),
		quests:   make([]progress, len(book.Quests)),
	}
	for i := range l.book.Quests {
		q := &l.book.Quests[i]
		l.index[q.ID] = i
		l.quests[i].counts = make([]int, len(q.Objectives))
		for j, o := range q.Objectives {
			key := eventKey{o.Kind, o.Target}
			l.byTarget[key] = append(l.byTarget[key], objRef{i, j})
		}
	}
	return l, nil
}

func cloneBook(b *Book) Book {
	out := Book{Version: b.Version, Quests: make([]Quest, len(b.Quests))}
	for i, q := range b.Quests {
		q.Requires = slices.Clone(q.Requires)
		q.Objectives = slices.Clone(q.Objectives)
		q.Rewards = slices.Clone(q.Rewards)
		out.Quests[i] = q
	}
	return out
}

// Book returns a copy of the book the Log runs.
func (l *Log) Book() *Book {
	b := cloneBook(&l.book)
	return &b
}

// State returns the quest's state; an unknown ID reports Inactive.
func (l *Log) State(id string) State {
	i, ok := l.index[id]
	if !ok {
		return Inactive
	}
	return l.quests[i].state
}

// Quests returns the IDs of the quests in state s, in book order.
func (l *Log) Quests(s State) []string {
	var out []string
	for i := range l.quests {
		if l.quests[i].state == s {
			out = append(out, l.book.Quests[i].ID)
		}
	}
	return out
}

// CanStart reports whether Start(id) would succeed.
func (l *Log) CanStart(id string) bool {
	i, ok := l.index[id]
	return ok && l.startable(i)
}

func (l *Log) startable(i int) bool {
	q := &l.book.Quests[i]
	switch l.quests[i].state {
	case Active:
		return false
	case Completed:
		if !q.Repeatable {
			return false
		}
	}
	for _, req := range q.Requires {
		if l.quests[l.index[req]].state != Completed {
			return false
		}
	}
	return true
}

// Start makes the quest active with no progress. It fails with ErrNotStartable while a prerequisite is incomplete, while the quest is active, or after it completed unless it is Repeatable; a failed quest can start again.
func (l *Log) Start(id string) error {
	i, ok := l.index[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownQuest, id)
	}
	if !l.startable(i) {
		return fmt.Errorf("%w: %q is %s", ErrNotStartable, id, l.quests[i].state)
	}
	l.start(i)
	l.flush()
	return nil
}

func (l *Log) start(i int) {
	p := &l.quests[i]
	p.state = Active
	clear(p.counts)
	l.emit(Update{Kind: QuestStarted, Quest: l.book.Quests[i].ID})
}

// StartReady starts every AutoStart quest that is Inactive and whose prerequisites are complete, in book order, and returns how many started. A Log calls it after every quest completion; call it yourself once after NewLog or Restore so quests with no prerequisites begin.
func (l *Log) StartReady() int {
	n := l.startReady()
	l.flush()
	return n
}

func (l *Log) startReady() int {
	n := 0
	for i := range l.book.Quests {
		if l.book.Quests[i].AutoStart && l.quests[i].state == Inactive && l.startable(i) {
			l.start(i)
			n++
		}
	}
	return n
}

// Fail marks an active quest Failed, keeping its progress for display until it starts again.
func (l *Log) Fail(id string) error {
	i, err := l.activeQuest(id)
	if err != nil {
		return err
	}
	l.quests[i].state = Failed
	l.emit(Update{Kind: QuestFailed, Quest: id})
	l.flush()
	return nil
}

// Abandon returns an active quest to Inactive and drops its progress.
func (l *Log) Abandon(id string) error {
	i, err := l.activeQuest(id)
	if err != nil {
		return err
	}
	p := &l.quests[i]
	p.state = Inactive
	clear(p.counts)
	l.emit(Update{Kind: QuestAbandoned, Quest: id})
	l.flush()
	return nil
}

func (l *Log) activeQuest(id string) (int, error) {
	i, ok := l.index[id]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownQuest, id)
	}
	if l.quests[i].state != Active {
		return 0, fmt.Errorf("%w: %q is %s", ErrNotActive, id, l.quests[i].state)
	}
	return i, nil
}

// Objectives returns the status of each of the quest's objectives in definition order, or nil for an unknown ID.
func (l *Log) Objectives(id string) []ObjectiveStatus {
	i, ok := l.index[id]
	if !ok {
		return nil
	}
	q := &l.book.Quests[i]
	out := make([]ObjectiveStatus, len(q.Objectives))
	for j, o := range q.Objectives {
		out[j] = ObjectiveStatus{
			ID:       o.ID,
			Kind:     o.Kind,
			Target:   o.Target,
			Optional: o.Optional,
			Text:     o.Text,
			Progress: l.quests[i].counts[j],
			Required: o.Required(),
			State:    l.objectiveState(i, j),
		}
	}
	return out
}

// Objective returns the status of one objective; ok is false when the quest or objective is unknown.
func (l *Log) Objective(questID, objectiveID string) (status ObjectiveStatus, ok bool) {
	for _, s := range l.Objectives(questID) {
		if s.ID == objectiveID {
			return s, true
		}
	}
	return ObjectiveStatus{}, false
}

func (l *Log) objectiveState(i, j int) ObjectiveState {
	q := &l.book.Quests[i]
	p := &l.quests[i]
	if p.counts[j] >= q.Objectives[j].Required() {
		return ObjectiveDone
	}
	if p.state != Active {
		return ObjectiveLocked
	}
	if q.Ordered {
		for k := range j {
			if !q.Objectives[k].Optional && p.counts[k] < q.Objectives[k].Required() {
				return ObjectiveLocked
			}
		}
	}
	return ObjectiveActive
}

// Handle advances every active objective whose kind and target match e by e.Amount (below 1 counts as 1), completing objectives and quests that reach their counts, and reports whether anything changed. In an ordered quest, an event that completes one objective does not also count toward the next.
func (l *Log) Handle(e Event) bool {
	refs := l.byTarget[eventKey{e.Kind, e.Target}]
	if len(refs) == 0 {
		return false
	}
	amount := max(e.Amount, 1)
	// Decide eligibility before applying the event, so one event cannot unlock and then advance the next objective of an ordered quest.
	eligible := l.eligible[:0]
	for _, r := range refs {
		if l.objectiveState(r.quest, r.objective) == ObjectiveActive {
			eligible = append(eligible, r)
		}
	}
	l.eligible = eligible[:0]
	if len(eligible) == 0 {
		return false
	}
	for _, r := range eligible {
		cur := l.quests[r.quest].counts[r.objective]
		// Clamp before adding so a huge Amount cannot overflow.
		l.advance(r.quest, r.objective, cur+min(amount, l.book.Quests[r.quest].Objectives[r.objective].Required()-cur))
	}
	l.flush()
	return true
}

// SetProgress sets an active objective's progress to n, clamped to [0, Required], completing it and its quest when n reaches the count. Use it to mirror a value the game already tracks, such as how many of an item the inventory holds. It fails with ErrNotActive when the quest is not active or the objective is locked or already completed.
func (l *Log) SetProgress(questID, objectiveID string, n int) error {
	i, err := l.activeQuest(questID)
	if err != nil {
		return err
	}
	j := slices.IndexFunc(l.book.Quests[i].Objectives, func(o Objective) bool { return o.ID == objectiveID })
	if j < 0 {
		return fmt.Errorf("%w: %q in quest %q", ErrUnknownObjective, objectiveID, questID)
	}
	if st := l.objectiveState(i, j); st != ObjectiveActive {
		return fmt.Errorf("%w: objective %q in quest %q is %s", ErrNotActive, objectiveID, questID, st)
	}
	l.advance(i, j, max(n, 0))
	l.flush()
	return nil
}

func (l *Log) advance(i, j, n int) {
	q := &l.book.Quests[i]
	p := &l.quests[i]
	if p.state != Active {
		return
	}
	o := &q.Objectives[j]
	n = min(n, o.Required())
	if n == p.counts[j] {
		return
	}
	p.counts[j] = n
	u := Update{Kind: ObjectiveProgressed, Quest: q.ID, Objective: o.ID, Progress: n, Required: o.Required()}
	l.emit(u)
	if n < o.Required() {
		return
	}
	u.Kind = ObjectiveCompleted
	l.emit(u)
	if !requiredDone(q, p.counts) {
		return
	}
	p.state = Completed
	l.emit(Update{Kind: QuestCompleted, Quest: q.ID, Rewards: slices.Clone(q.Rewards)})
	l.startReady()
}

func (l *Log) emit(u Update) {
	l.pending = append(l.pending, u)
}

// flush delivers pending updates in order. Nested calls from handlers only queue theirs, so the outermost call delivers everything FIFO.
func (l *Log) flush() {
	if l.dispatching {
		return
	}
	l.dispatching = true
	defer func() {
		clear(l.pending)
		l.pending = l.pending[:0]
		l.dispatching = false
	}()
	for k := 0; k < len(l.pending); k++ {
		u := l.pending[k]
		if l.OnUpdate != nil {
			l.OnUpdate(u)
		}
		if l.bus != nil {
			flow.Publish(l.bus, u)
		}
	}
}
