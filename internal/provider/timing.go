package provider

import "time"

// ElapsedMS reports the whole milliseconds between start and end, clamped at
// zero.  Runners take both timestamps from an injectable clock, so a test clock
// (or a wall clock stepping backwards) must not be able to produce a negative
// duration in a stored measurement.
func ElapsedMS(start, end time.Time) int64 {
	duration := end.Sub(start).Milliseconds()
	if duration < 0 {
		return 0
	}
	return duration
}
