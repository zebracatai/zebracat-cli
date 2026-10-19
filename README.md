<div align="center">

# 🦓 Zebracat CLI

**Make AI videos from the command line. Drive Zebracat with code, not clicks.**

[![Release](https://img.shields.io/github/v/release/zebracatai/zebracat-cli?color=7c3aed)](https://github.com/zebracatai/zebracat-cli/releases)
[![Go](https://img.shields.io/badge/go-1.22+-7c3aed)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-7c3aed)](LICENSE)

</div>

---

`zebracat` turns ideas, scripts, blog posts, and audio into finished videos — from your
terminal, your CI pipeline, or an AI coding agent. JSON in, JSON out.

## Built for

- **Coding agents** — Claude Code, Cursor, Codex. Stable JSON + exit codes.
- **CI/CD** — generate release-note vlogs, weekly recaps, localized variants.
- **Bulk jobs** — translate 100 videos in a shell loop.
- **Humans** — `--human` gives you clean, colorful tables.

## Install

```bash
# macOS / Linux / WSL — one line, no dependencies
curl -fsSL https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh | bash

# pin a specific version
curl -fsSL https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh | ZEBRACAT_INSTALL_VERSION=v0.1.0 bash

# or with Go
go install github.com/zebracatai/zebracat-cli@latest   # installs as `zebracat-cli`

# or grab a binary from the Releases page
```

> The installer downloads binaries from GitHub Releases. To serve it from a branded
> URL like `https://get.zebracat.ai/install.sh`, just host this same `install.sh` on
> your own CDN — it works unchanged.

## Interactive shell

Just run `zebracat` (no arguments) to open the interactive shell — a branded,
purple-themed REPL:

```
╭──────────────────────────────────────────────────╮
│  ╺━┓ ┏━╸ ┣━┓ ┏━┓ ┏━┓ ┏━╸ ┏━┓ ╺┳╸                 │
│   ┏┛ ┣━╸ ┣━┫ ┣┳┛ ┣━┫ ┃   ┣━┫  ┃                  │
│  ┗━╸ ┗━╸ ┗━┛ ┻┗╸ ┻ ┻ ┗━╸ ┻ ┻  ╹                  │
│                                                  │
│  🦓  AI video generation, right in your terminal.│
│                                                  │
│  Type /help for commands  ·  /login to start     │
╰──────────────────────────────────────────────────╯

╭────────────────────────────────────────────────────╮
│ 🦓 › a 30-second cinematic ad for my coffee brand    │
╰────────────────────────────────────────────────────╯
  ● you@example.com · Pro · 1,240 credits
```

- **Slash commands** with autocomplete: `/video`, `/status`, `/projects`, `/voices`,
  `/styles`, `/account`, `/login`, `/update`, `/help`, `/quit` (type `/` and press Tab).
- **Auth-aware**: signed out, it shows a `/login` nudge and gates anything that spends
  credits; `/login` lets you paste your API key (stored locally, never shown).
- **Just describe a video** in plain language, then pick type / length / format with
  **↑/↓ + Enter** — a guided, keyboard-driven wizard. Zebracat writes the **plan**
  first; you see its summary and the **quote**, then choose **Generate?** with ↑/↓.
  Nothing is spent until you say yes.
- Output stays in your scrollback after you exit (no alt-screen).

Prefer one-shot commands? Everything below also works non-interactively
(`zebracat video create …`), which is what scripts and agents should use.

## Authentication

The CLI uses the **public API**, which authenticates with an **API key** (billed
pay-as-you-go from your API dollar balance). Create one at
[studio.zebracat.ai → API Keys](https://studio.zebracat.ai).

```bash
zebracat auth login            # paste your API key (stored at ~/.zebracat, 0600)
export ZEBRACAT_API_KEY=sk-…   # …or just set the env var (CI/agents)
zebracat auth status           # am I logged in?
zebracat auth whoami           # account + balances
```

With `ZEBRACAT_API_KEY` set, nothing reads the terminal — the CLI is non-interactive
by default, which is what scripts and agents should use.

> The **CLI is API-key only**. The **MCP server** (for editors/agents) accepts
> *either* an API key *or* OAuth sign-in (which bills your plan credits) — see the
> MCP docs.

## Quick start

Videos are made by **Director v2** — the same engine as the dashboard's `/chat`. It
writes a **plan** (the screenplay: scenes, words, voices, look), then generates it.

**One call** — plan, generate and render, then print the finished video:

```bash
zebracat video create "A 30-second explainer on compound interest" --wait
```

**Plan first** — read the plan, change it, see the price, then generate:

```bash
id=$(zebracat video create --brief "Why cats knock things off tables" --seconds 20 --plan-only --wait | jq -r .task_id)
zebracat plan show "$id"                                   # the screenplay, scene by scene
zebracat plan chat "$id" "make it funnier"                 # change it in plain words…
zebracat plan edit "$id" --tool set_duration --args '{"seconds":15}'   # …or with a structured edit
zebracat video quote "$id"                                 # exactly what Generate charges
zebracat video generate "$id" --yes --wait                 # spend, make, render
zebracat video download "$id" -o out.mp4
```

**What would it cost?** `--dry-run` validates and quotes; it creates and charges nothing:

```bash
zebracat video create --brief "…" --seconds 30 --quality cinematic --dry-run
zebracat video generate "$id" --dry-run
```

More ways in:

```bash
zebracat video create --script script.txt --audio dialogue --aspect horizontal
zebracat video create --url https://example.com/post --wait          # from a web page
zebracat video create --brief "coffee on the street" --preset street_interview \
  --upload https://…/me.png:person --upload https://…/cafe.jpg:place
zebracat video clone --url https://www.tiktok.com/@x/video/1 --mode remake_niche --topic "home coffee" --wait
zebracat video translate --url https://…/in.mp4 --to spanish --render --wait
```

Studio tools, one call each:

```bash
zebracat studio script "3 tips for better sleep" --seconds 30 --wait   # free, daily allowance
zebracat studio hooks --script script.txt                             # free
zebracat studio voice --text script.txt --voice <id> -o vo.mp3        # paid (voice credits)
zebracat studio image "a neon city at dusk" --aspect vertical --count 2
zebracat studio clip "a cat surfing a wave" --seconds 5 --dry-run     # paid per started 5 s
```

### Phases

`video status` (and every `--wait`) reads the task's **phase**:

| phase | meaning | `--wait` |
|---|---|---|
| `planning` | the plan is being written | keeps waiting |
| `needs_input` | the plan has a question — answer with `video answer` | **stops** (exit 0) |
| `plan_ready` | review, then `video generate` | **stops** with `--plan-only` / `video status` |
| `generating`, `rendering` | making the clips, exporting the MP4 | keeps waiting |
| `completed`, `failed`, `cancelled` | done | **stops** |

v1 task ids (older videos) keep their v1 `status`.

### Money

- `--dry-run` (on `video create`, `video generate`, `video clone`, `studio voice`,
  `studio clip`) answers the quote and spends nothing.
- `video generate` on a terminal shows the quote and asks; scripts and agents are
  never asked and must pass `--yes`.
- Every paid POST sends an `Idempotency-Key` (a fresh UUID per run, printed as
  `idempotency_key`); re-run with `--idempotency-key <that key>` and the server
  answers the first result instead of spending twice.

## Commands

```
zebracat auth        login | logout | status | whoami
zebracat video       create | list | get | status | quote | generate | answer | cancel
                     download | clone | translate | prompt-styles | languages
zebracat plan        show | edit | chat | undo
zebracat studio      script | hooks | voice | image | clip | job
zebracat models      list                       # qualities, models, credits per 5 s
zebracat presets     list                       # street interview, podcast, UGC ad, …
zebracat script      <idea>                     # alias of `studio script` when the server has Studio
zebracat image       <prompt>                   # alias of `studio image` when the server has Studio
zebracat voice       list | clone
zebracat avatar      list
zebracat style       list
zebracat music       list --mood <mood>
zebracat template    list
zebracat character   list | create
zebracat brand       list
zebracat account     show | pricing | usage
zebracat webhook     list | create | delete
zebracat config      list | set
zebracat completion  bash | zsh | fish | powershell
zebracat update | version
```

Run `zebracat <command> --help` for full flags. The allowed values in the help text
(media types, audio modes, qualities, presets, clone modes, edit tools) are generated
from the API's OpenAPI schema.

Old flags still work and print a note on stderr: `--prompt` (= `--brief`),
`--type`/`--footage` (→ `--media-type`; `ai_avatar` → an AI video with the presenter
speaking, add them with `--upload <portrait>:person`), `--duration` (→ `--seconds`),
`--from` (→ `--input`). `--mood`/`--prompt-style` have no v2 meaning and are ignored.
Audio-to-video stays on v1: `--audio-file <url>`.

## Output: themed for you, JSON for scripts

In a terminal you get **themed, branded output** (purple tables, key/value blocks);
when piped or scripted it emits **JSON** automatically. Force either with `--human`
or `--json` (or set `ZEBRACAT_OUTPUT`). Finished videos print a **studio link**
(`studio.zebracat.ai/storyboard/<id>`) and the MP4 when rendered — never raw JSON.

## Output & exit codes (agent-friendly)

- **stdout** is JSON by default — pipe it into `jq`, scripts, or an agent.
- **stderr** carries the structured error envelope. API errors keep the server's
  typed fields:
  ```json
  { "error": { "code": "insufficient_funds", "message": "...", "hint": "...",
               "type": "insufficient_funds", "retryable": false, "request_id": "req_…" } }
  ```
- **exit codes**: `0` ok · `1` API/network · `2` usage · `3` auth · `4` timeout.

| error `type` | exit | what to do |
|---|---|---|
| `invalid_request` | 2 | fix the arguments (`field`, `allowed_values` in the message) |
| `authentication`, `permission` | 3 | set a valid `ZEBRACAT_API_KEY` |
| `insufficient_funds` | 1 | top up; the `hint` says where |
| `rate_limited` | 1 | `retryable: true` — wait and retry (`--wait` honours `Retry-After`) |
| `conflict` | 1 | the plan changed: re-read it and retry with its `rev` |
| `not_found`, `engine`, `unavailable` | 1 | see `message`; quote `request_id` to support |
| (local) timeout | 4 | the job is still running — `zebracat video status <id> --wait` |

Add `--human` for tables and color (auto-disabled when output isn't a terminal or `NO_COLOR` is set).

## Async pattern

Video generation is asynchronous. Either block:

```bash
zebracat video create --brief "..." --wait --timeout 45m
```

…or submit and poll yourself (exit `4` on timeout, with the task still running):

```bash
id=$(zebracat video create --brief "..." | jq -r .task_id)
zebracat video status "$id"          # .phase
```

## Configuration

`~/.zebracat/config.json` (settings) and `~/.zebracat/credentials.json` (secrets, `0600`).
Override per-invocation with `--base-url`, `--api-key`, or the `ZEBRACAT_API_KEY`,
`ZEBRACAT_BASE_URL`, `ZEBRACAT_OUTPUT` environment variables.

## Updating

```bash
zebracat update           # download the latest release and replace this binary
zebracat update --check   # just tell me if an update is available
```

The CLI also checks once a day (cached, non-blocking) and prints a one-line notice
to stderr when a newer version is out. In the interactive shell, run `/update`.

## Development

`make build` · `make test` · `make vet` · `make openapi` (refresh the schema copy and
the generated enums from the live server). How the code is laid out and how a request
flows through it: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## License

MIT © Zebracat
