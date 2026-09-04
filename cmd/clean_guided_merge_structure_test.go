package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestGuidedClassicMergePolicyLivesInDedicatedModule(t *testing.T) {
	mergeSource := readCmdSource(t, "clean_guided_merge.go")
	cleanSource := readCmdSource(t, "clean.go")
	for _, name := range []string{
		"mergeGuidedPreviewWithClassicTargets",
		"mergeCleanupOverlapComponents",
		"applyGuidedCleanDefaults",
		"guidedCleanAge",
		"shouldRelaxCacheAge",
	} {
		definition := "func " + name + "("
		if !strings.Contains(mergeSource, definition) {
			t.Errorf("%s is not defined in clean_guided_merge.go", name)
		}
		if strings.Contains(cleanSource, definition) {
			t.Errorf("%s is still defined in clean.go", name)
		}
	}
}

func readCmdSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
