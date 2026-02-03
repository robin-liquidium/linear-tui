package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	CustomViewsFileVersion = 1
)

// CustomViewStateMode defines how status filtering behaves for a view.
type CustomViewStateMode string

const (
	CustomViewStateAny     CustomViewStateMode = ""
	CustomViewStateNotDone CustomViewStateMode = "not_done"
)

// CustomViewSortField stores the sort field names for persistence.
type CustomViewSortField string

const (
	CustomViewSortUpdatedAt CustomViewSortField = "updatedAt"
	CustomViewSortCreatedAt CustomViewSortField = "createdAt"
	CustomViewSortPriority  CustomViewSortField = "priority"
	CustomViewSortStatus    CustomViewSortField = "status"
	CustomViewSortNone      CustomViewSortField = ""
)

// CustomView defines a saved navigation view and its filters.
type CustomView struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	TeamID        string              `json:"team_id,omitempty"`
	ProjectID     string              `json:"project_id,omitempty"`
	StateID       string              `json:"state_id,omitempty"`
	StateMode     CustomViewStateMode `json:"state_mode,omitempty"`
	AssigneeID    string              `json:"assignee_id,omitempty"`
	LabelID       string              `json:"label_id,omitempty"`
	DueWithinDays int                 `json:"due_within_days,omitempty"`
	SortPrimary   CustomViewSortField `json:"sort_primary,omitempty"`
	SortSecondary CustomViewSortField `json:"sort_secondary,omitempty"`
}

// CustomViewsFile wraps custom view storage on disk.
type CustomViewsFile struct {
	Version int          `json:"version"`
	Views   []CustomView `json:"views"`
}

// CustomViewsFilePath returns the default custom views file path.
func CustomViewsFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".linear-tui", "views.json"), nil
}

// EnsureCustomViewsFile ensures the custom views file exists and returns its views.
func EnsureCustomViewsFile(path string) ([]CustomView, error) {
	if path == "" {
		return nil, fmt.Errorf("custom views path is empty")
	}

	if _, err := os.Stat(path); err == nil {
		return LoadCustomViews(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat custom views file: %w", err)
	}

	if err := SaveCustomViews(path, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

// LoadCustomViews reads custom views from disk.
func LoadCustomViews(path string) ([]CustomView, error) {
	if path == "" {
		return nil, fmt.Errorf("custom views path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read custom views file: %w", err)
	}

	var file CustomViewsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse custom views file: %w", err)
	}

	return file.Views, nil
}

// SaveCustomViews writes custom views to disk.
func SaveCustomViews(path string, views []CustomView) error {
	if path == "" {
		return fmt.Errorf("custom views path is empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create custom views directory: %w", err)
	}

	file := CustomViewsFile{
		Version: CustomViewsFileVersion,
		Views:   views,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal custom views: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write custom views file: %w", err)
	}

	return nil
}
