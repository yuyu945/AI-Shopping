package server

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"github.com/yuyu945/AI-Shopping/services/knowledge-service/internal/knowledge"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Uploader interface {
	UploadDocument(context.Context, knowledge.UploadInput) (knowledge.Document, error)
}

type Searcher interface {
	SearchProductKnowledge(context.Context, knowledge.SearchKnowledgeInput) (knowledge.SearchKnowledgeResult, error)
}

type GRPCServer struct {
	knowledgepb.UnimplementedKnowledgeServiceServer
	uploader Uploader
	searcher Searcher
	auth     *platformauth.Manager
	timeout  time.Duration
}

func NewGRPCServer(uploader Uploader, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	return NewGRPCServerWithSearch(uploader, nil, auth, timeout)
}

func NewGRPCServerWithSearch(uploader Uploader, searcher Searcher, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &GRPCServer{uploader: uploader, searcher: searcher, auth: auth, timeout: timeout}
}

func (s *GRPCServer) UploadDocument(ctx context.Context, req *knowledgepb.UploadDocumentRequest) (*knowledgepb.UploadDocumentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	if s.uploader == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	content, err := base64.StdEncoding.DecodeString(req.GetContentBase64())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	document, err := s.uploader.UploadDocument(callCtx, knowledge.UploadInput{
		UserID: userID, ProductID: req.GetProductId(), DocType: knowledge.DocType(req.GetDocType()),
		FileName: req.GetFileName(), ContentType: req.GetContentType(), Content: content,
	})
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return nil, toStatus(err)
	}
	return &knowledgepb.UploadDocumentResponse{Document: documentWire(document)}, nil
}

func (s *GRPCServer) SearchProductKnowledge(ctx context.Context, req *knowledgepb.SearchProductKnowledgeRequest) (*knowledgepb.SearchProductKnowledgeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s.searcher == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	result, err := s.searcher.SearchProductKnowledge(callCtx, knowledge.SearchKnowledgeInput{
		ProductID: req.GetProductId(),
		Query:     req.GetQuery(),
		DocTypes:  docTypes(req.GetDocTypes()),
		TopK:      int(req.GetTopK()),
	})
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return nil, toStatus(err)
	}
	return &knowledgepb.SearchProductKnowledgeResponse{Snippets: snippetWire(result.Snippets), FallbackReason: result.FallbackReason}, nil
}

func (s *GRPCServer) userID(ctx context.Context) (uint64, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md.Get("authorization")) != 1 || s.auth == nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	principal, err := s.auth.VerifyBearer(md.Get("authorization")[0], time.Now())
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return principal.UserID, nil
}

func toStatus(err error) error {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal server error")
	}
	switch appErr.Code {
	case apperror.InvalidArgument:
		return status.Error(codes.InvalidArgument, "invalid request")
	case apperror.Unauthenticated:
		return status.Error(codes.Unauthenticated, "authentication required")
	case apperror.NotFound:
		return status.Error(codes.NotFound, "resource not found")
	case apperror.DependencyTimeout:
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func docTypes(values []string) []knowledge.DocType {
	out := make([]knowledge.DocType, 0, len(values))
	for _, value := range values {
		out = append(out, knowledge.DocType(value))
	}
	return out
}

func documentWire(document knowledge.Document) *knowledgepb.Document {
	return &knowledgepb.Document{
		DocumentNo: document.DocumentNo,
		ProductId:  document.ProductID,
		DocType:    string(document.DocType),
		Version:    document.Version,
		Status:     string(document.Status),
	}
}

func snippetWire(snippets []knowledge.KnowledgeSnippet) []*knowledgepb.KnowledgeSnippet {
	out := make([]*knowledgepb.KnowledgeSnippet, 0, len(snippets))
	for _, snippet := range snippets {
		var sourcePage uint32
		if snippet.SourcePage != nil {
			sourcePage = *snippet.SourcePage
		}
		out = append(out, &knowledgepb.KnowledgeSnippet{
			ChunkId:    snippet.ChunkID,
			DocumentNo: snippet.DocumentNo,
			ProductId:  snippet.ProductID,
			DocType:    string(snippet.DocType),
			Version:    snippet.Version,
			Section:    snippet.Section,
			SourcePage: sourcePage,
			Content:    snippet.Content,
			Score:      snippet.Score,
		})
	}
	return out
}
