package adapter

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// grokEntry creates ~/.grok/sessions/<url-encoded cwd> with one session per
// working_directory given (an empty string writes no prompt_context.json).
func grokEntry(t *testing.T, home, cwd string, sessionCWDs ...string) string {
	t.Helper()
	entry := filepath.Join(home, ".grok", "sessions", url.PathEscape(cwd))
	if err := os.MkdirAll(entry, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry, "prompt_history.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, sessionCWD := range sessionCWDs {
		session := filepath.Join(entry, "01a0000"+string(rune('a'+i)))
		if err := os.MkdirAll(session, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(session, "chat_history.jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if sessionCWD == "" {
			continue
		}
		body := `{"version":1,"working_directory":` + quoteJSON(sessionCWD) + `}`
		if err := os.WriteFile(filepath.Join(session, "prompt_context.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return entry
}

func quoteJSON(s string) string {
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		default:
			out = append(out, string(r)...)
		}
	}
	return string(append(out, '"'))
}

func TestGrokSessionsClassification(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	live := filepath.Join(home, "work", "live-project")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(home, "work", "gone-project")
	if err := os.MkdirAll(filepath.Join(home, "work"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want types.EntryClass
	}{
		{"recorded project gone", grokEntry(t, home, gone, gone, gone), types.EntryClassOrphaned},
		{"recorded project exists", grokEntry(t, home, live, live), types.EntryClassLive},
		{"session disagrees with the name", grokEntry(t, home, filepath.Join(home, "work", "renamed"), gone), types.EntryClassUndetermined},
		{"no session corroborates the name", grokEntry(t, home, filepath.Join(home, "work", "only-history")), types.EntryClassUndetermined},
		{"session without context", grokEntry(t, home, filepath.Join(home, "work", "no-context"), ""), types.EntryClassUndetermined},
	}
	malformed := filepath.Join(home, ".grok", "sessions", "not-an-encoded-path")
	if err := os.MkdirAll(malformed, 0o700); err != nil {
		t.Fatal(err)
	}
	tests = append(tests, struct {
		name string
		path string
		want types.EntryClass
	}{"entry name is not an encoded absolute path", malformed, types.EntryClassUndetermined})

	results, err := (&GrokSessionsAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]types.DebrisInfo{}
	for _, item := range results {
		byPath[item.Path] = item
		if item.Tool != types.ToolGrok || item.Category != types.CategoryAgentState {
			t.Errorf("%s: tool/category = %s/%s", item.Path, item.Tool, item.Category)
		}
	}
	adapter := &GrokSessionsAdapter{}
	for _, tt := range tests {
		item, ok := byPath[tt.path]
		if !ok {
			t.Errorf("%s: entry not scanned", tt.name)
			continue
		}
		if item.Classification != tt.want {
			t.Errorf("%s: classification = %s (%s); want %s", tt.name, item.Classification, item.Reason, tt.want)
		}
		revalidated, err := adapter.RevalidateAgentState(context.Background(), tt.path)
		if err != nil || revalidated != tt.want {
			t.Errorf("%s: revalidated = %s, %v; want %s", tt.name, revalidated, err, tt.want)
		}
	}
	if got := byPath[tests[0].path].Project; got != "gone-project" {
		t.Errorf("orphaned project = %q; want gone-project", got)
	}
}

func TestGrokSessionsRegisteredAsAgentStateStore(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	roots, err := AgentStateStoreRoots()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".grok", "sessions")
	found := false
	for _, root := range roots {
		found = found || root == want
	}
	if !found {
		t.Fatalf("AgentStateStoreRoots() = %v; missing %s", roots, want)
	}
	if _, err := NewAgentStateRevalidators(DefaultProviders()).Registration(types.ToolGrok); err != nil {
		t.Fatalf("grok revalidator not registered: %v", err)
	}
}
