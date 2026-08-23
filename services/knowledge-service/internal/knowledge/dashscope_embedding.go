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

const dashScopeEmbeddingPath = "/api/v1/services/embeddings/text-embedding/text-embedding"
const dashScopeMaxEmbeddingTexts = 10

type DashScopeConfig struct {
	Endpoint   string
	APIKey     string
	Model      string
	Dimension  int
	HTTPClient *http.Client
}

type DashScopeEmbeddingProvider struct {
	endpoint   string
	apiKey     string
	model      string
	dimension  int
	httpClient *http.Client
}

func NewDashScopeEmbeddingProvider(config DashScopeConfig) (*DashScopeEmbeddingProvider, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" || strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" || config.Dimension <= 0 {
		return nil, errors.New("dashscope embedding config is invalid")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &DashScopeEmbeddingProvider{endpoint: endpoint, apiKey: strings.TrimSpace(config.APIKey), model: strings.TrimSpace(config.Model), dimension: config.Dimension, httpClient: client}, nil
}

func (p *DashScopeEmbeddingProvider) EmbedDocuments(ctx context.Context, input EmbeddingInput) (EmbeddingOutput, error) {
	if p == nil || p.httpClient == nil {
		return EmbeddingOutput{}, errors.New("dashscope embedding provider is unavailable")
	}
	if len(input.Texts) == 0 || len(input.Texts) > dashScopeMaxEmbeddingTexts {
		return EmbeddingOutput{}, errors.New("dashscope embedding batch size is invalid")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = p.model
	}
	payload := dashScopeEmbeddingRequest{
		Model: model,
		Input: dashScopeEmbeddingInput{Texts: input.Texts},
		Parameters: dashScopeEmbeddingParameters{
			TextType:  "document",
			Dimension: p.dimension,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return EmbeddingOutput{}, fmt.Errorf("encode dashscope embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+dashScopeEmbeddingPath, bytes.NewReader(body))
	if err != nil {
		return EmbeddingOutput{}, fmt.Errorf("create dashscope embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return EmbeddingOutput{}, fmt.Errorf("call dashscope embedding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EmbeddingOutput{}, fmt.Errorf("dashscope embedding status %d", resp.StatusCode)
	}
	var decoded dashScopeEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return EmbeddingOutput{}, fmt.Errorf("decode dashscope embedding response: %w", err)
	}
	vectors := make([][]float32, 0, len(decoded.Output.Embeddings))
	for _, item := range decoded.Output.Embeddings {
		vectors = append(vectors, append([]float32(nil), item.Embedding...))
	}
	return EmbeddingOutput{Vectors: vectors}, nil
}

type dashScopeEmbeddingRequest struct {
	Model      string                       `json:"model"`
	Input      dashScopeEmbeddingInput      `json:"input"`
	Parameters dashScopeEmbeddingParameters `json:"parameters"`
}

type dashScopeEmbeddingInput struct {
	Texts []string `json:"texts"`
}

type dashScopeEmbeddingParameters struct {
	TextType  string `json:"text_type"`
	Dimension int    `json:"dimension"`
}

type dashScopeEmbeddingResponse struct {
	Output dashScopeEmbeddingOutput `json:"output"`
}

type dashScopeEmbeddingOutput struct {
	Embeddings []dashScopeEmbedding `json:"embeddings"`
}

type dashScopeEmbedding struct {
	Embedding []float32 `json:"embedding"`
}
