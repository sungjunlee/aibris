#!/usr/bin/env bash
# Pour sungjunlee/tap/aibris after a v* release and assert the binary version.

set -euo pipefail

TAP_NAME="${AIBRIS_BREW_TAP:-sungjunlee/tap}"
FORMULA_NAME="${AIBRIS_BREW_FORMULA:-aibris}"
FORMULA_PATH="Formula/${FORMULA_NAME}.rb"
TAP_REPO="${AIBRIS_TAP_REPO:-https://github.com/sungjunlee/homebrew-tap.git}"

pour_usage() {
	printf 'usage: pour-homebrew-formula.sh vX.Y.Z\n' >&2
	return 2
}

pour_resolve_tag() {
	if [ -n "${1:-}" ]; then
		printf '%s\n' "$1"
		return 0
	fi
	if [ -n "${GITHUB_REF_NAME:-}" ]; then
		printf '%s\n' "$GITHUB_REF_NAME"
		return 0
	fi
	return 1
}

pour_tag_version() {
	printf '%s\n' "${1#v}"
}

pour_expected_version_line() {
	printf 'aibris version %s\n' "$(pour_tag_version "$1")"
}

pour_assert_version() {
	local got=$1
	local want=$2
	case "$got" in
	*dev* | *-next*)
		printf 'refusing snapshot version: %s\n' "$got" >&2
		return 1
		;;
	esac
	if [ "$got" != "$want" ]; then
		printf 'version mismatch: got %s want %s\n' "$got" "$want" >&2
		return 1
	fi
}

pour_should_upgrade() {
	[ "${1:-0}" -ge 2 ]
}

pour_formula_revisions() {
	local repo=$1
	if [ ! -f "${repo}/${FORMULA_PATH}" ]; then
		printf '0\n'
		return 0
	fi
	git -C "$repo" rev-list --count HEAD -- "$FORMULA_PATH"
}

pour_previous_formula_commit() {
	git -C "$1" log -2 --format=%H -- "$FORMULA_PATH" | tail -n 1
}

pour_clone_tap() {
	local dest=$1
	git clone --depth=50 "$TAP_REPO" "$dest"
}

pour_read_version() {
	aibris --version
}

pour_install_latest() {
	brew install "${TAP_NAME}/${FORMULA_NAME}"
}

pour_write_formula() {
	local dest=$1
	cat >"${dest}/${FORMULA_NAME}.rb"
}

pour_install_previous_then_upgrade() {
	local tap_dir=$1
	local prev old_dir new_dir
	prev="$(pour_previous_formula_commit "$tap_dir")"
	old_dir="$(mktemp -d)"
	new_dir="$(mktemp -d)"
	git -C "$tap_dir" show "${prev}:${FORMULA_PATH}" |
		pour_write_formula "$old_dir"
	pour_write_formula "$new_dir" <"${tap_dir}/${FORMULA_PATH}"
	brew install --formula "${old_dir}/${FORMULA_NAME}.rb"
	brew upgrade --formula "${new_dir}/${FORMULA_NAME}.rb"
}

pour_main() {
	local tag want tap_dir
	tag="$(pour_resolve_tag "${1:-}")" || {
		pour_usage
		return 2
	}
	want="$(pour_expected_version_line "$tag")"
	tap_dir="$(mktemp -d)"
	pour_clone_tap "$tap_dir"
	if pour_should_upgrade "$(pour_formula_revisions "$tap_dir")"; then
		pour_install_previous_then_upgrade "$tap_dir"
	else
		pour_install_latest
	fi
	pour_assert_version "$(pour_read_version)" "$want"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	pour_main "$@"
fi
