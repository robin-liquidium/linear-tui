package calendar

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCacheLoadSaveWeek(t *testing.T) {
	cache := NewCache(filepath.Join(t.TempDir(), "events.json"))
	week := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	events := []Event{{
		ID:         "event-1",
		CalendarID: "primary",
		Calendar:   "Home",
		Summary:    "Standup",
		Start:      week,
		End:        week.Add(30 * time.Minute),
		Attendees:  []string{"robin@example.com"},
	}}

	if err := cache.SaveWeek(week, events); err != nil {
		t.Fatalf("SaveWeek() error = %v", err)
	}
	got, fetchedAt, ok := cache.LoadWeek(week)
	if !ok {
		t.Fatal("LoadWeek() ok = false, want true")
	}
	if fetchedAt.IsZero() {
		t.Fatal("LoadWeek() fetchedAt is zero")
	}
	if !reflect.DeepEqual(got, events) {
		t.Fatalf("LoadWeek() events = %+v, want %+v", got, events)
	}
}
