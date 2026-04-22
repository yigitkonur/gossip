#!/usr/bin/env bash

set -uo pipefail

INPUT="$(cat 2>/dev/null || true)"
workspace="${CLAUDE_PROJECT_DIR:-${PWD}}"
cooldown_seconds="${GOSSIP_HEALTH_HOOK_COOLDOWN_SECONDS:-120}"
state_root="${GOSSIP_HOOK_STATE_DIR:-${TMPDIR:-/tmp}/gossip-hooks}"
default_control_port="${GOSSIP_CONTROL_PORT:-4502}"

resolve_state_dir() {
  if [ -n "${GOSSIP_STATE_DIR:-}" ]; then
    printf '%s' "${GOSSIP_STATE_DIR}"
    return
  fi
  if [ "$(uname -s)" = "Darwin" ]; then
    printf '%s' "${HOME}/Library/Application Support/Gossip"
    return
  fi
  printf '%s' "${XDG_STATE_HOME:-${HOME}/.local/state}/gossip"
}

extract_json_int() {
  local file="$1"
  local key="$2"
  sed -nE 's/.*"'"${key}"'"[[:space:]]*:[[:space:]]*([0-9]+).*/\1/p' "$file" | head -n1
}

mkdir -p "$state_root" 2>/dev/null || true
workspace_key="$(printf '%s' "$workspace" | cksum | awk '{print $1}')"
stamp_file="${state_root}/sessionstart-${workspace_key}.stamp"
now="$(date +%s)"

if [ -f "$stamp_file" ]; then
  last_notice="$(cat "$stamp_file" 2>/dev/null || echo 0)"
  if [ $((now - last_notice)) -lt "$cooldown_seconds" ]; then
    exit 0
  fi
fi

printf '%s' "$now" >"$stamp_file" 2>/dev/null || true

state_dir="$(resolve_state_dir)"
ports_file="${state_dir}/ports.json"
status_file="${state_dir}/status.json"
control_port="$default_control_port"
ports_state="missing"

if [ -f "$ports_file" ]; then
  ports_state="present"
  maybe_port="$(extract_json_int "$ports_file" controlPort)"
  if [ -n "$maybe_port" ]; then
    control_port="$maybe_port"
  fi
elif [ -f "$status_file" ]; then
  maybe_port="$(extract_json_int "$status_file" controlPort)"
  if [ -n "$maybe_port" ]; then
    control_port="$maybe_port"
  fi
fi

if ! command -v curl >/dev/null 2>&1; then
  exit 0
fi

health_json="$(curl -fsS --max-time 1 "http://127.0.0.1:${control_port}/healthz" 2>/dev/null || true)"

if [ -n "$health_json" ]; then
  tui_connected="false"
  if printf '%s' "$health_json" | grep -q '"tuiConnected":true'; then
    tui_connected="true"
  fi

  if [ "$tui_connected" = "true" ]; then
    cat <<JSON
{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Gossip is running. Daemon healthy, Codex TUI connected, ports file ${ports_state}."}}
JSON
  else
    cat <<JSON
{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Gossip daemon is running but Codex TUI is not connected yet. Start Codex in another terminal with: gossip codex (ports file ${ports_state})."}}
JSON
  fi
else
  cat <<JSON
{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Gossip daemon is not reachable on http://127.0.0.1:${control_port}/healthz yet. Ports file ${ports_state}. Start the bridge with: gossip claude (this terminal) + gossip codex (another terminal)."}}
JSON
fi
