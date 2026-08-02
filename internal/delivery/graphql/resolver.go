package graphql

import "github.com/overmindv/users/internal/usecase"

// Resolver хранит usecase-зависимости GraphQL transport.
type Resolver struct{ Users *usecase.UserService }
