// Package antiaddiction contains server-side age-window and spending rules for
// mainland China publishing compliance. It has no clock or platform dependency.
package antiaddiction

import "time"

type AgeBand int

const (
	Unknown AgeBand = iota
	Under8
	Age8To15
	Age16To17
	Adult
)

func Band(age int) AgeBand {
	switch {
	case age < 0:
		return Unknown
	case age < 8:
		return Under8
	case age < 16:
		return Age8To15
	case age < 18:
		return Age16To17
	default:
		return Adult
	}
}

// Window reports whether a player may play at now in Asia/Shanghai. The caller
// supplies statutory holiday dates that are allowed even when they fall on a
// weekday. The returned until is the next boundary at which the answer changes.
func Window(age int, now time.Time, holidays map[string]bool) (bool, time.Time) {
	if age >= 18 {
		return true, time.Time{}
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	n := now.In(loc)
	day := n.Weekday()
	date := n.Format("2006-01-02")
	allowed := holidays[date] || day == time.Friday || day == time.Saturday || day == time.Sunday
	start := time.Date(n.Year(), n.Month(), n.Day(), 20, 0, 0, 0, loc)
	end := start.Add(time.Hour)
	if !allowed {
		return false, nextDay(start)
	}
	if n.Before(start) || !n.Before(end) {
		return false, nextDay(start)
	}
	return true, end
}
func nextDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
}

// SpendLimit returns the per-payment and per-calendar-month caps in yuan.
func SpendLimit(age int) (perPayment, perMonth int) {
	switch Band(age) {
	case Under8:
		return 0, 0
	case Age8To15:
		return 50, 200
	case Age16To17:
		return 100, 400
	default:
		return 0, 0
	}
}
func SpendAllowed(age, amount, monthTotal int) bool {
	p, m := SpendLimit(age)
	if p == 0 && m == 0 {
		return age >= 18
	}
	return amount >= 0 && amount <= p && monthTotal+amount <= m
}
