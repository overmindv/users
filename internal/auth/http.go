package auth

import (
	"net/http"
)

// OptionalHTTP проверяет JWT, если Authorization header присутствует.
// Resolver-методы отдельно запрещают доступ к операциям, где пользователь обязателен.
func OptionalHTTP(manager *Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			next.ServeHTTP(w, r)
			return
		}

		token, err := BearerToken(header)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		userID, err := manager.Parse(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(ContextWithUserID(r.Context(), userID)))
	})
}
