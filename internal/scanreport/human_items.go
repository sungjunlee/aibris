package scanreport

import (
	"fmt"
	"io"
	"sort"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// Item display helpers for the human scan report:
// by-category summary and largest-item section.

func writeCategorySummary(w io.Writer, summary map[types.Category]types.CategorySummary) {
	if len(summary) == 0 {
		return
	}

	fmt.Fprintln(w, "\nby category")
	for _, category := range sortedCategories(summary) {
		entry := summary[category]
		fmt.Fprintf(w, "  %-13s %3d   %s\n", category, entry.PhysicalUnitCount, cleaner.FormatSize(entry.PhysicalTotalBytes))
	}
}

func writeLargestItems(w io.Writer, items []Item) {
	if len(items) == 0 {
		return
	}

	limit := 5
	if len(items) < limit {
		limit = len(items)
	}

	fmt.Fprintln(w, "\nlargest")
	for _, item := range items[:limit] {
		fmt.Fprintf(w, "  %8s  %-13s %-12s %-18s %s\n",
			cleaner.FormatSize(item.Size),
			item.Category,
			itemName(item),
			itemProject(item),
			itemAgeAndStatus(item))
	}
	if len(items) > limit {
		fmt.Fprintf(w, "  + %d more\n", len(items)-limit)
	}
}

func sortedCategories(summary map[types.Category]types.CategorySummary) []types.Category {
	categories := make([]types.Category, 0, len(summary))
	for category := range summary {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool {
		left := summary[categories[i]]
		right := summary[categories[j]]
		if left.Size == right.Size {
			return categories[i] < categories[j]
		}
		return left.Size > right.Size
	})
	return categories
}

func itemName(item Item) string {
	return ItemName(item.debrisInfo())
}

func itemProject(item Item) string {
	return ItemProject(item.debrisInfo())
}

func itemAgeAndStatus(item Item) string {
	return ItemAgeAndStatus(item.debrisInfo())
}
