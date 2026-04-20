# AGENTS.md

This file applies to `internal/smoke/`.

## What this folder is
- This folder holds tiny smoke tests.
- It proves the Go test harness is alive before deeper tests matter.

## When editing here
- Keep tests tiny, fast, and obvious.
- Do not turn this folder into a second integration-test suite.

## Keep these rules true
- Smoke tests should be easy to understand in one glance.
- Failures here should point to something very basic being broken.
