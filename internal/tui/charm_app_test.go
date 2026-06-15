package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/calendar"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func testCharmConfig() config.Config {
	return config.Config{
		LinearAPIKey:    "test-key",
		PageSize:        50,
		CacheTTL:        time.Minute,
		ShowNavigation:  true,
		ShowMyIssues:    true,
		ShowOtherIssues: true,
		Theme:           config.ThemeLinear,
	}
}

func TestNewCharmAppRendersCorePanels(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	for _, want := range []string{"Navigation", "Issues", "Details", "All Issues"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered Charm app missing %q:\n%s", want, rendered)
		}
	}
}

func TestCharmAppViewEnablesMouseMode(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	view := app.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("MouseMode = %v, want cell motion", view.MouseMode)
	}
}

func TestCharmAppInitSchedulesAutoRefresh(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	if cmd := app.Init(); cmd == nil {
		t.Fatal("Init returned nil cmd, want startup load plus auto-refresh timer")
	}
}

func TestCharmAppAutoRefreshClearsCacheAndStartsLinearLoad(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	cacheKey := issueCacheKeyFromParams(app.buildCharmFetchParams())
	app.issueResults.Set(cacheKey, []linearapi.Issue{{ID: "cached", Identifier: "LT-CACHED"}})
	if _, ok := app.issueResults.Get(cacheKey); !ok {
		t.Fatal("test setup did not cache issues")
	}

	model, cmd := app.Update(charmAutoRefreshMsg{})
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("auto-refresh returned nil cmd, want load plus next timer")
	}
	if !app.loading || app.status != "Refreshing issues..." {
		t.Fatalf("auto-refresh loading=%v status=%q, want active refresh", app.loading, app.status)
	}
	if !app.calendarLoading {
		t.Fatal("auto-refresh did not start the calendar refresh")
	}
	if _, ok := app.issueResults.Get(cacheKey); ok {
		t.Fatal("auto-refresh did not clear the in-memory issue cache")
	}
}

func TestCharmAppAutoRefreshSkipsWhileOverlayOpen(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.overlay = charmOverlayPalette
	cacheKey := issueCacheKeyFromParams(app.buildCharmFetchParams())
	app.issueResults.Set(cacheKey, []linearapi.Issue{{ID: "cached", Identifier: "LT-CACHED"}})

	model, cmd := app.Update(charmAutoRefreshMsg{})
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("auto-refresh skip returned nil cmd, want next timer")
	}
	if app.loading {
		t.Fatal("auto-refresh should not start loading while an overlay is open")
	}
	if _, ok := app.issueResults.Get(cacheKey); !ok {
		t.Fatal("auto-refresh should not clear cache while skipped")
	}
}

func TestCharmAppMainLayoutAvoidsLegacyPanelChrome(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	for _, legacy := range []string{"╭", "╮", "╰", "╯"} {
		if strings.Contains(rendered, legacy) {
			t.Fatalf("main layout still rendered legacy panel border %q:\n%s", legacy, rendered)
		}
	}
}

func TestCharmAppMainLayoutAvoidsBackgroundBlocks(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	if strings.Contains(rendered, "\x1b[48;") {
		t.Fatalf("main layout rendered ANSI background blocks:\n%s", rendered)
	}
}

func TestCharmAppFocusedPaneShowsFocusRail(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)
	app.focusedPane = charmPaneDetails

	rendered := app.View().Content
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "┃") {
		t.Fatalf("focused pane rendered without a visible focus rail:\n%s", rendered)
	}
	if !strings.Contains(rendered, "38;2;94;106;210") {
		t.Fatalf("focused pane rail missing Linear focus color:\n%s", rendered)
	}
	for _, legacy := range []string{"╭", "╮", "╰", "╯"} {
		if strings.Contains(plain, legacy) {
			t.Fatalf("focused pane rail brought back legacy panel border %q:\n%s", legacy, rendered)
		}
	}
}

func TestCharmAppCommandPaletteKeepsWorkspaceVisible(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: ":"}))
	app = model.(CharmApp)

	rendered := app.View().Content
	for _, want := range []string{"Commands", "Navigation", "Issues", "Details", "All Issues"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("palette render missing %q, workspace should remain visible:\n%s", want, rendered)
		}
	}
}

func TestCharmAppLoadingIndicatorStaysInBottomStatus(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)
	app.loading = true
	app.searchQuery = "x"
	app.richFilters = IssueFilters{AssigneeIDs: []string{"me"}, AssigneeNames: []string{"Robin"}}

	rendered := app.View().Content
	if strings.Contains(rendered, "Loading issues") || strings.Contains(rendered, "syncing") {
		t.Fatalf("loading details leaked into top chrome or issue body:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading") {
		t.Fatalf("rendered view missing compact bottom loading indicator:\n%s", rendered)
	}
}

func TestCharmAppDetailsPanelIsWiderByDefault(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.width = 180
	_, _, detailsWidth := app.charmColumnWidths()
	if detailsWidth <= 46 {
		t.Fatalf("detailsWidth = %d, want wider than old cap", detailsWidth)
	}
}

func TestCharmAppMouseDragResizesDetailsPanel(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 32})
	app = model.(CharmApp)
	layout := app.workspaceLayout()
	if layout.detailsDividerX < 0 {
		t.Fatal("details divider missing")
	}

	model, _ = app.Update(tea.MouseClickMsg(tea.Mouse{X: layout.detailsDividerX, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.MouseMotionMsg(tea.Mouse{X: layout.detailsDividerX - 12, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)

	if !app.draggingDetails {
		t.Fatal("draggingDetails = false, want active drag")
	}
	if app.detailsWidth <= layout.detailsWidth {
		t.Fatalf("detailsWidth = %d, want larger than %d after dragging left", app.detailsWidth, layout.detailsWidth)
	}
	model, _ = app.Update(tea.MouseReleaseMsg(tea.Mouse{X: layout.detailsDividerX - 12, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)
	if app.draggingDetails {
		t.Fatal("draggingDetails stayed true after release")
	}
}

func TestCharmAppMouseDragResizesNavigationPanel(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 32})
	app = model.(CharmApp)
	layout := app.workspaceLayout()
	if layout.navDividerX < 0 {
		t.Fatal("navigation divider missing")
	}

	model, _ = app.Update(tea.MouseClickMsg(tea.Mouse{X: layout.navDividerX, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.MouseMotionMsg(tea.Mouse{X: layout.navDividerX + 10, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)

	if !app.draggingNavigation {
		t.Fatal("draggingNavigation = false, want active drag")
	}
	if app.navWidth <= layout.navWidth {
		t.Fatalf("navWidth = %d, want larger than %d after dragging right", app.navWidth, layout.navWidth)
	}
	model, _ = app.Update(tea.MouseReleaseMsg(tea.Mouse{X: layout.navDividerX + 10, Y: layout.bodyTop + 2, Button: tea.MouseLeft}))
	app = model.(CharmApp)
	if app.draggingNavigation {
		t.Fatal("draggingNavigation stayed true after release")
	}
}

func TestCharmAppRendersEmbeddedCalendarPane(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	day := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	app.calendarToday = day
	app.calendarWeekStart = calendar.StartOfWeek(day)
	app.calendarSelectedDay = calendar.DayIndex(app.calendarWeekStart, day)
	app.calendarEvents = []calendar.Event{{
		ID:         "event-1",
		CalendarID: "primary",
		Calendar:   "Home",
		Summary:    "Dev Standup",
		Start:      day,
		End:        day.Add(30 * time.Minute),
		Organizer:  "robin@liquidium.fi",
	}}

	model, _ := app.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	app = model.(CharmApp)
	rendered := app.View().Content

	for _, want := range []string{"Navigation", "Calendar", "Thu Jun 11", "Dev Standup"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered view missing %q:\n%s", want, rendered)
		}
	}
}

func TestCharmAppCalendarPaneUsesDayNavigationBeforePaneNavigation(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	day := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	app.calendarToday = day
	app.calendarWeekStart = calendar.StartOfWeek(day)
	app.calendarSelectedDay = calendar.DayIndex(app.calendarWeekStart, day)
	app.focusedPane = charmPaneCalendar

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "l", Code: 'l'}))
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("calendar same-week day navigation returned cmd=%v, want nil", cmd)
	}
	if app.focusedPane != charmPaneCalendar {
		t.Fatalf("focusedPane = %v, want calendar", app.focusedPane)
	}
	if got := app.calendarSelectedDate(); !calendar.SameDay(got, day.AddDate(0, 0, 1)) {
		t.Fatalf("selected date = %s, want next day", got)
	}

	model, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	app = model.(CharmApp)
	if app.focusedPane == charmPaneCalendar {
		t.Fatal("tab should leave the calendar pane")
	}
}

func TestCharmAppTabCyclesVisiblePanes(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.focusedPane = charmPaneNav

	model, _ := app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	app = model.(CharmApp)
	if app.focusedPane != charmPaneCalendar {
		t.Fatalf("focusedPane after tab = %v, want calendar", app.focusedPane)
	}

	model, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	app = model.(CharmApp)
	if app.focusedPane != charmPaneIssues {
		t.Fatalf("focusedPane after second tab = %v, want issues", app.focusedPane)
	}

	model, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}))
	app = model.(CharmApp)
	if app.focusedPane != charmPaneCalendar {
		t.Fatalf("focusedPane after shift+tab = %v, want calendar", app.focusedPane)
	}
}

func TestCharmAppCalendarCacheLoadsBeforeLiveRefresh(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.calendarCache = calendar.NewCache(filepath.Join(t.TempDir(), "events.json"))
	day := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	app.calendarWeekStart = calendar.StartOfWeek(day)
	cachedAt := day.Add(-time.Hour)
	cached := []calendar.Event{{ID: "cached", CalendarID: "primary", Summary: "Cached", Start: day, End: day.Add(time.Hour)}}

	model, _ := app.Update(charmCalendarLoadedMsg{
		weekStart: app.calendarWeekStart,
		events:    cached,
		fetchedAt: cachedAt,
		fromCache: true,
	})
	app = model.(CharmApp)
	if !app.calendarLoading {
		t.Fatal("cached calendar load should keep live refresh loading")
	}
	if app.calendarCacheTime != cachedAt || len(app.calendarEvents) != 1 || app.calendarEvents[0].ID != "cached" {
		t.Fatalf("cached calendar state = events %+v cacheTime %s", app.calendarEvents, app.calendarCacheTime)
	}

	liveAt := day
	live := []calendar.Event{{ID: "live", CalendarID: "primary", Summary: "Live", Start: day, End: day.Add(time.Hour)}}
	model, _ = app.Update(charmCalendarLoadedMsg{
		weekStart: app.calendarWeekStart,
		events:    live,
		fetchedAt: liveAt,
	})
	app = model.(CharmApp)
	if app.calendarLoading {
		t.Fatal("live calendar load should stop loading")
	}
	if app.calendarCacheTime != liveAt || len(app.calendarEvents) != 1 || app.calendarEvents[0].ID != "live" {
		t.Fatalf("live calendar state = events %+v cacheTime %s", app.calendarEvents, app.calendarCacheTime)
	}
	got, _, ok := app.calendarCache.LoadWeek(app.calendarWeekStart)
	if !ok || len(got) != 1 || got[0].ID != "live" {
		t.Fatalf("cached live events = %+v ok=%v, want live event", got, ok)
	}
}

func TestCharmAppCalendarDeleteIsOptimisticAndRestoresOnFailure(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	day := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	event := calendar.Event{ID: "event-1", CalendarID: "primary", Summary: "Demo", Start: day, End: day.Add(time.Hour)}
	app.calendarWeekStart = calendar.StartOfWeek(day)
	app.calendarSelectedDay = calendar.DayIndex(app.calendarWeekStart, day)
	app.calendarEvents = []calendar.Event{event}
	app.focusedPane = charmPaneCalendar
	app.calendarDeleteFunc = func(_ context.Context, calendarID string, eventID string) error {
		if calendarID != "primary" || eventID != "event-1" {
			t.Fatalf("delete target = %s/%s", calendarID, eventID)
		}
		return errors.New("google refused")
	}

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("calendar delete returned nil cmd")
	}
	if len(app.calendarEvents) != 0 {
		t.Fatalf("calendarEvents = %+v, want optimistic removal", app.calendarEvents)
	}
	msg := cmd()
	model, _ = app.Update(msg)
	app = model.(CharmApp)
	if len(app.calendarEvents) != 1 || app.calendarEvents[0].ID != "event-1" {
		t.Fatalf("calendarEvents after failure = %+v, want restored event", app.calendarEvents)
	}
}

func TestCharmAppDetailsWheelSuppressesBoundaryMomentum(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	app = model.(CharmApp)
	app.details.SetContent(strings.Repeat("details line\n", 100))
	app.details.GotoBottom()
	layout := app.workspaceLayout()
	mouse := tea.Mouse{X: layout.detailsX + 1, Y: layout.bodyTop + 1}

	bottom := app.details.YOffset()
	mouse.Button = tea.MouseWheelDown
	model, _ = app.handleMouseWheel(mouse)
	app = model.(CharmApp)
	if app.details.YOffset() != bottom {
		t.Fatalf("down wheel at bottom moved offset from %d to %d", bottom, app.details.YOffset())
	}

	mouse.Button = tea.MouseWheelUp
	model, _ = app.handleMouseWheel(mouse)
	app = model.(CharmApp)
	afterUp := app.details.YOffset()
	if afterUp >= bottom {
		t.Fatalf("up wheel after bottom momentum did not move immediately: offset=%d bottom=%d", afterUp, bottom)
	}

	mouse.Button = tea.MouseWheelDown
	model, _ = app.handleMouseWheel(mouse)
	app = model.(CharmApp)
	if app.details.YOffset() != afterUp {
		t.Fatalf("stale down momentum was not suppressed: offset=%d want %d", app.details.YOffset(), afterUp)
	}

	app.wheelSuppressTill = time.Now().Add(-time.Millisecond)
	model, _ = app.handleMouseWheel(mouse)
	app = model.(CharmApp)
	if app.details.YOffset() <= afterUp {
		t.Fatalf("down wheel stayed suppressed after expiry: offset=%d afterUp=%d", app.details.YOffset(), afterUp)
	}
}

func TestCharmAppIssueWheelSkipsBoundarySelectionReload(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	app = model.(CharmApp)
	app.myRows = []IssueRow{{IssueID: "issue-1"}}
	app.myIssueMap = map[string]*linearapi.Issue{"issue-1": {ID: "issue-1", Identifier: "LT-1"}}
	app.myTable.SetRows([]table.Row{{"LT-1", "Todo", "-", "Robin", "Title"}})
	app.myTable.SetCursor(0)
	layout := app.workspaceLayout()
	mouse := tea.Mouse{X: layout.issuesX + 1, Y: layout.bodyTop + 2, Button: tea.MouseWheelUp}

	model, cmd := app.handleMouseWheel(mouse)
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("boundary issue wheel returned a detail reload command")
	}
	if app.myTable.Cursor() != 0 {
		t.Fatalf("boundary issue wheel moved cursor to %d", app.myTable.Cursor())
	}
}

func TestCharmAppShowsActiveIssueQueryContext(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.searchQuery = "x"
	app.richFilters = IssueFilters{
		AssigneeIDs:   []string{"me"},
		AssigneeNames: []string{"Robin"},
	}

	contextText := app.issueContextText()
	if !strings.Contains(contextText, "search: x") || !strings.Contains(contextText, "filters: assignee=Robin") {
		t.Fatalf("issue context = %q, want visible search and filter details", contextText)
	}
	if got := app.loadingIssueText(); got != "loading" {
		t.Fatalf("loading text = %q, want compact loading label", got)
	}
}

func TestCharmAppClampsTinyIssueFetchPageSize(t *testing.T) {
	cfg := testCharmConfig()
	cfg.PageSize = 1
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)

	params := app.buildCharmFetchParams()
	if params.First != minIssueFetchPageSize {
		t.Fatalf("fetch page size = %d, want %d", params.First, minIssueFetchPageSize)
	}
}

func TestCharmAppBuildFetchParamsUsesSelectedSort(t *testing.T) {
	cfg := testCharmConfig()
	cfg.IssueSort = string(SortByPriority)
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)

	params := app.buildCharmFetchParams()

	if params.OrderBy != string(SortByPriority) {
		t.Fatalf("OrderBy = %q, want persisted priority sort", params.OrderBy)
	}
	app.sortOverride = SortByCreatedAt
	params = app.buildCharmFetchParams()
	if params.OrderBy != string(SortByCreatedAt) {
		t.Fatalf("OrderBy = %q, want created override", params.OrderBy)
	}
	app.sortOverride = SortByOrder
	params = app.buildCharmFetchParams()
	if params.OrderBy != string(SortByUpdatedAt) {
		t.Fatalf("OrderBy = %q, want updatedAt fallback for Linear order fetch", params.OrderBy)
	}
}

func TestCharmAppIgnoresUnsupportedPersistedIssueSort(t *testing.T) {
	cfg := testCharmConfig()
	cfg.IssueSort = "wat"
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)

	params := app.buildCharmFetchParams()

	if app.sortOverride != "" || params.OrderBy != string(SortByUpdatedAt) {
		t.Fatalf("sortOverride=%q OrderBy=%q, want default updatedAt", app.sortOverride, params.OrderBy)
	}
}

func TestCharmAppBuildsUnsearchedAllMyIssuesCompanionQuery(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	base := linearapi.FetchIssuesParams{
		First:       25,
		Search:      "x",
		TeamID:      "team-1",
		StateTypes:  []string{"backlog", "unstarted", "started"},
		AssigneeIDs: []string{"someone-else"},
		OrderBy:     string(SortByUpdatedAt),
	}

	got, ok := app.buildAllMyIssuesFetchParams(base)
	if !ok {
		t.Fatal("buildAllMyIssuesFetchParams() ok = false, want true")
	}
	if got.Search != "" {
		t.Fatalf("Search = %q, want empty companion query", got.Search)
	}
	if got.AssigneeID != "me" || len(got.AssigneeIDs) != 0 {
		t.Fatalf("assignee filters = id:%q ids:%v, want current user only", got.AssigneeID, got.AssigneeIDs)
	}
	if got.TeamID != "team-1" || strings.Join(got.StateTypes, ",") != "backlog,unstarted,started" {
		t.Fatalf("context filters changed: %+v", got)
	}
}

func TestMergeLinearIssuesAddsMissingMyIssues(t *testing.T) {
	primary := []linearapi.Issue{{ID: "issue-1", Identifier: "LT-1"}}
	extras := []linearapi.Issue{{ID: "issue-1", Identifier: "LT-1"}, {ID: "issue-2", Identifier: "LT-2"}}

	merged := mergeLinearIssues(primary, extras)
	if len(merged) != 2 || merged[0].ID != "issue-1" || merged[1].ID != "issue-2" {
		t.Fatalf("merged = %+v, want primary order with missing extra issue appended", merged)
	}
}

func TestCharmAppHidesNavigationPanelCompletely(t *testing.T) {
	cfg := testCharmConfig()
	cfg.ShowNavigation = false
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	if strings.Contains(rendered, "Navigation") || strings.Contains(rendered, "All Issues") {
		t.Fatalf("hidden navigation still rendered:\n%s", rendered)
	}

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "h"}))
	app = model.(CharmApp)
	if app.focusedPane == charmPaneNav {
		t.Fatal("focus moved to hidden navigation pane")
	}
}

func TestCharmAppDefaultsFocusToVisiblePane(t *testing.T) {
	cfg := testCharmConfig()
	cfg.ShowMyIssues = false
	cfg.ShowOtherIssues = false
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)

	if app.focusedPane != charmPaneNav {
		t.Fatalf("focusedPane = %v, want visible navigation", app.focusedPane)
	}

	cfg.ShowNavigation = false
	app = NewCharmApp(&linearapi.Client{}, cfg, nil)
	if app.focusedPane != charmPaneDetails {
		t.Fatalf("focusedPane = %v, want visible details", app.focusedPane)
	}
}

func TestCharmAppNavigationIncludesExpandedTeamMetadata(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.teams = []linearapi.Team{{ID: "team-1", Name: "Platform"}}
	app.expandedTeams["team-1"] = true
	app.teamChildren["team-1"] = charmTeamChildNodes(
		"team-1",
		[]linearapi.Project{{ID: "project-1", Name: "Launch"}},
		[]linearapi.WorkflowState{{ID: "state-1", Name: "Todo", Position: 1}},
		[]linearapi.Cycle{{ID: "cycle-1", Name: "Sprint", IsActive: true}},
	)
	app.rebuildNavigation()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	navText := ""
	for _, node := range app.navigation {
		navText += node.Text + "\n"
	}
	for _, want := range []string{"Platform", "Cycle: Sprint (active)", "Status: Todo", "Launch"} {
		if !strings.Contains(navText, want) {
			t.Fatalf("expanded navigation missing %q:\n%s", want, navText)
		}
	}

	app.selectedNavigation = &NavigationNode{ID: "state-1", TeamID: "team-1", IsStatus: true, StateID: "state-1"}
	params := app.buildCharmFetchParams()
	if params.TeamID != "team-1" || params.StateID != "state-1" {
		t.Fatalf("params = %+v, want selected team status filter", params)
	}
}

func TestCharmAppSetIssuesSplitsMyAndOtherIssues(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}

	myIssue := linearapi.Issue{
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Assigned to me",
		State:      "Todo",
		AssigneeID: "me",
		Assignee:   "Robin",
		UpdatedAt:  time.Now(),
	}
	otherIssue := linearapi.Issue{
		ID:         "issue-2",
		Identifier: "LT-2",
		Title:      "Assigned elsewhere",
		State:      "Todo",
		AssigneeID: "someone-else",
		Assignee:   "Ada",
		UpdatedAt:  time.Now().Add(-time.Hour),
	}

	app.setIssues([]linearapi.Issue{otherIssue, myIssue}, "")

	if len(app.myRows) != 1 || app.myRows[0].IssueID != myIssue.ID {
		t.Fatalf("myRows = %+v, want %s", app.myRows, myIssue.ID)
	}
	if len(app.otherRows) != 1 || app.otherRows[0].IssueID != otherIssue.ID {
		t.Fatalf("otherRows = %+v, want %s", app.otherRows, otherIssue.ID)
	}
	if app.selectedIssue == nil || app.selectedIssue.ID != myIssue.ID {
		t.Fatalf("selectedIssue = %+v, want my issue", app.selectedIssue)
	}
}

func TestCharmAppSortPickerAppliesLinearOrder(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	first := linearapi.Issue{ID: "issue-1", Identifier: "LT-1", Title: "First", SortOrder: 20, UpdatedAt: time.Now()}
	second := linearapi.Issue{ID: "issue-2", Identifier: "LT-2", Title: "Second", SortOrder: 10, UpdatedAt: time.Now().Add(time.Hour)}
	app.setIssues([]linearapi.Issue{first, second}, "")

	model, cmd := app.runCharmCommand("sort_issues")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayPicker || app.pickerAction != charmPickerIssueSort {
		t.Fatalf("sort command overlay=%v action=%v cmd=%v", app.overlay, app.pickerAction, cmd)
	}
	model, cmd = app.applyPickerSelection(charmPickerIssueSort, charmPickerItem{ID: string(SortByOrder), Label: "Linear order"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("sort selection returned nil cmd, want persisted sort save")
	}
	if app.sortOverride != SortByOrder {
		t.Fatalf("sortOverride = %q, want order", app.sortOverride)
	}
	if len(app.otherRows) != 2 || app.otherRows[0].IssueID != "issue-2" {
		t.Fatalf("otherRows = %+v, want issue-2 first by Linear order", app.otherRows)
	}
}

func TestCharmAppIssueRowsUseVisualStatusAndPriorityCues(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	issue := linearapi.Issue{
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Styled",
		State:      "In Progress",
		Priority:   1,
		UpdatedAt:  time.Now(),
	}
	app.setIssues([]linearapi.Issue{issue}, "")

	rows := app.otherTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one row", rows)
	}
	if !strings.Contains(rows[0][1], "●") || !strings.Contains(rows[0][1], "In Progress") {
		t.Fatalf("state cell = %q, want progress marker and label", rows[0][1])
	}
	if !strings.Contains(rows[0][2], "!! Urgent") {
		t.Fatalf("priority cell = %q, want urgent marker", rows[0][2])
	}
}

func TestCharmAppIssueTableShowsTitleBeforeAssignee(t *testing.T) {
	columns := issueTableColumns()
	if len(columns) < 5 {
		t.Fatalf("columns = %+v, want at least five", columns)
	}
	if columns[3].Title != "Title" || columns[4].Title != "Assignee" {
		t.Fatalf("columns[3:5] = %q, %q; want Title then Assignee", columns[3].Title, columns[4].Title)
	}

	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.setIssues([]linearapi.Issue{{
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Important title",
		Assignee:   "Robin",
		UpdatedAt:  time.Now(),
	}}, "")

	rows := app.otherTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one row", rows)
	}
	if rows[0][3] != "Important title" || rows[0][4] != "Robin" {
		t.Fatalf("row title/assignee = %q/%q, want title before assignee", rows[0][3], rows[0][4])
	}
}

func TestCharmAppIssueRowsShowDueToday(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	today := todayLinearDate()
	app.setIssues([]linearapi.Issue{{
		ID:         "issue-0",
		Identifier: "LT-0",
		Title:      "Selected normal issue",
		State:      "Todo",
		UpdatedAt:  time.Now(),
	}, {
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Handle today",
		State:      "Todo",
		DueDate:    &today,
		UpdatedAt:  time.Now(),
	}}, "")
	app.selectIssueByID("issue-0")

	rows := app.otherTable.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two rows", rows)
	}
	var dueColumn string
	for _, row := range rows {
		if strings.Contains(row[0], "LT-1") {
			dueColumn = row[5]
			break
		}
	}
	if !strings.Contains(dueColumn, "Today") {
		t.Fatalf("due column = %q, want Today in LT-1 row %+v", dueColumn, rows)
	}
	rendered := app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable)
	if !strings.Contains(rendered, "Today") || !strings.Contains(rendered, "38;2;242;201;76") {
		t.Fatalf("due-today row was not visibly highlighted:\n%s", rendered)
	}
}

func TestCharmAppIssueRowsShowOverdueDatesInOrange(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	overdue := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	app.setIssues([]linearapi.Issue{{
		ID:         "issue-0",
		Identifier: "LT-0",
		Title:      "Selected normal issue",
		State:      "Todo",
		UpdatedAt:  time.Now(),
	}, {
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Handle yesterday",
		State:      "Todo",
		DueDate:    &overdue,
		UpdatedAt:  time.Now(),
	}}, "")
	app.selectIssueByID("issue-0")

	rendered := app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable)
	if !strings.Contains(rendered, overdue) || !strings.Contains(rendered, "38;2;255;159;28") {
		t.Fatalf("overdue row was not visibly highlighted orange:\n%s", rendered)
	}
}

func TestCharmAppSelectedIssueRowSpansTableWidth(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 180, Height: 32})
	app = model.(CharmApp)
	app.activeSection = IssuesSectionOther
	app.setIssues([]linearapi.Issue{{
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Selected row should fill the issue pane",
		State:      "Todo",
		UpdatedAt:  time.Now(),
	}}, "")
	app.otherTable.Focus()
	app.otherTable.SetCursor(0)
	app.resizeComponents()

	view := app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable)
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("table view rendered too few lines:\n%s", view)
	}
	if got, want := lipgloss.Width(lines[1]), app.otherTable.Width(); got < want {
		t.Fatalf("selected row width = %d, want at least table width %d:\n%s", got, want, view)
	}
	if !strings.Contains(lines[1], "48;2;") {
		t.Fatalf("selected row has no background color escape:\n%q", lines[1])
	}
	if resets := strings.Count(lines[1], "\x1b[m"); resets > 1 {
		t.Fatalf("selected row background is interrupted by %d ANSI resets:\n%q", resets, lines[1])
	}
}

func TestCharmAppMainViewHighlightsSelectedIssueRow(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	if !strings.Contains(rendered, "48;2;48;50;79") || !strings.Contains(rendered, "LT-1") {
		t.Fatalf("main view missing selected issue highlight:\n%s", rendered)
	}
	if strings.Contains(rendered, "› LT-1") {
		t.Fatalf("main view rendered a redundant selected-row marker:\n%s", rendered)
	}
}

func TestCharmAppDetailsHeadingStaysMinimal(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	app = model.(CharmApp)

	rendered := app.View().Content
	if strings.Contains(rendered, "SELECTED") {
		t.Fatalf("details pane rendered redundant selected badge:\n%s", rendered)
	}
}

func TestCharmAppDetailsShowsCreatedAndEditedDates(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	issue := app.currentIssue()
	issue.CreatedAt = time.Date(2026, 6, 8, 9, 10, 0, 0, time.UTC)
	issue.UpdatedAt = time.Date(2026, 6, 9, 11, 12, 0, 0, time.UTC)
	app.selectedIssue = issue

	app.updateDetailsContent()
	content := app.details.GetContent()
	for _, want := range []string{"Created: 2026-06-08 09:10", "Edited: 2026-06-09 11:12"} {
		if !strings.Contains(content, want) {
			t.Fatalf("details missing %q:\n%s", want, content)
		}
	}
}

func TestCharmAppPickerOverlayUsesBackgroundSelection(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.pickerTitle = "Change Status"
	app.pickerCursor = 1
	app.pickerItems = []charmPickerItem{
		{ID: "todo", Label: "Todo"},
		{ID: "progress", Label: "In Progress"},
	}

	rendered := app.renderPickerOverlay()
	if strings.Contains(rendered, "> In Progress") {
		t.Fatalf("picker overlay rendered prefix marker instead of a clean selected row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "48;2;48;50;79") {
		t.Fatalf("picker overlay selected row has no background highlight:\n%s", rendered)
	}
}

func TestCharmAppEnterExpandsIssueChildRefs(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	parent := linearapi.Issue{
		ID:         "parent-1",
		Identifier: "LT-1",
		Title:      "Parent",
		AssigneeID: "me",
		Assignee:   "Robin",
		TeamID:     "team-1",
		Children: []linearapi.IssueChildRef{
			{ID: "child-1", Identifier: "LT-2", Title: "Child", State: "Todo"},
		},
		UpdatedAt: time.Now(),
	}
	app.setIssues([]linearapi.Issue{parent}, "")
	app.focusedPane = charmPaneIssues
	app.activeSection = IssuesSectionMy
	app.myTable.Focus()
	app.myTable.SetCursor(0)

	model, cmd := app.handleIssuesKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatalf("expand returned command %v, want nil", cmd)
	}
	if !app.expanded["parent-1"] {
		t.Fatal("parent was not marked expanded")
	}
	if len(app.myRows) != 2 {
		t.Fatalf("myRows = %+v, want parent plus child", app.myRows)
	}
	if app.myRows[1].IssueID != "child-1" || app.myRows[1].Level != 1 {
		t.Fatalf("child row = %+v, want expanded level-1 child", app.myRows[1])
	}
	if child := app.myIssueMap["child-1"]; child == nil || child.Identifier != "LT-2" {
		t.Fatalf("child issue map entry = %+v, want synthetic child LT-2", child)
	}
}

func TestCharmAppSearchModeUpdatesQuery(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(CharmApp)

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "/"}))
	app = model.(CharmApp)
	if !app.searchMode {
		t.Fatal("search mode was not enabled")
	}

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "a"}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "b"}))
	app = model.(CharmApp)

	if app.searchQuery != "ab" {
		t.Fatalf("searchQuery = %q, want ab", app.searchQuery)
	}
	if !strings.Contains(app.View().Content, "Search: ab") {
		t.Fatalf("rendered search status missing query:\n%s", app.View().Content)
	}
}

func TestCharmAppAppliesPersistedIssueFilters(t *testing.T) {
	cfg := testCharmConfig()
	cfg.IssueSearchQuery = "saved"
	cfg.IssueFilters = config.IssueFilterSettings{
		TeamIDs:  []string{"team-1", "team-2"},
		StateIDs: []string{"state-1"},
	}
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)

	params := app.buildCharmFetchParams()

	if params.Search != "saved" {
		t.Fatalf("Search = %q, want saved", params.Search)
	}
	if strings.Join(params.TeamIDs, ",") != "team-1,team-2" {
		t.Fatalf("TeamIDs = %+v, want persisted teams", params.TeamIDs)
	}
	if len(params.StateIDs) != 1 || params.StateIDs[0] != "state-1" {
		t.Fatalf("StateIDs = %+v, want persisted state", params.StateIDs)
	}
	if len(params.StateTypes) != 0 {
		t.Fatalf("StateTypes = %+v, want rich state filter to override defaults", params.StateTypes)
	}
}

func TestCharmAppIssueContextResolvesPersistedTeamFilterNames(t *testing.T) {
	cfg := testCharmConfig()
	cfg.IssueFilters = config.IssueFilterSettings{
		TeamIDs: []string{"team-1", "team-2"},
	}
	app := NewCharmApp(&linearapi.Client{}, cfg, nil)
	model, _ := app.Update(charmInitialLoadedMsg{
		currentUser: &linearapi.User{ID: "me", DisplayName: "Robin"},
		teams: []linearapi.Team{
			{ID: "team-1", Name: "Platform", Key: "PLT"},
			{ID: "team-2", Name: "Backend", Key: "BD"},
		},
	})
	app = model.(CharmApp)

	context := app.issueContextText()
	if strings.Contains(context, "team-1") || strings.Contains(context, "team-2") {
		t.Fatalf("issue context leaked team IDs: %q", context)
	}
	if !strings.Contains(context, "team=Platform (PLT),Backend (BD)") {
		t.Fatalf("issue context = %q, want resolved team names", context)
	}
}

func TestCharmAppPersistsSearchQuery(t *testing.T) {
	settingsPath := t.TempDir() + "/config.json"
	settings := config.DefaultSettings()
	settings.LinearAPIKey = "old-key"
	if err := config.SaveSettings(settingsPath, settings); err != nil {
		t.Fatalf("SaveSettings() error: %v", err)
	}
	app := NewCharmAppWithSettingsPath(&linearapi.Client{}, testCharmConfig(), nil, settingsPath)
	app.searchQuery = "saved filter"
	app.sortOverride = SortByPriority
	app.richFilters = IssueFilters{TeamIDs: []string{"team-1"}, TeamNames: []string{"Platform"}}

	msg := app.persistSearchQueryCmd()()
	if persisted, ok := msg.(charmSettingsPersistedMsg); !ok || persisted.err != nil {
		t.Fatalf("persistSearchQueryCmd() = %#v, want success", msg)
	}
	updated, err := config.LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}
	if updated.LinearAPIKey != "old-key" {
		t.Fatalf("LinearAPIKey = %q, want preserved key", updated.LinearAPIKey)
	}
	if updated.IssueSearchQuery != "saved filter" {
		t.Fatalf("IssueSearchQuery = %q, want saved filter", updated.IssueSearchQuery)
	}
	if updated.IssueSort != string(SortByPriority) {
		t.Fatalf("IssueSort = %q, want priority", updated.IssueSort)
	}
	if len(updated.IssueFilters.TeamIDs) != 1 || updated.IssueFilters.TeamIDs[0] != "team-1" {
		t.Fatalf("IssueFilters.TeamIDs = %+v, want persisted team filter", updated.IssueFilters.TeamIDs)
	}
}

func TestCharmAppSavesAPIKeyWithoutDroppingSettings(t *testing.T) {
	settingsPath := t.TempDir() + "/config.json"
	settings := config.DefaultSettings()
	settings.LogLevel = "error"
	settings.IssueSearchQuery = "existing"
	if err := config.SaveSettings(settingsPath, settings); err != nil {
		t.Fatalf("SaveSettings() error: %v", err)
	}
	cfg := testCharmConfig()
	cfg.LinearAPIKey = ""
	app := NewCharmAppWithSettingsPath(&linearapi.Client{}, cfg, nil, settingsPath)

	msg := app.saveAPIKeyCmd("new-key")()
	saved, ok := msg.(charmAPIKeySavedMsg)
	if !ok {
		t.Fatalf("saveAPIKeyCmd() = %#v, want charmAPIKeySavedMsg", msg)
	}
	if saved.err != nil {
		t.Fatalf("saveAPIKeyCmd() error: %v", saved.err)
	}
	if saved.cfg.LinearAPIKey != "new-key" {
		t.Fatalf("cfg.LinearAPIKey = %q, want new key", saved.cfg.LinearAPIKey)
	}
	updated, err := config.LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}
	if updated.LinearAPIKey != "new-key" {
		t.Fatalf("LinearAPIKey = %q, want new key", updated.LinearAPIKey)
	}
	if updated.LogLevel != "error" || updated.IssueSearchQuery != "existing" {
		t.Fatalf("settings were not preserved: %+v", updated)
	}
}

func TestCharmAppSettingsOverlaySavesAndPreservesAPIKey(t *testing.T) {
	settingsPath := t.TempDir() + "/config.json"
	settings := config.DefaultSettings()
	settings.LinearAPIKey = "existing-key"
	settings.ShowNavigation = true
	if err := config.SaveSettings(settingsPath, settings); err != nil {
		t.Fatalf("SaveSettings() error: %v", err)
	}
	app := NewCharmAppWithSettingsPath(&linearapi.Client{}, testCharmConfig(), nil, settingsPath)
	app.focusedPane = charmPaneNav

	model, cmd := app.runCharmCommand("settings")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("settings command should focus settings input")
	}
	if app.overlay != charmOverlaySettings || len(app.settingsFields) == 0 {
		t.Fatalf("settings overlay not opened: overlay=%v fields=%d", app.overlay, len(app.settingsFields))
	}
	setCharmSettingForTest(t, &app, "page_size", "75")
	setCharmSettingForTest(t, &app, "show_navigation", "false")
	setCharmSettingForTest(t, &app, "theme", config.ThemeHighContrast)
	updatedSettings, err := app.settingsFromCharmFields()
	if err != nil {
		t.Fatalf("settingsFromCharmFields() error: %v", err)
	}

	msg := app.saveSettingsCmd(updatedSettings)()
	saved, ok := msg.(charmSettingsSavedMsg)
	if !ok {
		t.Fatalf("saveSettingsCmd() = %#v, want charmSettingsSavedMsg", msg)
	}
	if saved.err != nil {
		t.Fatalf("saveSettingsCmd() error: %v", saved.err)
	}
	persisted, err := config.LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}
	if persisted.LinearAPIKey != "existing-key" {
		t.Fatalf("LinearAPIKey = %q, want preserved key", persisted.LinearAPIKey)
	}
	if persisted.PageSize != 75 || persisted.ShowNavigation || persisted.Theme != config.ThemeHighContrast {
		t.Fatalf("persisted settings = %+v, want changed page size/navigation/theme", persisted)
	}

	model, cmd = app.Update(saved)
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("settings save update should refresh issues")
	}
	if app.cfg.PageSize != 75 || app.cfg.ShowNavigation || app.focusedPane == charmPaneNav {
		t.Fatalf("app state after settings save cfg=%+v focused=%v", app.cfg, app.focusedPane)
	}
}

func TestCharmAppSettingsOverlayCyclesOptions(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.openSettingsForm(config.DefaultSettings())
	index := charmSettingIndexForTest(t, app, "include_completed")
	app.settingsCursor = index
	app.syncSettingsInput()

	model, _ := app.handleSettingsKey(tea.KeyPressMsg(tea.Key{Text: " ", Code: tea.KeySpace}))
	app = model.(CharmApp)
	if app.settingsFields[index].Value != "true" {
		t.Fatalf("include_completed = %q, want true after cycle", app.settingsFields[index].Value)
	}
	if !strings.Contains(app.renderSettingsOverlay(), "Include completed") {
		t.Fatal("settings overlay render missing include completed row")
	}
}

func TestCharmAppCustomViewAddEditAndDelete(t *testing.T) {
	viewsPath := t.TempDir() + "/views.json"
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.customViewsPath = viewsPath
	app.selectedNavigation = &NavigationNode{ID: "team-1", Text: "Platform", TeamID: "team-1", IsTeam: true}

	model, cmd := app.runCharmCommand("add_custom_view")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("add custom view command should focus custom view input")
	}
	if app.overlay != charmOverlayCustomView || len(app.customViewFields) == 0 {
		t.Fatalf("custom view overlay not opened: overlay=%v fields=%d", app.overlay, len(app.customViewFields))
	}
	setCharmCustomViewFieldForTest(t, &app, "name", "Platform Bugs")
	setCharmCustomViewFieldForTest(t, &app, "state_mode", string(config.CustomViewStateNotDone))
	view, err := app.customViewFromCharmFields()
	if err != nil {
		t.Fatalf("customViewFromCharmFields() error: %v", err)
	}
	if view.TeamID != "team-1" || view.StateMode != config.CustomViewStateNotDone {
		t.Fatalf("view = %+v, want current team and not-done state mode", view)
	}

	msg := app.saveCustomViewCmd(view)()
	saved, ok := msg.(charmCustomViewsSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("saveCustomViewCmd() = %#v, want success", msg)
	}
	if len(saved.views) != 1 || saved.views[0].Name != "Platform Bugs" || saved.selectedViewID == "" {
		t.Fatalf("saved = %+v, want one selected custom view", saved)
	}
	persisted, err := config.LoadCustomViews(viewsPath)
	if err != nil {
		t.Fatalf("LoadCustomViews() error: %v", err)
	}
	if len(persisted) != 1 || persisted[0].TeamID != "team-1" {
		t.Fatalf("persisted = %+v, want team-scoped view", persisted)
	}

	model, cmd = app.Update(saved)
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("custom view save should refresh issues")
	}
	if app.selectedCustomView == nil || app.selectedCustomView.ID != saved.selectedViewID {
		t.Fatalf("selectedCustomView = %+v, want saved view", app.selectedCustomView)
	}
	params := app.buildCharmFetchParams()
	if params.TeamID != "team-1" || len(params.StateTypes) == 0 {
		t.Fatalf("params = %+v, want saved custom view filters", params)
	}

	model, cmd = app.runCharmCommand("edit_custom_view")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("edit custom view command should focus custom view input")
	}
	setCharmCustomViewFieldForTest(t, &app, "name", "Renamed View")
	edited, err := app.customViewFromCharmFields()
	if err != nil {
		t.Fatalf("customViewFromCharmFields() edit error: %v", err)
	}
	if edited.ID != saved.selectedViewID {
		t.Fatalf("edited ID = %q, want existing %q", edited.ID, saved.selectedViewID)
	}
	editMsg := app.saveCustomViewCmd(edited)()
	editSaved, ok := editMsg.(charmCustomViewsSavedMsg)
	if !ok || editSaved.err != nil {
		t.Fatalf("edit save msg = %#v, want success", editMsg)
	}
	if len(editSaved.views) != 1 || editSaved.views[0].Name != "Renamed View" {
		t.Fatalf("editSaved.views = %+v, want renamed single view", editSaved.views)
	}

	app.customViews = editSaved.views
	app.selectedCustomView = &app.customViews[0]
	model, cmd = app.runCharmCommand("delete_custom_view")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayConfirmDeleteView {
		t.Fatalf("delete should open confirmation overlay=%v cmd=%v", app.overlay, cmd)
	}
	model, cmd = app.handleConfirmDeleteViewKey(tea.KeyPressMsg(tea.Key{Text: "y"}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("delete confirmation returned nil cmd")
	}
	deleteMsg := cmd()
	deleted, ok := deleteMsg.(charmCustomViewsSavedMsg)
	if !ok || deleted.err != nil {
		t.Fatalf("delete msg = %#v, want success", deleteMsg)
	}
	if len(deleted.views) != 0 || deleted.selectedViewID != "" {
		t.Fatalf("deleted = %+v, want no views and all issues selection", deleted)
	}
}

func TestCharmAppCommandPaletteFiltersCommands(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(CharmApp)

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: ":"}))
	app = model.(CharmApp)
	if app.overlay != charmOverlayPalette {
		t.Fatalf("overlay = %v, want command palette", app.overlay)
	}
	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "p"}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "r"}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "i"}))
	app = model.(CharmApp)
	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: "o"}))
	app = model.(CharmApp)

	filtered := app.filteredCharmCommands()
	if len(filtered) != 1 || filtered[0].ID != "change_priority" {
		t.Fatalf("filtered commands = %+v, want change priority", filtered)
	}
	if !strings.Contains(app.View().Content, "Change priority") {
		t.Fatalf("palette render missing filtered command:\n%s", app.View().Content)
	}
}

func TestCharmAppCommandPaletteScrollsFilteredCommands(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(CharmApp)

	model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: ":"}))
	app = model.(CharmApp)
	for _, r := range "filter" {
		model, _ = app.Update(tea.KeyPressMsg(tea.Key{Text: string(r)}))
		app = model.(CharmApp)
	}
	filtered := app.filteredCharmCommands()
	if len(filtered) <= 10 {
		t.Fatalf("filtered command count = %d, want enough results to exercise scrolling", len(filtered))
	}
	first := filtered[0].Title
	target := filtered[len(filtered)-1].Title
	for range filtered {
		model, _ = app.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
		app = model.(CharmApp)
	}

	rendered := app.View().Content
	if strings.Contains(rendered, "...and") {
		t.Fatalf("palette still renders synthetic overflow text:\n%s", rendered)
	}
	if strings.Contains(rendered, first) {
		t.Fatalf("palette did not scroll first filtered command %q out of view:\n%s", first, rendered)
	}
	if !strings.Contains(rendered, target) {
		t.Fatalf("palette render missing last filtered command %q after scrolling:\n%s", target, rendered)
	}
}

func TestCharmAppFilterTeamClearsTeamScopedFilters(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.teams = []linearapi.Team{{ID: "team-1", Name: "Platform", Key: "PLT"}}
	app.richFilters = IssueFilters{
		AssigneeIDs:   []string{"user-1"},
		LabelIDs:      []string{"label-1"},
		StateIDs:      []string{"state-1"},
		ProjectIDs:    []string{"project-1"},
		CycleIDs:      []string{"cycle-1"},
		AssigneeNames: []string{"Ada"},
	}

	model, cmd := app.runCharmCommand("filter_team")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayMultiSelect || app.multiAction != charmMultiSelectFilterTeam {
		t.Fatalf("filter team overlay=%v action=%v cmd=%v", app.overlay, app.multiAction, cmd)
	}
	model, cmd = app.applyMultiSelectSelection(charmMultiSelectFilterTeam, []string{"team-1"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("team filter should persist and reload")
	}
	if len(app.richFilters.TeamIDs) != 1 || app.richFilters.TeamIDs[0] != "team-1" {
		t.Fatalf("TeamIDs = %+v, want team-1", app.richFilters.TeamIDs)
	}
	if len(app.richFilters.AssigneeIDs) != 0 || len(app.richFilters.LabelIDs) != 0 || len(app.richFilters.StateIDs) != 0 ||
		len(app.richFilters.ProjectIDs) != 0 || len(app.richFilters.CycleIDs) != 0 {
		t.Fatalf("team-scoped filters were not cleared: %+v", app.richFilters)
	}
	if !strings.Contains(app.richFilters.Summary(), "team=Platform (PLT)") {
		t.Fatalf("filter summary = %q, want team label", app.richFilters.Summary())
	}
}

func TestCharmAppFilterTextDueDateAndEstimateForms(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)

	model, cmd := app.runCharmCommand("filter_text")
	app = model.(CharmApp)
	if cmd == nil || app.overlay != charmOverlayIssueForm || app.formMode != charmFormFilterText {
		t.Fatalf("filter text state overlay=%v mode=%v cmd=%v", app.overlay, app.formMode, cmd)
	}
	app.titleInput.SetValue("bug bash")
	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil || app.searchQuery != "bug bash" {
		t.Fatalf("text filter search=%q cmd=%v, want applied search", app.searchQuery, cmd)
	}

	model, _ = app.runCharmCommand("filter_due_date")
	app = model.(CharmApp)
	app.titleInput.SetValue("2026-06-15")
	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil || app.richFilters.DueDate.Eq != "2026-06-15" {
		t.Fatalf("due filter = %+v cmd=%v, want date", app.richFilters.DueDate, cmd)
	}

	model, _ = app.runCharmCommand("filter_estimate")
	app = model.(CharmApp)
	app.titleInput.SetValue("2.5")
	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil || app.richFilters.Estimate.Eq == nil || *app.richFilters.Estimate.Eq != 2.5 {
		t.Fatalf("estimate filter = %+v cmd=%v, want 2.5", app.richFilters.Estimate, cmd)
	}
}

func TestCharmAppFilterLabelsUsesSelectedTeamContext(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.richFilters.TeamIDs = []string{"team-1"}
	app.loadLabelsFunc = func(_ context.Context, teamID string) ([]linearapi.IssueLabel, error) {
		if teamID != "team-1" {
			t.Fatalf("teamID = %q, want team-1", teamID)
		}
		return []linearapi.IssueLabel{{ID: "label-1", Name: "Bug"}}, nil
	}

	model, cmd := app.runCharmCommand("filter_labels")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("filter labels returned nil load cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(CharmApp)
	if app.overlay != charmOverlayMultiSelect || app.multiAction != charmMultiSelectFilterLabel || len(app.multiItems) != 1 {
		t.Fatalf("label filter overlay=%v action=%v items=%+v", app.overlay, app.multiAction, app.multiItems)
	}
	model, cmd = app.applyMultiSelectSelection(charmMultiSelectFilterLabel, []string{"label-1"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("saving label filter returned nil cmd")
	}
	if len(app.richFilters.LabelIDs) != 1 || app.richFilters.LabelNames[0] != "Bug" {
		t.Fatalf("label filters = %+v names=%+v, want Bug", app.richFilters.LabelIDs, app.richFilters.LabelNames)
	}
}

func TestCharmAppAssignMeCommandBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("assign_me")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("assign me command returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.AssigneeID != "me" || issue.Assignee != "Robin" {
		t.Fatalf("current issue = %+v, want optimistic assignment to Robin", issue)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("assign me msg = %#v, want successful update", msg)
	}
	if got.ID != "issue-1" || got.AssigneeID == nil || *got.AssigneeID != "me" {
		t.Fatalf("UpdateIssueInput = %+v, want issue assigned to me", got)
	}
	if !app.loading {
		t.Fatal("assign command should mark app loading")
	}
}

func TestCharmAppPriorityPickerBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("change_priority")
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("change priority should open a local picker, not run a command yet")
	}
	if app.overlay != charmOverlayPicker || len(app.pickerItems) == 0 {
		t.Fatalf("priority picker not opened: overlay=%v items=%+v", app.overlay, app.pickerItems)
	}
	model, cmd = app.applyPickerSelection(charmPickerPriority, charmPickerItem{Label: "High", Priority: 2})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("priority picker returned nil update cmd")
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackPriority {
		t.Fatalf("priority msg = %#v, want successful update", msg)
	}
	if got.ID != "issue-1" || got.Priority == nil || *got.Priority != 2 {
		t.Fatalf("UpdateIssueInput = %+v, want priority 2", got)
	}
	if !app.loading {
		t.Fatal("priority update should mark app loading")
	}
	if issue := app.currentIssue(); issue == nil || issue.Priority != 2 {
		t.Fatalf("current issue = %+v, want optimistic priority 2", issue)
	}
}

func TestCharmAppPriorityShortcutOpensPriorityPicker(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	model, cmd := app.Update(tea.KeyPressMsg(tea.Key{Text: "p", Code: 'p'}))
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("priority shortcut returned cmd=%v, want local picker only", cmd)
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerPriority || app.pickerTitle != "Change Priority" {
		t.Fatalf("priority shortcut overlay=%v action=%v title=%q", app.overlay, app.pickerAction, app.pickerTitle)
	}
	if len(app.pickerItems) == 0 {
		t.Fatal("priority shortcut opened empty picker")
	}
}

func TestCharmAppPriorityShortcutDoesNotHijackSearchInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.searchMode = true

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "p", Code: 'p'}))
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("search input returned cmd %v, want nil", cmd)
	}
	if app.overlay != charmOverlayNone || app.searchQuery != "p" {
		t.Fatalf("search input hijacked by priority shortcut: overlay=%v query=%q", app.overlay, app.searchQuery)
	}
}

func TestCharmAppAssigneeAndCyclePickersOptimisticallyUpdateIssue(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got []linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = append(got, input)
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerAssignee, charmPickerItem{ID: "user-2", Label: "Ada"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("assignee picker returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.AssigneeID != "user-2" || issue.Assignee != "Ada" {
		t.Fatalf("current issue = %+v, want optimistic assignee Ada", issue)
	}
	if updated, ok := cmd().(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("assignee msg = %#v, want optimistic snapshot update", updated)
	}

	model, cmd = app.applyPickerSelection(charmPickerCycle, charmPickerItem{ID: "cycle-1", Label: "Sprint"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("cycle picker returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Cycle == nil || issue.Cycle.ID != "cycle-1" {
		t.Fatalf("current issue = %+v, want optimistic cycle", issue)
	}
	if updated, ok := cmd().(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("cycle msg = %#v, want optimistic snapshot update", updated)
	}

	if len(got) != 2 || got[0].AssigneeID == nil || *got[0].AssigneeID != "user-2" || got[1].CycleID == nil || *got[1].CycleID != "cycle-1" {
		t.Fatalf("UpdateIssueInput calls = %+v, want assignee then cycle", got)
	}
}

func TestCharmAppDueTodayShortcutOptimisticallySetsDueDate(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "t", Code: 't'}))
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("due today shortcut returned nil cmd")
	}
	today := todayLinearDate()
	if issue := app.currentIssue(); issue == nil || issue.DueDate == nil || *issue.DueDate != today {
		t.Fatalf("current issue = %+v, want optimistic due date %s", issue, today)
	}
	if app.undo == nil || app.undo.Input.DueDate == nil || *app.undo.Input.DueDate != "" {
		t.Fatalf("undo action = %+v, want clear due date input", app.undo)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackDueDate {
		t.Fatalf("due today msg = %#v, want successful optimistic update message", msg)
	}
	if got.ID != "issue-1" || got.DueDate == nil || *got.DueDate != today {
		t.Fatalf("UpdateIssueInput = %+v, want due date %s", got, today)
	}
}

func TestCharmAppClearDueDateOptimisticallyUpdatesIssueList(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	today := todayLinearDate()
	app.applyIssueDueDate("issue-1", &today)
	if rendered := app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable); !strings.Contains(rendered, "Today") {
		t.Fatalf("test setup did not render due today:\n%s", rendered)
	}
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("clear_due_date")
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("clear due date returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.DueDate == nil || *issue.DueDate != "" {
		t.Fatalf("current issue = %+v, want optimistic empty due date", issue)
	}
	if rendered := app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable); strings.Contains(rendered, "Today") {
		t.Fatalf("issue list still rendered Today after optimistic clear:\n%s", rendered)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackDueDate {
		t.Fatalf("clear due date msg = %#v, want successful optimistic update message", msg)
	}
	if got.ID != "issue-1" || got.DueDate == nil || *got.DueDate != "" {
		t.Fatalf("UpdateIssueInput = %+v, want empty due date clear", got)
	}
}

func TestCharmAppDueTodayShortcutDoesNotHijackSearchInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.searchMode = true

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "t", Code: 't'}))
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("search input returned cmd %v, want nil", cmd)
	}
	if app.overlay != charmOverlayNone || app.searchQuery != "t" {
		t.Fatalf("search input hijacked by due today shortcut: overlay=%v query=%q", app.overlay, app.searchQuery)
	}
}

func TestCharmAppStatusPickerOptimisticallyUpdatesIssue(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerStatus, charmPickerItem{ID: "state-progress", Label: "In Progress"})
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("status picker returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.State != "In Progress" || issue.StateID != "state-progress" {
		t.Fatalf("current issue = %+v, want optimistic In Progress", issue)
	}
	if app.selectedIssue == nil || app.selectedIssue.State != "In Progress" {
		t.Fatalf("selected issue = %+v, want optimistic status", app.selectedIssue)
	}
	if !strings.Contains(app.renderIssueTable(app.otherRows, app.otherIssueMap, app.otherTable), "In Progress") {
		t.Fatal("issue table did not render optimistic status")
	}

	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackStatus {
		t.Fatalf("status msg = %#v, want successful optimistic update message", msg)
	}
	if got.ID != "issue-1" || got.StateID == nil || *got.StateID != "state-progress" {
		t.Fatalf("UpdateIssueInput = %+v, want state-progress", got)
	}
}

func TestCharmAppOptimisticStatusSuccessDoesNotReloadStaleIssues(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerStatus, charmPickerItem{ID: "state-progress", Label: "In Progress"})
	app = model.(CharmApp)
	model, next := app.Update(cmd())
	app = model.(CharmApp)

	if next != nil {
		t.Fatalf("successful optimistic status update returned reload cmd %v", next)
	}
	if issue := app.currentIssue(); issue == nil || issue.State != "In Progress" || issue.StateID != "state-progress" {
		t.Fatalf("current issue = %+v, want optimistic status kept after success", issue)
	}
}

func TestCharmAppOptimisticDoneStatusLeavesActiveView(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerStatus, charmPickerItem{ID: "state-done", Label: "Done"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("status picker returned nil update cmd")
	}
	if len(app.issues) != 0 || len(app.otherRows) != 0 {
		t.Fatalf("done issue still visible: issues=%+v rows=%+v", app.issues, app.otherRows)
	}

	model, next := app.Update(cmd())
	app = model.(CharmApp)
	if next != nil {
		t.Fatalf("successful done update returned reload cmd %v", next)
	}
	if len(app.issues) != 0 || len(app.otherRows) != 0 {
		t.Fatalf("done issue came back after success: issues=%+v rows=%+v", app.issues, app.otherRows)
	}
}

func TestCharmAppStatusPickerRollsBackOnFailure(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("linear refused")
	}

	model, cmd := app.applyPickerSelection(charmPickerStatus, charmPickerItem{ID: "state-progress", Label: "In Progress"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("status picker returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.State != "In Progress" {
		t.Fatalf("current issue = %+v, want optimistic In Progress before failure", issue)
	}

	model, next := app.Update(cmd())
	app = model.(CharmApp)
	if next != nil {
		t.Fatalf("failed optimistic status update returned next cmd %v", next)
	}
	if issue := app.currentIssue(); issue == nil || issue.State != "Todo" {
		t.Fatalf("current issue = %+v, want rollback to Todo", issue)
	}
	if app.selectedIssue == nil || app.selectedIssue.State != "Todo" {
		t.Fatalf("selected issue = %+v, want rollback to Todo", app.selectedIssue)
	}
	if app.status != "linear refused" {
		t.Fatalf("status = %q, want error", app.status)
	}
}

func TestCharmAppUndoRestoresLastStatusChange(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got []linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = append(got, input)
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerStatus, charmPickerItem{ID: "state-done", Label: "Done"})
	app = model.(CharmApp)
	_ = cmd()

	model, cmd = app.Update(tea.KeyPressMsg(tea.Key{Text: "z", Code: 'z'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("undo returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.State != "Todo" || issue.StateID != "state-todo" {
		t.Fatalf("current issue = %+v, want restored Todo", issue)
	}

	model, next := app.Update(cmd())
	app = model.(CharmApp)
	if next != nil {
		t.Fatalf("undo returned follow-up cmd %v", next)
	}
	if app.undo != nil {
		t.Fatalf("undo action = %+v, want cleared after success", app.undo)
	}
	if len(got) != 2 || got[1].StateID == nil || *got[1].StateID != "state-todo" {
		t.Fatalf("undo UpdateIssueInput calls = %+v, want second call restoring state-todo", got)
	}
}

func TestCharmAppCtrlZUsesUndoShortcut(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerPriority, charmPickerItem{Label: "High", Priority: 2})
	app = model.(CharmApp)
	_ = cmd()

	model, cmd = app.Update(tea.KeyPressMsg(tea.Key{Code: 'z', Mod: tea.ModCtrl}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("ctrl+z returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Priority != 0 {
		t.Fatalf("current issue = %+v, want restored priority 0", issue)
	}
}

func TestCharmAppPriorityPickerOptimisticallyUpdatesAndRollsBack(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.issues[0].Priority = 3
	app.selectedIssue.Priority = 3
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("linear refused")
	}

	model, cmd := app.applyPickerSelection(charmPickerPriority, charmPickerItem{Label: "High", Priority: 2})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("priority picker returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Priority != 2 {
		t.Fatalf("current issue = %+v, want optimistic priority 2", issue)
	}

	model, next := app.Update(cmd())
	app = model.(CharmApp)
	if next != nil {
		t.Fatalf("failed optimistic priority update returned next cmd %v", next)
	}
	if issue := app.currentIssue(); issue == nil || issue.Priority != 3 {
		t.Fatalf("current issue = %+v, want rollback priority 3", issue)
	}
	if app.status != "linear refused" {
		t.Fatalf("status = %q, want error", app.status)
	}
}

func TestCharmAppOptimisticPrioritySuccessDoesNotReloadStaleIssues(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, nil
	}

	model, cmd := app.applyPickerSelection(charmPickerPriority, charmPickerItem{Label: "High", Priority: 2})
	app = model.(CharmApp)
	model, next := app.Update(cmd())
	app = model.(CharmApp)

	if next != nil {
		t.Fatalf("successful optimistic priority update returned reload cmd %v", next)
	}
	if issue := app.currentIssue(); issue == nil || issue.Priority != 2 {
		t.Fatalf("current issue = %+v, want optimistic priority kept after success", issue)
	}
}

func TestCharmAppStatusShortcutStartsStatusPickerLoad(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	model, cmd := app.Update(tea.KeyPressMsg(tea.Key{Text: "s", Code: 's'}))
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("s shortcut returned nil cmd, want status picker load")
	}
	if !app.loading || app.status != "loading" {
		t.Fatalf("loading=%v status=%q, want status picker loading", app.loading, app.status)
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerStatus || app.pickerTitle != "Change Status" || !app.pickerLoading {
		t.Fatalf("status shortcut overlay=%v action=%v title=%q pickerLoading=%v", app.overlay, app.pickerAction, app.pickerTitle, app.pickerLoading)
	}
	if rendered := app.View().Content; !strings.Contains(rendered, "Change Status") || !strings.Contains(rendered, "loading") || strings.Contains(rendered, "Loading statuses") {
		t.Fatalf("status shortcut did not render compact loading modal:\n%s", rendered)
	}
}

func TestCharmAppStatusShortcutUsesCachedStatusesImmediately(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.cache.PrimeWorkflowStates("team-1", []linearapi.WorkflowState{
		{ID: "progress", Name: "In Progress", Position: 2},
		{ID: "todo", Name: "Todo", Position: 1},
	})
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)

	model, cmd := app.Update(tea.KeyPressMsg(tea.Key{Text: "s", Code: 's'}))
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("s shortcut with cached statuses should still refresh in the background")
	}
	if app.loading || app.pickerLoading {
		t.Fatalf("loading=%v pickerLoading=%v, want cached picker to open instantly", app.loading, app.pickerLoading)
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerStatus || len(app.pickerItems) != 2 {
		t.Fatalf("cached status picker overlay=%v action=%v items=%+v", app.overlay, app.pickerAction, app.pickerItems)
	}
	if app.pickerItems[0].Label != "Todo" || app.pickerItems[1].Label != "In Progress" {
		t.Fatalf("picker items = %+v, want Linear position order", app.pickerItems)
	}
}

func TestCharmAppStatusShortcutUsesSelectedIssueOutsideIssuesPane(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	app = model.(CharmApp)
	app.focusedPane = charmPaneDetails
	app.applyComponentFocus()

	model, cmd := app.Update(tea.KeyPressMsg(tea.Key{Text: "s", Code: 's'}))
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("s shortcut from details returned nil cmd, want status picker load")
	}
	if !app.loading || app.status != "loading" {
		t.Fatalf("loading=%v status=%q, want status picker loading", app.loading, app.status)
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerStatus || !app.pickerLoading {
		t.Fatalf("status shortcut from details overlay=%v action=%v pickerLoading=%v", app.overlay, app.pickerAction, app.pickerLoading)
	}
}

func TestCharmAppStatusShortcutExplainsMissingTeamContext(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.selectedIssue.TeamID = ""
	app.otherIssueMap[app.selectedIssue.ID].TeamID = ""

	model, cmd := app.Update(tea.KeyPressMsg(tea.Key{Text: "s", Code: 's'}))
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("missing team context returned cmd=%v, want no API load", cmd)
	}
	if app.status != "No issue or team selected" {
		t.Fatalf("status = %q, want missing team explanation", app.status)
	}
}

func TestCharmAppAsyncIssuePickersOpenLoadingOverlay(t *testing.T) {
	tests := []struct {
		command string
		title   string
		action  charmPickerAction
		status  string
	}{
		{command: "assign_user", title: "Assign User", action: charmPickerAssignee, status: "Loading users..."},
		{command: "set_cycle", title: "Set Cycle", action: charmPickerCycle, status: "Loading cycles..."},
		{command: "set_milestone", title: "Set Milestone", action: charmPickerMilestone, status: "Loading milestones..."},
		{command: "list_project_milestones", title: "Project Milestones", action: charmPickerListMilestone, status: "Loading milestones..."},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			app := testCharmAppWithSelectedIssue()

			model, cmd := app.runCharmCommand(charmCommandID(tt.command))
			app = model.(CharmApp)

			if cmd == nil {
				t.Fatal("command returned nil cmd, want async picker load")
			}
			if app.overlay != charmOverlayPicker || app.pickerTitle != tt.title || app.pickerAction != tt.action || !app.pickerLoading {
				t.Fatalf("overlay=%v title=%q action=%v pickerLoading=%v", app.overlay, app.pickerTitle, app.pickerAction, app.pickerLoading)
			}
			if app.status != tt.status {
				t.Fatalf("status = %q, want %q", app.status, tt.status)
			}
		})
	}
}

func TestCharmAppClosedLoadingPickerIgnoresLateResults(t *testing.T) {
	app := testCharmAppWithSelectedIssue()

	model, cmd := app.runCharmCommand("change_status")
	app = model.(CharmApp)
	if cmd == nil || app.overlay != charmOverlayPicker || !app.pickerLoading {
		t.Fatalf("status command overlay=%v pickerLoading=%v cmd=%v", app.overlay, app.pickerLoading, cmd)
	}

	app.closeOverlay()
	model, _ = app.Update(charmPickerLoadedMsg{
		title:  "Change Status",
		action: charmPickerStatus,
		items:  []charmPickerItem{{ID: "state-1", Label: "Todo"}},
	})
	app = model.(CharmApp)

	if app.overlay != charmOverlayNone || app.pickerTitle != "" || len(app.pickerItems) != 0 {
		t.Fatalf("late picker result reopened overlay=%v title=%q items=%+v", app.overlay, app.pickerTitle, app.pickerItems)
	}
}

func TestCharmAppArchiveConfirmationRunsArchive(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var archivedID string
	app.archiveIssueFunc = func(_ context.Context, issueID string) error {
		archivedID = issueID
		return nil
	}

	model, cmd := app.runCharmCommand("archive")
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("archive should open confirmation before running")
	}
	if app.overlay != charmOverlayConfirmArchive {
		t.Fatalf("overlay = %v, want archive confirmation", app.overlay)
	}
	model, cmd = app.handleConfirmArchiveKey(tea.KeyPressMsg(tea.Key{Text: "y"}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("archive confirmation returned nil cmd")
	}
	msg := cmd()
	if archived, ok := msg.(charmIssueArchivedMsg); !ok || archived.err != nil {
		t.Fatalf("archive msg = %#v, want successful archive", msg)
	}
	if archivedID != "issue-1" {
		t.Fatalf("archivedID = %q, want issue-1", archivedID)
	}
	if app.overlay != charmOverlayNone || !app.loading {
		t.Fatalf("post-confirm state overlay=%v loading=%v, want closed+loading", app.overlay, app.loading)
	}
}

func TestCharmAppCreateIssueFormBuildsCreateInput(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	app.selectedNavigation = &NavigationNode{ID: "project-1", TeamID: "team-1", IsProject: true}
	var got linearapi.CreateIssueInput
	app.createIssueFunc = func(_ context.Context, input linearapi.CreateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{ID: "issue-new", Identifier: "LT-9", AssigneeID: "me", Assignee: "Robin"}, nil
	}

	model, cmd := app.runCharmCommand("create_issue")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("create issue command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormCreateIssue {
		t.Fatalf("create form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.titleInput.SetValue("New issue")
	app.bodyArea.SetValue("Details")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("create issue submit returned nil cmd")
	}
	msg := cmd()
	if created, ok := msg.(charmIssueCreatedMsg); !ok || created.err != nil {
		t.Fatalf("create issue msg = %#v, want success", msg)
	}
	if got.TeamID != "team-1" || got.ProjectID != "project-1" || got.Title != "New issue" || got.Description != "Details" || got.AssigneeID != "me" {
		t.Fatalf("CreateIssueInput = %+v, want selected project context, text, and current user assignee", got)
	}
	if app.overlay != charmOverlayNone || !app.loading {
		t.Fatalf("post-submit state overlay=%v loading=%v, want closed+loading", app.overlay, app.loading)
	}
}

func TestCharmAppCreateIssueAssigneePickerPreservesDraft(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	app.openCreateIssueForm("team-1", "")
	app.titleInput.SetValue("Draft issue")
	app.bodyArea.SetValue("Draft body")
	app.formFocus = 2

	model, cmd := app.applyPickerSelection(charmPickerCreateAssignee, charmPickerItem{ID: "user-2", Label: "Ada"})
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatal("create assignee picker selection returned unexpected cmd")
	}
	if app.overlay != charmOverlayIssueForm || app.formFocus != 2 {
		t.Fatalf("overlay/focus = %v/%d, want create form assignee focus", app.overlay, app.formFocus)
	}
	if app.formAssigneeID != "user-2" || app.formAssigneeName != "Ada" {
		t.Fatalf("form assignee = %q/%q, want Ada", app.formAssigneeID, app.formAssigneeName)
	}
	if app.titleInput.Value() != "Draft issue" || app.bodyArea.Value() != "Draft body" {
		t.Fatalf("draft was not preserved: title=%q body=%q", app.titleInput.Value(), app.bodyArea.Value())
	}
}

func TestCharmAppCreatedIssueAppearsAndIsSelectedImmediately(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	app.activeSection = IssuesSectionMy
	app.setIssues([]linearapi.Issue{{
		ID:         "issue-1",
		Identifier: "LT-1",
		Title:      "Existing",
		AssigneeID: "me",
		Assignee:   "Robin",
		TeamID:     "team-1",
		UpdatedAt:  time.Now().Add(-time.Hour),
	}}, "")

	created := linearapi.Issue{
		ID:         "issue-new",
		Identifier: "LT-9",
		Title:      "New issue",
		AssigneeID: "me",
		Assignee:   "Robin",
		TeamID:     "team-1",
		UpdatedAt:  time.Now(),
	}
	model, cmd := app.Update(charmIssueCreatedMsg{issue: created, issueID: created.ID, status: "Created issue LT-9"})
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatal("created issue handler should not reload issues")
	}
	if app.selectedIssue == nil || app.selectedIssue.ID != "issue-new" {
		t.Fatalf("selectedIssue = %+v, want created issue selected", app.selectedIssue)
	}
	if issue := app.currentIssue(); issue == nil || issue.ID != "issue-new" {
		t.Fatalf("currentIssue = %+v, want created issue in table", issue)
	}
}

func TestCharmAppCreateIssueShortcutOpensForm(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.selectedNavigation = &NavigationNode{ID: "team-1", TeamID: "team-1", IsTeam: true}

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("create shortcut should focus the title input")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormCreateIssue || app.formTeamID != "team-1" {
		t.Fatalf("create shortcut state overlay=%v mode=%v team=%q", app.overlay, app.formMode, app.formTeamID)
	}
}

func TestCharmAppCreateIssueShortcutUsesSelectedIssueTeamFromIssuesPane(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	app.focusedPane = charmPaneIssues

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("create shortcut should focus the title input")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormCreateIssue || app.formTeamID != "team-1" {
		t.Fatalf("create shortcut state overlay=%v mode=%v team=%q", app.overlay, app.formMode, app.formTeamID)
	}
}

func TestCharmAppCreateIssueShortcutUsesSelectedIssueTeamFromDetailsPane(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	app.focusedPane = charmPaneDetails

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("create shortcut should focus the title input")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormCreateIssue || app.formTeamID != "team-1" {
		t.Fatalf("create shortcut from details overlay=%v mode=%v team=%q", app.overlay, app.formMode, app.formTeamID)
	}
}

func TestCharmAppCreateIssueShortcutDoesNotHijackSearchInput(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.selectedNavigation = &NavigationNode{ID: "team-1", TeamID: "team-1", IsTeam: true}
	app.searchMode = true

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatalf("search input returned cmd %v, want nil", cmd)
	}
	if app.overlay != charmOverlayNone || app.searchQuery != "c" {
		t.Fatalf("search input hijacked by create shortcut: overlay=%v query=%q", app.overlay, app.searchQuery)
	}
}

func TestCharmAppEditTitleFormBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("edit_title")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("edit title command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormEditTitle {
		t.Fatalf("edit title form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.titleInput.SetValue("Renamed")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("edit title submit returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Title != "Renamed" {
		t.Fatalf("current issue = %+v, want optimistic title", issue)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("edit title msg = %#v, want success", msg)
	}
	if got.ID != "issue-1" || got.Title == nil || *got.Title != "Renamed" {
		t.Fatalf("UpdateIssueInput = %+v, want title update", got)
	}
}

func TestCharmAppEditDescriptionFormBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("edit_description")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("edit description command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormEditDescription {
		t.Fatalf("description form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	if app.bodyArea.Value() != "Existing description" {
		t.Fatalf("bodyArea = %q, want existing description", app.bodyArea.Value())
	}
	app.bodyArea.SetValue("Updated description")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("description submit returned nil cmd")
	}
	if app.selectedIssue == nil {
		t.Fatal("selectedIssue = nil, want optimistic issue")
	}
	if app.selectedIssue.Description != "Updated description" {
		t.Fatalf("selectedIssue.Description = %q, want optimistic update", app.selectedIssue.Description)
	}
	if !strings.Contains(app.details.GetContent(), "Updated") || !strings.Contains(app.details.GetContent(), "description") {
		t.Fatalf("details did not update optimistically:\n%s", app.details.GetContent())
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackDescription {
		t.Fatalf("description msg = %#v, want success", msg)
	}
	if got.ID != "issue-1" || got.Description == nil || *got.Description != "Updated description" {
		t.Fatalf("UpdateIssueInput = %+v, want description update", got)
	}
}

func TestCharmAppDescriptionEditRollsBackOnFailure(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("linear refused")
	}

	model, _ := app.runCharmCommand("edit_description")
	app = model.(CharmApp)
	app.bodyArea.SetValue("Updated description")

	model, cmd := app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("description submit returned nil cmd")
	}
	if app.selectedIssue == nil {
		t.Fatal("selectedIssue = nil, want optimistic issue")
	}
	if app.selectedIssue.Description != "Updated description" {
		t.Fatalf("selectedIssue.Description = %q, want optimistic update", app.selectedIssue.Description)
	}

	model, next := app.Update(cmd())
	app = model.(CharmApp)
	if next != nil {
		t.Fatalf("failed optimistic description update returned next cmd %v", next)
	}
	if app.selectedIssue == nil {
		t.Fatal("selectedIssue = nil, want rolled back issue")
	}
	if app.selectedIssue.Description != "Existing description" {
		t.Fatalf("selectedIssue.Description = %q, want rollback", app.selectedIssue.Description)
	}
	if app.status != "linear refused" {
		t.Fatalf("status = %q, want error", app.status)
	}
}

func TestCharmAppAddCommentFormBuildsCommentInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.CreateCommentInput
	app.createCommentFunc = func(_ context.Context, input linearapi.CreateCommentInput) (linearapi.Comment, error) {
		got = input
		return linearapi.Comment{ID: "comment-1"}, nil
	}

	model, cmd := app.runCharmCommand("add_comment")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("add comment command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormAddComment {
		t.Fatalf("comment form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.bodyArea.SetValue("Looks good")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("comment submit returned nil cmd")
	}
	msg := cmd()
	if commented, ok := msg.(charmCommentCreatedMsg); !ok || commented.err != nil {
		t.Fatalf("comment msg = %#v, want success", msg)
	}
	if got.IssueID != "issue-1" || got.Body != "Looks good" {
		t.Fatalf("CreateCommentInput = %+v, want selected issue comment", got)
	}
}

func TestCharmAppAddCommentShortcutOpensForm(t *testing.T) {
	app := testCharmAppWithSelectedIssue()

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	app = model.(CharmApp)

	if cmd == nil {
		t.Fatal("add comment shortcut should focus the body input")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormAddComment || app.formIssueID != "issue-1" {
		t.Fatalf("add comment shortcut state overlay=%v mode=%v issue=%q", app.overlay, app.formMode, app.formIssueID)
	}
}

func TestCharmAppAddCommentShortcutDoesNotHijackSearchInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.searchMode = true

	model, _ := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	app = model.(CharmApp)

	if app.overlay != charmOverlayNone || app.searchQuery != "a" {
		t.Fatalf("search input hijacked by add-comment shortcut: overlay=%v query=%q", app.overlay, app.searchQuery)
	}
}

func TestCharmAppAddCommentEnterSubmitsAndShiftEnterAddsNewline(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.CreateCommentInput
	app.createCommentFunc = func(_ context.Context, input linearapi.CreateCommentInput) (linearapi.Comment, error) {
		got = input
		return linearapi.Comment{ID: "comment-1"}, nil
	}
	app.openCommentForm(*app.currentIssue())
	app.bodyArea.SetValue("First line")

	model, cmd := app.handleIssueFormKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}))
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("shift+enter should insert a newline, not submit")
	}
	if !strings.Contains(app.bodyArea.Value(), "\n") {
		t.Fatalf("bodyArea.Value() = %q, want newline", app.bodyArea.Value())
	}

	app.bodyArea.InsertString("Second line")
	model, cmd = app.handleIssueFormKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("enter should submit add comment form")
	}
	msg := cmd()
	if commented, ok := msg.(charmCommentCreatedMsg); !ok || commented.err != nil {
		t.Fatalf("comment msg = %#v, want success", msg)
	}
	if got.IssueID != "issue-1" || !strings.Contains(got.Body, "Second line") {
		t.Fatalf("CreateCommentInput = %+v, want multiline comment for issue-1", got)
	}
	if app.overlay != charmOverlayNone || !app.loading {
		t.Fatalf("post-enter state overlay=%v loading=%v, want closed+loading", app.overlay, app.loading)
	}
}

func TestCharmAppDueDateFormBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("set_due_date")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("set due date command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormSetDueDate {
		t.Fatalf("due date form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.titleInput.SetValue("2026-06-15")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("due date submit returned nil cmd")
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackDueDate {
		t.Fatalf("due date msg = %#v, want success", msg)
	}
	if got.ID != "issue-1" || got.DueDate == nil || *got.DueDate != "2026-06-15" {
		t.Fatalf("UpdateIssueInput = %+v, want due date update", got)
	}
	if issue := app.currentIssue(); issue == nil || issue.DueDate == nil || *issue.DueDate != "2026-06-15" {
		t.Fatalf("current issue = %+v, want optimistic due date", issue)
	}
}

func TestCharmAppDueDateFormRejectsInvalidDate(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.openDueDateForm(*app.currentIssue())
	app.titleInput.SetValue("tomorrow")

	model, cmd := app.submitIssueForm()
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("invalid due date should not return update cmd")
	}
	if app.overlay != charmOverlayIssueForm {
		t.Fatal("invalid due date should keep form open")
	}
	if !strings.Contains(app.status, "YYYY-MM-DD") {
		t.Fatalf("status = %q, want validation error", app.status)
	}
}

func TestCharmAppEstimateCommandsBuildUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("set_estimate")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("set estimate command should focus the form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormSetEstimate {
		t.Fatalf("estimate form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.titleInput.SetValue("3.5")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("estimate submit returned nil cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Estimate == nil || *issue.Estimate != 3.5 {
		t.Fatalf("current issue = %+v, want optimistic estimate 3.5", issue)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("estimate msg = %#v, want success", msg)
	}
	if got.ID != "issue-1" || got.Estimate == nil || *got.Estimate != 3.5 {
		t.Fatalf("UpdateIssueInput = %+v, want estimate update", got)
	}

	model, cmd = app.runCharmCommand("clear_estimate")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("clear estimate returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.Estimate != nil {
		t.Fatalf("current issue = %+v, want optimistic estimate clear", issue)
	}
	_ = cmd()
	if got.ID != "issue-1" || !got.ClearEstimate {
		t.Fatalf("UpdateIssueInput = %+v, want clear estimate", got)
	}
}

func TestCharmAppEditLabelsCommandBuildsUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.loadLabelsFunc = func(_ context.Context, teamID string) ([]linearapi.IssueLabel, error) {
		if teamID != "team-1" {
			t.Fatalf("teamID = %q, want team-1", teamID)
		}
		return []linearapi.IssueLabel{
			{ID: "label-bug", Name: "Bug"},
			{ID: "label-feature", Name: "Feature"},
		}, nil
	}
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("edit_labels")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("edit labels command returned nil load cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(CharmApp)
	if app.overlay != charmOverlayMultiSelect || !app.multiSelected["label-bug"] {
		t.Fatalf("labels multi-select not opened with current labels: overlay=%v selected=%+v", app.overlay, app.multiSelected)
	}

	model, _ = app.handleMultiSelectKey(tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}))
	app = model.(CharmApp)
	model, _ = app.handleMultiSelectKey(tea.KeyPressMsg(tea.Key{Text: " ", Code: tea.KeySpace}))
	app = model.(CharmApp)
	model, cmd = app.handleMultiSelectKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("saving labels returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || strings.Join(charmIssueLabelIDs(*issue), ",") != "label-bug,label-feature" {
		t.Fatalf("current issue = %+v, want optimistic labels", issue)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("labels msg = %#v, want successful update", msg)
	}
	if got.ID != "issue-1" || got.LabelIDs == nil || strings.Join(*got.LabelIDs, ",") != "label-bug,label-feature" {
		t.Fatalf("UpdateIssueInput = %+v, want both labels", got)
	}
	if app.overlay != charmOverlayNone || !app.loading {
		t.Fatalf("post-label state overlay=%v loading=%v, want closed+loading", app.overlay, app.loading)
	}
}

func TestCharmAppMilestoneCommandsBuildUpdateInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	targetDate := "2026-07-01"
	app.loadMilestonesFunc = func(_ context.Context, projectID string) ([]linearapi.ProjectMilestone, error) {
		if projectID != "project-1" {
			t.Fatalf("projectID = %q, want project-1", projectID)
		}
		return []linearapi.ProjectMilestone{
			{ID: "milestone-1", Name: "Beta", ProjectID: "project-1", TargetDate: &targetDate},
			{ID: "milestone-2", Name: "GA", ProjectID: "project-1"},
		}, nil
	}
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("set_milestone")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("set milestone command returned nil load cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(CharmApp)
	if app.overlay != charmOverlayPicker || len(app.pickerItems) != 2 {
		t.Fatalf("milestone picker not opened: overlay=%v items=%+v", app.overlay, app.pickerItems)
	}
	if !strings.Contains(app.pickerItems[0].Label, "Beta (2026-07-01)") {
		t.Fatalf("first milestone label = %q, want target date", app.pickerItems[0].Label)
	}

	model, cmd = app.applyPickerSelection(charmPickerMilestone, charmPickerItem{ID: "milestone-1", Label: "Beta"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("milestone selection returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.ProjectMilestone == nil || issue.ProjectMilestone.ID != "milestone-1" {
		t.Fatalf("current issue = %+v, want optimistic milestone", issue)
	}
	msg := cmd()
	if updated, ok := msg.(charmIssueUpdatedMsg); !ok || updated.err != nil || !updated.rollbackIssueSnapshot {
		t.Fatalf("milestone msg = %#v, want successful update", msg)
	}
	if got.ID != "issue-1" || got.ProjectMilestoneID == nil || *got.ProjectMilestoneID != "milestone-1" {
		t.Fatalf("UpdateIssueInput = %+v, want milestone set", got)
	}

	model, cmd = app.runCharmCommand("clear_milestone")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("clear milestone returned nil update cmd")
	}
	if issue := app.currentIssue(); issue == nil || issue.ProjectMilestone != nil {
		t.Fatalf("current issue = %+v, want optimistic milestone clear", issue)
	}
	_ = cmd()
	if got.ID != "issue-1" || got.ProjectMilestoneID == nil || *got.ProjectMilestoneID != "" {
		t.Fatalf("UpdateIssueInput = %+v, want milestone cleared", got)
	}
}

func TestCharmAppListMilestoneCommandDoesNotMutateIssue(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	model, cmd := app.applyPickerSelection(charmPickerListMilestone, charmPickerItem{ID: "milestone-1", Label: "Beta"})
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("list milestone selection should not mutate issue")
	}
	if app.status != "Milestone: Beta" {
		t.Fatalf("status = %q, want selected milestone summary", app.status)
	}
}

func TestCharmAppIssueRelationCommandsBuildExpectedInputs(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var created linearapi.CreateIssueRelationInput
	var deletedID string
	app.createRelationFunc = func(_ context.Context, input linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error) {
		created = input
		return linearapi.IssueRelation{ID: "relation-new"}, nil
	}
	app.deleteRelationFunc = func(_ context.Context, relationID string) error {
		deletedID = relationID
		return nil
	}

	model, cmd := app.runCharmCommand("add_issue_relation")
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("add relation should open relation type picker first")
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerRelationType {
		t.Fatalf("relation picker not opened: overlay=%v action=%v", app.overlay, app.pickerAction)
	}

	model, cmd = app.applyPickerSelection(charmPickerRelationType, charmPickerItem{ID: "blocked by", Label: "blocked by"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("relation type selection should focus target issue form")
	}
	if app.overlay != charmOverlayIssueForm || app.formMode != charmFormIssueRelationTarget {
		t.Fatalf("relation target form not opened: overlay=%v mode=%v", app.overlay, app.formMode)
	}
	app.titleInput.SetValue("issue-2")

	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("relation form submit returned nil cmd")
	}
	msg := cmd()
	if action, ok := msg.(charmIssueActionMsg); !ok || action.err != nil || !action.reloadDetails {
		t.Fatalf("relation create msg = %#v, want detail reload", msg)
	}
	if created.IssueID != "issue-2" || created.RelatedIssueID != "issue-1" || created.Type != linearapi.IssueRelationBlocks {
		t.Fatalf("relation input = %+v, want issue-2 blocks issue-1", created)
	}

	model, cmd = app.runCharmCommand("remove_issue_relation")
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatal("remove relation should open picker before deleting")
	}
	if app.overlay != charmOverlayPicker || app.pickerAction != charmPickerRemoveRelation || len(app.pickerItems) != 1 {
		t.Fatalf("remove relation picker state overlay=%v action=%v items=%+v", app.overlay, app.pickerAction, app.pickerItems)
	}
	model, cmd = app.applyPickerSelection(charmPickerRemoveRelation, app.pickerItems[0])
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("remove relation selection returned nil cmd")
	}
	_ = cmd()
	if deletedID != "relation-1" {
		t.Fatalf("deletedID = %q, want relation-1", deletedID)
	}
}

func TestCharmAppSubscriptionCommandsCallAPI(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var subscribedID string
	var unsubscribedID string
	app.subscribeIssueFunc = func(_ context.Context, issueID string) (linearapi.Issue, error) {
		subscribedID = issueID
		return linearapi.Issue{ID: issueID}, nil
	}
	app.unsubscribeIssueFunc = func(_ context.Context, issueID string) (linearapi.Issue, error) {
		unsubscribedID = issueID
		return linearapi.Issue{ID: issueID}, nil
	}

	model, cmd := app.runCharmCommand("subscribe_issue")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("subscribe command returned nil cmd")
	}
	msg := cmd()
	if action, ok := msg.(charmIssueActionMsg); !ok || action.err != nil || action.status != "Subscribed to issue" || !action.reloadDetails {
		t.Fatalf("subscribe msg = %#v, want successful detail reload", msg)
	}
	if subscribedID != "issue-1" {
		t.Fatalf("subscribedID = %q, want issue-1", subscribedID)
	}

	model, cmd = app.runCharmCommand("unsubscribe_issue")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("unsubscribe command returned nil cmd")
	}
	_ = cmd()
	if unsubscribedID != "issue-1" {
		t.Fatalf("unsubscribedID = %q, want issue-1", unsubscribedID)
	}
}

func TestCharmAppAttachmentCommandsUseInjectedOpenAndCopyFunctions(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var openedURL string
	var copiedURL string
	app.openURLFunc = func(url string) error {
		openedURL = url
		return nil
	}
	app.copyToClipboardFunc = func(text string) error {
		copiedURL = text
		return nil
	}

	model, cmd := app.runCharmCommand("open_attachment")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("single attachment open should run immediately")
	}
	msg := cmd()
	if action, ok := msg.(charmIssueActionMsg); !ok || action.err != nil || action.status != "Opened attachment" {
		t.Fatalf("open attachment msg = %#v, want success", msg)
	}
	if openedURL != "https://example.com/spec" {
		t.Fatalf("openedURL = %q, want attachment URL", openedURL)
	}

	model, cmd = app.runCharmCommand("copy_attachment_url")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("single attachment copy should run immediately")
	}
	_ = cmd()
	if copiedURL != "https://example.com/spec" {
		t.Fatalf("copiedURL = %q, want attachment URL", copiedURL)
	}
}

func TestCharmAppAskAgentStreamsOutput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.cfg.AgentProvider = config.DefaultAgentProvider
	app.cfg.AgentSandbox = config.DefaultAgentSandbox
	app.cfg.AgentModel = "gpt-test"
	app.agentPromptTemplates = []config.AgentPromptTemplate{{Name: "Triage", Prompt: "Summarize this issue"}}
	app.agentRunner = &agents.Runner{LookPath: func(string) (string, error) { return "/bin/echo", nil }}
	selectedIssue := *app.currentIssue()
	app.fetchIssueByIDFunc = func(_ context.Context, issueID string) (linearapi.Issue, error) {
		if issueID != "issue-1" {
			t.Fatalf("issueID = %q, want issue-1", issueID)
		}
		issue := selectedIssue
		issue.Description = "Full issue context"
		return issue, nil
	}
	var gotPrompt string
	var gotContext string
	var gotOptions agents.AgentRunOptions
	app.agentRunFunc = func(_ context.Context, _ agents.Provider, prompt string, issueContext string, options agents.AgentRunOptions, onEvent func(agents.AgentEvent), onLine func(string), _ func(error)) error {
		gotPrompt = prompt
		gotContext = issueContext
		gotOptions = options
		onLine("raw line")
		onEvent(agents.AgentEvent{Type: agents.AgentEventAssistant, Text: "final answer"})
		onEvent(agents.AgentEvent{Type: agents.AgentEventResult, SessionID: "session-1", ResumeCommand: "cursor-agent resume session-1"})
		return nil
	}

	model, cmd := app.runCharmCommand("ask_agent")
	app = model.(CharmApp)
	if cmd == nil || app.overlay != charmOverlayAgentPrompt {
		t.Fatalf("ask agent did not open prompt overlay: overlay=%v cmd=%v", app.overlay, cmd)
	}
	if app.agentPromptArea.Value() != "Summarize this issue" {
		t.Fatalf("prompt template = %q, want template body", app.agentPromptArea.Value())
	}
	app.agentWorkspace.SetValue("/tmp/workspace")
	model, cmd = app.submitAgentPrompt()
	app = model.(CharmApp)
	if cmd == nil || app.overlay != charmOverlayAgentOutput || !app.agentRunning {
		t.Fatalf("agent submit state overlay=%v running=%v cmd=%v", app.overlay, app.agentRunning, cmd)
	}

	app, cmd = updateCharmForTest(t, app, cmd)
	for cmd != nil {
		app, cmd = updateCharmForTest(t, app, cmd)
	}

	if gotPrompt != "Summarize this issue" {
		t.Fatalf("gotPrompt = %q, want submitted prompt", gotPrompt)
	}
	if !strings.Contains(gotContext, "Full issue context") || !strings.Contains(gotContext, "Selected issue") {
		t.Fatalf("issue context missing selected issue fields:\n%s", gotContext)
	}
	if gotOptions.Workspace != "/tmp/workspace" || gotOptions.Model != "gpt-test" || gotOptions.Sandbox != config.DefaultAgentSandbox {
		t.Fatalf("options = %+v, want configured agent options", gotOptions)
	}
	if app.agentRunning || app.agentOutputStatus != "Completed" {
		t.Fatalf("agent status running=%v outputStatus=%q, want completed", app.agentRunning, app.agentOutputStatus)
	}
	output := strings.Join(app.agentOutputLines, "\n") + "\n" + app.agentFinalText + "\n" + app.agentSessionID + "\n" + app.agentResumeCmd
	for _, want := range []string{"raw line", "final answer", "session-1", "cursor-agent resume session-1", "Agent run completed."} {
		if !strings.Contains(output, want) {
			t.Fatalf("agent output missing %q:\n%s", want, output)
		}
	}
}

func TestCharmAppWithSettingsPathUsesPromptTemplates(t *testing.T) {
	templates := []config.AgentPromptTemplate{{Name: "Custom", Prompt: "Use custom prompt"}}
	app := NewCharmAppWithSettingsPath(&linearapi.Client{}, testCharmConfig(), nil, t.TempDir()+"/config.json", templates)

	if len(app.agentPromptTemplates) != 1 || app.agentPromptTemplates[0].Name != "Custom" {
		t.Fatalf("agentPromptTemplates = %+v, want constructor templates", app.agentPromptTemplates)
	}
	app.openAgentPrompt()
	if app.agentPromptArea.Value() != "Use custom prompt" {
		t.Fatalf("agentPromptArea = %q, want custom prompt", app.agentPromptArea.Value())
	}
}

func TestCharmAppPromptTemplateEditorPersistsTemplates(t *testing.T) {
	promptsPath := t.TempDir() + "/prompts.json"
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.promptsPath = promptsPath
	app.agentPromptTemplates = []config.AgentPromptTemplate{{Name: "Old", Prompt: "Old prompt"}}

	model, cmd := app.runCharmCommand("edit_agent_prompts")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayPromptTemplates {
		t.Fatalf("prompt template command state overlay=%v cmd=%v", app.overlay, cmd)
	}
	app.promptTplName.SetValue("New")
	app.promptTplBody.SetValue("New prompt")
	model, cmd = app.handlePromptTemplatesKey(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("saving prompt templates returned nil cmd")
	}
	msg := cmd()
	saved, ok := msg.(charmPromptTemplatesSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("save prompt templates msg = %#v, want success", msg)
	}
	model, _ = app.Update(saved)
	app = model.(CharmApp)
	if len(app.agentPromptTemplates) != 1 || app.agentPromptTemplates[0].Name != "New" {
		t.Fatalf("agentPromptTemplates = %+v, want saved template", app.agentPromptTemplates)
	}
	loaded, err := config.LoadPromptTemplates(promptsPath)
	if err != nil {
		t.Fatalf("LoadPromptTemplates() error: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Prompt != "New prompt" {
		t.Fatalf("loaded templates = %+v, want persisted prompt", loaded)
	}
}

func TestCharmAppIssueLinkCommandsUseOpenAndCopy(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.selectedIssue.URL = "https://linear.app/team/issue/LT-1"
	var opened string
	var copied []string
	app.openURLFunc = func(url string) error {
		opened = url
		return nil
	}
	app.copyToClipboardFunc = func(text string) error {
		copied = append(copied, text)
		return nil
	}

	model, cmd := app.runCharmCommand("open_browser")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("open_browser returned nil cmd")
	}
	_ = cmd()
	if opened != "https://linear.app/team/issue/LT-1" {
		t.Fatalf("opened = %q, want issue URL", opened)
	}
	model, cmd = app.runCharmCommand("copy_id")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("copy_id returned nil cmd")
	}
	_ = cmd()
	model, cmd = app.runCharmCommand("copy_url")
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("copy_url returned nil cmd")
	}
	_ = cmd()
	if strings.Join(copied, ",") != "LT-1,https://linear.app/team/issue/LT-1" {
		t.Fatalf("copied = %+v, want issue ID and URL", copied)
	}
}

func TestCharmAppCopyURLShortcutCopiesSelectedIssueLink(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.selectedIssue.URL = "https://linear.app/team/issue/LT-1"
	var copied string
	app.copyToClipboardFunc = func(text string) error {
		copied = text
		return nil
	}

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("copy URL shortcut returned nil cmd")
	}
	msg := cmd()
	if action, ok := msg.(charmIssueActionMsg); !ok || action.err != nil || action.status != "Copied URL for LT-1" {
		t.Fatalf("copy URL shortcut msg = %#v, want success", msg)
	}
	if copied != "https://linear.app/team/issue/LT-1" {
		t.Fatalf("copied = %q, want issue URL", copied)
	}
}

func TestCharmAppCopyURLShortcutDoesNotHijackSearchInput(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.searchMode = true

	model, cmd := app.handleKey(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	app = model.(CharmApp)
	if cmd != nil {
		t.Fatalf("search input returned cmd %v, want nil", cmd)
	}
	if app.searchQuery != "y" {
		t.Fatalf("searchQuery = %q, want y", app.searchQuery)
	}
}

func TestCharmAppParentCommandsBuildExpectedInputs(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	parent := linearapi.Issue{
		ID:         "parent-1",
		Identifier: "LT-1",
		Title:      "Parent",
		TeamID:     "team-1",
		UpdatedAt:  time.Now(),
	}
	child := linearapi.Issue{
		ID:         "child-1",
		Identifier: "LT-2",
		Title:      "Child",
		TeamID:     "team-1",
		Parent:     &linearapi.IssueRef{ID: "parent-1", Identifier: "LT-1", Title: "Parent"},
		UpdatedAt:  time.Now().Add(time.Minute),
	}
	candidate := linearapi.Issue{
		ID:         "candidate-1",
		Identifier: "LT-3",
		Title:      "Candidate",
		TeamID:     "team-1",
		UpdatedAt:  time.Now().Add(-time.Minute),
	}
	app.expanded["parent-1"] = true
	app.setIssues([]linearapi.Issue{parent, child, candidate}, "")
	app.selectIssueByID("child-1")
	var got linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{}, nil
	}

	model, cmd := app.runCharmCommand("view_parent")
	app = model.(CharmApp)
	if cmd != nil || app.selectedIssue == nil || app.selectedIssue.ID != "parent-1" {
		t.Fatalf("view parent selected=%+v cmd=%v, want parent", app.selectedIssue, cmd)
	}
	app.selectIssueByID("child-1")
	model, cmd = app.runCharmCommand("set_parent")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayPicker || app.pickerAction != charmPickerParent {
		t.Fatalf("set parent overlay=%v action=%v cmd=%v", app.overlay, app.pickerAction, cmd)
	}
	model, cmd = app.applyPickerSelection(charmPickerParent, charmPickerItem{ID: "candidate-1", Label: "LT-3 - Candidate"})
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("parent selection returned nil update cmd")
	}
	_ = cmd()
	if got.ID != "child-1" || got.ParentID == nil || *got.ParentID != "candidate-1" {
		t.Fatalf("set parent input = %+v, want candidate parent", got)
	}

	app.selectIssueByID("child-1")
	model, cmd = app.runCharmCommand("remove_parent")
	app = model.(CharmApp)
	if cmd != nil || app.overlay != charmOverlayConfirmRemoveParent {
		t.Fatalf("remove parent overlay=%v cmd=%v, want confirmation", app.overlay, cmd)
	}
	model, cmd = app.handleConfirmRemoveParentKey(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("remove parent confirmation returned nil cmd")
	}
	_ = cmd()
	if got.ID != "child-1" || got.ParentID == nil || *got.ParentID != "" {
		t.Fatalf("remove parent input = %+v, want cleared parent", got)
	}
}

func TestCharmAppCreateSubIssueIncludesParentID(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	var got linearapi.CreateIssueInput
	app.createIssueFunc = func(_ context.Context, input linearapi.CreateIssueInput) (linearapi.Issue, error) {
		got = input
		return linearapi.Issue{ID: "child-new", Identifier: "LT-9"}, nil
	}

	model, cmd := app.runCharmCommand("create_sub_issue")
	app = model.(CharmApp)
	if cmd == nil || app.overlay != charmOverlayIssueForm || app.formParentID != "issue-1" {
		t.Fatalf("create sub issue state overlay=%v parent=%q cmd=%v", app.overlay, app.formParentID, cmd)
	}
	app.titleInput.SetValue("Child issue")
	model, cmd = app.submitIssueForm()
	app = model.(CharmApp)
	if cmd == nil {
		t.Fatal("sub issue submit returned nil cmd")
	}
	_ = cmd()
	if got.ParentID != "issue-1" || got.Title != "Child issue" {
		t.Fatalf("CreateIssueInput = %+v, want parent issue-1", got)
	}
}

func TestCharmAppDetailsRenderCollaborationFields(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	app.details.SetWidth(100)
	app.details.SetHeight(30)
	app.updateDetailsContent()
	content := app.details.View()
	for _, want := range []string{"Relations", "blocking LT-2 - Dependency", "Subscribers", "Robin", "Attachments", "Spec (github)", "https://example.com/spec"} {
		if !strings.Contains(content, want) {
			t.Fatalf("details missing %q:\n%s", want, content)
		}
	}
}

func TestCharmAppDetailsRenderMarkdown(t *testing.T) {
	app := testCharmAppWithSelectedIssue()
	issue := app.currentIssue()
	issue.Description = "# Plan\n\nShip **fast** without breaking things."
	issue.Comments = []linearapi.Comment{{
		Body:   "Use `cache` and keep **errors** visible.",
		Author: linearapi.User{DisplayName: "Robin"},
	}}
	app.selectedIssue = issue
	app.details.SetWidth(100)
	app.details.SetHeight(30)

	app.updateDetailsContent()
	content := app.details.GetContent()
	for _, want := range []string{"Description", "Plan", "fast", "Comments", "Robin", "cache", "errors"} {
		if !strings.Contains(content, want) {
			t.Fatalf("markdown details missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "**fast**") || strings.Contains(content, "**errors**") {
		t.Fatalf("markdown details still contain raw emphasis markers:\n%s", content)
	}
}

func TestRenderCharmMarkdownCompactsLongBareURLs(t *testing.T) {
	markdown := "- Transfer of Token Rights to Fungus Inc. https://docs.google.com/document/d/14EL0bEWhyAqnQN-jci5ZmcUwtvXTDz0A/edit?usp=sharing&ouid=109651727255072099593&rtpof=true&sd=true\n" +
		"- Software Development Agreement https://frontend.dev.asigna.io/vault/bc1qfxaucdux7wrf48nh00czwkrzff20l86f4furzsw5h0cmalcrqtq56aweh/payroll"

	rendered := renderCharmMarkdown(markdown, 48)
	if strings.Contains(rendered, "https://docs.google.com") || strings.Contains(rendered, "sharing&ouid") {
		t.Fatalf("markdown renderer leaked long raw URL:\n%s", rendered)
	}
	for _, want := range []string{"Transfer of Token Rights", "docs.google.com/document", "frontend.dev.asigna.io"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("markdown renderer missing compact link label %q:\n%s", want, rendered)
		}
	}
	for _, line := range strings.Split(rendered, "\n") {
		if width := ansi.StringWidth(line); width > 58 {
			t.Fatalf("rendered markdown line width = %d, want <= 58:\n%q\nfull:\n%s", width, line, rendered)
		}
	}
}

func TestIssueDiskCacheRoundTripsIssueQueries(t *testing.T) {
	cache := newIssueDiskCache(t.TempDir() + "/issues-cache.json")
	key := `{"assignee_id":"me"}`
	issues := []linearapi.Issue{{ID: "issue-1", Identifier: "LT-1", Title: "Cached"}}

	if err := cache.Set(key, issues); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if len(got) != 1 || got[0].ID != "issue-1" || got[0].Title != "Cached" {
		t.Fatalf("cached issues = %+v, want stored issue", got)
	}
}

func TestCharmAppDoesNotAutoRefreshDiskCachedIssueViews(t *testing.T) {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	msg := charmIssuesLoadedMsg{
		issues:   []linearapi.Issue{},
		fromDisk: true,
	}

	model, cmd := app.Update(msg)
	app = model.(CharmApp)

	if cmd != nil {
		t.Fatalf("Update(disk cached issues) returned cmd=%v, want no automatic network refresh", cmd)
	}
	if app.loading {
		t.Fatal("loading = true after disk cache hit, want false")
	}
	if app.status != "Loaded 0 cached issues" {
		t.Fatalf("status = %q, want cached load status", app.status)
	}
}

func updateCharmForTest(t *testing.T, app CharmApp, cmd tea.Cmd) (CharmApp, tea.Cmd) {
	t.Helper()
	msgCh := make(chan tea.Msg, 1)
	go func() {
		msgCh <- cmd()
	}()
	select {
	case msg := <-msgCh:
		model, next := app.Update(msg)
		return model.(CharmApp), next
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Charm command")
		return app, nil
	}
}

func charmSettingIndexForTest(t *testing.T, app CharmApp, key string) int {
	t.Helper()
	for i, field := range app.settingsFields {
		if field.Key == key {
			return i
		}
	}
	t.Fatalf("setting %q not found in %+v", key, app.settingsFields)
	return 0
}

func setCharmSettingForTest(t *testing.T, app *CharmApp, key string, value string) {
	t.Helper()
	index := charmSettingIndexForTest(t, *app, key)
	app.settingsFields[index].Value = value
}

func charmCustomViewFieldIndexForTest(t *testing.T, app CharmApp, key string) int {
	t.Helper()
	for i, field := range app.customViewFields {
		if field.Key == key {
			return i
		}
	}
	t.Fatalf("custom view field %q not found in %+v", key, app.customViewFields)
	return 0
}

func setCharmCustomViewFieldForTest(t *testing.T, app *CharmApp, key string, value string) {
	t.Helper()
	index := charmCustomViewFieldIndexForTest(t, *app, key)
	app.customViewFields[index].Value = value
}

func testCharmAppWithSelectedIssue() CharmApp {
	app := NewCharmApp(&linearapi.Client{}, testCharmConfig(), nil)
	app.currentUser = &linearapi.User{ID: "me", DisplayName: "Robin"}
	app.setIssues([]linearapi.Issue{{
		ID:          "issue-1",
		Identifier:  "LT-1",
		Title:       "Selected issue",
		Description: "Existing description",
		State:       "Todo",
		StateID:     "state-todo",
		TeamID:      "team-1",
		ProjectID:   "project-1",
		AssigneeID:  "other",
		Assignee:    "Ada",
		Labels:      []linearapi.IssueLabel{{ID: "label-bug", Name: "Bug"}},
		Relations: []linearapi.IssueRelation{{
			ID:   "relation-1",
			Type: string(linearapi.IssueRelationBlocks),
			RelatedIssue: linearapi.IssueRef{
				ID:         "issue-2",
				Identifier: "LT-2",
				Title:      "Dependency",
			},
		}},
		Subscribers: []linearapi.User{{ID: "me", DisplayName: "Robin", IsMe: true}},
		Attachments: []linearapi.Attachment{{
			ID:         "attachment-1",
			Title:      "Spec",
			SourceType: "github",
			URL:        "https://example.com/spec",
		}},
		UpdatedAt: time.Now(),
	}}, "")
	return app
}
