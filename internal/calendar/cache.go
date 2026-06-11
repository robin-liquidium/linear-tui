package calendar

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Cache stores fetched calendar weeks on disk so the embedded pane can render before gws finishes.
type Cache struct {
	path string
}

type cacheFile struct {
	Weeks map[string]cacheWeek `json:"weeks"`
}

type cacheWeek struct {
	FetchedAt time.Time `json:"fetched_at"`
	Events    []Event   `json:"events"`
}

// NewCache creates a calendar cache at path.
func NewCache(path string) *Cache {
	return &Cache{path: path}
}

// NewDefaultCache creates the shared gc-compatible calendar cache.
func NewDefaultCache() (*Cache, error) {
	path, err := DefaultCachePath()
	if err != nil {
		return nil, err
	}
	return NewCache(path), nil
}

// DefaultCachePath returns the durable event cache path shared with the gc TUI.
func DefaultCachePath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "gc", "events.json"), nil
}

// LoadWeek returns cached events for the week containing start.
func (c *Cache) LoadWeek(start time.Time) ([]Event, time.Time, bool) {
	if c == nil || c.path == "" {
		return nil, time.Time{}, false
	}
	data, err := c.read()
	if err != nil {
		return nil, time.Time{}, false
	}
	week, ok := data.Weeks[weekKey(start)]
	if !ok {
		return nil, time.Time{}, false
	}
	events := append([]Event(nil), week.Events...)
	SortEvents(events)
	return events, week.FetchedAt, true
}

// SaveWeek persists events for the week containing start.
func (c *Cache) SaveWeek(start time.Time, events []Event) error {
	if c == nil || c.path == "" {
		return nil
	}
	data, err := c.read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if data.Weeks == nil {
		data.Weeks = make(map[string]cacheWeek)
	}
	copied := append([]Event(nil), events...)
	SortEvents(copied)
	data.Weeks[weekKey(start)] = cacheWeek{FetchedAt: time.Now(), Events: copied}
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, raw, 0600)
}

func (c *Cache) read() (cacheFile, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return cacheFile{}, err
	}
	var data cacheFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return cacheFile{}, err
	}
	if data.Weeks == nil {
		data.Weeks = make(map[string]cacheWeek)
	}
	return data, nil
}

func weekKey(start time.Time) string {
	return StartOfWeek(start).Format("2006-01-02")
}
