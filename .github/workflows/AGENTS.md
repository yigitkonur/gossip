# AGENTS.md

This file applies to `.github/workflows/`.

## What this folder is
- This folder holds GitHub Actions workflows.
- It controls how the project is tested and released in automation.

## When editing here
- Be explicit about which workflow is current and which is legacy.
- Prefer the Go toolchain for active CI because Go is the current implementation.

## Keep these rules true
- CI should prove the current Go code builds and tests cleanly.
- If a workflow is still legacy, label that clearly in comments or docs.

## Gotchas hit before — don't re-discover these

### `publish.yml` needs `permissions: contents: write`
- Without it, GoReleaser fails at the release-upload step with `PATCH repos/.../releases/NNN: 403 Resource not accessible by integration`.
- The default `GITHUB_TOKEN` in a workflow is read-only unless you grant write scopes explicitly.
- Also set `fetch-depth: 0` on `actions/checkout` so GoReleaser can diff against previous tags instead of warning `running against a shallow clone`.

### `install-smoke.yml` — scoping shellcheck
- `ludeeus/action-shellcheck` scans `scandir` recursively and will flag unrelated scripts (e.g. `plugins/gossip/scripts/health-check.sh` has an intentional `INPUT=$(cat)` that trips SC2034).
- For single-file lint, call `shellcheck` directly via `apt-get install shellcheck && shellcheck --severity=warning install.sh` — no third-party action scan semantics.

### Two-phase trigger pattern for installer smoke
- Lint job runs on `push`/`pull_request` that touches `install.sh` or its workflow.
- Install job (`if: github.event_name == 'release'`) runs end-to-end install against the just-published tag via `${{ github.event.release.tag_name }}` — the only time this path is exercised against real published archives.
- Use `$RUNNER_TEMP` for the test install dir, not `/usr/local/bin` (unprivileged runner; also gives a fresh dir for idempotency tests).

### Testing workflow changes against a release trigger
- Workflow changes only run when the workflow file is on the branch/tag being evaluated. For release-triggered workflows, merge the fix to `master` first, then delete and recreate the release (`gh release delete --cleanup-tag --yes`, then `gh release create ...`) — recreating fires the trigger against the new workflow file.
