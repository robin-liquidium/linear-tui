package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Event is the compact Google Calendar event model used by the embedded pane.
type Event struct {
	ID          string    `json:"id"`
	CalendarID  string    `json:"calendar_id"`
	Calendar    string    `json:"calendar"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	AllDay      bool      `json:"all_day"`
	Status      string    `json:"status"`
	HTMLLink    string    `json:"html_link"`
	Organizer   string    `json:"organizer"`
	Attendees   []string  `json:"attendees"`
	Color       string    `json:"color"`
}

// Service lists and mutates calendar events.
type Service interface {
	ListWeek(ctx context.Context, weekStart time.Time) ([]Event, error)
	DeleteEvent(ctx context.Context, calendarID string, eventID string) error
}

// GWSClient talks to Google Calendar through the user's existing gws OAuth setup.
type GWSClient struct {
	bin string
}

// NewGWSClient returns a gws-backed calendar service after checking Calendar scope.
func NewGWSClient(ctx context.Context) (*GWSClient, error) {
	bin, err := exec.LookPath("gws")
	if err != nil {
		return nil, err
	}
	client := &GWSClient{bin: bin}
	if err := client.check(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// ListWeek returns all visible-calendar events for the week containing weekStart.
func (c *GWSClient) ListWeek(ctx context.Context, weekStart time.Time) ([]Event, error) {
	calendars, err := c.visibleCalendars(ctx)
	if err != nil {
		return nil, err
	}
	start := StartOfWeek(weekStart)
	end := start.AddDate(0, 0, 7)
	var all []Event
	var skipped []string
	for _, cal := range calendars {
		params, err := json.Marshal(map[string]any{
			"calendarId":   cal.ID,
			"showDeleted":  false,
			"singleEvents": true,
			"orderBy":      "startTime",
			"timeMin":      start.Format(time.RFC3339),
			"timeMax":      end.Format(time.RFC3339),
			"maxResults":   2500,
		})
		if err != nil {
			return nil, err
		}
		var events gwsEventsList
		if err := c.runJSON(ctx, &events, "calendar", "events", "list", "--params", string(params), "--format", "json"); err != nil {
			skipped = append(skipped, cal.Summary)
			continue
		}
		for _, item := range events.Items {
			if item.Status == "cancelled" {
				continue
			}
			all = append(all, convertGWSEvent(cal, item))
		}
	}
	if len(skipped) == len(calendars) && len(skipped) > 0 {
		return nil, fmt.Errorf("could not load events for selected calendars: %s", strings.Join(skipped, ", "))
	}
	SortEvents(all)
	return all, nil
}

// DeleteEvent removes an event without sending attendee updates.
func (c *GWSClient) DeleteEvent(ctx context.Context, calendarID string, eventID string) error {
	params, err := json.Marshal(map[string]any{
		"calendarId":  calendarID,
		"eventId":     eventID,
		"sendUpdates": "none",
	})
	if err != nil {
		return err
	}
	return c.runJSON(ctx, &map[string]any{}, "calendar", "events", "delete", "--params", string(params), "--format", "json")
}

// SortEvents keeps all-day events before timed events, then sorts by start time.
func SortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Start.Equal(b.Start) {
			return a.End.Before(b.End)
		}
		if a.AllDay != b.AllDay {
			return a.AllDay
		}
		return a.Start.Before(b.Start)
	})
}

// StartOfWeek returns Monday midnight for the week containing t.
func StartOfWeek(t time.Time) time.Time {
	local := t.In(time.Local)
	y, m, d := local.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, local.Location())
	offset := (int(midnight.Weekday()) + 6) % 7
	return midnight.AddDate(0, 0, -offset)
}

// SameDay reports whether two times fall on the same local date.
func SameDay(a time.Time, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}

// DayIndex returns the zero-based day offset from weekStart to t.
func DayIndex(weekStart time.Time, t time.Time) int {
	start := StartOfWeek(weekStart)
	day := startOfDay(t)
	for i := 0; i < 7; i++ {
		if SameDay(start.AddDate(0, 0, i), day) {
			return i
		}
	}
	if day.Before(start) {
		return -1
	}
	return 7
}

// OccursOnDay reports whether an event overlaps a local day.
func (e Event) OccursOnDay(day time.Time) bool {
	dayStart := startOfDay(day)
	dayEnd := dayStart.AddDate(0, 0, 1)
	start := e.Start.In(time.Local)
	end := e.End.In(time.Local)
	if end.IsZero() || !end.After(start) {
		end = start.Add(time.Minute)
	}
	if e.AllDay {
		end = end.Add(-time.Nanosecond)
	}
	return start.Before(dayEnd) && end.After(dayStart)
}

// DisplayEnd returns the inclusive display end for all-day events.
func (e Event) DisplayEnd() time.Time {
	if e.AllDay && e.End.After(e.Start) {
		return e.End.AddDate(0, 0, -1)
	}
	return e.End
}

func (c *GWSClient) check(ctx context.Context) error {
	var status struct {
		TokenValid bool     `json:"token_valid"`
		Scopes     []string `json:"scopes"`
	}
	if err := c.runJSON(ctx, &status, "auth", "status"); err != nil {
		return err
	}
	if !status.TokenValid {
		return fmt.Errorf("gws token is not valid")
	}
	for _, scope := range status.Scopes {
		if scope == "https://www.googleapis.com/auth/calendar" || scope == "https://www.googleapis.com/auth/calendar.events" {
			return nil
		}
	}
	return fmt.Errorf("gws auth is missing Calendar scope")
}

func (c *GWSClient) visibleCalendars(ctx context.Context) ([]calendarMeta, error) {
	params, err := json.Marshal(map[string]any{
		"showHidden":    false,
		"minAccessRole": "reader",
		"maxResults":    250,
	})
	if err != nil {
		return nil, err
	}
	var list gwsCalendarList
	if err := c.runJSON(ctx, &list, "calendar", "calendarList", "list", "--params", string(params), "--format", "json"); err != nil {
		return nil, err
	}
	var result []calendarMeta
	for _, item := range list.Items {
		if item.Deleted || item.Hidden {
			continue
		}
		if !item.Selected && item.ID != "primary" && !item.Primary {
			continue
		}
		result = append(result, calendarMeta{ID: item.ID, Summary: item.Summary, Color: item.BackgroundColor})
	}
	if len(result) == 0 {
		result = append(result, calendarMeta{ID: "primary", Summary: "Primary"})
	}
	return result, nil
}

func (c *GWSClient) runJSON(ctx context.Context, target any, args ...string) error {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	if len(out) == 0 {
		return nil
	}
	if err := json.Unmarshal(out, target); err != nil {
		return fmt.Errorf("decode gws output: %w", err)
	}
	return nil
}

type calendarMeta struct {
	ID      string
	Summary string
	Color   string
}

type gwsCalendarList struct {
	Items []gwsCalendarListEntry `json:"items"`
}

type gwsCalendarListEntry struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	Selected        bool   `json:"selected"`
	Primary         bool   `json:"primary"`
	Deleted         bool   `json:"deleted"`
	Hidden          bool   `json:"hidden"`
	BackgroundColor string `json:"backgroundColor"`
}

type gwsEventsList struct {
	Items []gwsEvent `json:"items"`
}

type gwsEvent struct {
	ID          string           `json:"id"`
	Summary     string           `json:"summary"`
	Description string           `json:"description"`
	Location    string           `json:"location"`
	Start       gwsEventDateTime `json:"start"`
	End         gwsEventDateTime `json:"end"`
	Status      string           `json:"status"`
	HTMLLink    string           `json:"htmlLink"`
	Organizer   gwsEventPerson   `json:"organizer"`
	Attendees   []gwsEventPerson `json:"attendees"`
	ColorID     string           `json:"colorId"`
}

type gwsEventDateTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

type gwsEventPerson struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

func convertGWSEvent(cal calendarMeta, item gwsEvent) Event {
	start, allDay := gwsEventTime(item.Start)
	end, _ := gwsEventTime(item.End)
	organizer := firstNonEmpty(item.Organizer.DisplayName, item.Organizer.Email)
	var attendees []string
	for _, attendee := range item.Attendees {
		name := firstNonEmpty(attendee.DisplayName, attendee.Email)
		if name != "" {
			attendees = append(attendees, name)
		}
	}
	return Event{
		ID:          item.ID,
		CalendarID:  cal.ID,
		Calendar:    cal.Summary,
		Summary:     firstNonEmpty(item.Summary, "(no title)"),
		Description: item.Description,
		Location:    item.Location,
		Start:       start,
		End:         end,
		AllDay:      allDay,
		Status:      item.Status,
		HTMLLink:    item.HTMLLink,
		Organizer:   organizer,
		Attendees:   attendees,
		Color:       firstNonEmpty(item.ColorID, cal.Color),
	}
}

func gwsEventTime(dt gwsEventDateTime) (time.Time, bool) {
	if dt.Date != "" {
		t, err := time.ParseInLocation("2006-01-02", dt.Date, time.Local)
		if err == nil {
			return t, true
		}
	}
	if dt.DateTime != "" {
		t, err := time.Parse(time.RFC3339, dt.DateTime)
		if err == nil {
			return t.In(time.Local), false
		}
	}
	return time.Time{}, false
}

func startOfDay(t time.Time) time.Time {
	local := t.In(time.Local)
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, local.Location())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
