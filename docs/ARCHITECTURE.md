# Architecture — zebracat CLI

Single Go binary (cobra + bubbletea) over the Zebracat public API (`/api/v1/public/*`
on the backend). It does no generation itself: every command is a thin HTTP call.

## Module map

| Path | Owns |
|---|---|
| `main.go` | entrypoint → `cmd.Execute()` |
| `cmd/root.go` | root command, global flags, `newClient()`, `emit()` (JSON vs human), error → exit code mapping |
| `cmd/video.go` | `video create/list/get/status/cancel/download/translate`, polling (`pollStatus`) |
| `cmd/assets.go` | voices, avatars, styles, music, templates, characters, brand kits, languages |
| `cmd/account.go`, `cmd/auth.go`, `cmd/webhook.go`, `cmd/generate.go`, `cmd/misc.go` | account/pricing, API-key login/logout, webhooks, script/image generation, config + version/update |
| `cmd/chat.go` | `zebracat` with no args / `zebracat chat` → launches the TUI |
| `internal/client/` | HTTP client: auth resolution (flag > env > stored key > OAuth token), one 401 refresh retry, API error envelope |
| `internal/config/` | `~/.zebracat/` settings + credentials, env overrides |
| `internal/clierr/` | error type and exit codes (0 ok, 1 API, 2 usage, 3 auth, 4 timeout) |
| `internal/ui/` | JSON/human output, colours, spinner, tables |
| `internal/dash/` | project-status labels/icons/relative time shared by `video list` and the TUI dashboard |
| `internal/update/` | GitHub release check (cached daily) and self-update |
| `internal/tui/tui.go` | TUI model, `New`, `Init`, `Update` (message loop), message types |
| `internal/tui/commands.go` | Enter handling, API-key capture, slash-command table + dispatcher + signed-out gate, `/help` text |
| `internal/tui/wizard.go` | guided "make a video" wizard: steps, state, advance/submit, view |
| `internal/tui/api.go` | background API calls returning `tea.Cmd` (account, projects, create, poll, update) |
| `internal/tui/view.go` | splash, auth banner, input/footer view, projects dashboard, payload formatters |
| `internal/tui/theme.go` | palette, lipgloss styles, logo |
| `deploy/nginx-oauth-patch.py` | one-off prod nginx patch for the public API / OAuth discovery routes (run by hand) |

## Entry points

- `zebracat <noun> <verb> …` — cobra commands under `cmd/`; each calls `client.Client.Do` and `emit`.
- `zebracat` / `zebracat chat` — interactive shell (`internal/tui`).

## Trace: `zebracat video create --prompt … --wait`

1. `main.go:main` → `cmd/root.go:Execute` → cobra dispatches `videoCreateCmd.RunE` (`cmd/video.go`).
2. `cmd/root.go:newClient` → `config.LoadSettings/LoadCredentials` → `client.New`.
3. `cmd/video.go:buildCreate` builds the path + JSON body → `client.Client.Do(POST …)`.
4. `--wait` → `cmd/video.go:pollStatus` polls `GET /api/v1/public/video/status?task_id=…` until terminal.
5. `cmd/root.go:emit` prints JSON (or the human view); exit 0.

TUI equivalent: typed idea → `commands.go:handleEnter` → `wizard.go:wizAdvance` (last step) →
`api.go:createCmd` → `apiMsg{kind:"create"}` in `tui.go:Update` → `api.go:pollCmd` every 5s →
`pollMsg` terminal → link printed by `view.go:bestLink`.

## Trace: failure

- Non-2xx → `client.go:apiError` returns a `*clierr.Error` → `cmd/root.go:Execute` prints
  `{"error":{code,message,hint}}` to stderr and exits with its code (401 → 3).
- OAuth 401 → one `client.refresh` and a single retry before failing.
- `pollStatus` past `--timeout` → `clierr.Timeout` (exit 4).
- In the TUI, errors become a red `✗ …` line; signed-out users hit `lockMsg` before any API call.

## Config / env

`ZEBRACAT_API_KEY`, `ZEBRACAT_BASE_URL`, `ZEBRACAT_OUTPUT`; files in `~/.zebracat/`
(`config.json`, `credentials.json`). No LLM prompts live in this repo.
