# AGENTS.md

This file applies to `plugins/`.

## What this folder is
- This folder contains plugin packaging for the current project.
- It is a thin layer that points Claude Code at the Go bridge.

## When editing here
- Keep plugin packaging simple.
- Let the Go binary own the real runtime behavior.

## Keep these rules true
- Plugin config should match the current CLI commands.
- Avoid reintroducing legacy complexity here.

## Staying in sync with the embed
- `plugins/gossip/` is mirrored into `internal/pluginbundle/assets/gossip/` and shipped via `go:embed` in the gossip binary.
- After editing anything under `plugins/gossip/`, run `make sync-plugin` (or `make build`, which depends on it) before committing.
- CI catches drift via `TestBundleInSync` in `internal/pluginbundle/`.
