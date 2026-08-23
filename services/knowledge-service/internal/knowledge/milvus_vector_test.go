package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMilvusVectorStoreUpsertsChunkMetadata(t *testing.T) {
	client := &fakeMilvusClient{}
	store, err := NewMilvusVectorStore(MilvusConfig{Collection: "knowledge_chunks", Dimension: 2}, client)
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpsertChunks(t.Context(), VectorUpsertInput{Model: "text-embedding-v4", Chunks: []VectorChunk{{
		Chunk:  Chunk{ID: 11, DocumentID: 123, ProductID: 1001, DocType: DocFAQ, Version: 2},
		Vector: []float32{0.1, 0.2}, VectorID: "knowledge_chunk_11",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.rows) != 1 {
		t.Fatalf("rows=%#v", client.rows)
	}
	row := client.rows[0]
	if row.VectorID != "knowledge_chunk_11" || row.ChunkID != 11 || row.ProductID != 1001 || row.DocType != "FAQ" || row.Version != 2 || row.IsCurrentReady != true || row.EmbeddingModel != "text-embedding-v4" {
		t.Fatalf("row=%#v", row)
	}
}

func TestMilvusVectorStoreRejectsWrongDimension(t *testing.T) {
	store, err := NewMilvusVectorStore(MilvusConfig{Collection: "knowledge_chunks", Dimension: 1024}, &fakeMilvusClient{})
	if err != nil {
		t.Fatal(err)
	}
	err = store.UpsertChunks(t.Context(), VectorUpsertInput{Model: "text-embedding-v4", Chunks: []VectorChunk{{Chunk: Chunk{ID: 11}, Vector: []float32{0.1}, VectorID: "knowledge_chunk_11"}}})
	if err == nil {
		t.Fatal("UpsertChunks() error = nil, want dimension mismatch")
	}
}

func TestMilvusVectorStoreSearchBuildsCurrentReadyFilter(t *testing.T) {
	client := &fakeMilvusClient{hits: []milvusSearchHit{{ChunkID: 11, Score: 0.91}}}
	store, err := NewMilvusVectorStore(MilvusConfig{Collection: "knowledge_chunks", Dimension: 2}, client)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Search(t.Context(), VectorSearchInput{
		ProductID: 1001,
		Query:     []float32{0.1, 0.2},
		TopK:      5,
		Filters:   []VectorDocumentFilter{{DocumentID: 123, DocType: DocFAQ, Version: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.search.Collection != "knowledge_chunks" || client.search.Filter != "product_id == 1001 && is_current_ready == true && document_id in [123]" {
		t.Fatalf("search=%#v", client.search)
	}
	if len(got) != 1 || got[0].ChunkID != 11 || got[0].Score != 0.91 {
		t.Fatalf("hits=%#v", got)
	}
}

func TestMilvusRESTVectorStoreSearchUsesVectorBatchPayload(t *testing.T) {
	var gotPath string
	var gotRequest milvusRESTSearchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(milvusRESTSearchResponse{Data: []struct {
			ChunkID  uint64  `json:"chunk_id"`
			Distance float64 `json:"distance"`
		}{{ChunkID: 11, Distance: 0.91}}})
	}))
	defer server.Close()

	store, err := NewMilvusRESTVectorStore(MilvusConfig{Address: server.URL, Collection: "knowledge_chunks", Dimension: 2}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Search(t.Context(), VectorSearchInput{ProductID: 1001, Query: []float32{0.1, 0.2}, TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/vectordb/entities/search" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotRequest.CollectionName != "knowledge_chunks" || gotRequest.AnnsField != "embedding" || len(gotRequest.Data) != 1 || len(gotRequest.Data[0]) != 2 {
		t.Fatalf("request=%#v", gotRequest)
	}
	if len(got) != 1 || got[0].ChunkID != 11 {
		t.Fatalf("hits=%#v", got)
	}
}

type fakeMilvusClient struct {
	rows   []milvusVectorRow
	search milvusSearchRequest
	hits   []milvusSearchHit
}

func (c *fakeMilvusClient) UpsertRows(_ context.Context, collection string, rows []milvusVectorRow) error {
	for i := range rows {
		rows[i].Collection = collection
	}
	c.rows = append([]milvusVectorRow(nil), rows...)
	return nil
}

func (c *fakeMilvusClient) SearchRows(_ context.Context, request milvusSearchRequest) ([]milvusSearchHit, error) {
	c.search = request
	return append([]milvusSearchHit(nil), c.hits...), nil
}
