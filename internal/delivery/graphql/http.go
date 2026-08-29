package graphql

import (
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/overmindv/users/internal/delivery/graphql/generated"
)

// Router — минимальный контракт регистрации HTTP-роутов. Реализуется как
// *parker.HTTPServer (в проде), так и *http.ServeMux (в тестах).
type Router interface {
	Handle(pattern string, handler http.Handler)
}

// Register регистрирует GraphQL transport на роутер.
// serve — HTTP middleware поверх GraphQL (JWT-auth и т.п.), оборачивает /query и /graphql,
// но не /playground. health/ready/metrics и request-id предоставляет parker.
func Register(router Router, resolver *Resolver, serve func(http.Handler) http.Handler, log *slog.Logger) {
	graphqlServer := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
	graphqlServer.SetErrorPresenter(errorPresenter(log))

	query := http.Handler(graphqlServer)
	if serve != nil {
		query = serve(query)
	}

	router.Handle("POST /query", query)
	router.Handle("POST /graphql", query)
	router.Handle("GET /playground", playground.Handler("Users GraphQL", "/query"))
}
