package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

const customViewAssigneeMe = "me"

// CustomViewModal manages the custom view form overlay.
type CustomViewModal struct {
	app *App

	modal     *tview.Flex
	form      *tview.Form
	headerView *tview.TextView

	teamField      *tview.DropDown
	projectField   *tview.DropDown
	statusField    *tview.DropDown
	assigneeField  *tview.DropDown
	labelField     *tview.DropDown
	sortPrimary    *tview.DropDown
	sortSecondary  *tview.DropDown
	dueWithinField *tview.InputField

	teamIDs      []string
	projectIDs   []string
	stateIDs     []string
	assigneeIDs  []string
	labelIDs     []string
	primarySorts []config.CustomViewSortField
	secondary    []config.CustomViewSortField

	currentView *config.CustomView
	onSave      func(view config.CustomView)
}

// NewCustomViewModal creates a new custom view modal.
func NewCustomViewModal(app *App) *CustomViewModal {
	cm := &CustomViewModal{app: app}

	cm.form = tview.NewForm()
	cm.form.SetBackgroundColor(app.theme.HeaderBg)
	cm.form.SetFieldBackgroundColor(app.theme.InputBg)
	cm.form.SetFieldTextColor(app.theme.Foreground)
	cm.form.SetButtonBackgroundColor(app.theme.Accent)
	cm.form.SetButtonTextColor(app.theme.SelectionText)
	cm.form.SetLabelColor(app.theme.Foreground)

	cm.form.AddInputField("Name", "", 40, nil, nil)

	cm.form.AddDropDown("Team", []string{"Any"}, 0, func(_ string, index int) {
		teamID := ""
		if index > 0 && index <= len(cm.teamIDs) {
			teamID = cm.teamIDs[index-1]
		}
		if cm.currentView == nil {
			return
		}
		cm.updateTeamSelection(teamID)
	})
	if item := cm.form.GetFormItemByLabel("Team"); item != nil {
		cm.teamField = item.(*tview.DropDown)
	}

	cm.form.AddDropDown("Project", []string{"Any (select team)"}, 0, func(_ string, index int) {
		if index == 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Project"); item != nil {
		cm.projectField = item.(*tview.DropDown)
	}

	cm.form.AddDropDown("Status", []string{"Any", "Not Done"}, 0, func(_ string, index int) {
		if index < 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Status"); item != nil {
		cm.statusField = item.(*tview.DropDown)
	}

	cm.form.AddDropDown("Assignee", []string{"Any", "Me"}, 0, func(_ string, index int) {
		if index < 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Assignee"); item != nil {
		cm.assigneeField = item.(*tview.DropDown)
	}

	cm.form.AddDropDown("Label", []string{"Any (select team)"}, 0, func(_ string, index int) {
		if index < 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Label"); item != nil {
		cm.labelField = item.(*tview.DropDown)
	}

	cm.form.AddInputField("Due within days", "", 10, func(text string, last rune) bool {
		return (last >= '0' && last <= '9') || last == 0
	}, nil)
	if item := cm.form.GetFormItemByLabel("Due within days"); item != nil {
		cm.dueWithinField = item.(*tview.InputField)
	}

	cm.form.AddDropDown("Sort primary", []string{"Updated", "Created", "Priority", "Status"}, 0, func(_ string, index int) {
		if index < 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Sort primary"); item != nil {
		cm.sortPrimary = item.(*tview.DropDown)
	}

	cm.form.AddDropDown("Sort secondary", []string{"None", "Updated", "Created", "Priority", "Status"}, 0, func(_ string, index int) {
		if index < 0 {
			return
		}
	})
	if item := cm.form.GetFormItemByLabel("Sort secondary"); item != nil {
		cm.sortSecondary = item.(*tview.DropDown)
	}

	cm.form.AddButton("Save", func() {
		cm.handleSave()
	})
	cm.form.AddButton("Cancel", func() {
		cm.Hide()
	})

	cm.headerView = tview.NewTextView()
	cm.headerView.SetTextColor(app.theme.Accent)
	cm.headerView.SetBackgroundColor(app.theme.HeaderBg)

	helpView := tview.NewTextView()
	helpView.SetText("Tab: next field • Enter: open dropdown • Esc: cancel")
	helpView.SetTextColor(app.theme.SecondaryText)
	helpView.SetBackgroundColor(app.theme.HeaderBg)
	helpView.SetTextAlign(tview.AlignCenter)

	modalContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(cm.headerView, 1, 0, false).
		AddItem(cm.form, 0, 1, true).
		AddItem(helpView, 1, 0, false)
	modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" Custom View ").
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	cm.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modalContent, 20, 0, true).
			AddItem(nil, 0, 1, false), 85, 0, true).
		AddItem(nil, 0, 1, false)
	cm.modal.SetBackgroundColor(app.theme.Background)

	cm.applyDropdownStyles()
	cm.configureSortOptions()

	return cm
}

func (cm *CustomViewModal) applyDropdownStyles() {
	if cm.teamField != nil {
		cm.teamField.SetFieldWidth(50)
		cm.teamField.SetListStyles(
			tcell.StyleDefault.Background(cm.app.theme.HeaderBg).Foreground(cm.app.theme.Foreground),
			tcell.StyleDefault.Background(cm.app.theme.Accent).Foreground(cm.app.theme.SelectionText),
		)
	}
	fields := []*tview.DropDown{cm.projectField, cm.statusField, cm.assigneeField, cm.labelField, cm.sortPrimary, cm.sortSecondary}
	for _, field := range fields {
		if field == nil {
			continue
		}
		field.SetFieldWidth(50)
		field.SetListStyles(
			tcell.StyleDefault.Background(cm.app.theme.HeaderBg).Foreground(cm.app.theme.Foreground),
			tcell.StyleDefault.Background(cm.app.theme.Accent).Foreground(cm.app.theme.SelectionText),
		)
	}
}

func (cm *CustomViewModal) configureSortOptions() {
	primaryLabels := []string{"Updated", "Created", "Priority", "Status"}
	cm.primarySorts = []config.CustomViewSortField{
		config.CustomViewSortUpdatedAt,
		config.CustomViewSortCreatedAt,
		config.CustomViewSortPriority,
		config.CustomViewSortStatus,
	}
	cm.sortPrimary.SetOptions(primaryLabels, nil)

	secondaryLabels := []string{"None", "Updated", "Created", "Priority", "Status"}
	cm.secondary = []config.CustomViewSortField{
		config.CustomViewSortNone,
		config.CustomViewSortUpdatedAt,
		config.CustomViewSortCreatedAt,
		config.CustomViewSortPriority,
		config.CustomViewSortStatus,
	}
	cm.sortSecondary.SetOptions(secondaryLabels, nil)
}

// Show displays the custom view modal.
// Show displays the modal, preloading data from an existing view if provided.
func (cm *CustomViewModal) Show(view *config.CustomView, onSave func(view config.CustomView)) {
	cm.currentView = nil
	cm.onSave = onSave

	editing := view != nil
	if editing {
		copyView := *view
		cm.currentView = &copyView
		cm.headerView.SetText("Edit Custom View")
	} else {
		cm.currentView = &config.CustomView{
			SortPrimary:   config.CustomViewSortUpdatedAt,
			SortSecondary: config.CustomViewSortNone,
		}
		cm.headerView.SetText("Add Custom View")
	}

	cm.resetForm()
	cm.setAssigneeOptions(nil)
	cm.setStatusOptions(nil)
	cm.setProjectOptions(nil)
	cm.setLabelOptions(nil)
	cm.app.pages.AddPage("custom_view", cm.modal, true, true)
	cm.app.pages.SendToFront("custom_view")
	cm.app.app.SetFocus(cm.form)

	cm.loadTeams()
}

// Hide hides the custom view modal.
// Hide closes the modal and restores focus.
func (cm *CustomViewModal) Hide() {
	cm.app.pages.RemovePage("custom_view")
	cm.app.updateFocus()
}

// HandleKey handles keyboard input for the modal.
// HandleKey processes modal-level key events.
func (cm *CustomViewModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		cm.Hide()
		return nil
	}
	return event
}

func (cm *CustomViewModal) resetForm() {
	if item := cm.form.GetFormItemByLabel("Name"); item != nil {
		if input, ok := item.(*tview.InputField); ok {
			_ = input.SetText(cm.currentView.Name)
		}
	}
	if cm.dueWithinField != nil {
		if cm.currentView.DueWithinDays > 0 {
			cm.dueWithinField.SetText(strconv.Itoa(cm.currentView.DueWithinDays))
		} else {
			cm.dueWithinField.SetText("")
		}
	}
	cm.sortPrimary.SetCurrentOption(0)
	cm.sortSecondary.SetCurrentOption(0)
	cm.applySortSelections()
}

func (cm *CustomViewModal) applySortSelections() {
	cm.selectSortOption(cm.sortPrimary, cm.primarySorts, cm.currentView.SortPrimary)
	cm.selectSortOption(cm.sortSecondary, cm.secondary, cm.currentView.SortSecondary)
}

func (cm *CustomViewModal) selectSortOption(field *tview.DropDown, options []config.CustomViewSortField, target config.CustomViewSortField) {
	if field == nil {
		return
	}
	if target == "" {
		return
	}
	for i, opt := range options {
		if opt == target {
			field.SetCurrentOption(i)
			return
		}
	}
}

func (cm *CustomViewModal) loadTeams() {
	teams := cm.app.teams
	if len(teams) > 0 {
		cm.populateTeams(teams)
		return
	}

	go func() {
		loaded, err := cm.app.cache.GetTeams(context.Background())
		if err != nil {
			cm.app.QueueUpdateDraw(func() {
				cm.teamField.SetOptions([]string{"(Failed to load teams)"}, nil)
			})
			return
		}
		cm.app.QueueUpdateDraw(func() {
			cm.app.teams = loaded
			cm.populateTeams(loaded)
		})
	}()
}

func (cm *CustomViewModal) populateTeams(teams []linearapi.Team) {
	labels := []string{"Any"}
	cm.teamIDs = nil
	for _, team := range teams {
		labels = append(labels, team.Name)
		cm.teamIDs = append(cm.teamIDs, team.ID)
	}
	cm.teamField.SetOptions(labels, func(_ string, index int) {
		teamID := ""
		if index > 0 && index <= len(cm.teamIDs) {
			teamID = cm.teamIDs[index-1]
		}
		cm.updateTeamSelection(teamID)
	})

	selectedIndex := 0
	if cm.currentView.TeamID != "" {
		for i, teamID := range cm.teamIDs {
			if teamID == cm.currentView.TeamID {
				selectedIndex = i + 1
				break
			}
		}
	}
	cm.teamField.SetCurrentOption(selectedIndex)

	teamID := ""
	if selectedIndex > 0 && selectedIndex <= len(cm.teamIDs) {
		teamID = cm.teamIDs[selectedIndex-1]
	}
	cm.updateTeamSelection(teamID)
}

func (cm *CustomViewModal) updateTeamSelection(teamID string) {
	cm.currentView.TeamID = teamID
	if teamID == "" {
		cm.setProjectOptions(nil)
		cm.setStatusOptions(nil)
		cm.setLabelOptions(nil)
		cm.setAssigneeOptions(nil)
		return
	}

	go func() {
		ctx := context.Background()
		projects, projErr := cm.app.cache.GetProjects(ctx, teamID)
		states, statesErr := cm.app.cache.GetWorkflowStates(ctx, teamID)
		labels, labelsErr := cm.app.cache.GetIssueLabels(ctx, teamID)
		users, usersErr := cm.app.cache.GetUsers(ctx, teamID)

		cm.app.QueueUpdateDraw(func() {
			if projErr == nil {
				cm.setProjectOptions(projects)
			}
			if statesErr == nil {
				cm.setStatusOptions(states)
			}
			if labelsErr == nil {
				cm.setLabelOptions(labels)
			}
			if usersErr == nil {
				cm.setAssigneeOptions(users)
			}
		})
	}()
}

func (cm *CustomViewModal) setProjectOptions(projects []linearapi.Project) {
	if cm.projectField == nil {
		return
	}
	labels := []string{"Any"}
	cm.projectIDs = nil
	for _, proj := range projects {
		labels = append(labels, proj.Name)
		cm.projectIDs = append(cm.projectIDs, proj.ID)
	}
	cm.projectField.SetOptions(labels, nil)
	cm.projectField.SetCurrentOption(0)
	if cm.currentView.ProjectID != "" {
		for i, id := range cm.projectIDs {
			if id == cm.currentView.ProjectID {
				cm.projectField.SetCurrentOption(i + 1)
				break
			}
		}
	}
}

func (cm *CustomViewModal) setStatusOptions(states []linearapi.WorkflowState) {
	if cm.statusField == nil {
		return
	}
	if len(states) > 1 {
		sort.Slice(states, func(i, j int) bool {
			return states[i].Position < states[j].Position
		})
	}
	labels := []string{"Any", "Not Done"}
	cm.stateIDs = nil
	for _, state := range states {
		labels = append(labels, state.Name)
		cm.stateIDs = append(cm.stateIDs, state.ID)
	}
	cm.statusField.SetOptions(labels, nil)
	cm.statusField.SetCurrentOption(0)
	if cm.currentView.StateMode == config.CustomViewStateNotDone {
		cm.statusField.SetCurrentOption(1)
		return
	}
	if cm.currentView.StateID != "" {
		for i, id := range cm.stateIDs {
			if id == cm.currentView.StateID {
				cm.statusField.SetCurrentOption(i + 2)
				break
			}
		}
	}
}

func (cm *CustomViewModal) setAssigneeOptions(users []linearapi.User) {
	if cm.assigneeField == nil {
		return
	}
	labels := []string{"Any", "Me"}
	cm.assigneeIDs = []string{"", customViewAssigneeMe}
	for _, user := range users {
		if user.IsMe {
			continue
		}
		label := user.Name
		labels = append(labels, label)
		cm.assigneeIDs = append(cm.assigneeIDs, user.ID)
	}
	cm.assigneeField.SetOptions(labels, nil)
	cm.assigneeField.SetCurrentOption(0)
	if cm.currentView.AssigneeID != "" {
		for i, id := range cm.assigneeIDs {
			if id == cm.currentView.AssigneeID {
				cm.assigneeField.SetCurrentOption(i)
				break
			}
		}
	}
}

func (cm *CustomViewModal) setLabelOptions(labels []linearapi.IssueLabel) {
	if cm.labelField == nil {
		return
	}
	options := []string{"Any"}
	cm.labelIDs = nil
	for _, label := range labels {
		options = append(options, label.Name)
		cm.labelIDs = append(cm.labelIDs, label.ID)
	}
	cm.labelField.SetOptions(options, nil)
	cm.labelField.SetCurrentOption(0)
	if cm.currentView.LabelID != "" {
		for i, id := range cm.labelIDs {
			if id == cm.currentView.LabelID {
				cm.labelField.SetCurrentOption(i + 1)
				break
			}
		}
	}
}

func (cm *CustomViewModal) handleSave() {
	nameItem := cm.form.GetFormItemByLabel("Name")
	name := ""
	if nameItem != nil {
		if input, ok := nameItem.(*tview.InputField); ok {
			name = strings.TrimSpace(input.GetText())
		}
	}
	if name == "" {
		cm.app.updateStatusBarWithError(fmt.Errorf("view name is required"))
		return
	}

	view := *cm.currentView
	view.Name = name

	if cm.teamField != nil {
		idx, _ := cm.teamField.GetCurrentOption()
		if idx > 0 && idx <= len(cm.teamIDs) {
			view.TeamID = cm.teamIDs[idx-1]
		} else {
			view.TeamID = ""
		}
	}

	if cm.projectField != nil {
		idx, _ := cm.projectField.GetCurrentOption()
		if idx > 0 && idx <= len(cm.projectIDs) {
			view.ProjectID = cm.projectIDs[idx-1]
		} else {
			view.ProjectID = ""
		}
	}

	if cm.statusField != nil {
		idx, _ := cm.statusField.GetCurrentOption()
		view.StateID = ""
		view.StateMode = config.CustomViewStateAny
		if idx == 1 {
			view.StateMode = config.CustomViewStateNotDone
		} else if idx >= 2 && idx-2 < len(cm.stateIDs) {
			view.StateID = cm.stateIDs[idx-2]
		}
	}

	if cm.assigneeField != nil {
		idx, _ := cm.assigneeField.GetCurrentOption()
		if idx >= 0 && idx < len(cm.assigneeIDs) {
			view.AssigneeID = cm.assigneeIDs[idx]
		} else {
			view.AssigneeID = ""
		}
	}

	if cm.labelField != nil {
		idx, _ := cm.labelField.GetCurrentOption()
		if idx > 0 && idx-1 < len(cm.labelIDs) {
			view.LabelID = cm.labelIDs[idx-1]
		} else {
			view.LabelID = ""
		}
	}

	if cm.dueWithinField != nil {
		text := strings.TrimSpace(cm.dueWithinField.GetText())
		if text == "" {
			view.DueWithinDays = 0
		} else {
			value, err := strconv.Atoi(text)
			if err != nil || value < 0 {
				cm.app.updateStatusBarWithError(fmt.Errorf("invalid due within days"))
				return
			}
			view.DueWithinDays = value
		}
	}

	if cm.sortPrimary != nil {
		idx, _ := cm.sortPrimary.GetCurrentOption()
		if idx >= 0 && idx < len(cm.primarySorts) {
			view.SortPrimary = cm.primarySorts[idx]
		}
	}
	if cm.sortSecondary != nil {
		idx, _ := cm.sortSecondary.GetCurrentOption()
		if idx >= 0 && idx < len(cm.secondary) {
			view.SortSecondary = cm.secondary[idx]
		}
	}

	cm.Hide()
	if cm.onSave != nil {
		cm.onSave(view)
	}
}
