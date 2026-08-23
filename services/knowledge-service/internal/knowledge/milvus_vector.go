package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type MilvusConfig struct {
	Address    string
	Collection string
	Dimension  int
}

type MilvusVectorStore struct {
	config MilvusConfig
	client milvusVectorClient
}

type milvusVectorClient interface {
	UpsertRows(context.Context, string, []milvusVectorRow) error
	SearchRows(context.Context, milvusSearchRequest) ([]milvusSearchHit, error)
}

type milvusVectorRow struct {
	Collection     string
	VectorID       string
	ChunkID        uint64
	ProductID      uint64
	DocumentID     uint64
	DocType        string
	Version        uint32
	IsCurrentReady bool
	EmbeddingModel string
	Vector         []float32
}

type milvusSearchRequest struct {
	Collection string
	Vector     []float32
	TopK       int
	Filter     string
}

type milvusSearchHit struct {
	ChunkID uint64
	Score   float64
}

func NewMilvusVectorStore(config MilvusConfig, client milvusVectorClient) (*MilvusVectorStore, error) {
	if strings.TrimSpace(config.Collection) == "" || config.Dimension <= 0 || client == nil {
		return nil, errors.New("milvus vector store config is invalid")
	}
	return &MilvusVectorStore{config: config, client: client}, nil
}

func NewMilvusRESTVectorStore(config MilvusConfig, httpClient *http.Client) (*MilvusVectorStore, error) {
	if strings.TrimSpace(config.Address) == "" {
		return nil, errors.New("milvus address is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return NewMilvusVectorStore(config, &milvusRESTClient{endpoint: strings.TrimRight(strings.TrimSpace(config.Address), "/"), httpClient: httpClient})
}

func (s *MilvusVectorStore) UpsertChunks(ctx context.Context, input VectorUpsertInput) error {
	if s == nil || s.client == nil {
		return errors.New("milvus vector store is unavailable")
	}
	rows := make([]milvusVectorRow, 0, len(input.Chunks))
	for _, item := range input.Chunks {
		if len(item.Vector) != s.config.Dimension {
			return fmt.Errorf("milvus vector dimension mismatch")
		}
		rows = append(rows, milvusVectorRow{
			VectorID:       item.VectorID,
			ChunkID:        item.Chunk.ID,
			ProductID:      item.Chunk.ProductID,
			DocumentID:     item.Chunk.DocumentID,
			DocType:        string(item.Chunk.DocType),
			Version:        item.Chunk.Version,
			IsCurrentReady: true,
			EmbeddingModel: input.Model,
			Vector:         append([]float32(nil), item.Vector...),
		})
	}
	return s.client.UpsertRows(ctx, s.config.Collection, rows)
}

func (s *MilvusVectorStore) Search(ctx context.Context, input VectorSearchInput) ([]VectorSearchHit, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("milvus vector store is unavailable")
	}
	if len(input.Query) != s.config.Dimension {
		return nil, fmt.Errorf("milvus query vector dimension mismatch")
	}
	hits, err := s.client.SearchRows(ctx, milvusSearchRequest{
		Collection: s.config.Collection,
		Vector:     append([]float32(nil), input.Query...),
		TopK:       input.TopK,
		Filter:     milvusCurrentReadyFilter(input.ProductID, input.Filters),
	})
	if err != nil {
		return nil, err
	}
	out := make([]VectorSearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, VectorSearchHit{ChunkID: hit.ChunkID, Score: hit.Score})
	}
	return out, nil
}

func milvusCurrentReadyFilter(productID uint64, filters []VectorDocumentFilter) string {
	ids := make([]string, 0, len(filters))
	for _, filter := range filters {
		ids = append(ids, fmt.Sprintf("%d", filter.DocumentID))
	}
	if len(ids) == 0 {
		return fmt.Sprintf("product_id == %d && is_current_ready == true", productID)
	}
	return fmt.Sprintf("product_id == %d && is_current_ready == true && document_id in [%s]", productID, strings.Join(ids, ","))
}

type milvusRESTClient struct {
	endpoint   string
	httpClient *http.Client
}

func (c *milvusRESTClient) UpsertRows(ctx context.Context, collectionName string, rows []milvusVectorRow) error {
	data := make([]milvusRESTEntity, 0, len(rows))
	for _, row := range rows {
		data = append(data, milvusRESTEntity{
			VectorID: row.VectorID, ChunkID: row.ChunkID, ProductID: row.ProductID, DocumentID: row.DocumentID,
			DocType: row.DocType, Version: row.Version, IsCurrentReady: row.IsCurrentReady,
			EmbeddingModel: row.EmbeddingModel, Embedding: row.Vector,
		})
	}
	return c.post(ctx, "/v2/vectordb/entities/upsert", milvusRESTUpsertRequest{CollectionName: collectionName, Data: data}, nil)
}

func (c *milvusRESTClient) SearchRows(ctx context.Context, request milvusSearchRequest) ([]milvusSearchHit, error) {
	var response milvusRESTSearchResponse
	if err := c.post(ctx, "/v2/vectordb/entities/search", milvusRESTSearchRequest{
		CollectionName: request.Collection,
		Data:           [][]float32{append([]float32(nil), request.Vector...)},
		AnnsField:      "embedding",
		Limit:          request.TopK,
		Filter:         request.Filter,
		OutputFields:   []string{"chunk_id"},
	}, &response); err != nil {
		return nil, err
	}
	hits := make([]milvusSearchHit, 0, len(response.Data))
	for _, item := range response.Data {
		hits = append(hits, milvusSearchHit{ChunkID: item.ChunkID, Score: item.Distance})
	}
	return hits, nil
}

func (c *milvusRESTClient) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode milvus request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create milvus request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call milvus: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("milvus status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode milvus response: %w", err)
	}
	return nil
}

type milvusRESTUpsertRequest struct {
	CollectionName string             `json:"collectionName"`
	Data           []milvusRESTEntity `json:"data"`
}

type milvusRESTEntity struct {
	VectorID       string    `json:"vector_id"`
	ChunkID        uint64    `json:"chunk_id"`
	ProductID      uint64    `json:"product_id"`
	DocumentID     uint64    `json:"document_id"`
	DocType        string    `json:"doc_type"`
	Version        uint32    `json:"version"`
	IsCurrentReady bool      `json:"is_current_ready"`
	EmbeddingModel string    `json:"embedding_model"`
	Embedding      []float32 `json:"embedding"`
}

type milvusRESTSearchRequest struct {
	CollectionName string      `json:"collectionName"`
	Data           [][]float32 `json:"data"`
	AnnsField      string      `json:"annsField"`
	Limit          int         `json:"limit"`
	Filter         string      `json:"filter"`
	OutputFields   []string    `json:"outputFields"`
}

type milvusRESTSearchResponse struct {
	Data []struct {
		ChunkID  uint64  `json:"chunk_id"`
		Distance float64 `json:"distance"`
	} `json:"data"`
}
