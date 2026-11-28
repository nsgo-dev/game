package gamedebug

// VLogFormat and VLogVersion identify the JSON Lines header WriteJSONL writes first and ReadVLog requires.
const (
	VLogFormat  = "gamedebug.vlog"
	VLogVersion = 1
)

// DefaultVLogCapacity is the number of entries, and separately of shapes, a VLog keeps when NewVLog is given a capacity of 0 or less.
const DefaultVLogCapacity = 4096

// Field is one named value in a Category snapshot.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Category is a gameplay system that can describe one entity, such as its abilities, stats, perception or navigation state. VLog.Snapshot records what a Category reports for an entity at a fixed step.
type Category interface {
	Name() string
	Snapshot(entity string) []Field
}

// VLogLevel is the severity of a VLogEntry.
type VLogLevel string

// The levels a VLogEntry may carry; ReadVLog refuses any other.
const (
	VLogInfo  VLogLevel = "info"
	VLogWarn  VLogLevel = "warn"
	VLogError VLogLevel = "error"
)

// VLogEntry is one message, or one Category snapshot, recorded for an entity at a fixed step.
type VLogEntry struct {
	Step     uint64    `json:"step"`
	Entity   string    `json:"entity"`
	Category string    `json:"category"`
	Level    VLogLevel `json:"level"`
	Message  string    `json:"message,omitempty"`
	Fields   []Field   `json:"fields,omitempty"`
}

// VLogShape is one debug shape, such as a path or a box outline, recorded for an entity at a fixed step. Kind names the shape and Points holds its world-space points.
type VLogShape struct {
	Step   uint64       `json:"step"`
	Entity string       `json:"entity"`
	Kind   string       `json:"kind"`
	Points [][3]float32 `json:"points,omitempty"`
}
