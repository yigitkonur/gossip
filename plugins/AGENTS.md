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
