# Repository Guidelines

## Parent Rules
- Before changing this repo, read and follow `../AGENTS.md` when it exists. These repo-local notes extend those shared TUI rules.

## Stack
- This TUI is the Charm rewrite. Use Bubble Tea, Bubbles, Lip Gloss, and Glamour; do not reintroduce tview/tcell patterns.
- Docs: [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubble Tea commands](https://charm.land/blog/commands-in-bubbletea/), [Bubbles](https://github.com/charmbracelet/bubbles), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Glamour](https://github.com/charmbracelet/glamour).
- Prefer Charm-native models, commands, key bindings, help text, viewport/table/textarea/list components, and Lip Gloss styles over custom terminal drawing.

## UX Rules
- Keep the main app flat and clean: header, visible columns, footer. Use modal borders only for dialogs/overlays.
- Hidden panes must be removed from layout and focus order, not collapsed into placeholder strips.
- Loading state belongs in the footer with the spinner; avoid top-of-screen loading text that shifts layout.
- Preserve mouse support for focus, table/detail scrolling, and pane resizing.
- Details should render Markdown with Glamour and keep selected issue metadata compact.

## Data And Mutations
- Prefer optimistic local updates for issue field mutations: status, priority, title, description, assignee, cycle, milestone, labels, due date, and estimate.
- On optimistic failure, roll back the local snapshot and show the Linear error.
- Keep structural/network-heavy actions confirmation-based or reload-based unless rollback is carefully modeled: archive, comments, relations, subscribe/unsubscribe, parent tree changes.
- Issue lists should use cache for view switches, manual `r` for explicit refresh, and the hourly auto-refresh for long-running sessions.

## Verification
- For TUI behavior changes, add focused tests in `internal/tui/charm_app_test.go`.
- Before handoff, run the relevant focused test, `go test ./...`, `git diff --check`, and `go build -o linear-tui ./cmd/linear-tui`.
- After rebuilding, check for a stale running `linear-tui` process and report if restart is needed.
