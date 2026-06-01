#!/usr/bin/env bash
# aibris installer for manual installs.
set -euo pipefail

REPO="sungjunlee/aibris"
BINARY="aibris"
INSTALL_DIR="${AIBRIS_INSTALL_DIR:-/usr/local/bin}"
VERSION=""
TMP_ROOT=""

cleanup() {
  if [[ -n "${TMP_ROOT:-}" ]]; then
    rm -rf "$TMP_ROOT"
  fi
}
trap cleanup EXIT

log() {
  printf '%s\n' "$*"
}

err() {
  printf 'error: %s\n' "$*" >&2
}

usage() {
  cat <<EOF
Install aibris.

Usage:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- main
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- 0.3.0

Options:
  --prefix DIR   Install into DIR (default: ${INSTALL_DIR})
  -h, --help     Show this help

Arguments:
  no argument    Install latest GitHub Release binary, or main if no release exists
  main/latest    Build and install current main branch with Go
  X.Y.Z/vX.Y.Z   Install that GitHub Release binary
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --prefix)
        [[ $# -ge 2 ]] || { err "missing value for --prefix"; exit 1; }
        INSTALL_DIR="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      -*)
        err "unknown option: $1"
        exit 1
        ;;
      *)
        [[ -z "$VERSION" ]] || { err "unexpected argument: $1"; exit 1; }
        VERSION="$1"
        shift
        ;;
    esac
  done
}

need() {
  command -v "$1" >/dev/null 2>&1 || { err "required command not found: $1"; exit 1; }
}

maybe_sudo() {
  if [[ -w "$INSTALL_DIR" ]]; then
    "$@"
  else
    need sudo
    sudo "$@"
  fi
}

normalize_tag() {
  local tag="$1"
  if [[ "$tag" =~ ^[0-9] ]]; then
    tag="v${tag}"
  fi
  printf '%s\n' "$tag"
}

latest_release_tag() {
  need curl
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
    sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    *) err "unsupported OS: $(uname -s)"; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) err "unsupported architecture: $(uname -m)"; exit 1 ;;
  esac
}

sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1; exit}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1; exit}'
  else
    err "required command not found: shasum or sha256sum"
    exit 1
  fi
}

install_binary() {
  local source="$1"
  mkdir -p "$INSTALL_DIR" 2>/dev/null || maybe_sudo mkdir -p "$INSTALL_DIR"
  maybe_sudo install -m 0755 "$source" "${INSTALL_DIR}/${BINARY}"
  log "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"
}

install_from_main() {
  need go
  local tmp
  tmp="$(mktemp -d)"
  TMP_ROOT="$tmp"
  log "Building ${BINARY} from ${REPO}@main..."
  GOBIN="${tmp}/bin" go install "github.com/${REPO}@main"
  install_binary "${tmp}/bin/${BINARY}"
}

download_release() {
  need curl
  need tar

  local tag="$1"
  local os arch asset url checksums_url tmp archive checksums expected actual extract_dir binary_path
  os="$(detect_os)"
  arch="$(detect_arch)"
  checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"

  tmp="$(mktemp -d)"
  TMP_ROOT="$tmp"
  checksums="${tmp}/checksums.txt"
  extract_dir="${tmp}/extract"

  curl -fsSL -o "$checksums" "$checksums_url"

  for asset in \
    "${BINARY}_${tag}_${os}_${arch}.tar.gz" \
    "${BINARY}_${tag#v}_${os}_${arch}.tar.gz"; do
    archive="${tmp}/${asset}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
    if curl -fsSL -o "$archive" "$url" 2>/dev/null; then
      log "Downloaded ${asset}"
      break
    fi
    rm -f "$archive"
  done

  [[ -f "${archive:-}" ]] || { err "release archive not found for ${tag} ${os}/${arch}"; exit 1; }

  expected="$(awk -v asset="$asset" '$2 == asset { print $1; found = 1; exit } END { exit found ? 0 : 1 }' "$checksums")" ||
    { err "checksum for ${asset} not found"; exit 1; }
  actual="$(sha256 "$archive")"
  [[ "$actual" == "$expected" ]] || { err "checksum mismatch for ${asset}"; exit 1; }

  mkdir -p "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir"
  binary_path="$(find "$extract_dir" -type f -name "$BINARY" -perm -111 | head -n 1)"
  [[ -n "$binary_path" ]] || { err "${BINARY} not found in archive"; exit 1; }

  install_binary "$binary_path"
}

main() {
  parse_args "$@"

  case "${VERSION:-}" in
    main|latest)
      install_from_main
      ;;
    "")
      local tag
      tag="$(latest_release_tag || true)"
      if [[ -n "$tag" ]]; then
        download_release "$tag"
      else
        log "No GitHub Release found, installing main branch from source..."
        install_from_main
      fi
      ;;
    *)
      download_release "$(normalize_tag "$VERSION")"
      ;;
  esac

  "${INSTALL_DIR}/${BINARY}" --version || true
}

main "$@"
