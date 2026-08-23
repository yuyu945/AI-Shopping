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
}

type grpcClient struct {
	client knowledgepb.KnowledgeServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) Client {
	return &grpcClient{client: knowledgepb.NewKnowledgeServiceClient(conn)}
}

func (c *grpcClient) UploadDocument(parent context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
	ctx, cancel := context.WithTimeout(parent, knowledgeCallTimeout)
	defer cancel()
	if bearer, ok := platformauth.BearerFromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
	}
	if traceID := platformtrace.TraceID(ctx); validTraceID(traceID) {
		ctx = metadata.AppendToOutgoingContext(ctx, "trace_id", traceID)
	}
	return c.client.UploadDocument(ctx, req)
}

func validTraceID(value string) bool {
	_, err := oteltrace.TraceIDFromHex(value)
	return err == nil
}
