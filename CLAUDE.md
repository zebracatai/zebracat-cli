# CLAUDE.md — Zebracat CLI

Guidance for Claude Code (and other agents) using or modifying this repository.

## What this is

`zebracat` is a single-binary Go CLI (cobra) over the Zebracat public API. It turns
ideas/scripts/blogs/audio into AI videos and manages voices, avatars, projects, and
billing. Module: `github.com/zebracatai/zebracat-cli`.

## Using the CLI from an agent

- **Output is JSON on stdout** by default — parse it directly. Do **not** pass `--human`
  when you intend to parse output.
- **Errors** go to stderr as `{"error":{"code","message","hint"}}`.
- **Exit codes**: `0` ok · `1` API/network · `2` usage · `3` auth · `4` timeout. Branch on these.
- **Auth**: prefer `ZEBRACAT_API_KEY` (env) for non-interactive runs; it short-circuits any
  prompt. Otherwise `zebracat auth login` (browser) or `--device` on headless hosts.
- **Long jobs**: pass `--wait` to block until the video is ready, or submit and poll
  `zebracat video status <task_id>`.

Typical flow:
```bash
id=$(zebracat video create --prompt "<idea>" --render | jq -r .task_id)
zebracat video status "$id" --wait
zebracat video download "$id" -o out.mp4
```

## Repo layout

```
main.go                     # entrypoint -> cmd.Execute()
cmd/                        # cobra commands (root, auth, video, assets, account, webhook, misc)
internal/client/            # HTTP client, auth resolution, OAuth token refresh
internal/auth/              # interactive OAuth 2.1 (PKCE) login: browser loopback + device
internal/config/            # ~/.zebracat config + credentials
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
```

- Releases: push a `vX.Y.Z` tag → `.github/workflows/release.yml` runs goreleaser and
  publishes archives named `zebracat_<os>_<arch>.(tar.gz|zip)` (consumed by `install.sh`).
- Keep deps minimal — currently only `spf13/cobra`. Everything else is stdlib.

## Conventions

- Every command returns JSON via `emit(v, humanFn)`; the human renderer is optional.
- Return `*clierr.Error` (via `clierr.Usage/Auth/API/Timeout`) so exit codes stay correct.
- New endpoints: add a thin command that calls `client.Client.Do(ctx, method, path, body, &out)`.
- Commits in this repo are attributed to the user only — no AI/Claude mentions.
