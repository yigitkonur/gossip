# AGENTS.md

This file applies to `internal/protocol/`.

## What this folder is
- This folder is the shared protocol vocabulary.
- It defines wire types, method names, and the generic JSON-RPC envelope.

## When editing here
- Keep files focused on plain data shapes and constants.
- Do not add subprocess logic, network loops, or business rules here.

## Keep these rules true
- `Envelope.Kind()` must keep classifying messages only from `id` and `method`.
- Method constants and lookup helpers must stay in sync.
- `BridgeMessage.Source` should stay limited to `claude` and `codex`.
