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
curl -fsSL https://static.zebracat.ai/cli/install.sh | bash

# or with Go
go install github.com/zebracatai/zebracat-cli@latest   # installs as `zebracat-cli`

# or grab a binary from the Releases page
```

## Interactive shell

Just run `zebracat` (no arguments) to open the interactive shell — a branded,
purple-themed REPL:

```
╭───────────────────────────────────────────────────────╮
│     __ ___ _ ___ _ _ __ __ _ ___                        │
│    |_ / -_) '_ \ '_/ _`/ _/ _`|  _|                     │
│    /__\___|_.__/_| \__,_\__\__,_|\__|                    │
│                                                         │
│  AI video generation, right in your terminal.           │
│                                                         │
│  Type /help for commands · /quit to exit                │
│  Try /video — or just describe the video you want.      │
╰───────────────────────────────────────────────────────╯
🦓 › a 30-second cinematic ad for my coffee brand
```

- **Slash commands** with autocomplete: `/video`, `/status`, `/projects`, `/voices`,
  `/styles`, `/account`, `/login`, `/help`, `/quit` (start typing `/` and press Tab).
- **Just describe a video** in plain language and it walks you through the rest.
- Output stays in your scrollback after you exit (no alt-screen).

Prefer one-shot commands? Everything below also works non-interactively
(`zebracat video create …`), which is what scripts and agents should use.

## Authentication — two ways, two wallets

| Method | How | Billed from |
|--------|-----|-------------|
| **OAuth login** (recommended for people) | `zebracat auth login` → browser sign-in | your **plan credits** |
| **API key** (recommended for CI/agents) | `export ZEBRACAT_API_KEY=sk-…` | **API dollar balance** (pay-as-you-go) |

With `ZEBRACAT_API_KEY` set, nothing reads the terminal — the CLI is non-interactive
by default. On a headless box, `zebracat auth login --device` prints a URL + code to paste.

```bash
zebracat auth login            # browser OAuth (plan credits)
zebracat auth status           # am I logged in?
zebracat auth whoami           # account + balances
```

## Quick start

```bash
# Let the AI pick the best settings, and wait for the finished MP4
zebracat video create --prompt "A 30-second explainer on compound interest" --render --wait

# Specific recipe
zebracat video create --from idea --prompt "Top 3 productivity tips" \
  --type ai_video --duration 30 --aspect vertical --voice <id> --render --wait

# From a blog post
zebracat video create --from blog --url https://example.com/post --wait

# Track + fetch
zebracat video list
zebracat video status <task_id> --wait
zebracat video download <task_id> -o out.mp4

# Translate / dub
zebracat video translate --url https://.../in.mp4 --to spanish --render --wait
```

## Commands

```
zebracat auth        login | logout | status | whoami
zebracat video       create | list | get | status | download | cancel | translate
                     prompt-styles | languages
zebracat voice       list | clone
zebracat avatar      list
zebracat style       list
zebracat music       list
zebracat template    list
zebracat character   list
zebracat account     show | pricing | usage
zebracat webhook     list | create | delete
zebracat config      list | set
zebracat completion  bash | zsh | fish | powershell
zebracat update | version
```

Run `zebracat <command> --help` for full flags.

## Output & exit codes (agent-friendly)

- **stdout** is JSON by default — pipe it into `jq`, scripts, or an agent.
- **stderr** carries the structured error envelope:
  ```json
  { "error": { "code": "auth_error", "message": "...", "hint": "..." } }
  ```
- **exit codes**: `0` ok · `1` API/network · `2` usage · `3` auth · `4` timeout.

Add `--human` for tables and color (auto-disabled when output isn't a terminal or `NO_COLOR` is set).

## Async pattern

Video generation is asynchronous. Either block:

```bash
zebracat video create --prompt "..." --wait --timeout 45m
```

…or submit and poll yourself (exit `4` on timeout, with the task still running):

```bash
id=$(zebracat video create --prompt "..." | jq -r .task_id)
zebracat video status "$id"
```

## Configuration

`~/.zebracat/config.json` (settings) and `~/.zebracat/credentials.json` (secrets, `0600`).
Override per-invocation with `--base-url`, `--api-key`, or the `ZEBRACAT_API_KEY`,
`ZEBRACAT_BASE_URL`, `ZEBRACAT_OUTPUT` environment variables.

## License

MIT © Zebracat
