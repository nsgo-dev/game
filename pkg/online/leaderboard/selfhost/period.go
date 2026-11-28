package selfhost

import (
	"time"
)

// AllTimePeriod is the ID of an all-time board's only period.
const AllTimePeriod = "all"

// periodLayout is the form of a resetting board's period ID: the UTC date the period starts on.
const periodLayout = "2006-01-02"

// Period is one stretch of a board between resets.
type Period struct {
	// ID names the period: AllTimePeriod for an all-time board, otherwise the UTC date it starts on, such as "2026-11-23".
	ID string
	// Start is when the period starts; zero for an all-time board.
	Start time.Time
	// End is when the next period starts; zero for an all-time board.
	End time.Time
}

// length returns the length of the board's periods, or 0 for an all-time board.
func (b Board) length() time.Duration {
	switch b.Reset {
	case Daily:
		return 24 * time.Hour
	case Weekly:
		return 7 * 24 * time.Hour
	case Monthly:
		return 0
	}
	return 0
}

// periodAt returns the board's period that holds t.
func (b Board) periodAt(t time.Time) Period {
	if b.Reset == AllTime {
		return Period{ID: AllTimePeriod}
	}
	t = t.UTC()
	if b.Reset == Monthly {
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return Period{ID: start.Format("2006-01"), Start: start, End: start.AddDate(0, 1, 0)}
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if b.Reset == Weekly {
		back := (int(start.Weekday()) - int(b.weekStart()) + 7) % 7
		start = start.AddDate(0, 0, -back)
	}
	return b.periodStarting(start)
}

// periodStarting returns the resetting board's period that starts at start.
func (b Board) periodStarting(start time.Time) Period {
	return Period{ID: start.Format(periodLayout), Start: start, End: start.Add(b.length())}
}

// previous returns the period k periods before p on a resetting board.
func (b Board) previous(p Period, k int) Period {
	if b.Reset == Monthly {
		return b.periodAt(p.Start.AddDate(0, -k, 0))
	}
	return b.periodStarting(p.Start.Add(-time.Duration(k) * b.length()))
}

// readable returns the period of the board that query names, given the current period: "" or "current", "previous", or a period ID still kept. It reports false when the board has no such period, or no longer keeps it.
func (b Board) readable(query string, cur Period) (Period, bool) {
	switch query {
	case "", "current", cur.ID:
		return cur, true
	case "previous":
		if b.Reset == AllTime {
			return Period{}, false
		}
		return b.previous(cur, 1), true
	}
	if b.Reset == AllTime {
		return Period{}, false
	}
	p, ok := b.aligned(query)
	if !ok || !p.Start.Before(cur.Start) || p.Start.Before(b.previous(cur, b.keep()).Start) {
		return Period{}, false
	}
	return p, true
}

// aligned returns the board's period with ID id, and reports whether id names one: AllTimePeriod on an all-time board, otherwise a date a period starts on under the board's reset and weekStart. A period stored under another reset or weekStart does not align.
func (b Board) aligned(id string) (Period, bool) {
	if b.Reset == AllTime {
		return Period{ID: AllTimePeriod}, id == AllTimePeriod
	}
	layout := periodLayout
	if b.Reset == Monthly {
		layout = "2006-01"
	}
	start, err := time.Parse(layout, id)
	if err != nil {
		return Period{}, false
	}
	p := b.periodAt(start)
	return p, p.ID == id
}

// expired reports whether id names a period of the board, aligned to its reset and weekStart, that starts before the earliest period the board keeps given the current period cur. Only such a period may be dropped from the store: one that does not align, or one later than cur because the clock was behind, is left alone.
func (b Board) expired(id string, cur Period) bool {
	if b.Reset == AllTime {
		return false
	}
	p, ok := b.aligned(id)
	return ok && p.Start.Before(b.previous(cur, b.keep()).Start)
}
