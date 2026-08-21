package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	userclient "github.com/yuyu945/AI-Shopping/apps/gateway/internal/userclient"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserHandler struct{ client userclient.Client }

func NewUserHandler(c userclient.Client) *UserHandler { return &UserHandler{c} }
func (h *UserHandler) Register() http.HandlerFunc {
	return h.auth(false, func(r *http.Request) (any, error) {
		var v struct{ Email, Password string }
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid request")
		}
		return h.client.Register(r.Context(), &userpb.RegisterRequest{Email: v.Email, Password: v.Password})
	})
}
func (h *UserHandler) Login() http.HandlerFunc {
	return h.auth(false, func(r *http.Request) (any, error) {
		var v struct{ Email, Password string }
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid request")
		}
		return h.client.Login(r.Context(), &userpb.LoginRequest{Email: v.Email, Password: v.Password})
	})
}
func (h *UserHandler) Profile() http.HandlerFunc {
	return h.auth(true, func(r *http.Request) (any, error) {
		if r.Method == http.MethodGet {
			return h.client.GetMyProfile(r.Context(), &userpb.GetMyProfileRequest{})
		}
		var v struct {
			Preference json.RawMessage `json:"preference_json"`
			BudgetMin  *string         `json:"budget_min"`
			BudgetMax  *string         `json:"budget_max"`
		}
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid request")
		}
		return h.client.UpdateMyProfile(r.Context(), &userpb.UpdateMyProfileRequest{PreferenceJson: v.Preference, BudgetMin: v.BudgetMin, BudgetMax: v.BudgetMax})
	})
}
func (h *UserHandler) Addresses() http.HandlerFunc {
	return h.auth(true, func(r *http.Request) (any, error) {
		if r.Method == http.MethodGet {
			return h.client.ListMyAddresses(r.Context(), &userpb.ListMyAddressesRequest{})
		}
		v, e := addressRequest(r)
		if e != nil {
			return nil, e
		}
		return h.client.CreateMyAddress(r.Context(), v)
	})
}
func (h *UserHandler) Address() http.HandlerFunc {
	return h.auth(true, func(r *http.Request) (any, error) {
		id, e := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/api/v1/users/me/addresses/"), 10, 64)
		if e != nil || id == 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid address id")
		}
		if r.Method == http.MethodDelete {
			return h.client.DeleteMyAddress(r.Context(), &userpb.DeleteMyAddressRequest{AddressId: id})
		}
		v, e := addressRequest(r)
		if e != nil {
			return nil, e
		}
		return h.client.UpdateMyAddress(r.Context(), &userpb.UpdateMyAddressRequest{AddressId: id, ReceiverName: v.ReceiverName, ReceiverPhone: v.ReceiverPhone, Province: v.Province, City: v.City, District: v.District, Detail: v.Detail, IsDefault: v.IsDefault})
	})
}
func addressRequest(r *http.Request) (*userpb.CreateMyAddressRequest, error) {
	var v userpb.CreateMyAddressRequest
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	return &v, nil
}
func (h *UserHandler) auth(_ bool, fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, e := fn(r)
		if e != nil {
			writeUserError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
func writeUserError(w http.ResponseWriter, e error) {
	code := codes.Internal
	if s, ok := status.FromError(e); ok {
		code = s.Code()
	}
	httpCode := http.StatusInternalServerError
	body := map[string]string{"code": "INTERNAL", "message": "internal server error"}
	if code == codes.InvalidArgument {
		httpCode = http.StatusBadRequest
		body = map[string]string{"code": "INVALID_ARGUMENT", "message": "invalid request"}
	} else if code == codes.Unauthenticated {
		httpCode = http.StatusUnauthorized
		body = map[string]string{"code": "UNAUTHENTICATED", "message": "authentication required"}
	} else if code == codes.NotFound {
		httpCode = http.StatusNotFound
		body = map[string]string{"code": "NOT_FOUND", "message": "resource not found"}
	} else if code == codes.Unavailable || code == codes.DeadlineExceeded {
		httpCode = http.StatusGatewayTimeout
		body = map[string]string{"code": "DEPENDENCY_TIMEOUT", "message": "user service timeout"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(body)
}
