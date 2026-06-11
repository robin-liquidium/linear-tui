package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// SortField represents a Linear issue order field.
type SortField string

const (
	SortByUpdatedAt SortField = "updatedAt"
	SortByCreatedAt SortField = "createdAt"
	SortByPriority  SortField = "priority"
	SortByStatus    SortField = "status"
	SortByOrder     SortField = "order"
)

const (
	customViewAssigneeMe = "me"
	IconExpanded         = "▼"
	IconCollapsed        = "▶"
)

// IssueFilters contains structured filters applied in addition to navigation.
type IssueFilters struct {
	TeamIDs       []string
	TeamNames     []string
	AssigneeIDs   []string
	AssigneeNames []string
	LabelIDs      []string
	LabelNames    []string
	StateIDs      []string
	StateNames    []string
	ProjectIDs    []string
	ProjectNames  []string
	CycleIDs      []string
	CycleNames    []string
	DueDate       linearapi.DateFilter
	Estimate      linearapi.NumberFilter
}

// Empty reports whether no rich issue filters are active.
func (f IssueFilters) Empty() bool {
	return len(f.TeamIDs) == 0 &&
		len(f.AssigneeIDs) == 0 &&
		len(f.LabelIDs) == 0 &&
		len(f.StateIDs) == 0 &&
		len(f.ProjectIDs) == 0 &&
		len(f.CycleIDs) == 0 &&
		f.DueDate.Empty() &&
		f.Estimate.Empty()
}

// Summary returns the status-bar text for active rich filters.
func (f IssueFilters) Summary() string {
	parts := make([]string, 0, 9)
	if len(f.TeamIDs) > 0 {
		parts = append(parts, "team="+formatFilterSummaryValues(f.TeamIDs, f.TeamNames))
	}
	if len(f.AssigneeIDs) > 0 {
		parts = append(parts, "assignee="+formatFilterSummaryValues(f.AssigneeIDs, f.AssigneeNames))
	}
	if len(f.LabelIDs) > 0 {
		parts = append(parts, "labels="+formatFilterSummaryValues(f.LabelIDs, f.LabelNames))
	}
	if len(f.StateIDs) > 0 {
		parts = append(parts, "status="+formatFilterSummaryValues(f.StateIDs, f.StateNames))
	}
	if len(f.ProjectIDs) > 0 {
		parts = append(parts, "project="+formatFilterSummaryValues(f.ProjectIDs, f.ProjectNames))
	}
	if len(f.CycleIDs) > 0 {
		parts = append(parts, "cycle="+formatFilterSummaryValues(f.CycleIDs, f.CycleNames))
	}
	if !f.DueDate.Empty() {
		parts = append(parts, "due="+formatDateFilterSummary(f.DueDate))
	}
	if !f.Estimate.Empty() {
		parts = append(parts, "estimate="+formatNumberFilterSummary(f.Estimate))
	}
	return strings.Join(parts, ", ")
}

// IssuesSection identifies which issue table is active.
type IssuesSection int

const (
	IssuesSectionMy IssuesSection = iota
	IssuesSectionOther
)

// defaultIssuesSection returns the issue section that should receive focus by default.
func defaultIssuesSection(showMyIssues bool) IssuesSection {
	if showMyIssues {
		return IssuesSectionMy
	}
	return IssuesSectionOther
}

// sortCyclesForNavigation orders cycles in the way users expect to scan them.
func sortCyclesForNavigation(cycles []linearapi.Cycle) {
	sort.SliceStable(cycles, func(i, j int) bool {
		leftRank := cycleNavigationRank(cycles[i])
		rightRank := cycleNavigationRank(cycles[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if cycles[i].IsFuture || cycles[i].IsNext {
			return cycles[i].StartsAt.Before(cycles[j].StartsAt)
		}
		return cycles[i].StartsAt.After(cycles[j].StartsAt)
	})
}

func cycleNavigationRank(cycle linearapi.Cycle) int {
	switch {
	case cycle.IsActive:
		return 0
	case cycle.IsNext:
		return 1
	case cycle.IsFuture:
		return 2
	case cycle.IsPrevious:
		return 3
	case cycle.IsPast:
		return 4
	default:
		return 5
	}
}

// NavigationNode represents one selectable node in the workspace navigation.
type NavigationNode struct {
	ID              string
	Text            string
	TeamID          string
	Children        []*NavigationNode
	IsTeam          bool
	IsProject       bool
	IsStatus        bool
	IsCycle         bool
	StateID         string
	StateName       string
	CycleID         string
	CycleName       string
	IsCustomView    bool
	CustomViewID    string
	IsCustomViewAdd bool
}

// validateLinearDate accepts only Linear's YYYY-MM-DD date format.
func validateLinearDate(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errRequiredDate()
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errInvalidDate()
	}
	return nil
}

// parseEstimateInput converts a user-entered estimate into Linear points.
func parseEstimateInput(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errRequiredEstimate()
	}
	estimate, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, errInvalidEstimate()
	}
	if estimate < 0 {
		return 0, errNegativeEstimate()
	}
	return estimate, nil
}

// namesForIDs maps selected IDs back to human labels while preserving order.
func namesForIDs(ids []string, names map[string]string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := names[id]; name != "" {
			result = append(result, name)
			continue
		}
		result = append(result, id)
	}
	return result
}

// resolvedNamesForIDs overlays known labels on selected IDs while preserving saved labels as fallback.
func resolvedNamesForIDs(ids []string, saved []string, known map[string]string) []string {
	if len(ids) == 0 {
		return nil
	}
	result := make([]string, 0, len(ids))
	for index, id := range ids {
		if name := strings.TrimSpace(known[id]); name != "" {
			result = append(result, name)
			continue
		}
		if index < len(saved) {
			if name := strings.TrimSpace(saved[index]); name != "" {
				result = append(result, name)
				continue
			}
		}
		result = append(result, id)
	}
	return result
}

// relationInputForIssue converts a friendly relation label into Linear mutation input.
func relationInputForIssue(issueID, label, targetIssueID string) (linearapi.CreateIssueRelationInput, error) {
	label = strings.ToLower(strings.TrimSpace(label))
	targetIssueID = strings.TrimSpace(targetIssueID)
	if issueID == "" {
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("no issue selected")
	}
	if targetIssueID == "" {
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("related issue ID is required")
	}
	switch label {
	case "blocking":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationBlocks}, nil
	case "blocked by":
		return linearapi.CreateIssueRelationInput{IssueID: targetIssueID, RelatedIssueID: issueID, Type: linearapi.IssueRelationBlocks}, nil
	case "related":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationRelated}, nil
	case "duplicate":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationDuplicate}, nil
	case "similar":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationSimilar}, nil
	default:
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("unsupported relation type %q", label)
	}
}

// excludedParentCandidateIDs returns the selected issue and descendants disallowed as parents.
func excludedParentCandidateIDs(selected *linearapi.Issue, issues []linearapi.Issue) map[string]bool {
	excluded := make(map[string]bool)
	if selected == nil {
		return excluded
	}
	excluded[selected.ID] = true
	byID := make(map[string]linearapi.Issue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	var visit func(issue linearapi.Issue)
	visit = func(issue linearapi.Issue) {
		for _, child := range issue.Children {
			if excluded[child.ID] {
				continue
			}
			excluded[child.ID] = true
			if fullChild, ok := byID[child.ID]; ok {
				visit(fullChild)
			}
		}
	}
	visit(*selected)
	return excluded
}

// compareCustomSort compares two issues for a configured custom-view sort field.
func compareCustomSort(field config.CustomViewSortField, left linearapi.Issue, right linearapi.Issue, statusOrder map[string]float64) int {
	switch field {
	case config.CustomViewSortUpdatedAt:
		return compareTimeDesc(left.UpdatedAt, right.UpdatedAt)
	case config.CustomViewSortCreatedAt:
		return compareTimeDesc(left.CreatedAt, right.CreatedAt)
	case config.CustomViewSortPriority:
		li := left.Priority
		ri := right.Priority
		if li == 0 {
			li = 5
		}
		if ri == 0 {
			ri = 5
		}
		return compareInts(li, ri)
	case config.CustomViewSortStatus:
		if cmp := compareStatusByName(left.State, right.State); cmp != 0 {
			return cmp
		}
		li, lok := statusOrder[left.StateID]
		ri, rok := statusOrder[right.StateID]
		if lok && rok {
			return compareFloats(li, ri)
		}
		if lok && !rok {
			return -1
		}
		if !lok && rok {
			return 1
		}
		return strings.Compare(left.State, right.State)
	default:
		return 0
	}
}

// formatCycleName returns a compact cycle label for table/details rendering.
func formatCycleName(cycle *linearapi.CycleRef) string {
	if cycle == nil {
		return "-"
	}
	return cycle.DisplayName()
}

// formatDueDate returns a compact due-date label for table/details rendering.
func formatDueDate(dueDate *string) string {
	if dueDate == nil || strings.TrimSpace(*dueDate) == "" {
		return "-"
	}
	return strings.TrimSpace(*dueDate)
}

// formatEstimate returns a compact estimate label for table/details rendering.
func formatEstimate(estimate *float64) string {
	if estimate == nil {
		return "-"
	}
	return strconv.FormatFloat(*estimate, 'f', -1, 64)
}

// formatMilestoneName returns a compact project milestone label.
func formatMilestoneName(milestone *linearapi.ProjectMilestoneRef) string {
	if milestone == nil || strings.TrimSpace(milestone.Name) == "" {
		return "-"
	}
	return milestone.Name
}

// formatIssueReference returns a compact issue reference for relations and parent links.
func formatIssueReference(ref linearapi.IssueRef) string {
	if ref.Identifier == "" {
		return ref.ID
	}
	if ref.Title == "" {
		return ref.Identifier
	}
	return fmt.Sprintf("%s - %s", ref.Identifier, ref.Title)
}

// formatUserDisplayName picks the best available human label for a Linear user.
func formatUserDisplayName(user linearapi.User) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Name != "" {
		return user.Name
	}
	return user.ID
}

// renderIssueRow formats an issue into stable table-model columns for tests.
func renderIssueRow(issue linearapi.Issue) []string {
	identifier := truncateIssueColumn(issue.Identifier)
	state := truncateIssueColumn(issue.State)
	assignee := issue.Assignee
	if assignee == "" {
		assignee = "Unassigned"
	}
	cycle := truncateIssueColumn(formatCycleName(issue.Cycle))
	milestone := truncateIssueColumn(formatMilestoneName(issue.ProjectMilestone))
	return []string{
		identifier,
		state,
		formatPriorityLabel(issue.Priority),
		issue.Title,
		truncateIssueColumn(assignee),
		cycle,
		formatDueDate(issue.DueDate),
		formatEstimate(issue.Estimate),
		milestone,
	}
}

// formatPriorityLabel returns Linear priority text without binding to a UI color library.
func formatPriorityLabel(priority int) string {
	switch priority {
	case 1:
		return "Urgent"
	case 2:
		return "High"
	case 3:
		return "Normal"
	case 4:
		return "Low"
	default:
		return "-"
	}
}

func truncateIssueColumn(value string) string {
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func formatFilterSummaryValues(ids []string, labels []string) string {
	if len(labels) > 0 {
		return strings.Join(labels, ",")
	}
	return strings.Join(ids, ",")
}

func formatDateFilterSummary(filter linearapi.DateFilter) string {
	switch {
	case filter.Eq != "":
		return filter.Eq
	case filter.GTE != "":
		return ">=" + filter.GTE
	case filter.GT != "":
		return ">" + filter.GT
	case filter.LTE != "":
		return "<=" + filter.LTE
	case filter.LT != "":
		return "<" + filter.LT
	case filter.Null != nil && *filter.Null:
		return "none"
	case filter.Null != nil:
		return "set"
	default:
		return ""
	}
}

func formatNumberFilterSummary(filter linearapi.NumberFilter) string {
	switch {
	case filter.Eq != nil:
		return formatEstimate(filter.Eq)
	case filter.GTE != nil:
		return ">=" + formatEstimate(filter.GTE)
	case filter.GT != nil:
		return ">" + formatEstimate(filter.GT)
	case filter.LTE != nil:
		return "<=" + formatEstimate(filter.LTE)
	case filter.LT != nil:
		return "<" + formatEstimate(filter.LT)
	case filter.Null != nil && *filter.Null:
		return "none"
	case filter.Null != nil:
		return "set"
	default:
		return ""
	}
}

func compareTimeDesc(left time.Time, right time.Time) int {
	if left.After(right) {
		return -1
	}
	if left.Before(right) {
		return 1
	}
	return 0
}

func compareInts(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareFloats(left float64, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

var statusNameOrder = map[string]int{
	"in review":   0,
	"in progress": 1,
	"to do":       2,
	"todo":        2,
	"backlog":     3,
}

func compareStatusByName(left string, right string) int {
	li, lok := statusNameOrder[strings.ToLower(strings.TrimSpace(left))]
	ri, rok := statusNameOrder[strings.ToLower(strings.TrimSpace(right))]
	if lok && rok {
		return compareInts(li, ri)
	}
	if lok && !rok {
		return -1
	}
	if !lok && rok {
		return 1
	}
	return 0
}
