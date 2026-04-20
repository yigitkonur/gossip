# AGENTS.md

This file applies to `internal/config/`.

## What this folder is
- This folder owns the project-local `.gossip/` configuration.
- It creates and reads `config.json` and `collaboration.md` inside a project.

## When editing here
- Keep config files friendly and easy to reason about.
- Separate project configuration from runtime state.

## Keep these rules true
- `InitDefaults()` should create missing files without overwriting existing ones.
- Defaults should stay readable by a beginner.
- If a config field is not wired yet, do not pretend it is active runtime behavior.
