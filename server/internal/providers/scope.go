package providers

import (
	"time"

	"github.com/google/uuid"
)

var (
	taskWindowStart = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	taskWindowEnd   = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	preparedTTL = 10 * time.Minute
	sentGrace   = 2 * time.Minute
)

// DayScope is the UTC calendar window provider:<key>:day:YYYY-MM-DD.
func DayScope(providerKey string, now time.Time) (scope string, start, end time.Time) {
	u := now.UTC()
	start = time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	end = start.Add(24 * time.Hour)
	return "provider:" + providerKey + ":day:" + start.Format("2006-01-02"), start, end
}

// TaskScope is one window for the whole processing run.
func TaskScope(runID uuid.UUID) (scope string, start, end time.Time) {
	return "task:" + runID.String(), taskWindowStart, taskWindowEnd
}
