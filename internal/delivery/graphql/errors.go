package graphql

import (
	"context"
	"errors"
	"log/slog"

	gqlgen "github.com/99designs/gqlgen/graphql"
	"github.com/overmindv/users/internal/domain"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// errorPresenter преобразует внутренние ошибки GraphQL в публичный ответ.
// На вход получает logger, на выход возвращает presenter с обезличенным сообщением и машинным code.
func errorPresenter(log *slog.Logger) gqlgen.ErrorPresenterFunc {
	return func(ctx context.Context, err error) *gqlerror.Error {
		presented := gqlgen.DefaultErrorPresenter(ctx, err)
		if gqlErr, ok := err.(*gqlerror.Error); ok && gqlErr.Err == nil {
			presented.Message = "Не удалось выполнить действие."
			if presented.Extensions == nil {
				presented.Extensions = map[string]any{}
			}
			presented.Extensions["code"] = "GRAPHQL_VALIDATION_FAILED"
			return presented
		}

		code, message := domainError(err)
		presented.Message = message
		if presented.Extensions == nil {
			presented.Extensions = map[string]any{}
		}
		presented.Extensions["code"] = code

		if log != nil {
			log.WarnContext(ctx, "graphql error", "request_id", requestID(ctx), "code", code, "error", err)
		}

		return presented
	}
}

// domainError сопоставляет доменные ошибки Arcee с GraphQL error code.
// На вход получает исходную ошибку, на выход возвращает машинный code и пользовательское сообщение.
func domainError(err error) (string, string) {
	const genericMessage = "Не удалось выполнить действие."

	switch {
	case errors.Is(err, domain.ErrPermissionDenied), errors.Is(err, domain.ErrCannotDemoteSuperuser):
		return "FORBIDDEN", genericMessage
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials):
		return "UNAUTHENTICATED", genericMessage
	case errors.Is(err, domain.ErrUserNotFound):
		return "NOT_FOUND", genericMessage
	case errors.Is(err, domain.ErrEmailAlreadyExists), errors.Is(err, domain.ErrUsernameExists):
		return "ALREADY_EXISTS", genericMessage
	case errors.Is(err, domain.ErrInvalidEmail), errors.Is(err, domain.ErrInvalidPhone), errors.Is(err, domain.ErrInvalidUsername),
		errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidBirthDate), errors.Is(err, domain.ErrInvalidPassword), errors.Is(err, domain.ErrInvalidUserID):
		return "INVALID_ARGUMENT", genericMessage
	default:
		return "INTERNAL_SERVER_ERROR", genericMessage
	}
}
