package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func TestPublicDocumentationLocalLinks(t *testing.T) {
	files := []string{
		"README.md",
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
