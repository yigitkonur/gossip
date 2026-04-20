# AGENTS.md

This file applies to `scripts/`.

## What this folder is
- This folder holds small maintenance scripts for protocol work.
- It mainly fetches Codex schema files and regenerates protocol code.

## When editing here
- Keep scripts reproducible and easy to run by hand.
- Prefer plain shell over clever tricks.

## Keep these rules true
- Fetch scripts should pin the upstream source clearly.
- Generation scripts should write predictable outputs.
- If generation breaks, document why instead of hiding it.
