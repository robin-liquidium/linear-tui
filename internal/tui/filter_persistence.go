package tui

import (
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// issueFiltersFromSettings converts persisted settings into runtime issue filters.
func issueFiltersFromSettings(settings config.IssueFilterSettings) IssueFilters {
	return IssueFilters{
		TeamIDs:       copyStringSlice(settings.TeamIDs),
		TeamNames:     copyStringSlice(settings.TeamNames),
		AssigneeIDs:   copyStringSlice(settings.AssigneeIDs),
		AssigneeNames: copyStringSlice(settings.AssigneeNames),
		LabelIDs:      copyStringSlice(settings.LabelIDs),
		LabelNames:    copyStringSlice(settings.LabelNames),
		StateIDs:      copyStringSlice(settings.StateIDs),
		StateNames:    copyStringSlice(settings.StateNames),
		ProjectIDs:    copyStringSlice(settings.ProjectIDs),
		ProjectNames:  copyStringSlice(settings.ProjectNames),
		CycleIDs:      copyStringSlice(settings.CycleIDs),
		CycleNames:    copyStringSlice(settings.CycleNames),
		DueDate:       dateFilterFromSettings(settings.DueDate),
		Estimate:      numberFilterFromSettings(settings.Estimate),
	}
}

// issueFiltersToSettings converts runtime issue filters into persisted settings.
func issueFiltersToSettings(filters IssueFilters) config.IssueFilterSettings {
	return config.IssueFilterSettings{
		TeamIDs:       copyStringSlice(filters.TeamIDs),
		TeamNames:     copyStringSlice(filters.TeamNames),
		AssigneeIDs:   copyStringSlice(filters.AssigneeIDs),
		AssigneeNames: copyStringSlice(filters.AssigneeNames),
		LabelIDs:      copyStringSlice(filters.LabelIDs),
		LabelNames:    copyStringSlice(filters.LabelNames),
		StateIDs:      copyStringSlice(filters.StateIDs),
		StateNames:    copyStringSlice(filters.StateNames),
		ProjectIDs:    copyStringSlice(filters.ProjectIDs),
		ProjectNames:  copyStringSlice(filters.ProjectNames),
		CycleIDs:      copyStringSlice(filters.CycleIDs),
		CycleNames:    copyStringSlice(filters.CycleNames),
		DueDate:       dateFilterToSettings(filters.DueDate),
		Estimate:      numberFilterToSettings(filters.Estimate),
	}
}

// dateFilterFromSettings converts a persisted date filter into the Linear API shape.
func dateFilterFromSettings(settings config.DateFilterSettings) linearapi.DateFilter {
	return linearapi.DateFilter{
		Eq:   settings.Eq,
		GT:   settings.GT,
		GTE:  settings.GTE,
		LT:   settings.LT,
		LTE:  settings.LTE,
		Null: copyBoolPtr(settings.Null),
	}
}

// dateFilterToSettings converts a runtime date filter into the persisted shape.
func dateFilterToSettings(filter linearapi.DateFilter) config.DateFilterSettings {
	return config.DateFilterSettings{
		Eq:   filter.Eq,
		GT:   filter.GT,
		GTE:  filter.GTE,
		LT:   filter.LT,
		LTE:  filter.LTE,
		Null: copyBoolPtr(filter.Null),
	}
}

// numberFilterFromSettings converts a persisted number filter into the Linear API shape.
func numberFilterFromSettings(settings config.NumberFilterSettings) linearapi.NumberFilter {
	return linearapi.NumberFilter{
		Eq:   copyFloatPtr(settings.Eq),
		GT:   copyFloatPtr(settings.GT),
		GTE:  copyFloatPtr(settings.GTE),
		LT:   copyFloatPtr(settings.LT),
		LTE:  copyFloatPtr(settings.LTE),
		Null: copyBoolPtr(settings.Null),
	}
}

// numberFilterToSettings converts a runtime number filter into the persisted shape.
func numberFilterToSettings(filter linearapi.NumberFilter) config.NumberFilterSettings {
	return config.NumberFilterSettings{
		Eq:   copyFloatPtr(filter.Eq),
		GT:   copyFloatPtr(filter.GT),
		GTE:  copyFloatPtr(filter.GTE),
		LT:   copyFloatPtr(filter.LT),
		LTE:  copyFloatPtr(filter.LTE),
		Null: copyBoolPtr(filter.Null),
	}
}

// copyStringSlice returns an isolated copy of a string slice.
func copyStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

// copyFloatPtr returns an isolated copy of a float pointer.
func copyFloatPtr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// copyBoolPtr returns an isolated copy of a bool pointer.
func copyBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
