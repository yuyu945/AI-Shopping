package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/knowledgeclient"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type KnowledgeHandler struct {
	client knowledgeclient.Client
}

const (
	defaultKnowledgeTopK = uint32(3)
	maxKnowledgeTopK     = uint32(5)
)

func NewKnowledgeHandler(client knowledgeclient.Client) *KnowledgeHandler {
	return &KnowledgeHandler{client: client}
}

func (h *KnowledgeHandler) Documents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		if h.client == nil {
			writeKnowledgeError(w, status.Error(codes.Internal, "internal server error"))
			return
		}
		var body struct {
			ProductID     uint64 `json:"product_id"`
			DocType       string `json:"doc_type"`
			FileName      string `json:"file_name"`
			ContentType   string `json:"content_type"`
			ContentBase64 string `json:"content_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, err := h.client.UploadDocument(r.Context(), &knowledgepb.UploadDocumentRequest{
			ProductId: body.ProductID, DocType: body.DocType, FileName: body.FileName,
			ContentType: body.ContentType, ContentBase64: body.ContentBase64,
		})
		if err != nil {
			writeKnowledgeError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"document": documentJSON(out.GetDocument())})
	}
}

func (h *KnowledgeHandler) ProductQuestion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		if h.client == nil {
			writeKnowledgeError(w, status.Error(codes.Internal, "internal server error"))
			return
		}
		productID, err := strconv.ParseUint(r.PathValue("product_id"), 10, 64)
		if err != nil || productID == 0 {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		var body struct {
			Question string   `json:"question"`
			DocTypes []string `json:"doc_types"`
			TopK     uint32   `json:"top_k"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		question := strings.TrimSpace(body.Question)
		if question == "" {
			writeKnowledgeError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		topK := body.TopK
		if topK == 0 {
			topK = defaultKnowledgeTopK
		}
		if topK > maxKnowledgeTopK {
			topK = maxKnowledgeTopK
		}
		out, err := h.client.SearchProductKnowledge(r.Context(), &knowledgepb.SearchProductKnowledgeRequest{
			ProductId: productID,
			Query:     question,
			DocTypes:  body.DocTypes,
			TopK:      topK,
		})
		if err != nil {
			writeKnowledgeError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, knowledgeSearchJSON(out))
	}
}

func documentJSON(document *knowledgepb.Document) map[string]any {
	if document == nil {
		return nil
	}
	return map[string]any{
		"document_no": document.GetDocumentNo(),
		"product_id":  document.GetProductId(),
		"doc_type":    document.GetDocType(),
		"version":     document.GetVersion(),
		"status":      document.GetStatus(),
	}
}

func knowledgeSearchJSON(out *knowledgepb.SearchProductKnowledgeResponse) map[string]any {
	snippets := make([]any, 0, len(out.GetSnippets()))
	for _, snippet := range out.GetSnippets() {
		snippets = append(snippets, knowledgeSnippetJSON(snippet))
	}
	return map[string]any{"snippets": snippets, "fallback_reason": out.GetFallbackReason()}
}

func knowledgeSnippetJSON(snippet *knowledgepb.KnowledgeSnippet) map[string]any {
	if snippet == nil {
		return nil
	}
	return map[string]any{
		"chunk_id":    snippet.GetChunkId(),
		"document_no": snippet.GetDocumentNo(),
		"product_id":  snippet.GetProductId(),
		"doc_type":    snippet.GetDocType(),
		"version":     snippet.GetVersion(),
		"section":     snippet.GetSection(),
		"source_page": snippet.GetSourcePage(),
		"content":     snippet.GetContent(),
		"score":       snippet.GetScore(),
	}
}

func writeKnowledgeError(w http.ResponseWriter, err error) {
	code := codes.Internal
	if s, ok := status.FromError(err); ok {
		code = s.Code()
	}
	httpCode := http.StatusInternalServerError
	body := map[string]string{"code": "INTERNAL", "message": "internal server error"}
	switch code {
	case codes.InvalidArgument:
		httpCode = http.StatusBadRequest
		body = map[string]string{"code": "INVALID_ARGUMENT", "message": "invalid request"}
	case codes.Unauthenticated:
		httpCode = http.StatusUnauthorized
		body = map[string]string{"code": "UNAUTHENTICATED", "message": "authentication required"}
	case codes.NotFound:
		httpCode = http.StatusNotFound
		body = map[string]string{"code": "NOT_FOUND", "message": "resource not found"}
	case codes.DeadlineExceeded, codes.Unavailable:
		httpCode = http.StatusGatewayTimeout
		body = map[string]string{"code": "DEPENDENCY_TIMEOUT", "message": "knowledge service timeout"}
	}
	writeJSONValue(w, httpCode, body)
}
