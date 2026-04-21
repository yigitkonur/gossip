#!/usr/bin/env bash
#
# Gossip installer — downloads the right prebuilt binary for your OS/arch
# and installs it to /usr/local/bin/gossip.
#
#   curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash
#
# Or, to inspect before running:
#   curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh -o install.sh
#   less install.sh
#   sudo bash install.sh
#
# Flags:
#   --help          show usage and exit
#   --version V     install a specific version (e.g. --version v0.2.0)
#   --install-dir D override install dir (default: /usr/local/bin)
#   --uninstall     remove gossip from the install dir
#   --force         reinstall even if the target version is already present
#
# Environment overrides (flags win if both are set):
#   GOSSIP_VERSION=v0.2.0
#   GOSSIP_INSTALL_DIR=/usr/local/bin
#   GOSSIP_REPO=yigitkonur/gossip
#   NO_COLOR=1                        disable color output
#
# Re-running upgrades the binary in place. If the target version is already
# installed, the script exits cleanly without touching anything (pass --force
# to reinstall anyway).

set -eEuo pipefail

# ------------------------------------------------------------------- defaults
REPO="${GOSSIP_REPO:-yigitkonur/gossip}"
VERSION="${GOSSIP_VERSION:-latest}"
INSTALL_DIR="${GOSSIP_INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="gossip"
MODE="install"          # install | uninstall
FORCE=0

# ------------------------------------------------------------------- colors
if [ -n "${NO_COLOR:-}" ] || [ ! -t 1 ] || ! command -v tput >/dev/null 2>&1 \
   || [ "$(tput colors 2>/dev/null || echo 0)" -lt 8 ]; then
  C_RESET=""; C_BOLD=""; C_DIM=""
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_CYAN=""
else
  C_RESET="$(tput sgr0)"
  C_BOLD="$(tput bold)"
  C_DIM="$(tput dim 2>/dev/null || true)"
  C_RED="$(tput setaf 1)"
  C_GREEN="$(tput setaf 2)"
  C_YELLOW="$(tput setaf 3)"
  C_BLUE="$(tput setaf 4)"
  C_CYAN="$(tput setaf 6)"
fi

info() { printf "%s==>%s %s\n" "${C_BLUE}${C_BOLD}" "${C_RESET}" "$*"; }
ok()   { printf "%s✓%s   %s\n" "${C_GREEN}${C_BOLD}" "${C_RESET}" "$*"; }
warn() { printf "%s⚠%s   %s\n" "${C_YELLOW}${C_BOLD}" "${C_RESET}" "$*" >&2; }
die()  { printf "%s✗%s   %s\n" "${C_RED}${C_BOLD}" "${C_RESET}" "$*" >&2; exit 1; }
dim()  { printf "%s%s%s"      "${C_DIM}"             "$*" "${C_RESET}"; }

on_err() { local ec=$?; [ $ec -ne 0 ] && warn "install aborted (exit $ec)"; }
trap on_err ERR

# ------------------------------------------------------------------- banner
banner() {
  printf "%s" "${C_CYAN}"
  cat <<'BANNER'
   ____  ___   ____ ____ ___ ____
  / ___|/ _ \ / ___/ ___|_ _|  _ \
 | |  _| | | |\___ \___ \| || |_) |
 | |_| | |_| | ___) |__) | ||  __/
  \____|\___/ |____/____/___|_|
BANNER
  printf "%s\n" "${C_RESET}"
  printf "  %sbridge between Claude Code and Codex CLI%s\n\n" "${C_DIM}" "${C_RESET}"
}

usage() {
  cat <<EOF
Usage: install.sh [--version V] [--install-dir D] [--uninstall] [--force]

Downloads the gossip binary for your OS/arch and installs it.

Flags:
  --version V        install a specific version (default: latest)
  --install-dir D    override install dir (default: ${INSTALL_DIR})
  --uninstall        remove gossip from the install dir
  --force            reinstall even if the target version is already installed
  -h, --help         show this message

Environment: GOSSIP_VERSION, GOSSIP_INSTALL_DIR, GOSSIP_REPO, NO_COLOR.

Docs: https://github.com/${REPO}
EOF
}

# ------------------------------------------------------------------- argv
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)         usage; exit 0 ;;
    --version)         shift; [ $# -gt 0 ] || die "--version requires an argument"; VERSION="$1" ;;
    --version=*)       VERSION="${1#*=}" ;;
    --install-dir)     shift; [ $# -gt 0 ] || die "--install-dir requires an argument"; INSTALL_DIR="$1" ;;
    --install-dir=*)   INSTALL_DIR="${1#*=}" ;;
    --uninstall)       MODE="uninstall" ;;
    --force)           FORCE=1 ;;
    --)                shift; break ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
  shift
done

# ------------------------------------------------------------------- helpers
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *) die "unsupported OS: $(uname -s) (gossip supports macOS and Linux)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

resolve_version() {
  local v="$1"
  if [ "$v" != "latest" ]; then
    case "$v" in v*) printf "%s" "$v" ;; *) printf "v%s" "$v" ;; esac
    return 0
  fi
  local tag
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
         | grep -E '"tag_name":\s*"[^"]+"' \
         | head -n 1 \
         | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/')"
  [ -n "$tag" ] || die "could not resolve latest release from GitHub API — set --version or GOSSIP_VERSION"
  printf "%s" "$tag"
}

download() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL --retry 3 --retry-delay 2 --progress-bar -o "$dest" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --show-progress --tries=3 -O "$dest" "$url"
  else
    die "need either curl or wget"
  fi
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1;   then shasum -a 256 "$1" | awk '{print $1}'
  else return 1; fi
}

verify_checksum() {
  local archive="$1" sums="$2"
  local expected actual
  expected="$(grep -F "$(basename "$archive")" "$sums" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    warn "no checksum listed for $(basename "$archive") — skipping verification"
    return 0
  fi
  if ! actual="$(sha256_of "$archive")"; then
    warn "no sha256 tool available (sha256sum/shasum) — skipping verification"
    return 0
  fi
  [ "$expected" = "$actual" ] || die "checksum mismatch: expected $expected got $actual"
  ok "checksum verified ($(dim "${expected:0:12}…"))"
}

installed_version() {
  command -v "$BINARY_NAME" >/dev/null 2>&1 || return 1
  "$BINARY_NAME" version 2>/dev/null | awk '{print $NF; exit}'
}

can_write() { [ -w "$1" ] || [ ! -e "$1" ] && [ -w "$(dirname "$1")" ] 2>/dev/null; }

install_binary() {
  local src="$1" dst="$2"
  local dst_dir
  dst_dir="$(dirname "$dst")"
  [ -d "$dst_dir" ] || mkdir -p "$dst_dir" || die "cannot create ${dst_dir}"
  if ! can_write "$dst_dir"; then
    die "cannot write to ${dst_dir} — re-run with sudo, or pass --install-dir \$HOME/.local/bin"
  fi
  install -m 0755 "$src" "$dst"
}

uninstall() {
  local dst="${INSTALL_DIR}/${BINARY_NAME}"
  if [ ! -e "$dst" ]; then
    warn "nothing to remove: ${dst} does not exist"
    exit 0
  fi
  if ! can_write "$(dirname "$dst")"; then
    die "cannot remove ${dst} — re-run with sudo"
  fi
  rm -f "$dst"
  ok "removed ${C_BOLD}${dst}${C_RESET}"
}

# ------------------------------------------------------------------- main
banner

if [ "$MODE" = "uninstall" ]; then
  uninstall
  exit 0
fi

require_cmd uname
require_cmd tar
require_cmd curl || require_cmd wget

os="$(detect_os)"
arch="$(detect_arch)"
version="$(resolve_version "$VERSION")"
dst="${INSTALL_DIR}/${BINARY_NAME}"

info "platform    : ${C_BOLD}${os}/${arch}${C_RESET}"
info "version     : ${C_BOLD}${version}${C_RESET}"
info "install dir : ${C_BOLD}${INSTALL_DIR}${C_RESET}"

# Idempotency: skip if already installed at this exact version.
if [ "$FORCE" -ne 1 ] && [ -x "$dst" ]; then
  current="$("$dst" version 2>/dev/null | awk '{print $NF; exit}' || true)"
  if [ -n "$current" ] && [ "$current" = "$version" ]; then
    ok "gossip ${version} already installed at ${dst} — nothing to do (use --force to reinstall)"
    exit 0
  fi
fi

archive_name="gossip_${version#v}_${os}_${arch}.tar.gz"
archive_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
sums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t gossip)"
trap 'rm -rf "$tmp"' EXIT

info "downloading $(dim "${archive_url}")"
download "$archive_url" "$tmp/${archive_name}"

if download "$sums_url" "$tmp/checksums.txt" >/dev/null 2>&1; then
  verify_checksum "$tmp/${archive_name}" "$tmp/checksums.txt"
else
  warn "checksums.txt not published for ${version} — skipping verification"
fi

info "extracting"
mkdir -p "$tmp/extract"
tar -xzf "$tmp/${archive_name}" -C "$tmp/extract"

extracted_bin="$(find "$tmp/extract" -type f -name "${BINARY_NAME}" -perm -u+x | head -n 1)"
[ -n "$extracted_bin" ] || die "archive layout unexpected: no ${BINARY_NAME} executable in ${archive_name}"

install_binary "$extracted_bin" "$dst"
ok "installed ${C_BOLD}${dst}${C_RESET}"

if command -v "$BINARY_NAME" >/dev/null 2>&1; then
  ok "gossip version: ${C_BOLD}$(installed_version || echo unknown)${C_RESET}"
else
  warn "${INSTALL_DIR} is not on your PATH — add it to use 'gossip' as a command"
  warn "  e.g. echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc"
fi

printf "\n%sNext steps:%s\n" "${C_BOLD}" "${C_RESET}"
printf "  1. %sgossip init%s        scaffold .gossip/ in your project\n"            "${C_CYAN}" "${C_RESET}"
printf "  2. %sgossip codex%s       start the Codex TUI via the bridge\n"            "${C_CYAN}" "${C_RESET}"
printf "  3. Claude Code will invoke %sgossip claude%s automatically via the plugin.\n" "${C_CYAN}" "${C_RESET}"
printf "\n%sDocs:%s https://github.com/%s\n\n" "${C_BOLD}" "${C_RESET}" "${REPO}"
