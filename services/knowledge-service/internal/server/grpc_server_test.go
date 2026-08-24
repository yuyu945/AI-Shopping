package server

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"github.com/yuyu945/AI-Shopping/services/knowledge-service/internal/knowledge"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCServerUploadDocumentDerivesUserFromBearer(t *testing.T) {
	uploader := &fakeUploader{}
	server := NewGRPCServer(uploader, testAuthManager(t), time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", testBearer(t, 7)))

	out, err := server.UploadDocument(ctx, &knowledgepb.UploadDocumentRequest{
		ProductId: 1001, DocType: "FAQ", FileName: "faq.md", ContentType: "text/markdown",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("# FAQ")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetDocument().GetDocumentNo() != "doc_1" || uploader.input.UserID != 7 || string(uploader.input.Content) != "# FAQ" {
		t.Fatalf("out=%#v input=%#v", out, uploader.input)
	}
}

func TestGRPCServerUploadDocumentRejectsInvalidBase64(t *testing.T) {
	server := NewGRPCServer(&fakeUploader{}, testAuthManager(t), time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", testBearer(t, 7)))

	_, err := server.UploadDocument(ctx, &knowledgepb.UploadDocumentRequest{ProductId: 1001, DocType: "FAQ", FileName: "faq.md", ContentType: "text/markdown", ContentBase64: "not base64"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err=%v", err)
	}
}

func TestGRPCServerUploadDocumentRequiresBearer(t *testing.T) {
	server := NewGRPCServer(&fakeUploader{}, testAuthManager(t), time.Second)

	_, err := server.UploadDocument(context.Background(), &knowledgepb.UploadDocumentRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("err=%v", err)
	}
}

func TestGRPCServerSearchProductKnowledgeReturnsSnippets(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{result: knowledge.SearchKnowledgeResult{Snippets: []knowledge.KnowledgeSnippet{{
		ChunkID: 11, DocumentNo: "doc_1", ProductID: 1001, DocType: knowledge.DocFAQ, Version: 2, Section: "Battery", Content: "Lasts 10 hours.", Score: 0.91,
	}}}}
	server := NewGRPCServerWithSearch(&fakeUploader{}, searcher, testAuthManager(t), time.Second)

	out, err := server.SearchProductKnowledge(context.Background(), &knowledgepb.SearchProductKnowledgeRequest{ProductId: 1001, Query: "battery", DocTypes: []string{"FAQ"}, TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.GetSnippets()) != 1 || out.GetSnippets()[0].GetChunkId() != 11 || searcher.input.DocTypes[0] != knowledge.DocFAQ {
		t.Fatalf("out=%#v input=%#v", out, searcher.input)
	}
}

func TestGRPCServerListDocumentsReturnsOperationalFields(t *testing.T) {
	ops := &fakeKnowledgeOps{listResult: knowledge.DocumentListResult{Documents: []knowledge.Document{{
		DocumentNo: "doc_failed", ProductID: 1001, DocType: knowledge.DocFAQ, Version: 2,
		Status: knowledge.DocumentFailed, FileName: "faq.md", ErrorCode: "EMBEDDING_FAILED",
	}}}}
	server := NewGRPCServerWithOps(&fakeUploader{}, &fakeKnowledgeSearcher{}, ops, testAuthManager(t), time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", testBearer(t, 7)))

	out, err := server.ListDocuments(ctx, &knowledgepb.ListDocumentsRequest{ProductId: 1001, DocType: "FAQ", Status: "FAILED", PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.GetDocuments()) != 1 || out.GetDocuments()[0].GetErrorCode() != "EMBEDDING_FAILED" || ops.listFilter.Status != knowledge.DocumentFailed {
		t.Fatalf("out=%#v filter=%#v", out, ops.listFilter)
	}
}

func TestGRPCServerRetryDocumentReturnsPendingDocument(t *testing.T) {
	ops := &fakeKnowledgeOps{retryResult: knowledge.Document{DocumentNo: "doc_failed", ProductID: 1001, DocType: knowledge.DocFAQ, Version: 2, Status: knowledge.DocumentPending}}
	server := NewGRPCServerWithOps(&fakeUploader{}, &fakeKnowledgeSearcher{}, ops, testAuthManager(t), time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", testBearer(t, 7)))

	out, err := server.RetryDocument(ctx, &knowledgepb.RetryDocumentRequest{DocumentNo: "doc_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetDocument().GetStatus() != "PENDING" || ops.retryDocumentNo != "doc_failed" {
		t.Fatalf("out=%#v retry=%s", out, ops.retryDocumentNo)
	}
}

type fakeUploader struct {
	input knowledge.UploadInput
	err   error
}

func (f *fakeUploader) UploadDocument(_ context.Context, input knowledge.UploadInput) (knowledge.Document, error) {
	f.input = input
	if f.err != nil {
		return knowledge.Document{}, f.err
	}
	return knowledge.Document{DocumentNo: "doc_1", ProductID: input.ProductID, DocType: input.DocType, Version: 1, Status: knowledge.DocumentPending}, nil
}

type fakeKnowledgeSearcher struct {
	input  knowledge.SearchKnowledgeInput
	result knowledge.SearchKnowledgeResult
	err    error
}

func (f *fakeKnowledgeSearcher) SearchProductKnowledge(_ context.Context, input knowledge.SearchKnowledgeInput) (knowledge.SearchKnowledgeResult, error) {
	f.input = input
	if f.err != nil {
		return knowledge.SearchKnowledgeResult{}, f.err
	}
	return f.result, nil
}

type fakeKnowledgeOps struct {
	listFilter      knowledge.DocumentListFilter
	listResult      knowledge.DocumentListResult
	detailDocument  string
	detailResult    knowledge.DocumentDetail
	retryDocumentNo string
	retryResult     knowledge.Document
	err             error
}

func (f *fakeKnowledgeOps) ListDocuments(_ context.Context, filter knowledge.DocumentListFilter) (knowledge.DocumentListResult, error) {
	f.listFilter = filter
	if f.err != nil {
		return knowledge.DocumentListResult{}, f.err
	}
	return f.listResult, nil
}

func (f *fakeKnowledgeOps) GetDocumentDetail(_ context.Context, documentNo string) (knowledge.DocumentDetail, error) {
	f.detailDocument = documentNo
	if f.err != nil {
		return knowledge.DocumentDetail{}, f.err
	}
	return f.detailResult, nil
}

func (f *fakeKnowledgeOps) RetryDocument(_ context.Context, documentNo string) (knowledge.Document, error) {
	f.retryDocumentNo = documentNo
	if f.err != nil {
		return knowledge.Document{}, f.err
	}
	return f.retryResult, nil
}

func testAuthManager(t *testing.T) *platformauth.Manager {
	t.Helper()
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testBearer(t *testing.T, userID uint64) string {
	t.Helper()
	token, _, err := testAuthManager(t).Issue(platformauth.Principal{UserID: userID, Email: "user@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}
