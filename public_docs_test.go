package main

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func TestPublicDocumentationLocalLinks(t *testing.T) {
	files := []string{
		"README.md",
		filepath.Join("docs", "WINDOWS.md"),
		"SECURITY.md",
		"SECURITY_AUDIT.md",
		"CONTRIBUTING.md",
		"CODE_OF_CONDUCT.md",
		"ROADMAP.md",
	}
	templates, err := filepath.Glob(filepath.Join(".github", "ISSUE_TEMPLATE", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, templates...)

	for _, document := range files {
		document := document
		t.Run(filepath.ToSlash(document), func(t *testing.T) {
			data, err := os.ReadFile(document)
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(data), -1) {
				target := strings.TrimSpace(match[1])
				if isExternalDocumentationLink(target) {
					continue
				}
				target = strings.SplitN(target, "#", 2)[0]
				target = strings.SplitN(target, "?", 2)[0]
				decoded, err := url.PathUnescape(target)
				if err != nil {
					t.Errorf("invalid link %q: %v", match[1], err)
					continue
				}
				if decoded == "" || filepath.IsAbs(decoded) {
					t.Errorf("invalid repository-local link %q", match[1])
					continue
				}
				resolved := filepath.Clean(filepath.Join(filepath.Dir(document), filepath.FromSlash(decoded)))
				if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
					t.Errorf("repository-local link escapes the repository: %q", match[1])
					continue
				}
				if _, err := os.Stat(resolved); err != nil {
					t.Errorf("broken repository-local link %q resolves to %q: %v", match[1], resolved, err)
				}
			}
		})
	}
}

func TestScanJSONSchemaVersioningDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("docs", "JSON_SCHEMA.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"schema_version",
		"`items` is the canonical all-debris array",
		"0.x compatibility\n  alias",
		"mirrors `items` exactly",
		"[0.x compatibility and deprecation policy](COMPATIBILITY.md)",
		"retained throughout 0.x",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("docs/JSON_SCHEMA.md must document %q", want)
		}
	}
	// The canonical items array and the 0.x worktrees alias must both appear in
	// the top-level structure fixture with identical item content.
	if !strings.Contains(content, `"schema_version": 1,`+"\n"+`  "items": [`) {
		t.Errorf("docs/JSON_SCHEMA.md top-level fixture must lead with schema_version then items")
	}
	if !strings.Contains(content, `"worktrees": [`) {
		t.Errorf("docs/JSON_SCHEMA.md must retain the worktrees compatibility alias in the fixture")
	}
}

func TestCompatibilityPolicyDocumentsStable0xContracts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("docs", "COMPATIBILITY.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"Stable documented surfaces",
		"Flag names, documented short aliases, accepted selector values, defaults",
		"`scan --json`, `clean --dry-run --json` (`clean_plan`), and execution",
		"Process exit status",
		"`CHANGELOG.md` entry",
		"Upgrade and migration",
		"new schema version",
		"retained throughout 0.x",
		"two subsequent 0.x feature/minor releases and 90\ncalendar days, whichever is longer",
		"does not promise a v1.0 scope",
		"release schedule",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("compatibility policy must document %q", want)
		}
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "[0.x compatibility and deprecation policy](docs/COMPATIBILITY.md)") {
		t.Error("README must link to the canonical compatibility policy")
	}

	spec, err := os.ReadFile(filepath.Join("docs", "SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "[COMPATIBILITY.md](COMPATIBILITY.md)") {
		t.Error("SPEC must link to the canonical compatibility policy")
	}
}

func TestReleaseNotesTemplatePromptsCompatibilityImpact(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".github", "release-notes", "TEMPLATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"## Compatibility impact",
		"No compatibility change",
		"Compatible addition",
		"Breaking change",
		"Deprecation",
		"replacement and the earliest removal version",
		"## Upgrade and migration",
		"## Windows status",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("release-notes template must prompt %q", want)
		}
	}
}

func TestWindowsReleaseDocumentationContract(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	readme := read("README.md")
	if !strings.Contains(readme, "[Windows support contract](docs/WINDOWS.md)") {
		t.Error("README must link to the canonical Windows support contract")
	}

	windows := read(filepath.Join("docs", "WINDOWS.md"))
	for _, required := range []string{
		"Windows archives are **experimental**",
		"`windows-latest`",
		"`aibris_windows_amd64.zip`",
		"`aibris_windows_arm64.zip`",
		"`install.sh`",
	} {
		if !strings.Contains(windows, required) {
			t.Errorf("Windows support contract is missing %q", required)
		}
	}

	goreleaser := read(".goreleaser.yaml")
	for _, required := range []string{
		"- windows",
		"- amd64",
		"- arm64",
		"goos: windows",
		"formats: [zip]",
		"name_template: 'checksums.txt'",
	} {
		if !strings.Contains(goreleaser, required) {
			t.Errorf("GoReleaser no longer satisfies the documented Windows contract: missing %q", required)
		}
	}

	releaseWorkflow := read(filepath.Join(".github", "workflows", "release.yml"))
	for _, required := range []string{
		`RELEASE_NOTES: .github/release-notes/${{ github.ref_name }}.md`,
		`.github/scripts/validate-windows-release-status.sh "$RELEASE_NOTES"`,
	} {
		if !strings.Contains(releaseWorkflow, required) {
			t.Errorf("release workflow is missing the Windows-status publication gate %q", required)
		}
	}
}

func TestHomebrewInstallContract(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	for _, required := range []string{
		"brew install sungjunlee/tap/aibris",
		"third-party",
		"https://github.com/sungjunlee/homebrew-tap",
		"item trust",
		"not the whole tap",
		"checksums.txt",
		"TOFU",
		"~/.local/bin",
		"install.sh",
		"--prefix /usr/local/bin",
		"PATH",
		"brew upgrade",
		"$HOME",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README Homebrew install contract is missing %q", required)
		}
	}
	if strings.Contains(readme, "system-wide") {
		t.Error("README must not call --prefix /usr/local/bin \"system-wide\"")
	}
	if strings.Contains(readme, "brew tap sungjunlee/tap") {
		t.Error("README must not document brew tap + short name as the install path")
	}
}

func TestHomebrewReleaseContract(t *testing.T) {
	goreleaser := readRepoFile(t, ".goreleaser.yaml")
	if strings.Contains(goreleaser, "homebrew_casks:") {
		t.Error("GoReleaser must publish a Formula via brews, not a cask block")
	}
	for _, required := range []string{
		"brews:",
		"name: homebrew-tap",
		`private_key: "{{ .Env.HOMEBREW_TAP_TOKEN }}"`,
		"https://github.com/sungjunlee/aibris/releases/download/{{ .Tag }}/aibris_{{ .Os }}_{{ .Arch }}.tar.gz",
		`bin.install "aibris"`,
		"skip_upload:",
		".IsSnapshot",
	} {
		if !strings.Contains(goreleaser, required) {
			t.Errorf("GoReleaser Homebrew contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"latest/download",
		"homebrew_casks:",
		"bottles:",
	} {
		if strings.Contains(goreleaser, forbidden) {
			t.Errorf("GoReleaser Homebrew contract must not contain %q", forbidden)
		}
	}
}

func TestHomebrewSecurityAuditContract(t *testing.T) {
	audit := readRepoFile(t, "SECURITY_AUDIT.md")
	for _, required := range []string{
		"sungjunlee/tap",
		"https://github.com/sungjunlee/homebrew-tap",
		"item-trust",
		"TOFU",
		"checksums.txt",
	} {
		if !strings.Contains(audit, required) {
			t.Errorf("SECURITY_AUDIT.md Homebrew contract is missing %q", required)
		}
	}
	if strings.Contains(audit, "Homebrew installation is documented as pending") {
		t.Error("SECURITY_AUDIT.md must not still describe Homebrew as pending")
	}
	if strings.Contains(audit, "Homebrew verifies") {
		t.Error("SECURITY_AUDIT.md must not claim Homebrew verifies the binary")
	}
}

func TestHomebrewPourWorkflowContract(t *testing.T) {
	releaseWorkflow := readRepoFile(t, filepath.Join(".github", "workflows", "release.yml"))
	for _, required := range []string{
		"HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}",
		"macos-latest",
		"pour-homebrew-formula.sh",
		"needs: goreleaser",
	} {
		if !strings.Contains(releaseWorkflow, required) {
			t.Errorf("release workflow Homebrew pour contract is missing %q", required)
		}
	}
	if strings.Contains(releaseWorkflow, "GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}") {
		t.Error("release workflow must not use HOMEBREW_TAP_TOKEN as GITHUB_TOKEN")
	}
	if !strings.Contains(releaseWorkflow, "HOMEBREW_TAP_TOKEN must be set") {
		t.Error("release workflow must fail closed when HOMEBREW_TAP_TOKEN is missing")
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWindowsReleaseStatusGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release gate runs in the Ubuntu release job")
	}

	script := filepath.Join(".github", "scripts", "validate-windows-release-status.sh")
	tests := []struct {
		name    string
		notes   string
		wantErr bool
	}{
		{
			name:    "missing section",
			notes:   "# Release notes\n\nNo Windows status is documented.\n",
			wantErr: true,
		},
		{
			name:    "empty section",
			notes:   "# Release notes\n\n## Windows status\n\n \t\n",
			wantErr: true,
		},
		{
			name:    "heading-only section",
			notes:   "# Release notes\n\n## Windows status\n\n## Checksums\n\nChecksums are attached.\n",
			wantErr: true,
		},
		{
			name:    "heading-only section followed by indented heading",
			notes:   "# Release notes\n\n## Windows status\n\n  ## Checksums\n\nChecksums are attached.\n",
			wantErr: true,
		},
		{
			name:    "HTML-comment-only section",
			notes:   "# Release notes\n\n## Windows status\n\n<!-- TODO: document Windows status -->\n\n## Checksums\n",
			wantErr: true,
		},
		{
			name:    "reference-definition-only section",
			notes:   "# Release notes\n\n## Windows status\n\n[windows-docs]: https://example.com\n\n## Checksums\n",
			wantErr: true,
		},
		{
			name:    "space-indented-code-only section",
			notes:   "# Release notes\n\n## Windows status\n\n    ## Next section\n",
			wantErr: true,
		},
		{
			name:    "tab-indented-code-only section",
			notes:   "# Release notes\n\n## Windows status\n\n\t## Next section\n",
			wantErr: true,
		},
		{
			name:    "section inside fenced code",
			notes:   "# Release notes\n\n```markdown\n## Windows status\nWindows archives remain experimental.\n```\n\n## Checksums\n",
			wantErr: true,
		},
		{
			name:    "non-empty section",
			notes:   "# Release notes\n\n## Windows status\n\nWindows archives remain experimental.\n\n## Checksums\n",
			wantErr: false,
		},
		{
			name:    "non-empty CRLF section",
			notes:   "# Release notes\r\n\r\n## Windows status\r\n\r\nWindows archives remain experimental.\r\n\r\n## Checksums\r\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releaseNotes := filepath.Join(t.TempDir(), "release-notes.md")
			if err := os.WriteFile(releaseNotes, []byte(tt.notes), 0o600); err != nil {
				t.Fatal(err)
			}

			output, err := exec.Command("sh", script, releaseNotes).CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Fatalf("gate error = %v, wantErr %v; output: %s", err, tt.wantErr, output)
			}
		})
	}
}

func TestPublicDocumentationCommunityAndRoadmapContracts(t *testing.T) {
	config, err := os.ReadFile(filepath.Join(".github", "ISSUE_TEMPLATE", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "/discussions") {
		t.Error("issue template config links to disabled GitHub Discussions")
	}

	roadmap, err := os.ReadFile("ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(roadmap), "remain in the 0.x series until the maintainer is satisfied") {
		t.Error("roadmap is missing the explicit 0.x release posture")
	}
}

func isExternalDocumentationLink(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(target, "#") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "mailto:")
}
