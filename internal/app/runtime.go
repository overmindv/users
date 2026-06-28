package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/overmindv/arcee/internal/auth"
	graphqldelivery "github.com/overmindv/arcee/internal/delivery/graphql"
)

type Runtime struct{ container *Container }

func NewRuntime(container *Container) *Runtime {
	return &Runtime{container: container}
}

func (r *Runtime) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.container.Config.HTTP.Address)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}

	handler := graphqldelivery.Handler(&graphqldelivery.Resolver{Users: r.container.Users}, r.container.DB)
	server := &http.Server{
		Handler: auth.OptionalHTTP(r.container.JWT, handler), ReadTimeout: r.container.Config.HTTP.ReadTimeout,
		WriteTimeout: r.container.Config.HTTP.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		r.container.Log.Info("Arcee GraphQL server started", "address", r.container.Config.HTTP.Address)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve HTTP: %w", err)
		}
	}()

	var runErr error

	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), r.container.Config.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}

	return runErr
}
