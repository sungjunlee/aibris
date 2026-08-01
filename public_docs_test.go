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
			name:    "section inside fenced code",
			notes:   "# Release notes\n\n```markdown\n## Windows status\nWindows archives remain experimental.\n```\n\n## Checksums\n",
			wantErr: true,
		},
		{
			name:    "non-empty section",
			notes:   "# Release notes\n\n## Windows status\n\nWindows archives remain experimental.\n\n## Checksums\n",
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
