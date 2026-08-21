package middleware

import (
	"net/http"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
)

type AuthMiddleware struct{ manager *platformauth.Manager }

func NewAuthMiddleware(manager *platformauth.Manager) AuthMiddleware {
	return AuthMiddleware{manager: manager}
}
func (m AuthMiddleware) Wrap(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("X-User-ID")
		p, err := m.manager.VerifyBearer(r.Header.Get("Authorization"), time.Now())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"UNAUTHENTICATED","message":"authentication required"}`))
			return
		}
		ctx := platformauth.ContextWithBearer(platformauth.ContextWithPrincipal(r.Context(), p), r.Header.Get("Authorization"))
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
