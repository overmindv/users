package graphql

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/google/uuid"
	"github.com/overmindv/users/internal/delivery/graphql/generated"
)

// HealthChecker задаёт contract проверки PostgreSQL для health endpoint.
type HealthChecker interface{ Ping(context.Context) error }

const requestIDHeader = "X-Request-ID"

// requestIDKey задаёт приватный ключ context для request ID.
type requestIDKey struct{}

// Handler собирает HTTP transport для GraphQL, playground и healthcheck.
// В GraphQL context добавляется request_id из gateway или генерируется новый.
func Handler(resolver *Resolver, database HealthChecker, log *slog.Logger) http.Handler {
	graphqlServer := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
	graphqlServer.SetErrorPresenter(errorPresenter(log))

	mux := http.NewServeMux()

	mux.Handle("POST /query", graphqlServer)
	mux.Handle("POST /graphql", graphqlServer)
	mux.Handle("GET /playground", playground.Handler("Arcee GraphQL", "/query"))

	health := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := database.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "database": "unavailable"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "database": "ok"})
	}

	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /healthz", health)

	return requestIDMiddleware(mux)
}

// requestIDMiddleware добавляет request_id в HTTP response и request context.
// На вход получает следующий handler, на выход возвращает middleware handler с сохранением X-Request-ID.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}

		w.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

// requestID извлекает request_id из context.
// На вход получает context запроса, на выход возвращает строковый идентификатор или пустую строку.
func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)

	return value
}
