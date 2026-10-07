package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCWDMetadataExtractorAmbiguousRecords(t *testing.T) {
	cwd := mustMarshalJSON(t, filepath.Join(t.TempDir(), "owner"))
	otherCWD := mustMarshalJSON(t, filepath.Join(t.TempDir(), "different"))
	for keyIndex, keys := range [][2]string{
		{`"cwd"`, `"cwd"`},
		{`"cwd"`, `"\u0063wd"`},
		{`"c\u0077d"`, `"cwd"`},
		{`"\u0063\u0077\u0064"`, `"cw\u0064"`},
	} {
		for valueIndex, second := range []string{cwd, otherCWD, `"relative"`, `null`, `{}`, `[]`} {
			record := fmt.Sprintf(`{%s:%s,"message":"%s",%s:%s}`, keys[0], cwd,
				strings.Repeat("x", 8192), keys[1], second)
			for _, size := range []int{1, 2, 3, 7, 4096, len(record)} {
				t.Run(fmt.Sprintf("keys-%d/value-%d/fragment-%d", keyIndex, valueIndex, size), func(t *testing.T) {
					var extractor cwdMetadataExtractor
					found := false
					for offset := 0; offset < len(record); offset += size {
						if extractor.feed([]byte(record[offset:min(offset+size, len(record))])) != "" {
							found = true
						}
					}
					if !extractor.unverifiableRecord(found) {
						t.Fatal("duplicate cwd record was accepted as verifiable")
					}
				})
			}
		}
	}
}

func TestReadRecordedCWDsDuplicateEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cwd := mustMarshalJSON(t, filepath.Join(home, "owner"))
	path := filepath.Join(home, "session.jsonl")
	writeClaudeProjectSession(t, path, "{}\n"+
		`{"cwd":`+cwd+`,"message":"`+strings.Repeat("x", 8192)+`","cwd":`+cwd+"}\n{}\n")
	result, err := readRecordedCWDs(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.cwds) != 0 || result.unverifiableRecords != 1 || result.firstUnverifiableLine != 2 {
		t.Fatalf("duplicate evidence = %+v; want no cwd and one unverifiable record on line 2", result)
	}
}

func TestCWDMetadataExtractorReferenceDecoder(t *testing.T) {
	cwd := mustMarshalJSON(t, filepath.Join(t.TempDir(), "owner with spaces", "프로젝트"))
	limitCWD := cwd[:len(cwd)-1] + strings.Repeat("x", maxRecordedCWDBytes-len(cwd)) + `"`
	var records []string
	for mask := 0; mask < 8; mask++ {
		var key strings.Builder
		for i, b := range []byte("cwd") {
			if mask&(1<<i) != 0 {
				fmt.Fprintf(&key, `\u%04x`, b)
			} else {
				key.WriteByte(b)
			}
		}
		records = append(records, `{"`+key.String()+`":`+cwd+`}`)
	}
	records = append(records,
		` { "message": [true, false, null, -1.2e+3, {"cwd":"nested"}], "cwd": `+cwd+` } `,
		`{"\u0063wd":`+cwd+`,"nested":{"cwd":null,"cwd":"nested"}}`,
		`{"cwd":`+cwd+`,"message":"`+strings.Repeat("x", 128*1024)+`"}`,
		`{"message":"`+strings.Repeat("x", 128*1024)+`","\u0063wd":`+cwd+`}`,
		`{"cwd":"relative"}`, `{"\u0063wd":null}`, `{"cw\u0064":42}`,
		`{"c\u0077d":{}}`, `{"\u0063wd":[]}`,
		`{"cw\u0064x":`+cwd+`}`, `{"c\u0057d":`+cwd+`}`,
		`{"\uD83D\uDE00":`+cwd+`}`, `{"\uD800":`+cwd+`}`,
		`{"c\nwd":`+cwd+`}`, `{"c\/wd":`+cwd+`}`, `{"c\\wd":`+cwd+`}`,
		`{"c\"wd":`+cwd+`}`, `{"c\bwd":`+cwd+`}`, `{"c\fwd":`+cwd+`}`,
		`{"c\rwd":`+cwd+`}`, `{"c\twd":`+cwd+`}`,
		`{"\u0163wd":`+cwd+`}`, `{"c\u0177d":`+cwd+`}`, `{"cw\u0164":`+cwd+`}`,
		`{"cwd":`+strings.ReplaceAll(cwd, "/", `\/`)+`}`,
		`{"cwd":`+limitCWD+`}`, `{"cwd":`+limitCWD[:len(limitCWD)-1]+`x"}`,
		`{"`+strings.Repeat("x", 128*1024)+`\u0063wd":`+cwd+`}`,
		`{"message":{"\u0063wd":`+cwd+`}}`, `{}`,
	)
	for index, record := range records {
		want, count := referenceCWD(t, []byte(record))
		for _, size := range []int{1, 2, 3, 5, 7, 4096, len(record)} {
			t.Run(fmt.Sprintf("record-%d/fragment-%d", index, size), func(t *testing.T) {
				got, unverifiable := extractCWDFragments([]byte(record), size)
				if got != want || unverifiable != (count == 1 && want == "") {
					t.Fatalf("extracted cwd = %q, unverifiable = %v; decoder cwd = %q, count = %d", got, unverifiable, want, count)
				}
			})
		}
	}
}

func TestCWDMetadataExtractorFailClosed(t *testing.T) {
	cwd := mustMarshalJSON(t, filepath.Join(t.TempDir(), "owner"))
	for index, record := range []string{
		`{"\u0063wd":` + cwd, `{"cwd":` + cwd + `,"message":`,
		`{"cwd":` + cwd + `,}`, `{"cwd":` + cwd + `} {}`,
		`{"cwd":` + cwd + `,"message":"\q"}`,
		`{"cw\u00Xd":` + cwd + `}`, `{"cw\u006":` + cwd + `}`,
		`{"cw\q":` + cwd + `}`, `{"cwd":"\uXXXX"}`,
		`{"cwd":` + cwd[:len(cwd)-1] + strings.Repeat("x", maxRecordedCWDBytes) + `"}`,
		`{"cwd":` + cwd + `,"message":[1,]}`, `{"cwd":` + cwd + `,"message":01}`,
	} {
		for _, size := range []int{1, 2, 7, 4096, len(record)} {
			t.Run(fmt.Sprintf("record-%d/fragment-%d", index, size), func(t *testing.T) {
				if _, unverifiable := extractCWDFragments([]byte(record), size); !unverifiable {
					t.Fatal("malformed, truncated, or oversized cwd record was verifiable")
				}
			})
		}
	}
}

func extractCWDFragments(record []byte, size int) (string, bool) {
	var extractor cwdMetadataExtractor
	var cwd string
	for offset := 0; offset < len(record); offset += size {
		if found := extractor.feed(record[offset:min(offset+size, len(record))]); cwd == "" {
			cwd = found
		}
	}
	return cwd, extractor.unverifiableRecord(cwd != "")
}

// The decoder defines JSON key identity; duplicate-cwd policy is checked
// separately because decoding an object into a map would select the last value.
func referenceCWD(t *testing.T, record []byte) (string, int) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(record))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		t.Fatalf("reference requires a valid JSON object: %v", err)
	}
	var cwd string
	count := 0
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if key == "cwd" {
			count++
			var decoded string
			if len(value) <= maxRecordedCWDBytes && json.Unmarshal(value, &decoded) == nil && filepath.IsAbs(decoded) {
				cwd = filepath.Clean(decoded)
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	return cwd, count
}

func FuzzCWDMetadataExtractor(f *testing.F) {
	cwdJSON, err := json.Marshal(filepath.Join(f.TempDir(), "owner"))
	if err != nil {
		f.Fatal(err)
	}
	cwd := string(cwdJSON)
	for _, record := range []string{
		`{"cwd":` + cwd + `}`, `{"\u0063\u0077\u0064":` + cwd + `}`,
		`{"cwd":` + cwd + `,"cwd":` + cwd + `}`,
		`{"cwd":` + cwd + `,"c\u0077d":"relative"}`,
		`{"\u0063wd":null,"cwd":` + cwd + `}`, `{"cwd":` + cwd,
		`{"message":{"cwd":` + cwd + `}}`, `{"cwd":"\q"}`,
		`{"cwd":` + cwd + `,"message":"\uXXXX"}`,
		`{"cw\u00Xd":` + cwd + `}`, `{"message":[true,null,-1.25e+2]}`,
		`{"cw\u0064x":` + cwd + `}`, `{"c\nwd":` + cwd + `}`, `{}`, "",
	} {
		for _, size := range []uint16{1, 7, 4096} {
			f.Add([]byte(record), size)
		}
	}
	f.Fuzz(func(t *testing.T, record []byte, fragment uint16) {
		// Bound input, nesting, and work per iteration independently of the
		// generated fragment size. Ordinary go test runs every seed above.
		if len(record) > 16*1024 {
			t.Skip()
		}
		got, unverifiable := extractCWDFragments(record, 1+int(fragment)%4096)
		trimmed := bytes.TrimSpace(record)
		if !json.Valid(record) || len(trimmed) == 0 || trimmed[0] != '{' {
			if len(bytes.Trim(record, " \t\r\n")) > 0 && !unverifiable {
				t.Fatal("invalid object was accepted as verifiable")
			}
			return
		}
		want, count := referenceCWD(t, record)
		if count > 1 {
			if !unverifiable {
				t.Fatal("duplicate cwd record was accepted as verifiable")
			}
			return
		}
		if got != want || unverifiable != (count == 1 && want == "") {
			t.Fatalf("cwd = %q, unverifiable = %v; want %q, count = %d", got, unverifiable, want, count)
		}
	})
}
