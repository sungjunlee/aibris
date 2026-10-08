package adapter

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestGoCacheOverrideRequiresREADME(t *testing.T) {
	for _, source := range []string{"environment", "GOENV"} {
		for _, signature := range []string{"missing", "invalid", "directory", "symlink", "valid"} {
			t.Run(source+"/"+signature, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				path := filepath.Join(home, ".cache", "custom-go")
				mkdirs(t, path)
				readme := filepath.Join(path, "README")
				content := []byte("This directory holds cached build artifacts from the Go build system.\nextra text\n")
				switch signature {
				case "invalid":
					if err := os.WriteFile(readme, []byte("personal archive"), 0o644); err != nil {
						t.Fatal(err)
					}
				case "directory":
					mkdirs(t, readme)
				case "symlink":
					other := filepath.Join(home, "README")
					if err := os.WriteFile(other, content, 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, readme); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				case "valid":
					if err := os.WriteFile(readme, content, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if source == "environment" {
					t.Setenv("GOCACHE", path)
				} else {
					file := filepath.Join(home, "goenv")
					if err := os.WriteFile(file, []byte("GOCACHE="+path+"\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					t.Setenv("GOENV", file)
				}
				items, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{})
				if err != nil {
					t.Fatal(err)
				}
				want := signature == "valid"
				if (len(items) == 1) != want {
					t.Errorf("scan = %+v; target wanted %t", items, want)
				}
				if slices.Contains(CacheTargetPaths(), path) != want {
					t.Errorf("override allowlisted; want %t", want)
				}
			})
		}
	}
}
