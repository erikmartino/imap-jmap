package jmap

import (
	"strings"
	"time"
)

func loadLocation(tz string) *time.Location {
	if tz == "" || tz == "UTC" || tz == "Etc/UTC" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func parseLocalDateTimeBound(s string, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	// Try parsing local time in location first when no Z/offset is present
	if !strings.HasSuffix(s, "Z") && !strings.Contains(s, "+") && (!strings.Contains(s, "-") || strings.Count(s, "-") <= 2) {
		for _, layout := range []string{
			"2006-01-02T15:04:05.999999999",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04",
			"2006-01-02",
		} {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t, true
			}
		}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseISODuration(raw string) (time.Duration, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s[0] != 'P' {
		return 0, false
	}
	s = s[1:]
	var total time.Duration
	var value uint64
	inTime := false

	flush := func(unit time.Duration) bool {
		total += time.Duration(value) * unit
		value = 0
		return true
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			value = value*10 + uint64(c-'0')
		case c == 'T':
			inTime = true
		case c == 'W' && !inTime:
			flush(7 * 24 * time.Hour)
		case c == 'D' && !inTime:
			flush(24 * time.Hour)
		case c == 'H' && inTime:
			flush(time.Hour)
		case c == 'M' && inTime:
			flush(time.Minute)
		case c == 'S' && inTime:
			flush(time.Second)
		}
	}
	return total, true
}

func computeUTCStart(start, timeZone string) string {
	if start == "" {
		return ""
	}
	loc := loadLocation(timeZone)
	if t, ok := parseLocalDateTimeBound(start, loc); ok {
		return t.UTC().Format("2006-01-02T15:04:05Z")
	}
	return ""
}

func computeUTCEnd(start, duration, timeZone string) string {
	if start == "" {
		return ""
	}
	loc := loadLocation(timeZone)
	if t, ok := parseLocalDateTimeBound(start, loc); ok {
		dur := 1 * time.Hour
		if duration != "" {
			if d, ok := parseISODuration(duration); ok {
				dur = d
			}
		}
		return t.Add(dur).UTC().Format("2006-01-02T15:04:05Z")
	}
	return ""
}
