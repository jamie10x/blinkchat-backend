package models

import (
	"fmt"
	"strings"
	"time"
)

// RFC3339Milli is a common variant of RFC3339 for APIs, ensuring milliseconds.
// Example: "2006-01-02T15:04:05.000Z"
const RFC3339Milli = "2006-01-02T15:04:05.000Z"

// JSONTime is a wrapper around time.Time for custom JSON marshalling/unmarshalling
// to ensure a consistent timestamp format (e.g., RFC3339 with milliseconds).
type JSONTime time.Time

// MarshalJSON implements the json.Marshaler interface.
// It formats the time as UTC in RFC3339Nano format.
func (jt JSONTime) MarshalJSON() ([]byte, error) {
	// Using RFC3339Nano for full precision, clients can truncate if needed.
	// Alternatively, use RFC3339Milli for consistent millisecond precision:
	// return []byte(fmt.Sprintf("\"%s\"", time.Time(jt).UTC().Format(RFC3339Milli))), nil
	return []byte(fmt.Sprintf("\"%s\"", time.Time(jt).UTC().Format(time.RFC3339Nano))), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// It attempts to parse the time string from JSON using several common formats.
func (jt *JSONTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" { // Handle empty string as well as null
		*jt = JSONTime(time.Time{}) // Set to zero time
		return nil
	}

	// List of formats to try. RFC3339Nano is often preferred.
	formats := []string{
		time.RFC3339Nano,
		RFC3339Milli,           // "2006-01-02T15:04:05.000Z"
		"2006-01-02T15:04:05Z", // RFC3339 without fractional seconds
		time.RFC3339,           // Go's standard RFC3339, which includes timezone offset
	}

	var t time.Time
	var err error

	for _, format := range formats {
		t, err = time.Parse(format, s)
		if err == nil { // Successfully parsed
			*jt = JSONTime(t)
			return nil
		}
	}
	// If none of the formats worked
	return fmt.Errorf("JSONTime.UnmarshalJSON: failed to parse time string '%s' with known formats: %w", s, err)
}

// Time returns the underlying time.Time object.
func (jt JSONTime) Time() time.Time {
	return time.Time(jt)
}

// IsZero checks if the JSONTime is its zero value.
func (jt JSONTime) IsZero() bool {
	return time.Time(jt).IsZero()
}
