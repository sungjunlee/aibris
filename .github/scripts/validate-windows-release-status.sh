#!/bin/sh

set -eu

release_notes=$1

if [ ! -f "$release_notes" ]; then
	echo "Missing curated release notes: $release_notes" >&2
	exit 1
fi

if ! awk '
	function strip_html_comments(text, output, start, finish) {
		output = ""
		while (1) {
			if (in_html_comment) {
				finish = index(text, "-->")
				if (!finish) {
					return output
				}
				text = substr(text, finish + 3)
				in_html_comment = 0
				continue
			}

			start = index(text, "<!--")
			if (!start) {
				return output text
			}
			output = output substr(text, 1, start - 1)
			text = substr(text, start + 4)
			in_html_comment = 1
		}
	}

	function fence_run(text, indent, marker, run) {
		for (indent = 0; indent < 3 && substr(text, 1, 1) == " "; indent++) {
			text = substr(text, 2)
		}
		marker = substr(text, 1, 1)
		if (marker != "`" && marker != "~") {
			return ""
		}
		while (substr(text, length(run) + 1, 1) == marker) {
			run = run marker
		}
		return length(run) >= 3 ? run : ""
	}

	function closes_fence(text, run, rest) {
		run = fence_run(text)
		if (substr(run, 1, 1) != fence_marker || length(run) < fence_length) {
			return 0
		}
		sub(/^ {0,3}/, "", text)
		rest = substr(text, length(run) + 1)
		return rest !~ /[^[:space:]]/
	}

	{
		if (in_fence) {
			if (closes_fence($0)) {
				in_fence = 0
			}
			next
		}

		line = strip_html_comments($0)
		run = fence_run(line)
		if (run != "") {
			in_fence = 1
			fence_marker = substr(run, 1, 1)
			fence_length = length(run)
			next
		}

		if (line !~ /^ {0,3}#{1,2}([[:space:]]|$)/) {
			if (in_windows_status && line ~ /[^[:space:]]/) {
				has_content = 1
			}
			next
		}
		if (in_windows_status) {
			exit has_content ? 0 : 1
		}
		if (line == "## Windows status") {
			in_windows_status = 1
		}
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
