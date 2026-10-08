#!/usr/bin/env bash
set -euo pipefail

# Installs Baselock (`sec`) to /usr/local/bin and tells you what to run next.
# Requires Go 1.22+ until a GitHub release binary exists.
# Usage:
#   sudo apt-get install -y golang-go git
#   sudo ./install.sh
#   curl -sSL https://raw.githubusercontent.com/wakeoneself/Baselock/main/install.sh | sudo bash

REPO="${SECLI_REPO:-https://github.com/wakeoneself/Baselock}"
BIN_DIR="${SECLI_BIN:-/usr/local/bin}"
NAME="sec"
GO_HINT="Go 1.22+ is required. On Ubuntu/Debian: sudo apt-get update && sudo apt-get install -y golang-go git"

red() { printf '\033[31m%s\033[0m\n' "$*"; }
cyan() { printf '\033[36m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    red "install.sh must run as root (sudo ./install.sh)"
    exit 1
  fi
}

need_go() {
  if command -v go >/dev/null 2>&1; then
    return 0
  fi
  red "Go is not installed."
  red "${GO_HINT}"
  exit 1
}

detect_asset() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)
      red "unsupported architecture: $arch"
      exit 1
      ;;
  esac
  echo "${NAME}-${os}-${arch}"
}

# Callers run inside `if`, where `set -e` is off — every failure must return explicitly.
build_in() {
  local dir="$1"
  cyan "Building sec with Go $(go version | awk '{print $3}')…"
  # sec is pure Go; CGO_ENABLED=0 avoids needing gcc and libc headers.
  if ! (cd "$dir" && CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/wakeoneself/Baselock/internal/cli.Version=0.1.0" -o "${BIN_DIR}/${NAME}" ./cmd/sec); then
    red "go build failed."
    return 1
  fi
  if [[ ! -x "${BIN_DIR}/${NAME}" ]]; then
    chmod 0755 "${BIN_DIR}/${NAME}" 2>/dev/null || { red "build produced no ${BIN_DIR}/${NAME}"; return 1; }
  fi
  "${BIN_DIR}/${NAME}" version >/dev/null 2>&1 || { red "${BIN_DIR}/${NAME} does not run"; return 1; }
}

install_from_repo() {
  local here=""
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  fi
  if [[ -n "$here" && -f "${here}/go.mod" && -d "${here}/cmd/sec" ]]; then
    need_go
    build_in "$here" || return 1
    return 0
  fi
  return 1
}

install_from_release() {
  local asset url tmp
  asset="$(detect_asset)"
  tmp="$(mktemp)"
  url="${REPO}/releases/latest/download/${asset}"
  cyan "Trying prebuilt binary ${url}…"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmp" || { rm -f "$tmp"; return 1; }
  else
    wget -qO "$tmp" "$url" || { rm -f "$tmp"; return 1; }
  fi
  install -m 0755 "$tmp" "${BIN_DIR}/${NAME}" || { rm -f "$tmp"; return 1; }
  rm -f "$tmp"
  "${BIN_DIR}/${NAME}" version >/dev/null 2>&1 || return 1
}

install_from_clone() {
  need_go
  if ! command -v git >/dev/null 2>&1; then
    red "git is not installed."
    red "On Ubuntu/Debian: sudo apt-get install -y git golang-go"
    exit 1
  fi
  local tmp
  tmp="$(mktemp -d)"
  cyan "Cloning ${REPO}…"
  if ! git clone --depth 1 "$REPO" "$tmp/Baselock"; then
    rm -rf "$tmp"
    return 1
  fi
  if ! build_in "$tmp/Baselock"; then
    rm -rf "$tmp"
    return 1
  fi
  rm -rf "$tmp"
}

need_root

if install_from_repo; then
  green "Installed ${BIN_DIR}/${NAME}"
elif install_from_release; then
  green "Installed ${BIN_DIR}/${NAME}"
elif install_from_clone; then
  green "Installed ${BIN_DIR}/${NAME}"
else
  red "Could not build or download sec."
  red "${GO_HINT}"
  red "Or clone ${REPO} and run: sudo ./install.sh"
  exit 1
fi

echo
green "Next:"
echo "  sudo sec          # wizard — Enter accepts recommended defaults"
echo "  sudo sec --yes    # no questions"
echo
