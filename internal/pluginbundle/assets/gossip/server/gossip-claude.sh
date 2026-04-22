#!/usr/bin/env bash

set -euo pipefail

if ! command -v gossip >/dev/null 2>&1; then
  echo "Gossip plugin error: 'gossip' is not installed or not in PATH." >&2
  exit 1
fi

exec gossip claude
