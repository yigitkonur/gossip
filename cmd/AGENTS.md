# AGENTS.md

This file applies to `cmd/`.

## What this folder is
- This folder contains command-line entrypoints.
- It is where users meet the system.

## When editing here
- Keep command packages thin.
- Push business logic down into `internal/` packages.

## Keep these rules true
- CLI code should mostly parse input, call internal packages, and print results.
- User-facing messages should stay clear and calm.
