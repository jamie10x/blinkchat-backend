package models

import (
	"fmt"
	"strings"
	"time"
)

const RFC3339Milli = "2006-01-02T15:04:05.000Z"

// JSONTime wraps time.Time to provide consistent JSON marshaling.
type JSONTime time.Time

func (jt JSONTime) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", time.Time(jt).UTC().Format(time.RFC3339Nano))), nil
}

func (jt *JSONTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" {
		*jt = JSONTime(time.Time{})
		return nil
	}

	formats := []string{
		time.RFC3339Nano,
		RFC3339Milli,
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}

	var t time.Time
	var err error

	for _, format := range formats {
		t, err = time.Parse(format, s)
		if err == nil {
			*jt = JSONTime(t)
			return nil
		}
	}
	return fmt.Errorf("JSONTime.UnmarshalJSON: failed to parse time string '%s' with known formats: %w", s, err)
}

func (jt JSONTime) Time() time.Time {
	return time.Time(jt)
}

func (jt JSONTime) IsZero() bool {
	return time.Time(jt).IsZero()
}
