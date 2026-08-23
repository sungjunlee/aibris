package cleaner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestPhysicalInventoryCountsNestedAliasesOnce(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "848f")
	nested := filepath.Join(owner, "proj")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	items := []types.DebrisInfo{
		{ID: "a", Path: owner, Size: 400, Category: types.CategoryWorktree},
		{ID: "b", Path: owner, Size: 400, Category: types.CategoryWorktree},
		{ID: "c", Path: owner, Size: 400, Category: types.CategoryWorktree},
		{ID: "nested", Path: nested, Size: 50, Category: types.CategoryNodeModules},
	}

	units := PhysicalInventory(items)
	if len(units) != 1 {
		t.Fatalf("physical units = %d; want 1 outer owner", len(units))
	}
	if units[0].Path != owner || units[0].Size != 400 {
		t.Fatalf("physical unit = %+v; want owner path and 400 bytes", units[0])
	}
}

func TestPhysicalInventoryKeepsUnkeyedRows(t *testing.T) {
	items := []types.DebrisInfo{
		{ID: "a", Size: 10},
		{ID: "b", Size: 20},
	}
	units := PhysicalInventory(items)
	if len(units) != 2 {
		t.Fatalf("unkeyed units = %d; want 2", len(units))
	}
}
