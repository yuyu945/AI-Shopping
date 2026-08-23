package knowledge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashScopeEmbeddingProviderEmbedsDocuments(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody dashScopeEmbeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(dashScopeEmbeddingResponse{Output: dashScopeEmbeddingOutput{Embeddings: []dashScopeEmbedding{{Embedding: []float32{0.1, 0.2}}, {Embedding: []float32{0.3, 0.4}}}}})
	}))
	defer server.Close()

	provider, err := NewDashScopeEmbeddingProvider(DashScopeConfig{Endpoint: server.URL, APIKey: "secret", Model: "text-embedding-v4", Dimension: 1024, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.EmbedDocuments(t.Context(), EmbeddingInput{Model: "text-embedding-v4", Texts: []string{"first", "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret" || gotPath != "/api/v1/services/embeddings/text-embedding/text-embedding" {
		t.Fatalf("auth=%q path=%q", gotAuth, gotPath)
	}
	if gotBody.Model != "text-embedding-v4" || gotBody.Parameters.Dimension != 1024 || len(gotBody.Input.Texts) != 2 {
		t.Fatalf("body=%#v", gotBody)
	}
	if len(got.Vectors) != 2 || got.Vectors[1][0] != 0.3 {
		t.Fatalf("vectors=%#v", got.Vectors)
	}
}

func TestDashScopeEmbeddingProviderRejectsTooManyTexts(t *testing.T) {
	provider, err := NewDashScopeEmbeddingProvider(DashScopeConfig{Endpoint: "http://example.invalid", APIKey: "secret", Model: "text-embedding-v4", Dimension: 1024, HTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.EmbedDocuments(t.Context(), EmbeddingInput{Model: "text-embedding-v4", Texts: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}})
	if err == nil {
		t.Fatal("EmbedDocuments() error = nil, want batch limit error")
	}
}
