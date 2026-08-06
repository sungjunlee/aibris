package codexsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFirstMetadataAdmitsOnlyApprovedFirstRecordScalars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-valid.jsonl")
	first := `{"timestamp":"2026-07-01T00:00:00Z","type":"session_meta","private":"DO-NOT-RETAIN","payload":{"id":"session-secret","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"1.2.3","source":"cli","nested":{"text":"DO-NOT-RETAIN"}}}`
	body := `{"type":"session_meta","payload":{"cwd":"/body/fake","cli_version":"999.0.0","text":"BODY-SECRET"}}`
	if err := os.WriteFile(path, []byte(first+"\n"+body+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	metadata, err := ReadFirstMetadata(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CWD != "/tmp/project" || !metadata.HasSessionIdentity {
		t.Fatalf("metadata = %+v; want approved first-record fields", metadata)
	}
	if metadata.Producer != "codex_cli_rs" || metadata.Version != "1.2.3" {
		t.Fatalf("producer/version = %q/%q", metadata.Producer, metadata.Version)
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"session-secret",
		"DO-NOT-RETAIN",
		"BODY-SECRET",
		"/body/fake",
		"999.0.0",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("parsed metadata retained forbidden value %q: %s", forbidden, raw)
		}
	}
}

func TestReadFirstMetadataFailsClosedForRecordBoundariesAndShapes(t *testing.T) {
	valid := `{"type":"session_meta","payload":{"id":"id","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"1.2.3"}}`
	tests := []struct {
		name    string
		content string
		kind    ErrorKind
	}{
		{name: "empty", content: "", kind: ErrorEmpty},
		{name: "empty record", content: "\n", kind: ErrorEmpty},
		{name: "truncated without newline", content: valid, kind: ErrorTruncated},
		{name: "malformed", content: `{"type":` + "\n", kind: ErrorMalformed},
		{name: "wrong first record", content: `{"type":"message"}` + "\n" + valid + "\n", kind: ErrorWrongRecord},
		{name: "duplicate type", content: `{"type":"message","type":"session_meta","payload":{}}` + "\n", kind: ErrorAmbiguous},
		{name: "duplicate cwd", content: `{"type":"session_meta","payload":{"cwd":"/a","cwd":"/b"}}` + "\n", kind: ErrorAmbiguous},
		{name: "nonscalar cwd", content: `{"type":"session_meta","payload":{"cwd":["/tmp"]}}` + "\n", kind: ErrorInvalidField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout-test.jsonl")
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := ReadFirstMetadata(context.Background(), path)
			kind, ok := ErrorKindOf(err)
			if !ok || kind != tt.kind {
				t.Fatalf("error = %v (%q); want kind %q", err, kind, tt.kind)
			}
		})
	}
}

func TestReadFirstMetadataRejectsMalformedUnicode(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{
			name: "invalid utf8",
			content: append(
				[]byte(`{"type":"session_meta","payload":{"cwd":"`),
				append([]byte{0xff}, []byte(`","originator":"codex_cli_rs","cli_version":"1.2.3"}}`+"\n")...)...,
			),
		},
		{
			name:    "unpaired utf16 surrogate",
			content: []byte(`{"type":"session_meta","payload":{"cwd":"\ud800","originator":"codex_cli_rs","cli_version":"1.2.3"}}` + "\n"),
		},
		{
			name:    "unpaired low utf16 surrogate",
			content: []byte(`{"type":"session_meta","payload":{"cwd":"\udc00","originator":"codex_cli_rs","cli_version":"1.2.3"}}` + "\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout-malformed-unicode.jsonl")
			if err := os.WriteFile(path, tt.content, 0600); err != nil {
				t.Fatal(err)
			}
			_, err := ReadFirstMetadata(context.Background(), path)
			kind, ok := ErrorKindOf(err)
			if !ok || kind != ErrorMalformed {
				t.Fatalf("error = %v (%q); want malformed", err, kind)
			}
		})
	}
}

func TestReadFirstMetadataAllowsValidUnicodeAndEscapedSurrogateText(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
	}{
		{name: "valid surrogate pair", cwd: `\ud83d\ude00`},
		{name: "literal replacement character", cwd: "\ufffd"},
		{name: "escaped replacement character", cwd: `\ufffd`},
		{name: "escaped backslash surrogate text", cwd: `\\ud800`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout-valid-unicode.jsonl")
			record := `{"type":"session_meta","payload":{"cwd":"` + tt.cwd +
				`","originator":"codex_cli_rs","cli_version":"1.2.3"}}` + "\n"
			if err := os.WriteFile(path, []byte(record), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadFirstMetadata(context.Background(), path); err != nil {
				t.Fatalf("valid Unicode rejected: %v", err)
			}
		})
	}
}

func TestReadFirstMetadataRejectsOversizedFirstRecordWithoutUsingBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-oversized.jsonl")
	oversized := `{"type":"session_meta","padding":"` +
		strings.Repeat("x", MaxFirstRecordBytes) +
		`","payload":{"cwd":"/oversized/fake"}}` + "\n"
	body := `{"type":"session_meta","payload":{"cwd":"/body/fake","originator":"codex_cli_rs","cli_version":"1.2.3"}}` + "\n"
	if err := os.WriteFile(path, []byte(oversized+body), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := ReadFirstMetadata(context.Background(), path)
	kind, ok := ErrorKindOf(err)
	if !ok || kind != ErrorOversized {
		t.Fatalf("error = %v (%q); want oversized", err, kind)
	}
}

func TestReadFirstMetadataHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ReadFirstMetadata(ctx, filepath.Join(t.TempDir(), "unused"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled", err)
	}
}
