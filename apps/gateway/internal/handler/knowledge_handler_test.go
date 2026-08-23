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

type fakeKnowledgeClient struct {
	upload func(context.Context, *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error)
}

func (f *fakeKnowledgeClient) UploadDocument(ctx context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
	if f.upload == nil {
		return &knowledgepb.UploadDocumentResponse{Document: &knowledgepb.Document{}}, nil
	}
	return f.upload(ctx, req)
}
