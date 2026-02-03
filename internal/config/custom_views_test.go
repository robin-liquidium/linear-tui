package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCustomViewsSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "views.json")

	views := []CustomView{
		{
			ID:            "view-1",
			Name:          "My Open",
			TeamID:        "team-1",
			ProjectID:     "proj-1",
			StateMode:     CustomViewStateNotDone,
			AssigneeID:    "user-1",
			LabelID:       "label-1",
			DueWithinDays: 7,
			SortPrimary:   CustomViewSortStatus,
			SortSecondary: CustomViewSortPriority,
		},
	}

	if err := SaveCustomViews(path, views); err != nil {
		t.Fatalf("SaveCustomViews() error: %v", err)
	}

	loaded, err := LoadCustomViews(path)
	if err != nil {
		t.Fatalf("LoadCustomViews() error: %v", err)
	}

	if !reflect.DeepEqual(views, loaded) {
		t.Fatalf("Loaded views mismatch. got=%#v want=%#v", loaded, views)
	}
}

func TestEnsureCustomViewsFileCreates(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "views.json")

	views, err := EnsureCustomViewsFile(path)
	if err != nil {
		t.Fatalf("EnsureCustomViewsFile() error: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("Expected empty views, got %d", len(views))
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Expected views file to be created: %v", err)
	}
}
