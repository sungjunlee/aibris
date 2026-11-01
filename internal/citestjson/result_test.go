package citestjson

import (
	"errors"
	"strings"
	"testing"
)

func TestSummarizeDistinguishesRunSkipAndEmptySelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantErr  error
		selected int
		passed   int
		failed   int
		skipped  int
	}{
		{
			name: "empty selection is not success",
			input: strings.Join([]string{
				`{"Action":"start","Package":"example.com/pkg"}`,
				`{"Action":"output","Package":"example.com/pkg","Output":"testing: warning: no tests to run\n"}`,
				`{"Action":"output","Package":"example.com/pkg","Output":"PASS\n"}`,
				`{"Action":"output","Package":"example.com/pkg","Output":"ok  \texample.com/pkg\t0.001s [no tests to run]\n"}`,
				`{"Action":"pass","Package":"example.com/pkg"}`,
			}, "\n"),
			wantErr: ErrNoTests,
		},
		{
			name: "skip is not a pass",
			input: strings.Join([]string{
				`{"Action":"run","Test":"TestPass"}`,
				`{"Action":"pass","Test":"TestPass"}`,
				`{"Action":"run","Test":"TestSkip"}`,
				`{"Action":"skip","Test":"TestSkip"}`,
				`{"Action":"run","Test":"TestSub"}`,
				`{"Action":"run","Test":"TestSub/inner"}`,
				`{"Action":"skip","Test":"TestSub/inner"}`,
				`{"Action":"pass","Test":"TestSub"}`,
				`{"Action":"pass","Package":"example.com/pkg"}`,
			}, "\n"),
			selected: 4,
			passed:   2,
			skipped:  2,
		},
		{
			name: "failure",
			input: strings.Join([]string{
				`{"Action":"run","Test":"TestFail"}`,
				`{"Action":"fail","Test":"TestFail"}`,
				`{"Action":"fail","Package":"example.com/pkg"}`,
			}, "\n"),
			wantErr:  ErrFailed,
			selected: 1,
			failed:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			summary, err := Summarize(strings.NewReader(tt.input))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Summarize error = %v; want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Summarize: %v", err)
			}
			if summary.Selected != tt.selected || summary.Passed != tt.passed || summary.Failed != tt.failed || summary.Skipped != tt.skipped {
				t.Fatalf("summary = %+v", summary)
			}
			text := summary.String()
			if !strings.Contains(text, "passed=") || !strings.Contains(text, "skipped=") {
				t.Fatalf("summary %q does not report passes and skips separately", text)
			}
		})
	}
}

func TestSummarizeRejectsNonJSON(t *testing.T) {
	t.Parallel()
	_, err := Summarize(strings.NewReader("# pkg\n./file.go:1:1: syntax error\n"))
	if err == nil {
		t.Fatal("non-JSON stdout was accepted")
	}
}
