package cmd

import (
	"context"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/scancache"
)

func TestLastScanSessionReusesMatchingIdentity(t *testing.T) {
	_, workspace := seededReuseWorkspace(t)
	roots := mustNormalizeRoots(t, workspace)
	result, source, err := loadLastScanSession(context.Background(), roots, nil, "delete", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != scanSourceCached {
		t.Fatalf("source = %q; want cached", source.Kind)
	}
	if result == nil {
		t.Fatal("cached session returned nil result")
	}
}

func TestLastScanSessionMismatchForcesLiveScan(t *testing.T) {
	_, workspace := reuseScanFixture(t)
	roots := mustNormalizeRoots(t, workspace)
	foreign := adapter.Identity([]adapter.DebrisProvider{adapter.NewWorktreeAdapter()})
	cache := validReuseCache(roots, "delete")
	cache.ProviderIdentity = foreign
	if err := saveLastScanCache(cache); err != nil {
		t.Fatal(err)
	}

	result, source, err := loadLastScanSession(context.Background(), roots, nil, "delete", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != scanSourceLive {
		t.Fatalf("source = %q; want live", source.Kind)
	}
	if result == nil {
		t.Fatal("live session returned nil result")
	}

	stored, ok := readLastScanCache()
	if !ok {
		t.Fatal("live scan did not write last-scan.json")
	}
	if stored.SchemaVersion != lastScanCacheSchemaVersion {
		t.Fatalf("schema_version = %d; want %d", stored.SchemaVersion, lastScanCacheSchemaVersion)
	}
	if reason := scancache.IdentityMismatch(roots, true, stored); reason != "" {
		t.Fatalf("live scan wrote an identity the helper refuses: %s", reason)
	}
	assertOnlyLastScanCacheFile(t)
}
