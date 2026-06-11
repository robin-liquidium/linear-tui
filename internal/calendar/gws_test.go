package calendar

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGWSClientListWeekAndDeleteEvent(t *testing.T) {
	binDir := t.TempDir()
	deleted := filepath.Join(binDir, "deleted")
	fakeGWS := filepath.Join(binDir, "gws")
	script := `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  printf '%s\n' '{"token_valid":true,"scopes":["https://www.googleapis.com/auth/calendar"]}'
  exit 0
fi
if [ "$1 $2 $3" = "calendar calendarList list" ]; then
  printf '%s\n' '{"items":[{"id":"primary","summary":"Home","selected":true,"primary":true}]}'
  exit 0
fi
if [ "$1 $2 $3" = "calendar events list" ]; then
  printf '%s\n' '{"items":[{"id":"event-1","summary":"Demo","start":{"dateTime":"2026-06-11T09:00:00Z"},"end":{"dateTime":"2026-06-11T10:00:00Z"},"status":"confirmed","organizer":{"email":"robin@liquidium.fi"}}]}'
  exit 0
fi
if [ "$1 $2 $3" = "calendar events delete" ]; then
  case "$5" in *'"calendarId":"primary"'*) case "$5" in *'"eventId":"event-1"'*) printf deleted > "` + deleted + `"; exit 0 ;; esac ;; esac
fi
exit 1
`
	if err := os.WriteFile(fakeGWS, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	client, err := NewGWSClient(context.Background())
	if err != nil {
		t.Fatalf("NewGWSClient() error = %v", err)
	}
	events, err := client.ListWeek(context.Background(), time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ListWeek() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-1" || events[0].CalendarID != "primary" {
		t.Fatalf("events = %+v", events)
	}
	if err := client.DeleteEvent(context.Background(), "primary", "event-1"); err != nil {
		t.Fatalf("DeleteEvent() error = %v", err)
	}
	if _, err := os.Stat(deleted); err != nil {
		t.Fatalf("delete marker missing: %v", err)
	}
}
