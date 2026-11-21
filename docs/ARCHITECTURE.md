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
| `cmd/swap.go` | `swap`: `POST /character-swap/analyze`, then `POST /character-swap` (consent, quoted credits, confirm); `swap status`; both only when the server's schema lists `/character-swap` |
| `cmd/catalog.go` | `models list`, `presets list` |
| `cmd/common.go` | idempotency keys, `postIdempotent` (429 + Retry-After under `--wait`), file/stdin args, the Generate confirmation, the injectable clock |
| `cmd/assets.go` | voices, avatars, styles, music, templates, characters, brand kits, languages |
| `cmd/account.go`, `cmd/auth.go`, `cmd/webhook.go`, `cmd/generate.go`, `cmd/misc.go` | account/pricing, API-key login/logout, webhooks, `script`/`image`, config + version/update |
| `cmd/chat.go` | `zebracat` with no args / `zebracat chat` → launches the TUI (only with a terminal; else exit 2 `needs_terminal`) |
| `cmd/create.go` | `zebracat create`: the TUI on a terminal; otherwise a plan-only create that prints plan + quote (`--yes` also generates); `zebracat login` |
| `internal/attach/` | pictures: `Clipboard()` (macOS osascript, Linux wl-paste/xclip, Windows PowerShell → temp PNG), `Paths()` (a paste/drop that names picture files: quotes, `\ `, `file://`, `~/`), `Open()` (PNG/JPEG/WebP ≤12 MB, size), `Upload()` (`POST /api/v1/public/uploads`, one multipart `file` → `{url, kind_hint}`), `Uploads()` (the plan's `uploads`) |
| `internal/brief/` | a typed message → the `POST /videos` body the studio's /chat sends: video-post links → `source_video` (read from URL parts, no regex), a lone page link → the URL door, always `auto_generate: false` |
| `internal/client/` | HTTP client: auth resolution (flag > env > stored key > OAuth token), one 401 refresh retry, typed API errors (`apiError`: type/code/retryable/hint/request_id/Retry-After); `enums_gen.go` + `enums.go` (allowed flag values); `capabilities.go` (`HasPath`: the server's OpenAPI paths, cached a day) |
| `internal/openapi/` | `openapi.json` (checked-in copy of the public API schema), enum extraction and example lookup; `enumgen/` writes `internal/client/enums_gen.go` (`go generate`) |
| `internal/videos/` | the v2 task lifecycle shared by the commands and the TUI: `Fetch` (v2 `GET /videos/{id}`, v1 `/video/status` fallback), `StopsWait`, `Wait` (injectable clock, Retry-After), `QuoteLine`, `SummaryLines`, `Beats`, `EditorURL` |
| `internal/apitest/` | tests only: an httptest server answering with the schema's examples, else `fixtures.json` (routes the schema documents without examples), else a canned typed error; queue answers per route, read back calls |
| `internal/config/` | `~/.zebracat/` settings + credentials, env overrides |
| `internal/clierr/` | error type and exit codes (0 ok, 1 API, 2 usage, 3 auth, 4 timeout) |
| `internal/ui/` | JSON/human output, colours, spinner, tables |
| `internal/dash/` | project-status labels/icons/relative time shared by `video list` and the TUI dashboard |
| `internal/update/` | launch check via the `/releases/latest` redirect (cached a day in `update.json`, 1.5 s cap, off in CI / `ZEBRACAT_NO_UPDATE_CHECK=1`), install-method detection (npm/brew/go), self-update that refuses a download without a matching line in the release's `checksums.txt` (so does `install.sh`) |
| `internal/tui/tui.go` | TUI model, `Run` (account + update check before the welcome box; returns a relaunch request after an in-app update), keys (history, completion, the `u` offer), message types |
| `internal/tui/commands.go` | Enter handling, API-key capture, slash-command table + dispatcher + signed-out gate, `/help` text |
| `internal/tui/convo.go` | the conversation: stages (idle → planning → asking/ready ⇄ editing → confirm → making → done); a `plan_blocked` / `*_request` end is an assistant turn (the answer is sent with the first message), real failures say what to do; the animated progress line; Esc / Ctrl+C (never cancel) |
| `internal/tui/cards.go` | plan card, question, done card, spend question, `clean()` (strips control characters from server text) |
| `internal/tui/theme.go` | one accent (#6B3BF4, the logo's), dim secondary text, the marks, the animated frames, NO_COLOR |
| `internal/logo/` | the real logo (squanchy `public/logo.svg`: striped cat-head shield) as geometry, rasterised into quadrant blocks: `Sized(1/2/4)` = 9×5, 17×10, 34×20 (stripes whole columns, mirrored), `Small` 1 row |
| `tools/logopreview/` | `go run ./tools/logopreview -o x.png` (SVG vs terminal render) or `-sizes` (every terminal size), on dark and light |
| `cmd/doctor.go` | `zebracat doctor`: every zebracat on PATH, which one runs, how to remove the others; sign-in and update status |
| `internal/tui/api.go` | background API calls returning `tea.Cmd` (account, projects, create, poll, quote, generate, update) |
| `internal/tui/view.go` | `say`/`sayR` (every printed block keeps a render func; a resize clears and re-prints the visible tail at the new size), the welcome (logo size from columns × rows), `View` = the live area padded so the input sits on the last rows, the `/` menu above the input, the two-line status footer, projects dashboard |
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

TUI equivalent: typed text → `commands.go:handleEnter` → `convo.go:converse` → `startPlan`
(`brief.Body`, plan only) → `api.go:createCmd` → `createdMsg` → `pollCmd` every 5 s →
`plan_ready` → `planCmd` (GET /plan + /quote) → `planMsg` → plan card. More text →
`chatCmd` (POST /plan/chat) → `planMsg` with the reply. Ctrl+G → `stageConfirm` → `y` →
`generateCmd` → `generatedMsg` → polls until `completed` → done card, Ctrl+O opens it.

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
