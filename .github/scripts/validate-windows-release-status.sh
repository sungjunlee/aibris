#!/bin/sh

set -eu

release_notes=$1

if [ ! -f "$release_notes" ]; then
	echo "Missing curated release notes: $release_notes" >&2
	exit 1
fi

if ! awk '
	/^#{1,2}([[:space:]]|$)/ {
		if (in_windows_status) {
			exit has_content ? 0 : 1
		}
		if ($0 == "## Windows status") {
			in_windows_status = 1
		}
		next
	}
	in_windows_status && /[^[:space:]]/ {
		has_content = 1
	}
	END {
		if (!in_windows_status || !has_content) {
			exit 1
		}
	}
' "$release_notes"; then
	echo "Curated release notes must contain a non-empty exact '## Windows status' section." >&2
	exit 1
fi
