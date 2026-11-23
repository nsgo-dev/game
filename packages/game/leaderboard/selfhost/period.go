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
	}
	return 0
}

// periodAt returns the board's period that holds t.
func (b Board) periodAt(t time.Time) Period {
	n := b.length()
	if n == 0 {
		return Period{ID: AllTimePeriod}
	}
	t = t.UTC()
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
	start, err := time.Parse(periodLayout, query)
	if err != nil {
		return Period{}, false
	}
	p := b.periodAt(start)
	if p.ID != query || !p.Start.Before(cur.Start) || p.Start.Before(b.previous(cur, b.keep()).Start) {
		return Period{}, false
	}
	return p, true
}

// kept reports whether the period with ID id is the current period cur or one of the earlier periods the board keeps.
func (b Board) kept(id string, cur Period) bool {
	if b.Reset == AllTime {
		return id == AllTimePeriod
	}
	_, ok := b.readable(id, cur)
	return ok
}
