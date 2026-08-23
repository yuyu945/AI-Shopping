package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/knowledgeclient"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type KnowledgeHandler struct {
	client knowledgeclient.Client
}

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
