package tui

import (
	"context"
	"fmt"
	"html"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/cache"
	"github.com/roeyazroel/linear-tui/internal/calendar"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

var markdownURLPattern = regexp.MustCompile(`https?://[^\s<>()\[\]]+`)
var calendarHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

const minIssueFetchPageSize = 25
const wheelScrollStep = 3
const wheelBoundarySuppressWindow = 180 * time.Millisecond
const issueAutoRefreshInterval = time.Hour

type charmPane int

const (
	charmPaneNav charmPane = iota
	charmPaneCalendar
	charmPaneIssues
	charmPaneDetails
)

type charmOverlay int

const (
	charmOverlayNone charmOverlay = iota
	charmOverlayPalette
	charmOverlayPicker
	charmOverlayMultiSelect
	charmOverlaySettings
	charmOverlayCustomView
	charmOverlayConfirmDeleteView
	charmOverlayConfirmArchive
	charmOverlayConfirmRemoveParent
	charmOverlayIssueForm
	charmOverlayAgentPrompt
	charmOverlayAgentOutput
	charmOverlayPromptTemplates
)

type charmPickerAction int

const (
	charmPickerPriority charmPickerAction = iota
	charmPickerStatus
	charmPickerAssignee
	charmPickerCreateAssignee
	charmPickerCycle
	charmPickerMilestone
	charmPickerListMilestone
	charmPickerRelationType
	charmPickerRemoveRelation
	charmPickerOpenAttachment
	charmPickerCopyAttachment
	charmPickerParent
	charmPickerFilterKind
	charmPickerIssueSort
)

type charmMultiSelectAction int

const (
	charmMultiSelectLabels charmMultiSelectAction = iota
	charmMultiSelectFilterTeam
	charmMultiSelectFilterAssignee
	charmMultiSelectFilterLabel
	charmMultiSelectFilterStatus
	charmMultiSelectFilterProject
	charmMultiSelectFilterCycle
)

type charmCommandID string

type charmIssueFormMode int

const (
	charmFormCreateIssue charmIssueFormMode = iota
	charmFormEditTitle
	charmFormEditDescription
	charmFormAddComment
	charmFormSetDueDate
	charmFormSetEstimate
	charmFormIssueRelationTarget
	charmFormFilterText
	charmFormFilterDueDate
	charmFormFilterEstimate
)

type charmCommand struct {
	ID       charmCommandID
	Title    string
	Keywords []string
}

// charmCommandItem lets Bubbles render commands through its default list delegate.
type charmCommandItem struct {
	command charmCommand
}

func (i charmCommandItem) FilterValue() string {
	return strings.TrimSpace(i.command.Title + " " + string(i.command.ID) + " " + strings.Join(i.command.Keywords, " "))
}

func (i charmCommandItem) Title() string {
	return i.command.Title
}

func (i charmCommandItem) Description() string {
	return ""
}

type charmPickerItem struct {
	ID       string
	Label    string
	Priority int
}

// charmUndoAction stores the inverse of the latest optimistic issue edit.
type charmUndoAction struct {
	Before linearapi.Issue
	After  linearapi.Issue
	Input  linearapi.UpdateIssueInput
	Status string
}

// charmAgentRunFunc lets tests replace the external agent process while the app keeps provider plumbing.
type charmAgentRunFunc func(context.Context, agents.Provider, string, string, agents.AgentRunOptions, func(agents.AgentEvent), func(string), func(error)) error

// charmSettingsField describes one editable settings row.
type charmSettingsField struct {
	Key     string
	Label   string
	Value   string
	Options []string
}

// charmMultiSelectItem describes one row in a Charm multi-select overlay.
type charmMultiSelectItem struct {
	ID    string
	Label string
}

// CharmApp is the Bubble Tea application model for the Linear TUI.
type CharmApp struct {
	api                  *linearapi.Client
	cache                *cache.TeamCache
	cfg                  config.Config
	settingsPath         string
	updateIssueFunc      func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error)
	archiveIssueFunc     func(context.Context, string) error
	createIssueFunc      func(context.Context, linearapi.CreateIssueInput) (linearapi.Issue, error)
	createCommentFunc    func(context.Context, linearapi.CreateCommentInput) (linearapi.Comment, error)
	createRelationFunc   func(context.Context, linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error)
	deleteRelationFunc   func(context.Context, string) error
	subscribeIssueFunc   func(context.Context, string) (linearapi.Issue, error)
	unsubscribeIssueFunc func(context.Context, string) (linearapi.Issue, error)
	openURLFunc          func(string) error
	copyToClipboardFunc  func(string) error
	loadLabelsFunc       func(context.Context, string) ([]linearapi.IssueLabel, error)
	loadMilestonesFunc   func(context.Context, string) ([]linearapi.ProjectMilestone, error)
	fetchIssueByIDFunc   func(context.Context, string) (linearapi.Issue, error)
	calendarListWeekFunc func(context.Context, time.Time) ([]calendar.Event, error)
	calendarDeleteFunc   func(context.Context, string, string) error
	calendarCache        *calendar.Cache
	agentRunFunc         charmAgentRunFunc

	customViews []config.CustomView

	theme  Theme
	styles charmStyles
	keys   charmKeyMap
	help   help.Model
	undo   *charmUndoAction

	width  int
	height int

	focusedPane         charmPane
	searchMode          bool
	searchQuery         string
	sortOverride        SortField
	richFilters         IssueFilters
	apiKeyMode          bool
	apiKeyInput         textinput.Model
	overlay             charmOverlay
	paletteInput        textinput.Model
	paletteList         list.Model
	pickerTitle         string
	pickerAction        charmPickerAction
	pickerItems         []charmPickerItem
	pickerCursor        int
	pickerLoading       bool
	multiTitle          string
	multiAction         charmMultiSelectAction
	multiItems          []charmMultiSelectItem
	multiSelected       map[string]bool
	multiCursor         int
	settingsFields      []charmSettingsField
	settingsCursor      int
	settingsInput       textinput.Model
	customViewsPath     string
	customViewFields    []charmSettingsField
	customViewCursor    int
	customViewInput     textinput.Model
	customViewEditing   string
	formMode            charmIssueFormMode
	formTitle           string
	formFocus           int
	formIssueID         string
	formTeamID          string
	formProjectID       string
	formCycleID         string
	formParentID        string
	formAssigneeID      string
	formAssigneeName    string
	formRelationLabel   string
	titleInput          textinput.Model
	bodyArea            textarea.Model
	agentPromptArea     textarea.Model
	agentWorkspace      textinput.Model
	agentPromptFocus    int
	agentTemplate       int
	promptsPath         string
	promptTplCursor     int
	promptTplFocus      int
	promptTplName       textinput.Model
	promptTplBody       textarea.Model
	agentOutput         viewport.Model
	agentOutputLines    []string
	agentFinalText      string
	agentOutputTitle    string
	agentOutputStatus   string
	agentSessionID      string
	agentResumeCmd      string
	agentRunning        bool
	agentCancel         context.CancelFunc
	agentEvents         chan tea.Msg
	agentBuffer         *AgentStreamBuffer
	status              string
	err                 error
	loading             bool
	loadingSpinner      spinner.Model
	draggingNavigation  bool
	draggingDetails     bool
	navWidth            int
	detailsWidth        int
	wheelSuppressPane   charmPane
	wheelSuppressBtn    tea.MouseButton
	wheelSuppressTill   time.Time
	calendarWeekStart   time.Time
	calendarToday       time.Time
	calendarEvents      []calendar.Event
	calendarSelectedDay int
	calendarSelectedIdx int
	calendarLoading     bool
	calendarErr         error
	calendarDetails     bool
	calendarCacheTime   time.Time

	currentUser        *linearapi.User
	teams              []linearapi.Team
	navigation         []*NavigationNode
	navigationCursor   int
	selectedNavigation *NavigationNode
	selectedCustomView *config.CustomView
	expandedTeams      map[string]bool
	loadingTeams       map[string]bool
	teamChildren       map[string][]*NavigationNode

	issues        []linearapi.Issue
	myRows        []IssueRow
	myIssueMap    map[string]*linearapi.Issue
	otherRows     []IssueRow
	otherIssueMap map[string]*linearapi.Issue
	expanded      map[string]bool

	myTable       table.Model
	otherTable    table.Model
	activeSection IssuesSection
	details       viewport.Model
	selectedIssue *linearapi.Issue

	issueResults *issueResultCache
	issueDetails *issueDetailCache
	issueDisk    *issueDiskCache

	agentRunner          *agents.Runner
	agentPromptTemplates []config.AgentPromptTemplate
}

// NewCharmApp creates a Bubble Tea application with existing Linear configuration.
func NewCharmApp(api *linearapi.Client, cfg config.Config, customViews []config.CustomView) CharmApp {
	settingsPath, _ := config.ConfigFilePath()
	return NewCharmAppWithSettingsPath(api, cfg, customViews, settingsPath)
}

// NewCharmAppWithSettingsPath creates a Bubble Tea app that persists user settings at path.
func NewCharmAppWithSettingsPath(api *linearapi.Client, cfg config.Config, customViews []config.CustomView, settingsPath string, promptTemplateArgs ...[]config.AgentPromptTemplate) CharmApp {
	theme := ResolveTheme(cfg.Theme)
	apiKeyInput := textinput.New()
	apiKeyInput.Placeholder = "lin_api_..."
	apiKeyInput.Prompt = "Linear API key: "
	apiKeyInput.EchoMode = textinput.EchoPassword
	apiKeyInput.EchoCharacter = '*'
	apiKeyInput.CharLimit = 256
	apiKeyInput.SetWidth(64)
	paletteInput := textinput.New()
	paletteInput.Placeholder = "Type to filter commands..."
	paletteInput.Prompt = "> "
	paletteInput.CharLimit = 120
	paletteInput.SetWidth(56)
	settingsInput := textinput.New()
	settingsInput.Prompt = ""
	settingsInput.CharLimit = 256
	settingsInput.SetWidth(54)
	customViewInput := textinput.New()
	customViewInput.Prompt = ""
	customViewInput.CharLimit = 256
	customViewInput.SetWidth(54)
	titleInput := textinput.New()
	titleInput.Prompt = "Title: "
	titleInput.Placeholder = "Issue title"
	titleInput.CharLimit = 255
	titleInput.SetWidth(64)
	bodyArea := textarea.New()
	bodyArea.Prompt = ""
	bodyArea.Placeholder = "Description"
	bodyArea.CharLimit = 6000
	bodyArea.SetWidth(64)
	bodyArea.SetHeight(8)
	agentPromptArea := textarea.New()
	agentPromptArea.Prompt = ""
	agentPromptArea.Placeholder = "Ask the agent what to do with the selected issue"
	agentPromptArea.CharLimit = 12000
	agentPromptArea.SetWidth(72)
	agentPromptArea.SetHeight(9)
	agentWorkspace := textinput.New()
	agentWorkspace.Prompt = "Workspace: "
	agentWorkspace.Placeholder = "Blank uses current directory"
	agentWorkspace.CharLimit = 512
	agentWorkspace.SetWidth(72)
	promptTplName := textinput.New()
	promptTplName.Prompt = "Name: "
	promptTplName.Placeholder = "Template name"
	promptTplName.CharLimit = 120
	promptTplName.SetWidth(64)
	promptTplBody := textarea.New()
	promptTplBody.Prompt = ""
	promptTplBody.Placeholder = "Prompt text"
	promptTplBody.CharLimit = 12000
	promptTplBody.SetWidth(72)
	promptTplBody.SetHeight(8)
	promptTemplates := config.DefaultAgentPromptTemplates()
	if len(promptTemplateArgs) > 0 && len(promptTemplateArgs[0]) > 0 {
		promptTemplates = append([]config.AgentPromptTemplate(nil), promptTemplateArgs[0]...)
	}
	teamCache := cache.NewTeamCache(api, cfg.CacheTTL)
	calendarCache, _ := calendar.NewDefaultCache()
	now := time.Now()
	calendarWeekStart := calendar.StartOfWeek(now)
	app := CharmApp{
		api:                  api,
		cache:                teamCache,
		cfg:                  cfg,
		settingsPath:         settingsPath,
		customViewsPath:      defaultCharmCustomViewsPath(),
		updateIssueFunc:      api.UpdateIssue,
		archiveIssueFunc:     api.ArchiveIssue,
		createIssueFunc:      api.CreateIssue,
		createCommentFunc:    api.CreateComment,
		createRelationFunc:   api.CreateIssueRelation,
		deleteRelationFunc:   api.DeleteIssueRelation,
		subscribeIssueFunc:   api.SubscribeToIssue,
		unsubscribeIssueFunc: api.UnsubscribeFromIssue,
		openURLFunc:          openURL,
		copyToClipboardFunc:  copyToClipboard,
		loadLabelsFunc:       teamCache.GetIssueLabels,
		loadMilestonesFunc:   teamCache.GetProjectMilestones,
		fetchIssueByIDFunc:   api.FetchIssueByID,
		calendarListWeekFunc: defaultCalendarListWeek,
		calendarDeleteFunc:   defaultCalendarDeleteEvent,
		calendarCache:        calendarCache,
		customViews:          customViews,
		theme:                theme,
		styles:               newCharmStyles(theme),
		keys:                 defaultCharmKeyMap(),
		help:                 help.New(),
		focusedPane:          defaultCharmFocusedPane(cfg),
		searchQuery:          strings.TrimSpace(cfg.IssueSearchQuery),
		sortOverride:         sortFieldFromSettings(cfg.IssueSort),
		richFilters:          issueFiltersFromSettings(cfg.IssueFilters),
		apiKeyMode:           strings.TrimSpace(cfg.LinearAPIKey) == "",
		apiKeyInput:          apiKeyInput,
		paletteInput:         paletteInput,
		settingsInput:        settingsInput,
		customViewInput:      customViewInput,
		titleInput:           titleInput,
		bodyArea:             bodyArea,
		agentPromptArea:      agentPromptArea,
		agentWorkspace:       agentWorkspace,
		promptsPath:          defaultCharmPromptsPath(),
		promptTplName:        promptTplName,
		promptTplBody:        promptTplBody,
		agentOutput:          viewport.New(),
		agentBuffer:          NewAgentStreamBuffer(),
		agentRunner:          agents.NewRunner(),
		loadingSpinner:       spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(charmColor(theme.Accent)))),
		agentPromptTemplates: promptTemplates,
		calendarWeekStart:    calendarWeekStart,
		calendarToday:        now,
		calendarSelectedDay:  calendar.DayIndex(calendarWeekStart, now),
		expanded:             make(map[string]bool),
		expandedTeams:        make(map[string]bool),
		loadingTeams:         make(map[string]bool),
		teamChildren:         make(map[string][]*NavigationNode),
		multiSelected:        make(map[string]bool),
		myIssueMap:           make(map[string]*linearapi.Issue),
		otherIssueMap:        make(map[string]*linearapi.Issue),
		activeSection:        defaultIssuesSection(cfg.ShowMyIssues),
		issueResults:         newIssueResultCache(cfg.CacheTTL),
		issueDetails:         newIssueDetailCache(cfg.CacheTTL),
		issueDisk:            newIssueDiskCache(defaultIssueDiskCachePath(settingsPath)),
	}
	app.myTable = app.newIssueTable("My Issues")
	app.otherTable = app.newIssueTable("Other Issues")
	app.paletteList = newCommandPaletteList(app.styles)
	app.syncCommandPaletteList(true)
	app.applyWidgetStyles()
	app.details = viewport.New()
	app.details.SoftWrap = true
	app.details.FillHeight = true
	app.details.MouseWheelEnabled = true
	app.agentOutput.SoftWrap = true
	app.agentOutput.FillHeight = true
	app.agentOutput.MouseWheelEnabled = true
	if app.apiKeyMode {
		app.status = "Enter Linear API key"
	}
	app.rebuildNavigation()
	app.rebuildIssueTables("")
	return app
}

// defaultIssueDiskCachePath returns the durable issue cache path beside user settings.
func defaultIssueDiskCachePath(settingsPath string) string {
	settingsPath = strings.TrimSpace(settingsPath)
	if settingsPath != "" {
		return filepath.Join(filepath.Dir(settingsPath), "issues-cache.json")
	}
	path, err := config.ConfigFilePath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "issues-cache.json")
}

func defaultCharmCustomViewsPath() string {
	path, err := config.CustomViewsFilePath()
	if err != nil {
		return ""
	}
	return path
}

func defaultCharmPromptsPath() string {
	path, err := config.PromptTemplatesFilePath()
	if err != nil {
		return ""
	}
	return path
}

// Run starts the Bubble Tea application.
func (a CharmApp) Run() error {
	_, err := tea.NewProgram(a, tea.WithColorProfile(colorprofile.TrueColor)).Run()
	return err
}

// Init starts the initial Linear load.
func (a CharmApp) Init() tea.Cmd {
	if a.apiKeyMode {
		return tea.Batch(a.apiKeyInput.Focus(), a.loadingSpinner.Tick, autoRefreshIssuesCmd(), a.loadCalendarWeekCmd(a.calendarWeekStart, true))
	}
	return tea.Batch(a.loadInitialDataCmd(), a.loadingSpinner.Tick, autoRefreshIssuesCmd(), a.loadCalendarWeekCmd(a.calendarWeekStart, true))
}

// loadedIssuesStatus describes whether the current issue list came from Linear or cache.
func loadedIssuesStatus(count int, fromDisk bool) string {
	if fromDisk {
		return fmt.Sprintf("Loaded %d cached issues", count)
	}
	return fmt.Sprintf("Loaded %d issues", count)
}

// Update handles Bubble Tea messages and returns the next app state.
func (a CharmApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.resizeComponents()
		return a, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		a.loadingSpinner, cmd = a.loadingSpinner.Update(msg)
		return a, cmd
	case charmAutoRefreshMsg:
		return a.handleAutoRefresh()
	case charmCalendarLoadedMsg:
		if !calendar.StartOfWeek(msg.weekStart).Equal(calendar.StartOfWeek(a.calendarWeekStart)) {
			return a, nil
		}
		if msg.fromCache {
			a.calendarLoading = true
		} else {
			a.calendarLoading = false
		}
		a.calendarErr = msg.err
		if msg.err != nil {
			return a, nil
		}
		a.calendarEvents = msg.events
		a.calendarCacheTime = msg.fetchedAt
		if !msg.fromCache {
			_ = a.saveCalendarCache()
		}
		a.clampCalendarSelection()
		return a, nil
	case charmCalendarEventDeletedMsg:
		a.calendarLoading = false
		a.calendarErr = msg.err
		if msg.err != nil {
			a.calendarEvents = append(a.calendarEvents, msg.event)
			calendar.SortEvents(a.calendarEvents)
			a.clampCalendarSelection()
		}
		_ = a.saveCalendarCache()
		return a, nil
	case charmInitialLoadedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.currentUser = msg.currentUser
		a.teams = msg.teams
		a.resolvePersistedFilterLabels()
		a.rebuildNavigation()
		a.status = loadedIssuesStatus(len(msg.issues), msg.fromDisk)
		a.setIssues(msg.issues, "")
		detailsCmd := a.selectedIssueDetailsCmd()
		return a, detailsCmd
	case charmIssuesLoadedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = loadedIssuesStatus(len(msg.issues), msg.fromDisk)
		a.setIssues(msg.issues, msg.targetIssueID)
		detailsCmd := a.selectedIssueDetailsCmd()
		return a, detailsCmd
	case charmIssueDetailsLoadedMsg:
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		if a.selectedIssue != nil && a.selectedIssue.ID == msg.issue.ID {
			a.selectedIssue = &msg.issue
			a.issueDetails.Set(msg.issue)
			a.updateDetailsContent()
		}
		return a, nil
	case charmTeamMetadataLoadedMsg:
		delete(a.loadingTeams, msg.teamID)
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.teamChildren[msg.teamID] = charmTeamChildNodes(msg.teamID, msg.projects, msg.states, msg.cycles)
		a.expandedTeams[msg.teamID] = true
		a.rebuildNavigation()
		a.status = fmt.Sprintf("Loaded team metadata")
		return a, nil
	case charmAPIKeySavedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.apiKeyMode = true
			_ = a.apiKeyInput.Focus()
			a.status = msg.err.Error()
			return a, nil
		}
		a.cfg = msg.cfg
		a.searchQuery = strings.TrimSpace(msg.cfg.IssueSearchQuery)
		a.sortOverride = sortFieldFromSettings(msg.cfg.IssueSort)
		a.richFilters = issueFiltersFromSettings(msg.cfg.IssueFilters)
		a.api = linearapi.NewClient(linearapi.ClientConfig{
			Token:    msg.cfg.LinearAPIKey,
			Endpoint: msg.cfg.APIEndpoint,
			Timeout:  msg.cfg.Timeout,
		})
		a.cache = cache.NewTeamCache(a.api, msg.cfg.CacheTTL)
		a.updateIssueFunc = a.api.UpdateIssue
		a.archiveIssueFunc = a.api.ArchiveIssue
		a.createIssueFunc = a.api.CreateIssue
		a.createCommentFunc = a.api.CreateComment
		a.createRelationFunc = a.api.CreateIssueRelation
		a.deleteRelationFunc = a.api.DeleteIssueRelation
		a.subscribeIssueFunc = a.api.SubscribeToIssue
		a.unsubscribeIssueFunc = a.api.UnsubscribeFromIssue
		a.openURLFunc = openURL
		a.copyToClipboardFunc = copyToClipboard
		a.loadLabelsFunc = a.cache.GetIssueLabels
		a.loadMilestonesFunc = a.cache.GetProjectMilestones
		a.fetchIssueByIDFunc = a.api.FetchIssueByID
		a.issueResults = newIssueResultCache(msg.cfg.CacheTTL)
		a.issueDetails = newIssueDetailCache(msg.cfg.CacheTTL)
		a.issueDisk = newIssueDiskCache(defaultIssueDiskCachePath(a.settingsPath))
		a.loading = true
		a.status = "Loading Linear..."
		return a, a.loadInitialDataCmd()
	case charmSettingsPersistedMsg:
		if msg.err != nil {
			a.err = msg.err
			a.status = msg.err.Error()
		}
		return a, nil
	case charmSettingsSavedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.cfg = msg.cfg
		a.theme = ResolveTheme(msg.cfg.Theme)
		a.styles = newCharmStyles(a.theme)
		a.cache = cache.NewTeamCache(a.api, msg.cfg.CacheTTL)
		a.loadLabelsFunc = a.cache.GetIssueLabels
		a.loadMilestonesFunc = a.cache.GetProjectMilestones
		a.issueResults = newIssueResultCache(msg.cfg.CacheTTL)
		a.issueDetails = newIssueDetailCache(msg.cfg.CacheTTL)
		a.issueDisk = newIssueDiskCache(defaultIssueDiskCachePath(a.settingsPath))
		a.focusedPane = a.ensureVisibleCharmPane(a.focusedPane)
		a.rebuildNavigation()
		a.resizeComponents()
		a.applyComponentFocus()
		a.status = "Settings saved"
		a.loading = true
		return a, a.loadIssuesCmd("", false)
	case charmCustomViewsSavedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.customViews = msg.views
		a.rebuildNavigation()
		if msg.selectedViewID != "" {
			a.selectedNavigation = &NavigationNode{
				ID:           msg.selectedViewID,
				Text:         msg.selectedViewName,
				IsCustomView: true,
				CustomViewID: msg.selectedViewID,
			}
			a.selectedCustomView = a.getCharmCustomView(msg.selectedViewID)
		} else {
			a.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
			a.selectedCustomView = nil
		}
		a.focusedPane = charmPaneIssues
		a.status = msg.status
		a.loading = true
		return a, a.loadIssuesCmd("", false)
	case charmIssueUpdatedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			if msg.rollbackStatus {
				a.restoreIssueForRollback(msg.rollbackIssue)
				a.applyIssueStatus(msg.issueID, msg.rollbackStateID, msg.rollbackState)
			}
			if msg.rollbackPriority {
				a.applyIssuePriority(msg.issueID, msg.rollbackPriorityValue)
			}
			if msg.rollbackDescription {
				a.restoreIssueForRollback(msg.rollbackIssue)
			}
			if msg.rollbackDueDate {
				a.restoreIssueForRollback(msg.rollbackIssue)
			}
			if msg.rollbackIssueSnapshot {
				a.restoreIssueForRollback(msg.rollbackIssue)
			}
			a.undo = nil
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		if msg.rollbackStatus || msg.rollbackPriority || msg.rollbackDescription || msg.rollbackDueDate || msg.rollbackIssueSnapshot {
			return a, nil
		}
		a.issueResults.Clear()
		if msg.issueID != "" {
			a.issueDetails.Delete(msg.issueID)
		}
		return a, a.loadIssuesCmd(msg.issueID, false)
	case charmIssueArchivedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		a.selectedIssue = nil
		a.issueResults.Clear()
		return a, a.loadIssuesCmd("", false)
	case charmIssueCreatedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		a.applyCreatedIssue(msg.issue)
		return a, nil
	case charmCommentCreatedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		a.issueDetails.Delete(msg.issueID)
		return a, a.loadIssueDetailsCmd(msg.issueID)
	case charmIssueActionMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		if msg.reloadDetails && msg.issueID != "" {
			a.issueDetails.Delete(msg.issueID)
			return a, a.loadIssueDetailsCmd(msg.issueID)
		}
		return a, nil
	case charmIssueUndoMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.restoreIssueForRollback(msg.rollbackIssue)
			a.status = msg.err.Error()
			return a, nil
		}
		a.status = msg.status
		a.undo = nil
		return a, nil
	case charmAgentStartedMsg:
		a.status = msg.status
		a.agentOutputStatus = msg.status
		return a, a.waitAgentMsgCmd()
	case charmAgentLineMsg:
		a.appendAgentOutputLine(msg.line)
		return a, a.waitAgentMsgCmd()
	case charmAgentEventMsg:
		a.applyAgentEvent(msg.event)
		return a, a.waitAgentMsgCmd()
	case charmAgentRunFinishedMsg:
		a.agentRunning = false
		a.agentCancel = nil
		if msg.err != nil {
			a.err = msg.err
			a.status = msg.err.Error()
			a.agentOutputStatus = "Failed"
			a.appendAgentOutputLine("error: " + msg.err.Error())
			return a, nil
		}
		a.status = "Agent run completed"
		a.agentOutputStatus = "Completed"
		a.appendAgentOutputLine("Agent run completed.")
		return a, nil
	case charmAgentChannelClosedMsg:
		a.agentRunning = false
		a.agentCancel = nil
		return a, nil
	case charmPromptTemplatesSavedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.agentPromptTemplates = msg.templates
		a.agentTemplate = clamp(a.agentTemplate, 0, maxInt(0, len(a.agentPromptTemplates)-1))
		a.status = "Prompt templates saved"
		return a, nil
	case charmPickerLoadedMsg:
		if msg.background {
			if a.overlay == charmOverlayPicker && a.pickerAction == msg.action && msg.err == nil {
				a.replacePickerItems(msg.title, msg.items)
			}
			return a, nil
		}
		if a.overlay != charmOverlayPicker || !a.pickerLoading || a.pickerAction != msg.action {
			return a, nil
		}
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.pickerLoading = false
			a.pickerItems = nil
			a.status = msg.err.Error()
			return a, nil
		}
		a.openPicker(msg.title, msg.action, msg.items)
		return a, nil
	case charmMultiSelectLoadedMsg:
		a.loading = false
		a.err = msg.err
		if msg.err != nil {
			a.status = msg.err.Error()
			return a, nil
		}
		a.openMultiSelect(msg.title, msg.action, msg.items, msg.selectedIDs)
		return a, nil
	case tea.KeyPressMsg:
		if a.apiKeyMode {
			return a.handleAPIKey(msg)
		}
		if a.overlay != charmOverlayNone {
			return a.handleOverlayKey(msg)
		}
		return a.handleKey(msg)
	case tea.MouseMsg:
		if a.apiKeyMode || a.overlay != charmOverlayNone {
			return a, nil
		}
		return a.handleMouse(msg)
	}

	if a.focusedPane == charmPaneDetails {
		next, cmd := a.details.Update(msg)
		a.details = next
		return a, cmd
	}
	return a, nil
}

// View renders the full Bubble Tea screen.
func (a CharmApp) View() tea.View {
	content := a.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (a CharmApp) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.searchMode {
		return a.handleSearchKey(msg)
	}

	switch {
	case key.Matches(msg, a.keys.quit):
		return a, tea.Quit
	case key.Matches(msg, a.keys.search):
		a.searchMode = true
		a.status = "Search"
		return a, nil
	case key.Matches(msg, a.keys.palette):
		a.openCommandPalette()
		return a, a.paletteInput.Focus()
	case a.focusedPane == charmPaneCalendar:
		return a.handleCalendarKey(msg)
	case key.Matches(msg, a.keys.refresh):
		a.status = "Refreshing issues..."
		a.loading = true
		a.issueResults.Clear()
		return a, a.loadIssuesCmd("", false)
	case key.Matches(msg, a.keys.create):
		return a.runCharmCommand("create_issue")
	case key.Matches(msg, a.keys.addComment):
		return a.runCharmCommand("add_comment")
	case key.Matches(msg, a.keys.status):
		return a.runCharmCommand("change_status")
	case key.Matches(msg, a.keys.priority):
		return a.runCharmCommand("change_priority")
	case key.Matches(msg, a.keys.dueToday):
		return a.runCharmCommand("due_today")
	case key.Matches(msg, a.keys.copyURL):
		return a.runCharmCommand("copy_url")
	case key.Matches(msg, a.keys.undo):
		return a.runUndoLastAction()
	case key.Matches(msg, a.keys.nextPane):
		a.focusNextPane()
		return a, nil
	case key.Matches(msg, a.keys.prevPane):
		a.focusPrevPane()
		return a, nil
	}

	switch a.focusedPane {
	case charmPaneNav:
		return a.handleNavigationKey(msg)
	case charmPaneIssues:
		return a.handleIssuesKey(msg)
	case charmPaneDetails:
		next, cmd := a.details.Update(msg)
		a.details = next
		return a, cmd
	}
	return a, nil
}

// handleCalendarKey gives the embedded Google Calendar pane the same day/event keys as gc.
func (a CharmApp) handleCalendarKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.nextPane) && msg.String() == "tab" {
		a.focusNextPane()
		return a, nil
	}
	if key.Matches(msg, a.keys.prevPane) && msg.String() == "shift+tab" {
		a.focusPrevPane()
		return a, nil
	}
	switch msg.String() {
	case "esc":
		if a.calendarDetails {
			a.calendarDetails = false
		}
		return a, nil
	case "left", "h":
		oldWeek := a.calendarWeekStart
		a.moveCalendarDay(-1)
		return a, a.loadCalendarIfWeekChanged(oldWeek)
	case "right", "l":
		oldWeek := a.calendarWeekStart
		a.moveCalendarDay(1)
		return a, a.loadCalendarIfWeekChanged(oldWeek)
	case "up", "k":
		a.moveCalendarEvent(-1)
		return a, nil
	case "down", "j":
		a.moveCalendarEvent(1)
		return a, nil
	case "t":
		a.calendarToday = time.Now()
		a.calendarWeekStart = calendar.StartOfWeek(a.calendarToday)
		a.calendarSelectedDay = calendar.DayIndex(a.calendarWeekStart, a.calendarToday)
		a.calendarSelectedIdx = 0
		a.calendarDetails = false
		a.calendarLoading = true
		a.calendarErr = nil
		return a, a.loadCalendarWeekCmd(a.calendarWeekStart, true)
	case "r":
		a.calendarLoading = true
		a.calendarErr = nil
		return a, a.loadCalendarWeekCmd(a.calendarWeekStart, false)
	case "enter":
		if _, ok := a.selectedCalendarEvent(); ok {
			a.calendarDetails = !a.calendarDetails
		}
		return a, nil
	case "delete", "backspace":
		event, ok := a.selectedCalendarEvent()
		if !ok {
			return a, nil
		}
		a.removeCalendarEvent(event.ID, event.CalendarID)
		a.calendarLoading = true
		a.calendarErr = nil
		return a, a.deleteCalendarEventCmd(event)
	}
	return a, nil
}

// moveCalendarDay shifts the selected calendar day, wrapping across weeks like gc.
func (a *CharmApp) moveCalendarDay(delta int) {
	next := a.calendarSelectedDay + delta
	if next < 0 {
		a.calendarWeekStart = a.calendarWeekStart.AddDate(0, 0, -7)
		a.calendarSelectedDay = 6
		a.calendarSelectedIdx = 0
		a.calendarDetails = false
		return
	}
	if next > 6 {
		a.calendarWeekStart = a.calendarWeekStart.AddDate(0, 0, 7)
		a.calendarSelectedDay = 0
		a.calendarSelectedIdx = 0
		a.calendarDetails = false
		return
	}
	a.calendarSelectedDay = next
	a.calendarSelectedIdx = 0
	a.calendarDetails = false
	a.clampCalendarSelection()
}

// loadCalendarIfWeekChanged refreshes calendar data when day navigation crosses a week boundary.
func (a *CharmApp) loadCalendarIfWeekChanged(oldWeek time.Time) tea.Cmd {
	if calendar.StartOfWeek(oldWeek).Equal(calendar.StartOfWeek(a.calendarWeekStart)) {
		return nil
	}
	a.calendarLoading = true
	a.calendarErr = nil
	return a.loadCalendarWeekCmd(a.calendarWeekStart, true)
}

// moveCalendarEvent changes the selected event within the current day.
func (a *CharmApp) moveCalendarEvent(delta int) {
	events := a.calendarEventsForSelectedDay()
	if len(events) == 0 {
		a.calendarSelectedIdx = 0
		return
	}
	a.calendarSelectedIdx = clamp(a.calendarSelectedIdx+delta, 0, len(events)-1)
}

// clampCalendarSelection keeps the selected event index valid after loads and deletes.
func (a *CharmApp) clampCalendarSelection() {
	a.calendarSelectedDay = clamp(a.calendarSelectedDay, 0, 6)
	events := a.calendarEventsForSelectedDay()
	if len(events) == 0 {
		a.calendarSelectedIdx = 0
		a.calendarDetails = false
		return
	}
	a.calendarSelectedIdx = clamp(a.calendarSelectedIdx, 0, len(events)-1)
}

// calendarSelectedDate returns the date shown in the embedded calendar pane.
func (a CharmApp) calendarSelectedDate() time.Time {
	return a.calendarWeekStart.AddDate(0, 0, clamp(a.calendarSelectedDay, 0, 6))
}

// calendarEventsForSelectedDay returns events for the selected calendar day.
func (a CharmApp) calendarEventsForSelectedDay() []calendar.Event {
	date := a.calendarSelectedDate()
	result := make([]calendar.Event, 0)
	for _, event := range a.calendarEvents {
		if event.OccursOnDay(date) {
			result = append(result, event)
		}
	}
	calendar.SortEvents(result)
	return result
}

// selectedCalendarEvent returns the current event in the embedded calendar pane.
func (a CharmApp) selectedCalendarEvent() (calendar.Event, bool) {
	events := a.calendarEventsForSelectedDay()
	if len(events) == 0 || a.calendarSelectedIdx >= len(events) {
		return calendar.Event{}, false
	}
	return events[a.calendarSelectedIdx], true
}

// removeCalendarEvent removes one event locally before gws confirms deletion.
func (a *CharmApp) removeCalendarEvent(eventID string, calendarID string) {
	next := a.calendarEvents[:0]
	for _, event := range a.calendarEvents {
		if event.ID == eventID && event.CalendarID == calendarID {
			continue
		}
		next = append(next, event)
	}
	a.calendarEvents = next
	a.clampCalendarSelection()
}

// handleAutoRefresh starts a quiet hourly data refresh when the UI is idle enough.
func (a CharmApp) handleAutoRefresh() (tea.Model, tea.Cmd) {
	nextTick := autoRefreshIssuesCmd()
	if a.apiKeyMode || a.loading || a.searchMode || a.overlay != charmOverlayNone {
		return a, nextTick
	}
	a.loading = true
	a.status = "Refreshing issues..."
	a.issueResults.Clear()
	a.calendarLoading = true
	a.calendarErr = nil
	return a, tea.Batch(a.loadIssuesCmd("", false), a.loadCalendarWeekCmd(a.calendarWeekStart, false), nextTick)
}

func (a CharmApp) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseReleaseMsg:
		a.draggingNavigation = false
		a.draggingDetails = false
		return a, nil
	case tea.MouseMotionMsg:
		if a.draggingNavigation {
			a.setNavigationWidthFromMouse(mouse.X)
			return a, nil
		}
		if a.draggingDetails {
			a.setDetailsWidthFromMouse(mouse.X)
			return a, nil
		}
		return a, nil
	case tea.MouseWheelMsg:
		return a.handleMouseWheel(mouse)
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return a, nil
		}
		layout := a.workspaceLayout()
		if layout.navDividerX >= 0 && mouse.X == layout.navDividerX && mouse.Y >= layout.bodyTop && mouse.Y < layout.bodyTop+layout.bodyHeight {
			a.draggingNavigation = true
			a.setNavigationWidthFromMouse(mouse.X)
			return a, nil
		}
		if layout.detailsDividerX >= 0 && mouse.X == layout.detailsDividerX && mouse.Y >= layout.bodyTop && mouse.Y < layout.bodyTop+layout.bodyHeight {
			a.draggingDetails = true
			a.setDetailsWidthFromMouse(mouse.X)
			return a, nil
		}
		return a.handleMouseClick(mouse, layout)
	default:
		return a, nil
	}
}

func (a CharmApp) handleMouseWheel(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	layout := a.workspaceLayout()
	if layout.inDetails(mouse.X, mouse.Y) {
		a.focusedPane = charmPaneDetails
		a.scrollDetailsByWheel(mouse.Button)
		a.applyComponentFocus()
		return a, nil
	}
	if layout.inIssues(mouse.X, mouse.Y) {
		a.focusedPane = charmPaneIssues
		if !a.scrollIssuesByWheel(mouse.Button) {
			a.applyComponentFocus()
			return a, nil
		}
		a.selectIssueFromActiveTable()
		a.applyComponentFocus()
		return a, a.selectedIssueDetailsCmd()
	}
	if layout.inCalendar(mouse.X, mouse.Y) {
		a.focusedPane = charmPaneCalendar
		if mouse.Button == tea.MouseWheelUp {
			a.moveCalendarEvent(-1)
		} else if mouse.Button == tea.MouseWheelDown {
			a.moveCalendarEvent(1)
		}
		a.applyComponentFocus()
		return a, nil
	}
	if layout.inNavigation(mouse.X, mouse.Y) {
		a.focusedPane = charmPaneNav
		if mouse.Button == tea.MouseWheelUp && a.navigationCursor > 0 {
			a.navigationCursor--
		} else if mouse.Button == tea.MouseWheelDown && a.navigationCursor < len(a.navigation)-1 {
			a.navigationCursor++
		}
		a.applyComponentFocus()
		return a, nil
	}
	return a, nil
}

// scrollDetailsByWheel scrolls the details viewport while dropping stale trackpad momentum at boundaries.
func (a *CharmApp) scrollDetailsByWheel(button tea.MouseButton) bool {
	if button != tea.MouseWheelUp && button != tea.MouseWheelDown {
		return false
	}
	if a.shouldSuppressWheel(charmPaneDetails, button) {
		return false
	}
	if button == tea.MouseWheelUp && a.details.AtTop() {
		a.suppressWheel(charmPaneDetails, button)
		return false
	}
	if button == tea.MouseWheelDown && a.details.AtBottom() {
		a.suppressWheel(charmPaneDetails, button)
		return false
	}
	before := a.details.YOffset()
	if button == tea.MouseWheelUp {
		a.details.ScrollUp(wheelScrollStep)
	} else {
		a.details.ScrollDown(wheelScrollStep)
	}
	return a.details.YOffset() != before
}

// scrollIssuesByWheel moves the active issue table and skips boundary wheel bursts.
func (a *CharmApp) scrollIssuesByWheel(button tea.MouseButton) bool {
	if button != tea.MouseWheelUp && button != tea.MouseWheelDown {
		return false
	}
	if a.shouldSuppressWheel(charmPaneIssues, button) {
		return false
	}
	rows := a.activeIssueRowCount()
	if rows == 0 {
		return false
	}
	table := a.activeIssueTable()
	if button == tea.MouseWheelUp && table.Cursor() <= 0 {
		a.suppressWheel(charmPaneIssues, button)
		return false
	}
	if button == tea.MouseWheelDown && table.Cursor() >= rows-1 {
		a.suppressWheel(charmPaneIssues, button)
		return false
	}
	before := table.Cursor()
	if button == tea.MouseWheelUp {
		table.MoveUp(wheelScrollStep)
	} else {
		table.MoveDown(wheelScrollStep)
	}
	return table.Cursor() != before
}

// shouldSuppressWheel reports whether a recent boundary hit should absorb more same-direction wheel events.
func (a CharmApp) shouldSuppressWheel(pane charmPane, button tea.MouseButton) bool {
	return a.wheelSuppressPane == pane &&
		a.wheelSuppressBtn == button &&
		time.Now().Before(a.wheelSuppressTill)
}

// suppressWheel temporarily absorbs same-direction wheel momentum after a top or bottom boundary is reached.
func (a *CharmApp) suppressWheel(pane charmPane, button tea.MouseButton) {
	a.wheelSuppressPane = pane
	a.wheelSuppressBtn = button
	a.wheelSuppressTill = time.Now().Add(wheelBoundarySuppressWindow)
}

func (a CharmApp) handleMouseClick(mouse tea.Mouse, layout charmWorkspaceLayout) (tea.Model, tea.Cmd) {
	switch {
	case layout.inNavigation(mouse.X, mouse.Y):
		row := mouse.Y - layout.navY - 1
		if row >= 0 && row < len(a.navigation) {
			a.focusedPane = charmPaneNav
			a.navigationCursor = row
			a.applyComponentFocus()
			return a.activateNavigationSelection()
		}
	case layout.inCalendar(mouse.X, mouse.Y):
		a.focusedPane = charmPaneCalendar
		if index, ok := a.calendarEventIndexAtMouse(mouse.Y, layout); ok {
			a.calendarSelectedIdx = index
			a.calendarDetails = false
		}
		a.applyComponentFocus()
		return a, nil
	case layout.inIssues(mouse.X, mouse.Y):
		if section, index, ok := a.issueRowAtMouse(mouse.Y, layout); ok {
			a.focusedPane = charmPaneIssues
			a.activeSection = section
			if section == IssuesSectionMy {
				a.myTable.SetCursor(index)
			} else {
				a.otherTable.SetCursor(index)
			}
			a.selectIssueFromActiveTable()
			a.applyComponentFocus()
			return a, a.selectedIssueDetailsCmd()
		}
	case layout.inDetails(mouse.X, mouse.Y):
		a.focusedPane = charmPaneDetails
		a.applyComponentFocus()
		return a, nil
	}
	return a, nil
}

// calendarEventIndexAtMouse maps a click inside the calendar pane to a visible event row.
func (a CharmApp) calendarEventIndexAtMouse(y int, layout charmWorkspaceLayout) (int, bool) {
	row := y - layout.calendarY - 1
	if row < 0 || a.calendarDetails {
		return 0, false
	}
	row -= 2
	if a.calendarLoading {
		row--
	}
	if a.calendarErr != nil {
		row--
	}
	if row < 0 {
		return 0, false
	}
	for i, event := range a.calendarEventsForSelectedDay() {
		rowHeight := lipgloss.Height(a.renderCalendarEventRow(event, i == a.calendarSelectedIdx, layout.navWidth))
		if row < rowHeight {
			return i, true
		}
		row -= rowHeight
	}
	return 0, false
}

func (a *CharmApp) setDetailsWidthFromMouse(x int) {
	layout := a.workspaceLayout()
	maxWidth := maxInt(32, layout.maxDetailsWidth)
	a.detailsWidth = clamp(a.width-x-1, 32, maxWidth)
	a.resizeComponents()
}

// setNavigationWidthFromMouse converts a divider x-coordinate into a clamped navigation width.
func (a *CharmApp) setNavigationWidthFromMouse(x int) {
	minWidth, maxWidth := a.navigationWidthBounds()
	a.navWidth = clamp(x, minWidth, maxWidth)
	a.resizeComponents()
}

func (a *CharmApp) activeIssueTable() *table.Model {
	if a.activeSection == IssuesSectionMy {
		return &a.myTable
	}
	return &a.otherTable
}

// activeIssueRowCount returns the number of rows in the currently focused issue section.
func (a CharmApp) activeIssueRowCount() int {
	if a.activeSection == IssuesSectionMy {
		return len(a.myRows)
	}
	return len(a.otherRows)
}

func (a CharmApp) issueRowAtMouse(y int, layout charmWorkspaceLayout) (IssuesSection, int, bool) {
	offset := layout.bodyTop + 1
	if a.loading {
		offset++
	}
	if a.cfg.ShowMyIssues && a.cfg.ShowOtherIssues {
		offset += lipgloss.Height(a.renderIssueTabs(layout.issuesWidth))
	}
	if a.cfg.ShowMyIssues {
		start, end := issueTableVisibleRange(len(a.myRows), a.myTable.Cursor(), a.myTable.Height())
		rowsStart := offset + 2
		if y >= rowsStart && y < rowsStart+(end-start) {
			return IssuesSectionMy, start + clamp(y-rowsStart, 0, maxInt(0, end-start-1)), true
		}
		offset += 1 + lipgloss.Height(a.renderIssueTable(a.myRows, a.myIssueMap, a.myTable))
	}
	if a.cfg.ShowOtherIssues {
		start, end := issueTableVisibleRange(len(a.otherRows), a.otherTable.Cursor(), a.otherTable.Height())
		rowsStart := offset + 2
		if y >= rowsStart && y < rowsStart+(end-start) {
			return IssuesSectionOther, start + clamp(y-rowsStart, 0, maxInt(0, end-start-1)), true
		}
	}
	return IssuesSectionMy, 0, false
}

func (a CharmApp) selectedIssueDetailsCmd() tea.Cmd {
	if a.selectedIssue == nil || strings.TrimSpace(a.selectedIssue.ID) == "" {
		return nil
	}
	return a.loadIssueDetailsCmd(a.selectedIssue.ID)
}

func (a CharmApp) detailsCmdIfSelectionChanged(previousIssueID string) tea.Cmd {
	if a.selectedIssue == nil || a.selectedIssue.ID == previousIssueID {
		return nil
	}
	return a.selectedIssueDetailsCmd()
}

func (a CharmApp) handleOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch a.overlay {
	case charmOverlayPalette:
		return a.handlePaletteKey(msg)
	case charmOverlayPicker:
		return a.handlePickerKey(msg)
	case charmOverlayMultiSelect:
		return a.handleMultiSelectKey(msg)
	case charmOverlaySettings:
		return a.handleSettingsKey(msg)
	case charmOverlayCustomView:
		return a.handleCustomViewKey(msg)
	case charmOverlayConfirmDeleteView:
		return a.handleConfirmDeleteViewKey(msg)
	case charmOverlayConfirmArchive:
		return a.handleConfirmArchiveKey(msg)
	case charmOverlayConfirmRemoveParent:
		return a.handleConfirmRemoveParentKey(msg)
	case charmOverlayIssueForm:
		return a.handleIssueFormKey(msg)
	case charmOverlayAgentPrompt:
		return a.handleAgentPromptKey(msg)
	case charmOverlayAgentOutput:
		return a.handleAgentOutputKey(msg)
	case charmOverlayPromptTemplates:
		return a.handlePromptTemplatesKey(msg)
	default:
		return a, nil
	}
}

func (a CharmApp) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "up", "k":
		a.paletteList.CursorUp()
		return a, nil
	case "down", "j":
		a.paletteList.CursorDown()
		return a, nil
	case "enter":
		selected, ok := a.paletteList.SelectedItem().(charmCommandItem)
		if !ok {
			a.status = "No command selected"
			return a, nil
		}
		a.closeOverlay()
		return a.runCharmCommand(selected.command.ID)
	}
	next, cmd := a.paletteInput.Update(msg)
	a.paletteInput = next
	a.syncCommandPaletteList(true)
	return a, cmd
}

func (a CharmApp) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if a.pickerAction == charmPickerCreateAssignee {
			a.returnToIssueFormFromPicker()
			return a, nil
		}
		a.closeOverlay()
		return a, nil
	case "up", "k":
		if a.pickerCursor > 0 {
			a.pickerCursor--
		}
		return a, nil
	case "down", "j":
		if a.pickerCursor < len(a.pickerItems)-1 {
			a.pickerCursor++
		}
		return a, nil
	case "enter":
		if a.pickerLoading {
			return a, nil
		}
		if len(a.pickerItems) == 0 {
			a.closeOverlay()
			return a, nil
		}
		item := a.pickerItems[clamp(a.pickerCursor, 0, len(a.pickerItems)-1)]
		action := a.pickerAction
		if action == charmPickerCreateAssignee {
			return a.applyPickerSelection(action, item)
		}
		a.closeOverlay()
		return a.applyPickerSelection(action, item)
	}
	return a, nil
}

// handleMultiSelectKey updates checkbox state or saves selected multi-select values.
func (a CharmApp) handleMultiSelectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "up", "k":
		if a.multiCursor > 0 {
			a.multiCursor--
		}
		return a, nil
	case "down", "j":
		if a.multiCursor < len(a.multiItems)-1 {
			a.multiCursor++
		}
		return a, nil
	case " ", "space", "t":
		if len(a.multiItems) == 0 {
			return a, nil
		}
		item := a.multiItems[clamp(a.multiCursor, 0, len(a.multiItems)-1)]
		if a.multiSelected[item.ID] {
			delete(a.multiSelected, item.ID)
		} else {
			a.multiSelected[item.ID] = true
		}
		return a, nil
	case "enter":
		action := a.multiAction
		ids := a.selectedMultiSelectIDs()
		a.closeOverlay()
		return a.applyMultiSelectSelection(action, ids)
	}
	return a, nil
}

// handleSettingsKey updates the focused settings row or saves the form.
func (a CharmApp) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "ctrl+s":
		a.commitSettingsInputValue()
		settings, err := a.settingsFromCharmFields()
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.closeOverlay()
		a.loading = true
		a.status = "Saving settings..."
		return a, a.saveSettingsCmd(settings)
	case "tab", "down", "j":
		a.moveSettingsCursor(1)
		return a, nil
	case "shift+tab", "up", "k":
		a.moveSettingsCursor(-1)
		return a, nil
	case "left", "h":
		a.cycleSettingsOption(-1)
		return a, nil
	case "right", "l", " ", "space":
		a.cycleSettingsOption(1)
		return a, nil
	}
	if a.currentSettingsField().Options == nil {
		next, cmd := a.settingsInput.Update(msg)
		a.settingsInput = next
		a.commitSettingsInputValue()
		return a, cmd
	}
	return a, nil
}

// handleCustomViewKey updates the custom-view editor or saves the form.
func (a CharmApp) handleCustomViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "ctrl+s":
		a.commitCustomViewInputValue()
		view, err := a.customViewFromCharmFields()
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.closeOverlay()
		a.loading = true
		a.status = "Saving custom view..."
		return a, a.saveCustomViewCmd(view)
	case "tab", "down", "j":
		a.moveCustomViewCursor(1)
		return a, nil
	case "shift+tab", "up", "k":
		a.moveCustomViewCursor(-1)
		return a, nil
	case "left", "h":
		a.cycleCustomViewOption(-1)
		return a, nil
	case "right", "l", " ", "space":
		a.cycleCustomViewOption(1)
		return a, nil
	}
	if a.currentCustomViewField().Options == nil {
		next, cmd := a.customViewInput.Update(msg)
		a.customViewInput = next
		a.commitCustomViewInputValue()
		return a, cmd
	}
	return a, nil
}

func (a CharmApp) handleConfirmDeleteViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		a.closeOverlay()
		return a, nil
	case "enter", "y":
		view := a.selectedCustomView
		a.closeOverlay()
		if view == nil {
			a.status = "No custom view selected"
			return a, nil
		}
		a.loading = true
		a.status = "Deleting custom view..."
		return a, a.deleteCustomViewCmd(view.ID)
	}
	return a, nil
}

func (a CharmApp) handleConfirmArchiveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		a.closeOverlay()
		return a, nil
	case "enter", "y":
		issue := a.selectedIssue
		a.closeOverlay()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.loading = true
		a.status = fmt.Sprintf("Archiving %s...", issue.Identifier)
		return a, a.archiveIssueCmd(*issue)
	}
	return a, nil
}

// handleConfirmRemoveParentKey confirms clearing the current issue parent.
func (a CharmApp) handleConfirmRemoveParentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		a.closeOverlay()
		return a, nil
	case "enter", "y":
		issue := a.selectedIssue
		a.closeOverlay()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		empty := ""
		return a.runIssueUpdate(
			*issue,
			linearapi.UpdateIssueInput{ID: issue.ID, ParentID: &empty},
			fmt.Sprintf("Removed parent from %s", issue.Identifier),
		)
	}
	return a, nil
}

func (a CharmApp) handleIssueFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "tab":
		a.toggleFormFocus()
		return a, nil
	case "ctrl+a":
		if a.formMode == charmFormCreateIssue {
			return a.openCreateAssigneePicker()
		}
	case "ctrl+s":
		return a.submitIssueForm()
	case "shift+enter":
		if a.formMode == charmFormAddComment {
			a.bodyArea.InsertString("\n")
			return a, nil
		}
	case "enter":
		if a.formMode == charmFormAddComment {
			return a.submitIssueForm()
		}
		if a.formMode == charmFormCreateIssue && a.formFocus == 2 {
			return a.openCreateAssigneePicker()
		}
		if a.formMode == charmFormEditTitle ||
			a.formMode == charmFormSetDueDate ||
			a.formMode == charmFormSetEstimate ||
			a.formMode == charmFormIssueRelationTarget ||
			a.formMode == charmFormFilterText ||
			a.formMode == charmFormFilterDueDate ||
			a.formMode == charmFormFilterEstimate ||
			(a.formMode == charmFormCreateIssue && a.formFocus == 0) {
			return a.submitIssueForm()
		}
	}
	if a.formFocus == 0 {
		next, cmd := a.titleInput.Update(msg)
		a.titleInput = next
		return a, cmd
	}
	if a.formMode == charmFormCreateIssue && a.formFocus == 2 {
		return a, nil
	}
	next, cmd := a.bodyArea.Update(msg)
	a.bodyArea = next
	return a, cmd
}

func (a CharmApp) handleAPIKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		a.apiKeyInput.SetValue("")
		a.err = nil
		a.status = "API key required"
		return a, nil
	case "enter":
		key := strings.TrimSpace(a.apiKeyInput.Value())
		if key == "" {
			a.err = fmt.Errorf("Linear API key cannot be empty")
			a.status = a.err.Error()
			return a, nil
		}
		a.apiKeyInput.Blur()
		a.apiKeyMode = false
		a.loading = true
		a.err = nil
		a.status = "Saving API key..."
		return a, a.saveAPIKeyCmd(key)
	}
	next, cmd := a.apiKeyInput.Update(msg)
	a.apiKeyInput = next
	return a, cmd
}

func (a CharmApp) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.searchMode = false
		a.status = ""
		return a, nil
	case "enter":
		a.searchMode = false
		a.loading = true
		a.status = "Searching..."
		return a, tea.Batch(a.persistSearchQueryCmd(), a.loadIssuesCmd("", true))
	case "backspace", "ctrl+h":
		if len(a.searchQuery) > 0 {
			a.searchQuery = a.searchQuery[:len(a.searchQuery)-1]
		}
		return a, nil
	default:
		if s := msg.String(); len(s) == 1 {
			a.searchQuery += s
		}
		return a, nil
	}
}

func (a CharmApp) handleNavigationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.navigationCursor > 0 {
			a.navigationCursor--
		}
		return a, nil
	case "down", "j":
		if a.navigationCursor < len(a.navigation)-1 {
			a.navigationCursor++
		}
		return a, nil
	case "enter":
		if len(a.navigation) == 0 {
			return a, nil
		}
		return a.activateNavigationSelection()
	}
	return a, nil
}

func (a CharmApp) activateNavigationSelection() (tea.Model, tea.Cmd) {
	if len(a.navigation) == 0 {
		return a, nil
	}
	a.selectedNavigation = a.navigation[clamp(a.navigationCursor, 0, len(a.navigation)-1)]
	a.selectedCustomView = nil
	if a.selectedNavigation.IsCustomView {
		a.selectedCustomView = a.getCharmCustomView(a.selectedNavigation.CustomViewID)
	}
	a.focusedPane = charmPaneIssues
	a.loading = true
	a.status = "Loading issues..."
	cmds := []tea.Cmd{a.loadIssuesCmd("", true)}
	if a.selectedNavigation.IsTeam {
		teamID := a.selectedNavigation.TeamID
		if _, ok := a.teamChildren[teamID]; ok {
			a.expandedTeams[teamID] = !a.expandedTeams[teamID]
			a.rebuildNavigation()
		} else if !a.loadingTeams[teamID] {
			a.expandedTeams[teamID] = true
			a.loadingTeams[teamID] = true
			a.rebuildNavigation()
			cmds = append(cmds, a.loadTeamMetadataCmd(teamID))
		}
	}
	return a, tea.Batch(cmds...)
}

func (a CharmApp) handleIssuesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		a.focusNextPane()
		return a, nil
	case "shift+tab":
		a.focusPrevPane()
		return a, nil
	case "enter":
		issue := a.currentIssue()
		if issue == nil {
			return a, nil
		}
		if len(issue.Children) > 0 {
			ToggleExpanded(a.expanded, issue.ID)
			a.rebuildIssueTables(issue.ID)
			return a, nil
		}
		a.selectedIssue = issue
		a.focusedPane = charmPaneDetails
		a.updateDetailsContent()
		return a, a.loadIssueDetailsCmd(issue.ID)
	}

	beforeID := ""
	if a.selectedIssue != nil {
		beforeID = a.selectedIssue.ID
	}
	if a.activeSection == IssuesSectionMy {
		next, cmd := a.myTable.Update(msg)
		a.myTable = next
		a.selectIssueFromActiveTable()
		return a, tea.Batch(cmd, a.detailsCmdIfSelectionChanged(beforeID))
	}
	next, cmd := a.otherTable.Update(msg)
	a.otherTable = next
	a.selectIssueFromActiveTable()
	return a, tea.Batch(cmd, a.detailsCmdIfSelectionChanged(beforeID))
}

func (a *CharmApp) focusNextPane() {
	panes := a.visiblePanes()
	if len(panes) == 0 {
		return
	}
	a.focusedPane = panes[(a.focusedPaneIndex(panes)+1)%len(panes)]
	a.applyComponentFocus()
}

func (a *CharmApp) focusPrevPane() {
	panes := a.visiblePanes()
	if len(panes) == 0 {
		return
	}
	a.focusedPane = panes[(a.focusedPaneIndex(panes)+len(panes)-1)%len(panes)]
	a.applyComponentFocus()
}

func (a *CharmApp) applyComponentFocus() {
	if a.focusedPane == charmPaneIssues && a.activeSection == IssuesSectionMy {
		a.myTable.Focus()
		a.otherTable.Blur()
		return
	}
	if a.focusedPane == charmPaneIssues {
		a.myTable.Blur()
		a.otherTable.Focus()
		return
	}
	a.myTable.Blur()
	a.otherTable.Blur()
}

func defaultCharmFocusedPane(cfg config.Config) charmPane {
	if cfg.ShowMyIssues || cfg.ShowOtherIssues {
		return charmPaneIssues
	}
	if cfg.ShowNavigation {
		return charmPaneNav
	}
	return charmPaneDetails
}

func (a CharmApp) visiblePanes() []charmPane {
	panes := []charmPane{}
	if a.cfg.ShowNavigation {
		panes = append(panes, charmPaneNav)
		panes = append(panes, charmPaneCalendar)
	}
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		panes = append(panes, charmPaneIssues)
	}
	panes = append(panes, charmPaneDetails)
	return panes
}

func (a CharmApp) focusedPaneIndex(panes []charmPane) int {
	for i, pane := range panes {
		if pane == a.focusedPane {
			return i
		}
	}
	return 0
}

func (a CharmApp) ensureVisibleCharmPane(pane charmPane) charmPane {
	for _, visible := range a.visiblePanes() {
		if visible == pane {
			return pane
		}
	}
	return defaultCharmFocusedPane(a.cfg)
}

func (a *CharmApp) toggleIssuesSection() {
	if a.activeSection == IssuesSectionMy && len(a.otherRows) > 0 {
		a.activeSection = IssuesSectionOther
	} else if len(a.myRows) > 0 {
		a.activeSection = IssuesSectionMy
	} else {
		a.activeSection = IssuesSectionOther
	}
	a.applyComponentFocus()
	a.selectIssueFromActiveTable()
}

type charmInitialLoadedMsg struct {
	currentUser *linearapi.User
	teams       []linearapi.Team
	issues      []linearapi.Issue
	fromDisk    bool
	err         error
}

type charmIssuesLoadedMsg struct {
	issues        []linearapi.Issue
	targetIssueID string
	fromDisk      bool
	err           error
}

type charmAutoRefreshMsg struct{}

type charmCalendarLoadedMsg struct {
	weekStart time.Time
	events    []calendar.Event
	fetchedAt time.Time
	fromCache bool
	err       error
}

type charmCalendarEventDeletedMsg struct {
	event calendar.Event
	err   error
}

type charmIssueDetailsLoadedMsg struct {
	issue linearapi.Issue
	err   error
}

type charmTeamMetadataLoadedMsg struct {
	teamID   string
	projects []linearapi.Project
	states   []linearapi.WorkflowState
	cycles   []linearapi.Cycle
	err      error
}

type charmAPIKeySavedMsg struct {
	cfg config.Config
	err error
}

type charmSettingsPersistedMsg struct {
	err error
}

type charmSettingsSavedMsg struct {
	cfg config.Config
	err error
}

type charmCustomViewsSavedMsg struct {
	views            []config.CustomView
	selectedViewID   string
	selectedViewName string
	status           string
	err              error
}

type charmIssueUpdatedMsg struct {
	issueID               string
	status                string
	rollbackStatus        bool
	rollbackStateID       string
	rollbackState         string
	rollbackIssue         linearapi.Issue
	rollbackPriority      bool
	rollbackPriorityValue int
	rollbackDescription   bool
	rollbackDueDate       bool
	rollbackIssueSnapshot bool
	err                   error
}

type charmIssueUndoMsg struct {
	status        string
	rollbackIssue linearapi.Issue
	err           error
}

type charmIssueArchivedMsg struct {
	status string
	err    error
}

type charmIssueCreatedMsg struct {
	issue   linearapi.Issue
	issueID string
	status  string
	err     error
}

type charmCommentCreatedMsg struct {
	issueID string
	status  string
	err     error
}

// charmIssueActionMsg reports a non-standard issue action such as relations or attachments.
type charmIssueActionMsg struct {
	issueID       string
	status        string
	reloadDetails bool
	err           error
}

// charmAgentStartedMsg indicates the agent goroutine is ready to stream output.
type charmAgentStartedMsg struct {
	status string
}

// charmAgentLineMsg appends one unstructured agent output line.
type charmAgentLineMsg struct {
	line string
}

// charmAgentEventMsg appends one structured provider event.
type charmAgentEventMsg struct {
	event agents.AgentEvent
}

// charmAgentRunFinishedMsg closes out an agent run with its final error state.
type charmAgentRunFinishedMsg struct {
	err error
}

// charmAgentChannelClosedMsg handles an unexpectedly closed agent event channel.
type charmAgentChannelClosedMsg struct{}

// charmPromptTemplatesSavedMsg reports prompt template persistence results.
type charmPromptTemplatesSavedMsg struct {
	templates []config.AgentPromptTemplate
	err       error
}

type charmPickerLoadedMsg struct {
	title      string
	action     charmPickerAction
	items      []charmPickerItem
	background bool
	err        error
}

// charmMultiSelectLoadedMsg carries asynchronously loaded options for a multi-select overlay.
type charmMultiSelectLoadedMsg struct {
	title       string
	action      charmMultiSelectAction
	items       []charmMultiSelectItem
	selectedIDs []string
	err         error
}

func (a CharmApp) saveAPIKeyCmd(key string) tea.Cmd {
	return func() tea.Msg {
		settingsPath := a.settingsPath
		if settingsPath == "" {
			path, err := config.ConfigFilePath()
			if err != nil {
				return charmAPIKeySavedMsg{err: err}
			}
			settingsPath = path
		}
		settings, err := config.EnsureSettingsFile(settingsPath)
		if err != nil {
			return charmAPIKeySavedMsg{err: err}
		}
		settings.LinearAPIKey = strings.TrimSpace(key)
		if err := config.SaveSettings(settingsPath, settings); err != nil {
			return charmAPIKeySavedMsg{err: err}
		}
		cfg, err := config.ConfigFromSettings("", settings)
		if err != nil {
			return charmAPIKeySavedMsg{err: err}
		}
		return charmAPIKeySavedMsg{cfg: cfg}
	}
}

func (a CharmApp) persistSearchQueryCmd() tea.Cmd {
	query := strings.TrimSpace(a.searchQuery)
	sortField := string(a.sortOverride)
	filters := a.richFilters
	return func() tea.Msg {
		if a.settingsPath == "" {
			return charmSettingsPersistedMsg{}
		}
		settings, err := config.EnsureSettingsFile(a.settingsPath)
		if err != nil {
			return charmSettingsPersistedMsg{err: err}
		}
		settings.IssueSearchQuery = query
		settings.IssueSort = sortField
		settings.IssueFilters = issueFiltersToSettings(filters)
		if err := config.SaveSettings(a.settingsPath, settings); err != nil {
			return charmSettingsPersistedMsg{err: err}
		}
		return charmSettingsPersistedMsg{}
	}
}

// currentSettings loads persisted settings while preserving the in-memory API key.
func (a CharmApp) currentSettings() (config.Settings, error) {
	settingsPath := a.settingsPath
	if settingsPath == "" {
		path, err := config.ConfigFilePath()
		if err != nil {
			return config.Settings{}, err
		}
		settingsPath = path
	}
	settings, err := config.EnsureSettingsFile(settingsPath)
	if err != nil {
		return config.Settings{}, err
	}
	if settings.LinearAPIKey == "" && strings.TrimSpace(a.cfg.LinearAPIKey) != "" {
		settings.LinearAPIKey = strings.TrimSpace(a.cfg.LinearAPIKey)
	}
	return settings, nil
}

// saveSettingsCmd validates and saves settings to disk.
func (a CharmApp) saveSettingsCmd(settings config.Settings) tea.Cmd {
	return func() tea.Msg {
		if settings.LinearAPIKey == "" && strings.TrimSpace(a.cfg.LinearAPIKey) != "" {
			settings.LinearAPIKey = strings.TrimSpace(a.cfg.LinearAPIKey)
		}
		cfg, err := config.ConfigFromSettings("", settings)
		if err != nil {
			return charmSettingsSavedMsg{err: err}
		}
		settingsPath := a.settingsPath
		if settingsPath == "" {
			path, pathErr := config.ConfigFilePath()
			if pathErr != nil {
				return charmSettingsSavedMsg{err: pathErr}
			}
			settingsPath = path
		}
		if err := config.SaveSettings(settingsPath, settings); err != nil {
			return charmSettingsSavedMsg{err: err}
		}
		return charmSettingsSavedMsg{cfg: cfg}
	}
}

// saveCustomViewCmd upserts and persists a custom view.
func (a CharmApp) saveCustomViewCmd(view config.CustomView) tea.Cmd {
	return func() tea.Msg {
		views := append([]config.CustomView(nil), a.customViews...)
		if view.ID == "" {
			view.ID = fmt.Sprintf("view-%d", time.Now().UnixNano())
		}
		found := false
		for i := range views {
			if views[i].ID == view.ID {
				views[i] = view
				found = true
				break
			}
		}
		if !found {
			views = append(views, view)
		}
		if err := a.saveCustomViews(views); err != nil {
			return charmCustomViewsSavedMsg{err: err}
		}
		return charmCustomViewsSavedMsg{
			views:            views,
			selectedViewID:   view.ID,
			selectedViewName: view.Name,
			status:           "Saved custom view",
		}
	}
}

// deleteCustomViewCmd removes and persists a custom view by ID.
func (a CharmApp) deleteCustomViewCmd(viewID string) tea.Cmd {
	return func() tea.Msg {
		views := make([]config.CustomView, 0, len(a.customViews))
		for _, view := range a.customViews {
			if view.ID != viewID {
				views = append(views, view)
			}
		}
		if err := a.saveCustomViews(views); err != nil {
			return charmCustomViewsSavedMsg{err: err}
		}
		return charmCustomViewsSavedMsg{
			views:  views,
			status: "Deleted custom view",
		}
	}
}

func (a CharmApp) saveCustomViews(views []config.CustomView) error {
	if a.customViewsPath == "" {
		return fmt.Errorf("custom views path is not configured")
	}
	return config.SaveCustomViews(a.customViewsPath, views)
}

// savePromptTemplatesCmd persists agent prompt templates to the configured prompts file.
func (a CharmApp) savePromptTemplatesCmd(templates []config.AgentPromptTemplate) tea.Cmd {
	return func() tea.Msg {
		path := a.promptsPath
		if path == "" {
			return charmPromptTemplatesSavedMsg{err: fmt.Errorf("prompt templates path is not configured")}
		}
		if err := config.SavePromptTemplates(path, templates); err != nil {
			return charmPromptTemplatesSavedMsg{err: err}
		}
		return charmPromptTemplatesSavedMsg{templates: templates}
	}
}

func (a *CharmApp) openCommandPalette() {
	a.overlay = charmOverlayPalette
	a.paletteInput.SetValue("")
	a.syncCommandPaletteList(true)
	a.status = "Command palette"
}

func (a *CharmApp) closeOverlay() {
	if a.overlay == charmOverlayAgentOutput && a.agentRunning && a.agentCancel != nil {
		a.agentCancel()
	}
	a.overlay = charmOverlayNone
	a.paletteInput.Blur()
	a.settingsInput.Blur()
	a.customViewInput.Blur()
	a.titleInput.Blur()
	a.bodyArea.Blur()
	a.agentPromptArea.Blur()
	a.agentWorkspace.Blur()
	a.promptTplName.Blur()
	a.promptTplBody.Blur()
	a.pickerItems = nil
	a.pickerCursor = 0
	a.pickerTitle = ""
	a.pickerLoading = false
	a.multiItems = nil
	a.multiSelected = make(map[string]bool)
	a.multiCursor = 0
	a.multiTitle = ""
	a.settingsFields = nil
	a.settingsCursor = 0
	a.customViewFields = nil
	a.customViewCursor = 0
	a.customViewEditing = ""
	a.formIssueID = ""
	a.formTeamID = ""
	a.formProjectID = ""
	a.formCycleID = ""
	a.formParentID = ""
	a.formAssigneeID = ""
	a.formAssigneeName = ""
	a.formRelationLabel = ""
}

// openPromptTemplatesForm opens the Charm-native prompt template editor.
func (a *CharmApp) openPromptTemplatesForm() tea.Cmd {
	if len(a.agentPromptTemplates) == 0 {
		a.agentPromptTemplates = config.DefaultAgentPromptTemplates()
	}
	a.overlay = charmOverlayPromptTemplates
	a.promptTplCursor = clamp(a.promptTplCursor, 0, len(a.agentPromptTemplates)-1)
	a.promptTplFocus = 0
	a.syncPromptTemplateInputs()
	a.status = "Agent prompt templates"
	return nil
}

// syncPromptTemplateInputs loads the selected prompt template into edit fields.
func (a *CharmApp) syncPromptTemplateInputs() {
	if len(a.agentPromptTemplates) == 0 {
		a.promptTplName.SetValue("")
		a.promptTplBody.SetValue("")
		return
	}
	template := a.agentPromptTemplates[clamp(a.promptTplCursor, 0, len(a.agentPromptTemplates)-1)]
	a.promptTplName.SetValue(template.Name)
	a.promptTplBody.SetValue(template.Prompt)
	a.promptTplName.Blur()
	a.promptTplBody.Blur()
	if a.promptTplFocus == 1 {
		a.promptTplName.Focus()
	} else if a.promptTplFocus == 2 {
		a.promptTplBody.Focus()
	}
}

// commitPromptTemplateInputs writes the current edit fields back to the selected template.
func (a *CharmApp) commitPromptTemplateInputs() {
	if len(a.agentPromptTemplates) == 0 {
		return
	}
	index := clamp(a.promptTplCursor, 0, len(a.agentPromptTemplates)-1)
	a.agentPromptTemplates[index].Name = strings.TrimSpace(a.promptTplName.Value())
	a.agentPromptTemplates[index].Prompt = strings.TrimSpace(a.promptTplBody.Value())
}

// handlePromptTemplatesKey edits prompt templates and saves them to disk.
func (a CharmApp) handlePromptTemplatesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "ctrl+s":
		a.commitPromptTemplateInputs()
		templates, err := validateCharmPromptTemplates(a.agentPromptTemplates)
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.closeOverlay()
		a.loading = true
		a.status = "Saving prompt templates..."
		return a, a.savePromptTemplatesCmd(templates)
	case "tab":
		a.promptTplFocus = (a.promptTplFocus + 1) % 3
		a.syncPromptTemplateInputs()
		return a, nil
	case "shift+tab":
		a.promptTplFocus = (a.promptTplFocus + 2) % 3
		a.syncPromptTemplateInputs()
		return a, nil
	case "up", "k":
		if a.promptTplFocus == 0 {
			a.commitPromptTemplateInputs()
			a.promptTplCursor = clamp(a.promptTplCursor-1, 0, len(a.agentPromptTemplates)-1)
			a.syncPromptTemplateInputs()
			return a, nil
		}
	case "down", "j":
		if a.promptTplFocus == 0 {
			a.commitPromptTemplateInputs()
			a.promptTplCursor = clamp(a.promptTplCursor+1, 0, len(a.agentPromptTemplates)-1)
			a.syncPromptTemplateInputs()
			return a, nil
		}
	case "a":
		if a.promptTplFocus == 0 {
			a.commitPromptTemplateInputs()
			a.agentPromptTemplates = append(a.agentPromptTemplates, config.AgentPromptTemplate{Name: a.nextPromptTemplateName(), Prompt: ""})
			a.promptTplCursor = len(a.agentPromptTemplates) - 1
			a.promptTplFocus = 1
			a.syncPromptTemplateInputs()
			return a, a.promptTplName.Focus()
		}
	case "d":
		if a.promptTplFocus == 0 && len(a.agentPromptTemplates) > 1 {
			a.agentPromptTemplates = append(a.agentPromptTemplates[:a.promptTplCursor], a.agentPromptTemplates[a.promptTplCursor+1:]...)
			a.promptTplCursor = clamp(a.promptTplCursor, 0, len(a.agentPromptTemplates)-1)
			a.syncPromptTemplateInputs()
			return a, nil
		}
	}
	if a.promptTplFocus == 1 {
		next, cmd := a.promptTplName.Update(msg)
		a.promptTplName = next
		return a, cmd
	}
	if a.promptTplFocus == 2 {
		next, cmd := a.promptTplBody.Update(msg)
		a.promptTplBody = next
		return a, cmd
	}
	return a, nil
}

// nextPromptTemplateName returns a unique default name for a newly added template.
func (a CharmApp) nextPromptTemplateName() string {
	existing := make(map[string]bool, len(a.agentPromptTemplates))
	for _, template := range a.agentPromptTemplates {
		existing[template.Name] = true
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("New Prompt %d", i)
		if !existing[name] {
			return name
		}
	}
}

// validateCharmPromptTemplates ensures templates have the fields required by config persistence.
func validateCharmPromptTemplates(templates []config.AgentPromptTemplate) ([]config.AgentPromptTemplate, error) {
	valid := make([]config.AgentPromptTemplate, 0, len(templates))
	for _, template := range templates {
		name := strings.TrimSpace(template.Name)
		prompt := strings.TrimSpace(template.Prompt)
		if name == "" || prompt == "" {
			return nil, fmt.Errorf("prompt templates require a name and prompt")
		}
		valid = append(valid, config.AgentPromptTemplate{Name: name, Prompt: prompt})
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("at least one prompt template is required")
	}
	return valid, nil
}

// openAgentPrompt opens the Charm-native prompt form for the selected issue.
func (a *CharmApp) openAgentPrompt() tea.Cmd {
	a.overlay = charmOverlayAgentPrompt
	a.agentPromptFocus = 0
	a.agentTemplate = clamp(a.agentTemplate, 0, maxInt(0, len(a.agentPromptTemplates)-1))
	a.agentPromptArea.SetValue(a.defaultAgentPromptText())
	a.agentWorkspace.SetValue(a.defaultAgentWorkspace())
	a.agentPromptArea.Focus()
	a.agentWorkspace.Blur()
	a.status = "Ask agent"
	return a.agentPromptArea.Focus()
}

// defaultAgentPromptText returns the currently selected prompt template body.
func (a CharmApp) defaultAgentPromptText() string {
	if len(a.agentPromptTemplates) == 0 {
		return ""
	}
	index := clamp(a.agentTemplate, 0, len(a.agentPromptTemplates)-1)
	return a.agentPromptTemplates[index].Prompt
}

// defaultAgentWorkspace returns the configured workspace or the current process directory.
func (a CharmApp) defaultAgentWorkspace() string {
	if workspace := strings.TrimSpace(a.cfg.AgentWorkspace); workspace != "" {
		return workspace
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// setAgentPromptFocus moves focus between prompt, workspace, and template selection.
func (a *CharmApp) setAgentPromptFocus(index int) tea.Cmd {
	maxFocus := 1
	if len(a.agentPromptTemplates) > 0 {
		maxFocus = 2
	}
	a.agentPromptFocus = clamp(index, 0, maxFocus)
	a.agentPromptArea.Blur()
	a.agentWorkspace.Blur()
	switch a.agentPromptFocus {
	case 0:
		return a.agentPromptArea.Focus()
	case 1:
		return a.agentWorkspace.Focus()
	default:
		return nil
	}
}

// handleAgentPromptKey updates or submits the agent prompt overlay.
func (a CharmApp) handleAgentPromptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.closeOverlay()
		return a, nil
	case "tab":
		return a, a.setAgentPromptFocus(a.agentPromptFocus + 1)
	case "shift+tab":
		return a, a.setAgentPromptFocus(a.agentPromptFocus - 1)
	case "ctrl+s", "ctrl+enter":
		return a.submitAgentPrompt()
	case "enter":
		if a.agentPromptFocus != 0 {
			return a.submitAgentPrompt()
		}
	case "up", "k":
		if a.agentPromptFocus == 2 && len(a.agentPromptTemplates) > 0 {
			a.agentTemplate = clamp(a.agentTemplate-1, 0, len(a.agentPromptTemplates)-1)
			a.agentPromptArea.SetValue(a.defaultAgentPromptText())
			return a, nil
		}
	case "down", "j":
		if a.agentPromptFocus == 2 && len(a.agentPromptTemplates) > 0 {
			a.agentTemplate = clamp(a.agentTemplate+1, 0, len(a.agentPromptTemplates)-1)
			a.agentPromptArea.SetValue(a.defaultAgentPromptText())
			return a, nil
		}
	}
	if a.agentPromptFocus == 1 {
		next, cmd := a.agentWorkspace.Update(msg)
		a.agentWorkspace = next
		return a, cmd
	}
	if a.agentPromptFocus == 0 {
		next, cmd := a.agentPromptArea.Update(msg)
		a.agentPromptArea = next
		return a, cmd
	}
	return a, nil
}

// handleAgentOutputKey handles output overlay cancellation, copying, and close behavior.
func (a CharmApp) handleAgentOutputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if a.agentRunning && a.agentCancel != nil {
			a.agentCancel()
			a.agentOutputStatus = "Canceling..."
			a.status = "Canceling agent run..."
			return a, nil
		}
		a.closeOverlay()
		return a, nil
	case "c":
		if strings.TrimSpace(a.agentFinalText) == "" {
			a.status = "No final agent output to copy"
			return a, nil
		}
		return a, a.copyAgentFinalCmd()
	}
	next, cmd := a.agentOutput.Update(msg)
	a.agentOutput = next
	return a, cmd
}

// submitAgentPrompt validates the prompt and starts streaming agent output.
func (a CharmApp) submitAgentPrompt() (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	prompt := strings.TrimSpace(a.agentPromptArea.Value())
	if prompt == "" {
		a.status = "Prompt is required"
		return a, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 64)
	a.overlay = charmOverlayAgentOutput
	a.agentRunning = true
	a.agentCancel = cancel
	a.agentEvents = events
	a.agentBuffer = NewAgentStreamBuffer()
	a.agentOutputLines = nil
	a.agentFinalText = ""
	a.agentSessionID = ""
	a.agentResumeCmd = ""
	a.agentOutputTitle = "Agent Output"
	a.agentOutputStatus = "Starting..."
	a.agentOutput.SetContent("")
	a.status = "Starting agent..."
	return a, a.startAgentRunCmd(ctx, events, issue.ID, prompt, strings.TrimSpace(a.agentWorkspace.Value()))
}

// startAgentRunCmd resolves issue context and streams provider output through Bubble Tea messages.
func (a CharmApp) startAgentRunCmd(ctx context.Context, events chan tea.Msg, issueID string, prompt string, workspace string) tea.Cmd {
	return func() tea.Msg {
		go a.runAgent(ctx, events, issueID, prompt, workspace)
		return charmAgentStartedMsg{status: "Starting agent run..."}
	}
}

// runAgent executes the selected provider and sends all UI updates to the event channel.
func (a CharmApp) runAgent(ctx context.Context, events chan tea.Msg, issueID string, prompt string, workspace string) {
	defer close(events)
	runner := a.agentRunner
	if runner == nil {
		runner = agents.NewRunner()
	}
	fetchIssue := a.fetchIssueByIDFunc
	if fetchIssue == nil {
		fetchIssue = a.api.FetchIssueByID
	}
	fullIssue, err := fetchIssue(ctx, issueID)
	if err != nil {
		sendAgentMsg(ctx, events, charmAgentRunFinishedMsg{err: err})
		return
	}
	provider, err := agents.ProviderForKey(a.cfg.AgentProvider, runner.LookPath)
	if err != nil {
		sendAgentMsg(ctx, events, charmAgentRunFinishedMsg{err: err})
		return
	}
	if _, ok := provider.ResolveBinary(); !ok {
		sendAgentMsg(ctx, events, charmAgentRunFinishedMsg{err: fmt.Errorf("agent binary not found for %s", provider.Name())})
		return
	}
	if strings.TrimSpace(workspace) == "" {
		workspace = a.defaultAgentWorkspace()
	}
	options := agents.AgentRunOptions{
		Workspace: workspace,
		Model:     strings.TrimSpace(a.cfg.AgentModel),
		Sandbox:   strings.TrimSpace(a.cfg.AgentSandbox),
	}
	runFunc := a.agentRunFunc
	if runFunc == nil {
		runFunc = runner.Run
	}
	sendAgentMsg(ctx, events, charmAgentLineMsg{line: fmt.Sprintf("Starting %s agent run...", provider.Name())})
	err = runFunc(ctx, provider, prompt, agents.BuildIssueContext(fullIssue), options, func(event agents.AgentEvent) {
		sendAgentMsg(ctx, events, charmAgentEventMsg{event: event})
	}, func(line string) {
		sendAgentMsg(ctx, events, charmAgentLineMsg{line: line})
	}, func(runErr error) {
		sendAgentMsg(ctx, events, charmAgentLineMsg{line: fmt.Sprintf("error: %v", runErr)})
	})
	sendAgentMsg(ctx, events, charmAgentRunFinishedMsg{err: err})
}

// sendAgentMsg sends a message unless the run has already been canceled.
func sendAgentMsg(ctx context.Context, events chan tea.Msg, msg tea.Msg) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- msg:
		return true
	}
}

// waitAgentMsgCmd blocks for the next streamed agent message.
func (a CharmApp) waitAgentMsgCmd() tea.Cmd {
	events := a.agentEvents
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return charmAgentChannelClosedMsg{}
		}
		return msg
	}
}

// applyAgentEvent updates stream, final text, and resume metadata from a structured event.
func (a *CharmApp) applyAgentEvent(event agents.AgentEvent) {
	if event.SessionID != "" {
		a.agentSessionID = event.SessionID
	}
	if event.ResumeCommand != "" {
		a.agentResumeCmd = event.ResumeCommand
	}
	update := a.agentBuffer.Append(event)
	for _, line := range update.Lines {
		a.appendAgentOutputLine(formatCharmAgentStreamLine(line))
	}
	if update.FinalText != "" {
		a.agentFinalText = update.FinalText
	}
	if len(update.Lines) == 0 && event.Text != "" && event.Type != agents.AgentEventAssistant && event.Type != agents.AgentEventAssistantDelta {
		a.appendAgentOutputLine(fmt.Sprintf("%s: %s", event.Type, strings.TrimSpace(event.Text)))
	}
}

// appendAgentOutputLine appends one rendered output line and refreshes the viewport.
func (a *CharmApp) appendAgentOutputLine(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	a.agentOutputLines = append(a.agentOutputLines, line)
	a.refreshAgentOutputContent()
}

// refreshAgentOutputContent rebuilds the streaming viewport content.
func (a *CharmApp) refreshAgentOutputContent() {
	content := strings.Join(a.agentOutputLines, "\n")
	if strings.TrimSpace(a.agentFinalText) != "" {
		content += "\n\nFinal\n" + strings.TrimSpace(a.agentFinalText)
	}
	if a.agentSessionID != "" {
		content += "\n\nSession: " + a.agentSessionID
	}
	if a.agentResumeCmd != "" {
		content += "\nResume: " + a.agentResumeCmd
	}
	a.agentOutput.SetContent(content)
	a.agentOutput.GotoBottom()
}

// formatCharmAgentStreamLine renders a structured stream line for the output overlay.
func formatCharmAgentStreamLine(line StreamLine) string {
	prefix := string(line.Kind)
	if prefix == "" {
		prefix = string(StreamLineUnknown)
	}
	return fmt.Sprintf("%s: %s", prefix, strings.TrimSpace(line.Text))
}

// copyAgentFinalCmd copies the final agent answer to the clipboard.
func (a CharmApp) copyAgentFinalCmd() tea.Cmd {
	text := strings.TrimSpace(a.agentFinalText)
	copyFn := a.copyToClipboardFunc
	if copyFn == nil {
		copyFn = copyToClipboard
	}
	return func() tea.Msg {
		err := copyFn(text)
		return charmIssueActionMsg{status: "Copied final agent output", err: err}
	}
}

func (a *CharmApp) openPicker(title string, action charmPickerAction, items []charmPickerItem) {
	a.overlay = charmOverlayPicker
	a.pickerTitle = title
	a.pickerAction = action
	a.pickerItems = items
	a.pickerCursor = 0
	a.pickerLoading = false
	a.status = title
}

// replacePickerItems swaps refreshed picker options while preserving the selected item when possible.
func (a *CharmApp) replacePickerItems(title string, items []charmPickerItem) {
	selectedID := ""
	if len(a.pickerItems) > 0 {
		selectedID = a.pickerItems[clamp(a.pickerCursor, 0, len(a.pickerItems)-1)].ID
	}
	if title != "" {
		a.pickerTitle = title
	}
	a.pickerItems = items
	a.pickerLoading = false
	a.pickerCursor = 0
	if selectedID == "" {
		return
	}
	for i, item := range items {
		if item.ID == selectedID {
			a.pickerCursor = i
			return
		}
	}
}

// openLoadingPicker opens a picker shell immediately while async options load.
func (a *CharmApp) openLoadingPicker(title string, action charmPickerAction, status string) {
	a.overlay = charmOverlayPicker
	a.pickerTitle = title
	a.pickerAction = action
	a.pickerItems = nil
	a.pickerCursor = 0
	a.pickerLoading = true
	a.status = status
}

// openMultiSelect opens a Charm multi-select overlay with the provided selected IDs.
func (a *CharmApp) openMultiSelect(title string, action charmMultiSelectAction, items []charmMultiSelectItem, selectedIDs []string) {
	a.overlay = charmOverlayMultiSelect
	a.multiTitle = title
	a.multiAction = action
	a.multiItems = items
	a.multiCursor = 0
	a.multiSelected = make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		a.multiSelected[id] = true
	}
	a.status = title
}

// openSettingsForm opens the Charm-native settings editor.
func (a *CharmApp) openSettingsForm(settings config.Settings) {
	a.overlay = charmOverlaySettings
	a.settingsFields = charmSettingsFields(settings)
	a.settingsCursor = 0
	a.syncSettingsInput()
	a.settingsInput.Focus()
	a.status = "Settings"
}

// openCustomViewForm opens the Charm-native custom view editor.
func (a *CharmApp) openCustomViewForm(view config.CustomView) {
	a.overlay = charmOverlayCustomView
	a.customViewEditing = view.ID
	a.customViewFields = charmCustomViewFields(view)
	a.customViewCursor = 0
	a.syncCustomViewInput()
	a.customViewInput.Focus()
	if view.ID == "" {
		a.status = "Add custom view"
		return
	}
	a.status = "Edit custom view"
}

// charmCustomViewFields converts a custom view into editable form rows.
func charmCustomViewFields(view config.CustomView) []charmSettingsField {
	return []charmSettingsField{
		{Key: "name", Label: "Name", Value: view.Name},
		{Key: "team_id", Label: "Team ID", Value: view.TeamID},
		{Key: "project_id", Label: "Project ID", Value: view.ProjectID},
		{Key: "state_id", Label: "State ID", Value: view.StateID},
		{Key: "state_mode", Label: "State mode", Value: string(view.StateMode), Options: []string{string(config.CustomViewStateAny), string(config.CustomViewStateNotDone)}},
		{Key: "assignee_id", Label: "Assignee ID", Value: view.AssigneeID},
		{Key: "label_id", Label: "Label ID", Value: view.LabelID},
		{Key: "due_within_days", Label: "Due within days", Value: charmOptionalInt(view.DueWithinDays)},
		{Key: "sort_primary", Label: "Primary sort", Value: string(defaultCustomViewSort(view.SortPrimary, config.CustomViewSortUpdatedAt)), Options: []string{string(config.CustomViewSortUpdatedAt), string(config.CustomViewSortCreatedAt), string(config.CustomViewSortPriority), string(config.CustomViewSortStatus)}},
		{Key: "sort_secondary", Label: "Secondary sort", Value: string(view.SortSecondary), Options: []string{string(config.CustomViewSortNone), string(config.CustomViewSortUpdatedAt), string(config.CustomViewSortCreatedAt), string(config.CustomViewSortPriority), string(config.CustomViewSortStatus)}},
	}
}

func charmOptionalInt(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func defaultCustomViewSort(value config.CustomViewSortField, fallback config.CustomViewSortField) config.CustomViewSortField {
	if value == "" {
		return fallback
	}
	return value
}

// charmSettingsFields converts settings into editable rows.
func charmSettingsFields(settings config.Settings) []charmSettingsField {
	return []charmSettingsField{
		{Key: "api_endpoint", Label: "API endpoint", Value: settings.APIEndpoint},
		{Key: "timeout", Label: "Timeout", Value: settings.Timeout},
		{Key: "page_size", Label: "Page size", Value: strconv.Itoa(settings.PageSize)},
		{Key: "cache_ttl", Label: "Cache TTL", Value: settings.CacheTTL},
		{Key: "search_debounce", Label: "Search debounce", Value: settings.SearchDebounce},
		{Key: "include_completed", Label: "Include completed", Value: boolSetting(settings.IncludeCompleted), Options: []string{"false", "true"}},
		{Key: "show_navigation", Label: "Show navigation", Value: boolSetting(settings.ShowNavigation), Options: []string{"false", "true"}},
		{Key: "show_my_issues", Label: "Show my issues", Value: boolSetting(settings.ShowMyIssues), Options: []string{"false", "true"}},
		{Key: "show_other_issues", Label: "Show other issues", Value: boolSetting(settings.ShowOtherIssues), Options: []string{"false", "true"}},
		{Key: "log_file", Label: "Log file", Value: settings.LogFile},
		{Key: "log_level", Label: "Log level", Value: settings.LogLevel, Options: []string{"debug", "info", "warning", "error"}},
		{Key: "theme", Label: "Theme", Value: settings.Theme, Options: []string{config.ThemeLinear, config.ThemeHighContrast, config.ThemeColorBlind}},
		{Key: "density", Label: "Density", Value: settings.Density, Options: []string{config.DensityComfortable, config.DensityCompact}},
		{Key: "agent_provider", Label: "Agent provider", Value: settings.AgentProvider, Options: []string{"cursor", "claude"}},
		{Key: "agent_sandbox", Label: "Agent sandbox", Value: settings.AgentSandbox, Options: []string{config.DefaultAgentSandbox, "disabled"}},
		{Key: "agent_model", Label: "Agent model", Value: settings.AgentModel},
		{Key: "agent_workspace", Label: "Agent workspace", Value: settings.AgentWorkspace},
	}
}

func boolSetting(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func parseBoolSetting(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func (a CharmApp) currentSettingsField() charmSettingsField {
	if len(a.settingsFields) == 0 {
		return charmSettingsField{}
	}
	return a.settingsFields[clamp(a.settingsCursor, 0, len(a.settingsFields)-1)]
}

func (a *CharmApp) moveSettingsCursor(delta int) {
	a.commitSettingsInputValue()
	if len(a.settingsFields) == 0 {
		return
	}
	a.settingsCursor = clamp(a.settingsCursor+delta, 0, len(a.settingsFields)-1)
	a.syncSettingsInput()
}

func (a *CharmApp) cycleSettingsOption(delta int) {
	if len(a.settingsFields) == 0 {
		return
	}
	field := &a.settingsFields[clamp(a.settingsCursor, 0, len(a.settingsFields)-1)]
	if len(field.Options) == 0 {
		return
	}
	index := 0
	for i, option := range field.Options {
		if option == field.Value {
			index = i
			break
		}
	}
	field.Value = field.Options[(index+delta+len(field.Options))%len(field.Options)]
}

func (a *CharmApp) syncSettingsInput() {
	field := a.currentSettingsField()
	a.settingsInput.SetValue(field.Value)
	a.settingsInput.Placeholder = field.Label
	if len(field.Options) > 0 {
		a.settingsInput.Blur()
		return
	}
	a.settingsInput.Focus()
}

func (a *CharmApp) commitSettingsInputValue() {
	if len(a.settingsFields) == 0 {
		return
	}
	index := clamp(a.settingsCursor, 0, len(a.settingsFields)-1)
	if len(a.settingsFields[index].Options) == 0 {
		a.settingsFields[index].Value = a.settingsInput.Value()
	}
}

func (a CharmApp) currentCustomViewField() charmSettingsField {
	if len(a.customViewFields) == 0 {
		return charmSettingsField{}
	}
	return a.customViewFields[clamp(a.customViewCursor, 0, len(a.customViewFields)-1)]
}

func (a *CharmApp) moveCustomViewCursor(delta int) {
	a.commitCustomViewInputValue()
	if len(a.customViewFields) == 0 {
		return
	}
	a.customViewCursor = clamp(a.customViewCursor+delta, 0, len(a.customViewFields)-1)
	a.syncCustomViewInput()
}

func (a *CharmApp) cycleCustomViewOption(delta int) {
	if len(a.customViewFields) == 0 {
		return
	}
	field := &a.customViewFields[clamp(a.customViewCursor, 0, len(a.customViewFields)-1)]
	if len(field.Options) == 0 {
		return
	}
	index := 0
	for i, option := range field.Options {
		if option == field.Value {
			index = i
			break
		}
	}
	field.Value = field.Options[(index+delta+len(field.Options))%len(field.Options)]
}

func (a *CharmApp) syncCustomViewInput() {
	field := a.currentCustomViewField()
	a.customViewInput.SetValue(field.Value)
	a.customViewInput.Placeholder = field.Label
	if len(field.Options) > 0 {
		a.customViewInput.Blur()
		return
	}
	a.customViewInput.Focus()
}

func (a *CharmApp) commitCustomViewInputValue() {
	if len(a.customViewFields) == 0 {
		return
	}
	index := clamp(a.customViewCursor, 0, len(a.customViewFields)-1)
	if len(a.customViewFields[index].Options) == 0 {
		a.customViewFields[index].Value = a.customViewInput.Value()
	}
}

// settingsFromCharmFields builds settings from the active Charm settings form rows.
func (a CharmApp) settingsFromCharmFields() (config.Settings, error) {
	settings, err := a.currentSettings()
	if err != nil {
		return config.Settings{}, err
	}
	values := make(map[string]string, len(a.settingsFields))
	for _, field := range a.settingsFields {
		values[field.Key] = strings.TrimSpace(field.Value)
	}
	settings.APIEndpoint = values["api_endpoint"]
	settings.Timeout = values["timeout"]
	settings.CacheTTL = values["cache_ttl"]
	settings.SearchDebounce = values["search_debounce"]
	settings.LogFile = values["log_file"]
	settings.LogLevel = values["log_level"]
	settings.Theme = values["theme"]
	settings.Density = values["density"]
	settings.AgentProvider = values["agent_provider"]
	settings.AgentSandbox = values["agent_sandbox"]
	settings.AgentModel = values["agent_model"]
	settings.AgentWorkspace = values["agent_workspace"]
	settings.IncludeCompleted = parseBoolSetting(values["include_completed"])
	settings.ShowNavigation = parseBoolSetting(values["show_navigation"])
	settings.ShowMyIssues = parseBoolSetting(values["show_my_issues"])
	settings.ShowOtherIssues = parseBoolSetting(values["show_other_issues"])
	pageSize, err := strconv.Atoi(values["page_size"])
	if err != nil {
		return config.Settings{}, fmt.Errorf("invalid page size %q", values["page_size"])
	}
	settings.PageSize = pageSize
	return settings, nil
}

// customViewFromCharmFields builds a custom view from the active Charm form rows.
func (a CharmApp) customViewFromCharmFields() (config.CustomView, error) {
	values := make(map[string]string, len(a.customViewFields))
	for _, field := range a.customViewFields {
		values[field.Key] = strings.TrimSpace(field.Value)
	}
	name := values["name"]
	if name == "" {
		return config.CustomView{}, fmt.Errorf("view name is required")
	}
	view := config.CustomView{
		ID:            a.customViewEditing,
		Name:          name,
		TeamID:        values["team_id"],
		ProjectID:     values["project_id"],
		StateID:       values["state_id"],
		StateMode:     config.CustomViewStateMode(values["state_mode"]),
		AssigneeID:    values["assignee_id"],
		LabelID:       values["label_id"],
		SortPrimary:   config.CustomViewSortField(values["sort_primary"]),
		SortSecondary: config.CustomViewSortField(values["sort_secondary"]),
	}
	if view.StateMode == config.CustomViewStateNotDone {
		view.StateID = ""
	}
	dueText := values["due_within_days"]
	if dueText != "" {
		days, err := strconv.Atoi(dueText)
		if err != nil || days < 0 {
			return config.CustomView{}, fmt.Errorf("invalid due within days")
		}
		view.DueWithinDays = days
	}
	return view, nil
}

func (a *CharmApp) openCreateIssueForm(teamID string, parentID string) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormCreateIssue
	a.formTitle = "Create Issue"
	a.formFocus = 0
	a.formTeamID = teamID
	a.formParentID = parentID
	a.formProjectID = ""
	a.formCycleID = ""
	a.formAssigneeID = ""
	a.formAssigneeName = ""
	if a.currentUser != nil && a.currentUser.ID != "" {
		a.formAssigneeID = a.currentUser.ID
		a.formAssigneeName = formatUserDisplayName(*a.currentUser)
	}
	if a.selectedNavigation != nil && a.selectedNavigation.IsProject {
		a.formProjectID = a.selectedNavigation.ID
	}
	if a.selectedNavigation != nil && a.selectedNavigation.IsCycle {
		a.formCycleID = a.selectedNavigation.CycleID
	}
	a.titleInput.Prompt = "Title: "
	a.titleInput.Placeholder = "Issue title"
	a.titleInput.SetValue("")
	a.bodyArea.SetValue("")
	a.bodyArea.Placeholder = "Description"
	a.titleInput.Focus()
	a.bodyArea.Blur()
	if parentID != "" {
		a.formTitle = "Create Sub-Issue"
		a.status = "Create sub-issue"
		return
	}
	a.status = "Create issue"
}

// openCreateAssigneePicker lets the create form choose an assignee without losing draft text.
func (a CharmApp) openCreateAssigneePicker() (tea.Model, tea.Cmd) {
	teamID := strings.TrimSpace(a.formTeamID)
	if teamID == "" {
		a.status = "Select a team before choosing an assignee"
		return a, nil
	}
	a.openLoadingPicker("Issue Assignee", charmPickerCreateAssignee, "loading")
	return a, a.loadPickerItemsCmd(charmPickerCreateAssignee, teamID)
}

// returnToIssueFormFromPicker closes a nested picker and restores create-form focus.
func (a *CharmApp) returnToIssueFormFromPicker() {
	a.overlay = charmOverlayIssueForm
	a.pickerItems = nil
	a.pickerCursor = 0
	a.pickerTitle = ""
	a.pickerLoading = false
	a.titleInput.Blur()
	a.bodyArea.Blur()
	if a.formFocus == 0 {
		a.titleInput.Focus()
	} else if a.formFocus == 1 {
		a.bodyArea.Focus()
	}
	a.status = "Create issue"
}

// createIssueAssigneeLabel returns the create-form assignee display text.
func (a CharmApp) createIssueAssigneeLabel() string {
	name := strings.TrimSpace(a.formAssigneeName)
	if name == "" {
		name = strings.TrimSpace(a.formAssigneeID)
	}
	if name == "" {
		return "Assignee: Unassigned"
	}
	return "Assignee: " + name
}

func (a *CharmApp) openEditTitleForm(issue linearapi.Issue) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormEditTitle
	a.formTitle = "Edit Title"
	a.formFocus = 0
	a.formIssueID = issue.ID
	a.titleInput.Prompt = "Title: "
	a.titleInput.Placeholder = "Issue title"
	a.titleInput.SetValue(issue.Title)
	a.bodyArea.SetValue("")
	a.titleInput.Focus()
	a.bodyArea.Blur()
	a.status = "Edit title"
}

// openEditDescriptionForm opens a markdown editor for the selected issue description.
func (a *CharmApp) openEditDescriptionForm(issue linearapi.Issue) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormEditDescription
	a.formTitle = "Edit Description"
	a.formFocus = 1
	a.formIssueID = issue.ID
	a.titleInput.SetValue("")
	a.bodyArea.SetValue(issue.Description)
	a.bodyArea.Placeholder = "Description"
	a.titleInput.Blur()
	a.bodyArea.Focus()
	a.status = "Edit description"
}

func (a *CharmApp) openCommentForm(issue linearapi.Issue) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormAddComment
	a.formTitle = "Add Comment"
	a.formFocus = 1
	a.formIssueID = issue.ID
	a.titleInput.SetValue("")
	a.bodyArea.SetValue("")
	a.bodyArea.Placeholder = "Comment"
	a.titleInput.Blur()
	a.bodyArea.Focus()
	a.status = "Add comment"
}

func (a *CharmApp) openDueDateForm(issue linearapi.Issue) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormSetDueDate
	a.formTitle = "Set Due Date"
	a.formFocus = 0
	a.formIssueID = issue.ID
	initial := ""
	if issue.DueDate != nil {
		initial = *issue.DueDate
	}
	a.titleInput.Prompt = "Due date: "
	a.titleInput.Placeholder = "YYYY-MM-DD"
	a.titleInput.SetValue(initial)
	a.bodyArea.SetValue("")
	a.titleInput.Focus()
	a.bodyArea.Blur()
	a.status = "Set due date"
}

func (a *CharmApp) openEstimateForm(issue linearapi.Issue) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormSetEstimate
	a.formTitle = "Set Estimate"
	a.formFocus = 0
	a.formIssueID = issue.ID
	initial := ""
	if issue.Estimate != nil {
		initial = formatEstimate(issue.Estimate)
	}
	a.titleInput.Prompt = "Points: "
	a.titleInput.Placeholder = "0"
	a.titleInput.SetValue(initial)
	a.bodyArea.SetValue("")
	a.titleInput.Focus()
	a.bodyArea.Blur()
	a.status = "Set estimate"
}

// openIssueRelationTargetForm asks for the Linear issue ID used by the selected relation type.
func (a *CharmApp) openIssueRelationTargetForm(issue linearapi.Issue, relationLabel string) {
	a.overlay = charmOverlayIssueForm
	a.formMode = charmFormIssueRelationTarget
	a.formTitle = "Add Issue Relation"
	a.formFocus = 0
	a.formIssueID = issue.ID
	a.formRelationLabel = relationLabel
	a.titleInput.Prompt = "Issue ID: "
	a.titleInput.Placeholder = "Related Linear issue ID"
	a.titleInput.SetValue("")
	a.bodyArea.SetValue("")
	a.titleInput.Focus()
	a.bodyArea.Blur()
	a.status = "Related issue"
}

// openFilterTextForm opens a text input form for text, due date, or estimate filters.
func (a *CharmApp) openFilterTextForm(mode charmIssueFormMode) {
	a.overlay = charmOverlayIssueForm
	a.formMode = mode
	a.formFocus = 0
	a.titleInput.Prompt = "Value: "
	a.titleInput.Placeholder = ""
	a.bodyArea.SetValue("")
	switch mode {
	case charmFormFilterText:
		a.formTitle = "Filter Text"
		a.titleInput.Prompt = "Search: "
		a.titleInput.SetValue(a.searchQuery)
		a.status = "Filter text"
	case charmFormFilterDueDate:
		a.formTitle = "Filter Due Date"
		a.titleInput.Prompt = "Due date: "
		a.titleInput.Placeholder = "YYYY-MM-DD"
		a.titleInput.SetValue(formatDateFilterSummary(a.richFilters.DueDate))
		a.status = "Filter due date"
	case charmFormFilterEstimate:
		a.formTitle = "Filter Estimate"
		a.titleInput.Prompt = "Points: "
		a.titleInput.Placeholder = "0"
		a.titleInput.SetValue(formatNumberFilterSummary(a.richFilters.Estimate))
		a.status = "Filter estimate"
	}
	a.titleInput.Focus()
	a.bodyArea.Blur()
}

func (a *CharmApp) toggleFormFocus() {
	if a.formMode == charmFormAddComment || a.formMode == charmFormEditDescription {
		a.formFocus = 1
		return
	}
	if a.formMode == charmFormCreateIssue {
		a.formFocus = (a.formFocus + 1) % 3
		a.titleInput.Blur()
		a.bodyArea.Blur()
		if a.formFocus == 0 {
			a.titleInput.Focus()
		} else if a.formFocus == 1 {
			a.bodyArea.Focus()
		}
		return
	}
	if a.formMode == charmFormEditTitle || a.formMode == charmFormIssueRelationTarget {
		a.formFocus = 0
		return
	}
	if a.formMode == charmFormFilterText || a.formMode == charmFormFilterDueDate || a.formMode == charmFormFilterEstimate {
		a.formFocus = 0
		return
	}
	if a.formFocus == 0 {
		a.formFocus = 1
		a.titleInput.Blur()
		a.bodyArea.Focus()
		return
	}
	a.formFocus = 0
	a.bodyArea.Blur()
	a.titleInput.Focus()
}

func (a CharmApp) submitIssueForm() (tea.Model, tea.Cmd) {
	switch a.formMode {
	case charmFormCreateIssue:
		title := strings.TrimSpace(a.titleInput.Value())
		if title == "" {
			a.status = "Title is required"
			return a, nil
		}
		input := linearapi.CreateIssueInput{
			TeamID:      a.formTeamID,
			Title:       title,
			Description: strings.TrimSpace(a.bodyArea.Value()),
			ProjectID:   a.formProjectID,
			CycleID:     a.formCycleID,
			AssigneeID:  a.formAssigneeID,
			ParentID:    a.formParentID,
		}
		a.closeOverlay()
		a.loading = true
		a.status = "Creating issue..."
		return a, a.createIssueCmd(input)
	case charmFormEditTitle:
		title := strings.TrimSpace(a.titleInput.Value())
		if title == "" {
			a.status = "Title is required"
			return a, nil
		}
		issue := linearapi.Issue{ID: a.formIssueID, Identifier: a.formIssueID}
		if current := a.currentIssue(); current != nil {
			issue = *current
		}
		a.closeOverlay()
		return a.runOptimisticTitleUpdate(issue, title)
	case charmFormEditDescription:
		description := strings.TrimSpace(a.bodyArea.Value())
		issue := linearapi.Issue{ID: a.formIssueID, Identifier: a.formIssueID}
		if current := a.currentIssue(); current != nil {
			issue = *current
		}
		a.closeOverlay()
		return a.runOptimisticDescriptionUpdate(issue, description)
	case charmFormAddComment:
		body := strings.TrimSpace(a.bodyArea.Value())
		if body == "" {
			a.status = "Comment is required"
			return a, nil
		}
		issueID := a.formIssueID
		a.closeOverlay()
		a.loading = true
		a.status = "Adding comment..."
		return a, a.createCommentCmd(issueID, body)
	case charmFormSetDueDate:
		value := strings.TrimSpace(a.titleInput.Value())
		if err := validateLinearDate(value); err != nil {
			a.status = err.Error()
			return a, nil
		}
		issue := linearapi.Issue{ID: a.formIssueID, Identifier: a.formIssueID}
		if current := a.currentIssue(); current != nil {
			issue = *current
		}
		a.closeOverlay()
		return a.runOptimisticDueDateUpdate(issue, value, fmt.Sprintf("Updated due date for %s", issue.Identifier))
	case charmFormSetEstimate:
		estimate, err := parseEstimateInput(a.titleInput.Value())
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		issue := linearapi.Issue{ID: a.formIssueID, Identifier: a.formIssueID}
		if current := a.currentIssue(); current != nil {
			issue = *current
		}
		a.closeOverlay()
		return a.runOptimisticEstimateUpdate(issue, &estimate, fmt.Sprintf("Updated estimate for %s", issue.Identifier))
	case charmFormIssueRelationTarget:
		targetIssueID := strings.TrimSpace(a.titleInput.Value())
		input, err := relationInputForIssue(a.formIssueID, a.formRelationLabel, targetIssueID)
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		issue := linearapi.Issue{ID: a.formIssueID, Identifier: a.formIssueID}
		if current := a.currentIssue(); current != nil {
			issue = *current
		}
		a.closeOverlay()
		a.loading = true
		a.status = "Adding issue relation..."
		return a, a.createIssueRelationCmd(issue, input)
	case charmFormFilterText:
		a.searchQuery = strings.TrimSpace(a.titleInput.Value())
		a.closeOverlay()
		return a.applyFiltersAndReload("Applied text filter", false)
	case charmFormFilterDueDate:
		value := strings.TrimSpace(a.titleInput.Value())
		if err := validateLinearDate(value); err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.richFilters.DueDate = linearapi.DateFilter{Eq: value}
		a.closeOverlay()
		return a.applyFiltersAndReload("Applied due date filter", false)
	case charmFormFilterEstimate:
		estimate, err := parseEstimateInput(a.titleInput.Value())
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.richFilters.Estimate = linearapi.NumberFilter{Eq: &estimate}
		a.closeOverlay()
		return a.applyFiltersAndReload("Applied estimate filter", false)
	default:
		a.status = "Unsupported form"
		return a, nil
	}
}

func charmCommands() []charmCommand {
	return []charmCommand{
		{ID: "refresh", Title: "Refresh issues", Keywords: []string{"reload", "sync"}},
		{ID: "clear_search", Title: "Clear search", Keywords: []string{"reset", "query"}},
		{ID: "clear_filters", Title: "Clear filters", Keywords: []string{"reset", "search"}},
		{ID: "settings", Title: "Settings", Keywords: []string{"config", "preferences"}},
		{ID: "ask_agent", Title: "Ask agent", Keywords: []string{"ai", "cursor", "claude", "prompt"}},
		{ID: "edit_agent_prompts", Title: "Edit agent prompts", Keywords: []string{"ai", "agent", "prompt", "template"}},
		{ID: "open_browser", Title: "Open in browser", Keywords: []string{"open", "browser", "web"}},
		{ID: "copy_id", Title: "Copy issue ID", Keywords: []string{"copy", "identifier"}},
		{ID: "copy_url", Title: "Copy issue URL", Keywords: []string{"copy", "url", "link"}},
		{ID: "sort_issues", Title: "Sort issues", Keywords: []string{"sort", "order", "board", "linear order"}},
		{ID: "filter_issues", Title: "Filter issues", Keywords: []string{"filter", "search", "query"}},
		{ID: "filter_team", Title: "Filter by team", Keywords: []string{"filter", "team"}},
		{ID: "filter_assignee", Title: "Filter by assignee", Keywords: []string{"filter", "assignee", "user"}},
		{ID: "filter_labels", Title: "Filter by labels", Keywords: []string{"filter", "labels", "tags"}},
		{ID: "filter_status", Title: "Filter by status", Keywords: []string{"filter", "status", "state"}},
		{ID: "filter_project", Title: "Filter by project", Keywords: []string{"filter", "project"}},
		{ID: "filter_cycle", Title: "Filter by cycle", Keywords: []string{"filter", "cycle", "sprint"}},
		{ID: "filter_due_date", Title: "Filter by due date", Keywords: []string{"filter", "due", "date"}},
		{ID: "filter_estimate", Title: "Filter by estimate", Keywords: []string{"filter", "estimate", "points"}},
		{ID: "filter_text", Title: "Filter by text search", Keywords: []string{"filter", "text", "search"}},
		{ID: "add_custom_view", Title: "Add custom view", Keywords: []string{"custom", "view", "add", "filter"}},
		{ID: "edit_custom_view", Title: "Edit custom view", Keywords: []string{"custom", "view", "edit", "rename"}},
		{ID: "delete_custom_view", Title: "Delete custom view", Keywords: []string{"custom", "view", "delete", "remove"}},
		{ID: "assign_me", Title: "Assign to me", Keywords: []string{"take", "self"}},
		{ID: "assign_user", Title: "Assign to user", Keywords: []string{"assignee", "member"}},
		{ID: "unassign", Title: "Unassign issue", Keywords: []string{"clear assignee"}},
		{ID: "change_status", Title: "Change status", Keywords: []string{"state", "workflow"}},
		{ID: "change_priority", Title: "Change priority", Keywords: []string{"urgent", "high", "low"}},
		{ID: "set_cycle", Title: "Set cycle", Keywords: []string{"sprint", "iteration"}},
		{ID: "clear_cycle", Title: "Clear cycle", Keywords: []string{"remove cycle"}},
		{ID: "edit_labels", Title: "Edit issue labels", Keywords: []string{"label", "labels", "tag", "tags"}},
		{ID: "list_project_milestones", Title: "List project milestones", Keywords: []string{"project", "milestone", "roadmap"}},
		{ID: "set_milestone", Title: "Set milestone", Keywords: []string{"project", "milestone"}},
		{ID: "clear_milestone", Title: "Clear milestone", Keywords: []string{"project", "milestone", "remove"}},
		{ID: "add_issue_relation", Title: "Add issue relation", Keywords: []string{"relation", "dependency", "blocking", "blocked", "related", "duplicate", "similar"}},
		{ID: "remove_issue_relation", Title: "Remove issue relation", Keywords: []string{"relation", "dependency", "remove", "unlink"}},
		{ID: "subscribe_issue", Title: "Subscribe", Keywords: []string{"subscribe", "watch", "subscriber"}},
		{ID: "unsubscribe_issue", Title: "Unsubscribe", Keywords: []string{"unsubscribe", "watch", "subscriber"}},
		{ID: "open_attachment", Title: "Open attachment", Keywords: []string{"attachment", "link", "open", "github", "jira", "slack", "url"}},
		{ID: "copy_attachment_url", Title: "Copy attachment URL", Keywords: []string{"attachment", "link", "copy", "url"}},
		{ID: "create_issue", Title: "Create issue", Keywords: []string{"new", "add"}},
		{ID: "create_sub_issue", Title: "Create sub-issue", Keywords: []string{"new", "add", "child", "parent"}},
		{ID: "view_parent", Title: "View parent issue", Keywords: []string{"parent", "up", "back"}},
		{ID: "set_parent", Title: "Set parent issue", Keywords: []string{"parent", "link"}},
		{ID: "remove_parent", Title: "Remove parent", Keywords: []string{"parent", "unlink"}},
		{ID: "expand_all", Title: "Expand all sub-issues", Keywords: []string{"expand", "children"}},
		{ID: "collapse_all", Title: "Collapse all sub-issues", Keywords: []string{"collapse", "children"}},
		{ID: "edit_title", Title: "Edit issue title", Keywords: []string{"rename"}},
		{ID: "edit_description", Title: "Edit issue description", Keywords: []string{"description", "body", "markdown", "notes"}},
		{ID: "add_comment", Title: "Add comment", Keywords: []string{"note", "reply"}},
		{ID: "set_due_date", Title: "Set due date", Keywords: []string{"date", "deadline"}},
		{ID: "due_today", Title: "Mark due today", Keywords: []string{"today", "due", "date"}},
		{ID: "clear_due_date", Title: "Clear due date", Keywords: []string{"date", "remove"}},
		{ID: "set_estimate", Title: "Set estimate", Keywords: []string{"points", "size"}},
		{ID: "clear_estimate", Title: "Clear estimate", Keywords: []string{"points", "remove"}},
		{ID: "archive", Title: "Archive issue", Keywords: []string{"delete", "remove"}},
	}
}

// newCommandPaletteList builds the scrollable command list used inside the palette.
func newCommandPaletteList(styles charmStyles) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	itemStyles := list.NewDefaultItemStyles(true)
	itemStyles.NormalTitle = lipgloss.NewStyle().Foreground(styles.palette.fg).Width(54).PaddingLeft(1)
	itemStyles.SelectedTitle = lipgloss.NewStyle().
		Foreground(styles.palette.selectedText).
		Background(styles.palette.selected).
		Bold(true).
		Width(54).
		PaddingLeft(1).
		PaddingRight(1)
	itemStyles.DimmedTitle = lipgloss.NewStyle().Foreground(styles.palette.subtle).Width(54).PaddingLeft(1)
	itemStyles.FilterMatch = lipgloss.NewStyle().Underline(true).Foreground(styles.palette.focus)
	delegate.Styles = itemStyles

	palette := list.New(commandItemsFromCommands(charmCommands()), delegate, 56, 11)
	palette.SetShowTitle(false)
	palette.SetShowFilter(false)
	palette.SetShowStatusBar(false)
	palette.SetShowHelp(false)
	palette.SetShowPagination(true)
	palette.SetFilteringEnabled(false)
	return palette
}

// commandItemsFromCommands adapts app commands to the Bubbles list item interface.
func commandItemsFromCommands(commands []charmCommand) []list.Item {
	items := make([]list.Item, 0, len(commands))
	for _, command := range commands {
		items = append(items, charmCommandItem{command: command})
	}
	return items
}

func (a CharmApp) filteredCharmCommands() []charmCommand {
	query := strings.ToLower(strings.TrimSpace(a.paletteInput.Value()))
	commands := charmCommands()
	if query == "" {
		return commands
	}
	terms := strings.Fields(query)
	filtered := make([]charmCommand, 0, len(commands))
	for _, command := range commands {
		haystack := strings.ToLower(command.Title + " " + string(command.ID) + " " + strings.Join(command.Keywords, " "))
		matches := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matches = false
				break
			}
		}
		if matches {
			filtered = append(filtered, command)
		}
	}
	return filtered
}

// syncCommandPaletteList refreshes visible commands after the palette filter changes.
func (a *CharmApp) syncCommandPaletteList(selectFirst bool) {
	a.paletteList.SetItems(commandItemsFromCommands(a.filteredCharmCommands()))
	if selectFirst {
		a.paletteList.Select(0)
	}
}

func (a CharmApp) runCharmCommand(id charmCommandID) (tea.Model, tea.Cmd) {
	switch id {
	case "refresh":
		a.loading = true
		a.status = "Refreshing issues..."
		a.issueResults.Clear()
		return a, a.loadIssuesCmd("", false)
	case "clear_search":
		a.searchQuery = ""
		a.loading = true
		a.status = "Clearing search..."
		return a, tea.Batch(a.persistSearchQueryCmd(), a.loadIssuesCmd("", true))
	case "clear_filters":
		a.searchQuery = ""
		a.richFilters = IssueFilters{}
		a.loading = true
		a.status = "Clearing filters..."
		return a, tea.Batch(a.persistSearchQueryCmd(), a.loadIssuesCmd("", true))
	case "settings":
		settings, err := a.currentSettings()
		if err != nil {
			a.status = err.Error()
			return a, nil
		}
		a.openSettingsForm(settings)
		return a, a.settingsInput.Focus()
	case "ask_agent":
		if a.currentIssue() == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a, a.openAgentPrompt()
	case "edit_agent_prompts":
		return a, a.openPromptTemplatesForm()
	case "open_browser":
		return a.runIssueURLCommand(false)
	case "copy_id":
		return a.runCopyIssueIDCommand()
	case "copy_url":
		return a.runIssueURLCommand(true)
	case "sort_issues":
		a.openPicker("Sort Issues", charmPickerIssueSort, charmIssueSortItems(a.activeIssueSortField()))
		return a, nil
	case "filter_issues":
		a.openPicker("Filter Issues", charmPickerFilterKind, charmFilterKindItems())
		return a, nil
	case "filter_team":
		return a.openFilterMultiSelect(charmMultiSelectFilterTeam)
	case "filter_assignee":
		return a.openFilterMultiSelect(charmMultiSelectFilterAssignee)
	case "filter_labels":
		return a.openFilterMultiSelect(charmMultiSelectFilterLabel)
	case "filter_status":
		return a.openFilterMultiSelect(charmMultiSelectFilterStatus)
	case "filter_project":
		return a.openFilterMultiSelect(charmMultiSelectFilterProject)
	case "filter_cycle":
		return a.openFilterMultiSelect(charmMultiSelectFilterCycle)
	case "filter_due_date":
		a.openFilterTextForm(charmFormFilterDueDate)
		return a, tea.Batch(a.titleInput.Focus())
	case "filter_estimate":
		a.openFilterTextForm(charmFormFilterEstimate)
		return a, tea.Batch(a.titleInput.Focus())
	case "filter_text":
		a.openFilterTextForm(charmFormFilterText)
		return a, tea.Batch(a.titleInput.Focus())
	case "add_custom_view":
		a.openCustomViewForm(a.customViewFromCurrentContext())
		return a, a.customViewInput.Focus()
	case "edit_custom_view":
		if a.selectedCustomView == nil {
			a.status = "No custom view selected"
			return a, nil
		}
		a.openCustomViewForm(*a.selectedCustomView)
		return a, a.customViewInput.Focus()
	case "delete_custom_view":
		if a.selectedCustomView == nil {
			a.status = "No custom view selected"
			return a, nil
		}
		a.overlay = charmOverlayConfirmDeleteView
		a.status = "Confirm delete custom view"
		return a, nil
	case "assign_me":
		issue := a.currentIssue()
		if issue == nil || a.currentUser == nil {
			a.status = "No issue or current user selected"
			return a, nil
		}
		return a.runOptimisticAssigneeUpdate(*issue, a.currentUser.ID, formatUserDisplayName(*a.currentUser), fmt.Sprintf("Assigned %s to %s", issue.Identifier, emptyDash(a.currentUser.DisplayName)))
	case "unassign":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticAssigneeUpdate(*issue, "", "", fmt.Sprintf("Unassigned %s", issue.Identifier))
	case "change_priority":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openPicker("Change Priority", charmPickerPriority, charmPriorityItems())
		return a, nil
	case "change_status":
		issue := a.currentIssue()
		teamID := a.teamIDForIssue(issue)
		if issue == nil || teamID == "" {
			a.status = "No issue or team selected"
			return a, nil
		}
		if items, ok := a.cachedStatusPickerItems(teamID); ok {
			a.openPicker("Change Status", charmPickerStatus, items)
			return a, a.refreshPickerItemsCmd(charmPickerStatus, teamID)
		}
		a.loading = true
		a.openLoadingPicker("Change Status", charmPickerStatus, "loading")
		return a, a.loadPickerItemsCmd(charmPickerStatus, teamID)
	case "assign_user":
		issue := a.currentIssue()
		teamID := a.teamIDForIssue(issue)
		if issue == nil || teamID == "" {
			a.status = "No issue or team selected"
			return a, nil
		}
		a.loading = true
		a.openLoadingPicker("Assign User", charmPickerAssignee, "Loading users...")
		return a, a.loadPickerItemsCmd(charmPickerAssignee, teamID)
	case "set_cycle":
		issue := a.currentIssue()
		teamID := a.teamIDForIssue(issue)
		if issue == nil || teamID == "" {
			a.status = "No issue or team selected"
			return a, nil
		}
		a.loading = true
		a.openLoadingPicker("Set Cycle", charmPickerCycle, "Loading cycles...")
		return a, a.loadPickerItemsCmd(charmPickerCycle, teamID)
	case "clear_cycle":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticCycleUpdate(*issue, "", "", fmt.Sprintf("Cleared cycle for %s", issue.Identifier))
	case "edit_labels":
		issue := a.currentIssue()
		teamID := a.teamIDForIssue(issue)
		if issue == nil || teamID == "" {
			a.status = "No issue or team selected"
			return a, nil
		}
		a.loading = true
		a.status = "Loading labels..."
		return a, a.loadLabelItemsCmd(*issue, teamID)
	case "list_project_milestones":
		issue := a.currentIssue()
		projectID := a.projectIDForIssue(issue)
		if issue == nil || projectID == "" {
			a.status = "Issue must have a project"
			return a, nil
		}
		a.loading = true
		a.openLoadingPicker("Project Milestones", charmPickerListMilestone, "Loading milestones...")
		return a, a.loadPickerItemsCmd(charmPickerListMilestone, projectID)
	case "set_milestone":
		issue := a.currentIssue()
		projectID := a.projectIDForIssue(issue)
		if issue == nil || projectID == "" {
			a.status = "Issue must have a project"
			return a, nil
		}
		a.loading = true
		a.openLoadingPicker("Set Milestone", charmPickerMilestone, "Loading milestones...")
		return a, a.loadPickerItemsCmd(charmPickerMilestone, projectID)
	case "clear_milestone":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticMilestoneUpdate(*issue, "", "", fmt.Sprintf("Cleared milestone for %s", issue.Identifier))
	case "add_issue_relation":
		if a.currentIssue() == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openPicker("Relation Type", charmPickerRelationType, charmRelationTypeItems())
		return a, nil
	case "remove_issue_relation":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		items := charmRelationPickerItems(issue.Relations)
		if len(items) == 0 {
			a.status = "No issue relations"
			return a, nil
		}
		a.openPicker("Remove Relation", charmPickerRemoveRelation, items)
		return a, nil
	case "subscribe_issue":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.loading = true
		a.status = "Subscribing..."
		return a, a.issueSubscriptionCmd(*issue, true)
	case "unsubscribe_issue":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.loading = true
		a.status = "Unsubscribing..."
		return a, a.issueSubscriptionCmd(*issue, false)
	case "open_attachment":
		return a.runAttachmentCommand(charmPickerOpenAttachment)
	case "copy_attachment_url":
		return a.runAttachmentCommand(charmPickerCopyAttachment)
	case "create_issue":
		teamID := a.selectedTeamIDForCreate()
		if teamID == "" {
			a.status = "Select a team before creating an issue"
			return a, nil
		}
		a.openCreateIssueForm(teamID, "")
		return a, tea.Batch(a.titleInput.Focus())
	case "create_sub_issue":
		issue := a.currentIssue()
		teamID := a.selectedTeamIDForCreate()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		if teamID == "" {
			teamID = issue.TeamID
		}
		if teamID == "" {
			a.status = "Select a team before creating a sub-issue"
			return a, nil
		}
		a.openCreateIssueForm(teamID, issue.ID)
		return a, tea.Batch(a.titleInput.Focus())
	case "view_parent":
		return a.viewParentIssue()
	case "set_parent":
		return a.openParentPickerCommand()
	case "remove_parent":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		if issue.Parent == nil {
			a.status = "No parent issue"
			return a, nil
		}
		a.selectedIssue = issue
		a.overlay = charmOverlayConfirmRemoveParent
		a.status = "Confirm remove parent"
		return a, nil
	case "expand_all":
		ExpandAll(a.expanded, a.issues)
		a.rebuildIssueTables("")
		a.status = "Expanded all sub-issues"
		return a, nil
	case "collapse_all":
		CollapseAll(a.expanded)
		a.rebuildIssueTables("")
		a.status = "Collapsed all sub-issues"
		return a, nil
	case "edit_title":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openEditTitleForm(*issue)
		return a, tea.Batch(a.titleInput.Focus())
	case "edit_description":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openEditDescriptionForm(*issue)
		return a, tea.Batch(a.bodyArea.Focus())
	case "add_comment":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openCommentForm(*issue)
		return a, tea.Batch(a.bodyArea.Focus())
	case "set_due_date":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openDueDateForm(*issue)
		return a, tea.Batch(a.titleInput.Focus())
	case "clear_due_date":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticDueDateUpdate(*issue, "", fmt.Sprintf("Cleared due date for %s", issue.Identifier))
	case "due_today":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		today := todayLinearDate()
		return a.runOptimisticDueDateUpdate(*issue, today, fmt.Sprintf("Marked %s due today", issue.Identifier))
	case "set_estimate":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.openEstimateForm(*issue)
		return a, tea.Batch(a.titleInput.Focus())
	case "clear_estimate":
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticEstimateUpdate(*issue, nil, fmt.Sprintf("Cleared estimate for %s", issue.Identifier))
	case "archive":
		if a.currentIssue() == nil {
			a.status = "No issue selected"
			return a, nil
		}
		a.selectedIssue = a.currentIssue()
		a.overlay = charmOverlayConfirmArchive
		a.status = "Confirm archive"
		return a, nil
	default:
		a.status = "Command not implemented yet"
		return a, nil
	}
}

func (a CharmApp) runIssueUpdate(issue linearapi.Issue, input linearapi.UpdateIssueInput, success string) (tea.Model, tea.Cmd) {
	a.loading = true
	a.status = "Updating issue..."
	if input.ID == "" {
		input.ID = issue.ID
	}
	return a, a.updateIssueCmd(issue, input, success)
}

// runOptimisticStatusUpdate applies a status change locally before Linear confirms it.
func (a CharmApp) runOptimisticStatusUpdate(issue linearapi.Issue, stateID string, stateName string) (tea.Model, tea.Cmd) {
	before := issue
	after := issue
	after.StateID = stateID
	after.State = stateName
	a.loading = true
	a.status = fmt.Sprintf("Changed status for %s", issue.Identifier)
	a.undo = &charmUndoAction{
		Before: before,
		After:  after,
		Input:  linearapi.UpdateIssueInput{ID: issue.ID, StateID: &before.StateID},
		Status: fmt.Sprintf("Undid status change for %s", issue.Identifier),
	}
	a.applyIssueStatus(issue.ID, stateID, stateName)
	input := linearapi.UpdateIssueInput{ID: issue.ID, StateID: &stateID}
	return a, a.updateIssueStatusCmd(issue, input, before, a.status)
}

// runOptimisticPriorityUpdate applies a priority change locally before Linear confirms it.
func (a CharmApp) runOptimisticPriorityUpdate(issue linearapi.Issue, priority int) (tea.Model, tea.Cmd) {
	before := issue
	after := issue
	after.Priority = priority
	a.loading = true
	a.status = fmt.Sprintf("Changed priority for %s", issue.Identifier)
	a.undo = &charmUndoAction{
		Before: before,
		After:  after,
		Input:  linearapi.UpdateIssueInput{ID: issue.ID, Priority: &before.Priority},
		Status: fmt.Sprintf("Undid priority change for %s", issue.Identifier),
	}
	a.applyIssuePriority(issue.ID, priority)
	input := linearapi.UpdateIssueInput{ID: issue.ID, Priority: &priority}
	return a, a.updateIssuePriorityCmd(issue, input, before.Priority, a.status)
}

// runOptimisticDescriptionUpdate applies a description edit locally before Linear confirms it.
func (a CharmApp) runOptimisticDescriptionUpdate(issue linearapi.Issue, description string) (tea.Model, tea.Cmd) {
	before := issue
	after := issue
	after.Description = description
	a.loading = true
	a.status = fmt.Sprintf("Updated description for %s", issue.Identifier)
	a.undo = &charmUndoAction{
		Before: before,
		After:  after,
		Input:  linearapi.UpdateIssueInput{ID: issue.ID, Description: &before.Description},
		Status: fmt.Sprintf("Undid description change for %s", issue.Identifier),
	}
	a.applyIssueDescription(issue.ID, description)
	input := linearapi.UpdateIssueInput{ID: issue.ID, Description: &description}
	return a, a.updateIssueDescriptionCmd(issue, input, before, a.status)
}

// runOptimisticDueDateUpdate applies a due date locally before Linear confirms it.
func (a CharmApp) runOptimisticDueDateUpdate(issue linearapi.Issue, dueDate string, success string) (tea.Model, tea.Cmd) {
	before := issue
	after := issue
	after.DueDate = &dueDate
	a.loading = true
	a.status = success
	a.undo = &charmUndoAction{
		Before: before,
		After:  after,
		Input:  linearapi.UpdateIssueInput{ID: issue.ID, DueDate: rollbackDueDateInput(before.DueDate)},
		Status: fmt.Sprintf("Undid due date change for %s", issue.Identifier),
	}
	a.applyIssueDueDate(issue.ID, &dueDate)
	input := linearapi.UpdateIssueInput{ID: issue.ID, DueDate: &dueDate}
	return a, a.updateIssueDueDateCmd(issue, input, before, success)
}

// runOptimisticIssueSnapshotUpdate applies a full local issue snapshot before Linear confirms it.
func (a CharmApp) runOptimisticIssueSnapshotUpdate(before linearapi.Issue, after linearapi.Issue, input linearapi.UpdateIssueInput, undoInput linearapi.UpdateIssueInput, success string) (tea.Model, tea.Cmd) {
	if input.ID == "" {
		input.ID = before.ID
	}
	if undoInput.ID == "" {
		undoInput.ID = before.ID
	}
	a.loading = true
	a.status = success
	a.undo = &charmUndoAction{
		Before: before,
		After:  after,
		Input:  undoInput,
		Status: fmt.Sprintf("Undid update for %s", before.Identifier),
	}
	a.applyIssueSnapshot(after)
	return a, a.updateIssueSnapshotCmd(before, input, before, success)
}

// runOptimisticTitleUpdate updates the issue title locally before Linear confirms it.
func (a CharmApp) runOptimisticTitleUpdate(issue linearapi.Issue, title string) (tea.Model, tea.Cmd) {
	after := issue
	after.Title = title
	input := linearapi.UpdateIssueInput{ID: issue.ID, Title: &title}
	previous := issue.Title
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, Title: &previous}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, fmt.Sprintf("Updated title for %s", issue.Identifier))
}

// runOptimisticAssigneeUpdate updates the issue assignee locally before Linear confirms it.
func (a CharmApp) runOptimisticAssigneeUpdate(issue linearapi.Issue, assigneeID string, assigneeName string, success string) (tea.Model, tea.Cmd) {
	after := issue
	after.AssigneeID = assigneeID
	after.Assignee = assigneeName
	inputID := assigneeID
	input := linearapi.UpdateIssueInput{ID: issue.ID, AssigneeID: &inputID}
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, AssigneeID: rollbackClearableIDInput(issue.AssigneeID)}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, success)
}

// runOptimisticCycleUpdate updates the issue cycle locally before Linear confirms it.
func (a CharmApp) runOptimisticCycleUpdate(issue linearapi.Issue, cycleID string, cycleName string, success string) (tea.Model, tea.Cmd) {
	after := issue
	if cycleID == "" {
		after.Cycle = nil
	} else {
		after.Cycle = &linearapi.CycleRef{ID: cycleID, Name: cycleName}
	}
	inputID := cycleID
	input := linearapi.UpdateIssueInput{ID: issue.ID, CycleID: &inputID}
	previousID := ""
	if issue.Cycle != nil {
		previousID = issue.Cycle.ID
	}
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, CycleID: rollbackClearableIDInput(previousID)}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, success)
}

// runOptimisticMilestoneUpdate updates the issue milestone locally before Linear confirms it.
func (a CharmApp) runOptimisticMilestoneUpdate(issue linearapi.Issue, milestoneID string, milestoneName string, success string) (tea.Model, tea.Cmd) {
	after := issue
	if milestoneID == "" {
		after.ProjectMilestone = nil
	} else {
		after.ProjectMilestone = &linearapi.ProjectMilestoneRef{ID: milestoneID, Name: milestoneName, ProjectID: issue.ProjectID}
	}
	inputID := milestoneID
	input := linearapi.UpdateIssueInput{ID: issue.ID, ProjectMilestoneID: &inputID}
	previousID := ""
	if issue.ProjectMilestone != nil {
		previousID = issue.ProjectMilestone.ID
	}
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, ProjectMilestoneID: rollbackClearableIDInput(previousID)}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, success)
}

// runOptimisticEstimateUpdate updates the issue estimate locally before Linear confirms it.
func (a CharmApp) runOptimisticEstimateUpdate(issue linearapi.Issue, estimate *float64, success string) (tea.Model, tea.Cmd) {
	after := issue
	after.Estimate = cloneFloatPointer(estimate)
	input := linearapi.UpdateIssueInput{ID: issue.ID, Estimate: cloneFloatPointer(estimate)}
	if estimate == nil {
		input.ClearEstimate = true
	}
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, Estimate: cloneFloatPointer(issue.Estimate)}
	if issue.Estimate == nil {
		undoInput.ClearEstimate = true
	}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, success)
}

// runOptimisticLabelsUpdate updates issue labels locally before Linear confirms them.
func (a CharmApp) runOptimisticLabelsUpdate(issue linearapi.Issue, labelIDs []string) (tea.Model, tea.Cmd) {
	after := issue
	after.Labels = labelsForIDs(labelIDs, a.multiItemLabelMap())
	sortedLabelIDs := append([]string(nil), labelIDs...)
	sort.Strings(sortedLabelIDs)
	input := linearapi.UpdateIssueInput{ID: issue.ID, LabelIDs: &sortedLabelIDs}
	previousLabelIDs := charmIssueLabelIDs(issue)
	undoInput := linearapi.UpdateIssueInput{ID: issue.ID, LabelIDs: &previousLabelIDs}
	return a.runOptimisticIssueSnapshotUpdate(issue, after, input, undoInput, fmt.Sprintf("Updated labels for %s", issue.Identifier))
}

// runUndoLastAction applies the saved inverse issue edit locally and persists it to Linear.
func (a CharmApp) runUndoLastAction() (tea.Model, tea.Cmd) {
	if a.undo == nil {
		a.status = "Nothing to undo"
		return a, nil
	}
	action := *a.undo
	a.loading = true
	a.status = action.Status
	a.restoreIssueForRollback(action.Before)
	return a, a.undoIssueCmd(action)
}

// runCopyIssueIDCommand copies the selected issue identifier.
func (a CharmApp) runCopyIssueIDCommand() (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	copyFn := a.copyToClipboardFunc
	if copyFn == nil {
		copyFn = copyToClipboard
	}
	return a, func() tea.Msg {
		err := copyFn(issue.Identifier)
		return charmIssueActionMsg{status: fmt.Sprintf("Copied %s", issue.Identifier), err: err}
	}
}

// runIssueURLCommand opens or copies the selected issue URL.
func (a CharmApp) runIssueURLCommand(copyURL bool) (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	if issue.URL == "" {
		a.status = fmt.Sprintf("No URL for %s", issue.Identifier)
		return a, nil
	}
	if copyURL {
		copyFn := a.copyToClipboardFunc
		if copyFn == nil {
			copyFn = copyToClipboard
		}
		return a, func() tea.Msg {
			err := copyFn(issue.URL)
			return charmIssueActionMsg{status: fmt.Sprintf("Copied URL for %s", issue.Identifier), err: err}
		}
	}
	openFn := a.openURLFunc
	if openFn == nil {
		openFn = openURL
	}
	return a, func() tea.Msg {
		err := openFn(issue.URL)
		return charmIssueActionMsg{status: fmt.Sprintf("Opened %s", issue.Identifier), err: err}
	}
}

// viewParentIssue selects the current issue parent when it is present in loaded rows.
func (a CharmApp) viewParentIssue() (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	if issue.Parent == nil {
		a.status = "No parent issue"
		return a, nil
	}
	a.selectIssueByID(issue.Parent.ID)
	if a.selectedIssue != nil && a.selectedIssue.ID == issue.Parent.ID {
		a.status = fmt.Sprintf("Selected parent %s", issue.Parent.Identifier)
		return a, nil
	}
	a.status = "Parent issue is not loaded"
	return a, nil
}

// openParentPickerCommand opens a picker of top-level issues that can parent the selection.
func (a CharmApp) openParentPickerCommand() (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	if len(issue.Children) > 0 {
		a.status = "Cannot set parent on issue with sub-issues"
		return a, nil
	}
	excluded := excludedParentCandidateIDs(issue, a.issues)
	items := make([]charmPickerItem, 0)
	for _, candidate := range a.issues {
		if candidate.Parent == nil && !excluded[candidate.ID] {
			items = append(items, charmPickerItem{ID: candidate.ID, Label: candidate.Identifier + " - " + candidate.Title})
		}
	}
	if len(items) == 0 {
		a.status = "No parent issues available"
		return a, nil
	}
	a.openPicker("Select Parent Issue", charmPickerParent, items)
	return a, nil
}

// runAttachmentCommand opens a picker only when an issue has multiple attachments.
func (a CharmApp) runAttachmentCommand(action charmPickerAction) (tea.Model, tea.Cmd) {
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	if len(issue.Attachments) == 0 {
		a.status = "No attachments"
		return a, nil
	}
	if len(issue.Attachments) == 1 {
		attachment := issue.Attachments[0]
		a.loading = true
		if action == charmPickerOpenAttachment {
			a.status = "Opening attachment..."
			return a, a.openAttachmentCmd(attachment)
		}
		a.status = "Copying attachment URL..."
		return a, a.copyAttachmentCmd(attachment)
	}
	title := "Open Attachment"
	if action == charmPickerCopyAttachment {
		title = "Copy Attachment URL"
	}
	a.openPicker(title, action, charmAttachmentPickerItems(issue.Attachments))
	return a, nil
}

func (a CharmApp) applyPickerSelection(action charmPickerAction, item charmPickerItem) (tea.Model, tea.Cmd) {
	if action == charmPickerFilterKind {
		return a.runCharmCommand(charmCommandID("filter_" + item.ID))
	}
	if action == charmPickerIssueSort {
		return a.applyIssueSort(SortField(item.ID), item.Label)
	}
	if action == charmPickerCreateAssignee {
		a.formAssigneeID = item.ID
		a.formAssigneeName = item.Label
		a.formFocus = 2
		a.returnToIssueFormFromPicker()
		a.status = "Assignee: " + item.Label
		return a, nil
	}
	issue := a.currentIssue()
	if issue == nil {
		a.status = "No issue selected"
		return a, nil
	}
	switch action {
	case charmPickerPriority:
		priority := item.Priority
		return a.runOptimisticPriorityUpdate(*issue, priority)
	case charmPickerStatus:
		stateID := item.ID
		return a.runOptimisticStatusUpdate(*issue, stateID, item.Label)
	case charmPickerAssignee:
		return a.runOptimisticAssigneeUpdate(*issue, item.ID, item.Label, fmt.Sprintf("Assigned %s", issue.Identifier))
	case charmPickerCycle:
		return a.runOptimisticCycleUpdate(*issue, item.ID, item.Label, fmt.Sprintf("Set cycle for %s", issue.Identifier))
	case charmPickerMilestone:
		return a.runOptimisticMilestoneUpdate(*issue, item.ID, item.Label, fmt.Sprintf("Set milestone for %s", issue.Identifier))
	case charmPickerListMilestone:
		a.status = "Milestone: " + item.Label
		return a, nil
	case charmPickerRelationType:
		a.openIssueRelationTargetForm(*issue, item.ID)
		return a, tea.Batch(a.titleInput.Focus())
	case charmPickerRemoveRelation:
		a.loading = true
		a.status = "Removing issue relation..."
		return a, a.deleteIssueRelationCmd(*issue, item.ID)
	case charmPickerOpenAttachment:
		attachment, ok := charmAttachmentByID(issue.Attachments, item.ID)
		if !ok {
			a.status = "Attachment not found"
			return a, nil
		}
		a.loading = true
		a.status = "Opening attachment..."
		return a, a.openAttachmentCmd(attachment)
	case charmPickerCopyAttachment:
		attachment, ok := charmAttachmentByID(issue.Attachments, item.ID)
		if !ok {
			a.status = "Attachment not found"
			return a, nil
		}
		a.loading = true
		a.status = "Copying attachment URL..."
		return a, a.copyAttachmentCmd(attachment)
	case charmPickerParent:
		parentID := item.ID
		return a.runIssueUpdate(
			*issue,
			linearapi.UpdateIssueInput{ID: issue.ID, ParentID: &parentID},
			fmt.Sprintf("Set parent for %s", issue.Identifier),
		)
	default:
		a.status = "Unsupported picker action"
		return a, nil
	}
}

// applyIssueSort applies a board/list sort selected from the command palette.
func (a CharmApp) applyIssueSort(field SortField, label string) (tea.Model, tea.Cmd) {
	switch field {
	case SortByOrder, SortByUpdatedAt, SortByCreatedAt, SortByPriority, SortByStatus:
	default:
		a.status = "Unsupported sort"
		return a, nil
	}
	a.sortOverride = field
	a.closeOverlay()
	a.status = "Sorted by " + strings.TrimSuffix(label, " (current)")
	if field == SortByOrder && !issuesHaveSortOrder(a.issues) {
		a.loading = true
		a.issueResults.Clear()
		return a, tea.Batch(a.persistSearchQueryCmd(), a.loadIssuesCmd("", false))
	}
	targetIssueID := ""
	if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}
	a.sortCharmIssues()
	a.rebuildIssueTables(targetIssueID)
	return a, a.persistSearchQueryCmd()
}

// issuesHaveSortOrder reports whether at least one cached issue carries Linear order data.
func issuesHaveSortOrder(issues []linearapi.Issue) bool {
	for _, issue := range issues {
		if issue.SortOrder != 0 {
			return true
		}
	}
	return len(issues) == 0
}

// charmIssueSortItems returns the available board/list sort orders.
func charmIssueSortItems(active SortField) []charmPickerItem {
	items := []charmPickerItem{
		{ID: string(SortByOrder), Label: "Linear order"},
		{ID: string(SortByUpdatedAt), Label: "Recently updated"},
		{ID: string(SortByCreatedAt), Label: "Recently created"},
		{ID: string(SortByPriority), Label: "Priority"},
		{ID: string(SortByStatus), Label: "Status"},
	}
	for i := range items {
		if SortField(items[i].ID) == active {
			items[i].Label += " (current)"
		}
	}
	return items
}

// sortFieldFromSettings returns a supported persisted issue sort, or no override.
func sortFieldFromSettings(value string) SortField {
	switch field := SortField(strings.TrimSpace(value)); field {
	case SortByOrder, SortByUpdatedAt, SortByCreatedAt, SortByPriority, SortByStatus:
		return field
	default:
		return ""
	}
}

// charmFilterKindItems returns the top-level filter categories.
func charmFilterKindItems() []charmPickerItem {
	return []charmPickerItem{
		{ID: "team", Label: "Team"},
		{ID: "assignee", Label: "Assignee"},
		{ID: "labels", Label: "Labels"},
		{ID: "status", Label: "Status"},
		{ID: "project", Label: "Project"},
		{ID: "cycle", Label: "Cycle"},
		{ID: "due_date", Label: "Due date"},
		{ID: "estimate", Label: "Estimate"},
		{ID: "text", Label: "Text search"},
	}
}

// openFilterMultiSelect loads and opens a multi-select filter overlay.
func (a CharmApp) openFilterMultiSelect(action charmMultiSelectAction) (tea.Model, tea.Cmd) {
	if action == charmMultiSelectFilterTeam && len(a.teams) > 0 {
		a.openMultiSelect("Filter Team", action, charmTeamFilterItems(a.teams), a.richFilters.TeamIDs)
		return a, nil
	}
	teamID := a.selectedFilterTeamID()
	if action != charmMultiSelectFilterTeam && teamID == "" {
		a.status = "Select exactly one team before filtering"
		return a, nil
	}
	a.loading = true
	a.status = "Loading filter options..."
	return a, a.loadFilterItemsCmd(action, teamID)
}

// selectedFilterTeamID returns the single team context for team-scoped filters.
func (a CharmApp) selectedFilterTeamID() string {
	if len(a.richFilters.TeamIDs) == 1 {
		return a.richFilters.TeamIDs[0]
	}
	if a.selectedNavigation != nil && a.selectedNavigation.TeamID != "" {
		return a.selectedNavigation.TeamID
	}
	if issue := a.currentIssue(); issue != nil && issue.TeamID != "" {
		return issue.TeamID
	}
	if len(a.teams) == 1 {
		return a.teams[0].ID
	}
	return ""
}

// applyMultiSelectSelection applies the selected IDs from a multi-select overlay.
func (a CharmApp) applyMultiSelectSelection(action charmMultiSelectAction, ids []string) (tea.Model, tea.Cmd) {
	switch action {
	case charmMultiSelectLabels:
		issue := a.currentIssue()
		if issue == nil {
			a.status = "No issue selected"
			return a, nil
		}
		return a.runOptimisticLabelsUpdate(*issue, ids)
	case charmMultiSelectFilterTeam:
		a.richFilters.TeamIDs = ids
		a.richFilters.TeamNames = namesForIDs(ids, a.multiItemLabelMap())
		a.clearCharmTeamScopedFilters()
		return a.applyFiltersAndReload("Applied team filters", len(ids) == 1)
	case charmMultiSelectFilterAssignee:
		a.richFilters.AssigneeIDs = ids
		a.richFilters.AssigneeNames = namesForIDs(ids, a.multiItemLabelMap())
		return a.applyFiltersAndReload("Applied assignee filters", false)
	case charmMultiSelectFilterLabel:
		a.richFilters.LabelIDs = ids
		a.richFilters.LabelNames = namesForIDs(ids, a.multiItemLabelMap())
		return a.applyFiltersAndReload("Applied label filters", false)
	case charmMultiSelectFilterStatus:
		a.richFilters.StateIDs = ids
		a.richFilters.StateNames = namesForIDs(ids, a.multiItemLabelMap())
		return a.applyFiltersAndReload("Applied status filters", false)
	case charmMultiSelectFilterProject:
		a.richFilters.ProjectIDs = ids
		a.richFilters.ProjectNames = namesForIDs(ids, a.multiItemLabelMap())
		return a.applyFiltersAndReload("Applied project filters", false)
	case charmMultiSelectFilterCycle:
		a.richFilters.CycleIDs = ids
		a.richFilters.CycleNames = namesForIDs(ids, a.multiItemLabelMap())
		return a.applyFiltersAndReload("Applied cycle filters", false)
	default:
		a.status = "Unsupported multi-select action"
		return a, nil
	}
}

// multiItemLabelMap returns labels for the currently visible multi-select items.
func (a CharmApp) multiItemLabelMap() map[string]string {
	labels := make(map[string]string, len(a.multiItems))
	for _, item := range a.multiItems {
		labels[item.ID] = item.Label
	}
	return labels
}

// clearCharmTeamScopedFilters drops filters whose IDs are only valid inside one team.
func (a *CharmApp) clearCharmTeamScopedFilters() {
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

// applyFiltersAndReload persists filters, then loads the matching issue view cache-first.
func (a CharmApp) applyFiltersAndReload(status string, preloadSingleTeam bool) (tea.Model, tea.Cmd) {
	a.loading = true
	a.status = status
	cmds := []tea.Cmd{a.persistSearchQueryCmd(), a.loadIssuesCmd("", true)}
	if preloadSingleTeam && len(a.richFilters.TeamIDs) == 1 {
		teamID := a.richFilters.TeamIDs[0]
		if teamID != "" && !a.loadingTeams[teamID] {
			a.loadingTeams[teamID] = true
			cmds = append(cmds, a.loadTeamMetadataCmd(teamID))
		}
	}
	return a, tea.Batch(cmds...)
}

func (a CharmApp) updateIssueCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{issueID: issue.ID, status: success, err: err}
	}
}

// updateIssueStatusCmd persists an optimistic status change and carries rollback data on failure.
func (a CharmApp) updateIssueStatusCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, rollbackIssue linearapi.Issue, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{
			issueID:         issue.ID,
			status:          success,
			rollbackStatus:  true,
			rollbackStateID: rollbackIssue.StateID,
			rollbackState:   rollbackIssue.State,
			rollbackIssue:   rollbackIssue,
			err:             err,
		}
	}
}

// updateIssuePriorityCmd persists an optimistic priority change and carries rollback data on failure.
func (a CharmApp) updateIssuePriorityCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, rollbackPriority int, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{
			issueID:               issue.ID,
			status:                success,
			rollbackPriority:      true,
			rollbackPriorityValue: rollbackPriority,
			err:                   err,
		}
	}
}

// updateIssueDescriptionCmd persists an optimistic description edit and carries rollback data on failure.
func (a CharmApp) updateIssueDescriptionCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, rollbackIssue linearapi.Issue, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{
			issueID:             issue.ID,
			status:              success,
			rollbackDescription: true,
			rollbackIssue:       rollbackIssue,
			err:                 err,
		}
	}
}

// updateIssueDueDateCmd persists an optimistic due-date edit and carries rollback data on failure.
func (a CharmApp) updateIssueDueDateCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, rollbackIssue linearapi.Issue, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{
			issueID:         issue.ID,
			status:          success,
			rollbackDueDate: true,
			rollbackIssue:   rollbackIssue,
			err:             err,
		}
	}
}

// updateIssueSnapshotCmd persists an optimistic issue snapshot edit and carries rollback data on failure.
func (a CharmApp) updateIssueSnapshotCmd(issue linearapi.Issue, input linearapi.UpdateIssueInput, rollbackIssue linearapi.Issue, success string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), input)
		return charmIssueUpdatedMsg{
			issueID:               issue.ID,
			status:                success,
			rollbackIssueSnapshot: true,
			rollbackIssue:         rollbackIssue,
			err:                   err,
		}
	}
}

// undoIssueCmd persists an inverse optimistic edit.
func (a CharmApp) undoIssueCmd(action charmUndoAction) tea.Cmd {
	return func() tea.Msg {
		_, err := a.updateIssueFunc(context.Background(), action.Input)
		return charmIssueUndoMsg{status: action.Status, rollbackIssue: action.After, err: err}
	}
}

// defaultCalendarListWeek loads Google Calendar through the same gws auth path as gc.
func defaultCalendarListWeek(ctx context.Context, weekStart time.Time) ([]calendar.Event, error) {
	client, err := calendar.NewGWSClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.ListWeek(ctx, weekStart)
}

// defaultCalendarDeleteEvent deletes a Google Calendar event through gws.
func defaultCalendarDeleteEvent(ctx context.Context, calendarID string, eventID string) error {
	client, err := calendar.NewGWSClient(ctx)
	if err != nil {
		return err
	}
	return client.DeleteEvent(ctx, calendarID, eventID)
}

// loadCalendarWeekCmd loads cached calendar events immediately and refreshes live gws data.
func (a CharmApp) loadCalendarWeekCmd(weekStart time.Time, includeCache bool) tea.Cmd {
	weekStart = calendar.StartOfWeek(weekStart)
	live := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		events, err := a.calendarListWeekFunc(ctx, weekStart)
		return charmCalendarLoadedMsg{weekStart: weekStart, events: events, fetchedAt: time.Now(), err: err}
	}
	if !includeCache {
		return live
	}
	cached := func() tea.Msg {
		events, fetchedAt, ok := a.calendarCache.LoadWeek(weekStart)
		if !ok {
			return nil
		}
		return charmCalendarLoadedMsg{weekStart: weekStart, events: events, fetchedAt: fetchedAt, fromCache: true}
	}
	return tea.Batch(cached, live)
}

// deleteCalendarEventCmd persists an optimistic calendar event delete.
func (a CharmApp) deleteCalendarEventCmd(event calendar.Event) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.calendarDeleteFunc(ctx, event.CalendarID, event.ID)
		return charmCalendarEventDeletedMsg{event: event, err: err}
	}
}

// saveCalendarCache writes the current week after optimistic or live calendar updates.
func (a CharmApp) saveCalendarCache() error {
	if a.calendarCache == nil {
		return nil
	}
	return a.calendarCache.SaveWeek(a.calendarWeekStart, a.calendarEvents)
}

func (a CharmApp) archiveIssueCmd(issue linearapi.Issue) tea.Cmd {
	return func() tea.Msg {
		err := a.archiveIssueFunc(context.Background(), issue.ID)
		return charmIssueArchivedMsg{status: fmt.Sprintf("Archived %s", issue.Identifier), err: err}
	}
}

func (a CharmApp) createIssueCmd(input linearapi.CreateIssueInput) tea.Cmd {
	return func() tea.Msg {
		issue, err := a.createIssueFunc(context.Background(), input)
		status := "Created issue"
		if issue.Identifier != "" {
			status = fmt.Sprintf("Created issue %s", issue.Identifier)
		}
		return charmIssueCreatedMsg{issue: issue, issueID: issue.ID, status: status, err: err}
	}
}

func (a CharmApp) createCommentCmd(issueID string, body string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.createCommentFunc(context.Background(), linearapi.CreateCommentInput{
			IssueID: issueID,
			Body:    body,
		})
		return charmCommentCreatedMsg{issueID: issueID, status: "Comment added", err: err}
	}
}

// createIssueRelationCmd creates an issue relation and reloads issue details on success.
func (a CharmApp) createIssueRelationCmd(issue linearapi.Issue, input linearapi.CreateIssueRelationInput) tea.Cmd {
	return func() tea.Msg {
		_, err := a.createRelationFunc(context.Background(), input)
		return charmIssueActionMsg{
			issueID:       issue.ID,
			status:        "Added issue relation",
			reloadDetails: true,
			err:           err,
		}
	}
}

// deleteIssueRelationCmd removes an issue relation and reloads issue details on success.
func (a CharmApp) deleteIssueRelationCmd(issue linearapi.Issue, relationID string) tea.Cmd {
	return func() tea.Msg {
		err := a.deleteRelationFunc(context.Background(), relationID)
		return charmIssueActionMsg{
			issueID:       issue.ID,
			status:        "Removed issue relation",
			reloadDetails: true,
			err:           err,
		}
	}
}

// issueSubscriptionCmd toggles the current user's subscription state for an issue.
func (a CharmApp) issueSubscriptionCmd(issue linearapi.Issue, subscribe bool) tea.Cmd {
	return func() tea.Msg {
		status := "Subscribed to issue"
		var err error
		if subscribe {
			_, err = a.subscribeIssueFunc(context.Background(), issue.ID)
		} else {
			status = "Unsubscribed from issue"
			_, err = a.unsubscribeIssueFunc(context.Background(), issue.ID)
		}
		return charmIssueActionMsg{
			issueID:       issue.ID,
			status:        status,
			reloadDetails: true,
			err:           err,
		}
	}
}

// openAttachmentCmd opens an attachment URL using the configured platform opener.
func (a CharmApp) openAttachmentCmd(attachment linearapi.Attachment) tea.Cmd {
	return func() tea.Msg {
		err := a.openURLFunc(attachment.URL)
		return charmIssueActionMsg{status: "Opened attachment", err: err}
	}
}

// copyAttachmentCmd copies an attachment URL using the configured clipboard helper.
func (a CharmApp) copyAttachmentCmd(attachment linearapi.Attachment) tea.Cmd {
	return func() tea.Msg {
		err := a.copyToClipboardFunc(attachment.URL)
		return charmIssueActionMsg{status: "Copied attachment URL", err: err}
	}
}

func (a CharmApp) loadPickerItemsCmd(action charmPickerAction, contextID string) tea.Cmd {
	return a.loadPickerItemsCmdWithMode(action, contextID, false, false)
}

// refreshPickerItemsCmd fetches fresh picker options without blocking an already-open cached picker.
func (a CharmApp) refreshPickerItemsCmd(action charmPickerAction, contextID string) tea.Cmd {
	return a.loadPickerItemsCmdWithMode(action, contextID, true, true)
}

// loadPickerItemsCmdWithMode loads picker options, optionally bypassing cache and marking the result as background.
func (a CharmApp) loadPickerItemsCmdWithMode(action charmPickerAction, contextID string, forceRefresh bool, background bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		switch action {
		case charmPickerStatus:
			var states []linearapi.WorkflowState
			var err error
			if forceRefresh {
				states, err = a.cache.RefreshWorkflowStates(ctx, contextID)
			} else {
				states, err = a.cache.GetWorkflowStates(ctx, contextID)
			}
			if err != nil {
				return charmPickerLoadedMsg{action: action, background: background, err: err}
			}
			return charmPickerLoadedMsg{title: "Change Status", action: action, items: charmStatusPickerItems(states), background: background}
		case charmPickerAssignee, charmPickerCreateAssignee:
			users, err := a.cache.GetUsers(ctx, contextID)
			if err != nil {
				return charmPickerLoadedMsg{action: action, err: err}
			}
			sort.SliceStable(users, func(i, j int) bool {
				return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
			})
			items := make([]charmPickerItem, 0, len(users))
			for _, user := range users {
				label := user.DisplayName
				if label == "" {
					label = user.Name
				}
				items = append(items, charmPickerItem{ID: user.ID, Label: label})
			}
			title := "Assign User"
			if action == charmPickerCreateAssignee {
				title = "Issue Assignee"
			}
			return charmPickerLoadedMsg{title: title, action: action, items: items}
		case charmPickerCycle:
			cycles, err := a.cache.GetCycles(ctx, contextID)
			if err != nil {
				return charmPickerLoadedMsg{action: action, err: err}
			}
			sortCyclesForNavigation(cycles)
			items := make([]charmPickerItem, 0, len(cycles))
			for _, cycle := range cycles {
				label := cycle.DisplayName()
				if cycle.IsActive {
					label += " (active)"
				}
				items = append(items, charmPickerItem{ID: cycle.ID, Label: label})
			}
			return charmPickerLoadedMsg{title: "Set Cycle", action: action, items: items}
		case charmPickerMilestone, charmPickerListMilestone:
			milestones, err := a.loadMilestonesFunc(ctx, contextID)
			if err != nil {
				return charmPickerLoadedMsg{action: action, err: err}
			}
			items := charmMilestonePickerItems(milestones)
			title := "Set Milestone"
			if action == charmPickerListMilestone {
				title = "Project Milestones"
			}
			return charmPickerLoadedMsg{title: title, action: action, items: items}
		default:
			return charmPickerLoadedMsg{action: action, err: fmt.Errorf("unsupported picker action")}
		}
	}
}

// loadLabelItemsCmd loads cached label options and current issue selections for editing.
func (a CharmApp) loadLabelItemsCmd(issue linearapi.Issue, teamID string) tea.Cmd {
	return func() tea.Msg {
		labels, err := a.loadLabelsFunc(context.Background(), teamID)
		if err != nil {
			return charmMultiSelectLoadedMsg{action: charmMultiSelectLabels, err: err}
		}
		items := make([]charmMultiSelectItem, 0, len(labels))
		for _, label := range labels {
			items = append(items, charmMultiSelectItem{ID: label.ID, Label: label.Name})
		}
		return charmMultiSelectLoadedMsg{
			title:       "Edit Labels",
			action:      charmMultiSelectLabels,
			items:       items,
			selectedIDs: charmIssueLabelIDs(issue),
		}
	}
}

// loadFilterItemsCmd loads options for a Charm rich-filter multi-select.
func (a CharmApp) loadFilterItemsCmd(action charmMultiSelectAction, teamID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		switch action {
		case charmMultiSelectFilterTeam:
			teams, err := a.cache.GetTeams(ctx)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			return charmMultiSelectLoadedMsg{
				title:       "Filter Team",
				action:      action,
				items:       charmTeamFilterItems(teams),
				selectedIDs: a.richFilters.TeamIDs,
			}
		case charmMultiSelectFilterAssignee:
			users, err := a.cache.GetUsers(ctx, teamID)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			return charmMultiSelectLoadedMsg{
				title:       "Filter Assignees",
				action:      action,
				items:       charmUserFilterItems(users),
				selectedIDs: a.richFilters.AssigneeIDs,
			}
		case charmMultiSelectFilterLabel:
			labels, err := a.loadLabelsFunc(ctx, teamID)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			return charmMultiSelectLoadedMsg{
				title:       "Filter Labels",
				action:      action,
				items:       charmLabelFilterItems(labels),
				selectedIDs: a.richFilters.LabelIDs,
			}
		case charmMultiSelectFilterStatus:
			states, err := a.cache.GetWorkflowStates(ctx, teamID)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			sort.SliceStable(states, func(i, j int) bool {
				return states[i].Position < states[j].Position
			})
			return charmMultiSelectLoadedMsg{
				title:       "Filter Statuses",
				action:      action,
				items:       charmStateFilterItems(states),
				selectedIDs: a.richFilters.StateIDs,
			}
		case charmMultiSelectFilterProject:
			projects, err := a.cache.GetProjects(ctx, teamID)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			return charmMultiSelectLoadedMsg{
				title:       "Filter Projects",
				action:      action,
				items:       charmProjectFilterItems(projects),
				selectedIDs: a.richFilters.ProjectIDs,
			}
		case charmMultiSelectFilterCycle:
			cycles, err := a.cache.GetCycles(ctx, teamID)
			if err != nil {
				return charmMultiSelectLoadedMsg{action: action, err: err}
			}
			sortCyclesForNavigation(cycles)
			return charmMultiSelectLoadedMsg{
				title:       "Filter Cycles",
				action:      action,
				items:       charmCycleFilterItems(cycles),
				selectedIDs: a.richFilters.CycleIDs,
			}
		default:
			return charmMultiSelectLoadedMsg{action: action, err: fmt.Errorf("unsupported filter action")}
		}
	}
}

// charmTeamFilterItems converts Linear teams into filter rows.
func charmTeamFilterItems(teams []linearapi.Team) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(teams))
	for _, team := range teams {
		items = append(items, charmMultiSelectItem{ID: team.ID, Label: teamFilterLabel(team)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

// teamLabelsByID returns command-palette team labels indexed by Linear team ID.
func teamLabelsByID(teams []linearapi.Team) map[string]string {
	labels := make(map[string]string, len(teams))
	for _, team := range teams {
		labels[team.ID] = teamFilterLabel(team)
	}
	return labels
}

// teamFilterLabel formats a team the same way in pickers and filter summaries.
func teamFilterLabel(team linearapi.Team) string {
	label := team.Name
	if team.Key != "" {
		label = fmt.Sprintf("%s (%s)", team.Name, team.Key)
	}
	return label
}

// charmUserFilterItems converts Linear users into filter rows.
func charmUserFilterItems(users []linearapi.User) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(users))
	for _, user := range users {
		label := formatUserDisplayName(user)
		if user.IsMe {
			label += " (me)"
		}
		items = append(items, charmMultiSelectItem{ID: user.ID, Label: label})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

// charmLabelFilterItems converts issue labels into filter rows.
func charmLabelFilterItems(labels []linearapi.IssueLabel) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(labels))
	for _, label := range labels {
		items = append(items, charmMultiSelectItem{ID: label.ID, Label: label.Name})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

// charmStateFilterItems converts workflow states into filter rows.
func charmStateFilterItems(states []linearapi.WorkflowState) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(states))
	for _, state := range states {
		items = append(items, charmMultiSelectItem{ID: state.ID, Label: state.Name})
	}
	return items
}

// cachedStatusPickerItems returns stale-or-fresh cached status options for instant picker display.
func (a CharmApp) cachedStatusPickerItems(teamID string) ([]charmPickerItem, bool) {
	states, ok := a.cache.PeekWorkflowStates(teamID)
	if !ok {
		return nil, false
	}
	return charmStatusPickerItems(states), true
}

// charmStatusPickerItems converts workflow states into status picker rows ordered by Linear position.
func charmStatusPickerItems(states []linearapi.WorkflowState) []charmPickerItem {
	states = append([]linearapi.WorkflowState(nil), states...)
	sort.SliceStable(states, func(i, j int) bool {
		return states[i].Position < states[j].Position
	})
	items := make([]charmPickerItem, 0, len(states))
	for _, state := range states {
		items = append(items, charmPickerItem{ID: state.ID, Label: state.Name})
	}
	return items
}

// charmProjectFilterItems converts projects into filter rows.
func charmProjectFilterItems(projects []linearapi.Project) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(projects))
	for _, project := range projects {
		items = append(items, charmMultiSelectItem{ID: project.ID, Label: project.Name})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

// charmCycleFilterItems converts cycles into filter rows.
func charmCycleFilterItems(cycles []linearapi.Cycle) []charmMultiSelectItem {
	items := make([]charmMultiSelectItem, 0, len(cycles))
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
		items = append(items, charmMultiSelectItem{ID: cycle.ID, Label: label})
	}
	return items
}

// charmMilestonePickerItems converts Linear milestones into picker rows.
func charmMilestonePickerItems(milestones []linearapi.ProjectMilestone) []charmPickerItem {
	items := make([]charmPickerItem, 0, len(milestones))
	for _, milestone := range milestones {
		label := milestone.Name
		if milestone.TargetDate != nil && *milestone.TargetDate != "" {
			label += " (" + *milestone.TargetDate + ")"
		}
		items = append(items, charmPickerItem{ID: milestone.ID, Label: label})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

func (a CharmApp) teamIDForIssue(issue *linearapi.Issue) string {
	if issue != nil && issue.TeamID != "" {
		return issue.TeamID
	}
	if a.selectedNavigation != nil && a.selectedNavigation.TeamID != "" {
		return a.selectedNavigation.TeamID
	}
	if len(a.richFilters.TeamIDs) == 1 {
		return a.richFilters.TeamIDs[0]
	}
	return ""
}

// projectIDForIssue returns the selected issue project ID, if any.
func (a CharmApp) projectIDForIssue(issue *linearapi.Issue) string {
	if issue != nil && issue.ProjectID != "" {
		return issue.ProjectID
	}
	if issue != nil && issue.ProjectMilestone != nil && issue.ProjectMilestone.ProjectID != "" {
		return issue.ProjectMilestone.ProjectID
	}
	if a.selectedNavigation != nil && a.selectedNavigation.IsProject {
		return a.selectedNavigation.ID
	}
	return ""
}

// customViewFromCurrentContext builds a new custom view from the active navigation context.
func (a CharmApp) customViewFromCurrentContext() config.CustomView {
	view := config.CustomView{
		Name:          "New View",
		SortPrimary:   config.CustomViewSortUpdatedAt,
		SortSecondary: config.CustomViewSortNone,
	}
	if a.selectedNavigation != nil {
		switch {
		case a.selectedNavigation.IsTeam:
			view.TeamID = a.selectedNavigation.TeamID
			view.Name = a.selectedNavigation.Text
		case a.selectedNavigation.IsProject:
			view.TeamID = a.selectedNavigation.TeamID
			view.ProjectID = a.selectedNavigation.ID
			view.Name = strings.TrimSpace(a.selectedNavigation.Text)
		case a.selectedNavigation.IsStatus:
			view.TeamID = a.selectedNavigation.TeamID
			view.StateID = a.selectedNavigation.StateID
			view.Name = strings.TrimSpace(a.selectedNavigation.Text)
		}
	}
	if len(a.richFilters.TeamIDs) == 1 {
		view.TeamID = a.richFilters.TeamIDs[0]
	}
	if len(a.richFilters.ProjectIDs) == 1 {
		view.ProjectID = a.richFilters.ProjectIDs[0]
	}
	if len(a.richFilters.StateIDs) == 1 {
		view.StateID = a.richFilters.StateIDs[0]
	}
	if len(a.richFilters.AssigneeIDs) == 1 {
		view.AssigneeID = a.richFilters.AssigneeIDs[0]
	}
	if len(a.richFilters.LabelIDs) == 1 {
		view.LabelID = a.richFilters.LabelIDs[0]
	}
	return view
}

// charmIssueLabelIDs returns stable current label IDs for an issue.
func charmIssueLabelIDs(issue linearapi.Issue) []string {
	ids := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		ids = append(ids, label.ID)
	}
	sort.Strings(ids)
	return ids
}

// selectedMultiSelectIDs returns sorted selected IDs from the active multi-select overlay.
func (a CharmApp) selectedMultiSelectIDs() []string {
	ids := make([]string, 0, len(a.multiSelected))
	for id := range a.multiSelected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a CharmApp) selectedTeamIDForCreate() string {
	if a.selectedNavigation != nil && a.selectedNavigation.TeamID != "" {
		return a.selectedNavigation.TeamID
	}
	if len(a.richFilters.TeamIDs) == 1 {
		return a.richFilters.TeamIDs[0]
	}
	if issue := a.currentIssue(); issue != nil && issue.TeamID != "" {
		return issue.TeamID
	}
	if len(a.teams) == 1 {
		return a.teams[0].ID
	}
	return ""
}

func charmPriorityItems() []charmPickerItem {
	return []charmPickerItem{
		{Label: "Urgent", Priority: 1},
		{Label: "High", Priority: 2},
		{Label: "Normal", Priority: 3},
		{Label: "Low", Priority: 4},
		{Label: "No priority", Priority: 0},
	}
}

// charmRelationTypeItems returns supported Linear relation types for the add-relation picker.
func charmRelationTypeItems() []charmPickerItem {
	items := make([]charmPickerItem, 0, len(issueRelationTypeLabels))
	for _, item := range issueRelationTypeLabels {
		items = append(items, charmPickerItem{ID: item.ID, Label: item.Label})
	}
	return items
}

// charmRelationPickerItems converts issue relations into picker rows.
func charmRelationPickerItems(relations []linearapi.IssueRelation) []charmPickerItem {
	items := make([]charmPickerItem, 0, len(relations))
	for _, relation := range relations {
		ref := relation.RelatedIssue
		if relation.Inverse {
			ref = relation.Issue
		}
		items = append(items, charmPickerItem{
			ID:    relation.ID,
			Label: relation.DisplayType() + " " + formatIssueReference(ref),
		})
	}
	return items
}

// charmAttachmentPickerItems converts attachments into picker rows.
func charmAttachmentPickerItems(attachments []linearapi.Attachment) []charmPickerItem {
	items := make([]charmPickerItem, 0, len(attachments))
	for _, attachment := range attachments {
		items = append(items, charmPickerItem{ID: attachment.ID, Label: charmAttachmentLabel(attachment)})
	}
	return items
}

// charmAttachmentLabel returns a compact, source-aware attachment label.
func charmAttachmentLabel(attachment linearapi.Attachment) string {
	label := strings.TrimSpace(attachment.Title)
	if label == "" {
		label = attachment.URL
	}
	if attachment.SourceType != "" {
		label += " (" + attachment.SourceType + ")"
	}
	return label
}

// charmAttachmentByID returns the attachment with the requested ID.
func charmAttachmentByID(attachments []linearapi.Attachment, attachmentID string) (linearapi.Attachment, bool) {
	for _, attachment := range attachments {
		if attachment.ID == attachmentID {
			return attachment, true
		}
	}
	return linearapi.Attachment{}, false
}

func (a CharmApp) loadInitialDataCmd() tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(a.cfg.LinearAPIKey) == "" {
			return charmInitialLoadedMsg{err: fmt.Errorf("Linear API key is not configured")}
		}
		ctx := context.Background()
		var user linearapi.User
		var teams []linearapi.Team
		var userErr, teamsErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			user, userErr = a.cache.GetCurrentUser(ctx)
		}()
		go func() {
			defer wg.Done()
			teams, teamsErr = a.cache.GetTeams(ctx)
		}()
		wg.Wait()
		if userErr != nil {
			return charmInitialLoadedMsg{err: userErr}
		}
		if teamsErr != nil {
			return charmInitialLoadedMsg{err: teamsErr}
		}
		a.currentUser = &user
		result, err := a.fetchIssues(ctx, true)
		if err != nil {
			return charmInitialLoadedMsg{err: err}
		}
		return charmInitialLoadedMsg{currentUser: &user, teams: teams, issues: result.issues, fromDisk: result.fromDisk}
	}
}

func (a CharmApp) loadIssuesCmd(targetIssueID string, useCache bool) tea.Cmd {
	return func() tea.Msg {
		result, err := a.fetchIssues(context.Background(), useCache)
		return charmIssuesLoadedMsg{issues: result.issues, targetIssueID: targetIssueID, fromDisk: result.fromDisk, err: err}
	}
}

// autoRefreshIssuesCmd emits a message once per hour to keep long-running data fresh.
func autoRefreshIssuesCmd() tea.Cmd {
	return tea.Tick(issueAutoRefreshInterval, func(time.Time) tea.Msg {
		return charmAutoRefreshMsg{}
	})
}

func (a CharmApp) loadIssueDetailsCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		if issue, ok := a.issueDetails.Get(issueID); ok {
			return charmIssueDetailsLoadedMsg{issue: issue}
		}
		issue, err := a.api.FetchIssueByID(context.Background(), issueID)
		return charmIssueDetailsLoadedMsg{issue: issue, err: err}
	}
}

func (a CharmApp) loadTeamMetadataCmd(teamID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var projects []linearapi.Project
		var states []linearapi.WorkflowState
		var cycles []linearapi.Cycle
		var projectsErr, statesErr, cyclesErr error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			projects, projectsErr = a.cache.GetProjects(ctx, teamID)
		}()
		go func() {
			defer wg.Done()
			states, statesErr = a.cache.GetWorkflowStates(ctx, teamID)
		}()
		go func() {
			defer wg.Done()
			cycles, cyclesErr = a.cache.GetCycles(ctx, teamID)
		}()
		wg.Wait()
		if projectsErr != nil {
			return charmTeamMetadataLoadedMsg{teamID: teamID, err: projectsErr}
		}
		if statesErr != nil {
			return charmTeamMetadataLoadedMsg{teamID: teamID, err: statesErr}
		}
		if cyclesErr != nil {
			return charmTeamMetadataLoadedMsg{teamID: teamID, err: cyclesErr}
		}
		return charmTeamMetadataLoadedMsg{
			teamID:   teamID,
			projects: projects,
			states:   states,
			cycles:   cycles,
		}
	}
}

type charmIssueFetchResult struct {
	issues   []linearapi.Issue
	fromDisk bool
}

func (a CharmApp) fetchIssues(ctx context.Context, useCache bool) (charmIssueFetchResult, error) {
	params := a.buildCharmFetchParams()
	result, err := a.fetchIssueQuery(ctx, params, useCache)
	if err != nil {
		return charmIssueFetchResult{}, err
	}
	if myParams, ok := a.buildAllMyIssuesFetchParams(params); ok && issueCacheKeyFromParams(myParams) != issueCacheKeyFromParams(params) {
		myResult, err := a.fetchIssueQuery(ctx, myParams, useCache)
		if err != nil {
			return charmIssueFetchResult{}, err
		}
		result.issues = mergeLinearIssues(result.issues, myResult.issues)
		result.fromDisk = result.fromDisk || myResult.fromDisk
	}
	return result, nil
}

// fetchIssueQuery loads one normalized issue query from memory, disk, or Linear.
func (a CharmApp) fetchIssueQuery(ctx context.Context, params linearapi.FetchIssuesParams, useCache bool) (charmIssueFetchResult, error) {
	cacheKey := issueCacheKeyFromParams(params)
	if useCache {
		if issues, ok := a.issueResults.Get(cacheKey); ok {
			logger.Debug("tui.charm: issue memory cache hit count=%d context=%s", len(issues), a.issueContextText())
			return charmIssueFetchResult{issues: issues}, nil
		}
		if issues, ok := a.issueDisk.Get(cacheKey); ok {
			a.issueResults.Set(cacheKey, issues)
			logger.Info("tui.charm: issue disk cache hit count=%d context=%s", len(issues), a.issueContextText())
			return charmIssueFetchResult{issues: issues, fromDisk: true}, nil
		}
	}
	started := time.Now()
	params.OnProgress = func(progress linearapi.IssueFetchProgress) {
		logger.Debug("tui.charm: issue fetch progress page=%d fetched=%d context=%s", progress.Page, progress.Fetched, a.issueContextText())
	}
	issues, err := a.api.FetchIssues(ctx, params)
	if err != nil {
		logger.ErrorWithErr(err, "tui.charm: failed to fetch issues")
		return charmIssueFetchResult{}, err
	}
	a.issueResults.Set(cacheKey, issues)
	if err := a.issueDisk.Set(cacheKey, issues); err != nil {
		logger.Warning("tui.charm: failed to persist issue disk cache: %v", err)
	}
	logger.Info("tui.charm: fetched issues count=%d duration=%s context=%s", len(issues), time.Since(started).Round(time.Millisecond), a.issueContextText())
	return charmIssueFetchResult{issues: issues}, nil
}

func (a CharmApp) buildCharmFetchParams() linearapi.FetchIssuesParams {
	params := linearapi.FetchIssuesParams{
		First:   effectiveIssueFetchPageSize(a.cfg.PageSize),
		Search:  strings.TrimSpace(a.searchQuery),
		OrderBy: charmOrderByForSort(a.activeIssueSortField()),
	}
	if view := a.selectedCustomView; view != nil {
		params.TeamID = view.TeamID
		params.ProjectID = view.ProjectID
		if view.StateID != "" {
			params.StateID = view.StateID
		} else if view.StateMode == config.CustomViewStateNotDone {
			params.StateTypes = []string{"backlog", "unstarted", "started"}
		} else if !a.cfg.IncludeCompleted {
			params.StateTypes = []string{"backlog", "unstarted", "started"}
		}
		params.AssigneeID = a.resolveCharmAssigneeID(view.AssigneeID)
		if view.LabelID != "" {
			params.LabelIDs = []string{view.LabelID}
		}
		params.DueWithinDays = view.DueWithinDays
		params.OrderBy = charmOrderByForSort(a.activeIssueSortField())
		a.applyRichFiltersToParams(&params)
		return params
	}
	if a.selectedNavigation != nil {
		switch {
		case a.selectedNavigation.IsTeam:
			params.TeamID = a.selectedNavigation.TeamID
		case a.selectedNavigation.IsProject:
			params.TeamID = a.selectedNavigation.TeamID
			params.ProjectID = a.selectedNavigation.ID
		case a.selectedNavigation.IsStatus:
			params.TeamID = a.selectedNavigation.TeamID
			params.StateID = a.selectedNavigation.StateID
		case a.selectedNavigation.IsCycle:
			params.TeamID = a.selectedNavigation.TeamID
			params.CycleID = a.selectedNavigation.CycleID
		}
	}
	if params.StateID == "" && len(params.StateTypes) == 0 && !a.cfg.IncludeCompleted {
		params.StateTypes = []string{"backlog", "unstarted", "started"}
	}
	a.applyRichFiltersToParams(&params)
	return params
}

// buildAllMyIssuesFetchParams returns an unsearched companion query for the My Issues panel.
func (a CharmApp) buildAllMyIssuesFetchParams(base linearapi.FetchIssuesParams) (linearapi.FetchIssuesParams, bool) {
	if !a.cfg.ShowMyIssues || a.currentUser == nil || strings.TrimSpace(a.currentUser.ID) == "" {
		return linearapi.FetchIssuesParams{}, false
	}
	base.Search = ""
	base.AssigneeID = a.currentUser.ID
	base.AssigneeIDs = nil
	return base, true
}

// mergeLinearIssues keeps issue order while adding missing issues from extras.
func mergeLinearIssues(primary []linearapi.Issue, extras []linearapi.Issue) []linearapi.Issue {
	seen := make(map[string]bool, len(primary)+len(extras))
	merged := make([]linearapi.Issue, 0, len(primary)+len(extras))
	for _, issue := range primary {
		if issue.ID == "" || seen[issue.ID] {
			continue
		}
		seen[issue.ID] = true
		merged = append(merged, issue)
	}
	for _, issue := range extras {
		if issue.ID == "" || seen[issue.ID] {
			continue
		}
		seen[issue.ID] = true
		merged = append(merged, issue)
	}
	return merged
}

// effectiveIssueFetchPageSize keeps Linear pagination responsive even if settings are tiny.
func effectiveIssueFetchPageSize(pageSize int) int {
	if pageSize < minIssueFetchPageSize {
		return minIssueFetchPageSize
	}
	return pageSize
}

func charmOrderByForSort(field SortField) string {
	switch field {
	case SortByCreatedAt:
		return string(SortByCreatedAt)
	case SortByPriority:
		return string(SortByPriority)
	case SortByUpdatedAt, SortByStatus, SortByOrder, "":
		return string(SortByUpdatedAt)
	default:
		return string(SortByUpdatedAt)
	}
}

func (a CharmApp) applyRichFiltersToParams(params *linearapi.FetchIssuesParams) {
	if params == nil {
		return
	}
	filters := a.richFilters
	if len(filters.TeamIDs) > 0 {
		params.TeamID = ""
		params.TeamIDs = append([]string(nil), filters.TeamIDs...)
		params.ProjectID = ""
		params.StateID = ""
		params.CycleID = ""
	}
	if len(filters.AssigneeIDs) > 0 {
		params.AssigneeID = ""
		params.AssigneeIDs = append([]string(nil), filters.AssigneeIDs...)
	}
	if len(filters.LabelIDs) > 0 {
		params.LabelIDs = append([]string(nil), filters.LabelIDs...)
	}
	if len(filters.StateIDs) > 0 {
		params.StateID = ""
		params.StateIDs = append([]string(nil), filters.StateIDs...)
		params.StateTypes = nil
	}
	if len(filters.ProjectIDs) > 0 {
		params.ProjectID = ""
		params.ProjectIDs = append([]string(nil), filters.ProjectIDs...)
	}
	if len(filters.CycleIDs) > 0 {
		params.CycleID = ""
		params.CycleIDs = append([]string(nil), filters.CycleIDs...)
	}
	if !filters.DueDate.Empty() {
		params.DueDate = filters.DueDate
		params.DueWithinDays = 0
	}
	if !filters.Estimate.Empty() {
		params.Estimate = filters.Estimate
	}
}

func (a CharmApp) resolveCharmAssigneeID(value string) string {
	if value == customViewAssigneeMe && a.currentUser != nil {
		return a.currentUser.ID
	}
	return value
}

func (a *CharmApp) rebuildNavigation() {
	selectedKey := charmNavigationKey(a.selectedNavigation)
	nodes := []*NavigationNode{{ID: "all", Text: "All Issues"}}
	for _, view := range a.customViews {
		nodes = append(nodes, &NavigationNode{
			ID:           view.ID,
			Text:         view.Name,
			IsCustomView: true,
			CustomViewID: view.ID,
		})
	}
	teams := append([]linearapi.Team(nil), a.teams...)
	sort.SliceStable(teams, func(i, j int) bool {
		return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
	})
	for _, team := range teams {
		nodes = append(nodes, &NavigationNode{
			ID:     team.ID,
			Text:   team.Name,
			TeamID: team.ID,
			IsTeam: true,
		})
		if a.loadingTeams[team.ID] {
			nodes = append(nodes, &NavigationNode{
				ID:     team.ID + ":loading",
				Text:   "  Loading metadata...",
				TeamID: team.ID,
			})
		} else if a.expandedTeams[team.ID] {
			nodes = append(nodes, a.teamChildren[team.ID]...)
		}
	}
	a.navigation = nodes
	if a.selectedNavigation == nil && len(nodes) > 0 {
		a.selectedNavigation = nodes[0]
	}
	if selectedKey != "" {
		for i, node := range nodes {
			if charmNavigationKey(node) == selectedKey {
				a.selectedNavigation = node
				a.navigationCursor = i
				break
			}
		}
	}
	if a.navigationCursor >= len(nodes) {
		a.navigationCursor = maxInt(0, len(nodes)-1)
	}
}

func charmTeamChildNodes(teamID string, projects []linearapi.Project, states []linearapi.WorkflowState, cycles []linearapi.Cycle) []*NavigationNode {
	nodes := []*NavigationNode{}
	sortCyclesForNavigation(cycles)
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
		nodes = append(nodes, &NavigationNode{
			ID:        cycle.ID,
			Text:      "  Cycle: " + label,
			TeamID:    teamID,
			IsCycle:   true,
			CycleID:   cycle.ID,
			CycleName: cycle.DisplayName(),
		})
	}
	sort.SliceStable(states, func(i, j int) bool {
		return states[i].Position < states[j].Position
	})
	for _, state := range states {
		nodes = append(nodes, &NavigationNode{
			ID:        state.ID,
			Text:      "  Status: " + state.Name,
			TeamID:    teamID,
			IsStatus:  true,
			StateID:   state.ID,
			StateName: state.Name,
		})
	}
	sort.SliceStable(projects, func(i, j int) bool {
		return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
	})
	for _, project := range projects {
		nodes = append(nodes, &NavigationNode{
			ID:        project.ID,
			Text:      "  " + project.Name,
			TeamID:    teamID,
			IsProject: true,
		})
	}
	return nodes
}

func charmNavigationKey(node *NavigationNode) string {
	if node == nil {
		return ""
	}
	switch {
	case node.IsCustomView:
		return "view:" + node.CustomViewID
	case node.IsProject:
		return "project:" + node.ID
	case node.IsStatus:
		return "status:" + node.StateID
	case node.IsCycle && node.CycleID != "":
		return "cycle:" + node.CycleID
	case node.IsTeam:
		return "team:" + node.TeamID
	default:
		return "node:" + node.ID
	}
}

func (a CharmApp) getCharmCustomView(id string) *config.CustomView {
	for i := range a.customViews {
		if a.customViews[i].ID == id {
			return &a.customViews[i]
		}
	}
	return nil
}

func (a *CharmApp) setIssues(issues []linearapi.Issue, targetIssueID string) {
	a.issues = append([]linearapi.Issue(nil), issues...)
	a.sortCharmIssues()
	a.rebuildIssueTables(targetIssueID)
}

// applyIssueStatus updates every local copy of an issue status before rebuilding visible rows.
func (a *CharmApp) applyIssueStatus(issueID string, stateID string, stateName string) {
	if issueID == "" {
		return
	}
	updated := a.issueSnapshot(issueID)
	if updated.ID != "" {
		updated.StateID = stateID
		updated.State = stateName
	}
	for i := range a.issues {
		if a.issues[i].ID == issueID {
			a.issues[i].StateID = stateID
			a.issues[i].State = stateName
		}
		for childIndex := range a.issues[i].Children {
			if a.issues[i].Children[childIndex].ID == issueID {
				a.issues[i].Children[childIndex].StateID = stateID
				a.issues[i].Children[childIndex].State = stateName
			}
		}
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.selectedIssue.StateID = stateID
		a.selectedIssue.State = stateName
	}
	if cached, ok := a.issueDetails.Get(issueID); ok {
		cached.StateID = stateID
		cached.State = stateName
		a.issueDetails.Set(cached)
	}
	if updated.ID != "" && !a.issueMatchesCurrentStatusScope(updated) {
		a.removeIssueLocally(issueID)
		return
	}
	a.sortCharmIssues()
	a.rebuildIssueTables(issueID)
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.updateDetailsContent()
	}
}

// restoreIssueForRollback puts a full issue snapshot back into local state.
func (a *CharmApp) restoreIssueForRollback(issue linearapi.Issue) {
	if issue.ID == "" {
		return
	}
	replaced := false
	for i := range a.issues {
		if a.issues[i].ID == issue.ID {
			a.issues[i] = issue
			replaced = true
			break
		}
	}
	if !replaced {
		a.issues = append(a.issues, issue)
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
		a.selectedIssue = &issue
	}
	a.issueDetails.Set(issue)
	a.sortCharmIssues()
	a.rebuildIssueTables(issue.ID)
}

// applyIssueSnapshot replaces local issue copies with a complete optimistic snapshot.
func (a *CharmApp) applyIssueSnapshot(issue linearapi.Issue) {
	if issue.ID == "" {
		return
	}
	replaced := false
	for i := range a.issues {
		if a.issues[i].ID == issue.ID {
			a.issues[i] = issue
			replaced = true
		}
		for childIndex := range a.issues[i].Children {
			if a.issues[i].Children[childIndex].ID == issue.ID {
				a.issues[i].Children[childIndex].Title = issue.Title
				a.issues[i].Children[childIndex].State = issue.State
				a.issues[i].Children[childIndex].StateID = issue.StateID
			}
		}
	}
	if !replaced {
		a.issues = append(a.issues, issue)
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
		a.selectedIssue = &issue
	}
	a.issueDetails.Set(issue)
	if !a.issueMatchesCurrentMutableScope(issue) {
		a.removeIssueLocally(issue.ID)
		return
	}
	a.sortCharmIssues()
	a.rebuildIssueTables(issue.ID)
	if a.selectedIssue != nil && a.selectedIssue.ID == issue.ID {
		a.updateDetailsContent()
	}
}

// applyCreatedIssue inserts a freshly created issue locally and selects it when visible.
func (a *CharmApp) applyCreatedIssue(issue linearapi.Issue) {
	if issue.ID == "" {
		return
	}
	a.applyIssueSnapshot(issue)
	a.cacheCurrentIssueList()
}

// cacheCurrentIssueList writes the current visible list through to the active issue cache key.
func (a CharmApp) cacheCurrentIssueList() {
	cacheKey := issueCacheKeyFromParams(a.buildCharmFetchParams())
	a.issueResults.Set(cacheKey, a.issues)
	if err := a.issueDisk.Set(cacheKey, a.issues); err != nil {
		logger.Warning("tui.charm: failed to persist updated issue disk cache: %v", err)
	}
}

// removeIssueLocally drops an issue from the visible local result set without touching Linear.
func (a *CharmApp) removeIssueLocally(issueID string) {
	if issueID == "" {
		return
	}
	filtered := a.issues[:0]
	for _, issue := range a.issues {
		if issue.ID != issueID {
			filtered = append(filtered, issue)
		}
	}
	a.issues = filtered
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.selectedIssue = nil
	}
	a.sortCharmIssues()
	a.rebuildIssueTables("")
}

// issueSnapshot returns the best local copy of an issue before an optimistic edit.
func (a CharmApp) issueSnapshot(issueID string) linearapi.Issue {
	for _, issue := range a.issues {
		if issue.ID == issueID {
			return issue
		}
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		return *a.selectedIssue
	}
	if cached, ok := a.issueDetails.Get(issueID); ok {
		return cached
	}
	return linearapi.Issue{}
}

// issueMatchesCurrentStatusScope checks only state filters that can be affected by a status edit.
func (a CharmApp) issueMatchesCurrentStatusScope(issue linearapi.Issue) bool {
	params := a.buildCharmFetchParams()
	if params.StateID != "" && issue.StateID != params.StateID {
		return false
	}
	if len(params.StateIDs) > 0 && !containsString(params.StateIDs, issue.StateID) {
		return false
	}
	if len(params.StateTypes) > 0 && !issueMatchesStateTypes(issue, params.StateTypes) {
		return false
	}
	return true
}

// issueMatchesCurrentMutableScope checks local filters affected by optimistic field edits.
func (a CharmApp) issueMatchesCurrentMutableScope(issue linearapi.Issue) bool {
	if !a.issueMatchesCurrentStatusScope(issue) {
		return false
	}
	params := a.buildCharmFetchParams()
	if search := strings.TrimSpace(params.Search); search != "" && !issueMatchesSearch(issue, search) {
		return false
	}
	if params.AssigneeID != "" && issue.AssigneeID != params.AssigneeID {
		return false
	}
	if len(params.AssigneeIDs) > 0 && !containsString(params.AssigneeIDs, issue.AssigneeID) {
		return false
	}
	if params.CycleID != "" && issueCycleID(issue) != params.CycleID {
		return false
	}
	if len(params.CycleIDs) > 0 && !containsString(params.CycleIDs, issueCycleID(issue)) {
		return false
	}
	if params.ProjectMilestoneID != "" && issueMilestoneID(issue) != params.ProjectMilestoneID {
		return false
	}
	if len(params.LabelIDs) > 0 && !issueHasAllLabels(issue, params.LabelIDs) {
		return false
	}
	if !params.DueDate.Empty() && !dateFilterMatches(issue.DueDate, params.DueDate) {
		return false
	}
	if !params.Estimate.Empty() && !numberFilterMatches(issue.Estimate, params.Estimate) {
		return false
	}
	return true
}

// issueMatchesSearch approximates Linear text search for immediate local optimistic edits.
func issueMatchesSearch(issue linearapi.Issue, search string) bool {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{issue.Identifier, issue.Title, issue.Description}, " "))
	return strings.Contains(haystack, needle)
}

func issueCycleID(issue linearapi.Issue) string {
	if issue.Cycle == nil {
		return ""
	}
	return issue.Cycle.ID
}

func issueMilestoneID(issue linearapi.Issue) string {
	if issue.ProjectMilestone == nil {
		return ""
	}
	return issue.ProjectMilestone.ID
}

func issueHasAllLabels(issue linearapi.Issue, labelIDs []string) bool {
	available := make(map[string]bool, len(issue.Labels))
	for _, label := range issue.Labels {
		available[label.ID] = true
	}
	for _, id := range labelIDs {
		if !available[id] {
			return false
		}
	}
	return true
}

// issueMatchesStateTypes approximates Linear workflow state types from the loaded state name.
func issueMatchesStateTypes(issue linearapi.Issue, stateTypes []string) bool {
	kind := issueStatusKind(issue.State)
	for _, stateType := range stateTypes {
		switch strings.ToLower(strings.TrimSpace(stateType)) {
		case "backlog", "unstarted":
			if kind == "todo" {
				return true
			}
		case "started":
			if kind == "progress" {
				return true
			}
		case "completed":
			if kind == "done" {
				return true
			}
		case "canceled":
			if kind == "canceled" {
				return true
			}
		}
	}
	return false
}

// containsString reports whether values contains target exactly.
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// dateFilterMatches applies the active Linear date filter to a local YYYY-MM-DD value.
func dateFilterMatches(value *string, filter linearapi.DateFilter) bool {
	date := ""
	if value != nil {
		date = strings.TrimSpace(*value)
	}
	if filter.Null != nil {
		isNull := date == ""
		if isNull != *filter.Null {
			return false
		}
	}
	if filter.Eq != "" && date != filter.Eq {
		return false
	}
	if filter.GT != "" && !(date > filter.GT) {
		return false
	}
	if filter.GTE != "" && !(date >= filter.GTE) {
		return false
	}
	if filter.LT != "" && !(date < filter.LT) {
		return false
	}
	if filter.LTE != "" && !(date <= filter.LTE) {
		return false
	}
	return date != "" || filter.Null != nil
}

// numberFilterMatches applies the active Linear number filter to a local numeric value.
func numberFilterMatches(value *float64, filter linearapi.NumberFilter) bool {
	if filter.Null != nil {
		isNull := value == nil
		if isNull != *filter.Null {
			return false
		}
	}
	if value == nil {
		return filter.Null != nil
	}
	number := *value
	if filter.Eq != nil && number != *filter.Eq {
		return false
	}
	if filter.GT != nil && !(number > *filter.GT) {
		return false
	}
	if filter.GTE != nil && !(number >= *filter.GTE) {
		return false
	}
	if filter.LT != nil && !(number < *filter.LT) {
		return false
	}
	if filter.LTE != nil && !(number <= *filter.LTE) {
		return false
	}
	return true
}

// applyIssuePriority updates every local copy of an issue priority before rebuilding visible rows.
func (a *CharmApp) applyIssuePriority(issueID string, priority int) {
	if issueID == "" {
		return
	}
	for i := range a.issues {
		if a.issues[i].ID == issueID {
			a.issues[i].Priority = priority
		}
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.selectedIssue.Priority = priority
	}
	if cached, ok := a.issueDetails.Get(issueID); ok {
		cached.Priority = priority
		a.issueDetails.Set(cached)
	}
	a.sortCharmIssues()
	a.rebuildIssueTables(issueID)
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.updateDetailsContent()
	}
}

// applyIssueDescription updates local issue description copies and rerenders details immediately.
func (a *CharmApp) applyIssueDescription(issueID string, description string) {
	if issueID == "" {
		return
	}
	for i := range a.issues {
		if a.issues[i].ID == issueID {
			a.issues[i].Description = description
		}
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.selectedIssue.Description = description
	}
	if cached, ok := a.issueDetails.Get(issueID); ok {
		cached.Description = description
		a.issueDetails.Set(cached)
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.updateDetailsContent()
	}
}

// applyIssueDueDate updates local issue due-date copies and rebuilds visible rows.
func (a *CharmApp) applyIssueDueDate(issueID string, dueDate *string) {
	if issueID == "" {
		return
	}
	for i := range a.issues {
		if a.issues[i].ID == issueID {
			a.issues[i].DueDate = cloneStringPointer(dueDate)
		}
	}
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.selectedIssue.DueDate = cloneStringPointer(dueDate)
	}
	if cached, ok := a.issueDetails.Get(issueID); ok {
		cached.DueDate = cloneStringPointer(dueDate)
		a.issueDetails.Set(cached)
	}
	a.sortCharmIssues()
	a.rebuildIssueTables(issueID)
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.updateDetailsContent()
	}
}

// activeIssueSortField returns the current user-selected sort, or the view/default sort.
func (a CharmApp) activeIssueSortField() SortField {
	if a.sortOverride != "" {
		return a.sortOverride
	}
	if a.selectedCustomView != nil {
		return sortFieldFromCustomView(a.selectedCustomView.SortPrimary)
	}
	return SortByUpdatedAt
}

// sortFieldFromCustomView maps persisted custom-view fields into runtime issue sorting.
func sortFieldFromCustomView(field config.CustomViewSortField) SortField {
	switch field {
	case config.CustomViewSortCreatedAt:
		return SortByCreatedAt
	case config.CustomViewSortPriority:
		return SortByPriority
	case config.CustomViewSortStatus:
		return SortByStatus
	case config.CustomViewSortUpdatedAt, config.CustomViewSortNone:
		return SortByUpdatedAt
	default:
		return SortByUpdatedAt
	}
}

func (a *CharmApp) sortCharmIssues() {
	if a.selectedCustomView != nil && a.sortOverride == "" {
		a.sortCharmIssuesForCustomView(*a.selectedCustomView)
		return
	}
	field := a.activeIssueSortField()
	sort.SliceStable(a.issues, func(i, j int) bool {
		cmp := compareIssueSort(field, a.issues[i], a.issues[j])
		if cmp != 0 {
			return cmp < 0
		}
		return strings.Compare(a.issues[i].Identifier, a.issues[j].Identifier) < 0
	})
}

// sortCharmIssuesForCustomView applies saved primary and secondary custom-view sorts.
func (a *CharmApp) sortCharmIssuesForCustomView(view config.CustomView) {
	primary := defaultCustomViewSort(view.SortPrimary, config.CustomViewSortUpdatedAt)
	secondary := view.SortSecondary
	sort.SliceStable(a.issues, func(i, j int) bool {
		if cmp := compareCustomSort(primary, a.issues[i], a.issues[j], nil); cmp != 0 {
			return cmp < 0
		}
		if secondary != config.CustomViewSortNone {
			if cmp := compareCustomSort(secondary, a.issues[i], a.issues[j], nil); cmp != 0 {
				return cmp < 0
			}
		}
		return strings.Compare(a.issues[i].Identifier, a.issues[j].Identifier) < 0
	})
}

// compareIssueSort compares two issues using the selected board/list sort.
func compareIssueSort(field SortField, left linearapi.Issue, right linearapi.Issue) int {
	switch field {
	case SortByOrder:
		return compareFloats(left.SortOrder, right.SortOrder)
	case SortByCreatedAt:
		return compareTimeDesc(left.CreatedAt, right.CreatedAt)
	case SortByPriority:
		return compareCustomSort(config.CustomViewSortPriority, left, right, nil)
	case SortByStatus:
		return compareCustomSort(config.CustomViewSortStatus, left, right, nil)
	case SortByUpdatedAt, "":
		return compareTimeDesc(left.UpdatedAt, right.UpdatedAt)
	default:
		return compareTimeDesc(left.UpdatedAt, right.UpdatedAt)
	}
}

func (a *CharmApp) rebuildIssueTables(targetIssueID string) {
	currentUserID := ""
	if a.currentUser != nil {
		currentUserID = a.currentUser.ID
	}
	myIssues, otherIssues := splitIssuesByAssignee(a.issues, currentUserID)
	a.myRows, a.myIssueMap = BuildIssueRows(myIssues, a.expanded)
	a.otherRows, a.otherIssueMap = BuildIssueRows(otherIssues, a.expanded)

	a.myTable.SetRows(a.charmRowsFromIssueRows(a.myRows, a.myIssueMap))
	a.otherTable.SetRows(a.charmRowsFromIssueRows(a.otherRows, a.otherIssueMap))
	if targetIssueID != "" {
		a.selectIssueByID(targetIssueID)
	} else {
		a.selectIssueFromActiveTable()
	}
	a.applyComponentFocus()
}

// charmRowsFromIssueRows renders issue rows with semantic color cues for scanning.
func (a CharmApp) charmRowsFromIssueRows(rows []IssueRow, issues map[string]*linearapi.Issue) []table.Row {
	result := make([]table.Row, 0, len(rows))
	for _, row := range rows {
		issue := issues[row.IssueID]
		if issue == nil {
			continue
		}
		indent := strings.Repeat("  ", row.Level)
		prefix := " "
		if len(issue.Children) > 0 {
			if row.IsExpanded {
				prefix = IconExpanded
			} else {
				prefix = IconCollapsed
			}
		}
		result = append(result, table.Row{
			indent + prefix + " " + a.styles.issueIdentifier.Render(issue.Identifier),
			a.renderIssueStatus(issue.State),
			a.renderIssuePriority(issue.Priority),
			issue.Title,
			emptyDash(issue.Assignee),
			a.renderDueDate(issue.DueDate),
		})
	}
	return result
}

// renderIssueTable draws issue rows with a clean full-row selection highlight.
func (a CharmApp) renderIssueTable(rows []IssueRow, issues map[string]*linearapi.Issue, model table.Model) string {
	columns := issueTableColumns()
	headerCells := make([]string, 0, len(columns))
	for _, col := range columns {
		headerCells = append(headerCells, a.renderIssueCell(col.Title, col.Width, a.styles.subtle.Bold(true)))
	}
	lines := []string{strings.Join(headerCells, "  ")}
	start, end := issueTableVisibleRange(len(rows), model.Cursor(), model.Height())
	cursor := clamp(model.Cursor(), 0, maxInt(0, len(rows)-1))
	for index := start; index < end; index++ {
		line := a.renderIssueTableRow(rows[index], issues, columns, index == cursor, model.Width())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// issueTableVisibleRange mirrors the Bubbles table viewport range for cursor-centered scrolling.
func issueTableVisibleRange(rowCount int, cursor int, height int) (int, int) {
	if rowCount <= 0 {
		return 0, 0
	}
	height = maxInt(1, height)
	cursor = clamp(cursor, 0, rowCount-1)
	start := clamp(cursor-height, 0, cursor)
	end := minInt(rowCount, maxInt(cursor+height, cursor+1))
	return start, end
}

// issueTableColumns centralizes column widths for both Bubbles navigation state and custom rendering.
func issueTableColumns() []table.Column {
	return []table.Column{
		{Title: "ID", Width: 12},
		{Title: "State", Width: 16},
		{Title: "Priority", Width: 10},
		{Title: "Title", Width: 42},
		{Title: "Assignee", Width: 14},
		{Title: "Due", Width: 10},
	}
}

// renderIssueTableRow renders one issue row; selected rows avoid nested ANSI resets so the background spans the row.
func (a CharmApp) renderIssueTableRow(row IssueRow, issues map[string]*linearapi.Issue, columns []table.Column, selected bool, width int) string {
	issue := issues[row.IssueID]
	if issue == nil {
		return ""
	}
	cells := a.issueTableCells(row, *issue, selected)
	style := a.styles.columnBody
	if selected {
		style = lipgloss.NewStyle().
			Foreground(a.styles.palette.selectedText).
			Background(a.styles.palette.selected).
			Bold(true)
	} else if isIssueDueToday(issue) {
		style = a.styles.dueTodayRow
	} else if isIssueOverdue(issue) {
		style = a.styles.dueOverdueRow
	}
	rendered := make([]string, 0, len(columns))
	for i, col := range columns {
		value := ""
		if i < len(cells) {
			value = cells[i]
		}
		if selected {
			rendered = append(rendered, padANSIWidth(ansi.Truncate(value, col.Width, "…"), col.Width))
		} else {
			rendered = append(rendered, a.renderIssueCell(value, col.Width, style))
		}
	}
	line := strings.Join(rendered, "  ")
	if selected {
		line = padANSIWidth(line, maxInt(1, width))
		return lipgloss.NewStyle().
			Foreground(a.styles.palette.selectedText).
			Background(a.styles.palette.selected).
			Bold(true).
			Render(line)
	}
	return line
}

// issueTableCells returns display cells for a row, preserving colors only when the row is not selected.
func (a CharmApp) issueTableCells(row IssueRow, issue linearapi.Issue, selected bool) []string {
	indent := strings.Repeat("  ", row.Level)
	prefix := " "
	if len(issue.Children) > 0 {
		if row.IsExpanded {
			prefix = IconExpanded
		} else {
			prefix = IconCollapsed
		}
	}
	identifier := indent + strings.TrimSpace(prefix+" "+issue.Identifier)
	state := plainIssueStatus(issue.State)
	priority := plainIssuePriority(issue.Priority)
	due := plainDueDateLabel(issue.DueDate)
	if !selected {
		identifier = indent + strings.TrimSpace(prefix+" "+a.styles.issueIdentifier.Render(issue.Identifier))
		state = a.renderIssueStatus(issue.State)
		priority = a.renderIssuePriority(issue.Priority)
		due = a.renderDueDate(issue.DueDate)
	}
	return []string{
		identifier,
		state,
		priority,
		issue.Title,
		emptyDash(issue.Assignee),
		due,
	}
}

// renderDueDate returns a compact due-date marker with stronger accents for urgent dates.
func (a CharmApp) renderDueDate(dueDate *string) string {
	if isDueDateToday(dueDate) {
		return a.styles.dueToday.Render("Today")
	}
	if isDueDateOverdue(dueDate) {
		return a.styles.dueOverdue.Render(plainDueDateLabel(dueDate))
	}
	return a.styles.subtle.Render(plainDueDateLabel(dueDate))
}

// plainDueDateLabel returns unstyled due-date text for selected rows and tests.
func plainDueDateLabel(dueDate *string) string {
	if isDueDateToday(dueDate) {
		return "Today"
	}
	return formatDueDate(dueDate)
}

// renderIssueCell truncates and pads a cell while preserving ANSI escape sequences.
func (a CharmApp) renderIssueCell(value string, width int, style lipgloss.Style) string {
	cell := ansi.Truncate(value, width, "…")
	cell = padANSIWidth(cell, width)
	return style.Width(width).MaxWidth(width).Inline(true).Render(cell)
}

// renderIssueStatus returns a compact colored status marker plus label.
func (a CharmApp) renderIssueStatus(state string) string {
	label := emptyDash(state)
	icon := "○"
	style := a.styles.statusTodo
	switch issueStatusKind(state) {
	case "progress":
		icon = "●"
		style = a.styles.statusInProgress
	case "done":
		icon = "✓"
		style = a.styles.statusDone
	case "canceled":
		icon = "×"
		style = a.styles.statusCanceled
	}
	return style.Render(icon) + " " + label
}

// plainIssueStatus returns an unstyled status label for selected rows.
func plainIssueStatus(state string) string {
	icon := "○"
	switch issueStatusKind(state) {
	case "progress":
		icon = "●"
	case "done":
		icon = "✓"
	case "canceled":
		icon = "×"
	}
	return icon + " " + emptyDash(state)
}

// renderIssuePriority returns a compact colored priority marker plus label.
func (a CharmApp) renderIssuePriority(priority int) string {
	switch priority {
	case 1:
		return a.styles.priorityUrgent.Render("!! Urgent")
	case 2:
		return a.styles.priorityHigh.Render("! High")
	case 3:
		return a.styles.priorityNormal.Render("• Normal")
	case 4:
		return a.styles.priorityLow.Render("· Low")
	default:
		return a.styles.priorityNone.Render("-")
	}
}

// plainIssuePriority returns an unstyled priority label for selected rows.
func plainIssuePriority(priority int) string {
	switch priority {
	case 1:
		return "!! Urgent"
	case 2:
		return "! High"
	case 3:
		return "• Normal"
	case 4:
		return "· Low"
	default:
		return "-"
	}
}

// issueStatusKind maps Linear status names to a small set of visual categories.
func issueStatusKind(state string) string {
	state = strings.ToLower(strings.TrimSpace(state))
	switch {
	case strings.Contains(state, "cancel"):
		return "canceled"
	case strings.Contains(state, "done"), strings.Contains(state, "complete"), strings.Contains(state, "closed"), strings.Contains(state, "shipped"):
		return "done"
	case strings.Contains(state, "progress"), strings.Contains(state, "review"), strings.Contains(state, "started"):
		return "progress"
	default:
		return "todo"
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (a *CharmApp) selectIssueByID(issueID string) {
	for i, row := range a.myRows {
		if row.IssueID == issueID {
			a.activeSection = IssuesSectionMy
			a.myTable.SetCursor(i)
			a.selectedIssue = a.myIssueMap[issueID]
			a.updateDetailsContent()
			return
		}
	}
	for i, row := range a.otherRows {
		if row.IssueID == issueID {
			a.activeSection = IssuesSectionOther
			a.otherTable.SetCursor(i)
			a.selectedIssue = a.otherIssueMap[issueID]
			a.updateDetailsContent()
			return
		}
	}
	a.selectIssueFromActiveTable()
}

func (a *CharmApp) selectIssueFromActiveTable() {
	issue := a.currentIssue()
	if issue == nil {
		a.selectedIssue = nil
		a.details.SetContent("No issue selected")
		return
	}
	a.selectedIssue = issue
	a.updateDetailsContent()
}

func (a CharmApp) currentIssue() *linearapi.Issue {
	if a.activeSection == IssuesSectionMy && len(a.myRows) > 0 {
		cursor := clamp(a.myTable.Cursor(), 0, len(a.myRows)-1)
		return a.myIssueMap[a.myRows[cursor].IssueID]
	}
	if len(a.otherRows) > 0 {
		cursor := clamp(a.otherTable.Cursor(), 0, len(a.otherRows)-1)
		return a.otherIssueMap[a.otherRows[cursor].IssueID]
	}
	return nil
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (a *CharmApp) updateDetailsContent() {
	if a.selectedIssue == nil {
		a.details.SetContent("No issue selected")
		return
	}
	issue := a.selectedIssue
	width := maxInt(24, a.details.Width()-2)
	heading := lipgloss.JoinHorizontal(
		lipgloss.Center,
		a.styles.issueIdentifier.Render(issue.Identifier),
		" ",
		a.styles.title.Render(issue.Title),
	)
	meta := strings.Join([]string{
		a.renderIssueStatus(issue.State),
		"Assignee: " + emptyDash(issue.Assignee),
		a.renderIssuePriority(issue.Priority),
		"Created: " + formatTime(issue.CreatedAt),
		"Edited: " + formatTime(issue.UpdatedAt),
	}, "  ")
	lines := []string{
		heading,
		meta,
	}
	description := strings.TrimSpace(issue.Description)
	if description != "" {
		lines = append(lines, "", a.styles.subtle.Render("Description"), renderCharmMarkdown(description, width))
	} else {
		lines = append(lines, "", "-")
	}
	if len(issue.Comments) > 0 {
		lines = append(lines, "", a.styles.subtle.Render("Comments"))
		for _, comment := range issue.Comments {
			body := strings.TrimSpace(comment.Body)
			meta := emptyDash(comment.Author.DisplayName)
			if !comment.CreatedAt.IsZero() {
				meta += "  " + a.styles.subtle.Render(formatTime(comment.CreatedAt))
			}
			lines = append(lines, a.styles.title.Render(meta))
			if body != "" {
				lines = append(lines, renderCharmMarkdown(body, width))
			} else {
				lines = append(lines, "-")
			}
			lines = append(lines, "")
		}
	}
	if len(issue.Relations) > 0 {
		lines = append(lines, "", a.styles.subtle.Render("Relations"))
		for _, relation := range issue.Relations {
			ref := relation.RelatedIssue
			if relation.Inverse {
				ref = relation.Issue
			}
			lines = append(lines, relation.DisplayType()+" "+formatIssueReference(ref))
		}
	}
	if len(issue.Subscribers) > 0 {
		subscribers := make([]string, 0, len(issue.Subscribers))
		for _, subscriber := range issue.Subscribers {
			subscribers = append(subscribers, formatUserDisplayName(subscriber))
		}
		lines = append(lines, "", a.styles.subtle.Render("Subscribers"), strings.Join(subscribers, ", "))
	}
	if len(issue.Attachments) > 0 {
		lines = append(lines, "", a.styles.subtle.Render("Attachments"))
		for _, attachment := range issue.Attachments {
			lines = append(lines, charmAttachmentLabel(attachment)+" "+attachment.URL)
		}
	}
	a.details.SetContent(strings.Join(lines, "\n"))
}

// renderCharmMarkdown renders Linear markdown through Charm's terminal renderer.
func renderCharmMarkdown(markdown string, width int) string {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return ""
	}
	markdown = compactMarkdownURLs(markdown, width)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(maxInt(24, width-2)),
	)
	if err != nil {
		return markdown
	}
	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}
	return strings.TrimSpace(rendered)
}

// compactMarkdownURLs shortens long display URLs before terminal wrapping mangles them.
func compactMarkdownURLs(markdown string, width int) string {
	maxLabelWidth := clamp(width-8, 24, 48)
	lines := strings.Split(markdown, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = compactBareURLs(line, maxLabelWidth)
	}
	return strings.Join(lines, "\n")
}

// compactBareURLs replaces long bare URLs with readable labels while preserving surrounding text.
func compactBareURLs(line string, maxLabelWidth int) string {
	matches := markdownURLPattern.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		if isMarkdownLinkDestination(line, start) {
			continue
		}
		raw := trimURLTrailingPunctuation(line[start:end])
		trimmedEnd := start + len(raw)
		prefix := line[last:start]
		label := compactURLLabel(raw, maxLabelWidth)
		if shouldBreakBeforeURLLabel(line, start, label, maxLabelWidth) {
			out.WriteString(strings.TrimRight(prefix, " \t"))
			out.WriteString("\n  ")
		} else {
			out.WriteString(prefix)
		}
		out.WriteString(label)
		out.WriteString(line[trimmedEnd:end])
		last = end
	}
	if last == 0 {
		return line
	}
	out.WriteString(line[last:])
	return out.String()
}

// shouldBreakBeforeURLLabel keeps compact links from being wrapped through hostnames.
func shouldBreakBeforeURLLabel(line string, start int, label string, maxWidth int) bool {
	prefix := strings.TrimSpace(line[:start])
	if prefix == "" {
		return false
	}
	return len(prefix)+1+len(label) > maxWidth
}

// isMarkdownLinkDestination reports whether a URL is already inside a markdown link target.
func isMarkdownLinkDestination(line string, start int) bool {
	return start >= 2 && line[start-1] == '(' && line[start-2] == ']'
}

// compactURLLabel turns a long URL into a stable host/path label for narrow panes.
func compactURLLabel(raw string, maxWidth int) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return truncateMiddle(raw, maxWidth)
	}
	label := parsed.Host
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		label += parsed.EscapedPath()
	}
	if len(label) > maxWidth {
		label = compactURLPathLabel(parsed)
	}
	return truncateMiddle(label, maxWidth)
}

// compactURLPathLabel preserves the host and useful path anchors for long URLs.
func compactURLPathLabel(parsed *url.URL) string {
	parts := strings.FieldsFunc(parsed.EscapedPath(), func(r rune) bool {
		return r == '/'
	})
	if len(parts) == 0 {
		return parsed.Host
	}
	if len(parts) == 1 {
		return parsed.Host + "/" + parts[0]
	}
	return parsed.Host + "/" + parts[0] + "/…/" + parts[len(parts)-1]
}

// trimURLTrailingPunctuation keeps sentence punctuation outside compacted URL labels.
func trimURLTrailingPunctuation(raw string) string {
	return strings.TrimRight(raw, ".,;:")
}

// truncateMiddle keeps both ends of a long token visible in compact terminal layouts.
func truncateMiddle(value string, maxWidth int) string {
	if maxWidth <= 1 || len(value) <= maxWidth {
		return value
	}
	left := maxInt(1, (maxWidth-1)/2)
	right := maxInt(1, maxWidth-1-left)
	if left+right >= len(value) {
		return value
	}
	return value[:left] + "…" + value[len(value)-right:]
}

// calendarEventTimeLabel returns gc-style display time for an embedded event.
func calendarEventTimeLabel(event calendar.Event) string {
	if event.AllDay {
		return "all day"
	}
	end := event.DisplayEnd()
	if end.IsZero() {
		return event.Start.Format("15:04")
	}
	if !calendar.SameDay(event.Start, end) {
		return fmt.Sprintf("%s-%s", event.Start.Format("Mon 15:04"), end.Format("Mon 15:04"))
	}
	return fmt.Sprintf("%s-%s", event.Start.Format("15:04"), end.Format("15:04"))
}

// calendarEventMetaLine returns secondary context for the embedded event row.
func calendarEventMetaLine(event calendar.Event) string {
	var parts []string
	if event.Calendar != "" {
		parts = append(parts, oneLineText(event.Calendar))
	}
	if event.Location != "" {
		parts = append(parts, oneLineText(event.Location))
	}
	if event.Organizer != "" {
		parts = append(parts, "with "+oneLineText(event.Organizer))
	}
	return strings.Join(parts, " / ")
}

// calendarEventCountLabel returns a compact count label for the selected day.
func calendarEventCountLabel(count int) string {
	if count == 1 {
		return "1 event"
	}
	return fmt.Sprintf("%d events", count)
}

// oneLineText collapses remote calendar text so it cannot break compact rows.
func oneLineText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// stripCalendarHTML removes simple HTML markup from Google Calendar descriptions.
func stripCalendarHTML(value string) string {
	text := calendarHTMLTagPattern.ReplaceAllString(value, " ")
	return html.UnescapeString(oneLineText(text))
}

// wrapText wraps plain text to a terminal width using whitespace boundaries.
func wrapText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if ansi.StringWidth(line)+1+ansi.StringWidth(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// truncatePlain truncates unstyled text to a terminal width.
func truncatePlain(value string, width int) string {
	return ansi.Truncate(value, width, "…")
}

// truncateANSI truncates styled text to a terminal width.
func truncateANSI(value string, width int) string {
	return ansi.Truncate(value, width, "…")
}

// truncateLines keeps a rendered block within a fixed number of rows.
func truncateLines(value string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxLines {
		return value
	}
	return strings.Join(lines[:maxLines], "\n")
}

// applyWidgetStyles keeps Bubbles inputs transparent inside the Charm shell.
func (a *CharmApp) applyWidgetStyles() {
	textStyles := textinput.DefaultDarkStyles()
	textStyles.Focused.Text = lipgloss.NewStyle().Foreground(a.styles.palette.fg)
	textStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	textStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(a.styles.palette.focus).Bold(true)
	textStyles.Blurred.Text = lipgloss.NewStyle().Foreground(a.styles.palette.fg)
	textStyles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	textStyles.Blurred.Prompt = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	textStyles.Cursor.Color = a.styles.palette.focus
	a.apiKeyInput.SetStyles(textStyles)
	a.paletteInput.SetStyles(textStyles)
	a.settingsInput.SetStyles(textStyles)
	a.customViewInput.SetStyles(textStyles)
	a.titleInput.SetStyles(textStyles)
	a.agentWorkspace.SetStyles(textStyles)
	a.promptTplName.SetStyles(textStyles)

	areaStyles := textarea.DefaultDarkStyles()
	areaStyles.Focused.Base = lipgloss.NewStyle()
	areaStyles.Focused.Text = lipgloss.NewStyle().Foreground(a.styles.palette.fg)
	areaStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	areaStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(a.styles.palette.focus)
	areaStyles.Focused.CursorLine = lipgloss.NewStyle()
	areaStyles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	areaStyles.Blurred.Base = lipgloss.NewStyle()
	areaStyles.Blurred.Text = lipgloss.NewStyle().Foreground(a.styles.palette.fg)
	areaStyles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	areaStyles.Blurred.Prompt = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	areaStyles.Blurred.CursorLine = lipgloss.NewStyle()
	areaStyles.Blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(a.styles.palette.subtle)
	areaStyles.Cursor.Color = a.styles.palette.focus
	a.bodyArea.SetStyles(areaStyles)
	a.agentPromptArea.SetStyles(areaStyles)
	a.promptTplBody.SetStyles(areaStyles)
}

func formatTime(value interface {
	IsZero() bool
	Format(string) string
}) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02 15:04")
}

// todayLinearDate returns today's date in Linear's due-date format.
func todayLinearDate() string {
	return time.Now().Format("2006-01-02")
}

// isIssueDueToday reports whether an issue is marked for today's work.
func isIssueDueToday(issue *linearapi.Issue) bool {
	if issue == nil {
		return false
	}
	return isDueDateToday(issue.DueDate)
}

// isIssueOverdue reports whether an issue has a due date before today's local date.
func isIssueOverdue(issue *linearapi.Issue) bool {
	if issue == nil {
		return false
	}
	return isDueDateOverdue(issue.DueDate)
}

// isDueDateToday reports whether a Linear due-date string is today's local date.
func isDueDateToday(dueDate *string) bool {
	if dueDate == nil {
		return false
	}
	return strings.TrimSpace(*dueDate) == todayLinearDate()
}

// isDueDateOverdue reports whether a Linear due-date string is before today's local date.
func isDueDateOverdue(dueDate *string) bool {
	if dueDate == nil {
		return false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*dueDate))
	if err != nil {
		return false
	}
	today, err := time.Parse("2006-01-02", todayLinearDate())
	if err != nil {
		return false
	}
	return parsed.Before(today)
}

// cloneStringPointer returns an independent copy of an optional string.
func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

// cloneFloatPointer returns an independent copy of an optional float.
func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

// rollbackClearableIDInput converts a previous relation ID into Linear's clearable ID semantics.
func rollbackClearableIDInput(previous string) *string {
	value := strings.TrimSpace(previous)
	return &value
}

// rollbackDueDateInput converts a previous due date into Linear's update semantics.
func rollbackDueDateInput(previous *string) *string {
	if previous == nil {
		empty := ""
		return &empty
	}
	return cloneStringPointer(previous)
}

// labelsForIDs rebuilds local issue labels from selected IDs and the active picker labels.
func labelsForIDs(ids []string, labels map[string]string) []linearapi.IssueLabel {
	result := make([]linearapi.IssueLabel, 0, len(ids))
	for _, id := range ids {
		name := labels[id]
		if strings.TrimSpace(name) == "" {
			name = id
		}
		result = append(result, linearapi.IssueLabel{ID: id, Name: name})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (a *CharmApp) newIssueTable(title string) table.Model {
	t := table.New(
		table.WithColumns(issueTableColumns()),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(8),
		table.WithWidth(100),
		table.WithStyles(a.issueTableStyles(100)),
	)
	_ = title
	return t
}

// issueTableStyles returns table styles sized so the selected row spans the whole issue list.
func (a CharmApp) issueTableStyles(width int) table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Foreground(a.styles.palette.subtle).
		Bold(true)
	styles.Cell = styles.Cell.Foreground(a.styles.palette.fg)
	styles.Selected = styles.Selected.
		Foreground(a.styles.palette.selectedText).
		Background(a.styles.palette.selected).
		Bold(true).
		Width(maxInt(1, width))
	return styles
}

func (a *CharmApp) resizeComponents() {
	if a.width <= 0 || a.height <= 0 {
		return
	}
	_, issuesWidth, detailsWidth := a.charmColumnWidths()
	bodyHeight := a.charmBodyHeight()
	visibleIssueSections := 0
	if a.cfg.ShowMyIssues {
		visibleIssueSections++
	}
	if a.cfg.ShowOtherIssues {
		visibleIssueSections++
	}
	tableHeight := maxInt(5, bodyHeight-4)
	if visibleIssueSections > 1 {
		tableHeight = maxInt(4, (bodyHeight-6)/2)
	}
	tableWidth := maxInt(20, issuesWidth-2)
	a.myTable.SetWidth(tableWidth)
	a.otherTable.SetWidth(tableWidth)
	a.myTable.SetStyles(a.issueTableStyles(tableWidth))
	a.otherTable.SetStyles(a.issueTableStyles(tableWidth))
	a.myTable.SetHeight(tableHeight)
	a.otherTable.SetHeight(tableHeight)
	a.details.SetWidth(maxInt(20, detailsWidth-2))
	a.details.SetHeight(maxInt(4, bodyHeight-3))
	overlayWidth := minInt(maxInt(72, a.width-12), 100)
	a.agentPromptArea.SetWidth(overlayWidth - 6)
	a.agentWorkspace.SetWidth(overlayWidth - 6)
	a.promptTplName.SetWidth(overlayWidth - 6)
	a.promptTplBody.SetWidth(overlayWidth - 6)
	a.agentOutput.SetWidth(overlayWidth - 6)
	a.agentOutput.SetHeight(maxInt(8, a.height-12))
}

// charmBodyHeight returns the available height below header and above footer.
func (a CharmApp) charmBodyHeight() int {
	return maxInt(1, a.height-4)
}

// charmColumnWidths computes stable widths for the visible main layout columns.
func (a CharmApp) charmColumnWidths() (int, int, int) {
	gaps := 0
	if a.cfg.ShowNavigation {
		gaps++
	}
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		gaps++
	}
	available := maxInt(40, a.width-gaps)
	navWidth := 0
	if a.cfg.ShowNavigation {
		minNavWidth, maxNavWidth := a.navigationWidthBounds()
		navWidth = minInt(30, maxInt(minNavWidth, available/5))
		if a.navWidth > 0 {
			navWidth = clamp(a.navWidth, minNavWidth, maxNavWidth)
		}
	}
	remaining := maxInt(1, available-navWidth)
	minIssuesWidth := 36
	minDetailsWidth := 32
	maxDetailsWidth := maxInt(minDetailsWidth, remaining-minIssuesWidth)
	detailsWidth := clamp((remaining*2)/5, 48, minInt(96, maxDetailsWidth))
	if a.detailsWidth > 0 {
		detailsWidth = clamp(a.detailsWidth, minDetailsWidth, maxDetailsWidth)
	}
	issuesWidth := maxInt(minIssuesWidth, remaining-detailsWidth)
	if !(a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues) {
		issuesWidth = 0
		detailsWidth = maxInt(28, available-navWidth)
	}
	return navWidth, issuesWidth, detailsWidth
}

// navigationWidthBounds returns safe resize limits for the left navigation pane.
func (a CharmApp) navigationWidthBounds() (int, int) {
	if !a.cfg.ShowNavigation {
		return 0, 0
	}
	minNavWidth := 14
	gaps := 1
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		gaps = 2
	}
	available := maxInt(40, a.width-gaps)
	minRemainder := 28
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		minRemainder = 68
	}
	maxNavWidth := maxInt(minNavWidth, minInt(48, available-minRemainder))
	return minNavWidth, maxNavWidth
}

// navigationPaneHeight keeps the left navigation compact so the calendar has real space.
func (a CharmApp) navigationPaneHeight(bodyHeight int) int {
	if bodyHeight <= 12 {
		return maxInt(4, bodyHeight/2)
	}
	desired := len(a.navigation) + 2
	desired = clamp(desired, 6, 14)
	maxHeight := maxInt(6, bodyHeight-10)
	return clamp(desired, 6, maxHeight)
}

type charmWorkspaceLayout struct {
	bodyTop         int
	bodyHeight      int
	navX            int
	navY            int
	navHeight       int
	navDividerX     int
	navWidth        int
	calendarX       int
	calendarY       int
	calendarHeight  int
	issuesX         int
	issuesWidth     int
	detailsDividerX int
	detailsX        int
	detailsWidth    int
	maxDetailsWidth int
}

func (a CharmApp) workspaceLayout() charmWorkspaceLayout {
	headerHeight := lipgloss.Height(a.renderHeader())
	statusHeight := lipgloss.Height(a.renderStatus())
	bodyHeight := maxInt(1, a.height-headerHeight-statusHeight)
	navWidth, issuesWidth, detailsWidth := a.charmColumnWidths()
	layout := charmWorkspaceLayout{
		bodyTop:         headerHeight,
		bodyHeight:      bodyHeight,
		navWidth:        navWidth,
		navDividerX:     -1,
		issuesWidth:     issuesWidth,
		detailsWidth:    detailsWidth,
		detailsDividerX: -1,
		maxDetailsWidth: maxInt(32, a.width-36),
	}
	x := 0
	if a.cfg.ShowNavigation {
		layout.navX = x
		layout.navY = layout.bodyTop
		layout.navHeight = a.navigationPaneHeight(bodyHeight)
		layout.calendarX = x
		layout.calendarY = layout.bodyTop + layout.navHeight
		layout.calendarHeight = maxInt(0, bodyHeight-layout.navHeight)
		x += navWidth
		layout.navDividerX = x
		x++
	}
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		layout.issuesX = x
		x += issuesWidth
		layout.detailsDividerX = x
		x++
	}
	layout.detailsX = x
	return layout
}

func (l charmWorkspaceLayout) inNavigation(x int, y int) bool {
	return l.navWidth > 0 && x >= l.navX && x < l.navX+l.navWidth && y >= l.navY && y < l.navY+l.navHeight
}

func (l charmWorkspaceLayout) inCalendar(x int, y int) bool {
	return l.navWidth > 0 && l.calendarHeight > 0 && x >= l.calendarX && x < l.calendarX+l.navWidth && y >= l.calendarY && y < l.calendarY+l.calendarHeight
}

func (l charmWorkspaceLayout) inIssues(x int, y int) bool {
	return l.issuesWidth > 0 && x >= l.issuesX && x < l.issuesX+l.issuesWidth && y >= l.bodyTop && y < l.bodyTop+l.bodyHeight
}

func (l charmWorkspaceLayout) inDetails(x int, y int) bool {
	return x >= l.detailsX && x < l.detailsX+l.detailsWidth && y >= l.bodyTop && y < l.bodyTop+l.bodyHeight
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a CharmApp) render() string {
	if a.width == 0 {
		return a.styles.screen.Render("Loading...")
	}
	if a.apiKeyMode {
		return a.renderAPIKeyPrompt()
	}
	if a.overlay != charmOverlayNone {
		return a.renderOverlayScreen()
	}
	return a.renderMainScreen()
}

// renderMainScreen draws the normal workspace without modal overlays.
func (a CharmApp) renderMainScreen() string {
	header := a.renderHeader()
	status := a.renderStatus()
	bodyHeight := maxInt(1, a.height-lipgloss.Height(header)-lipgloss.Height(status))
	body := a.renderWorkspace(bodyHeight)
	return a.styles.screen.Width(a.width).Height(a.height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
}

// renderHeader draws the app chrome without putting primary content inside another bordered box.
func (a CharmApp) renderHeader() string {
	title := a.styles.headerTitle.Render(" linear-tui")
	metaParts := []string{a.issueCountSummary()}
	if a.currentUser != nil && strings.TrimSpace(a.currentUser.DisplayName) != "" {
		metaParts = append(metaParts, strings.TrimSpace(a.currentUser.DisplayName))
	}
	meta := a.styles.headerMeta.Render(strings.Join(metaParts, "  |  "))
	spacer := strings.Repeat(" ", maxInt(1, a.width-lipgloss.Width(title)-lipgloss.Width(meta)-1))
	return a.styles.header.Width(a.width).Render(title + spacer + meta + " ")
}

// issueCountSummary returns the loaded issue counts shown in the top chrome.
func (a CharmApp) issueCountSummary() string {
	if a.cfg.ShowMyIssues && a.cfg.ShowOtherIssues {
		return fmt.Sprintf("%d issues (%d mine, %d other)", len(a.issues), len(a.myRows), len(a.otherRows))
	}
	if a.cfg.ShowMyIssues {
		return fmt.Sprintf("%d my issues", len(a.myRows))
	}
	if a.cfg.ShowOtherIssues {
		return fmt.Sprintf("%d other issues", len(a.otherRows))
	}
	return fmt.Sprintf("%d issues", len(a.issues))
}

// renderWorkspace lays out only the visible content columns.
func (a CharmApp) renderWorkspace(height int) string {
	navWidth, issuesWidth, detailsWidth := a.charmColumnWidths()
	type workspaceColumn struct {
		pane    charmPane
		content string
	}
	columns := []workspaceColumn{}
	if a.cfg.ShowNavigation {
		columns = append(columns, workspaceColumn{pane: charmPaneNav, content: a.renderLeftSidebar(navWidth, height)})
	}
	if a.cfg.ShowMyIssues || a.cfg.ShowOtherIssues {
		columns = append(columns, workspaceColumn{pane: charmPaneIssues, content: a.renderIssues(issuesWidth, height)})
	}
	columns = append(columns, workspaceColumn{pane: charmPaneDetails, content: a.renderDetails(detailsWidth, height)})
	parts := make([]string, 0, len(columns)*2)
	for i, column := range columns {
		if i > 0 {
			resizePane := charmPane(-1)
			if i == 1 && columns[0].pane == charmPaneNav {
				resizePane = charmPaneNav
			} else if column.pane == charmPaneDetails {
				resizePane = charmPaneDetails
			}
			parts = append(parts, a.renderColumnGap(height, resizePane))
		}
		parts = append(parts, column.content)
	}
	return a.styles.workspace.Width(a.width).Height(height).Render(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

// renderColumnGap renders the thin separator between workspace columns.
func (a CharmApp) renderColumnGap(height int, resizePane charmPane) string {
	if resizePane < 0 {
		return a.styles.columnGap.Width(1).Height(height).Render("")
	}
	line := strings.Repeat("│\n", maxInt(0, height-1)) + "│"
	style := a.styles.resizeHandle
	if (resizePane == charmPaneNav && a.draggingNavigation) || (resizePane == charmPaneDetails && a.draggingDetails) {
		style = a.styles.resizeHandleActive
	}
	return style.Width(1).Height(height).Render(line)
}

// renderLeftSidebar stacks navigation above the embedded Google Calendar day pane.
func (a CharmApp) renderLeftSidebar(width int, height int) string {
	navHeight := a.navigationPaneHeight(height)
	calendarHeight := maxInt(0, height-navHeight)
	parts := []string{a.renderNavigation(width, navHeight)}
	if calendarHeight > 0 {
		parts = append(parts, a.renderCalendar(width, calendarHeight))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (a CharmApp) renderNavigation(width int, height int) string {
	lines := []string{}
	for i, node := range a.navigation {
		prefix := "  "
		style := a.styles.navItem
		if charmNavigationKey(node) == charmNavigationKey(a.selectedNavigation) {
			prefix = "* "
			style = style.Bold(true)
		}
		if a.focusedPane == charmPaneNav && i == a.navigationCursor {
			style = a.styles.selected
			prefix = "> "
		}
		lines = append(lines, style.Render(prefix+node.Text))
	}
	if len(lines) == 0 {
		lines = append(lines, a.styles.subtle.Render("Loading teams..."))
	}
	bodyHeight := maxInt(1, height-2)
	body := a.styles.columnBody.Width(width).Height(bodyHeight).Render(strings.Join(lines, "\n"))
	return a.renderColumn(charmPaneNav, "Navigation", "", width, height, body)
}

func (a CharmApp) renderCalendar(width int, height int) string {
	bodyHeight := maxInt(1, height-2)
	body := a.styles.columnBody.Width(width).Height(bodyHeight).Render(a.renderCalendarBody(width, bodyHeight))
	return a.renderColumn(charmPaneCalendar, "Calendar", a.calendarPaneMeta(), width, height, body)
}

// renderCalendarBody renders a single selected-day agenda from Google Calendar.
func (a CharmApp) renderCalendarBody(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if a.calendarDetails {
		return a.renderCalendarDetails(width, height)
	}
	date := a.calendarSelectedDate()
	events := a.calendarEventsForSelectedDay()
	title := date.Format("Mon Jan 2")
	if calendar.SameDay(date, time.Now()) {
		title += " today"
	}
	lines := []string{
		a.styles.issueSectionLabel.Render(title),
		a.styles.subtle.Render(calendarEventCountLabel(len(events))),
	}
	if a.calendarLoading {
		lines = append(lines, a.styles.loading.Render(a.loadingIndicatorText()))
	}
	if a.calendarErr != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(a.styles.palette.error).Render(truncatePlain(a.calendarErr.Error(), maxInt(8, width-2))))
	}
	if len(events) == 0 && a.calendarErr == nil {
		lines = append(lines, a.styles.subtle.Render("No events"))
	}
	for i, event := range events {
		if len(lines) >= height {
			break
		}
		lines = append(lines, a.renderCalendarEventRow(event, i == a.calendarSelectedIdx, width))
	}
	return strings.Join(lines, "\n")
}

// renderCalendarEventRow formats one compact event row for the embedded agenda.
func (a CharmApp) renderCalendarEventRow(event calendar.Event, selected bool, width int) string {
	line := truncatePlain(fmt.Sprintf("%s  %s", calendarEventTimeLabel(event), oneLineText(event.Summary)), maxInt(4, width-4))
	if meta := calendarEventMetaLine(event); meta != "" {
		line += "\n" + a.styles.subtle.Render(truncatePlain(meta, maxInt(4, width-4)))
	}
	style := a.styles.navItem.Width(maxInt(1, width-1))
	if selected && a.focusedPane == charmPaneCalendar {
		style = a.styles.selected.Width(maxInt(1, width-1))
	} else if selected {
		style = style.Bold(true)
	}
	return style.Render(line)
}

// renderCalendarDetails shows the selected event details inside the calendar pane.
func (a CharmApp) renderCalendarDetails(width int, height int) string {
	event, ok := a.selectedCalendarEvent()
	if !ok {
		return a.styles.subtle.Render("No event selected")
	}
	lines := []string{
		a.styles.issueIdentifier.Render(event.Summary),
		calendarEventTimeLabel(event),
	}
	if event.Calendar != "" {
		lines = append(lines, "Calendar: "+event.Calendar)
	}
	if event.Location != "" {
		lines = append(lines, "Location: "+event.Location)
	}
	if event.Organizer != "" {
		lines = append(lines, "Organizer: "+event.Organizer)
	}
	if len(event.Attendees) > 0 {
		lines = append(lines, "Guests: "+strings.Join(event.Attendees, ", "))
	}
	if strings.TrimSpace(event.Description) != "" {
		lines = append(lines, "", wrapText(stripCalendarHTML(event.Description), maxInt(8, width-3)))
	}
	if event.HTMLLink != "" {
		lines = append(lines, "", compactURLLabel(event.HTMLLink, maxInt(8, width-3)))
	}
	lines = append(lines, "", a.styles.subtle.Render("enter/esc: close"))
	return truncateLines(strings.Join(lines, "\n"), maxInt(1, height))
}

func (a CharmApp) calendarPaneMeta() string {
	if a.calendarLoading {
		return "loading"
	}
	if a.calendarErr != nil {
		return "error"
	}
	return calendarEventCountLabel(len(a.calendarEventsForSelectedDay()))
}

func (a CharmApp) renderIssues(width int, height int) string {
	parts := []string{}
	if a.cfg.ShowMyIssues && a.cfg.ShowOtherIssues {
		parts = append(parts, a.renderIssueTabs(width))
	}
	if a.cfg.ShowMyIssues {
		content := a.renderIssueTable(a.myRows, a.myIssueMap, a.myTable)
		parts = append(parts, a.renderIssueSection("My Issues", IssuesSectionMy, len(a.myRows), content, width))
	}
	if a.cfg.ShowOtherIssues {
		content := a.renderIssueTable(a.otherRows, a.otherIssueMap, a.otherTable)
		parts = append(parts, a.renderIssueSection("Other Issues", IssuesSectionOther, len(a.otherRows), content, width))
	}
	if len(parts) == 0 {
		parts = append(parts, a.styles.subtle.Render("No issue panels visible"))
	}
	body := a.styles.columnBody.Width(width).Height(maxInt(1, height-2)).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return a.renderColumn(charmPaneIssues, "Issues", a.issueContextText(), width, height, body)
}

// loadingIssueText returns the compact label used for all loading states.
func (a CharmApp) loadingIssueText() string {
	return "loading"
}

func (a CharmApp) renderDetails(width int, height int) string {
	body := a.styles.columnBody.Width(width).Height(maxInt(1, height-2)).Render(a.details.View())
	return a.renderColumn(charmPaneDetails, "Details", a.selectedIssueLabel(), width, height, body)
}

// renderColumn creates a flat Lip Gloss column with a focus-aware heading.
func (a CharmApp) renderColumn(pane charmPane, title string, meta string, width int, height int, body string) string {
	titleLine := title
	if strings.TrimSpace(meta) != "" {
		metaText := a.styles.paneMeta.Render(meta)
		spacer := strings.Repeat(" ", maxInt(1, width-lipgloss.Width(title)-lipgloss.Width(metaText)-1))
		titleLine = title + spacer + metaText
	}
	headerStyle := a.styles.paneHeader
	if a.focusedPane == pane {
		headerStyle = a.styles.paneHeaderFocused
	}
	header := headerStyle.Width(width).Render(titleLine)
	return a.styles.column.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

// renderIssueTabs presents My/Other issue lists as tabs instead of nested boxes.
func (a CharmApp) renderIssueTabs(width int) string {
	my := a.styles.issueTab.Render(fmt.Sprintf("My %d", len(a.myRows)))
	other := a.styles.issueTab.Render(fmt.Sprintf("Other %d", len(a.otherRows)))
	if a.activeSection == IssuesSectionMy {
		my = a.styles.issueTabActive.Render(fmt.Sprintf("My %d", len(a.myRows)))
	} else {
		other = a.styles.issueTabActive.Render(fmt.Sprintf("Other %d", len(a.otherRows)))
	}
	return a.styles.issueTabs.Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, my, other))
}

// renderIssueSection draws a table section with a compact label and no inner border.
func (a CharmApp) renderIssueSection(title string, section IssuesSection, count int, content string, width int) string {
	label := fmt.Sprintf("%s (%d)", title, count)
	style := a.styles.issueSectionLabel
	if a.focusedPane == charmPaneIssues && a.activeSection == section {
		style = a.styles.issueSectionLabelFocused
	}
	return lipgloss.JoinVertical(lipgloss.Left, style.Width(width).Render(label), content)
}

// issueContextText summarizes the active sort and filters without crowding the table.
func (a CharmApp) issueContextText() string {
	parts := make([]string, 0, 4)
	if sortLabel := issueSortLabel(a.activeIssueSortField()); sortLabel != "" {
		parts = append(parts, "sort: "+sortLabel)
	}
	if query := strings.TrimSpace(a.searchQuery); query != "" {
		parts = append(parts, "search: "+query)
	}
	if !a.richFilters.Empty() {
		parts = append(parts, "filters: "+a.filtersWithResolvedLabels().Summary())
	}
	if a.cfg.ShowMyIssues && a.currentUser != nil {
		parts = append(parts, "my: all active")
	}
	return strings.Join(parts, " | ")
}

// resolvePersistedFilterLabels hydrates stored filter IDs with labels already loaded at startup.
func (a *CharmApp) resolvePersistedFilterLabels() {
	a.richFilters = a.filtersWithResolvedLabels()
}

// filtersWithResolvedLabels returns active filters with local ID labels filled in for display.
func (a CharmApp) filtersWithResolvedLabels() IssueFilters {
	filters := a.richFilters
	filters.TeamNames = resolvedNamesForIDs(filters.TeamIDs, filters.TeamNames, teamLabelsByID(a.teams))
	return filters
}

// issueSortLabel returns a compact label for the current issue sort.
func issueSortLabel(field SortField) string {
	switch field {
	case SortByOrder:
		return "Linear order"
	case SortByUpdatedAt:
		return "updated"
	case SortByCreatedAt:
		return "created"
	case SortByPriority:
		return "priority"
	case SortByStatus:
		return "status"
	default:
		return ""
	}
}

// selectedIssueLabel returns the current issue identifier for the details heading.
func (a CharmApp) selectedIssueLabel() string {
	if a.selectedIssue == nil {
		return ""
	}
	return a.selectedIssue.Identifier
}

func (a CharmApp) renderStatus() string {
	left := a.status
	if a.err != nil {
		left = a.err.Error()
	}
	if a.loading {
		left = a.loadingIndicatorText()
	}
	if a.searchMode {
		left = "Search: " + a.searchQuery
	}
	if contextText := a.issueContextText(); contextText != "" {
		if left == "" || left == "Ready" {
			left = contextText
		} else if !a.searchMode && !a.loading && a.err == nil {
			left = left + " | " + contextText
		}
	}
	help := a.help.ShortHelpView(a.keys.ShortHelp())
	line := left
	if line == "" {
		line = "Ready"
	}
	spacer := strings.Repeat(" ", maxInt(1, a.width-lipgloss.Width(line)-lipgloss.Width(help)-2))
	return a.styles.status.Width(a.width).Render(line + spacer + help)
}

// loadingIndicatorText renders the bottom loading affordance without query details that shift layouts.
func (a CharmApp) loadingIndicatorText() string {
	return strings.TrimSpace(a.loadingSpinner.View() + " " + a.loadingIssueText())
}

func (a CharmApp) renderAPIKeyPrompt() string {
	title := a.styles.title.Render("Set Linear API Key")
	body := strings.Join([]string{
		title,
		"",
		a.apiKeyInput.View(),
		"",
		a.styles.subtle.Render("Enter: save | Esc: clear | Ctrl-C: quit"),
	}, "\n")
	boxWidth := minInt(maxInt(54, a.width/2), maxInt(54, a.width-4))
	boxHeight := 9
	box := a.styles.focusedPanel.Width(boxWidth).Height(boxHeight).Render(body)
	topPad := strings.Repeat("\n", maxInt(0, (a.height-boxHeight)/2))
	leftPad := strings.Repeat(" ", maxInt(0, (a.width-lipgloss.Width(box))/2))
	content := topPad + lipgloss.JoinHorizontal(lipgloss.Top, leftPad, box)
	status := a.styles.status.Width(a.width).Render(a.status)
	return a.styles.screen.Width(a.width).Height(a.height).Render(lipgloss.JoinVertical(lipgloss.Left, content, status))
}

func (a CharmApp) renderOverlayScreen() string {
	box := ""
	switch a.overlay {
	case charmOverlayPalette:
		box = a.renderPaletteOverlay()
	case charmOverlayPicker:
		box = a.renderPickerOverlay()
	case charmOverlayMultiSelect:
		box = a.renderMultiSelectOverlay()
	case charmOverlaySettings:
		box = a.renderSettingsOverlay()
	case charmOverlayCustomView:
		box = a.renderCustomViewOverlay()
	case charmOverlayConfirmDeleteView:
		box = a.renderDeleteViewOverlay()
	case charmOverlayConfirmArchive:
		box = a.renderArchiveOverlay()
	case charmOverlayConfirmRemoveParent:
		box = a.renderRemoveParentOverlay()
	case charmOverlayIssueForm:
		box = a.renderIssueFormOverlay()
	case charmOverlayAgentPrompt:
		box = a.renderAgentPromptOverlay()
	case charmOverlayAgentOutput:
		box = a.renderAgentOutputOverlay()
	case charmOverlayPromptTemplates:
		box = a.renderPromptTemplatesOverlay()
	}
	if box == "" {
		box = a.styles.subtle.Render("No overlay")
	}
	return placeOverlay(a.renderMainScreen(), box, a.width, a.height)
}

// placeOverlay draws a centered overlay over an existing ANSI-rendered screen.
func placeOverlay(base string, overlay string, width int, height int) string {
	if strings.TrimSpace(overlay) == "" || width <= 0 || height <= 0 {
		return base
	}
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	boxLines := strings.Split(overlay, "\n")
	boxWidth := lipgloss.Width(overlay)
	boxHeight := lipgloss.Height(overlay)
	startX := maxInt(0, (width-boxWidth)/2)
	startY := maxInt(0, (height-boxHeight)/2)
	for i, boxLine := range boxLines {
		y := startY + i
		if y >= len(baseLines) {
			break
		}
		boxLine = padANSIWidth(boxLine, boxWidth)
		line := padANSIWidth(baseLines[y], width)
		left := ansi.Cut(line, 0, startX)
		right := ansi.Cut(line, startX+boxWidth, width)
		baseLines[y] = left + boxLine + right
	}
	return strings.Join(baseLines[:minInt(len(baseLines), height)], "\n")
}

// padANSIWidth pads an ANSI-rendered line to a visible width.
func padANSIWidth(line string, width int) string {
	if width <= 0 {
		return line
	}
	if lineWidth := ansi.StringWidth(line); lineWidth < width {
		return line + strings.Repeat(" ", width-lineWidth)
	}
	return line
}

func (a CharmApp) renderPaletteOverlay() string {
	filtered := a.filteredCharmCommands()
	paletteList := a.paletteList
	paletteRows := minInt(10, maxInt(1, len(filtered)))
	showPagination := len(filtered) > paletteRows
	paletteHeight := paletteRows
	if showPagination {
		paletteHeight++
	}
	paletteList.SetShowPagination(showPagination)
	paletteList.SetSize(56, paletteHeight)
	lines := []string{
		a.styles.title.Render("Commands"),
		"",
		a.paletteInput.View(),
		"",
	}
	if len(filtered) == 0 {
		lines = append(lines, a.styles.subtle.Render("No matching commands"))
	} else {
		lines = append(lines, strings.TrimRight(paletteList.View(), "\n"))
	}
	lines = append(lines, "", a.styles.subtle.Render("enter: run   esc: close"))
	return a.renderOverlayPanel(64, lines)
}

func (a CharmApp) renderPickerOverlay() string {
	lines := []string{a.styles.title.Render(a.pickerTitle), ""}
	if a.pickerLoading {
		lines = append(lines, a.styles.loading.Render(a.loadingIndicatorText()))
	} else if len(a.pickerItems) == 0 {
		lines = append(lines, a.styles.subtle.Render("No options available"))
	} else {
		for i, item := range a.pickerItems {
			lines = append(lines, a.renderOverlayRow(item.Label, i == clamp(a.pickerCursor, 0, len(a.pickerItems)-1), 52))
		}
	}
	help := "enter: select   esc: close"
	if a.pickerLoading {
		help = "esc: close"
	}
	lines = append(lines, "", a.styles.subtle.Render(help))
	return a.renderOverlayPanel(56, lines)
}

// renderMultiSelectOverlay renders checkbox-style multi-select rows.
func (a CharmApp) renderMultiSelectOverlay() string {
	lines := []string{a.styles.title.Render(a.multiTitle), ""}
	if len(a.multiItems) == 0 {
		lines = append(lines, a.styles.subtle.Render("No options available"))
	} else {
		maxRows := minInt(14, len(a.multiItems))
		for i := 0; i < maxRows; i++ {
			item := a.multiItems[i]
			check := "( ) "
			if a.multiSelected[item.ID] {
				check = "(x) "
			}
			lines = append(lines, a.renderOverlayRow(check+item.Label, i == clamp(a.multiCursor, 0, len(a.multiItems)-1), 60))
		}
		if len(a.multiItems) > maxRows {
			lines = append(lines, a.styles.subtle.Render(fmt.Sprintf("...and %d more", len(a.multiItems)-maxRows)))
		}
	}
	lines = append(lines, "", a.styles.subtle.Render("space: toggle   enter: save   esc: close"))
	return a.renderOverlayPanel(64, lines)
}

// renderSettingsOverlay renders the Charm settings editor.
func (a CharmApp) renderSettingsOverlay() string {
	return a.renderFieldOverlay("Settings", a.settingsFields, a.settingsCursor, a.settingsInput, "Tab/Up/Down: field | Left/Right: option | Ctrl-S: save | Esc: cancel")
}

// renderCustomViewOverlay renders the Charm custom-view editor.
func (a CharmApp) renderCustomViewOverlay() string {
	title := "Add Custom View"
	if a.customViewEditing != "" {
		title = "Edit Custom View"
	}
	return a.renderFieldOverlay(title, a.customViewFields, a.customViewCursor, a.customViewInput, "Tab/Up/Down: field | Left/Right: option | Ctrl-S: save | Esc: cancel")
}

func (a CharmApp) renderFieldOverlay(title string, fields []charmSettingsField, cursor int, input textinput.Model, help string) string {
	lines := []string{a.styles.title.Render(title), ""}
	if len(fields) == 0 {
		lines = append(lines, a.styles.subtle.Render("No fields loaded"))
	} else {
		start := 0
		maxRows := minInt(14, len(fields))
		if cursor >= maxRows {
			start = cursor - maxRows + 1
		}
		for row := 0; row < maxRows; row++ {
			index := start + row
			if index >= len(fields) {
				break
			}
			field := fields[index]
			value := field.Value
			if len(field.Options) > 0 {
				value = "< " + value + " >"
			} else if index == cursor {
				value = input.View()
			}
			line := fmt.Sprintf("%-20s %s", field.Label, value)
			lines = append(lines, a.renderOverlayRow(line, index == cursor, 80))
		}
		if len(fields) > maxRows {
			lines = append(lines, a.styles.subtle.Render(fmt.Sprintf("%d/%d", cursor+1, len(fields))))
		}
	}
	lines = append(lines, "", a.styles.subtle.Render(help))
	return a.renderOverlayPanel(84, lines)
}

// renderOverlayPanel applies one quiet frame style to every modal-like overlay.
func (a CharmApp) renderOverlayPanel(width int, lines []string) string {
	return a.styles.focusedPanel.Width(width).Render(strings.Join(lines, "\n"))
}

// renderOverlayRow renders a selectable modal row without prefix markers.
func (a CharmApp) renderOverlayRow(label string, selected bool, width int) string {
	line := lipgloss.NewStyle().Width(width).Padding(0, 1).Render(label)
	if !selected {
		return line
	}
	return a.styles.overlaySelected.Width(width).Render(label)
}

func (a CharmApp) renderDeleteViewOverlay() string {
	name := "custom view"
	if a.selectedCustomView != nil && a.selectedCustomView.Name != "" {
		name = a.selectedCustomView.Name
	}
	lines := []string{
		a.styles.title.Render("Delete custom view?"),
		"",
		fmt.Sprintf("Delete %q?", name),
		"",
		"Y/Enter: delete   N/Esc: cancel",
	}
	return a.styles.focusedPanel.Width(56).Render(strings.Join(lines, "\n"))
}

func (a CharmApp) renderArchiveOverlay() string {
	issue := a.currentIssue()
	title := "Archive issue?"
	if issue != nil {
		title = fmt.Sprintf("Archive %s?", issue.Identifier)
	}
	lines := []string{
		a.styles.title.Render(title),
		"",
		a.styles.subtle.Render("This removes the issue from active views."),
		"",
		"Y/Enter: archive   N/Esc: cancel",
	}
	return a.styles.focusedPanel.Width(52).Render(strings.Join(lines, "\n"))
}

// renderRemoveParentOverlay renders the remove-parent confirmation.
func (a CharmApp) renderRemoveParentOverlay() string {
	issue := a.currentIssue()
	title := "Remove parent?"
	if issue != nil {
		title = fmt.Sprintf("Remove parent from %s?", issue.Identifier)
	}
	lines := []string{
		a.styles.title.Render(title),
		"",
		a.styles.subtle.Render("This keeps the issue but moves it to the top level."),
		"",
		"Y/Enter: remove   N/Esc: cancel",
	}
	return a.styles.focusedPanel.Width(56).Render(strings.Join(lines, "\n"))
}

func (a CharmApp) renderIssueFormOverlay() string {
	lines := []string{a.styles.title.Render(a.formTitle), ""}
	switch a.formMode {
	case charmFormCreateIssue:
		assignee := lipgloss.NewStyle().Width(64).Padding(0, 1).Render(a.createIssueAssigneeLabel())
		if a.formFocus == 2 {
			assignee = a.styles.overlaySelected.Width(64).Render(a.createIssueAssigneeLabel())
		}
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.bodyArea.View(),
			"",
			assignee,
			"",
			a.styles.subtle.Render("Tab: switch field | Enter/Ctrl-A: assignee | Ctrl-S: create | Esc: cancel"),
		)
	case charmFormEditTitle:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: save | Esc: cancel"),
		)
	case charmFormEditDescription:
		lines = append(lines,
			a.bodyArea.View(),
			"",
			a.styles.subtle.Render("Ctrl-S: save description | Esc: cancel"),
		)
	case charmFormAddComment:
		lines = append(lines,
			a.bodyArea.View(),
			"",
			a.styles.subtle.Render("Enter: comment | Shift-Enter: newline | Esc: cancel"),
		)
	case charmFormSetDueDate:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: save YYYY-MM-DD | Esc: cancel"),
		)
	case charmFormSetEstimate:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: save points | Esc: cancel"),
		)
	case charmFormIssueRelationTarget:
		lines = append(lines,
			a.styles.subtle.Render("Relation: "+a.formRelationLabel),
			"",
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: add relation | Esc: cancel"),
		)
	case charmFormFilterText:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: apply search | Esc: cancel"),
		)
	case charmFormFilterDueDate:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: apply YYYY-MM-DD | Esc: cancel"),
		)
	case charmFormFilterEstimate:
		lines = append(lines,
			a.titleInput.View(),
			"",
			a.styles.subtle.Render("Enter/Ctrl-S: apply points | Esc: cancel"),
		)
	}
	return a.styles.focusedPanel.Width(72).Render(strings.Join(lines, "\n"))
}

// renderAgentPromptOverlay renders the agent prompt and workspace form.
func (a CharmApp) renderAgentPromptOverlay() string {
	lines := []string{a.styles.title.Render("Ask Agent"), ""}
	if len(a.agentPromptTemplates) > 0 {
		template := a.agentPromptTemplates[clamp(a.agentTemplate, 0, len(a.agentPromptTemplates)-1)]
		line := fmt.Sprintf("Template: %s", template.Name)
		if a.agentPromptFocus == 2 {
			line = a.styles.selected.Render("> " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line, "")
	}
	promptLabel := "Prompt"
	if a.agentPromptFocus == 0 {
		promptLabel = a.styles.selected.Render("> Prompt")
	}
	lines = append(lines, promptLabel, a.agentPromptArea.View(), "")
	workspace := a.agentWorkspace.View()
	if a.agentPromptFocus == 1 {
		workspace = a.styles.selected.Render("> " + workspace)
	} else {
		workspace = "  " + workspace
	}
	lines = append(lines, workspace, "", a.styles.subtle.Render("Tab: field | Up/Down: template | Ctrl-S: run | Esc: cancel"))
	return a.styles.focusedPanel.Width(86).Render(strings.Join(lines, "\n"))
}

// renderAgentOutputOverlay renders the streaming agent output viewport.
func (a CharmApp) renderAgentOutputOverlay() string {
	title := a.agentOutputTitle
	if title == "" {
		title = "Agent Output"
	}
	status := a.agentOutputStatus
	if status == "" {
		status = "Running"
	}
	lines := []string{
		a.styles.title.Render(title),
		a.styles.subtle.Render(status),
		"",
		a.agentOutput.View(),
		"",
	}
	help := "Esc: cancel"
	if !a.agentRunning {
		help = "Esc: close | c: copy final"
	}
	lines = append(lines, a.styles.subtle.Render(help))
	width := minInt(maxInt(82, a.width-8), maxInt(82, a.width))
	return a.styles.focusedPanel.Width(width).Render(strings.Join(lines, "\n"))
}

// renderPromptTemplatesOverlay renders the agent prompt template editor.
func (a CharmApp) renderPromptTemplatesOverlay() string {
	lines := []string{a.styles.title.Render("Agent Prompts"), ""}
	maxRows := minInt(8, len(a.agentPromptTemplates))
	for i := 0; i < maxRows; i++ {
		template := a.agentPromptTemplates[i]
		line := "  " + template.Name
		if a.promptTplFocus == 0 && i == clamp(a.promptTplCursor, 0, len(a.agentPromptTemplates)-1) {
			line = a.styles.selected.Render("> " + template.Name)
		}
		lines = append(lines, line)
	}
	if len(a.agentPromptTemplates) > maxRows {
		lines = append(lines, a.styles.subtle.Render(fmt.Sprintf("...and %d more", len(a.agentPromptTemplates)-maxRows)))
	}
	lines = append(lines, "")
	name := a.promptTplName.View()
	if a.promptTplFocus == 1 {
		name = a.styles.selected.Render("> " + name)
	} else {
		name = "  " + name
	}
	bodyLabel := "  Prompt"
	if a.promptTplFocus == 2 {
		bodyLabel = a.styles.selected.Render("> Prompt")
	}
	lines = append(lines, name, "", bodyLabel, a.promptTplBody.View(), "", a.styles.subtle.Render("Tab: field | a: add | d: delete | Ctrl-S: save | Esc: cancel"))
	return a.styles.focusedPanel.Width(86).Render(strings.Join(lines, "\n"))
}

type charmPalette struct {
	bg           color.Color
	panel        color.Color
	fg           color.Color
	subtle       color.Color
	border       color.Color
	focus        color.Color
	selected     color.Color
	selectedText color.Color
	error        color.Color
}

type charmStyles struct {
	palette                  charmPalette
	screen                   lipgloss.Style
	overlayBackdrop          lipgloss.Style
	header                   lipgloss.Style
	headerTitle              lipgloss.Style
	headerMeta               lipgloss.Style
	workspace                lipgloss.Style
	column                   lipgloss.Style
	columnBody               lipgloss.Style
	columnGap                lipgloss.Style
	resizeHandle             lipgloss.Style
	resizeHandleActive       lipgloss.Style
	paneHeader               lipgloss.Style
	paneHeaderFocused        lipgloss.Style
	paneMeta                 lipgloss.Style
	issueTabs                lipgloss.Style
	issueTab                 lipgloss.Style
	issueTabActive           lipgloss.Style
	issueSectionLabel        lipgloss.Style
	issueSectionLabelFocused lipgloss.Style
	panel                    lipgloss.Style
	focusedPanel             lipgloss.Style
	panelTitle               lipgloss.Style
	navItem                  lipgloss.Style
	selected                 lipgloss.Style
	overlaySelected          lipgloss.Style
	status                   lipgloss.Style
	title                    lipgloss.Style
	subtle                   lipgloss.Style
	loading                  lipgloss.Style
	statusTodo               lipgloss.Style
	statusInProgress         lipgloss.Style
	statusDone               lipgloss.Style
	statusCanceled           lipgloss.Style
	priorityUrgent           lipgloss.Style
	priorityHigh             lipgloss.Style
	priorityNormal           lipgloss.Style
	priorityLow              lipgloss.Style
	priorityNone             lipgloss.Style
	dueToday                 lipgloss.Style
	dueTodayRow              lipgloss.Style
	dueOverdue               lipgloss.Style
	dueOverdueRow            lipgloss.Style
	issueIdentifier          lipgloss.Style
}

func newCharmStyles(theme Theme) charmStyles {
	palette := charmPalette{
		bg:           charmColor(theme.Background),
		panel:        charmColor(theme.HeaderBg),
		fg:           charmColor(theme.Foreground),
		subtle:       charmColor(theme.SecondaryText),
		border:       charmColor(theme.Border),
		focus:        charmColor(theme.BorderFocus),
		selected:     charmColor(theme.SelectionBg),
		selectedText: charmColor(theme.SelectionText),
		error:        charmColor(theme.StatusCanceled),
	}
	basePanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(palette.border).
		Foreground(palette.fg).
		Background(palette.bg).
		Padding(0, 1)
	return charmStyles{
		palette:                  palette,
		screen:                   lipgloss.NewStyle().Foreground(palette.fg),
		overlayBackdrop:          lipgloss.NewStyle().Background(palette.bg),
		header:                   lipgloss.NewStyle().Foreground(palette.fg),
		headerTitle:              lipgloss.NewStyle().Foreground(palette.fg).Bold(true),
		headerMeta:               lipgloss.NewStyle().Foreground(palette.subtle),
		workspace:                lipgloss.NewStyle().Foreground(palette.fg),
		column:                   lipgloss.NewStyle().Foreground(palette.fg),
		columnBody:               lipgloss.NewStyle().Foreground(palette.fg),
		columnGap:                lipgloss.NewStyle(),
		resizeHandle:             lipgloss.NewStyle().Foreground(palette.border),
		resizeHandleActive:       lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
		paneHeader:               lipgloss.NewStyle().Foreground(palette.subtle).Bold(true).PaddingBottom(0),
		paneHeaderFocused:        lipgloss.NewStyle().Foreground(palette.focus).Bold(true).PaddingBottom(0),
		paneMeta:                 lipgloss.NewStyle().Foreground(palette.subtle).Bold(false),
		issueTabs:                lipgloss.NewStyle().MarginBottom(1),
		issueTab:                 lipgloss.NewStyle().Foreground(palette.subtle).Padding(0, 1),
		issueTabActive:           lipgloss.NewStyle().Foreground(palette.focus).Bold(true).Underline(true).Padding(0, 1),
		issueSectionLabel:        lipgloss.NewStyle().Foreground(palette.subtle).Bold(true),
		issueSectionLabelFocused: lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
		panel:                    basePanel,
		focusedPanel:             basePanel.BorderForeground(palette.border),
		panelTitle:               lipgloss.NewStyle().Foreground(palette.subtle).Bold(true),
		navItem:                  lipgloss.NewStyle().Foreground(palette.fg),
		selected:                 lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
		overlaySelected:          lipgloss.NewStyle().Foreground(palette.selectedText).Background(palette.selected).Bold(true).Padding(0, 1),
		status:                   lipgloss.NewStyle().Foreground(palette.subtle),
		title:                    lipgloss.NewStyle().Foreground(palette.fg).Bold(true),
		subtle:                   lipgloss.NewStyle().Foreground(palette.subtle),
		loading:                  lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
		statusTodo:               lipgloss.NewStyle().Foreground(charmColor(theme.StatusTodo)).Bold(true),
		statusInProgress:         lipgloss.NewStyle().Foreground(charmColor(theme.StatusInProgress)).Bold(true),
		statusDone:               lipgloss.NewStyle().Foreground(charmColor(theme.StatusDone)).Bold(true),
		statusCanceled:           lipgloss.NewStyle().Foreground(charmColor(theme.StatusCanceled)).Bold(true),
		priorityUrgent:           lipgloss.NewStyle().Foreground(charmColor(theme.StatusCanceled)).Bold(true),
		priorityHigh:             lipgloss.NewStyle().Foreground(charmColor(theme.StatusInProgress)).Bold(true),
		priorityNormal:           lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
		priorityLow:              lipgloss.NewStyle().Foreground(palette.subtle),
		priorityNone:             lipgloss.NewStyle().Foreground(palette.subtle),
		dueToday:                 lipgloss.NewStyle().Foreground(charmColor(theme.StatusInProgress)).Bold(true),
		dueTodayRow:              lipgloss.NewStyle().Foreground(charmColor(theme.StatusInProgress)).Bold(true),
		dueOverdue:               lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9f1c")).Bold(true),
		dueOverdueRow:            lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9f1c")).Bold(true),
		issueIdentifier:          lipgloss.NewStyle().Foreground(palette.focus).Bold(true),
	}
}

func charmColor(value ThemeColor) color.Color {
	if strings.TrimSpace(string(value)) == "" {
		return lipgloss.Color("default")
	}
	return lipgloss.Color(string(value))
}

type charmKeyMap struct {
	quit       key.Binding
	create     key.Binding
	addComment key.Binding
	refresh    key.Binding
	search     key.Binding
	palette    key.Binding
	status     key.Binding
	priority   key.Binding
	dueToday   key.Binding
	copyURL    key.Binding
	undo       key.Binding
	nextPane   key.Binding
	prevPane   key.Binding
}

func defaultCharmKeyMap() charmKeyMap {
	return charmKeyMap{
		quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		create:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create")),
		addComment: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "comment")),
		refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		search:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		palette:    key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "commands")),
		status:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
		priority:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "priority")),
		dueToday:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
		copyURL:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy url")),
		undo:       key.NewBinding(key.WithKeys("z", "ctrl+z"), key.WithHelp("z", "undo")),
		nextPane:   key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab", "next pane")),
		prevPane:   key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab", "prev pane")),
	}
}

func (k charmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.palette, k.search, k.create, k.addComment, k.status, k.priority, k.dueToday, k.copyURL, k.undo, k.refresh, k.prevPane, k.nextPane, k.quit}
}

func (k charmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
