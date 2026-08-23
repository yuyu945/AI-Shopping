package server

import (
	"context"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"github.com/yuyu945/AI-Shopping/services/user-service/internal/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type GRPCServer struct {
	userpb.UnimplementedUserServiceServer
	service *user.UserService
	auth    *platformauth.Manager
	timeout time.Duration
}

func NewGRPCServer(service *user.UserService, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &GRPCServer{service: service, auth: auth, timeout: timeout}
}
func (s *GRPCServer) Register(ctx context.Context, req *userpb.RegisterRequest) (*userpb.AuthResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	v, e := s.service.Register(ctx, user.RegisterInput{Email: req.GetEmail(), Password: req.GetPassword()})
	if e != nil {
		return nil, toStatus(e)
	}
	return authResponse(v), nil
}
func (s *GRPCServer) Login(ctx context.Context, req *userpb.LoginRequest) (*userpb.AuthResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	v, e := s.service.Login(ctx, user.RegisterInput{Email: req.GetEmail(), Password: req.GetPassword()})
	if e != nil {
		return nil, toStatus(e)
	}
	return authResponse(v), nil
}
func (s *GRPCServer) GetMyProfile(ctx context.Context, _ *userpb.GetMyProfileRequest) (*userpb.GetMyProfileResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.service.GetMyProfile(ctx, id)
	if e != nil {
		return nil, toStatus(e)
	}
	return &userpb.GetMyProfileResponse{Profile: profileWire(v)}, nil
}
func (s *GRPCServer) UpdateMyProfile(ctx context.Context, req *userpb.UpdateMyProfileRequest) (*userpb.GetMyProfileResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	v, e := s.service.UpdateMyProfile(ctx, id, user.ProfileUpdate{PreferenceJSON: req.GetPreferenceJson(), BudgetMin: req.BudgetMin, BudgetMax: req.BudgetMax})
	if e != nil {
		return nil, toStatus(e)
	}
	return &userpb.GetMyProfileResponse{Profile: profileWire(v)}, nil
}
func (s *GRPCServer) ListMyAddresses(ctx context.Context, _ *userpb.ListMyAddressesRequest) (*userpb.ListMyAddressesResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	items, e := s.service.ListMyAddresses(ctx, id)
	if e != nil {
		return nil, toStatus(e)
	}
	out := &userpb.ListMyAddressesResponse{}
	for _, v := range items {
		out.Addresses = append(out.Addresses, addressWire(v))
	}
	return out, nil
}
func (s *GRPCServer) GetMyAddressSnapshot(ctx context.Context, req *userpb.GetMyAddressRequest) (*userpb.AddressResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	if req == nil || req.GetAddressId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "address_id is required")
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	v, e := s.service.GetMyAddress(callCtx, id, req.GetAddressId())
	if e != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return nil, toStatus(e)
	}
	return &userpb.AddressResponse{Address: addressWire(v)}, nil
}
func (s *GRPCServer) CreateMyAddress(ctx context.Context, req *userpb.CreateMyAddressRequest) (*userpb.AddressResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	v, e := s.service.CreateMyAddress(ctx, id, addressInput(req.GetReceiverName(), req.GetReceiverPhone(), req.GetProvince(), req.GetCity(), req.GetDistrict(), req.GetDetail(), req.GetIsDefault()))
	if e != nil {
		return nil, toStatus(e)
	}
	return &userpb.AddressResponse{Address: addressWire(v)}, nil
}
func (s *GRPCServer) UpdateMyAddress(ctx context.Context, req *userpb.UpdateMyAddressRequest) (*userpb.AddressResponse, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	v, e := s.service.UpdateMyAddress(ctx, id, req.GetAddressId(), addressInput(req.GetReceiverName(), req.GetReceiverPhone(), req.GetProvince(), req.GetCity(), req.GetDistrict(), req.GetDetail(), req.GetIsDefault()))
	if e != nil {
		return nil, toStatus(e)
	}
	return &userpb.AddressResponse{Address: addressWire(v)}, nil
}
func (s *GRPCServer) DeleteMyAddress(ctx context.Context, req *userpb.DeleteMyAddressRequest) (*userpb.Empty, error) {
	id, e := s.userID(ctx)
	if e != nil {
		return nil, e
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if e = s.service.DeleteMyAddress(ctx, id, req.GetAddressId()); e != nil {
		return nil, toStatus(e)
	}
	return &userpb.Empty{}, nil
}
func (s *GRPCServer) userID(ctx context.Context) (uint64, error) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(m.Get("authorization")) != 1 {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	p, e := s.auth.VerifyBearer(m.Get("authorization")[0], time.Now())
	if e != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return p.UserID, nil
}
func authResponse(v user.AuthResult) *userpb.AuthResponse {
	return &userpb.AuthResponse{AccessToken: v.AccessToken, ExpiresAt: v.ExpiresAt.UTC().Format(time.RFC3339Nano), User: &userpb.UserSummary{UserId: v.User.ID, Email: v.User.Email}}
}
func profileWire(v user.Profile) *userpb.UserProfile {
	out := &userpb.UserProfile{ProfileVersion: v.Version}
	if v.BudgetMin != nil {
		x := *v.BudgetMin
		out.BudgetMin = &x
	}
	if v.BudgetMax != nil {
		x := *v.BudgetMax
		out.BudgetMax = &x
	}
	if len(v.PreferenceJSON) > 0 {
		x := append([]byte(nil), v.PreferenceJSON...)
		out.PreferenceJson = x
	}
	return out
}
func addressInput(a, b, c, d, e, f string, g bool) user.AddressInput {
	return user.AddressInput{ReceiverName: a, ReceiverPhone: b, Province: c, City: d, District: e, Detail: f, IsDefault: g}
}
func addressWire(v user.Address) *userpb.Address {
	return &userpb.Address{AddressId: v.ID, ReceiverName: v.ReceiverName, ReceiverPhone: v.ReceiverPhone, Province: v.Province, City: v.City, District: v.District, Detail: v.Detail, IsDefault: v.IsDefault}
}
func toStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	}
	var a *apperror.Error
	if !asApp(err, &a) {
		return status.Error(codes.Internal, "internal server error")
	}
	switch a.Code {
	case apperror.InvalidArgument:
		return status.Error(codes.InvalidArgument, a.Message)
	case apperror.Unauthenticated:
		return status.Error(codes.Unauthenticated, "authentication required")
	case apperror.NotFound:
		return status.Error(codes.NotFound, "resource not found")
	}
	return status.Error(codes.Internal, "internal server error")
}
func asApp(err error, target **apperror.Error) bool {
	for err != nil {
		if a, ok := err.(*apperror.Error); ok {
			*target = a
			return true
		}
		break
	}
	return false
}
