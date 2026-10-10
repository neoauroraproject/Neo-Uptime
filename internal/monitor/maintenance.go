package monitor

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// Maintenance states, as the UI shows them.
const (
	MaintUnder     = "under-maintenance"
	MaintScheduled = "scheduled"
	MaintEnded     = "ended"
	MaintInactive  = "inactive"
)

func parseHM(s string) int {
	h, m, _ := strings.Cut(s, ":")
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	return hh*60 + mm
}

func loc(tz string) *time.Location {
	if tz == "" || strings.EqualFold(tz, "utc") {
		return time.UTC
	}
	if l, err := time.LoadLocation(tz); err == nil {
		return l
	}
	return time.UTC
}

// dayMatches reports whether a recurring window is scheduled to open on day d.
func dayMatches(m *store.Maintenance, d time.Time) bool {
	switch m.Strategy {
	case "weekly":
		return slices.Contains(m.Weekdays, int(d.Weekday()))
	case "monthly":
		dom := d.Day()
		if slices.Contains(m.DaysOfMonth, dom) {
			return true
		}
		// "Last day of month" is written as -1.
		last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, d.Location()).Day()
		return dom == last && slices.Contains(m.DaysOfMonth, -1)
	}
	return false
}

// windowAt returns the recurring window that opened on day d, in m's zone.
func windowAt(m *store.Maintenance, d time.Time) (time.Time, time.Time) {
	from, to := parseHM(m.TimeFrom), parseHM(m.TimeTo)
	start := time.Date(d.Year(), d.Month(), d.Day(), from/60, from%60, 0, 0, d.Location())
	if to <= from {
		to += 24 * 60 // the window runs past midnight
	}
	end := start.Add(time.Duration(to-from) * time.Minute)
	return start, end
}

func inBounds(m *store.Maintenance, t time.Time) bool {
	if m.StartAt > 0 && t.Unix() < m.StartAt {
		return false
	}
	if m.EndAt > 0 && t.Unix() > m.EndAt {
		return false
	}
	return true
}

// MaintenanceStatus works out where a maintenance stands at t, and when it
// next opens (zero when it never will).
func MaintenanceStatus(m *store.Maintenance, t time.Time) (string, time.Time, time.Time) {
	if !m.Active {
		return MaintInactive, time.Time{}, time.Time{}
	}
	switch m.Strategy {
	case "manual":
		return MaintUnder, time.Time{}, time.Time{}
	case "single":
		s, e := time.Unix(m.StartAt, 0), time.Unix(m.EndAt, 0)
		switch {
		case t.Before(s):
			return MaintScheduled, s, e
		case m.EndAt == 0 || t.Before(e):
			return MaintUnder, s, e
		default:
			return MaintEnded, s, e
		}
	case "weekly", "monthly":
		lt := t.In(loc(m.Timezone))
		// The window that opened yesterday may still be running.
		for back := 1; back >= 0; back-- {
			d := lt.AddDate(0, 0, -back)
			if !dayMatches(m, d) {
				continue
			}
			s, e := windowAt(m, d)
			if !lt.Before(s) && lt.Before(e) && inBounds(m, lt) {
				return MaintUnder, s, e
			}
		}
		for ahead := 0; ahead <= 62; ahead++ {
			d := lt.AddDate(0, 0, ahead)
			if !dayMatches(m, d) {
				continue
			}
			s, e := windowAt(m, d)
			if s.After(lt) {
				if m.EndAt > 0 && s.Unix() > m.EndAt {
					return MaintEnded, time.Time{}, time.Time{}
				}
				return MaintScheduled, s, e
			}
		}
		return MaintScheduled, time.Time{}, time.Time{}
	}
	return MaintInactive, time.Time{}, time.Time{}
}
