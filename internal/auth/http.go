package auth

import (
	"net/http"
)

// OptionalHTTP validates a token when present. Resolver guards enforce auth for
// every operation except register/login on the shared GraphQL endpoint.
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
