package cleanjson

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// These fixtures are captured from the pre-#603 implementation. They pin the
// complete guided wire document, including its existing reason-code vocabulary.
func TestGuidedReceiptWireCompatibility(t *testing.T) {
	for _, includePaths := range []bool{false, true} {
		name := "redacted"
		if includePaths {
			name = "paths"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			var components []SnapshotComponent
			var plan UnifiedPlan
			var prepared []PreparedTarget
			for i, id := range []string{"removed", "partial", "failed", "declined", "cancelled", "refused"} {
				item := types.DebrisInfo{Path: filepath.Join(home, id, "node_modules"), ID: id, Project: id, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8}
				if err := os.MkdirAll(item.Path, 0700); err != nil {
					t.Fatal(err)
				}
				key, ok := cleaner.TargetPathKey(item.Path)
				if !ok {
					t.Fatal("fixture path has no identity")
				}
				components = append(components, SnapshotComponent{Key: key, Owner: item, Decision: DecisionSelected, AccountingBytes: 8, Rows: []SnapshotRow{{Item: item, Relation: RelationOwner, PolicyDecision: PolicyEligible, Decision: DecisionSelected, ReasonCodes: []string{"classic_eligible"}}}})
				plan.Components = append(plan.Components, PlanComponent{Key: key, CanonicalPath: key, Owner: item, Selection: string(cleaner.CleanupPlanSelected)})
				if i < 5 {
					prepared = append(prepared, PreparedTarget{Item: item, ReceiptTargetKey: RowIdentityKey(item)})
				}
			}
			pending, err := NewGuidedExecutionReceipt(Source{Kind: SourceLive}, types.PruneOptions{Age: 7 * 24 * time.Hour}, &GuidedPolicy{MinIdleAge: 24 * time.Hour}, PlanEvidence{ObservedAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}, components, prepared, plan, includePaths)
			if err != nil {
				t.Fatal(err)
			}
			pending.ObserveInteractiveSkip(InteractiveSkipOutcome{Target: prepared[3], Declined: true})
			pending.ObserveInteractiveSkip(InteractiveSkipOutcome{Target: prepared[4], AfterConfirmation: true})
			receipt, err := pending.Finish(ExecutionReceipt{Units: []ExecutionUnit{
				{ReceiptTargetKey: RowIdentityKey(prepared[0].Item), State: "removed", PhysicalRemoved: true, FreedBytes: 8},
				{ReceiptTargetKey: RowIdentityKey(prepared[1].Item), State: "partial", FreedBytes: 3, ResidualBytes: 5},
				{ReceiptTargetKey: RowIdentityKey(prepared[2].Item), State: "failed", ResidualBytes: 8},
			}}, nil, func() (int, error) { return 0, nil })
			if err != nil {
				t.Fatal(err)
			}
			// Host capacity is outside the identity contract; use fixed path-free
			// host state so this fixture never depends on disk fullness.
			receipt.PostClean = &ReceiptPostClean{LocalAPFSSnapshots: 0}
			var encoded bytes.Buffer
			if err := EncodeReceipt(&encoded, receipt); err != nil {
				t.Fatal(err)
			}
			canonicalHome, ok := cleaner.TargetPathKey(home)
			if !ok {
				t.Fatal("fixture home has no identity")
			}
			got := encoded.String()
			for _, path := range []string{home, canonicalHome} {
				escaped, err := json.Marshal(path)
				if err != nil {
					t.Fatal(err)
				}
				got = strings.ReplaceAll(got, string(escaped[1:len(escaped)-1]), "<HOME>")
			}
			got = strings.ReplaceAll(got, `\\`, "/")
			goldenPath := filepath.Join("testdata", "guided_receipt_"+name+".json")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("guided receipt changed:\n%s", got)
			}
		})
	}
}
