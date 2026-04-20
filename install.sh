#!/usr/bin/env bash
#
# Gossip installer — downloads the right prebuilt binary for your OS/arch
# and installs it to /usr/local/bin/gossip.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash
#
# Environment overrides:
#   GOSSIP_VERSION=v0.2.0   Install a specific version (default: latest).
#   GOSSIP_INSTALL_DIR=/usr/local/bin
#   GOSSIP_REPO=yigitkonur/gossip
#
# Re-running upgrades the binary in place.

set -euo pipefail

REPO="${GOSSIP_REPO:-yigitkonur/gossip}"
VERSION="${GOSSIP_VERSION:-latest}"
INSTALL_DIR="${GOSSIP_INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="gossip"

# ---------- colors ----------
if [ -t 1 ] && command -v tput >/dev/null 2>&1 && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
  C_RESET="$(tput sgr0)"
  C_BOLD="$(tput bold)"
  C_RED="$(tput setaf 1)"
  C_GREEN="$(tput setaf 2)"
  C_YELLOW="$(tput setaf 3)"
  C_BLUE="$(tput setaf 4)"
  C_CYAN="$(tput setaf 6)"
else
  C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_CYAN=""
fi

info()  { printf "%s==>%s %s\n" "${C_BLUE}${C_BOLD}" "${C_RESET}" "$*"; }
ok()    { printf "%s✓%s   %s\n" "${C_GREEN}${C_BOLD}" "${C_RESET}" "$*"; }
warn()  { printf "%s⚠%s   %s\n" "${C_YELLOW}${C_BOLD}" "${C_RESET}" "$*" >&2; }
die()   { printf "%s✗%s   %s\n" "${C_RED}${C_BOLD}"  "${C_RESET}" "$*" >&2; exit 1; }

banner() {
  printf "%s" "${C_CYAN}"
  cat <<'BANNER'
   ____  ___   ____ ____ ___ ____
  / ___|/ _ \ / ___/ ___|_ _|  _ \
 | |  _| | | |\___ \___ \| || |_) |
 | |_| | |_| | ___) |__) | ||  __/
  \____|\___/ |____/____/___|_|
BANNER
  printf "%s\n\n" "${C_RESET}"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

detect_os() {
  local os
  os="$(uname -s)"
  case "$os" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) die "unsupported OS: $os (install a prebuilt binary from the Releases page)" ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $arch" ;;
  esac
}

resolve_version() {
  local v="$1"
  if [ "$v" != "latest" ]; then
    printf "%s" "$v"
    return 0
  fi
  local tag
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
         | grep -E '"tag_name":\s*"[^"]+"' \
         | head -n 1 \
         | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
  if [ -z "$tag" ]; then
    die "could not resolve latest release from GitHub API (set GOSSIP_VERSION=v0.x.y to pin)"
  fi
  printf "%s" "$tag"
}

download() {
  local url="$1" dest="$2"
  info "downloading ${url}"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL --progress-bar -o "$dest" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --show-progress -O "$dest" "$url"
  else
    die "need either curl or wget"
  fi
}

verify_checksum() {
  local archive="$1" sums="$2"
  local expected actual
  expected="$(grep "$(basename "$archive")" "$sums" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    warn "no checksum found for $(basename "$archive") — skipping verification"
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    warn "no sha256 tool available — skipping verification"
    return 0
  fi
  if [ "$expected" != "$actual" ]; then
    die "checksum mismatch: expected $expected got $actual"
  fi
  ok "checksum verified"
}

install_binary() {
  local src="$1" dst="$2"
  local dst_dir
  dst_dir="$(dirname "$dst")"
  if [ ! -d "$dst_dir" ]; then
    mkdir -p "$dst_dir" || die "cannot create ${dst_dir}"
  fi
  if [ ! -w "$dst_dir" ]; then
    die "cannot write to ${dst_dir} — re-run with sudo or set GOSSIP_INSTALL_DIR"
  fi
  install -m 0755 "$src" "$dst"
}

main() {
  banner
  require_cmd uname
  require_cmd tar

  local os arch version archive_name archive_url sums_url tmp
  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version "$VERSION")"

  info "platform    : ${C_BOLD}${os}/${arch}${C_RESET}"
  info "version     : ${C_BOLD}${version}${C_RESET}"
  info "install dir : ${C_BOLD}${INSTALL_DIR}${C_RESET}"

  if [ "$os" = "windows" ]; then
    archive_name="gossip_${version#v}_${os}_${arch}.zip"
  else
    archive_name="gossip_${version#v}_${os}_${arch}.tar.gz"
  fi
  archive_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
  sums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  download "$archive_url" "$tmp/${archive_name}"
  if curl -fsSL -o "$tmp/checksums.txt" "$sums_url" 2>/dev/null; then
    verify_checksum "$tmp/${archive_name}" "$tmp/checksums.txt"
  else
    warn "checksums.txt not published — skipping verification"
  fi

  info "extracting"
  if [ "$os" = "windows" ]; then
    require_cmd unzip
    unzip -q "$tmp/${archive_name}" -d "$tmp/extract"
  else
    mkdir -p "$tmp/extract"
    tar -xzf "$tmp/${archive_name}" -C "$tmp/extract"
  fi

  local extracted_bin
  extracted_bin="$(find "$tmp/extract" -type f -name "gossip*" ! -name "*.md" ! -name "*.txt" | head -n 1)"
  [ -n "$extracted_bin" ] || die "archive layout unexpected: could not find gossip binary in $tmp/extract"

  local dst="${INSTALL_DIR}/${BINARY_NAME}"
  [ "$os" = "windows" ] && dst="${INSTALL_DIR}/${BINARY_NAME}.exe"

  install_binary "$extracted_bin" "$dst"
  ok "installed ${C_BOLD}${dst}${C_RESET}"

  if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    local installed_version
    installed_version="$("$BINARY_NAME" version 2>/dev/null || echo "unknown")"
    ok "gossip version: ${C_BOLD}${installed_version}${C_RESET}"
  else
    warn "${INSTALL_DIR} is not on your PATH — add it to use 'gossip' as a command"
  fi

  printf "\n%sNext steps:%s\n" "${C_BOLD}" "${C_RESET}"
  printf "  1. %sgossip init%s        # scaffold .gossip/ in your project\n" "${C_CYAN}" "${C_RESET}"
  printf "  2. %sgossip codex%s       # start the Codex TUI via the bridge\n" "${C_CYAN}" "${C_RESET}"
  printf "  3. Claude Code will invoke %sgossip claude%s automatically via the plugin.\n" "${C_CYAN}" "${C_RESET}"
  printf "\n"
}

main "$@"
