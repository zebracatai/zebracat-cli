# @zebracat/cli

The **Zebracat CLI** — make AI videos from your terminal. Turn an idea, script, blog URL, or audio file into a finished video, plus voices, avatars, brands, and 170+ language dubbing.

## Install

```bash
npm install -g @zebracat/cli
```

This downloads the native `zebracat` binary for your platform (macOS, Linux, Windows · amd64/arm64) from the [official releases](https://github.com/zebracatai/zebracat-cli/releases), verified against the release checksums.

Prefer a self-contained script instead? `curl -fsSL https://cli.zebracat.ai | bash`

## Get started

```bash
zebracat login                 # or: export ZEBRACAT_API_KEY=sk-...
zebracat                       # describe a video, paste a script or a link; change the plan in words
zebracat create "<idea>" --json   # scripts and agents: a plan + quote, never prompts
```

Output is JSON on stdout (add `--human` for tables). Pattern is `zebracat <noun> <verb>`.

## Notes

- The npm package is a thin wrapper: the binary is fetched on install and re-fetched lazily on first run if install was run with `--ignore-scripts`.
- The npm version tracks the CLI release of the same number (e.g. `@zebracat/cli@0.6.3` installs `v0.6.3`). Update with `npm install -g @zebracat/cli@latest`; `zebracat doctor` shows which copy runs if you also used the curl installer..
- Requires Node.js ≥ 18 and `tar` on your PATH (bundled on macOS, Linux, and Windows 10+).

## Links

- [Zebracat](https://zebracat.ai) · [API docs](https://docs.zebracat.ai) · [Skills](https://mcp.zebracat.ai/skills)
- [CLI repository](https://github.com/zebracatai/zebracat-cli)

MIT © Zebracat
