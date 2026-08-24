package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestKnowledgeUploadHandlerReturnsPendingDocument(t *testing.T) {
	client := &fakeKnowledgeClient{upload: func(_ context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
		if req.GetProductId() != 1001 || req.GetDocType() != "FAQ" || req.GetContentBase64() == "" {
			t.Fatalf("req=%#v", req)
		}
		return &knowledgepb.UploadDocumentResponse{Document: &knowledgepb.Document{DocumentNo: "doc_1", ProductId: 1001, DocType: "FAQ", Version: 1, Status: "PENDING"}}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/documents", strings.NewReader(`{"product_id":1001,"doc_type":"FAQ","file_name":"faq.md","content_type":"text/markdown","content_base64":"IyBGQVE="}`))

	NewKnowledgeHandler(client).Documents().ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"PENDING"`) || !strings.Contains(w.Body.String(), `"document_no":"doc_1"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestKnowledgeUploadHandlerMapsStableErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		body string
	}{
		{name: "invalid", err: status.Error(codes.InvalidArgument, "raw validation"), want: http.StatusBadRequest, body: "INVALID_ARGUMENT"},
		{name: "unauthenticated", err: status.Error(codes.Unauthenticated, "raw auth"), want: http.StatusUnauthorized, body: "UNAUTHENTICATED"},
		{name: "timeout", err: status.Error(codes.DeadlineExceeded, "minio body"), want: http.StatusGatewayTimeout, body: "DEPENDENCY_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewKnowledgeHandler(&fakeKnowledgeClient{upload: func(context.Context, *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
				return nil, tc.err
			}})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"product_id":1001,"doc_type":"FAQ","file_name":"faq.md","content_type":"text/markdown","content_base64":"IyBGQVE="}`))
			h.Documents().ServeHTTP(w, r)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.body) || strings.Contains(w.Body.String(), "raw") || strings.Contains(w.Body.String(), "minio") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestKnowledgeUploadHandlerRejectsInvalidJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{`))
	NewKnowledgeHandler(&fakeKnowledgeClient{}).Documents().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_ARGUMENT") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestKnowledgeQuestionHandlerMapsRequestAndResponse(t *testing.T) {
	client := &fakeKnowledgeClient{search: func(_ context.Context, req *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
		if req.GetProductId() != 1001 || req.GetQuery() != "battery life" || req.GetTopK() != 3 {
			t.Fatalf("req=%#v", req)
		}
		if got := req.GetDocTypes(); len(got) != 2 || got[0] != "SPEC" || got[1] != "FAQ" {
			t.Fatalf("doc_types=%v", got)
		}
		return &knowledgepb.SearchProductKnowledgeResponse{
			Snippets: []*knowledgepb.KnowledgeSnippet{{
				ChunkId: 9001, DocumentNo: "doc_1", ProductId: 1001, DocType: "SPEC",
				Version: 2, Section: "Battery", SourcePage: 4, Content: "Up to 10 hours.", Score: 0.82,
			}},
		}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/products/1001/knowledge/questions", strings.NewReader(`{"question":" battery life ","doc_types":["SPEC","FAQ"],"top_k":3}`))
	r.SetPathValue("product_id", "1001")

	NewKnowledgeHandler(client).ProductQuestion().ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"chunk_id":9001`) || !strings.Contains(w.Body.String(), `"source_page":4`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"answer"`) {
		t.Fatalf("handler must not synthesize answer: %s", w.Body.String())
	}
}

func TestKnowledgeQuestionHandlerDefaultsAndCapsTopK(t *testing.T) {
	cases := []struct {
		name string
		body string
		want uint32
	}{
		{name: "default", body: `{"question":"battery"}`, want: 3},
		{name: "cap", body: `{"question":"battery","top_k":99}`, want: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeKnowledgeClient{search: func(_ context.Context, req *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
				if req.GetTopK() != tc.want {
					t.Fatalf("top_k=%d want %d", req.GetTopK(), tc.want)
				}
				return &knowledgepb.SearchProductKnowledgeResponse{FallbackReason: "NO_SOURCE"}, nil
			}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/products/1001/knowledge/questions", strings.NewReader(tc.body))
			r.SetPathValue("product_id", "1001")
			NewKnowledgeHandler(client).ProductQuestion().ServeHTTP(w, r)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"fallback_reason":"NO_SOURCE"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestKnowledgeQuestionHandlerRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name      string
		productID string
		body      string
	}{
		{name: "bad product", productID: "bad", body: `{"question":"battery"}`},
		{name: "zero product", productID: "0", body: `{"question":"battery"}`},
		{name: "empty question", productID: "1001", body: `{"question":"   "}`},
		{name: "bad json", productID: "1001", body: `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+tc.productID+"/knowledge/questions", strings.NewReader(tc.body))
			r.SetPathValue("product_id", tc.productID)
			NewKnowledgeHandler(&fakeKnowledgeClient{}).ProductQuestion().ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_ARGUMENT") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestKnowledgeQuestionHandlerMapsStableSearchErrors(t *testing.T) {
	h := NewKnowledgeHandler(&fakeKnowledgeClient{search: func(context.Context, *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
		return nil, status.Error(codes.DeadlineExceeded, "milvus internal detail")
	}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/products/1001/knowledge/questions", strings.NewReader(`{"question":"battery"}`))
	r.SetPathValue("product_id", "1001")

	h.ProductQuestion().ServeHTTP(w, r)

	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), "DEPENDENCY_TIMEOUT") || strings.Contains(w.Body.String(), "milvus") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestKnowledgeOpsListDocumentsReturnsOperationalFields(t *testing.T) {
	client := &fakeKnowledgeClient{list: func(_ context.Context, req *knowledgepb.ListDocumentsRequest) (*knowledgepb.ListDocumentsResponse, error) {
		if req.GetProductId() != 1001 || req.GetDocType() != "FAQ" || req.GetStatus() != "FAILED" {
			t.Fatalf("req=%#v", req)
		}
		return &knowledgepb.ListDocumentsResponse{Documents: []*knowledgepb.Document{{DocumentNo: "doc_failed", ProductId: 1001, DocType: "FAQ", Status: "FAILED", ErrorCode: "EMBEDDING_FAILED"}}}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ops/knowledge/documents?product_id=1001&doc_type=FAQ&status=FAILED", nil)

	NewKnowledgeHandler(client).OpsDocuments().ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"error_code":"EMBEDDING_FAILED"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestKnowledgeOpsRetryDocument(t *testing.T) {
	client := &fakeKnowledgeClient{retry: func(_ context.Context, req *knowledgepb.RetryDocumentRequest) (*knowledgepb.RetryDocumentResponse, error) {
		if req.GetDocumentNo() != "doc_failed" {
			t.Fatalf("req=%#v", req)
		}
		return &knowledgepb.RetryDocumentResponse{Document: &knowledgepb.Document{DocumentNo: "doc_failed", Status: "PENDING"}}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ops/knowledge/documents/doc_failed/retry", nil)
	r.SetPathValue("document_no", "doc_failed")

	NewKnowledgeHandler(client).OpsRetryDocument().ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"PENDING"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

type fakeKnowledgeClient struct {
	upload func(context.Context, *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error)
	search func(context.Context, *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error)
	list   func(context.Context, *knowledgepb.ListDocumentsRequest) (*knowledgepb.ListDocumentsResponse, error)
	get    func(context.Context, *knowledgepb.GetDocumentRequest) (*knowledgepb.GetDocumentResponse, error)
	retry  func(context.Context, *knowledgepb.RetryDocumentRequest) (*knowledgepb.RetryDocumentResponse, error)
}

func (f *fakeKnowledgeClient) UploadDocument(ctx context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
	if f.upload == nil {
		return &knowledgepb.UploadDocumentResponse{Document: &knowledgepb.Document{}}, nil
	}
	return f.upload(ctx, req)
}

func (f *fakeKnowledgeClient) SearchProductKnowledge(ctx context.Context, req *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
	if f.search == nil {
		return &knowledgepb.SearchProductKnowledgeResponse{}, nil
	}
	return f.search(ctx, req)
}

func (f *fakeKnowledgeClient) ListDocuments(ctx context.Context, req *knowledgepb.ListDocumentsRequest) (*knowledgepb.ListDocumentsResponse, error) {
	if f.list == nil {
		return &knowledgepb.ListDocumentsResponse{}, nil
	}
	return f.list(ctx, req)
}

func (f *fakeKnowledgeClient) GetDocument(ctx context.Context, req *knowledgepb.GetDocumentRequest) (*knowledgepb.GetDocumentResponse, error) {
	if f.get == nil {
		return &knowledgepb.GetDocumentResponse{}, nil
	}
	return f.get(ctx, req)
}

func (f *fakeKnowledgeClient) RetryDocument(ctx context.Context, req *knowledgepb.RetryDocumentRequest) (*knowledgepb.RetryDocumentResponse, error) {
	if f.retry == nil {
		return &knowledgepb.RetryDocumentResponse{}, nil
	}
	return f.retry(ctx, req)
}
