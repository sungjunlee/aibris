package scanner

import (
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func fillInventoryTotals(result *types.ScanResult, catByTool map[types.Tool]types.Category) {
	result.TotalCount = len(result.Worktrees)
	for _, item := range result.Worktrees {
		addEvidenceSummary(result, item, catByTool)
	}
	addPhysicalSummary(result, catByTool)
}

func addEvidenceSummary(result *types.ScanResult, item types.DebrisInfo, catByTool map[types.Tool]types.Category) {
	result.TotalSize += item.Size
	result.TotalStrippableBytes += item.StrippableBytes
	cat := itemCategory(item, catByTool)
	summary := result.ByCategory[cat]
	summary.Count++
	summary.Size += item.Size
	summary.StrippableBytes += item.StrippableBytes
	result.ByCategory[cat] = summary
	tool := result.ByTool[item.Tool]
	tool.Count++
	tool.Size += item.Size
	tool.StrippableBytes += item.StrippableBytes
	result.ByTool[item.Tool] = tool
}

func addPhysicalSummary(result *types.ScanResult, catByTool map[types.Tool]types.Category) {
	units := cleaner.PhysicalInventory(result.Worktrees)
	result.PhysicalUnitCount = len(units)
	for _, item := range units {
		result.PhysicalTotalBytes += item.Size
		cat := itemCategory(item, catByTool)
		summary := result.ByCategory[cat]
		summary.PhysicalUnitCount++
		summary.PhysicalTotalBytes += item.Size
		result.ByCategory[cat] = summary
		tool := result.ByTool[item.Tool]
		tool.PhysicalUnitCount++
		tool.PhysicalTotalBytes += item.Size
		result.ByTool[item.Tool] = tool
	}
}

func itemCategory(item types.DebrisInfo, catByTool map[types.Tool]types.Category) types.Category {
	if item.Category != "" {
		return item.Category
	}
	return catByTool[item.Tool]
}
