package graphql

import (
	"context"
	"errors"

	gqlgen "github.com/99designs/gqlgen/graphql"
	"github.com/overmindv/arcee/internal/domain"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func errorPresenter(ctx context.Context, err error) *gqlerror.Error {
	presented := gqlgen.DefaultErrorPresenter(ctx, err)
	if gqlErr, ok := err.(*gqlerror.Error); ok && gqlErr.Err == nil {
		return presented
	}

	code, message := domainError(err)
	presented.Message = message
	if presented.Extensions == nil {
		presented.Extensions = map[string]any{}
	}
	presented.Extensions["code"] = code

	return presented
}

func domainError(err error) (string, string) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials):
		return "UNAUTHENTICATED", err.Error()
	case errors.Is(err, domain.ErrUserNotFound):
		return "NOT_FOUND", err.Error()
	case errors.Is(err, domain.ErrEmailAlreadyExists), errors.Is(err, domain.ErrUsernameExists):
		return "ALREADY_EXISTS", err.Error()
	case errors.Is(err, domain.ErrInvalidEmail), errors.Is(err, domain.ErrInvalidPhone), errors.Is(err, domain.ErrInvalidUsername),
		errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidBirthDate), errors.Is(err, domain.ErrInvalidPassword), errors.Is(err, domain.ErrInvalidUserID):
		return "INVALID_ARGUMENT", err.Error()
	default:
		return "INTERNAL_SERVER_ERROR", "internal server error"
	}
}
