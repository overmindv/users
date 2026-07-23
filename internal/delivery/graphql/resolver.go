package graphql

import "github.com/overmindv/arcee/internal/usecase"

// Resolver хранит usecase-зависимости GraphQL transport.
type Resolver struct{ Users *usecase.UserService }
