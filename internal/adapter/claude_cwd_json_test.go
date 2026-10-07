package adapter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCWDMetadataExtractorAmbiguousRecords(t *testing.T) {
	cwd := mustMarshalJSON(t, filepath.Join(t.TempDir(), "owner"))
	for _, keys := range [][2]string{
		{`"cwd"`, `"cwd"`},
		{`"cwd"`, `"\u0063wd"`},
		{`"c\u0077d"`, `"cwd"`},
		{`"\u0063\u0077\u0064"`, `"cw\u0064"`},
	} {
		for _, second := range []string{cwd, `"relative"`, `null`, `{}`, `[]`} {
			record := fmt.Sprintf(`{%s:%s,"message":"%s",%s:%s}`, keys[0], cwd,
				strings.Repeat("x", 8192), keys[1], second)
			for _, size := range []int{1, 2, 3, 7, 4096, len(record)} {
				t.Run(fmt.Sprintf("%s-%s/%s/fragment-%d", keys[0], keys[1], second, size), func(t *testing.T) {
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
