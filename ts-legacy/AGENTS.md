# AGENTS.md

This file applies to `ts-legacy/`.

## What this folder is
- This whole tree is the archived TypeScript/Bun implementation.
- It exists so future agents can compare old behavior with the current Go rewrite.

## When editing here
- Assume reference-only by default.
- Only change this tree if a human explicitly asks for legacy maintenance or comparison artifacts.

## Keep these rules true
- Current implementation work belongs in Go, not here.
- When you cite old behavior, point to exact files in this tree.
