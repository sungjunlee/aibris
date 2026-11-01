package adapter

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
		{"one session corroborates, another lacks context", grokEntry(t, home, filepath.Join(home, "work", "mixed"), filepath.Join(home, "work", "mixed"), ""), types.EntryClassUndetermined},
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

// grokContextJSON builds one prompt_context.json object. Keys are raw JSON
// key text, so callers can pass unicode escapes; values are Go strings.
func grokContextJSON(t *testing.T, kv ...string) string {
	t.Helper()
	if len(kv)%2 != 0 {
		t.Fatal("odd key/value count")
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(kv[i])
		b.WriteString(`":`)
		encoded, err := json.Marshal(kv[i+1])
		if err != nil {
			t.Fatal(err)
		}
		b.Write(encoded)
	}
	b.WriteByte('}')
	return b.String()
}

func TestGrokPromptContextCWDRejectsDuplicateOwnershipKeys(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live")
	gone := filepath.Join(dir, "gone")
	note := strings.Repeat("x", 8192)

	accept := []struct {
		name string
		body string
		want string
	}{
		{"single key", grokContextJSON(t, "working_directory", gone), gone},
		{"version plus one key", `{"version":1,"working_directory":` + quoteJSON(gone) + `}`, gone},
		{"escaped single key", grokContextJSON(t, `\u0077orking_directory`, gone), gone},
		{"case-alias single key", grokContextJSON(t, "Working_Directory", gone), gone},
		{"duplicate non-ownership keys", grokContextJSON(t, "version", "1", "version", "2", "working_directory", gone), gone},
		{
			"ownership key named inside a string",
			grokContextJSON(t, "note", "working_directory", "working_directory", gone),
			gone,
		},
		{
			"nested duplicate does not count",
			`{"extra":{"working_directory":` + quoteJSON(live) + `,"working_directory":` + quoteJSON(live) + `},"working_directory":` + quoteJSON(gone) + `}`,
			gone,
		},
	}
	for _, tt := range accept {
		t.Run("accept/"+tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prompt_context.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := grokPromptContextCWD(path)
			if err != nil || got != tt.want {
				t.Fatalf("grokPromptContextCWD() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}

	reject := []struct {
		name string
		body string
	}{
		{"live then missing", grokContextJSON(t, "working_directory", live, "working_directory", gone)},
		{"missing then live", grokContextJSON(t, "working_directory", gone, "working_directory", live)},
		{"same value twice", grokContextJSON(t, "working_directory", gone, "working_directory", gone)},
		{"escaped then literal", grokContextJSON(t, `\u0077orking_directory`, live, "working_directory", gone)},
		{"literal then escaped", grokContextJSON(t, "working_directory", live, `\u0077orking_directory`, gone)},
		{"fully escaped duplicate", grokContextJSON(t, `\u0077orking_directory`, live, `\u0077orking_directory`, gone)},
		{"case alias then canonical", grokContextJSON(t, "Working_Directory", live, "working_directory", gone)},
		{"canonical then case alias", grokContextJSON(t, "working_directory", live, "WORKING_DIRECTORY", gone)},
		{"two case aliases", grokContextJSON(t, "Working_Directory", live, "WORKING_DIRECTORY", gone)},
		{"escaped case alias", grokContextJSON(t, `\u0057orking_directory`, live, "working_directory", gone)},
		{"kelvin alias", grokContextJSON(t, `wor\u212Aing_directory`, live, "working_directory", gone)},
		{"relative then absolute", grokContextJSON(t, "working_directory", "relative", "working_directory", gone)},
		{"number then absolute", `{"working_directory":1,"working_directory":` + quoteJSON(gone) + `}`},
		{
			"long string between duplicates",
			`{"working_directory":` + quoteJSON(live) + `,"note":"` + note + `","working_directory":` + quoteJSON(gone) + `}`,
		},
	}
	for _, tt := range reject {
		t.Run("reject/"+tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prompt_context.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := grokPromptContextCWD(path)
			if err == nil {
				t.Fatalf("duplicate ownership accepted as %q", got)
			}
		})
	}
}

func TestGrokDuplicateOwnershipKeysStayUndetermined(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, "work"), 0o755); err != nil {
		t.Fatal(err)
	}

	type ownershipCase struct {
		name        string
		nameMatches string
		body        func(live, gone string) string
		want        types.EntryClass
	}
	cases := []ownershipCase{
		{
			name:        "live then missing, name is missing",
			nameMatches: "gone",
			body: func(live, gone string) string {
				return grokContextJSON(t, "working_directory", live, "working_directory", gone)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "missing then live, name is missing",
			nameMatches: "gone",
			body: func(live, gone string) string {
				return grokContextJSON(t, "working_directory", gone, "working_directory", live)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "missing then live, name is live",
			nameMatches: "live",
			body: func(live, gone string) string {
				return grokContextJSON(t, "working_directory", gone, "working_directory", live)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "live then missing, name is live",
			nameMatches: "live",
			body: func(live, gone string) string {
				return grokContextJSON(t, "working_directory", live, "working_directory", gone)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "escaped duplicate",
			nameMatches: "gone",
			body: func(live, gone string) string {
				return grokContextJSON(t, "working_directory", live, `\u0077orking_directory`, gone)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "case-alias duplicate",
			nameMatches: "gone",
			body: func(live, gone string) string {
				return grokContextJSON(t, "WORKING_DIRECTORY", live, "working_directory", gone)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "identical duplicate values",
			nameMatches: "gone",
			body: func(_, gone string) string {
				return grokContextJSON(t, "working_directory", gone, "working_directory", gone)
			},
			want: types.EntryClassUndetermined,
		},
		{
			name:        "single key orphan",
			nameMatches: "gone",
			body: func(_, gone string) string {
				return grokContextJSON(t, "version", "1", "working_directory", gone)
			},
			want: types.EntryClassOrphaned,
		},
		{
			name:        "single escaped key orphan",
			nameMatches: "gone",
			body: func(_, gone string) string {
				return grokContextJSON(t, `\u0077orking_directory`, gone)
			},
			want: types.EntryClassOrphaned,
		},
	}

	adapter := &GrokSessionsAdapter{}
	entries := make([]string, len(cases))
	for i, tt := range cases {
		live := filepath.Join(home, "work", "live-"+tt.name)
		gone := filepath.Join(home, "work", "gone-"+tt.name)
		if err := os.MkdirAll(live, 0o755); err != nil {
			t.Fatal(err)
		}
		encoded := gone
		if tt.nameMatches == "live" {
			encoded = live
		}
		entry := grokEntry(t, home, encoded, encoded)
		matches, err := filepath.Glob(filepath.Join(entry, "*", "prompt_context.json"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("%s: prompt contexts = %v, %v", tt.name, matches, err)
		}
		if err := os.WriteFile(matches[0], []byte(tt.body(live, gone)), 0o600); err != nil {
			t.Fatal(err)
		}
		entries[i] = entry
	}

	results, err := adapter.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]types.DebrisInfo{}
	for _, item := range results {
		byPath[item.Path] = item
	}
	for i, tt := range cases {
		item, ok := byPath[entries[i]]
		if !ok {
			t.Errorf("%s: entry not scanned", tt.name)
			continue
		}
		if item.Classification != tt.want {
			t.Errorf("%s: classification = %s (%s); want %s", tt.name, item.Classification, item.Reason, tt.want)
		}
		revalidated, err := adapter.RevalidateAgentState(context.Background(), entries[i])
		if err != nil || revalidated != tt.want {
			t.Errorf("%s: revalidated = %s, %v; want %s", tt.name, revalidated, err, tt.want)
		}
	}
}
