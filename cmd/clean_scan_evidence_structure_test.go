package cmd

import (
	"strings"
	"testing"
)

func TestCleanScanEvidencePolicyLivesInDedicatedModule(t *testing.T) {
	evidenceSource := readCmdSource(t, "clean_scan_evidence.go")
	cleanSource := readCmdSource(t, "clean.go")
	for _, name := range []string{
		"scanForClean",
		"scanForCleanQuiet",
		"cleanScanSelector",
		"requireCompleteScan",
		"filterTargetsWithoutScanEvidence",
	} {
		definition := "func " + name + "("
		if !strings.Contains(evidenceSource, definition) {
			t.Errorf("%s is not defined in clean_scan_evidence.go", name)
		}
		if strings.Contains(cleanSource, definition) {
			t.Errorf("%s is still defined in clean.go", name)
		}
	}
}
