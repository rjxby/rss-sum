package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestGenProxyRequestMatchesReviewedConsumerContract(t *testing.T) {
	fixture, err := os.ReadFile("testdata/gen-proxy-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal(fixture, &expected); err != nil {
		t.Fatal(err)
	}
	const key = "PRIVATE_CONTRACT_KEY"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.Header.Get("x-api-key") != key {
			t.Error("provider request transport or authentication contract differs")
		}
		var actual any
		if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(expected, actual) {
			t.Error("gen-proxy consumer request differs from reviewed JSON fixture")
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"output_text":"{\"summary\":\"Contract summary.\"}"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider := newGenProxyProvider(&Settings{GenProxyBaseURL: server.URL, GenProxyModel: "contract-model", GenProxyAPIKey: key, RequestTimeoutInSeconds: 5})
	summary, err := provider.Generate(context.Background(), generationRequest{SystemPrompt: "system constraint", UserPrompt: "article text", Format: summaryOutputFormat()})
	if err != nil || summary != "Contract summary." {
		t.Fatalf("provider could not read the consumer response contract: summary=%q error=%v", summary, err)
	}
}
