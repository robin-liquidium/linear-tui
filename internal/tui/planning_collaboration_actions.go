package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

var issueRelationTypeLabels = []PickerItem{
	{ID: "blocking", Label: "blocking"},
	{ID: "blocked by", Label: "blocked by"},
	{ID: "related", Label: "related"},
	{ID: "duplicate", Label: "duplicate"},
	{ID: "similar", Label: "similar"},
}

func validateLinearDate(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("date is required")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	return nil
}

func parseEstimateInput(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("estimate is required")
	}
	estimate, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("estimate must be numeric")
	}
	if estimate < 0 {
		return 0, fmt.Errorf("estimate must be non-negative")
	}
	return estimate, nil
}

func (a *App) runIssueUpdate(input linearapi.UpdateIssueInput, successMessage string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	input.ID = issue.ID
	updateIssue := a.updateIssueFunc
	if updateIssue == nil {
		updateIssue = a.api.UpdateIssue
	}
	go func(issueID string) {
		_, err := updateIssue(context.Background(), input)
		a.QueueUpdateDraw(func() {
			if err != nil {
				logger.ErrorWithErr(err, "tui.planning: issue update failed issue=%s", issue.Identifier)
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus(successMessage)
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) showSetDueDateModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	initial := ""
	if issue.DueDate != nil {
		initial = *issue.DueDate
	}
	a.textInputModal.Show("Set Due Date", "YYYY-MM-DD: ", initial, func(value string) {
		if err := validateLinearDate(value); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.setDueDateForSelectedIssue(value)
	})
}

func (a *App) setDueDateForSelectedIssue(value string) {
	value = strings.TrimSpace(value)
	if err := validateLinearDate(value); err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.runIssueUpdate(linearapi.UpdateIssueInput{DueDate: &value}, "Updated due date")
}

func (a *App) clearDueDateForSelectedIssue() {
	empty := ""
	a.runIssueUpdate(linearapi.UpdateIssueInput{DueDate: &empty}, "Cleared due date")
}

func (a *App) showEditEstimateModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	initial := ""
	if issue.Estimate != nil {
		initial = formatEstimate(issue.Estimate)
	}
	a.textInputModal.Show("Edit Estimate", "Points: ", initial, func(value string) {
		a.editEstimateForSelectedIssue(value)
	})
}

func (a *App) editEstimateForSelectedIssue(value string) {
	estimate, err := parseEstimateInput(value)
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.runIssueUpdate(linearapi.UpdateIssueInput{Estimate: &estimate}, "Updated estimate")
}

func (a *App) clearEstimateForSelectedIssue() {
	a.runIssueUpdate(linearapi.UpdateIssueInput{ClearEstimate: true}, "Cleared estimate")
}

func (a *App) selectedIssueProjectID() (string, bool) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return "", false
	}
	if strings.TrimSpace(issue.ProjectID) == "" {
		a.updateStatusBarWithError(fmt.Errorf("issue must have a project"))
		return "", false
	}
	return issue.ProjectID, true
}

func (a *App) showProjectMilestonePicker(title string, onSelect func(linearapi.ProjectMilestone)) {
	projectID, ok := a.selectedIssueProjectID()
	if !ok {
		return
	}
	go func() {
		milestones, err := a.cache.GetProjectMilestones(context.Background(), projectID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			if len(milestones) == 0 {
				a.flashStatus("No project milestones available")
				return
			}
			items := make([]PickerItem, 0, len(milestones))
			byID := make(map[string]linearapi.ProjectMilestone, len(milestones))
			for _, milestone := range milestones {
				label := milestone.Name
				if milestone.TargetDate != nil && *milestone.TargetDate != "" {
					label += " (" + *milestone.TargetDate + ")"
				}
				items = append(items, PickerItem{ID: milestone.ID, Label: label})
				byID[milestone.ID] = milestone
			}
			a.pickerActive = true
			a.pickerModal.Show(title, items, func(item PickerItem) {
				a.pickerActive = false
				if onSelect != nil {
					onSelect(byID[item.ID])
				}
			})
		})
	}()
}

func (a *App) listProjectMilestonesForSelectedIssue() {
	a.showProjectMilestonePicker("Project Milestones", func(milestone linearapi.ProjectMilestone) {
		a.flashStatus(fmt.Sprintf("Milestone: %s", milestone.Name))
	})
}

func (a *App) showSetMilestonePicker() {
	a.showProjectMilestonePicker("Set Milestone", func(milestone linearapi.ProjectMilestone) {
		milestoneID := milestone.ID
		a.runIssueUpdate(linearapi.UpdateIssueInput{ProjectMilestoneID: &milestoneID}, "Set milestone")
	})
}

func (a *App) clearMilestoneForSelectedIssue() {
	empty := ""
	a.runIssueUpdate(linearapi.UpdateIssueInput{ProjectMilestoneID: &empty}, "Cleared milestone")
}

func (a *App) applyFiltersAndRefresh(message string) {
	a.flashStatus(message)
	go a.refreshIssues()
}

func (a *App) clearFilters() {
	a.richFilters = IssueFilters{}
	a.searchQuery = ""
	a.applyFiltersAndRefresh("Cleared filters")
}

func (a *App) showFilterIssuesPicker() {
	items := []PickerItem{
		{ID: "team", Label: "Team"},
		{ID: "assignee", Label: "Assignee"},
		{ID: "labels", Label: "Labels"},
		{ID: "status", Label: "Status"},
		{ID: "project", Label: "Project"},
		{ID: "cycle", Label: "Cycle"},
		{ID: "due", Label: "Due date"},
		{ID: "estimate", Label: "Estimate"},
		{ID: "text", Label: "Text search"},
		{ID: "clear", Label: "Clear filters"},
	}
	a.pickerActive = true
	a.pickerModal.Show("Filter Issues", items, func(item PickerItem) {
		a.pickerActive = false
		switch item.ID {
		case "team":
			a.showTeamFilter()
		case "assignee":
			a.showAssigneeFilter()
		case "labels":
			a.showLabelFilter()
		case "status":
			a.showStatusFilter()
		case "project":
			a.showProjectFilter()
		case "cycle":
			a.showCycleFilter()
		case "due":
			a.showDueDateFilter()
		case "estimate":
			a.showEstimateFilter()
		case "text":
			a.showTextFilter()
		case "clear":
			a.clearFilters()
		}
	})
}

// showTeamFilter opens a picker for workspace teams.
func (a *App) showTeamFilter() {
	teams := a.teams
	if len(teams) > 0 {
		a.showTeamFilterWithTeams(teams)
		return
	}

	go func() {
		loadedTeams, err := a.cache.GetTeams(context.Background())
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.teams = loadedTeams
			a.showTeamFilterWithTeams(loadedTeams)
		})
	}()
}

// showTeamFilterWithTeams renders the team picker with already loaded teams.
func (a *App) showTeamFilterWithTeams(teams []linearapi.Team) {
	items := make([]MultiSelectItem, 0, len(teams))
	teamNames := make(map[string]string, len(teams))
	for _, team := range teams {
		label := team.Name
		if team.Key != "" {
			label = fmt.Sprintf("%s (%s)", team.Name, team.Key)
		}
		items = append(items, MultiSelectItem{ID: team.ID, Label: label})
		teamNames[team.ID] = team.Name
	}

	a.multiSelectModal.Show("Filter Team", items, a.richFilters.TeamIDs, func(ids []string) {
		a.richFilters.TeamIDs = ids
		a.richFilters.TeamNames = namesForIDs(ids, teamNames)
		a.clearTeamScopedFilters()
		if len(ids) == 1 {
			a.preloadTeamFilterMetadata(ids[0])
		}
		a.applyFiltersAndRefresh("Applied team filters")
	})
}

// clearTeamScopedFilters removes filters whose IDs only make sense inside one Linear team.
func (a *App) clearTeamScopedFilters() {
	a.richFilters.AssigneeIDs = nil
	a.richFilters.AssigneeNames = nil
	a.richFilters.LabelIDs = nil
	a.richFilters.LabelNames = nil
	a.richFilters.StateIDs = nil
	a.richFilters.StateNames = nil
	a.richFilters.ProjectIDs = nil
	a.richFilters.ProjectNames = nil
	a.richFilters.CycleIDs = nil
	a.richFilters.CycleNames = nil
}

// preloadTeamFilterMetadata warms the team-specific caches used by follow-up filter pickers.
func (a *App) preloadTeamFilterMetadata(teamID string) {
	if teamID == "" {
		return
	}
	go func() {
		ctx := context.Background()
		_ = a.cache.PreloadTeamMetadata(ctx, teamID)
		users, _ := a.cache.GetUsers(ctx, teamID)
		projects, _ := a.cache.GetProjects(ctx, teamID)
		states, _ := a.cache.GetWorkflowStates(ctx, teamID)
		cycles, _ := a.cache.GetCycles(ctx, teamID)
		sortCyclesForNavigation(cycles)
		a.QueueUpdateDraw(func() {
			a.teamUsers = users
			a.teamProjects = projects
			a.workflowStates = states
			a.teamCycles = cycles
		})
	}()
}

func (a *App) showAssigneeFilter() {
	users := a.teamUsers
	if len(users) > 0 {
		a.showAssigneeFilterWithUsers(users)
		return
	}
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("select exactly one team before filtering assignees"))
		return
	}
	go func() {
		loadedUsers, err := a.cache.GetUsers(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.teamUsers = loadedUsers
			a.showAssigneeFilterWithUsers(loadedUsers)
		})
	}()
}

// showAssigneeFilterWithUsers renders a multi-select assignee filter for loaded users.
func (a *App) showAssigneeFilterWithUsers(users []linearapi.User) {
	items := make([]MultiSelectItem, 0, len(users))
	userNames := make(map[string]string, len(users))
	for _, user := range users {
		label := formatUserDisplayName(user)
		if user.IsMe {
			label += " (me)"
		}
		items = append(items, MultiSelectItem{ID: user.ID, Label: label})
		userNames[user.ID] = label
	}
	a.multiSelectModal.Show("Filter Assignees", items, a.richFilters.AssigneeIDs, func(ids []string) {
		a.richFilters.AssigneeIDs = ids
		a.richFilters.AssigneeNames = namesForIDs(ids, userNames)
		a.applyFiltersAndRefresh("Applied assignee filters")
	})
}

func (a *App) showLabelFilter() {
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("team context is required"))
		return
	}
	go func() {
		labels, err := a.cache.GetIssueLabels(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			items := make([]MultiSelectItem, 0, len(labels))
			labelByID := make(map[string]string, len(labels))
			for _, label := range labels {
				items = append(items, MultiSelectItem{ID: label.ID, Label: label.Name})
				labelByID[label.ID] = label.Name
			}
			a.multiSelectModal.Show("Filter Labels", items, a.richFilters.LabelIDs, func(ids []string) {
				a.richFilters.LabelIDs = ids
				a.richFilters.LabelNames = namesForIDs(ids, labelByID)
				a.applyFiltersAndRefresh("Applied label filters")
			})
		})
	}()
}

func (a *App) showStatusFilter() {
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("select exactly one team before filtering statuses"))
		return
	}
	go func() {
		states, err := a.cache.GetWorkflowStates(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.workflowStates = states
			a.showStatusFilterWithStates(states)
		})
	}()
}

// showStatusFilterWithStates renders a multi-select status filter for loaded workflow states.
func (a *App) showStatusFilterWithStates(states []linearapi.WorkflowState) {
	items := make([]MultiSelectItem, 0, len(states))
	stateNames := make(map[string]string, len(states))
	for _, state := range states {
		items = append(items, MultiSelectItem{ID: state.ID, Label: state.Name})
		stateNames[state.ID] = state.Name
	}
	a.multiSelectModal.Show("Filter Statuses", items, a.richFilters.StateIDs, func(ids []string) {
		a.richFilters.StateIDs = ids
		a.richFilters.StateNames = namesForIDs(ids, stateNames)
		a.applyFiltersAndRefresh("Applied status filters")
	})
}

func (a *App) showProjectFilter() {
	projects := a.teamProjects
	if len(projects) == 0 {
		teamID := a.GetSelectedTeamID()
		if teamID == "" {
			a.updateStatusBarWithError(fmt.Errorf("team context is required"))
			return
		}
		go func() {
			loadedProjects, err := a.cache.GetProjects(context.Background(), teamID)
			a.QueueUpdateDraw(func() {
				if err != nil {
					a.updateStatusBarWithError(err)
					return
				}
				a.teamProjects = loadedProjects
				a.showProjectFilterWithProjects(loadedProjects)
			})
		}()
		return
	}
	a.showProjectFilterWithProjects(projects)
}

func (a *App) showProjectFilterWithProjects(projects []linearapi.Project) {
	items := make([]MultiSelectItem, 0, len(projects))
	projectNames := make(map[string]string, len(projects))
	for _, project := range projects {
		items = append(items, MultiSelectItem{ID: project.ID, Label: project.Name})
		projectNames[project.ID] = project.Name
	}
	a.multiSelectModal.Show("Filter Projects", items, a.richFilters.ProjectIDs, func(ids []string) {
		a.richFilters.ProjectIDs = ids
		a.richFilters.ProjectNames = namesForIDs(ids, projectNames)
		a.applyFiltersAndRefresh("Applied project filters")
	})
}

func (a *App) showCycleFilter() {
	cycles := a.teamCycles
	if len(cycles) > 0 {
		a.showCycleFilterWithCycles(cycles)
		return
	}
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("select exactly one team before filtering cycles"))
		return
	}
	go func() {
		loadedCycles, err := a.cache.GetCycles(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			sortCyclesForNavigation(loadedCycles)
			a.teamCycles = loadedCycles
			a.showCycleFilterWithCycles(loadedCycles)
		})
	}()
}

// showCycleFilterWithCycles renders a multi-select cycle filter for loaded cycles.
func (a *App) showCycleFilterWithCycles(cycles []linearapi.Cycle) {
	items := make([]MultiSelectItem, 0, len(cycles))
	cycleNames := make(map[string]string, len(cycles))
	for _, cycle := range cycles {
		label := cycle.DisplayName()
		switch {
		case cycle.IsActive:
			label += " (active)"
		case cycle.IsNext:
			label += " (next)"
		case cycle.IsPrevious:
			label += " (previous)"
		}
		items = append(items, MultiSelectItem{ID: cycle.ID, Label: label})
		cycleNames[cycle.ID] = cycle.DisplayName()
	}
	a.multiSelectModal.Show("Filter Cycles", items, a.richFilters.CycleIDs, func(ids []string) {
		a.richFilters.CycleIDs = ids
		a.richFilters.CycleNames = namesForIDs(ids, cycleNames)
		a.applyFiltersAndRefresh("Applied cycle filters")
	})
}

func (a *App) showDueDateFilter() {
	initial := formatDateFilterSummary(a.richFilters.DueDate)
	a.textInputModal.Show("Filter Due Date", "YYYY-MM-DD: ", initial, func(value string) {
		if err := validateLinearDate(value); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.richFilters.DueDate = linearapi.DateFilter{Eq: value}
		a.applyFiltersAndRefresh("Applied due date filter")
	})
}

func (a *App) showEstimateFilter() {
	initial := formatNumberFilterSummary(a.richFilters.Estimate)
	a.textInputModal.Show("Filter Estimate", "Points: ", initial, func(value string) {
		estimate, err := parseEstimateInput(value)
		if err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.richFilters.Estimate = linearapi.NumberFilter{Eq: &estimate}
		a.applyFiltersAndRefresh("Applied estimate filter")
	})
}

func (a *App) showTextFilter() {
	a.textInputModal.Show("Filter Text", "Search: ", a.searchQuery, func(value string) {
		a.searchQuery = strings.TrimSpace(value)
		a.applyFiltersAndRefresh("Applied text filter")
	})
}

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

func (a *App) showAddIssueRelationPicker() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	a.pickerActive = true
	a.pickerModal.Show("Relation Type", issueRelationTypeLabels, func(item PickerItem) {
		a.pickerActive = false
		a.textInputModal.Show("Related Issue", "Issue ID: ", "", func(targetIssueID string) {
			a.createIssueRelationForSelectedIssue(item.ID, targetIssueID)
		})
	})
}

func (a *App) createIssueRelationForSelectedIssue(label, targetIssueID string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	input, err := relationInputForIssue(issue.ID, label, targetIssueID)
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	createRelation := a.createIssueRelationFunc
	if createRelation == nil {
		createRelation = a.api.CreateIssueRelation
	}
	go func(issueID string) {
		_, err := createRelation(context.Background(), input)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Added issue relation")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) showRemoveIssueRelationPicker() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	if len(issue.Relations) == 0 {
		a.flashStatus("No issue relations")
		return
	}
	items := make([]PickerItem, 0, len(issue.Relations))
	for _, relation := range issue.Relations {
		ref := relation.RelatedIssue
		if relation.Inverse {
			ref = relation.Issue
		}
		items = append(items, PickerItem{
			ID:    relation.ID,
			Label: relation.DisplayType() + " " + formatIssueReference(ref),
		})
	}
	a.pickerActive = true
	a.pickerModal.Show("Remove Relation", items, func(item PickerItem) {
		a.pickerActive = false
		a.deleteIssueRelationForSelectedIssue(item.ID)
	})
}

func (a *App) deleteIssueRelationForSelectedIssue(relationID string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	deleteRelation := a.deleteIssueRelationFunc
	if deleteRelation == nil {
		deleteRelation = a.api.DeleteIssueRelation
	}
	go func(issueID string) {
		err := deleteRelation(context.Background(), relationID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Removed issue relation")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) subscribeSelectedIssue() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	subscribe := a.subscribeIssueFunc
	if subscribe == nil {
		subscribe = a.api.SubscribeToIssue
	}
	go func(issueID string) {
		_, err := subscribe(context.Background(), issueID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Subscribed to issue")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) unsubscribeSelectedIssue() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	unsubscribe := a.unsubscribeIssueFunc
	if unsubscribe == nil {
		unsubscribe = a.api.UnsubscribeFromIssue
	}
	go func(issueID string) {
		_, err := unsubscribe(context.Background(), issueID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Unsubscribed from issue")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) openSelectedAttachment() {
	a.runAttachmentAction("Open Attachment", func(attachment linearapi.Attachment) {
		openFn := a.openURLFunc
		if openFn == nil {
			openFn = openURL
		}
		if err := openFn(attachment.URL); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.flashStatus("Opened attachment")
	})
}

func (a *App) copySelectedAttachmentURL() {
	a.runAttachmentAction("Copy Attachment URL", func(attachment linearapi.Attachment) {
		copyFn := a.copyToClipboardFunc
		if copyFn == nil {
			copyFn = copyToClipboard
		}
		if err := copyFn(attachment.URL); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.flashStatus("Copied attachment URL")
	})
}

func (a *App) runAttachmentAction(title string, action func(linearapi.Attachment)) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	attachments := issue.Attachments
	if len(attachments) == 0 {
		a.flashStatus("No attachments")
		return
	}
	if len(attachments) == 1 {
		action(attachments[0])
		return
	}
	byID := make(map[string]linearapi.Attachment, len(attachments))
	items := make([]PickerItem, 0, len(attachments))
	for _, attachment := range attachments {
		label := attachment.Title
		if label == "" {
			label = attachment.URL
		}
		if attachment.SourceType != "" {
			label += " (" + attachment.SourceType + ")"
		}
		byID[attachment.ID] = attachment
		items = append(items, PickerItem{ID: attachment.ID, Label: label})
	}
	a.pickerActive = true
	a.pickerModal.Show(title, items, func(item PickerItem) {
		a.pickerActive = false
		action(byID[item.ID])
	})
}
