#!/usr/bin/env bash
# Push the GoReleaser-generated Homebrew formula after the GitHub release
# is public. goreleaser skip_upload stays true so a failed attestation
# cannot leave the tap pointing at a draft.

set -euo pipefail

FORMULA_NAME="${AIBRIS_BREW_FORMULA:-aibris}"
FORMULA_RELPATH="Formula/${FORMULA_NAME}.rb"
TAP_CLONE_URL="${AIBRIS_TAP_CLONE_URL:-git@github.com:sungjunlee/homebrew-tap.git}"

publish_usage() {
	printf 'usage: publish-homebrew-formula.sh [dist-dir]\n' >&2
}

publish_find_formula() {
	local dist=${1:-dist}
	local found="" path count=0
	if [ ! -d "$dist" ]; then
		printf 'missing dist directory: %s\n' "$dist" >&2
		return 1
	fi
	# GoReleaser writes the skipped-upload formula under dist/; do not
	# rebuild archives (checksums would drift from the attested files).
	# Portable (macOS /bin/bash 3.2 has no mapfile).
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		found=$path
		count=$((count + 1))
	done <<EOF
$(find "$dist" -type f -name "${FORMULA_NAME}.rb" | sort)
EOF
	if [ "$count" -ne 1 ]; then
		printf 'expected exactly one %s.rb under %s, found %s\n' \
			"$FORMULA_NAME" "$dist" "$count" >&2
		return 1
	fi
	printf '%s\n' "$found"
}

publish_require_token() {
	if [ -z "${HOMEBREW_TAP_TOKEN:-}" ]; then
		printf 'HOMEBREW_TAP_TOKEN must be the tap deploy-key private key so a failed tap commit fails the release.\n' >&2
		return 1
	fi
}

publish_write_key() {
	local dest=$1
	printf '%s\n' "$HOMEBREW_TAP_TOKEN" >"$dest"
	chmod 600 "$dest"
}

publish_commit_formula() {
	local tap_dir=$1
	local formula_src=$2
	local dest="${tap_dir}/${FORMULA_RELPATH}"
	mkdir -p "$(dirname "$dest")"
	cp "$formula_src" "$dest"
	git -C "$tap_dir" add "$FORMULA_RELPATH"
	if git -C "$tap_dir" diff --cached --quiet; then
		printf 'homebrew formula already up to date\n'
		return 0
	fi
	git -C "$tap_dir" \
		-c user.name="aibris-release" \
		-c user.email="aibris-release@users.noreply.github.com" \
		commit -m "Brew formula update for ${FORMULA_NAME} version ${GITHUB_REF_NAME:-unknown}"
}

publish_clone_tap() {
	local dest=$1
	local key=$2
	GIT_SSH_COMMAND="ssh -i ${key} -o StrictHostKeyChecking=accept-new -F /dev/null" \
		git clone "$TAP_CLONE_URL" "$dest"
}

publish_push_tap() {
	local tap_dir=$1
	local key=$2
	GIT_SSH_COMMAND="ssh -i ${key} -o StrictHostKeyChecking=accept-new -F /dev/null" \
		git -C "$tap_dir" push origin HEAD
}

publish_main() {
	local dist=${1:-dist}
	local formula key tap
	publish_require_token
	formula=$(publish_find_formula "$dist")
	key=$(mktemp)
	tap=$(mktemp -d)
	trap 'rm -f "$key"; rm -rf "$tap"' EXIT
	publish_write_key "$key"
	publish_clone_tap "$tap" "$key"
	publish_commit_formula "$tap" "$formula"
	publish_push_tap "$tap" "$key"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
		publish_usage
		exit 0
	fi
	publish_main "${1:-dist}"
fi
