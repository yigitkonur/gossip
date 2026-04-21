# AGENTS.md

This file applies to the whole repository unless a deeper `AGENTS.md` overrides it.

## What this project is
- Gossip is a local bridge between **Claude Code** and **Codex**.
- The **Go code is the real implementation now**.
- The `ts-legacy/` tree is an archived reference implementation.

## How to work here
- Start with `README.md` for the big picture.
- Then read the nearest deeper `AGENTS.md` for the folder you plan to change.
- Use `rtk` in shell commands because this repo expects it.
- Prefer small, focused changes that respect the current package boundaries.

## Repo-wide coding rules
- Keep CLI glue in `cmd/` and real behavior in `internal/`.
- Keep protocol shapes in `internal/protocol/` and generic JSON-RPC mechanics in `internal/jsonrpc/`.
- Treat `schema/` as a vendored snapshot, not a casual editing area.
- Treat `ts-legacy/` as reference-only unless a human explicitly asks for legacy edits.

## Verification
- Run the most targeted test you can first.
- Before calling work done, run the full Go checks:
  - `rtk go test ./...`
  - `rtk go vet ./...`
  - `rtk go build ./...`
- If the change affects the release path, also run the GoReleaser snapshot build.

## Platform support — POSIX-only, intentionally
- Gossip targets macOS + Linux on `amd64` and `arm64`. Windows is **not** in the matrix and adding it requires substantial rework.
- Hard blockers for Windows: `syscall.Kill` / `syscall.SIGTERM` / `syscall.SIGKILL` are POSIX-only (used in `internal/daemon/lifecycle.go`, `cmd/gossip/cmd_kill.go`, `cmd/gossip/cmd_codex.go`, `internal/codex/process.go`); PID files under `internal/statedir`; `stty` terminal-state restore in the TUI attach path.
- Do not add no-op Windows stubs. If Windows becomes a real requirement, plan a dedicated effort to abstract signals, PIDs, and terminal state behind a platform seam.

## Release + install pipeline — contract the installer relies on
- Tagged GitHub Releases trigger `.github/workflows/publish.yml`, which runs GoReleaser using `.goreleaser.yml` at repo root.
- Archive naming is a **contract** with `install.sh`: `gossip_<version-without-v>_<os>_<arch>.tar.gz` plus `checksums.txt`. Do not change either side in isolation — update both.
- Inside each archive, the binary is named exactly `gossip`, at the top level of the extracted directory.
- Checksum file is `checksums.txt` (sha256). `install.sh` fetches it alongside the archive and skips verification gracefully if it's missing.
- `.goreleaser.yml` uses `mode: replace`, which **overwrites the release body**. On an initial tag (no prior tag), the auto-changelog falls back from `github` to `git` and will pull history from before any renames — pollution is almost guaranteed on the first tag after a rebrand. For the first tag after a rename, rewrite the body manually via `gh release edit --notes ...` after the workflow succeeds. Subsequent tags diff cleanly against the previous tag.
- `install.sh` is idempotent: if the target version is already at `$INSTALL_DIR/gossip`, it exits without work. `--force` reinstalls; `--uninstall` removes. Re-runs over piped curl are safe.
- Local dry-run of the pipeline: `rm -rf dist && $(go env GOPATH)/bin/goreleaser release --snapshot --clean --skip=publish,validate` — produces the same archive layout the workflow will, without needing a tag or token.
- `make release-all` (Makefile) and GoReleaser are kept in sync: both emit the same 4 tarballs with identical naming. Changing one without the other is a trap.

## Beginner mental model
- `cmd/gossip` = the commands a human runs.
- `internal/codex` = talks to Codex and its TUI.
- `internal/mcp` = talks to Claude Code.
- `internal/control` + `internal/daemon` = keep the background bridge alive and coordinated.
