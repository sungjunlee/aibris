package scanreport

import (
	"fmt"
	"io"
	"sort"
	"unicode"

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
		fmt.Fprintf(w, "  %-13s %4d  %9s\n", category, entry.PhysicalUnitCount, cleaner.FormatSize(entry.PhysicalTotalBytes))
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
		fmt.Fprintf(w, "  %9s  %-13s %s %s %s\n",
			cleaner.FormatSize(item.Size),
			item.Category,
			fitColumn(itemName(item), 16),
			fitColumn(itemProject(item), 18),
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

// fitColumn truncates s to width terminal cells, with a trailing ellipsis
// when it is cut, and pads it to exactly width cells, so wide (CJK) names and
// long encoded store entries keep the columns aligned.
func fitColumn(s string, width int) string {
	var out []rune
	used := 0
	runes := []rune(s)
	for i, r := range runes {
		w := runeCells(r)
		rest := 0
		for _, next := range runes[i:] {
			rest += runeCells(next)
		}
		if used+rest <= width {
			out = append(out, runes[i:]...)
			used += rest
			break
		}
		if used+w > width-1 {
			out = append(out, '…')
			used++
			break
		}
		out = append(out, r)
		used += w
	}
	for ; used < width; used++ {
		out = append(out, ' ')
	}
	return string(out)
}

// runeCells approximates terminal display width: combining marks take no
// cell, East Asian wide and fullwidth characters take two.
func runeCells(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return 0
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0xA4CF, // CJK radicals through Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	default:
		return 1
	}
}
