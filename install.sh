#!/usr/bin/env bash
# aibris installer for manual installs.
set -euo pipefail

REPO="sungjunlee/aibris"
BINARY="aibris"
INSTALL_DIR="${AIBRIS_INSTALL_DIR:-}"
INSTALL_DIR_EXPLICIT=0
VERSION=""
TMP_ROOT=""

if [[ -n "${AIBRIS_INSTALL_DIR:-}" ]]; then
  INSTALL_DIR_EXPLICIT=1
fi

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
  local default_install_dir
  default_install_dir="$(default_install_dir_label)"
  cat <<EOF
Install aibris.

Usage:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/refs/heads/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/${REPO}/refs/heads/main/install.sh | bash -s -- main
  curl -fsSL https://raw.githubusercontent.com/${REPO}/refs/heads/main/install.sh | bash -s -- 0.6.1

Options:
  --prefix DIR   Install into DIR (default: ${default_install_dir})
  -h, --help     Show this help

Arguments:
  no argument    Install latest GitHub Release binary
  latest         Install latest GitHub Release binary
  main           Build and install current main branch with Go
  X.Y.Z/vX.Y.Z   Install that GitHub Release binary
EOF
}

default_install_dir_label() {
  if [[ -n "${HOME:-}" ]]; then
    printf '%s\n' "${HOME}/.local/bin"
    return
  fi
  printf '%s\n' "\$HOME/.local/bin"
}

default_install_dir() {
  if [[ -z "${HOME:-}" ]]; then
    err "HOME is not set; pass --prefix DIR or set AIBRIS_INSTALL_DIR."
    exit 1
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --prefix)
        [[ $# -ge 2 ]] || { err "missing value for --prefix"; exit 1; }
        INSTALL_DIR="$2"
        INSTALL_DIR_EXPLICIT=1
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

expand_path() {
  case "$1" in
    \~)
      [[ -n "${HOME:-}" ]] || { err "HOME is not set; cannot expand ~"; exit 1; }
      printf '%s\n' "$HOME"
      ;;
    \~/*)
      [[ -n "${HOME:-}" ]] || { err "HOME is not set; cannot expand ~"; exit 1; }
      printf '%s/%s\n' "$HOME" "${1#\~/}"
      ;;
    *)
      printf '%s\n' "$1"
      ;;
  esac
}

need() {
  command -v "$1" >/dev/null 2>&1 || { err "required command not found: $1"; exit 1; }
}

run_install_command() {
  if [[ -w "$INSTALL_DIR" ]]; then
    "$@"
    return
  fi
  if [[ "$INSTALL_DIR_EXPLICIT" -eq 1 ]]; then
    need sudo
    log "Using sudo to install into ${INSTALL_DIR}"
    sudo "$@"
    return
  fi

  err "${INSTALL_DIR} is not writable"
  err "Choose a writable directory with --prefix DIR, or use --prefix /usr/local/bin for a system install."
  exit 1
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

archive_candidates() {
  local tag="$1" os="$2" arch="$3"
  printf '%s\n' "${BINARY}_${os}_${arch}.tar.gz"
  printf '%s\n' "${BINARY}_${tag}_${os}_${arch}.tar.gz"
  printf '%s\n' "${BINARY}_${tag#v}_${os}_${arch}.tar.gz"
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
  log "Installing ${BINARY} to ${INSTALL_DIR}"
  mkdir -p "$INSTALL_DIR" 2>/dev/null || run_install_command mkdir -p "$INSTALL_DIR"
  run_install_command install -m 0755 "$source" "${INSTALL_DIR}/${BINARY}"
  log "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"
}

path_contains_install_dir() {
  case ":${PATH:-}:" in
    *":${INSTALL_DIR}:"*) return 0 ;;
    *) return 1 ;;
  esac
}

display_path() {
  local home
  home="${HOME:-}"
  if [[ -z "$home" ]]; then
    printf '%s\n' "$1"
    return
  fi
  case "$1" in
    "$home") printf '%s\n' '~' ;;
    "$home"/*) printf '%s/%s\n' '~' "${1#"$home"/}" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

shell_path_value() {
  local home
  home="${HOME:-}"
  if [[ -z "$home" ]]; then
    printf '%s\n' "$1"
    return
  fi
  case "$1" in
    "$home") printf '%s\n' "\$HOME" ;;
    "$home"/*) printf '%s/%s\n' "\$HOME" "${1#"$home"/}" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

shell_path_hint() {
  local shell_name profile displayed_profile path_value
  shell_name="$(basename "${SHELL:-}")"
  path_value="$(shell_path_value "$INSTALL_DIR")"

  if [[ -z "${HOME:-}" ]]; then
    cat <<EOF
Add it for future shells:
  Add ${path_value} to PATH in your shell startup file.

Use it in this shell now:
  export PATH="${path_value}:\$PATH"
EOF
    return
  fi

  case "$shell_name" in
    zsh)
      profile="${HOME}/.zshrc"
      ;;
    bash)
      profile="${HOME}/.bashrc"
      ;;
    fish)
      profile="${HOME}/.config/fish/config.fish"
      ;;
    *)
      profile="${HOME}/.profile"
      ;;
  esac
  displayed_profile="$(display_path "$profile")"

  if [[ "$shell_name" == "fish" ]]; then
    cat <<EOF
Add it for future fish shells:
  mkdir -p $(display_path "$(dirname "$profile")")
  echo 'fish_add_path "${path_value}"' >> ${displayed_profile}

Use it in this shell now:
  set -gx PATH "${path_value}" \$PATH
EOF
    return
  fi

  cat <<EOF
Add it for future shells:
  echo 'export PATH="${path_value}:\$PATH"' >> ${displayed_profile}

Use it in this shell now:
  export PATH="${path_value}:\$PATH"
EOF
}

print_path_hint() {
  if path_contains_install_dir; then
    return
  fi

  cat <<EOF

${BINARY} was installed to $(display_path "$INSTALL_DIR"), but that directory is not on your PATH yet.

$(shell_path_hint)
EOF
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

  while IFS= read -r asset; do
    archive="${tmp}/${asset}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
    if curl -fsSL -o "$archive" "$url" 2>/dev/null; then
      log "Downloaded ${asset}"
      break
    fi
    rm -f "$archive"
  done < <(archive_candidates "$tag" "$os" "$arch")

  [[ -f "${archive:-}" ]] || { err "release archive not found for ${tag} ${os}/${arch}"; exit 1; }

  expected="$(awk -v asset="$asset" '$2 == asset { print $1; found = 1; exit } END { exit found ? 0 : 1 }' "$checksums")" ||
    { err "checksum for ${asset} not found"; exit 1; }
  actual="$(sha256 "$archive")"
  [[ "$actual" == "$expected" ]] || { err "checksum mismatch for ${asset}"; exit 1; }

  mkdir -p "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir"
  binary_path="$(find "$extract_dir" -type f -name "$BINARY" -perm -111 -print -quit)"
  [[ -n "$binary_path" ]] || { err "${BINARY} not found in archive"; exit 1; }

  install_binary "$binary_path"
}

download_latest_release() {
  need curl
  need tar

  local os arch asset url checksums_url tmp archive checksums expected actual extract_dir binary_path tag
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="${BINARY}_${os}_${arch}.tar.gz"
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
  checksums_url="https://github.com/${REPO}/releases/latest/download/checksums.txt"

  tmp="$(mktemp -d)"
  TMP_ROOT="$tmp"
  checksums="${tmp}/checksums.txt"
  archive="${tmp}/${asset}"
  extract_dir="${tmp}/extract"

  if curl -fsSL -o "$checksums" "$checksums_url" 2>/dev/null &&
    curl -fsSL -o "$archive" "$url" 2>/dev/null; then
    log "Downloaded ${asset}"
    expected="$(awk -v asset="$asset" '$2 == asset { print $1; found = 1; exit } END { exit found ? 0 : 1 }' "$checksums")" ||
      { err "checksum for ${asset} not found"; exit 1; }
    actual="$(sha256 "$archive")"
    [[ "$actual" == "$expected" ]] || { err "checksum mismatch for ${asset}"; exit 1; }

    mkdir -p "$extract_dir"
    tar -xzf "$archive" -C "$extract_dir"
    binary_path="$(find "$extract_dir" -type f -name "$BINARY" -perm -111 -print -quit)"
    [[ -n "$binary_path" ]] || { err "${BINARY} not found in archive"; exit 1; }

    install_binary "$binary_path"
    return
  fi

  log "Latest release does not provide ${asset}; trying legacy release asset lookup..."
  rm -rf "$tmp"
  TMP_ROOT=""
  tag="$(latest_release_tag)" || { err "could not resolve latest GitHub Release"; exit 1; }
  [[ -n "$tag" ]] || { err "no GitHub Release found"; exit 1; }
  download_release "$tag"
}

main() {
  parse_args "$@"
  if [[ -z "$INSTALL_DIR" ]]; then
    INSTALL_DIR="$(default_install_dir)"
  fi
  INSTALL_DIR="$(expand_path "$INSTALL_DIR")"

  case "${VERSION:-}" in
    main)
      install_from_main
      ;;
    ""|latest)
      download_latest_release
      ;;
    *)
      download_release "$(normalize_tag "$VERSION")"
      ;;
  esac

  "${INSTALL_DIR}/${BINARY}" --version || true
  print_path_hint
}

if [[ "${#BASH_SOURCE[@]}" -eq 0 || "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
