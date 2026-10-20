# CLAUDE.md — Zebracat CLI

Guidance for Claude Code (and other agents) using or modifying this repository.

## What this is

`zebracat` is a single-binary Go CLI (cobra) over the Zebracat public API. It turns
ideas/scripts/blogs/audio into AI videos and manages voices, avatars, projects, and
billing. Module: `github.com/zebracatai/zebracat-cli`.

## Using the CLI from an agent

- **Output is JSON on stdout** by default — parse it directly. Do **not** pass `--human`
  when you intend to parse output.
- **Errors** go to stderr as `{"error":{"code","message","hint","type","retryable","request_id"}}`
  (the last three when the API sent a typed error).
- **Exit codes**: `0` ok · `1` API/network · `2` usage · `3` auth · `4` timeout. Branch on these.
  A typed API error maps by `type`: `invalid_request` → 2, `authentication`/`permission` → 3,
  everything else (`insufficient_funds`, `rate_limited`, `conflict`, `not_found`, `engine`,
  `unavailable`) → 1. `video generate`/`swap` without `--yes` off a terminal → 2 `confirmation_required`; `swap`
  without `--consent` → 2 `consent_required`.
- **Auth**: API key only — `ZEBRACAT_API_KEY` (env) or `zebracat auth login` (paste a key).
- **Long jobs**: pass `--wait`, or submit and poll `zebracat video status <task_id>` (`.phase`).
  A wait stops at `plan_ready` (plan-only), `needs_input`, or `completed`/`failed`/`cancelled`.
- **Money**: `--dry-run` quotes without spending; every paid POST sends an `Idempotency-Key`
  (printed as `idempotency_key`).

Typical flows (Director v2):
```bash
zebracat video create "<idea>" --wait                                    # one call
id=$(zebracat video create --brief "<idea>" --plan-only --wait | jq -r .task_id)
zebracat plan show "$id"; zebracat video quote "$id"                     # plan first
zebracat video generate "$id" --yes --wait
zebracat video download "$id" -o out.mp4
```

Commands: `video create|list|get|status|quote|generate|answer|cancel|download|clone|translate`,
`plan show|edit|chat|undo`, `studio script|hooks|voice|image|clip|job`, `swap`, `models list`,
`presets list`, plus `script`/`image` (Studio aliases when the server's OpenAPI lists
`/studio/*`, cached a day in `~/.zebracat/capabilities.json`), voices, avatars, styles,
music, templates, characters, brands, account, webhooks, config, update.

## Repo layout

Full module map and traces: `docs/ARCHITECTURE.md`.

```
main.go                     # entrypoint -> cmd.Execute()
cmd/                        # cobra commands (root, auth, video, video_create, plan, studio, assets, …)
internal/client/            # HTTP client, typed API errors, enums_gen.go, server capabilities
internal/openapi/           # checked-in OpenAPI copy (openapi.json), enum extraction, enumgen
internal/videos/            # v2 task lifecycle: fetch (v1 fallback), phases, wait, plan/quote text
internal/apitest/           # test-only fake API serving the OpenAPI examples
internal/config/            # ~/.zebracat config + credentials
internal/tui/               # interactive shell (bubbletea): tui/commands/wizard/api/view/theme
internal/dash/              # project status labels shared by `video list` and the TUI
internal/update/            # release check + self-update
internal/ui/                # JSON/human output, branding, spinner, tables
internal/clierr/            # error envelope + exit codes
internal/version/           # version (set via -ldflags)
```

## Build / test / release

```bash
make build      # -> bin/zebracat (injects version via -ldflags)
make test
make vet
make fmt        # gofmt -w .  (run before committing; CI checks build+vet+test)
make generate   # internal/client/enums_gen.go from internal/openapi/openapi.json
make openapi    # refresh openapi.json from $(API)/docs/openapi.json, then generate
```

- Flag enums (media types, audio modes, presets, clone modes, edit tools…) come from
  `internal/openapi/openapi.json` through `go generate`; never type them by hand.
  `TestEnumsGenMatchesSchema` fails when the generated file is stale.
- `internal/openapi/openapi.json` is the DEV server's own schema (`make openapi API=https://bc-dev.zebracat.ai`).
  Routes it documents without examples answer from `internal/apitest/fixtures.json` (written to the
  backend's real shapes); error statuses without examples answer a canned typed error.
- Tests run against `internal/apitest` (the schema's examples), with golden JSON in
  `cmd/testdata/golden` (`go test ./cmd -update` rewrites them) and a fake clock: no
  test sleeps or touches the network.

- Releases: push a `vX.Y.Z` tag → `.github/workflows/release.yml` runs goreleaser and
  publishes archives named `zebracat_<os>_<arch>.(tar.gz|zip)` (consumed by `install.sh`).
- Keep deps minimal — `spf13/cobra` plus charmbracelet (bubbletea/bubbles/lipgloss) for the TUI.

## Conventions

- Every command returns JSON via `emit(v, humanFn)`; the human renderer is optional.
- Return `*clierr.Error` (via `clierr.Usage/Auth/API/Timeout`) so exit codes stay correct.
- New endpoints: add a thin command that calls `client.Client.Do(ctx, method, path, body, &out)`
  (`DoWith` for headers such as `Idempotency-Key`); add the operation and its examples to
  `internal/openapi/openapi.json` so the fake API answers it.
- No regex reads meaning from text: briefs go to the server as typed; values are checked
  against the generated enums only.
- Commits in this repo are attributed to the user only — no AI/Claude mentions.
