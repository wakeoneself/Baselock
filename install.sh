#!/usr/bin/env bash
set -euo pipefail

# Installs the `sec` binary to /usr/local/bin and tells you what to run next.
# Usage:
#   sudo ./install.sh
#   curl -sSL https://raw.githubusercontent.com/abyss/server-sec-cli/main/install.sh | sudo bash

REPO="${SECLI_REPO:-https://github.com/abyss/server-sec-cli}"
BIN_DIR="${SECLI_BIN:-/usr/local/bin}"
NAME="sec"

red() { printf '\033[31m%s\033[0m\n' "$*"; }
cyan() { printf '\033[36m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    red "install.sh must run as root (sudo ./install.sh)"
    exit 1
  fi
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

install_from_repo() {
  local here
  here="$(cd "$(dirname "$0")" && pwd)"
  if [[ -f "${here}/go.mod" && -d "${here}/cmd/sec" ]]; then
    if command -v go >/dev/null 2>&1; then
      cyan "Building sec from this repository…"
      (cd "$here" && go build -ldflags "-s -w -X github.com/abyss/server-sec-cli/internal/cli.Version=0.1.0" -o "${BIN_DIR}/${NAME}" ./cmd/sec)
      chmod 0755 "${BIN_DIR}/${NAME}"
      return 0
    fi
  fi
  return 1
}

install_from_release() {
  local asset url tmp
  asset="$(detect_asset)"
  tmp="$(mktemp)"
  url="${REPO}/releases/latest/download/${asset}"
  cyan "Downloading ${url}…"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmp" || return 1
  else
    wget -qO "$tmp" "$url" || return 1
  fi
  install -m 0755 "$tmp" "${BIN_DIR}/${NAME}"
  rm -f "$tmp"
}

need_root

if install_from_repo; then
  green "Installed ${BIN_DIR}/${NAME}"
elif install_from_release; then
  green "Installed ${BIN_DIR}/${NAME}"
else
  red "Could not build or download sec."
  red "Clone ${REPO}, install Go, then run: sudo ./install.sh"
  exit 1
fi

echo
green "Next:"
echo "  sudo sec          # wizard — Enter accepts recommended defaults"
echo "  sudo sec --yes    # no questions"
echo
