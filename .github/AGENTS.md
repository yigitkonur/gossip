# AGENTS.md

This file applies to `.github/`.

## What this folder is
- This folder contains GitHub-side project automation and contributor templates.
- It affects how humans report issues, how CI runs, and how releases ship.

## Layout
- `workflows/ci.yml` — Go build + test + vet on every push/PR. Gate for merges.
- `workflows/publish.yml` — tag-triggered GoReleaser run that produces the release archives + `checksums.txt`.
- `workflows/install-smoke.yml` — two-phase installer validation (lint on `push`/`pull_request`, end-to-end install on `release: [published]`).
- `ISSUE_TEMPLATE/` — bug / feature templates. Keep short and beginner-friendly.
- `pull_request_template.md` — PR skeleton.

## When editing here
- Prefer the current Go workflows for active automation.
- Keep contributor-facing files simple and welcoming.
- Before changing a workflow, read `workflows/AGENTS.md` for the traps already discovered (publish.yml permissions, shellcheck scoping, release-triggered workflow testing).

## Keep these rules true
- CI should reflect the current Go implementation.
- Any legacy automation that still lives here must be labeled clearly.
- The archive-naming contract between `publish.yml` → `.goreleaser.yml` → `install.sh` is load-bearing; see the root `AGENTS.md` "Release + install pipeline" section before touching either side.
