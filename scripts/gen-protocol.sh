#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
output_file="${repo_root}/internal/protocol/types_gen.go"
schema_dir="${repo_root}/schema"

if command -v go-jsonschema >/dev/null 2>&1; then
  generator=(go-jsonschema)
elif [ -x "$(go env GOPATH)/bin/go-jsonschema" ]; then
  generator=("$(go env GOPATH)/bin/go-jsonschema")
else
  generator=(go run github.com/atombender/go-jsonschema@v0.23.0)
fi

mkdir -p "$(dirname "${output_file}")"

"${generator[@]}" \
  --package protocol \
  --output "${output_file}" \
  --tags json \
  --only-models \
  "${schema_dir}/ClientRequest.json" \
  "${schema_dir}/ServerRequest.json" \
  "${schema_dir}/ServerNotification.json"
