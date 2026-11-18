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
curl -fsSL https://cli.zebracat.ai | bash

# pin a specific version
curl -fsSL https://cli.zebracat.ai | ZEBRACAT_INSTALL_VERSION=v0.1.0 bash

# or with Go
go install github.com/zebracatai/zebracat-cli@latest   # installs as `zebracat-cli`

# or grab a binary from the Releases page
```

> The installer downloads binaries from GitHub Releases. `https://cli.zebracat.ai`
> (and `/install.sh`) serves this repo's `install.sh` live from `main`; the raw GitHub
> URL `https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh` works too.

## Talk to it

Run `zebracat` (or `zebracat create "<idea>"`) and say what you want. It works like
the studio's chat (the same plan engine as /chat): Zebracat writes a **plan**, you
change it **in plain words**, and nothing is spent until you choose **Generate**.

```
 ▙               ▟
 █ ▖           ▗ █
 █ █           █ █    Zebracat v0.6.4
 █ █ █ █ █ █ █ █ █    you@example.com · $208.25 API balance
 █ █ █ █ █ █ █ █ █    ~/projects/coffee
 █ █ █ █ █ █ █ █ █
 █ █ █ █ █ █ █ █ █    Describe a video, paste a script or a link.
 █ █ █ █ █ █ █ █ █    /help for commands · ctrl+c twice to quit
 ▝ ▀ █ █ █ █ █ ▀ ▘
       ▀ █ ▀
───────────────────────────────────────────────────────────────────────
› hi
───────────────────────────────────────────────────────────────────────
● Hi! What should your video be about? Tell me the idea, a product, a story, or a topic.
› a 20-second video on why cats knock things off tables
▄▄ Writing the screenplay… (0:21 · esc to interrupt)
╭─────────────────────────────────────────────────────────────────────╮
│ Why cats knock things off tables                                    │
│ Type AI video · Style Cinematic · Format vertical · Length 20 s ·   │
│ Scenes 4                                                            │
│ Audio voice-over · Language english                                 │
│                                                                     │
│ 1. (5s) Ever wondered why your cat swipes your mug off the table?   │
│ …                                                                   │
│ Price  96 credits ($4.80) from your API balance                     │
│ Studio https://studio.zebracat.ai/chat/0b7e6a52-…                   │
╰─────────────────────────────────────────────────────────────────────╯
❯ Generate (96 credits)
  Discard
```

The mark is the real Zebracat logo (the striped cat-head shield from `logo.svg`), drawn
in `#6B3BF4` (nearest 256-colour on older terminals, plain with `NO_COLOR`). It follows
the window: 17×10 on a normal or full-screen terminal (never bigger), 9×5 on a small
one, a 1-row mark on a tiny one, and redraws when you resize.

The input stays on the **last lines of the terminal**, with the conversation scrolling
above it (your terminal's scrollback keeps everything). Under it, a two-line status
footer: what you can do now, then your **plan, balance, credits, account and version**
(cut from the end on a narrow terminal; the balance refreshes after you spend):

```
───────────────────────────────────────────────────────────────────────────────
› Describe your video, paste a script or a link
───────────────────────────────────────────────────────────────────────────────
  / for commands · alt+enter new line · ↑ history
  Cat Mode · $208.25 API balance · 1,240 credits · you@example.com · v0.6.4
```

- **Type an idea, paste a script, or paste a link.** A TikTok / YouTube / Reel link is
  read like the studio reads it: "clone this" copies it, "like this but about matcha"
  uses it as inspiration. A lone web-page link becomes a video about that page.
  Multi-line pastes keep their lines (Alt+Enter adds a line); `/script <file>` loads one.
- **When the plan asks something** ("What should your video be about?", or a question
  with options), that's Zebracat's turn — just answer; your first message goes with it.
- **The plan card** shows the video type (AI video, stock footage, brainrot…), style,
  format, length, scenes, voices and the price, and links the plan in the studio's /chat.
  A plan whose screenplay is written at Generate shows the studio's estimate and says
  the exact price comes then; stock footage says why it costs no scene credits.
- **Under the card: `❯ Generate (N credits)` / `Discard`.** ↑/↓ and Enter, or **g** /
  **d**. Generate is the studio's Generate (it writes the screenplay first when the plan
  has none yet). Discard lets the plan go (nothing is charged) and you start again.
  A balance that doesn't cover it is told so instead.
- **Or just type a change** in plain words ("use a female voice", "drop scene 3"): the
  plan is changed, and the card and the choice come back. (A change that starts with g
  or d: type a space first.)
- **The animated mark** shows what the server is doing (writing the screenplay, making
  the scenes, exporting the MP4), the elapsed time, and **Esc** to stop waiting.
  Esc and Ctrl+C never cancel anything: a paid video keeps being made.
- **Keys:** Enter send · Alt+Enter new line · ↑/↓ history · Tab completes `/` commands ·
  Ctrl+O opens the finished video · Ctrl+C clears the line, twice quits.
- **Commands:** `/help`, `/new`, `/generate`, `/plan`, `/open`, `/script`, `/videos`,
  `/status`, `/balance`, `/voices`, `/styles`, `/login`, `/logout`, `/update`, `/clear`, `/exit`.
- `NO_COLOR` turns colour off; output stays in your scrollback after you exit.

### Pictures: paste or drop them in

Attach pictures to your message (a product, a person, a place, a style). They show in
the input as chips, `[Image #1 · 1080×1920 · 240 KB]` (paste with **Ctrl+V**, or **Alt+V**); **Backspace** on an empty line
removes the last one. On send they're uploaded the way the studio's chat does it, and the
plan uses them. PNG, JPEG or WebP, up to 12 MB each; several per message are fine.

| Where | Paste a picture | Drop a file |
|---|---|---|
| macOS (Terminal, iTerm2, Ghostty, WezTerm, Kitty) | copy the picture, press **Ctrl+V** (not Cmd+V: the terminal keeps Cmd+V for text) | drag the file into the window |
| Linux (Wayland) | **Ctrl+V**, needs `wl-clipboard` (`wl-paste`) | drag the file in |
| Linux (X11) | **Ctrl+V**, needs `xclip` | drag the file in |
| Windows (Windows Terminal, PowerShell) | **Alt+V** (Windows Terminal keeps Ctrl+V for its own text paste); read through PowerShell. WSL: use a drop or a path | drag the file in |
| over SSH, in tmux on a remote box, in containers | Ctrl+V reads the *remote* clipboard, which has no picture | paste the file's path instead |

A dropped or pasted path (`/Users/me/photo.png`, a quoted path, `my\ photo.png`,
`file://…`) becomes a chip when the file is a picture; any other text stays text.
Text pastes, even long multi-line scripts, go into the input as one paste.

Pictures go with a new video. The plan chat takes words only, so to add pictures to an
existing plan, start a new video (`/new`) with them.

**Scripts and agents never get a prompt.** Without a terminal (a pipe, CI) or with
`--json`, `zebracat` alone exits 2 (`needs_terminal`), and `zebracat create` writes the
plan and prints it with its quote as JSON:

```bash
zebracat create "why cats knock things off tables" --json   # plan + quote, never spends
zebracat create "…" --yes --json                            # plan, generate, wait (spends)
zebracat create --script script.txt --json
zebracat create "a launch video for this mug" --image mug.png:product --json   # pictures: <path>[:person|place|product|style|logo|other]
zebracat create https://youtu.be/abc123 --json              # a link, like the studio
```

Every older command below keeps working unchanged.

## Authentication

The CLI uses the **public API**, which authenticates with an **API key** (billed
pay-as-you-go from your API dollar balance). Create one at
[studio.zebracat.ai → API Keys](https://studio.zebracat.ai).

```bash
zebracat login                 # paste your API key (stored at ~/.zebracat, 0600)
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
zebracat video clone --url https://www.tiktok.com/@x/video/1 --mode generate_similar --topic "home coffee" --wait
zebracat video translate --url https://…/in.mp4 --to spanish --render --wait
```

Character swap:

```bash
zebracat swap --video https://…/clip.mp4 --character https://…/face.jpg --consent --dry-run   # paid per second
zebracat swap status <job_id> --wait                                                         # follow a swap
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

- `--dry-run` (on `video create`, `video generate`, `swap`) answers the quote and spends nothing.
- `video generate` and `swap` on a terminal show the quote and ask; scripts and agents
  are never asked and must pass `--yes`. `swap` also needs `--consent`: you state you
  have the right to use the face.
- Every paid POST sends an `Idempotency-Key` (a fresh UUID per run, printed as
  `idempotency_key`); re-run with `--idempotency-key <that key>` and the server
  answers the first result instead of spending twice.

## Commands

```
zebracat [create]    the conversation (plan → change → Generate / Discard); `create --json` for scripts
zebracat login       same as `auth login`
zebracat auth        login | logout | status | whoami
zebracat video       create | list | get | status | quote | generate | answer | cancel
                     download | clone | translate | prompt-styles | languages
zebracat plan        show | edit | chat | undo
zebracat swap        --video <url> --character <image-url> --consent  # character swap; `swap status <job_id>`
zebracat models      list                       # qualities, models, credits per 5 s
zebracat presets     list                       # street interview, podcast, UGC ad, …
zebracat script      <idea>                     # a voice-over script (`POST /script_generator`)
zebracat image       <prompt>                   # an AI image (the classic image generator)
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
zebracat update | version | doctor
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
zebracat update           # download the latest release, check its checksum, replace this binary, start it
zebracat update --check   # just tell me if an update is available
```

On launch the CLI checks GitHub's latest release at most **once a day** (cached in
`~/.zebracat/update.json`, 1.5 s at most, never blocking or failing a command). In the
conversation the welcome box offers it: **press `u`** and the new version installs and
opens in place ("✓ Updated from v0.3.0"). Other commands print one line on stderr:

```
↑ Zebracat CLI v0.4.1 is available — run `zebracat update`
```

`zebracat update` on a terminal starts the new version right away (`--no-relaunch`,
`--json` and scripts only update). The check is off with `ZEBRACAT_NO_UPDATE_CHECK=1`, in
CI, with `--quiet`, and when stderr is not a terminal. npm, Homebrew and `go install`
copies are told their own command (`npm install -g @zebracat/cli@latest`, …).

**Two copies?** `zebracat doctor` lists every `zebracat` on your PATH, which one runs,
its version and how to remove the others (an old `npm install -g @zebracat/cli` copy
can shadow the one `install.sh` put in `~/.local/bin`). `zebracat --version` prints the
path of the binary that answered.

## Development

`make build` · `make test` · `make vet` · `make openapi` (refresh the schema copy and
the generated enums from the live server). How the code is laid out and how a request
flows through it: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## License

MIT © Zebracat
