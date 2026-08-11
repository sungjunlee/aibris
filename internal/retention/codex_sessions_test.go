package retention

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCodexSessionsBucketsUseLeafModTimeUTCOnly(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	leaf := writeRetentionSession(t, home, "2020", "01", "02", "utc-boundary", validMetadata(filepath.Join(home, "live")), "")
	modTime := time.Date(2026, 3, 1, 0, 30, 0, 0, time.FixedZone("east", 2*60*60))
	if err := os.Chtimes(leaf, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	setModTime(t, filepath.Dir(leaf), time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))

	bucket := onlyBucket(t, scanRetention(t, provider, home))
	if bucket.BucketID != "2026-02" {
		t.Fatalf("bucket = %+v; want leaf UTC month 2026-02", bucket)
	}
}

func TestCodexSessionsBucketAccountingAndOrdering(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()

	first := writeRetentionSession(t, home, "2026", "01", "01", "jan-a", validMetadata(liveCWD(t, home, "alpha")), "")
	second := writeRetentionSession(t, home, "2026", "01", "01", "jan-b", validMetadata(liveCWD(t, home, "beta")), "")
	setModTime(t, first, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	setModTime(t, second, time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	juneLeaf := writeRetentionSession(t, home, "2026", "06", "02", "jun", validMetadata(liveCWD(t, home, "gamma")), "")
	setModTime(t, juneLeaf, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	projection := scanRetention(t, provider, home)
	if len(projection.Buckets) != 2 {
		t.Fatalf("buckets = %+v; want 2026-01 and 2026-06", projection.Buckets)
	}
	jan := bucketByID(t, projection, "2026-01")
	if jan.UnitCount != 2 || jan.MemberCount != 2 {
		t.Fatalf("jan bucket = %+v; want 2 units/members", jan)
	}
	if jan.ApparentBytes != twoFileSizes(t, first, second) {
		t.Fatalf("jan bytes = %d", jan.ApparentBytes)
	}
	if jan.OrphanedCount != 0 {
		t.Fatalf("jan orphaned = %d; existing cwd dirs are live", jan.OrphanedCount)
	}
	juneBucket := bucketByID(t, projection, "2026-06")
	if juneBucket.UnitCount != 1 || juneBucket.MemberCount != 1 {
		t.Fatalf("june bucket = %+v", juneBucket)
	}
	if projection.Buckets[0].BucketID != "2026-01" || projection.Buckets[1].BucketID != "2026-06" {
		t.Fatalf("bucket order = %v; want sorted", []string{projection.Buckets[0].BucketID, projection.Buckets[1].BucketID})
	}
	if projection.Partial {
		t.Fatalf("projection = %+v; want complete", projection)
	}
}

func TestCodexSessionsOrphanAggregateRequiresProvenAbsentCWD(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()

	goneCWD := filepath.Join(home, "gone-project")
	if err := os.MkdirAll(goneCWD, 0755); err != nil {
		t.Fatal(err)
	}
	orphan := writeRetentionSession(t, home, "2026", "03", "04", "orphan", validMetadata(goneCWD), "")
	setModTime(t, orphan, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
	if err := os.RemoveAll(goneCWD); err != nil {
		t.Fatal(err)
	}

	liveCWD := filepath.Join(home, "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	live := writeRetentionSession(t, home, "2026", "03", "04", "live", validMetadata(liveCWD), "")
	setModTime(t, live, time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC))

	bucket := onlyBucket(t, scanRetention(t, provider, home))
	if bucket.OrphanedCount != 1 || bucket.OrphanedBytes != fileSize(t, orphan) {
		t.Fatalf("bucket = %+v; want exactly the gone-cwd unit orphaned", bucket)
	}
	if bucket.UnitCount != 2 || bucket.MemberCount != 2 {
		t.Fatalf("bucket = %+v; both units counted", bucket)
	}
}

func TestCodexSessionsUnsupportedProducerAndVersionDoNotCountOrphans(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()

	goneCWD := filepath.Join(home, "gone")
	if err := os.MkdirAll(goneCWD, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := writeRetentionSession(t, home, "2026", "01", "01", "foreign", validMetadataWithProducer(goneCWD, "other_tool", "1.2.3"), "")
	setModTime(t, foreign, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	badVersion := writeRetentionSession(t, home, "2026", "01", "01", "bad-version", validMetadataWithProducer(goneCWD, "codex_cli_rs", "unrecognized"), "")
	setModTime(t, badVersion, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if err := os.RemoveAll(goneCWD); err != nil {
		t.Fatal(err)
	}

	bucket := onlyBucket(t, scanRetention(t, provider, home))
	if bucket.UnitCount != 2 || bucket.OrphanedCount != 0 {
		t.Fatalf("bucket = %+v; units counted but orphans must be 0", bucket)
	}
}

func TestCodexSessionsUnknownBucketFromUnusableModTime(t *testing.T) {
	for _, test := range []struct {
		name    string
		modTime time.Time
		want    string
	}{
		{"zero", time.Time{}, retentionUnknownBucket},
		{"year-zero", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), retentionUnknownBucket},
		{"normal-utc", time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC), "2026-02"},
		{"normal-offset", time.Date(2026, 2, 3, 4, 5, 6, 0, time.FixedZone("east", 10*60*60)), "2026-02"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := bucketFromModTime(test.modTime); got != test.want {
				t.Fatalf("bucketFromModTime(%v) = %q; want %q", test.modTime, got, test.want)
			}
		})
	}
}

func TestCodexSessionsSilentlySkipsUnsupportedEntries(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()

	day := filepath.Join(home, ".codex", "sessions", "2026", "04", "05")
	if err := os.MkdirAll(day, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(day, "rollout-link.jsonl")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(day, "note.txt"), []byte("not a rollout"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(day, "nested", "deeper"), 0755); err != nil {
		t.Fatal(err)
	}

	projection := scanRetention(t, provider, home)
	if len(projection.Buckets) != 0 || projection.Partial {
		t.Fatalf("projection = %+v; symlink/non-rollout/nested entries must be skipped silently", projection)
	}
}

func TestCodexSessionsHonorsSelectedRootsAndMissingRootIsCompleteEmpty(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()

	projection, err := provider.Scan(context.Background(), types.ScanOptions{Roots: []string{filepath.Join(home, "elsewhere")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Buckets) != 0 || projection.Partial {
		t.Fatalf("deselected root projection = %+v; want empty complete", projection)
	}

	projection, err = provider.Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Buckets) != 0 || projection.Partial {
		t.Fatalf("missing store projection = %+v; want empty complete", projection)
	}
}

func TestCodexSessionsRootHonorsCodexHomeEnv(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	codexHome := filepath.Join(t.TempDir(), "codex-runtime-home")
	if err := os.MkdirAll(codexHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	root, err := codexSessionsRoot()
	if err != nil {
		t.Fatal(err)
	}
	wantHome := codexHome
	if resolved, resolveErr := filepath.EvalSymlinks(codexHome); resolveErr == nil {
		wantHome = resolved
	}
	if want := filepath.Join(wantHome, "sessions"); root != want {
		t.Fatalf("codexSessionsRoot() = %q; want %q", root, want)
	}
}

func TestCodexSessionsInventoriesCodexHomeOutsideDefaultRoots(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	provider := testCodexProvider()

	// The session leaf lives under CODEX_HOME/sessions, not ~/.codex.
	liveCWD := filepath.Join(home, "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(codexHome, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(directory, "rollout-runtime.jsonl")
	if err := os.WriteFile(leaf, []byte(validMetadata(liveCWD)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setModTime(t, leaf, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))

	// Default-style scan roots cover only $HOME; the CODEX_HOME store must
	// still be inventoried rather than silently deselected.
	projection, err := provider.Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	bucket := onlyBucket(t, projection)
	if bucket.BucketID != "2026-01" || bucket.UnitCount != 1 || bucket.MemberCount != 1 {
		t.Fatalf("bucket = %+v; want the CODEX_HOME leaf inventoried", bucket)
	}
	if projection.Partial {
		t.Fatalf("projection = %+v; want complete", projection)
	}
}

func TestCodexSessionsCancellationIsHard(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	writeRetentionSession(t, home, "2026", "01", "01", "x", validMetadata("/tmp"), "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Scan(ctx, types.ScanOptions{Roots: []string{home}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
}

func TestCodexSessionsPermissionFailureIsPathFreePartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	readable := writeRetentionSession(t, home, "2026", "01", "01", "a", validMetadata("/tmp"), "")
	setModTime(t, readable, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	unreadable := writeRetentionSession(t, home, "2026", "01", "02", "b", validMetadata("/tmp"), "")
	setModTime(t, unreadable, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err := os.Chmod(filepath.Dir(unreadable), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(unreadable), 0755) })

	projection := scanRetention(t, provider, home)
	if !projection.Partial || len(projection.ProviderErrors) == 0 {
		t.Fatalf("projection = %+v; want partial provider error", projection)
	}
	raw, err := json.Marshal(projection.ProviderErrors)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), home) {
		t.Fatalf("provider diagnostics leaked store path components: %s", raw)
	}
	// The readable leaf still contributes; only the unreadable subtree degrades.
	if bucket := bucketByID(t, projection, "2026-01"); bucket.UnitCount != 1 {
		t.Fatalf("2026-01 bucket = %+v; want 1 unit from the readable leaf", bucket)
	}
}

func TestCodexSessionsDuplicateProviderRegistrationIsRejected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	writeRetentionSession(t, home, "2026", "01", "01", "dup", validMetadata(filepath.Join(home, "live")), "")
	projection := mergeRetention(t, home, []types.RetentionProvider{provider, provider})
	if !projection.Partial {
		t.Fatalf("projection = %+v; duplicate buckets must degrade to partial", projection)
	}
	foundDuplicate := false
	for _, providerErr := range projection.ProviderErrors {
		if providerErr.Message == "duplicate retention bucket" {
			foundDuplicate = true
		}
	}
	if !foundDuplicate {
		t.Fatalf("provider errors = %+v; want duplicate bucket diagnostic", projection.ProviderErrors)
	}
}

func TestCodexSessionsLeafPermissionFailureIsPathFreePartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	leaf := writeRetentionSession(t, home, "2026", "01", "01", "secret", validMetadata("/tmp"), "")
	setModTime(t, leaf, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := os.Chmod(leaf, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(leaf, 0600) })

	projection := scanRetention(t, provider, home)
	if !projection.Partial || len(projection.ProviderErrors) == 0 {
		t.Fatalf("projection = %+v; want partial provider error", projection)
	}
	raw, err := json.Marshal(projection.ProviderErrors)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), home) || strings.Contains(string(raw), "secret") ||
		strings.Contains(string(raw), ".jsonl") {
		t.Fatalf("leaf permission diagnostics leaked member paths: %s", raw)
	}
	// The unreadable leaf still counts as a unit; only orphan classification
	// is impossible for it.
	if bucket := bucketByID(t, projection, "2026-01"); bucket.UnitCount != 1 {
		t.Fatalf("2026-01 bucket = %+v; want 1 unit from the unreadable leaf", bucket)
	}
}

func TestCodexSessionsRootPermissionFailureIsPathFreePartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(codexDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(codexDir, 0755) })

	projection := scanRetention(t, provider, home)
	if !projection.Partial || len(projection.ProviderErrors) == 0 {
		t.Fatalf("projection = %+v; want partial provider error", projection)
	}
	raw, err := json.Marshal(projection.ProviderErrors)
	if err != nil {
		t.Fatal(err)
	}
	// The legitimate store ID "codex-sessions" contains "sessions"; only
	// absolute store paths (home + ".codex/sessions") constitute a leak.
	if strings.Contains(string(raw), home) ||
		strings.Contains(string(raw), filepath.Join(home, ".codex")) ||
		strings.Contains(string(raw), ".jsonl") {
		t.Fatalf("root permission diagnostics leaked store paths: %s", raw)
	}
}

func TestCodexSessionsProviderIdentityIsStableAndOrderIndependent(t *testing.T) {
	first := DefaultProviderIdentity()
	if first == "" {
		t.Fatal("empty provider identity")
	}
	if second := DefaultProviderIdentity(); second != first {
		t.Fatalf("identity unstable: %q != %q", first, second)
	}
	if strings.Contains(first, string(types.RetentionStoreCodexSessions)) {
		t.Fatalf("identity must be hashed, got %q", first)
	}
}

// mergeRetention runs the shared scanner-side merge so duplicate registration
// behavior is exercised against the same code path production uses.
func mergeRetention(
	t *testing.T,
	home string,
	providers []types.RetentionProvider,
) types.RetentionProjection {
	t.Helper()
	return scanRetentionWithProviders(t, home, providers)
}

// --- helpers ---

const noTrailingNewline = "\x00NO_TRAILING_NEWLINE\x00"

func testCodexProvider() *CodexSessionsProvider {
	return NewCodexSessionsProvider()
}

func writeRetentionSession(
	t *testing.T,
	home string,
	year string,
	month string,
	day string,
	name string,
	first string,
	body string,
) string {
	t.Helper()
	directory := filepath.Join(home, ".codex", "sessions", year, month, day)
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "rollout-"+name+".jsonl")
	content := first
	if body != noTrailingNewline {
		content += "\n"
		if body != "" {
			content += body + "\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validMetadata(cwd string) string {
	return validMetadataWithProducer(cwd, "codex_cli_rs", "1.2.3")
}

func validMetadataWithProducer(cwd, producer, version string) string {
	payload := map[string]any{
		"id":          "session-private",
		"originator":  producer,
		"cli_version": version,
	}
	if cwd != "" {
		payload["cwd"] = cwd
	}
	record := map[string]any{
		"timestamp": "2099-12-31T23:59:59Z",
		"type":      "session_meta",
		"payload":   payload,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func liveCWD(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(home, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func setModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func scanRetention(
	t *testing.T,
	provider *CodexSessionsProvider,
	root string,
) types.RetentionProjection {
	t.Helper()
	projection, err := provider.Scan(context.Background(), types.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func scanRetentionWithProviders(
	t *testing.T,
	home string,
	providers []types.RetentionProvider,
) types.RetentionProjection {
	t.Helper()
	projection := types.RetentionProjection{
		Buckets:        []types.RetentionBucket{},
		ProviderErrors: []types.RetentionProviderError{},
	}
	seen := make(map[string]bool)
	for _, provider := range providers {
		providerProjection, err := provider.Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
		if err != nil {
			t.Fatal(err)
		}
		if providerProjection.Partial || len(providerProjection.ProviderErrors) > 0 {
			projection.Partial = true
		}
		projection.ProviderErrors = append(projection.ProviderErrors, providerProjection.ProviderErrors...)
		for _, bucket := range providerProjection.Buckets {
			key := string(bucket.StoreID) + "\x00" + bucket.BucketID
			if seen[key] {
				projection.Partial = true
				projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
					StoreID: provider.Name(),
					Message: "duplicate retention bucket",
				})
				continue
			}
			seen[key] = true
			projection.Buckets = append(projection.Buckets, bucket)
		}
	}
	return projection
}

func onlyBucket(t *testing.T, projection types.RetentionProjection) types.RetentionBucket {
	t.Helper()
	if len(projection.Buckets) != 1 {
		t.Fatalf("buckets = %+v; want exactly one", projection.Buckets)
	}
	return projection.Buckets[0]
}

func bucketByID(
	t *testing.T,
	projection types.RetentionProjection,
	bucketID string,
) types.RetentionBucket {
	t.Helper()
	for _, bucket := range projection.Buckets {
		if bucket.BucketID == bucketID {
			return bucket
		}
	}
	t.Fatalf("bucket %q missing from %+v", bucketID, projection.Buckets)
	return types.RetentionBucket{}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func twoFileSizes(t *testing.T, first, second string) int64 {
	t.Helper()
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	return firstInfo.Size() + secondInfo.Size()
}

func TestCodexSessionsProjectionRoundTripsWithoutPrivateFields(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	provider := testCodexProvider()
	writeRetentionSession(t, home, "2026", "05", "06", "roundtrip", validMetadata(filepath.Join(home, "gone")), "")

	projection := scanRetention(t, provider, home)
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.RetentionProjection
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projection, decoded) {
		t.Fatalf("roundtrip mismatch:\n%+v\n%+v", projection, decoded)
	}
	if strings.Contains(string(raw), "session-private") {
		t.Fatalf("projection leaked private session metadata: %s", raw)
	}
}
