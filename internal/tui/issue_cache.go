package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// issueResultCache stores complete issue-list results by normalized query.
type issueResultCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]issueResultCacheEntry
}

// issueResultCacheEntry is one cached issue-list result with its expiry.
type issueResultCacheEntry struct {
	issues []linearapi.Issue
	expiry time.Time
}

// newIssueResultCache creates an in-memory issue-list cache.
func newIssueResultCache(ttl time.Duration) *issueResultCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &issueResultCache{
		ttl:     ttl,
		entries: make(map[string]issueResultCacheEntry),
	}
}

// Get returns a copy of a fresh cached issue-list result.
func (c *issueResultCache) Get(key string) ([]linearapi.Issue, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiry) {
		c.mu.RUnlock()
		if ok {
			c.Delete(key)
		}
		return nil, false
	}
	issues := append([]linearapi.Issue(nil), entry.issues...)
	c.mu.RUnlock()
	return issues, true
}

// Set stores a complete issue-list result for a normalized query key.
func (c *issueResultCache) Set(key string, issues []linearapi.Issue) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	c.entries[key] = issueResultCacheEntry{
		issues: append([]linearapi.Issue(nil), issues...),
		expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Delete removes one cached issue-list result.
func (c *issueResultCache) Delete(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes every cached issue-list result.
func (c *issueResultCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]issueResultCacheEntry)
	c.mu.Unlock()
}

// issueDetailCache stores full issue records by issue ID.
type issueDetailCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]issueDetailCacheEntry
}

// issueDetailCacheEntry is one cached full issue with its expiry.
type issueDetailCacheEntry struct {
	issue  linearapi.Issue
	expiry time.Time
}

// newIssueDetailCache creates an in-memory full-issue cache.
func newIssueDetailCache(ttl time.Duration) *issueDetailCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &issueDetailCache{
		ttl:     ttl,
		entries: make(map[string]issueDetailCacheEntry),
	}
}

// Get returns a fresh cached full issue.
func (c *issueDetailCache) Get(issueID string) (linearapi.Issue, bool) {
	if c == nil || issueID == "" {
		return linearapi.Issue{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[issueID]
	if !ok || time.Now().After(entry.expiry) {
		c.mu.RUnlock()
		if ok {
			c.Delete(issueID)
		}
		return linearapi.Issue{}, false
	}
	c.mu.RUnlock()
	return entry.issue, true
}

// Set stores a full issue by ID.
func (c *issueDetailCache) Set(issue linearapi.Issue) {
	if c == nil || issue.ID == "" {
		return
	}
	c.mu.Lock()
	c.entries[issue.ID] = issueDetailCacheEntry{
		issue:  issue,
		expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Delete removes one cached full issue.
func (c *issueDetailCache) Delete(issueID string) {
	if c == nil || issueID == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, issueID)
	c.mu.Unlock()
}

// Clear removes every cached full issue.
func (c *issueDetailCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]issueDetailCacheEntry)
	c.mu.Unlock()
}

type issueQueryCacheKey struct {
	TeamID             string                 `json:"team_id,omitempty"`
	TeamIDs            []string               `json:"team_ids,omitempty"`
	ProjectID          string                 `json:"project_id,omitempty"`
	ProjectIDs         []string               `json:"project_ids,omitempty"`
	StateID            string                 `json:"state_id,omitempty"`
	StateIDs           []string               `json:"state_ids,omitempty"`
	CycleID            string                 `json:"cycle_id,omitempty"`
	CycleIDs           []string               `json:"cycle_ids,omitempty"`
	AssigneeID         string                 `json:"assignee_id,omitempty"`
	AssigneeIDs        []string               `json:"assignee_ids,omitempty"`
	LabelID            string                 `json:"label_id,omitempty"`
	LabelIDs           []string               `json:"label_ids,omitempty"`
	ProjectMilestoneID string                 `json:"project_milestone_id,omitempty"`
	StateTypes         []string               `json:"state_types,omitempty"`
	DueWithinDays      int                    `json:"due_within_days,omitempty"`
	DueDate            linearapi.DateFilter   `json:"due_date,omitempty"`
	Estimate           linearapi.NumberFilter `json:"estimate,omitempty"`
	Search             string                 `json:"search,omitempty"`
	OrderBy            string                 `json:"order_by,omitempty"`
}

// issueCacheKeyFromParams returns a stable key for queries with the same result set.
func issueCacheKeyFromParams(params linearapi.FetchIssuesParams) string {
	key := issueQueryCacheKey{
		TeamID:             strings.TrimSpace(params.TeamID),
		TeamIDs:            sortedStrings(params.TeamIDs),
		ProjectID:          strings.TrimSpace(params.ProjectID),
		ProjectIDs:         sortedStrings(params.ProjectIDs),
		StateID:            strings.TrimSpace(params.StateID),
		StateIDs:           sortedStrings(params.StateIDs),
		CycleID:            strings.TrimSpace(params.CycleID),
		CycleIDs:           sortedStrings(params.CycleIDs),
		AssigneeID:         strings.TrimSpace(params.AssigneeID),
		AssigneeIDs:        sortedStrings(params.AssigneeIDs),
		LabelID:            strings.TrimSpace(params.LabelID),
		LabelIDs:           sortedStrings(params.LabelIDs),
		ProjectMilestoneID: strings.TrimSpace(params.ProjectMilestoneID),
		StateTypes:         sortedStrings(params.StateTypes),
		DueWithinDays:      params.DueWithinDays,
		DueDate:            params.DueDate,
		Estimate:           params.Estimate,
		Search:             strings.TrimSpace(params.Search),
		OrderBy:            strings.TrimSpace(params.OrderBy),
	}
	data, err := json.Marshal(key)
	if err != nil {
		return ""
	}
	return string(data)
}

// sortedStrings returns a trimmed, sorted copy of values for order-insensitive filters.
func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	sort.Strings(result)
	return result
}

// issueDiskCache stores issue-list results on disk so restart loads can be instant.
type issueDiskCache struct {
	path string
	mu   sync.Mutex
}

type issueDiskCacheFile struct {
	Version int                           `json:"version"`
	Entries map[string]issueDiskCacheItem `json:"entries"`
}

type issueDiskCacheItem struct {
	Issues    []linearapi.Issue `json:"issues"`
	FetchedAt time.Time         `json:"fetched_at"`
}

// newIssueDiskCache creates a durable cache at path, or disables it for a blank path.
func newIssueDiskCache(path string) *issueDiskCache {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return &issueDiskCache{path: path}
}

// Get returns a cached issue-list result from disk regardless of age.
func (c *issueDiskCache) Get(key string) ([]linearapi.Issue, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	file, err := c.readLocked()
	if err != nil {
		return nil, false
	}
	entry, ok := file.Entries[key]
	if !ok {
		return nil, false
	}
	return append([]linearapi.Issue(nil), entry.Issues...), true
}

// Set stores a complete issue-list result on disk.
func (c *issueDiskCache) Set(key string, issues []linearapi.Issue) error {
	if c == nil || key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	file, err := c.readLocked()
	if err != nil {
		file = issueDiskCacheFile{Version: 1, Entries: map[string]issueDiskCacheItem{}}
	}
	if file.Entries == nil {
		file.Entries = map[string]issueDiskCacheItem{}
	}
	file.Version = 1
	file.Entries[key] = issueDiskCacheItem{
		Issues:    append([]linearapi.Issue(nil), issues...),
		FetchedAt: time.Now(),
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return fmt.Errorf("create issue cache dir: %w", err)
	}
	data, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode issue cache: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0600); err != nil {
		return fmt.Errorf("write issue cache: %w", err)
	}
	return nil
}

// Clear removes every durable issue-list result.
func (c *issueDiskCache) Clear() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear issue cache: %w", err)
	}
	return nil
}

// readLocked reads the cache file while the caller holds c.mu.
func (c *issueDiskCache) readLocked() (issueDiskCacheFile, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return issueDiskCacheFile{}, err
	}
	var file issueDiskCacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return issueDiskCacheFile{}, err
	}
	if file.Entries == nil {
		file.Entries = map[string]issueDiskCacheItem{}
	}
	return file, nil
}
