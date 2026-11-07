package cmd

import (
	"fmt"
	"strings"
	"time"
)

// dateLayouts are the accepted user-facing date and time layouts, most
// specific first. RFC3339 carries its own offset; date-only values are UTC
// (matching GitHub's day-granular search qualifiers), and naive datetimes are
// interpreted in the local timezone.
var dateLayouts = []struct {
	layout   string
	location *time.Location
}{
	{time.RFC3339, nil},
	{"2006-01-02", time.UTC},
	{"2006-01-02 15:04:05", time.Local},
	{"2006-01-02 15:04", time.Local},
	{"2006-01-02T15:04:05", time.Local},
	{"2006-01-02T15:04", time.Local},
}

// parseDateOption parses a date or datetime supplied through a flag or query
// parameter. An empty value returns the zero time (unbounded). Date-only
// values are midnight UTC, or the last nanosecond of the day when endOfDay is
// true so an inclusive upper bound covers the whole day.
func parseDateOption(value, name string, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	for _, l := range dateLayouts {
		var (
			t   time.Time
			err error
		)
		if l.location == nil {
			t, err = time.Parse(l.layout, value)
		} else {
			t, err = time.ParseInLocation(l.layout, value, l.location)
		}
		if err != nil {
			continue
		}

		if endOfDay && l.layout == "2006-01-02" {
			return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location()), nil
		}
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid %s value %q: expected YYYY-MM-DD, YYYY-MM-DD 15:04[:05], or RFC3339", name, value)
}
