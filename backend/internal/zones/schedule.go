package zones

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// MaximumScheduleWindows bounds how many weekly windows one zone may carry.
const MaximumScheduleWindows = 14

var (
	scheduleDays = map[string]int{"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6}
	clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// Schedule describes when a zone rule is armed. A window whose end is earlier
// than its start crosses midnight, e.g. 22:00-06:00 for "after hours".
type Schedule struct {
	Timezone string           `json:"timezone"`
	Windows  []ScheduleWindow `json:"windows"`
}

type ScheduleWindow struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

// NormalizeSchedule validates a schedule and returns its canonical JSON. An
// empty value, JSON null or an empty object means "always active" (nil).
func NormalizeSchedule(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var schedule Schedule
	if err := decoder.Decode(&schedule); err != nil {
		return nil, errors.New("schedule must be an object with timezone and windows")
	}
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	if schedule.Timezone == "" || len(schedule.Timezone) > 64 {
		return nil, errors.New("schedule timezone is required")
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return nil, errors.New("schedule timezone must be an IANA timezone")
	}
	if len(schedule.Windows) == 0 || len(schedule.Windows) > MaximumScheduleWindows {
		return nil, errors.New("schedule needs between 1 and 14 windows")
	}
	for index := range schedule.Windows {
		window := &schedule.Windows[index]
		if !clockPattern.MatchString(window.Start) || !clockPattern.MatchString(window.End) || window.Start == window.End {
			return nil, errors.New("schedule window times must be distinct HH:MM values")
		}
		if len(window.Days) == 0 || len(window.Days) > 7 {
			return nil, errors.New("schedule window needs between 1 and 7 days")
		}
		seen := make(map[string]bool, len(window.Days))
		ordered := make([]string, 0, len(window.Days))
		for _, day := range window.Days {
			day = strings.ToLower(strings.TrimSpace(day))
			if _, ok := scheduleDays[day]; !ok || seen[day] {
				return nil, errors.New("schedule days must be unique mon..sun values")
			}
			seen[day] = true
		}
		for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
			if seen[day] {
				ordered = append(ordered, day)
			}
		}
		window.Days = ordered
	}
	canonical, err := json.Marshal(schedule)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

// Active reports whether a canonical schedule arms its zone at the instant.
// A nil schedule is always active.
func Active(raw json.RawMessage, at time.Time) bool {
	if len(raw) == 0 {
		return true
	}
	var schedule Schedule
	if json.Unmarshal(raw, &schedule) != nil {
		return true
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return true
	}
	local := at.In(location)
	minute := local.Hour()*60 + local.Minute()
	today := (int(local.Weekday()) + 6) % 7
	yesterday := (today + 6) % 7
	for _, window := range schedule.Windows {
		start, end := clockMinutes(window.Start), clockMinutes(window.End)
		for _, day := range window.Days {
			index := scheduleDays[day]
			if start < end && index == today && minute >= start && minute < end {
				return true
			}
			if start > end && ((index == today && minute >= start) || (index == yesterday && minute < end)) {
				return true
			}
		}
	}
	return false
}

func clockMinutes(value string) int {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0
	}
	return parsed.Hour()*60 + parsed.Minute()
}
