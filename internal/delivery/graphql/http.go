package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/overmindv/arcee/internal/delivery/graphql/generated"
)

type HealthChecker interface{ Ping(context.Context) error }

func Handler(resolver *Resolver, database HealthChecker) http.Handler {
	graphqlServer := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
	graphqlServer.SetErrorPresenter(errorPresenter)

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

	return mux
}
