package cleaner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

type CleanupOverlapRelation string

const (
	CleanupOverlapOwner      CleanupOverlapRelation = "physical-owner"
	CleanupOverlapExact      CleanupOverlapRelation = "exact-path"
	CleanupOverlapDescendant CleanupOverlapRelation = "nested-descendant"
	CleanupOverlapAncestor   CleanupOverlapRelation = "containing-ancestor"
	CleanupOverlapAmbiguous  CleanupOverlapRelation = "ambiguous-path"
)

type CleanupOverlapLogicalInput struct {
	Item           types.DebrisInfo
	PolicyReason   string
	PolicyDecision string
	ReasonCodes    []string
}

type CleanupOverlapLogicalRow struct {
	Key                  string
	Item                 types.DebrisInfo
	CanonicalPath        string
	Relation             CleanupOverlapRelation
	PolicyReason         string
	PolicyDecision       string
	ReasonCodes          []string
	L1Reason             string
	RevalidationRequired bool
	PhysicalBytes        int64
	DiscoveryOrdinal     int
}

type CleanupOverlapComponent struct {
	Key           string
	CanonicalPath string
	Owner         types.DebrisInfo
	LogicalRows   []CleanupOverlapLogicalRow
	Obligations   []AgentStateObligation
	Refusal       *OverlapSafetyRefusal
}

func CleanupLogicalRelation(ownerPath, rowPath string) (CleanupOverlapRelation, bool) {
	switch {
	case ownerPath == rowPath:
		return CleanupOverlapExact, true
	case PathContains(ownerPath, rowPath):
		return CleanupOverlapDescendant, true
	case PathContains(rowPath, ownerPath):
		return CleanupOverlapAncestor, true
	default:
		return "", false
	}
}

func CleanupLogicalPolicyReason(input CleanupOverlapLogicalInput) string {
	if input.PolicyReason != "" {
		return input.PolicyReason
	}
	if input.Item.Reason != "" {
		return input.Item.Reason
	}
	if input.Item.Category == types.CategoryAgentState {
		switch input.Item.Classification {
		case types.EntryClassOrphaned:
			return "recorded working directory is absent"
		case types.EntryClassLive:
			return "live agent-state protected"
		default:
			return "undetermined agent-state protected"
		}
	}
	return "discovered cleanup evidence"
}

func EnsureCleanupOwnerLogicalRow(
	rows []CleanupOverlapLogicalRow,
	owner types.DebrisInfo,
	canonicalPath string,
) []CleanupOverlapLogicalRow {
	for _, row := range rows {
		if row.CanonicalPath == canonicalPath &&
			TargetStableKey(row.Item) == TargetStableKey(owner) {
			return rows
		}
	}
	return append(rows, CleanupOverlapLogicalRow{
		Item:          owner,
		CanonicalPath: canonicalPath,
		Relation:      CleanupOverlapExact,
		PolicyReason:  "selected cleanup target",
	})
}

func SortCleanupOverlapLogicalRows(
	rows []CleanupOverlapLogicalRow,
	owner types.DebrisInfo,
) {
	sort.SliceStable(rows, func(i, j int) bool {
		return cleanupOverlapLogicalRowStableKey(rows[i], owner) <
			cleanupOverlapLogicalRowStableKey(rows[j], owner)
	})
	ordinals := make(map[string]int)
	ownerAssigned := false
	for i := range rows {
		baseKey := cleanupOverlapLogicalRowStableKey(rows[i], owner)
		ordinals[baseKey]++
		rows[i].DiscoveryOrdinal = ordinals[baseKey]
		if !ownerAssigned &&
			rows[i].Relation == CleanupOverlapExact &&
			TargetStableKey(rows[i].Item) == TargetStableKey(owner) {
			rows[i].Relation = CleanupOverlapOwner
			ownerAssigned = true
		}
		rows[i].Key = fmt.Sprintf("%s#%d", baseKey, rows[i].DiscoveryOrdinal)
	}
}

func cleanupOverlapLogicalRowStableKey(
	row CleanupOverlapLogicalRow,
	owner types.DebrisInfo,
) string {
	ownerRank := "1"
	if row.CanonicalPath != "" &&
		TargetStableKey(row.Item) == TargetStableKey(owner) {
		ownerRank = "0"
	}
	return strings.Join([]string{
		ownerRank,
		row.CanonicalPath,
		string(row.Relation),
		string(row.Item.Category),
		string(row.Item.Tool),
		row.Item.ID,
		row.Item.Path,
		string(row.Item.Classification),
		row.PolicyReason,
		row.L1Reason,
	}, "\x00")
}
