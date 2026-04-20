#!/usr/bin/env bash
set -euo pipefail

TAG="rust-v0.120.0"
REPO="openai/codex"
SCHEMA_PATH="codex-rs/app-server-protocol/schema/json"
BASE_URL="https://raw.githubusercontent.com/${REPO}/${TAG}/${SCHEMA_PATH}"
FILES=(
  "ClientRequest.json"
  "ServerRequest.json"
  "ServerNotification.json"
)

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
schema_dir="${repo_root}/schema"

mkdir -p "${schema_dir}"

for file in "${FILES[@]}"; do
  curl --fail --silent --show-error --location \
    "${BASE_URL}/${file}" \
    --output "${schema_dir}/${file}"
done

cat > "${schema_dir}/VERSION" <<VERSION
repo=${REPO}
tag=${TAG}
path=${SCHEMA_PATH}
VERSION
