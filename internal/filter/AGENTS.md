# AGENTS.md

This file applies to `internal/filter/`.

## What this folder is
- This folder decides which Codex messages are important, which are background noise, and which should be grouped into summaries.
- It also holds the bridge reminder text sent back to Codex.

## When editing here
- Make changes carefully because small marker changes affect the whole conversation flow.
- Keep the rules easy for humans and agents to understand.

## Keep these rules true
- Markers only count when they are at the start of the message.
- Filtered mode and full mode must stay clearly different.
- Status buffering should reduce noise without hiding important information.
