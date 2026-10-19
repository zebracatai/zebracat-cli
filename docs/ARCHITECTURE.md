# Architecture — zebracat CLI

Single Go binary (cobra + bubbletea) over the Zebracat public API (`/api/v1/public/*`
on the backend). It does no generation itself: every command is a thin HTTP call.

## Module map

| Path | Owns |
|---|---|
| `main.go` | entrypoint → `cmd.Execute()` |
| `cmd/root.go` | root command, global flags, `newClient()`, `emit()` (JSON vs human), error → exit code mapping |
| `cmd/video.go` | `video list/get/status/cancel/download/translate`, `waitTask`, the human task view |
| `cmd/video_create.go` | `video create` (Director v2 `POST /videos`; legacy flag mapping; `--audio-file` → v1 `/video/audio`) |
| `cmd/plan.go` | `plan show/edit/chat/undo`, `video quote/generate/answer/clone` |
| `cmd/studio.go` | `studio script/hooks/voice/image/clip/job`, `models list`, `presets list` |
| `cmd/swap.go` | `swap`: `POST /character-swap/analyze`, then `POST /character-swap` (consent, quoted credits, confirm) |
| `cmd/common.go` | idempotency keys, `postIdempotent` (429 + Retry-After under `--wait`), file/stdin args, the Generate confirmation, the injectable clock |
| `cmd/assets.go` | voices, avatars, styles, music, templates, characters, brand kits, languages |
| `cmd/account.go`, `cmd/auth.go`, `cmd/webhook.go`, `cmd/generate.go`, `cmd/misc.go` | account/pricing, API-key login/logout, webhooks, `script`/`image` (Studio aliases when the server has the routes), config + version/update |
| `cmd/chat.go` | `zebracat` with no args / `zebracat chat` → launches the TUI |
| `internal/client/` | HTTP client: auth resolution (flag > env > stored key > OAuth token), one 401 refresh retry, typed API errors (`apiError`: type/code/retryable/hint/request_id/Retry-After); `enums_gen.go` + `enums.go` (allowed flag values); `capabilities.go` (`HasPath`: the server's OpenAPI paths, cached a day) |
| `internal/openapi/` | `openapi.json` (checked-in copy of the public API schema), enum extraction and example lookup; `enumgen/` writes `internal/client/enums_gen.go` (`go generate`) |
| `internal/videos/` | the v2 task lifecycle shared by the commands and the TUI: `Fetch` (v2 `GET /videos/{id}`, v1 `/video/status` fallback), `StopsWait`, `Wait` (injectable clock, Retry-After), `QuoteLine`, `SummaryLines`, `Beats`, `EditorURL` |
| `internal/apitest/` | tests only: an httptest server answering with the schema's examples, else `fixtures.json` (routes the schema documents without examples), else a canned typed error; queue answers per route, read back calls |
| `internal/config/` | `~/.zebracat/` settings + credentials, env overrides |
| `internal/clierr/` | error type and exit codes (0 ok, 1 API, 2 usage, 3 auth, 4 timeout) |
| `internal/ui/` | JSON/human output, colours, spinner, tables |
| `internal/dash/` | project-status labels/icons/relative time shared by `video list` and the TUI dashboard |
| `internal/update/` | GitHub release check (cached daily) and self-update |
| `internal/tui/tui.go` | TUI model, `New`, `Init`, `Update` (message loop), message types |
| `internal/tui/commands.go` | Enter handling, API-key capture, slash-command table + dispatcher + signed-out gate, `/help` text |
| `internal/tui/wizard.go` | guided "make a video" wizard: steps, state, advance/submit (a plan-only `POST /videos`), view |
| `internal/tui/generate.go` | the plan card, the quote and the "Generate?" ↑/↓ question; the needs_input card |
| `internal/tui/api.go` | background API calls returning `tea.Cmd` (account, projects, create, poll, quote, generate, update) |
| `internal/tui/view.go` | splash, auth banner, input/footer view, projects dashboard, payload formatters |
| `internal/tui/theme.go` | palette, lipgloss styles, logo |
| `deploy/nginx-oauth-patch.py` | one-off prod nginx patch for the public API / OAuth discovery routes (run by hand) |

## Entry points

- `zebracat <noun> <verb> …` — cobra commands under `cmd/`; each calls `client.Client.Do` and `emit`.
- `zebracat` / `zebracat chat` — interactive shell (`internal/tui`).

## Trace: `zebracat video create "…" --plan-only --wait`

1. `main.go:main` → `cmd/root.go:Execute` → cobra dispatches `videoCreateCmd.RunE` (`cmd/video_create.go`).
2. `buildCreate` turns the flags into the `POST /api/v1/public/videos` body (enums checked
   against `client.Enums`; legacy flags mapped with a note on stderr).
3. `cmd/root.go:newClient` → `client.New`; `cmd/common.go:postIdempotent` sends it with an
   `Idempotency-Key` (retrying a 429 after `Retry-After` under `--wait`).
4. `--wait` → `cmd/video.go:waitTask` → `videos.Wait` polls `GET /videos/{task_id}` until
   `StopsWait` (plan_ready here; completed for a one-call create).
5. `cmd/root.go:emit` prints JSON (or `printStatusHuman`); exit 0.

Then `zebracat video generate <id>`: `cmd/plan.go` reads `GET /videos/{id}/quote`, asks on a
terminal (refuses without `--yes` otherwise), and posts `/videos/{id}/generate`.

TUI equivalent: typed idea → `commands.go:handleEnter` → `wizard.go:wizAdvance` (last step) →
`api.go:createCmd` (plan only) → `apiMsg{kind:"create"}` in `tui.go:Update` → `api.go:pollCmd`
every 5s → `plan_ready` → `quoteCmd` → `planReadyMsg` → `generate.go` shows the plan card and
"Generate?" → yes → `generateCmd` → polls until `completed` → link from `view.go:bestLink`.

## Trace: failure

- Non-2xx → `client.go:apiError` returns a `*clierr.Error` → `cmd/root.go:Execute` prints
  `{"error":{code,message,hint}}` to stderr and exits with its code (401 → 3).
- OAuth 401 → one `client.refresh` and a single retry before failing.
- A typed API error keeps the server's `type`, `code`, `hint`, `retryable` and `request_id`;
  `clierr.ExitForType` picks the exit code (`invalid_request` → 2, `authentication`/`permission` → 3, else 1).
- `videos.Wait` past `--timeout` → `clierr.Timeout` (exit 4), with the last state on stdout.
- In the TUI, errors become a red `✗ …` line; signed-out users hit `lockMsg` before any API call.

## Config / env

`ZEBRACAT_API_KEY`, `ZEBRACAT_BASE_URL`, `ZEBRACAT_OUTPUT`; files in `~/.zebracat/`
(`config.json`, `credentials.json`, `capabilities.json` — the server's path list, a day). No LLM prompts live in this repo.
