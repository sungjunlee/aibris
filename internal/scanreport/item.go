package scanreport

import (
	"fmt"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func projectItems(items []types.DebrisInfo) []Item {
	ids := physicalTargetIDs(items)
	out := make([]Item, len(items))
	for i, w := range items {
		cleanupCommand := append([]string(nil), w.CleanupCommand...)
		if cleanupCommand == nil {
			cleanupCommand = []string{}
		}
		out[i] = Item{
			Tool:             w.Tool,
			Category:         w.Category,
			ID:               w.ID,
			Project:          w.Project,
			Source:           w.Source,
			Path:             w.Path,
			Size:             w.Size,
			ModTime:          w.ModTime,
			Status:           w.Status,
			Classification:   w.Classification,
			Risk:             ItemRisk(w),
			Reason:           ItemReason(w),
			CleanupKind:      ItemCleanupKind(w),
			CleanupCommand:   cleanupCommand,
			PhysicalTargetID: ids[i],
			StrippableBytes:  w.StrippableBytes,
			StrippablePaths:  append([]string(nil), w.StrippablePaths...),
			ReviewOnly:       isReviewOnlyWorktree(w),
		}
	}
	return out
}

// ItemCleanupKind is the effective cleanup kind, defaulting to remove-path.
func ItemCleanupKind(w types.DebrisInfo) types.CleanupKind {
	if w.CleanupKind != "" {
		return w.CleanupKind
	}
	return types.CleanupRemovePath
}

// ItemRisk is the JSON/human risk label derived from category.
func ItemRisk(w types.DebrisInfo) string {
	if w.Category.IsRisky() {
		return "high"
	}
	switch w.Category {
	case types.CategoryNodeModules, types.CategoryBuildCache:
		return "medium"
	default:
		return "low"
	}
}

// ItemReason is the JSON/human reason string derived from category, status,
// classification, or an explicit scanner reason.
func ItemReason(w types.DebrisInfo) string {
	if w.Reason != "" {
		return w.Reason
	}
	switch w.Category {
	case types.CategoryWorktree:
		switch w.Status {
		case types.WorktreeActive:
			return "active worktree; protected from cleanup by default"
		case types.WorktreeOrphaned:
			return "orphaned worktree; parent repo metadata missing"
		default:
			return "worktree debris"
		}
	case types.CategoryNodeModules:
		return "dependency directory; can be reinstalled"
	case types.CategoryBuildCache:
		return "build cache; can be regenerated"
	case types.CategoryOtherCache:
		return "package cache; can be regenerated"
	case types.CategoryAgentState:
		switch w.Classification {
		case types.EntryClassLive:
			return "recorded cwd exists"
		case types.EntryClassOrphaned:
			return "recorded cwd does not exist"
		default:
			return "recorded cwd could not be determined"
		}
	case types.CategoryAILogs:
		return "AI tool logs; requires --risky to clean"
	default:
		return "unknown category; requires explicit review"
	}
}

func physicalTargetIDs(items []types.DebrisInfo) []string {
	units := cleaner.PhysicalInventory(items)
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = fmt.Sprintf("target-%d", physicalUnitIndex(item, units)+1)
	}
	return ids
}

func physicalUnitIndex(item types.DebrisInfo, units []types.DebrisInfo) int {
	itemPath, itemOK := cleaner.TargetPathKey(item.Path)
	for i, unit := range units {
		if physicalUnitOwns(unit, item, itemPath, itemOK) {
			return i
		}
	}
	return len(units)
}

func physicalUnitOwns(unit, item types.DebrisInfo, itemPath string, itemOK bool) bool {
	unitPath, unitOK := cleaner.TargetPathKey(unit.Path)
	if unitOK && itemOK {
		return unitPath == itemPath || cleaner.PathContains(unitPath, itemPath)
	}
	return unit.Path == item.Path && unit.ID == item.ID
}

// ItemName is the display name shared by scan largest rows and clean target rows.
func ItemName(w types.DebrisInfo) string {
	if w.Category == types.CategoryWorktree && w.Tool == types.ToolUnknown && w.Source != "" {
		return w.Source + "/" + w.ID
	}
	if w.ID != "" {
		return w.ID
	}
	return string(w.Tool)
}

// ItemProject is the project label shared by scan largest rows and clean target rows.
func ItemProject(w types.DebrisInfo) string {
	if w.Project != "" {
		return w.Project
	}
	switch w.Category {
	case types.CategoryBuildCache, types.CategoryOtherCache, types.CategoryAILogs:
		return "global"
	default:
		return "-"
	}
}

// ItemAgeAndStatus is the age/status label shared by scan largest rows and
// clean target rows.
func ItemAgeAndStatus(w types.DebrisInfo) string {
	age := AgeString(time.Since(w.ModTime).Round(time.Hour))
	if w.Status == "" {
		return age
	}
	return fmt.Sprintf("%s %s", w.Status, age)
}

// ItemNoun pluralizes "item" for counts.
func ItemNoun(count int) string {
	if count == 1 {
		return "item"
	}
	return "items"
}

// AgeString renders a rounded age as "today" or whole days.
func AgeString(d time.Duration) string {
	if d.Hours() < 24 {
		return "today"
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

// CleanAgeDisplay renders a policy age as days, hours, or a Go duration.
func CleanAgeDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

// debrisInfo reconstructs the source DebrisInfo fields the shared display
// helpers need.
func (it Item) debrisInfo() types.DebrisInfo {
	return types.DebrisInfo{
		Tool:            it.Tool,
		Category:        it.Category,
		ID:              it.ID,
		Project:         it.Project,
		Source:          it.Source,
		Path:            it.Path,
		Size:            it.Size,
		ModTime:         it.ModTime,
		Status:          it.Status,
		Classification:  it.Classification,
		Reason:          it.Reason,
		CleanupKind:     it.CleanupKind,
		CleanupCommand:  it.CleanupCommand,
		StrippableBytes: it.StrippableBytes,
		StrippablePaths: it.StrippablePaths,
	}
}

func (v View) sourceDebris() []types.DebrisInfo {
	if v.debris != nil {
		return v.debris
	}
	out := make([]types.DebrisInfo, len(v.Items))
	for i, it := range v.Items {
		out[i] = it.debrisInfo()
	}
	return out
}
