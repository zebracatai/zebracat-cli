---
name: zebracat-cli
version: 0.2.0
description: |
  Make AI videos from the terminal with the Zebracat CLI. Use when the user wants to
  generate, plan, edit, price, translate, clone, list or download Zebracat videos, write
  scripts/hooks/voice-overs/images/clips with Zebracat Studio, or manage voices/avatars/
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
   `curl -fsSL https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh | bash`
2. Are we authenticated? `zebracat auth status`. The CLI uses an API key: ensure
   `ZEBRACAT_API_KEY` is set (paid from the API dollar balance). Otherwise stop and ask
   the user for a key (studio.zebracat.ai → API Keys).

## Make a video: one line

```bash
zebracat video create "<the user's idea>" --wait --timeout 45m
```

Director v2 writes a plan, generates it and renders the MP4. With `--wait` the JSON has
`phase: "completed"` and `video_url`. If it stops at `phase: "needs_input"`, read
`.question` and answer (below).

## Make a video: plan first (preferred when money matters or the brief is vague)

```bash
id=$(zebracat video create --brief "<idea>" --plan-only --wait | jq -r .task_id)   # stops at plan_ready
zebracat plan show "$id"                       # show the user .screenplay / summary_lines
zebracat video quote "$id"                     # .credits, .usd, .why — tell the user the price
# change it if the user asks:
zebracat plan chat "$id" "<the user's words>"  # free, 20/hour
zebracat plan edit "$id" --tool set_duration --args '{"seconds":15}'
# only after the user says yes:
zebracat video generate "$id" --yes --wait
zebracat video download "$id" -o out.mp4
```

- `--dry-run` on `video create` / `video generate` returns the quote and spends nothing.
- `video generate` without `--yes` refuses in scripts (exit 2, `confirmation_required`):
  never pass `--yes` before the user agreed to the quote.
- Plan questions: `zebracat video status "$id"` shows `.question.id`; answer with
  `zebracat video answer "$id" --question <id> --answer "<text>"`, then wait again.

## Flags that matter

- What the brief is: `--input idea|script|vo-script|script-scenes|url`; `--script <file>`,
  `--url <page>`, `--file <brief-file>` set it for you.
- Look and sound: `--media-type ai_video|ai_image_video|brainrot|stock`,
  `--audio narration|dialogue|silent`, `--aspect vertical|horizontal|square`,
  `--quality quick|balanced|cinematic|auto`, `--seconds N`, `--language`.
- Assets: `--voice <id>`, `--style <id>`, `--preset <slug>` (`zebracat presets list`),
  `--upload <url>[:person|place|product|style|other]` (repeatable), `--brand <id>`.
- `--no-render` keeps an editable project; `--idempotency-key <k>` makes a retry safe
  (the default key is printed as `idempotency_key`).

Only pass what the user said; Director v2 decides the rest from the brief.

## Other jobs

```bash
zebracat video status <task_id> --wait        # .phase
zebracat video clone --url <video> --mode remix|generate_similar|recreate_script [--topic] --wait
zebracat video translate --url <mp4-url> --to spanish --render --wait
zebracat studio script "<idea>" --seconds 30 --wait      # .result.narration_text
zebracat studio hooks --script script.txt                # .hooks[].line
zebracat studio voice --text script.txt --voice <id> --dry-run   # paid: quote first
zebracat studio image "<prompt>" --aspect vertical --count 2 --wait
zebracat studio clip "<prompt>" --seconds 5 --dry-run           # paid: quote first
zebracat swap --video <clip-url> --character <face-url> --consent --dry-run   # paid: quote first
# --consent only when the user confirmed they may use that face; never add it yourself
zebracat models list ; zebracat presets list ; zebracat voice list --language english
zebracat account show                                    # balances
```

## Rules

- Parse stdout as JSON; **don't** add `--human`. Errors are JSON on stderr:
  `{"error":{code,message,hint,type,retryable,request_id}}`.
- Exit codes: `2` → fix the arguments (or `confirmation_required`: ask the user);
  `3` → tell the user to set a valid API key; `1` with `type: insufficient_funds` → show
  the `hint` verbatim (it says where to top up); `1` with `retryable: true` → wait and
  retry; `4` → the job is still running, poll later with `zebracat video status`.
- Tell the user the quote before any paid step (`video generate`, `studio voice`,
  `studio clip`, `swap`); `video clone` has no quote, so confirm it with the user first.
  Script, hooks, images, plans and plan edits are free.
- Log finished videos to `zebracat-video-log.jsonl` (one JSON line: task_id, phase, video_url, topic).
