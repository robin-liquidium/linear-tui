# Repository Guidelines

## Project Structure & Module Organization
- `cmd/linear-tui/`: CLI entrypoint and version metadata.
- `internal/`: Application code (TUI, agents, API client, config, cache, logging).
- `docs/`: Screenshots and demo assets.
- `main/`: Additional entrypoint helpers (if present).
- Tests live alongside code as `*_test.go` (for example, `internal/tui/app_test.go`).

## Build, Test, and Development Commands
- `make build`: Build the `linear-tui` binary from `./cmd/linear-tui`.
- `make test`: Run all tests with race detector and coverage output.
- `make coverage`: Print coverage summary from `coverage.out`.
- `make lint`: Run `gofmt`, `go mod tidy` checks, and `golangci-lint`.
- `make fmt-fix`: Auto-format code with `gofmt -w`.
- `make all`: Run lint, tests, and build in one pass.

## Coding Style & Naming Conventions
- Go formatting is enforced with `gofmt` and `goimports` (see `.golangci.yml`).
- Follow Effective Go conventions and keep functions small and focused.
- Naming: exported identifiers in `PascalCase`, unexported in `camelCase`, packages in lowercase.
- Error handling is explicit; wrap errors with context using `fmt.Errorf("...: %w", err)` when appropriate.
- The TUI uses `tview`/`tcell`, and Charm libraries like `glamour` (plus indirect `lipgloss`/`charmbracelet/x/*`). When touching Charm components, consult the Charm documentation for best practices and recommended usage patterns.

## Testing Guidelines
- Use standard Go testing (`go test`) with table-driven tests when useful.
- Place tests next to implementation files using the `*_test.go` suffix.
- Run `make test` for race + coverage; `go test ./internal/tui/...` for scoped runs.

## Commit & Pull Request Guidelines
- Recent history uses concise, imperative messages; some commits follow `type: subject` (for example, `fix: ...`, `chore: ...`). Either is acceptable as long as it is clear and action-oriented.
- Before opening a PR, run `make all` and update docs when behavior changes.
- PRs should include a clear description, link related issues, and add screenshots for UI changes (see `CONTRIBUTING.md`).

## Configuration & Security Notes
- A Linear API key is required; it can be stored in `~/.linear-tui/config.json` or provided via `LINEAR_API_KEY`.
- Local settings live in `~/.linear-tui/config.json`; avoid committing personal config data.

## Session Learnings
- Status updates must use workflow states from the issue’s own team. Using states from another team triggers Linear’s “discrepancy between issue team and state” error. The status picker should always load states for the issue’s team.
- Command palette input clearing should update both the input field and internal query state; otherwise cleared text can reappear on the next keypress.
- Collapsed/hidden panes should still be represented by focusable placeholders so Tab navigation remains stable, even when panes are empty.
- Use theme background colors for `tview` containers and text views; relying on `tcell.ColorDefault` can introduce unintended black padding in nested layouts.
