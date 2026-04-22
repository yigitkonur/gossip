# AGENTS.md

This file applies to `internal/pluginbundle/`.

## What this folder is
- Holds the Claude Code plugin bundle as an `embed.FS` so the gossip binary can seed `~/.claude/plugins/cache/gossip` without any on-disk source checkout.
- `bundle.go` exposes `Install(dst string) error`, used by `gossip init` as the default plugin source.

## Canonical source vs. embed mirror
- The canonical plugin bundle lives at `plugins/gossip/`. Edit there.
- `internal/pluginbundle/assets/gossip/` is a byte-for-byte mirror used by `//go:embed`.
- Run `make sync-plugin` after editing `plugins/gossip/`. CI (`TestBundleInSync`) fails on drift.

## When editing here
- Do not edit files under `assets/gossip/` directly.
- Changes to `bundle.go` should preserve the executable-bit handling for `*.sh` files: Claude spawns them as commands.
- Keep `Install` idempotent — `gossip init` may run multiple times over the same cache.

## Keep these rules true
- `plugins/gossip/` is the source of truth; `assets/gossip/` is generated.
- The embed must ship with the binary — never `.gitignore` `assets/gossip/`.
