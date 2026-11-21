# Changelog

## v0.7.0 (2026-11-20)

### Changed
- **Pictures upload to the public API.** `--image` and pasted/dropped pictures go to
  `POST /api/v1/public/uploads` (one multipart `file`, PNG/JPEG/WebP ≤ 12 MB) instead of the
  dashboard's `/api/user/upload_assets`, which `api.zebracat.ai` does not serve. A picture
  sent without a kind uses the server's `kind_hint`, else `other`.
- **`zebracat swap` checks the server first.** When the server's OpenAPI lists no
  `/character-swap` routes the swap says so (exit 1, "has no Character Swap API") instead of
  answering a 404, and nothing is analysed or charged. New `zebracat swap status <job_id>
  [--wait]` reads a swap job.
- **`zebracat update` refuses an unchecked download.** A release whose `checksums.txt` can't
  be read is no longer installed without a check.
- **`install.sh` verifies the archive** against the release's `checksums.txt` (`sha256sum`
  or `shasum`) and installs nothing on a mismatch or a missing line.

### Removed
- `zebracat studio script|hooks|voice|image|clip|job`: the Studio API was removed from the
  server. `zebracat script` and `zebracat image` keep using the classic endpoints and never
  call `/studio/*`.
