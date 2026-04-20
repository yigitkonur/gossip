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
