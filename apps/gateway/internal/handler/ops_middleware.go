package handler

import "net/http"

func RequireOperatorHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-AI-Shopping-Operator") != "true" {
			writeJSONValue(w, http.StatusForbidden, map[string]string{"code": "FORBIDDEN", "message": "operator access required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
