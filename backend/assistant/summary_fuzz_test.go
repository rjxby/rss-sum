package assistant

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzParseSummaryOutput(f *testing.F) {
	for _, seed := range []string{`{"summary":"A useful summary."}`, `{"summary":"記事の要約。"}`, `{"summary":" "}`, `{"summary":null}`, `{}`, `[]`, `{"summary":42}`, `not json`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		summary, err := parseSummaryOutput("fuzz-provider", output)
		if err != nil {
			if summary != "" {
				t.Fatal("a rejected provider response returned usable summary text")
			}
			return
		}
		if strings.TrimSpace(summary) == "" {
			t.Fatal("accepted a blank summary")
		}
		var document struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(output), &document); err != nil {
			t.Fatal("accepted malformed JSON", err)
		}
		if summary != document.Summary {
			t.Fatal("accepted response did not preserve its summary field")
		}
	})
}
