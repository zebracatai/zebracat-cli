---
name: zebracat-cli
version: 0.1.0
description: |
  Make AI videos from the terminal with the Zebracat CLI. Use when the user wants to
  generate, translate, list, or download Zebracat videos, or manage voices/avatars/
  account from a shell or an agent. Wraps `zebracat` (JSON out, stable exit codes).
argument-hint: "[idea or topic]"
allowed-tools: Bash, Read, Write
metadata:
  openclaw:
    requires:
      env:
        - ZEBRACAT_API_KEY
    primaryEnv: ZEBRACAT_API_KEY
---

# Zebracat CLI skill

> 🦓 **Using: zebracat-cli** — driving Zebracat from the terminal.

## Preflight

1. Is the CLI installed? `zebracat version` (exit 0 = yes). If not:
   `curl -fsSL https://static.zebracat.ai/cli/install.sh | bash`
2. Are we authenticated? `zebracat auth status`.
   - CI / non-interactive: ensure `ZEBRACAT_API_KEY` is set (pay-as-you-go).
   - Interactive user: `zebracat auth login` (browser; spends plan credits).
   Stop and ask the user to authenticate if neither is available.

## Create a video

Default to **agentic** mode (the AI chooses type/style/voice). Add specifics only when the
user gives them.

```bash
zebracat video create --prompt "<the user's idea>" --render --wait --timeout 30m
```

Useful flags: `--from idea|script|blog|audio|agentic`, `--type ai_video|moving_ai_images|ai_avatar|stock_footage|brainrot`,
`--duration 15|30|60|120|180`, `--aspect vertical|square|horizontal`, `--language`, `--voice <id>`, `--style <id>`.

Parse the JSON: on success you get `{ "task_id": ..., "status": ... }`. With `--wait` you get
the final status incl. `video_url` when `status == "completed"`.

## Track, download, translate

```bash
zebracat video list
zebracat video status <task_id> --wait
zebracat video download <task_id> -o out.mp4
zebracat video translate --url <mp4-url> --to spanish --render --wait
```

## Discover assets (before creating, if the user is picky)

```bash
zebracat voice list --language english
zebracat avatar list
zebracat style list
zebracat account show          # plan + credit balances
```

## Rules

- Parse stdout as JSON; **don't** add `--human`.
- Check exit codes: `3` → tell the user to authenticate; `2` → fix the arguments; `4` →
  the job is still running, poll later with `zebracat video status`.
- Announce the video type you chose and confirm before spending credits if the request is vague.
- Log finished videos to `zebracat-video-log.jsonl` (one JSON line: task_id, status, video_url, topic).
