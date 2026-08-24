package server

import (
	"context"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"github.com/yuyu945/AI-Shopping/services/agent-service/internal/agent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAgentServerStartRunRequiresAuth(t *testing.T) {
	server := NewGRPCServer(&fakeRunStarter{}, &fakeRunLoader{}, testAuthManager(t), time.Second)

	_, err := server.StartRun(context.Background(), &agentpb.StartRunRequest{UserInput: "买电脑"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("StartRun() code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestAgentServerStartRunReturnsStableInvalidRequest(t *testing.T) {
	server := NewGRPCServer(&fakeRunStarter{}, &fakeRunLoader{}, testAuthManager(t), time.Second)

	_, err := server.StartRun(authContext(t, 42), &agentpb.StartRunRequest{})
	if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "invalid request" {
		t.Fatalf("StartRun() error = %v", err)
	}
}

func TestAgentServerGetRunRequiresOwner(t *testing.T) {
	loader := &fakeRunLoader{timeline: agent.RunTimeline{Run: agent.Run{RunID: "run_1", UserID: 42, Status: agent.RunSucceeded}}}
	server := NewGRPCServer(&fakeRunStarter{}, loader, testAuthManager(t), time.Second)

	resp, err := server.GetRun(authContext(t, 42), &agentpb.GetRunRequest{RunId: "run_1"})
	if err != nil {
		t.Fatal(err)
	}
	if loader.userID != 42 || resp.GetRun().GetRunId() != "run_1" {
		t.Fatalf("loader user=%d response=%#v", loader.userID, resp)
	}
}

func TestAgentServerGetRunReturnsRecommendations(t *testing.T) {
	loader := &fakeRunLoader{timeline: agent.RunTimeline{
		Run: agent.Run{RunID: "run_1", UserID: 42, Status: agent.RunSucceeded},
		Recommendations: []agent.RecommendationSnapshot{{
			RankNo: 1, SKUID: 2001, ProductID: 1001, ProductTitleSnapshot: "轻薄笔记本",
			SKUCodeSnapshot: "LAPTOP-16G", SKUSpecSnapshotJSON: []byte(`{"memory":"16G"}`),
			PriceSnapshot: "4999.00", SaleableSnapshot: true, DiscountSnapshotJSON: []byte(`[{"promotion_id":3001}]`),
			Reason: "适合编程", ValidationStatus: agent.RecommendationVerified,
		}},
	}}
	server := NewGRPCServer(&fakeRunStarter{}, loader, testAuthManager(t), time.Second)

	resp, err := server.GetRun(authContext(t, 42), &agentpb.GetRunRequest{RunId: "run_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetRecommendations()) != 1 || resp.GetRecommendations()[0].GetSkuId() != 2001 || resp.GetRecommendations()[0].GetPrice() != "4999.00" {
		t.Fatalf("response=%#v", resp)
	}
}

func TestAgentServerGetRunMapsNonOwnerToNotFound(t *testing.T) {
	loader := &fakeRunLoader{err: agent.ErrRunNotFound}
	server := NewGRPCServer(&fakeRunStarter{}, loader, testAuthManager(t), time.Second)

	_, err := server.GetRun(authContext(t, 99), &agentpb.GetRunRequest{RunId: "run_1"})
	if status.Code(err) != codes.NotFound || status.Convert(err).Message() != "resource not found" {
		t.Fatalf("GetRun() error = %v", err)
	}
}

type fakeRunStarter struct {
	command agent.StartRunCommand
	result  agent.RunOutcome
	err     error
}

func (f *fakeRunStarter) StartRun(ctx context.Context, command agent.StartRunCommand) (agent.RunOutcome, error) {
	f.command = command
	if f.err != nil {
		return agent.RunOutcome{}, f.err
	}
	if f.result.RunID == "" {
		f.result = agent.RunOutcome{RunID: command.RunID, Status: agent.RunSucceeded, FinalText: "ok"}
	}
	return f.result, nil
}

type fakeRunLoader struct {
	userID   uint64
	runID    string
	timeline agent.RunTimeline
	err      error
}

func (f *fakeRunLoader) GetRunTimeline(ctx context.Context, userID uint64, runID string) (agent.RunTimeline, error) {
	f.userID = userID
	f.runID = runID
	return f.timeline, f.err
}

func testAuthManager(t *testing.T) *platformauth.Manager {
	t.Helper()
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func authContext(t *testing.T, userID uint64) context.Context {
	t.Helper()
	manager := testAuthManager(t)
	token, _, err := manager.Issue(platformauth.Principal{UserID: userID, Email: "buyer@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}
