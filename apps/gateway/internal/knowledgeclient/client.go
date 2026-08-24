package knowledgeclient

import (
	"context"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const knowledgeCallTimeout = 2 * time.Second

type Client interface {
	UploadDocument(context.Context, *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error)
	ListDocuments(context.Context, *knowledgepb.ListDocumentsRequest) (*knowledgepb.ListDocumentsResponse, error)
	GetDocument(context.Context, *knowledgepb.GetDocumentRequest) (*knowledgepb.GetDocumentResponse, error)
	RetryDocument(context.Context, *knowledgepb.RetryDocumentRequest) (*knowledgepb.RetryDocumentResponse, error)
	SearchProductKnowledge(context.Context, *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error)
}

type grpcClient struct {
	client knowledgepb.KnowledgeServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) Client {
	return &grpcClient{client: knowledgepb.NewKnowledgeServiceClient(conn)}
}

func (c *grpcClient) UploadDocument(parent context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.UploadDocument(ctx, req)
}

func (c *grpcClient) ListDocuments(parent context.Context, req *knowledgepb.ListDocumentsRequest) (*knowledgepb.ListDocumentsResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.ListDocuments(ctx, req)
}

func (c *grpcClient) GetDocument(parent context.Context, req *knowledgepb.GetDocumentRequest) (*knowledgepb.GetDocumentResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.GetDocument(ctx, req)
}

func (c *grpcClient) RetryDocument(parent context.Context, req *knowledgepb.RetryDocumentRequest) (*knowledgepb.RetryDocumentResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.RetryDocument(ctx, req)
}

func (c *grpcClient) SearchProductKnowledge(parent context.Context, req *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.SearchProductKnowledge(ctx, req)
}

func (c *grpcClient) auth(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, knowledgeCallTimeout)
	if bearer, ok := platformauth.BearerFromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
	}
	if traceID := platformtrace.TraceID(ctx); validTraceID(traceID) {
		ctx = metadata.AppendToOutgoingContext(ctx, "trace_id", traceID)
	}
	return ctx, cancel
}

func validTraceID(value string) bool {
	_, err := oteltrace.TraceIDFromHex(value)
	return err == nil
}
